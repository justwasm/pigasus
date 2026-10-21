package main

// shift_tab_test.go — cycle 254: AGY 1.3.5's headline feature is "Shift+Tab
// cycles modes", and the cycle this clone ships is pinned here as
// Build → Default → Auto → Plan → Build. The earlier cycle (104) put
// default first; cycle 254 reorders so a fresh Shift+Tab from
// accept-edits (Build) reaches Default, and wrapping back lands on
// Build again. See the deliverable's 硬約束 section for the
// rationale.

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/permissions"
)

var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plainRow strips SGR so an assertion can look for the visible text of a
// styled row (the status row interleaves per-mode colour escapes).
func plainRow(s string) string { return sgrRe.ReplaceAllString(s, "") }

// TestShiftTabCyclesBuildToDefault: starting at "build" (accept-edits),
// Shift+Tab → "default" (""). The cycle's first hop is the one the
// 1.3.5 changelog implicitly endorses by naming accept-edits as the
// "file edits auto-approved" mode — the user must be able to escape
// Build without burning two presses.
func TestShiftTabCyclesBuildToDefault(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAcceptEdits}
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.policy = &policy
	m.mode = policy.Mode

	m.Update(shiftTabKey())
	if m.mode != "" {
		t.Errorf("Shift+Tab from Build: m.mode = %q, want %q (default)", m.mode, "")
	}
	if policy.Mode != "" {
		t.Errorf("Shift+Tab from Build: policy.Mode = %q, want %q (default)", policy.Mode, "")
	}
	// Cycle 283: the cue is the input placeholder, not a transcript line.
	if got := m.input.Placeholder; got != "" {
		t.Errorf("Build→Default placeholder = %q, want empty", got)
	}
	if len(m.response) != 0 {
		t.Errorf("Build→Default wrote %d transcript lines, want 0", len(m.response))
	}
}

// TestShiftTabCyclesDefaultToAuto: starting at "default" (""), Shift+Tab
// → "auto". Cycle 254's reorder makes this the first hop from a fresh
// session's mode (was accept-edits in cycle 104). Documented in 硬約束.
func TestShiftTabCyclesDefaultToAuto(t *testing.T) {
	policy := permissions.Policy{Mode: ""}
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.policy = &policy
	m.mode = policy.Mode

	m.Update(shiftTabKey())
	if m.mode != permissions.ModeAuto {
		t.Errorf("Shift+Tab from Default: m.mode = %q, want %q", m.mode, permissions.ModeAuto)
	}
	if policy.Mode != permissions.ModeAuto {
		t.Errorf("Shift+Tab from Default: policy.Mode = %q, want %q", policy.Mode, permissions.ModeAuto)
	}
	if got := m.input.Placeholder; got != permissions.AutoBanner {
		t.Errorf("Default→Auto placeholder = %q, want AutoBanner %q", got, permissions.AutoBanner)
	}
	if len(m.response) != 0 {
		t.Errorf("Default→Auto wrote %d transcript lines, want 0", len(m.response))
	}
}

// TestShiftTabCyclesAutoToPlan: starting at "auto", Shift+Tab → "plan".
func TestShiftTabCyclesAutoToPlan(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAuto}
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.policy = &policy
	m.mode = policy.Mode

	m.Update(shiftTabKey())
	if m.mode != permissions.ModePlan {
		t.Errorf("Shift+Tab from Auto: m.mode = %q, want %q", m.mode, permissions.ModePlan)
	}
	if policy.Mode != permissions.ModePlan {
		t.Errorf("Shift+Tab from Auto: policy.Mode = %q, want %q", policy.Mode, permissions.ModePlan)
	}
	if got := m.input.Placeholder; got != permissions.PlanBanner {
		t.Errorf("Auto→Plan placeholder = %q, want PlanBanner %q", got, permissions.PlanBanner)
	}
	if len(m.response) != 0 {
		t.Errorf("Auto→Plan wrote %d transcript lines, want 0", len(m.response))
	}
}

// TestShiftTabCyclesPlanToBuild: starting at "plan", Shift+Tab → "build"
// (accept-edits). This is the cycle's wrap; the cycle reaches back to
// Build so the user can re-enter the file-edits mode without a
// second lap.
func TestShiftTabCyclesPlanToBuild(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModePlan}
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.policy = &policy
	m.mode = policy.Mode

	m.Update(shiftTabKey())
	if m.mode != permissions.ModeAcceptEdits {
		t.Errorf("Shift+Tab from Plan (wrap): m.mode = %q, want %q", m.mode, permissions.ModeAcceptEdits)
	}
	if policy.Mode != permissions.ModeAcceptEdits {
		t.Errorf("Shift+Tab from Plan (wrap): policy.Mode = %q, want %q", policy.Mode, permissions.ModeAcceptEdits)
	}
	if got := m.input.Placeholder; got != permissions.AcceptEditsBanner {
		t.Errorf("Plan→Build placeholder = %q, want AcceptEditsBanner %q", got, permissions.AcceptEditsBanner)
	}
	if len(m.response) != 0 {
		t.Errorf("Plan→Build wrote %d transcript lines, want 0", len(m.response))
	}
}

