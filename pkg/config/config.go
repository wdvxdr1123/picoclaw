package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/caarlos0/env/v11"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

// rrCounter is a global counter for round-robin load balancing across models.
var rrCounter atomic.Uint64

// FlexibleStringSlice is a []string that also accepts JSON numbers,
// so allow_from can contain both "123" and 123.
type FlexibleStringSlice []string

func (f *FlexibleStringSlice) UnmarshalJSON(data []byte) error {
	// Try []string first
	var ss []string
	if err := json.Unmarshal(data, &ss); err == nil {
		*f = ss
		return nil
	}

	// Try []interface{} to handle mixed types
	var raw []any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	result := make([]string, 0, len(raw))
	for _, v := range raw {
		switch val := v.(type) {
		case string:
			result = append(result, val)
		case float64:
			result = append(result, fmt.Sprintf("%.0f", val))
		default:
			result = append(result, fmt.Sprintf("%v", val))
		}
	}
	*f = result
	return nil
}

type Config struct {
	DefaultModel string            `json:"default_model,omitempty"` // Deprecated compatibility input only
	LoopControl  LoopControlConfig `json:"loop_control,omitempty"`
	Agents       AgentsConfig      `json:"agents"`
	Bindings     []AgentBinding    `json:"bindings,omitempty"`
	Session      SessionConfig     `json:"session,omitempty"`
	Channels     ChannelsConfig    `json:"channels"`
	Providers    ProvidersConfig   `json:"providers,omitempty"`
	Models       ModelsConfig      `json:"models,omitempty"`
	ModelList    []ModelConfig     `json:"model_list,omitempty"` // Deprecated: legacy model-centric provider configuration
	Gateway      GatewayConfig     `json:"gateway"`
	Tools        ToolsConfig       `json:"tools"`
	Heartbeat    HeartbeatConfig   `json:"heartbeat"`
	Devices      DevicesConfig     `json:"devices"`
}

// MarshalJSON implements custom JSON marshaling for Config
// to omit providers/session when empty and prefer the new models layout.
func (c Config) MarshalJSON() ([]byte, error) {
	type marshaledConfig struct {
		LoopControl *LoopControlConfig `json:"loop_control,omitempty"`
		Agents      AgentsConfig       `json:"agents"`
		Bindings    []AgentBinding     `json:"bindings,omitempty"`
		Session     *SessionConfig     `json:"session,omitempty"`
		Channels    ChannelsConfig     `json:"channels"`
		Providers   *ProvidersConfig   `json:"providers,omitempty"`
		Models      ModelsConfig       `json:"models,omitempty"`
		ModelList   []ModelConfig      `json:"model_list,omitempty"`
		Gateway     GatewayConfig      `json:"gateway"`
		Tools       ToolsConfig        `json:"tools"`
		Heartbeat   HeartbeatConfig    `json:"heartbeat"`
		Devices     DevicesConfig      `json:"devices"`
	}

	out := marshaledConfig{
		Agents:    c.Agents,
		Bindings:  c.Bindings,
		Channels:  c.Channels,
		Gateway:   c.Gateway,
		Tools:     c.Tools,
		Heartbeat: c.Heartbeat,
		Devices:   c.Devices,
	}

	if !c.LoopControl.IsEmpty() {
		out.LoopControl = &c.LoopControl
	}
	if c.Session.DMScope != "" || len(c.Session.IdentityLinks) > 0 {
		out.Session = &c.Session
	}
	if !c.Providers.IsEmpty() {
		out.Providers = &c.Providers
	}
	if len(c.Models) > 0 {
		out.Models = c.Models
	} else if len(c.ModelList) > 0 {
		out.ModelList = c.ModelList
	}

	return json.Marshal(out)
}

type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
	List     []AgentConfig `json:"list,omitempty"`
}

// AgentModelConfig supports both string and structured model config.
// String format: "gpt-4" (just primary, no fallbacks)
// Object format: {"primary": "gpt-4", "fallbacks": ["claude-haiku"]}
type AgentModelConfig struct {
	Primary   string   `json:"primary,omitempty"`
	Fallbacks []string `json:"fallbacks,omitempty"`
}

func (m *AgentModelConfig) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.Primary = s
		m.Fallbacks = nil
		return nil
	}
	type raw struct {
		Primary   string   `json:"primary"`
		Fallbacks []string `json:"fallbacks"`
	}
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	m.Primary = r.Primary
	m.Fallbacks = r.Fallbacks
	return nil
}

func (m AgentModelConfig) MarshalJSON() ([]byte, error) {
	if len(m.Fallbacks) == 0 && m.Primary != "" {
		return json.Marshal(m.Primary)
	}
	type raw struct {
		Primary   string   `json:"primary,omitempty"`
		Fallbacks []string `json:"fallbacks,omitempty"`
	}
	return json.Marshal(raw{Primary: m.Primary, Fallbacks: m.Fallbacks})
}

type AgentConfig struct {
	ID        string            `json:"id"`
	Default   bool              `json:"default,omitempty"`
	Name      string            `json:"name,omitempty"`
	Workspace string            `json:"workspace,omitempty"`
	Model     *AgentModelConfig `json:"model,omitempty"`
	Skills    []string          `json:"skills,omitempty"`
	Subagents *SubagentsConfig  `json:"subagents,omitempty"`
}

type SubagentsConfig struct {
	AllowAgents []string          `json:"allow_agents,omitempty"`
	Model       *AgentModelConfig `json:"model,omitempty"`
}

type PeerMatch struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type BindingMatch struct {
	Channel   string     `json:"channel"`
	AccountID string     `json:"account_id,omitempty"`
	Peer      *PeerMatch `json:"peer,omitempty"`
	GuildID   string     `json:"guild_id,omitempty"`
	TeamID    string     `json:"team_id,omitempty"`
}

type AgentBinding struct {
	AgentID string       `json:"agent_id"`
	Match   BindingMatch `json:"match"`
}

type SessionConfig struct {
	DMScope       string              `json:"dm_scope,omitempty"`
	IdentityLinks map[string][]string `json:"identity_links,omitempty"`
}

