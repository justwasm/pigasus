// support.go holds the small pieces the non-network tools share: workspace
// walking, a text/binary heuristic, the background-command table, the notice
// inbox, and the notebook runner.
package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// errStopWalk ends a walk early once a result limit is reached.
var errStopWalk = errors.New("stop walk")

// skipDirs are never descended into by the search tools.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true,
	"__pycache__": true, ".idea": true, ".vscode": true,
}

// walkFiles calls fn for every regular file under root. rel is root-relative
// (or the base name when root is itself a file).
func walkFiles(root string, fn func(path, rel string) error) error {
	base := filepath.Clean(root)
	info, err := os.Stat(base)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fn(base, filepath.Base(base))
	}
	return filepath.Walk(base, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if fi.IsDir() {
			if path == base {
				return nil
			}
			if skipDirs[fi.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return nil
		}
		return fn(path, rel)
	})
}

// looksTextual reports whether data is plausibly text: no NUL byte and valid
// UTF-8 in the leading window.
func looksTextual(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	window := data
	if len(window) > 8192 {
		window = window[:8192]
	}
	if bytes.IndexByte(window, 0) >= 0 {
		return false
	}
	return utf8.Valid(window)
}

// trimTo shortens s to at most n bytes without splitting a rune.
func trimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\u2026"
}

// ---- background commands ---------------------------------------------------

// bgTask is one background command started by run_command.
//
// 【一手】 The reference's background commands are *tasks*: they carry the
// conversation's task id ("<conversation-id>/task-<n>"), write their output
// to the conversation's task log, and are addressed by that id. See
// background_task.go.
type bgTask struct {
	// id is the reference's task id: "<conversation-id>/task-<n>".
	id      string
	command string
	started time.Time
	// logPath is the reference's task log file. The command's output is
	// written there when it finishes.
	logPath string
	// finished is closed when the command exits. A caller that waits with
	// a deadline (the reference's WaitMsBeforeAsync) selects on it.
	finished chan struct{}

	mu     sync.Mutex
	out    SafeBuffer
	log    SafeBuffer // cycle 258: persistent task log (finalized once at exit)
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc
	done   bool
	err    error
	// completed is set when the command is gone. The "the command is
	// still running" timer checks it: a command that finished gets the
	// completion notification, never both.
	completed bool
	// killed is set when the task was stopped on purpose (the user killed
	// it through manage_task, or the tool's own cancel path). A killed
	// task reports "was canceled", not a result.
	killed bool
}

// SafeBuffer wraps a bytes.Buffer with a mutex so concurrent writes
// from os/exec's stdout/stderr goroutines and reads from status() do
// not race. The pre-cycle-tasks-panel bytes.Buffer-as-Reader pattern
// was racy under -race (cmd.Wait → cmd.writerDescriptor writes the
// buffer while the panel-render goroutine calls t.out.String()).
//
// Tasks-panel regression: this wrapper was added so the race detector
// in TestBackgroundCommandKill / TestBackgroundCommands stays clean
// when run with -race.
//
// appendOnce is the cycle-258 field: recordFinalOutput uses it to make
// the "first writer wins" guarantee idempotent. It is a zero-value
// sync.Once, so callers can use it without explicit initialisation.
// See bg_output.go for the call sites.
type SafeBuffer struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	appendOnce sync.Once
}

// Write appends p to the buffer under the lock.
func (b *SafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// WriteString writes s to the buffer as if it were a single Write call.
// Cycle 258 added this method so recordFinalOutput can append the exit
// reason without allocating a []byte for the literal — same trick as
// bytes.Buffer.WriteString.
func (b *SafeBuffer) WriteString(s string) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.WriteString(s)
}

// WriteByte appends a single byte to the buffer. Cycle 258 added this
// method for the "\n" terminator recordFinalOutput writes after the
// pending output and the exit reason.
func (b *SafeBuffer) WriteByte(c byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.WriteByte(c)
}

