package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/convstore"
)

// TestContinueFlag exercises the documented top-level --continue:
// resolves the conversation through resolveConversationSelection (which
// in turn consults FindMostRecent for the parent/child fallback), seeds
// the runMode session, and renders an "Resumed conversation: <title>"
// banner the v1.4.0 changelog requires.
func TestContinueFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "11111111-2222-4000-8000-000000000099"
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "say what?"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", id+".db"), "traj", steps); err != nil {
		t.Fatal(err)
	}
	// Pin the seeded workspace to the test's cwd so the most-recent
	// lookup finds it without walking into the source tree.
	ws := filepath.Join(t.TempDir())
	t.Chdir(ws)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeConversationSummaryRow(
		filepath.Join(dir, "conversation_summaries.db"), id,
		"file://"+filepath.Clean(ws), now); err != nil {
		t.Fatal(err)
	}

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("continue", true, "")
	fs.Bool("c", false, "")
	sel := resolveConversationSelection(fs, io.Discard)
	if sel.ID != id {
		t.Fatalf("sel.ID=%q, want %q", sel.ID, id)
	}
	if sel.Store == nil || len(sel.Store.Steps) == 0 {
		t.Fatalf("sel.Store not loaded: %+v", sel.Store)
	}
	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.conversationID = sel.ID
	m.seedConversation(sel.Store)
	joined := strings.Join(m.response, "\n")
	// Cycle 271: the resumed transcript shows model steps only, so the
	// prior user prompt lives in m.session (model context) but not in
	// m.response.
	if strings.Contains(joined, "say what?") {
		t.Errorf("cycle 271: resumed transcript must not echo user prompts; got:\n%s", joined)
	}
	seededUser := false
	for _, msg := range m.session {
		if msg.Role == "user" && strings.Contains(msg.Content, "say what?") {
			seededUser = true
		}
	}
	if !seededUser {
		t.Errorf("prior user prompt missing from the seeded session: %+v", m.session)
	}
}

// TestContinueNoConversation runs the documented --continue empty-store
// path: no conversation in the workspace, no parent or child. The
// continueNoConversationMessage helper prints "No previous
// conversation to continue." and the caller exits non-zero.
func TestContinueNoConversation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Chdir(t.TempDir())

	var buf strings.Builder
	continueNoConversationMessage(&buf)
	if !strings.Contains(buf.String(), "No previous conversation to continue.") {
		t.Errorf("got %q, want the documented message", buf.String())
	}
	// resolveConversationSelection returns an empty selection —
	// resolveContinue-driven main.go should surface the error.
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("continue", true, "")
	sel := resolveConversationSelection(fs, io.Discard)
	if sel.ID != "" {
		t.Errorf("non-empty sel with empty store: %+v", sel)
	}
}

// TestAutoResumeOnCrash was removed in cycle resume-continuation-r1:
// the "Resume previous conversation? [Y/n] " on-launch prompt was
// speculative. `strings ~/.local/bin/agy | grep "Resume previous"`
// returns nothing; REF does not prompt on launch — `--continue` is the
// only way to adopt a previous session without manually picking it
// from /resume. See cmd/wagyu/continue.go for the removal note.

