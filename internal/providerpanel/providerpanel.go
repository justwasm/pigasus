// Package providerpanel holds the pure parts of the /provider panel: the
// row model the list is built from, the edit form's field model, and the
// rules that turn a form's values back into a provider or a model.
//
// The TUI front-end (cmd/wagyu/slash_panel_provider.go) owns the
// interactive surface — the cursor, the textinput, the keys and the
// verification probe. Everything that is a *decision* rather than an
// interaction lives here, so it can be tested without a running TUI.
//
// # One writer
//
// The panel writes through models.Editor, the same layer the clone's
// `agy provider` / `agy model` subcommands and the wrapper's
// `agy wrapper providers` / `models` commands use. This package builds the
// ProviderConfig (or ProviderModel) the editor is handed; it never writes
// the file itself.
package providerpanel

import (
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/models"
)

// RowKind says what a row of the /provider list holds.
type RowKind int

const (
	// RowProvider is a provider header: the provider's id, its protocol
	// and its endpoint.
	RowProvider RowKind = iota
	// RowModel is a model listed under a provider.
	RowModel
	// RowBuiltin is one of the reference's own models. The row with an
	// empty model id is the group's header.
	RowBuiltin
)

// Row is one row of the /provider list.
type Row struct {
	Kind     RowKind
	Provider string // the provider id (provider and model rows)
	Model    string // the model id (model and builtin rows)
	Label    string // the display label (builtin rows)
}

// BuiltinProviderLabel is the name the reference's own models are listed
// under. They are served by Google's CloudCode endpoint, not by a
// configured provider, so the group says where they come from.
const BuiltinProviderLabel = "builtin (Google CloudCode)"

// FormKind says what a form edits.
type FormKind int

const (
	// FormProvider edits a provider.
	FormProvider FormKind = iota
	// FormModel edits a model under a provider.
	FormModel
)

// FieldKind says how a field is edited.
type FieldKind int

const (
	// FieldText is a free-text field, edited with the bubbles textinput.
	FieldText FieldKind = iota
	// FieldChoice cycles through a fixed set of values with ←/→.
	FieldChoice
)

// FormField is one editable field of the form.
type FormField struct {
	Label string
	Value string
	Kind  FieldKind
	// Choices are the values a choice field cycles through.
	Choices []string
	// Placeholder is the textinput's placeholder.
	Placeholder string
	// Note is what a non-cursor row shows when the value is empty.
	Note string
	// Secret marks the credential field: its value is never rendered and
	// an empty value keeps the stored credential.
	Secret bool
}

// Form is the /provider edit form's model.
type Form struct {
	Kind     FormKind
	Provider string // the provider the form writes to
	Model    string // the model the form writes to ("" = a new model)
	Fields   []FormField
	Cursor   int
	// StoredKey is the credential as the file holds it. The form shows
	// an environment reference verbatim and hides a literal.
	StoredKey string
	// HiddenKey is true when StoredKey is a literal: the field starts
	// empty and an empty field keeps the stored value.
	HiddenKey bool
}

// Form field labels. The form's fields are addressed by label, so the
// constants below are the single source of truth for both the form and the
// write.
const (
	FieldID        = "ID"
	FieldName      = "Name"
	FieldType      = "Type"
	FieldBaseURL   = "Base URL"
	FieldAPIURL    = "API URL"
	FieldAPIKey    = "API Key"
	FieldTimeout   = "Timeout (s)"
	FieldRetries   = "Max retries"
	FieldUpstream  = "Upstream id"
	FieldContext   = "Context window"
	FieldMaxTokens = "Max output tokens"
	FieldEffort    = "Reasoning effort"
	FieldTiers     = "Reasoning tiers"
)

// FormInputWidth is the width of the form's text input.
const FormInputWidth = 52

// FormInputLimit bounds what a single field can hold.
const FormInputLimit = 512

// Title is the form's heading.
func (f *Form) Title() string {
	switch f.Kind {
	case FormProvider:
		if f.Provider == "" {
			return "Add provider"
		}
		return "Provider · " + f.Provider
	case FormModel:
		if f.Model == "" {
			return "Add model · " + f.Provider
		}
		return "Model · " + f.Model
	}
	return "Providers"
}

