// Package programstatus_test tests the OSC 7501 environment variables.
package programstatus_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/programstatus"
)

func withEnvOSC(t *testing.T, mode, messages string) {
	t.Helper()
	cleanupEnvOSC(t)
	if mode != "" {
		t.Setenv(programstatus.EnvOSC7501, mode)
	}
	if messages != "" {
		t.Setenv(programstatus.EnvOSC7501Messages, messages)
	}
}

func cleanupEnvOSC(t *testing.T) {
	t.Helper()
	t.Setenv(programstatus.EnvOSC7501, "")
	t.Setenv(programstatus.EnvOSC7501Messages, "")
}

func TestEnvOff(t *testing.T) {
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
			if !programstatus.EnvOff() {
				t.Fatalf("EnvOff() = false, want true for %q", c.set)
			}
			got := programstatus.Resolve(programstatus.Inputs{})
			if got != nil {
				t.Errorf("AGY_CLI_OSC7501=%q: Resolve() = %+v, want nil", c.set, got)
			}
		})
	}
}

func TestEnvOff_SuppressesAllStates(t *testing.T) {
	cases := []struct {
		name string
		in   programstatus.Inputs
	}{
		{"idle", programstatus.Inputs{}},
		{"working-via-busy", programstatus.Inputs{IsAgentBusy: true}},
		{"blocked-permission", programstatus.Inputs{PendingPermission: true}},
		{"blocked-question", programstatus.Inputs{HasPendingQuestion: true}},
		{"blocked-auth", programstatus.Inputs{PendingAuth: true}},
		{"done", programstatus.Inputs{LastTurnOutcome: tea.ProgramStateDone}},
		{"error", programstatus.Inputs{LastTurnOutcome: tea.ProgramStateError}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withEnvOSC(t, "off", "")
			if got := programstatus.Resolve(c.in); got != nil {
				t.Errorf("AGY_CLI_OSC7501=off + %s: Resolve() = %+v, want nil", c.name, got)
			}
		})
	}
}

func TestEnvOn(t *testing.T) {
	cases := []struct {
		name, set string
	}{
		{"on", "on"},
		{"ON", "ON"},
		{"On", "On"},
		{"padded", "   on   "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withEnvOSC(t, c.set, "")
			if !programstatus.EnvOn() {
				t.Fatalf("EnvOn() = false, want true for %q", c.set)
			}
			got := programstatus.Resolve(programstatus.Inputs{PendingPermission: true})
			if got == nil {
				t.Fatalf("AGY_CLI_OSC7501=%q + Blocked/Permission: Resolve() = nil, want non-nil", c.set)
			}
		})
	}
}

func TestEnvBrief_BlocksMessage(t *testing.T) {
	cases := []struct {
		name string
		in   programstatus.Inputs
		kind tea.ProgramStatusKind
	}{
		{"permission", programstatus.Inputs{PendingPermission: true}, tea.ProgramStatusKindPermission},
		{"question", programstatus.Inputs{HasPendingQuestion: true}, tea.ProgramStatusKindQuestion},
		{"auth", programstatus.Inputs{PendingAuth: true}, tea.ProgramStatusKindAuth},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withEnvOSC(t, "", "brief")
			got := programstatus.Resolve(c.in)
			if got == nil {
				t.Fatalf("Resolve() = nil, want non-nil")
			}
			if got.State != tea.ProgramStateBlocked {
				t.Errorf("State = %q, want %q", got.State, tea.ProgramStateBlocked)
			}
			if got.Kind != c.kind {
				t.Errorf("Kind = %q, want %q", got.Kind, c.kind)
			}
			if got.Message != "" {
				t.Errorf("Message = %q, want empty (suppressed by brief)", got.Message)
			}
		})
	}
}

func TestEnvBrief_PreservesErrorAndDone(t *testing.T) {
	cases := []struct {
		name    string
		outcome tea.ProgramState
	}{
		{"error", tea.ProgramStateError},
		{"done", tea.ProgramStateDone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withEnvOSC(t, "", "")
			a := programstatus.Resolve(programstatus.Inputs{LastTurnOutcome: c.outcome})

			withEnvOSC(t, "", "brief")
			b := programstatus.Resolve(programstatus.Inputs{LastTurnOutcome: c.outcome})

			if a == nil || b == nil {
				t.Fatalf("Resolve() returned nil: a=%v b=%v", a, b)
			}
			if *a != *b {
				t.Errorf("brief changed outcome %q: without=%+v with=%+v", c.outcome, *a, *b)
			}
		})
	}
}

func TestEnvBrief_LeavesWorkingUnchanged(t *testing.T) {
	withEnvOSC(t, "", "")
	noBrief := programstatus.Resolve(programstatus.Inputs{IsAgentBusy: true})

	withEnvOSC(t, "", "brief")
	brief := programstatus.Resolve(programstatus.Inputs{IsAgentBusy: true})

	if noBrief == nil || brief == nil {
		t.Fatalf("Resolve() returned nil: noBrief=%v brief=%v", noBrief, brief)
	}
	if *noBrief != *brief {
		t.Errorf("brief changed Working: without=%+v with=%+v", *noBrief, *brief)
	}
}

func TestEnvOff_WinsOverBrief(t *testing.T) {
	withEnvOSC(t, "off", "brief")
	if got := programstatus.Resolve(programstatus.Inputs{PendingPermission: true}); got != nil {
		t.Errorf("AGY_CLI_OSC7501=off + MESSAGES=brief: Resolve() = %+v, want nil", got)
	}
}
