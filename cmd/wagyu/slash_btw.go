// slash_btw.go — cycle 230: /btw side-question card lifecycle.
//
// The clone's /btw command launches an independent PiG coding.Session
// (no parent transcript, no hooks) and renders the streamed answer as
// a floating card above the input area (aboveInputCard). /copy btw copies the
// active card's text to the system clipboard.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/hooks"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/sidequestion"
)

// btwHeaderLabel is the prefix the side-question card renders in the
// transcript area. The reference uses the `⎿` glyph (the same one the
// slash command echo uses).
const btwHeaderLabel = "btw · "

// newBtwManager is the hook the slash command uses to build a side-
// question Manager. Production callers go through btwManagerFor which
// binds to the runMode's backend; tests replace the variable directly
// to inject the test-faux model.
var newBtwManager = func(opts sidequestion.Options) *sidequestion.Manager {
	return sidequestion.NewManager(opts)
}

// btwManagerFor returns (or constructs on first call) the Manager the
// runMode uses for /btw. Nil is a deliberate possibility: a runMode
// without a backend cannot open a coding.Session, so we surface
// /btw unavailable instead of panicking on nil.
func (m *runMode) btwManagerFor() *sidequestion.Manager {
	if m.btwManager != nil {
		return m.btwManager
	}
	model := m.btwModel()
	if model == nil {
		return nil
	}
	m.btwManager = newBtwManager(sidequestion.Options{
		Model:        model,
		CWD:          m.cwd,
		AgentDir:     pigrt.DefaultAgentHome(),
		SystemPrompt: sidequestion.DefaultSystemPrompt,
	})
	return m.btwManager
}

// btwModel derives the PiG model the side-question Session calls.
// Mirrors the print path's pigPrintModel (print_pig.go:54): the agw
// backend yields the native provider; every other backend is wrapped
// by piagentbridge.NewBackendProvider so the gateway, fingerprint and
// *backend.HTTPError classification stay identical.
func (m *runMode) btwModel() *ai.Model {
	if m.backend == nil {
		return nil
	}
	if provider := agent.NativeProvider(m.backend); provider != nil {
		return provider.Model(m.model)
	}
	return piagentbridge.NewBackendProvider(m.backend, m.model).Model(m.model)
}

// runBtwSideQuestion is the /btw slash-command body.
//
// In reference agy:
//   - If args is empty:
//       If history exists and no question is active, shows history card (ShowBtwHistory).
//       Else emits "Usage: /btw <question>".
//   - If a question is already active (IsBtwActive), emits:
//       "A /btw question is already in progress. Please wait for it to finish."
//   - Otherwise, dispatches the question, sets active card index, and renders above the input.
func (m *runMode) runBtwSideQuestion(args string) tea.Cmd {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		if m.ShowBtwHistory() {
			return nil
		}
		m.slashRespond("/btw", "Usage: /btw <question>")
		return nil
	}
	if m.IsBtwActive() {
		m.slashRespond("/btw", "A /btw question is already in progress. Please wait for it to finish.")
		return nil
	}
	mgr := m.btwManagerFor()
	if mgr == nil {
		m.slashRespond("/btw", "/btw is unavailable in this environment.")
		return nil
	}

	pre := hooks.SideQuestion(hooks.PreSideQuestion, trimmed)
	preCtx := context.Background()
	if m.hooks != nil {
		blocked, final, reason := m.hooks.Fire(preCtx, hooks.PreSideQuestion, pre)
		if blocked {
			m.slashRespond("/btw", "blocked by hook: "+reason)
			m.appendBtwToSession(final.Question)
			post := hooks.SideQuestion(hooks.PostSideQuestion, final.Question)
			post.Blocked = true
			post.BlockReason = reason
			if m.hooks != nil {
				_, _, _ = m.hooks.Fire(preCtx, hooks.PostSideQuestion, post)
			}
			return nil
		}
		pre = final
	}
	trimmed = pre.Question
	m.appendBtwToSession(trimmed)

	card, err := mgr.Ask(context.Background(), trimmed)
	if err != nil {
		m.response = append(m.response,
			panelGray238+"btw failed: "+err.Error()+panelGray238C,
			"",
		)
		if m.hooks != nil {
			post := hooks.SideQuestion(hooks.PostSideQuestion, trimmed)
			post.Blocked = true
			post.BlockReason = err.Error()
			_, _, _ = m.hooks.Fire(preCtx, hooks.PostSideQuestion, post)
		}
		return nil
	}

	m.btwCards = append(m.btwCards, card)
	m.activeBtwIndex = len(m.btwCards) - 1
	m.activeBtwID = card.ID
	m.btwScroll = 0

	if m.hooks != nil {
		post := hooks.SideQuestion(hooks.PostSideQuestion, trimmed)
		post.Answer = card.Answer
		post.Tokens = card.Tokens
		_, _, _ = m.hooks.Fire(preCtx, hooks.PostSideQuestion, post)
	}
	return nil
}

