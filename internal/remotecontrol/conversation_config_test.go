// Tests for the rc-conversation-config cycle. Covers:
//
//   - The single JSON-RPC method
//     (UpsertRemoteAgentConversationConfig), including
//     the validation rules
//     (ValidateConversationConfig: closed-set
//     model_effort, bounded max_turns / temperature /
//     top_p), the sentinel-error mapping
//     (ErrConversationConfigPersistenceFailed →
//     -32603 "Internal error"), and the
//     created:true|false envelope;
//
//   - The ConversationConfigStore.MemConversationConfigStore
//     (Snapshot / Upsert / Get / empty default /
//     upsert) at the in-process level (no Unix
//     socket, no ws adapter);
//
//   - The FSConversationConfigStore happy path
//     against a real t.TempDir() so the on-disk
//     conversation_config.json layout is exercised
//     (atomic temp + rename, 0o600 perms, version
//     stamp, fresh-host empty state, post-Upsert
//     reload, atomic save);
//
//   - The normaliseConversationID / sortConversationIDs
//     helpers — the dispatcher / future-read-cycle
//     seams;
//
//   - The NewMemConversationConfigStoreWith
//     custom-snapshot constructor (deep-copy).
//
// Wire-level tests live in
// internal/remotetransport/tcp_test.go (mirroring
// the project's split: in-process envelopes vs.
// wire round-trips).
package remotecontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newConversationConfigDispatcher returns a
// Dispatcher with the supplied
// ConversationConfigStore pre-installed. The helper
// mirrors newFastpushDispatcher / newResourcesDispatcher.
//
// When ps is nil, NewDispatcher fabricates a fresh
// empty MemConversationConfigStore (the nil-guard
// pattern rc-conversation-config adds). Tests that
// want to assert "no store installed" pass nil and
// exercise the -32603 path; tests that want a
// specific store pass it directly.
func newConversationConfigDispatcher(t *testing.T, ps ConversationConfigStore) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:                 sess,
		ConversationConfigStore: ps,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchConversationConfig sends an
// UpsertRemoteAgentConversationConfig request
// through the dispatcher and returns the
// (result, error) pair. Mirrors dispatchFastpush /
// dispatchResources.
//
// nil params marshals to JSON `null` so the
// dispatcher's "len(params) > 0" guards see a
// present (but null) params block; jsonRPCFor
// takes a byte slice and appends verbatim, so we
// mirror the convention `[]byte("null")` rather
// than a missing-key shape.
func dispatchConversationConfig(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	switch v := params.(type) {
	case nil:
		raw = []byte("null")
	case []byte:
		raw = v
	default:
		var err error
		raw, err = jsonMarshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
	}
	req := jsonRPCFor(1, method, raw)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"].(map[string]any); ok {
		return nil, errObj
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: no result, got %#v", method, resp)
	}
	return res, nil
}

// ---- helper / parse tests --------------------------------

