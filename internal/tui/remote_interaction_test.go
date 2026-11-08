package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
	"github.com/justwasm/pigasus/internal/ui/views/subagentquestionpanel"
)

// TestPermissionPromptLines pins the modal rendering: a raised prompt is
// drawn by the view, so the remote turn's goroutine does not have to send
// the program a UI message first.
func TestPermissionPromptLines(t *testing.T) {
	DefaultPermissionPrompts.Reset()
	defer DefaultPermissionPrompts.Reset()

	prompt := DefaultPermissionPrompts.Raise(permissionprompt.Request{
		Tool:    "run_command",
		Command: "rm -rf dist",
	})
	defer prompt.Resolve(permissionprompt.Result{Decision: permissionprompt.DecisionDeny})

	m := &RootModel{}
	lines := m.permissionPromptLines(80)
	if len(lines) == 0 {
		t.Fatal("permissionPromptLines returned nothing for an open prompt")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "Permission Required") {
		t.Errorf("rendered modal has no title:\n%s", strings.Join(lines, "\n"))
	}
	if got := m.permissionPromptLines(0); len(got) == 0 {
		t.Error("permissionPromptLines(0) should fall back to a default width")
	}
}

// TestHandleRemotePermission maps the modal's verdict onto the remote answer.
func TestHandleRemotePermission(t *testing.T) {
	DefaultPermissionPrompts.Reset()
	defer DefaultPermissionPrompts.Reset()

	m := &RootModel{}
	done := make(chan agentturn.RemoteInteractionAnswer, 1)
	go func() {
		ans, _ := m.handleRemoteInteraction(context.Background(), agentturn.RemoteInteraction{
			Kind:     agentturn.RemoteInteractionPermission,
			ToolName: "run_command",
			Action:   "command",
			Target:   "git status",
			Reason:   "read the working tree",
		})
		done <- ans
	}()

	prompt := waitForPermissionPrompt(t)
	prompt.Resolve(permissionprompt.Result{Decision: permissionprompt.DecisionApproveOnce})

	select {
	case ans := <-done:
		if !ans.Allowed {
			t.Fatal("approval did not reach the remote answer")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleRemoteInteraction did not return")
	}
}

// TestHandleRemoteQuestion drives a remote question through the same
// interactive form as a local ask_question and maps the picked option to
// its wire id.
func TestHandleRemoteQuestion(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	req := agentturn.RemoteInteraction{
		Kind: agentturn.RemoteInteractionQuestion,
		Questions: []agentturn.RemoteQuestion{{
			Prompt: "Would you like tea or coffee?",
			Options: []agentturn.RemoteOption{
				{ID: "1", Text: "Tea"},
				{ID: "2", Text: "Coffee"},
			},
		}},
	}
	done := startRemoteQuestion(t, context.Background(), m, req)

	if !m.askQuestion.Active() {
		t.Fatal("the interactive form did not open")
	}
	m.Update(testKeyPress("2", '2')) // Coffee

	select {
	case ans := <-done:
		if len(ans.Answers) != 1 {
			t.Fatalf("answers = %+v", ans.Answers)
		}
		got := ans.Answers[0]
		if got.Prompt != "Would you like tea or coffee?" || len(got.SelectedOptionIDs) != 1 || got.SelectedOptionIDs[0] != "2" {
			t.Fatalf("answer = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleRemoteInteraction did not return")
	}
}

// TestHandleRemoteQuestion_MultiSelect pins that a remote multi-select
// question keeps every picked option id instead of degrading to one.
func TestHandleRemoteQuestion_MultiSelect(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	req := agentturn.RemoteInteraction{
		Kind: agentturn.RemoteInteractionQuestion,
		Questions: []agentturn.RemoteQuestion{{
			Prompt:        "Pick tools",
			IsMultiSelect: true,
			Options: []agentturn.RemoteOption{
				{ID: "git", Text: "Git"},
				{ID: "docker", Text: "Docker"},
			},
		}},
	}
	done := startRemoteQuestion(t, context.Background(), m, req)

	m.Update(testKeyPress("1", '1')) // toggle Git
	m.Update(testKeyPress("2", '2')) // toggle Docker
	m.Update(testKeyPress("enter", tea.KeyEnter))

	select {
	case ans := <-done:
		got := ans.Answers[0]
		if strings.Join(got.SelectedOptionIDs, ",") != "git,docker" {
			t.Errorf("selectedOptionIds = %v, want [git docker]", got.SelectedOptionIDs)
		}
		if got.Skipped {
			t.Error("the multi-select answer was marked skipped")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleRemoteInteraction did not return")
	}
}

// TestHandleRemoteQuestion_WriteInAndSkip pins the write-in and skip
// paths through the remote wire answer.
func TestHandleRemoteQuestion_WriteInAndSkip(t *testing.T) {
	t.Run("write-in", func(t *testing.T) {
		m := newRootModelForSubagentQuestion(t)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		req := agentturn.RemoteInteraction{
			Kind: agentturn.RemoteInteractionQuestion,
			Questions: []agentturn.RemoteQuestion{{
				Prompt:  "Pick a drink",
				Options: []agentturn.RemoteOption{{ID: "1", Text: "Tea"}},
			}},
		}
		done := startRemoteQuestion(t, context.Background(), m, req)

		m.Update(testKeyPress("2", '2')) // write-in row
		for _, ch := range "Latte" {
			m.Update(testKeyPress(string(ch), ch))
		}
		m.Update(testKeyPress("enter", tea.KeyEnter))

		select {
		case ans := <-done:
			if got := ans.Answers[0]; got.WriteInResponse != "Latte" || len(got.SelectedOptionIDs) != 0 {
				t.Errorf("answer = %+v, want the Latte write-in", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("handleRemoteInteraction did not return")
		}
	})

	t.Run("skip", func(t *testing.T) {
		m := newRootModelForSubagentQuestion(t)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		req := agentturn.RemoteInteraction{
			Kind: agentturn.RemoteInteractionQuestion,
			Questions: []agentturn.RemoteQuestion{{
				Prompt:  "Pick a drink",
				Options: []agentturn.RemoteOption{{ID: "1", Text: "Tea"}},
			}},
		}
		done := startRemoteQuestion(t, context.Background(), m, req)

		m.Update(testKeyPress("esc", tea.KeyEsc))

		select {
		case ans := <-done:
			if !ans.Answers[0].Skipped {
				t.Errorf("answer = %+v, want skipped", ans.Answers[0])
			}
		case <-time.After(2 * time.Second):
			t.Fatal("handleRemoteInteraction did not return")
		}
	})
}

// TestHandleRemoteQuestion_ContextCancel pins that a cancelled turn closes
// the form and answers the wire with cancelled instead of blocking.
func TestHandleRemoteQuestion_ContextCancel(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := agentturn.RemoteInteraction{
		Kind: agentturn.RemoteInteractionQuestion,
		Questions: []agentturn.RemoteQuestion{{
			Prompt:  "Pick a drink",
			Options: []agentturn.RemoteOption{{ID: "1", Text: "Tea"}},
		}},
	}
	done := startRemoteQuestion(t, ctx, m, req)
	cancel()

	select {
	case ans := <-done:
		if !ans.Cancelled {
			t.Errorf("answer = %+v, want cancelled", ans)
		}
		if m.askQuestion.Active() {
			t.Error("the form is still active after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleRemoteInteraction did not return")
	}
}

// startRemoteQuestion runs the remote answer path with a send seam that
// feeds the open message to the model, then returns the result channel.
func startRemoteQuestion(t *testing.T, ctx context.Context, m *RootModel, req agentturn.RemoteInteraction) <-chan agentturn.RemoteInteractionAnswer {
	t.Helper()
	opened := make(chan struct{})
	done := make(chan agentturn.RemoteInteractionAnswer, 1)
	var closed bool
	send := func(msg tea.Msg) {
		m.Update(msg)
		if _, ok := msg.(askQuestionOpenMsg); ok && !closed {
			closed = true
			close(opened)
		}
	}
	go func() {
		ans, _ := m.answerRemoteQuestionsVia(ctx, req, send)
		done <- ans
	}()
	select {
	case <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("the interactive question never opened")
	}
	return done
}

func waitForPermissionPrompt(t *testing.T) *Prompt {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := DefaultPermissionPrompts.Active(); p != nil && p.IsOpen() {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("permission prompt was never raised")
	return nil
}

// TestQuestionPickerClaimsEscBeforeGlobalHandlers pins the modal rule: an
// open question picker closes on Esc instead of letting cli.escape cancel
// the turn, and the pending question stays pending.
func TestQuestionPickerClaimsEscBeforeGlobalHandlers(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/keybindings.json"
	if err := os.WriteFile(path, []byte(`{"cli.escape":["esc"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	kb, err := keybind.Load(path)
	if err != nil {
		t.Fatalf("keybind.Load: %v", err)
	}

	manager := subagentquestions.NewManager(false)
	question := manager.Enqueue(subagentquestions.ParentKey, "Pick one", []string{"red", "blue"})
	panel := subagentquestionpanel.New(subagentquestionpanel.Seams{Manager: manager})
	panel.Toggle()
	if !panel.Active() {
		t.Fatal("picker did not open")
	}

	m := &RootModel{}
	m.askQuestion.panel = panel
	m.prompt.keybindings = kb
	m.conversation.streaming = true
	bridge, ctx := armRunningBridge(t)
	m.turn.activeBridge = bridge

	_, _ = m.updateKeyMsg(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))

	if panel.Active() {
		t.Fatal("Esc did not close the picker")
	}
	if question.State != subagentquestions.StateSkipped {
		t.Errorf("question state = %q, want skipped (Esc skips so the wait unblocks)", question.State)
	}
	requireNotCancelled(t, ctx)
}

// TestStagedAnswerWinsOverStreamingQueue pins the answer path: while the
// turn is parked on a question, Enter consumes the composer as the answer
// instead of queueing the text for the next turn.
func TestStagedAnswerWinsOverStreamingQueue(t *testing.T) {
	manager := subagentquestions.NewManager(false)
	question := manager.Enqueue(subagentquestions.ParentKey, "Pick one", nil)

	m := &RootModel{}
	initRootModelInput(m)
	panel := subagentquestionpanel.New(subagentquestionpanel.Seams{
		Manager:    manager,
		InputValue: func() string { return m.prompt.input.Value() },
		ResetInput: func() { m.prompt.input.SetValue("") },
	})
	m.askQuestion.panel = panel
	m.askQuestion.manager = manager
	m.conversation.streaming = true

	panel.Toggle()
	if handled, _ := panel.HandleKeyMsg(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter did not stage the question")
	}
	m.prompt.input.SetValue("blue")

	m.submitPrompt()

	if question.State != subagentquestions.StateAnswered || question.Answer != "blue" {
		t.Fatalf("question = state %q answer %q, want answered/blue", question.State, question.Answer)
	}
	if m.session.store.QueuedPrompt != "" || len(m.session.store.StreamQueue) != 0 {
		t.Errorf("answer was queued instead: prompt=%q queue=%v", m.session.store.QueuedPrompt, m.session.store.StreamQueue)
	}
}

// TestRemoteSlashesSkipLocalPanels pins the routing: /permissions and
// /plugin are answered by the proxy in a --remote-agent session, not by the
// local permission editor or plugin installer.
func TestRemoteSlashesSkipLocalPanels(t *testing.T) {
	m := &RootModel{}
	m.session.remoteAgent = true
	for _, cmd := range []string{"permissions", "plugin"} {
		if m.openSlashPanelArgs(cmd, "") {
			t.Errorf("/%s opened a local panel in a remote session", cmd)
		}
	}
}
