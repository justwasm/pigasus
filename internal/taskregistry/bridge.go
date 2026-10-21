// Package taskregistry — bridge.go.
//
// This file is the only place that knows how to translate a
// internal/subagent.Subagent state machine into a taskregistry.Task.
// The translation lives here (not in internal/subagent) so the
// subagent package stays zero-dep — the upstream library is imported
// by internal/tools and other low-level callers that may not want
// taskregistry in their import graph.
//
// Two helpers are exported:
//
//   - AttachSubagent(s *subagent.Subagent, owner string) — registers
//     s under a freshly-minted task ID and wires SetOnStateChange so
//     every state transition updates the registry. The subagent's
//     existing ID is preserved on the Task.ID field so subscribers
//     can grep logs by either ID.
//   - KillByID(id string) — convenience wrapper for the /tasks
//     panel: looks up the Subagent registered for id, calls Kill,
//     and returns whether anything happened.
//
// Both helpers take the registry as the first argument so a future
// multi-registry caller (per-session isolation) can compose without
// re-plumbing every callsite.
package taskregistry

import (
	"github.com/justwasm/pigasus/internal/subagent"
)

// subagentKind is the taskregistry.Kind a Subagent gets registered as.
// /tasks renders a "shell" badge for these — see KindShell.
const subagentKind = KindShell

// AttachSubagent registers s in r and wires SetOnStateChange so every
// transition s makes is reflected in the registry. The owner string
// is stored on Task.OwnerID so a future multi-session build can scope
// /tasks to the active conversation. Pass "" when no owner applies.
//
// AttachSubagent is the only entry point the bash wrapper in
// internal/tools/stubs.go needs — create_task calls it once per
// spawn, then runs Start / Wait as before.
//
// The returned ID is the task's ID, which equals the subagent's ID
// (we preserve the upstream ID format so a grep for "sub-…" still
// finds the right line in the logs).
func (r *Registry) AttachSubagent(s *subagent.Subagent, owner string) string {
	if s == nil {
		return ""
	}
	t := &Task{
		ID:      s.ID,
		Name:    subagentDisplayName(s),
		Kind:    subagentKind,
		State:   StatePending,
		OwnerID: owner,
	}
	id := r.Register(t)
	if id == "" {
		return ""
	}
	// Move the subagent out of pending and into running the
	// moment we observe Start. We don't fire this directly —
	// subagent.Start() already calls setState("running"), which
	// fires our hook below. We only Register here.
	s.SetOnStateChange(func(sa *subagent.Subagent, newState string) {
		r.Update(id, func(t *Task) {
			switch State(newState) {
			case StateRunning:
				t.State = StateRunning
				t.StartedAt = nowFn()
			case StateDone:
				t.State = StateDone
				t.ExitCode = 0
				t.EndedAt = nowFn()
			case StateFailed:
				t.State = StateFailed
				t.ExitCode = 1
				t.EndedAt = nowFn()
				t.Err = sa.Err
			case StateKilled:
				t.State = StateKilled
				t.ExitCode = 137 // SIGKILL convention
				t.EndedAt = nowFn()
			}
		})
	})
	return id
}

// KillByID looks up the Subagent attached to the task with the given
// ID and calls Kill on it. It is the path the /tasks panel takes when
// the user presses Enter (or k) on a running row.
//
// The lookup is best-effort: a Subagent that is no longer in any
// Manager (because the caller did not keep a reference) cannot be
// killed from here, and we return false to signal "nothing
// happened". The panel re-renders regardless; a true return value
// is what the panel uses to bump the cursor or close the view.
func KillByID(r *Registry, id string, m *subagent.Manager) bool {
	if r == nil || id == "" {
		return false
	}
	t := r.Get(id)
	if t == nil {
		return false
	}
	if t.State != StateRunning && t.State != StatePending {
		// Already terminal — pressing Enter is a no-op (no
		// fake exit code, matching the 1.4.0 changelog line
		// "shown as `completed` or `failed` without a made-up
		// exit code").
		return false
	}
	if m == nil {
		return false
	}
	sa, ok := m.Get(t.ID)
	if !ok || sa == nil {
		return false
	}
	_ = sa.Kill()
	return true
}

// subagentDisplayName picks a friendlier Name than the binary path
// for tasks listed in /tasks. When the caller set a human-given
// Title on the Subagent (create_task's `title` parameter) that wins
// outright so the task keeps the friendly name it was given instead
// of being renamed to an auto-derived "<binary> -c <prompt>"
// string. Otherwise bash wrappers get "bash -c <cmd>" with the
// command truncated to 60 chars so a 200-line install script does
// not blow up the panel width.
func subagentDisplayName(s *subagent.Subagent) string {
	if s == nil {
		return ""
	}
	// User-given title wins outright — preserves "the name it was
	// given when sent to the background" (cycle: bg-task-given-name-
	// preserved / AGY 1.2.5 changelog). Returned as-is; trimming /
	// joining below only applies to the auto-derived fallback.
	if s.Title != "" {
		return s.Title
	}
	name := s.Name
	if name == "" && s.Cmd != nil && len(s.Cmd.Args) > 0 {
		// exec.Cmd.Path is the absolute binary path (e.g.
		// "/bin/bash"). Strip to the basename so the panel
		// renders "bash -c …" instead of "/bin/bash -c …".
		name = baseName(s.Cmd.Path)
	}
	if s.Cmd != nil && len(s.Cmd.Args) > 1 {
		// Show "bash -c <command>" so the user can tell two
		// concurrent shells apart.
		args := s.Cmd.Args[1:]
		joined := ""
		for _, a := range args {
			if joined != "" {
				joined += " "
			}
			joined += a
		}
		if len(joined) > 60 {
			joined = joined[:57] + "..."
		}
		return name + " " + joined
	}
	return name
}

// baseName is filepath.Base without dragging in the path package.
// We need only the trailing path component — import cycle pressure
// (path/filepath pulls runtime-internal flags we want to avoid) makes
// the 4-line loop below cheaper than a full import.
func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