// TestNormaliseConversationID pins the trim
// helper used by both the dispatcher layer and the
// store layer. Mirrors TestNormaliseCascadeID /
// TestNormaliseProviderIDAndResourceID.
func TestNormaliseConversationID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Trim only.
		{"conv_abcdef0123456789", "conv_abcdef0123456789"},
		{"  conv_abcdef0123456789  ", "conv_abcdef0123456789"},
		{"\tconv_xyz\n", "conv_xyz"},
		// Already trimmed (idempotent).
		{"conv_qwerty", "conv_qwerty"},
		// Empty in → empty out.
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := normaliseConversationID(tc.in); got != tc.want {
				t.Errorf("normaliseConversationID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestAllModelEfforts pins the closed-set order
// so a future cycle that extends the set updates
// AllModelEfforts + the doc comment + the
// IsValidModelEffort helper in lockstep.
func TestAllModelEfforts(t *testing.T) {
	want := []string{"low", "medium", "high"}
	if len(AllModelEfforts) != len(want) {
		t.Fatalf("AllModelEfforts len = %d, want %d", len(AllModelEfforts), len(want))
	}
	for i, e := range want {
		if AllModelEfforts[i] != e {
			t.Errorf("AllModelEfforts[%d] = %q, want %q", i, AllModelEfforts[i], e)
		}
	}
}

// TestIsValidModelEffort pins the closed-set
// validator that drives the dispatcher's
// model_effort pre-flight check.
func TestIsValidModelEffort(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"low", true},
		{"medium", true},
		{"high", true},
		{"", false},
		{"  low  ", false}, // whitespace not normalised — closed-set strict
		{"low ", false},    // trim not applied
		{"unknown", false},
		{"LOW", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := IsValidModelEffort(tc.in); got != tc.want {
				t.Errorf("IsValidModelEffort(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestNotifConversationConfigChangedMethod pins
// the JSON-RPC notification method name the
// dispatcher uses for the
// "conversation_config/changed" server-push frame.
func TestNotifConversationConfigChangedMethod(t *testing.T) {
	if NotifConversationConfigChangedMethod != "conversation_config/changed" {
		t.Errorf("NotifConversationConfigChangedMethod = %q, want conversation_config/changed", NotifConversationConfigChangedMethod)
	}
}

// TestConversationConfigStateVersion pins the
// on-disk format version the current store writes.
// v1 is the brief-pinned value; a future cycle
// that introduces a breaking change (renamed keys,
// removed fields) bumps this constant AND updates
// the load() branch that rejects version >
// ConversationConfigStateVersion.
func TestConversationConfigStateVersion(t *testing.T) {
	if ConversationConfigStateVersion != 1 {
		t.Errorf("ConversationConfigStateVersion = %d, want 1", ConversationConfigStateVersion)
	}
}

// TestMaxConversationConfigBounds pins the brief's
// per-field bounds. A future cycle that wants to
// raise the limits updates these constants AND the
// ValidateConversationConfig validator in
// lockstep.
func TestMaxConversationConfigBounds(t *testing.T) {
	if MaxConversationConfigMaxTurns != 1000 {
		t.Errorf("MaxConversationConfigMaxTurns = %d, want 1000", MaxConversationConfigMaxTurns)
	}
	if MaxConversationConfigTemperature != 2.0 {
		t.Errorf("MaxConversationConfigTemperature = %g, want 2.0", MaxConversationConfigTemperature)
	}
	if MaxConversationConfigTopP != 1.0 {
		t.Errorf("MaxConversationConfigTopP = %g, want 1.0", MaxConversationConfigTopP)
	}
}

// ---- ValidateConversationConfig tests --------------------

// TestValidateConversationConfig_Ok pins the
// happy-path validator: a config with all four
// bounded fields at their canonical values passes.
func TestValidateConversationConfig_Ok(t *testing.T) {
	c := ConversationConfig{
		Model:             "claude-3-7-sonnet",
		ModelEffort:       "medium",
		SystemPromptPatch: "be brief",
		AllowedTools:      []string{"Read", "Grep"},
		DisallowedTools:   []string{"Bash"},
		MaxTurns:          50,
		Temperature:       0.5,
		TopP:              0.95,
		Metadata:          map[string]string{"dashboard": "v2"},
	}
	if err := ValidateConversationConfig(c); err != nil {
		t.Errorf("ValidateConversationConfig(ok) = %v, want nil", err)
	}
}

// TestValidateConversationConfig_AllEmptyOk pins
// the "no override" semantic: a fully-empty config
// (zero values on every bounded field, empty
// strings, nil slices / maps) is valid — the brief
// treats 0 / "" / nil as "the dashboard didn't
// override the default".
func TestValidateConversationConfig_AllEmptyOk(t *testing.T) {
	c := ConversationConfig{}
	if err := ValidateConversationConfig(c); err != nil {
		t.Errorf("ValidateConversationConfig(empty) = %v, want nil", err)
	}
}

// TestValidateConversationConfig_BadModelEffort
// pins the unknown model_effort → -32602 path.
// The validator rejects the value before the
// dispatcher's store call.
func TestValidateConversationConfig_BadModelEffort(t *testing.T) {
	cases := []string{"ultra", "LoW", "low ", " medium"}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			err := ValidateConversationConfig(ConversationConfig{ModelEffort: in})
			if err == nil {
				t.Errorf("ValidateConversationConfig(ModelEffort=%q) = nil, want error", in)
				return
			}
			if !strings.Contains(err.Error(), "model_effort") {
				t.Errorf("ValidateConversationConfig(ModelEffort=%q) = %q, want error mentioning model_effort", in, err)
			}
		})
	}
}

// TestValidateConversationConfig_BadMaxTurns
// pins the bounded [0, 1000] max_turns rule.
func TestValidateConversationConfig_BadMaxTurns(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{-1, "max_turns must be >= 0"},
		{-100, "max_turns must be >= 0"},
		{1001, "max_turns must be <= 1000"},
		{9999, "max_turns must be <= 1000"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			err := ValidateConversationConfig(ConversationConfig{MaxTurns: tc.in})
			if err == nil {
				t.Errorf("ValidateConversationConfig(MaxTurns=%d) = nil, want %q", tc.in, tc.want)
				return
			}
			if err.Error() != tc.want {
				t.Errorf("ValidateConversationConfig(MaxTurns=%d) = %q, want %q", tc.in, err, tc.want)
			}
		})
	}
}

