package providers

import "github.com/sipeed/picoclaw/pkg/config"

const (
	defaultMaxTokens     = 32 * 1024
	defaultContextWindow = 128 * 1024
	defaultTopP          = 0.95
)

func EffectiveMaxTokens(cfg *config.ModelConfig) int {
	if cfg != nil && cfg.MaxTokens > 0 {
		return cfg.MaxTokens
	}
	return defaultMaxTokens
}

func EffectiveContextWindow(cfg *config.ModelConfig) int {
	if cfg != nil {
		if cfg.MaxContextSize > 0 {
			return cfg.MaxContextSize
		}
		if cfg.MaxTokens > 0 {
			return cfg.MaxTokens
		}
	}
	return defaultContextWindow
}

func EffectiveTemperature(cfg *config.ModelConfig) (float64, bool) {
	if cfg != nil && cfg.Temperature != nil {
		return *cfg.Temperature, true
	}
	return 0, false
}

func EffectiveTopP(cfg *config.ModelConfig) float64 {
	if cfg != nil && cfg.TopP != nil {
		return *cfg.TopP
	}
	return defaultTopP
}

func EffectiveThinkingLevel(cfg *config.ModelConfig) string {
	if cfg == nil {
		return ""
	}
	return NormalizeThinkingLevel(cfg.ThinkingLevel)
}

func BuildLLMOptions(cfg *config.ModelConfig, promptCacheKey string, provider LLMProvider) map[string]any {
	opts := map[string]any{
		"max_tokens": EffectiveMaxTokens(cfg),
		"top_p":      EffectiveTopP(cfg),
	}
	if promptCacheKey != "" {
		opts["prompt_cache_key"] = promptCacheKey
	}
	if temperature, ok := EffectiveTemperature(cfg); ok {
		opts["temperature"] = temperature
	}
	if thinkingLevel := EffectiveThinkingLevel(cfg); thinkingLevel != "" {
		opts["thinking_level"] = thinkingLevel
	}
	return opts
}
