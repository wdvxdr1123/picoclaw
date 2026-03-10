// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package providers

import (
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
	anthropicprovider "github.com/sipeed/picoclaw/pkg/providers/anthropic"
)

const defaultKimiUserAgent = "KimiCLI/1.18.0"

// createClaudeAuthProvider creates a Claude provider using OAuth credentials from auth store.
func createClaudeAuthProvider() (LLMProvider, error) {
	cred, err := getCredential("anthropic")
	if err != nil {
		return nil, fmt.Errorf("loading auth credentials: %w", err)
	}
	if cred == nil {
		return nil, fmt.Errorf("no credentials for anthropic. Run: picoclaw auth login --provider anthropic")
	}
	return NewClaudeProviderWithTokenSource(cred.AccessToken, createClaudeTokenSource()), nil
}

// createCodexAuthProvider creates a Codex provider using OAuth credentials from auth store.
func createCodexAuthProvider() (LLMProvider, error) {
	cred, err := getCredential("openai")
	if err != nil {
		return nil, fmt.Errorf("loading auth credentials: %w", err)
	}
	if cred == nil {
		return nil, fmt.Errorf("no credentials for openai. Run: picoclaw auth login --provider openai")
	}
	return NewCodexProviderWithTokenSource(cred.AccessToken, cred.AccountID, createCodexTokenSource()), nil
}

// ExtractProtocol extracts the protocol prefix and model identifier from a model string.
// If no prefix is specified, it defaults to "openai".
// Examples:
//   - "openai/gpt-4o" -> ("openai", "gpt-4o")
//   - "anthropic/claude-sonnet-4.6" -> ("anthropic", "claude-sonnet-4.6")
//   - "gpt-4o" -> ("openai", "gpt-4o")  // default protocol
func ExtractProtocol(model string) (protocol, modelID string) {
	model = strings.TrimSpace(model)
	protocol, modelID, found := strings.Cut(model, "/")
	if !found {
		return "openai", model
	}
	return protocol, modelID
}

// CreateProviderFromConfig creates a provider based on the resolved ModelConfig.
// Wire formats are intentionally collapsed to two families:
//   - Anthropic-compatible
//   - OpenAI-compatible
//
// Some legacy transports (CLI/grpc/custom auth) remain special-cased, but all
// HTTP provider routing is reduced to these two wire formats.
func CreateProviderFromConfig(cfg *config.ModelConfig) (LLMProvider, string, error) {
	if cfg == nil {
		return nil, "", fmt.Errorf("config is nil")
	}

	if cfg.Model == "" {
		return nil, "", fmt.Errorf("model is required")
	}

	protocol, modelID := ExtractProtocol(cfg.Model)

	switch ResolveWireFormat(protocol) {
	case WireFormatAnthropic:
		if cfg.AuthMethod == "oauth" || cfg.AuthMethod == "token" {
			provider, err := createClaudeAuthProvider()
			if err != nil {
				return nil, "", err
			}
			return provider, modelID, nil
		}
		apiBase := cfg.APIBase
		if apiBase == "" {
			apiBase = defaultAnthropicAPIBase
		}
		if cfg.APIKey == "" {
			return nil, "", fmt.Errorf("api_key is required for anthropic protocol (model: %s)", cfg.Model)
		}
		return anthropicprovider.NewProviderWithBaseURL(
			cfg.APIKey,
			apiBase,
		), modelID, nil

	case WireFormatOpenAI:
		if strings.EqualFold(protocol, "openai") && (cfg.AuthMethod == "oauth" || cfg.AuthMethod == "token") {
			provider, err := createCodexAuthProvider()
			if err != nil {
				return nil, "", err
			}
			return provider, modelID, nil
		}
		if cfg.APIKey == "" && cfg.APIBase == "" {
			return nil, "", fmt.Errorf("api_key or api_base is required for openai-compatible protocol %q", protocol)
		}
		apiBase := cfg.APIBase
		if apiBase == "" {
			return nil, "", fmt.Errorf("api_base is required for custom openai-compatible protocol %q", protocol)
		}
		userAgent := ""
		if strings.EqualFold(protocol, "kimi") {
			userAgent = defaultKimiUserAgent
		}
		return NewHTTPProviderWithOptions(
			cfg.APIKey,
			apiBase,
			cfg.Proxy,
			cfg.MaxTokensField,
			cfg.RequestTimeout,
			userAgent,
		), modelID, nil
	}

	return nil, "", fmt.Errorf("unsupported provider wire format for model %q", cfg.Model)
}
