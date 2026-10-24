package providerpanel

import (
	"testing"

	"github.com/justwasm/pigasus/internal/models"
)

func TestFormTitle(t *testing.T) {
	cases := []struct {
		form Form
		want string
	}{
		{Form{Kind: FormProvider}, "Add provider"},
		{Form{Kind: FormProvider, Provider: "gw"}, "Provider · gw"},
		{Form{Kind: FormModel, Provider: "gw"}, "Add model · gw"},
		{Form{Kind: FormModel, Provider: "gw", Model: "m3"}, "Model · m3"},
		{Form{Kind: FormKind(99)}, "Providers"},
	}
	for _, c := range cases {
		if got := c.form.Title(); got != c.want {
			t.Errorf("Title() = %q, want %q", got, c.want)
		}
	}
}

func TestFormFieldAndValue(t *testing.T) {
	f := &Form{
		Fields: []FormField{
			{Label: FieldName, Value: "gateway"},
			{Label: FieldTimeout, Value: "30"},
		},
		Cursor: 1,
	}
	if got := f.Field().Label; got != FieldTimeout {
		t.Errorf("Field() = %q, want %q", got, FieldTimeout)
	}
	if got := f.Value(FieldName); got != "gateway" {
		t.Errorf("Value(Name) = %q, want %q", got, "gateway")
	}
	if got := f.Value("nope"); got != "" {
		t.Errorf("Value(nope) = %q, want it empty", got)
	}
	if got := f.IntValue(FieldTimeout); got != 30 {
		t.Errorf("IntValue(Timeout) = %d, want 30", got)
	}
	f.Fields[1].Value = "not a number"
	if got := f.IntValue(FieldTimeout); got != 0 {
		t.Errorf("IntValue(Timeout) = %d, want 0 for a non-number", got)
	}
}

func TestFormOutOfRangeCursor(t *testing.T) {
	f := &Form{Fields: []FormField{{Label: FieldName}}}
	f.Cursor = 5
	if got := f.Field().Label; got != "" {
		t.Errorf("Field() = %q, want the zero field", got)
	}
	empty := &Form{}
	if got := empty.Field().Label; got != "" {
		t.Errorf("Field() on an empty form = %q, want the zero field", got)
	}
	var nilForm *Form
	if got := nilForm.Field().Label; got != "" {
		t.Errorf("Field() on a nil form = %q, want the zero field", got)
	}
}

func TestFormHasChoice(t *testing.T) {
	plain := &Form{Fields: []FormField{{Label: FieldName, Kind: FieldText}}}
	if plain.HasChoice() {
		t.Error("HasChoice() = true, want false for a text-only form")
	}
	withChoice := &Form{Fields: []FormField{{Label: FieldType, Kind: FieldChoice}}}
	if !withChoice.HasChoice() {
		t.Error("HasChoice() = false, want true for a form with a choice field")
	}
	var nilForm *Form
	if nilForm.HasChoice() {
		t.Error("HasChoice() on a nil form = true, want false")
	}
}

func TestFormFieldDisplay(t *testing.T) {
	cases := []struct {
		field FormField
		want  string
	}{
		{FormField{Kind: FieldChoice, Value: "openai"}, "openai"},
		{FormField{Kind: FieldChoice}, "(unset)"},
		{FormField{Kind: FieldText, Value: "hello"}, "hello"},
		{FormField{Kind: FieldText, Secret: true}, "(not set)"},
		{FormField{Kind: FieldText, Secret: true, Note: "(hidden)"}, "(hidden)"},
	}
	for _, c := range cases {
		if got := c.field.Display(); got != c.want {
			t.Errorf("Display() = %q, want %q", got, c.want)
		}
	}
}

