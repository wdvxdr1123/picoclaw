package qqbot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/h2non/filetype"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/identity"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/utils"
)

const (
	qqBotTokenURL   = "https://bots.qq.com/app/getAppAccessToken"
	qqBotAPIBase    = "https://api.sgroup.qq.com"
	qqBotGatewayURL = "/gateway"

	qqBotIntentDirectMessage = 1 << 12
	qqBotIntentGroupAndC2C   = 1 << 25
	qqBotDefaultIntents      = qqBotIntentDirectMessage | qqBotIntentGroupAndC2C

	qqBotMsgSeqBase            = 1000000
	qqBotDefaultChunkLimit     = 5000
	qqBotDefaultFileSizeMB     = 100
	qqBotDefaultMediaTimeoutMS = 30000
)

var (
	qqBotThinkBlockRe = regexp.MustCompile(`(?is)<think\b[^>]*>.*?</think>`)
	qqBotFinalBlockRe = regexp.MustCompile(`(?is)<final\b[^>]*>(.*?)</final>`)
)

type targetKind string

const targetKindC2C targetKind = "c2c"

type qqbotTarget struct {
	kind targetKind
	id   string
}

type qqbotGatewayPayload struct {
	Op      int             `json:"op"`
	Type    string          `json:"t,omitempty"`
	Seq     *uint32         `json:"s,omitempty"`
	EventID string          `json:"id,omitempty"`
	Data    json.RawMessage `json:"d,omitempty"`
}

type qqbotReadyData struct {
	SessionID string `json:"session_id"`
}

type qqbotHelloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type qqbotTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (r *qqbotTokenResponse) UnmarshalJSON(data []byte) error {
	type rawTokenResponse struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   any    `json:"expires_in"`
	}

	var raw rawTokenResponse
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	r.AccessToken = raw.AccessToken
	expiresIn, err := parseExpiresIn(raw.ExpiresIn)
	if err != nil {
		return err
	}
	r.ExpiresIn = expiresIn
	return nil
}

func parseExpiresIn(v any) (int, error) {
	switch v := v.(type) {
	case float64:
		return int(v), nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("parse expires_in: %w", err)
		}
		return n, nil
	case nil:
		return 0, nil
	default:
		return 0, fmt.Errorf("unsupported expires_in type %T", v)
	}
}

type qqbotUploadResponse struct {
	FileInfo string `json:"file_info"`
}

type qqbotAttachment struct {
	URL         string
	Filename    string
	ContentType string
	Size        int64
}

type QQBotChannel struct {
	*channels.BaseChannel

	config config.QQBotConfig
	client *http.Client

	ctx    context.Context
	cancel context.CancelFunc

	conn    *websocket.Conn
	connMu  sync.Mutex
	writeMu sync.Mutex

	tokenMu      sync.RWMutex
	accessToken  string
	tokenExpires time.Time

	sessionMu sync.RWMutex
	sessionID string

	lastSeq atomic.Uint32

	processedMu  sync.Mutex
	processedIDs map[string]time.Time

	msgSeqMu sync.Mutex
	msgSeq   map[string]uint32
}

func NewQQBotChannel(cfg config.QQBotConfig, messageBus *bus.MessageBus) (*QQBotChannel, error) {
	chunkLimit := cfg.TextChunkLimit
	if chunkLimit <= 0 {
		chunkLimit = qqBotDefaultChunkLimit
	}

	base := channels.NewBaseChannel(
		"qqbot",
		cfg,
		messageBus,
		nil,
		channels.WithMaxMessageLength(chunkLimit),
		channels.WithReasoningChannelID(cfg.ReasoningChannelID),
	)

	return &QQBotChannel{
		BaseChannel:  base,
		config:       cfg,
		client:       &http.Client{Timeout: 30 * time.Second},
		processedIDs: make(map[string]time.Time),
		msgSeq:       make(map[string]uint32),
	}, nil
}

func (c *QQBotChannel) Start(ctx context.Context) error {
	if strings.TrimSpace(c.config.AppID) == "" || strings.TrimSpace(c.config.ClientSecret) == "" {
		return fmt.Errorf("qqbot app_id and client_secret not configured")
	}

	logger.InfoC("qqbot", "Starting QQ Bot channel")

	c.ctx, c.cancel = context.WithCancel(ctx)
	readyCh := make(chan struct{})
	go c.gatewayLoop(readyCh)

	select {
	case <-readyCh:
		c.SetRunning(true)
		logger.InfoC("qqbot", "QQ Bot channel started")
		return nil
	case <-time.After(20 * time.Second):
		if c.cancel != nil {
			c.cancel()
		}
		return fmt.Errorf("qqbot gateway ready timeout")
	case <-ctx.Done():
		if c.cancel != nil {
			c.cancel()
		}
		return ctx.Err()
	}
}

