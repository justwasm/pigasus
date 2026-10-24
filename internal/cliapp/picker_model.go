// picker_model.go — the "/model <query>" completion popup.
//
// The reference's composer completes the /model *argument* with a popup
// of models: the display label in a 30-column field, the catalogue id two
// columns further right. 【一手】 agy 1.4.6, live tmux capture:
//
//	> /model claude
//	────────────────────────────────────────────────────────
//	> Claude Sonnet 4.6 (Thinking)  claude-sonnet-4-6
//	  Claude Opus 4.6 (Thinking)    claude-opus-4-6-thinking
//
//	  ↑/↓ Navigate · enter Select · tab Complete
//
// The filter is a case-insensitive *substring* of the label or of the id
// (internal/tuilib/picker.go), the popup replaces the command rows while
// it is open, and Enter has three outcomes:
//
//   - the argument already names a model → the command runs and the model
//     changes ("Model set to <label>");
//   - the argument does not, but a row is highlighted → the row's label is
//     completed into the composer and the popup closes;
//   - the argument does not and nothing matches → the command runs and
//     reports `Invalid model "zzz" (available: ...)`.
//
// Tab always completes the highlighted row into the composer.
package cliapp

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/atfiles"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/subagentrouter"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// completionModelRows returns the catalogue the "/model <query>" popup
// offers: the reference's own models first, then the ones the user
// configured (internal/cli.ModelCatalogue).
//
// The rows are re-read on every keystroke, so a catalogue that changed
// (the user edited the provider configuration) shows up immediately.
func completionModelRows() []tuilib.ModelRow {
	entries := cli.ModelCatalogue()
	out := make([]tuilib.ModelRow, 0, len(entries))
	for _, e := range entries {
		out = append(out, tuilib.ModelRow{Label: e.Label, ID: e.ID})
	}
	return out
}

// resolveModelArgument resolves a "/model" argument to a catalogue row.
//
// The reference matches the display label or the catalogue id,
// case-insensitively, and only in full: "/model gpt-oss" names no model
// (the popup completes the row instead), while "/model GPT-OSS 120B
// (Medium)", "/model gpt-oss-120b-medium" and "/model gpt-oss 120b
// (medium)" all resolve to the same row. 【一手】 agy 1.4.6, live.
func resolveModelArgument(arg string) (cli.ModelEntry, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return cli.ModelEntry{}, false
	}
	for _, e := range cli.ModelCatalogue() {
		if strings.EqualFold(e.Label, arg) || strings.EqualFold(e.ID, arg) {
			return e, true
		}
	}
	return cli.ModelEntry{}, false
}

// invalidModelMessage renders the reference's error for a "/model"
// argument that names no model.
//
// 【一手】 agy 1.4.6, live:
//
//	Invalid model "zzz" (available: gemini-3.8-flash-high, gemini-3.8-flash-medium, gemini-3.8-flash-low, claude-sonnet-4-6, claude-opus-4-6-thinking, gpt-oss-120b-medium)
func invalidModelMessage(arg string) string {
	ids := make([]string, 0, 8)
	for _, e := range cli.ModelCatalogue() {
		ids = append(ids, e.ID)
	}
	return fmt.Sprintf("Invalid model %q (available: %s)", arg, strings.Join(ids, ", "))
}

// applyModelArgument makes the model the argument names the session's
// model and echoes the reference's confirmation.
//
// 【一手】 agy 1.4.6, live: "/model Claude Sonnet 4.6 (Thinking)" answers
// "Model set to Claude Sonnet 4.6 (Thinking)" and the status row follows;
// naming the model that is already selected answers "Model already set to
// <label>".
func (m *runMode) applyModelArgument(arg string) tea.Cmd {
	entry, ok := resolveModelArgument(arg)
	if !ok {
		m.slashRespond("/model", invalidModelMessage(arg))
		return nil
	}
	if m.model == entry.Label {
		m.slashRespond("/model", "Model already set to "+entry.Label)
		return nil
	}
	_ = auth.SetSettingsField("model", entry.ID)
	m.model = entry.Label
	m.statusModel = stripTier(entry.Label)
	m.syncBackendForModel()
	m.slashRespond("/model", "Model set to "+entry.Label)
	return nil
}