// appendBtwToSession records a /btw question in the parent
// conversation's transcript.
func (m *runMode) appendBtwToSession(question string) {
	if strings.TrimSpace(question) == "" {
		return
	}
	m.session = append(m.session, backend.Message{
		Role:    "user",
		Content: "> /btw " + question,
	})
}

// copyBtwCard handles "/copy btw".
// Matches the reference binary's copyBtwAnswer (google3/third_party/jetski/cli/commands/commands.copyBtwAnswer):
//   - If no active card or len == 0: "No /btw response to copy."
//   - If active card has answer: returns answer, "Copied /btw answer to clipboard."
//   - If active card is still generating: "The /btw answer is still being generated."
//   - If active card failed: "The /btw question failed: <err>"
func (m *runMode) copyBtwCard() (string, string) {
	card := m.currentBtwResponse()
	if card == nil {
		return "", "No /btw response to copy."
	}
	_, answer, _, state, err := card.LockedSnapshot()
	if answer != "" {
		return answer, "Copied /btw answer to clipboard."
	}
	if state == sidequestion.StatePending || state == sidequestion.StateStreaming {
		return "", "The /btw answer is still being generated."
	}
	if state == sidequestion.StateFailed || err != nil {
		if err == nil {
			err = fmt.Errorf("unknown error")
		}
		return "", "The /btw question failed: " + err.Error()
	}
	return "", "No /btw response to copy."
}

// ShowBtwHistory reveals the most recent BTW card if history exists
// and no card is currently generating. Returns true if history was shown.
func (m *runMode) ShowBtwHistory() bool {
	if len(m.btwCards) == 0 {
		return false
	}
	if m.IsBtwActive() {
		return false
	}
	m.activeBtwIndex = len(m.btwCards) - 1
	m.activeBtwID = m.btwCards[m.activeBtwIndex].ID
	m.btwScroll = 0
	return true
}

// IsBtwActive reports whether any /btw question is currently in progress.
func (m *runMode) IsBtwActive() bool {
	for _, c := range m.btwCards {
		if c != nil && c.IsActive() {
			return true
		}
	}
	return false
}

// DismissBtwResponse dismisses the active BTW card and cancels any in-flight generation.
func (m *runMode) DismissBtwResponse() {
	if m.activeBtwCancel != nil {
		m.activeBtwCancel()
		m.activeBtwCancel = nil
	}
	if card := m.currentBtwResponse(); card != nil {
		card.MarkCancelled()
	}
	m.activeBtwIndex = -1
	m.activeBtwID = ""
	m.btwScroll = 0
}

// ToggleBtwExpanded flips the active BTW card's expansion state.
func (m *runMode) ToggleBtwExpanded() {
	card := m.currentBtwResponse()
	if card == nil {
		return
	}
	card.ToggleExpanded()
	m.btwScroll = 0
}

