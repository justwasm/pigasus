package askquestion

import (
	"fmt"
	"strings"

	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/justwasm/pigasus/palette"
)

// Question is a single question with options presented to the user.
type Question struct {
	Question      string   `json:"question"`
	Options       []string `json:"options"`
	IsMultiSelect bool     `json:"is_multi_select"`
}

// QuestionState tracks the navigation, selection, and write-in state for one question.
type QuestionState struct {
	Cursor          int          // focused option row: 0..len(Options)
	SelectedOption  int          // -1 if unselected, len(Options) if write-in selected
	SelectedOptions map[int]bool // for multi-select
	WriteInText     string       // custom typed answer
	WriteInActive   bool         // whether textarea is currently focused for typing
	Skipped         bool         // whether user skipped this question via Esc
}

// Response records the user's final answer to a Question.
type Response struct {
	Question        string
	SelectedOptions []string
	WriteInText     string
	Skipped         bool
}

// FormattedAnswer returns the human-readable string answer for tool output.
func (r Response) FormattedAnswer() string {
	if r.Skipped {
		return "User Skipped"
	}
	var parts []string
	parts = append(parts, r.SelectedOptions...)
	if r.WriteInText != "" {
		parts = append(parts, r.WriteInText)
	}
	if len(parts) == 0 {
		return "User Skipped"
	}
	return strings.Join(parts, ", ")
}

// Model is the Bubble Tea component for dynamic interactive questions.
// It aligns 1:1 with jetski's AskQuestionModel (google3/third_party/jetski/cli/model/model.AskQuestionModel).
type Model struct {
	questions       []Question
	states          []QuestionState
	currentQuestion int
	input           tas.Model
	width           int
	active          bool
	submitted       bool
	responses       []Response
	onComplete      func([]Response)
}

// New constructs an AskQuestionModel initialized with questions.
func New(questions []Question, width int) *Model {
	if width <= 0 {
		width = 80
	}
	m := &Model{
		questions: questions,
		width:     width,
		active:    len(questions) > 0,
	}
	m.initTextarea()
	m.resetStates()
	return m
}

func (m *Model) initTextarea() {
	m.input = tas.New()
	m.input.Prompt = ""
	m.input.ShowLineNumbers = false
	w := m.width - 4
	if w < 20 {
		w = 76
	}
	m.input.SetWidth(w)
	m.input.SetHeight(3)

	// Clean styling: no black background, no hardcoded colors, transparent background
	// so the input is crystal clear across all terminal backgrounds and themes.
	st := m.input.Styles()
	st.Focused.Base = lipgloss.NewStyle()
	st.Focused.CursorLine = lipgloss.NewStyle() // Transparent background, no Background(0) black box
	st.Focused.Text = lipgloss.NewStyle()       // Default terminal foreground
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	st.Focused.EndOfBuffer = lipgloss.NewStyle()
	st.Blurred.Base = lipgloss.NewStyle()
	st.Blurred.CursorLine = lipgloss.NewStyle()
	st.Blurred.Text = lipgloss.NewStyle()
	st.Blurred.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	st.Blurred.EndOfBuffer = lipgloss.NewStyle()
	m.input.SetStyles(st)
}

func (m *Model) resetStates() {
	m.states = make([]QuestionState, len(m.questions))
	for i := range m.questions {
		m.states[i] = QuestionState{
			Cursor:          0,
			SelectedOption:  -1,
			SelectedOptions: make(map[int]bool),
		}
	}
	m.currentQuestion = 0
	m.syncWriteInInput()
}

// SetQuestions resets the model with a new set of questions.
func (m *Model) SetQuestions(questions []Question) {
	m.questions = questions
	m.active = len(questions) > 0
	m.submitted = false
	m.responses = nil
	m.resetStates()
}

// SetOnComplete registers a callback invoked when the user finishes answering all questions.
func (m *Model) SetOnComplete(cb func([]Response)) {
	m.onComplete = cb
}

// SetWidth updates the viewport width and resizes the embedded textarea.
func (m *Model) SetWidth(w int) {
	m.width = w
	if w > 4 {
		m.input.SetWidth(w - 4)
	}
}

// Active reports whether the question prompt is currently open and accepting input.
func (m *Model) Active() bool {
	return m != nil && m.active && !m.submitted && len(m.questions) > 0
}

// Submitted reports whether all questions have been answered or skipped.
func (m *Model) Submitted() bool {
	return m != nil && m.submitted
}

// Responses returns the collected responses after submission.
func (m *Model) Responses() []Response {
	if m == nil {
		return nil
	}
	return m.responses
}

