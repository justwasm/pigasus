// slash_panel_provider_keys.go — the /provider panel's key handling.
//
// # What it is
//
// The companion of slash_panel_provider.go: the panel's list and form
// rendering live there, this file owns every key the panel receives.
//
// # Two modes
//
// The panel has a list mode and a form mode, and both are handled here so
// the panel never falls through to the generic navigation handler — the
// form's arrow keys move between fields, not between rows.
package cliapp

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/models"
	"github.com/justwasm/pigasus/internal/providerpanel"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// ---- keys --------------------------------------------------------------

// handleProviderPanelKey owns every key the /provider panel receives.
//
// The panel has two modes: the list and the edit form. Both are handled
// here so the panel never falls through to the generic navigation
// handler — the form's arrow keys move between fields, not between rows.
func (m *runMode) handleProviderPanelKey(key tea.Key) (bool, tea.Cmd) {
	if m.panel.prov.form != nil {
		return m.handleProviderFormKey(key)
	}
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		m.providerMoveCursor(-1)
		return true, nil
	case tuilib.NavDown:
		m.providerMoveCursor(+1)
		return true, nil
	case tuilib.NavPageUp:
		m.providerMoveCursor(-10)
		return true, nil
	case tuilib.NavPageDown:
		m.providerMoveCursor(+10)
		return true, nil
	case tuilib.NavTop:
		m.panel.cursor = 0
		m.panel.prov.confirm = -1
		return true, nil
	case tuilib.NavBottom:
		if n := len(m.panel.prov.rows); n > 0 {
			m.panel.cursor = n - 1
		}
		m.panel.prov.confirm = -1
		return true, nil
	case tuilib.NavCancel:
		return true, m.closePanel()
	case tuilib.NavSelect:
		m.openProviderEditForm()
		return true, nil
	}
	switch key.Code {
	case 'a':
		m.openProviderAddForm()
		return true, nil
	case 'm':
		m.openProviderAddModelForm()
		return true, nil
	case 'd':
		m.deleteProviderRow()
		return true, nil
	case 'v':
		return true, m.verifySelectedProvider()
	}
	return true, nil
}

// providerMoveCursor moves the list cursor by delta rows.
func (m *runMode) providerMoveCursor(delta int) {
	n := len(m.panel.prov.rows)
	if n == 0 {
		return
	}
	c := m.panel.cursor + delta
	if c < 0 {
		c = 0
	}
	if c >= n {
		c = n - 1
	}
	if c != m.panel.cursor {
		m.panel.prov.confirm = -1
	}
	m.panel.cursor = c
}

// handleProviderFormKey owns every key the edit form receives.
//
// Only the arrow keys, enter and escape are interpreted; every other key
// is typed into the field. (The generic list navigation is not used here:
// it would swallow "j", "k", "h", "l", "g" and "q" as navigation, and
// those are ordinary characters in a field.)
func (m *runMode) handleProviderFormKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyEsc:
		m.panel.prov.form = nil
		m.panel.prov.notice = ""
		return true, nil
	case tea.KeyEnter:
		return true, m.saveProviderForm()
	case 'v':
		return true, m.verifyProviderForm()
	case tea.KeyUp:
		m.providerFormMove(-1)
		return true, nil
	case tea.KeyDown:
		m.providerFormMove(+1)
		return true, nil
	case tea.KeyLeft:
		m.providerFormCycle(-1)
		return true, nil
	case tea.KeyRight:
		m.providerFormCycle(+1)
		return true, nil
	}
	// Everything else is text. A choice field has no text to type.
	if m.panel.prov.form.Field().Kind == providerFieldChoice {
		return true, nil
	}
	updated, cmd := m.panel.textInput.Update(keyMsgFromKey(key))
	m.panel.textInput = updated
	return true, cmd
}

// providerFormMove moves the form's cursor by delta fields.
func (m *runMode) providerFormMove(delta int) {
	form := m.panel.prov.form
	if form == nil || len(form.Fields) == 0 {
		return
	}
	m.providerFormStore()
	next := form.Cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(form.Fields) {
		next = len(form.Fields) - 1
	}
	form.Cursor = next
	m.providerFormSync()
}

// providerFormStore writes the textinput's value back into the field the
// cursor was on.
func (m *runMode) providerFormStore() {
	form := m.panel.prov.form
	if form == nil || form.Cursor < 0 || form.Cursor >= len(form.Fields) {
		return
	}
	if form.Fields[form.Cursor].Kind != providerFieldText {
		return
	}
	form.Fields[form.Cursor].Value = m.panel.textInput.Value()
}

