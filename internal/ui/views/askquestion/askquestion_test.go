package askquestion

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func key(k string) tea.KeyPressMsg {
	var run rune
	if len(k) == 1 {
		run = rune(k[0])
	}
	return tea.KeyPressMsg{
		Code: run,
		Text: k,
	}
}

func specialKey(k string) tea.KeyPressMsg {
	var code rune
	switch k {
	case "up":
		code = tea.KeyUp
	case "down":
		code = tea.KeyDown
	case "left":
		code = tea.KeyLeft
	case "right":
		code = tea.KeyRight
	case "enter":
		code = tea.KeyEnter
	case "esc", "escape":
		code = tea.KeyEsc
	case "space":
		code = tea.KeySpace
	}
	return tea.KeyPressMsg{
		Code: code,
		Text: k,
	}
}

func TestAskQuestion_SingleSelect_NumberShortcut(t *testing.T) {
	questions := []Question{
		{
			Question: "What is your favorite color?",
			Options:  []string{"Red", "Green", "Blue"},
		},
	}
	m := New(questions, 80)
	if !m.Active() {
		t.Fatal("expected model to be active")
	}

	// Press '2' to select Green
	m.Update(key("2"))

	if !m.Submitted() {
		t.Fatal("expected model to be submitted after single-select shortcut")
	}
	responses := m.Responses()
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].FormattedAnswer() != "Green" {
		t.Errorf("got %q, want %q", responses[0].FormattedAnswer(), "Green")
	}
}

func TestAskQuestion_SingleSelect_ArrowAndEnter(t *testing.T) {
	questions := []Question{
		{
			Question: "Select deployment target",
			Options:  []string{"Staging", "Production"},
		},
	}
	m := New(questions, 80)

	// Down to option 2 (Production)
	m.Update(specialKey("down"))
	st := m.CurrentState()
	if st.Cursor != 1 {
		t.Fatalf("expected cursor at 1, got %d", st.Cursor)
	}

	// Hit enter
	m.Update(specialKey("enter"))

	if !m.Submitted() {
		t.Fatal("expected model to be submitted")
	}
	res := m.Responses()
	if res[0].FormattedAnswer() != "Production" {
		t.Errorf("got %q, want %q", res[0].FormattedAnswer(), "Production")
	}
}

func TestAskQuestion_MultiSelect_SpaceAndNumbers(t *testing.T) {
	questions := []Question{
		{
			Question:      "Select features to enable",
			Options:       []string{"Auth", "Metrics", "Tracing"},
			IsMultiSelect: true,
		},
	}
	m := New(questions, 80)

	// Press '1' to toggle Auth
	m.Update(key("1"))
	// Press '3' to toggle Tracing
	m.Update(key("3"))

	// Cursor is currently at 2 (Tracing). Let's go up to Metrics (1) and press space
	m.Update(specialKey("up"))
	m.Update(specialKey("space"))

	// Hit Enter to submit
	m.Update(specialKey("enter"))

	if !m.Submitted() {
		t.Fatal("expected submitted")
	}
	res := m.Responses()
	// All three should be selected
	ans := res[0].FormattedAnswer()
	if ans != "Auth, Metrics, Tracing" {
		t.Errorf("got %q, want %q", ans, "Auth, Metrics, Tracing")
	}
}

func TestAskQuestion_WriteIn(t *testing.T) {
	questions := []Question{
		{
			Question: "Specify database name",
			Options:  []string{"postgres", "sqlite"},
		},
	}
	m := New(questions, 80)

	// Navigate to Write-in (index 2)
	m.Update(specialKey("down"))
	m.Update(specialKey("down"))
	st := m.CurrentState()
	if st.Cursor != 2 {
		t.Fatalf("expected cursor at 2, got %d", st.Cursor)
	}

	// Press Enter to activate write-in textarea
	m.Update(specialKey("enter"))
	st = m.CurrentState()
	if !st.WriteInActive {
		t.Fatal("expected WriteInActive to be true")
	}

	// Type "mysql" into textarea
	for _, ch := range "mysql" {
		m.Update(key(string(ch)))
	}

	// Hit Enter to submit write-in
	m.Update(specialKey("enter"))

	if !m.Submitted() {
		t.Fatal("expected submitted")
	}
	res := m.Responses()
	if res[0].FormattedAnswer() != "mysql" {
		t.Errorf("got %q, want %q", res[0].FormattedAnswer(), "mysql")
	}
}