// LoopControlConfig controls per-turn agent execution limits.
// It mirrors the top-level shape used by modern model CLIs while keeping
// backward compatibility with agents.defaults.max_tool_iterations.
type LoopControlConfig struct {
	MaxStepsPerTurn   int `json:"max_steps_per_turn,omitempty"`
	MaxRetriesPerStep int `json:"max_retries_per_step,omitempty"`
	ReservedContext   int `json:"reserved_context_size,omitempty"`
}

func (c LoopControlConfig) IsEmpty() bool {
	return c.MaxStepsPerTurn == 0 && c.MaxRetriesPerStep == 0 && c.ReservedContext == 0
}

// RoutingConfig controls the intelligent model routing feature.
// When enabled, each incoming message is scored against structural features
// (message length, code blocks, tool call history, conversation depth, attachments).
// Messages scoring below Threshold are sent to LightModel; all others use the
// agent's primary model. This reduces cost and latency for simple tasks without
// requiring any keyword matching - all scoring is language-agnostic.
type RoutingConfig struct {
	Enabled    bool    `json:"enabled"`
	LightModel string  `json:"light_model"` // model_name from model_list to use for simple tasks
	Threshold  float64 `json:"threshold"`   // complexity score in [0,1]; score >= threshold -> primary model
}

type AgentDefaults struct {
	Workspace                 string         `json:"workspace"                       env:"PICOCLAW_AGENTS_DEFAULTS_WORKSPACE"`
	RestrictToWorkspace       bool           `json:"restrict_to_workspace"           env:"PICOCLAW_AGENTS_DEFAULTS_RESTRICT_TO_WORKSPACE"`
	AllowReadOutsideWorkspace bool           `json:"allow_read_outside_workspace"    env:"PICOCLAW_AGENTS_DEFAULTS_ALLOW_READ_OUTSIDE_WORKSPACE"`
	Provider                  string         `json:"provider,omitempty"              env:"PICOCLAW_AGENTS_DEFAULTS_PROVIDER"`
	ModelName                 string         `json:"model_name,omitempty"            env:"PICOCLAW_AGENTS_DEFAULTS_MODEL_NAME"`
	Model                     string         `json:"model,omitempty"                 env:"PICOCLAW_AGENTS_DEFAULTS_MODEL"` // Primary model alias selected by this agent
	ModelFallbacks            []string       `json:"model_fallbacks,omitempty"`                                            // Deprecated: use agent.model or models config instead
	ImageModel                string         `json:"image_model,omitempty"           env:"PICOCLAW_AGENTS_DEFAULTS_IMAGE_MODEL"`
	ImageModelFallbacks       []string       `json:"image_model_fallbacks,omitempty"`
	MaxToolIterations         int            `json:"max_tool_iterations,omitempty"   env:"PICOCLAW_AGENTS_DEFAULTS_MAX_TOOL_ITERATIONS"` // Deprecated: use loop_control.max_steps_per_turn
	SummarizeMessageThreshold int            `json:"summarize_message_threshold"     env:"PICOCLAW_AGENTS_DEFAULTS_SUMMARIZE_MESSAGE_THRESHOLD"`
	SummarizeTokenPercent     int            `json:"summarize_token_percent"         env:"PICOCLAW_AGENTS_DEFAULTS_SUMMARIZE_TOKEN_PERCENT"`
	MaxMediaSize              int            `json:"max_media_size,omitempty"        env:"PICOCLAW_AGENTS_DEFAULTS_MAX_MEDIA_SIZE"`
	Routing                   *RoutingConfig `json:"routing,omitempty"`
}

// MarshalJSON keeps legacy agent defaults readable on input but omits fields
// that are no longer valid in the new config layout when saving config.
func (d AgentDefaults) MarshalJSON() ([]byte, error) {
	type visible struct {
		Workspace                 string         `json:"workspace,omitempty"`
		RestrictToWorkspace       bool           `json:"restrict_to_workspace,omitempty"`
		AllowReadOutsideWorkspace bool           `json:"allow_read_outside_workspace,omitempty"`
		Model                     string         `json:"model,omitempty"`
		SummarizeMessageThreshold int            `json:"summarize_message_threshold,omitempty"`
		SummarizeTokenPercent     int            `json:"summarize_token_percent,omitempty"`
		MaxMediaSize              int            `json:"max_media_size,omitempty"`
		Routing                   *RoutingConfig `json:"routing,omitempty"`
	}

	return json.Marshal(visible{
		Workspace:                 d.Workspace,
		RestrictToWorkspace:       d.RestrictToWorkspace,
		AllowReadOutsideWorkspace: d.AllowReadOutsideWorkspace,
		Model:                     d.GetModelName(),
		SummarizeMessageThreshold: d.SummarizeMessageThreshold,
		SummarizeTokenPercent:     d.SummarizeTokenPercent,
		MaxMediaSize:              d.MaxMediaSize,
		Routing:                   d.Routing,
	})
}

const DefaultMaxMediaSize = 20 * 1024 * 1024 // 20 MB

func (d *AgentDefaults) GetMaxMediaSize() int {
	if d.MaxMediaSize > 0 {
		return d.MaxMediaSize
	}
	return DefaultMaxMediaSize
}

// GetModelName returns the effective model name for the agent defaults.
// It prefers the new "model_name" field but falls back to "model" for backward compatibility.
func (d *AgentDefaults) GetModelName() string {
	if d.ModelName != "" {
		return d.ModelName
	}
	return d.Model
}

type ChannelsConfig struct {
	Feishu   FeishuConfig   `json:"feishu"`
	QQBot    QQBotConfig    `json:"qqbot"`
	DingTalk DingTalkConfig `json:"dingtalk"`
	OneBot   OneBotConfig   `json:"onebot"`
	Pico     PicoConfig     `json:"pico"`
}

// GroupTriggerConfig controls when the bot responds in group chats.
type GroupTriggerConfig struct {
	MentionOnly bool     `json:"mention_only,omitempty"`
	Prefixes    []string `json:"prefixes,omitempty"`
}

