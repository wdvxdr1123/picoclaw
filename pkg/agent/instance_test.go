package agent

import (
	"os"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestNewAgentInstance_UsesModelGenerationSettings(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configuredTemp := 1.0
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxToolIterations: 5,
			},
		},
		ModelList: []config.ModelConfig{{
			ModelName:   "test-model",
			Model:       "openai/gpt-5.2",
			MaxTokens:   1234,
			Temperature: &configuredTemp,
		}},
	}

	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

	if providers.EffectiveMaxTokens(agent.ResolvedModelConfig) != 1234 {
		t.Fatalf("EffectiveMaxTokens = %d, want %d", providers.EffectiveMaxTokens(agent.ResolvedModelConfig), 1234)
	}
	if temp, ok := providers.EffectiveTemperature(agent.ResolvedModelConfig); !ok || temp != 1.0 {
		t.Fatalf("EffectiveTemperature = (%v, %v), want (1.0, true)", temp, ok)
	}
}

func TestNewAgentInstance_UsesModelTemperatureWhenZero(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configuredTemp := 0.0
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxToolIterations: 5,
			},
		},
		ModelList: []config.ModelConfig{{
			ModelName:   "test-model",
			Model:       "openai/gpt-5.2",
			MaxTokens:   1234,
			Temperature: &configuredTemp,
		}},
	}

	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

	if temp, ok := providers.EffectiveTemperature(agent.ResolvedModelConfig); !ok || temp != 0.0 {
		t.Fatalf("EffectiveTemperature = (%v, %v), want (0.0, true)", temp, ok)
	}
}

func TestNewAgentInstance_DefaultsTopPWhenUnset(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxToolIterations: 5,
			},
		},
		ModelList: []config.ModelConfig{{
			ModelName: "test-model",
			Model:     "openai/gpt-5.2",
			MaxTokens: 1234,
		}},
	}

	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

	if providers.EffectiveTopP(agent.ResolvedModelConfig) != 0.95 {
		t.Fatalf("EffectiveTopP = %f, want %f", providers.EffectiveTopP(agent.ResolvedModelConfig), 0.95)
	}
	if _, ok := providers.EffectiveTemperature(agent.ResolvedModelConfig); ok {
		t.Fatal("EffectiveTemperature should be unset when model temperature is not configured")
	}
}

func TestNewAgentInstance_ResolveCandidatesFromModelListAlias(t *testing.T) {
	tests := []struct {
		name         string
		aliasName    string
		modelName    string
		apiBase      string
		wantProvider string
		wantModel    string
	}{
		{
			name:         "alias with provider prefix",
			aliasName:    "step-3.5-flash",
			modelName:    "openrouter/stepfun/step-3.5-flash:free",
			apiBase:      "https://openrouter.ai/api/v1",
			wantProvider: "openrouter",
			wantModel:    "stepfun/step-3.5-flash:free",
		},
		{
			name:         "alias without provider prefix",
			aliasName:    "glm-5",
			modelName:    "glm-5",
			apiBase:      "https://api.z.ai/api/coding/paas/v4",
			wantProvider: "openai",
			wantModel:    "glm-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			cfg := &config.Config{
				Agents: config.AgentsConfig{
					Defaults: config.AgentDefaults{
						Workspace: tmpDir,
						Model:     tt.aliasName,
					},
				},
				ModelList: []config.ModelConfig{
					{
						ModelName: tt.aliasName,
						Model:     tt.modelName,
						APIBase:   tt.apiBase,
					},
				},
			}

			provider := &mockProvider{}
			agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

			if len(agent.Candidates) != 1 {
				t.Fatalf("len(Candidates) = %d, want 1", len(agent.Candidates))
			}
			if agent.Candidates[0].Provider != tt.wantProvider {
				t.Fatalf("candidate provider = %q, want %q", agent.Candidates[0].Provider, tt.wantProvider)
			}
			if agent.Candidates[0].Model != tt.wantModel {
				t.Fatalf("candidate model = %q, want %q", agent.Candidates[0].Model, tt.wantModel)
			}
		})
	}
}

func TestNewAgentInstance_PrefersModelConfigForGenerationSettings(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	modelTemp := 0.15
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "coder",
				MaxToolIterations: 5,
			},
		},
		ModelList: []config.ModelConfig{
			{
				ModelName:      "coder",
				Model:          "openai/gpt-5.2",
				MaxTokens:      4096,
				Temperature:    &modelTemp,
				MaxContextSize: 200000,
			},
		},
	}

	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

	if providers.EffectiveMaxTokens(agent.ResolvedModelConfig) != 4096 {
		t.Fatalf("EffectiveMaxTokens = %d, want %d", providers.EffectiveMaxTokens(agent.ResolvedModelConfig), 4096)
	}
	if temp, ok := providers.EffectiveTemperature(agent.ResolvedModelConfig); !ok || temp != 0.15 {
		t.Fatalf("EffectiveTemperature = (%v, %v), want (0.15, true)", temp, ok)
	}
	if providers.EffectiveContextWindow(agent.ResolvedModelConfig) != 200000 {
		t.Fatalf("EffectiveContextWindow = %d, want %d", providers.EffectiveContextWindow(agent.ResolvedModelConfig), 200000)
	}
	if agent.ResolvedModelConfig == nil {
		t.Fatal("ResolvedModelConfig should not be nil")
	}
}

func TestNewAgentInstance_PrefersLoopControlOverLegacyMaxIterations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		LoopControl: config.LoopControlConfig{
			MaxStepsPerTurn: 7,
		},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxToolIterations: 99,
			},
		},
	}

	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)

	if agent.MaxIterations != 7 {
		t.Fatalf("MaxIterations = %d, want %d", agent.MaxIterations, 7)
	}
}
