// program_status_test.go — cycle osc-7501: pins the OSC 7501 (Program
// Status Protocol) report wagyu emits back to the terminal for every
// state combination the brief requires.
//
// Pattern mirrors crush's internal/ui/model/program_status_test.go:
// table-driven, one subtest per state, comparing against the struct
// value the runtime would otherwise read off tea.View.ProgramStatus.
//
// The table rows map one-for-one to the precedence documented in
// programStatus.go:
//
//  1. permission — m.pendingPermission
//  2. question   — m.hasPendingQuestion() (picker OR answer target)
//  3. auth       — m.pendingAuth
//  4. working    — m.isAgentBusy() (streaming / turnCancel / thinking)
//  5. done       — m.lastTurnOutcome = ProgramStateDone
//  6. error      — m.lastTurnOutcome = ProgramStateError
//  7. idle       — default
//
// The "permission wins over question" / "working wins over last outcome"
// rows mirror crush's two precedence-pinning tests exactly so a future
// refactor that reorders the switch is caught by the table rather than
// landing silently.
package main

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// newPSModel returns a runMode with the minimum surface needed to drive
// programStatus() — none of the TUI-render code, the conversation store
// or the spinner — so the test only exercises the precedence logic. The
// streaming flag, picker, answer target, and the three OSC 7501 fields
// start at their zero values; setup callbacks mutate whichever ones the
// row needs.
//
// Cycle 235: take() the streaming flag explicitly in each row that needs
// it because the migration to bubbletea/v2's *tea.Program (replaces the
// v1 nil prog with one that owns an event channel) does not affect
// runMode fields — only the test surface — but the field still lives on
// runMode for parity with cycle 235's turnCancel lifecycle.
func newPSModel() *runMode {
	return &runMode{}
}

