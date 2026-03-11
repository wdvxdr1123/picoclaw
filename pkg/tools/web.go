package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/sipeed/picoclaw/pkg/providers"
)

const (
	userAgent                 = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	defaultOpenAISearchPrompt = "You are a web search assistant. Answer with current information when available, prefer fresh results, and include source URLs when the provider returns them."

	searchTimeout = 30 * time.Second
	fetchTimeout  = 60 * time.Second

	defaultMaxChars = 50000
	maxRedirects    = 5
)

var (
	reScript     = regexp.MustCompile(`<script[\s\S]*?</script>`)
	reStyle      = regexp.MustCompile(`<style[\s\S]*?</style>`)
	reTags       = regexp.MustCompile(`<[^>]+>`)
	reWhitespace = regexp.MustCompile(`[^\S\n]+`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
)

func createHTTPClient(proxyURL string, timeout time.Duration) (*http.Client, error) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  false,
			TLSHandshakeTimeout: 15 * time.Second,
		},
	}

	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		switch strings.ToLower(proxy.Scheme) {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, fmt.Errorf(
				"unsupported proxy scheme %q (supported: http, https, socks5, socks5h)",
				proxy.Scheme,
			)
		}
		if proxy.Host == "" {
			return nil, fmt.Errorf("invalid proxy URL: missing host")
		}
		client.Transport.(*http.Transport).Proxy = http.ProxyURL(proxy)
	} else {
		client.Transport.(*http.Transport).Proxy = http.ProxyFromEnvironment
	}
	return client, nil
}

type SearchProvider interface {
	Search(ctx context.Context, query string, count int) (string, error)
}

type OpenAISearchProvider struct {
	provider providers.LLMProvider
	model    string
}

func normalizeOpenAISearchBase(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.TrimRight(raw, "/")
	raw = strings.TrimSuffix(raw, "/chat/completions")
	return strings.TrimRight(raw, "/")
}

func formatOpenAISearchQuery(query string, count int) string {
	query = strings.TrimSpace(query)
	if count <= 0 {
		return query
	}
	return fmt.Sprintf("User query: %s\nPreferred result count: %d", query, count)
}

func stringifyOpenAIMessageContent(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			switch part := item.(type) {
			case string:
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					parts = append(parts, trimmed)
				}
			case map[string]any:
				if text, ok := part["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, strings.TrimSpace(text))
					continue
				}
				if textObj, ok := part["text"].(map[string]any); ok {
					if value, ok := textObj["value"].(string); ok && strings.TrimSpace(value) != "" {
						parts = append(parts, strings.TrimSpace(value))
						continue
					}
				}
				if content, ok := part["content"].(string); ok && strings.TrimSpace(content) != "" {
					parts = append(parts, strings.TrimSpace(content))
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n\n"))
	default:
		return ""
	}
}

func (p *OpenAISearchProvider) Search(ctx context.Context, query string, count int) (string, error) {
	if strings.TrimSpace(p.model) == "" {
		return "", fmt.Errorf("openai search model is required")
	}
	if p.provider == nil {
		return "", fmt.Errorf("openai search provider is not configured")
	}
	searchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := p.provider.Chat(
		searchCtx,
		[]providers.Message{
			{Role: "system", Content: defaultOpenAISearchPrompt},
			{Role: "user", Content: formatOpenAISearchQuery(query, count)},
		},
		nil,
		strings.TrimSpace(p.model),
		map[string]any{
			"max_tokens":  8192,
			"temperature": 1.0,
		},
	)
	if err != nil {
		return "", fmt.Errorf("provider chat failed: %w", err)
	}
	content := stringifyOpenAIMessageContent(resp.Content)
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("openai search returned empty content")
	}

	return content, nil
}

type WebSearchTool struct {
	provider   SearchProvider
	maxResults int
}

type WebSearchToolOptions struct {
	SearchProvider      string
	OpenAISearchEnabled bool
	OpenAISearchAPIKey  string
	OpenAISearchBaseURL string
	OpenAISearchModel   string
}

func NewWebSearchTool(opts WebSearchToolOptions) (*WebSearchTool, error) {
	searchProvider := strings.ToLower(strings.TrimSpace(opts.SearchProvider))
	if searchProvider == "" {
		searchProvider = "auto"
	}

	maxResults := 5
	isOpenAIReady := opts.OpenAISearchEnabled &&
		strings.TrimSpace(opts.OpenAISearchAPIKey) != "" &&
		strings.TrimSpace(opts.OpenAISearchModel) != "" &&
		strings.TrimSpace(normalizeOpenAISearchBase(opts.OpenAISearchBaseURL)) != ""

	newOpenAITool := func() (*WebSearchTool, error) {
		apiBase := normalizeOpenAISearchBase(opts.OpenAISearchBaseURL)
		return &WebSearchTool{
			provider: &OpenAISearchProvider{
				provider: providers.NewHTTPProviderWithOptions(
					strings.TrimSpace(opts.OpenAISearchAPIKey),
					apiBase,
					"",
					"",
					int(searchTimeout/time.Second),
					"",
				),
				model: strings.TrimSpace(opts.OpenAISearchModel),
			},
			maxResults: maxResults,
		}, nil
	}

	switch searchProvider {
	case "auto":
		if isOpenAIReady {
			return newOpenAITool()
		}
		return nil, nil
	case "openai":
		if !isOpenAIReady {
			return nil, fmt.Errorf("tools.web.search_provider is openai but openai_search is not fully configured")
		}
		return newOpenAITool()
	default:
		return nil, fmt.Errorf("unsupported web search provider %q", opts.SearchProvider)
	}
}

func (t *WebSearchTool) Name() string {
	return "web_search"
}

