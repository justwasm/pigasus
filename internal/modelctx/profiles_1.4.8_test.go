// profiles_1.4.8_test.go — the cascade catalogue against the binary's own
// document.
//
// 【一手】 agy 1.4.8 embeds model_profiles.json (file offset 0x9ab160a,
// 57 984 bytes). The document is committed verbatim at
// testdata/model_profiles_1.4.8.json, and these tests re-derive every
// number the clone uses from it — so a table edit that drifts from the
// binary fails here, and a *binary* that drifts from the document fails
// the same way.
//
// The 1.4.7 document stays on disk as the previous release's evidence;
// TestProfilesDeltaFromThePreviousRelease pins what moved between the
// two documents.

package modelctx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// document is the embedded model_profiles.json, as far as the catalogue
// needs it.
type document struct {
	Base struct {
		CheckpointVariant string `json:"checkpoint-variant"`
	} `json:"base"`
	// ModelProfiles maps a profile name to its experiment payloads. A
	// payload's value is a JSON *string* holding the configuration; the
	// document also carries a boolean flag beside them
	// (retry_model_capacity_exhausted), so the values stay raw.
	ModelProfiles map[string]map[string]json.RawMessage `json:"model_profiles"`
	Models        map[string]string                     `json:"models"`
}

// loadDocument reads the committed copy of the binary's embedded
// document.
func loadDocument(t *testing.T) document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "model_profiles_1.4.8.json"))
	if err != nil {
		t.Fatalf("read the embedded document: %v", err)
	}
	var d document
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("parse the embedded document: %v", err)
	}
	return d
}

// number is a JSON number the document spells as a string. The payload
// quotes most of its numbers ("max_token_limit": "256000") and leaves
// others bare ("max_user_requests": 10), so the type accepts both.
type number int

// UnmarshalJSON reads a quoted or bare integer.
func (n *number) UnmarshalJSON(b []byte) error {
	text := strings.Trim(string(b), `"`)
	v, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("number %q: %w", text, err)
	}
	*n = number(v)
	return nil
}

// ratio is a JSON number the document spells as a string.
type ratio float64

// UnmarshalJSON reads a quoted or bare float.
func (r *ratio) UnmarshalJSON(b []byte) error {
	text := strings.Trim(string(b), `"`)
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("ratio %q: %w", text, err)
	}
	*r = ratio(v)
	return nil
}

// payload is one CASCADE_USE_EXPERIMENT_CHECKPOINTER payload, in the
// document's own spelling.
type payload struct {
	Strategy                 string `json:"strategy"`
	MaxTokenLimit            number `json:"max_token_limit"`
	MaxOutputTokens          number `json:"max_output_tokens"`
	TokenThreshold           number `json:"token_threshold"`
	MaxOverheadRatio         ratio  `json:"max_overhead_ratio"`
	Enabled                  bool   `json:"enabled"`
	CheckpointModel          string `json:"checkpoint_model"`
	UseLastPlannerModel      bool   `json:"use_last_planner_model"`
	IsSync                   bool   `json:"is_sync"`
	MaxUserRequests          number `json:"max_user_requests"`
	IncludeLastUserMessage   bool   `json:"include_last_user_message"`
	IncludeConversationLog   bool   `json:"include_conversation_log"`
	IncludeRunningTaskSnaps  bool   `json:"include_running_task_snapshots"`
	IncludeSubagentSnapshots bool   `json:"include_subagent_snapshots"`
	IncludeArtifactSnapshots bool   `json:"include_artifact_snapshots"`
	RetryMaxRetries          number `json:"-"`
	UseModelReportedTokenCnt bool   `json:"use_model_reported_token_count"`
	SessionSummaryPromptSet  bool   `json:"-"`
}

