// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package providers

import (
	"context"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers/openai_compat"
)

type HTTPProvider struct {
	delegate *openai_compat.Provider
}

func NewHTTPProvider(apiKey, apiBase, proxy string) *HTTPProvider {
	return &HTTPProvider{
		delegate: openai_compat.NewProvider(apiKey, apiBase, proxy),
	}
}

func NewHTTPProviderWithMaxTokensField(apiKey, apiBase, proxy, maxTokensField string) *HTTPProvider {
	return NewHTTPProviderWithMaxTokensFieldAndRequestTimeout(apiKey, apiBase, proxy, maxTokensField, 0)
}

func NewHTTPProviderWithMaxTokensFieldAndRequestTimeout(
	apiKey, apiBase, proxy, maxTokensField string,
	requestTimeoutSeconds int,
) *HTTPProvider {
	return NewHTTPProviderWithOptions(apiKey, apiBase, proxy, maxTokensField, requestTimeoutSeconds, "")
}

func NewHTTPProviderWithOptions(
	apiKey, apiBase, proxy, maxTokensField string,
	requestTimeoutSeconds int,
	userAgent string,
) *HTTPProvider {
	opts := []openai_compat.Option{
		openai_compat.WithMaxTokensField(maxTokensField),
	}
	if requestTimeoutSeconds > 0 {
		opts = append(opts, openai_compat.WithRequestTimeout(time.Duration(requestTimeoutSeconds)*time.Second))
	}
	if userAgent != "" {
		opts = append(opts, openai_compat.WithUserAgent(userAgent))
	}
	return &HTTPProvider{
		delegate: openai_compat.NewProvider(apiKey, apiBase, proxy, opts...),
	}
}

func (p *HTTPProvider) Chat(
	ctx context.Context,
	messages []Message,
	tools []ToolDefinition,
	model string,
	options map[string]any,
) (*LLMResponse, error) {
	return p.delegate.Chat(ctx, messages, tools, model, options)
}

func (p *HTTPProvider) GetDefaultModel() string {
	return ""
}