// String returns the buffer's contents under the lock.
func (b *SafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type bgTable struct {
	mu    sync.Mutex
	seq   int
	tasks map[string]*bgTask
	order []string
}

var backgroundTasks = &bgTable{tasks: map[string]*bgTask{}}

// newTask creates a task, wires the command's output streams to it and
// registers it in the table.
//
// The task has to exist before the command starts: the command's stdout
// and stderr are the task's buffers, and the wait-window path (the
// reference's WaitMsBeforeAsync) starts the command before it knows
// whether the command will be foregrounded or backgrounded.
func (b *bgTable) newTask(taskID, logPath, name string, cmd *exec.Cmd) (*bgTask, error) {
	t := &bgTask{
		id:       taskID,
		command:  name,
		cmd:      cmd,
		started:  time.Now(),
		cancel:   context.CancelFunc(func() {}),
		logPath:  logPath,
		finished: make(chan struct{}),
	}
	if cmd != nil {
		cmd.Stdout = &t.out
		cmd.Stderr = &t.out
	}
	if !b.register(t) {
		return nil, errDuplicateTask
	}
	return t, nil
}

// watch hands the task's wait function to the monitor goroutine.
func (b *bgTable) watch(t *bgTask, wait func() error, stdin io.Closer) {
	go t.monitor(wait, stdin)
}

// start registers and launches a background command over a pipe.
//
// The command's stdin is a pipe (not the reference's pty) so that
// send_command_input can write to it. taskID and logPath are the task's
// identity in the reference's shape — the caller mints them because the
// tool knows the conversation.
func (b *bgTable) start(ws, taskID, logPath, name string, argv []string, timeout time.Duration) (*bgTask, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty command")
	}
	ctx := context.Background()
	cancel := context.CancelFunc(func() {})
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = ws
	cmd.Env = AgentShellEnv()
	setProcessGroup(cmd)

	t, err := b.newTask(taskID, logPath, name, cmd)
	if err != nil {
		cancel()
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		b.remove(taskID)
		return nil, err
	}
	t.stdin = stdin

	if err := cmd.Start(); err != nil {
		cancel()
		b.remove(t.id)
		return nil, err
	}
	b.watch(t, func() error { return cmd.Wait() }, stdin)
	return t, nil
}

// errDuplicateTask is returned when a task id is already in the table.
var errDuplicateTask = errors.New("background task id already in use")

// register adds the task to the table. It reports false when the id is
// already taken.
func (b *bgTable) register(t *bgTask) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.tasks[t.id]; ok {
		return false
	}
	b.tasks[t.id] = t
	b.order = append(b.order, t.id)
	return true
}

// remove drops the task from the table.
func (b *bgTable) remove(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.tasks, id)
}

// monitor waits for the command and records its final state.
//
// The wait function owns the process (cmd.Wait for the pipe path, the pty
// helper's wait for the pty path); stdin is closed once the command is
// gone. When the command finishes, its output is written to the task log
// — the reference's own task logs hold exactly the command's output
// (【一手】 ~/.gemini/antigravity-cli/brain/<conv>/.system_generated/tasks/
// task-56.log holds the `go test` output of a backgrounded run_command, and
// the log of a still-running task is empty).
func (t *bgTask) monitor(wait func() error, stdin io.Closer) {
	var reason string
	defer func() {
		if r := recover(); r != nil {
			if reason == "" {
				reason = "exited with panic: " + fmt.Sprint(r)
			}
		}
		recordFinalOutput(&t.out, &t.log, reason)
		t.writeTaskLog()
		t.mu.Lock()
		t.done = true
		t.completed = true
		t.mu.Unlock()
		close(t.finished)
	}()

	waitErr := wait()
	if stdin != nil {
		_ = stdin.Close()
	}
	t.mu.Lock()
	t.err = waitErr
	t.mu.Unlock()

	reason = bgExitReason(waitErr)
}

// hasCompleted reports whether the command is gone.
func (t *bgTask) hasCompleted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.completed
}

