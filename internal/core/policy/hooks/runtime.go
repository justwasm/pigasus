package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Input is the JSON a hook receives on stdin.
//
// The field names are the documentation's own (camelCase): the common
// fields every hook receives, and the event-specific ones.
type Input struct {
	// HookEventName is the event being run, as written in hooks.json.
	HookEventName string `json:"hookEventName,omitempty"`
	// Cwd is the agent's working directory, where its commands run by default.
	Cwd string `json:"cwd,omitempty"`

	// ConversationID is the unique id of the active conversation.
	ConversationID string `json:"conversationId"`
	// WorkspacePaths are the absolute paths of the mounted workspaces.
	WorkspacePaths []string `json:"workspacePaths"`
	// TranscriptPath is the absolute path of the conversation's
	// transcript.jsonl.
	TranscriptPath string `json:"transcriptPath"`
	// ArtifactDirectoryPath is the absolute path of the directory that
	// holds the conversation's artifacts.
	ArtifactDirectoryPath string `json:"artifactDirectoryPath"`
	// ModelName is the name of the model that handles the invocation.
	ModelName string `json:"modelName"`

	// ToolCall is the proposed (PreToolUse) or the executed (PostToolUse)
	// tool call.
	ToolCall *ToolCall `json:"toolCall,omitempty"`
	// StepIdx is the 0-based index of the step in the trajectory.
	StepIdx int `json:"stepIdx,omitempty"`
	// Error is the runtime error of a failed tool call, empty when the
	// call succeeded.
	Error string `json:"error,omitempty"`

	// InvocationNum is the 0-indexed sequence number of the model
	// invocation.
	InvocationNum int `json:"invocationNum,omitempty"`
	// InitialNumSteps is the number of steps in the trajectory when the
	// invocation started.
	InitialNumSteps int `json:"initialNumSteps,omitempty"`

	// ExecutionNum is the sequence number of the execution attempt.
	ExecutionNum int `json:"executionNum,omitempty"`
	// TerminationReason says why the execution is stopping.
	TerminationReason string `json:"terminationReason,omitempty"`
	// FullyIdle reports whether every background task has finished.
	FullyIdle bool `json:"fullyIdle,omitempty"`
}

// ToolCall is the tool call a hook sees.
type ToolCall struct {
	// ID is the unique identifier of the tool call.
	ID string `json:"id,omitempty"`
	// Name is the name of the tool.
	Name string `json:"name"`
	// Args are the arguments the tool was called with.
	Args json.RawMessage `json:"args,omitempty"`
}

// ToolName returns the name of the tool the input carries, or the empty
// string when the input carries no tool call.
func (in Input) ToolName() string {
	if in.ToolCall == nil {
		return ""
	}
	return in.ToolCall.Name
}

// Output is the JSON a hook writes on stdout.
//
// The documentation names the fields per event; the ones that belong to
// another event simply stay empty.
type Output struct {
	// Decision is the hook's answer. For the tool events it is one of
	// the six decisions; for Stop it is "continue" (or anything else, and
	// the agent stops).
	Decision string `json:"decision,omitempty"`
	// Reason is the explanation shown to the agent or to the user.
	Reason string `json:"reason,omitempty"`
	// Overwrite replaces the tool call's arguments before the call
	// runs.
	Overwrite map[string]json.RawMessage `json:"overwrite,omitempty"`
	// PermissionOverrides are the resources whose default tool
	// permissions the hook overrides.
	PermissionOverrides []string `json:"permissionOverrides,omitempty"`
	// InjectSteps are the steps the hook adds to the conversation.
	InjectSteps []json.RawMessage `json:"injectSteps,omitempty"`
	// TerminationBehavior controls the flow after the injection:
	// "force_continue" or "terminate".
	TerminationBehavior string `json:"terminationBehavior,omitempty"`
}

// Outcome is what the handlers of one event answered.
//
// The decision is the strongest answer the handlers gave: a deny beats
// every other answer, and a plain allow is the floor.
type Outcome struct {
	// Decision is the aggregate decision of the handlers.
	Decision Decision
	// Reason is the reason the deciding handler gave.
	Reason string
	// Continue is the Stop event's "keep the loop running" answer.
	Continue bool
	// Overwrite are the replacement arguments the deciding handler
	// asked for.
	Overwrite map[string]json.RawMessage
	// PermissionOverrides are the resources the deciding handler
	// overrode.
	PermissionOverrides []string
	// InjectSteps are the steps the handlers asked to inject.
	InjectSteps []json.RawMessage
	// TerminationBehavior is the flow control the handlers asked for.
	TerminationBehavior string
	// Ran is the number of handlers that ran.
	Ran int
	// Problems are the handlers that failed or answered unusably.
	Problems []string
}

// Runtime executes the handlers of the documented events.
//
// The runtime holds the registry the loader produced and the shell the
// commands run through.
type Runtime struct {
	// Registry holds the entries the runtime fires.
	Registry *Registry
	// Shell returns the argv that runs a hook's command. A nil Shell
	// uses FallbackShellArgv.
	//
	// The authority for the shell is the tier that owns the reference's
	// command execution (internal/app/tools/shell), which this package
	// cannot reach from the core tier — so the caller hands the argv
	// over.
	Shell func(command string) []string
	// WorkDir is the directory a hook's command runs in. An empty
	// WorkDir means the process's own directory.
	WorkDir string
	// Env is the environment a hook's command sees. A nil Env means the
	// process's own environment.
	Env []string
}

