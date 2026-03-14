package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/cron"
	"github.com/sipeed/picoclaw/pkg/utils"
)

// JobExecutor is the interface for executing cron jobs through the agent
type JobExecutor interface {
	ProcessDirectWithChannel(ctx context.Context, content, sessionKey, channel, chatID string) (string, error)
}

// CronTool provides scheduling capabilities for the agent
type CronTool struct {
	cronService *cron.CronService
	executor    JobExecutor
	msgBus      *bus.MessageBus
	execTool    *ExecTool
}

type cronParams struct {
	Action       string `json:"action" jsonschema:"Action to perform. Use 'add' when user wants to schedule a reminder or task."`
	Message      string `json:"message,omitempty" jsonschema:"The reminder/task message to display when triggered. If 'command' is used, this describes what the command does."`
	Command      string `json:"command,omitempty" jsonschema:"Optional: Shell command to execute directly (e.g., 'df -h'). If set, the agent will run this command and report output instead of just showing the message. 'deliver' will be forced to false for commands."`
	AtSeconds    int    `json:"at_seconds,omitempty" jsonschema:"One-time reminder: seconds from now when to trigger (e.g., 600 for 10 minutes later). Use this for one-time reminders like 'remind me in 10 minutes'."`
	EverySeconds int    `json:"every_seconds,omitempty" jsonschema:"Recurring interval in seconds (e.g., 3600 for every hour). Use this ONLY for recurring tasks like 'every 2 hours' or 'daily reminder'."`
	CronExpr     string `json:"cron_expr,omitempty" jsonschema:"Cron expression for complex recurring schedules (e.g., '0 9 * * *' for daily at 9am). Use this for complex recurring schedules."`
	JobID        string `json:"job_id,omitempty" jsonschema:"Job ID (for remove/enable/disable)"`
	Deliver      bool   `json:"deliver,omitempty" jsonschema:"If true, send message directly to channel. If false, let agent process message (for complex tasks). Default: true"`
}

var cronToolSpec = &ToolSpec{
	Name:        "cron",
	Description: "Schedule reminders, tasks, or system commands. IMPORTANT: When user asks to be reminded or scheduled, you MUST call this tool. Use 'at_seconds' for one-time reminders (e.g., 'remind me in 10 minutes' -> at_seconds=600). Use 'every_seconds' ONLY for recurring tasks (e.g., 'every 2 hours' -> every_seconds=7200). Use 'cron_expr' for complex recurring schedules. Use 'command' to execute shell commands directly.",
	Parameters: schemaForParams[cronParams](
		func(schema *jsonschema.Schema) {
			schema.Properties["action"].Enum = []any{"add", "list", "remove", "enable", "disable"}
		},
	),
}

// NewCronTool creates a new CronTool
// execTimeout: 0 means no timeout, >0 sets the timeout duration
func NewCronTool(
	cronService *cron.CronService, executor JobExecutor, msgBus *bus.MessageBus, workspace string, restrict bool,
	execTimeout time.Duration, config *config.Config,
) (*CronTool, error) {
	execTool, err := NewExecToolWithConfig(workspace, restrict, config)
	if err != nil {
		return nil, fmt.Errorf("unable to configure exec tool: %w", err)
	}

	execTool.SetTimeout(execTimeout)
	return &CronTool{
		cronService: cronService,
		executor:    executor,
		msgBus:      msgBus,
		execTool:    execTool,
	}, nil
}

func (t *CronTool) Spec() *ToolSpec {
	return cronToolSpec
}

