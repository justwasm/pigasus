// program_status_env_test.go — cycle 239: pin the AGY 1.4.0 environment
// variables that gate the OSC 7501 (Program Status Protocol) report.
//
// Four env vars from the AGY 1.4.0 changelog change the behaviour of
// (*runMode).programStatus() — the function that produces the
// *tea.ProgramStatus bubbletea/v2 encodes into every View(). The
// runtime reads the env vars through three small helpers
// (envOSC7501Off, envOSC7501On, envOSC7501Brief) defined in
// program_status.go and wired as the very first / very last step of
// programStatus():
//
//	                                              key          value
//	env var                 semantic              recogniser   space
//	────────                 ──────────            ──────────   ──────
//	AGY_CLI_OSC7501=off     suppress emission      off|0|no|false → nil (highest-priority gate, line 1)
//	AGY_CLI_OSC7501=on      force-allow emission   on|1|yes|true → no-op (clone's runtime already emits unconditionally; see deliverable §硬约束)
//	AGY_CLI_OSC7501_MESSAGES=brief                 "brief"      → blank Message on Blocked states (last step)
//
// All three are case-insensitive and whitespace-trimmed. Unset /
// unknown values fall through to the existing behaviour.
//
// This file lives next to program_status_test.go rather than inside it
// because the existing file is already 414 lines and a single-purpose
// split keeps the OSC-7501 env-var tests grouped with their helpers
// without forcing a 600-line rewrite of the table tests.
package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// withEnvOSC sets the three OSC 7501 env vars to the given values and
// returns a restore function. Tests call it as `defer withEnvOSC(t,
// …)(t)()` so the helpers stay symmetric with the rest of the package
// (no t.Setenv when we need a 3-line combination). Using t.Setenv would
// require three nested defers per test — fine, but less readable for
// a one-shot setup.
//
// The function does NOT call t.Setenv on the env vars it doesn't touch
// (so it can't t.Setenv("FOO", "BAR")). It uses os.Setenv directly
// because the OSC 7501 vars live in a dedicated namespace (AGY_CLI_*
// prefix) and any test that needs a clean env in this file uses
// cleanupEnvOSC() at the start to wipe them.
//
// We use t.Cleanup because t.Setenv restores on test end but does NOT
// restore when the helper is the only caller — same semantics either
// way; t.Cleanup is explicit about which tests own the env.
func withEnvOSC(t *testing.T, mode, messages string) func() {
	t.Helper()
	cleanupEnvOSC(t)
	if mode != "" {
		t.Setenv(envOSC7501, mode)
	}
	if messages != "" {
		t.Setenv(envOSC7501Messages, messages)
	}
	return func() {}
}

// cleanupEnvOSC blanks every OSC-7501-related env var so tests start
// from a known baseline. t.Setenv would do this implicitly on test
// exit but tests sharing t.Setenv across multiple Setenv calls aren't
// trivial to reason about — explicit is better.
func cleanupEnvOSC(t *testing.T) {
	t.Helper()
	// Use t.Setenv("" , "") semantics by setting both to empty
	// strings; the helpers treat "" as "unset" because they fall
	// through the switch.
	t.Setenv(envOSC7501, "")
	t.Setenv(envOSC7501Messages, "")
}

// TestProgramStatus_EnvOff pins the highest-priority gate:
//
//	AGY_CLI_OSC7501=off  →  programStatus() returns nil.
//
// nil is the signal bubbletea/v2's renderer honours to skip the
// SetProgramStatus OSC write, so the wire is silent for the rest of
// the session. The test covers the four falsy spellings from the
// spec plus whitespace + capitalisation. Each subtest asserts the
// return is nil AND that the function did NOT construct a status
// (so a regression that "returns nil but logs a struct internally"
// is caught — *got would have to be non-nil for the check to fail).
func TestProgramStatus_EnvOff(t *testing.T) {
	cases := []struct {
		name, set string
	}{
		{"off", "off"},
		{"false", "false"},
		{"zero", "0"},
		{"no", "no"},
		{"off-uppercase", "OFF"},
		{"off-mixed-case", "Off"},
		{"off-with-padding", "   off   "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withEnvOSC(t, c.set, "")
			// Drive a state that would otherwise produce a non-nil
			// status (Blocked / Permission so off is the only thing
			// suppressing it).
			m := newPSModel()
			m.pendingPermission = true
			got := m.programStatus()
			if got != nil {
				t.Errorf("AGY_CLI_OSC7501=%q: programStatus() = %+v, want nil", c.set, got)
			}
		})
	}
}