func (c *QQBotChannel) Stop(ctx context.Context) error {
	logger.InfoC("qqbot", "Stopping QQ Bot channel")
	if c.cancel != nil {
		c.cancel()
	}
	c.closeConn()
	c.SetRunning(false)
	return nil
}

func (c *QQBotChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	text := sanitizeQQBotOutboundText(msg.Content)
	if text == "" {
		return nil
	}

	target, err := parseQQBotTarget(msg.ChatID)
	if err != nil {
		return fmt.Errorf("invalid qqbot target %q: %w", msg.ChatID, channels.ErrSendFailed)
	}

	accessToken, err := c.getAccessToken(ctx)
	if err != nil {
		return err
	}

	switch target.kind {
	default:
		body := map[string]any{"msg_seq": c.nextMsgSeq("user:" + target.id)}
		if c.config.MarkdownSupport {
			body["msg_type"] = 2
			body["markdown"] = map[string]any{"content": text}
		} else {
			body["msg_type"] = 0
			body["content"] = text
		}
		if err := c.postJSON(ctx, accessToken, fmt.Sprintf("/v2/users/%s/messages", url.PathEscape(target.id)), body, nil); err != nil {
			return err
		}
		return nil
	}
}

func (c *QQBotChannel) SendMedia(ctx context.Context, msg bus.OutboundMediaMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	target, err := parseQQBotTarget(msg.ChatID)
	if err != nil {
		return fmt.Errorf("invalid qqbot target %q: %w", msg.ChatID, channels.ErrSendFailed)
	}

	store := c.GetMediaStore()
	if store == nil {
		return fmt.Errorf("no media store available: %w", channels.ErrSendFailed)
	}

	accessToken, err := c.getAccessToken(ctx)
	if err != nil {
		return err
	}

	for _, part := range msg.Parts {
		localPath, meta, err := store.ResolveWithMeta(part.Ref)
		if err != nil {
			logger.ErrorCF("qqbot", "Failed to resolve media ref", map[string]any{"ref": part.Ref, "error": err.Error()})
			continue
		}

		fileInfo, err := c.uploadMedia(ctx, accessToken, target, localPath, meta, part)
		if err != nil {
			logger.ErrorCF("qqbot", "Failed to upload qqbot media", map[string]any{"path": localPath, "error": err.Error()})
			if strings.TrimSpace(part.Caption) != "" {
				if sendErr := c.Send(ctx, bus.OutboundMessage{Channel: msg.Channel, ChatID: msg.ChatID, Content: part.Caption}); sendErr != nil {
					return sendErr
				}
			}
			continue
		}

		content := strings.TrimSpace(part.Caption)
		body := map[string]any{
			"msg_type": 7,
			"media": map[string]any{
				"file_info": fileInfo,
			},
		}
		body["msg_seq"] = c.nextMsgSeq(string(target.kind) + ":" + target.id)
		if content != "" {
			body["content"] = content
		}
		if err := c.postJSON(ctx, accessToken, fmt.Sprintf("/v2/users/%s/messages", url.PathEscape(target.id)), body, nil); err != nil {
			return err
		}
	}

	return nil
}

