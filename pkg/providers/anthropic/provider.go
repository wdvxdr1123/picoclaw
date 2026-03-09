package anthropicprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/sipeed/picoclaw/pkg/providers/protocoltypes"
)

type (
	ToolCall               = protocoltypes.ToolCall
	FunctionCall           = protocoltypes.FunctionCall
	LLMResponse            = protocoltypes.LLMResponse
	UsageInfo              = protocoltypes.UsageInfo
	Message                = protocoltypes.Message
	ToolDefinition         = protocoltypes.ToolDefinition
	ToolFunctionDefinition = protocoltypes.ToolFunctionDefinition
)

const (
	defaultBaseURL      = "https://api.anthropic.com"
	anthropicBetaHeader = "oauth-2025-04-20"
)

type Provider struct {
	client      *anthropic.Client
	tokenSource func() (string, error)
	baseURL     string
}

// SupportsThinking implements providers.ThinkingCapable.
func (p *Provider) SupportsThinking() bool { return true }

func NewProvider(token string) *Provider {
	return NewProviderWithBaseURL(token, "")
}

func NewProviderWithBaseURL(token, apiBase string) *Provider {
	baseURL := normalizeBaseURL(apiBase)
	client := anthropic.NewClient(
		option.WithAuthToken(token),
		option.WithBaseURL(baseURL),
	)
	return &Provider{
		client:  &client,
		baseURL: baseURL,
	}
}

func NewProviderWithClient(client *anthropic.Client) *Provider {
	return &Provider{
		client:  client,
		baseURL: defaultBaseURL,
	}
}

func NewProviderWithTokenSource(token string, tokenSource func() (string, error)) *Provider {
	return NewProviderWithTokenSourceAndBaseURL(token, tokenSource, "")
}

func NewProviderWithTokenSourceAndBaseURL(token string, tokenSource func() (string, error), apiBase string) *Provider {
	p := NewProviderWithBaseURL(token, apiBase)
	p.tokenSource = tokenSource
	return p
}

func CountTokens(
	ctx context.Context,
	token string,
	apiBase string,
	messages []Message,
	model string,
	options map[string]any,
) (int, error) {
	params, err := buildParams(messages, nil, model, options)
	if err != nil {
		return 0, err
	}

	body, err := json.Marshal(params)
	if err != nil {
		return 0, fmt.Errorf("marshal count_tokens request: %w", err)
	}

	baseURL := normalizeBaseURL(apiBase)
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/messages/count_tokens", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create count_tokens request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("send count_tokens request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return 0, fmt.Errorf("count_tokens request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var out struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("decode count_tokens response: %w", err)
	}
	if out.InputTokens <= 0 {
		return 0, fmt.Errorf("count_tokens returned no input_tokens")
	}
	return out.InputTokens, nil
}

func (p *Provider) Chat(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
) (*LLMResponse, error) {
	var opts []option.RequestOption
	if p.tokenSource != nil {
		tok, err := p.tokenSource()
		if err != nil {
			return nil, fmt.Errorf("refreshing token: %w", err)
		}
		opts = append(opts,
			option.WithAuthToken(tok),
			option.WithHeader("anthropic-beta", anthropicBetaHeader),
		)
	}

	params, err := buildParams(messages, tools, model, options)
	if err != nil {
		return nil, err
	}

	// streaming is required to support slowly requests.
	return p.chatStreaming(ctx, params, opts)
}

func (p *Provider) chatStreaming(
	ctx context.Context,
	params anthropic.MessageNewParams,
	opts []option.RequestOption,
) (*LLMResponse, error) {
	stream := p.client.Messages.NewStreaming(ctx, params, opts...)
	defer stream.Close()

	var msg anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			return nil, fmt.Errorf("claude streaming accumulate: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("claude API call: %w", err)
	}

	return parseResponse(&msg), nil
}

func (p *Provider) GetDefaultModel() string {
	return "claude-sonnet-4.6"
}

func (p *Provider) BaseURL() string {
	return p.baseURL
}

