// slash_btw.go — cycle 230: /btw side-question card lifecycle.
//
// The clone's /btw command launches an independent PiG coding.Session
// (no parent transcript, no hooks) and renders the streamed answer as
// a floating card above the input area (aboveInputCard). /copy btw copies the
// active card's text to the system clipboard.
package cliapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
)

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
// Mirrors the print path's pigPrintModel (print_pig.go:54): the gateway
// backend yields the native provider; every other backend is wrapped
// by piagentbridge.NewBackendProvider so the gateway, fingerprint and
// *apierror.HTTPError classification stay identical.
func (m *runMode) btwModel() *ai.Model {
	if m.backend == nil {
		return nil
	}
	if provider := agent.NativeProvider(m.backend); provider != nil {
		return provider.Model(m.model)
	}
	return piagentbridge.NewBackendProvider(m.backend, m.model).Model(m.model)
}

// btwDoneMsg delivers the settled side-question Card or failure.
type btwDoneMsg struct {
	id   string
	card *sidequestion.Card
	err  error
}

// btwTickMsg drives the spinner frame and answer delta re-renders while
// an asynchronous /btw query is in flight.
type btwTickMsg struct{}

func btwTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return btwTickMsg{} })
}

// runBtwSideQuestion is the /btw slash-command body.
//
// In reference agy:
//   - If args is empty:
//     If history exists and no question is active, shows history card (ShowBtwHistory).
//     Else emits "Usage: /btw <question>".
//   - If a question is already active (IsBtwActive), emits:
//     "A /btw question is already in progress. Please wait for it to finish."
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

	preCtx := context.Background()
	question, blocked := m.firePreSideQuestionHook(preCtx, trimmed)
	if blocked {
		return nil
	}
	trimmed = question
	m.appendBtwToSession(trimmed)

	ctx, cancel := context.WithCancel(context.Background())
	m.activeBtwCancel = cancel

	// When running without an active Bubble Tea program (e.g. unit tests that
	// call handleSlashCommand directly without driving an event loop), run
	// synchronously so assertions see the settled Card immediately.
	if m.prog == nil && !m.asyncBtw {
		card, err := mgr.Ask(ctx, trimmed)
		m.activeBtwCancel = nil
		cancel()
		if err != nil {
			m.response = append(m.response,
				panelGray238+"btw failed: "+err.Error()+panelGray238C,
				"",
			)
			m.firePostSideQuestionHook(preCtx, trimmed, nil, err)
			return nil
		}
		m.btwCards = append(m.btwCards, card)
		m.activeBtwIndex = len(m.btwCards) - 1
		m.activeBtwID = card.ID
		m.btwScroll = 0
		m.firePostSideQuestionHook(preCtx, trimmed, card, nil)
		return nil
	}

	card, run, err := mgr.StartAsk(ctx, trimmed)
	if err != nil {
		m.activeBtwCancel = nil
		cancel()
		m.response = append(m.response,
			panelGray238+"btw failed: "+err.Error()+panelGray238C,
			"",
		)
		m.firePostSideQuestionHook(preCtx, trimmed, nil, err)
		return nil
	}

	m.btwCards = append(m.btwCards, card)
	m.activeBtwIndex = len(m.btwCards) - 1
	m.activeBtwID = card.ID
	m.btwScroll = 0

	askCmd := func() tea.Msg {
		runErr := run()
		return btwDoneMsg{id: card.ID, card: card, err: runErr}
	}
	return tea.Batch(askCmd, btwTick())
}

func (m *runMode) updateBtwDoneMsg(msg btwDoneMsg) (tea.Model, tea.Cmd) {
	if m.activeBtwCancel != nil {
		m.activeBtwCancel = nil
	}
	if msg.err != nil {
		m.firePostSideQuestionHook(context.Background(), msg.card.Question, nil, msg.err)
	} else {
		m.firePostSideQuestionHook(context.Background(), msg.card.Question, msg.card, nil)
	}
	return m, nil
}

func (m *runMode) firePreSideQuestionHook(preCtx context.Context, trimmed string) (string, bool) {
	if m.hooks == nil {
		return trimmed, false
	}
	pre := hooks.SideQuestion(hooks.PreSideQuestion, trimmed)
	blocked, final, reason := m.hooks.Fire(preCtx, hooks.PreSideQuestion, pre)
	if !blocked {
		return final.Question, false
	}
	m.slashRespond("/btw", "blocked by hook: "+reason)
	m.appendBtwToSession(final.Question)
	post := hooks.SideQuestion(hooks.PostSideQuestion, final.Question)
	post.Blocked = true
	post.BlockReason = reason
	_, _, _ = m.hooks.Fire(preCtx, hooks.PostSideQuestion, post)
	return final.Question, true
}

func (m *runMode) firePostSideQuestionHook(preCtx context.Context, trimmed string, card *sidequestion.Card, err error) {
	if m.hooks == nil {
		return
	}
	post := hooks.SideQuestion(hooks.PostSideQuestion, trimmed)
	if err != nil {
		post.Blocked = true
		post.BlockReason = err.Error()
	} else if card != nil {
		post.Answer = card.Answer
		post.Tokens = card.Tokens
	}
	_, _, _ = m.hooks.Fire(preCtx, hooks.PostSideQuestion, post)
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

// btwScrollBy scrolls the active BTW card answer by delta (-1 or +1)
// when expanded. Returns true if the event was consumed.
func (m *runMode) btwScrollBy(delta int) bool {
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
	maxLines := sidequestion.MaxBtwAnswerLines(m.height, true)
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

// renderBtwCard renders the active side-question card.
func (m *runMode) renderBtwCard() string {
	card := m.currentBtwResponse()
	if card == nil {
		return ""
	}
	return sidequestion.RenderBtwCard(card, sidequestion.CardRenderOptions{
		Width:     m.width,
		Height:    m.height,
		CardCount: len(m.btwCards),
		Scroll:    m.btwScroll,
		RenderMd:  mdrender.RenderMarkdown,
	})
}

// aboveInputCard renders the card directly above the input prompt.
func (m *runMode) aboveInputCard() string {
	if m.isBtwCardVisible() {
		return m.renderBtwCard()
	}
	return ""
}
