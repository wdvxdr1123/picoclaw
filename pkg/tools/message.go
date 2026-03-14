package tools

import (
	"context"
	"fmt"
	"sync/atomic"
)

type SendCallback func(channel, chatID, content string) error

type MessageTool struct {
	sendCallback SendCallback
	sentInRound  atomic.Bool // Tracks whether a message was sent in the current processing round
}

type messageParams struct {
	Content string `json:"content" jsonschema:"The message content to send"`
	Channel string `json:"channel,omitempty" jsonschema:"Optional: target channel (qqbot, feishu, etc.)"`
	ChatID  string `json:"chat_id,omitempty" jsonschema:"Optional: target chat/user ID"`
}

var messageToolSpec = &ToolSpec{
	Name:        "message",
	Description: "Send a message to user on a chat channel. Use this when you want to communicate something.",
	Parameters:  schemaForParams[messageParams](),
}

func NewMessageTool() *MessageTool {
	return &MessageTool{}
}

func (t *MessageTool) Spec() *ToolSpec {
	return messageToolSpec
}

// ResetSentInRound resets the per-round send tracker.
// Called by the agent loop at the start of each inbound message processing round.
func (t *MessageTool) ResetSentInRound() {
	t.sentInRound.Store(false)
}

// HasSentInRound returns true if the message tool sent a message during the current round.
func (t *MessageTool) HasSentInRound() bool {
	return t.sentInRound.Load()
}

func (t *MessageTool) SetSendCallback(callback SendCallback) {
	t.sendCallback = callback
}

func (t *MessageTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	content, ok := args["content"].(string)
	if !ok {
		return &ToolResult{ForLLM: "content is required", IsError: true}
	}

	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)

	if channel == "" {
		channel = ToolChannel(ctx)
	}
	if chatID == "" {
		chatID = ToolChatID(ctx)
	}

	if channel == "" || chatID == "" {
		return &ToolResult{ForLLM: "No target channel/chat specified", IsError: true}
	}

	if t.sendCallback == nil {
		return &ToolResult{ForLLM: "Message sending not configured", IsError: true}
	}

	if err := t.sendCallback(channel, chatID, content); err != nil {
		return &ToolResult{
			ForLLM:  fmt.Sprintf("sending message: %v", err),
			IsError: true,
			Err:     err,
		}
	}

	t.sentInRound.Store(true)
	// Silent: user already received the message directly
	return &ToolResult{
		ForLLM: fmt.Sprintf("Message sent to %s:%s", channel, chatID),
		Silent: true,
	}
}
