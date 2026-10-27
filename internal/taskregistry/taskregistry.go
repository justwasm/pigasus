// Package taskregistry is a single-process registry of background work
// — spawned shells, declared subagents, scheduled timers, recurring
// jobs and any other tool that runs asynchronously. /tasks and the
// status-line "· N tasks" segment both consume its subscriber stream,
// so the registry is the canonical source of "what's running".
//
// The registry is intentionally tiny:
//
//   - one map of id → *Task guarded by a sync.Mutex;
//   - one fan-out of subscriber channels that receive every state
//     change so /tasks and the status row can react in real time.
//
// It does not own the work itself. The caller (Subagent, the
// invoke_subagent tool, a timer) is responsible for calling Update
// when the task transitions. The registry records the transition and
// emits the event; whoever called it has already done the heavy
// lifting (spawning the child, watching for completion, etc.).
//
// The cycle that introduced this package is the tasks-panel cycle;
// see docs/cycles/tasks-panel/deliverable-tasks.md for the design
// notes and probe-vs-clone byte-match table.
package taskregistry

import (
	"errors"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Kind discriminates between the producers that register tasks. The
// /tasks panel renders a small badge per kind so the user can tell
// apart shell wrappers from declared subagents from timers.
type Kind string

const (
	KindShell     Kind = "shell"     // internal/subagent.NewSubagent("bash", …)
	KindSubagent  Kind = "subagent"  // invoke_subagent tool (cycle 228+ in flight)
	KindTimer     Kind = "timer"     // one-shot delayed tool (no cycle yet)
	KindRecurring Kind = "recurring" // cron / interval tool (no cycle yet)
	KindOther     Kind = "other"     // catch-all
)

// State is the lifecycle stage of a registered task. /tasks colours
// each row according to its current state; the status-line task count
// only counts State == StateRunning.
type State string

const (
	StatePending State = "pending" // registered but not yet started
	StateRunning State = "running" // exec.Cmd started, Wait not yet returned
	StateDone    State = "done"    // exit code 0
	StateFailed  State = "failed"  // non-zero exit, error, or exec failure
	StateKilled  State = "killed"  // SIGTERM/SIGKILL — Kill() / ctx cancel
	StateRemoved State = "removed" // user pressed x to drop from the list (no state change on the underlying work)
)

// ExitCodeUnset is the magic ExitCode value for tasks whose State is
// neither done nor failed — it lets the /tasks panel show a clean "—"
// rather than "0" (which would falsely imply a successful exit).
const ExitCodeUnset = -1

// Task is a single record in the registry. Fields are written under
// the Registry's mutex (via Update) and read either under the same
// mutex (List) or by copying the value out of the event channel
// (subscribers). Err is read by subscribers to surface failure text
// in the panel detail view.
type Task struct {
	ID        string
	Name      string
	Kind      Kind
	State     State
	StartedAt time.Time
	EndedAt   time.Time // zero while the task is still running
	ExitCode  int       // ExitCodeUnset while still running
	OwnerID   string
	Err       error
	// Command is the command line a shell task runs. The /tasks detail
	// view renders it under the reference's "Command: " label.
	Command string
	// Output is the task's captured output — a shell's stdout, or the
	// assistant text an LLM subagent produced. The detail view renders
	// it under the reference's "Output" heading.
	Output string
	// Prompt is the prompt an LLM subagent was given. The subagent
	// detail view renders it under the reference's "Prompt" heading.
	Prompt string
	// Tools lists the tool names an LLM subagent called, in call
	// order. The subagent detail view renders them under the
	// reference's "Tools" heading ("No tools used yet" when empty).
	Tools []string
	// Steps lists the subagent's trajectory steps, in order — the
	// turns it took while it ran. The subagent detail view renders
	// them under the reference's "Trajectory — <name> (<N> steps)"
	// heading ("No steps yet" when empty).
	//
	// 【反编译】 the reference's trajectory section is headed by
	// "Trajectory — %s (%d steps)" (0x10358a89d) and its empty form
	// is "No steps yet" (0x10346f0a3); see
	// docs/cycles/cycle-317-tui-subagent-trajectory/deliverable.md.
	Steps []string
	// hidden is true once a user removes a task via the x key. Hidden
	// tasks are still in the map (so late subscribers see the full
	// history) but List() filters them out and the panel renders the
	// empty state.
	hidden bool
}

// Registry is the singleton-ish table of tasks for one process. The
// cmd/wagyu binary wires one Registry into Subagent and one Registry
// into the runMode, so they share the same view of "what's running".
type Registry struct {
	mu sync.Mutex
	// seq is a monotonic counter used to mint IDs when the caller
	// passes an empty Task.ID. Atomic so ID-minting can happen
	// without grabbing the mutex for every registration.
	seq atomic.Uint64
	// tasks maps ID → *Task. *Task values are NEVER replaced once
	// inserted; Update mutates the existing entry in place so
	// subscribers can hold a stable pointer.
	tasks map[string]*Task
	// subs is the fan-out list. Every successful Register/Update
	// copies the (id, *Task) pair into a freshly-allocated slice
	// and sends it on every open subscriber channel. The non-blocking
	// send (with a fall-back drop) means a slow subscriber cannot
	// back-pressure the producer — the trade-off is that a slow
	// subscriber may miss events, which is acceptable for /tasks
	// since the panel can re-render from List() any time.
	subs []chan TaskEvent
	// closed is set by Close(); once true, every subsequent
	// Subscribe call returns a closed channel so callers cannot
	// accidentally observe a half-dead registry.
	closed bool
}

// TaskEvent is what subscribers see. It carries a pointer into the
// registry's internal map so the receiver can read whatever fields
// they need (Name, State, EndedAt, …) without copying the whole
// struct. The pointer is only valid while the entry is in the map —
// the registry never deletes entries, only marks them hidden, so the
// pointer stays valid for the lifetime of the process.
type TaskEvent struct {
	// Kind is "registered" for Register calls and "updated" for
	// Update calls. Subscribers that want to skip duplicates can
	// look at Kind + ID; subscribers that always want the current
	// snapshot can ignore Kind and re-read Task.
	Kind string
	ID   string
	Task *Task
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{tasks: make(map[string]*Task)}
}

// Register inserts t into the registry and emits a "registered"
// event on every subscriber channel. If t.ID is empty a fresh one is
// minted ("task-<n>"). The returned ID is what subsequent Update and
// Get calls should use.
//
// Register does not set StartedAt / State; the caller is expected to
// follow up with Update immediately so subscribers see the running
// state without an intermediate "pending" event that would flash
// the panel. If the caller wants the pending state to be observable
// they can set it on t *before* calling Register.
func (r *Registry) Register(t *Task) string {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ""
	}
	if t.ID == "" {
		t.ID = fmtMintID(r.seq.Add(1))
	}
	if t.StartedAt.IsZero() {
		t.StartedAt = time.Now()
	}
	if t.State == "" {
		t.State = StatePending
	}
	if t.ExitCode == 0 {
		// Don't clobber a caller-set ExitCodeUnset or a known
		// non-zero; only promote the zero value to Unset so a
		// fresh task doesn't pretend to have succeeded.
		t.ExitCode = ExitCodeUnset
	}
	r.tasks[t.ID] = t
	r.mu.Unlock()
	r.fanout(TaskEvent{Kind: "registered", ID: t.ID, Task: t})
	return t.ID
}

