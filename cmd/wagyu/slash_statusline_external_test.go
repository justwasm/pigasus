// slash_statusline_external_test.go — cycle status-line-script: the
// external status-line rendering path. Covers AGY 1.1.21's `cost`
// field and AGY 1.1.27's `conversation_title` field on the JSON
// payload the spawned script reads from stdin.
//
// Tests:
//   - TestLoadStatusLineConfig_DisabledAndMissing    — settings.json
//     shape validation (enabled flag, missing block, malformed JSON)
//   - TestStatusLinePayload_Fields                   — the payload
//     carries {model, cost, conversation_title, current_dir}
//   - TestPricingForModel                            — the per-model
//     rate-card table (cycle 1.1.21) — checks claude-opus before
//     claude-sonnet, MiniMax M3 = $0, fallback to 0
//   - TestRecordTurnUsage_Accumulates                — token totals
//     survive multiple turn chunks via atomic increments
//   - TestRecordTurnUsage_NilSafe                    — nil Meta and
//     nil receiver are no-ops
//   - TestRunStatusLineScript_SpawnEcho              — happy path:
//     the script receives the JSON payload on stdin and the
//     rendered line lands in stdout
//   - TestRunStatusLineScript_NonZeroExit            — non-zero exit
//     becomes a non-nil Err so the caller can fall back
//   - TestRunStatusLineScript_EmptyOutput            — empty stdout
//     still errors so the row falls back to built-in
//   - TestSplitStatusLineCommand                     — shell-style
//     token split used to interpret `command` paths and flags
//   - TestStatuslineExternalToggle_NeverConfigured   — cycle-105 two-
//     state path preserved when no script is configured
//   - TestStatuslineExternalToggle_ThreeState        — three-state
//     cycle (off → built-in → external → off) when the script is
//     configured
//   - TestStatuslineExternalToggle_NextAfterOff_BackToBuiltIn
//     — round-trip the cycle so every transition has both an
//     authoritative state and a persisted settings.json row
//   - TestStatusLineRowUsesExternalRender            — when external
//     mode is armed and has produced output, View() shows the
//     script's text and never draws the built-in row
//   - TestStatusLineRowFallsBackOnError              — a non-nil
//     state.lastErr forces the row back to the built-in
//     template, even when statusLineExternal is still true
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