// TestValidateConversationConfig_BadTemperature
// pins the bounded [0, 2] temperature rule.
func TestValidateConversationConfig_BadTemperature(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{-0.01, "temperature must be >= 0"},
		{-1.0, "temperature must be >= 0"},
		{2.01, "temperature must be <= 2"},
		{3.5, "temperature must be <= 2"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			err := ValidateConversationConfig(ConversationConfig{Temperature: tc.in})
			if err == nil {
				t.Errorf("ValidateConversationConfig(Temperature=%g) = nil, want %q", tc.in, tc.want)
				return
			}
			if err.Error() != tc.want {
				t.Errorf("ValidateConversationConfig(Temperature=%g) = %q, want %q", tc.in, err, tc.want)
			}
		})
	}
}

// TestValidateConversationConfig_BadTopP pins
// the bounded [0, 1] top_p rule.
func TestValidateConversationConfig_BadTopP(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{-0.01, "top_p must be >= 0"},
		{-1.0, "top_p must be >= 0"},
		{1.01, "top_p must be <= 1"},
		{2.0, "top_p must be <= 1"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			err := ValidateConversationConfig(ConversationConfig{TopP: tc.in})
			if err == nil {
				t.Errorf("ValidateConversationConfig(TopP=%g) = nil, want %q", tc.in, tc.want)
				return
			}
			if err.Error() != tc.want {
				t.Errorf("ValidateConversationConfig(TopP=%g) = %q, want %q", tc.in, err, tc.want)
			}
		})
	}
}

// TestValidateConversationConfig_BoundsInclusive
// pins that the upper bounds are inclusive: a
// max_turns of exactly 1000, temperature of
// exactly 2.0, and top_p of exactly 1.0 are valid.
func TestValidateConversationConfig_BoundsInclusive(t *testing.T) {
	c := ConversationConfig{
		MaxTurns:    MaxConversationConfigMaxTurns,
		Temperature: MaxConversationConfigTemperature,
		TopP:        MaxConversationConfigTopP,
	}
	if err := ValidateConversationConfig(c); err != nil {
		t.Errorf("ValidateConversationConfig(boundary) = %v, want nil", err)
	}
}

// ---- MemConversationConfigStore in-process tests ---------

// TestConversationConfigStore_DefaultState_Empty
// pins the fresh-store baseline: a fresh
// MemConversationConfigStore has no configs yet
// (the brief's "fresh host has no conversation
// configs" rule). The store's Snapshot returns
// version=1 with configs={} so the wire always sees
// "{}" rather than "null".
//
// NewMemConversationConfigStore's default-
// fabrication path also drives the dispatcher's
// NewDispatcher nil-guard, so this test is the
// in-process mirror of the brief's "fresh store
// seeds empty state" line.
func TestConversationConfigStore_DefaultState_Empty(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	state, err := cs.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != ConversationConfigStateVersion {
		t.Errorf("Version = %d, want %d", state.Version, ConversationConfigStateVersion)
	}
	if len(state.Configs) != 0 {
		t.Fatalf("len(Configs) = %d, want 0", len(state.Configs))
	}
	if state.Configs == nil {
		// Defensive: the wire relies on "{}"
		// rather than "null"; the store must
		// never hand back a nil map.
		t.Errorf("Configs is nil (wire would emit null)")
	}
}