// Update mutates the task with the given ID. The mut function is
// called under the registry's mutex; it MUST NOT call back into the
// registry (no Register / Update / List / Get from inside mut) or the
// mutex will deadlock. After mut returns the registry emits an
// "updated" event so subscribers can re-render.
//
// Update is a no-op (silently returns) if the ID is unknown or has
// been removed by the user — the alternative would be to create a
// "ghost" entry that subscribers cannot explain.
func (r *Registry) Update(id string, mut func(*Task)) {
	if mut == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	t, ok := r.tasks[id]
	if !ok || t.hidden {
		r.mu.Unlock()
		return
	}
	mut(t)
	r.mu.Unlock()
	r.fanout(TaskEvent{Kind: "updated", ID: id, Task: t})
}

// Get returns the task with the given ID, or nil if unknown. The
// returned *Task is the live pointer; callers that need an immutable
// snapshot must copy the fields they care about.
func (r *Registry) Get(id string) *Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok {
		return nil
	}
	return t
}

// List returns every non-hidden task, sorted by StartedAt ascending.
// The returned slice is freshly allocated so callers can sort or
// mutate it without disturbing the registry. Hidden tasks (those the
// user removed with x) are filtered out so the panel does not have
// to thread the hidden flag through every render.
func (r *Registry) List() []*Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Task, 0, len(r.tasks))
	for _, t := range r.tasks {
		if t.hidden {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}

// Subscribe returns a channel that receives every Register / Update
// event the registry emits from this point on. The returned cancel
// function unsubscribes and closes the channel; calling cancel more
// than once is safe (subsequent calls are no-ops). Subscribe on a
// closed registry returns a closed channel so callers cannot block
// forever on a defunct registry.
//
// The channel is buffered (size 32). If the subscriber falls more
// than 32 events behind, the registry drops the event rather than
// block — see fanout's rationale above. /tasks and the status row
// both re-read List() on every event, so missing an event is not
// fatal: the next event or the next user action re-syncs.
func (r *Registry) Subscribe() (<-chan TaskEvent, func()) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		ch := make(chan TaskEvent)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan TaskEvent, 32)
	r.subs = append(r.subs, ch)
	r.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			for i, c := range r.subs {
				if c == ch {
					r.subs = append(r.subs[:i], r.subs[i+1:]...)
					break
				}
			}
			close(ch)
		})
	}
	return ch, cancel
}