func TestAskQuestion_WriteIn_Cancel(t *testing.T) {
	questions := []Question{
		{
			Question: "Specify database name",
			Options:  []string{"postgres", "sqlite"},
		},
	}
	m := New(questions, 80)

	// Jump to write-in via number key '3'
	m.Update(key("3"))
	st := m.CurrentState()
	if !st.WriteInActive {
		t.Fatal("expected WriteInActive to be true")
	}

	// Type something and cancel with Esc
	m.Update(key("a"))
	m.Update(specialKey("esc"))

	st = m.CurrentState()
	if st.WriteInActive {
		t.Fatal("expected WriteInActive to be false after Esc")
	}
	if m.Submitted() {
		t.Fatal("model should not be submitted on cancel write-in")
	}
}

func TestAskQuestion_MultiQuestion_Paging(t *testing.T) {
	questions := []Question{
		{
			Question: "First question",
			Options:  []string{"A", "B"},
		},
		{
			Question: "Second question",
			Options:  []string{"X", "Y"},
		},
	}
	m := New(questions, 80)

	// Question 1: choose A
	m.Update(key("1"))

	// Should now be on question 2!
	if m.CurrentQuestionIndex() != 1 {
		t.Fatalf("expected question index 1, got %d", m.CurrentQuestionIndex())
	}
	if m.Submitted() {
		t.Fatal("should not be submitted yet")
	}

	// Go back to question 1 via Left
	m.Update(specialKey("left"))
	if m.CurrentQuestionIndex() != 0 {
		t.Fatalf("expected question index 0 after Left, got %d", m.CurrentQuestionIndex())
	}
	// Verify state preserved: SelectedOption is 0 (A)
	if m.CurrentState().SelectedOption != 0 {
		t.Errorf("expected selected option 0, got %d", m.CurrentState().SelectedOption)
	}

	// Go back to question 2 via Right
	m.Update(specialKey("right"))
	if m.CurrentQuestionIndex() != 1 {
		t.Fatalf("expected question index 1 after Right, got %d", m.CurrentQuestionIndex())
	}

	// Question 2: choose Y (2)
	m.Update(key("2"))

	if !m.Submitted() {
		t.Fatal("expected submitted after last question")
	}
	res := m.Responses()
	if len(res) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(res))
	}
	if res[0].FormattedAnswer() != "A" {
		t.Errorf("res[0] = %q, want A", res[0].FormattedAnswer())
	}
	if res[1].FormattedAnswer() != "Y" {
		t.Errorf("res[1] = %q, want Y", res[1].FormattedAnswer())
	}
}

func TestAskQuestion_Skip(t *testing.T) {
	questions := []Question{
		{
			Question: "Optional question",
			Options:  []string{"Opt1", "Opt2"},
		},
	}
	m := New(questions, 80)

	// Press Esc to skip
	m.Update(specialKey("esc"))

	if !m.Submitted() {
		t.Fatal("expected submitted")
	}
	res := m.Responses()
	if !res[0].Skipped {
		t.Error("expected question to be marked skipped")
	}
	if res[0].FormattedAnswer() != "User Skipped" {
		t.Errorf("got %q, want %q", res[0].FormattedAnswer(), "User Skipped")
	}
}

func TestAskQuestion_ViewRendering(t *testing.T) {
	questions := []Question{
		{
			Question:      "Pick options",
			Options:       []string{"Alpha", "Beta"},
			IsMultiSelect: true,
		},
		{
			Question: "Single choice",
			Options:  []string{"Yes", "No"},
		},
	}
	m := New(questions, 80)
	view := m.Render()

	if !strings.Contains(view, "Question 1/2: Pick options") {
		t.Errorf("expected header in view, got: %s", view)
	}
	if !strings.Contains(view, "1. [ ] Alpha") {
		t.Errorf("expected first row with [ ], got: %s", view)
	}
	if !strings.Contains(view, "2. [ ] Beta") {
		t.Errorf("expected second row with [ ], got: %s", view)
	}
	if !strings.Contains(view, "3. Write-in...") {
		t.Errorf("expected write-in row without brackets, got: %s", view)
	}
	if strings.Contains(view, "3. [ ]") {
		t.Errorf("write-in row should not have [ ] checkbox, got: %s", view)
	}
	if !strings.Contains(view, "Navigate") || !strings.Contains(view, "Toggle") || !strings.Contains(view, "Skip") {
		t.Errorf("expected nav hints, got: %s", view)
	}

	// Move to single choice question
	m.Update(specialKey("right"))
	view2 := m.Render()

	if !strings.Contains(view2, "Question 2/2: Single choice") {
		t.Errorf("expected header for question 2, got: %s", view2)
	}
	if strings.Contains(view2, "( )") || strings.Contains(view2, "(*)") {
		t.Errorf("single choice question should not contain parentheses, got: %s", view2)
	}
	if !strings.Contains(view2, "1. Yes") || !strings.Contains(view2, "2. No") {
		t.Errorf("expected single choice options without parentheses, got: %s", view2)
	}
	if !strings.Contains(view2, "enter Select") {
		t.Errorf("expected 'enter Select' hint for single choice, got: %s", view2)
	}
}
