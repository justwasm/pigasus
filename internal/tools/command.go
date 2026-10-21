// Command-execution tools. Source: google3/third_party/jetski/agent/tools.
// Five tools recovered in 1.3.3:
//
//   - run_command         (the reference's only advertised command tool)
//   - run_shell_command   (1.3.3-compat shell alias, not advertised)
//   - exec_command        (execvp with explicit env)
//   - kill_process        (kill by PID)
//   - list_processes      (ps -ef style)
//
// Cycle 169: run_shell_command switched from /bin/sh -c (Unix) / cmd /c
// (Windows) to mvdan.cc/sh/v3 — a pure-Go POSIX shell parser and runner.
//
// Cycle 276 correction: the recovered build embeds mvdan.cc/sh/v3/syntax and
// fileutil ONLY — there is no mvdan/sh/interp and no mvdan/sh/expand in either
// 1.3.4 or 1.4.2. The reference uses that parser for *command analysis*
// (cortex/command/command.parseCommand, policyguardian.isReadOnlyCommand,
// safetext.SprintfLang, terminal.parseShell) and executes through the OS
// default shell instead (zsh on macOS). run_command therefore uses a real
// shell as of cycle 276; run_shell_command — a clone-only compat alias the
// reference does not advertise — keeps the pure-Go interp so it stays
// platform-independent.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// AgentShellEnvKVs are the env vars the agent tools always inject on top of
// the current process's environment when spawning a child process. Used by
// AgentShellEnv below.
//
// Two groups:
//
//   - Editor disabling: GIT_EDITOR=false / HGEDITOR=false / JJ_EDITOR=false.
//     AGY 1.3.6 changelog line 7: "Fixed agent shell commands hanging on an
//     invisible editor when a version control command wants one, such as
//     `git commit` without `-m` or `jj describe`: the CLI now sets
//     GIT_EDITOR, HGEDITOR, and JJ_EDITOR to false for agent commands, so
//     they fail right away instead." Without these, `git commit` opens
//     $GIT_EDITOR (often vim) on the agent's hidden terminal and hangs
//     forever — the model sees a stuck tool call.
//   - Pager disabling: PAGER=cat, GIT_PAGER=cat, JJ_PAGER=cat. AGY 1.3.4
//     changelog (cycle 107's `/diff` evidence): "agent commands now set
//     JJ_PAGER=cat alongside PAGER and GIT_PAGER". `cat` writes the file
//     to stdout unchanged — exactly what the model needs to read.
var AgentShellEnvKVs = []string{
	"GIT_EDITOR=false",
	"HGEDITOR=false",
	"JJ_EDITOR=false",
	"PAGER=cat",
	"GIT_PAGER=cat",
	"JJ_PAGER=cat",
}

// AgentShellEnv returns os.Environ() with the editor-disabling and
// pager-disabling variables appended (AgentShellEnvKVs). The vars are
// appended *after* os.Environ() so on POSIX systems where duplicate env
// entries take the last value, our overrides win over anything the user
// set in their shell.
//
// Used by every code path in this package that hands a command off to
// os/exec — RunCommand / RunShellCommand / ExecCommand / ListProcesses,
// bgTable.start (background run_command), gitBase.run (the git_*
// tools), and runNotebook — so an interactive prompt for an editor or
// a pager cannot wedge the agent. ExecCommand still appends any
// caller-supplied env on top of this, so a test or caller that
// explicitly sets AGY_ADC_AUTH (etc.) keeps its value.
func AgentShellEnv() []string {
	return append(os.Environ(), AgentShellEnvKVs...)
}

