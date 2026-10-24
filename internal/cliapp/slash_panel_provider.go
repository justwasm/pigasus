// slash_panel_provider.go — cycle provider-model-editor: the /provider
// configuration panel.
//
// # What it is
//
// The TUI's front-end onto ~/.config/openagy/providers.json. It lists the
// configured providers and the models each one serves, and it can add,
// edit and delete both.
//
// # One writer
//
// Every write goes through models.Editor — the same layer the clone's
// `agy provider` / `agy model` subcommands and the wrapper's
// `agy wrapper providers` / `models` commands use. The panel holds no
// configuration logic of its own: it builds a ProviderConfig (or a
// ProviderModel) and hands it to the editor, which rewrites the one field
// that changed and leaves the rest of the document alone.
//
// # Not in the catalogue
//
// The reference's slash-command catalogue is byte-pinned: the picker
// prints "↓ 94 more" and 99 is the entry count. /provider is an
// openagy-only command, so it is invocable but *not* listed — the
// picker catalogue (internal/tuilib.DefaultSlashCommands) is untouched.
//
// # Layout
//
// The reference has no panel for this, so there is nothing to match byte
// for byte. The shape follows the clone's own panels: a title, the rows
// with a blue ">" cursor, a notice line, the file's path, and the
// "Keyboard: …" legend.
package cliapp

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/models"
	"github.com/justwasm/pigasus/internal/providerpanel"
)

// The verification line's colours: green for a pass, red for a failure.
// They are the repo's true-colour constants (the effort slider's green
// and the bash-mode failure red), not new ones.
const (
	panelVerifyOKOpen    = "\x1b[38;2;133;153;0m"
	panelVerifyOKClose   = "\x1b[39m"
	panelVerifyFailOpen  = "\x1b[38;2;220;50;47m"
	panelVerifyFailClose = "\x1b[39m"
)

// providerRowKind says what a /provider row holds.
//
// The row model itself lives in internal/providerpanel; these aliases keep
// the panel's own vocabulary (and its tests) readable.
type providerRowKind = providerpanel.RowKind

const (
	// providerRowProvider is a provider header: the provider's id, its
	// protocol and its endpoint.
	providerRowProvider = providerpanel.RowProvider
	// providerRowModel is a model listed under a provider.
	providerRowModel = providerpanel.RowModel
	// providerRowBuiltin is one of the reference's own models. The row
	// with an empty model id is the group's header.
	providerRowBuiltin = providerpanel.RowBuiltin
)

// providerRow is one row of the /provider list.
type providerRow = providerpanel.Row

// builtinProviderLabel is the name the reference's own models are listed
// under. They are served by Google's CloudCode endpoint, not by a
// configured provider, so the group says where they come from.
const builtinProviderLabel = providerpanel.BuiltinProviderLabel

// providerPanelState is the /provider panel's state.
type providerPanelState struct {
	// providers is the provider section as it was last read.
	providers map[string]models.ProviderConfig
	// rows is the rendered list: one row per provider, one per model it
	// serves, and the built-in group at the end.
	rows []providerRow
	// notice is the last write's result, or the reason it failed.
	notice string
	// confirm is the index of the row a pending delete targets. `d` twice
	// on the same row carries the delete out; any other key clears it.
	confirm int
	// form is the edit form. nil means the list is showing.
	form *providerForm
	// verifying is true while a verification probe is in flight.
	verifying bool
	// verify is the outcome of the last probe.
	verify *models.VerifyResult
	// verifyLabel names what was probed ("provider gateway").
	verifyLabel string
}

// providerFormKind says what a form edits.
type providerFormKind = providerpanel.FormKind

const (
	providerFormProvider = providerpanel.FormProvider
	providerFormModel    = providerpanel.FormModel
)

// providerFieldKind says how a field is edited.
type providerFieldKind = providerpanel.FieldKind

const (
	// providerFieldText is a free-text field, edited with the
	// bubbles textinput.
	providerFieldText = providerpanel.FieldText
	// providerFieldChoice cycles through a fixed set of values with ←/→.
	providerFieldChoice = providerpanel.FieldChoice
)

// providerFormField is one editable field of the form.
type providerFormField = providerpanel.FormField

// providerForm is the /provider edit form.
type providerForm = providerpanel.Form

// providerFormFieldLabels. The form's fields are addressed by label, so
// the constants below are the single source of truth for both the form
// and the write.
const (
	providerFieldID        = providerpanel.FieldID
	providerFieldName      = providerpanel.FieldName
	providerFieldType      = providerpanel.FieldType
	providerFieldBaseURL   = providerpanel.FieldBaseURL
	providerFieldAPIURL    = providerpanel.FieldAPIURL
	providerFieldAPIKey    = providerpanel.FieldAPIKey
	providerFieldTimeout   = providerpanel.FieldTimeout
	providerFieldRetries   = providerpanel.FieldRetries
	providerFieldUpstream  = providerpanel.FieldUpstream
	providerFieldContext   = providerpanel.FieldContext
	providerFieldMaxTokens = providerpanel.FieldMaxTokens
	providerFieldEffort    = providerpanel.FieldEffort
)