func (c *QQBotChannel) gatewayLoop(readyCh chan struct{}) {
	var readyOnce sync.Once
	signalReady := func() {
		readyOnce.Do(func() { close(readyCh) })
	}

	delays := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second}
	attempt := 0
	for {
		if c.ctx.Err() != nil {
			return
		}
		err := c.runGatewaySession(signalReady)
		if c.ctx.Err() != nil {
			return
		}
		logger.WarnCF("qqbot", "Gateway session ended", map[string]any{"error": errString(err), "attempt": attempt + 1})
		delay := delays[attempt]
		if attempt < len(delays)-1 {
			attempt++
		}
		select {
		case <-time.After(delay):
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *QQBotChannel) runGatewaySession(signalReady func()) error {
	accessToken, err := c.getAccessToken(c.ctx)
	if err != nil {
		return err
	}

	gatewayURL, err := c.getGatewayURL(c.ctx, accessToken)
	if err != nil {
		return err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(c.ctx, gatewayURL, nil)
	if err != nil {
		return channels.ClassifyNetError(err)
	}
	conn.SetReadLimit(8 << 20)
	c.setConn(conn)
	defer c.closeConn()

	stopHeartbeat := func() {}
	defer stopHeartbeat()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, netErrClosed) || c.ctx.Err() != nil {
				return nil
			}
			return channels.ClassifyNetError(err)
		}

		var payload qqbotGatewayPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			logger.WarnCF("qqbot", "Failed to parse gateway payload", map[string]any{"error": err.Error()})
			continue
		}
		if payload.Seq != nil {
			c.lastSeq.Store(*payload.Seq)
		}

		switch payload.Op {
		case 10:
			var hello qqbotHelloData
			if err := json.Unmarshal(payload.Data, &hello); err != nil {
				return fmt.Errorf("parse hello: %w", err)
			}
			stopHeartbeat()
			stopHeartbeat = c.startHeartbeat(conn, time.Duration(hello.HeartbeatInterval)*time.Millisecond)
			sessionID := c.getSessionID()
			if sessionID != "" && c.lastSeq.Load() > 0 {
				if err := c.writeGatewayPayload(conn, map[string]any{
					"op": 6,
					"d": map[string]any{
						"token":      "QQBot " + accessToken,
						"session_id": sessionID,
						"seq":        c.lastSeq.Load(),
					},
				}); err != nil {
					return err
				}
			} else {
				if err := c.writeGatewayPayload(conn, map[string]any{
					"op": 2,
					"d": map[string]any{
						"token":   "QQBot " + accessToken,
						"intents": qqBotDefaultIntents,
						"shard":   []uint32{0, 1},
					},
				}); err != nil {
					return err
				}
			}
		case 11:
			continue
		case 7:
			return fmt.Errorf("qqbot server requested reconnect")
		case 9:
			c.clearSession()
			c.expireToken()
			return fmt.Errorf("qqbot invalid session")
		case 0:
			switch payload.Type {
			case "READY":
				var ready qqbotReadyData
				if err := json.Unmarshal(payload.Data, &ready); err == nil && ready.SessionID != "" {
					c.setSessionID(ready.SessionID)
				}
				signalReady()
			case "RESUMED":
				signalReady()
			default:
				if err := c.handleDispatchEvent(payload.Type, payload.EventID, payload.Data); err != nil {
					logger.WarnCF("qqbot", "Failed to handle dispatch event", map[string]any{"type": payload.Type, "error": err.Error()})
				}
			}
		}
	}
}

var netErrClosed = errors.New("use of closed network connection")

func (c *QQBotChannel) startHeartbeat(conn *websocket.Conn, interval time.Duration) func() {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = c.writeGatewayPayload(conn, map[string]any{"op": 1, "d": c.lastSeq.Load()})
			case <-stopCh:
				return
			case <-c.ctx.Done():
				return
			}
		}
	}()
	return func() { close(stopCh) }
}

func (c *QQBotChannel) handleDispatchEvent(eventType, eventID string, raw json.RawMessage) error {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}

	switch eventType {
	case "C2C_MESSAGE_CREATE":
		return c.handleDirectEvent(payload, eventID, false)
	case "DIRECT_MESSAGE_CREATE":
		return c.handleDirectEvent(payload, eventID, true)
	default:
		return nil
	}
}

func (c *QQBotChannel) handleDirectEvent(payload map[string]any, eventID string, isGuildDM bool) error {
	messageID := valueString(payload, "id")
	if c.isDuplicate(messageID) {
		return nil
	}

	author := valueMap(payload, "author")
	senderID := valueString(author, "user_openid")
	if senderID == "" {
		senderID = valueString(author, "id")
	}
	if senderID == "" {
		return nil
	}

	sender := bus.SenderInfo{
		Platform:    "qqbot",
		PlatformID:  senderID,
		CanonicalID: identity.BuildCanonicalID("qqbot", senderID),
		DisplayName: firstNonEmpty(valueString(author, "username"), valueString(author, "nickname")),
	}
	if !c.isDirectAllowed(sender) {
		return nil
	}

	chatID := "user:" + senderID
	content, mediaRefs := c.buildInboundContent(c.ctx, chatID, messageID, strings.TrimSpace(valueString(payload, "content")), parseAttachments(payload))
	if content == "" && len(mediaRefs) == 0 {
		return nil
	}

	metadata := map[string]string{
		"event_id":  eventID,
		"chat_type": "direct",
	}
	if isGuildDM {
		metadata["guild_id"] = valueString(payload, "guild_id")
	}

	c.HandleMessage(
		c.ctx,
		bus.Peer{Kind: "direct", ID: senderID},
		messageID,
		senderID,
		chatID,
		content,
		mediaRefs,
		metadata,
		sender,
	)
	return nil
}

