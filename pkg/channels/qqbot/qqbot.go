package qqbot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/event"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"
	"golang.org/x/oauth2"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/identity"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/utils"
)

type QQChannel struct {
	*channels.BaseChannel
	config         config.QQBotConfig
	api            openapi.OpenAPI
	tokenSource    oauth2.TokenSource
	ctx            context.Context
	cancel         context.CancelFunc
	sessionManager botgo.SessionManager
	processedIDs   map[string]bool
	groupChatIDs   map[string]bool
	mu             sync.RWMutex
}

func NewQQChannel(cfg config.QQBotConfig, messageBus *bus.MessageBus) (*QQChannel, error) {
	base := channels.NewBaseChannel("qqbot", cfg, messageBus, cfg.AllowFrom,
		channels.WithGroupTrigger(config.GroupTriggerConfig{}),
		channels.WithReasoningChannelID(cfg.ReasoningChannelID),
	)

	return &QQChannel{
		BaseChannel:  base,
		config:       cfg,
		processedIDs: make(map[string]bool),
		groupChatIDs: make(map[string]bool),
	}, nil
}

func (c *QQChannel) Start(ctx context.Context) error {
	if c.config.AppID == "" || c.config.ClientSecret == "" {
		return fmt.Errorf("QQ app_id and client_secret not configured")
	}

	logger.InfoC("qqbot", "Starting QQ bot (WebSocket mode)")

	// create token source
	credentials := &token.QQBotCredentials{
		AppID:     c.config.AppID,
		AppSecret: c.config.ClientSecret,
	}
	c.tokenSource = token.NewQQBotTokenSource(credentials)

	// create child context
	c.ctx, c.cancel = context.WithCancel(ctx)

	// start auto-refresh token goroutine
	if err := token.StartRefreshAccessToken(c.ctx, c.tokenSource); err != nil {
		return fmt.Errorf("failed to start token refresh: %w", err)
	}

	// initialize OpenAPI client
	c.api = botgo.NewOpenAPI(c.config.AppID, c.tokenSource).WithTimeout(5 * time.Second)

	// register event handlers
	intent := event.RegisterHandlers(
		c.handleC2CMessage(),
		c.handleGroupATMessage(),
	)

	// get WebSocket endpoint
	wsInfo, err := c.api.WS(c.ctx, nil, "")
	if err != nil {
		return fmt.Errorf("failed to get websocket info: %w", err)
	}

	logger.InfoCF("qqbot", "Got WebSocket info", map[string]any{
		"shards": wsInfo.Shards,
	})

	// create and save sessionManager
	c.sessionManager = botgo.NewSessionManager()

	// start WebSocket connection in goroutine to avoid blocking
	go func() {
		if err := c.sessionManager.Start(wsInfo, c.tokenSource, &intent); err != nil {
			logger.ErrorCF("qqbot", "WebSocket session error", map[string]any{
				"error": err.Error(),
			})
			c.SetRunning(false)
		}
	}()

	c.SetRunning(true)
	logger.InfoC("qqbot", "QQ bot started successfully")

	return nil
}

func (c *QQChannel) Stop(ctx context.Context) error {
	logger.InfoC("qqbot", "Stopping QQ bot")
	c.SetRunning(false)

	if c.cancel != nil {
		c.cancel()
	}

	return nil
}

func (c *QQChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	msgToCreate := &dto.MessageToCreate{
		MsgType:  dto.MarkdownMsg,
		Markdown: &dto.Markdown{Content: msg.Content},
	}

	_, err := c.postMessage(ctx, msg.ChatID, msgToCreate)
	if err != nil {
		logger.ErrorCF("qqbot", "Failed to send message", map[string]any{
			"error": err.Error(),
		})
		return fmt.Errorf("qqbot send: %w", channels.ErrTemporary)
	}

	return nil
}

func (c *QQChannel) SendMedia(ctx context.Context, msg bus.OutboundMediaMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	store := c.GetMediaStore()
	if store == nil {
		return fmt.Errorf("no media store available: %w", channels.ErrSendFailed)
	}

	for _, part := range msg.Parts {
		if err := c.sendMediaPart(ctx, msg.ChatID, part, store); err != nil {
			return err
		}
	}
	return nil
}