// TypingConfig controls typing indicator behavior (Phase 10).
type TypingConfig struct {
	Enabled bool `json:"enabled,omitempty"`
}

// PlaceholderConfig controls placeholder message behavior (Phase 10).
type PlaceholderConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Text    string `json:"text,omitempty"`
}

type FeishuConfig struct {
	Enabled            bool                `json:"enabled"                 env:"PICOCLAW_CHANNELS_FEISHU_ENABLED"`
	AppID              string              `json:"app_id"                  env:"PICOCLAW_CHANNELS_FEISHU_APP_ID"`
	AppSecret          string              `json:"app_secret"              env:"PICOCLAW_CHANNELS_FEISHU_APP_SECRET"`
	EncryptKey         string              `json:"encrypt_key"             env:"PICOCLAW_CHANNELS_FEISHU_ENCRYPT_KEY"`
	VerificationToken  string              `json:"verification_token"      env:"PICOCLAW_CHANNELS_FEISHU_VERIFICATION_TOKEN"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"              env:"PICOCLAW_CHANNELS_FEISHU_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty"`
	Placeholder        PlaceholderConfig   `json:"placeholder,omitempty"`
	ReasoningChannelID string              `json:"reasoning_channel_id"    env:"PICOCLAW_CHANNELS_FEISHU_REASONING_CHANNEL_ID"`
}

type QQBotConfig struct {
	Enabled            bool                `json:"enabled"                 env:"PICOCLAW_CHANNELS_QQBOT_ENABLED"`
	AppID              string              `json:"app_id"                  env:"PICOCLAW_CHANNELS_QQBOT_APP_ID"`
	ClientSecret       string              `json:"client_secret"           env:"PICOCLAW_CHANNELS_QQBOT_CLIENT_SECRET"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"              env:"PICOCLAW_CHANNELS_QQBOT_ALLOW_FROM"`
	GroupAllowFrom     FlexibleStringSlice `json:"group_allow_from"        env:"PICOCLAW_CHANNELS_QQBOT_GROUP_ALLOW_FROM"`
	MarkdownSupport    bool                `json:"markdown_support"        env:"PICOCLAW_CHANNELS_QQBOT_MARKDOWN_SUPPORT"`
	DmPolicy           string              `json:"dm_policy"               env:"PICOCLAW_CHANNELS_QQBOT_DM_POLICY"`
	GroupPolicy        string              `json:"group_policy"            env:"PICOCLAW_CHANNELS_QQBOT_GROUP_POLICY"`
	RequireMention     bool                `json:"require_mention"         env:"PICOCLAW_CHANNELS_QQBOT_REQUIRE_MENTION"`
	TextChunkLimit     int                 `json:"text_chunk_limit"        env:"PICOCLAW_CHANNELS_QQBOT_TEXT_CHUNK_LIMIT"`
	MaxFileSizeMB      int                 `json:"max_file_size_mb"        env:"PICOCLAW_CHANNELS_QQBOT_MAX_FILE_SIZE_MB"`
	MediaTimeoutMs     int                 `json:"media_timeout_ms"        env:"PICOCLAW_CHANNELS_QQBOT_MEDIA_TIMEOUT_MS"`
	ReasoningChannelID string              `json:"reasoning_channel_id"    env:"PICOCLAW_CHANNELS_QQBOT_REASONING_CHANNEL_ID"`
}

type DingTalkConfig struct {
	Enabled            bool                `json:"enabled"                 env:"PICOCLAW_CHANNELS_DINGTALK_ENABLED"`
	ClientID           string              `json:"client_id"               env:"PICOCLAW_CHANNELS_DINGTALK_CLIENT_ID"`
	ClientSecret       string              `json:"client_secret"           env:"PICOCLAW_CHANNELS_DINGTALK_CLIENT_SECRET"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"              env:"PICOCLAW_CHANNELS_DINGTALK_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty"`
	ReasoningChannelID string              `json:"reasoning_channel_id"    env:"PICOCLAW_CHANNELS_DINGTALK_REASONING_CHANNEL_ID"`
}
type OneBotConfig struct {
	Enabled            bool                `json:"enabled"                 env:"PICOCLAW_CHANNELS_ONEBOT_ENABLED"`
	WSUrl              string              `json:"ws_url"                  env:"PICOCLAW_CHANNELS_ONEBOT_WS_URL"`
	AccessToken        string              `json:"access_token"            env:"PICOCLAW_CHANNELS_ONEBOT_ACCESS_TOKEN"`
	ReconnectInterval  int                 `json:"reconnect_interval"      env:"PICOCLAW_CHANNELS_ONEBOT_RECONNECT_INTERVAL"`
	GroupTriggerPrefix []string            `json:"group_trigger_prefix"    env:"PICOCLAW_CHANNELS_ONEBOT_GROUP_TRIGGER_PREFIX"`
	AllowFrom          FlexibleStringSlice `json:"allow_from"              env:"PICOCLAW_CHANNELS_ONEBOT_ALLOW_FROM"`
	GroupTrigger       GroupTriggerConfig  `json:"group_trigger,omitempty"`
	Typing             TypingConfig        `json:"typing,omitempty"`
	Placeholder        PlaceholderConfig   `json:"placeholder,omitempty"`
	ReasoningChannelID string              `json:"reasoning_channel_id"    env:"PICOCLAW_CHANNELS_ONEBOT_REASONING_CHANNEL_ID"`
}
type PicoConfig struct {
	Enabled         bool                `json:"enabled"                     env:"PICOCLAW_CHANNELS_PICO_ENABLED"`
	Token           string              `json:"token"                       env:"PICOCLAW_CHANNELS_PICO_TOKEN"`
	AllowTokenQuery bool                `json:"allow_token_query,omitempty"`
	AllowOrigins    []string            `json:"allow_origins,omitempty"`
	PingInterval    int                 `json:"ping_interval,omitempty"`
	ReadTimeout     int                 `json:"read_timeout,omitempty"`
	WriteTimeout    int                 `json:"write_timeout,omitempty"`
	MaxConnections  int                 `json:"max_connections,omitempty"`
	AllowFrom       FlexibleStringSlice `json:"allow_from"                  env:"PICOCLAW_CHANNELS_PICO_ALLOW_FROM"`
	Placeholder     PlaceholderConfig   `json:"placeholder,omitempty"`
}
type HeartbeatConfig struct {
	Enabled  bool `json:"enabled"  env:"PICOCLAW_HEARTBEAT_ENABLED"`
	Interval int  `json:"interval" env:"PICOCLAW_HEARTBEAT_INTERVAL"` // minutes, min 5
}