func (c *QQBotChannel) buildInboundContent(ctx context.Context, chatID, messageID, content string, attachments []qqbotAttachment) (string, []string) {
	if len(attachments) == 0 {
		return content, nil
	}

	store := c.GetMediaStore()
	scope := channels.BuildMediaScope("qqbot", chatID, messageID)
	lines := make([]string, 0, len(attachments))
	mediaRefs := make([]string, 0, len(attachments))
	for _, att := range attachments {
		label := formatAttachmentLabel(att)
		if store != nil {
			ref, err := c.downloadAndStoreAttachment(ctx, store, scope, att)
			if err != nil {
				logger.WarnCF("qqbot", "Failed to cache inbound attachment", map[string]any{"url": att.URL, "error": err.Error()})
			} else if ref != "" {
				mediaRefs = append(mediaRefs, ref)
			}
		}
		lines = append(lines, label)
	}

	attachmentBlock := strings.Join(lines, "\n")
	if content == "" {
		return attachmentBlock, mediaRefs
	}
	if attachmentBlock == "" {
		return content, mediaRefs
	}
	return content + "\n" + attachmentBlock, mediaRefs
}

func (c *QQBotChannel) downloadAndStoreAttachment(ctx context.Context, store media.MediaStore, scope string, att qqbotAttachment) (string, error) {
	attachmentURL := normalizeAttachmentURL(att.URL)
	if attachmentURL == "" {
		return "", nil
	}

	maxBytes := int64(c.maxFileSizeBytes())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, attachmentURL, nil)
	if err != nil {
		return "", err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(c.mediaTimeoutMS())*time.Millisecond)
	defer cancel()

	path, err := utils.DownloadToFile(timeoutCtx, c.client, req, maxBytes)
	if err != nil {
		return "", err
	}

	filename := strings.TrimSpace(att.Filename)
	if filename == "" {
		filename = deriveFilename(attachmentURL, path)
	}
	ref, err := store.Store(path, media.MediaMeta{
		Filename:    filename,
		ContentType: att.ContentType,
		Source:      "qqbot",
	}, scope)
	if err != nil {
		return "", err
	}
	return ref, nil
}

func (c *QQBotChannel) uploadMedia(ctx context.Context, accessToken string, target qqbotTarget, localPath string, meta media.MediaMeta, part bus.MediaPart) (string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", fmt.Errorf("stat media: %w", err)
	}
	if info.Size() > int64(c.maxFileSizeBytes()) {
		return "", fmt.Errorf("qqbot media exceeds limit")
	}

	fileData, err := os.ReadFile(localPath)
	if err != nil {
		return "", fmt.Errorf("read media: %w", err)
	}

	fileType, sendAsFileName := detectQQBotFileType(localPath, meta, part)
	body := map[string]any{
		"file_type":    fileType,
		"file_data":    base64.StdEncoding.EncodeToString(fileData),
		"srv_send_msg": false,
	}
	if sendAsFileName != "" && fileType == 4 {
		body["file_name"] = sendAsFileName
	}

	var resp qqbotUploadResponse
	if err := c.postJSON(ctx, accessToken, fmt.Sprintf("/v2/users/%s/files", url.PathEscape(target.id)), body, &resp); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.FileInfo) == "" {
		return "", fmt.Errorf("qqbot upload response missing file_info")
	}
	return resp.FileInfo, nil
}

func (c *QQBotChannel) getAccessToken(ctx context.Context) (string, error) {
	c.tokenMu.RLock()
	if c.accessToken != "" && time.Until(c.tokenExpires) > 5*time.Minute {
		token := c.accessToken
		c.tokenMu.RUnlock()
		return token, nil
	}
	c.tokenMu.RUnlock()

	payload := map[string]string{
		"appId":        c.config.AppID,
		"clientSecret": c.config.ClientSecret,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qqBotTokenURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", channels.ClassifyNetError(err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read qqbot token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", channels.ClassifySendError(resp.StatusCode, fmt.Errorf("qqbot token API error: %s", summarizeBody(respBody)))
	}

	var tokenResp qqbotTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("parse qqbot token response: %w", err)
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return "", fmt.Errorf("qqbot token response missing access_token")
	}

	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 7200
	}

	c.tokenMu.Lock()
	c.accessToken = tokenResp.AccessToken
	c.tokenExpires = time.Now().Add(time.Duration(expiresIn) * time.Second)
	c.tokenMu.Unlock()
	return tokenResp.AccessToken, nil
}

