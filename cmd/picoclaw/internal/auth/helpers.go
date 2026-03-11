package auth

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/config"
)

const (
	supportedProvidersMsg = "supported providers: openai"
	defaultOpenAIModel    = "gpt-5.2"
)

// OpenAIModels contains the list of supported OpenAI models via subscription
var OpenAIModels = []string{
	"gpt-5.2",
	"gpt-5.4",
	"gpt-5.1-codex",
	"gpt-5.2-codex",
	"gpt-5.3-codex",
	"gpt-5.1-codex-max",
	"gpt-5.1-codex-mini",
}

func authLoginCmd(provider string, useDeviceCode bool) error {
	switch provider {
	case "openai":
		return authLoginOpenAIInteractive(useDeviceCode)
	default:
		return fmt.Errorf("unsupported provider: %s (%s)", provider, supportedProvidersMsg)
	}
}

func authLoginOpenAIInteractive(useDeviceCode bool) error {
	// If --device-code flag is explicitly set, use device code flow directly
	if useDeviceCode {
		return authLoginOpenAIDeviceCode()
	}

	// Interactive login method selection
	fmt.Println("OpenAI login method:")
	fmt.Println("  1) ChatGPT Pro/Plus (browser) - Recommended")
	fmt.Println("  2) ChatGPT Pro/Plus (device code) - For headless environments")
	fmt.Println("  3) API Key (from platform.openai.com)")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("Choose [1]: ")
		choice := "1"
		if scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text != "" {
				choice = text
			}
		}

		switch choice {
		case "1":
			return authLoginOpenAIBrowser()
		case "2":
			return authLoginOpenAIDeviceCode()
		case "3":
			return authLoginOpenAIAPIKey()
		default:
			fmt.Printf("Invalid choice: %s. Please enter 1, 2, or 3.\n", choice)
		}
	}
}

func authLoginOpenAIBrowser() error {
	cfg := auth.OpenAIOAuthConfig()

	cred, err := auth.LoginBrowser(cfg)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	if err = auth.SetCredential("openai", cred); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	appCfg, err := internal.LoadConfig()
	if err == nil {
		ensureProviderConfig(appCfg, "openai", func(cfg *config.ProviderConfig) {
			cfg.AuthMethod = "oauth"
		})
		ensureOpenAIModels(appCfg, "openai")
		setDefaultModel(appCfg, defaultOpenAIModel)

		if err = config.SaveConfig(internal.GetConfigPath(), appCfg); err != nil {
			return fmt.Errorf("could not update config: %w", err)
		}
	}

	fmt.Println("Login successful!")
	if cred.AccountID != "" {
		fmt.Printf("Account: %s\n", cred.AccountID)
	}
	fmt.Printf("Default model set to: %s\n", defaultOpenAIModel)
	fmt.Println("\nAvailable models:")
	for _, model := range OpenAIModels {
		fmt.Printf("  - %s\n", model)
	}

	return nil
}

func authLoginOpenAIDeviceCode() error {
	cfg := auth.OpenAIOAuthConfig()

	cred, err := auth.LoginDeviceCode(cfg)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	if err = auth.SetCredential("openai", cred); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	appCfg, err := internal.LoadConfig()
	if err == nil {
		ensureProviderConfig(appCfg, "openai", func(cfg *config.ProviderConfig) {
			cfg.AuthMethod = "oauth"
		})
		ensureOpenAIModels(appCfg, "openai")
		setDefaultModel(appCfg, defaultOpenAIModel)

		if err = config.SaveConfig(internal.GetConfigPath(), appCfg); err != nil {
			return fmt.Errorf("could not update config: %w", err)
		}
	}

	fmt.Println("Login successful!")
	if cred.AccountID != "" {
		fmt.Printf("Account: %s\n", cred.AccountID)
	}
	fmt.Printf("Default model set to: %s\n", defaultOpenAIModel)
	fmt.Println("\nAvailable models:")
	for _, model := range OpenAIModels {
		fmt.Printf("  - %s\n", model)
	}

	return nil
}

func authLoginOpenAIAPIKey() error {
	fmt.Println("\nTo use your OpenAI API key:")
	fmt.Println("1. Go to https://platform.openai.com/api-keys")
	fmt.Println("2. Create a new API key")
	fmt.Println("3. Paste it below")
	fmt.Println()

	cred, err := auth.LoginPasteToken("openai", os.Stdin)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	if err = auth.SetCredential("openai", cred); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	appCfg, err := internal.LoadConfig()
	if err == nil {
		ensureProviderConfig(appCfg, "openai", func(cfg *config.ProviderConfig) {
			cfg.AuthMethod = "token"
		})
		// For API key, we use standard OpenAI API models
		ensureNamedModel(appCfg, "gpt-4o", "openai", "gpt-4o")
		ensureNamedModel(appCfg, "gpt-4o-mini", "openai", "gpt-4o-mini")
		ensureNamedModel(appCfg, "o3-mini", "openai", "o3-mini")
		ensureNamedModel(appCfg, "o1", "openai", "o1")
		setDefaultModel(appCfg, "gpt-4o")

		if err = config.SaveConfig(internal.GetConfigPath(), appCfg); err != nil {
			return fmt.Errorf("could not update config: %w", err)
		}
	}

	fmt.Println("API Key saved for OpenAI!")
	fmt.Println("Default model set to: gpt-4o")
	fmt.Println("\nAvailable models via API:")
	fmt.Println("  - gpt-4o")
	fmt.Println("  - gpt-4o-mini")
	fmt.Println("  - o3-mini")
	fmt.Println("  - o1")

	return nil
}