// notifyOnCompletion arms the reference's completion notification for a
// task: the model is told that the task is over once the command is gone.
//
// The notification is armed, not fired: the command may still be running,
// and the task's own monitor closes t.finished when it is gone. A command
// that finished inside the wait window never became a task, so nothing is
// armed for it — the foreground result is what the model reads.
func notifyOnCompletion(t *bgTask) {
	if t == nil {
		return
	}
	go func() {
		<-t.finished
		t.notifyCompletion()
	}()
}

// notifyCompletion fires the reference's completion notification for a
// finished background task.
//
// 【一手】 The reference delivers the notification when the command is gone:
// the model is told that the task finished and what it produced, and the
// task log is named so the model can read the whole output. The captured
// notification — the reference's own text — is in
// internal/tools/testdata/reference_background_task_1.4.7.txt:
//
//	Task id "<conv>/task-56" finished with result:
//	Command exited with code 0.
//
//	Command output:
//	...
//
//	Log: file://<task log>
//
// A canceled task has no result: the reference's own status word for it is
// "was canceled", and the log still names the output the command produced
// before it was stopped.
//
// A non-zero exit is not a failure here (the repo's own invariant: a
// non-zero exit is a nil-error result, not an error), so the task
// "finished" and the result carries the exit code.
func (t *bgTask) notifyCompletion() {
	t.mu.Lock()
	err, killed := t.err, t.killed
	t.mu.Unlock()

	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
		if code < 0 {
			code = 128
		}
	}
	// A task that was stopped on purpose — the user killed it, or the
	// command's own context was canceled — reports the reference's own
	// status word and carries no result.
	if killed || errors.Is(err, context.Canceled) || (exitErr != nil && exitErr.ExitCode() < 0) {
		notifyTask(TaskNotification{
			TaskID:   t.id,
			Body:     taskNotification(t.id, "was canceled", "", t.logPath),
			LogPath:  t.logPath,
			Priority: TaskPriorityHigh,
			At:       time.Now(),
		})
		return
	}
	// The result is the CLI's own formatter text for the finished command
	// — the exit-code line and the output — without the "Created At:" /
	// "Completed At:" stamp the tool result carries: the reference's
	// completion notification has no stamp.
	result := commandResultBody(code, normalizeCommandOutput(t.out.String()), nil)
	notifyTask(TaskNotification{
		TaskID:   t.id,
		Body:     taskCompletionNotification(t.id, result, t.logPath),
		LogPath:  t.logPath,
		Priority: TaskPriorityHigh,
		At:       time.Now(),
	})
}

// writeTaskLog writes the task's output to the reference's task log file.
//
// The reference rewrites the file with the command's raw output (【一手】
// the reference's own task-56.log holds exactly the `go test` output of a
// backgrounded run_command, with no exit-reason line), so this writes the
// live buffer rather than the in-memory task log.
func (t *bgTask) writeTaskLog() {
	if t.logPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(t.logPath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(t.logPath, []byte(t.out.String()), 0o644)
}

func (b *bgTable) get(id string) (*bgTask, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.tasks[id]
	return t, ok
}

func (b *bgTable) list() []string {
	b.mu.Lock()
	ids := append([]string(nil), b.order...)
	b.mu.Unlock()
	sort.Strings(ids)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if t, ok := b.get(id); ok {
			out = append(out, t.status())
		}
	}
	return out
}

// stepState returns the command's state in the form the reference's
// send_command_input step string reads: the output as it stands, whether
// the command is still running, and the exit code once it has one.
//
// The step string is the reference's own renderer
// (step_strings/send_command_input.tmpl); see internal/agystep.
func (t *bgTask) stepState() (output string, running bool, exitCode int) {
	t.mu.Lock()
	done, err := t.done, t.err
	t.mu.Unlock()
	// Cycle 258: a finished task's authoritative output is the
	// persistent task log, not the live buffer — see status().
	if done {
		output = t.log.String()
	} else {
		output = t.out.String()
	}
	if !done {
		return output, true, 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return output, false, exitErr.ExitCode()
	}
	return output, false, 0
}

