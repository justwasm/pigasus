package workflow

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/procgroup"
)

//go:embed runtime.py
var runtimePy []byte

var runSeq atomic.Uint64

// RunWorkflowSchema is the JSON schema advertised for the run_workflow tool.
var RunWorkflowSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"Args":{"type":"object","description":"Optional arguments for the script, which reads them from the args dict."},` +
	`"Cwd":{"type":"string","description":"Optional working directory for the script. Defaults to the primary workspace."},` +
	`"Description":{"type":"string","description":"Brief summary of the workflow goal."},` +
	`"Script":{"type":"string","description":"Python source of the workflow script. Set exactly one of Script and ScriptPath."},` +
	`"ScriptPath":{"type":"string","description":"Absolute path of a Python workflow script file. Set exactly one of Script and ScriptPath."},` +
	`"WaitMsBeforeAsync":{"type":"integer","description":"Optional milliseconds to wait (up to 10000) before running in the background."},` +
	`"toolAction":{"type":"string","description":"Brief 2-5 word phrase in -ing form describing the specific action."},` +
	`"toolSummary":{"type":"string","description":"Brief 2-5 word noun phrase describing the specific task."}` +
	`},"required":["toolSummary","toolAction"]}`)

// BackgroundHandle represents a workflow execution that outlived WaitMsBeforeAsync
// or was launched directly in the background.
type BackgroundHandle struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`

	done   chan struct{}
	cancel context.CancelFunc

	mu     sync.Mutex
	result string
	err    error
}

// Done returns a channel that is closed when the background workflow finishes.
func (h *BackgroundHandle) Done() <-chan struct{} {
	return h.done
}

// Result waits for the background workflow to finish and returns its formatted
// output and error.
func (h *BackgroundHandle) Result() (string, error) {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.result, h.err
}

// Cancel terminates the background workflow and its process group.
func (h *BackgroundHandle) Cancel() {
	if h.cancel != nil {
		h.cancel()
	}
}

// String returns a short human-readable summary of the running handle.
func (h *BackgroundHandle) String() string {
	return fmt.Sprintf("Workflow %s running in background", h.ID)
}

// RunWorkflow implements tools.Tool and tools.Describer for `run_workflow`.
type RunWorkflow struct {
	Workspace      string
	Workspaces     []string
	ConversationID string
	StepIndex      int
	PythonBin      string
	LibraryPaths   []string

	AgentExecutor      AgentExecutor
	ToolExecutor       ToolExecutor
	TrajectoryProvider TrajectoryProvider
	OnEvent            func(ev Event)

	// AsyncLauncher, when non-nil, is invoked when a workflow runs in the
	// background (either WaitMsBeforeAsync == 0 or WaitMsBeforeAsync elapsed).
	AsyncLauncher func(ctx context.Context, handle *BackgroundHandle) (any, error)
}

func (r *RunWorkflow) Name() string { return "run_workflow" }

func (r *RunWorkflow) Description() string {
	return "Run a Python workflow script that orchestrates many agents and tool calls. " +
		"Use it when the orchestration itself is code: fanning out over many independent pieces of work " +
		"(files, modules, bugs, review angles), staged pipelines where each stage feeds the next, or " +
		"aggregating, filtering, or cross-checking the results of many agents. Don't use it for a single " +
		"delegated task; use a subagent. Read the run-workflow skill before writing a script: it documents " +
		"the primitives, their arguments, and how the script runs. The workflow runs in the background; " +
		"you are notified when it ends."
}

func (r *RunWorkflow) Schema() json.RawMessage {
	return RunWorkflowSchema
}

type runWorkflowParams struct {
	Script            string `json:"script"`
	ScriptPath        string `json:"script_path"`
	Description       string `json:"description"`
	Cwd               string `json:"cwd"`
	WaitMsBeforeAsync int    `json:"wait_ms_before_async"`
}