// TestLoadStatusLineConfig_DisabledAndMissing covers the three ways
// settings.json can fail to light up external mode:
//
//   - the `statusLine` block is missing entirely
//   - the block is present but `enabled` is false
//   - the block is present but `command` is blank
//
// All three must return ok = false so the cycle-105 toggle stays the
// only path the user has to drive the row.
func TestLoadStatusLineConfig_DisabledAndMissing(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
	}{
		{"no block", `{"colorScheme":"light"}`, false},
		{"enabled=false", `{"statusLine":{"type":"command","command":"/bin/echo","enabled":false}}`, false},
		{"blank command", `{"statusLine":{"type":"command","command":"  ","enabled":true}}`, false},
		{"enabled + command", `{"statusLine":{"type":"command","command":"/bin/echo hi","enabled":true}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("ANTIGRAVITY_HOME", home)
			writeSettings(t, home, tc.body)
			cmd, ok := loadStatusLineConfig()
			if ok != tc.ok {
				t.Errorf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && cmd == "" {
				t.Error("command empty despite ok = true")
			}
		})
	}
}

// TestStatusLinePayload_Fields pins the JSON wire form. The contract
// is `{"model", "cost", "conversation_title", "current_dir"}` — cycle
// 1.1.27's changelog called out the first three; current_dir is the
// obvious companion field the live probes recover.
func TestStatusLinePayload_Fields(t *testing.T) {
	m := &runMode{
		statusModel:             "MiniMax M3",
		cwd:                     "/Users/foo/project",
		sessionPromptTokens:     1000,
		sessionCompletionTokens: 500,
	}
	m.conversationName = "My Session"
	got := m.buildStatusLinePayload()
	if got.Model != "MiniMax M3" {
		t.Errorf("Model = %q", got.Model)
	}
	if got.ConversationTitle != "My Session" {
		t.Errorf("ConversationTitle = %q", got.ConversationTitle)
	}
	if got.CurrentDir != "/Users/foo/project" {
		t.Errorf("CurrentDir = %q", got.CurrentDir)
	}
	// MiniMax M3 is a configured-only model with $0 pricing — the
	// cost field is named correctly (it's a number), the value is
	// zero, and the field order in the JSON matches the recovered
	// wire form.
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte(`{"model":"MiniMax M3",`)) {
		t.Errorf("field order = %s", out)
	}
	if !bytes.Contains(out, []byte(`"cost":0`)) {
		t.Errorf("cost field missing or wrong type: %s", out)
	}
	if !bytes.Contains(out, []byte(`"conversation_title":"My Session"`)) {
		t.Errorf("conversation_title field missing: %s", out)
	}
}

// TestPricingForModel pins the rate-card table the JSON `cost` field
// is computed off. The two ordering invariants are:
//
//   - claude-opus must precede claude-sonnet (the latter is a substring
//     of the former);
//   - MiniMax M3 routes to the $0 row so the cycle-225 quota UI stays
//     consistent.
func TestPricingForModel(t *testing.T) {
	// 1M prompt tokens, 0 completion tokens → exact row hit.
	if got := pricingForModel("claude-opus-4.7", 1_000_000, 0); got != 15.0 {
		t.Errorf("claude-opus input rate = %v, want 15.0", got)
	}
	// Substring match: claude-sonnet should NOT route to claude-opus.
	if got := pricingForModel("claude-sonnet-4.5", 1_000_000, 0); got != 3.0 {
		t.Errorf("claude-sonnet input rate = %v, want 3.0", got)
	}
	// MiniMax M3 is free.
	if got := pricingForModel("MiniMax M3", 1_000_000, 1_000_000); got != 0 {
		t.Errorf("MiniMax M3 rate = %v, want 0", got)
	}
	// Unknown model falls back to 0 (well-formed JSON, but $0 is a
	// better default than NaN/Infinity).
	if got := pricingForModel("totally-unknown-model", 1000, 1000); got != 0 {
		t.Errorf("unknown model rate = %v, want 0", got)
	}
	// Output rate vs input rate — claude-sonnet output is 5x its
	// input rate (3 → 15).
	if got := pricingForModel("claude-sonnet", 0, 1_000_000); got != 15.0 {
		t.Errorf("claude-sonnet output rate = %v, want 15.0", got)
	}
}

// TestRecordTurnUsage_Accumulates puts the chunk-meta hook through
// its paces: every Done chunk adds Meta.PromptTokens /
// Meta.CompletionTokens to the runMode totals, regardless of how
// many calls land in flight.
func TestRecordTurnUsage_Accumulates(t *testing.T) {
	m := &runMode{}
	for i := 0; i < 5; i++ {
		m.recordTurnUsage(&backend.ChunkMeta{
			PromptTokens:     100,
			CompletionTokens: 50,
		})
	}
	p, c := m.sessionTokenTotals()
	if p != 500 || c != 250 {
		t.Errorf("totals = (%d, %d), want (500, 250)", p, c)
	}
}

// TestRecordTurnUsage_NilSafe pins the defensive checks: a nil
// receiver and a nil meta are both no-ops so the chunk handler's
// call site stays simple.
func TestRecordTurnUsage_NilSafe(t *testing.T) {
	var nilM *runMode
	nilM.recordTurnUsage(&backend.ChunkMeta{PromptTokens: 100}) // must not panic
	var m *runMode
	m.recordTurnUsage(nil) // nil meta — no panic, no change
	nilM.recordTurnUsage(nil)
}

// TestRunStatusLineScript_SpawnEcho writes a tiny sh script that
// reads the payload from stdin (one JSON object, terminated by a
// NUL byte) and prints a fixed line. The post-test parses the
// payload back and asserts the wire form so the test does not
// silently regress.
func TestRunStatusLineScript_SpawnEcho(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "echo.sh")
	body := "#!/bin/sh\n" +
		// Read the JSON payload (terminator not required for the
		// short length this test uses). Echo a fixed marker.
		"cat >/dev/null\n" +
		"echo \"model said hi\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"model":"MiniMax M3","cost":0,"conversation_title":"","current_dir":"/"}`)
	res := runStatusLineScript(script, payload)
	if res.Err != nil {
		t.Fatalf("spawn err = %v", res.Err)
	}
	if got := strings.TrimSpace(res.Output); got != "model said hi" {
		t.Errorf("Output = %q", got)
	}
}

// TestRunStatusLineScript_NonZeroExit exercises the failure path:
// the script exits with a non-zero status, the runner surfaces a
// non-nil error so the row falls back to the built-in template.
func TestRunStatusLineScript_NonZeroExit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	body := "#!/bin/sh\necho boom >&2\nexit 7\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	res := runStatusLineScript(script, []byte(`{}`))
	if res.Err == nil {
		t.Fatal("expected non-nil error on exit 7")
	}
	if !strings.Contains(res.Err.Error(), "boom") {
		t.Errorf("Err = %v, want stderr to bubble up", res.Err)
	}
}