// Field is the field the cursor is on.
func (f *Form) Field() FormField {
	if f == nil || len(f.Fields) == 0 {
		return FormField{}
	}
	if f.Cursor < 0 || f.Cursor >= len(f.Fields) {
		return FormField{}
	}
	return f.Fields[f.Cursor]
}

// HasChoice reports whether any field cycles through a fixed set.
func (f *Form) HasChoice() bool {
	if f == nil {
		return false
	}
	for _, field := range f.Fields {
		if field.Kind == FieldChoice {
			return true
		}
	}
	return false
}

// Value returns the form's value for a label.
func (f *Form) Value(label string) string {
	for _, field := range f.Fields {
		if field.Label == label {
			return field.Value
		}
	}
	return ""
}

// IntValue returns the form's value for a label as an integer. A value
// that is not a number is zero.
func (f *Form) IntValue(label string) int {
	n, err := strconv.Atoi(strings.TrimSpace(f.Value(label)))
	if err != nil {
		return 0
	}
	return n
}

// Display is what a field shows when it is not the cursor's.
func (f FormField) Display() string {
	if f.Kind == FieldChoice {
		if strings.TrimSpace(f.Value) == "" {
			return "(unset)"
		}
		return f.Value
	}
	if f.Secret && strings.TrimSpace(f.Value) == "" {
		if f.Note != "" {
			return f.Note
		}
		return "(not set)"
	}
	return f.Value
}

// ProviderFields builds a provider form's fields.
//
// A new provider has to name itself; an existing one cannot be renamed —
// its id is the map key every other reference to it uses.
func ProviderFields(p models.ProviderConfig, isNew bool) []FormField {
	fields := []FormField{}
	if isNew {
		fields = append(fields, FormField{
			Label:       FieldID,
			Placeholder: "the name you pass to --model and /model",
		})
	}
	keyField := FormField{
		Label:       FieldAPIKey,
		Placeholder: "$ENV_VAR, or a literal (hidden)",
		Secret:      true,
	}
	switch {
	case strings.TrimSpace(p.APIKey) == "":
		keyField.Note = "(not set)"
	case strings.HasPrefix(strings.TrimSpace(p.APIKey), "$"):
		// An environment reference is not a secret: it names a variable.
		keyField.Value = p.APIKey
	default:
		// A literal is never shown. An empty field keeps it.
		keyField.Note = "(hidden — leave empty to keep)"
	}
	fields = append(fields,
		FormField{Label: FieldName, Value: p.Name, Placeholder: "shown in /model"},
		FormField{
			Label:   FieldType,
			Value:   p.WireType(),
			Kind:    FieldChoice,
			Choices: []string{models.ProviderTypeOpenAI, models.ProviderTypeAnthropic},
		},
		FormField{Label: FieldBaseURL, Value: p.BaseURL,
			Placeholder: "https://host/v1"},
		FormField{Label: FieldAPIURL, Value: p.APIURL,
			Placeholder: "full endpoint, used verbatim"},
		keyField,
		FormField{Label: FieldTimeout, Value: strconv.Itoa(p.TimeoutSeconds)},
		FormField{Label: FieldRetries, Value: strconv.Itoa(p.MaxRetries)},
	)
	return fields
}

// ModelFields builds a model form's fields.
func ModelFields(m models.ProviderModel, isNew bool) []FormField {
	fields := []FormField{}
	if isNew {
		fields = append(fields, FormField{
			Label:       FieldID,
			Placeholder: "the name you pass to --model and /model",
		})
	}
	effort := strings.TrimSpace(m.ReasoningEffort)
	fields = append(fields,
		FormField{Label: FieldName, Value: m.Name, Placeholder: "shown in /model"},
		FormField{Label: FieldUpstream, Value: m.Model,
			Placeholder: "the id sent on the wire"},
		FormField{Label: FieldContext, Value: strconv.Itoa(m.ContextWindow)},
		FormField{Label: FieldMaxTokens, Value: strconv.Itoa(m.DefaultMaxTokens)},
		FormField{
			Label:   FieldEffort,
			Value:   effort,
			Kind:    FieldChoice,
			Choices: []string{"", "low", "medium", "high", "xhigh", "max", "default"},
		},
		// The tiers the model offers. A model that declares them is
		// listed once per tier — the reference's own shape — and the
		// tier rides the row's id.
		FormField{
			Label:       FieldTiers,
			Value:       strings.Join(m.ReasoningEfforts, ", "),
			Placeholder: "low, medium, high, xhigh, max",
		},
	)
	return fields
}