// asProfile turns the document's payload into the clone's Profile.
func (p payload) asProfile() Profile {
	return Profile{
		Strategy:                   p.Strategy,
		MaxTokenLimit:              int(p.MaxTokenLimit),
		MaxOutputTokens:            int(p.MaxOutputTokens),
		TokenThreshold:             int(p.TokenThreshold),
		MaxOverheadRatio:           float64(p.MaxOverheadRatio),
		Enabled:                    p.Enabled,
		CheckpointModel:            p.CheckpointModel,
		UseLastPlannerModel:        p.UseLastPlannerModel,
		IsSync:                     p.IsSync,
		MaxUserRequests:            int(p.MaxUserRequests),
		IncludeLastUserMessage:     p.IncludeLastUserMessage,
		IncludeConversationLog:     p.IncludeConversationLog,
		IncludeRunningTaskSnaps:    p.IncludeRunningTaskSnaps,
		IncludeSubagentSnapshots:   p.IncludeSubagentSnapshots,
		IncludeArtifactSnapshots:   p.IncludeArtifactSnapshots,
		RetryMaxRetries:            int(p.RetryMaxRetries),
		UseModelReportedTokenCount: p.UseModelReportedTokenCnt,
		SessionSummaryPrompt:       p.SessionSummaryPromptSet,
	}
}

// parsePayload reads one payload out of the document.
func parsePayload(t *testing.T, raw string) payload {
	t.Helper()
	var p payload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("parse a checkpoint payload: %v", err)
	}
	// The retry policy is a nested object; read it on its own.
	var withRetry struct {
		Retry struct {
			MaxRetries number `json:"max_retries"`
		} `json:"retry_config"`
	}
	if err := json.Unmarshal([]byte(raw), &withRetry); err != nil {
		t.Fatalf("parse the retry policy: %v", err)
	}
	p.RetryMaxRetries = withRetry.Retry.MaxRetries
	var withPrompt struct {
		Override string `json:"session_summary_prompt_override"`
	}
	if err := json.Unmarshal([]byte(raw), &withPrompt); err != nil {
		t.Fatalf("parse the session-summary override: %v", err)
	}
	p.SessionSummaryPromptSet = withPrompt.Override != ""
	return p
}

// compareProfiles reports every field the clone's table got wrong.
func compareProfiles(t *testing.T, what string, want, got Profile) {
	t.Helper()
	if want.Strategy != got.Strategy {
		t.Errorf("%s: strategy = %q, want %q", what, got.Strategy, want.Strategy)
	}
	if want.MaxTokenLimit != got.MaxTokenLimit {
		t.Errorf("%s: max_token_limit = %d, want %d", what, got.MaxTokenLimit, want.MaxTokenLimit)
	}
	if want.MaxOutputTokens != got.MaxOutputTokens {
		t.Errorf("%s: max_output_tokens = %d, want %d", what, got.MaxOutputTokens, want.MaxOutputTokens)
	}
	if want.TokenThreshold != got.TokenThreshold {
		t.Errorf("%s: token_threshold = %d, want %d", what, got.TokenThreshold, want.TokenThreshold)
	}
	if want.MaxOverheadRatio != got.MaxOverheadRatio {
		t.Errorf("%s: max_overhead_ratio = %v, want %v", what, got.MaxOverheadRatio, want.MaxOverheadRatio)
	}
	if want.Enabled != got.Enabled {
		t.Errorf("%s: enabled = %v, want %v", what, got.Enabled, want.Enabled)
	}
	if want.CheckpointModel != got.CheckpointModel {
		t.Errorf("%s: checkpoint_model = %q, want %q", what, got.CheckpointModel, want.CheckpointModel)
	}
	if want.UseLastPlannerModel != got.UseLastPlannerModel {
		t.Errorf("%s: use_last_planner_model = %v, want %v", what, got.UseLastPlannerModel, want.UseLastPlannerModel)
	}
	if want.IsSync != got.IsSync {
		t.Errorf("%s: is_sync = %v, want %v", what, got.IsSync, want.IsSync)
	}
	if want.MaxUserRequests != got.MaxUserRequests {
		t.Errorf("%s: max_user_requests = %d, want %d", what, got.MaxUserRequests, want.MaxUserRequests)
	}
	if want.IncludeLastUserMessage != got.IncludeLastUserMessage {
		t.Errorf("%s: include_last_user_message = %v, want %v", what, got.IncludeLastUserMessage, want.IncludeLastUserMessage)
	}
	if want.IncludeConversationLog != got.IncludeConversationLog {
		t.Errorf("%s: include_conversation_log = %v, want %v", what, got.IncludeConversationLog, want.IncludeConversationLog)
	}
	if want.IncludeRunningTaskSnaps != got.IncludeRunningTaskSnaps {
		t.Errorf("%s: include_running_task_snapshots = %v, want %v", what, got.IncludeRunningTaskSnaps, want.IncludeRunningTaskSnaps)
	}
	if want.IncludeSubagentSnapshots != got.IncludeSubagentSnapshots {
		t.Errorf("%s: include_subagent_snapshots = %v, want %v", what, got.IncludeSubagentSnapshots, want.IncludeSubagentSnapshots)
	}
	if want.IncludeArtifactSnapshots != got.IncludeArtifactSnapshots {
		t.Errorf("%s: include_artifact_snapshots = %v, want %v", what, got.IncludeArtifactSnapshots, want.IncludeArtifactSnapshots)
	}
	if want.RetryMaxRetries != got.RetryMaxRetries {
		t.Errorf("%s: retry.max_retries = %d, want %d", what, got.RetryMaxRetries, want.RetryMaxRetries)
	}
	if want.UseModelReportedTokenCount != got.UseModelReportedTokenCount {
		t.Errorf("%s: use_model_reported_token_count = %v, want %v", what, got.UseModelReportedTokenCount, want.UseModelReportedTokenCount)
	}
	if want.SessionSummaryPrompt != got.SessionSummaryPrompt {
		t.Errorf("%s: session_summary_prompt_override = %v, want %v", what, got.SessionSummaryPrompt, want.SessionSummaryPrompt)
	}
}

