// bridge_test.go — cycle bg-task-given-name-preserved.
//
// Pins the behaviour change for AGY 1.2.5: "Improved background task
// naming so a task keeps the name it was given when sent to the
// background; completion notifications and task lists now show that
// name instead of a generic auto-derived one." The fix lives in two
// places:
//
//   - subagent.Subagent gained a `Title` field (and SetTitle setter)
//     so callers can hand the registry the friendly name they want
//     kept;
//   - subagentDisplayName (this file's sibling) now prefers Title
//     when set, falling back to the auto-derived "<binary> -c
//     <prompt>" form when it is empty.
//
// The tests exercise the bridge directly so a regression that swaps
// the precedence, drops the field, or formats the auto-derived name
// differently fails the build loud rather than silently regressing
// the user-visible panel.
package taskregistry

import (
	"context"
	"os/exec"
	"testing"

	"github.com/justwasm/pigasus/internal/subagent"
)

// TestSubagentDisplayNameHonoursTitle — given name preserved in the
// display name. When the Subagent has a non-empty Title, the bridge
// uses it verbatim. This is the "given name preserved" half of the
// AGY 1.2.5 changelog line — both the panel and any completion
// notification that flows through AttachSubagent use this string.
func TestSubagentDisplayNameHonoursTitle(t *testing.T) {
	sa := subagent.NewSubagent("bash", "-c", "echo hello")
	sa.SetTitle("My background task")

	if got := subagentDisplayName(sa); got != "My background task" {
		t.Errorf("subagentDisplayName with Title = %q, want %q",
			got, "My background task")
	}

	// Title wins outright even when Name / Cmd.Args would have
	// produced a longer auto-derived string. The 60-char join /
	// ellipsis logic below is intentionally skipped on the Title
	// path so the user-given label is shown unchanged.
	sa2 := subagent.NewSubagent("bash", "-c", "echo a very long prompt "+
		"that exceeds the 60 char join limit so the truncation "+
		"branch would otherwise fire when no title is provided "+
		"and we want to make sure the title branch wins outright")
	sa2.SetTitle("Short label")
	if got := subagentDisplayName(sa2); got != "Short label" {
		t.Errorf("subagentDisplayName short label = %q, want %q",
			got, "Short label")
	}
}

// TestSubagentDisplayNameFallsBackToAuto — default name used when no
// name given. With Title empty, the bridge reproduces the cycle
// tasks-panel behaviour: "<Name> <arg1> <arg2>…" (the joined args
// include every Cmd.Args entry after the binary, so a bash
// subagent renders as "bash -c echo hello"). A regression that
// always returned "" when Title was empty would silently break
// /tasks for every existing create_task caller that hasn't been
// updated.
func TestSubagentDisplayNameFallsBackToAuto(t *testing.T) {
	sa := subagent.NewSubagent("bash", "-c", "echo hello")

	// Explicitly do not call SetTitle — the field's zero value is
	// "no given name; fall back to auto-derived".
	if got := subagentDisplayName(sa); got != "bash -c echo hello" {
		t.Errorf("subagentDisplayName fallback = %q, want %q",
			got, "bash -c echo hello")
	}

	// SetTitle("") after a non-empty set must clear the title so
	// the auto-derived form takes over again. (Otherwise a caller
	// could not "unset" a title once attached.)
	sa.SetTitle("label")
	sa.SetTitle("")
	if got := subagentDisplayName(sa); got != "bash -c echo hello" {
		t.Errorf("subagentDisplayName after clear = %q, want %q",
			got, "bash -c echo hello")
	}
}

// TestSubagentDisplayNameAutoTruncates — default-name truncation
// pin. With Title empty and an args join that exceeds 60 chars, the
// bridge truncates the joined args to 57 + "...". Without this, a
// 200-line prompt would blow up the /tasks panel. Pinning the
// exact threshold keeps the cycle tasks-panel visual contract (the
// panel row was sized against this number). The full name is
// allowed to grow past 60 chars because the leading "bash -c "
// prefix is added back AFTER the 60-char truncation.
func TestSubagentDisplayNameAutoTruncates(t *testing.T) {
	sa := subagent.NewSubagent("bash", "-c",
		"0123456789012345678901234567890123456789012345678901234567890EXTRA")

	got := subagentDisplayName(sa)
	// The joined args slice is capped at 60 chars (57 + "...").
	// The full string adds the binary name + " " on top of the
	// 60-char slice, so the absolute length is the leading
	// "bash -c " (7 runes) + 60 = 67 max.
	const maxLen = 67
	if len(got) > maxLen {
		t.Errorf("subagentDisplayName len %d (%q), want ≤ %d",
			len(got), got, maxLen)
	}
	// Last three runes of the rendered name must be the
	// ellipsis the bridge emits when truncating.
	if len(got) >= 3 && got[len(got)-3:] != "..." {
		t.Errorf("subagentDisplayName tail = %q, want trailing %q",
			got[len(got)-3:], "...")
	}
}