func (c *QQBotChannel) getGatewayURL(ctx context.Context, accessToken string) (string, error) {
	var resp struct {
		URL string `json:"url"`
	}
	if err := c.getJSON(ctx, accessToken, qqBotGatewayURL, &resp); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.URL) == "" {
		return "", fmt.Errorf("qqbot gateway response missing url")
	}
	return resp.URL, nil
}

func (c *QQBotChannel) getJSON(ctx context.Context, accessToken, apiPath string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qqBotAPIBase+apiPath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "QQBot "+accessToken)
	return c.doRequest(req, out)
}

func (c *QQBotChannel) postJSON(ctx context.Context, accessToken, apiPath string, payload any, out any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qqBotAPIBase+apiPath, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "QQBot "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req, out)
}

func (c *QQBotChannel) doRequest(req *http.Request, out any) error {
	resp, err := c.client.Do(req)
	if err != nil {
		return channels.ClassifyNetError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read qqbot response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return channels.ClassifySendError(resp.StatusCode, fmt.Errorf("qqbot API error: %s", summarizeBody(body)))
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse qqbot response: %w", err)
	}
	return nil
}

func (c *QQBotChannel) writeGatewayPayload(conn *websocket.Conn, payload any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := conn.WriteJSON(payload); err != nil {
		return channels.ClassifyNetError(err)
	}
	return nil
}

func (c *QQBotChannel) nextMsgSeq(key string) int {
	c.msgSeqMu.Lock()
	defer c.msgSeqMu.Unlock()
	next := c.msgSeq[key] + 1
	c.msgSeq[key] = next
	trimMap(c.msgSeq, 1000, 500)
	return qqBotMsgSeqBase + int(next)
}

func (c *QQBotChannel) isDuplicate(messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false
	}
	now := time.Now()
	c.processedMu.Lock()
	defer c.processedMu.Unlock()
	if _, exists := c.processedIDs[messageID]; exists {
		return true
	}
	c.processedIDs[messageID] = now
	trimMapByTime(c.processedIDs, 10000, now.Add(-30*time.Minute))
	return false
}

func (c *QQBotChannel) isDirectAllowed(sender bus.SenderInfo) bool {
	policy := strings.ToLower(strings.TrimSpace(c.config.DmPolicy))
	if policy == "pairing" || policy == "allowlist" {
		for _, allowed := range c.config.AllowFrom {
			if identity.MatchAllowed(sender, allowed) {
				return true
			}
		}
		return false
	}
	return true
}

func (c *QQBotChannel) setConn(conn *websocket.Conn) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = conn
}

func (c *QQBotChannel) closeConn() {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *QQBotChannel) setSessionID(sessionID string) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	c.sessionID = sessionID
}

func (c *QQBotChannel) getSessionID() string {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.sessionID
}

func (c *QQBotChannel) clearSession() {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	c.sessionID = ""
	c.lastSeq.Store(0)
}

func (c *QQBotChannel) expireToken() {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.accessToken = ""
	c.tokenExpires = time.Time{}
}

func (c *QQBotChannel) maxFileSizeBytes() int {
	mb := c.config.MaxFileSizeMB
	if mb <= 0 {
		mb = qqBotDefaultFileSizeMB
	}
	return mb * 1024 * 1024
}

func (c *QQBotChannel) mediaTimeoutMS() int {
	ms := c.config.MediaTimeoutMs
	if ms <= 0 {
		ms = qqBotDefaultMediaTimeoutMS
	}
	return ms
}

func parseQQBotTarget(raw string) (qqbotTarget, error) {
	value := strings.TrimSpace(raw)
	value, _ = strings.CutPrefix(value, "qqbot:")
	value = strings.TrimSpace(value)
	if value == "" {
		return qqbotTarget{}, fmt.Errorf("empty target")
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "group:") || strings.HasPrefix(lower, "channel:") {
		return qqbotTarget{}, fmt.Errorf("qqbot only supports c2c targets")
	}
	if id, ok := strings.CutPrefix(lower, "user:"); ok {
		value = id
	} else if id, ok := strings.CutPrefix(lower, "c2c:"); ok {
		value = id
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return qqbotTarget{}, fmt.Errorf("empty target id")
	}
	return qqbotTarget{kind: targetKindC2C, id: value}, nil
}

