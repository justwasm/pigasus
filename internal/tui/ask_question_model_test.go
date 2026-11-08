package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/ui/views/askquestion"
)

// TestAskQuestionModel_OwnsManagerAndPanel pins the ownership move: the
// pending-question manager and its picker hang off AskQuestionModel.
func TestAskQuestionModel_OwnsManagerAndPanel(t *testing.T) {
	m := diffTestModel()
	if m.askQuestion.manager != nil || m.askQuestion.panel != nil {
		t.Fatal("questions exist before construction")
	}
	m.askQuestion.manager = subagentquestions.NewManager(false)
	m.askQuestion.panel = m.newSubAgentQuestionPanel()
	if m.askQuestion.manager == nil || m.askQuestion.panel == nil {
		t.Fatal("the model did not keep the manager and panel")
	}
	if m.askQuestion.panel.Active() {
		t.Fatal("fresh picker is active")
	}
}

func testKeyPress(k string, code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{
		Code: code,
		Text: k,
	}
}

func TestAskQuestion_TUIInteraction_SingleSelect_KeyboardFlow(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	replyCh := make(chan []askquestion.Response, 1)

	// Send askQuestionOpenMsg
	m.Update(askQuestionOpenMsg{
		questions: []askquestion.Question{
			{
				Question: "Which database?",
				Options:  []string{"PostgreSQL", "SQLite", "MySQL"},
			},
		},
		replyCh: replyCh,
	})

	if !m.askQuestion.Active() {
		t.Fatal("expected askQuestion to be active in input area")
	}

	// Verify view replaces composer
	body, _ := m.viewBody(nil)
	if !strings.Contains(body, "Which database?") {
		t.Fatalf("expected question in viewBody, got:\n%s", body)
	}
	if !strings.Contains(body, "PostgreSQL") || !strings.Contains(body, "SQLite") {
		t.Fatalf("expected options in viewBody, got:\n%s", body)
	}

	// Press '2' (SQLite)
	m.Update(testKeyPress("2", '2'))

	select {
	case resps := <-replyCh:
		if len(resps) != 1 {
			t.Fatalf("expected 1 response, got %d", len(resps))
		}
		if resps[0].FormattedAnswer() != "SQLite" {
			t.Errorf("got %q, want SQLite", resps[0].FormattedAnswer())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reply")
	}

	if m.askQuestion.Active() {
		t.Error("expected askQuestion to be inactive after submission")
	}
}

func TestAskQuestion_TUIInteraction_MultiSelect_SpaceAndEnter(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	replyCh := make(chan []askquestion.Response, 1)

	m.Update(askQuestionOpenMsg{
		questions: []askquestion.Question{
			{
				Question:      "Select tools",
				Options:       []string{"Git", "Docker", "Node"},
				IsMultiSelect: true,
			},
		},
		replyCh: replyCh,
	})

	if !m.askQuestion.Active() {
		t.Fatal("expected askQuestion to be active")
	}

	// Press '1' to toggle Git, and '3' to toggle Node
	m.Update(testKeyPress("1", '1'))
	m.Update(testKeyPress("3", '3'))

	// Press Enter to submit
	m.Update(testKeyPress("enter", tea.KeyEnter))

	select {
	case resps := <-replyCh:
		if len(resps) != 1 {
			t.Fatalf("expected 1 response, got %d", len(resps))
		}
		ans := resps[0].FormattedAnswer()
		if ans != "Git, Node" {
			t.Errorf("got %q, want 'Git, Node'", ans)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reply")
	}
}

func TestAskQuestion_TUIInteraction_WriteIn(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	replyCh := make(chan []askquestion.Response, 1)

	m.Update(askQuestionOpenMsg{
		questions: []askquestion.Question{
			{
				Question: "Pick color",
				Options:  []string{"Red", "Blue"},
			},
		},
		replyCh: replyCh,
	})

	// Press '3' to jump to Write-in
	m.Update(testKeyPress("3", '3'))

	// Type "Magenta"
	for _, ch := range "Magenta" {
		m.Update(testKeyPress(string(ch), ch))
	}

	// Hit enter to submit write-in
	m.Update(testKeyPress("enter", tea.KeyEnter))

	select {
	case resps := <-replyCh:
		if len(resps) != 1 {
			t.Fatalf("expected 1 response, got %d", len(resps))
		}
		if resps[0].FormattedAnswer() != "Magenta" {
			t.Errorf("got %q, want Magenta", resps[0].FormattedAnswer())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reply")
	}
}

func TestAskQuestion_TUIInteraction_EscSkips(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	replyCh := make(chan []askquestion.Response, 1)

	m.Update(askQuestionOpenMsg{
		questions: []askquestion.Question{
			{
				Question: "Optional question",
				Options:  []string{"Yes", "No"},
			},
		},
		replyCh: replyCh,
	})

	// Hit Esc to skip
	m.Update(testKeyPress("esc", tea.KeyEsc))

	select {
	case resps := <-replyCh:
		if len(resps) != 1 {
			t.Fatalf("expected 1 response, got %d", len(resps))
		}
		if !resps[0].Skipped || resps[0].FormattedAnswer() != "User Skipped" {
			t.Errorf("got %+v, want User Skipped", resps[0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reply")
	}
}

// TestAskQuestion_HidesTheComposerCursor pins the view contract: while
// the question form replaces the composer the real terminal cursor is
// hidden (the write-in textarea draws its own), and the cached view is
// not reusable.
func TestAskQuestion_HidesTheComposerCursor(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	replyCh := make(chan []askquestion.Response, 1)
	m.Update(askQuestionOpenMsg{
		questions: []askquestion.Question{{
			Question: "Which database?",
			Options:  []string{"PostgreSQL", "SQLite"},
		}},
		replyCh: replyCh,
	})

	if v := m.View(); v.Cursor != nil {
		t.Fatalf("cursor = %+v, want nil while the question replaces the composer", v.Cursor)
	}
	if m.isViewCacheValid() {
		t.Error("the view cache is valid while the question is active")
	}

	m.Update(testKeyPress("3", '3')) // write-in row
	if v := m.View(); v.Cursor != nil {
		t.Errorf("cursor = %+v, want nil in the write-in state", v.Cursor)
	}
}

// TestAskQuestion_PasteGoesToWriteIn pins the paste route: PasteMsg
// reaches the write-in textarea and never the hidden composer.
func TestAskQuestion_PasteGoesToWriteIn(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	replyCh := make(chan []askquestion.Response, 1)
	m.Update(askQuestionOpenMsg{
		questions: []askquestion.Question{{
			Question: "Pick a colour",
			Options:  []string{"Red", "Blue"},
		}},
		replyCh: replyCh,
	})

	m.Update(testKeyPress("3", '3')) // write-in row
	m.Update(tea.PasteMsg{Content: "hello"})

	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("the hidden composer received the paste: %q", got)
	}

	m.Update(testKeyPress("enter", tea.KeyEnter))
	select {
	case resps := <-replyCh:
		if len(resps) != 1 || resps[0].FormattedAnswer() != "hello" {
			t.Errorf("responses = %+v, want hello", resps)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the pasted answer")
	}
}

// TestAskQuestion_CtrlCPreservesAnsweredQuestions pins the dismissal
// path: Ctrl+C keeps the answers already given and marks the rest
// skipped instead of reporting every question as skipped.
func TestAskQuestion_CtrlCPreservesAnsweredQuestions(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	replyCh := make(chan []askquestion.Response, 1)
	m.Update(askQuestionOpenMsg{
		questions: []askquestion.Question{
			{Question: "One?", Options: []string{"A", "B"}},
			{Question: "Two?", Options: []string{"C", "D"}},
		},
		replyCh: replyCh,
	})

	m.Update(testKeyPress("1", '1')) // answer Q1 = A, advance to Q2
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))

	select {
	case resps := <-replyCh:
		if len(resps) != 2 {
			t.Fatalf("responses = %d, want 2", len(resps))
		}
		if got := resps[0].FormattedAnswer(); got != "A" {
			t.Errorf("Q1 = %q, want A", got)
		}
		if !resps[1].Skipped {
			t.Errorf("Q2 = %+v, want skipped", resps[1])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the cancelled form")
	}
	if m.askQuestion.Active() {
		t.Error("the question form is still active after Ctrl+C")
	}
}

// TestAskQuestion_ResizeUpdatesTheFormWidth pins that a terminal resize
// reaches the question model instead of leaving the old layout behind.
func TestAskQuestion_ResizeUpdatesTheFormWidth(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	replyCh := make(chan []askquestion.Response, 1)
	m.Update(askQuestionOpenMsg{
		questions: []askquestion.Question{{
			Question: "Which database?",
			Options:  []string{"PostgreSQL", "SQLite"},
		}},
		replyCh: replyCh,
	})

	if got := m.askQuestion.model.Width(); got != 80 {
		t.Fatalf("width = %d, want 80", got)
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if got := m.askQuestion.model.Width(); got != 120 {
		t.Errorf("width after resize = %d, want 120", got)
	}
}

// TestAskQuestion_SecondRequestDismissesTheFirst pins that a second
// ask_question cannot orphan the first caller: the open form is
// dismissed with its partial answers and the new one takes over.
func TestAskQuestion_SecondRequestDismissesTheFirst(t *testing.T) {
	m := newRootModelForSubagentQuestion(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	firstCh := make(chan []askquestion.Response, 1)
	m.Update(askQuestionOpenMsg{
		questions: []askquestion.Question{
			{Question: "One?", Options: []string{"A", "B"}},
			{Question: "Two?", Options: []string{"C", "D"}},
		},
		replyCh: firstCh,
	})
	m.Update(testKeyPress("1", '1')) // answer Q1 = A

	secondCh := make(chan []askquestion.Response, 1)
	m.Update(askQuestionOpenMsg{
		questions: []askquestion.Question{{Question: "Three?", Options: []string{"E", "F"}}},
		replyCh:   secondCh,
	})

	select {
	case resps := <-firstCh:
		if len(resps) != 2 || resps[0].FormattedAnswer() != "A" || !resps[1].Skipped {
			t.Errorf("the dismissed form returned %+v, want A + skipped", resps)
		}
	default:
		t.Fatal("the first caller was orphaned")
	}
	if !m.askQuestion.Active() {
		t.Error("the second question is not active")
	}
}