// handleC2CMessage handles QQ private messages
func (c *QQChannel) handleC2CMessage() event.C2CMessageEventHandler {
	return func(event *dto.WSPayload, data *dto.WSC2CMessageData) error {
		// deduplication check
		if c.isDuplicate(data.ID) {
			return nil
		}

		// extract user info
		var senderID string
		if data.Author != nil && data.Author.ID != "" {
			senderID = data.Author.ID
		} else {
			logger.WarnC("qqbot", "Received message with no sender ID")
			return nil
		}

		store := c.GetMediaStore()
		mediaRefs := c.downloadInboundAttachments(data.ID, senderID, data.Attachments, store)

		// extract message content
		content := data.Content
		content = appendAttachmentTags(content, mediaRefs, data.Attachments)
		if content == "" && len(mediaRefs) == 0 {
			logger.DebugC("qqbot", "Received empty message, ignoring")
			return nil
		}

		logger.InfoCF("qqbot", "Received C2C message", map[string]any{
			"sender": senderID,
			"length": len(content),
		})

		// 转发到消息总线
		metadata := map[string]string{}

		sender := bus.SenderInfo{
			Platform:    "qqbot",
			PlatformID:  data.Author.ID,
			CanonicalID: identity.BuildCanonicalID("qqbot", data.Author.ID),
		}

		if !c.IsAllowedSender(sender) {
			return nil
		}

		c.HandleMessage(c.ctx,
			bus.Peer{Kind: "direct", ID: senderID},
			data.ID,
			senderID,
			senderID,
			content,
			mediaRefs,
			metadata,
			sender,
		)

		return nil
	}
}

// handleGroupATMessage handles QQ group @ messages
func (c *QQChannel) handleGroupATMessage() event.GroupATMessageEventHandler {
	return func(event *dto.WSPayload, data *dto.WSGroupATMessageData) error {
		// deduplication check
		if c.isDuplicate(data.ID) {
			return nil
		}

		// extract user info
		var senderID string
		if data.Author != nil && data.Author.ID != "" {
			senderID = data.Author.ID
		} else {
			logger.WarnC("qqbot", "Received group message with no sender ID")
			return nil
		}

		store := c.GetMediaStore()
		mediaRefs := c.downloadInboundAttachments(data.ID, data.GroupID, data.Attachments, store)

		// extract message content (remove @ bot part)
		content := data.Content
		content = appendAttachmentTags(content, mediaRefs, data.Attachments)
		if content == "" && len(mediaRefs) == 0 {
			logger.DebugC("qqbot", "Received empty group message, ignoring")
			return nil
		}

		// GroupAT event means bot is always mentioned; apply group trigger filtering
		respond, cleaned := c.ShouldRespondInGroup(true, content)
		if !respond {
			return nil
		}
		content = cleaned

		logger.InfoCF("qqbot", "Received group AT message", map[string]any{
			"sender": senderID,
			"group":  data.GroupID,
			"length": len(content),
		})
		c.rememberGroupChat(data.GroupID)

		// 转发到消息总线（使用 GroupID 作为 ChatID）
		metadata := map[string]string{
			"group_id": data.GroupID,
		}

		sender := bus.SenderInfo{
			Platform:    "qqbot",
			PlatformID:  data.Author.ID,
			CanonicalID: identity.BuildCanonicalID("qqbot", data.Author.ID),
		}

		if !c.IsAllowedSender(sender) {
			return nil
		}

		c.HandleMessage(c.ctx,
			bus.Peer{Kind: "group", ID: data.GroupID},
			data.ID,
			senderID,
			data.GroupID,
			content,
			mediaRefs,
			metadata,
			sender,
		)

		return nil
	}
}

// isDuplicate 检查消息是否重复
func (c *QQChannel) isDuplicate(messageID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.processedIDs[messageID] {
		return true
	}

	c.processedIDs[messageID] = true

	// 简单清理：限制 map 大小
	if len(c.processedIDs) > 10000 {
		// 清空一半
		count := 0
		for id := range c.processedIDs {
			if count >= 5000 {
				break
			}
			delete(c.processedIDs, id)
			count++
		}
	}

	return false
}

func (c *QQChannel) postMessage(ctx context.Context, chatID string, msg dto.APIMessage) (*dto.Message, error) {
	if c.isGroupChat(chatID) {
		return c.api.PostGroupMessage(ctx, chatID, msg)
	}
	return c.api.PostC2CMessage(ctx, chatID, msg)
}