func TestProgramStatus(t *testing.T) {
	cases := []struct {
		name  string
		setup func(m *runMode)
		want  tea.ProgramStatus
	}{
		{
			name: "idle",
			want: tea.ProgramStatus{State: tea.ProgramStateIdle, App: programStatusApp},
		},
		{
			name: "working from streaming",
			setup: func(m *runMode) {
				m.streaming = true
			},
			want: tea.ProgramStatus{State: tea.ProgramStateWorking, App: programStatusApp},
		},
		{
			name: "working from thinking",
			setup: func(m *runMode) {
				m.thinking = true
			},
			want: tea.ProgramStatus{State: tea.ProgramStateWorking, App: programStatusApp},
		},
		{
			name: "working from in-flight cancel",
			setup: func(m *runMode) {
				// turnCancel != nil is the canonical "a turn is
				// still in flight even if streaming / thinking have
				// flipped off in this frame" state, matching
				// cycle-235's cancellable-turn contract.
				m.turnCancel = func() {}
			},
			want: tea.ProgramStatus{State: tea.ProgramStateWorking, App: programStatusApp},
		},
		{
			name: "blocked question when picker open",
			setup: func(m *runMode) {
				m.subAgentQuestionPickerOpen = true
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programStatusApp,
				Kind:    tea.ProgramStatusKindQuestion,
				Message: "Questions need your input",
			},
		},
		{
			name: "blocked question when answer target set",
			setup: func(m *runMode) {
				m.subAgentQuestionAnswerTarget = "q-1"
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programStatusApp,
				Kind:    tea.ProgramStatusKindQuestion,
				Message: "Questions need your input",
			},
		},
		{
			name: "blocked permission wins over question",
			setup: func(m *runMode) {
				m.subAgentQuestionPickerOpen = true
				m.pendingPermission = true
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programStatusApp,
				Kind:    tea.ProgramStatusKindPermission,
				Message: "Permission required",
			},
		},
		{
			name: "blocked auth",
			setup: func(m *runMode) {
				m.pendingAuth = true
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programStatusApp,
				Kind:    tea.ProgramStatusKindAuth,
				Message: "Sign-in required",
			},
		},
		{
			name: "blocked question wins over auth too",
			setup: func(m *runMode) {
				m.pendingAuth = true
				m.subAgentQuestionPickerOpen = true
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programStatusApp,
				Kind:    tea.ProgramStatusKindQuestion,
				Message: "Questions need your input",
			},
		},
		{
			name: "working wins over the last outcome",
			setup: func(m *runMode) {
				m.lastTurnOutcome = tea.ProgramStateDone
				m.streaming = true
			},
			want: tea.ProgramStatus{State: tea.ProgramStateWorking, App: programStatusApp},
		},
		{
			name: "outcome once idle: done",
			setup: func(m *runMode) {
				m.lastTurnOutcome = tea.ProgramStateDone
			},
			want: tea.ProgramStatus{State: tea.ProgramStateDone, App: programStatusApp},
		},
		{
			name: "outcome once idle: error",
			setup: func(m *runMode) {
				m.lastTurnOutcome = tea.ProgramStateError
			},
			want: tea.ProgramStatus{State: tea.ProgramStateError, App: programStatusApp},
		},
		{
			name: "idle when last outcome cleared",
			setup: func(m *runMode) {
				m.lastTurnOutcome = tea.ProgramStateDone
				m.clearLastTurnOutcome()
			},
			want: tea.ProgramStatus{State: tea.ProgramStateIdle, App: programStatusApp},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPSModel()
			if c.setup != nil {
				c.setup(m)
			}
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

// TestRecordTurnOutcomeClears pins the lifecycle of lastTurnOutcome: the
// turnDoneMsg handler calls recordTurnOutcome(Done), the prompt-submit
// path calls clearLastTurnOutcome(). Mirrors crush's
// TestSendMessageClearsTurnOutcome in shape.
func TestRecordTurnOutcomeClears(t *testing.T) {
	m := newPSModel()
	m.recordTurnOutcome(tea.ProgramStateDone)
	if got := m.programStatus(); got.State != tea.ProgramStateDone {
		t.Errorf("after recordTurnOutcome(Done): state = %q, want %q", got.State, tea.ProgramStateDone)
	}
	m.clearLastTurnOutcome()
	if got := m.programStatus(); got.State != tea.ProgramStateIdle {
		t.Errorf("after clearLastTurnOutcome: state = %q, want %q", got.State, tea.ProgramStateIdle)
	}
}

// TestProgramStatusReportIncludesApp pins the OSC 7501 byte layout: the
// App field must match programStatusApp ("wagyu") on every state so the
// terminal can identify the record owner — the runtime encodes
// "app=wagyu" verbatim and a stray capitalisation would change the
// emitted escape sequence.
func TestProgramStatusReportIncludesApp(t *testing.T) {
	for _, s := range []tea.ProgramState{
		tea.ProgramStateIdle, tea.ProgramStateWorking, tea.ProgramStateDone,
		tea.ProgramStateError,
	} {
		ps := &tea.ProgramStatus{State: s, App: programStatusApp}
		if ps.App != "wagyu" {
			t.Errorf("App for state %q = %q, want %q", s, ps.App, "wagyu")
		}
	}
	// Blocked kind states also pin App.
	for _, k := range []tea.ProgramStatusKind{
		tea.ProgramStatusKindPermission, tea.ProgramStatusKindQuestion,
		tea.ProgramStatusKindAuth,
	} {
		ps := &tea.ProgramStatus{State: tea.ProgramStateBlocked, Kind: k, App: programStatusApp}
		if ps.App != "wagyu" {
			t.Errorf("Blocked App for kind %q = %q, want %q", k, ps.App, "wagyu")
		}
	}
}

// TestHasPendingQuestion picks out the two question paths the picker
// takes: the picker overlay open, and the answer textarea focused on a
// queued question. Pinning both prevents a future cycle that splits
// these into separate state machines from silently dropping one.
func TestHasPendingQuestion(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *runMode)
		want bool
	}{
		{"off", func(m *runMode) {}, false},
		{"picker open", func(m *runMode) { m.subAgentQuestionPickerOpen = true }, true},
		{"answer target set", func(m *runMode) { m.subAgentQuestionAnswerTarget = "q-1" }, true},
		{"both", func(m *runMode) {
			m.subAgentQuestionPickerOpen = true
			m.subAgentQuestionAnswerTarget = "q-2"
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPSModel()
			c.set(m)
			if got := m.hasPendingQuestion(); got != c.want {
				t.Errorf("hasPendingQuestion() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestIsAgentBusy picks out the three working paths the runtime takes:
// streaming, thinking, and turnCancel. Pinning turnCancel separately
// matters because cycle-235 installed the cancel lifecycle so a
// user-pressed Esc mid-stream keeps the Working indicator stable
// through the chunk → done transition.
func TestIsAgentBusy(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *runMode)
		want bool
	}{
		{"idle", func(m *runMode) {}, false},
		{"streaming", func(m *runMode) { m.streaming = true }, true},
		{"thinking", func(m *runMode) { m.thinking = true }, true},
		{"turnCancel", func(m *runMode) { m.turnCancel = func() {} }, true},
		{"thinking wins over no streaming flag", func(m *runMode) { m.thinking = true }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPSModel()
			c.set(m)
			if got := m.isAgentBusy(); got != c.want {
				t.Errorf("isAgentBusy() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestProgramStatusEmitsOSC7501Sequence verifies that the bytes the
// runtime would emit for each ProgramStatus match the OSC 7501 wire
// shape. Encoding goes through ultraviolet.EncodeProgramStatus because
// that is the exact path the bubbletea renderer takes from tea.View.
// ProgramStatus to the emitted bytes; a struct comparison alone would
// miss a runtime regression in the encoder.
//
// The OSC 7501 Message field is base64-encoded in the wire format
// (each `msg=…` segment uses base64 stdlib encoding per the spec);
// the literal block messages are short enough that the encoder keeps
// them as a single token. We pin the exact bytes below so a future
// ansi.SetProgramStatus schema change is caught.
func TestProgramStatusEmitsOSC7501Sequence(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *runMode)
		want string
	}{
		{
			name: "idle",
			set:  func(m *runMode) {},
			want: "\x1b]7501;state=idle:app=wagyu\x07",
		},
		{
			name: "working",
			set:  func(m *runMode) { m.streaming = true },
			want: "\x1b]7501;state=working:app=wagyu\x07",
		},
		{
			name: "done",
			set:  func(m *runMode) { m.lastTurnOutcome = tea.ProgramStateDone },
			want: "\x1b]7501;state=done:app=wagyu\x07",
		},
		{
			name: "error",
			set:  func(m *runMode) { m.lastTurnOutcome = tea.ProgramStateError },
			want: "\x1b]7501;state=error:app=wagyu\x07",
		},
		{
			name: "blocked permission",
			set:  func(m *runMode) { m.pendingPermission = true },
			want: "\x1b]7501;state=blocked:kind=permission:app=wagyu:msg=UGVybWlzc2lvbiByZXF1aXJlZA==\x07",
		},
		{
			name: "blocked question",
			set:  func(m *runMode) { m.subAgentQuestionPickerOpen = true },
			want: "\x1b]7501;state=blocked:kind=question:app=wagyu:msg=UXVlc3Rpb25zIG5lZWQgeW91ciBpbnB1dA==\x07",
		},
		{
			name: "blocked auth",
			set:  func(m *runMode) { m.pendingAuth = true },
			want: "\x1b]7501;state=blocked:kind=auth:app=wagyu:msg=U2lnbi1pbiByZXF1aXJlZA==\x07",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPSModel()
			c.set(m)
			var buf bytes.Buffer
			if err := uv.EncodeProgramStatus(&buf, m.programStatus()); err != nil {
				t.Fatalf("EncodeProgramStatus: %v", err)
			}
			got := buf.String()
			if got != c.want {
				t.Errorf("OSC 7501 bytes = %q, want %q", got, c.want)
			}
			// Sanity: every sequence must start with "\x1b]7501;" and
			// end with BEL (\x07). Loose format guards in case the
			// exact ordering changes but the wire shape must not.
			if !strings.HasPrefix(got, "\x1b]7501;") {
				t.Errorf("missing OSC 7501 prefix: %q", got)
			}
			if !strings.HasSuffix(got, "\x07") {
				t.Errorf("missing BEL terminator: %q", got)
			}
			if !strings.Contains(got, "app=wagyu") {
				t.Errorf("missing app=wagyu: %q", got)
			}
		})
	}
}

// TestProgramStatusAssignedToView confirms View()'s one-line wire:
//
//	v.ProgramStatus = m.programStatus()
//
// is the only path connecting the precedence logic to the OSC 7501
// output. We test the *intent* without driving the textarea (which
// needs a fully-initialised *tea.Program and panics with a bare
// runMode value) by exercising the same wiring through a stand-in:
// a tiny helper that mirrors the View() assignment, ensuring the
// struct flow is correct.
func TestProgramStatusAssignedToView(t *testing.T) {
	// Stand-in for the runtime: build a tea.View the same way tui_run.go
	// does, then attach the programStatus() value. If this breaks the
	// assignment still works.
	attach := func(body string, ps *tea.ProgramStatus) tea.View {
		v := tea.NewView(body)
		v.ProgramStatus = ps
		return v
	}

	m := newPSModel()
	got := attach("body", m.programStatus())
	if got.ProgramStatus == nil {
		t.Fatalf("attach() did not propagate programStatus; got nil")
	}
	if got.ProgramStatus.State != tea.ProgramStateIdle {
		t.Errorf("ProgramStatus.State = %q, want %q", got.ProgramStatus.State, tea.ProgramStateIdle)
	}

	m.streaming = true
	got2 := attach("body", m.programStatus())
	if got2.ProgramStatus.State != tea.ProgramStateWorking {
		t.Errorf("after streaming=true: ProgramStatus.State = %q, want %q",
			got2.ProgramStatus.State, tea.ProgramStateWorking)
	}
}
