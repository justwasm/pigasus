package cliapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
)

// currentEffort maps the clone's recovered /effort value onto the slider
// index. flags.effort is the parsed --effort value; anything that is not
// "low" or "medium" is treated as high, which is the reference default
// and the value the clone printed before this cycle.
func (m *runMode) currentEffort() int {
	switch word := cliflags.StringValue(m.flags, "effort"); word {
	case "low", "medium", "high":
		// Cycle 381: the word → slider index mapping belongs to the
		// effort panel, which owns the three levels and their names.
		return effortpanel.EffortIndexForName(word)
	}
	// Cycle 292: with no explicit --effort the tier comes from the model
	// label, which is what the reference renders. A settings.json holding
	// "Gemini 3.8 Flash (Medium)" prints "· medium" in the status row; the
	// clone used to print "· high" there while the banner said Medium.
	return effortCursorForLabel(m.model)
}

// currentEffortName is the word form of currentEffort, used to feed the
// runtime layer with the slider's choice ("low", "medium", "high"). It
// picks up both --effort at startup and a /effort <level> / picker
// rewrite at runtime, since both surfaces mutate m.flags.
//
// Cycle effort-picker: the TUI's per-turn loop and the print path's PiG
// session both read the same flag value through this helper.
//
// A model that declares reasoning tiers runs at its own tier, and the
// slider's three positions cannot say "xhigh": the label carries the
// tier ("MiniMax M3.1 Flash (Xhigh)"), so it is read from there. The
// --effort flag still wins — it is the user's own word.
func (m *runMode) currentEffortName() string {
	switch word := strings.ToLower(strings.TrimSpace(cliflags.StringValue(m.flags, "effort"))); word {
	case "low", "medium", "high", "xhigh", "max":
		return word
	}
	if tier := models.TierFromLabel(m.model); tier != "" {
		return tier
	}
	return effortpanel.EffortLevelName(m.currentEffort())
}

// panelView renders the panel body. It returns "" when no panel is open,
// mirroring pickerView/resumepicker's View so viewBody can call it
// unconditionally.
func (m *runMode) panelView() string {
	if m.panel.kind == panelregistry.None {
		return ""
	}
	var b strings.Builder
	for _, l := range m.panelLines() {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if m.panel.kind.TrailingBlank() {
		b.WriteString("\n")
	}
	return b.String()
}

// panelLines returns the panel's fully styled rows, one per terminal row.
func (m *runMode) panelLines() []string {
	return m.getPanelRegistry().Lines(m.panel.kind)
}

// closePanel clears the panel and, when the reference does so, appends
// the "> /cmd" + "⎿  Exited /cmd command" block to the transcript.
func (m *runMode) closePanel() tea.Cmd {
	cmd := m.panel.cmd
	notice := m.panel.kind.ExitNotice()
	// Cycle 381: a /codesearch search that is still running is stopped
	// when the panel closes — the runner's goroutine and the ripgrep
	// child it spawned would otherwise outlive the panel.
	m.panel.csp.Cancel()
	m.panel = slashPanel{}
	// Cycle 216: /config can change "Editor Mode › Vim" and its Insert
	// First child, so the vim gate is refreshed when the panel closes.
	m.reloadVimSettings()
	if notice {
		m.response = append(m.response, tuilib.SlashEchoLines(cmd, "Exited "+cmd+" command")...)
	}
	// The input was already cleared when the panel opened (see the
	// openSlashPanel call site in Update) and every key was swallowed
	// while it was up, so there is nothing left to reset here.
	return nil
}

// closePanelSilent clears the panel without writing the
// `Exited /cmd command` echo. Cycle 223: /add-dir's commit path
// (Enter → addDirFeedback → slashRespond → close) already prints its
// own transcript block via slashRespond; emitting the exit notice on
// top would yield two stacked `> /add-dir` headers and is not what
// the brief wants. Esc still goes through closePanel so the standard
// exit notice does fire when the user backs out without committing.
//
// reloadVimSettings is called by both paths — /add-dir doesn't toggle
// vim gates, but every close should refresh so a follow-up panel
// across a /config change sees the fresh state.
func (m *runMode) closePanelSilent() tea.Cmd {
	// Cycle 381: the silent close stops a running /codesearch search
	// too — see closePanel.
	m.panel.csp.Cancel()
	m.panel = slashPanel{}
	m.reloadVimSettings()
	return nil
}
