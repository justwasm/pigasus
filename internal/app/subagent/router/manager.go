// Package subagentrouter is the registry the prompt-input layer's
// `@<subagent> <message>` syntax dispatches against.
//
// AGY 1.2.9 changelog line 177: "Added `@<subagent> <message>` prompt
// syntax to send a message directly to a subagent conversation, with
// autocomplete listing running and completed subagents".
//
// The clone's `internal/subagent` package holds the bash-process
// Subagents that `internal/tools` (`create_task`, ddgr search) spawn;
// the `internal/subagenttools` package drives LLM-side delegation via
// PiG `coding.Session`. Neither is a long-lived, name-keyed registry
// today. Cycle 231 introduces that registry here so the chat input
// layer has a stable target to route against.
//
// The package is stdlib-only and imports neither subagent nor
// subagenttools — the dispatch contract is a thin `Sender` interface
// the wiring layer adapts the existing Subagent / subagenttools types
// onto (cycle 232 work, not this cycle). Tests drive a fake Sender so
// the routing semantics stay verifiable without spinning up PiG or
// bash subprocesses.
//
// Stop-reason display (cycle subagent-stopped-clear-on-progress,
// AGY 1.2.13): each Entry carries a `StopReason` string that the
// `/agents` panel and the running-agents list render in place of the
// default `Done` label while a Stop is the most recent terminal event
// for that subagent. The rule, restated from the changelog line:
// "Subagent Stop reason should remain visible until a NEW Stop event
// replaces it, but progress events while running should never clear
// the displayed Stop reason." The two methods that mutate the reason
// — ReportStop and ReportProgress — make the rule explicit at the
// API level so the wiring layer cannot accidentally clear it from
// elsewhere.
package router

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// State is the lifecycle tag a registered Entry carries. Today it is
// purely informational (the Sender decides whether it is "running" or
// "done" — this package just renders the tag through List and the
// future picker).
type State string

const (
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
)

// Sender is the dispatch target. A real wiring adapts a PiG
// `coding.Session` (subagenttools) or a bash `Subagent.Send` onto this
// interface; the registry itself stays free of those imports.
type Sender interface {
	// Send delivers message to the registered subagent. The
	// returned error is the wiring's own failure (network down,
	// context cancelled, pipe closed) — the Manager does not
	// interpret it. Dispatch expects the call to be
	// context-aware so a parent that wants to cancel a stuck
	// subagent can do so without killing the whole TUI.
	Send(ctx context.Context, message string) error
}

// SenderFunc is the function-shaped adapter so a closure can implement
// Sender without a named type.
type SenderFunc func(ctx context.Context, message string) error

// Send calls f(ctx, message).
func (f SenderFunc) Send(ctx context.Context, message string) error {
	return f(ctx, message)
}

// Entry is what the registry hands out. The name is immutable after
// Register; State is the wiring's lifecycle tag (running / completed /
// failed). Started is when the entry was first inserted and is
// preserved across State transitions so the picker can sort by recency.
//
// StopReason is the displayed terminal reason for the entry — what
// `/agents` and the running-agents list show in place of the default
// `Done` label while a Stop is the most recent terminal event. It is
// mutated only by ReportStop and ReportProgress on the parent
// Manager (see those methods for the full invariant); direct field
// access from package callers is allowed for read-only rendering.
type Entry struct {
	Name       string
	State      State
	Started    time.Time
	Sender     Sender
	StopReason string
}

// ErrEmptyName is returned when Register/SetState/Get/Unregister/Dispatch
// is given an empty name. The cycle-231 contract treats every name as
// a user-visible identifier; an empty one is always a programming bug.
var ErrEmptyName = errors.New("subagentrouter: empty name")

// ErrAlreadyRegistered is returned when Register is given a name that
// the Manager has already seen. The cycle-231 contract is "name -> the
// single conversation a `@<name>` line reaches"; a duplicate name
// would alias two conversations under one identifier, which the
// reference binary cannot do. Callers that want to re-bind must
// Unregister first.
var ErrAlreadyRegistered = errors.New("subagentrouter: name already registered")

// ErrUnknownName is returned when Get / SetState / Unregister /
// Dispatch is given a name the Manager has never seen. Dispatch uses
// this as the prompt-layer's "no subagent named X is running"
// signal.
var ErrUnknownName = errors.New("subagentrouter: no subagent registered under that name")

// Manager is a thread-safe registry mapping names to Senders.
//
// The zero value is not usable; callers must construct one with
// NewManager. The same Manager instance is shared by the TUI run mode
// and the print path (cycle 232 will lift that promotion to a global
// for parity with `internal/tools.SharedSubagentManager`).
type Manager struct {
	mu      sync.RWMutex
	entries map[string]*Entry
}

// NewManager returns an empty Manager ready for Register calls.
func NewManager() *Manager {
	return &Manager{entries: map[string]*Entry{}}
}