// payloadString returns the JSON string a payload's raw value holds. The
// document's payloads are strings (the configuration itself is embedded
// as text); the boolean flags beside them are not.
func payloadString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// TestProfilesMatchTheEmbeddedDocument pins the clone's profile table
// against the document the binary ships.
func TestProfilesMatchTheEmbeddedDocument(t *testing.T) {
	d := loadDocument(t)
	for name, entries := range d.ModelProfiles {
		for key, raw := range entries {
			// Only the checkpoint experiment's payloads are profiles;
			// the document's other keys (task-details-suffix, the
			// planning-mode template, …) are unrelated settings.
			if !strings.HasPrefix(key, "CASCADE_USE_EXPERIMENT_CHECKPOINTER") {
				continue
			}
			text, ok := payloadString(raw)
			if !ok {
				// A flag, not a payload.
				continue
			}
			want := parsePayload(t, text).asProfile()
			if key == "CASCADE_USE_EXPERIMENT_CHECKPOINTER" {
				got, ok := profiles[name]
				if !ok {
					t.Errorf("the clone has no profile %q", name)
					continue
				}
				compareProfiles(t, name, want, got)
				continue
			}
			// A variant payload: "<experiment>/<variant>".
			variant := key[len("CASCADE_USE_EXPERIMENT_CHECKPOINTER/"):]
			got, ok := profileVariants[name+"/"+variant]
			if !ok {
				t.Errorf("the clone has no variant payload %q for %q", variant, name)
				continue
			}
			compareProfiles(t, name+"/"+variant, want, got)
		}
	}
	if len(profiles) != len(d.ModelProfiles) {
		t.Errorf("the clone has %d profiles, the document has %d", len(profiles), len(d.ModelProfiles))
	}
}

// TestProfileAliasesMatchTheEmbeddedDocument pins the alias map: the
// model's id → the profile it folds onto.
func TestProfileAliasesMatchTheEmbeddedDocument(t *testing.T) {
	d := loadDocument(t)
	for enum, profile := range d.Models {
		got, ok := profileAliases[enum]
		if !ok {
			t.Errorf("the clone has no alias for %q", enum)
			continue
		}
		if got != profile {
			t.Errorf("alias for %q = %q, want %q", enum, got, profile)
		}
	}
	if len(profileAliases) != len(d.Models) {
		t.Errorf("the clone has %d aliases, the document has %d", len(profileAliases), len(d.Models))
	}
}