// CurrentQuestionIndex returns the index of the question currently being answered (0-indexed).
func (m *Model) CurrentQuestionIndex() int {
	if m == nil {
		return 0
	}
	return m.currentQuestion
}

// CurrentState returns the state of the question currently in focus.
func (m *Model) CurrentState() QuestionState {
	if m == nil || m.currentQuestion < 0 || m.currentQuestion >= len(m.states) {
		return QuestionState{}
	}
	return m.states[m.currentQuestion]
}

func (m *Model) syncWriteInInput() {
	if m.currentQuestion < 0 || m.currentQuestion >= len(m.states) {
		return
	}
	st := &m.states[m.currentQuestion]
	m.input.Reset()
	m.input.SetValue(st.WriteInText)
	if st.WriteInActive {
		m.input.Focus()
	} else {
		m.input.Blur()
	}
}

func (m *Model) advanceOrSubmit() tea.Cmd {
	if m.currentQuestion+1 < len(m.questions) {
		m.currentQuestion++
		m.syncWriteInInput()
		return nil
	}
	m.submitted = true
	m.active = false
	m.responses = m.buildResponses()
	if m.onComplete != nil {
		m.onComplete(m.responses)
	}
	return nil
}

func (m *Model) buildResponses() []Response {
	res := make([]Response, len(m.questions))
	for i, q := range m.questions {
		st := m.states[i]
		r := Response{
			Question: q.Question,
			Skipped:  st.Skipped,
		}
		if !st.Skipped {
			if q.IsMultiSelect {
				for optIdx, opt := range q.Options {
					if st.SelectedOptions[optIdx] {
						r.SelectedOptions = append(r.SelectedOptions, opt)
					}
				}
				if st.WriteInText != "" {
					r.WriteInText = st.WriteInText
				}
				if len(r.SelectedOptions) == 0 && r.WriteInText == "" {
					r.Skipped = true
				}
			} else {
				if st.SelectedOption == len(q.Options) {
					if st.WriteInText != "" {
						r.WriteInText = st.WriteInText
					} else {
						r.Skipped = true
					}
				} else if st.SelectedOption >= 0 && st.SelectedOption < len(q.Options) {
					r.SelectedOptions = []string{q.Options[st.SelectedOption]}
				} else {
					r.Skipped = true
				}
			}
		}
		res[i] = r
	}
	return res
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles keyboard navigation, selection, write-in editing, and question paging.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !m.Active() {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetWidth(msg.Width)
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKeyPress(tea.Key(msg), msg)
	}

	return m, nil
}

func (m *Model) handleKeyPress(key tea.Key, origMsg tea.Msg) (tea.Model, tea.Cmd) {
	q := m.questions[m.currentQuestion]
	st := &m.states[m.currentQuestion]
	totalOptions := len(q.Options) + 1 // including Write-in
	keyStr := key.String()

	// When WriteInActive: user is typing inside the embedded textarea
	if st.WriteInActive {
		switch keyStr {
		case "enter":
			st.WriteInActive = false
			st.WriteInText = strings.TrimSpace(m.input.Value())
			m.input.Blur()
			if !q.IsMultiSelect {
				st.SelectedOption = len(q.Options)
			}
			cmd := m.advanceOrSubmit()
			return m, cmd
		case "esc", "escape":
			st.WriteInActive = false
			m.input.SetValue(st.WriteInText)
			m.input.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(origMsg)
			return m, cmd
		}
	}

	// When WriteInActive is false: list navigation & selection
	switch keyStr {
	case "up", "k":
		if st.Cursor > 0 {
			st.Cursor--
		}
		return m, nil

	case "down", "j":
		if st.Cursor < totalOptions-1 {
			st.Cursor++
		}
		return m, nil

	case "left", "h":
		if m.currentQuestion > 0 {
			m.currentQuestion--
			m.syncWriteInInput()
		}
		return m, nil

	case "right", "l":
		if m.currentQuestion < len(m.questions)-1 {
			m.currentQuestion++
			m.syncWriteInInput()
		}
		return m, nil

	case "enter":
		if st.Cursor == len(q.Options) {
			st.WriteInActive = true
			m.input.Focus()
			return m, nil
		}
		if !q.IsMultiSelect {
			st.SelectedOption = st.Cursor
		}
		cmd := m.advanceOrSubmit()
		return m, cmd

	case "esc", "escape":
		st.Skipped = true
		st.SelectedOption = -1
		st.SelectedOptions = make(map[int]bool)
		st.WriteInText = ""
		cmd := m.advanceOrSubmit()
		return m, cmd

	default:
		// Space or 'x' toggles checkbox in multi-select mode
		if key.Code == tea.KeySpace || keyStr == "space" || keyStr == " " || keyStr == "x" {
			if q.IsMultiSelect {
				if st.Cursor < len(q.Options) {
					st.SelectedOptions[st.Cursor] = !st.SelectedOptions[st.Cursor]
				} else {
					st.WriteInActive = true
					m.input.Focus()
				}
			}
			return m, nil
		}

		// Number keys 1..9
		if len(keyStr) == 1 && keyStr[0] >= '1' && keyStr[0] <= '9' {
			choice := int(keyStr[0] - '1')
			if choice >= 0 && choice < totalOptions {
				st.Cursor = choice
				if choice == len(q.Options) {
					st.WriteInActive = true
					m.input.Focus()
					return m, nil
				}
				if q.IsMultiSelect {
					st.SelectedOptions[choice] = !st.SelectedOptions[choice]
					return m, nil
				}
				st.SelectedOption = choice
				cmd := m.advanceOrSubmit()
				return m, cmd
			}
		}
	}

	return m, nil
}

