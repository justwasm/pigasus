// Package programstatus_test pins the OSC 7501 status report precedence.
package programstatus_test

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/justwasm/pigasus/internal/programstatus"
)

type precedenceCase struct {
	name string
	in   programstatus.Inputs
	want tea.ProgramStatus
}

func runPrecedenceCases(t *testing.T, cases []precedenceCase) {
	t.Helper()
	cleanupEnvOSC(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := programstatus.Resolve(c.in)
			if got == nil {
				t.Fatalf("Resolve() = nil, want %+v", c.want)
			}
			if *got != c.want {
				t.Errorf("Resolve() = %+v, want %+v", *got, c.want)
			}
		})
	}
}

func TestResolve_Precedence_IdleWorking(t *testing.T) {
	runPrecedenceCases(t, []precedenceCase{
		{
			name: "idle",
			in:   programstatus.Inputs{},
			want: tea.ProgramStatus{State: tea.ProgramStateIdle, App: programstatus.App},
		},
		{
			name: "working from busy",
			in:   programstatus.Inputs{IsAgentBusy: true},
			want: tea.ProgramStatus{State: tea.ProgramStateWorking, App: programstatus.App},
		},
		{
			name: "working wins over last outcome",
			in: programstatus.Inputs{
				IsAgentBusy:     true,
				LastTurnOutcome: tea.ProgramStateDone,
			},
			want: tea.ProgramStatus{State: tea.ProgramStateWorking, App: programstatus.App},
		},
	})
}

func TestResolve_Precedence_Blocked(t *testing.T) {
	runPrecedenceCases(t, []precedenceCase{
		{
			name: "blocked question",
			in:   programstatus.Inputs{HasPendingQuestion: true},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programstatus.App,
				Kind:    tea.ProgramStatusKindQuestion,
				Message: "Questions need your input",
			},
		},
		{
			name: "blocked permission wins over question",
			in: programstatus.Inputs{
				PendingPermission:  true,
				HasPendingQuestion: true,
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programstatus.App,
				Kind:    tea.ProgramStatusKindPermission,
				Message: "Permission required",
			},
		},
		{
			name: "blocked auth",
			in:   programstatus.Inputs{PendingAuth: true},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programstatus.App,
				Kind:    tea.ProgramStatusKindAuth,
				Message: "Sign-in required",
			},
		},
		{
			name: "blocked question wins over auth",
			in: programstatus.Inputs{
				HasPendingQuestion: true,
				PendingAuth:        true,
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programstatus.App,
				Kind:    tea.ProgramStatusKindQuestion,
				Message: "Questions need your input",
			},
		},
	})
}

func TestResolve_Precedence_Outcomes(t *testing.T) {
	runPrecedenceCases(t, []precedenceCase{
		{
			name: "done",
			in:   programstatus.Inputs{LastTurnOutcome: tea.ProgramStateDone},
			want: tea.ProgramStatus{State: tea.ProgramStateDone, App: programstatus.App},
		},
		{
			name: "error",
			in:   programstatus.Inputs{LastTurnOutcome: tea.ProgramStateError},
			want: tea.ProgramStatus{State: tea.ProgramStateError, App: programstatus.App},
		},
	})
}

func verifyOSCSequence(t *testing.T, in programstatus.Inputs, want string) {
	t.Helper()
	var buf bytes.Buffer
	if err := uv.EncodeProgramStatus(&buf, programstatus.Resolve(in)); err != nil {
		t.Fatalf("EncodeProgramStatus: %v", err)
	}
	got := buf.String()
	if got != want {
		t.Errorf("OSC 7501 bytes = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "\x1b]7501;") || !strings.HasSuffix(got, "\x07") || !strings.Contains(got, "app=wagyu") {
		t.Errorf("malformed OSC 7501: %q", got)
	}
}

func TestResolve_EncodeOSC7501Sequence(t *testing.T) {
	cleanupEnvOSC(t)
	verifyOSCSequence(t, programstatus.Inputs{}, "\x1b]7501;state=idle:app=wagyu\x07")
	verifyOSCSequence(t, programstatus.Inputs{IsAgentBusy: true}, "\x1b]7501;state=working:app=wagyu\x07")
	verifyOSCSequence(t, programstatus.Inputs{LastTurnOutcome: tea.ProgramStateDone}, "\x1b]7501;state=done:app=wagyu\x07")
	verifyOSCSequence(t, programstatus.Inputs{LastTurnOutcome: tea.ProgramStateError}, "\x1b]7501;state=error:app=wagyu\x07")
	verifyOSCSequence(t, programstatus.Inputs{PendingPermission: true}, "\x1b]7501;state=blocked:kind=permission:app=wagyu:msg=UGVybWlzc2lvbiByZXF1aXJlZA==\x07")
	verifyOSCSequence(t, programstatus.Inputs{HasPendingQuestion: true}, "\x1b]7501;state=blocked:kind=question:app=wagyu:msg=UXVlc3Rpb25zIG5lZWQgeW91ciBpbnB1dA==\x07")
	verifyOSCSequence(t, programstatus.Inputs{PendingAuth: true}, "\x1b]7501;state=blocked:kind=auth:app=wagyu:msg=U2lnbi1pbiByZXF1aXJlZA==\x07")
}