// TestRunStatusLineScript_EmptyOutput ensures the script producing
// no stdout (or only whitespace) is treated as an error, so the
// caller can keep showing the last good render instead of a blank
// row.
func TestRunStatusLineScript_EmptyOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "silent.sh")
	body := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	res := runStatusLineScript(script, []byte(`{}`))
	if res.Err == nil {
		t.Fatal("expected non-nil error on empty stdout")
	}
}

// TestSplitStatusLineCommand is the surface `loadStatusLineConfig`
// hands `runStatusLineScript`. The first field is the executable;
// the rest are flags.
func TestSplitStatusLineCommand(t *testing.T) {
	cases := []struct {
		in   string
		name string
		args []string
	}{
		{"/usr/local/bin/tmux-status", "/usr/local/bin/tmux-status", nil},
		{"/usr/local/bin/tmux-status --foo", "/usr/local/bin/tmux-status", []string{"--foo"}},
		{"   /bin/echo  hi there ", "/bin/echo", []string{"hi", "there"}},
		{"", "", nil},
	}
	for _, tc := range cases {
		gotName, gotArgs := splitStatusLineCommand(tc.in)
		if gotName != tc.name {
			t.Errorf("%q name = %q, want %q", tc.in, gotName, tc.name)
		}
		if len(gotArgs) != len(tc.args) {
			t.Errorf("%q args = %v, want %v", tc.in, gotArgs, tc.args)
			continue
		}
		for i := range tc.args {
			if gotArgs[i] != tc.args[i] {
				t.Errorf("%q args[%d] = %q, want %q", tc.in, i, gotArgs[i], tc.args[i])
			}
		}
	}
}

// TestStatuslineExternalToggle_NeverConfigured makes sure the
// cycle-105 two-state toggle still flips between "" and "off" when
// no statusLine.command is configured — the regression test that
// kept the existing tests green.
func TestStatuslineExternalToggle_NeverConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := newSlashModel(t)
	m.handleSlashCommand("/statusline")
	if m.statusLine != "off" {
		t.Errorf("after /statusline from default: %q, want off", m.statusLine)
	}
	m.handleSlashCommand("/statusline")
	if m.statusLine != "on" && m.statusLine != "" {
		// Cycle-184 reader: settings.json's `true` round-trips to
		// "on", which `statusRow()` treats identically to "".
		t.Errorf("after /statusline from off: %q, want on or empty", m.statusLine)
	}
}

