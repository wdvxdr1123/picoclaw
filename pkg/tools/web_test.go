package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
)

const testFetchLimit = int64(10 * 1024 * 1024)

func TestWebTool_WebFetch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><h1>Test Page</h1><p>Content here</p></body></html>"))
	}))
	defer server.Close()

	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		t.Fatalf("Failed to create web fetch tool: %v", err)
	}

	result := tool.Execute(context.Background(), map[string]any{"url": server.URL})
	if result.IsError {
		t.Errorf("Expected success, got IsError=true: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "Test Page") {
		t.Errorf("Expected ForLLM to contain 'Test Page', got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForUser, "bytes") && !strings.Contains(result.ForUser, "extractor") {
		t.Errorf("Expected ForUser to contain summary, got: %s", result.ForUser)
	}
}

func TestWebTool_WebFetch_JSON(t *testing.T) {
	testData := map[string]string{"key": "value", "number": "123"}
	expectedJSON, _ := json.MarshalIndent(testData, "", "  ")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(expectedJSON)
	}))
	defer server.Close()

	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
	}

	result := tool.Execute(context.Background(), map[string]any{"url": server.URL})
	if result.IsError {
		t.Errorf("Expected success, got IsError=true: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "key") && !strings.Contains(result.ForLLM, "value") {
		t.Errorf("Expected ForLLM to contain JSON data, got: %s", result.ForLLM)
	}
}

func TestWebTool_WebFetch_InvalidURL(t *testing.T) {
	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
	}

	result := tool.Execute(context.Background(), map[string]any{"url": "not-a-valid-url"})
	if !result.IsError {
		t.Errorf("Expected error for invalid URL")
	}
	if !strings.Contains(result.ForLLM, "URL") && !strings.Contains(result.ForUser, "URL") {
		t.Errorf("Expected error message for invalid URL, got ForLLM: %s", result.ForLLM)
	}
}

func TestWebTool_WebFetch_UnsupportedScheme(t *testing.T) {
	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
	}

	result := tool.Execute(context.Background(), map[string]any{"url": "ftp://example.com/file.txt"})
	if !result.IsError {
		t.Errorf("Expected error for unsupported URL scheme")
	}
	if !strings.Contains(result.ForLLM, "http/https") && !strings.Contains(result.ForUser, "http/https") {
		t.Errorf("Expected scheme error message, got ForLLM: %s", result.ForLLM)
	}
}

func TestWebTool_WebFetch_MissingURL(t *testing.T) {
	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Errorf("Expected error when URL is missing")
	}
	if !strings.Contains(result.ForLLM, "url is required") && !strings.Contains(result.ForUser, "url is required") {
		t.Errorf("Expected 'url is required' message, got ForLLM: %s", result.ForLLM)
	}
}

func TestWebTool_WebFetch_Truncation(t *testing.T) {
	longContent := strings.Repeat("x", 20000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(longContent))
	}))
	defer server.Close()

	tool, err := NewWebFetchTool(1000, testFetchLimit)
	if err != nil {
		logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
	}

	result := tool.Execute(context.Background(), map[string]any{"url": server.URL})
	if result.IsError {
		t.Errorf("Expected success, got IsError=true: %s", result.ForLLM)
	}

	resultMap := make(map[string]any)
	json.Unmarshal([]byte(result.ForLLM), &resultMap)
	if text, ok := resultMap["text"].(string); ok && len(text) > 1100 {
		t.Errorf("Expected content to be truncated to ~1000 chars, got: %d", len(text))
	}
	if truncated, ok := resultMap["truncated"].(bool); !ok || !truncated {
		t.Errorf("Expected 'truncated' to be true in result")
	}
}

func TestWebFetchTool_PayloadTooLarge(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write(bytes.Repeat([]byte("A"), int(testFetchLimit)+100))
	}))
	defer ts.Close()

	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
	}

	result := tool.Execute(context.Background(), map[string]any{"url": ts.URL})
	if result == nil {
		t.Fatal("expected a ToolResult, got nil")
	}

	expectedErrorMsg := fmt.Sprintf("size exceeded %d bytes limit", testFetchLimit)
	if !strings.Contains(result.ForLLM, expectedErrorMsg) && !strings.Contains(result.ForUser, expectedErrorMsg) {
		t.Errorf("test failed: expected error %q, but got: %+v", expectedErrorMsg, result)
	}
}

