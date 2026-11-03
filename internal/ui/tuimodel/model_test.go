package tuimodel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type dummyModel struct {
	Base
}

func (d *dummyModel) Init() tea.Cmd { return nil }
func (d *dummyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return d, nil }
func (d *dummyModel) View() tea.View { return tea.NewView("") }

var _ Model = (*dummyModel)(nil)

func TestBaseDefaults(t *testing.T) {
	m := &dummyModel{}
	if m.Store() != nil {
		t.Errorf("expected nil store, got %v", m.Store())
	}
	if m.HasArtifacts() {
		t.Errorf("expected HasArtifacts false")
	}
	if m.ActionFromMsg(nil) != nil {
		t.Errorf("expected nil ActionFromMsg")
	}
	if m.UIPhase() != UIPhaseAuthStart {
		t.Errorf("expected UIPhaseAuthStart, got %v", m.UIPhase())
	}
	m.SetUIPhase(UIPhaseAgentWorking)
	if m.UIPhase() != UIPhaseAgentWorking {
		t.Errorf("expected UIPhaseAgentWorking, got %v", m.UIPhase())
	}

	l := Layout{
		PromptHeight:        1,
		InputAreaHeight:     2,
		PendingLineHeight:   1,
		HintHeight:          1,
		BtwHeight:           0,
		SubagentAlertHeight: 0,
		StatusLineHeight:    1,
	}
	m.SetLayout(l)
	if m.Layout() != l {
		t.Errorf("expected layout %+v, got %+v", l, m.Layout())
	}
	if m.Layout().FrameOverhead() != 6 {
		t.Errorf("expected overhead 6, got %d", m.Layout().FrameOverhead())
	}
	if m.Layout().MessageListAvailableLines(24) != 18 {
		t.Errorf("expected 18 available, got %d", m.Layout().MessageListAvailableLines(24))
	}
	if m.Layout().PanelAvailableLines(24) != 17 {
		t.Errorf("expected 17 available, got %d", m.Layout().PanelAvailableLines(24))
	}
	if !strings.Contains(m.Layout().String(), "FrameOverhead: 6") {
		t.Errorf("unexpected Layout.String: %s", m.Layout().String())
	}

	dummyStore := "store"
	m.SetStore(dummyStore)
	if m.Store() != dummyStore {
		t.Errorf("expected %v, got %v", dummyStore, m.Store())
	}

	dummyKB := "keybindings"
	m.SetKeybindings(dummyKB)
	if m.Keybindings() != dummyKB {
		t.Errorf("expected %v, got %v", dummyKB, m.Keybindings())
	}
}

func TestUIPhaseString(t *testing.T) {
	if UIPhaseAuthStart.String() != "AuthStart" {
		t.Errorf("expected AuthStart, got %s", UIPhaseAuthStart.String())
	}
	if UIPhaseBattleIdle.String() != "BattleIdle" {
		t.Errorf("expected BattleIdle, got %s", UIPhaseBattleIdle.String())
	}
	if UIPhase(999).String() != "UIPhase(999)" {
		t.Errorf("expected UIPhase(999), got %s", UIPhase(999).String())
	}
}
