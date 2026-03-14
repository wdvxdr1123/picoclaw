package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// --- mock types ---

type mockRegistryTool struct {
	name   string
	desc   string
	params *Schema
	result *ToolResult
}

func (m *mockRegistryTool) Spec() *ToolSpec {
	return &ToolSpec{
		Name:        m.name,
		Description: m.desc,
		Parameters:  m.params,
	}
}
func (m *mockRegistryTool) Execute(_ context.Context, _ map[string]any) *ToolResult {
	return m.result
}

type mockContextAwareTool struct {
	mockRegistryTool
	lastCtx  context.Context
	lastArgs map[string]any
}

func (m *mockContextAwareTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	m.lastCtx = ctx
	m.lastArgs = args
	return m.result
}

type mockAsyncRegistryTool struct {
	mockRegistryTool
	lastCB AsyncCallback
}

func (m *mockAsyncRegistryTool) ExecuteAsync(_ context.Context, args map[string]any, cb AsyncCallback) *ToolResult {
	m.lastCB = cb
	return m.result
}

// --- helpers ---

func newMockTool(name, desc string) *mockRegistryTool {
	return &mockRegistryTool{
		name:   name,
		desc:   desc,
		params: schema(map[string]any{"type": "object"}),
		result: SilentResult("ok"),
	}
}

// --- tests ---

func TestNewToolRegistry(t *testing.T) {
	r := NewToolRegistry()
	if r.Count() != 0 {
		t.Errorf("expected empty registry, got count %d", r.Count())
	}
	if len(r.List()) != 0 {
		t.Errorf("expected empty list, got %v", r.List())
	}
}

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	r := NewToolRegistry()
	tool := newMockTool("echo", "echoes input")
	r.Register(tool)

	got, ok := r.Get("echo")
	if !ok {
		t.Fatal("expected to find registered tool")
	}
	if got.Spec().Name != "echo" {
		t.Errorf("expected name 'echo', got %q", got.Spec().Name)
	}
}

func TestToolRegistry_Get_NotFound(t *testing.T) {
	r := NewToolRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected ok=false for unregistered tool")
	}
}

func TestToolRegistry_RegisterOverwrite(t *testing.T) {
	r := NewToolRegistry()
	r.Register(newMockTool("dup", "first"))
	r.Register(newMockTool("dup", "second"))

	if r.Count() != 1 {
		t.Errorf("expected count 1 after overwrite, got %d", r.Count())
	}
	tool, _ := r.Get("dup")
	if tool.Spec().Description != "second" {
		t.Errorf("expected overwritten description 'second', got %q", tool.Spec().Description)
	}
}

func TestToolRegistry_Execute_Success(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&mockRegistryTool{
		name:   "greet",
		desc:   "says hello",
		params: schema(map[string]any{"type": "object"}),
		result: SilentResult("hello"),
	})

	result := r.Execute(context.Background(), "greet", nil)
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.ForLLM)
	}
	if result.ForLLM != "hello" {
		t.Errorf("expected ForLLM 'hello', got %q", result.ForLLM)
	}
}

func TestToolRegistry_Execute_NotFound(t *testing.T) {
	r := NewToolRegistry()
	result := r.Execute(context.Background(), "missing", nil)
	if !result.IsError {
		t.Error("expected error for missing tool")
	}
	if !strings.Contains(result.ForLLM, "not found") {
		t.Errorf("expected 'not found' in error, got %q", result.ForLLM)
	}
	if result.Err == nil {
		t.Error("expected Err to be set via WithError")
	}
}