func (r *RunWorkflow) Run(ctx context.Context, rawArgs json.RawMessage) (any, error) {
	started := time.Now()

	var p runWorkflowParams
	if err := tools.ArgsOf(rawArgs, &p); err != nil {
		return tools.ResultStamp(started) + "Encountered error in tool validation: " + err.Error(), nil
	}

	hasScript := strings.TrimSpace(p.Script) != ""
	hasPath := strings.TrimSpace(p.ScriptPath) != ""
	if hasScript == hasPath {
		return tools.ResultStamp(started) + "Encountered error in tool validation: set exactly one of Script and ScriptPath", nil
	}

	scriptSource := p.Script
	scriptName := "<workflow>"
	if hasPath {
		if !filepath.IsAbs(p.ScriptPath) {
			return tools.ResultStamp(started) + "Encountered error in tool validation: ScriptPath must be an absolute path", nil
		}
		data, err := os.ReadFile(p.ScriptPath)
		if err != nil {
			return tools.ResultStamp(started) + fmt.Sprintf("Encountered error in tool validation: read ScriptPath %q: %v", p.ScriptPath, err), nil
		}
		scriptSource = string(data)
		scriptName = p.ScriptPath
	}

	scriptArgs, err := extractRawArgsMap(rawArgs)
	if err != nil {
		return tools.ResultStamp(started) + "Encountered error in tool validation: invalid Args: " + err.Error(), nil
	}

	workspaces := r.resolveWorkspaces()
	cwd := r.resolveCwd(workspaces, p.Cwd)
	convID := r.resolveConversationID()
	stepIdx := r.resolveStepIndex(ctx)

	waitMs := p.WaitMsBeforeAsync
	if waitMs < 0 {
		waitMs = 0
	}
	if waitMs > 10000 {
		waitMs = 10000
	}

	cfg := runtimeConfig{
		Script:         scriptSource,
		ScriptName:     scriptName,
		Args:           scriptArgs,
		ConversationID: convID,
		StepIndex:      stepIdx,
		Workspaces:     workspaces,
		LibraryPaths:   r.LibraryPaths,
		Cwd:            cwd,
	}

	// Synchronous path when WaitMsBeforeAsync == 0 and no AsyncLauncher is configured.
	if waitMs == 0 && r.AsyncLauncher == nil {
		res, execErr := r.execute(ctx, started, cfg)
		if execErr != nil {
			return tools.ResultStamp(started) + "Encountered error in tool execution: " + execErr.Error(), nil
		}
		return res, nil
	}

	// Background-capable path.
	bgCtx, bgCancel := context.WithCancel(context.Background())
	handle := &BackgroundHandle{
		ID:          fmt.Sprintf("wf-%d", runSeq.Add(1)),
		Description: p.Description,
		done:        make(chan struct{}),
		cancel:      bgCancel,
	}

	go func() {
		defer close(handle.done)
		defer bgCancel()
		res, execErr := r.execute(bgCtx, started, cfg)
		handle.mu.Lock()
		if execErr != nil {
			handle.result = tools.ResultStamp(started) + "Encountered error in tool execution: " + execErr.Error()
			handle.err = execErr
		} else {
			handle.result = res
		}
		handle.mu.Unlock()
	}()

	if waitMs > 0 {
		timer := time.NewTimer(time.Duration(waitMs) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-handle.done:
			res, _ := handle.Result()
			return res, nil
		case <-ctx.Done():
			handle.Cancel()
			res, _ := handle.Result()
			return res, nil
		case <-timer.C:
			if r.AsyncLauncher != nil {
				return r.AsyncLauncher(ctx, handle)
			}
			return handle, nil
		}
	}

	// waitMs == 0 && r.AsyncLauncher != nil
	return r.AsyncLauncher(ctx, handle)
}

type runtimeConfig struct {
	Host           string         `json:"host"`
	Port           int            `json:"port"`
	Token          string         `json:"token"`
	Script         string         `json:"script"`
	ScriptName     string         `json:"script_name"`
	Args           map[string]any `json:"args"`
	ConversationID string         `json:"conversation_id"`
	StepIndex      int            `json:"step_index"`
	Workspaces     []string       `json:"workspaces"`
	LibraryPaths   []string       `json:"library_paths,omitempty"`
	Cwd            string         `json:"-"`
}

