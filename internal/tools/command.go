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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
		"Commands will be run with PAGER=cat. You may want to limit the length of output for commands that usually rely on paging and may contain very long output (e.g. git log, use git log -n <N>)." +
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

// BypassSandboxRequested reports whether a tool call asked to run outside the
// terminal sandbox.
//
// 【一手】 agy 1.4.6 added the flag to the run_command declaration
// ("Set to true to run outside the sandbox (requires elevated user permission
// for auth exchange, network, or port binding).", docs/cycles/reference-drift-146/).
// The reference treats it as a per-call decision — the sandbox package keeps
// it off the Sandbox state for exactly that reason.
func BypassSandboxRequested(args json.RawMessage) bool {
	var p map[string]json.RawMessage
	if err := json.Unmarshal(args, &p); err != nil {
		return false
	}
	raw, ok := p["BypassSandbox"]
	if !ok {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false
	}
	return b
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
//
// `NotificationTimeoutSeconds` is the reference's own parameter, read out
// of the 1.4.8 binary (【反编译】
// cortex/core/tools.(*RunCommandTool).withTimeoutNotificationField at
// 0x101dfa4b0, which bolts it onto the reflected schema with
// utils.AugmentSchemaField — name 26 B, type "integer" 7 B, description
// 386 B, all copied verbatim here). The reference's *default* declaration
// does not carry it — the field is added only when the tool has the
// notification capability — and the request bodies captured on 2026-10-22
// do not have it either, so the shipped wire fixture
// (model_tools_1.4.6.json) is untouched. The catalogue is the clone's own
// surface for custom agents, and there the parameter is advertised, so the
// model can actually use it.
var runCommandSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"CommandLine":{"type":"string","description":"The exact command line string to execute."},` +
	`"Cwd":{"type":"string","description":"The current working directory for the command"},` +
	`"BypassSandbox":{"type":"boolean","description":"Set to true to run outside the sandbox (requires elevated user permission for auth exchange, network, or port binding)."},` +
	`"IsDaemon":{"type":"boolean","description":"Set to true for long-running support processes that are meant to keep running in the background indefinitely (e.g. dev servers, file watchers, tunnels)."},` +
	`"RequestedTerminalID":{"type":"string","description":"Optional ID of a persistent terminal to reuse."},` +
	`"RunPersistent":{"type":"boolean","description":"Set to true to run this command in a persistent terminal."},` +
	`"WaitMsBeforeAsync":{"type":"integer","description":"Milliseconds to wait before sending the command to the background."},` +
	`"NotificationTimeoutSeconds":{"type":"integer","description":"Optional. If the command runs for longer than this duration (in seconds) after the wait time (WaitMsBeforeAsync), the system will notify you that it is still running. Use this instead of setting a manual timer for checking status of commands. Highly recommended if the command is expected to terminate at all. Do not set this for daemon commands, which are expected to run indefinitely."},` +
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
type RunCommand struct {
	Workspace string
	// ConversationID scopes the spilled output file under
	// brain/<conv>/.system_generated/steps/<n>/output.txt. Empty falls back
	// to OPENAGY_CONVERSATION_ID, then the process id (as ReadURLContent).
	ConversationID string
	// Home overrides the antigravity home (brain/ lives under it).
	Home string
}

func (r *RunCommand) Name() string { return "run_command" }
func (r *RunCommand) Schema() json.RawMessage {
	return runCommandSchema
}

// runCommandParams are run_command's arguments.
//
// The reference's own parameter names are in the captured 1.4.6
// declaration: CommandLine, Cwd, BypassSandbox, WaitMsBeforeAsync,
// IsDaemon, toolSummary. The clone's canonicaliser maps CommandLine to
// "command" (tools_params.go) and the rest arrive as they are.
type runCommandParams struct {
	Command    string `json:"command"`
	Cwd        string `json:"cwd"`
	Timeout    int    `json:"timeout"`
	Background bool   `json:"background"`
	// WaitMsBeforeAsync is the reference's own parameter: how long (in
	// milliseconds) the command gets to finish before it is sent to the
	// background as a task.
	WaitMsBeforeAsync int `json:"wait_ms_before_async"`
	// NotificationTimeoutSeconds is the reference's own parameter: how
	// long (in seconds) the command may keep running after the wait
	// window before the system notifies the model that it is still
	// running. Zero means no such notification.
	NotificationTimeoutSeconds int `json:"notification_timeout_seconds"`
	// IsDaemon marks a command that is expected to run indefinitely; the
	// reference sends it to the background straight away.
	IsDaemon bool `json:"is_daemon"`
}

func (r *RunCommand) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p runCommandParams
	// argsOf canonicalises the reference's CommandLine / Cwd keys.
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	p.Command = strings.TrimSpace(p.Command)
	if p.Command == "" {
		return nil, errors.New("empty command")
	}
	// 【一手】 The reference's own transcript (1.4.7): a run_command with
	// "WaitMsBeforeAsync": "5000" that outlives the wait window comes
	// back as "Tool is running as a background task with task id: …" —
	// the command keeps running as a task of the conversation.
	if p.IsDaemon {
		// A daemon command is expected to run forever, so there is
		// nothing to wait for: it goes straight to the background.
		return r.sendToBackground(p)
	}
	if p.Background {
		// Cycle 276: the background path now goes through the same shell as
		// the foreground one, so `background: true` can finally express a
		// pipeline / redirect (it used to shlex-split argv and reach the
		// program with `|` as a literal argument).
		return r.sendToBackground(p)
	}
	if p.WaitMsBeforeAsync > 0 {
		return r.runWithAsyncWindow(ctx, p)
	}
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, secondsToDuration(p.Timeout))
		defer cancel()
	}
	started := time.Now()
	var out SafeBuffer
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
	code, err := runExitCode(ctx, runErr)
	if err != nil {
		return nil, err
	}
	return r.renderResult(started, time.Now(), code, normalizeCommandOutput(out.String())), nil
}

// runWithAsyncWindow runs the command and gives it the reference's
// WaitMsBeforeAsync to finish.
//
// A command that finishes inside the window returns the foreground
// result — the same text the plain foreground path returns. A command
// that is still running when the window closes is sent to the background
// as a task: it keeps running, it gets the conversation's task id and
// task log, and the tool returns the reference's launch acknowledgement
// (【一手】 the reference's own transcript, 1.4.7).
//
// The command runs with the reference's pty on stdin and *without* the
// clone's VEOF: the reference's pty belongs to a persistent terminal
// whose input end stays open, so a command that reads stdin blocks, the
// wait window elapses and the command is handed to the background — which
// is what the reference does. The plain foreground path keeps the VEOF:
// it has no wait window, so a blocking read would wedge the turn.
func (r *RunCommand) runWithAsyncWindow(ctx context.Context, p runCommandParams) (any, error) {
	started := time.Now()
	convID := r.conversationID()
	taskID := nextTaskID(convID)
	// The task's id in the reference's shape: "<conversation-id>/task-<n>".
	// The table is keyed by it, so command_status, manage_task and
	// send_command_input all address a task the way the model names it.
	fullID := convID + "/" + taskID
	logPath := taskLogPath(scheduleHome(r.Home), convID, taskID)
	if err := createTaskLog(logPath); err != nil {
		return nil, err
	}

	argv := DefaultShellArgv(p.Command)
	// context.Background, not the caller's ctx: a command that outlives
	// the wait window must outlive the turn that started it.
	cmd := exec.CommandContext(context.Background(), argv[0], argv[1:]...)
	cmd.Dir = resolveCwd(r.Workspace, p.Cwd)
	cmd.Env = AgentShellEnv()

	// The task is created before the command starts: the command's
	// output is the task's output, and the task may yet become a
	// background task.
	t, err := backgroundTasks.newTask(fullID, logPath, p.Command, cmd)
	if err != nil {
		return nil, err
	}
	wait, stdin, err := startWithPTYStdin(cmd, false)
	if err != nil {
		backgroundTasks.remove(fullID)
		return nil, err
	}
	t.stdin = stdin
	backgroundTasks.watch(t, wait, stdin)

	window := time.Duration(p.WaitMsBeforeAsync) * time.Millisecond
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case <-t.finished:
		// The command finished inside the window: the foreground result.
		code, err := runExitCode(ctx, t.err)
		if err != nil {
			return nil, err
		}
		output, _, _ := t.stepState()
		return r.renderResult(started, time.Now(), code, normalizeCommandOutput(output)), nil
	case <-timer.C:
		// The window closed with the command still running: it is a task
		// now, and the model gets the reference's launch acknowledgement.
		// The reference arms the notification timer here too — after the
		// wait window, not before it.
		armNotificationTimeout(t, p.NotificationTimeoutSeconds)
		// The task is a task now, so it owes the model a completion
		// notification when the command is gone.
		notifyOnCompletion(t)
		return taskLaunchResult(started, convID, taskID, logPath), nil
	}
}

// sendToBackground starts the command as a background task and returns the
// reference's launch acknowledgement.
func (r *RunCommand) sendToBackground(p runCommandParams) (any, error) {
	convID := r.conversationID()
	taskID := nextTaskID(convID)
	fullID := convID + "/" + taskID
	logPath := taskLogPath(scheduleHome(r.Home), convID, taskID)
	if err := createTaskLog(logPath); err != nil {
		return nil, err
	}
	if _, err := backgroundTasks.start(r.Workspace, fullID, logPath, p.Command,
		DefaultShellArgv(p.Command), secondsToDuration(p.Timeout)); err != nil {
		return nil, err
	}
	if t, ok := backgroundTasks.get(fullID); ok {
		armNotificationTimeout(t, p.NotificationTimeoutSeconds)
		notifyOnCompletion(t)
	}
	return taskLaunchResult(time.Now(), convID, taskID, logPath), nil
}

// conversationID resolves the conversation the task belongs to.
func (r *RunCommand) conversationID() string {
	if r.ConversationID != "" {
		return r.ConversationID
	}
	if id := os.Getenv("OPENAGY_CONVERSATION_ID"); id != "" {
		return id
	}
	return processConversationID()
}

func runExitCode(ctx context.Context, err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return 124, nil
	case errors.As(err, &exitErr):
		code := exitErr.ExitCode()
		if code < 0 {
			code = 128
		}
		return code, nil
	case ctx.Err() != nil:
		return 130, nil
	default:
		return 0, fmt.Errorf("run_command: %w", err)
	}
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

// The run_command result text, captured from live 1.4.4 traffic (一手,
// docs/cycles/command-output-truncation). Plain path, non-empty output:
//
//	Created At: 2026-10-22T11:14:02+08:00
//	Completed At: 2026-10-22T11:14:03+08:00
//	Command exited with code 0.
//
//	Command output:
//	a
//	b
//
// Empty output replaces the last block with "No output from the command."
// There is no blank line between the stamp and the exit-code line, the exit
// line has no leading "The", the block header is "Command output:" (not
// "Output:"), and no trailing newline is appended to the output.
const (
	// maxCommandBodyBytes is where the reference stops echoing the output
	// verbatim: a 10195-byte output (10240-byte body) comes back whole, a
	// 10196-byte one (10241) is replaced by a note plus a head+tail preview.
	maxCommandBodyBytes = 10240
	// commandPreviewKept is the number of body bytes that survive
	// truncation, header included — constant across output shapes, which is
	// what the marker's arithmetic reports.
	commandPreviewKept = 9894
	// commandPreviewHead / commandPreviewTail split the surviving content
	// across the two ends, measured on output with no newlines. Newline-rich
	// output keeps fewer bytes in the reference (its cut is tokenizer-driven:
	// "a\n" x 10000 keeps 3269 + 3300), but the marker still reports the
	// fixed total — see the cycle report's 存疑 section.
	commandPreviewHead = 4902
	commandPreviewTail = 4947
)

func commandBody(code int, output string) string {
	if output == "" {
		return fmt.Sprintf("Command exited with code %d.\n\nNo output from the command.", code)
	}
	return fmt.Sprintf("Command exited with code %d.\n\nCommand output:\n%s", code, output)
}

func commandStamp(started, finished time.Time) string {
	return "Created At: " + started.Format(time.RFC3339) +
		"\nCompleted At: " + finished.Format(time.RFC3339) + "\n"
}

// renderResult renders the reference's run_command result text. Output whose
// body exceeds maxCommandBodyBytes is spilled to
// brain/<conv>/.system_generated/steps/<n>/output.txt and replaced by a note
// pointing at it plus a preview with the middle removed:
//
//	Created At: …
//	Completed At: …
//	The tool's output was truncated due to being too long. You can find the full output in a file at file://…/output.txt.
//
//	Here is a preview with the middle of the output truncated:
//	Command exited with code 0.
//
//	Command output:
//	<head>
//	<truncated 119072 of 128964 bytes from the middle of the output>
//	<tail>
//
// The preview itself is CRLF-terminated — the one place the reference emits
// CRLF — while the plain path keeps the command's own line endings.
func (r *RunCommand) renderResult(started, finished time.Time, code int, output string) string {
	return commandStamp(started, finished) + commandResultBody(code, output, r.spillOutput)
}

// commandResultBody renders the reference's run_command result body: the
// exit-code line, the output block, and — when the body is longer than
// maxCommandBodyBytes — the reference's head+tail preview with the middle
// removed.
//
// The body is what the tool result is made of, minus the timestamp stamp.
// The reference's completion notification carries the body alone (the
// captured notification in internal/tools/testdata/reference_background_task_1.4.7.txt
// has no "Created At:"/"Completed At:" line), so the notification path
// calls this directly.
//
// spill writes the untruncated output to a file and returns its path. A nil
// spill omits the note — the task notification names the task log instead,
// and that log holds the command's whole output.
func commandResultBody(code int, output string, spill func(string) (string, error)) string {
	body := commandBody(code, output)
	if len(body) <= maxCommandBodyBytes {
		return body
	}

	rendered := strings.ReplaceAll(output, "\n", "\r\n")
	head, tail := rendered, ""
	if len(rendered) > commandPreviewHead+commandPreviewTail {
		head = rendered[:commandPreviewHead]
		tail = rendered[len(rendered)-commandPreviewTail:]
	}
	header := fmt.Sprintf("Command exited with code %d.\n\nCommand output:\n", code)
	total := len(header) + len(rendered)
	marker := fmt.Sprintf("<truncated %d of %d bytes from the middle of the output>",
		total-commandPreviewKept, total)

	note := ""
	if spill != nil {
		if path, err := spill(output); err == nil && path != "" {
			note = "The tool's output was truncated due to being too long. " +
				"You can find the full output in a file at file://" + path + ".\n\n" +
				"Here is a preview with the middle of the output truncated:\n"
		}
	}
	return note + header + head + "\n" + marker + "\n" + tail
}

// spillOutput writes the full, unconverted output next to the reference's
// per-step artifacts. The note in the result text is only emitted when this
// succeeds — pointing the model at a file that does not exist would be worse
// than a bare preview.
func (r *RunCommand) spillOutput(output string) (string, error) {
	conversationID := r.ConversationID
	if conversationID == "" {
		conversationID = os.Getenv("OPENAGY_CONVERSATION_ID")
	}
	if conversationID == "" {
		conversationID = processConversationID()
	}
	dir := filepath.Join(scheduleHome(r.Home), "brain", conversationID,
		".system_generated", "steps", fmt.Sprintf("%d", nextStep(conversationID)))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(path, []byte(output), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