func TestToolRegistry_ExecuteWithContext_InjectsToolContext(t *testing.T) {
	r := NewToolRegistry()
	ct := &mockContextAwareTool{
		mockRegistryTool: *newMockTool("ctx_tool", "needs context"),
	}
	r.Register(ct)

	r.ExecuteWithContext(context.Background(), "ctx_tool", nil, "telegram", "chat-42", nil)

	if ct.lastCtx == nil {
		t.Fatal("expected Execute to be called")
	}
	if got := ToolChannel(ct.lastCtx); got != "telegram" {
		t.Errorf("expected channel 'telegram', got %q", got)
	}
	if got := ToolChatID(ct.lastCtx); got != "chat-42" {
		t.Errorf("expected chatID 'chat-42', got %q", got)
	}
}

func TestToolRegistry_ExecuteWithContext_EmptyContext(t *testing.T) {
	r := NewToolRegistry()
	ct := &mockContextAwareTool{
		mockRegistryTool: *newMockTool("ctx_tool", "needs context"),
	}
	r.Register(ct)

	r.ExecuteWithContext(context.Background(), "ctx_tool", nil, "", "", nil)

	if ct.lastCtx == nil {
		t.Fatal("expected Execute to be called")
	}
	// Empty values are still injected; tools decide what to do with them.
	if got := ToolChannel(ct.lastCtx); got != "" {
		t.Errorf("expected empty channel, got %q", got)
	}
	if got := ToolChatID(ct.lastCtx); got != "" {
		t.Errorf("expected empty chatID, got %q", got)
	}
}

func TestToolRegistry_ExecuteWithContext_ValidatesParameters(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&mockRegistryTool{
		name: "validate_me",
		desc: "validates arguments",
		params: schema(&jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"path": {
					Type:        "string",
					Description: "Path to validate",
				},
			},
			Required: []string{"path"},
		}),
		result: SilentResult("ok"),
	})

	result := r.Execute(context.Background(), "validate_me", map[string]any{})
	if !result.IsError {
		t.Fatal("expected validation failure")
	}
	if !strings.Contains(result.ForLLM, "invalid parameters") {
		t.Fatalf("expected validation error, got %q", result.ForLLM)
	}
}

func TestToolRegistry_ExecuteWithContext_AppliesSchemaDefaults(t *testing.T) {
	r := NewToolRegistry()
	ct := &mockContextAwareTool{
		mockRegistryTool: mockRegistryTool{
			name: "defaults_tool",
			desc: "applies defaults",
			params: schema(&jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"mode": {
						Type:        "string",
						Description: "Execution mode",
						Default:     json.RawMessage(`"fast"`),
					},
				},
			}),
			result: SilentResult("ok"),
		},
	}
	r.Register(ct)

	result := r.Execute(context.Background(), "defaults_tool", map[string]any{})
	if result.IsError {
		t.Fatalf("expected success, got %q", result.ForLLM)
	}
	if ct.lastArgs["mode"] != "fast" {
		t.Fatalf("expected schema default to be applied, got %#v", ct.lastArgs)
	}
}

func TestToolRegistry_ExecuteWithContext_AsyncCallback(t *testing.T) {
	r := NewToolRegistry()
	at := &mockAsyncRegistryTool{
		mockRegistryTool: *newMockTool("async_tool", "async work"),
	}
	at.result = AsyncResult("started")
	r.Register(at)

	called := false
	cb := func(_ context.Context, _ *ToolResult) { called = true }

	result := r.ExecuteWithContext(context.Background(), "async_tool", nil, "", "", cb)
	if at.lastCB == nil {
		t.Error("expected ExecuteAsync to have received a callback")
	}
	if !result.Async {
		t.Error("expected async result")
	}

	at.lastCB(context.Background(), SilentResult("done"))
	if !called {
		t.Error("expected callback to be invoked")
	}
}

func TestToolRegistry_GetDefinitions(t *testing.T) {
	r := NewToolRegistry()
	r.Register(newMockTool("alpha", "tool A"))

	defs := r.GetDefinitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	if defs[0]["type"] != "function" {
		t.Errorf("expected type 'function', got %v", defs[0]["type"])
	}
	fn, ok := defs[0]["function"].(map[string]any)
	if !ok {
		t.Fatal("expected 'function' key to be a map")
	}
	if fn["name"] != "alpha" {
		t.Errorf("expected name 'alpha', got %v", fn["name"])
	}
	if fn["description"] != "tool A" {
		t.Errorf("expected description 'tool A', got %v", fn["description"])
	}
}

