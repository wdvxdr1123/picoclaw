// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package config

import (
	"os"
	"path/filepath"
)

// DefaultConfig returns the default configuration for PicoClaw.
func DefaultConfig() *Config {
	// Determine the base path for the workspace.
	// Priority: $PICOCLAW_HOME > ~/.picoclaw
	var homePath string
	if picoclawHome := os.Getenv("PICOCLAW_HOME"); picoclawHome != "" {
		homePath = picoclawHome
	} else {
		userHome, _ := os.UserHomeDir()
		homePath = filepath.Join(userHome, ".picoclaw")
	}
	workspacePath := filepath.Join(homePath, "workspace")

	cfg := &Config{
		LoopControl: LoopControlConfig{
			MaxStepsPerTurn: 50,
		},
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:                 workspacePath,
				RestrictToWorkspace:       true,
				Provider:                  "",
				Model:                     "",
				MaxToolIterations:         50,
				SummarizeMessageThreshold: 20,
				SummarizeTokenPercent:     75,
			},
		},
		Bindings: []AgentBinding{},
		Session: SessionConfig{
			DMScope: "per-channel-peer",
		},
		Channels: ChannelsConfig{
			Feishu: FeishuConfig{
				Enabled:           false,
				AppID:             "",
				AppSecret:         "",
				EncryptKey:        "",
				VerificationToken: "",
				AllowFrom:         FlexibleStringSlice{},
			},
			QQBot: QQBotConfig{
				Enabled:         false,
				AppID:           "",
				ClientSecret:    "",
				AllowFrom:       FlexibleStringSlice{},
				MarkdownSupport: true,
				DmPolicy:        "open",
				TextChunkLimit:  1500,
				MaxFileSizeMB:   100,
				MediaTimeoutMs:  30000,
			},
			DingTalk: DingTalkConfig{
				Enabled:      false,
				ClientID:     "",
				ClientSecret: "",
				AllowFrom:    FlexibleStringSlice{},
			},
			OneBot: OneBotConfig{
				Enabled:            false,
				WSUrl:              "ws://127.0.0.1:3001",
				AccessToken:        "",
				ReconnectInterval:  5,
				GroupTriggerPrefix: []string{},
				AllowFrom:          FlexibleStringSlice{},
			},
			Pico: PicoConfig{
				Enabled:        false,
				Token:          "",
				PingInterval:   30,
				ReadTimeout:    60,
				WriteTimeout:   10,
				MaxConnections: 100,
				AllowFrom:      FlexibleStringSlice{},
			},
		},
		Providers: ProvidersConfig{
			OpenAI: ProviderConfig{
				Type:      "openai",
				APIBase:   "https://api.openai.com/v1",
				WebSearch: true,
			},
			Anthropic: ProviderConfig{
				Type:    "anthropic",
				APIBase: "https://api.anthropic.com/v1",
			},
			Zhipu: ProviderConfig{
				Type:    "zhipu",
				APIBase: "https://open.bigmodel.cn/api/paas/v4",
			},
			DeepSeek: ProviderConfig{
				Type:    "deepseek",
				APIBase: "https://api.deepseek.com/v1",
			},
			Gemini: ProviderConfig{
				Type:    "gemini",
				APIBase: "https://generativelanguage.googleapis.com/v1beta",
			},
			Qwen: ProviderConfig{
				Type:    "qwen",
				APIBase: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			},
			Kimi: ProviderConfig{
				Type:    "kimi",
				APIBase: "https://api.moonshot.ai/v1",
			},
			Moonshot: ProviderConfig{
				Type:    "moonshot",
				APIBase: "https://api.moonshot.cn/v1",
			},
			Groq: ProviderConfig{
				Type:    "groq",
				APIBase: "https://api.groq.com/openai/v1",
			},
			OpenRouter: ProviderConfig{
				Type:    "openrouter",
				APIBase: "https://openrouter.ai/api/v1",
			},
			Nvidia: ProviderConfig{
				Type:    "nvidia",
				APIBase: "https://integrate.api.nvidia.com/v1",
			},
			Cerebras: ProviderConfig{
				Type:    "cerebras",
				APIBase: "https://api.cerebras.ai/v1",
			},
			Vivgrid: ProviderConfig{
				Type:    "vivgrid",
				APIBase: "https://api.vivgrid.com/v1",
			},
			VolcEngine: ProviderConfig{
				Type:    "volcengine",
				APIBase: "https://ark.cn-beijing.volces.com/api/v3",
			},
			ShengSuanYun: ProviderConfig{
				Type:    "shengsuanyun",
				APIBase: "https://api.shengsuanyun.com/v1",
			},
			Antigravity: ProviderConfig{
				Type:       "antigravity",
				AuthMethod: "oauth",
			},
			GitHubCopilot: ProviderConfig{
				Type:       "github-copilot",
				APIBase:    "http://localhost:4321",
				AuthMethod: "oauth",
			},
			Ollama: ProviderConfig{
				Type:    "ollama",
				APIBase: "http://localhost:11434/v1",
			},
			Mistral: ProviderConfig{
				Type:    "mistral",
				APIBase: "https://api.mistral.ai/v1",
			},
			Avian: ProviderConfig{
				Type:    "avian",
				APIBase: "https://api.avian.io/v1",
			},
			VLLM: ProviderConfig{
				Type:    "vllm",
				APIBase: "http://localhost:8000/v1",
			},
		},
		Models: ModelsConfig{
			// ============================================
			// Add your API key to the model you want to use
			// ============================================

			// Zhipu AI - https://open.bigmodel.cn/usercenter/apikeys
			"glm-4.7": {{Provider: "zhipu", Model: "glm-4.7"}},

			// OpenAI - https://platform.openai.com/api-keys
			"gpt-5.2": {{Provider: "openai", Model: "gpt-5.2"}},

			// Anthropic Claude - https://console.anthropic.com/settings/keys
			"claude-sonnet-4.6": {{Provider: "anthropic", Model: "claude-sonnet-4.6"}},

			// DeepSeek - https://platform.deepseek.com/
			"deepseek-chat": {{Provider: "deepseek", Model: "deepseek-chat"}},

			// Google Gemini - https://ai.google.dev/
			"gemini-2.0-flash": {{Provider: "gemini", Model: "gemini-2.0-flash-exp"}},

			// Qwen - https://dashscope.console.aliyun.com/apiKey
			"qwen-plus": {{Provider: "qwen", Model: "qwen-plus"}},

			// Kimi - https://platform.moonshot.ai/console/api-keys
			"kimi-k2.5-official": {{Provider: "kimi", Model: "kimi-k2.5"}},

			// Moonshot - https://platform.moonshot.cn/console/api-keys
			"moonshot-v1-8k": {{Provider: "moonshot", Model: "moonshot-v1-8k"}},

			// Groq - https://console.groq.com/keys
			"llama-3.3-70b": {{Provider: "groq", Model: "llama-3.3-70b-versatile"}},

			// OpenRouter (100+ models) - https://openrouter.ai/keys
			"openrouter-auto":    {{Provider: "openrouter", Model: "auto"}},
			"openrouter-gpt-5.2": {{Provider: "openrouter", Model: "openai/gpt-5.2"}},

			// NVIDIA - https://build.nvidia.com/
			"nemotron-4-340b": {{Provider: "nvidia", Model: "nemotron-4-340b-instruct"}},

			// Cerebras - https://inference.cerebras.ai/
			"cerebras-llama-3.3-70b": {{Provider: "cerebras", Model: "llama-3.3-70b"}},

			// Vivgrid - https://vivgrid.com
			"vivgrid-auto": {{Provider: "vivgrid", Model: "auto"}},

			// Volcengine - https://console.volcengine.com/ark
			"doubao-pro": {{Provider: "volcengine", Model: "doubao-pro-32k"}},

			// ShengsuanYun
			"deepseek-v3": {{Provider: "shengsuanyun", Model: "deepseek-v3"}},

			// Antigravity (Google Cloud Code Assist) - OAuth only
			"gemini-flash": {{Provider: "antigravity", Model: "gemini-3-flash"}},

			// GitHub Copilot - https://github.com/settings/tokens
			"copilot-gpt-5.2": {{Provider: "github_copilot", Model: "gpt-5.2"}},

			// Ollama (local) - https://ollama.com
			"llama3": {{Provider: "ollama", Model: "llama3"}},

			// Mistral AI - https://console.mistral.ai/api-keys
			"mistral-small": {{Provider: "mistral", Model: "mistral-small-latest"}},

			// Avian - https://avian.io
			"deepseek-v3.2": {{Provider: "avian", Model: "deepseek/deepseek-v3.2"}},
			"kimi-k2.5":     {{Provider: "avian", Model: "moonshotai/kimi-k2.5"}},

			// VLLM (local) - http://localhost:8000
			"local-model": {{Provider: "vllm", Model: "custom-model"}},
		},
		Gateway: GatewayConfig{
			Host: "127.0.0.1",
			Port: 18790,
		},
		Tools: ToolsConfig{
			MediaCleanup: MediaCleanupConfig{
				ToolConfig: ToolConfig{
					Enabled: true,
				},
				MaxAge:   30,
				Interval: 5,
			},
			Web: WebToolsConfig{
				ToolConfig: ToolConfig{
					Enabled: true,
				},
				SearchProvider:  "auto",
				Proxy:           "",
				FetchLimitBytes: 10 * 1024 * 1024, // 10MB by default
				OpenAISearch: OpenAISearchConfig{
					Enabled: false,
					BaseURL: "",
					Model:   "",
				},
			},
			Cron: CronToolsConfig{
				ToolConfig: ToolConfig{
					Enabled: true,
				},
				ExecTimeoutMinutes: 5,
			},
			Exec: ExecConfig{
				ToolConfig: ToolConfig{
					Enabled: true,
				},
				EnableDenyPatterns: true,
				TimeoutSeconds:     60,
			},
			Skills: SkillsToolsConfig{
				ToolConfig: ToolConfig{
					Enabled: true,
				},
				Registries: SkillsRegistriesConfig{
					ClawHub: ClawHubRegistryConfig{
						Enabled: true,
						BaseURL: "https://clawhub.ai",
					},
				},
				MaxConcurrentSearches: 2,
				SearchCache: SearchCacheConfig{
					MaxSize:    50,
					TTLSeconds: 300,
				},
			},
			SendFile: ToolConfig{
				Enabled: true,
			},
			MCP: MCPConfig{
				ToolConfig: ToolConfig{
					Enabled: false,
				},
				Servers: map[string]MCPServerConfig{},
			},
			AppendFile: ToolConfig{
				Enabled: true,
			},
			EditFile: ToolConfig{
				Enabled: true,
			},
			FindSkills: ToolConfig{
				Enabled: true,
			},
			InstallSkill: ToolConfig{
				Enabled: true,
			},
			ListDir: ToolConfig{
				Enabled: true,
			},
			Message: ToolConfig{
				Enabled: true,
			},
			ReadFile: ToolConfig{
				Enabled: true,
			},
			Spawn: ToolConfig{
				Enabled: true,
			},
			Subagent: ToolConfig{
				Enabled: true,
			},
			WebFetch: ToolConfig{
				Enabled: true,
			},
			WriteFile: ToolConfig{
				Enabled: true,
			},
		},
		Heartbeat: HeartbeatConfig{
			Enabled:  true,
			Interval: 30,
		},
		Devices: DevicesConfig{
			Enabled:    false,
			MonitorUSB: true,
		},
	}

	cfg.ApplyCompatibilityDefaults()
	cfg.ModelList, _ = cfg.ResolveModelListFromModels()
	return cfg
}