type DevicesConfig struct {
	Enabled    bool `json:"enabled"     env:"PICOCLAW_DEVICES_ENABLED"`
	MonitorUSB bool `json:"monitor_usb" env:"PICOCLAW_DEVICES_MONITOR_USB"`
}

type ProvidersConfig struct {
	Anthropic     ProviderConfig            `json:"-"`
	OpenAI        ProviderConfig            `json:"-"`
	LiteLLM       ProviderConfig            `json:"-"`
	OpenRouter    ProviderConfig            `json:"-"`
	Groq          ProviderConfig            `json:"-"`
	Zhipu         ProviderConfig            `json:"-"`
	VLLM          ProviderConfig            `json:"-"`
	Gemini        ProviderConfig            `json:"-"`
	Nvidia        ProviderConfig            `json:"-"`
	Ollama        ProviderConfig            `json:"-"`
	Moonshot      ProviderConfig            `json:"-"`
	ShengSuanYun  ProviderConfig            `json:"-"`
	DeepSeek      ProviderConfig            `json:"-"`
	Cerebras      ProviderConfig            `json:"-"`
	Vivgrid       ProviderConfig            `json:"-"`
	VolcEngine    ProviderConfig            `json:"-"`
	GitHubCopilot ProviderConfig            `json:"-"`
	Antigravity   ProviderConfig            `json:"-"`
	Qwen          ProviderConfig            `json:"-"`
	Mistral       ProviderConfig            `json:"-"`
	Avian         ProviderConfig            `json:"-"`
	Named         map[string]ProviderConfig `json:"-"`
}

// IsEmpty checks if all provider configs are empty (no API keys or API bases set)
// Note: WebSearch is an optimization option and doesn't count as "non-empty".
func (p ProvidersConfig) IsEmpty() bool {
	for _, cfg := range p.All() {
		if !cfg.IsZero() {
			return false
		}
	}
	return true
}

// MarshalJSON implements custom JSON marshaling for ProvidersConfig
// to omit the entire section when empty
func (p ProvidersConfig) MarshalJSON() ([]byte, error) {
	if p.IsEmpty() {
		return []byte("null"), nil
	}
	return json.Marshal(p.All())
}

func (p *ProvidersConfig) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || len(data) == 0 {
		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if p.Named == nil {
		p.Named = make(map[string]ProviderConfig)
	}

	for name, payload := range raw {
		if name == "_comment" {
			continue
		}
		cfg := p.Get(name)
		if err := json.Unmarshal(payload, &cfg); err != nil {
			return fmt.Errorf("providers.%s: %w", name, err)
		}
		if cfg.Type == "" {
			cfg.Type = NormalizeProviderName(name)
		}
		p.Set(name, cfg)
	}
	return nil
}

func (p ProvidersConfig) All() map[string]ProviderConfig {
	all := map[string]ProviderConfig{}
	appendIfConfigured := func(name string, cfg ProviderConfig) {
		if cfg.IsZero() && cfg.Type == "" {
			return
		}
		if cfg.Type == "" {
			cfg.Type = NormalizeProviderName(name)
		}
		all[name] = cfg
	}

	if len(p.Named) > 0 {
		for name, cfg := range p.Named {
			appendIfConfigured(name, cfg)
		}
		return all
	}

	appendIfConfigured("anthropic", p.Anthropic)
	appendIfConfigured("openai", p.OpenAI)
	appendIfConfigured("litellm", p.LiteLLM)
	appendIfConfigured("openrouter", p.OpenRouter)
	appendIfConfigured("groq", p.Groq)
	appendIfConfigured("zhipu", p.Zhipu)
	appendIfConfigured("vllm", p.VLLM)
	appendIfConfigured("gemini", p.Gemini)
	appendIfConfigured("nvidia", p.Nvidia)
	appendIfConfigured("ollama", p.Ollama)
	appendIfConfigured("moonshot", p.Moonshot)
	appendIfConfigured("shengsuanyun", p.ShengSuanYun)
	appendIfConfigured("deepseek", p.DeepSeek)
	appendIfConfigured("cerebras", p.Cerebras)
	appendIfConfigured("vivgrid", p.Vivgrid)
	appendIfConfigured("volcengine", p.VolcEngine)
	appendIfConfigured("github_copilot", p.GitHubCopilot)
	appendIfConfigured("antigravity", p.Antigravity)
	appendIfConfigured("qwen", p.Qwen)
	appendIfConfigured("mistral", p.Mistral)
	appendIfConfigured("avian", p.Avian)
	return all
}

func (p ProvidersConfig) Get(name string) ProviderConfig {
	name = NormalizeProviderName(name)
	switch name {
	case "anthropic":
		return p.Anthropic
	case "openai":
		return p.OpenAI
	case "litellm":
		return p.LiteLLM
	case "openrouter":
		return p.OpenRouter
	case "groq":
		return p.Groq
	case "zhipu":
		return p.Zhipu
	case "vllm":
		return p.VLLM
	case "gemini":
		return p.Gemini
	case "nvidia":
		return p.Nvidia
	case "ollama":
		return p.Ollama
	case "moonshot":
		return p.Moonshot
	case "shengsuanyun":
		return p.ShengSuanYun
	case "deepseek":
		return p.DeepSeek
	case "cerebras":
		return p.Cerebras
	case "vivgrid":
		return p.Vivgrid
	case "volcengine":
		return p.VolcEngine
	case "github_copilot":
		return p.GitHubCopilot
	case "antigravity":
		return p.Antigravity
	case "qwen":
		return p.Qwen
	case "mistral":
		return p.Mistral
	case "avian":
		return p.Avian
	default:
		if p.Named == nil {
			return ProviderConfig{}
		}
		return p.Named[name]
	}
}

