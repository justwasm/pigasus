// Package remotecontrol — model_config_alignment_test.go: the model
// config, and the Web UI's model picker that reads it.
//
// The picker maps every model config through one function — the `Rk`
// mapper in internal/rcfrontend/assets/main.js:
//
//	Rk = a => a.modelOrAlias ? {
//	  label: a.label,
//	  value: a.modelOrAlias.choice.case === "model" ? a.modelOrAlias.choice.value : void 0,
//	  modelAlias: a.modelOrAlias.choice.case === "alias" ? a.modelOrAlias.choice.value : void 0,
//	  disabled: a.disabled,
//	  supportedMimeTypes: new Map(Object.entries(a.supportedMimeTypes)),
//	  quotaInfo: a.quotaInfo,
//	  tagTitle: a.tagTitle,
//	  tagDescription: a.tagDescription,
//	  supportsThoughtCirculation: a.supportsThoughtCirculation,
//	  modelUrl: a.modelUrl,
//	  modelId: a.modelId || void 0,
//	} : null
//
// and the picker filters the disabled entries out of the list
// (`filter(b => !b.disabled)`).
//
// So every field the mapper names has to have a source in the clone's
// model configuration, or the picker sees `undefined` where the
// reference's own catalogue says something. The clone used to fill
// five of them; these tests pin that it fills all of them, and that it
// fills no others.
//
// The field names are the reference's own — the `ClientModelConfig`
// proto's JSON names (lsproto/descriptor/language_server.protoset,
// field numbers in the comments).
package remotecontrol

import (
	"context"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/models"
)

// pickerFields are the fields the Web UI's model picker reads off a
// model config: the ten the `Rk` mapper names, plus `provider`, which
// the picker reads in a hundred places of its own.
var pickerFields = []string{
	"label",                      // 1
	"modelOrAlias",               // 2
	"provider",                   // 10
	"disabled",                   // 4
	"supportedMimeTypes",         // 18
	"quotaInfo",                  // 15
	"tagTitle",                   // 16
	"tagDescription",             // 17
	"supportsThoughtCirculation", // 19
	"modelUrl",                   // 20
	"modelId",                    // 21
}

// fullyStatedModel is a catalogue row that says everything the picker
// can read.
func fullyStatedModel() ModelInfo {
	yes := true
	fraction := 0.25
	return ModelInfo{
		ID:                         "minimax-m3",
		Label:                      "MiniMax M3",
		SupportedMimeTypes:         []string{"image/png", "application/pdf"},
		Disabled:                   true,
		QuotaInfo:                  &models.QuotaInfo{RemainingFraction: &fraction, ResetTime: "2026-10-25T22:33:00.530076Z"},
		SupportsThoughtCirculation: &yes,
		ModelURL:                   "https://example.com/models/minimax-m3",
		ModelID:                    "MiniMax-M3",
		TagTitle:                   "NEW",
		TagDescription:             "The newest model",
	}
}

// TestModelConfigCarriesEveryFieldThePickerReads pins the alignment:
// a model that states every attribute carries every field the picker's
// mapper reads.
//
// The clone used to fill five of the eleven; the rest were `undefined`
// in the picker, which is a different thing from "false" and from
// "empty".
func TestModelConfigCarriesEveryFieldThePickerReads(t *testing.T) {
	configs, _ := userStatusClientModelConfigs([]ModelInfo{fullyStatedModel()})
	if len(configs) != 1 {
		t.Fatalf("configs = %d, want 1", len(configs))
	}
	cfg := configs[0]
	for _, field := range pickerFields {
		if _, ok := cfg[field]; !ok {
			t.Errorf("the model config carries no %q, and the picker reads it", field)
		}
	}
}

// TestModelConfigEmitsNoFieldThePickerCannotUse pins the other
// direction: the clone emits nothing the picker has no use for. A field
// the picker ignores is a field nobody reads.
func TestModelConfigEmitsNoFieldThePickerCannotUse(t *testing.T) {
	known := map[string]bool{}
	for _, field := range pickerFields {
		known[field] = true
	}
	catalogue := []ModelInfo{
		fullyStatedModel(),
		{ID: "gemini-3.8-flash-high", Label: "Gemini 3.8 Flash (High)"},
		{ID: "gpt-oss-120b-medium", Label: "GPT-OSS 120B (Medium)"},
	}
	configs, _ := userStatusClientModelConfigs(catalogue)
	for i, cfg := range configs {
		for key := range cfg {
			if !known[key] {
				t.Errorf("config %d carries %q, which the picker never reads", i, key)
			}
		}
	}
}

// TestModelConfigOmitsTheDefaults pins proto-JSON's rule: a field
// holding its default is not emitted at all.
//
// The reference's own catalogue is thinner for it — the model it
// enables carries no `disabled`, and the models that take no picture
// carry no `supportedMimeTypes` (an empty map is a map's default).
func TestModelConfigOmitsTheDefaults(t *testing.T) {
	configs, _ := userStatusClientModelConfigs([]ModelInfo{
		{ID: "gpt-oss-120b-medium", Label: "GPT-OSS 120B (Medium)"},
	})
	if len(configs) != 1 {
		t.Fatalf("configs = %d, want 1", len(configs))
	}
	cfg := configs[0]
	for _, field := range []string{
		"disabled", "supportedMimeTypes", "quotaInfo", "tagTitle",
		"tagDescription", "supportsThoughtCirculation", "modelUrl", "modelId",
	} {
		if _, ok := cfg[field]; ok {
			t.Errorf("the model config carries %q, which holds its default", field)
		}
	}
	// The two fields that are never a default are always there.
	for _, field := range []string{"label", "modelOrAlias", "provider"} {
		if _, ok := cfg[field]; !ok {
			t.Errorf("the model config carries no %q", field)
		}
	}
}