// providerFormInputWidth is the width of the form's text input.
const providerFormInputWidth = providerpanel.FormInputWidth

// providerFormInputLimit bounds what a single field can hold.
const providerFormInputLimit = providerpanel.FormInputLimit

// ---- the panel ---------------------------------------------------------

// reloadProviderRows re-reads the configuration and rebuilds the rows.
//
// The panel reads the file on every open and after every write: the file
// is shared with the CLI and the wrapper, so the panel must not hold a
// stale copy of it.
func (m *runMode) reloadProviderRows() {
	providers, err := models.ProviderEditor().List()
	if err != nil {
		m.panel.prov.providers = nil
		m.panel.prov.rows = nil
		m.panel.prov.notice = err.Error()
		return
	}
	m.panel.prov.providers = providers
	m.panel.prov.rows = providerPanelRows(providers)
	if m.panel.cursor >= len(m.panel.prov.rows) {
		m.panel.cursor = 0
	}
	m.panel.prov.confirm = -1
}

// providerPanelRows builds the rows from the provider section.
func providerPanelRows(providers map[string]models.ProviderConfig) []providerRow {
	return providerpanel.BuildRows(providers)
}

// providerNotice sets the panel's notice line.
func (m *runMode) providerNotice(msg string) {
	m.panel.prov.notice = msg
}

// providerVerifyLine renders the verification outcome: the probe that is
// in flight, or the last probe's answer.
//
// The answer is the panel's feedback on the credential — "the credential
// was rejected" and "the endpoint could not be reached" call for different
// fixes, so the probe's wording names the difference.
func (m *runMode) providerVerifyLine() string {
	if m.panel.prov.verifying {
		return panelGray238 + "verifying " + m.panel.prov.verifyLabel + "…" + panelGray238C
	}
	result := m.panel.prov.verify
	if result == nil {
		return ""
	}
	mark := panelVerifyFailOpen + "✗" + panelVerifyFailClose
	if result.OK {
		mark = panelVerifyOKOpen + "✓" + panelVerifyOKClose
	}
	detail := result.Detail
	if detail == "" {
		detail = "no answer"
	}
	label := m.panel.prov.verifyLabel
	if label == "" {
		return mark + " " + detail
	}
	return mark + " " + label + ": " + detail
}

// ---- rendering ---------------------------------------------------------

// panelProviderLines renders the /provider panel.
func (m *runMode) panelProviderLines() []string {
	if m.panel.prov.form != nil {
		return m.panelProviderFormLines()
	}
	lines := []string{panelTitleOpen + "Providers" + panelTitleClose, ""}
	rows := m.panel.prov.rows
	if len(rows) == 0 {
		lines = append(lines, "  "+panelGray238+"No providers configured."+panelGray238C)
	}
	for i, row := range rows {
		lines = append(lines, m.providerRowLine(row, i == m.panel.cursor))
	}
	if m.panel.prov.notice != "" {
		lines = append(lines, "", "  "+panelGray238+m.panel.prov.notice+panelGray238C)
	}
	if line := m.providerVerifyLine(); line != "" {
		lines = append(lines, "  "+line)
	}
	lines = append(lines, "", "  "+panelGray238+models.ProvidersPath()+panelGray238C)
	lines = append(lines, panelKeyboardHint("↑/↓", "Navigate", "a", "Add provider",
		"m", "Add model", "enter", "Edit", "d", "Delete", "v", "Verify", "esc", "Exit"))
	return lines
}

// providerRowLine renders one row of the list.
func (m *runMode) providerRowLine(row providerRow, cursor bool) string {
	switch row.Kind {
	case providerRowProvider:
		p := m.panel.prov.providers[row.Provider]
		return providerRowText(row.Provider, providerpanel.ProviderRowValue(p), cursor, false)
	case providerRowModel:
		p := m.panel.prov.providers[row.Provider]
		return providerRowText(row.Model, providerpanel.ModelRowValue(p, row.Model), cursor, true)
	case providerRowBuiltin:
		if row.Model == "" {
			// The group's header.
			return "  " + panelGray238 + row.Provider + panelGray238C
		}
		return providerRowText(row.Model, row.Label, cursor, true)
	}
	return ""
}

// providerRowText renders a row with the cursor marker.
func providerRowText(name, value string, cursor, indent bool) string {
	return providerpanel.RowText(name, value, cursor, indent, cBlueOpen, cBlueClose)
}

// providerEndpoint is the URL a provider's requests go to.
func providerEndpoint(p models.ProviderConfig) string {
	return providerpanel.Endpoint(p)
}