func (p *ProvidersConfig) Set(name string, cfg ProviderConfig) {
	name = NormalizeProviderName(name)
	if p.Named == nil {
		p.Named = make(map[string]ProviderConfig)
	}
	p.Named[name] = cfg

	switch name {
	case "anthropic":
		p.Anthropic = cfg
	case "openai":
		p.OpenAI = cfg
	case "litellm":
		p.LiteLLM = cfg
	case "openrouter":
		p.OpenRouter = cfg
	case "groq":
		p.Groq = cfg
	case "zhipu":
		p.Zhipu = cfg
	case "vllm":
		p.VLLM = cfg
	case "gemini":
		p.Gemini = cfg
	case "nvidia":
		p.Nvidia = cfg
	case "ollama":
		p.Ollama = cfg
	case "moonshot":
		p.Moonshot = cfg
	case "shengsuanyun":
		p.ShengSuanYun = cfg
	case "deepseek":
		p.DeepSeek = cfg
	case "cerebras":
		p.Cerebras = cfg
	case "vivgrid":
		p.Vivgrid = cfg
	case "volcengine":
		p.VolcEngine = cfg
	case "github_copilot":
		p.GitHubCopilot = cfg
	case "antigravity":
		p.Antigravity = cfg
	case "qwen":
		p.Qwen = cfg
	case "mistral":
		p.Mistral = cfg
	case "avian":
		p.Avian = cfg
	}
}

type ProviderConfig struct {
	Type           string `json:"type,omitempty"`
	APIKey         string `json:"api_key"                   env:"PICOCLAW_PROVIDERS_{{.Name}}_API_KEY"`
	APIBase        string `json:"api_base"                  env:"PICOCLAW_PROVIDERS_{{.Name}}_API_BASE"`
	Proxy          string `json:"proxy,omitempty"           env:"PICOCLAW_PROVIDERS_{{.Name}}_PROXY"`
	RequestTimeout int    `json:"request_timeout,omitempty" env:"PICOCLAW_PROVIDERS_{{.Name}}_REQUEST_TIMEOUT"`
	AuthMethod     string `json:"auth_method,omitempty"     env:"PICOCLAW_PROVIDERS_{{.Name}}_AUTH_METHOD"`
	ConnectMode    string `json:"connect_mode,omitempty"    env:"PICOCLAW_PROVIDERS_{{.Name}}_CONNECT_MODE"` // only for Github Copilot, `stdio` or `grpc`
	Workspace      string `json:"workspace,omitempty"`
	WebSearch      bool   `json:"web_search,omitempty"      env:"PICOCLAW_PROVIDERS_OPENAI_WEB_SEARCH"`
}

func (c *ProviderConfig) UnmarshalJSON(data []byte) error {
	type alias ProviderConfig
	var raw struct {
		alias
		BaseURL string `json:"base_url,omitempty"`
	}
	raw.alias = alias(*c)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = ProviderConfig(raw.alias)
	if c.APIBase == "" && raw.BaseURL != "" {
		c.APIBase = raw.BaseURL
	}
	return nil
}

func (c ProviderConfig) IsZero() bool {
	return c.APIKey == "" &&
		c.APIBase == "" &&
		c.Proxy == "" &&
		c.RequestTimeout == 0 &&
		c.AuthMethod == "" &&
		c.ConnectMode == "" &&
		c.Workspace == ""
}

type ModelsConfig map[string]ModelVariants

type ModelVariants []ModelDefinition

func (v *ModelVariants) UnmarshalJSON(data []byte) error {
	var single ModelDefinition
	if err := json.Unmarshal(data, &single); err == nil && single.Provider != "" {
		*v = ModelVariants{single}
		return nil
	}

	var many []ModelDefinition
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*v = ModelVariants(many)
	return nil
}

func (v ModelVariants) MarshalJSON() ([]byte, error) {
	if len(v) == 1 {
		return json.Marshal(v[0])
	}
	return json.Marshal([]ModelDefinition(v))
}

type ModelDefinition struct {
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	RPM            int      `json:"rpm,omitempty"`
	MaxTokens      int      `json:"max_tokens,omitempty"`
	MaxTokensField string   `json:"max_tokens_field,omitempty"`
	MaxContextSize int      `json:"max_context_size,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
	TopP           *float64 `json:"top_p,omitempty"`
	ThinkingLevel  string   `json:"thinking_level,omitempty"`
	TokenCountAPI  string   `json:"token_count_api,omitempty"`
	Capabilities   []string `json:"capabilities,omitempty"`
}

// ModelConfig is the resolved provider+model configuration used internally.
type ModelConfig struct {
	// Required fields
	ModelName string `json:"model_name"`         // User-facing alias for the model
	Provider  string `json:"provider,omitempty"` // Named provider entry for new-style config
	Model     string `json:"model"`              // Protocol/model-identifier (e.g., "openai/gpt-4o", "anthropic/claude-sonnet-4.6")

	// HTTP-based providers
	APIBase string `json:"api_base,omitempty"` // API endpoint URL
	APIKey  string `json:"api_key"`            // API authentication key
	Proxy   string `json:"proxy,omitempty"`    // HTTP proxy URL

	// Special providers (CLI-based, OAuth, etc.)
	AuthMethod  string `json:"auth_method,omitempty"`  // Authentication method: oauth, token
	ConnectMode string `json:"connect_mode,omitempty"` // Connection mode: stdio, grpc
	Workspace   string `json:"workspace,omitempty"`    // Workspace path for CLI-based providers

	// Optional optimizations
	MaxTokens      int      `json:"max_tokens,omitempty"`
	RPM            int      `json:"rpm,omitempty"`              // Requests per minute limit
	MaxTokensField string   `json:"max_tokens_field,omitempty"` // Field name for max tokens (e.g., "max_completion_tokens")
	RequestTimeout int      `json:"request_timeout,omitempty"`
	MaxContextSize int      `json:"max_context_size,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
	TopP           *float64 `json:"top_p,omitempty"`
	ThinkingLevel  string   `json:"thinking_level,omitempty"` // Extended thinking: off|low|medium|high|xhigh|adaptive
	TokenCountAPI  string   `json:"token_count_api,omitempty"`
}