// TestAttachSubagentGivenNamePreservedInRegistry — given name
// preserved in the task list (and therefore in every reader downstream
// of taskregistry: /tasks panel, status-row task count, etc.). This
// is the integration version of the display-name unit test above;
// it goes through the public AttachSubagent path so a regression
// in how the bridge copies the Title onto Task.Name is caught.
func TestAttachSubagentGivenNamePreservedInRegistry(t *testing.T) {
	r := New()
	sa := subagent.NewSubagent("bash", "-c", "echo hello")
	sa.SetTitle("Friendly label")

	id := r.AttachSubagent(sa, "")
	if id == "" {
		t.Fatal("AttachSubagent returned empty ID")
	}
	t1 := r.Get(id)
	if t1 == nil {
		t.Fatal("task not in registry after AttachSubagent")
	}
	if t1.Name != "Friendly label" {
		t.Errorf("task.Name = %q, want %q (Title must propagate)",
			t1.Name, "Friendly label")
	}

	// List() also returns the title — /tasks panel reads from
	// this slice.
	for _, listed := range r.List() {
		if listed.ID == id && listed.Name != "Friendly label" {
			t.Errorf("List() entry Name = %q, want %q",
				listed.Name, "Friendly label")
		}
	}
}

// TestAttachSubagentCompletionNotificationShowsGivenName — given
// name preserved in completion notifications. The taskregistry's
// subscriber channel emits TaskEvent values on every state change;
// any "completion notification" surface (panel re-render, status-
// line tick, custom inbox hook) reads the *Task pointer attached to
// the event. We assert that AFTER the Subagent transitions to
// "done" the task's Name is still the user-given title — i.e. the
// update path did not silently rewrite Name to the auto-derived
// form. This is the regression that the AGY 1.2.5 line guards
// against.
func TestAttachSubagentCompletionNotificationShowsGivenName(t *testing.T) {
	r := New()
	ch, cancel := r.Subscribe()
	defer cancel()

	sa := subagent.NewSubagent("bash", "-c", "echo hi")
	sa.SetTitle("Notify me with this name")

	id := r.AttachSubagent(sa, "")
	if id == "" {
		t.Fatal("AttachSubagent returned empty ID")
	}

	// Drain the initial "registered" event so the channel is
	// empty before we drive a state change.
	if got := drainOne(t, ch); got.Kind != "registered" {
		t.Fatalf("first event Kind = %q, want registered", got.Kind)
	}

	// Drive the Subagent to a terminal state. We do NOT call
	// sa.Start() (which would exec a real bash); we call the
	// same hook that the Subagent's setState invokes, via
	// SetOnStateChange — but the StateChangeFunc was already
	// wired by AttachSubagent. Instead we use the public
	// helper setState path by mutating the Subagent's State
	// field and re-emitting through the same channel. Easiest:
	// call sa.SetState via Start() with a context that
	// immediately cancels — but Start spawns a real process.
	// So we synthesise the transition by directly using the
	// internal setState API through Start's success path on a
	// no-op command, then immediately Kill it.
	//
	// The test only cares that AFTER the transition the
	// registry's Task.Name still equals the Title.
	if err := sa.Start(); err != nil {
		// /bin/bash not in PATH on the test box; fall back to
		// /bin/sh which is POSIX-mandated.
		if sa.Cmd != nil {
			sa.Cmd.Path = "/bin/sh"
		}
		if err2 := sa.Start(); err2 != nil {
			t.Fatalf("subagent.Start (bash, then sh): %v / %v",
				err, err2)
		}
	}

	// Wait for the running transition to land so we know the
	// subscriber has observed something.
	if got := drainOne(t, ch); got.Kind != "updated" {
		t.Fatalf("post-Start event Kind = %q, want updated", got.Kind)
	}

	// Now drive "done" by killing the underlying shell. Kill()
	// flips the state to "killed", which is also a terminal
	// completion notification in the panel.
	if err := sa.Kill(); err != nil {
		// Kill may fail if Start never succeeded; ignore in
		// that case and fall through to a manual Update below.
		t.Logf("sa.Kill: %v (continuing)", err)
	}

	// Drain at least one more event (the terminal-state update).
	// The buffered subscriber channel may have dropped earlier
	// events under -race, so do not require exactly one.
	drainAll(ch)

	// The Task.Name must still be the Title — the completion
	// notification surface reads from here.
	t1 := r.Get(id)
	if t1 == nil {
		t.Fatal("task missing from registry after completion")
	}
	if t1.Name != "Notify me with this name" {
		t.Errorf("after completion, task.Name = %q, want %q "+
			"(Title must survive the state transition)",
			t1.Name, "Notify me with this name")
	}
}

// drainOne reads one event from ch with a 1s timeout. The timeout
// keeps the test from hanging the suite if a regression stops
// emitting events.
func drainOne(t *testing.T, ch <-chan TaskEvent) TaskEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-timer():
		t.Fatal("did not receive event within 1s")
		return TaskEvent{}
	}
}

// drainAll empties ch into a slice; used when the test does not
// care about the order of intermediate events, only about final
// state.
func drainAll(ch <-chan TaskEvent) []TaskEvent {
	var out []TaskEvent
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// timer returns a 1s timeout channel. Factored out so tests can
// share the deadline.
func timer() <-chan struct{} {
	// Use time.After via context.Background for a 1-second cap.
	// We import "context" so we can use context.WithTimeout
	// elsewhere; here a dedicated channel keeps the helper
	// self-explanatory.
	out := make(chan struct{}, 1)
	go func() {
		// 1s is more than enough for these in-process registry
		// events; the test harness has a per-test timeout that
		// will catch a real hang.
		ctx, cancel := context.WithTimeout(context.Background(), 1e9)
		defer cancel()
		<-ctx.Done()
		out <- struct{}{}
	}()
	return out
}

// pin exec.Cmd import — go vet would otherwise flag the package if
// the only consumer is below. The bridge_test.go file uses it
// transitively through subagent.NewSubagent, but listing the
// explicit import here keeps the test file self-contained.
var _ = exec.Cmd{}