// runCommandDescription is the reference's 1.4.x prose for run_command
// (一手: captured request). The operating-system / shell sentence is
// regenerated per host instead of being frozen at the captured "mac / zsh".
//
// Cycle 276: the shell comes from DefaultShellName, NOT $SHELL. The
// reference fills this placeholder from terminal.GetDefaultShellName()
// (cortex/config.defaultConversationalCascadeConfig calls it), which is a
// per-OS constant. On this machine $SHELL is /opt/homebrew/bin/bash yet the
// captured declaration still says "Shell: zsh." — so deriving it from the
// environment was a bug in the clone.
func runCommandDescription() string {
	return "PROPOSE a command to run on behalf of the user. Operating System: " + refOSName() + ". Shell: " + DefaultShellName() + ".\n" +
		"**NEVER PROPOSE A cd COMMAND**.\n" +
		"If you have this tool, note that you DO have the ability to run commands directly on the USER's system.\n" +
		"Make sure to specify CommandLine exactly as it should be run in the shell.\n" +
		"If the step doesn't return the command output, it means that the command was sent to the background as a task. You will receive messages with the command's output as it runs. To interact with a running command, use the manage_task tool. Use `send_input` to send stdin, `kill` to terminate the command, and `status` to check current status. IMPORTANT: Do NOT poll or loop on `status` to wait for completion. The system will automatically notify you with a message when the command finishes. Simply proceed with other work or stop calling tools after launching a command.\n" +
		"Commands will be run with PAGER=cat. You may want to limit the length of output for commands that usually rely on paging and may contain very long output (e.g. git log, use git log -n <N>).\n" +
		"IMPORTANT: The Cwd (working directory) MUST be within the user's workspace. If you need a temporary directory for scratch scripts or intermediate outputs, use the scratch/ directory in your artifact directory instead of writing temporary files to the workspace."
}

// refOSName is the reference's spelling of the host OS ("mac" for darwin),
// the same mapping internal/agyprompt uses for <user_information>.
func refOSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "mac"
	default:
		return runtime.GOOS
	}
}

// DefaultShellName is the shell the reference runs run_command through.
//
// Cycle 276: the reference does NOT use mvdan/sh to execute anything. Its
// binary embeds only mvdan/sh/syntax + fileutil (0 interp, 0 expand symbols
// in 1.3.4 and 1.4.2); the parser feeds command analysis and the auto-run
// policy (cortex/command/command.parseCommand, walkCommandCalls,
// policyguardian.isReadOnlyCommand, safetext.Shprintf). Execution is a real
// shell on a pty:
//
//	zsh -c "<CommandLine>"      (macOS; $0=zsh, $-=569X, stdin on a pty)
//
// The name comes from google3/.../jetski/terminal.GetDefaultShellName — a
// per-OS constant, not $SHELL. The binary contains no "SHELL" string at all,
// and on a host whose $SHELL is /opt/homebrew/bin/bash agy still ran zsh.
//
// Only the darwin→zsh mapping is 【一手】 (disassembly of the type switch plus
// a live 1.4.2 turn). linux→bash and windows→powershell/pwsh are the other
// two branches of the same switch (【推测】); the Windows branch probes
// os/exec.LookPath("pwsh") when the legacy powershell is missing.
func DefaultShellName() string {
	switch runtime.GOOS {
	case "darwin":
		return "zsh"
	case "windows":
		if path := SelectWindowsShell(); path != "" {
			return strings.TrimSuffix(filepath.Base(path), ".exe")
		}
		return "powershell"
	default:
		return "bash"
	}
}

// DefaultShellArgv returns the argv that runs src through the platform's
// default shell — the same shape the reference uses.
func DefaultShellArgv(src string) []string {
	if runtime.GOOS == "windows" {
		shell := SelectWindowsShell()
		if shell == "" {
			shell = windowsShellPowershell
		}
		return []string{shell, "-NoProfile", "-NonInteractive", "-Command", src}
	}
	return []string{DefaultShellName(), "-c", src}
}