// submitModelCompletion runs the Enter semantics of the "/model <query>"
// popup. It reports whether it consumed the key.
func (m *runMode) submitModelCompletion() (bool, tea.Cmd) {
	if m.picker.Kind() != tuilib.PickerKindModel {
		return false, nil
	}
	arg := strings.TrimSpace(m.picker.Query())
	if entry, ok := resolveModelArgument(arg); ok {
		m.picker.Reset()
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		if m.model == entry.Label {
			m.slashRespond("/model", "Model already set to "+entry.Label)
			return true, nil
		}
		_ = auth.SetSettingsField("model", entry.ID)
		m.model = entry.Label
		m.statusModel = stripTier(entry.Label)
		m.syncBackendForModel()
		m.slashRespond("/model", "Model set to "+entry.Label)
		return true, nil
	}
	// The argument names no model: the highlighted row is completed into
	// the composer instead of running the command.
	if sel := m.picker.Selected(); sel != nil {
		m.input.SetValue("/model " + sel.Name)
		m.picker.Reset()
		m.historyIdx = 0
		m.historyEdit = ""
		return true, nil
	}
	// Nothing matches at all: the command runs and reports the argument.
	m.picker.Reset()
	m.input.Reset()
	m.clearInputSelection()
	m.historyIdx = 0
	m.historyEdit = ""
	m.slashRespond("/model", invalidModelMessage(arg))
	return true, nil
}

var DefaultRouter = subagentrouter.NewManager()

const atDispatchTimeout = 30 * 1000 * 1000 * 1000 // 30 seconds

func parseAtSubagentPrompt(buf string) (name, message string, ok bool) {
	return subagentrouter.ParsePrompt(buf)
}

func namedSubagentEmptyMessage(name string) string {
	return subagentrouter.NamedSubagentEmptyMessage(name)
}

func namedSubagentUnknownMessage(name string) string {
	return subagentrouter.NamedSubagentUnknownMessage(name)
}

func routeAtSubagent(ctx context.Context, buf string, m *runMode) (*subagentrouter.Entry, bool, error) {
	if m.atRouter == nil {
		m.atRouter = DefaultRouter
	}
	return subagentrouter.Route(ctx, m.atRouter, buf)
}

func (m *runMode) handlePromptAtPrefix(buf string) (bool, tea.Cmd) {
	if len(buf) == 0 || buf[0] != '@' {
		return false, nil
	}
	name, message, ok := parseAtSubagentPrompt(buf)
	if !ok {
		return false, nil
	}
	m.resetPromptForAt(buf)
	if message == "" {
		m.response = append(m.response, "", "> "+buf, "", namedSubagentEmptyMessage(name))
		m.saveSession()
		return true, nil
	}
	m.deliverAtSubagent(name, message, buf)
	return true, nil
}

func (m *runMode) resetPromptForAt(buf string) {
	m.pushHistory(buf)
	m.input.Reset()
	m.streaming = false
	m.historyIdx = 0
	m.historyEdit = ""
	m.picker.Reset()
}

func (m *runMode) deliverAtSubagent(name, message, buf string) {
	if m.atRouter == nil {
		m.atRouter = DefaultRouter
	}
	entry, ok := m.atRouter.Lookup(name)
	if !ok {
		m.response = append(m.response, "", "> "+buf, "", namedSubagentUnknownMessage(name))
		m.saveSession()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), atDispatchTimeout)
	defer cancel()
	if err := entry.Sender.Send(ctx, message); err != nil {
		m.response = append(m.response,
			"",
			"> "+buf,
			"",
			strings.TrimSpace("at-subagent: failed to deliver: "+err.Error()),
		)
		m.saveSession()
		return
	}
	m.response = append(m.response,
		"",
		cWarningBlueOpen+"> @"+entry.Name+" "+message+cWarningBlueClose,
		"",
		panelGray238+"delivered to \""+entry.Name+"\""+panelGray238C,
		"",
	)
	m.saveSession()
}

type atFileDropdown = atfiles.Dropdown

// tabSpliceAtFile applies TabCompletion to the textarea value.
func (m *runMode) tabSpliceAtFile(keepOpen bool) bool {
	if !m.atFileDropdown.Visible() {
		return false
	}
	repl, start, end := m.atFileDropdown.TabCompletion()
	if repl == "" {
		return false
	}
	cur := m.input.Value()
	if start < 0 || end > len(cur) || start > end {
		return false
	}
	next := cur[:start] + "@" + repl + cur[end:]
	m.input.SetValue(next)
	if keepOpen {
		m.atFileDropdown.Update(next, m.cwd)
	} else {
		m.atFileDropdown.Reset()
	}
	return true
}

// atFileView exposes the dropdown overlay rows for viewBody.
func (m *runMode) atFileView() string {
	if !m.atFileDropdown.Visible() {
		return ""
	}
	return m.atFileDropdown.View(m.width)
}