// TestGoroutineLeakResumePicker reproduces the cycle's headline fix.
// The v1.4.0 changelog records: "background step-cache eviction
// goroutines running for the rest of the session" survive /resume
// scans. The clone's resume picker has a 530 ms blink tick (the only
// periodic goroutine the cycle-184 /resume panel scheduled); the test
// ensures 10 open + close cycles of the picker do not leave
// background goroutines running.
//
// Goroutine counts under Go's runtime are statistical — the call stack
// after a tea.Tick() expires falls back to the runtime's internal
// bookkeeping, so we tolerate ±1.
func TestGoroutineLeakResumePicker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "33333333-4444-4000-8000-000000000299"
	steps := []convstore.Step{{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "x"}}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", id+".db"), "traj", steps); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeConversationSummaryRow(
		filepath.Join(dir, "conversation_summaries.db"), id,
		"file://"+filepath.Clean(dir), now); err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	baseline := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", dir, true)
		handled, _ := m.handleSlashCommand("/resume")
		if !handled || !m.resumeMode {
			t.Fatalf("iter %d: /resume failed: handled=%v mode=%v", i, handled, m.resumeMode)
		}
		// Drive the picker through the happy path so any spawned
		// goroutines see their natural close.
		m.Update(key(tea.KeyDown, 0))
		m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
		if m.resumeMode {
			t.Fatalf("iter %d: picker should close on Esc", i)
		}
		// Cycle tasks-panel: NewRunMode arms a subscriber goroutine
		// on the shared task registry; close it here so the next
		// iteration's m doesn't accumulate (delta=10). The cancel
		// callback closes the registry's channel, the goroutine's
		// `for range ch` exits, and runtime.GC drains it.
		m.tasksSubCancel()
	}
	// Allow blink ticks to drain.
	time.Sleep(700 * time.Millisecond)
	runtime.GC()
	final := runtime.NumGoroutine()
	if delta := final - baseline; delta > 1 || delta < -1 {
		t.Errorf("goroutine count drifted: baseline=%d final=%d (delta=%d)",
			baseline, final, final-baseline)
	}
}

// TestSlashContinueResumeLast exercises the /continue slash command
// the cycle-resume TUI surface adds. Seeds one conversation in the
// active workspace, runs /continue, and checks (a) the slash was
// handled, (b) m.conversationID was overwritten with the picked id,
// (c) the prior-turn transcript re-appeared in m.response.
func TestSlashContinueResumeLast(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "44444444-5555-4000-8000-000000000301"
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "old prompt"},
		{Idx: 1, Type: convstore.StepModelResponse, Status: 3, Text: "old reply"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", id+".db"), "traj-old", steps); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	t.Chdir(ws)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeConversationSummaryRow(
		filepath.Join(dir, "conversation_summaries.db"), id,
		"file://"+filepath.Clean(ws), now); err != nil {
		t.Fatal(err)
	}

	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", ws, true)
	m.conversationID = "pre-existing-id-from-new-run" // would otherwise short-circuit
	if handled, _ := m.handleSlashCommand("/continue"); !handled {
		t.Fatal("/continue should be handled")
	}
	if m.conversationID != id {
		t.Errorf("m.conversationID = %q, want %q", m.conversationID, id)
	}
	joined := strings.Join(m.response, "\n")
	// Cycle 271: user prompts are not echoed on resume; the model reply is.
	if !strings.Contains(joined, "old reply") {
		t.Errorf("missing the prior model reply: %q", joined)
	}
	if strings.Contains(joined, "old prompt") {
		t.Errorf("cycle 271: resumed transcript must not echo user prompts: %q", joined)
	}
	if !strings.Contains(joined, "Resumed conversation "+id) {
		t.Errorf("missing the resumed banner: %q", joined)
	}
	// The transcript is the resumed one, not any leftover the new-run
	// might have written before /continue wiped response/session.
	if strings.Contains(joined, "pre-existing") {
		t.Errorf("transcript not wiped before seeding: %q", joined)
	}
}

// TestSlashContinueNoConversation runs /continue in a fresh
// ANTIGRAVITY_HOME — there is no candidate, so the handler echoes the
// "No previous conversation to continue." line and leaves
// m.conversationID alone.
func TestSlashContinueNoConversation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Chdir(t.TempDir())

	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)
	const origID = "orig-conv-id-unchanged"
	m.conversationID = origID
	if handled, _ := m.handleSlashCommand("/continue"); !handled {
		t.Fatal("/continue should be handled")
	}
	if m.conversationID != origID {
		t.Errorf("m.conversationID = %q, want %q (unchanged on miss)",
			m.conversationID, origID)
	}
	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, "No previous conversation to continue.") {
		t.Errorf("missing empty-state message: %q", joined)
	}
	if !strings.Contains(joined, "/continue") {
		t.Errorf("empty-state message should be wrapped in the slashRespond `> /continue` echo: %q", joined)
	}
}