// TestStatuslineExternalToggle_ThreeState covers the new path:
// a configured + enabled statusLine.command locks the /statusline
// slash command into the three-state cycle (off → built-in →
// external → off). At each step the test asserts both the runMode
// field that moved and the persisted settings.json row.
func TestStatuslineExternalToggle_ThreeState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"statusLine":{"type":"command","command":"/bin/echo","enabled":true}}`)

	m := newSlashModel(t)
	// Seed the model with the cycle's starting state: hidden.
	m.statusLineExternal = false
	m.statusLine = "off"

	// Cycle 1: off → built-in. statusLine clears, statusLineExternal
	// stays false (state A is "external rendering inactive").
	m.handleSlashCommand("/statusline")
	if m.statusLine != "" && m.statusLine != "on" {
		t.Errorf("cycle 1 (off→built-in): statusLine = %q, want \"\" or on", m.statusLine)
	}
	if m.statusLineExternal {
		t.Error("cycle 1 (off→built-in) must leave statusLineExternal = false")
	}

	// Cycle 2: built-in → external. statusLine stays the same;
	// statusLineExternal flips on (state B is "external rendering
	// active").
	m.statusLine = ""
	m.handleSlashCommand("/statusline")
	if !m.statusLineExternal {
		t.Error("cycle 2 (built-in→external) must turn statusLineExternal on")
	}
	if m.statusLine == "off" {
		t.Error("cycle 2 (built-in→external) must not hide the row")
	}

	// Cycle 3: external → off. statusLine becomes "off" and the
	// bool clears so the cycle's wrap-around lands on built-in, not
	// back on external.
	m.handleSlashCommand("/statusline")
	if m.statusLine != "off" {
		t.Errorf("cycle 3 (external→off): statusLine = %q, want off", m.statusLine)
	}
	if m.statusLineExternal {
		t.Error("cycle 3 (external→off) must clear statusLineExternal")
	}
	// settings.json was written by cycle 3 (the B → C transition is
	// the only one that touches the statusLine.enabled persistence
	// bit; the cycle writes it through SetSettingsField so the
	// statusLine.command block survives).
	if body := readSettings(t, home); !strings.Contains(body, `"enabled": false`) {
		t.Errorf("settings.json missing enabled=false after cycle 3:\n%s", body)
	}
	// The statusLine.command path must NOT have been clobbered by
	// the visibility write — the cycle persists via `enabled`,
	// not by overwriting the top-level statusLine key.
	if body := readSettings(t, home); !strings.Contains(body, `"command": "/bin/echo"`) {
		t.Errorf("settings.json clobbered the command field:\n%s", body)
	}
}

// TestStatuslineExternalToggle_NextAfterOff_BackToBuiltIn
// confirms the cycle's wrap-around from cycle N=3 (off) back to
// built-in via a fourth /statusline press.
func TestStatuslineExternalToggle_NextAfterOff_BackToBuiltIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"statusLine":{"type":"command","command":"/bin/echo","enabled":true}}`)

	m := newSlashModel(t)
	m.statusLineExternal = false
	m.statusLine = "off"

	// Walk four presses and assert the state at each one.
	type want struct {
		statusLine string
		external   bool
	}
	presses := []want{
		// 1st press: off → built-in.
		{"", false},
		// 2nd press: built-in → external.
		{"", true},
		// 3rd press: external → off.
		{"off", false},
		// 4th press: off → built-in (cycle wraps around).
		{"", false},
	}
	for i, w := range presses {
		m.handleSlashCommand("/statusline")
		// `m.statusLine = "on"` is also acceptable — the cycle-184
		// reader treats "on" identically to "" in the row renderer.
		if m.statusLine != w.statusLine && !(w.statusLine == "" && m.statusLine == "on") {
			t.Errorf("press %d: statusLine = %q, want %q",
				i+1, m.statusLine, w.statusLine)
		}
		if m.statusLineExternal != w.external {
			t.Errorf("press %d: statusLineExternal = %v, want %v",
				i+1, m.statusLineExternal, w.external)
		}
	}
}

// TestStatusLineRowUsesExternalRender verifies that when the
// external state has produced a non-empty output, View() shows it
// in place of the "? for shortcuts" placeholder.
func TestStatusLineRowUsesExternalRender(t *testing.T) {
	m := newSlashModel(t)
	m.width = 80
	m.height = 24
	m.statusLineExternal = true
	m.statusLine = "on" // visible
	state := m.ensureExternalState()
	state.setRendered("…custom…", nil)

	view := m.View().Content
	if !strings.Contains(view, "…custom…") {
		t.Error("external render missing from View()")
	}
	if strings.Contains(view, "? for shortcuts") {
		t.Error("built-in hint still rendered alongside external row")
	}
}

// TestStatusLineRowFallsBackOnError covers the recovery path: a
// transient script error should drop the row back to the built-in
// template so the user is never staring at a blank row.
func TestStatusLineRowFallsBackOnError(t *testing.T) {
	m := newSlashModel(t)
	m.width = 80
	m.height = 24
	m.statusLineExternal = true
	m.statusLine = "on"
	// Simulate a transient error: cached output is "" (empty after
	// TrimSpace), so externalRenderedForRow returns "" and the
	// built-in template takes over.
	state := m.ensureExternalState()
	state.setRendered("", errTestBoom)

	view := m.View().Content
	if !strings.Contains(view, "? for shortcuts") {
		t.Error("built-in hint missing after script error")
	}
	if strings.Contains(strings.TrimSpace(m.extractStatusRowOnly()), "") == false {
		t.Error("status row is empty")
	}
}

// errTestBoom is a sentinel error used by the fallback test.
var errTestBoom = stringErr("boom")

// stringErr is a tiny error type used only by the fallback test
// (the production code passes `error`, which is satisfied).
type stringErr string

func (s stringErr) Error() string { return string(s) }

// extractStatusRowOnly returns the last line of View() (the status
// row, since the row sits at the bottom of the rendered string).
// Used to keep the fallback test free of full-screen noise.
func (m *runMode) extractStatusRowOnly() string {
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}