// NavigateBtw shifts the active BTW card index by delta (-1 or +1),
// clamping to [0, len-1].
func (m *runMode) NavigateBtw(delta int) {
	if len(m.btwCards) == 0 {
		return
	}
	idx := m.activeBtwIndex + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.btwCards) {
		idx = len(m.btwCards) - 1
	}
	m.activeBtwIndex = idx
	m.activeBtwID = m.btwCards[idx].ID
	m.btwScroll = 0
}

// BtwScrollBy scrolls the active BTW card answer by delta (-1 or +1)
// when expanded. Returns true if the event was consumed.
func (m *runMode) BtwScrollBy(delta int) bool {
	card := m.currentBtwResponse()
	if card == nil {
		return false
	}
	if !card.IsExpanded() {
		return false
	}
	_, answer, _, _, _ := card.LockedSnapshot()
	if answer == "" {
		return false
	}
	maxLines := m.maxBtwAnswerLines(true)
	lines := strings.Split(answer, "\n")
	if len(lines) <= maxLines {
		return false
	}
	maxScroll := len(lines) - maxLines
	newScroll := m.btwScroll + delta
	if newScroll < 0 {
		newScroll = 0
	}
	if newScroll > maxScroll {
		newScroll = maxScroll
	}
	if newScroll == m.btwScroll {
		return true
	}
	m.btwScroll = newScroll
	return true
}

func (m *runMode) currentBtwResponse() *sidequestion.Card {
	if m.activeBtwIndex >= 0 && m.activeBtwIndex < len(m.btwCards) {
		return m.btwCards[m.activeBtwIndex]
	}
	return nil
}

func (m *runMode) isBtwCardVisible() bool {
	return m.activeBtwIndex >= 0 && m.activeBtwIndex < len(m.btwCards)
}

