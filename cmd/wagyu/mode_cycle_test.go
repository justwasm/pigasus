package main

// mode_cycle_test.go — cycle 104 added Shift+Tab cycling and cycle
// 254 reordered the cycle to Build → Default → Auto → Plan → Build.
// The picker-overlap test asserts only that the mode moved away from
// default (the per-target transitions are pinned by
// cmd/wagyu/shift_tab_test.go).

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/permissions"
)

func shiftTabKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
}

func TestShiftTabCyclesMode(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAcceptEdits}
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.policy = &policy
	m.mode = policy.Mode

	// Cycle 254: Build → Default → Auto → Plan → Build.
	// accept-edits (Build) → default → auto → plan → accept-edits.
	//
	// Cycle 283: the cue is the input placeholder ("" for the default
	// mode), and the transcript stays untouched.
	for _, want := range []struct{ mode, placeholder string }{
		{"", ""},
		{permissions.ModeAuto, permissions.AutoBanner},
		{permissions.ModePlan, permissions.PlanBanner},
		{permissions.ModeAcceptEdits, permissions.AcceptEditsBanner},
	} {
		m.Update(shiftTabKey())
		if m.mode != want.mode {
			t.Fatalf("after Shift+Tab m.mode = %q, want %q", m.mode, want.mode)
		}
		if policy.Mode != want.mode {
			t.Fatalf("after Shift+Tab policy.Mode = %q, want %q (the tool gate must see it)", policy.Mode, want.mode)
		}
		if got := m.input.Placeholder; got != want.placeholder {
			t.Errorf("mode %q placeholder = %q, want %q", want.mode, got, want.placeholder)
		}
		if len(m.response) != 0 {
			t.Errorf("mode %q wrote %d transcript lines, want 0", want.mode, len(m.response))
		}
	}
}

// TestShiftTabStartsFromFlagMode checks that a session started with --mode
// continues the cycle from there rather than restarting at the first
// element of CycleModes. With the cycle-254 order
// (Build → Default → Auto → Plan → Build), --mode auto + one press
// still advances to plan, identical to the pre-254 behaviour.
func TestShiftTabStartsFromFlagMode(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAuto}
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.policy = &policy
	m.mode = policy.Mode

	m.Update(shiftTabKey())
	if m.mode != permissions.ModePlan {
		t.Errorf("--mode auto + Shift+Tab = %q, want plan", m.mode)
	}
}

// TestShiftTabDoesNotCompletePickerRow guards the ordering fix: plain Tab
// completes the highlighted picker row, Shift+Tab must still cycle.
// Cycle 254: with the cycle starting from accept-edits (Build), a
// fresh session's first Shift+Tab now advances to auto (not
// accept-edits as cycle 104 had it). The test asserts the cycle moved
// away from the default mode rather than pinning a specific target —
// the per-target assertions live in cmd/wagyu/shift_tab_test.go.
func TestShiftTabDoesNotCompletePickerRow(t *testing.T) {
	policy := permissions.Policy{}
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.policy = &policy
	m.input.SetValue("/a")
	m.picker.Update([]rune("/a"))
	if m.picker.Selected() == nil {
		t.Skip("picker has no selection for this prefix")
	}
	before := m.input.Value()

	m.Update(shiftTabKey())
	if m.input.Value() != before {
		t.Errorf("Shift+Tab rewrote the input to %q", m.input.Value())
	}
	if m.mode == "" {
		t.Errorf("Shift+Tab with the picker open did not cycle (mode=%q)", m.mode)
	}
}
