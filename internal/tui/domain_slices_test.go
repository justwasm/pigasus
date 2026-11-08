package tui

import (
	"testing"

	tas "charm.land/bubbles/v2/textarea"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

func TestDomainComposerSlice(t *testing.T) {
	m := &RootModel{
		prompt: PromptModel{
			input: tas.New(),
		},
	}
	if !m.IsComposerEmpty() {
		t.Error("expected composer to start empty")
	}

	m.SetComposerValue("hello world")
	if m.ComposerValue() != "hello world" {
		t.Errorf("got composer value %q, want 'hello world'", m.ComposerValue())
	}
	if m.IsComposerEmpty() {
		t.Error("expected composer not to be empty")
	}

	m.ArmEsc("hello world")
	if !m.IsEscArmed() {
		t.Error("expected Esc to be armed")
	}

	m.ClearComposer()
	if !m.IsComposerEmpty() || m.IsEscArmed() {
		t.Error("expected ClearComposer to clear value and disarm Esc")
	}

	m.AddPendingMedia(clipboard.Media{Path: "test.png"})
	if !m.HasPendingMedia() {
		t.Error("expected pending media")
	}
	m.ClearPendingMedia()
	if m.HasPendingMedia() {
		t.Error("expected no pending media after clear")
	}

	m.SetVimMode(true)
	if !m.VimActive() {
		t.Error("expected vim active")
	}
}

func TestDomainOverlaySlice(t *testing.T) {
	m := &RootModel{}
	if m.HasActivePanel() {
		t.Error("expected no active panel initially")
	}

	m.overlays.panel.kind = panelregistry.Tasks
	if !m.HasActivePanel() || m.ActivePanelKind() != panelregistry.Tasks {
		t.Errorf("got active panel %v, want Tasks", m.ActivePanelKind())
	}
	if !m.HasActiveOverlay() {
		t.Error("expected active overlay when panel is open")
	}

	m.overlays.panel.kind = panelregistry.None
	if m.HasActivePanel() {
		t.Error("expected panel closed")
	}
}

func TestDomainSessionSlice(t *testing.T) {
	m := &RootModel{}
	m.SetCurrentModel("claude-sonnet-4")
	if m.CurrentModel() != "claude-sonnet-4" {
		t.Errorf("got model %q, want claude-sonnet-4", m.CurrentModel())
	}

	m.SetSessionTier("Pro Tier", "user@example.com")
	if m.CurrentTier() != "Pro Tier" || !m.IsTierFetched() {
		t.Errorf("tier = %q (fetched=%v), want Pro Tier", m.CurrentTier(), m.IsTierFetched())
	}

	m.SetCurrentConversationID("conv-123")
	m.SetConversationTitle("My Conversation")
	if m.CurrentConversationID() != "conv-123" || m.ConversationTitle() != "My Conversation" {
		t.Errorf("id=%q title=%q", m.CurrentConversationID(), m.ConversationTitle())
	}

	m.SetSessionGoal("Ship feature")
	m.AddSessionLearning("Rule 1")
	if m.SessionGoal() != "Ship feature" || len(m.SessionLearnings()) != 1 {
		t.Errorf("goal=%q learnings=%v", m.SessionGoal(), m.SessionLearnings())
	}
}

func TestDomainUISlice(t *testing.T) {
	m := &RootModel{}
	m.viewport.width = 120
	m.viewport.height = 40
	w, h := m.TerminalDimensions()
	if w != 120 || h != 40 {
		t.Errorf("dimensions = (%d, %d), want (120, 40)", w, h)
	}

	m.SetActiveColorScheme("nord")
	if m.ActiveColorScheme() != "nord" {
		t.Errorf("color scheme = %q, want nord", m.ActiveColorScheme())
	}

	m.SetAltScreen(true)
	if !m.IsAltScreenEnabled() {
		t.Error("expected alt screen enabled")
	}

	m.SetViewportScroll(15)
	if m.ViewportScroll() != 15 {
		t.Errorf("scroll = %d, want 15", m.ViewportScroll())
	}

	m.SetAutoScroll(true)
	if !m.IsAutoScrollEnabled() {
		t.Error("expected auto scroll enabled")
	}
}

func TestDomainEngineSlice(t *testing.T) {
	m := &RootModel{}
	m.SetStreamActive(true)
	if !m.IsStreamActive() {
		t.Error("expected stream active")
	}

	m.SetThinkingActive(true)
	if !m.IsThinkingActive() {
		t.Error("expected thinking active")
	}

	m.SetTurnCancelled(true)
	if !m.WasTurnCancelled() {
		t.Error("expected turn cancelled")
	}

	m.SetQuitting(true)
	if !m.IsQuitting() {
		t.Error("expected quitting")
	}
}