func buildParams(
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
) (anthropic.MessageNewParams, error) {
	var system []anthropic.TextBlockParam
	var anthropicMessages []anthropic.MessageParam

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			// Prefer structured SystemParts for per-block cache_control.
			// This enables LLM-side KV cache reuse: the static block's prefix
			// hash stays stable across requests while dynamic parts change freely.
			if len(msg.SystemParts) > 0 {
				for _, part := range msg.SystemParts {
					block := anthropic.TextBlockParam{Text: part.Text}
					if part.CacheControl != nil && part.CacheControl.Type == "ephemeral" {
						block.CacheControl = anthropic.NewCacheControlEphemeralParam()
					}
					system = append(system, block)
				}
			} else {
				system = append(system, anthropic.TextBlockParam{Text: msg.Content})
			}
		case "user":
			if msg.ToolCallID != "" {
				anthropicMessages = append(anthropicMessages,
					anthropic.NewUserMessage(newAnthropicToolResultBlock(msg)...),
				)
			} else {
				blocks := buildAnthropicContentBlocks(msg)
				if len(blocks) == 0 {
					blocks = append(blocks, anthropic.NewTextBlock(""))
				}
				anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(blocks...))
			}
		case "assistant":
			if len(msg.ToolCalls) > 0 || len(msg.Media) > 0 {
				blocks := buildAnthropicContentBlocks(msg)
				for _, tc := range msg.ToolCalls {
					args := tc.Arguments
					if args == nil && tc.Function != nil && tc.Function.Arguments != "" {
						if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
							args = map[string]any{}
						}
					}
					if args == nil {
						args = map[string]any{}
					}
					blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, args, tc.Name))
				}
				if len(blocks) == 0 {
					blocks = append(blocks, anthropic.NewTextBlock(""))
				}
				anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(blocks...))
			} else {
				anthropicMessages = append(anthropicMessages,
					anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)),
				)
			}
		case "tool":
			anthropicMessages = append(anthropicMessages,
				anthropic.NewUserMessage(newAnthropicToolResultBlock(msg)...),
			)
		}
	}

	maxTokens := int64(16 * 1024)
	if mt, ok := options["max_tokens"].(int); ok {
		maxTokens = int64(mt)
	}

	// Normalize model ID: Anthropic API uses hyphens (claude-sonnet-4-6),
	// but config may use dots (claude-sonnet-4.6).
	apiModel := strings.ReplaceAll(model, ".", "-")

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(apiModel),
		Messages:  anthropicMessages,
		MaxTokens: maxTokens,
	}

	if len(system) > 0 {
		params.System = system
	}

	if len(tools) > 0 {
		params.Tools = translateTools(tools)
	}

	// Extended Thinking / Adaptive Thinking
	// The thinking_level value directly determines the API parameter format:
	//   "adaptive" → {thinking: {type: "adaptive"}} + output_config.effort
	//   "low/medium/high/xhigh" → {thinking: {type: "enabled", budget_tokens: N}}
	level, ok := options["thinking_level"].(string)
	if ok && level != "" && level != "off" {
		applyThinkingConfig(&params, level)
	}
	if temp, ok := options["temperature"].(float64); ok && level == "off" { // Anthropic API rejects temperature if thinking is enabled.
		params.Temperature = anthropic.Float(temp)
	}
	if topP, ok := options["top_p"].(float64); ok {
		params.TopP = anthropic.Float(topP)
	}

	return params, nil
}

// applyThinkingConfig sets thinking parameters based on the level value.
// "adaptive" uses the adaptive thinking API (Claude 4.6+).
// All other levels use budget_tokens which is universally supported.
//
// Anthropic API constraint: temperature must not be set when thinking is enabled.
// budget_tokens must be strictly less than max_tokens.
func applyThinkingConfig(params *anthropic.MessageNewParams, level string) {
	// Anthropic API rejects requests with temperature set alongside thinking.
	// Reset to zero value (omitted from JSON serialization).
	if params.Temperature.Valid() {
		log.Printf("anthropic: temperature cleared because thinking is enabled (level=%s)", level)
	}
	params.Temperature = anthropic.MessageNewParams{}.Temperature

	if level == "adaptive" {
		adaptive := anthropic.NewThinkingConfigAdaptiveParam()
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}
		params.OutputConfig = anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortHigh,
		}
		return
	}

	budget := int64(levelToBudget(level))
	if budget <= 0 {
		return
	}

	// budget_tokens must be < max_tokens; clamp to respect user's max_tokens setting.
	if budget >= params.MaxTokens {
		log.Printf("anthropic: budget_tokens (%d) clamped to %d (max_tokens-1)", budget, params.MaxTokens-1)
		budget = params.MaxTokens - 1
	} else if budget > params.MaxTokens*80/100 {
		log.Printf("anthropic: thinking budget (%d) exceeds 80%% of max_tokens (%d), output may be truncated",
			budget, params.MaxTokens)
	}
	params.Thinking = anthropic.ThinkingConfigParamOfEnabled(budget)
}

// levelToBudget maps a thinking level to budget_tokens.
// Values are based on Anthropic's recommendations and community best practices:
//
//	low    =  4,096  — simple reasoning, quick debugging (Claude Code "think")
//	medium = 16,384  — Anthropic recommended sweet spot for most tasks
//	high   = 32,000  — complex architecture, deep analysis (diminishing returns above this)
//	xhigh  = 64,000  — extreme reasoning, research problems, benchmarks
//
// Note: For Claude 4.6+, prefer adaptive thinking over manual budget_tokens.
func levelToBudget(level string) int {
	switch level {
	case "low":
		return 4096
	case "medium":
		return 16384
	case "high":
		return 32000
	case "xhigh":
		return 64000
	default:
		return 0
	}
}

