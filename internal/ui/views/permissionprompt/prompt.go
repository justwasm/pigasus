package permissionprompt

import (
	"fmt"
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
	selectedIndex  int
	selectedOption Option
	feedbackMode   bool
	feedbackText   string
	amendMode      bool
	amendText      string
	expandedView   bool
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
		selectedIndex:  0,
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

// IsAmendMode reports whether the prompt is currently in command amend mode.
func (m *Model) IsAmendMode() bool {
	return m != nil && m.amendMode
}

// AmendText returns the current amended command buffer.
func (m *Model) AmendText() string {
	if m == nil {
		return ""
	}
	return m.amendText
}

// IsExpandedView reports whether the prompt is in expanded diff or full command view.
func (m *Model) IsExpandedView() bool {
	return m != nil && m.expandedView
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
	choices := m.Choices()
	if m.selectedIndex >= 0 && m.selectedIndex < len(choices) {
		switch choices[m.selectedIndex].Decision {
		case DecisionApproveOnce:
			return OptionApproveOnce
		case DecisionAlwaysAllow:
			return OptionAlwaysAllow
		case DecisionDeny:
			return OptionDeny
		case DecisionFeedback:
			return OptionFeedback
		}
	}
	return m.selectedOption
}

// SelectedIndex returns the index of the currently focused choice.
func (m *Model) SelectedIndex() int {
	if m == nil {
		return 0
	}
	return m.selectedIndex
}

// Choices returns the active list of numbered options for the prompt.
func (m *Model) Choices() []Choice {
	if m == nil {
		return nil
	}
	if len(m.req.Options) > 0 {
		choices := make([]Choice, len(m.req.Options))
		for i, opt := range m.req.Options {
			dec := DecisionApproveOnce
			switch opt.Kind {
			case "allow_once", "approve_once", "once":
				dec = DecisionApproveOnce
			case "allow_always", "always_allow", "always":
				dec = DecisionAlwaysAllow
			case "reject", "deny":
				dec = DecisionDeny
			case "reject_with_feedback", "feedback":
				dec = DecisionFeedback
			default:
				if i == 0 {
					dec = DecisionApproveOnce
				} else {
					dec = DecisionDeny
				}
			}
			choices[i] = Choice{
				Number:   i + 1,
				Title:    opt.Title,
				Decision: dec,
				OptionID: opt.ID,
			}
		}
		return choices
	}

	switch m.req.Kind {
	case KindFileEdit:
		choices := []Choice{
			{Number: 1, Title: "Yes, accept this change", Decision: DecisionApproveOnce, OptionID: m.req.MatchOptionID(DecisionApproveOnce)},
			{Number: 2, Title: "No, reject this change", Decision: DecisionDeny, OptionID: m.req.MatchOptionID(DecisionDeny)},
		}
		if m.req.OutsideWS {
			choices = append(choices, Choice{
				Number:   len(choices) + 1,
				Title:    "Yes, and always allow non-workspace access",
				Decision: DecisionAlwaysAllow,
				OptionID: m.req.MatchOptionID(DecisionAlwaysAllow),
			})
		}
		return choices

	case KindFileCreate:
		choices := []Choice{
			{Number: 1, Title: "Yes, allow creation", Decision: DecisionApproveOnce, OptionID: m.req.MatchOptionID(DecisionApproveOnce)},
			{Number: 2, Title: "No, deny creation", Decision: DecisionDeny, OptionID: m.req.MatchOptionID(DecisionDeny)},
		}
		if m.req.OutsideWS {
			choices = append(choices, Choice{
				Number:   len(choices) + 1,
				Title:    "Yes, and always allow non-workspace access",
				Decision: DecisionAlwaysAllow,
				OptionID: m.req.MatchOptionID(DecisionAlwaysAllow),
			})
		}
		return choices

	default:
		cmdTitle := "run command"
		if m.req.Tool != "" && m.req.Tool != "run_command" && m.req.Tool != "run_shell_command" && m.req.CommandStr() == "" {
			cmdTitle = fmt.Sprintf("run %s", m.req.Tool)
		}
		cmd := m.req.CommandStr()
		prefix := ""
		if fields := strings.Fields(cmd); len(fields) > 0 {
			prefix = fields[0]
			if slash := strings.LastIndex(prefix, "/"); slash >= 0 {
				prefix = prefix[slash+1:]
			}
		}
		choices := []Choice{
			{Number: 1, Title: fmt.Sprintf("Yes, %s", cmdTitle), Decision: DecisionApproveOnce, OptionID: m.req.MatchOptionID(DecisionApproveOnce)},
		}
		if prefix != "" {
			n := len(choices)
			choices = append(choices,
				Choice{
					Number:   n + 1,
					Title:    fmt.Sprintf("Yes, and always allow in this conversation for commands that start with '%s'", prefix),
					Decision: DecisionAlwaysAllow,
					OptionID: m.req.MatchOptionID(DecisionAlwaysAllow),
				},
				Choice{
					Number:   n + 2,
					Title:    fmt.Sprintf("Yes, and always allow for commands that start with '%s' (Persist to settings.json)", prefix),
					Decision: DecisionAlwaysAllow,
					OptionID: m.req.MatchOptionID(DecisionAlwaysAllow),
				},
			)
		}
		choices = append(choices, Choice{
			Number:   len(choices) + 1,
			Title:    "No, cancel",
			Decision: DecisionDeny,
			OptionID: m.req.MatchOptionID(DecisionDeny),
		})
		return choices
	}
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
	m.selectedIndex = 0
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
	if m.amendMode {
		return m.handleAmendKey(key)
	}
	return m.handleChoiceKey(key)
}

func (m *Model) handleAmendKey(key tea.Key) (Result, bool) {
	switch key.Code {
	case tea.KeyEsc:
		m.amendMode = false
		return Result{}, true

	case tea.KeyEnter:
		amended := strings.TrimSpace(m.amendText)
		if amended == "" {
			amended = m.req.CommandStr()
		}
		res := Result{
			Decision:       DecisionApproveOnce,
			OptionID:       m.req.MatchOptionID(DecisionApproveOnce),
			AmendedCommand: amended,
		}
		m.complete(res)
		return res, true

	case tea.KeyBackspace:
		if len(m.amendText) > 0 {
			runes := []rune(m.amendText)
			m.amendText = string(runes[:len(runes)-1])
		}
		return Result{}, true

	case tea.KeyDelete:
		return Result{}, true

	default:
		if key.Code == 'u' && key.Mod == tea.ModCtrl {
			m.amendText = ""
			return Result{}, true
		}
		text := key.Text
		if text == "" && key.Code >= 32 && key.Code != 127 && (key.Mod == 0 || key.Mod == tea.ModShift) {
			text = string(key.Code)
		}
		if text != "" {
			m.amendText += text
			return Result{}, true
		}
	}
	return Result{}, true
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
	choices := m.Choices()
	if len(choices) == 0 {
		return Result{}, false
	}

	ch := strings.ToLower(key.Text)
	if ch == "" && unicode.IsPrint(key.Code) {
		ch = strings.ToLower(string(key.Code))
	}

	// For file edits, official-docs/cli/modes.md:53-60:
	// "A pending file edit stays open until you accept or reject it. Pressing Esc doesn’t dismiss it."
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

	// 1. Direct number shortcuts: 1..9
	if len(ch) == 1 && ch[0] >= '1' && ch[0] <= '9' {
		idx := int(ch[0] - '1')
		if idx < len(choices) {
			targetChoice := choices[idx]
			if targetChoice.Decision == DecisionFeedback {
				m.feedbackMode = true
				m.feedbackText = ""
				return Result{}, true
			}
			res := Result{
				Decision: targetChoice.Decision,
				OptionID: targetChoice.OptionID,
			}
			m.complete(res)
			return res, true
		}
	}

	// 2. Direct letter shortcuts: y, a, n, d, f
	switch ch {
	case "y":
		// Approve once
		res := Result{
			Decision: DecisionApproveOnce,
			OptionID: m.req.MatchOptionID(DecisionApproveOnce),
		}
		m.complete(res)
		return res, true

	case "a":
		// Always allow
		res := Result{
			Decision: DecisionAlwaysAllow,
			OptionID: m.req.MatchOptionID(DecisionAlwaysAllow),
		}
		m.complete(res)
		return res, true

	case "n":
		// Deny / Cancel
		res := Result{
			Decision: DecisionDeny,
			OptionID: m.req.MatchOptionID(DecisionDeny),
		}
		m.complete(res)
		return res, true

	case "d":
		// Enter feedback mode
		m.feedbackMode = true
		m.feedbackText = ""
		return Result{}, true

	case "f":
		if m.req.Kind == KindFileEdit || m.req.Kind == KindFileCreate {
			m.expandedView = !m.expandedView
			return Result{}, true
		}
	}

	// 2.5. Ctrl+G: edit / expand command in command approval
	if (key.Code == 'g' && key.Mod == tea.ModCtrl) || key.Code == 7 {
		m.expandedView = !m.expandedView
		return Result{}, true
	}

	// 3. Navigation: Up / Down / k / j / Left / Right
	switch key.Code {
	case tea.KeyUp, tea.KeyLeft, 'k':
		if m.selectedIndex > 0 {
			m.selectedIndex--
		} else {
			m.selectedIndex = len(choices) - 1
		}
		return Result{}, true

	case tea.KeyDown, tea.KeyRight, 'j':
		if m.selectedIndex < len(choices)-1 {
			m.selectedIndex++
		} else {
			m.selectedIndex = 0
		}
		return Result{}, true

	case tea.KeyTab:
		// Tab in command prompt enters Amend mode
		if m.req.Kind == KindDefault && (m.req.CommandStr() != "" || m.req.Tool == "run_command" || m.req.Tool == "run_shell_command") {
			m.amendMode = true
			m.amendText = m.req.CommandStr()
			return Result{}, true
		}
		// Tab in file edit/create, if on Reject/Deny, opens feedback
		if (m.req.Kind == KindFileEdit || m.req.Kind == KindFileCreate) && m.selectedIndex == 1 {
			m.feedbackMode = true
			m.feedbackText = ""
			return Result{}, true
		}
		if m.selectedIndex < len(choices)-1 {
			m.selectedIndex++
		} else {
			m.selectedIndex = 0
		}
		return Result{}, true

	case tea.KeyEnter:
		if m.selectedIndex >= 0 && m.selectedIndex < len(choices) {
			c := choices[m.selectedIndex]
			if c.Decision == DecisionFeedback {
				m.feedbackMode = true
				m.feedbackText = ""
				return Result{}, true
			}
			res := Result{
				Decision: c.Decision,
				OptionID: c.OptionID,
			}
			m.complete(res)
			return res, true
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