func TestWebTool_WebSearch_NoApiKey(t *testing.T) {
	tool, err := NewWebSearchTool(WebSearchToolOptions{OpenAISearchEnabled: true, OpenAISearchAPIKey: ""})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if tool != nil {
		t.Errorf("Expected nil tool when OpenAI search API key is empty")
	}

	tool, err = NewWebSearchTool(WebSearchToolOptions{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if tool != nil {
		t.Errorf("Expected nil tool when web search is disabled")
	}
}

func TestWebTool_WebSearch_MissingQuery(t *testing.T) {
	tool, err := NewWebSearchTool(WebSearchToolOptions{
		OpenAISearchEnabled: true,
		OpenAISearchAPIKey:  "test-key",
		OpenAISearchBaseURL: "https://example.com/v1/chat/completions",
		OpenAISearchModel:   "grok-4-fast",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Errorf("Expected error when query is missing")
	}
}

func TestWebTool_WebFetch_HTMLExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write(
			[]byte(`<html><body><script>alert('test');</script><style>body{color:red;}</style><h1>Title</h1><p>Content</p></body></html>`),
		)
	}))
	defer server.Close()

	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
	}

	result := tool.Execute(context.Background(), map[string]any{"url": server.URL})
	if result.IsError {
		t.Errorf("Expected success, got IsError=true: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "Title") && !strings.Contains(result.ForLLM, "Content") {
		t.Errorf("Expected ForLLM to contain extracted text, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "<script>") || strings.Contains(result.ForLLM, "<style>") {
		t.Errorf("Expected script/style tags to be removed, got: %s", result.ForLLM)
	}
}

func TestWebFetchTool_extractText(t *testing.T) {
	tool := &WebFetchTool{}
	tests := []struct {
		name     string
		input    string
		wantFunc func(t *testing.T, got string)
	}{
		{
			name:  "preserves newlines between block elements",
			input: "<html><body><h1>Title</h1>\n<p>Paragraph 1</p>\n<p>Paragraph 2</p></body></html>",
			wantFunc: func(t *testing.T, got string) {
				lines := strings.Split(got, "\n")
				if len(lines) < 2 {
					t.Errorf("Expected multiple lines, got %d: %q", len(lines), got)
				}
				if !strings.Contains(got, "Title") || !strings.Contains(got, "Paragraph 1") || !strings.Contains(got, "Paragraph 2") {
					t.Errorf("Missing expected text: %q", got)
				}
			},
		},
		{
			name:  "removes script and style tags",
			input: "<script>alert('x');</script><style>body{}</style><p>Keep this</p>",
			wantFunc: func(t *testing.T, got string) {
				if strings.Contains(got, "alert") || strings.Contains(got, "body{}") {
					t.Errorf("Expected script/style content removed, got: %q", got)
				}
				if !strings.Contains(got, "Keep this") {
					t.Errorf("Expected 'Keep this' to remain, got: %q", got)
				}
			},
		},
		{
			name:  "collapses excessive blank lines",
			input: "<p>A</p>\n\n\n\n\n<p>B</p>",
			wantFunc: func(t *testing.T, got string) {
				if strings.Contains(got, "\n\n\n") {
					t.Errorf("Expected excessive blank lines collapsed, got: %q", got)
				}
			},
		},
		{
			name:  "collapses horizontal whitespace",
			input: "<p>hello     world</p>",
			wantFunc: func(t *testing.T, got string) {
				if strings.Contains(got, "     ") {
					t.Errorf("Expected spaces collapsed, got: %q", got)
				}
				if !strings.Contains(got, "hello world") {
					t.Errorf("Expected 'hello world', got: %q", got)
				}
			},
		},
		{
			name:  "empty input",
			input: "",
			wantFunc: func(t *testing.T, got string) {
				if got != "" {
					t.Errorf("Expected empty string, got: %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tool.extractText(tt.input)
			tt.wantFunc(t, got)
		})
	}
}

func TestWebTool_WebFetch_MissingDomain(t *testing.T) {
	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
	}

	result := tool.Execute(context.Background(), map[string]any{"url": "https://"})
	if !result.IsError {
		t.Errorf("Expected error for URL without domain")
	}
	if !strings.Contains(result.ForLLM, "domain") && !strings.Contains(result.ForUser, "domain") {
		t.Errorf("Expected domain error message, got ForLLM: %s", result.ForLLM)
	}
}

func TestCreateHTTPClient_ProxyConfigured(t *testing.T) {
	client, err := createHTTPClient("http://127.0.0.1:7890", 12*time.Second)
	if err != nil {
		t.Fatalf("createHTTPClient() error: %v", err)
	}
	if client.Timeout != 12*time.Second {
		t.Fatalf("client.Timeout = %v, want %v", client.Timeout, 12*time.Second)
	}

	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport type = %T, want *http.Transport", client.Transport)
	}
	if tr.Proxy == nil {
		t.Fatal("transport.Proxy is nil, want non-nil")
	}

	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error: %v", err)
	}
	proxyURL, err := tr.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy(req) error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy URL = %v, want %q", proxyURL, "http://127.0.0.1:7890")
	}
}

func TestCreateHTTPClient_InvalidProxy(t *testing.T) {
	_, err := createHTTPClient("://bad-proxy", 10*time.Second)
	if err == nil {
		t.Fatal("createHTTPClient() expected error for invalid proxy URL, got nil")
	}
}

