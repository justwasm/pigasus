// Package remotecontrol — modelenum_test.go: the Model enum on the
// wire.
//
// The reference's own server spells an enum by its NAME — the live
// GetUserStatus capture in out/ide-language-server/ reads
// `{"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"}}` — and the Web
// UI re-encodes the number it decoded into that same name. These
// tests pin both directions: the catalogue answers with the name, the
// app state accepts the name, and StartCascade resolves the name back
// to the catalogue entry.
package remotecontrol

import (
	"context"
	"encoding/json"
	"testing"
)

// enumCatalogue is a two-entry catalogue whose entries take the first
// two placeholder slots.
func enumCatalogue() []ModelInfo {
	return []ModelInfo{
		{ID: "mini-m3", Label: "MiniMax M3"},
		{ID: "mini-m3.1-flash", Label: "MiniMax M3.1 Flash"},
	}
}

// TestClientModelConfigsCarryTheEnumName pins the catalogue's wire
// form: the model choice is the *member* field of the oneof, spelled
// with the enum's name.
func TestClientModelConfigsCarryTheEnumName(t *testing.T) {
	models := enumCatalogue()
	configs, labels := userStatusClientModelConfigs(models)
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}
	if len(labels) != 2 || labels[0] != "MiniMax M3" {
		t.Fatalf("unexpected labels %v", labels)
	}
	want := []string{"MODEL_PLACEHOLDER_M0", "MODEL_PLACEHOLDER_M1"}
	for i, cfg := range configs {
		choice, ok := cfg["modelOrAlias"].(map[string]any)
		if !ok {
			t.Fatalf("config %d: modelOrAlias is %T, want a map", i, cfg["modelOrAlias"])
		}
		got, ok := choice["model"].(string)
		if !ok {
			t.Fatalf("config %d: model is %T, want the enum name as a string", i, choice["model"])
		}
		if got != want[i] {
			t.Errorf("config %d: model = %q, want %q", i, got, want[i])
		}
	}
}

// TestClientModelConfigOmitsDefaults pins proto-JSON's rule: a field
// holding its default is not emitted. The reference's own catalogue is
// thinner for it — no `disabled: false`, no empty `supportedMimeTypes`,
// no `groupName` on the sort group.
func TestClientModelConfigOmitsDefaults(t *testing.T) {
	models := []ModelInfo{
		{ID: "gemini-3.8-flash-high", Label: "Gemini 3.8 Flash (High)"},
		{ID: "minimax-m3", Label: "MiniMax M3"},
		{ID: "gpt-oss-120b-medium", Label: "GPT-OSS 120B Medium"},
	}
	configs, _ := userStatusClientModelConfigs(models)
	if _, ok := configs[0]["disabled"]; ok {
		t.Errorf("disabled must be omitted when it is false")
	}
	// A model that takes no picture declares no mime types, and an
	// empty map is a map's default: proto-JSON omits the field.
	if _, ok := configs[2]["supportedMimeTypes"]; ok {
		t.Errorf("an empty supportedMimeTypes must be omitted")
	}
	// A model that does take pictures has to say so: the Web UI's
	// attachment check reads this map, and an absent map reads as an
	// empty one — so a model that says nothing refuses every picture.
	mimes, ok := configs[0]["supportedMimeTypes"].(map[string]any)
	if !ok {
		t.Fatalf("a Gemini model must declare its supported mime types")
	}
	for _, mime := range []string{"image/jpeg", "image/png"} {
		if mimes[mime] != true {
			t.Errorf("supportedMimeTypes[%q] = %v, want true", mime, mimes[mime])
		}
	}
	// The vendor, the way the reference's own catalogue names it.
	if got := configs[0]["provider"]; got != "MODEL_PROVIDER_GOOGLE" {
		t.Errorf("provider = %v, want MODEL_PROVIDER_GOOGLE", got)
	}
	// A model of the user's own belongs to no vendor in the enum.
	if got := configs[1]["provider"]; got != "MODEL_PROVIDER_ANTIGRAVITY" {
		t.Errorf("provider = %v, want MODEL_PROVIDER_ANTIGRAVITY", got)
	}
	groups, ok := userStatusClientModelSorts([]string{"Gemini 3.8 Flash (High)"})[0]["groups"].([]map[string]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("unexpected sort shape")
	}
	if _, ok := groups[0]["groupName"]; ok {
		t.Errorf("the sort group must not carry a groupName")
	}
}