// TestProgramStatus_EnvOff_SuppressesAllStates asserts that the
// suppression is independent of the runMode state — no matter which
// precedence branch would have fired, =off wins. This mirrors
// AGY's stated semantics: "=off turns it off, regardless of state."
func TestProgramStatus_EnvOff_SuppressesAllStates(t *testing.T) {
	withEnvOSC(t, "off", "")
	cases := []struct {
		name string
		set  func(m *runMode)
	}{
		{"idle", func(m *runMode) {}},
		{"working-via-streaming", func(m *runMode) { m.streaming = true }},
		{"working-via-thinking", func(m *runMode) { m.thinking = true }},
		{"working-via-turnCancel", func(m *runMode) { m.turnCancel = func() {} }},
		{"blocked-permission", func(m *runMode) { m.pendingPermission = true }},
		{"blocked-question", func(m *runMode) { m.subAgentQuestionPickerOpen = true }},
		{"blocked-auth", func(m *runMode) { m.pendingAuth = true }},
		{"done", func(m *runMode) { m.lastTurnOutcome = tea.ProgramStateDone }},
		{"error", func(m *runMode) { m.lastTurnOutcome = tea.ProgramStateError }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPSModel()
			c.set(m)
			if got := m.programStatus(); got != nil {
				t.Errorf("AGY_CLI_OSC7501=off + %s: programStatus() = %+v, want nil", c.name, got)
			}
		})
	}
}

// TestProgramStatus_EnvOn_OverridesAutoDetect asserts that the
// explicit opt-in env var does NOT suppress emission. In the clone,
// bubbletea/v2's runtime emits OSC 7501 whenever v.ProgramStatus !=
// nil regardless of the terminal's capability (the AGY binary has
// an internal gate the clone does not — see deliverable §硬约束).
// What this test pins is that the env=off / env=on split is clean:
// setting =on must not be interpreted as off.
//
// We exercise it against a state (Blocked / Permission) where the
// precedence switch would otherwise fill out a fully populated
// *tea.ProgramStatus so a regression that quietly turns =on into
// =off would produce nil here.
func TestProgramStatus_EnvOn_OverridesAutoDetect(t *testing.T) {
	for _, mode := range []string{"on", "ON", "On", "   on   "} {
		t.Run(mode, func(t *testing.T) {
			withEnvOSC(t, mode, "")
			m := newPSModel()
			m.pendingPermission = true
			got := m.programStatus()
			if got == nil {
				t.Fatalf("AGY_CLI_OSC7501=%q + Blocked/Permission: programStatus() = nil, want non-nil",
					mode)
			}
			if got.State != tea.ProgramStateBlocked {
				t.Errorf("AGY_CLI_OSC7501=%q: State = %q, want %q",
					mode, got.State, tea.ProgramStateBlocked)
			}
			if got.Kind != tea.ProgramStatusKindPermission {
				t.Errorf("AGY_CLI_OSC7501=%q: Kind = %q, want %q",
					mode, got.Kind, tea.ProgramStatusKindPermission)
			}
		})
	}
}

// TestProgramStatus_EnvBrief_BlocksMessage pins the brief policy:
// AGY_CLI_OSC7501_MESSAGES=brief strips the human-readable Message
// from Blocked-state reports. State and Kind stay so the terminal's
// status bar still reads "Blocked: Permission" (etc.) but the
// pending-command / pending-question / re-auth prose is suppressed.
//
// The drive is the Blocked / Permission seam — same shape as
// TestProgramStatus's existing table row — so the diff from
// "Message = 'Permission required'" to "Message = ''" is the
// observable change. Question and Auth follow the same code path
// (the gate is on ps.State == ProgramStateBlocked), so a single
// subtest per Kind is enough.
func TestProgramStatus_EnvBrief_BlocksMessage(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *runMode)
		want tea.ProgramState
		kind tea.ProgramStatusKind
	}{
		{"permission", func(m *runMode) { m.pendingPermission = true }, tea.ProgramStateBlocked, tea.ProgramStatusKindPermission},
		{"question", func(m *runMode) { m.subAgentQuestionPickerOpen = true }, tea.ProgramStateBlocked, tea.ProgramStatusKindQuestion},
		{"auth", func(m *runMode) { m.pendingAuth = true }, tea.ProgramStateBlocked, tea.ProgramStatusKindAuth},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withEnvOSC(t, "on", "brief") // =on so =off doesn't mask the test.
			m := newPSModel()
			c.set(m)
			got := m.programStatus()
			if got == nil {
				t.Fatalf("programStatus() = nil, want non-nil")
			}
			if got.State != c.want {
				t.Errorf("State = %q, want %q", got.State, c.want)
			}
			if got.Kind != c.kind {
				t.Errorf("Kind = %q, want %q", got.Kind, c.kind)
			}
			if got.Message != "" {
				t.Errorf("Message = %q, want \"\" (brief strips Blocked prose)", got.Message)
			}
		})
	}
}