// Register records sender under name. The State defaults to
// StateRunning and the Started timestamp is wall-clock `now`. Register
// returns the new Entry so the caller can update its State later.
//
// Two registrations under the same name reject — see
// ErrAlreadyRegistered. Re-binding is the caller's job.
func (m *Manager) Register(name string, sender Sender) (*Entry, error) {
	if name == "" {
		return nil, ErrEmptyName
	}
	if sender == nil {
		return nil, errors.New("subagentrouter: nil sender")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[name]; ok {
		return nil, ErrAlreadyRegistered
	}
	e := &Entry{
		Name:    name,
		State:   StateRunning,
		Started: time.Now(),
		Sender:  sender,
	}
	m.entries[name] = e
	return e, nil
}

// Unregister drops the entry under name. Returns ErrUnknownName when
// no such name is registered.
func (m *Manager) Unregister(name string) error {
	if name == "" {
		return ErrEmptyName
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[name]; !ok {
		return ErrUnknownName
	}
	delete(m.entries, name)
	return nil
}

// SetState replaces the State field on the entry. It is the wiring's
// way to mark a completed/failed subagent after the work settles, so
// the picker can keep listing it under "completed". Returns
// ErrUnknownName when no such name is registered.
func (m *Manager) SetState(name string, state State) error {
	if name == "" {
		return ErrEmptyName
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[name]
	if !ok {
		return ErrUnknownName
	}
	e.State = state
	return nil
}

// ReportStop records a Stop event for the named entry. The displayed
// StopReason is replaced only when the new reason differs from the
// existing one — an idempotent re-emission of the same reason is a
// no-op so a flapping subagent that re-stops with the same reason
// does not flicker the UI.
//
// An empty new reason clears the displayed stop reason (a fully
// resumed subagent may emit a Stop with no body), which is itself a
// "different reason" by definition. Returns ErrEmptyName when name
// is the empty string and ErrUnknownName when no entry is registered
// under name.
//
// Invariant (AGY 1.2.13, cycle
// subagent-stopped-clear-on-progress): once a Stop event has set a
// non-empty reason, subsequent ReportProgress calls leave the reason
// in place — only a NEW Stop event (this method) can change it.
func (m *Manager) ReportStop(name string, reason string) error {
	if name == "" {
		return ErrEmptyName
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[name]
	if !ok {
		return ErrUnknownName
	}
	if e.StopReason == reason {
		return nil
	}
	e.StopReason = reason
	return nil
}

// ReportProgress records a Progress event for the named entry. The
// displayed StopReason is NEVER touched here — that is the entire
// point of cycle subagent-stopped-clear-on-progress (AGY 1.2.13).
//
// Bug history: the prior implementation treated "new progress came
// in" as a signal to clear the previously displayed Stop reason.
// That left a subagent that briefly emitted progress while still in
// a stopped state showing a bare `Done` label with no body. The fix
// moves the responsibility to ReportStop: only a NEW Stop event
// replaces the displayed reason, progress is a no-op for it.
//
// A typical wiring layer calls ReportProgress on every text delta,
// tool batch, or heartbeat that the subagent's runtime emits while
// it is still running. Returns ErrEmptyName when name is the empty
// string and ErrUnknownName when no entry is registered under name.
func (m *Manager) ReportProgress(name string) error {
	if name == "" {
		return ErrEmptyName
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[name]
	if !ok {
		return ErrUnknownName
	}
	_ = e // StopReason deliberately left alone.
	return nil
}

// Get returns the entry registered under name. The second return
// value is false when no such entry exists.
func (m *Manager) Get(name string) (*Entry, bool) {
	if name == "" {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.entries[name]
	return e, ok
}

// Lookup is the case-insensitive counterpart of Get. It first
// attempts an exact match (the common case, including the names the
// prompt-input layer echoes back to the user) and falls back to a
// ASCII-lowercased linear scan.
//
// Lookup is the function `@<name>` actually calls: a user who types
// `@Explore-Bot` should still reach the `explore-bot` registration.
// Lookup returns the canonical name on success so the prompt layer
// can echo it consistently.
func (m *Manager) Lookup(name string) (*Entry, bool) {
	if name == "" {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.entries[name]; ok {
		return e, true
	}
	fold := strings.ToLower(name)
	for _, e := range m.entries {
		if strings.ToLower(e.Name) == fold {
			return e, true
		}
	}
	return nil, false
}

// Names returns every registered name, sorted in ASCII order so the
// picker's prefix-matching renders them deterministically.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.entries))
	for name := range m.entries {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// NamesWithPrefix returns the sorted Names whose ASCII-lowercased
// spelling starts with the ASCII-lowercased prefix. An empty prefix
// returns every name. The matching is purely textual; no fuzzy
// ranking. Autocomplete callers feed `Name` from the picker here.
func (m *Manager) NamesWithPrefix(prefix string) []string {
	all := m.Names()
	if prefix == "" {
		return all
	}
	fold := strings.ToLower(prefix)
	out := make([]string, 0, len(all))
	for _, n := range all {
		if strings.HasPrefix(strings.ToLower(n), fold) {
			out = append(out, n)
		}
	}
	return out
}

// List returns every Entry sorted by name. The slice is a defensive
// copy — concurrent mutations after List returns do not perturb the
// caller.
func (m *Manager) List() []*Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Entry, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Dispatch resolves name (case-insensitive) and delivers message to
// the registered Sender. The returned error wraps ErrUnknownName when
// the name is unknown; callers surface that as the prompt layer's
// "@<name> is not running" line.
func (m *Manager) Dispatch(ctx context.Context, name, message string) error {
	if name == "" {
		return ErrEmptyName
	}
	e, ok := m.Lookup(name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownName, name)
	}
	return e.Sender.Send(ctx, message)
}

// Count reports how many entries the Manager holds. Useful for tests
// and for the prompt-input layer's decision to render the `@`
// autocomplete at all.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}
