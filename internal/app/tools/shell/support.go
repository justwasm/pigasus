// support.go — the background-command table of the shell capability.
//
// Cycle 379: this file used to be internal/app/tools/support.go and held
// five unrelated things — the workspace walker, the text/binary
// heuristic, the background-command table, the notice inbox and the
// notebook runner. Each of the five has exactly one owner:
//
//   - the walker and the heuristic belong to the filesystem capability
//     (internal/app/tools/filesystem/walk.go),
//   - the notebook runner belongs to the filesystem capability too
//     (internal/app/tools/filesystem/notebook.go),
//   - the inbox belongs to the task capability
//     (internal/app/tools/tasks/inbox.go),
//   - the background-command table belongs here, with the shell.
//
// The table is the shell's own state: the command capability starts a
// task, the task capability asks for its status, and the remote-control
// surface kills it. It stays unexported — the exported door is
// tasks_kill.go and the BackgroundTask accessor below.

package shell

import (
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

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools"
)

// ---- background commands ---------------------------------------------------

// BackgroundTask is one background command started by run_command.
//
// 【一手】 The reference's background commands are *tasks*: they carry the
// conversation's task id ("<conversation-id>/task-<n>"), write their output
// to the conversation's task log, and are addressed by that id. See
// background_task.go.
type BackgroundTask struct {
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
	out    tools.SafeBuffer
	log    tools.SafeBuffer // cycle 258: persistent task log (finalized once at exit)
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
	killFn func() error
}

type bgTable struct {
	mu    sync.Mutex
	seq   int
	tasks map[string]*BackgroundTask
	order []string
}

var backgroundTasks = &bgTable{tasks: map[string]*BackgroundTask{}}

// newTask creates a task, wires the command's output streams to it and
// registers it in the table.
//
// The task has to exist before the command starts: the command's stdout
// and stderr are the task's buffers, and the wait-window path (the
// reference's WaitMsBeforeAsync) starts the command before it knows
// whether the command will be foregrounded or backgrounded.
func (b *bgTable) newTask(taskID, logPath, name string, cmd *exec.Cmd) (*BackgroundTask, error) {
	t := &BackgroundTask{
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
func (b *bgTable) watch(t *BackgroundTask, wait func() error, stdin io.Closer) {
	go t.monitor(wait, stdin)
}

// start registers and launches a background command over a pipe.
//
// The command's stdin is a pipe (not the reference's pty) so that
// send_command_input can write to it. taskID and logPath are the task's
// identity in the reference's shape — the caller mints them because the
// tool knows the conversation.
func (b *bgTable) start(ws, taskID, logPath, name string, argv []string, timeout time.Duration) (*BackgroundTask, error) {
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
func (b *bgTable) register(t *BackgroundTask) bool {
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
func (t *BackgroundTask) monitor(wait func() error, stdin io.Closer) {
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
func (t *BackgroundTask) HasCompleted() bool {
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
func notifyOnCompletion(t *BackgroundTask) {
	if t == nil {
		return
	}
	go func() {
		<-t.finished
		updateShellBackgroundTaskCompletion(t, t.err)
		t.NotifyCompletion()
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
func (t *BackgroundTask) NotifyCompletion() {
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
	result := commandResultBody(code, tools.NormalizeCommandOutput(t.out.String()), nil)
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
func (t *BackgroundTask) writeTaskLog() {
	if t.logPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(t.logPath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(t.logPath, []byte(t.out.String()), 0o644)
}

func (b *bgTable) get(id string) (*BackgroundTask, bool) {
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
			out = append(out, t.Status())
		}
	}
	return out
}

// stepState returns the command's state in the form the reference's
// send_command_input step string reads: the output as it stands, whether
// the command is still running, and the exit code once it has one.
//
// The step string is the reference's own renderer
// (step_strings/send_command_input.tmpl); see internal/app/prompt/step.
func (t *BackgroundTask) StepState() (output string, running bool, exitCode int) {
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

func (t *BackgroundTask) Status() string {
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
	// tools.SafeBuffer.String() takes its own lock — release t.mu first so
	// the writer goroutine can grab it if it ever needs to (it doesn't
	// today; tools.SafeBuffer is fully independent of t.mu).
	return fmt.Sprintf("%s\t%s\t%s\t%gs\n%s", id, state, command,
		time.Since(started).Seconds(), strings.TrimRight(body, "\n"))
}

func (t *BackgroundTask) WriteStdin(s string) error {
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

func (t *BackgroundTask) Kill() (string, error) {
	t.mu.Lock()
	done, cmd, killFn := t.done, t.cmd, t.killFn
	t.mu.Unlock()
	if done {
		return fmt.Sprintf("Task %s has already finished.", t.id), nil
	}
	if killFn != nil {
		if err := killFn(); err != nil {
			return "", err
		}
		t.mu.Lock()
		t.killed = true
		t.done = true
		t.completed = true
		t.mu.Unlock()
		if t.cancel != nil {
			t.cancel()
		}
		select {
		case <-t.finished:
		default:
			close(t.finished)
		}
		return fmt.Sprintf("Task %s killed.", t.id), nil
	}
	if cmd == nil || cmd.Process == nil {
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

// registerShellBackgroundTask registers a running background shell task into
// the shared task registry so it is surfaced in the active-items block under the prompt and in /tasks.
func registerShellBackgroundTask(t *BackgroundTask, command string) {
	if t == nil {
		return
	}
	reg := tools.SharedTaskRegistry()
	if reg == nil {
		return
	}
	reg.Register(&taskregistry.Task{
		ID:        t.id,
		Name:      command,
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: t.started,
		Command:   command,
		OutputFunc: func() string {
			return t.out.String()
		},
	})
	reg.RegisterHandle(t.id, &taskregistry.TaskHandle{
		Abort: func() {
			_, _ = t.Kill()
		},
		Cancel: func() {
			_, _ = t.Kill()
		},
	})
}

// updateShellBackgroundTaskCompletion updates the shared task registry upon command exit.
func updateShellBackgroundTaskCompletion(t *BackgroundTask, exitErr error) {
	if t == nil {
		return
	}
	reg := tools.SharedTaskRegistry()
	if reg == nil {
		return
	}
	reg.Update(t.id, func(tk *taskregistry.Task) {
		if tk.State == taskregistry.StateKilled {
			return
		}
		if exitErr == nil {
			tk.State = taskregistry.StateDone
			tk.ExitCode = 0
		} else {
			tk.State = taskregistry.StateFailed
			tk.ExitCode = 1
			tk.Err = exitErr
		}
		tk.EndedAt = time.Now()
		tk.Output = t.out.String()
	})
}
