package tools

import (
	"context"
	"fmt"
	"strings"
)

type SpawnTool struct {
	manager        *SubagentManager
	allowlistCheck func(targetAgentID string) bool
}

type spawnParams struct {
	Task    string `json:"task" jsonschema:"The task for subagent to complete"`
	Label   string `json:"label,omitempty" jsonschema:"Optional short label for the task (for display)"`
	AgentID string `json:"agent_id,omitempty" jsonschema:"Optional target agent ID to delegate the task to"`
}

var spawnToolSpec = &ToolSpec{
	Name:        "spawn",
	Description: "Spawn a subagent to handle a task in the background. Use this for complex or time-consuming tasks that can run independently. The subagent will complete the task and report back when done.",
	Parameters:  schemaForParams[spawnParams](),
}

// Compile-time check: SpawnTool implements AsyncExecutor.
var _ AsyncExecutor = (*SpawnTool)(nil)

func NewSpawnTool(manager *SubagentManager) *SpawnTool {
	return &SpawnTool{
		manager: manager,
	}
}

func (t *SpawnTool) Spec() *ToolSpec {
	return spawnToolSpec
}

func (t *SpawnTool) SetAllowlistChecker(check func(targetAgentID string) bool) {
	t.allowlistCheck = check
}

func (t *SpawnTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	return t.execute(ctx, args, nil)
}

// ExecuteAsync implements AsyncExecutor. The callback is passed through to the
// subagent manager as a call parameter — never stored on the SpawnTool instance.
func (t *SpawnTool) ExecuteAsync(ctx context.Context, args map[string]any, cb AsyncCallback) *ToolResult {
	return t.execute(ctx, args, cb)
}

func (t *SpawnTool) execute(ctx context.Context, args map[string]any, cb AsyncCallback) *ToolResult {
	task, ok := args["task"].(string)
	if !ok || strings.TrimSpace(task) == "" {
		return ErrorResult("task is required and must be a non-empty string")
	}

	label, _ := args["label"].(string)
	agentID, _ := args["agent_id"].(string)

	// Check allowlist if targeting a specific agent
	if agentID != "" && t.allowlistCheck != nil {
		if !t.allowlistCheck(agentID) {
			return ErrorResult(fmt.Sprintf("not allowed to spawn agent '%s'", agentID))
		}
	}

	if t.manager == nil {
		return ErrorResult("Subagent manager not configured")
	}

	// Read channel/chatID from context (injected by registry).
	// Fall back to "cli"/"direct" for non-conversation callers (e.g., CLI, tests)
	// to preserve the same defaults as the original NewSpawnTool constructor.
	channel := ToolChannel(ctx)
	if channel == "" {
		channel = "cli"
	}
	chatID := ToolChatID(ctx)
	if chatID == "" {
		chatID = "direct"
	}

	// Pass callback to manager for async completion notification
	result, err := t.manager.Spawn(ctx, task, label, agentID, channel, chatID, cb)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to spawn subagent: %v", err))
	}

	// Return AsyncResult since the task runs in background
	return AsyncResult(result)
}