// Validate checks if the ModelConfig has all required fields.
func (c *ModelConfig) Validate() error {
	if c.ModelName == "" {
		return fmt.Errorf("model_name is required")
	}
	if c.Model == "" {
		return fmt.Errorf("model is required")
	}
	return nil
}

type GatewayConfig struct {
	Host string `json:"host" env:"PICOCLAW_GATEWAY_HOST"`
	Port int    `json:"port" env:"PICOCLAW_GATEWAY_PORT"`
}

type ToolConfig struct {
	Enabled bool `json:"enabled" env:"ENABLED"`
}

type BraveConfig struct {
	Enabled    bool   `json:"enabled"     env:"PICOCLAW_TOOLS_WEB_BRAVE_ENABLED"`
	APIKey     string `json:"api_key"     env:"PICOCLAW_TOOLS_WEB_BRAVE_API_KEY"`
	MaxResults int    `json:"max_results" env:"PICOCLAW_TOOLS_WEB_BRAVE_MAX_RESULTS"`
}

type TavilyConfig struct {
	Enabled    bool   `json:"enabled"     env:"PICOCLAW_TOOLS_WEB_TAVILY_ENABLED"`
	APIKey     string `json:"api_key"     env:"PICOCLAW_TOOLS_WEB_TAVILY_API_KEY"`
	BaseURL    string `json:"base_url"    env:"PICOCLAW_TOOLS_WEB_TAVILY_BASE_URL"`
	MaxResults int    `json:"max_results" env:"PICOCLAW_TOOLS_WEB_TAVILY_MAX_RESULTS"`
}

type DuckDuckGoConfig struct {
	Enabled    bool `json:"enabled"     env:"PICOCLAW_TOOLS_WEB_DUCKDUCKGO_ENABLED"`
	MaxResults int  `json:"max_results" env:"PICOCLAW_TOOLS_WEB_DUCKDUCKGO_MAX_RESULTS"`
}

type PerplexityConfig struct {
	Enabled    bool   `json:"enabled"     env:"PICOCLAW_TOOLS_WEB_PERPLEXITY_ENABLED"`
	APIKey     string `json:"api_key"     env:"PICOCLAW_TOOLS_WEB_PERPLEXITY_API_KEY"`
	MaxResults int    `json:"max_results" env:"PICOCLAW_TOOLS_WEB_PERPLEXITY_MAX_RESULTS"`
}

type SearXNGConfig struct {
	Enabled    bool   `json:"enabled"     env:"PICOCLAW_TOOLS_WEB_SEARXNG_ENABLED"`
	BaseURL    string `json:"base_url"    env:"PICOCLAW_TOOLS_WEB_SEARXNG_BASE_URL"`
	MaxResults int    `json:"max_results" env:"PICOCLAW_TOOLS_WEB_SEARXNG_MAX_RESULTS"`
}

type GLMSearchConfig struct {
	Enabled bool   `json:"enabled"  env:"PICOCLAW_TOOLS_WEB_GLM_ENABLED"`
	APIKey  string `json:"api_key"  env:"PICOCLAW_TOOLS_WEB_GLM_API_KEY"`
	BaseURL string `json:"base_url" env:"PICOCLAW_TOOLS_WEB_GLM_BASE_URL"`
	// SearchEngine specifies the search backend: "search_std" (default),
	// "search_pro", "search_pro_sogou", or "search_pro_quark".
	SearchEngine string `json:"search_engine" env:"PICOCLAW_TOOLS_WEB_GLM_SEARCH_ENGINE"`
	MaxResults   int    `json:"max_results"   env:"PICOCLAW_TOOLS_WEB_GLM_MAX_RESULTS"`
}

type WebToolsConfig struct {
	ToolConfig `                 envPrefix:"PICOCLAW_TOOLS_WEB_"`
	Brave      BraveConfig      `                                json:"brave"`
	Tavily     TavilyConfig     `                                json:"tavily"`
	DuckDuckGo DuckDuckGoConfig `                                json:"duckduckgo"`
	Perplexity PerplexityConfig `                                json:"perplexity"`
	SearXNG    SearXNGConfig    `                                json:"searxng"`
	GLMSearch  GLMSearchConfig  `                                json:"glm_search"`
	// Proxy is an optional proxy URL for web tools (http/https/socks5/socks5h).
	// For authenticated proxies, prefer HTTP_PROXY/HTTPS_PROXY env vars instead of embedding credentials in config.
	Proxy           string `json:"proxy,omitempty"             env:"PICOCLAW_TOOLS_WEB_PROXY"`
	FetchLimitBytes int64  `json:"fetch_limit_bytes,omitempty" env:"PICOCLAW_TOOLS_WEB_FETCH_LIMIT_BYTES"`
}

type CronToolsConfig struct {
	ToolConfig         `    envPrefix:"PICOCLAW_TOOLS_CRON_"`
	ExecTimeoutMinutes int `                                 env:"PICOCLAW_TOOLS_CRON_EXEC_TIMEOUT_MINUTES" json:"exec_timeout_minutes"` // 0 means no timeout
}

type ExecConfig struct {
	ToolConfig          `         envPrefix:"PICOCLAW_TOOLS_EXEC_"`
	EnableDenyPatterns  bool     `                                 env:"PICOCLAW_TOOLS_EXEC_ENABLE_DENY_PATTERNS"  json:"enable_deny_patterns"`
	CustomDenyPatterns  []string `                                 env:"PICOCLAW_TOOLS_EXEC_CUSTOM_DENY_PATTERNS"  json:"custom_deny_patterns"`
	CustomAllowPatterns []string `                                 env:"PICOCLAW_TOOLS_EXEC_CUSTOM_ALLOW_PATTERNS" json:"custom_allow_patterns"`
	TimeoutSeconds      int      `                                 env:"PICOCLAW_TOOLS_EXEC_TIMEOUT_SECONDS"       json:"timeout_seconds"` // 0 means use default (60s)
}