func sanitizeQQBotOutboundText(text string) string {
	text = qqBotThinkBlockRe.ReplaceAllString(text, "")
	if matches := qqBotFinalBlockRe.FindStringSubmatch(text); len(matches) == 2 {
		text = matches[1]
	}
	text = strings.TrimSpace(text)
	if strings.EqualFold(text, "NO_REPLY") {
		return ""
	}
	return text
}

func parseAttachments(payload map[string]any) []qqbotAttachment {
	raw, ok := payload["attachments"].([]any)
	if !ok {
		return nil
	}
	items := make([]qqbotAttachment, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, qqbotAttachment{
			URL:         normalizeAttachmentURL(valueString(m, "url")),
			Filename:    valueString(m, "filename"),
			ContentType: valueString(m, "content_type"),
			Size:        int64(valueNumber(m, "size")),
		})
	}
	return items
}

func normalizeAttachmentURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return "https:" + strings.TrimPrefix(raw, "https:")
}

func formatAttachmentLabel(att qqbotAttachment) string {
	name := strings.TrimSpace(att.Filename)
	if name == "" {
		name = deriveFilename(att.URL, "")
	}
	switch detectAttachmentKind(name, att.ContentType) {
	case "image":
		return "[image: " + name + "]"
	case "audio":
		return "[voice: " + name + "]"
	case "video":
		return "[video: " + name + "]"
	default:
		return "[file: " + name + "]"
	}
}

func detectAttachmentKind(filename, contentType string) string {
	lowerType := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(lowerType, "image/") {
		return "image"
	}
	if strings.HasPrefix(lowerType, "audio/") || lowerType == "voice" {
		return "audio"
	}
	if strings.HasPrefix(lowerType, "video/") {
		return "video"
	}
	lowerName := strings.ToLower(filename)
	switch filepath.Ext(lowerName) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return "image"
	case ".mp3", ".wav", ".ogg", ".m4a", ".aac", ".silk":
		return "audio"
	case ".mp4", ".mov", ".avi", ".webm", ".mkv":
		return "video"
	default:
		return "file"
	}
}

func detectQQBotFileType(localPath string, meta media.MediaMeta, part bus.MediaPart) (int, string) {
	filename := firstNonEmpty(part.Filename, meta.Filename, filepath.Base(localPath))
	kind := detectAttachmentKind(filename, firstNonEmpty(part.ContentType, meta.ContentType))
	if match, err := filetype.MatchFile(localPath); err == nil && match != filetype.Unknown {
		switch {
		case strings.HasPrefix(match.MIME.Value, "image/"):
			kind = "image"
		case strings.HasPrefix(match.MIME.Value, "video/"):
			kind = "video"
		}
	}
	switch kind {
	case "image":
		return 1, ""
	case "video":
		return 2, ""
	case "audio":
		if strings.EqualFold(filepath.Ext(filename), ".silk") {
			return 3, ""
		}
		return 4, filename
	default:
		return 4, filename
	}
}

func summarizeBody(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	runes := []rune(trimmed)
	if len(runes) > 300 {
		return string(runes[:300]) + "..."
	}
	return trimmed
}

func deriveFilename(source string, fallbackPath string) string {
	if u, err := url.Parse(source); err == nil {
		if base := path.Base(u.Path); base != "." && base != "/" && base != "" {
			return base
		}
	}
	if fallbackPath != "" {
		return filepath.Base(fallbackPath)
	}
	return "attachment"
}

func valueMap(m map[string]any, key string) map[string]any {
	value, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return value
}

func valueString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	value := m[key]
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func valueNumber(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if v := strings.TrimSpace(value); v != "" {
			return v
		}
	}
	return ""
}

func trimMap(m map[string]uint32, maxSize, deleteCount int) {
	if len(m) <= maxSize {
		return
	}
	count := 0
	for k := range m {
		delete(m, k)
		count++
		if count >= deleteCount {
			break
		}
	}
}

func trimMapByTime(m map[string]time.Time, maxSize int, cutoff time.Time) {
	if len(m) <= maxSize {
		return
	}
	for id, seenAt := range m {
		if seenAt.Before(cutoff) {
			delete(m, id)
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