func TestToolRegistry_ToProviderDefs(t *testing.T) {
	r := NewToolRegistry()
	params := map[string]any{"type": "object", "properties": map[string]any{}}
	r.Register(&mockRegistryTool{
		name:   "beta",
		desc:   "tool B",
		params: schema(params),
		result: SilentResult("ok"),
	})

	defs := r.ToProviderDefs()
	if len(defs) != 1 {
		t.Fatalf("expected 1 provider def, got %d", len(defs))
	}

	want := providers.ToolDefinition{
		Type: "function",
		Function: providers.ToolFunctionDefinition{
			Name:        "beta",
			Description: "tool B",
			Parameters:  params,
		},
	}
	got := defs[0]
	if got.Type != want.Type {
		t.Errorf("Type: want %q, got %q", want.Type, got.Type)
	}
	if got.Function.Name != want.Function.Name {
		t.Errorf("Name: want %q, got %q", want.Function.Name, got.Function.Name)
	}
	if got.Function.Description != want.Function.Description {
		t.Errorf("Description: want %q, got %q", want.Function.Description, got.Function.Description)
	}
}

func TestToolRegistry_List(t *testing.T) {
	r := NewToolRegistry()
	r.Register(newMockTool("x", ""))
	r.Register(newMockTool("y", ""))

	names := r.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}

	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["x"] || !nameSet["y"] {
		t.Errorf("expected names {x, y}, got %v", names)
	}
}

func TestToolRegistry_Count(t *testing.T) {
	r := NewToolRegistry()
	if r.Count() != 0 {
		t.Errorf("expected 0, got %d", r.Count())
	}

	r.Register(newMockTool("a", ""))
	r.Register(newMockTool("b", ""))
	if r.Count() != 2 {
		t.Errorf("expected 2, got %d", r.Count())
	}

	r.Register(newMockTool("a", "replaced"))
	if r.Count() != 2 {
		t.Errorf("expected 2 after overwrite, got %d", r.Count())
	}
}

func TestToolRegistry_GetSummaries(t *testing.T) {
	r := NewToolRegistry()
	r.Register(newMockTool("read_file", "Reads a file"))

	summaries := r.GetSummaries()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if !strings.Contains(summaries[0], "`read_file`") {
		t.Errorf("expected backtick-quoted name in summary, got %q", summaries[0])
	}
	if !strings.Contains(summaries[0], "Reads a file") {
		t.Errorf("expected description in summary, got %q", summaries[0])
	}
}

func TestToolToSchema(t *testing.T) {
	tool := newMockTool("demo", "demo tool")
	schema := ToolToSchema(tool)

	if schema["type"] != "function" {
		t.Errorf("expected type 'function', got %v", schema["type"])
	}
	fn, ok := schema["function"].(map[string]any)
	if !ok {
		t.Fatal("expected 'function' to be a map")
	}
	if fn["name"] != "demo" {
		t.Errorf("expected name 'demo', got %v", fn["name"])
	}
	if fn["description"] != "demo tool" {
		t.Errorf("expected description 'demo tool', got %v", fn["description"])
	}
	if fn["parameters"] == nil {
		t.Error("expected parameters to be set")
	}
}

func TestToolRegistry_ConcurrentAccess(t *testing.T) {
	r := NewToolRegistry()
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := string(rune('A' + n%26))
			r.Register(newMockTool(name, "concurrent"))
			r.Get(name)
			r.Count()
			r.List()
			r.GetDefinitions()
		}(i)
	}

	wg.Wait()

	if r.Count() == 0 {
		t.Error("expected tools to be registered after concurrent access")
	}
}