// ProviderConfig turns a provider form's values into the config the
// editor writes. ok is false when the form does not name a provider; the
// reason is the panel's notice.
//
// The existing config is the base: an edit keeps every field the form does
// not carry (the extra headers, for one), and the credential rule is the
// same one the panel shows — a typed key wins, an empty field on a hidden
// literal keeps the stored one, and an empty field on an environment
// reference keeps the reference.
func ProviderConfig(f *Form, existing models.ProviderConfig) (models.ProviderConfig, bool) {
	id := strings.TrimSpace(f.Provider)
	if id == "" {
		id = strings.TrimSpace(f.Value(FieldID))
	}
	if id == "" {
		return models.ProviderConfig{}, false
	}
	p := existing
	p.ID = id
	p.Name = strings.TrimSpace(f.Value(FieldName))
	p.Type = f.Value(FieldType)
	p.BaseURL = strings.TrimSpace(f.Value(FieldBaseURL))
	p.APIURL = strings.TrimSpace(f.Value(FieldAPIURL))
	key := strings.TrimSpace(f.Value(FieldAPIKey))
	switch {
	case key != "":
		p.APIKey = key
	case f.HiddenKey:
		// The field was empty and the stored credential is a literal:
		// keep it. The literal lives in the existing config, so the
		// assignment above already carried it over.
	default:
		// An environment reference is not a secret, so the form shows
		// it and the field is never empty for an existing provider.
		p.APIKey = f.StoredKey
	}
	p.TimeoutSeconds = f.IntValue(FieldTimeout)
	p.MaxRetries = f.IntValue(FieldRetries)
	return p, true
}

// ProviderModel turns a model form's values into the model the editor
// writes. ok is false when the form does not name a model.
//
// The existing entry is the base: a field the form does not carry (the
// model id, for an edit) keeps what the file already holds.
func ProviderModel(f *Form, existing models.ProviderModel) (models.ProviderModel, bool) {
	id := strings.TrimSpace(f.Model)
	if id == "" {
		id = strings.TrimSpace(f.Value(FieldID))
	}
	if id == "" {
		return models.ProviderModel{}, false
	}
	entry := existing
	entry.ID = id
	entry.Name = strings.TrimSpace(f.Value(FieldName))
	entry.Model = strings.TrimSpace(f.Value(FieldUpstream))
	entry.ContextWindow = f.IntValue(FieldContext)
	entry.DefaultMaxTokens = f.IntValue(FieldMaxTokens)
	entry.ReasoningEffort = f.Value(FieldEffort)
	entry.ReasoningEfforts = models.NormalizeEfforts(splitTiers(f.Value(FieldTiers)))
	return entry, true
}

// splitTiers splits the tiers field's comma-separated list.
func splitTiers(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// ProbeModel is the wire id of the first model in the list — the one the
// probe talks to. "" means the probe falls back to a model listing, which
// needs no model id.
func ProbeModel(list []models.ProviderModel) string {
	for _, m := range list {
		wire := strings.TrimSpace(m.Model)
		if wire == "" {
			wire = strings.TrimSpace(m.ID)
		}
		if wire != "" {
			return wire
		}
	}
	return ""
}

// Endpoint is the URL a provider's requests go to.
func Endpoint(p models.ProviderConfig) string {
	if e := strings.TrimSpace(p.Endpoint()); e != "" {
		return e
	}
	return "(no endpoint)"
}