func TestProviderFields(t *testing.T) {
	// A new provider names itself; an existing one does not.
	newFields := ProviderFields(models.ProviderConfig{}, true)
	if newFields[0].Label != FieldID {
		t.Errorf("the first field = %q, want %q", newFields[0].Label, FieldID)
	}
	existing := ProviderFields(models.ProviderConfig{ID: "gw"}, false)
	for _, f := range existing {
		if f.Label == FieldID {
			t.Error("an existing provider has an ID field, want none")
		}
	}
	// The credential field never shows a literal.
	literal := ProviderFields(models.ProviderConfig{APIKey: "sk-secret"}, false)
	for _, f := range literal {
		if f.Label == FieldAPIKey && f.Value != "" {
			t.Errorf("the credential field shows %q, want it hidden", f.Value)
		}
	}
	// An environment reference is shown verbatim.
	env := ProviderFields(models.ProviderConfig{APIKey: "$GW_KEY"}, false)
	for _, f := range env {
		if f.Label == FieldAPIKey && f.Value != "$GW_KEY" {
			t.Errorf("the credential field = %q, want %q", f.Value, "$GW_KEY")
		}
	}
}

func TestModelFields(t *testing.T) {
	fields := ModelFields(models.ProviderModel{ID: "m3", Name: "M3"}, false)
	if len(fields) == 0 {
		t.Fatal("ModelFields returned no fields")
	}
	for _, f := range fields {
		if f.Label == FieldID {
			t.Error("an existing model has an ID field, want none")
		}
	}
	withID := ModelFields(models.ProviderModel{}, true)
	if withID[0].Label != FieldID {
		t.Errorf("the first field = %q, want %q", withID[0].Label, FieldID)
	}
}

func TestProviderConfig(t *testing.T) {
	f := &Form{
		Provider: "gw",
		Fields: []FormField{
			{Label: FieldName, Value: " Gateway "},
			{Label: FieldType, Value: "anthropic"},
			{Label: FieldBaseURL, Value: " https://host/anthropic "},
			{Label: FieldAPIURL, Value: " https://host/full "},
			{Label: FieldAPIKey, Value: " sk-typed "},
			{Label: FieldTimeout, Value: "12"},
			{Label: FieldRetries, Value: "3"},
		},
	}
	p, ok := ProviderConfig(f, models.ProviderConfig{})
	if !ok {
		t.Fatal("ProviderConfig returned ok=false, want true")
	}
	if p.ID != "gw" {
		t.Errorf("ID = %q, want %q", p.ID, "gw")
	}
	if p.Name != "Gateway" {
		t.Errorf("Name = %q, want the trimmed %q", p.Name, "Gateway")
	}
	if p.Type != "anthropic" {
		t.Errorf("Type = %q, want %q", p.Type, "anthropic")
	}
	if p.BaseURL != "https://host/anthropic" {
		t.Errorf("BaseURL = %q, want it trimmed", p.BaseURL)
	}
	if p.APIURL != "https://host/full" {
		t.Errorf("APIURL = %q, want it trimmed", p.APIURL)
	}
	if p.APIKey != "sk-typed" {
		t.Errorf("APIKey = %q, want the typed key", p.APIKey)
	}
	if p.TimeoutSeconds != 12 || p.MaxRetries != 3 {
		t.Errorf("timeout/retries = %d/%d, want 12/3", p.TimeoutSeconds, p.MaxRetries)
	}
}

func TestProviderConfigNeedsAnID(t *testing.T) {
	f := &Form{Fields: []FormField{{Label: FieldName, Value: "x"}}}
	if _, ok := ProviderConfig(f, models.ProviderConfig{}); ok {
		t.Error("ProviderConfig returned ok=true, want false without an id")
	}
}