func (m *runMode) findBtwCard(id string) *sidequestion.Card {
	if m.activeBtwID == id && m.currentBtwResponse() != nil {
		return m.currentBtwResponse()
	}
	for _, c := range m.btwCards {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// activeBtwCard returns the card /copy btw addresses.
func (m *runMode) activeBtwCard() *sidequestion.Card {
	if cur := m.currentBtwResponse(); cur != nil {
		return cur
	}
	if m.activeBtwID != "" {
		return m.findBtwCard(m.activeBtwID)
	}
	if len(m.btwCards) > 0 {
		return m.btwCards[len(m.btwCards)-1]
	}
	return nil
}

// BtwCardExpanded reports whether the card with the given ID is expanded.
func (m *runMode) BtwCardExpanded(id string) bool {
	if card := m.findBtwCard(id); card != nil {
		return card.IsExpanded()
	}
	if m.btwExpanded != nil {
		return m.btwExpanded[id]
	}
	return false
}

// ToggleBtwCardExpansion flips the per-card expansion flag.
func (m *runMode) ToggleBtwCardExpansion(id string) bool {
	if card := m.findBtwCard(id); card != nil {
		return card.ToggleExpanded()
	}
	if m.btwExpanded == nil {
		m.btwExpanded = make(map[string]bool)
	}
	next := !m.btwExpanded[id]
	m.btwExpanded[id] = next
	return next
}

// CollapseBtwCard forces the card to render collapsed.
func (m *runMode) CollapseBtwCard(id string) {
	if card := m.findBtwCard(id); card != nil {
		card.SetExpanded(false)
	}
	if m.btwExpanded != nil {
		m.btwExpanded[id] = false
	}
}

// maxBtwAnswerLines computes maximum visible answer lines:
//   - Collapsed: 30% of windowHeight (min 5)
//   - Expanded: windowHeight - 8 (min 5)
func (m *runMode) maxBtwAnswerLines(expanded bool) int {
	h := m.height
	if h <= 0 {
		h = 24
	}
	if expanded {
		lines := h - 8
		if lines < 5 {
			lines = 5
		}
		return lines
	}
	lines := int(float64(h) * 0.3)
	if lines < 5 {
		lines = 5
	}
	return lines
}

// btwHintText generates the card hint bar joined with " · ":
// "Tab to collapse/expand · ←/→ to navigate · Enter/Esc to dismiss"
func (m *runMode) btwHintText(card *sidequestion.Card) string {
	expanded := card.IsExpanded()

	var hints []string
	if expanded {
		hints = append(hints, "Tab to collapse")
	} else {
		hints = append(hints, "Tab to expand")
	}
	if len(m.btwCards) > 1 {
		hints = append(hints, "←/→ to navigate")
	}
	hints = append(hints, "Enter/Esc to dismiss")
	return strings.Join(hints, " · ")
}

func (m *runMode) btwSpinner() string {
	frames := []string{"⣾", "⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽"}
	return frames[int(time.Now().UnixMilli()/100)%len(frames)]
}

// renderInputCard wraps content in a box with a thick left border (┃),
// matching google3/third_party/jetski/cli/model/model.renderInputCard.
func (m *runMode) renderInputCard(width int, content string) string {
	cardWidth := width - 3
	if cardWidth < 10 {
		cardWidth = 10
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("#268BD2")).
		PaddingLeft(1).
		Width(cardWidth)
	return style.Render(content)
}

// renderBtwCard renders the active side-question card.
// Matches google3/third_party/jetski/cli/model/model.(*btwModel).renderCard.
func (m *runMode) renderBtwCard() string {
	card := m.currentBtwResponse()
	if card == nil {
		return ""
	}
	question, answer, _, state, err := card.LockedSnapshot()
	expanded := card.IsExpanded()

	w := m.width
	if w <= 0 {
		w = 80
	}
	cardWidth := w - 3
	if cardWidth < 10 {
		cardWidth = 10
	}

	var lines []string

	// 1. Question line
	firstQ := firstLine(question)
	lines = append(lines, firstQ)
	lines = append(lines, "")

	// 2. Body / Answer
	if state == sidequestion.StatePending || (state == sidequestion.StateStreaming && answer == "") {
		lines = append(lines, m.btwSpinner()+" Thinking...")
	} else if err != nil || state == sidequestion.StateFailed {
		lines = append(lines, "Error: "+err.Error())
		lines = append(lines, "Press Enter/Esc to dismiss")
	} else if answer != "" {
		styled := renderMarkdown(answer, cardWidth)
		ansLines := strings.Split(styled, "\n")
		maxLines := m.maxBtwAnswerLines(expanded)
		if len(ansLines) <= maxLines {
			lines = append(lines, ansLines...)
		} else if !expanded {
			lines = append(lines, ansLines[:maxLines]...)
			lines = append(lines, "... (truncated)")
		} else {
			scroll := m.btwScroll
			if scroll < 0 {
				scroll = 0
			}
			maxScroll := len(ansLines) - maxLines
			if scroll > maxScroll {
				scroll = maxScroll
			}
			end := scroll + maxLines
			if end > len(ansLines) {
				end = len(ansLines)
			}
			lines = append(lines, ansLines[scroll:end]...)
			lines = append(lines, fmt.Sprintf("↑/↓ to scroll (%d-%d of %d)", scroll+1, end, len(ansLines)))
		}
	} else {
		lines = append(lines, "Error: /btw ended unexpectedly without a response")
	}

	lines = append(lines, "")

	// 3. Footer
	if state == sidequestion.StatePending || state == sidequestion.StateStreaming {
		lines = append(lines, m.btwSpinner()+" Generating... (Enter/Esc to cancel)")
	} else if err == nil && state != sidequestion.StateFailed {
		lines = append(lines, m.btwHintText(card))
	}

	content := strings.Join(lines, "\n")
	return m.renderInputCard(w, content)
}

// aboveInputCard renders the card directly above the input prompt.
// Matches google3/third_party/jetski/cli/model/model.(*ConversationModel).aboveInputCard.
func (m *runMode) aboveInputCard() string {
	if m.isBtwCardVisible() {
		return m.renderBtwCard()
	}
	return ""
}