func translateTools(tools []ToolDefinition) []anthropic.ToolUnionParam {
	result := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		tool := anthropic.ToolParam{
			Name: t.Function.Name,
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: t.Function.Parameters["properties"],
			},
		}
		if desc := t.Function.Description; desc != "" {
			tool.Description = anthropic.String(desc)
		}
		if req, ok := t.Function.Parameters["required"].([]any); ok {
			required := make([]string, 0, len(req))
			for _, r := range req {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
			tool.InputSchema.Required = required
		}
		result = append(result, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return result
}

func buildAnthropicContentBlocks(msg Message) []anthropic.ContentBlockParamUnion {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(msg.Media))
	if msg.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
	}
	for _, mediaURL := range msg.Media {
		if img := parseImageURL(mediaURL); img != nil {
			blocks = append(blocks, anthropic.ContentBlockParamUnion{OfImage: img})
		}
	}
	return blocks
}

func newAnthropicToolResultBlock(msg Message) []anthropic.ContentBlockParamUnion {
	content := make([]anthropic.ToolResultBlockParamContentUnion, 0, 1+len(msg.Media))
	if msg.Content != "" {
		content = append(content, anthropic.ToolResultBlockParamContentUnion{
			OfText: &anthropic.TextBlockParam{Text: msg.Content},
		})
	}
	for _, mediaURL := range msg.Media {
		if img := parseImageURL(mediaURL); img != nil {
			content = append(content, anthropic.ToolResultBlockParamContentUnion{OfImage: img})
		}
	}
	if len(content) == 0 {
		content = append(content, anthropic.ToolResultBlockParamContentUnion{
			OfText: &anthropic.TextBlockParam{Text: ""},
		})
	}
	return []anthropic.ContentBlockParamUnion{{
		OfToolResult: &anthropic.ToolResultBlockParam{
			ToolUseID: msg.ToolCallID,
			Content:   content,
		},
	}}
}

// parseImageURL parses an image URL (data URI or HTTP URL) into an ImageBlockParam.
// Returns nil if the URL is not a valid image source.
func parseImageURL(mediaURL string) *anthropic.ImageBlockParam {
	mediaURL = strings.TrimSpace(mediaURL)
	switch {
	case strings.HasPrefix(mediaURL, "data:image/"):
		// Parse data:image/<type>;base64,<encoded>
		header, encoded, ok := strings.Cut(strings.TrimPrefix(mediaURL, "data:"), ";base64,")
		if !ok {
			return nil
		}
		switch header {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
			block := anthropic.NewImageBlockBase64(header, encoded)
			return block.OfImage
		default:
			return nil
		}
	case strings.HasPrefix(mediaURL, "http://"), strings.HasPrefix(mediaURL, "https://"):
		block := anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: mediaURL})
		return block.OfImage
	default:
		return nil
	}
}

func parseResponse(resp *anthropic.Message) *LLMResponse {
	var content strings.Builder
	var reasoning strings.Builder
	var toolCalls []ToolCall

	for _, block := range resp.Content {
		switch block.Type {
		case "thinking":
			tb := block.AsThinking()
			reasoning.WriteString(tb.Thinking)
		case "text":
			tb := block.AsText()
			content.WriteString(tb.Text)
		case "tool_use":
			tu := block.AsToolUse()
			var args map[string]any
			if err := json.Unmarshal(tu.Input, &args); err != nil {
				log.Printf("anthropic: failed to decode tool call input for %q: %v", tu.Name, err)
				args = map[string]any{"raw": string(tu.Input)}
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:        tu.ID,
				Name:      tu.Name,
				Arguments: args,
			})
		}
	}

	finishReason := "stop"
	switch resp.StopReason {
	case anthropic.StopReasonToolUse:
		finishReason = "tool_calls"
	case anthropic.StopReasonMaxTokens:
		finishReason = "length"
	case anthropic.StopReasonEndTurn:
		finishReason = "stop"
	}

	return &LLMResponse{
		Content:      content.String(),
		Reasoning:    reasoning.String(),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage: &UsageInfo{
			PromptTokens:     int(resp.Usage.InputTokens),
			CompletionTokens: int(resp.Usage.OutputTokens),
			TotalTokens:      int(resp.Usage.InputTokens + resp.Usage.OutputTokens),
		},
	}
}

func normalizeBaseURL(apiBase string) string {
	base := strings.TrimSpace(apiBase)
	if base == "" {
		return defaultBaseURL
	}

	base = strings.TrimRight(base, "/")
	if before, ok := strings.CutSuffix(base, "/v1"); ok {
		base = before
	}
	if base == "" {
		return defaultBaseURL
	}

	return base
}