// TestCheckpointVariantMatchesTheBaseBlock pins the variant the clone
// applies: the base block's "checkpoint-variant" setting.
func TestCheckpointVariantMatchesTheBaseBlock(t *testing.T) {
	d := loadDocument(t)
	if d.Base.CheckpointVariant != CheckpointVariant {
		t.Errorf("the base block's checkpoint-variant = %q, want %q", d.Base.CheckpointVariant, CheckpointVariant)
	}
}

// TestProfileForResolvesTheCatalogue pins that every catalogue model the
// 1.4.8 document knows resolves to its profile, and that the window the
// clone derives is the smaller of the model's own limit and the
// profile's.
func TestProfileForResolvesTheCatalogue(t *testing.T) {
	resolved := 0
	for slug, m := range catalogue {
		p, ok := ProfileFor(slug)
		if !ok {
			continue
		}
		resolved++
		want := p.MaxTokenLimit
		if m.inputLimit > 0 && m.inputLimit < want {
			want = m.inputLimit
		}
		got := For(slug)
		if got.ContextWindow != want {
			t.Errorf("For(%q).ContextWindow = %d, want %d", slug, got.ContextWindow, want)
		}
		if got.ReserveTokens != p.MaxOutputTokens {
			t.Errorf("For(%q).ReserveTokens = %d, want %d", slug, got.ReserveTokens, p.MaxOutputTokens)
		}
	}
	if resolved == 0 {
		t.Fatal("no catalogue model resolved to a 1.4.8 profile")
	}
}

// TestThe840kVariant pins the variant payload that the 1.4.8 document
// ships for MODEL_PLACEHOLDER_M400: an 840 000-token window and the
// model-reported token count.
//
// The clone has no catalogue id that names that profile — the mapping from
// the profile's model enums to the CLI's model ids is not resolved — so
// the variant is reached through the profile table, not through For().
func TestThe840kVariant(t *testing.T) {
	p, ok := profileVariants["MODEL_PLACEHOLDER_M400/"+CheckpointVariant]
	if !ok {
		t.Fatalf("no variant payload for MODEL_PLACEHOLDER_M400/%s", CheckpointVariant)
	}
	if p.MaxTokenLimit != 840_000 {
		t.Errorf("the variant's max_token_limit = %d, want 840000", p.MaxTokenLimit)
	}
	if !p.UseModelReportedTokenCount {
		t.Error("the variant should set use_model_reported_token_count")
	}
	if p.Strategy != StrategySameModel {
		t.Errorf("the variant's strategy = %q, want %q", p.Strategy, StrategySameModel)
	}
}

