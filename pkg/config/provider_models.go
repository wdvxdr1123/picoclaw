package config

import (
	"fmt"
	"sort"
	"strings"
)

func NormalizeProviderName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "gpt":
		return "openai"
	case "claude":
		return "anthropic"
	case "glm":
		return "zhipu"
	case "doubao":
		return "volcengine"
	case "tongyi":
		return "qwen"
	case "kimi":
		return "moonshot"
	case "github-copilot", "copilot":
		return "github_copilot"
	case "google-antigravity":
		return "antigravity"
	default:
		return n
	}
}

func NormalizeProviderType(name string) string {
	switch NormalizeProviderName(name) {
	case "github_copilot":
		return "github-copilot"
	default:
		return NormalizeProviderName(name)
	}
}

func (p *ProvidersConfig) SyncNamed() {
	if p.Named == nil {
		p.Named = make(map[string]ProviderConfig)
	}

	syncBuiltIn := func(name string, cfg ProviderConfig) {
		if cfg.Type == "" {
			cfg.Type = NormalizeProviderType(name)
		}
		if cfg.IsZero() {
			if _, ok := p.Named[name]; !ok {
				return
			}
		}
		p.Named[name] = cfg
	}

	syncBuiltIn("anthropic", p.Anthropic)
	syncBuiltIn("openai", p.OpenAI)
	syncBuiltIn("litellm", p.LiteLLM)
	syncBuiltIn("openrouter", p.OpenRouter)
	syncBuiltIn("groq", p.Groq)
	syncBuiltIn("zhipu", p.Zhipu)
	syncBuiltIn("vllm", p.VLLM)
	syncBuiltIn("gemini", p.Gemini)
	syncBuiltIn("nvidia", p.Nvidia)
	syncBuiltIn("ollama", p.Ollama)
	syncBuiltIn("kimi", p.Kimi)
	syncBuiltIn("moonshot", p.Moonshot)
	syncBuiltIn("shengsuanyun", p.ShengSuanYun)
	syncBuiltIn("deepseek", p.DeepSeek)
	syncBuiltIn("cerebras", p.Cerebras)
	syncBuiltIn("vivgrid", p.Vivgrid)
	syncBuiltIn("volcengine", p.VolcEngine)
	syncBuiltIn("github_copilot", p.GitHubCopilot)
	syncBuiltIn("antigravity", p.Antigravity)
	syncBuiltIn("qwen", p.Qwen)
	syncBuiltIn("mistral", p.Mistral)
	syncBuiltIn("avian", p.Avian)
}

func (c *Config) ApplyCompatibilityDefaults() {
	if strings.TrimSpace(c.Agents.Defaults.GetModelName()) == "" && strings.TrimSpace(c.DefaultModel) != "" {
		c.Agents.Defaults.Model = strings.TrimSpace(c.DefaultModel)
	}

	if c.LoopControl.MaxStepsPerTurn == 0 && c.Agents.Defaults.MaxToolIterations > 0 {
		c.LoopControl.MaxStepsPerTurn = c.Agents.Defaults.MaxToolIterations
	}
	if c.Agents.Defaults.MaxToolIterations == 0 && c.LoopControl.MaxStepsPerTurn > 0 {
		c.Agents.Defaults.MaxToolIterations = c.LoopControl.MaxStepsPerTurn
	}

	c.Providers.SyncNamed()
}

func (c *Config) GetDefaultModelName() string {
	return strings.TrimSpace(c.Agents.Defaults.GetModelName())
}

func (c *Config) GetDefaultProviderName() string {
	if strings.TrimSpace(c.Agents.Defaults.Provider) != "" {
		return NormalizeProviderType(c.Agents.Defaults.Provider)
	}
	return "openai"
}

