// panel_write.go — the /provider panel's write and verification half.
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
// # A write is followed by a probe
//
// A save is followed by a verification probe: the panel's answer to "does
// this endpoint accept this credential?" is worth more than the save's own
// "saved provider gateway", and the probe is what tells the user that the
// key they just typed is the wrong one.

package providerpanel

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/models"
)

// ---- writing -----------------------------------------------------------

// saveForm writes the form's values back to the configuration.
func (p *Panel) saveForm() tea.Cmd {
	if p == nil {
		return nil
	}
	form := p.form
	if form == nil {
		return nil
	}
	p.formStore()
	ed := models.ProviderEditor()
	providers, err := ed.List()
	if err != nil {
		p.Notice(err.Error())
		return nil
	}
	switch form.Kind {
	case FormProvider:
		return p.saveProviderFormFields(ed, providers, form)
	case FormModel:
		return p.saveModelFormFields(ed, providers, form)
	}
	return nil
}

// saveProviderFormFields writes a provider form.
func (p *Panel) saveProviderFormFields(ed *models.Editor, providers map[string]models.ProviderConfig, form *Form) tea.Cmd {
	id, cfg, probeModel, err := ValidateAndSaveProvider(ed, providers, form)
	if err != nil {
		p.Notice(err.Error())
		return nil
	}
	p.form = nil
	p.Reload()
	p.Notice("saved provider " + id)
	return p.startVerify(cfg, cfg.APIKey, probeModel, id)
}

// saveModelFormFields writes a model form.
func (p *Panel) saveModelFormFields(ed *models.Editor, providers map[string]models.ProviderConfig, form *Form) tea.Cmd {
	provider, wire, err := ValidateAndSaveModel(ed, providers, form)
	if err != nil {
		p.Notice(err.Error())
		return nil
	}
	id := strings.TrimSpace(form.Model)
	if id == "" {
		id = strings.TrimSpace(form.Value(FieldID))
	}
	p.form = nil
	p.Reload()
	p.Notice("saved model " + id)
	// The new model is the one worth probing: it is the model the user
	// just named, and a probe against it proves the whole chain.
	cfg, ok := p.providers[provider]
	if !ok {
		return nil
	}
	return p.startVerify(cfg, cfg.APIKey, wire, provider)
}

// ---- verification ------------------------------------------------------

// VerifyMsg is the outcome of a verification probe.
//
// The probe runs off the UI thread — the panel is a modal, and a network
// round trip in the key handler would freeze the whole TUI — so its
// answer comes back as a message.
type VerifyMsg struct {
	// Result is the probe's answer.
	Result models.VerifyResult
}

// verifyCmd runs the probe off the UI thread.
func verifyCmd(p models.ProviderConfig, credential, modelID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), models.VerifyTimeout)
		defer cancel()
		return VerifyMsg{
			Result: models.VerifyProvider(ctx, p, credential, modelID),
		}
	}
}

// startVerify starts a probe and returns the command that carries its
// result back.
func (p *Panel) startVerify(cfg models.ProviderConfig, credential, modelID, label string) tea.Cmd {
	if p == nil {
		return nil
	}
	p.verifying = true
	p.verify = nil
	p.verifyLabel = label
	return verifyCmd(cfg, credential, modelID)
}

// ApplyVerifyMsg records the outcome of a verification probe.
//
// The panel may have been closed while the request was in flight; the
// host decides whether the message still applies.
func (p *Panel) ApplyVerifyMsg(msg VerifyMsg) {
	if p == nil {
		return
	}
	result := msg.Result
	p.verifying = false
	p.verify = &result
}

// verifySelectedProvider probes the provider under the cursor, with the
// credential the file holds.
func (p *Panel) verifySelectedProvider() tea.Cmd {
	if p == nil {
		return nil
	}
	row, ok := p.CurrentRow()
	if !ok {
		return nil
	}
	if row.Kind != RowProvider && row.Kind != RowModel {
		if row.Kind == RowPreset {
			p.Notice("configure this preset first to verify it")
		} else {
			p.Notice("the built-in models are selected with /model")
		}
		return nil
	}
	cfg, ok := p.providers[row.Provider]
	if !ok {
		p.Notice(fmt.Sprintf("no provider named %q", row.Provider))
		return nil
	}
	return p.startVerify(cfg, cfg.APIKey, ProbeModel(cfg.Models), row.Provider)
}

// verifyForm probes the form's current values: the endpoint and the
// credential the user has typed, before anything is saved.
//
// This is the flow crush's API-key dialog has: type the key, see whether
// the provider accepts it, then commit.
func (p *Panel) verifyForm() tea.Cmd {
	if p == nil {
		return nil
	}
	form := p.form
	if form == nil {
		return nil
	}
	p.formStore()
	cfg := models.ProviderConfig{
		ID:      strings.TrimSpace(form.Provider),
		Type:    form.Value(FieldType),
		BaseURL: strings.TrimSpace(form.Value(FieldBaseURL)),
		APIURL:  strings.TrimSpace(form.Value(FieldAPIURL)),
	}
	key := strings.TrimSpace(form.Value(FieldAPIKey))
	if key == "" {
		// The field is empty: either the provider has no credential or
		// the stored one is a literal the field does not show.
		key = form.StoredKey
	}
	cfg.APIKey = key
	if cfg.Endpoint() == "" {
		p.Notice("the provider needs a base URL or an API URL")
		return nil
	}
	label := cfg.ID
	if label == "" {
		label = "provider"
	}
	var probe []models.ProviderModel
	if stored, ok := p.providers[cfg.ID]; ok && len(stored.Models) > 0 {
		probe = stored.Models
	} else if preset, ok := models.PresetProviderConfig(cfg.ID); ok {
		probe = preset.Models
	}
	return p.startVerify(cfg, key, ProbeModel(probe), label)
}

// ---- delete ------------------------------------------------------------

// deleteRow deletes the row under the cursor.
//
// The first `d` arms the delete and the second one carries it out: a
// provider can hold several models, and deleting one of them by accident
// is not recoverable.
func (p *Panel) deleteRow() {
	if p == nil {
		return
	}
	row, ok := p.CurrentRow()
	if !ok {
		return
	}
	if row.Kind == RowBuiltin {
		p.Notice("the built-in models are selected with /model")
		return
	}
	if row.Kind == RowPreset {
		p.Notice("preset providers are catalog entries, not configured ones")
		return
	}
	if p.confirm != p.cursor {
		p.confirm = p.cursor
		target := row.Provider
		if row.Kind == RowModel {
			target = "model " + row.Model + " (from " + row.Provider + ")"
		} else if row.Kind == RowProvider {
			target = "provider " + row.Provider
		}
		p.Notice("press d again to delete " + target)
		return
	}
	p.confirm = -1
	ed := models.ProviderEditor()
	switch row.Kind {
	case RowProvider:
		if err := ed.DeleteProvider(row.Provider); err != nil {
			p.Notice(err.Error())
			return
		}
		p.Reload()
		p.Notice("removed provider " + row.Provider)
	case RowModel:
		found, err := ed.DeleteModelFromProvider(row.Provider, row.Model)
		if err != nil {
			p.Notice(err.Error())
			return
		}
		if !found {
			p.Notice(fmt.Sprintf("no model named %q in %s", row.Model, row.Provider))
			return
		}
		p.Reload()
		p.Notice("removed model " + row.Model + " from " + row.Provider)
	}
}