func authLoginPasteToken(provider string) error {
	cred, err := auth.LoginPasteToken(provider, os.Stdin)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	if err = auth.SetCredential(provider, cred); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	appCfg, err := internal.LoadConfig()
	if err == nil {
		switch provider {
		case "openai":
			ensureProviderConfig(appCfg, "openai", func(cfg *config.ProviderConfig) {
				cfg.AuthMethod = "token"
			})
			ensureNamedModel(appCfg, "gpt-5.2", "openai", "gpt-5.2")
			setDefaultModel(appCfg, "gpt-5.2")
		}
		if err := config.SaveConfig(internal.GetConfigPath(), appCfg); err != nil {
			return fmt.Errorf("could not update config: %w", err)
		}
	}

	fmt.Printf("Token saved for %s!\n", provider)

	if appCfg != nil {
		fmt.Printf("Default model set to: %s\n", appCfg.GetDefaultModelName())
	}

	return nil
}

func authLogoutCmd(provider string) error {
	if provider != "" {
		if err := auth.DeleteCredential(provider); err != nil {
			return fmt.Errorf("failed to remove credentials: %w", err)
		}

		appCfg, err := internal.LoadConfig()
		if err == nil {
			switch provider {
			case "openai":
				ensureProviderConfig(appCfg, "openai", func(cfg *config.ProviderConfig) {
					cfg.AuthMethod = ""
				})
			}
			config.SaveConfig(internal.GetConfigPath(), appCfg)
		}

		fmt.Printf("Logged out from %s\n", provider)

		return nil
	}

	if err := auth.DeleteAllCredentials(); err != nil {
		return fmt.Errorf("failed to remove credentials: %w", err)
	}

	appCfg, err := internal.LoadConfig()
	if err == nil {
		ensureProviderConfig(appCfg, "openai", func(cfg *config.ProviderConfig) {
			cfg.AuthMethod = ""
		})
		config.SaveConfig(internal.GetConfigPath(), appCfg)
	}

	fmt.Println("Logged out from all providers")

	return nil
}

func authStatusCmd() error {
	store, err := auth.LoadStore()
	if err != nil {
		return fmt.Errorf("failed to load auth store: %w", err)
	}

	if len(store.Credentials) == 0 {
		fmt.Println("No authenticated providers.")
		fmt.Println("Run: picoclaw auth login --provider <name>")
		return nil
	}

	fmt.Println("\nAuthenticated Providers:")
	fmt.Println("------------------------")
	for provider, cred := range store.Credentials {
		status := "active"
		if cred.IsExpired() {
			status = "expired"
		} else if cred.NeedsRefresh() {
			status = "needs refresh"
		}

		fmt.Printf("  %s:\n", provider)
		fmt.Printf("    Method: %s\n", cred.AuthMethod)
		fmt.Printf("    Status: %s\n", status)
		if cred.AccountID != "" {
			fmt.Printf("    Account: %s\n", cred.AccountID)
		}
		if cred.Email != "" {
			fmt.Printf("    Email: %s\n", cred.Email)
		}
		if cred.ProjectID != "" {
			fmt.Printf("    Project: %s\n", cred.ProjectID)
		}
		if !cred.ExpiresAt.IsZero() {
			fmt.Printf("    Expires: %s\n", cred.ExpiresAt.Format("2006-01-02 15:04"))
		}
	}

	return nil
}

func ensureProviderConfig(appCfg *config.Config, name string, update func(*config.ProviderConfig)) {
	cfg := appCfg.Providers.Get(name)
	if cfg.Type == "" {
		cfg.Type = config.NormalizeProviderType(name)
	}
	update(&cfg)
	appCfg.Providers.Set(name, cfg)
}

func ensureNamedModel(appCfg *config.Config, alias, provider, model string) {
	if appCfg.Models == nil {
		appCfg.Models = config.ModelsConfig{}
	}
	if variants, ok := appCfg.Models[alias]; ok && len(variants) > 0 {
		variants[0].Provider = provider
		variants[0].Model = model
		appCfg.Models[alias] = variants
		return
	}
	appCfg.Models[alias] = config.ModelVariants{{
		Provider: provider,
		Model:    model,
	}}
}

func setDefaultModel(appCfg *config.Config, alias string) {
	appCfg.Agents.Defaults.Model = alias
	appCfg.Agents.Defaults.ModelName = ""
}

// ensureOpenAIModels sets up all supported OpenAI models in the config
func ensureOpenAIModels(appCfg *config.Config, provider string) {
	for _, model := range OpenAIModels {
		ensureNamedModel(appCfg, model, provider, model)
	}
}