func TestCreateHTTPClient_Socks5ProxyConfigured(t *testing.T) {
	client, err := createHTTPClient("socks5://127.0.0.1:1080", 8*time.Second)
	if err != nil {
		t.Fatalf("createHTTPClient() error: %v", err)
	}

	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport type = %T, want *http.Transport", client.Transport)
	}
	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error: %v", err)
	}
	proxyURL, err := tr.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy(req) error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxy URL = %v, want %q", proxyURL, "socks5://127.0.0.1:1080")
	}
}

func TestCreateHTTPClient_UnsupportedProxyScheme(t *testing.T) {
	_, err := createHTTPClient("ftp://127.0.0.1:21", 10*time.Second)
	if err == nil {
		t.Fatal("createHTTPClient() expected error for unsupported scheme, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported proxy scheme") {
		t.Fatalf("error = %q, want to contain %q", err.Error(), "unsupported proxy scheme")
	}
}

func TestCreateHTTPClient_ProxyFromEnvironmentWhenConfigEmpty(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:8888")
	t.Setenv("http_proxy", "http://127.0.0.1:8888")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8888")
	t.Setenv("https_proxy", "http://127.0.0.1:8888")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("all_proxy", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	client, err := createHTTPClient("", 10*time.Second)
	if err != nil {
		t.Fatalf("createHTTPClient() error: %v", err)
	}

	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport type = %T, want *http.Transport", client.Transport)
	}
	if tr.Proxy == nil {
		t.Fatal("transport.Proxy is nil, want proxy function from environment")
	}

	req, err := http.NewRequest("GET", "https://example.com", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error: %v", err)
	}
	if _, err := tr.Proxy(req); err != nil {
		t.Fatalf("transport.Proxy(req) error: %v", err)
	}
}

func TestNewWebFetchToolWithProxy(t *testing.T) {
	tool, err := NewWebFetchToolWithProxy(1024, "http://127.0.0.1:7890", testFetchLimit)
	if err != nil {
		logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
	} else if tool.maxChars != 1024 {
		t.Fatalf("maxChars = %d, want %d", tool.maxChars, 1024)
	}
	if tool.proxy != "http://127.0.0.1:7890" {
		t.Fatalf("proxy = %q, want %q", tool.proxy, "http://127.0.0.1:7890")
	}

	tool, err = NewWebFetchToolWithProxy(0, "http://127.0.0.1:7890", testFetchLimit)
	if err != nil {
		logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
	}
	if tool.maxChars != 50000 {
		t.Fatalf("default maxChars = %d, want %d", tool.maxChars, 50000)
	}
}

func TestNewWebSearchTool_PropagatesProxy(t *testing.T) {
	tool, err := NewWebSearchTool(WebSearchToolOptions{
		SearchProvider:      "openai",
		OpenAISearchEnabled: true,
		OpenAISearchAPIKey:  "k",
		OpenAISearchBaseURL: "https://example.com/v1/chat/completions",
		OpenAISearchModel:   "grok-4-fast",
	})
	if err != nil {
		t.Fatalf("NewWebSearchTool() error: %v", err)
	}
	p, ok := tool.provider.(*OpenAISearchProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *OpenAISearchProvider", tool.provider)
	}
	if p.provider == nil {
		t.Fatal("expected OpenAI search provider to be initialized")
	}
}

func TestNewWebSearchTool_OpenAIMissingConfigWhenForced(t *testing.T) {
	tool, err := NewWebSearchTool(WebSearchToolOptions{
		SearchProvider:      "openai",
		OpenAISearchEnabled: true,
		OpenAISearchAPIKey:  "search-key",
	})
	if err == nil {
		t.Fatal("expected error when openai search is forced without full config")
	}
	if tool != nil {
		t.Fatal("expected nil tool when config is incomplete")
	}
}

func TestNewWebSearchTool_OpenAISelectedWhenConfigured(t *testing.T) {
	tool, err := NewWebSearchTool(WebSearchToolOptions{
		SearchProvider:      "openai",
		OpenAISearchEnabled: true,
		OpenAISearchAPIKey:  "search-key",
		OpenAISearchBaseURL: "https://example.com/v1",
		OpenAISearchModel:   "grok-4-fast",
	})
	if err != nil {
		t.Fatalf("NewWebSearchTool() error: %v", err)
	}
	p, ok := tool.provider.(*OpenAISearchProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *OpenAISearchProvider", tool.provider)
	}
	if p.model != "grok-4-fast" {
		t.Fatalf("provider model = %q", p.model)
	}
	if p.provider == nil {
		t.Fatal("expected OpenAI search provider to be initialized")
	}
}

func TestWebTool_WebFetch_Markdown(t *testing.T) {
	htmlContent := `<html><body><h1>Test Title</h1><p>This is a <strong>bold</strong> paragraph with <a href="https://example.com">a link</a>.</p><ul><li>Item 1</li><li>Item 2</li></ul></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(htmlContent))
	}))
	defer server.Close()

	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		t.Fatalf("Failed to create web fetch tool: %v", err)
	}

	result := tool.Execute(context.Background(), map[string]any{
		"url":    server.URL,
		"format": "markdown",
	})
	if result.IsError {
		t.Errorf("Expected success, got IsError=true: %s", result.ForLLM)
	}

	// Check that markdown content is present
	if !strings.Contains(result.ForLLM, "Test Title") {
		t.Errorf("Expected ForLLM to contain 'Test Title', got: %s", result.ForLLM)
	}
	// Check for markdown formatting
	if !strings.Contains(result.ForLLM, "**bold**") {
		t.Errorf("Expected ForLLM to contain markdown bold '**bold**', got: %s", result.ForLLM)
	}
	// Check extractor is set to markdown
	if !strings.Contains(result.ForLLM, `"extractor": "markdown"`) {
		t.Errorf("Expected extractor to be 'markdown', got: %s", result.ForLLM)
	}
}

func TestWebTool_WebFetch_MarkdownWithDomain(t *testing.T) {
	htmlContent := `<html><body><img src="/assets/image.png" /><a href="/page">Link</a></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(htmlContent))
	}))
	defer server.Close()

	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		t.Fatalf("Failed to create web fetch tool: %v", err)
	}

	result := tool.Execute(context.Background(), map[string]any{
		"url":    server.URL,
		"format": "markdown",
	})
	if result.IsError {
		t.Errorf("Expected success, got IsError=true: %s", result.ForLLM)
	}

	// Check that relative URLs are converted to absolute
	if !strings.Contains(result.ForLLM, server.URL) {
		t.Errorf("Expected ForLLM to contain absolute URLs with domain, got: %s", result.ForLLM)
	}
}