func TestProviderConfigCredentialRules(t *testing.T) {
	// An empty field on a hidden literal keeps the stored credential.
	hidden := &Form{
		Provider:  "gw",
		StoredKey: "sk-stored",
		HiddenKey: true,
		Fields:    []FormField{{Label: FieldAPIKey}},
	}
	p, _ := ProviderConfig(hidden, models.ProviderConfig{APIKey: "sk-stored"})
	if p.APIKey != "sk-stored" {
		t.Errorf("APIKey = %q, want the stored %q", p.APIKey, "sk-stored")
	}

	// An empty field on an environment reference keeps the reference.
	env := &Form{
		Provider:  "gw",
		StoredKey: "$GW_KEY",
		Fields:    []FormField{{Label: FieldAPIKey}},
	}
	p, _ = ProviderConfig(env, models.ProviderConfig{})
	if p.APIKey != "$GW_KEY" {
		t.Errorf("APIKey = %q, want the stored %q", p.APIKey, "$GW_KEY")
	}

	// A typed key wins over the stored one.
	typed := &Form{
		Provider:  "gw",
		StoredKey: "$GW_KEY",
		HiddenKey: true,
		Fields:    []FormField{{Label: FieldAPIKey, Value: "sk-new"}},
	}
	p, _ = ProviderConfig(typed, models.ProviderConfig{})
	if p.APIKey != "sk-new" {
		t.Errorf("APIKey = %q, want the typed %q", p.APIKey, "sk-new")
	}
}

func TestProviderModel(t *testing.T) {
	f := &Form{
		Provider: "gw",
		Model:    "m3",
		Fields: []FormField{
			{Label: FieldName, Value: "M3"},
			{Label: FieldUpstream, Value: " minimax-m3 "},
			{Label: FieldContext, Value: "128000"},
			{Label: FieldMaxTokens, Value: "4096"},
			{Label: FieldEffort, Value: "high"},
		},
	}
	m, ok := ProviderModel(f, models.ProviderModel{})
	if !ok {
		t.Fatal("ProviderModel returned ok=false, want true")
	}
	if m.ID != "m3" {
		t.Errorf("ID = %q, want %q", m.ID, "m3")
	}
	if m.Model != "minimax-m3" {
		t.Errorf("Model = %q, want it trimmed", m.Model)
	}
	if m.ContextWindow != 128000 || m.DefaultMaxTokens != 4096 {
		t.Errorf("ctx/max = %d/%d, want 128000/4096", m.ContextWindow, m.DefaultMaxTokens)
	}
	if m.ReasoningEffort != "high" {
		t.Errorf("ReasoningEffort = %q, want %q", m.ReasoningEffort, "high")
	}
}

func TestProviderModelNeedsAnID(t *testing.T) {
	f := &Form{Provider: "gw", Fields: []FormField{{Label: FieldName, Value: "x"}}}
	if _, ok := ProviderModel(f, models.ProviderModel{}); ok {
		t.Error("ProviderModel returned ok=true, want false without an id")
	}
}

func TestProbeModel(t *testing.T) {
	cases := []struct {
		list []models.ProviderModel
		want string
	}{
		{nil, ""},
		{[]models.ProviderModel{{ID: "a"}}, "a"},
		{[]models.ProviderModel{{ID: "a", Model: "wire-a"}}, "wire-a"},
		{[]models.ProviderModel{{ID: "a", Model: "  "}}, "a"},
		{[]models.ProviderModel{{ID: ""}}, ""},
	}
	for _, c := range cases {
		if got := ProbeModel(c.list); got != c.want {
			t.Errorf("ProbeModel(%v) = %q, want %q", c.list, got, c.want)
		}
	}
}

func TestEndpoint(t *testing.T) {
	// The endpoint is the *derived* URL: the API URL when set, otherwise
	// the base plus the protocol's path.
	if got := Endpoint(models.ProviderConfig{APIURL: "https://host/full"}); got != "https://host/full" {
		t.Errorf("Endpoint = %q, want the API URL", got)
	}
	if got := Endpoint(models.ProviderConfig{BaseURL: "https://host/v1"}); got != "https://host/v1/chat/completions" {
		t.Errorf("Endpoint = %q, want the OpenAI path", got)
	}
	if got := Endpoint(models.ProviderConfig{BaseURL: "https://host/anthropic", Type: models.ProviderTypeAnthropic}); got != "https://host/anthropic/v1/messages" {
		t.Errorf("Endpoint = %q, want the Anthropic path", got)
	}
	if got := Endpoint(models.ProviderConfig{}); got != "(no endpoint)" {
		t.Errorf("Endpoint = %q, want the placeholder", got)
	}
}