// TestValidateReportsTheReferenceMessages pins the validator's four
// messages, byte for byte.
//
// 【反编译】 cortex/utils.ValidateCheckpointConfigForModelInfo.
func TestValidateReportsTheReferenceMessages(t *testing.T) {
	planner := &ModelInfo{ContextWindowLimit: 1_048_576, MaxOutputTokens: 65_536}
	checkpoint := &ModelInfo{ContextWindowLimit: 128_000, MaxOutputTokens: 16_384}

	if err := Validate(Config{}, nil, checkpoint); err == nil ||
		err.Error() != "plannerModelInfo cannot be nil" {
		t.Errorf("nil planner: got %v, want %q", err, "plannerModelInfo cannot be nil")
	}

	over := Config{MaxTokenLimit: 1_048_576, MaxOutputTokens: 16_384, MaxOverheadRatio: 0.15}
	want := fmt.Sprintf("config.MaxTokenLimit (%d) cannot exceed the planner model's context window limit (%d) minus the planner model's MaxOutputTokens (%d)",
		1_048_576, 1_048_576, 65_536)
	if err := Validate(over, planner, checkpoint); err == nil || err.Error() != want {
		t.Errorf("an oversized limit: got %v, want %q", err, want)
	}

	big := Config{MaxTokenLimit: 128_000, MaxOutputTokens: 32_768, MaxOverheadRatio: 0.15}
	want = fmt.Sprintf("config.MaxOutputTokens %d is greater than checkpointModelInfo.MaxOutputTokens %d",
		32_768, 16_384)
	if err := Validate(big, planner, checkpoint); err == nil || err.Error() != want {
		t.Errorf("an oversized output cap: got %v, want %q", err, want)
	}

	for _, ratio := range []float32{0, 0.5, -0.1, 1} {
		cfg := Config{MaxTokenLimit: 128_000, MaxOutputTokens: 16_384, MaxOverheadRatio: ratio}
		want = fmt.Sprintf("overheadRatio %f is not between 0 and %f", ratio, MaxOverheadRatio)
		if err := Validate(cfg, planner, checkpoint); err == nil || err.Error() != want {
			t.Errorf("an overhead ratio of %v: got %v, want %q", ratio, err, want)
		}
	}

	ok := Config{MaxTokenLimit: 128_000, MaxOutputTokens: 16_384, MaxOverheadRatio: 0.15}
	if err := Validate(ok, planner, checkpoint); err != nil {
		t.Errorf("a valid configuration was rejected: %v", err)
	}
}

// TestClamp pins the reference's clamps.
//
// 【反编译】 cortex/utils.GetCheckpointConfig.
func TestClamp(t *testing.T) {
	got := Clamp(Config{MaxTokenLimit: 840_000, MaxOutputTokens: 32_768})
	if got.MaxTokenLimit != 256_000 {
		t.Errorf("the clamped max_token_limit = %d, want 256000", got.MaxTokenLimit)
	}
	if got.MaxOutputTokens != 16_384 {
		t.Errorf("the clamped max_output_tokens = %d, want 16384", got.MaxOutputTokens)
	}
	got = Clamp(Config{MaxTokenLimit: 128_000})
	if got.MaxOutputTokens != 16_384 {
		t.Errorf("an unset output cap = %d, want 16384", got.MaxOutputTokens)
	}
	got = ClampForModel(Config{MaxTokenLimit: 128_000}, MillionTokenModelID)
	if got.MaxTokenLimit != 1_000_000 {
		t.Errorf("model %d's limit = %d, want 1000000", MillionTokenModelID, got.MaxTokenLimit)
	}
	if !got.Enabled {
		t.Error("model 323's checkpoint should be enabled")
	}
}

// TestOverflowFormatMatchesTheReference pins the overflow message against
// the bytes the binary carries.
//
// 【strings】 agy 1.4.8, file offset 0x36c73e2 (vmaddr 0x1036c73e2),
// 438 bytes. The bytes are the same ones the 1.4.7 binary carried (at
// its own vmaddr 0x1036904eb).
func TestOverflowFormatMatchesTheReference(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "overflow_1.4.7.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if got := string(raw); got != OverflowFormat {
		t.Errorf("the overflow message differs from the reference:\n got %d bytes\nwant %d bytes",
			len(got), len(OverflowFormat))
	}
	if len(OverflowFormat) != 438 {
		t.Errorf("len(OverflowFormat) = %d, want 438", len(OverflowFormat))
	}
}