// TestConversationConfigStore_Upsert_New pins the
// happy-path upsert on a fresh store: Upsert with
// an unknown conversation_id creates a new row,
// returns created=true, and the store's Snapshot
// reflects the new row.
func TestConversationConfigStore_Upsert_New(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	created, err := cs.Upsert(context.Background(), "conv_abc", ConversationConfig{
		Model:       "claude-3-7-sonnet",
		ModelEffort: "medium",
		MaxTurns:    50,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !created {
		t.Errorf("Upsert(new) created = false, want true")
	}
	state, err := cs.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Configs) != 1 {
		t.Fatalf("len(state.Configs) = %d, want 1", len(state.Configs))
	}
	got, ok := state.Configs["conv_abc"]
	if !ok {
		t.Fatalf("state.Configs[conv_abc] missing")
	}
	if got.Model != "claude-3-7-sonnet" {
		t.Errorf("state.Configs[conv_abc].Model = %q, want claude-3-7-sonnet", got.Model)
	}
	if got.ModelEffort != "medium" {
		t.Errorf("state.Configs[conv_abc].ModelEffort = %q, want medium", got.ModelEffort)
	}
	if got.MaxTurns != 50 {
		t.Errorf("state.Configs[conv_abc].MaxTurns = %d, want 50", got.MaxTurns)
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("state.Configs[conv_abc].UpdatedAt is zero")
	}
}

// TestConversationConfigStore_Upsert_Update pins
// the idempotent-update path: Upsert with an
// existing conversation_id replaces the row in
// place (the map doesn't grow on a second call).
// The post-mutation config reflects the new
// values; UpdatedAt is bumped to the new clock.
func TestConversationConfigStore_Upsert_Update(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)

	// First upsert: model = claude-3-7-sonnet,
	// effort = medium, max_turns = 50.
	created, err := cs.Upsert(context.Background(), "conv_abc", ConversationConfig{
		Model:       "claude-3-7-sonnet",
		ModelEffort: "medium",
		MaxTurns:    50,
	})
	if err != nil {
		t.Fatalf("Upsert(1): %v", err)
	}
	if !created {
		t.Errorf("Upsert(1) created = false, want true")
	}
	first, _ := cs.Get(context.Background(), "conv_abc")
	firstAt := first.UpdatedAt

	// Sleep so the new clock advances past the
	// first stamp.
	time.Sleep(2 * time.Millisecond)

	// Second upsert: model = gemini-3.8-flash,
	// effort = high, max_turns = 100.
	created, err = cs.Upsert(context.Background(), "conv_abc", ConversationConfig{
		Model:       "gemini-3.8-flash",
		ModelEffort: "high",
		MaxTurns:    100,
	})
	if err != nil {
		t.Fatalf("Upsert(2): %v", err)
	}
	if created {
		t.Errorf("Upsert(2) created = true, want false (upsert should not re-create)")
	}

	second, ok := cs.Get(context.Background(), "conv_abc")
	if !ok {
		t.Fatalf("Get: missing")
	}
	if second.Model != "gemini-3.8-flash" {
		t.Errorf("second.Model = %q, want gemini-3.8-flash", second.Model)
	}
	if second.ModelEffort != "high" {
		t.Errorf("second.ModelEffort = %q, want high", second.ModelEffort)
	}
	if second.MaxTurns != 100 {
		t.Errorf("second.MaxTurns = %d, want 100", second.MaxTurns)
	}
	if !second.UpdatedAt.After(firstAt) {
		t.Errorf("second.UpdatedAt = %v, want > %v", second.UpdatedAt, firstAt)
	}

	// Map should still hold one row (upsert, not
	// append).
	state, err := cs.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Configs) != 1 {
		t.Errorf("len(state.Configs) = %d, want 1 (upsert should not append)", len(state.Configs))
	}
}

// TestConversationConfigStore_Get_Missing pins the
// "unknown conversation_id" path: Get returns
// (zero, false) — the dispatcher doesn't currently
// call Get (the brief is write-only), but the
// helper exists for parity with the
// FSConversationConfigStore so the future
// rc-conversation-config-read cycle can wire it.
func TestConversationConfigStore_Get_Missing(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	c, ok := cs.Get(context.Background(), "conv_unknown")
	if ok {
		t.Errorf("Get(unknown) ok = true, want false")
	}
	if c.UpdatedAt.IsZero() == false {
		// Defensive: a zero-value
		// ConversationConfig should be the
		// return shape.
		t.Errorf("Get(unknown) c = %+v, want zero-value", c)
	}
}

// TestConversationConfigStore_EmptyConvID pins the
// store's defence-in-depth empty-id → sentinel
// guard. The dispatcher's primary validation is
// "empty conversation_id → -32602" (a dashboard
// bug), but the store also enforces strictness so
// a future non-dispatcher caller can't slip
// through.
func TestConversationConfigStore_EmptyConvID(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	_, err := cs.Upsert(context.Background(), "", ConversationConfig{Model: "x"})
	if !errors.Is(err, ErrConversationConfigPersistenceFailed) {
		t.Errorf("Upsert(\"\") err = %v, want ErrConversationConfigPersistenceFailed", err)
	}
	_, err = cs.Upsert(context.Background(), "   ", ConversationConfig{Model: "x"})
	if !errors.Is(err, ErrConversationConfigPersistenceFailed) {
		t.Errorf("Upsert(\"   \") err = %v, want ErrConversationConfigPersistenceFailed", err)
	}
	// Defensive: a refused Upsert must not mutate
	// the in-memory state.
	state, _ := cs.Snapshot(context.Background())
	if len(state.Configs) != 0 {
		t.Errorf("len(state.Configs) = %d, want 0 (refused Upsert must not mutate)", len(state.Configs))
	}
}