func writeInDisplayLabel(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Write-in..."
	}
	text = strings.ReplaceAll(text, "\n", " ")
	runes := []rune(text)
	if len(runes) > 40 {
		return string(runes[:40]) + "..."
	}
	return text
}

// Render renders the ask_question UI aligned 1:1 with jetski.
func (m *Model) Render() string {
	if !m.Active() {
		return ""
	}
	q := m.questions[m.currentQuestion]
	st := m.states[m.currentQuestion]

	var b strings.Builder

	// Header: Question 1/1: <Question text>
	header := fmt.Sprintf("Question %d/%d: %s", m.currentQuestion+1, len(m.questions), q.Question)
	b.WriteString(palette.BoldOpen + header + palette.BoldClose)
	b.WriteString("\n\n")

	// Option rows
	totalOptions := len(q.Options) + 1
	for i := 0; i < totalOptions; i++ {
		isFocused := (i == st.Cursor)
		numStr := fmt.Sprintf("%d. ", i+1)

		if i < len(q.Options) {
			if q.IsMultiSelect {
				checkStr := "[ ] "
				if st.SelectedOptions[i] {
					checkStr = "[x] "
				}
				rowText := numStr + checkStr + q.Options[i]
				if isFocused {
					b.WriteString(palette.BlueOpen + "> " + rowText + palette.BlueClose + "\n")
				} else {
					b.WriteString("  " + rowText + "\n")
				}
			} else {
				// Single select: no parentheses at all!
				rowText := numStr + q.Options[i]
				if isFocused {
					b.WriteString(palette.BlueOpen + "> " + rowText + palette.BlueClose + "\n")
				} else {
					b.WriteString("  " + rowText + "\n")
				}
			}
		} else {
			// Write-in row (no checkbox)
			label := writeInDisplayLabel(st.WriteInText)
			rowText := numStr + label
			if isFocused {
				b.WriteString(palette.BlueOpen + "> " + rowText + palette.BlueClose + "\n")
			} else {
				b.WriteString("  " + rowText + "\n")
			}

			if st.WriteInActive {
				b.WriteString("\n")
				b.WriteString(palette.DimGrayOpen + "  Your answer:" + palette.DimGrayClose + "\n")
				lines := strings.Split(m.input.View(), "\n")
				for _, line := range lines {
					b.WriteString("  " + line + "\n")
				}
			}
		}
	}

	b.WriteString("\n")

	// Bottom navigation hints
	if st.WriteInActive {
		hint := "enter Submit · esc Back"
		if m.currentQuestion < len(m.questions)-1 {
			hint = "enter Next · esc Back"
		}
		b.WriteString(palette.DimGrayOpen + "  " + hint + palette.DimGrayClose + "\n")
	} else {
		var hints []string
		hints = append(hints, "↑/↓ Navigate")
		if len(m.questions) > 1 {
			hints = append(hints, "←/→ Switch Question")
		}
		if q.IsMultiSelect {
			hints = append(hints, "space Toggle")
		}
		if st.Cursor == len(q.Options) {
			hints = append(hints, "enter Write-in")
		} else {
			if q.IsMultiSelect {
				if m.currentQuestion == len(m.questions)-1 {
					hints = append(hints, "enter Submit")
				} else {
					hints = append(hints, "enter Next")
				}
			} else {
				if m.currentQuestion == len(m.questions)-1 {
					hints = append(hints, "enter Select")
				} else {
					hints = append(hints, "enter Next")
				}
			}
		}
		hints = append(hints, "esc Skip")

		hintLine := palette.DimGrayOpen + "  " + strings.Join(hints, " · ") + palette.DimGrayClose
		b.WriteString(hintLine + "\n")
	}

	return b.String()
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	return tea.NewView(m.Render())
}