// TestProfilesDeltaFromThePreviousRelease pins what moved between the
// reference's 1.4.7 and 1.4.8 cascade documents.
//
// 【一手】 both documents are committed verbatim, and the diff below is
// the whole of it: three model ids joined the alias map, the browser
// subagent's model changed, and the marketplace search tool was turned
// on. The per-profile payloads — the numbers the clone's window and
// reserve come from — did not move at all.
//
// The test is deliberately exact: a second release's delta has to be
// written down again, and a delta that is not the one recorded here
// means the reference moved in a way the clone has not looked at.
func TestProfilesDeltaFromThePreviousRelease(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "model_profiles_1.4.7.json"))
	if err != nil {
		t.Fatalf("read the 1.4.7 document: %v", err)
	}
	var previous struct {
		Base          map[string]any                        `json:"base"`
		ModelProfiles map[string]map[string]json.RawMessage `json:"model_profiles"`
		Models        map[string]string                     `json:"models"`
	}
	if err := json.Unmarshal(raw, &previous); err != nil {
		t.Fatalf("parse the 1.4.7 document: %v", err)
	}
	current := loadDocument(t)

	// The alias map grew by three model ids, all of them folding onto
	// the M408 profile.
	for enum, profile := range map[string]string{
		"MODEL_PLACEHOLDER_M429": "MODEL_PLACEHOLDER_M408",
		"MODEL_PLACEHOLDER_M430": "MODEL_PLACEHOLDER_M408",
		"MODEL_PLACEHOLDER_M431": "MODEL_PLACEHOLDER_M408",
	} {
		got, ok := current.Models[enum]
		if !ok {
			t.Errorf("the 1.4.8 document has no alias for %q", enum)
			continue
		}
		if got != profile {
			t.Errorf("alias for %q = %q, want %q", enum, got, profile)
		}
	}
	if len(current.Models) != len(previous.Models)+3 {
		t.Errorf("the 1.4.8 document has %d aliases, the 1.4.7 one had %d (want three more)",
			len(current.Models), len(previous.Models))
	}
	for enum, profile := range previous.Models {
		got, ok := current.Models[enum]
		if !ok {
			t.Errorf("the 1.4.8 document dropped the alias for %q", enum)
			continue
		}
		if got != profile {
			t.Errorf("alias for %q changed from %q to %q", enum, profile, got)
		}
	}

	// The per-profile payloads did not move.
	if len(current.ModelProfiles) != len(previous.ModelProfiles) {
		t.Errorf("the 1.4.8 document has %d profiles, the 1.4.7 one had %d",
			len(current.ModelProfiles), len(previous.ModelProfiles))
	}
	for name, entry := range previous.ModelProfiles {
		got, ok := current.ModelProfiles[name]
		if !ok {
			t.Errorf("the 1.4.8 document dropped the profile %q", name)
			continue
		}
		if len(got) != len(entry) {
			t.Errorf("the profile %q changed between 1.4.7 and 1.4.8", name)
			continue
		}
		for key, value := range entry {
			other, ok := got[key]
			if !ok {
				t.Errorf("the profile %q dropped %q between 1.4.7 and 1.4.8", name, key)
				continue
			}
			if !jsonEqual(other, value) {
				t.Errorf("the profile %q's %q changed between 1.4.7 and 1.4.8", name, key)
			}
		}
	}

	// The base block: the browser subagent's model changed, and the
	// marketplace search tool was turned on.
	var currentBase map[string]any
	raw, err = os.ReadFile(filepath.Join("testdata", "model_profiles_1.4.8.json"))
	if err != nil {
		t.Fatalf("read the 1.4.8 document: %v", err)
	}
	if err := json.Unmarshal(raw, &currentBase); err != nil {
		t.Fatalf("parse the 1.4.8 document: %v", err)
	}
	base, _ := currentBase["base"].(map[string]any)
	if got := base["browser-subagent-model"]; got != "gemini-3.8-flash-low" {
		t.Errorf("base.browser-subagent-model = %v, want %q", got, "gemini-3.8-flash-low")
	}
	if got := base["enable-marketplace-search-tool"]; got != true {
		t.Errorf("base.enable-marketplace-search-tool = %v, want true", got)
	}
	if _, ok := previous.Base["enable-marketplace-search-tool"]; ok {
		t.Error("the 1.4.7 document already carried enable-marketplace-search-tool")
	}
	if got := previous.Base["browser-subagent-model"]; got != "MODEL_PLACEHOLDER_M18" {
		t.Errorf("the 1.4.7 document's browser-subagent-model = %v, want %q", got, "MODEL_PLACEHOLDER_M18")
	}
}

// jsonEqual compares two raw JSON documents by their decoded value, so
// that a re-serialised document still compares equal.
func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &y); err != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