// TestConversationConfigStore_Snapshot_DeepCopy
// pins the defensive-copy contract: a Snapshot
// call must not return a map / slice that shares
// storage with the store's internal state. A test
// that mutates the returned map should not
// perturb subsequent Snapshot calls.
func TestConversationConfigStore_Snapshot_DeepCopy(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	_, err := cs.Upsert(context.Background(), "conv_abc", ConversationConfig{
		Model:        "claude-3-7-sonnet",
		AllowedTools: []string{"Read"},
		Metadata:     map[string]string{"k": "v"},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	s1, _ := cs.Snapshot(context.Background())
	// Mutate the returned map. Map values are not
	// addressable, so we copy to a local, mutate,
	// and assign back. The store's defensive-copy
	// contract means the second Snapshot must
	// still see the original values.
	abc := s1.Configs["conv_abc"]
	abc.AllowedTools[0] = "MUTATED_TOOL"
	abc.Metadata["k"] = "MUTATED_VALUE"
	abc.Model = "MUTATED"
	s1.Configs["conv_abc"] = abc
	s1.Configs["conv_NEW"] = ConversationConfig{Model: "NEW"}

	// Re-snapshot — must not see the mutation.
	s2, _ := cs.Snapshot(context.Background())
	if _, ok := s2.Configs["conv_NEW"]; ok {
		t.Errorf("Snapshot returned shared map (mutation leaked)")
	}
	got := s2.Configs["conv_abc"]
	if got.Model != "claude-3-7-sonnet" {
		t.Errorf("snapshot mutation leaked: Model = %q, want claude-3-7-sonnet", got.Model)
	}
	if got.AllowedTools[0] != "Read" {
		t.Errorf("snapshot mutation leaked: AllowedTools[0] = %q, want Read", got.AllowedTools[0])
	}
	if got.Metadata["k"] != "v" {
		t.Errorf("snapshot mutation leaked: Metadata[k] = %q, want v", got.Metadata["k"])
	}
}

// TestMemConversationConfigStoreWith_DeepCopy pins
// the custom-snapshot constructor: the supplied
// map is deep-copied (and so is each config row)
// so a test that mutates the input doesn't race
// the store's internal state.
func TestMemConversationConfigStoreWith_DeepCopy(t *testing.T) {
	original := map[string]ConversationConfig{
		"conv_abc": {
			Model:        "claude-3-7-sonnet",
			AllowedTools: []string{"Read"},
			Metadata:     map[string]string{"k": "v"},
		},
	}
	cs := NewMemConversationConfigStoreWith(original, nil)
	// Mutate the input map after construction. Map
	// values are not addressable, so we replace
	// the row rather than mutate a field on it.
	abc := original["conv_abc"]
	abc.Model = "MUTATED"
	abc.AllowedTools[0] = "MUTATED_TOOL"
	original["conv_abc"] = abc
	original["conv_NEW"] = ConversationConfig{Model: "NEW"}

	// Snapshot must not see the mutations.
	state, _ := cs.Snapshot(context.Background())
	if _, ok := state.Configs["conv_NEW"]; ok {
		t.Errorf("input map mutation leaked into store")
	}
	got := state.Configs["conv_abc"]
	if got.Model != "claude-3-7-sonnet" {
		t.Errorf("input map mutation leaked: Model = %q, want claude-3-7-sonnet", got.Model)
	}
	if got.AllowedTools[0] != "Read" {
		t.Errorf("input map mutation leaked: AllowedTools[0] = %q, want Read", got.AllowedTools[0])
	}
}

// ---- FSConversationConfigStore on-disk tests -------------

// TestFSConversationConfigStore_RoundTrip pins the
// on-disk persistence: an Upsert writes the
// config to JSON; a fresh FSConversationConfigStore
// over the same path reads it back. The atomic
// temp+rename save path is exercised; the
// version stamp round-trips.
func TestFSConversationConfigStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversation_config.json")
	cs1 := NewFSConversationConfigStore(path, nil)
	created, err := cs1.Upsert(context.Background(), "conv_abc", ConversationConfig{
		Model:       "claude-3-7-sonnet",
		ModelEffort: "medium",
		MaxTurns:    50,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !created {
		t.Errorf("Upsert(1) created = false, want true")
	}

	// File exists, is mode 0o600, and has the
	// expected JSON shape.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 0o600", perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Errorf("file missing version stamp:\n%s", data)
	}
	if !strings.Contains(string(data), `"conv_abc"`) {
		t.Errorf("file missing conv_abc key:\n%s", data)
	}
	if !strings.Contains(string(data), `"claude-3-7-sonnet"`) {
		t.Errorf("file missing model value:\n%s", data)
	}

	// Reload via a fresh FSConversationConfigStore
	// over the same path; verify the config
	// round-trips.
	cs2 := NewFSConversationConfigStore(path, nil)
	got, ok := cs2.Get(context.Background(), "conv_abc")
	if !ok {
		t.Fatalf("Get(reload) ok = false, want true")
	}
	if got.Model != "claude-3-7-sonnet" {
		t.Errorf("reload Model = %q, want claude-3-7-sonnet", got.Model)
	}
	if got.ModelEffort != "medium" {
		t.Errorf("reload ModelEffort = %q, want medium", got.ModelEffort)
	}
	if got.MaxTurns != 50 {
		t.Errorf("reload MaxTurns = %d, want 50", got.MaxTurns)
	}

	// Update the same row.
	created, err = cs2.Upsert(context.Background(), "conv_abc", ConversationConfig{
		Model:       "gemini-3.8-flash",
		ModelEffort: "high",
		MaxTurns:    100,
	})
	if err != nil {
		t.Fatalf("Upsert(update): %v", err)
	}
	if created {
		t.Errorf("Upsert(update) created = true, want false")
	}

	// Reload again; verify the update round-trips.
	cs3 := NewFSConversationConfigStore(path, nil)
	got, ok = cs3.Get(context.Background(), "conv_abc")
	if !ok {
		t.Fatalf("Get(reload2) ok = false, want true")
	}
	if got.Model != "gemini-3.8-flash" {
		t.Errorf("reload2 Model = %q, want gemini-3.8-flash", got.Model)
	}
	if got.MaxTurns != 100 {
		t.Errorf("reload2 MaxTurns = %d, want 100", got.MaxTurns)
	}

	// Snapshot returns a non-nil map with one row.
	state, err := cs3.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Configs) != 1 {
		t.Errorf("reload2 len(state.Configs) = %d, want 1", len(state.Configs))
	}
}

// TestFSConversationConfigStore_FreshHost pins the
// fresh-host empty state: a fresh
// FSConversationConfigStore over a path that
// doesn't exist returns the empty state
// (version=1, configs={}).
func TestFSConversationConfigStore_FreshHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversation_config.json")
	cs := NewFSConversationConfigStore(path, nil)
	state, err := cs.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != ConversationConfigStateVersion {
		t.Errorf("Version = %d, want %d", state.Version, ConversationConfigStateVersion)
	}
	if len(state.Configs) != 0 {
		t.Errorf("len(Configs) = %d, want 0", len(state.Configs))
	}
	if state.Configs == nil {
		t.Errorf("Configs is nil (wire would emit null)")
	}
}

