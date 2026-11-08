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