// providerFormCycle moves a choice field to the next (or previous) value.
func (m *runMode) providerFormCycle(delta int) {
	form := m.panel.prov.form
	if form == nil || form.Cursor < 0 || form.Cursor >= len(form.Fields) {
		return
	}
	f := form.Fields[form.Cursor]
	if f.Kind != providerFieldChoice || len(f.Choices) == 0 {
		return
	}
	idx := 0
	for i, c := range f.Choices {
		if c == f.Value {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(f.Choices)) % len(f.Choices)
	form.Fields[form.Cursor].Value = f.Choices[idx]
}

// providerFormSync loads the field the cursor is on into the textinput.
func (m *runMode) providerFormSync() {
	form := m.panel.prov.form
	if form == nil {
		return
	}
	f := form.Field()
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = f.Placeholder
	in.CharLimit = providerFormInputLimit
	in.SetWidth(providerFormInputWidth)
	in.Focus()
	if f.Kind == providerFieldText {
		in.SetValue(f.Value)
	}
	m.panel.textInput = in
}

// ---- merged from slash_panel_provider_write.go ----
// slash_panel_provider_write.go — the /provider panel's write and
// verification half.
//
// # What it is
//
// The companion of slash_panel_provider.go: the panel's *list* and *form*
// live there, this file owns what happens when the form is saved — the
// write through models.Editor, the verification probe, and the delete.
//
// # One writer
//
// Every write goes through models.Editor — the same layer the clone's
// `agy provider` / `agy model` subcommands and the wrapper's
// `agy wrapper providers` / `models` commands use. The panel holds no
// configuration logic of its own: it builds a ProviderConfig (or a
// ProviderModel) and hands it to the editor, which rewrites the one field
// that changed and leaves the rest of the document alone.

// ---- writing -----------------------------------------------------------

// saveProviderForm writes the form's values back to the configuration.
//
// A write is followed by a verification probe: the panel's answer to
// "does this endpoint accept this credential?" is worth more than the
// save's own "saved provider gateway", and the probe is what tells the
// user that the key they just typed is the wrong one.
func (m *runMode) saveProviderForm() tea.Cmd {
	form := m.panel.prov.form
	if form == nil {
		return nil
	}
	m.providerFormStore()
	ed := models.ProviderEditor()
	providers, err := ed.List()
	if err != nil {
		m.providerNotice(err.Error())
		return nil
	}
	switch form.Kind {
	case providerFormProvider:
		return m.saveProviderFormFields(ed, providers, form)
	case providerFormModel:
		return m.saveModelFormFields(ed, providers, form)
	}
	return nil
}

// saveProviderFormFields writes a provider form.
func (m *runMode) saveProviderFormFields(ed *models.Editor, providers map[string]models.ProviderConfig, form *providerForm) tea.Cmd {
	id, p, probeModel, err := providerpanel.ValidateAndSaveProvider(ed, providers, form)
	if err != nil {
		m.providerNotice(err.Error())
		return nil
	}
	m.panel.prov.form = nil
	m.reloadProviderRows()
	m.providerNotice("saved provider " + id)
	return m.startProviderVerify(p, p.APIKey, probeModel, id)
}

// saveModelFormFields writes a model form.
func (m *runMode) saveModelFormFields(ed *models.Editor, providers map[string]models.ProviderConfig, form *providerForm) tea.Cmd {
	provider, wire, err := providerpanel.ValidateAndSaveModel(ed, providers, form)
	if err != nil {
		m.providerNotice(err.Error())
		return nil
	}
	id := strings.TrimSpace(form.Model)
	if id == "" {
		id = strings.TrimSpace(form.Value(providerFieldID))
	}
	m.panel.prov.form = nil
	m.reloadProviderRows()
	m.providerNotice("saved model " + id)
	// The new model is the one worth probing: it is the model the user
	// just named, and a probe against it proves the whole chain.
	p, ok := m.panel.prov.providers[provider]
	if !ok {
		return nil
	}
	return m.startProviderVerify(p, p.APIKey, wire, provider)
}

// ---- verification ------------------------------------------------------

// providerVerifyMsg is the outcome of a verification probe.
type providerVerifyMsg struct {
	result models.VerifyResult
	label  string
}

// providerVerifyCmd runs the probe off the UI thread.
//
// The panel is a modal: a network round trip in the key handler would
// freeze the whole TUI, so the probe is a command and its answer comes
// back as a message.
func providerVerifyCmd(p models.ProviderConfig, credential, modelID, label string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), models.VerifyTimeout)
		defer cancel()
		return providerVerifyMsg{
			result: models.VerifyProvider(ctx, p, credential, modelID),
			label:  label,
		}
	}
}