// Fire runs the handlers of ev and aggregates their answers.
//
// A handler that fails does not decide: the failure is recorded in the
// outcome's Problems and the remaining handlers still run. A handler
// that writes no JSON has no opinion, and a handler that writes a
// decision the documentation does not name is treated as a prompt — the
// conservative reading of an unusable answer.
func (r *Runtime) Fire(ctx context.Context, ev Event, in Input) Outcome {
	out := Outcome{Decision: DecisionAllow}
	if r == nil {
		return out
	}
	if in.HookEventName == "" {
		in.HookEventName = string(ev)
	}
	if in.Cwd == "" && r.WorkDir != "" {
		in.Cwd = r.WorkDir
	}
	for _, entry := range r.Registry.Match(ev, in.ToolName()) {
		res, err := r.run(ctx, entry, in)
		if err != nil {
			out.Problems = append(out.Problems, err.Error())
			continue
		}
		out.Ran++
		out.merge(ev, res)
	}
	return out
}

// merge folds one handler's answer into the outcome.
func (o *Outcome) merge(ev Event, res Output) {
	if len(res.InjectSteps) > 0 {
		o.InjectSteps = append(o.InjectSteps, res.InjectSteps...)
	}
	if res.TerminationBehavior != "" {
		o.TerminationBehavior = res.TerminationBehavior
	}
	// The argument replacement is independent of the decision: a handler
	// may replace the call's arguments and delegate the decision. The
	// last replacement wins.
	if len(res.Overwrite) > 0 {
		o.Overwrite = res.Overwrite
	}
	if len(res.PermissionOverrides) > 0 {
		o.PermissionOverrides = res.PermissionOverrides
	}
	if ev == EventStop {
		if _, ok := ParseStopDecision(res.Decision); ok {
			o.Continue = true
			if res.Reason != "" {
				o.Reason = res.Reason
			}
		}
		return
	}
	if strings.TrimSpace(res.Decision) == "" {
		// No decision: the handler delegates to the next hook or to the
		// default policy.
		return
	}
	decision, ok := ParseDecision(res.Decision)
	if !ok {
		o.Problems = append(o.Problems, "unknown decision "+strconv.Quote(res.Decision))
		decision = DecisionAsk
	}
	if decisionRank(decision) <= decisionRank(o.Decision) {
		return
	}
	o.Decision = decision
	if res.Reason != "" {
		o.Reason = res.Reason
	}
}

// run executes one handler and reads its answer.
func (r *Runtime) run(ctx context.Context, entry Entry, in Input) (Output, error) {
	command := entry.Handler.Command
	if entry.PluginRoot != "" {
		command = strings.ReplaceAll(command, "${PLUGIN_ROOT}", entry.PluginRoot)
		command = strings.ReplaceAll(command, "$PLUGIN_ROOT", entry.PluginRoot)
	}
	if entry.PluginData != "" {
		command = strings.ReplaceAll(command, "${PLUGIN_DATA}", entry.PluginData)
		command = strings.ReplaceAll(command, "$PLUGIN_DATA", entry.PluginData)
	}
	argv := r.argv(command)
	if len(argv) == 0 {
		return Output{}, errors.New("hook " + strconv.Quote(entry.Hook) + ": no shell to run the command with")
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return Output{}, fmt.Errorf("hook %s: %w", strconv.Quote(entry.Hook), err)
	}
	timeout := time.Duration(entry.Handler.TimeoutSeconds()) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.WaitDelay = 500 * time.Millisecond
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = r.WorkDir
	cmdEnv := r.Env
	if entry.PluginRoot != "" || entry.PluginData != "" {
		cmdEnv = append([]string{}, cmdEnv...)
		if entry.PluginRoot != "" {
			cmdEnv = append(cmdEnv, "PLUGIN_ROOT="+entry.PluginRoot)
		}
		if entry.PluginData != "" {
			cmdEnv = append(cmdEnv, "PLUGIN_DATA="+entry.PluginData)
		}
	}
	cmd.Env = cmdEnv
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return Output{}, fmt.Errorf("hook %s: %v: %s", strconv.Quote(entry.Hook), err, detail)
		}
		return Output{}, fmt.Errorf("hook %s: %v", strconv.Quote(entry.Hook), err)
	}
	text := strings.TrimSpace(stdout.String())
	if text == "" {
		// A hook that writes nothing has no opinion.
		return Output{}, nil
	}
	var out Output
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return Output{}, fmt.Errorf("hook %s: wrote invalid JSON: %v", strconv.Quote(entry.Hook), err)
	}
	return out, nil
}

// argv returns the argv that runs a hook's command.
func (r *Runtime) argv(command string) []string {
	if r.Shell != nil {
		return r.Shell(command)
	}
	return FallbackShellArgv(command)
}

// FallbackShellArgv runs a command through the platform's usual shell.
//
// It is the fallback for a caller that has no shell to offer. The
// authority for the shell the reference runs its commands through lives
// in the app tier (internal/app/tools/shell), which the core tier cannot
// reach — so a caller that has it hands the argv over instead.
func FallbackShellArgv(command string) []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"powershell", "-NoProfile", "-NonInteractive", "-Command", command}
	case "darwin":
		return []string{"zsh", "-c", command}
	default:
		return []string{"bash", "-c", command}
	}
}