// Execute runs the tool with the given arguments
func (t *CronTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	action, ok := args["action"].(string)
	if !ok {
		return ErrorResult("action is required")
	}

	switch action {
	case "add":
		return t.addJob(ctx, args)
	case "list":
		return t.listJobs()
	case "remove":
		return t.removeJob(args)
	case "enable":
		return t.enableJob(args, true)
	case "disable":
		return t.enableJob(args, false)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *CronTool) addJob(ctx context.Context, args map[string]any) *ToolResult {
	channel := ToolChannel(ctx)
	chatID := ToolChatID(ctx)

	if channel == "" || chatID == "" {
		return ErrorResult("no session context (channel/chat_id not set). Use this tool in an active conversation.")
	}

	message, ok := args["message"].(string)
	if !ok || message == "" {
		return ErrorResult("message is required for add")
	}

	var schedule cron.CronSchedule

	// Check for at_seconds (one-time), every_seconds (recurring), or cron_expr
	atSeconds, hasAt := args["at_seconds"].(float64)
	everySeconds, hasEvery := args["every_seconds"].(float64)
	cronExpr, hasCron := args["cron_expr"].(string)

	// Fix: type assertions return true for zero values, need additional validity checks
	// This prevents LLMs that fill unused optional parameters with defaults (0) from triggering wrong type
	hasAt = hasAt && atSeconds > 0
	hasEvery = hasEvery && everySeconds > 0
	hasCron = hasCron && cronExpr != ""

	// Priority: at_seconds > every_seconds > cron_expr
	if hasAt {
		atMS := time.Now().UnixMilli() + int64(atSeconds)*1000
		schedule = cron.CronSchedule{
			Kind: "at",
			AtMS: &atMS,
		}
	} else if hasEvery {
		everyMS := int64(everySeconds) * 1000
		schedule = cron.CronSchedule{
			Kind:    "every",
			EveryMS: &everyMS,
		}
	} else if hasCron {
		schedule = cron.CronSchedule{
			Kind: "cron",
			Expr: cronExpr,
		}
	} else {
		return ErrorResult("one of at_seconds, every_seconds, or cron_expr is required")
	}

	// Read deliver parameter, default to true
	deliver := true
	if d, ok := args["deliver"].(bool); ok {
		deliver = d
	}

	command, _ := args["command"].(string)
	if command != "" {
		deliver = false
	}

	messagePreview := utils.Truncate(message, 30)

	job, err := t.cronService.AddJob(
		messagePreview,
		schedule,
		message,
		deliver,
		channel,
		chatID,
	)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Error adding job: %v", err))
	}

	if command != "" {
		job.Payload.Command = command
		t.cronService.UpdateJob(job)
	}

	return SilentResult(fmt.Sprintf("Cron job added: %s (id: %s)", job.Name, job.ID))
}

func (t *CronTool) listJobs() *ToolResult {
	jobs := t.cronService.ListJobs(false)

	if len(jobs) == 0 {
		return SilentResult("No scheduled jobs")
	}

	var result strings.Builder
	result.WriteString("Scheduled jobs:\n")
	for _, j := range jobs {
		var scheduleInfo string
		if j.Schedule.Kind == "every" && j.Schedule.EveryMS != nil {
			scheduleInfo = fmt.Sprintf("every %ds", *j.Schedule.EveryMS/1000)
		} else if j.Schedule.Kind == "cron" {
			scheduleInfo = j.Schedule.Expr
		} else if j.Schedule.Kind == "at" {
			scheduleInfo = "one-time"
		} else {
			scheduleInfo = "unknown"
		}
		result.WriteString(fmt.Sprintf("- %s (id: %s, %s)\n", j.Name, j.ID, scheduleInfo))
	}

	return SilentResult(result.String())
}

func (t *CronTool) removeJob(args map[string]any) *ToolResult {
	jobID, ok := args["job_id"].(string)
	if !ok || jobID == "" {
		return ErrorResult("job_id is required for remove")
	}

	if t.cronService.RemoveJob(jobID) {
		return SilentResult(fmt.Sprintf("Cron job removed: %s", jobID))
	}
	return ErrorResult(fmt.Sprintf("Job %s not found", jobID))
}

func (t *CronTool) enableJob(args map[string]any, enable bool) *ToolResult {
	jobID, ok := args["job_id"].(string)
	if !ok || jobID == "" {
		return ErrorResult("job_id is required for enable/disable")
	}

	job := t.cronService.EnableJob(jobID, enable)
	if job == nil {
		return ErrorResult(fmt.Sprintf("Job %s not found", jobID))
	}

	status := "enabled"
	if !enable {
		status = "disabled"
	}
	return SilentResult(fmt.Sprintf("Cron job '%s' %s", job.Name, status))
}

// ExecuteJob executes a cron job through the agent
func (t *CronTool) ExecuteJob(ctx context.Context, job *cron.CronJob) string {
	channel := job.Payload.Channel
	chatID := job.Payload.To

	if channel == "" {
		channel = "cli"
	}
	if chatID == "" {
		chatID = "direct"
	}

	if job.Payload.Command != "" {
		args := map[string]any{
			"command": job.Payload.Command,
		}

		result := t.execTool.Execute(ctx, args)
		var output string
		if result.IsError {
			output = fmt.Sprintf("Error executing scheduled command: %s", result.ForLLM)
		} else {
			output = fmt.Sprintf("Scheduled command '%s' executed:\n%s", job.Payload.Command, result.ForLLM)
		}

		pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pubCancel()
		t.msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: output,
		})
		return "ok"
	}

	if job.Payload.Deliver {
		pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pubCancel()
		t.msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: job.Payload.Message,
		})
		return "ok"
	}

	sessionKey := fmt.Sprintf("cron-%s", job.ID)

	response, err := t.executor.ProcessDirectWithChannel(
		ctx,
		job.Payload.Message,
		sessionKey,
		channel,
		chatID,
	)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	_ = response
	return "ok"
}
