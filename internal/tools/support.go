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
type bgTask struct {
	id      string
	command string
	started time.Time

	mu     sync.Mutex
	out    SafeBuffer
	log    SafeBuffer // cycle 258: persistent task log (finalized once at exit)
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc
	done   bool
	err    error
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

// start registers and launches a background command.
func (b *bgTable) start(ws, name string, argv []string, timeout time.Duration) (*bgTask, error) {
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

	t := &bgTask{command: name, cmd: cmd, started: time.Now(), cancel: cancel}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	t.stdin = stdin
	cmd.Stdout = &t.out
	cmd.Stderr = &t.out

	b.mu.Lock()
	b.seq++
	t.id = fmt.Sprintf("cmd-%d", b.seq)
	b.tasks[t.id] = t
	b.order = append(b.order, t.id)
	b.mu.Unlock()

	if err := cmd.Start(); err != nil {
		cancel()
		b.mu.Lock()
		delete(b.tasks, t.id)
		b.mu.Unlock()
		return nil, err
	}
	go t.monitor(cmd, stdin)
	return t, nil
}

func (t *bgTask) monitor(cmd *exec.Cmd, stdin io.Closer) {
	var reason string
	defer func() {
		if r := recover(); r != nil {
			if reason == "" {
				reason = "exited with panic: " + fmt.Sprint(r)
			}
		}
		recordFinalOutput(&t.out, &t.log, reason)
	}()

	waitErr := cmd.Wait()
	_ = stdin.Close()
	t.mu.Lock()
	t.done = true
	t.err = waitErr
	t.mu.Unlock()

	reason = bgExitReason(waitErr)
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
	t.cancel()
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