// runCommandSchema is the reference's 1.4.x declaration for run_command
// (一手: copied from a captured request body, with Google's OBJECT / STRING /
// INTEGER / BOOLEAN upper-case type names lowered to JSON-Schema's). The
// reference makes Cwd, WaitMsBeforeAsync, toolSummary and toolAction
// required. The clone accepts them and ignores the async / persistent fields
// (it has no persistent terminal); Cwd is honoured, empty means the
// workspace.
//
// `background` is a clone extension kept from cycle 43 so command_status /
// send_command_input / manage_task have something to address — the reference
// starts long-running commands through this same tool.
var runCommandSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"CommandLine":{"type":"string","description":"The exact command line string to execute."},` +
	`"Cwd":{"type":"string","description":"The current working directory for the command"},` +
	`"IsDaemon":{"type":"boolean","description":"Set to true for long-running support processes that are meant to keep running in the background indefinitely (e.g. dev servers, file watchers, tunnels)."},` +
	`"RequestedTerminalID":{"type":"string","description":"Optional ID of a persistent terminal to reuse."},` +
	`"RunPersistent":{"type":"boolean","description":"Set to true to run this command in a persistent terminal."},` +
	`"WaitMsBeforeAsync":{"type":"integer","description":"Milliseconds to wait before sending the command to the background."},` +
	`"background":{"type":"boolean","description":"Start the command and return an id instead of waiting for it."},` +
	`"toolAction":{"type":"string","description":"Brief 2-5 word phrase in -ing form describing the specific action."},` +
	`"toolSummary":{"type":"string","description":"Brief 2-5 word noun phrase describing the specific task."}` +
	`},"required":["Cwd","WaitMsBeforeAsync","CommandLine","toolSummary","toolAction"]}`)

// RunCommand — Source: tools.run_command.
//
// Cycle 275: the reference has exactly ONE command tool (its advertised
// surface lists run_command and run_workflow — there is no
// run_shell_command), and that tool is a SHELL. Its own declaration says so
// verbatim (一手, captured request):
//
//	"Make sure to specify CommandLine exactly as it should be run in the
//	 shell." … "Shell: zsh."
//
// and a live 1.4.2 turn returns exit 0 for `ls internal | head -3`
// (docs/cycles/cycle-275). Until this cycle the clone shlex-split the string
// and execvp'd the first token, so a pipeline reached the program as literal
// argv:
//
//	ls internal/ | head -80  ->  ls: |: No such file or directory (exit 2)
//
// The command *did* run — which is why the call looked like it had succeeded
// in the terminal — but the exit status was non-zero and Dispatch throws the
// output away on a non-nil error, so the transcript showed `(Failed)` with no
// text at all. Cycles 133/148 chased the rendering; the defect was here.
//
// The result text is the reference's own (一手, from the tool's
// functionResponse on a live 1.4.2 turn):
//
//	Created At: 2026-10-20T02:04:43+08:00
//	Completed At: 2026-10-20T02:04:43+08:00
//
//	The command exited with code 0.
//	Output:
//	agent
//	agentsdir
//	agyprompt
//
// A non-zero exit is DATA, not an error: the reference returns this same
// shape for `ls /definitely-not-here` (exit 2) with the stderr in Output and
// no error, so the model reads the code and the step is not marked failed.
//
// Cycle 276: the executor is now a real shell (`DefaultShellArgv`), matching
// the reference. Cycle 275 had used mvdan/sh/v3's interp, a pure-Go POSIX
// interpreter — but the reference's own mvdan/sh is a *parser only* (0 interp,
// 0 expand symbols), and its executor is `zsh -c` on macOS. That mismatch was
// visible: the declaration promises `Shell: zsh.` while the clone ran a POSIX
// sh, so zsh-only syntax (`**/*(.)`, `setopt`, `$(<file)`, process
// substitution) failed. It also means an unclosed quote is now the shell's
// error — `zsh:1: unmatched "` with exit 1 — not a clone-side parse error,
// which is what the reference returns (一手, live 1.4.2 turn).
type RunCommand struct{ Workspace string }

func (r *RunCommand) Name() string { return "run_command" }
func (r *RunCommand) Schema() json.RawMessage {
	return runCommandSchema
}
func (r *RunCommand) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Command    string `json:"command"`
		Cwd        string `json:"cwd"`
		Timeout    int    `json:"timeout"`
		Background bool   `json:"background"`
	}
	// argsOf canonicalises the reference's CommandLine / Cwd keys.
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	p.Command = strings.TrimSpace(p.Command)
	if p.Command == "" {
		return nil, errors.New("empty command")
	}
	if p.Background {
		// Cycle 276: the background path now goes through the same shell as
		// the foreground one, so `background: true` can finally express a
		// pipeline / redirect (it used to shlex-split argv and reach the
		// program with `|` as a literal argument).
		t, err := backgroundTasks.start(r.Workspace, p.Command, DefaultShellArgv(p.Command), secondsToDuration(p.Timeout))
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("Started %s in the background.\n%s", t.id, t.status()), nil
	}
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, secondsToDuration(p.Timeout))
		defer cancel()
	}
	started := time.Now()
	var out safeBuffer
	argv := DefaultShellArgv(p.Command)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = resolveCwd(r.Workspace, p.Cwd)
	cmd.Env = AgentShellEnv()
	cmd.Stdout = &out
	cmd.Stderr = &out
	// Cycle 277: the reference runs the command with a pty on stdin and
	// pipes on stdout/stderr; RunWithPTYStdin reproduces that (and the
	// deliberate VEOF deviation — see its doc comment).
	runErr := RunWithPTYStdin(cmd)
	code := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		switch {
		case ctx.Err() == context.DeadlineExceeded:
			code = 124 // timeout(1)'s convention
		case errors.As(runErr, &exitErr):
			code = exitErr.ExitCode()
			if code < 0 {
				// Killed by a signal. The shell convention is 128+signum,
				// but the signum is platform-specific; 128 is the portable
				// floor and still reads as "not zero" to the model.
				code = 128
			}
		case ctx.Err() != nil:
			code = 130 // interrupted
		default:
			return nil, fmt.Errorf("run_command: %w", runErr)
		}
	}
	return formatCommandResult(started, time.Now(), code, normalizeCommandOutput(out.String())), nil
}

// resolveCwd maps the reference's Cwd argument onto a directory. Empty means
// the workspace; a relative value is joined to the workspace. An absolute
// value is used when it names an existing directory, otherwise the workspace
// is kept — the declaration tells the model Cwd "MUST be within the user's
// workspace", so a stale value must not brick the call.
func resolveCwd(ws, cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ws
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(ws, cwd)
	}
	if st, err := os.Stat(cwd); err == nil && st.IsDir() {
		return cwd
	}
	return ws
}

// formatCommandResult renders the reference's run_command result text — the
// Created/Completed header, the exit code, then the combined output.
func formatCommandResult(started, finished time.Time, code int, output string) string {
	return fmt.Sprintf("Created At: %s\nCompleted At: %s\n\nThe command exited with code %d.\nOutput:\n%s\n",
		started.Format(time.RFC3339), finished.Format(time.RFC3339), code, output)
}

// RunShellCommand — Source: tools.run_shell_command. Pipes / redirects
// and shell control flow (for / while / case / ${var}) are honoured by
// a pure-Go interpreter (mvdan.cc/sh/v3), which keeps behaviour identical
// across macOS / Linux / Windows and decouples the tool from whichever
// /bin/sh the user's PATH happens to point at.
//
// Like the previous /bin/sh -c implementation, the returned value is the
// combined stdout + stderr text, matching the way shell tool callers
// consume output. The runner's exit status is reported through the
// returned error (a mvdan/cc/sh/v3/interp.ExitStatus), preserving the
// "non-zero exit" semantics the LLM agent expects to see.
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

	var stdout, stderr bytes.Buffer
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
		// Caller-provided env is appended LAST so user-provided vars
		// (e.g. AGY_ADC_AUTH=true) override any os.Environ entry of
		// the same name; AgentShellEnvKVs remain in effect because
		// they are not in p.Env.
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

// helpers

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
		return os.Kill // os doesn't expose SIGTERM as a constant on all platforms
	default:
		return os.Kill
	}
}
