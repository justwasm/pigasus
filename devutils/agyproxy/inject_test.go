package agyproxy

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/models"
)

func miniMax() models.Model {
	return models.Model{
		Name:              "minimax-m3",
		DisplayName:       "MiniMax M3",
		Provider:          "agw",
		APIKey:            "-",
		APIURL:            "https://agw.k0s.io/v1/chat/completions",
		ExternalModelName: "MiniMax-M3",
		ContextWindow:     1000000,
	}
}

func TestSlug(t *testing.T) {
	cases := []struct {
		in   models.Model
		want string
	}{
		{miniMax(), "custom-minimax-m3"},
		{models.Model{Name: "x", ExternalModelName: "models/gpt-4o mini"}, "custom-gpt-4o-mini"},
		{models.Model{Name: "Qwen 3 8B"}, "custom-qwen-3-8b"},
		{models.Model{Name: "a", ExternalModelName: "--Claude//Sonnet--"}, "custom-claude-sonnet"},
	}
	for _, tc := range cases {
		if got := Slug(tc.in); got != tc.want {
			t.Errorf("Slug(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPlaceholderShape(t *testing.T) {
	re := regexp.MustCompile(`^MODEL_PLACEHOLDER_M(\d+)$`)
	m := miniMax()
	ph := Placeholder(m)
	mt := re.FindStringSubmatch(ph)
	if mt == nil {
		t.Fatalf("Placeholder = %q, want MODEL_PLACEHOLDER_M<N>", ph)
	}
	n, err := strconv.Atoi(mt[1])
	if err != nil {
		t.Fatal(err)
	}
	if n < 400 || n > 599 {
		t.Fatalf("Placeholder number %d outside 400..599", n)
	}
	if Placeholder(m) != ph {
		t.Fatal("Placeholder is not deterministic")
	}
	// Falls back to Name when there is no display name.
	fallback := Placeholder(models.Model{Name: "MiniMax M3"})
	if fallback != ph {
		t.Fatalf("displayName fallback changed the hash: %q vs %q", fallback, ph)
	}
}

func TestMatchModel(t *testing.T) {
	m := miniMax()
	ph := Placeholder(m)
	for _, want := range []string{
		ph, "models/" + ph, strings.ToLower(ph),
		"custom-minimax-m3", "minimax-m3", "MiniMax-M3", "MiniMax M3",
	} {
		if !MatchModel(want, m) {
			t.Errorf("MatchModel(%q) = false, want true", want)
		}
	}
	for _, want := range []string{"", "gemini-2.5-pro", "MODEL_PLACEHOLDER_M100", "claude-sonnet-4"} {
		if MatchModel(want, m) {
			t.Errorf("MatchModel(%q) = true, want false", want)
		}
	}
}

const upstreamModels = `{
  "models": [
    {"name": "models/gemini-2.5-pro", "displayName": "Gemini 2.5 Pro"},
    {"name": "models/gemini-2.5-flash", "displayName": "Gemini 2.5 Flash"}
  ],
  "agentModelSorts": [
    {"displayName": "Agent", "groups": [
      {"modelIds": ["gemini-2.5-pro"]},
      {"modelIds": ["gemini-2.5-flash"]}
    ]},
    {"displayName": "Other", "groups": [{"modelIds": []}]}
  ],
  "defaultAgentModelId": "gemini-2.5-pro"
}`

func TestMergeAvailableModels(t *testing.T) {
	out, err := MergeAvailableModels([]byte(upstreamModels), []models.Model{miniMax()})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}

	// Upstream models survive, custom is first.
	ms := doc["models"].([]any)
	if len(ms) != 3 {
		t.Fatalf("models count = %d, want 3", len(ms))
	}
	first := ms[0].(map[string]any)
	if first["name"] != "models/"+Placeholder(miniMax()) {
		t.Fatalf("first model = %v", first["name"])
	}
	if first["displayName"] != "MiniMax M3" {
		t.Fatalf("displayName = %v", first["displayName"])
	}
	if first["inputTokenLimit"].(float64) != 1000000 {
		t.Fatalf("inputTokenLimit = %v", first["inputTokenLimit"])
	}
	if first["outputTokenLimit"].(float64) != 8192 {
		t.Fatalf("outputTokenLimit = %v", first["outputTokenLimit"])
	}
	methods := first["supportedGenerationMethods"].([]any)
	if len(methods) != 2 || methods[0] != "generateContent" || methods[1] != "countTokens" {
		t.Fatalf("supportedGenerationMethods = %v", methods)
	}
	names := []string{}
	for _, e := range ms {
		names = append(names, e.(map[string]any)["name"].(string))
	}
	for _, want := range []string{"models/gemini-2.5-pro", "models/gemini-2.5-flash"} {
		if !containsString(anySlice(names), want) {
			t.Fatalf("upstream model %s dropped (have %v)", want, names)
		}
	}

	// Every group of every sort learns the slug, including the empty one.
	sorts := doc["agentModelSorts"].([]any)
	if len(sorts) != 2 {
		t.Fatalf("sorts count = %d", len(sorts))
	}
	slug := Slug(miniMax())
	for i, s := range sorts {
		groups := s.(map[string]any)["groups"].([]any)
		for j, g := range groups {
			ids := g.(map[string]any)["modelIds"].([]any)
			if !containsString(ids, slug) {
				t.Fatalf("sorts[%d].groups[%d].modelIds = %v, missing %s", i, j, ids, slug)
			}
		}
	}

	// Idempotent: merging our own output is a no-op, byte for byte.
	again, err := MergeAvailableModels(out, []models.Model{miniMax()})
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(out) {
		t.Fatalf("merge not idempotent:\n%s\n%s", out, again)
	}
}

func TestMergeAvailableModelsMapShapeAndMissingSorts(t *testing.T) {
	in := `{"models": {"gemini-2.5-pro": {"displayName": "Gemini 2.5 Pro"}}}`
	out, err := MergeAvailableModels([]byte(in), []models.Model{miniMax()})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	m, ok := doc["models"].(map[string]any)
	if !ok {
		t.Fatalf("models is not a map: %T", doc["models"])
	}
	if _, ok := m["gemini-2.5-pro"]; !ok {
		t.Fatal("upstream map entry dropped")
	}
	entry, ok := m[Slug(miniMax())].(map[string]any)
	if !ok {
		t.Fatalf("custom slug missing from map: %v", m)
	}
	// The map form must advertise the placeholder id: agy resolves --model to
	// it, and without it the turn dies with "plan model not specified".
	if entry["model"] != Placeholder(miniMax()) {
		t.Fatalf("map entry model = %v, want %s", entry["model"], Placeholder(miniMax()))
	}
	if entry["apiProvider"] == "" || entry["maxTokens"].(float64) != 1000000 {
		t.Fatalf("map entry capabilities = %v", entry)
	}
	if _, ok := doc["agentModelSorts"]; !ok {
		t.Fatal("agentModelSorts should have been created")
	}
}

func TestMergeAvailableModelsNoUpstream(t *testing.T) {
	out, err := MergeAvailableModels(nil, []models.Model{miniMax()})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	// With no upstream body the merged document uses the map form agy reads.
	m, ok := doc["models"].(map[string]any)
	if !ok || len(m) != 1 {
		t.Fatalf("models = %#v", doc["models"])
	}
	if _, ok := m[Slug(miniMax())]; !ok {
		t.Fatalf("custom slug missing: %v", m)
	}
}

func TestMergeAvailableModelsBadJSON(t *testing.T) {
	if _, err := MergeAvailableModels([]byte("not json"), nil); err == nil {
		t.Fatal("want an error for a non-JSON upstream body")
	}
}

func anySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}