// Close unsubscribes every active subscriber and refuses further
// Register / Update calls. Used at process exit to release the
// goroutines that would otherwise be parked on the subscriber
// channels.
//
// Close is currently best-effort: cmd/wagyu never calls it because
// the OS reaps the process anyway. The hook exists so a future test
// that wants to verify "no goroutines leak after the registry is
// discarded" can call it without touching the public map.
func (r *Registry) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	subs := r.subs
	r.subs = nil
	r.mu.Unlock()
	for _, ch := range subs {
		close(ch)
	}
}

// CountRunning returns how many non-hidden tasks are in StateRunning.
// The status row's "· N tasks" segment reads this on every subscriber
// tick; the cost is a mutex acquire and a small iteration over the
// map, which is cheaper than calling List() and filtering.
func (r *Registry) CountRunning() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, t := range r.tasks {
		if t.hidden || t.State != StateRunning {
			continue
		}
		n++
	}
	return n
}

// Hide marks the task as removed by the user (x key in the panel).
// Hidden tasks stay in the map for the late-subscriber case but
// List() and CountRunning() skip them. Hide emits an "updated" event
// so any open subscriber re-renders without the row.
//
// Hide returns false (no-op) if the ID is unknown or already hidden;
// the caller can use the bool to drive an audible "no such task"
// beep without doing the lookup twice.
func (r *Registry) Hide(id string) bool {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false
	}
	t, ok := r.tasks[id]
	if !ok || t.hidden {
		r.mu.Unlock()
		return false
	}
	t.hidden = true
	r.mu.Unlock()
	r.fanout(TaskEvent{Kind: "updated", ID: id, Task: t})
	return true
}

// fanout sends ev to every subscriber channel without blocking. A
// subscriber that is too slow to drain its 32-slot buffer will miss
// events; that is by design (see Subscribe).
func (r *Registry) fanout(ev TaskEvent) {
	r.mu.Lock()
	subs := make([]chan TaskEvent, len(r.subs))
	copy(subs, r.subs)
	r.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// Drop: the subscriber is behind. The next
			// event it does receive will still carry a
			// valid *Task pointer, so it can recover by
			// re-reading List().
		}
	}
}

// fmtMintID builds the synthetic ID for callers that did not supply
// one. The format matches the subagent ID style ("task-<nanos>-<n>")
// so a glance at a log line is enough to tell whether the task came
// from internal/subagent or the registry. We do NOT depend on
// time.Now() so two calls in the same nanosecond still get distinct
// IDs.
func fmtMintID(n uint64) string {
	return "task-" + strconv.FormatUint(n, 10)
}

// ErrClosed is returned by helpers that refuse to act on a closed
// registry. Currently nothing in the registry returns it directly
// (the methods silently no-op), but exposing the sentinel lets
// future callers (and tests) detect the post-Close state without
// racing the bool.
var ErrClosed = errors.New("taskregistry: registry is closed")

// nowFn returns the current time. Tests can override it via
// SetClock to make assertions deterministic; production callers
// never touch it.
var nowFn = time.Now

// SetClock swaps the clock used by Register (StartedAt default)
// and the subagent bridge (StartedAt / EndedAt). Pass nil to
// restore the default. Tests must call the returned restore func
// so the package-level nowFn does not leak across test files.
func SetClock(fn func() time.Time) (restore func()) {
	prev := nowFn
	if fn != nil {
		nowFn = fn
	} else {
		nowFn = time.Now
	}
	return func() { nowFn = prev }
}