// TestFSConversationConfigStore_AtomicSave pins the
// atomic temp + rename save path: an Upsert writes
// to <path>.tmp first, then renames over <path>.
// A pre-existing stale .tmp file (from a crashed
// previous run) must not be left around.
func TestFSConversationConfigStore_AtomicSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversation_config.json")
	cs := NewFSConversationConfigStore(path, nil)
	_, err := cs.Upsert(context.Background(), "conv_abc", ConversationConfig{
		Model: "claude-3-7-sonnet",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Verify: a .tmp file is NOT left behind
	// after a successful atomicWrite.
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("atomic write left .tmp behind: %s", tmpPath)
	}

	// The final file IS at <path>.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("post-Upsert stat %s: %v", path, err)
	}
}

// TestFSConversationConfigStore_VersionBump pins
// the version-mismatch rejection: a file with
// version > ConversationConfigStateVersion
// returns an error (the operator needs to know
// the binary is too old to read the file).
func TestFSConversationConfigStore_VersionBump(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversation_config.json")
	// Hand-write a v2 file (newer than supported).
	rec := onDiskConversationConfigState{
		Version:   ConversationConfigStateVersion + 1,
		Configs:   map[string]ConversationConfig{},
		UpdatedAt: time.Now().UTC(),
	}
	data, _ := jsonMarshal(rec)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cs := NewFSConversationConfigStore(path, nil)
	_, err := cs.Snapshot(context.Background())
	if err == nil {
		t.Errorf("Snapshot on v2 file: err = nil, want non-nil (version mismatch)")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("Snapshot error = %q, want error mentioning version", err)
	}
}

