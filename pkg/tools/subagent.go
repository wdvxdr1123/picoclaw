package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/utils"
)

type SubagentTask struct {
	ID            string
	Task          string
	Label         string
	AgentID       string
	OriginChannel string
	OriginChatID  string
	Status        string
	Result        string
	Created       int64
}

type SubagentManager struct {
	tasks         map[string]*SubagentTask
	mu            sync.RWMutex
	provider      providers.LLMProvider
	defaultModel  string
	workspace     string
	tools         *ToolRegistry
	maxIterations int
	llmOptions    map[string]any
	nextID        int
}

func NewSubagentManager(
	provider providers.LLMProvider,
	defaultModel, workspace string,
) *SubagentManager {
	return &SubagentManager{
		tasks:         make(map[string]*SubagentTask),
		provider:      provider,
		defaultModel:  defaultModel,
		workspace:     workspace,
		tools:         NewToolRegistry(),
		maxIterations: 10,
		nextID:        1,
	}
}

// SetLLMOptions sets LLM generation options for subagent calls.
func (sm *SubagentManager) SetLLMOptions(options map[string]any) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if len(options) == 0 {
		sm.llmOptions = nil
		return
	}
	sm.llmOptions = make(map[string]any, len(options))
	for k, v := range options {
		sm.llmOptions[k] = v
	}
}

// SetTools sets the tool registry for subagent execution.
// If not set, subagent will have access to the provided tools.
func (sm *SubagentManager) SetTools(tools *ToolRegistry) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.tools = tools
}

// RegisterTool registers a tool for subagent execution.
func (sm *SubagentManager) RegisterTool(tool Tool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.tools.Register(tool)
}

func (sm *SubagentManager) Spawn(
	ctx context.Context,
	task, label, agentID, originChannel, originChatID string,
	callback AsyncCallback,
) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	taskID := fmt.Sprintf("subagent-%d", sm.nextID)
	sm.nextID++

	subagentTask := &SubagentTask{
		ID:            taskID,
		Task:          task,
		Label:         label,
		AgentID:       agentID,
		OriginChannel: originChannel,
		OriginChatID:  originChatID,
		Status:        "running",
		Created:       time.Now().UnixMilli(),
	}
	sm.tasks[taskID] = subagentTask

	// Start task in background with context cancellation support
	go sm.runTask(ctx, subagentTask, callback)

	if label != "" {
		return fmt.Sprintf("Spawned subagent '%s' for task: %s", label, task), nil
	}
	return fmt.Sprintf("Spawned subagent for task: %s", task), nil
}

func (sm *SubagentManager) runTask(ctx context.Context, task *SubagentTask, callback AsyncCallback) {
	task.Status = "running"
	task.Created = time.Now().UnixMilli()

	// Build system prompt for subagent
	systemPrompt := `You are a subagent. Complete the given task independently and report the result.
You have access to tools - use them as needed to complete your task.
After completing the task, provide a clear summary of what was done.`

	messages := []providers.Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: task.Task,
		},
	}

	// Check if context is already canceled before starting
	select {
	case <-ctx.Done():
		sm.mu.Lock()
		task.Status = "canceled"
		task.Result = "Task canceled before execution"
		sm.mu.Unlock()
		return
	default:
	}

	// Run tool loop with access to tools
	loopResult, err := sm.runToolLoop(ctx, messages, task.OriginChannel, task.OriginChatID)

	sm.mu.Lock()
	var result *ToolResult
	defer func() {
		sm.mu.Unlock()
		// Call callback if provided and result is set
		if callback != nil && result != nil {
			callback(ctx, result)
		}
	}()

	if err != nil {
		task.Status = "failed"
		task.Result = fmt.Sprintf("Error: %v", err)
		// Check if it was canceled
		if ctx.Err() != nil {
			task.Status = "canceled"
			task.Result = "Task canceled during execution"
		}
		result = &ToolResult{
			ForLLM:  task.Result,
			ForUser: "",
			Silent:  false,
			IsError: true,
			Async:   false,
			Err:     err,
		}
	} else {
		task.Status = "completed"
		task.Result = loopResult.Content
		result = &ToolResult{
			ForLLM: fmt.Sprintf(
				"Subagent '%s' completed (iterations: %d): %s",
				task.Label,
				loopResult.Iterations,
				loopResult.Content,
			),
			ForUser: loopResult.Content,
			Silent:  false,
			IsError: false,
			Async:   false,
		}
	}
}

func (sm *SubagentManager) GetTask(taskID string) (*SubagentTask, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	task, ok := sm.tasks[taskID]
	return task, ok
}