func (t *WebSearchTool) Description() string {
	return "Search the web for current information using the configured search backend."
}

func (t *WebSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
			},
			"count": map[string]any{
				"type":        "integer",
				"description": "Number of results (1-30)",
				"minimum":     1.0,
				"maximum":     30.0,
			},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	query, ok := args["query"].(string)
	if !ok {
		return ErrorResult("query is required")
	}

	count := t.maxResults
	if c, ok := args["count"].(float64); ok && int(c) > 0 && int(c) <= 30 {
		count = int(c)
	}

	result, err := t.provider.Search(ctx, query, count)
	if err != nil {
		return ErrorResult(fmt.Sprintf("search failed: %v", err))
	}

	return &ToolResult{ForLLM: result, ForUser: result}
}

type WebFetchTool struct {
	maxChars        int
	proxy           string
	client          *http.Client
	fetchLimitBytes int64
}

func NewWebFetchTool(maxChars int, fetchLimitBytes int64) (*WebFetchTool, error) {
	return NewWebFetchToolWithProxy(maxChars, "", fetchLimitBytes)
}

func NewWebFetchToolWithProxy(
	maxChars int,
	proxy string,
	fetchLimitBytes int64,
) (*WebFetchTool, error) {
	if maxChars <= 0 {
		maxChars = defaultMaxChars
	}
	client, err := createHTTPClient(proxy, fetchTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client for web fetch: %w", err)
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	}
	if fetchLimitBytes <= 0 {
		fetchLimitBytes = 10 * 1024 * 1024
	}
	return &WebFetchTool{
		maxChars:        maxChars,
		proxy:           proxy,
		client:          client,
		fetchLimitBytes: fetchLimitBytes,
	}, nil
}

func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

func (t *WebFetchTool) Description() string {
	return "Fetch a URL and extract readable content. Markdown format is used by default and recommended for HTML pages as it better preserves document structure, links, and formatting."
}

func (t *WebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL to fetch",
			},
			"maxChars": map[string]any{
				"type":        "integer",
				"description": "Maximum characters to extract",
				"minimum":     100.0,
			},
			"format": map[string]any{
				"type":        "string",
				"description": "Output format. Defaults to 'markdown' (recommended) which preserves document structure, links, and formatting. Use 'text' for plain text extraction only.",
				"enum":        []string{"markdown", "text"},
			},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	urlStr, ok := args["url"].(string)
	if !ok {
		return ErrorResult("url is required")
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid URL: %v", err))
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return ErrorResult("only http/https URLs are allowed")
	}
	if parsedURL.Host == "" {
		return ErrorResult("missing domain in URL")
	}

	maxChars := t.maxChars
	if mc, ok := args["maxChars"].(float64); ok && int(mc) > 100 {
		maxChars = int(mc)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to create request: %v", err))
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := t.client.Do(req)
	if err != nil {
		return ErrorResult(fmt.Sprintf("request failed: %v", err))
	}
	resp.Body = http.MaxBytesReader(nil, resp.Body, t.fetchLimitBytes)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return ErrorResult(fmt.Sprintf("failed to read response: size exceeded %d bytes limit", t.fetchLimitBytes))
		}
		return ErrorResult(fmt.Sprintf("failed to read response: %v", err))
	}

	// Determine output format
	outputFormat := "markdown"
	if f, ok := args["format"].(string); ok && f != "" {
		outputFormat = strings.ToLower(f)
	}

	contentType := resp.Header.Get("Content-Type")
	var text, extractor string
	if strings.Contains(contentType, "application/json") {
		var jsonData any
		if err := json.Unmarshal(body, &jsonData); err == nil {
			formatted, _ := json.MarshalIndent(jsonData, "", "  ")
			text = string(formatted)
			extractor = "json"
		} else {
			text = string(body)
			extractor = "raw"
		}
	} else if strings.Contains(contentType, "text/html") || len(body) > 0 &&
		(strings.HasPrefix(string(body), "<!DOCTYPE") || strings.HasPrefix(strings.ToLower(string(body)), "<html")) {
		if outputFormat == "markdown" {
			text = t.extractMarkdown(string(body), urlStr)
			extractor = "markdown"
		} else {
			text = t.extractText(string(body))
			extractor = "text"
		}
	} else {
		text = string(body)
		extractor = "raw"
	}

	truncated := len(text) > maxChars
	if truncated {
		text = text[:maxChars]
	}

	result := map[string]any{
		"url":       urlStr,
		"status":    resp.StatusCode,
		"extractor": extractor,
		"truncated": truncated,
		"length":    len(text),
		"text":      text,
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")

	return &ToolResult{
		ForLLM: string(resultJSON),
		ForUser: fmt.Sprintf(
			"Fetched %d bytes from %s (extractor: %s, truncated: %v)",
			len(text), urlStr, extractor, truncated,
		),
	}
}

func (t *WebFetchTool) extractText(htmlContent string) string {
	result := reScript.ReplaceAllLiteralString(htmlContent, "")
	result = reStyle.ReplaceAllLiteralString(result, "")
	result = reTags.ReplaceAllLiteralString(result, "")
	result = strings.TrimSpace(result)
	result = reWhitespace.ReplaceAllString(result, " ")
	result = reBlankLines.ReplaceAllString(result, "\n\n")

	lines := strings.Split(result, "\n")
	cleanLines := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}
	return strings.Join(cleanLines, "\n")
}

func (t *WebFetchTool) extractMarkdown(htmlContent string, domain string) string {
	markdown, err := htmltomarkdown.ConvertString(
		htmlContent,
		converter.WithDomain(domain),
	)
	if err != nil {
		// Fallback to plain text extraction if conversion fails
		return t.extractText(htmlContent)
	}
	return markdown
}