func (r *RunWorkflow) execute(ctx context.Context, started time.Time, cfg runtimeConfig) (string, error) {
	bridge, err := newBridgeServer(ctx, r.AgentExecutor, r.ToolExecutor, r.TrajectoryProvider, r.OnEvent)
	if err != nil {
		return "", err
	}
	defer bridge.close()

	tmpDir, err := os.MkdirTemp("", "openagy-wf-*")
	if err != nil {
		return "", fmt.Errorf("create workflow temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	runtimePath := filepath.Join(tmpDir, "runtime.py")
	if err := os.WriteFile(runtimePath, runtimePy, 0o600); err != nil {
		return "", fmt.Errorf("write workflow runtime: %w", err)
	}

	cfg.Host = bridge.host
	cfg.Port = bridge.port
	cfg.Token = bridge.token
	if cfg.Args == nil {
		cfg.Args = map[string]any{}
	}
	if cfg.Workspaces == nil {
		cfg.Workspaces = []string{}
	}

	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal workflow config: %w", err)
	}
	cfgPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(cfgPath, cfgBytes, 0o600); err != nil {
		return "", fmt.Errorf("write workflow config: %w", err)
	}

	pythonBin := r.PythonBin
	if pythonBin == "" {
		pythonBin = "python3"
	}

	cmd := exec.Command(pythonBin, runtimePath, cfgPath)
	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	procgroup.Set(cmd)

	var outBuf tools.SafeBuffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start %s: %w", pythonBin, err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		_ = procgroup.Kill(cmd)
		waitErr = <-waitCh
		if waitErr == nil {
			waitErr = ctx.Err()
		}
	}

	logs := bridge.formattedLogs()
	output := strings.TrimRight(outBuf.String(), "\r\n")
	se := bridge.getScriptError()

	if waitErr != nil || se != nil {
		var errMsg string
		switch {
		case se != nil:
			errMsg = se.Message
			if se.Traceback != "" && se.Traceback != se.Message {
				errMsg += "\n" + se.Traceback
			}
		case errors.Is(ctx.Err(), context.Canceled):
			errMsg = "workflow cancelled"
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			errMsg = "workflow timed out"
		default:
			errMsg = waitErr.Error()
		}
		return formatWorkflowFailure(started, errMsg, logs, output), nil
	}

	return formatWorkflowSuccess(started, logs, output), nil
}

func formatWorkflowSuccess(started time.Time, logs, output string) string {
	var sb strings.Builder
	sb.WriteString(tools.ResultStamp(started))
	if logs != "" {
		sb.WriteString("\nLogs:\n")
		sb.WriteString(logs)
		sb.WriteByte('\n')
	}
	if output != "" {
		sb.WriteString("\nOutput:\n")
		sb.WriteString(output)
		sb.WriteByte('\n')
	}
	if logs == "" && output == "" {
		sb.WriteString("\nWorkflow completed.\n")
	}
	return sb.String()
}

func formatWorkflowFailure(started time.Time, errMsg, logs, output string) string {
	var sb strings.Builder
	sb.WriteString(tools.ResultStamp(started))
	sb.WriteString("Encountered error in tool execution: ")
	sb.WriteString(errMsg)
	sb.WriteByte('\n')
	if logs != "" {
		sb.WriteString("\nLogs:\n")
		sb.WriteString(logs)
		sb.WriteByte('\n')
	}
	if output != "" {
		sb.WriteString("\nOutput:\n")
		sb.WriteString(output)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func extractRawArgsMap(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	var argsRaw json.RawMessage
	if v, ok := top["Args"]; ok {
		argsRaw = v
	} else if v, ok := top["args"]; ok {
		argsRaw = v
	}
	if len(argsRaw) == 0 || string(argsRaw) == "null" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(argsRaw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func (r *RunWorkflow) resolveWorkspaces() []string {
	if len(r.Workspaces) > 0 {
		out := make([]string, len(r.Workspaces))
		copy(out, r.Workspaces)
		return out
	}
	if r.Workspace != "" {
		return []string{r.Workspace}
	}
	return nil
}

func (r *RunWorkflow) resolveCwd(workspaces []string, cwd string) string {
	if cwd != "" {
		if !filepath.IsAbs(cwd) && len(workspaces) > 0 {
			return filepath.Join(workspaces[0], cwd)
		}
		return cwd
	}
	if len(workspaces) > 0 {
		return workspaces[0]
	}
	return ""
}

func (r *RunWorkflow) resolveConversationID() string {
	if r.ConversationID != "" {
		return r.ConversationID
	}
	if id := os.Getenv("OPENAGY_CONVERSATION_ID"); id != "" {
		return id
	}
	return tools.ProcessConversationID()
}

func (r *RunWorkflow) resolveStepIndex(ctx context.Context) int {
	if r.StepIndex > 0 {
		return r.StepIndex
	}
	if idx, ok := actor.ToolStepIndexFromContext(ctx); ok {
		return idx
	}
	return 0
}