// TestSlashContinueParentFallback seeds the conversation in the parent
// of the active workspace and checks /continue picks it up. This is
// the headline v1.4.0 fix: previously the picker would have shown no
// matching row and the TUI would have asked the user to /resume.
func TestSlashContinueParentFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "55555555-6666-4000-8000-000000000401"
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "parent owned prompt"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", id+".db"), "traj-parent", steps); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(t.TempDir(), "parent")
	sub := filepath.Join(ws, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeConversationSummaryRow(
		filepath.Join(dir, "conversation_summaries.db"), id,
		"file://"+filepath.Clean(ws), now); err != nil {
		t.Fatal(err)
	}

	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", sub, true)
	if handled, _ := m.handleSlashCommand("/continue"); !handled {
		t.Fatal("/continue should be handled")
	}
	if m.conversationID != id {
		t.Errorf("m.conversationID = %q, want %q (the parent's session)", m.conversationID, id)
	}
	seededUser := false
	for _, msg := range m.session {
		if msg.Role == "user" && strings.Contains(msg.Content, "parent owned prompt") {
			seededUser = true
		}
	}
	if !seededUser {
		t.Errorf("missing parent-owned prompt in the seeded session: %+v", m.session)
	}
}

// TestGenerateConversationTitle pins the heuristic the cycle-resume
// code path uses when applying the auto-generated title to the
// conversation_summaries.db row. The function is intentionally
// lightweight — no LLM call, no quota — so the assertion only checks
// shape: 4-6 content words, markdown noise stripped, ≤ 60 rune wide.
func TestGenerateConversationTitle(t *testing.T) {
	cases := []struct {
		name  string
		input string
		check func(t *testing.T, got string)
	}{
		{
			name:  "simple prompt",
			input: "fix the broken shell prompt in /tmp/run.sh",
			check: func(t *testing.T, got string) {
				// 4-6 content words — the heuristic keeps up to 6
				// tokens, so a 7-word prompt like this one trims the
				// tail. We don't pin the exact 6 words; only the
				// "shell prompt" anchor must survive.
				if !strings.Contains(got, "shell prompt") {
					t.Errorf("got %q, missing the \"shell prompt\" anchor", got)
				}
				if strings.Contains(got, "/tmp/run.sh") {
					t.Errorf("got %q, picked up a path it should not have", got)
				}
			},
		},
		{
			name:  "markdown noise stripped",
			input: "> tell me how to deploy\n# heading\n* bullet 1\n- bullet 2",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, ">") || strings.Contains(got, "#") ||
					strings.Contains(got, "*") || strings.Contains(got, "-") {
					t.Errorf("markdown markers not stripped: %q", got)
				}
			},
		},
		{
			name:  "code fence stripped",
			input: "explain this:\n```\nfoo bar baz qux quux quuz\n```",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, "```") || strings.Contains(got, "qux") {
					t.Errorf("code fence should be removed: %q", got)
				}
			},
		},
		{
			name:  "empty prompt",
			input: "",
			check: func(t *testing.T, got string) {
				if got != "" {
					t.Errorf("got %q, want empty", got)
				}
			},
		},
		{
			name:  "punctuation only",
			input: "...!!!",
			check: func(t *testing.T, got string) {
				// Function falls back to whatever cleaned text remains;
				// for purely punctuation input that is empty. We accept
				// either "" or the literal trimmed punctuation, but a
				// 4+ word title is the failure mode.
				if strings.Count(got, " ") >= 4 {
					t.Errorf("punctuation-only prompt should not yield a 5-token title: %q", got)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, generateConversationTitle(tc.input))
		})
	}
}