func (c *QQChannel) sendMediaPart(ctx context.Context, chatID string, part bus.MediaPart, store media.MediaStore) error {
	mediaURL, caption, err := c.resolveOutboundMedia(part, store)
	if err != nil {
		logger.ErrorCF("qqbot", "Failed to resolve outbound media", map[string]any{
			"ref":   part.Ref,
			"error": err.Error(),
		})
		return fmt.Errorf("qqbot send media: %w", channels.ErrSendFailed)
	}

	fileType := uint64(1)
	switch part.Type {
	case "video":
		fileType = 2
	case "audio":
		fileType = 3
	}

	rich := &dto.RichMediaMessage{
		FileType:   fileType,
		URL:        mediaURL,
		SrvSendMsg: true,
		Content:    caption,
	}
	if _, err := c.postMessage(ctx, chatID, rich); err != nil {
		logger.ErrorCF("qqbot", "Failed to send rich media", map[string]any{
			"chat_id": chatID,
			"error":   err.Error(),
		})
		return fmt.Errorf("qqbot send media: %w", channels.ErrTemporary)
	}
	return nil
}

func (c *QQChannel) resolveOutboundMedia(part bus.MediaPart, store media.MediaStore) (string, string, error) {
	caption := part.Caption
	ref := strings.TrimSpace(part.Ref)
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref, caption, nil
	}

	if !strings.HasPrefix(ref, "media://") {
		return "", "", fmt.Errorf("unsupported media ref: %s", ref)
	}

	localPath, meta, err := store.ResolveWithMeta(ref)
	if err != nil {
		return "", "", err
	}
	if caption == "" {
		caption = meta.Filename
	}
	return "", "", fmt.Errorf("qqbot outbound local media is not supported without a public URL (resolved path: %s)", localPath)
}

func (c *QQChannel) downloadInboundAttachments(
	messageID, chatID string,
	attachments []*dto.MessageAttachment,
	store media.MediaStore,
) []string {
	if store == nil || len(attachments) == 0 {
		return nil
	}

	scope := channels.BuildMediaScope("qqbot", chatID, messageID)
	refs := make([]string, 0, len(attachments))

	for _, att := range attachments {
		if att == nil || strings.TrimSpace(att.URL) == "" {
			continue
		}
		filename := att.FileName
		if filename == "" {
			filename = "attachment"
			if ext := extensionFromContentType(att.ContentType); ext != "" {
				filename += ext
			}
		}
		localPath := utils.DownloadFile(att.URL, filename, utils.DownloadOptions{LoggerPrefix: "qqbot"})
		if localPath == "" {
			continue
		}
		ref, err := store.Store(localPath, media.MediaMeta{
			Filename:    filename,
			ContentType: att.ContentType,
			Source:      "qqbot",
		}, scope)
		if err != nil {
			logger.ErrorCF("qqbot", "Failed to store inbound attachment", map[string]any{
				"url":   att.URL,
				"error": err.Error(),
			})
			_ = os.Remove(localPath)
			continue
		}
		refs = append(refs, ref)
	}

	return refs
}

func appendAttachmentTags(content string, mediaRefs []string, attachments []*dto.MessageAttachment) string {
	if len(mediaRefs) == 0 {
		return strings.TrimSpace(content)
	}

	tags := make([]string, 0, len(mediaRefs))
	for i := range mediaRefs {
		tag := "[attachment]"
		if i < len(attachments) && attachments[i] != nil {
			switch {
			case strings.HasPrefix(strings.ToLower(attachments[i].ContentType), "image/"):
				tag = "[image: photo]"
			case strings.HasPrefix(strings.ToLower(attachments[i].ContentType), "video/"):
				tag = "[video]"
			case strings.HasPrefix(strings.ToLower(attachments[i].ContentType), "audio/"), attachments[i].ContentType == "voice":
				tag = "[voice]"
			}
		}
		tags = append(tags, tag)
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return strings.Join(tags, " ")
	}
	return content + " " + strings.Join(tags, " ")
}

func extensionFromContentType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	default:
		return filepath.Ext(contentType)
	}
}

func (c *QQChannel) rememberGroupChat(chatID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.groupChatIDs[chatID] = true
}

func (c *QQChannel) isGroupChat(chatID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.groupChatIDs[chatID]
}
