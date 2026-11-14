package storybook

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/artifactpanel"
	"github.com/justwasm/pigasus/internal/ui/views/creditspanel"
	"github.com/justwasm/pigasus/internal/ui/views/feedbackpanel"
	"github.com/justwasm/pigasus/internal/ui/views/linecomments"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/viewerpanel"
	"github.com/justwasm/pigasus/palette"
)

// -----------------------------------------------------------------------------
// 1. LoginUI Story
// -----------------------------------------------------------------------------

type loginUIStory struct {
	width, height int
	stateIdx      int
	model         *loginui.Model
}

func newLoginUIStory() Story {
	return Story{
		ID:          "panel-login-ui",
		Name:        "OAuth Login Screen",
		Category:    CategoryPanels,
		Description: "Interactive device authorization login screen with method selection, verification code box, and polling.",
		UpstreamSym: "jetski/cli/auth.LoginUI",
		States: []State{
			{ID: "select-method", Name: "Select Auth Method", Description: "Browser OAuth vs Application Default Credentials selector."},
			{ID: "oauth-code-box", Name: "Device Code Box", Description: "Verification URL and bold 8-character user code display."},
			{ID: "exchanging-token", Name: "Token Exchange Spinner", Description: "Polling for authorization completion with animated spinner."},
			{ID: "error-state", Name: "Authentication Error", Description: "Clear error callout with retry or cancel instructions."},
		},
		Factory: func() StoryInstance {
			s := &loginUIStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *loginUIStory) rebuild() {
	s.model = loginui.New()
	s.model.Width = s.width
	s.model.Height = s.height
	s.model.AuthURL = "https://accounts.google.com/o/oauth2/v2/auth?client_id=123&response_type=code"
	s.model.CodeInput = "4/0AeanS0Y7g-auth-code-preset"

	switch s.stateIdx {
	case 0:
		s.model.State = loginui.StateSelectMethod
	case 1:
		s.model.State = loginui.StateOAuth
	case 2:
		s.model.State = loginui.StateExchanging
	case 3:
		s.model.State = loginui.StateError
		s.model.Err = fmt.Errorf("oauth2: token expired before user authorization completed")
	}
}

func (s *loginUIStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *loginUIStory) SetSize(w, h int) {
	s.width, s.height = w, h
	if s.model != nil {
		s.model.Width = w
		s.model.Height = h
	}
}

func (s *loginUIStory) View() string {
	if s.model == nil {
		return ""
	}
	return s.model.View().Content
}

func (s *loginUIStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.model == nil {
		return nil, false
	}
	updated, cmd := s.model.Update(msg)
	if m, ok := updated.(*loginui.Model); ok {
		s.model = m
	}
	return cmd, true
}

// -----------------------------------------------------------------------------
// 2. FeedbackPanel Story
// -----------------------------------------------------------------------------

type feedbackPanelStory struct {
	width, height int
	stateIdx      int
	panel         *feedbackpanel.Panel
}

func newFeedbackPanelStory() Story {
	return Story{
		ID:          "panel-feedback",
		Name:        "Feedback Panel (/feedback)",
		Category:    CategoryPanels,
		Description: "Two-stage issue reporter: 6 preset problem categories followed by an optional free-text description.",
		UpstreamSym: "jetski/cli/panel.FeedbackPanel",
		States: []State{
			{ID: "category-picker", Name: "Stage 1: Category Picker", Description: "6 canonical feedback issue categories with numbered selection."},
			{ID: "details-form", Name: "Stage 2: Details Form", Description: "Echoes selected category and opens free-text input buffer."},
		},
		Factory: func() StoryInstance {
			s := &feedbackPanelStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *feedbackPanelStory) rebuild() {
	s.panel = feedbackpanel.New(feedbackpanel.Seams{})
	if s.stateIdx == 1 {
		s.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	}
}

func (s *feedbackPanelStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *feedbackPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *feedbackPanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *feedbackPanelStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 3. CreditsPanel Story
// -----------------------------------------------------------------------------

type creditsPanelStory struct {
	width, height int
	stateIdx      int
	panel         *creditspanel.Panel
}

func newCreditsPanelStory() Story {
	return Story{
		ID:          "panel-credits",
		Name:        "Credits Panel (/credits)",
		Category:    CategoryPanels,
		Description: "AI credit balance card with actions for purchasing credits and checking usage activity.",
		UpstreamSym: "jetski/cli/panel.CreditsPanel",
		States: []State{
			{ID: "balance-card", Name: "Credit Balance Card", Description: "Balance display with cursor on 'Get More AI Credits'."},
			{ID: "second-action", Name: "Activity Action", Description: "Cursor moved onto 'See Activity' history action."},
		},
		Factory: func() StoryInstance {
			s := &creditsPanelStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *creditsPanelStory) rebuild() {
	s.panel = creditspanel.New(creditspanel.Seams{})
	if s.stateIdx == 1 {
		s.panel.HandleKey(tea.Key{Code: tea.KeyDown})
	}
}

func (s *creditsPanelStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *creditsPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *creditsPanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *creditsPanelStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 4. ArtifactPanel Story
// -----------------------------------------------------------------------------

type artifactPanelStory struct {
	width, height int
	stateIdx      int
}

func newArtifactPanelStory() Story {
	return Story{
		ID:          "panel-artifacts",
		Name:        "Artifacts Panel (/artifact)",
		Category:    CategoryPanels,
		Description: "Turn artifacts list, preview, and editor opening shortcuts legend.",
		UpstreamSym: "jetski/cli/panel.ArtifactsPanel",
		States: []State{
			{ID: "empty-state", Name: "Empty State Frame", Description: "Default empty artifacts list with keyboard navigation legend."},
		},
		Factory: func() StoryInstance {
			return &artifactPanelStory{width: 80, height: 24}
		},
	}
}

func (s *artifactPanelStory) SetState(idx int) {
	s.stateIdx = idx
}

func (s *artifactPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *artifactPanelStory) View() string {
	lines := artifactpanel.Lines()
	return strings.Join(lines, "\n")
}

func (s *artifactPanelStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// -----------------------------------------------------------------------------
// 5. ViewerPanel Story
// -----------------------------------------------------------------------------

type viewerPanelStory struct {
	width, height int
	stateIdx      int
	offset        int
	panel         *viewerpanel.Panel
	spec          tuiviewer.Spec
}

func newViewerPanelStory() Story {
	return Story{
		ID:          "panel-viewer",
		Name:        "Document Viewer Panel (Viewport)",
		Category:    CategoryPanels,
		Description: "Scrollable documentary viewport with line range indicator used across /usage and /changelog.",
		UpstreamSym: "jetski/cli/viewer.ViewerPanel",
		States: []State{
			{ID: "document-top", Name: "Document Top (Lines 1-8)", Description: "Initial viewport showing document header and line counter."},
			{ID: "document-scrolled", Name: "Scrolled Viewport (Lines 5-12)", Description: "Viewport scrolled downward with updated range indicators."},
		},
		Factory: func() StoryInstance {
			s := &viewerPanelStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *viewerPanelStory) rebuild() {
	lines := []string{
		bold("Pigasus Agent Engine — Reference System Architecture"),
		dim("============================================================"),
		"",
		blue("1. Clean-room Reference Compatibility"),
		"   Clean-room reimplementation of jetski/cortex CLI reference 1.5.5.",
		"   Zero binary leakage, identical terminal escape sequences and glyph metrics.",
		"",
		blue("2. Multi-Model Provider Architecture"),
		"   Direct native streaming for CloudCode, Anthropic, OpenAI, Gateway.",
		"   Out-of-band error boundary preserves typed backend.HTTPError values.",
		"",
		blue("3. TUI & Storybook Component Hierarchy"),
		"   Strict Tier 5 UI architecture with Elm Architecture state dispatch.",
		"   Zero circular dependencies, guaranteed responsive dual-pane layout.",
		"",
		blue("4. Tool Execution & Sandboxed Operations"),
		"   Absolute tool paths, symlink path verification, and non-zero exit code capture.",
	}

	s.spec = tuiviewer.Spec{
		Lines:   lines,
		Visible: 8,
		Total:   len(lines),
	}

	s.panel = viewerpanel.New(viewerpanel.Seams{
		Spec:  func() (tuiviewer.Spec, bool) { return s.spec, true },
		Width: func() int { return s.width },
	})
	s.panel.Open()

	if s.stateIdx == 1 {
		s.offset = 4
	} else {
		s.offset = 0
	}
	s.panel.Sync(s.offset)
}

func (s *viewerPanelStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *viewerPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
	if s.panel != nil {
		s.panel.Sync(s.offset)
	}
}

func (s *viewerPanelStory) View() string {
	rows, start, end, _ := tuiviewer.Window(s.spec, s.offset)
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r + "\n")
	}
	b.WriteString("\n" + palette.DimGrayOpen + fmt.Sprintf("[Lines %d-%d of %d]  %s", start, end, s.spec.Total,
		tuilib.PanelKeyboardHint("↑/↓", "Scroll", "esc", "Dismiss")) + palette.DimGrayClose)
	return b.String()
}

func (s *viewerPanelStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd, newOffset := s.panel.HandleKey(msg.Key(), s.offset)
	s.offset = newOffset
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 6. LineComments Story
// -----------------------------------------------------------------------------

type lineCommentsStory struct {
	width, height int
	stateIdx      int
}

func newLineCommentsStory() Story {
	return Story{
		ID:          "panel-line-comments",
		Name:        "Line Comments & Review (/diff)",
		Category:    CategoryPanels,
		Description: "Interactive inline code review comments on diff hunks with unsent decision screen.",
		UpstreamSym: "jetski/cli/review.LineComments",
		States: []State{
			{ID: "unsent-screen", Name: "Unsent Comments Screen", Description: "File header, line annotations, and Send / Discard / Keep actions."},
			{ID: "formatted-bubble", Name: "Wire Formatted Bubble", Description: "Transmitted line comment block formatted for model turn."},
		},
		Factory: func() StoryInstance {
			return &lineCommentsStory{width: 80, height: 24}
		},
	}
}

func (s *lineCommentsStory) SetState(idx int) {
	s.stateIdx = idx
}

func (s *lineCommentsStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *lineCommentsStory) View() string {
	comments := []linecomments.Comment{
		{Line: 42, Body: "Should we add a mutex lock here to prevent concurrent map write?"},
		{Line: 88, Body: "Consider using strings.Builder instead of string concatenation in hot loop."},
	}
	targetFile := "internal/ui/transcript/document.go"

	if s.stateIdx == 0 {
		rows := linecomments.ScreenRows(targetFile, comments)
		return strings.Join(rows, "\n")
	}

	var b strings.Builder
	b.WriteString(bold("Line Comment Review Annotation (Wire Payload):") + "\n\n")
	for _, c := range comments {
		b.WriteString(blue(linecomments.Format(targetFile, c)) + "\n")
	}
	return b.String()
}

func (s *lineCommentsStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// RegisterAuthFeedbackStories mounts all authentication, feedback, and document viewer stories into the catalogue.
func RegisterAuthFeedbackStories(c *Catalog) {
	c.Register(newLoginUIStory())
	c.Register(newFeedbackPanelStory())
	c.Register(newCreditsPanelStory())
	c.Register(newArtifactPanelStory())
	c.Register(newViewerPanelStory())
	c.Register(newLineCommentsStory())
}