func TestWebTool_WebFetch_DefaultFormat(t *testing.T) {
	htmlContent := `<html><body><h1>Test Title</h1><p>Content here</p></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(htmlContent))
	}))
	defer server.Close()

	tool, err := NewWebFetchTool(50000, testFetchLimit)
	if err != nil {
		t.Fatalf("Failed to create web fetch tool: %v", err)
	}

	// No format specified, should default to markdown
	result := tool.Execute(context.Background(), map[string]any{
		"url": server.URL,
	})
	if result.IsError {
		t.Errorf("Expected success, got IsError=true: %s", result.ForLLM)
	}

	// Check extractor is set to markdown (default)
	if !strings.Contains(result.ForLLM, `"extractor": "markdown"`) {
		t.Errorf("Expected extractor to be 'markdown' by default, got: %s", result.ForLLM)
	}
}

func TestWebTool_OpenAISearch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected /v1/chat/completions path, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer search-key" {
			t.Errorf("Expected Bearer auth, got %q", auth)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %q", contentType)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if payload["model"] != "grok-4-fast" {
			t.Errorf("Expected model grok-4-fast, got %#v", payload["model"])
		}
		if payload["max_tokens"] != float64(8192) {
			t.Errorf("Expected max_tokens 8192, got %#v", payload["max_tokens"])
		}
		if payload["temperature"] != 1.0 {
			t.Errorf("Expected temperature 1.0, got %#v", payload["temperature"])
		}

		messages, ok := payload["messages"].([]any)
		if !ok || len(messages) != 2 {
			t.Fatalf("Expected 2 messages, got %#v", payload["messages"])
		}
		userMessage, ok := messages[1].(map[string]any)
		if !ok {
			t.Fatalf("Unexpected user message payload: %#v", messages[1])
		}
		if content := userMessage["content"]; !strings.Contains(fmt.Sprint(content), "bilibili 热门视频top10") {
			t.Errorf("Expected search query in user content, got %#v", content)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "1. foo\n链接: https://example.com/foo",
				},
			}},
		})
	}))
	defer server.Close()

	tool, err := NewWebSearchTool(WebSearchToolOptions{
		SearchProvider:      "openai",
		OpenAISearchEnabled: true,
		OpenAISearchAPIKey:  "search-key",
		OpenAISearchBaseURL: server.URL + "/v1/chat/completions",
		OpenAISearchModel:   "grok-4-fast",
	})
	if err != nil {
		t.Fatalf("NewWebSearchTool() error: %v", err)
	}

	result := tool.Execute(context.Background(), map[string]any{
		"query": "bilibili 热门视频top10",
		"count": float64(10),
	})
	if result.IsError {
		t.Fatalf("Expected success, got error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "https://example.com/foo") {
		t.Fatalf("Expected URL in result, got %s", result.ForLLM)
	}
}
