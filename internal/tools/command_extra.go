package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// RunShellCommand — Source: tools.run_shell_command.
// Runs an arbitrary shell string in the workspace. Execution is driven by
// a pure-Go interpreter (mvdan.cc/sh/v3), which keeps behaviour identical
// across macOS / Linux / Windows and decouples the tool from whichever
// /bin/sh the user's PATH happens to point at.
type RunShellCommand struct{ Workspace string }

func (r *RunShellCommand) Name() string { return "run_shell_command" }
func (r *RunShellCommand) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout":{"type":"integer"}},"required":["command"]}`)
}
func (r *RunShellCommand) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, secondsToDuration(p.Timeout))
		defer cancel()
	}

	prog, err := syntax.NewParser().Parse(strings.NewReader(p.Command), "")
	if err != nil {
		return nil, fmt.Errorf("run_shell_command: parse: %w", err)
	}

	// SafeBuffer, not bytes.Buffer: the interpreter starts every
	// external command through os/exec, which copies the child's
	// output in a goroutine of its own. A script with a background job
	// (`cmd &`) therefore has two writers on the same buffer at once,
	// which is a data race on bytes.Buffer's internal slice. The
	// same reason run_command uses SafeBuffer above.
	var stdout, stderr SafeBuffer
	runner, err := interp.New(
		interp.StdIO(io.Reader(nil), &stdout, &stderr),
		interp.Dir(r.Workspace),
		interp.Env(expand.ListEnviron(AgentShellEnv()...)),
	)
	if err != nil {
		return nil, fmt.Errorf("run_shell_command: interp: %w", err)
	}
	runErr := runner.Run(ctx, prog)
	return stdout.String() + stderr.String(), runErr
}

// ExecCommand — Source: tools.exec_command. execvp with explicit env.
type ExecCommand struct{ Workspace string }

func (e *ExecCommand) Name() string { return "exec_command" }
func (e *ExecCommand) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"env":{"type":"object","additionalProperties":{"type":"string"}}},"required":["command"]}`)
}
func (e *ExecCommand) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, p.Command, p.Args...)
	cmd.Dir = e.Workspace
	env := AgentShellEnv()
	if len(p.Env) > 0 {
		for k, v := range p.Env {
			env = append(env, k+"="+v)
		}
	}
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return map[string]any{
		"stdout": stdout.String(),
		"stderr": stderr.String(),
		"exit":   cmd.ProcessState.ExitCode(),
		"err":    errString(err),
	}, nil
}

// KillProcess — Source: tools.kill_process. Kills a process by PID
// (SIGTERM, then SIGKILL after 5s).
type KillProcess struct{}

func (k *KillProcess) Name() string { return "kill_process" }
func (k *KillProcess) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pid":{"type":"integer"},"signal":{"type":"string"}},"required":["pid"]}`)
}
func (k *KillProcess) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		PID    int    `json:"pid"`
		Signal string `json:"signal"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.PID <= 0 {
		return nil, fmt.Errorf("kill_process: invalid pid %d", p.PID)
	}
	proc, err := os.FindProcess(p.PID)
	if err != nil {
		return nil, err
	}
	if p.Signal == "" {
		p.Signal = "SIGTERM"
	}
	return nil, proc.Signal(parseSignal(p.Signal))
}

// ListProcesses — Source: tools.list_processes. ps -ef on Unix, tasklist on Windows.
type ListProcesses struct{}

func (l *ListProcesses) Name() string { return "list_processes" }
func (l *ListProcesses) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (l *ListProcesses) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "tasklist", "/FO", "CSV", "/NH")
	} else {
		cmd = exec.CommandContext(ctx, "ps", "-ef")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	return string(out), nil
}

func secondsToDuration(s int) time.Duration {
	return time.Duration(s) * time.Second
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func parseSignal(s string) os.Signal {
	switch strings.ToUpper(s) {
	case "SIGKILL", "KILL", "9":
		return os.Kill
	case "SIGINT", "INT", "2":
		return os.Interrupt
	case "SIGTERM", "TERM", "15":
		return os.Kill
	default:
		return os.Kill
	}
}
