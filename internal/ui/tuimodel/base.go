package tuimodel

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// UIPhase represents the current phase of the TUI, matching
// agy 1.4.12's google3/third_party/jetski/cli/store/store.UIPhase.
type UIPhase int

const (
	UIPhaseAuthStart UIPhase = iota
	UIPhaseAuthWaiting
	UIPhaseAuthFailure
	UIPhaseSettingsError
	UIPhaseOnboarding
	UIPhaseWorkspaceTrust
	UIPhaseIdle
	UIPhaseAgentWorking
	UIPhaseToolConfirmation
	UIPhaseReviewAvailable
	UIPhaseArtifactReview
	UIPhaseEditingFeedback
	UIPhasePanelHelp
	UIPhasePanelSettings
	UIPhasePanelModelSelector
	UIPhasePanelPermissions
	UIPhasePanelSkills
	UIPhasePanelConversationPicker
	UIPhasePanelHooks
	UIPhasePanelMcp
	UIPhasePanelTasks
	UIPhasePanelContext
	UIPhasePanelUsage
	UIPhasePanelCredits
	UIPhaseBattleWorking
	UIPhaseBattleIdle
)

var phaseNames = [...]string{
	UIPhaseAuthStart:               "AuthStart",
	UIPhaseAuthWaiting:             "AuthWaiting",
	UIPhaseAuthFailure:             "AuthFailure",
	UIPhaseSettingsError:           "SettingsError",
	UIPhaseOnboarding:              "Onboarding",
	UIPhaseWorkspaceTrust:          "WorkspaceTrust",
	UIPhaseIdle:                    "Idle",
	UIPhaseAgentWorking:            "AgentWorking",
	UIPhaseToolConfirmation:        "ToolConfirmation",
	UIPhaseReviewAvailable:         "ReviewAvailable",
	UIPhaseArtifactReview:          "ArtifactReview",
	UIPhaseEditingFeedback:         "EditingFeedback",
	UIPhasePanelHelp:               "PanelHelp",
	UIPhasePanelSettings:           "PanelSettings",
	UIPhasePanelModelSelector:      "PanelModelSelector",
	UIPhasePanelPermissions:        "PanelPermissions",
	UIPhasePanelSkills:             "PanelSkills",
	UIPhasePanelConversationPicker: "PanelConversationPicker",
	UIPhasePanelHooks:              "PanelHooks",
	UIPhasePanelMcp:                "PanelMcp",
	UIPhasePanelTasks:              "PanelTasks",
	UIPhasePanelContext:            "PanelContext",
	UIPhasePanelUsage:              "PanelUsage",
	UIPhasePanelCredits:            "PanelCredits",
	UIPhaseBattleWorking:           "BattleWorking",
	UIPhaseBattleIdle:              "BattleIdle",
}

// String returns the canonical UIPhase name recovered from the agy binary.
func (p UIPhase) String() string {
	if int(p) >= 0 && int(p) < len(phaseNames) {
		return phaseNames[p]
	}
	return fmt.Sprintf("UIPhase(%d)", int(p))
}

// Layout captures the vertical line breakdown of the TUI frame, matching
// agy 1.4.12's google3/third_party/jetski/cli/layout/layout.Layout.
type Layout struct {
	PromptHeight        int
	InputAreaHeight     int
	PendingLineHeight   int
	HintHeight          int
	BtwHeight           int
	SubagentAlertHeight int
	StatusLineHeight    int
}

// FrameOverhead returns the sum of chrome and auxiliary line heights.
func (l Layout) FrameOverhead() int {
	return l.PromptHeight + l.InputAreaHeight + l.PendingLineHeight +
		l.HintHeight + l.BtwHeight + l.SubagentAlertHeight + l.StatusLineHeight
}

// MessageListAvailableLines calculates lines left for the transcript viewport.
func (l Layout) MessageListAvailableLines(totalLines int) int {
	avail := totalLines - l.FrameOverhead()
	if avail < 1 {
		return 1
	}
	return avail
}

// PanelAvailableLines calculates available vertical lines for an open panel.
func (l Layout) PanelAvailableLines(totalLines int) int {
	avail := totalLines - l.FrameOverhead() - 1
	if avail < 1 {
		return 1
	}
	return avail
}

// String renders the debug description matching agy 1.4.12's layout.Layout.String format:
// "PromptHeight: %d, InputAreaHeight: %d, PendingLineHeight: %d, HintHeight: %d, BtwHeight: %d, SubagentAlertHeight: %d, StatusLineHeight: %d, FrameOverhead: %d"
func (l Layout) String() string {
	return fmt.Sprintf("PromptHeight: %d, InputAreaHeight: %d, PendingLineHeight: %d, HintHeight: %d, BtwHeight: %d, SubagentAlertHeight: %d, StatusLineHeight: %d, FrameOverhead: %d",
		l.PromptHeight, l.InputAreaHeight, l.PendingLineHeight,
		l.HintHeight, l.BtwHeight, l.SubagentAlertHeight,
		l.StatusLineHeight, l.FrameOverhead())
}

// Base implements the 7 inherited default methods of the 10-method
// Model contract, mirroring agy 1.4.12's (*baseModel).
//
// In the reference binary, exactly 7 methods have a single implementor
// (baseModel): Store, Layout, UIPhase, Keybindings, ActionFromMsg,
// HasArtifacts, SetLayout. Individual models embed baseModel to inherit
// these defaults and implement only Init, Update, and View.
type Base struct {
	store       any
	layout      Layout
	uiPhase     UIPhase
	keybindings any
}

// Store returns the underlying store instance.
func (b *Base) Store() any {
	if b == nil {
		return nil
	}
	return b.store
}

// SetStore sets the store instance.
func (b *Base) SetStore(s any) {
	if b != nil {
		b.store = s
	}
}

// Layout returns the current terminal layout metrics.
func (b *Base) Layout() Layout {
	if b == nil {
		return Layout{}
	}
	return b.layout
}

// SetLayout updates the layout metrics.
func (b *Base) SetLayout(l Layout) {
	if b != nil {
		b.layout = l
	}
}

// UIPhase returns the active UI phase.
func (b *Base) UIPhase() UIPhase {
	if b == nil {
		return UIPhaseAuthStart
	}
	return b.uiPhase
}

// SetUIPhase updates the active UI phase.
func (b *Base) SetUIPhase(p UIPhase) {
	if b != nil {
		b.uiPhase = p
	}
}

// Keybindings returns the model's keybindings.
func (b *Base) Keybindings() any {
	if b == nil {
		return nil
	}
	return b.keybindings
}

// SetKeybindings sets the model's keybindings.
func (b *Base) SetKeybindings(kb any) {
	if b != nil {
		b.keybindings = kb
	}
}

// ActionFromMsg returns an action command for the given message, default nil.
func (b *Base) ActionFromMsg(msg tea.Msg) tea.Cmd {
	return nil
}

// HasArtifacts reports whether the model has active artifacts, default false.
func (b *Base) HasArtifacts() bool {
	return false
}
