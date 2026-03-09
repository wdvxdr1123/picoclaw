package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
	anthropicprovider "github.com/sipeed/picoclaw/pkg/providers/anthropic"
	"github.com/sipeed/picoclaw/pkg/providers/openai_compat"
)

func CountTokens(ctx context.Context, cfg *config.ModelConfig, messages []Message) (int, error) {
	if cfg == nil {
		return 0, fmt.Errorf("model config is nil")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return 0, fmt.Errorf("model is required")
	}

	protocol, modelID := ExtractProtocol(cfg.Model)
	switch ResolveWireFormat(protocol) {
	case WireFormatAnthropic:
		token := strings.TrimSpace(cfg.APIKey)
		if token == "" && (cfg.AuthMethod == "oauth" || cfg.AuthMethod == "token") {
			cred, err := getCredential("anthropic")
			if err != nil {
				return 0, fmt.Errorf("loading anthropic credential: %w", err)
			}
			if cred == nil {
				return 0, fmt.Errorf("no anthropic credential available")
			}
			token = cred.AccessToken
		}
		if token == "" {
			return 0, fmt.Errorf("anthropic token counting requires api_key or auth credential")
		}
		return anthropicprovider.CountTokens(ctx, token, cfg.APIBase, messages, modelID, map[string]any{
			"max_tokens": cfg.MaxTokens,
		})

	case WireFormatOpenAI:
		token := strings.TrimSpace(cfg.APIKey)
		if token == "" && strings.EqualFold(protocol, "openai") && (cfg.AuthMethod == "oauth" || cfg.AuthMethod == "token") {
			cred, err := getCredential("openai")
			if err != nil {
				return 0, fmt.Errorf("loading openai credential: %w", err)
			}
			if cred == nil {
				return 0, fmt.Errorf("no openai credential available")
			}
			token = cred.AccessToken
		}
		return openai_compat.CountTokens(ctx, token, cfg.APIBase, cfg.TokenCountAPI, modelID, messages)

	default:
		return 0, fmt.Errorf("unsupported wire format")
	}
}
