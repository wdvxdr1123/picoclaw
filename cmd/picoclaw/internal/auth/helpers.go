package auth

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/config"
)

const (
	supportedProvidersMsg = "supported providers: openai, anthropic, google-antigravity"
	defaultAnthropicModel = "claude-sonnet-4.6"
)

func authLoginCmd(provider string, useDeviceCode bool, useOauth bool) error {
	switch provider {
	case "openai":
		return authLoginOpenAI(useDeviceCode)
	case "anthropic":
		return authLoginAnthropic(useOauth)
	default:
		return fmt.Errorf("unsupported provider: %s (%s)", provider, supportedProvidersMsg)
	}
}

func authLoginOpenAI(useDeviceCode bool) error {
	cfg := auth.OpenAIOAuthConfig()

	var cred *auth.AuthCredential
	var err error

	if useDeviceCode {
		cred, err = auth.LoginDeviceCode(cfg)
	} else {
		cred, err = auth.LoginBrowser(cfg)
	}

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
		ensureNamedModel(appCfg, "gpt-5.2", "openai", "gpt-5.2")
		setDefaultModel(appCfg, "gpt-5.2")

		if err = config.SaveConfig(internal.GetConfigPath(), appCfg); err != nil {
			return fmt.Errorf("could not update config: %w", err)
		}
	}

	fmt.Println("Login successful!")
	if cred.AccountID != "" {
		fmt.Printf("Account: %s\n", cred.AccountID)
	}
	fmt.Println("Default model set to: gpt-5.2")

	return nil
}

func authLoginAnthropic(useOauth bool) error {
	if useOauth {
		return authLoginAnthropicSetupToken()
	}

	fmt.Println("Anthropic login method:")
	fmt.Println("  1) Setup token (from `claude setup-token`) (Recommended)")
	fmt.Println("  2) API key (from console.anthropic.com)")

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
			return authLoginAnthropicSetupToken()
		case "2":
			return authLoginPasteToken("anthropic")
		default:
			fmt.Printf("Invalid choice: %s. Please enter 1 or 2.\n", choice)
		}
	}
}

func authLoginAnthropicSetupToken() error {
	cred, err := auth.LoginSetupToken(os.Stdin)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	if err = auth.SetCredential("anthropic", cred); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	appCfg, err := internal.LoadConfig()
	if err == nil {
		ensureProviderConfig(appCfg, "anthropic", func(cfg *config.ProviderConfig) {
			cfg.AuthMethod = "oauth"
		})
		ensureNamedModel(appCfg, defaultAnthropicModel, "anthropic", defaultAnthropicModel)
		if appCfg.GetDefaultModelName() == "" {
			setDefaultModel(appCfg, defaultAnthropicModel)
		}

		if err := config.SaveConfig(internal.GetConfigPath(), appCfg); err != nil {
			return fmt.Errorf("could not update config: %w", err)
		}
	}

	fmt.Println("Setup token saved for Anthropic!")

	return nil
}

func fetchGoogleUserEmail(accessToken string) (string, error) {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading userinfo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo request failed: %s", string(body))
	}

	var userInfo struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return "", err
	}
	return userInfo.Email, nil
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
		case "anthropic":
			ensureProviderConfig(appCfg, "anthropic", func(cfg *config.ProviderConfig) {
				cfg.AuthMethod = "token"
			})
			ensureNamedModel(appCfg, defaultAnthropicModel, "anthropic", defaultAnthropicModel)
			setDefaultModel(appCfg, defaultAnthropicModel)
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
			case "anthropic":
				ensureProviderConfig(appCfg, "anthropic", func(cfg *config.ProviderConfig) {
					cfg.AuthMethod = ""
				})
			case "google-antigravity", "antigravity":
				ensureProviderConfig(appCfg, "antigravity", func(cfg *config.ProviderConfig) {
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
		ensureProviderConfig(appCfg, "anthropic", func(cfg *config.ProviderConfig) {
			cfg.AuthMethod = ""
		})
		ensureProviderConfig(appCfg, "antigravity", func(cfg *config.ProviderConfig) {
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

		if provider == "anthropic" && cred.AuthMethod == "oauth" {
			usage, err := auth.FetchAnthropicUsage(cred.AccessToken)
			if err != nil {
				fmt.Printf("    Usage: unavailable (%v)\n", err)
			} else {
				fmt.Printf("    Usage (5h):  %.1f%%\n", usage.FiveHourUtilization*100)
				fmt.Printf("    Usage (7d):  %.1f%%\n", usage.SevenDayUtilization*100)
			}
		}
	}

	return nil
}

// isAntigravityModel checks if a model string belongs to antigravity provider
func isAntigravityModel(model string) bool {
	return model == "antigravity" ||
		model == "google-antigravity" ||
		strings.HasPrefix(model, "antigravity/") ||
		strings.HasPrefix(model, "google-antigravity/")
}

// isOpenAIModel checks if a model string belongs to openai provider
func isOpenAIModel(model string) bool {
	return model == "openai" ||
		strings.HasPrefix(model, "openai/")
}

// isAnthropicModel checks if a model string belongs to anthropic provider
func isAnthropicModel(model string) bool {
	return model == "anthropic" ||
		strings.HasPrefix(model, "anthropic/")
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
	appCfg.DefaultModel = alias
	appCfg.Agents.Defaults.ModelName = alias
}