func (c *Config) ResolveModelListFromModels() ([]ModelConfig, error) {
	if len(c.Models) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(c.Models))
	for name := range c.Models {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]ModelConfig, 0)
	for _, name := range names {
		variants := c.Models[name]
		for idx, variant := range variants {
			if strings.TrimSpace(variant.Provider) == "" {
				return nil, fmt.Errorf("models.%s[%d]: provider is required", name, idx)
			}
			if strings.TrimSpace(variant.Model) == "" {
				return nil, fmt.Errorf("models.%s[%d]: model is required", name, idx)
			}

			providerName := NormalizeProviderName(variant.Provider)
			providerCfg := c.Providers.Get(providerName)
			if providerName == "kimi" && providerCfg.IsZero() && providerCfg.Type == "" {
				providerCfg = c.Providers.Get("moonshot")
			}
			if providerCfg.IsZero() && providerCfg.Type == "" {
				return nil, fmt.Errorf("models.%s[%d]: provider %q not found", name, idx, variant.Provider)
			}

			providerType := providerCfg.Type
			if providerType == "" {
				providerType = NormalizeProviderType(providerName)
			} else if providerName == "kimi" {
				providerType = "kimi"
			}

			result = append(result, ModelConfig{
				ModelName:      name,
				Provider:       providerName,
				Model:          buildModelWithProtocol(providerType, strings.TrimSpace(variant.Model)),
				APIBase:        providerCfg.APIBase,
				APIKey:         providerCfg.APIKey,
				Proxy:          providerCfg.Proxy,
				AuthMethod:     providerCfg.AuthMethod,
				ConnectMode:    providerCfg.ConnectMode,
				Workspace:      providerCfg.Workspace,
				MaxTokens:      variant.MaxTokens,
				RPM:            variant.RPM,
				MaxTokensField: variant.MaxTokensField,
				RequestTimeout: providerCfg.RequestTimeout,
				MaxContextSize: variant.MaxContextSize,
				Temperature:    variant.Temperature,
				TopP:           variant.TopP,
				ThinkingLevel:  variant.ThinkingLevel,
				TokenCountAPI:  variant.TokenCountAPI,
			})
		}
	}
	return result, nil
}

func ConvertModelListToSeparatedConfig(modelList []ModelConfig) (ProvidersConfig, ModelsConfig) {
	providers := ProvidersConfig{Named: map[string]ProviderConfig{}}
	models := ModelsConfig{}
	signatures := map[string]string{}
	nameCounts := map[string]int{}

	for _, mc := range modelList {
		protocol, modelID := splitResolvedModel(mc.Model)
		baseName := providerBaseName(mc, protocol)
		pcfg := ProviderConfig{
			Type:           protocol,
			APIKey:         mc.APIKey,
			APIBase:        mc.APIBase,
			Proxy:          mc.Proxy,
			RequestTimeout: mc.RequestTimeout,
			AuthMethod:     mc.AuthMethod,
			ConnectMode:    mc.ConnectMode,
			Workspace:      mc.Workspace,
		}
		sig := providerSignature(pcfg)
		providerName, ok := signatures[sig]
		if !ok {
			providerName = uniqueProviderName(baseName, nameCounts)
			signatures[sig] = providerName
			providers.Set(providerName, pcfg)
		}

		models[mc.ModelName] = append(models[mc.ModelName], ModelDefinition{
			Provider:       providerName,
			Model:          modelID,
			MaxTokens:      mc.MaxTokens,
			RPM:            mc.RPM,
			MaxTokensField: mc.MaxTokensField,
			MaxContextSize: mc.MaxContextSize,
			Temperature:    mc.Temperature,
			TopP:           mc.TopP,
			ThinkingLevel:  mc.ThinkingLevel,
			TokenCountAPI:  mc.TokenCountAPI,
		})
	}

	providers.SyncNamed()
	return providers, models
}

func splitResolvedModel(model string) (string, string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "openai", ""
	}
	before, after, ok := strings.Cut(model, "/")
	if !ok {
		return "openai", model
	}
	return NormalizeProviderType(before), after
}

func providerBaseName(mc ModelConfig, protocol string) string {
	if strings.TrimSpace(mc.Provider) != "" {
		return NormalizeProviderName(mc.Provider)
	}
	return NormalizeProviderName(protocol)
}

func providerSignature(cfg ProviderConfig) string {
	return strings.Join([]string{
		cfg.Type,
		cfg.APIKey,
		cfg.APIBase,
		cfg.Proxy,
		fmt.Sprintf("%d", cfg.RequestTimeout),
		cfg.AuthMethod,
		cfg.ConnectMode,
		cfg.Workspace,
	}, "\x1f")
}

func uniqueProviderName(base string, counts map[string]int) string {
	base = NormalizeProviderName(base)
	if base == "" {
		base = "provider"
	}
	counts[base]++
	if counts[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, counts[base])
}