// TestFSConversationConfigStore_Malformed pins the
// malformed-file rejection: a file that parses to
// non-JSON returns an error (the operator needs
// to know the file is corrupt).
func TestFSConversationConfigStore_Malformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversation_config.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cs := NewFSConversationConfigStore(path, nil)
	_, err := cs.Snapshot(context.Background())
	if err == nil {
		t.Errorf("Snapshot on malformed file: err = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("Snapshot error = %q, want error mentioning parse", err)
	}
}

// ---- Dispatcher in-process tests ------------------------

// TestUpsertRemoteAgentConversationConfig_Ok pins
// the happy-path in-process dispatch.
func TestUpsertRemoteAgentConversationConfig_Ok(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	res, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"model":        "claude-3-7-sonnet",
			"model_effort": "medium",
			"max_turns":    50,
		},
	})
	if errObj != nil {
		t.Fatalf("Upsert: error = %#v", errObj)
	}
	if got, _ := res["conversation_id"].(string); got != "conv_abc" {
		t.Errorf("conversation_id = %q, want conv_abc", got)
	}
	if got, _ := res["created"].(bool); !got {
		t.Errorf("created = false, want true (first upsert)")
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("ok = false, want true")
	}
	cfg, ok := res["config"].(ConversationConfig)
	if !ok {
		t.Fatalf("config is not a ConversationConfig, got %T", res["config"])
	}
	if cfg.Model != "claude-3-7-sonnet" {
		t.Errorf("config.model = %q, want claude-3-7-sonnet", cfg.Model)
	}
	if cfg.ModelEffort != "medium" {
		t.Errorf("config.model_effort = %q, want medium", cfg.ModelEffort)
	}
	if cfg.MaxTurns != 50 {
		t.Errorf("config.max_turns = %d, want 50", cfg.MaxTurns)
	}
}