type SkillsToolsConfig struct {
	ToolConfig            `                       envPrefix:"PICOCLAW_TOOLS_SKILLS_"`
	Registries            SkillsRegistriesConfig `                                   json:"registries"`
	MaxConcurrentSearches int                    `                                   json:"max_concurrent_searches" env:"PICOCLAW_TOOLS_SKILLS_MAX_CONCURRENT_SEARCHES"`
	SearchCache           SearchCacheConfig      `                                   json:"search_cache"`
}

type MediaCleanupConfig struct {
	ToolConfig `    envPrefix:"PICOCLAW_MEDIA_CLEANUP_"`
	MaxAge     int `                                    env:"PICOCLAW_MEDIA_CLEANUP_MAX_AGE"  json:"max_age_minutes"`
	Interval   int `                                    env:"PICOCLAW_MEDIA_CLEANUP_INTERVAL" json:"interval_minutes"`
}

type ToolsConfig struct {
	AllowReadPaths  []string           `json:"allow_read_paths"  env:"PICOCLAW_TOOLS_ALLOW_READ_PATHS"`
	AllowWritePaths []string           `json:"allow_write_paths" env:"PICOCLAW_TOOLS_ALLOW_WRITE_PATHS"`
	Web             WebToolsConfig     `json:"web"`
	Cron            CronToolsConfig    `json:"cron"`
	Exec            ExecConfig         `json:"exec"`
	Skills          SkillsToolsConfig  `json:"skills"`
	MediaCleanup    MediaCleanupConfig `json:"media_cleanup"`
	MCP             MCPConfig          `json:"mcp"`
	AppendFile      ToolConfig         `json:"append_file"                                              envPrefix:"PICOCLAW_TOOLS_APPEND_FILE_"`
	EditFile        ToolConfig         `json:"edit_file"                                                envPrefix:"PICOCLAW_TOOLS_EDIT_FILE_"`
	FindSkills      ToolConfig         `json:"find_skills"                                              envPrefix:"PICOCLAW_TOOLS_FIND_SKILLS_"`
	InstallSkill    ToolConfig         `json:"install_skill"                                            envPrefix:"PICOCLAW_TOOLS_INSTALL_SKILL_"`
	ListDir         ToolConfig         `json:"list_dir"                                                 envPrefix:"PICOCLAW_TOOLS_LIST_DIR_"`
	Message         ToolConfig         `json:"message"                                                  envPrefix:"PICOCLAW_TOOLS_MESSAGE_"`
	ReadFile        ToolConfig         `json:"read_file"                                                envPrefix:"PICOCLAW_TOOLS_READ_FILE_"`
	SendFile        ToolConfig         `json:"send_file"                                                envPrefix:"PICOCLAW_TOOLS_SEND_FILE_"`
	Spawn           ToolConfig         `json:"spawn"                                                    envPrefix:"PICOCLAW_TOOLS_SPAWN_"`
	Subagent        ToolConfig         `json:"subagent"                                                 envPrefix:"PICOCLAW_TOOLS_SUBAGENT_"`
	WebFetch        ToolConfig         `json:"web_fetch"                                                envPrefix:"PICOCLAW_TOOLS_WEB_FETCH_"`
	WriteFile       ToolConfig         `json:"write_file"                                               envPrefix:"PICOCLAW_TOOLS_WRITE_FILE_"`
}

type SearchCacheConfig struct {
	MaxSize    int `json:"max_size"    env:"PICOCLAW_SKILLS_SEARCH_CACHE_MAX_SIZE"`
	TTLSeconds int `json:"ttl_seconds" env:"PICOCLAW_SKILLS_SEARCH_CACHE_TTL_SECONDS"`
}

type SkillsRegistriesConfig struct {
	ClawHub ClawHubRegistryConfig `json:"clawhub"`
}

type ClawHubRegistryConfig struct {
	Enabled         bool   `json:"enabled"           env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_ENABLED"`
	BaseURL         string `json:"base_url"          env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_BASE_URL"`
	AuthToken       string `json:"auth_token"        env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_AUTH_TOKEN"`
	SearchPath      string `json:"search_path"       env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_SEARCH_PATH"`
	SkillsPath      string `json:"skills_path"       env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_SKILLS_PATH"`
	DownloadPath    string `json:"download_path"     env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_DOWNLOAD_PATH"`
	Timeout         int    `json:"timeout"           env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_TIMEOUT"`
	MaxZipSize      int    `json:"max_zip_size"      env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_MAX_ZIP_SIZE"`
	MaxResponseSize int    `json:"max_response_size" env:"PICOCLAW_SKILLS_REGISTRIES_CLAWHUB_MAX_RESPONSE_SIZE"`
}

// MCPServerConfig defines configuration for a single MCP server
type MCPServerConfig struct {
	// Enabled indicates whether this MCP server is active
	Enabled bool `json:"enabled"`
	// Command is the executable to run (e.g., "npx", "python", "/path/to/server")
	Command string `json:"command"`
	// Args are the arguments to pass to the command
	Args []string `json:"args,omitempty"`
	// Env are environment variables to set for the server process (stdio only)
	Env map[string]string `json:"env,omitempty"`
	// EnvFile is the path to a file containing environment variables (stdio only)
	EnvFile string `json:"env_file,omitempty"`
	// Type is "stdio", "sse", or "http" (default: stdio if command is set, sse if url is set)
	Type string `json:"type,omitempty"`
	// URL is used for SSE/HTTP transport
	URL string `json:"url,omitempty"`
	// Headers are HTTP headers to send with requests (sse/http only)
	Headers map[string]string `json:"headers,omitempty"`
}

