// panel_form.go — the /provider panel's edit form.
//
// The form is a list of fields: the cursor is on one of them, the text
// field under the cursor owns the keyboard's characters, and the choice
// fields cycle through a fixed set of values.
//
// The panel owns its own text input. The host's shared one belongs to the
// rename and pool panels, and the provider form is never open at the same
// time as either of them — but a shared field would still be a field two
// panels could write to, and this package does not depend on the host.

package providerpanel

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"

	"github.com/justwasm/pigasus/internal/features/models"
)

// openEditForm opens the form for the row under the cursor.
//
// The row decides what is edited: a provider opens the provider form, a
// model opens the model form, the built-in group says the built-ins are
// selected elsewhere, and a preset either opens its own form or starts
// the browser login it asks for.
func (p *Panel) openEditForm() {
	if p == nil {
		return
	}
	row, ok := p.CurrentRow()
	if !ok {
		return
	}
	switch row.Kind {
	case RowProvider:
		cfg, ok := p.providers[row.Provider]
		if !ok {
			p.Notice(fmt.Sprintf("no provider named %q", row.Provider))
			return
		}
		p.openForm(FormProvider, row.Provider, "", cfg)
	case RowModel:
		cfg, ok := p.providers[row.Provider]
		if !ok {
			p.Notice(fmt.Sprintf("no provider named %q", row.Provider))
			return
		}
		for _, model := range cfg.EffectiveModels() {
			if model.ID == row.Model {
				p.openForm(FormModel, row.Provider, row.Model, model)
				return
			}
		}
	case RowBuiltin:
		p.Notice("the built-in models are selected with /model")
	case RowPreset:
		if row.Label == "" {
			p.Notice("select a preset provider below to configure it")
			return
		}
		if models.CatwalkAuthMethod(row.Provider) == "browser" {
			// The command is dropped here, exactly as the panel has
			// always dropped it: the flow starts, but the poll that
			// carries its answer does not. The Enter key path returns
			// the command; this path does not.
			p.startOAuth(row.Provider)
			return
		}
		preset, ok := models.PresetProviderConfig(row.Provider)
		if !ok {
			p.Notice(fmt.Sprintf("no preset named %q", row.Provider))
			return
		}
		p.openForm(FormProvider, row.Provider, "", preset)
		// For a preset provider, focus directly on the API Key field
		if form := p.form; form != nil {
			for i, f := range form.Fields {
				if f.Label == FieldAPIKey {
					form.Cursor = i
					p.formSync()
					break
				}
			}
		}
	}
}

// openAddForm opens the form for a new provider.
func (p *Panel) openAddForm() {
	if p == nil {
		return
	}
	p.openForm(FormProvider, "", "", models.ProviderConfig{})
}

// openAddModelForm opens the form for a new model under the provider the
// cursor is on (or under the first provider).
func (p *Panel) openAddModelForm() {
	if p == nil {
		return
	}
	provider := ""
	if row, ok := p.CurrentRow(); ok {
		switch row.Kind {
		case RowProvider:
			provider = row.Provider
		case RowModel:
			provider = row.Provider
		}
	}
	if provider == "" {
		ids := models.SortedProviderIDs(p.providers)
		if len(ids) == 0 {
			p.Notice("add a provider first")
			return
		}
		provider = ids[0]
	}
	p.openForm(FormModel, provider, "", models.ProviderModel{})
}

// openForm builds the form and puts it on the panel.
func (p *Panel) openForm(kind FormKind, provider, model string, entry any) {
	if p == nil {
		return
	}
	p.notice = ""
	form := &Form{Kind: kind, Provider: provider, Model: model}
	switch kind {
	case FormProvider:
		cfg, _ := entry.(models.ProviderConfig)
		form.Fields = ProviderFields(cfg, provider == "")
		form.StoredKey = cfg.APIKey
		form.HiddenKey = cfg.APIKey != "" && !strings.HasPrefix(strings.TrimSpace(cfg.APIKey), "$")
	case FormModel:
		m, _ := entry.(models.ProviderModel)
		form.Fields = ModelFields(m, m.ID == "")
	}
	p.form = form
	p.formSync()
}

// formMove moves the form's cursor by delta fields.
//
// Leaving the id field of a *new* provider fills the rest of the form
// from the preset catalogue: the user names a preset and the fields it
// implies — its name, its wire type, its endpoints — arrive already
// filled in.
func (p *Panel) formMove(delta int) {
	if p == nil {
		return
	}
	form := p.form
	if form == nil || len(form.Fields) == 0 {
		return
	}
	p.formStore()
	if form.Kind == FormProvider && form.Provider == "" && form.Cursor == 0 {
		typedID := strings.ToLower(strings.TrimSpace(form.Fields[0].Value))
		if preset, ok := models.PresetProviderConfig(typedID); ok {
			for i := range form.Fields {
				switch form.Fields[i].Label {
				case FieldName:
					if form.Fields[i].Value == "" {
						form.Fields[i].Value = preset.Name
					}
				case FieldType:
					form.Fields[i].Value = preset.WireType()
				case FieldBaseURL:
					if form.Fields[i].Value == "" {
						form.Fields[i].Value = preset.BaseURL
					}
				case FieldAPIURL:
					if form.Fields[i].Value == "" && preset.APIURL != "" {
						form.Fields[i].Value = preset.APIURL
					}
				case FieldAPIKey:
					auth := models.CatwalkAuthMethod(typedID)
					if auth == "browser" {
						form.Fields[i].Placeholder = "browser login / OAuth (leave empty to use browser auth)"
						form.Fields[i].Note = "(authenticates via browser login)"
					} else if auth == "both" {
						form.Fields[i].Placeholder = "$ENV_VAR, API key, or leave empty for browser login"
						form.Fields[i].Note = "(API key or browser login)"
					}
				}
			}
		}
	}
	next := form.Cursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(form.Fields) {
		next = len(form.Fields) - 1
	}
	form.Cursor = next
	p.formSync()
}

// formStore writes the text input's value back into the field the cursor
// was on.
func (p *Panel) formStore() {
	if p == nil {
		return
	}
	form := p.form
	if form == nil || form.Cursor < 0 || form.Cursor >= len(form.Fields) {
		return
	}
	if form.Fields[form.Cursor].Kind != FieldText {
		return
	}
	form.Fields[form.Cursor].Value = p.input.Value()
}

// formCycle moves a choice field to the next (or previous) value.
func (p *Panel) formCycle(delta int) {
	if p == nil {
		return
	}
	form := p.form
	if form == nil || form.Cursor < 0 || form.Cursor >= len(form.Fields) {
		return
	}
	f := form.Fields[form.Cursor]
	if f.Kind != FieldChoice || len(f.Choices) == 0 {
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

// formSync loads the field the cursor is on into the text input.
func (p *Panel) formSync() {
	if p == nil {
		return
	}
	form := p.form
	if form == nil {
		return
	}
	f := form.Field()
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = f.Placeholder
	in.CharLimit = FormInputLimit
	in.SetWidth(FormInputWidth)
	in.Focus()
	if f.Kind == FieldText {
		in.SetValue(f.Value)
	}
	p.input = in
}