// TestUpsertRemoteAgentConversationConfig_Update
// pins the second-call created=false semantic.
func TestUpsertRemoteAgentConversationConfig_Update(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	d := newConversationConfigDispatcher(t, cs)

	// First upsert: created=true.
	res, _ := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"model": "claude-3-7-sonnet",
		},
	})
	if got, _ := res["created"].(bool); !got {
		t.Errorf("first call created = false, want true")
	}

	// Second upsert: created=false.
	res, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"model": "gemini-3.8-flash",
		},
	})
	if errObj != nil {
		t.Fatalf("Upsert(update): error = %#v", errObj)
	}
	if got, _ := res["created"].(bool); got {
		t.Errorf("second call created = true, want false")
	}
	cfg, _ := res["config"].(ConversationConfig)
	if cfg.Model != "gemini-3.8-flash" {
		t.Errorf("update config.model = %q, want gemini-3.8-flash", cfg.Model)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadModelEffort
// pins the -32602 envelope for an unknown
// model_effort value. The validator rejects the
// value before the store call.
func TestUpsertRemoteAgentConversationConfig_BadModelEffort(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"model_effort": "ultra",
		},
	})
	if errObj == nil {
		t.Fatalf("Upsert(bad effort): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
	if got, _ := errObj["message"].(string); got != "Invalid params" {
		t.Errorf("error.message = %q, want %q", got, "Invalid params")
	}
	if got, _ := errObj["data"].(string); !strings.Contains(got, "model_effort") {
		t.Errorf("error.data = %q, want mentioning model_effort", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadTemperature
// pins the -32602 envelope for an out-of-range
// temperature.
func TestUpsertRemoteAgentConversationConfig_BadTemperature(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"temperature": 3.5,
		},
	})
	if errObj == nil {
		t.Fatalf("Upsert(bad temperature): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadMaxTurns
// pins the -32602 envelope for an out-of-range
// max_turns.
func TestUpsertRemoteAgentConversationConfig_BadMaxTurns(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"max_turns": 9999,
		},
	})
	if errObj == nil {
		t.Fatalf("Upsert(bad max_turns): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadParams_EmptyConvID
// pins the -32602 envelope for an empty / missing
// conversation_id.
func TestUpsertRemoteAgentConversationConfig_BadParams_EmptyConvID(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	cases := []map[string]any{
		{"conversation_id": "", "config": map[string]any{"model": "x"}},
		{"conversation_id": "   ", "config": map[string]any{"model": "x"}},
	}
	for _, params := range cases {
		_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", params)
		if errObj == nil {
			t.Fatalf("Upsert(empty conv id): no error envelope")
		}
		if got, _ := errObj["code"].(int); got != -32602 {
			t.Errorf("error.code = %v, want -32602", got)
		}
	}
}

// TestUpsertRemoteAgentConversationConfig_BadParams_MissingConvID
// pins the -32602 envelope for a params block
// without a conversation_id key at all.
func TestUpsertRemoteAgentConversationConfig_BadParams_MissingConvID(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"config": map[string]any{"model": "x"},
	})
	if errObj == nil {
		t.Fatalf("Upsert(missing conv id): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadParams_MissingConfig
// pins the -32602 envelope for a params block
// without a config key at all (the brief requires
// config as a JSON object, not null).
func TestUpsertRemoteAgentConversationConfig_BadParams_MissingConfig(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
	})
	if errObj == nil {
		t.Fatalf("Upsert(missing config): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadParams_NullConfig
// pins the -32602 envelope for a params block
// with config:null (the brief requires config as
// a JSON object, not null).
func TestUpsertRemoteAgentConversationConfig_BadParams_NullConfig(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config":          nil,
	})
	if errObj == nil {
		t.Fatalf("Upsert(null config): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadParams_BadConvIDType
// pins the -32602 envelope for a numeric
// conversation_id (the dispatcher rejects
// non-string field types before the empty-check).
func TestUpsertRemoteAgentConversationConfig_BadParams_BadConvIDType(t *testing.T) {
	cs := NewMemConversationConfigStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	// Hand-craft the JSON so the numeric value
	// doesn't get auto-coerced by Go's
	// unmarshal path.
	req := jsonRPCFor(1, "UpsertRemoteAgentConversationConfig", []byte(`{"conversation_id":42,"config":{"model":"x"}}`))
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Upsert(numeric conv id): no error envelope, got %#v", resp)
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_NoStore
// pins the -32603 envelope for a dispatcher built
// without a ConversationConfigStore. (The brief
// only emits -32603 when the store is nil; the
// nil-guard in NewDispatcher fabricates a fresh
// store, so this test explicitly clears the field
// post-construction.)
func TestUpsertRemoteAgentConversationConfig_NoStore(t *testing.T) {
	d := newConversationConfigDispatcher(t, nil)
	d.cfg.ConversationConfigStore = nil // explicit clear
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config":          map[string]any{"model": "x"},
	})
	if errObj == nil {
		t.Fatalf("Upsert(no store): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32603 {
		t.Errorf("error.code = %v, want -32603", got)
	}
}

// TestConversationConfigChangedToWire_Pins pins
// the wire envelope the dispatcher emits. The
// response envelope is byte-for-byte identical to
// the conversation_config/changed broadcast
// notification — same helper, same shape.
func TestConversationConfigChangedToWire_Pins(t *testing.T) {
	c := ConversationConfig{
		Model:       "claude-3-7-sonnet",
		ModelEffort: "medium",
		MaxTurns:    50,
		UpdatedAt:   time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	}
	got := conversationConfigChangedToWire("conv_abc", true, c)
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
	if got["conversation_id"] != "conv_abc" {
		t.Errorf("conversation_id = %v, want conv_abc", got["conversation_id"])
	}
	if got["created"] != true {
		t.Errorf("created = %v, want true", got["created"])
	}
	if got["updated_at"] != "2026-10-19T12:00:00Z" {
		t.Errorf("updated_at = %q, want 2026-10-19T12:00:00Z", got["updated_at"])
	}
	cfg, ok := got["config"].(ConversationConfig)
	if !ok {
		t.Fatalf("config is not a ConversationConfig, got %T", got["config"])
	}
	if cfg.Model != "claude-3-7-sonnet" {
		t.Errorf("config.model = %q, want claude-3-7-sonnet", cfg.Model)
	}
}

// TestMapConversationConfigError pins the sentinel
// mapping. The brief calls for -32603 on
// persistence failures; -32602 ("Invalid params")
// is emitted directly from the handler, not via
// this helper.
func TestMapConversationConfigError(t *testing.T) {
	// nil error → nil envelope.
	if e := mapConversationConfigError(nil, "x"); e != nil {
		t.Errorf("mapConversationConfigError(nil) = %v, want nil", e)
	}
	// Persistence failure → -32603.
	e := mapConversationConfigError(ErrConversationConfigPersistenceFailed, "upsert")
	if e == nil {
		t.Fatalf("mapConversationConfigError: nil envelope")
	}
	if e.Code != -32603 {
		t.Errorf("Code = %d, want -32603", e.Code)
	}
	dataStr, _ := e.Data.(string)
	if !strings.Contains(dataStr, "upsert") {
		t.Errorf("Data = %q, want mentioning upsert", e.Data)
	}
}