// startProviderVerify starts a probe and returns the command that carries
// its result back.
func (m *runMode) startProviderVerify(p models.ProviderConfig, credential, modelID, label string) tea.Cmd {
	m.panel.prov.verifying = true
	m.panel.prov.verify = nil
	m.panel.prov.verifyLabel = label
	return providerVerifyCmd(p, credential, modelID, label)
}

// verifySelectedProvider probes the provider under the cursor, with the
// credential the file holds.
func (m *runMode) verifySelectedProvider() tea.Cmd {
	rows := m.panel.prov.rows
	if m.panel.cursor < 0 || m.panel.cursor >= len(rows) {
		return nil
	}
	row := rows[m.panel.cursor]
	if row.Kind != providerRowProvider && row.Kind != providerRowModel {
		m.providerNotice("the built-in models are selected with /model")
		return nil
	}
	p, ok := m.panel.prov.providers[row.Provider]
	if !ok {
		m.providerNotice(fmt.Sprintf("no provider named %q", row.Provider))
		return nil
	}
	return m.startProviderVerify(p, p.APIKey, providerProbeModel(p.Models), row.Provider)
}

// verifyProviderForm probes the form's current values: the endpoint and
// the credential the user has typed, before anything is saved.
//
// This is the flow crush's API-key dialog has: type the key, see whether
// the provider accepts it, then commit.
func (m *runMode) verifyProviderForm() tea.Cmd {
	form := m.panel.prov.form
	if form == nil {
		return nil
	}
	m.providerFormStore()
	p := models.ProviderConfig{
		ID:      strings.TrimSpace(form.Provider),
		Type:    form.Value(providerFieldType),
		BaseURL: strings.TrimSpace(form.Value(providerFieldBaseURL)),
		APIURL:  strings.TrimSpace(form.Value(providerFieldAPIURL)),
	}
	key := strings.TrimSpace(form.Value(providerFieldAPIKey))
	if key == "" {
		// The field is empty: either the provider has no credential or
		// the stored one is a literal the field does not show.
		key = form.StoredKey
	}
	p.APIKey = key
	if p.Endpoint() == "" {
		m.providerNotice("the provider needs a base URL or an API URL")
		return nil
	}
	label := p.ID
	if label == "" {
		label = "provider"
	}
	var probe []models.ProviderModel
	if stored, ok := m.panel.prov.providers[p.ID]; ok {
		probe = stored.Models
	}
	return m.startProviderVerify(p, key, providerProbeModel(probe), label)
}

// providerProbeModel is the wire id of the first model in the list — the
// one the probe talks to. "" means the probe falls back to a model
// listing, which needs no model id.
func providerProbeModel(list []models.ProviderModel) string {
	return providerpanel.ProbeModel(list)
}

// deleteProviderRow deletes the row under the cursor.
//
// The first `d` arms the delete and the second one carries it out: a
// provider can hold several models, and deleting one of them by accident
// is not recoverable.
func (m *runMode) deleteProviderRow() {
	rows := m.panel.prov.rows
	if m.panel.cursor < 0 || m.panel.cursor >= len(rows) {
		return
	}
	row := rows[m.panel.cursor]
	if row.Kind == providerRowBuiltin {
		m.providerNotice("the built-in models are selected with /model")
		return
	}
	if m.panel.prov.confirm != m.panel.cursor {
		m.panel.prov.confirm = m.panel.cursor
		m.providerNotice("press d again to delete")
		return
	}
	m.panel.prov.confirm = -1
	ed := models.ProviderEditor()
	switch row.Kind {
	case providerRowProvider:
		if err := ed.DeleteProvider(row.Provider); err != nil {
			m.providerNotice(err.Error())
			return
		}
		m.reloadProviderRows()
		m.providerNotice("removed provider " + row.Provider)
	case providerRowModel:
		provider, found, err := ed.DeleteModel(row.Model)
		if err != nil {
			m.providerNotice(err.Error())
			return
		}
		if !found {
			m.providerNotice(fmt.Sprintf("no model named %q", row.Model))
			return
		}
		m.reloadProviderRows()
		m.providerNotice("removed model " + row.Model + " from " + provider)
	}
}
