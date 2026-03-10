package status

import (
	"fmt"
	"os"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	"github.com/sipeed/picoclaw/pkg/auth"
)

func statusCmd() {
	cfg, err := internal.LoadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	configPath := internal.GetConfigPath()

	fmt.Printf("%s picoclaw Status\n", internal.Logo)
	fmt.Printf("Version: %s\n", internal.FormatVersion())
	build, _ := internal.FormatBuildInfo()
	if build != "" {
		fmt.Printf("Build: %s\n", build)
	}
	fmt.Println()

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("Config:", configPath, "ok")
	} else {
		fmt.Println("Config:", configPath, "missing")
	}

	workspace := cfg.WorkspacePath()
	if _, err := os.Stat(workspace); err == nil {
		fmt.Println("Workspace:", workspace, "ok")
	} else {
		fmt.Println("Workspace:", workspace, "missing")
	}

	if _, err := os.Stat(configPath); err != nil {
		return
	}

	fmt.Printf("Model: %s\n", cfg.GetDefaultModelName())

	status := func(enabled bool) string {
		if enabled {
			return "ok"
		}
		return "not set"
	}

	get := cfg.Providers.Get
	fmt.Println("OpenRouter API:", status(get("openrouter").APIKey != ""))
	fmt.Println("Anthropic API:", status(get("anthropic").APIKey != ""))
	fmt.Println("OpenAI API:", status(get("openai").APIKey != ""))
	fmt.Println("Gemini API:", status(get("gemini").APIKey != ""))
	fmt.Println("Zhipu API:", status(get("zhipu").APIKey != ""))
	fmt.Println("Qwen API:", status(get("qwen").APIKey != ""))
	fmt.Println("Groq API:", status(get("groq").APIKey != ""))
	fmt.Println("Kimi API:", status(get("kimi").APIKey != ""))
	fmt.Println("Moonshot API:", status(get("moonshot").APIKey != ""))
	fmt.Println("DeepSeek API:", status(get("deepseek").APIKey != ""))
	fmt.Println("VolcEngine API:", status(get("volcengine").APIKey != ""))
	fmt.Println("Nvidia API:", status(get("nvidia").APIKey != ""))
	if cfg := get("vllm"); cfg.APIBase != "" {
		fmt.Printf("vLLM/Local: ok %s\n", cfg.APIBase)
	} else {
		fmt.Println("vLLM/Local: not set")
	}
	if cfg := get("ollama"); cfg.APIBase != "" {
		fmt.Printf("Ollama: ok %s\n", cfg.APIBase)
	} else {
		fmt.Println("Ollama: not set")
	}

	store, _ := auth.LoadStore()
	if store == nil || len(store.Credentials) == 0 {
		return
	}

	fmt.Println("\nOAuth/Token Auth:")
	for provider, cred := range store.Credentials {
		authStatus := "authenticated"
		if cred.IsExpired() {
			authStatus = "expired"
		} else if cred.NeedsRefresh() {
			authStatus = "needs refresh"
		}
		fmt.Printf("  %s (%s): %s\n", provider, cred.AuthMethod, authStatus)
	}
}