// TestShiftTab_NotInConfigMenu: when the /config Agent Mode menu is
// open, Shift+Tab does NOT cycle the global agent mode. The /config
// panel swallows Tab (and its Shift+Tab variant) as the catch-all
// in handlePanelKey's panelConfig switch — the reference uses Tab
// for value cycling when the row is value-bearing and our clone
// uses ←/→. Either way the key is consumed before it can reach the
// global Shift+Tab handler in tui_run.go's key router. Pinning this
// prevents a future refactor of handlePanelKey from accidentally
// letting Tab fall through to cycleMode() while the user is editing
// the Agent Mode setting.
func TestShiftTab_NotInConfigMenu(t *testing.T) {
	policy := permissions.Policy{Mode: ""}
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.policy = &policy
	m.mode = policy.Mode
	// Open /config on Agent Mode (cursor 0 — first row).
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: 0}
	startMode := m.mode
	startRespLen := len(m.response)

	m.Update(shiftTabKey())

	if m.mode != startMode {
		t.Errorf("Shift+Tab in /config mutated m.mode = %q, want %q (panel must swallow)", m.mode, startMode)
	}
	if policy.Mode != startMode {
		t.Errorf("Shift+Tab in /config mutated policy.Mode = %q, want %q", policy.Mode, startMode)
	}
	if got := len(m.response); got != startRespLen {
		t.Errorf("Shift+Tab in /config appended to response (%d → %d); the panel owns the key", startRespLen, got)
	}
}

// TestShiftTab_StatusLineReflects pins the reference's actual mode cue,
// verified against agy 1.4.3 at 200x40: the *input placeholder* carries
// the banner and the *status row* prefixes the model with the mode
// ("auto · MiniMax M3"). Shift+Tab never writes a transcript line — the
// pre-283 clone did, which stacked stale banners in the conversation.
func TestShiftTab_StatusLineReflects(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAcceptEdits}
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.policy = &policy
	m.mode = policy.Mode
	m.statusModel = "MiniMax M3"

	m.Update(shiftTabKey()) // Build → Default
	if got := m.input.Placeholder; got != "" {
		t.Errorf("Build→Default placeholder = %q, want empty", got)
	}
	if row := plainRow(m.statusRow()); strings.Contains(row, "accept-edits · ") || strings.Contains(row, " · MiniMax M3") {
		t.Errorf("Build→Default status row still carries a mode prefix: %q", row)
	}

	m.Update(shiftTabKey()) // Default → Auto
	if got := m.input.Placeholder; got != permissions.AutoBanner {
		t.Errorf("Default→Auto placeholder = %q, want AutoBanner", got)
	}
	if row := plainRow(m.statusRow()); !strings.Contains(row, "auto · MiniMax M3") {
		t.Errorf("Default→Auto status row = %q, want it to contain %q", row, "auto · MiniMax M3")
	}

	m.Update(shiftTabKey()) // Auto → Plan
	if got := m.input.Placeholder; got != permissions.PlanBanner {
		t.Errorf("Auto→Plan placeholder = %q, want PlanBanner", got)
	}
	if row := plainRow(m.statusRow()); !strings.Contains(row, "plan · MiniMax M3") {
		t.Errorf("Auto→Plan status row = %q, want it to contain %q", row, "plan · MiniMax M3")
	}

	if len(m.response) != 0 {
		t.Errorf("Shift+Tab wrote %d transcript lines, want 0", len(m.response))
	}
}

// (shiftTabKey, the tea.KeyPressMsg constructor used above, lives in
// cmd/wagyu/mode_cycle_test.go — same package, same test binary.)

// TestModeHintHidesAfterEdit pins the reference's placeholder lifetime
// (agy 1.4.3, 200x40): the cue is shown at startup and after Shift+Tab,
// and any keystroke in the textarea removes it for good — emptying the
// buffer does not bring it back, and neither does submitting.
func TestModeHintHidesAfterEdit(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAuto}
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.policy = &policy
	m.mode = policy.Mode
	m.input.Placeholder = permissions.ModeBanner(m.mode)

	// A keystroke marks the input as edited.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
	if !m.modeHintDismissed {
		t.Fatal("typing did not dismiss the mode hint")
	}

	// Shift+Tab re-arms it (and the new mode's banner takes its place).
	m.Update(shiftTabKey())
	if m.modeHintDismissed {
		t.Fatal("Shift+Tab did not re-arm the mode hint")
	}
	if got := m.input.Placeholder; got != permissions.PlanBanner {
		t.Errorf("after Shift+Tab placeholder = %q, want PlanBanner", got)
	}
}