// TestModelEnumRoundTrip pins the name ↔ value mapping, including
// the placeholder range the reference reserves.
func TestModelEnumRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		value int
	}{
		{"MODEL_UNSPECIFIED", 0},
		{"MODEL_GOOGLE_GEMINI_2_5_PRO", 246},
		{"MODEL_CLAUDE_4_5_SONNET", 333},
		{"MODEL_OPENAI_GPT_OSS_120B_MEDIUM", 342},
		{"MODEL_GOOGLE_GEMINI_INTERNAL_BYOM", 326},
		{"MODEL_PLACEHOLDER_M0", 1000},
		{"MODEL_PLACEHOLDER_M1650", 2650},
	}
	for _, tc := range cases {
		value, ok := modelEnumValue(tc.name)
		if !ok {
			t.Errorf("modelEnumValue(%q) not found", tc.name)
			continue
		}
		if value != tc.value {
			t.Errorf("modelEnumValue(%q) = %d, want %d", tc.name, value, tc.value)
		}
	}
	if _, ok := modelEnumValue("MODEL_PLACEHOLDER_M1651"); ok {
		t.Errorf("MODEL_PLACEHOLDER_M1651 is outside the reference's range")
	}
	if _, ok := modelEnumValue("MODEL_NOT_A_MODEL"); ok {
		t.Errorf("an unknown name must not resolve to a value")
	}
}

// TestStateStoreAcceptsTheEnumName pins the app-state write path: the
// Web UI sends the selection as the enum's name, and the store keeps
// the number and hands the name back.
func TestStateStoreAcceptsTheEnumName(t *testing.T) {
	store := newStateStore()
	store.write(json.RawMessage(`{"appState":{"lastSelectedAgentModel":"MODEL_PLACEHOLDER_M1"}}`))

	store.mu.Lock()
	stored := store.appState["lastSelectedAgentModel"]
	store.mu.Unlock()
	if stored != 1001 {
		t.Fatalf("stored lastSelectedAgentModel = %v (%T), want 1001", stored, stored)
	}

	snapshot := store.snapshot()
	appState, _ := snapshot["appState"].(map[string]any)
	if got := appState["lastSelectedAgentModel"]; got != "MODEL_PLACEHOLDER_M1" {
		t.Errorf("read back lastSelectedAgentModel = %v, want the enum name", got)
	}
}

// TestStartCascadeResolvesTheEnumName pins the cascade path: the
// picker sends the name, the dispatcher turns it back into the
// catalogue id.
func TestStartCascadeResolvesTheEnumName(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.ListBackend.Models = func(string, string) ([]ModelInfo, error) {
		return enumCatalogue(), nil
	}
	if id := d.modelIDForRequestedModel(1001); id != "mini-m3.1-flash" {
		t.Errorf("modelIDForRequestedModel(1001) = %q, want mini-m3.1-flash", id)
	}
	var params lsStartCascadeParams
	if err := json.Unmarshal([]byte(`{"requestedModel":"MODEL_PLACEHOLDER_M1"}`), &params); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if int(params.RequestedModel) != 1001 {
		t.Errorf("requestedModel = %d, want 1001", int(params.RequestedModel))
	}
	if id := d.modelIDForRequestedModel(int(params.RequestedModel)); id != "mini-m3.1-flash" {
		t.Errorf("resolved id = %q, want mini-m3.1-flash", id)
	}
}

// TestJetboxReadRPCsAnswerEmpty pins the two read RPCs: the
// reference's JetboxReadAppStateResponse and
// JetboxReadUserConfigResponse carry no fields.
func TestJetboxReadRPCsAnswerEmpty(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	for _, method := range []string{"JetboxReadAppState", "JetboxReadUserConfig"} {
		txt, err := d.DispatchDirect(context.Background(), method, "{}")
		if err != nil {
			t.Fatalf("DispatchDirect %s: %v", method, err)
		}
		if txt != "{}" {
			t.Errorf("%s answered %q, want {}", method, txt)
		}
	}
}