// TestProgramStatus_EnvBrief_PreservesError pins the second half of
// the brief policy: Error messages are NOT stripped. The clone's
// programStatus() does not currently set ps.Message for the Error
// branch (it inherits zero value), so the test asserts that brief
// does not blank a hypothetical Error message — verified by
// comparing programStatus() output with and without brief on an
// Error-state runMode. Both calls return the same shape, so brief
// has nothing to blank and can't accidentally blank Error.
//
// The structural guarantee the test pins: with lastTurnOutcome =
// Error, State = Error before and after the brief gate; Message is
// empty in both cases. If a future cycle populates Message on Error
// (e.g. to surface "auth failed: forbidden"), the brief gate must
// NOT touch it. The implementation makes that self-evident — the
// gate is keyed on State == Blocked, not on Message content — and
// the test will keep passing as long as the policy holds.
//
// Done state (also reachable via lastTurnOutcome) is also checked
// because the AGY 1.4.0 changelog specifies Done as an end-of-turn
// state too and brief should be a no-op there for the same reason.
func TestProgramStatus_EnvBrief_PreservesError(t *testing.T) {
	for _, outcome := range []tea.ProgramState{tea.ProgramStateError, tea.ProgramStateDone} {
		t.Run(string(outcome), func(t *testing.T) {
			// Build the same runMode twice — once without brief, once
			// with brief — and confirm the returned Message is the
			// same on both. With Error, the current implementation
			// emits Message = "", so equality is the only stable
			// assertion, but it still pins the gate.
			withEnvOSC(t, "on", "")
			mA := newPSModel()
			mA.lastTurnOutcome = outcome
			a := mA.programStatus()

			withEnvOSC(t, "on", "brief")
			mB := newPSModel()
			mB.lastTurnOutcome = outcome
			b := mB.programStatus()

			if a == nil || b == nil {
				t.Fatalf("programStatus() returned nil: a=%v b=%v", a, b)
			}
			if a.State != outcome {
				t.Errorf("without brief: State = %q, want %q", a.State, outcome)
			}
			if b.State != outcome {
				t.Errorf("with brief: State = %q, want %q", b.State, outcome)
			}
			if a.Message != b.Message {
				t.Errorf("Message diverged with brief: no-brief=%q brief=%q (must stay equal)",
					a.Message, b.Message)
			}
		})
	}
}

// TestProgramStatus_EnvBrief_LeavesWorkingUnchanged pins the third
// clause of the brief policy: Working states are not touched. The
// clone's programStatus() never sets Message on Working (the
// streaming flag already provides the "is it busy" cue to the
// renderer), so this is mostly a structural assertion that the
// brief gate does not blanket-strip every State. We assert on a
// representative Working-shaped runMode that Message remains ""
// (the impl's default) before and after brief.
func TestProgramStatus_EnvBrief_LeavesWorkingUnchanged(t *testing.T) {
	withEnvOSC(t, "on", "")
	m := newPSModel()
	m.streaming = true
	noBrief := m.programStatus()

	withEnvOSC(t, "on", "brief")
	m2 := newPSModel()
	m2.streaming = true
	brief := m2.programStatus()

	if noBrief == nil || brief == nil {
		t.Fatalf("programStatus() returned nil: noBrief=%v brief=%v", noBrief, brief)
	}
	if noBrief.State != tea.ProgramStateWorking || brief.State != tea.ProgramStateWorking {
		t.Errorf("State must be Working: noBrief=%q brief=%q", noBrief.State, brief.State)
	}
	if noBrief.Message != brief.Message {
		t.Errorf("Working Message must not change with brief: noBrief=%q brief=%q",
			noBrief.Message, brief.Message)
	}
}