// panelProviderFormLines renders the edit form.
func (m *runMode) panelProviderFormLines() []string {
	form := m.panel.prov.form
	if form == nil {
		return nil
	}
	lines := []string{panelTitleOpen + form.Title() + panelTitleClose, ""}
	for i, f := range form.Fields {
		lines = append(lines, m.providerFormFieldLine(f, i == form.Cursor))
	}
	if m.panel.prov.notice != "" {
		lines = append(lines, "", "  "+panelGray238+m.panel.prov.notice+panelGray238C)
	}
	if line := m.providerVerifyLine(); line != "" {
		lines = append(lines, "  "+line)
	}
	hint := panelKeyboardHint("↑/↓", "Field", "enter", "Save", "v", "Verify", "esc", "Cancel")
	if form.HasChoice() {
		hint = panelKeyboardHint("↑/↓", "Field", "←/→", "Change", "enter", "Save", "v", "Verify", "esc", "Cancel")
	}
	lines = append(lines, "", hint)
	return lines
}

// providerFormFieldLine renders one field of the form.
func (m *runMode) providerFormFieldLine(f providerFormField, cursor bool) string {
	pad := 20 - len(f.Label)
	if pad < 1 {
		pad = 1
	}
	value := f.Display()
	if cursor {
		switch f.Kind {
		case providerFieldChoice:
			value = "‹ " + value + " ›"
		case providerFieldText:
			value = m.panel.textInput.View()
		}
		return cBlueOpen + "> " + f.Label + cBlueClose + strings.Repeat(" ", pad) +
			cBlueOpen + value + cBlueClose
	}
	return "  " + f.Label + strings.Repeat(" ", pad) + value
}

// ---- opening the form --------------------------------------------------

// openProviderEditForm opens the form for the row under the cursor.
func (m *runMode) openProviderEditForm() {
	rows := m.panel.prov.rows
	if m.panel.cursor < 0 || m.panel.cursor >= len(rows) {
		return
	}
	row := rows[m.panel.cursor]
	switch row.Kind {
	case providerRowProvider:
		p, ok := m.panel.prov.providers[row.Provider]
		if !ok {
			m.providerNotice(fmt.Sprintf("no provider named %q", row.Provider))
			return
		}
		m.openProviderForm(providerFormProvider, row.Provider, "", p)
	case providerRowModel:
		p, ok := m.panel.prov.providers[row.Provider]
		if !ok {
			m.providerNotice(fmt.Sprintf("no provider named %q", row.Provider))
			return
		}
		for _, model := range p.Models {
			if model.ID == row.Model {
				m.openProviderForm(providerFormModel, row.Provider, row.Model, model)
				return
			}
		}
	case providerRowBuiltin:
		m.providerNotice("the built-in models are selected with /model")
	}
}

// openProviderAddForm opens the form for a new provider.
func (m *runMode) openProviderAddForm() {
	m.openProviderForm(providerFormProvider, "", "", models.ProviderConfig{})
}

// openProviderAddModelForm opens the form for a new model under the
// provider the cursor is on (or under the first provider).
func (m *runMode) openProviderAddModelForm() {
	provider := ""
	rows := m.panel.prov.rows
	if m.panel.cursor >= 0 && m.panel.cursor < len(rows) {
		row := rows[m.panel.cursor]
		switch row.Kind {
		case providerRowProvider:
			provider = row.Provider
		case providerRowModel:
			provider = row.Provider
		}
	}
	if provider == "" {
		ids := models.SortedProviderIDs(m.panel.prov.providers)
		if len(ids) == 0 {
			m.providerNotice("add a provider first")
			return
		}
		provider = ids[0]
	}
	m.openProviderForm(providerFormModel, provider, "", models.ProviderModel{})
}

// openProviderForm builds the form and puts it on the panel.
func (m *runMode) openProviderForm(kind providerFormKind, provider, model string, entry any) {
	m.panel.prov.notice = ""
	form := &providerpanel.Form{Kind: kind, Provider: provider, Model: model}
	switch kind {
	case providerFormProvider:
		p, _ := entry.(models.ProviderConfig)
		form.Fields = providerFormFields(p, provider == "")
		form.StoredKey = p.APIKey
		form.HiddenKey = p.APIKey != "" && !strings.HasPrefix(strings.TrimSpace(p.APIKey), "$")
	case providerFormModel:
		model, _ := entry.(models.ProviderModel)
		form.Fields = modelFormFields(model, model.ID == "")
	}
	m.panel.prov.form = form
	m.providerFormSync()
}

// providerFormFields builds a provider form's fields.
//
// A new provider has to name itself; an existing one cannot be renamed —
// its id is the map key every other reference to it uses.
func providerFormFields(p models.ProviderConfig, isNew bool) []providerFormField {
	return providerpanel.ProviderFields(p, isNew)
}

// modelFormFields builds a model form's fields.
func modelFormFields(m models.ProviderModel, isNew bool) []providerFormField {
	return providerpanel.ModelFields(m, isNew)
}