func (sm *SubagentManager) ListTasks() []*SubagentTask {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	tasks := make([]*SubagentTask, 0, len(sm.tasks))
	for _, task := range sm.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// SubagentTool executes a subagent task synchronously and returns the result.
// Unlike SpawnTool which runs tasks asynchronously, SubagentTool waits for completion
// and returns the result directly in the ToolResult.
type SubagentTool struct {
	manager *SubagentManager
}

type subagentParams struct {
	Task  string `json:"task" jsonschema:"The task for subagent to complete"`
	Label string `json:"label,omitempty" jsonschema:"Optional short label for the task (for display)"`
}

var subagentToolSpec = &ToolSpec{
	Name:        "subagent",
	Description: "Execute a subagent task synchronously and return the result. Use this for delegating specific tasks to an independent agent instance. Returns execution summary to user and full details to LLM.",
	Parameters:  schemaForParams[subagentParams](),
}

func NewSubagentTool(manager *SubagentManager) *SubagentTool {
	return &SubagentTool{
		manager: manager,
	}
}

func (t *SubagentTool) Spec() *ToolSpec {
	return subagentToolSpec
}

func (t *SubagentTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	task, ok := args["task"].(string)
	if !ok {
		return ErrorResult("task is required").WithError(fmt.Errorf("task parameter is required"))
	}

	label, _ := args["label"].(string)

	if t.manager == nil {
		return ErrorResult("Subagent manager not configured").WithError(fmt.Errorf("manager is nil"))
	}

	// Build messages for subagent
	messages := []providers.Message{
		{
			Role:    "system",
			Content: "You are a subagent. Complete the given task independently and provide a clear, concise result.",
		},
		{
			Role:    "user",
			Content: task,
		},
	}

	// Use runToolLoop to execute with tools (same as async SpawnTool)
	sm := t.manager

	// Fall back to "cli"/"direct" for non-conversation callers (e.g., CLI, tests)
	channel := ToolChannel(ctx)
	if channel == "" {
		channel = "cli"
	}
	chatID := ToolChatID(ctx)
	if chatID == "" {
		chatID = "direct"
	}

	loopResult, err := sm.runToolLoop(ctx, messages, channel, chatID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Subagent execution failed: %v", err)).WithError(err)
	}

	// ForUser: Brief summary for user (truncated if too long)
	userContent := loopResult.Content
	maxUserLen := 500
	if len(userContent) > maxUserLen {
		userContent = userContent[:maxUserLen] + "..."
	}

	// ForLLM: Full execution details
	labelStr := label
	if labelStr == "" {
		labelStr = "(unnamed)"
	}
	llmContent := fmt.Sprintf("Subagent task completed:\nLabel: %s\nIterations: %d\nResult: %s",
		labelStr, loopResult.Iterations, loopResult.Content)

	return &ToolResult{
		ForLLM:  llmContent,
		ForUser: userContent,
	}
}

// toolLoopResult contains the result of running the tool loop.
type toolLoopResult struct {
	Content    string
	Iterations int
}

// runToolLoop executes the LLM + tool call iteration loop for subagent execution.
func (sm *SubagentManager) runToolLoop(
	ctx context.Context,
	messages []providers.Message,
	channel, chatID string,
) (*toolLoopResult, error) {
	sm.mu.RLock()
	tools := sm.tools
	maxIter := sm.maxIterations
	var llmOptions map[string]any
	if len(sm.llmOptions) > 0 {
		llmOptions = make(map[string]any, len(sm.llmOptions))
		for k, v := range sm.llmOptions {
			llmOptions[k] = v
		}
	}
	sm.mu.RUnlock()

	var finalContent string
	for iteration := 1; iteration <= maxIter; iteration++ {
		var toolDefs []providers.ToolDefinition
		if tools != nil {
			toolDefs = tools.ToProviderDefs()
		}

		opts := llmOptions
		if opts == nil {
			opts = map[string]any{}
		}

		response, err := sm.provider.Chat(ctx, messages, toolDefs, sm.defaultModel, opts)
		if err != nil {
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}

		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			break
		}

		normalized := make([]providers.ToolCall, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			normalized = append(normalized, providers.NormalizeToolCall(tc))
		}

		// Build assistant message
		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: response.Content,
		}
		for _, tc := range normalized {
			argumentsJSON, _ := json.Marshal(tc.Arguments)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Name: tc.Name,
				Function: &providers.FunctionCall{
					Name:      tc.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
		messages = append(messages, assistantMsg)

		// Execute tool calls in parallel
		type indexedResult struct {
			result *ToolResult
			tc     providers.ToolCall
		}
		results := make([]indexedResult, len(normalized))
		var wg sync.WaitGroup
		for i, tc := range normalized {
			results[i].tc = tc
			wg.Add(1)
			go func(idx int, tc providers.ToolCall) {
				defer wg.Done()
				argsJSON, _ := json.Marshal(tc.Arguments)
				logger.InfoCF("subagent", fmt.Sprintf("Tool call: %s(%s)", tc.Name, utils.Truncate(string(argsJSON), 200)), nil)
				if tools != nil {
					results[idx].result = tools.ExecuteWithContext(ctx, tc.Name, tc.Arguments, channel, chatID, nil)
				} else {
					results[idx].result = ErrorResult("no tools available")
				}
			}(i, tc)
		}
		wg.Wait()

		for _, r := range results {
			content := r.result.ForLLM
			if content == "" && r.result.Err != nil {
				content = r.result.Err.Error()
			}
			messages = append(messages, providers.Message{
				Role:       "tool",
				Content:    content,
				ToolCallID: r.tc.ID,
			})
		}
	}

	return &toolLoopResult{Content: finalContent}, nil
}