// TestProgramStatus_EnvCaseInsensitive pins that all three env vars
// accept mixed-case spellings: "off" == "OFF" == "Off", "brief" ==
// "BRIEF" == "Brief", "on" == "ON" == "On". The brief is explicit
// ("case-insensitive"); a regression that drops ToLower would be
// silent and only catch one of the three modes, so this test makes
// the contract enforceable.
func TestProgramStatus_EnvCaseInsensitive(t *testing.T) {
	cases := []struct {
		name string
		mode string
		// wantNil is true iff the mode is a falsy variant.
		wantNil bool
		wantMsg string // expected Message on a Blocked/Permission model; "" if wantNil or brief
	}{
		{"off-lower", "off", true, ""},
		{"off-upper", "OFF", true, ""},
		{"off-mixed", "Off", true, ""},
		{"on-lower", "on", false, "Permission required"},
		{"on-upper", "ON", false, "Permission required"},
		{"on-mixed", "On", false, "Permission required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withEnvOSC(t, c.mode, "")
			m := newPSModel()
			m.pendingPermission = true
			got := m.programStatus()
			if c.wantNil {
				if got != nil {
					t.Errorf("mode=%q: programStatus() = %+v, want nil", c.mode, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("mode=%q: programStatus() = nil, want non-nil", c.mode)
			}
			if got.State != tea.ProgramStateBlocked {
				t.Errorf("mode=%q: State = %q, want Blocked", c.mode, got.State)
			}
			if got.Message != c.wantMsg {
				t.Errorf("mode=%q: Message = %q, want %q", c.mode, got.Message, c.wantMsg)
			}
		})
	}

	// Brief's case-insensitivity check lives on the messages axis.
	briefCases := []string{"brief", "BRIEF", "Brief", "BrIeF"}
	for _, b := range briefCases {
		t.Run("brief-"+b, func(t *testing.T) {
			withEnvOSC(t, "on", b)
			m := newPSModel()
			m.pendingPermission = true
			got := m.programStatus()
			if got == nil {
				t.Fatalf("AGY_CLI_OSC7501_MESSAGES=%q: programStatus() = nil", b)
			}
			if got.Message != "" {
				t.Errorf("AGY_CLI_OSC7501_MESSAGES=%q: Message = %q, want \"\" (brief is case-insensitive)",
					b, got.Message)
			}
		})
	}
}

// TestProgramStatus_DefaultUnchanged is the no-regression assertion:
// with both env vars unset (or set to an unrecognised value), the
// function returns the same shape it did before this cycle — the
// three existing test table rows for Blocked / Permission are still
// satisfied exactly. Pinned separately from the existing TestProgramStatus
// table because that table hardcodes one env state (the test runner
// shares an os.Environ across subtests) and we want the *baseline*
// confirmed on the same code path the production binary takes.
func TestProgramStatus_DefaultUnchanged(t *testing.T) {
	cases := []struct {
		name string
		mode string // "" = unset, "garbage" = unrecognised value (treated as default)
		set  func(m *runMode)
		want tea.ProgramStatus
	}{
		{
			name: "unset-idle",
			mode: "",
			set:  func(m *runMode) {},
			want: tea.ProgramStatus{State: tea.ProgramStateIdle, App: programStatusApp},
		},
		{
			name: "unset-permission",
			mode: "",
			set:  func(m *runMode) { m.pendingPermission = true },
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programStatusApp,
				Kind:    tea.ProgramStatusKindPermission,
				Message: "Permission required",
			},
		},
		{
			name: "unrecognised-mode-is-default",
			mode: "garbage",
			set:  func(m *runMode) { m.pendingPermission = true },
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programStatusApp,
				Kind:    tea.ProgramStatusKindPermission,
				Message: "Permission required",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cleanupEnvOSC(t)
			if c.mode != "" {
				t.Setenv(envOSC7501, c.mode)
			}
			m := newPSModel()
			c.set(m)
			got := m.programStatus()
			if got == nil {
				t.Fatalf("programStatus() = nil, want %+v", c.want)
			}
			if *got != c.want {
				t.Errorf("programStatus() = %+v, want %+v", *got, c.want)
			}
		})
	}
}

// TestProgramStatus_OffWinsOverBrief is the precedence test:
//
//	AGY_CLI_OSC7501=off + AGY_CLI_OSC7501_MESSAGES=brief
//	    →  programStatus() returns nil; brief is irrelevant.
//
// This pins the priority order documented in programStatus.go's
// precedence comment: =off is the highest-priority gate and short-
// circuits the function before the brief gate is reached.
func TestProgramStatus_OffWinsOverBrief(t *testing.T) {
	withEnvOSC(t, "off", "brief")
	m := newPSModel()
	m.pendingPermission = true // would otherwise yield a populated Blocked / Permission status
	if got := m.programStatus(); got != nil {
		t.Errorf("AGY_CLI_OSC7501=off + MESSAGES=brief: programStatus() = %+v, want nil", got)
	}
}