func (t *bgTask) status() string {
	t.mu.Lock()
	state := "running"
	if t.done {
		state = "finished"
		if t.err != nil {
			state = "failed: " + t.err.Error()
		}
	}
	id, command, started := t.id, t.command, t.started
	t.mu.Unlock()
	// Cycle 258: when the task has finished, prefer t.log over t.out.
	// t.log is the persistent task log — the cycle-258 fix appends the
	// tail of the live buffer to it via recordFinalOutput on every exit
	// path (normal finish, non-zero exit, signal kill, user cancel). A
	// caller that races the cmd.Wait goroutine and sees t.done = true
	// but a still-empty t.out will now see the final state in t.log.
	// For tasks that are still running we keep the live buffer, which
	// reflects what the agent has seen so far.
	var body string
	if t.done {
		body = t.log.String()
	} else {
		body = t.out.String()
	}
	// SafeBuffer.String() takes its own lock — release t.mu first so
	// the writer goroutine can grab it if it ever needs to (it doesn't
	// today; SafeBuffer is fully independent of t.mu).
	return fmt.Sprintf("%s\t%s\t%s\t%gs\n%s", id, state, command,
		time.Since(started).Seconds(), strings.TrimRight(body, "\n"))
}

func (t *bgTask) writeStdin(s string) error {
	t.mu.Lock()
	done, stdin := t.done, t.stdin
	t.mu.Unlock()
	if done {
		return fmt.Errorf("command %s has already finished", t.id)
	}
	if stdin == nil {
		return fmt.Errorf("command %s does not accept input", t.id)
	}
	_, err := io.WriteString(stdin, s)
	return err
}

func (t *bgTask) kill() (string, error) {
	t.mu.Lock()
	done, cmd := t.done, t.cmd
	t.mu.Unlock()
	if done {
		return fmt.Sprintf("Task %s has already finished.", t.id), nil
	}
	if cmd.Process == nil {
		return "", fmt.Errorf("task %s has no process", t.id)
	}
	if err := killProcessGroup(cmd); err != nil {
		return "", err
	}
	// Cycle 258: AGY 1.4.0 changelog line 24 — the output that was
	// buffered up to the moment of the kill is also the final output
	// of the task; record it now so a caller that polls command_status
	// immediately after manage_task kill sees the tail in the log
	// without waiting for the cmd.Wait goroutine to settle. The
	// cmd.Wait defer is still the authoritative writer; if it lands
	// first (which it usually does, because the signal we just sent
	// propagates within microseconds) the sync.Once in log.appendOnce
	// makes this call a no-op.
	recordFinalOutput(&t.out, &t.log, "killed by user")
	t.mu.Lock()
	t.killed = true
	t.mu.Unlock()
	t.cancel()
	// Wait for the command to be reaped before returning. The task's log
	// file is written by the monitor goroutine when the command exits
	// (writeTaskLog), so a caller that tears the workspace down right
	// after the kill would race that write: the file would reappear in a
	// directory that is being removed. The signal is SIGKILL, so the wait
	// is short; the bound keeps a wedged child from blocking the caller.
	select {
	case <-t.finished:
	case <-time.After(5 * time.Second):
	}
	return fmt.Sprintf("Killed %s.", t.id), nil
}

// ---- inbox -----------------------------------------------------------------

type inboxStore struct {
	mu    sync.Mutex
	items []string
}

var inbox inboxStore

func (i *inboxStore) add(msg string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.items = append(i.items, msg)
}

func (i *inboxStore) list() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]string(nil), i.items...)
}

func (i *inboxStore) clear() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	n := len(i.items)
	i.items = nil
	return n
}

// ---- notebooks -------------------------------------------------------------

// runNotebook executes a notebook with nbconvert and returns the executed
// notebook on stdout. Errors name the missing binary explicitly so the model
// can report it.
func runNotebook(ctx context.Context, ws, absPath string) (any, error) {
	bin, err := exec.LookPath("jupyter")
	if err != nil {
		return nil, fmt.Errorf("notebook_execution: jupyter is not installed (%v)", err)
	}
	cmd := exec.CommandContext(ctx, bin, "nbconvert", "--to", "notebook", "--execute", "--stdout", absPath)
	cmd.Dir = ws
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("notebook_execution: %s", msg)
	}
	return out.String(), nil
}