// TestModelConfigSpellsTheQuotaTheWayThePickerReadsIt pins the
// quota's own field names.
//
// 【一手】 the picker reads `f.remainingFraction` and `f.resetTime`
// (the frontend's `lN` and `mN`): a model whose remaining fraction is
// zero and whose reset time is in the future is the one the picker
// warns about.
func TestModelConfigSpellsTheQuotaTheWayThePickerReadsIt(t *testing.T) {
	configs, _ := userStatusClientModelConfigs([]ModelInfo{fullyStatedModel()})
	quota, ok := configs[0]["quotaInfo"].(map[string]any)
	if !ok {
		t.Fatalf("quotaInfo = %v, want an object", configs[0]["quotaInfo"])
	}
	if got := quota["remainingFraction"]; got != 0.25 {
		t.Errorf("quotaInfo.remainingFraction = %v, want 0.25", got)
	}
	// The timestamp is protojson's spelling, not the configuration's:
	// the reference's own catalogue answers "2026-10-25T22:33:00.530076Z".
	if got := quota["resetTime"]; got != "2026-10-25T22:33:00.530076Z" {
		t.Errorf("quotaInfo.resetTime = %v, want the reference's own spelling", got)
	}
}

// TestModelConfigCarriesTheTagLine pins that the configuration's own
// words win over the catalogue's guess.
//
// 【一手】 the picker shows the tag line as a badge under the model's
// name, and a tag title of "NEW" is what the picker looks for when it
// marks a model as new (`l.tagTitle?.trim().toUpperCase() === "NEW"`).
func TestModelConfigCarriesTheTagLine(t *testing.T) {
	configs, _ := userStatusClientModelConfigs([]ModelInfo{
		{ID: "minimax-m3.1-flash", Label: "MiniMax M3.1 Flash"},
		{ID: "minimax-m3.1-flash", Label: "MiniMax M3.1 Flash", TagTitle: "NEW", TagDescription: "Fresh"},
	})
	if got := configs[0]["tagTitle"]; got != "Fast" {
		t.Errorf("the catalogue's guess = %v, want Fast", got)
	}
	if got := configs[1]["tagTitle"]; got != "NEW" {
		t.Errorf("the configuration's own tag line = %v, want NEW", got)
	}
	if got := configs[1]["tagDescription"]; got != "Fresh" {
		t.Errorf("the configuration's own tag description = %v, want Fresh", got)
	}
}

// TestModelConfigCarriesTheMimeTypesAsAMap pins the shape the picker
// reads: a map of a media type to whether it is allowed.
//
// 【一手】 the picker's check is `b.get(c) === true` against that map,
// and the frontend builds it with `new Map(Object.entries(...))` — so
// the value has to be the boolean true, not a string and not a number.
func TestModelConfigCarriesTheMimeTypesAsAMap(t *testing.T) {
	configs, _ := userStatusClientModelConfigs([]ModelInfo{fullyStatedModel()})
	mimes, ok := configs[0]["supportedMimeTypes"].(map[string]any)
	if !ok {
		t.Fatalf("supportedMimeTypes = %v, want a map", configs[0]["supportedMimeTypes"])
	}
	for _, mime := range []string{"image/png", "application/pdf"} {
		if mimes[mime] != true {
			t.Errorf("supportedMimeTypes[%q] = %v, want true", mime, mimes[mime])
		}
	}
}

// TestModelConfigAnswersInTheSchemaOrder pins the byte order: the
// reference's answers are protojson, and protojson writes a message's
// fields in the order the descriptor declares them.
//
// The order is the `ClientModelConfig` proto's own: label (1),
// modelOrAlias (2), disabled (4), provider (10), quotaInfo (15),
// tagTitle (16), tagDescription (17), supportedMimeTypes (18),
// supportsThoughtCirculation (19), modelUrl (20), modelId (21).
//
// The answer goes through the marshaller (lsproto/order.go):
// a Go map's keys are written in alphabetical order, and the
// marshaller puts them back into the schema's order.
func TestModelConfigAnswersInTheSchemaOrder(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.ListBackend.Models = func(string, string) ([]ModelInfo, error) {
		return []ModelInfo{fullyStatedModel()}, nil
	}
	raw, err := d.DispatchDirect(context.Background(), "GetUserStatus", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetUserStatus: %v", err)
	}
	want := []string{
		"label", "modelOrAlias", "disabled", "provider", "quotaInfo",
		"tagTitle", "tagDescription", "supportedMimeTypes",
		"supportsThoughtCirculation", "modelUrl", "modelId",
	}
	previous := -1
	for _, field := range want {
		at := strings.Index(raw, `"`+field+`":`)
		if at < 0 {
			t.Errorf("the answer carries no %q:\n%s", field, raw)
			continue
		}
		if at < previous {
			t.Errorf("the field %q is out of the schema's order:\n%s", field, raw)
		}
		previous = at
	}
}