// MCPConfig defines configuration for all MCP servers
type MCPConfig struct {
	ToolConfig `envPrefix:"PICOCLAW_TOOLS_MCP_"`
	// Servers is a map of server name to server configuration
	Servers map[string]MCPServerConfig `json:"servers,omitempty"`
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	// Pre-scan the JSON to check how many model_list entries the user provided.
	// Go's JSON decoder reuses existing slice backing-array elements rather than
	// zero-initializing them, so fields absent from the user's JSON (e.g. api_base)
	// would silently inherit values from the DefaultConfig template at the same
	// index position. We only reset cfg.ModelList when the user actually provides
	// entries; when count is 0 we keep DefaultConfig's built-in list as fallback.
	var tmp Config
	if err := json.Unmarshal(data, &tmp); err != nil {
		return nil, err
	}
	if len(tmp.Models) > 0 {
		cfg.Models = nil
		cfg.ModelList = nil
	}
	if len(tmp.ModelList) > 0 {
		cfg.ModelList = nil
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	cfg.ApplyCompatibilityDefaults()

	// Migrate legacy channel config fields to new unified structures
	cfg.migrateChannelConfigs()

	switch {
	case len(cfg.Models) > 0:
		cfg.ModelList, err = cfg.ResolveModelListFromModels()
		if err != nil {
			return nil, err
		}
	case len(cfg.ModelList) > 0:
		if cfg.Models == nil {
			cfg.Providers, cfg.Models = ConvertModelListToSeparatedConfig(cfg.ModelList)
		}
	case cfg.HasProvidersConfig():
		cfg.ModelList = ConvertProvidersToModelList(cfg)
		cfg.Providers, cfg.Models = ConvertModelListToSeparatedConfig(cfg.ModelList)
	}

	// Validate model_list for uniqueness and required fields
	if err := cfg.ValidateModelList(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) migrateChannelConfigs() {
	// OneBot: group_trigger_prefix -> group_trigger.prefixes
	if len(c.Channels.OneBot.GroupTriggerPrefix) > 0 &&
		len(c.Channels.OneBot.GroupTrigger.Prefixes) == 0 {
		c.Channels.OneBot.GroupTrigger.Prefixes = c.Channels.OneBot.GroupTriggerPrefix
	}
}

func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// Use unified atomic write utility with explicit sync for flash storage reliability.
	return fileutil.WriteFileAtomic(path, data, 0o600)
}

func (c *Config) WorkspacePath() string {
	return expandHome(c.Agents.Defaults.Workspace)
}

func (c *Config) GetAPIKey() string {
	for _, name := range []string{
		"openrouter", "anthropic", "openai", "gemini", "zhipu", "groq",
		"vllm", "shengsuanyun", "cerebras",
	} {
		if key := c.Providers.Get(name).APIKey; key != "" {
			return key
		}
	}
	for _, mc := range c.ModelList {
		if mc.APIKey != "" {
			return mc.APIKey
		}
	}
	return ""
}

func (c *Config) GetAPIBase() string {
	if p := c.Providers.Get("openrouter"); p.APIKey != "" {
		if p.APIBase != "" {
			return p.APIBase
		}
		return "https://openrouter.ai/api/v1"
	}
	if p := c.Providers.Get("zhipu"); p.APIKey != "" {
		return p.APIBase
	}
	if p := c.Providers.Get("vllm"); p.APIKey != "" && p.APIBase != "" {
		return p.APIBase
	}
	for _, mc := range c.ModelList {
		if mc.APIBase != "" {
			return mc.APIBase
		}
	}
	return ""
}

func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		if len(path) > 1 && path[1] == '/' {
			return home + path[1:]
		}
		return home
	}
	return path
}

// GetModelConfig returns the ModelConfig for the given model name.
// If multiple configs exist with the same model_name, it uses round-robin
// selection for load balancing. Returns an error if the model is not found.
func (c *Config) GetModelConfig(modelName string) (*ModelConfig, error) {
	matches := c.findMatches(modelName)
	if len(matches) == 0 {
		return nil, fmt.Errorf("model %q not found in models, model_list, or providers", modelName)
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}

	// Multiple configs - use round-robin for load balancing
	idx := rrCounter.Add(1) % uint64(len(matches))
	return &matches[idx], nil
}

// findMatches finds all ModelConfig entries with the given model_name.
func (c *Config) findMatches(modelName string) []ModelConfig {
	var matches []ModelConfig
	for i := range c.ModelList {
		if c.ModelList[i].ModelName == modelName {
			matches = append(matches, c.ModelList[i])
		}
	}
	return matches
}

// HasProvidersConfig checks if any provider in the old providers config has configuration.
func (c *Config) HasProvidersConfig() bool {
	return !c.Providers.IsEmpty()
}

// ValidateModelList validates all ModelConfig entries in the model_list.
// It checks that each model config is valid.
// Note: Multiple entries with the same model_name are allowed for load balancing.
func (c *Config) ValidateModelList() error {
	for i := range c.ModelList {
		if err := c.ModelList[i].Validate(); err != nil {
			return fmt.Errorf("model_list[%d]: %w", i, err)
		}
	}
	return nil
}

func (t *ToolsConfig) IsToolEnabled(name string) bool {
	switch name {
	case "web":
		return t.Web.Enabled
	case "cron":
		return t.Cron.Enabled
	case "exec":
		return t.Exec.Enabled
	case "skills":
		return t.Skills.Enabled
	case "media_cleanup":
		return t.MediaCleanup.Enabled
	case "append_file":
		return t.AppendFile.Enabled
	case "edit_file":
		return t.EditFile.Enabled
	case "find_skills":
		return t.FindSkills.Enabled
	case "install_skill":
		return t.InstallSkill.Enabled
	case "list_dir":
		return t.ListDir.Enabled
	case "message":
		return t.Message.Enabled
	case "read_file":
		return t.ReadFile.Enabled
	case "spawn":
		return t.Spawn.Enabled
	case "subagent":
		return t.Subagent.Enabled
	case "web_fetch":
		return t.WebFetch.Enabled
	case "send_file":
		return t.SendFile.Enabled
	case "write_file":
		return t.WriteFile.Enabled
	case "mcp":
		return t.MCP.Enabled
	default:
		return true
	}
}
