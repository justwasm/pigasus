package permissionprompt

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// OptionFunc configures the Model.
type OptionFunc func(*Model)

// WithWidth sets the default render width of the prompt box.
func WithWidth(w int) OptionFunc {
	return func(m *Model) {
		m.width = w
	}
}

// WithOnDecision registers a callback for when a decision is resolved.
func WithOnDecision(cb func(Result)) OptionFunc {
	return func(m *Model) {
		m.onDecision = cb
	}
}

// Model is the Bubbletea component representing an interactive permission approval prompt.
type Model struct {
	req            Request
	open           bool
	selectedOption Option
	feedbackMode   bool
	feedbackText   string
	result         Result
	hasResult      bool
	width          int
	onDecision     func(Result)
}

// New creates and initializes an open permission confirmation prompt for the given request.
func New(req Request, opts ...OptionFunc) *Model {
	m := &Model{
		req:            req,
		open:           true,
		selectedOption: OptionApproveOnce,
		width:          DefaultBoxWidth,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

// IsOpen reports whether the prompt is currently active and awaiting user response.
func (m *Model) IsOpen() bool {
	return m != nil && m.open
}

// IsFeedbackMode reports whether the prompt is currently prompting for rejection feedback text.
func (m *Model) IsFeedbackMode() bool {
	return m != nil && m.feedbackMode
}

// Feedback returns the current feedback buffer.
func (m *Model) Feedback() string {
	if m == nil {
		return ""
	}
	return m.feedbackText
}

// Request returns the current Request being prompted.
func (m *Model) Request() Request {
	if m == nil {
		return Request{}
	}
	return m.req
}

// Result returns the finalized Result and whether the prompt has settled.
func (m *Model) Result() (Result, bool) {
	if m == nil {
		return Result{}, false
	}
	return m.result, m.hasResult
}

// SelectedOption returns the currently focused button option.
func (m *Model) SelectedOption() Option {
	if m == nil {
		return OptionApproveOnce
	}
	return m.selectedOption
}

// SetWidth sets the render width for the dialog.
func (m *Model) SetWidth(w int) {
	if m != nil {
		m.width = w
	}
}

// Close closes the prompt without settling a result if not already settled.
func (m *Model) Close() {
	if m != nil {
		m.open = false
	}
}

// Open opens or resets the prompt for a new request.
func (m *Model) Open(req Request) {
	if m == nil {
		return
	}
	m.req = req
	m.open = true
	m.selectedOption = OptionApproveOnce
	m.feedbackMode = false
	m.feedbackText = ""
	m.result = Result{}
	m.hasResult = false
}

// ViewString returns the rendered string representation of the prompt.
func (m *Model) ViewString() string {
	if m == nil || !m.open {
		return ""
	}
	w := m.width
	if w <= 0 {
		w = DefaultBoxWidth
	}
	return m.Render(w)
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	return tea.NewView(m.ViewString())
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil || !m.open {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		_, _ = m.HandleKey(tea.Key(msg))
	}
	return m, nil
}

// HandleKey processes a keyboard event.
// It returns the Result (if a final decision was reached) and whether the key was handled.
func (m *Model) HandleKey(key tea.Key) (Result, bool) {
	if m == nil || !m.open {
		return Result{}, false
	}

	if m.feedbackMode {
		return m.handleFeedbackKey(key)
	}
	return m.handleChoiceKey(key)
}

func (m *Model) handleFeedbackKey(key tea.Key) (Result, bool) {
	switch key.Code {
	case tea.KeyEsc:
		// Escape cancels feedback input and returns to option choice mode.
		m.feedbackMode = false
		return Result{}, true

	case tea.KeyEnter:
		// Enter submits the rejection with the provided feedback.
		fb := strings.TrimSpace(m.feedbackText)
		res := Result{
			Decision: DecisionFeedback,
			Feedback: fb,
			OptionID: m.req.MatchOptionID(DecisionFeedback),
		}
		m.complete(res)
		return res, true

	case tea.KeyBackspace:
		if len(m.feedbackText) > 0 {
			runes := []rune(m.feedbackText)
			m.feedbackText = string(runes[:len(runes)-1])
		}
		return Result{}, true

	case tea.KeyDelete:
		// Delete at end is no-op for single line append.
		return Result{}, true

	default:
		// Ctrl+U clears the typed line.
		if key.Code == 'u' && key.Mod == tea.ModCtrl {
			m.feedbackText = ""
			return Result{}, true
		}

		// Printable character input
		text := key.Text
		if text == "" && key.Code >= 32 && key.Code != 127 && (key.Mod == 0 || key.Mod == tea.ModShift) {
			text = string(key.Code)
		}
		if text != "" {
			m.feedbackText += text
			return Result{}, true
		}
	}

	return Result{}, true
}

func (m *Model) handleChoiceKey(key tea.Key) (Result, bool) {
	// 1. Direct single-key shortcuts: y, a, n, d, 1, 2, f, and Esc.
	ch := strings.ToLower(key.Text)
	if ch == "" && unicode.IsPrint(key.Code) {
		ch = strings.ToLower(string(key.Code))
	}

	// For file edits, official-docs/cli/modes.md:53-60:
	// "A pending file edit stays open until you accept or reject it. Pressing Esc doesn’t dismiss it."
	// For file creation, Esc denies file creation (official-docs/cli/modes.md:78).
	if key.Code == tea.KeyEsc {
		if m.req.Kind == KindFileEdit {
			// Esc does not dismiss a pending file edit.
			return Result{}, true
		}
		res := Result{
			Decision: DecisionDeny,
			OptionID: m.req.MatchOptionID(DecisionDeny),
		}
		m.complete(res)
		return res, true
	}

	switch {
	case ch == "y" || (ch == "1" && (m.req.Kind == KindFileEdit || m.req.Kind == KindFileCreate)):
		res := Result{
			Decision: DecisionApproveOnce,
			OptionID: m.req.MatchOptionID(DecisionApproveOnce),
		}
		m.complete(res)
		return res, true

	case ch == "a":
		res := Result{
			Decision: DecisionAlwaysAllow,
			OptionID: m.req.MatchOptionID(DecisionAlwaysAllow),
		}
		m.complete(res)
		return res, true

	case ch == "n" || (ch == "2" && (m.req.Kind == KindFileEdit || m.req.Kind == KindFileCreate)):
		res := Result{
			Decision: DecisionDeny,
			OptionID: m.req.MatchOptionID(DecisionDeny),
		}
		m.complete(res)
		return res, true

	case ch == "d":
		m.feedbackMode = true
		m.feedbackText = ""
		return Result{}, true

	case ch == "f" && (m.req.Kind == KindFileEdit || m.req.Kind == KindFileCreate):
		// "Press f to open a full-screen, scrollable diff review"
		// Handled by returning OptionID="full_diff" or feedback mode
		res := Result{
			Decision: DecisionFeedback,
			OptionID: "full_diff",
		}
		m.complete(res)
		return res, true

	// 2. Navigation keys: Left / Right / Tab / Shift+Tab to change focused option.
	case key.Code == tea.KeyLeft, key.Code == tea.KeyUp, (key.Code == tea.KeyTab && (key.Mod&tea.ModShift != 0)):
		if m.selectedOption > OptionApproveOnce {
			m.selectedOption--
		} else {
			m.selectedOption = OptionFeedback
		}
		return Result{}, true

	case key.Code == tea.KeyRight, key.Code == tea.KeyDown:
		if m.selectedOption < OptionFeedback {
			m.selectedOption++
		} else {
			m.selectedOption = OptionApproveOnce
		}
		return Result{}, true

	case key.Code == tea.KeyTab && (m.req.Kind == KindFileEdit || m.req.Kind == KindFileCreate):
		// "To reject the edit and tell the agent what to do differently, highlight No, reject this change, press Tab..."
		if m.selectedOption == OptionDeny {
			m.feedbackMode = true
			m.feedbackText = ""
			return Result{}, true
		}
		if m.selectedOption < OptionFeedback {
			m.selectedOption++
		} else {
			m.selectedOption = OptionApproveOnce
		}
		return Result{}, true

	case key.Code == tea.KeyTab:
		if m.selectedOption < OptionFeedback {
			m.selectedOption++
		} else {
			m.selectedOption = OptionApproveOnce
		}
		return Result{}, true

	// 3. Enter key activates the currently focused option.
	case key.Code == tea.KeyEnter:
		switch m.selectedOption {
		case OptionApproveOnce:
			res := Result{
				Decision: DecisionApproveOnce,
				OptionID: m.req.MatchOptionID(DecisionApproveOnce),
			}
			m.complete(res)
			return res, true
		case OptionAlwaysAllow:
			res := Result{
				Decision: DecisionAlwaysAllow,
				OptionID: m.req.MatchOptionID(DecisionAlwaysAllow),
			}
			m.complete(res)
			return res, true
		case OptionDeny:
			res := Result{
				Decision: DecisionDeny,
				OptionID: m.req.MatchOptionID(DecisionDeny),
			}
			m.complete(res)
			return res, true
		case OptionFeedback:
			m.feedbackMode = true
			m.feedbackText = ""
			return Result{}, true
		}
	}

	return Result{}, false
}

func (m *Model) complete(res Result) {
	m.result = res
	m.hasResult = true
	m.open = false
	if m.onDecision != nil {
		m.onDecision(res)
	}
}
