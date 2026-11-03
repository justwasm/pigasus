// Package convconfig — store_test.go: the conversation-config
// domain's own tests.
//
// The domain is the fact: the validation rules, the bounds, the
// store's snapshot + upsert + deep-copy behaviour, the on-disk
// round trip, the wire shape of the change notification and the
// sentinel-error mapping.
//
// The dispatcher's round-trip against this domain lives in
// internal/remotecontrol/conversation_config_test.go, because
// the dispatcher is the host's.
package convconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	if NotifChangedMethod != "conversation_config/changed" {
		t.Errorf("NotifChangedMethod = %q, want conversation_config/changed", NotifChangedMethod)
	}
}

// TestConversationConfigStateVersion pins the
// on-disk format version the current store writes.
// v1 is the brief-pinned value; a future cycle
// that introduces a breaking change (renamed keys,
// removed fields) bumps this constant AND updates
// the load() branch that rejects version >
// StateVersion.
func TestConversationConfigStateVersion(t *testing.T) {
	if StateVersion != 1 {
		t.Errorf("StateVersion = %d, want 1", StateVersion)
	}
}

// TestMaxConversationConfigBounds pins the brief's
// per-field bounds. A future cycle that wants to
// raise the limits updates these constants AND the
// Validate validator in
// lockstep.
func TestMaxConversationConfigBounds(t *testing.T) {
	if MaxMaxTurns != 1000 {
		t.Errorf("MaxMaxTurns = %d, want 1000", MaxMaxTurns)
	}
	if MaxTemperature != 2.0 {
		t.Errorf("MaxTemperature = %g, want 2.0", MaxTemperature)
	}
	if MaxTopP != 1.0 {
		t.Errorf("MaxTopP = %g, want 1.0", MaxTopP)
	}
}

// ---- Validate tests --------------------

// TestValidateConversationConfig_Ok pins the
// happy-path validator: a config with all four
// bounded fields at their canonical values passes.
func TestValidateConversationConfig_Ok(t *testing.T) {
	c := Config{
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
	if err := Validate(c); err != nil {
		t.Errorf("Validate(ok) = %v, want nil", err)
	}
}

// TestValidateConversationConfig_AllEmptyOk pins
// the "no override" semantic: a fully-empty config
// (zero values on every bounded field, empty
// strings, nil slices / maps) is valid — the brief
// treats 0 / "" / nil as "the dashboard didn't
// override the default".
func TestValidateConversationConfig_AllEmptyOk(t *testing.T) {
	c := Config{}
	if err := Validate(c); err != nil {
		t.Errorf("Validate(empty) = %v, want nil", err)
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
			err := Validate(Config{ModelEffort: in})
			if err == nil {
				t.Errorf("Validate(ModelEffort=%q) = nil, want error", in)
				return
			}
			if !strings.Contains(err.Error(), "model_effort") {
				t.Errorf("Validate(ModelEffort=%q) = %q, want error mentioning model_effort", in, err)
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
			err := Validate(Config{MaxTurns: tc.in})
			if err == nil {
				t.Errorf("Validate(MaxTurns=%d) = nil, want %q", tc.in, tc.want)
				return
			}
			if err.Error() != tc.want {
				t.Errorf("Validate(MaxTurns=%d) = %q, want %q", tc.in, err, tc.want)
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
			err := Validate(Config{Temperature: tc.in})
			if err == nil {
				t.Errorf("Validate(Temperature=%g) = nil, want %q", tc.in, tc.want)
				return
			}
			if err.Error() != tc.want {
				t.Errorf("Validate(Temperature=%g) = %q, want %q", tc.in, err, tc.want)
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
			err := Validate(Config{TopP: tc.in})
			if err == nil {
				t.Errorf("Validate(TopP=%g) = nil, want %q", tc.in, tc.want)
				return
			}
			if err.Error() != tc.want {
				t.Errorf("Validate(TopP=%g) = %q, want %q", tc.in, err, tc.want)
			}
		})
	}
}

// TestValidateConversationConfig_BoundsInclusive
// pins that the upper bounds are inclusive: a
// max_turns of exactly 1000, temperature of
// exactly 2.0, and top_p of exactly 1.0 are valid.
func TestValidateConversationConfig_BoundsInclusive(t *testing.T) {
	c := Config{
		MaxTurns:    MaxMaxTurns,
		Temperature: MaxTemperature,
		TopP:        MaxTopP,
	}
	if err := Validate(c); err != nil {
		t.Errorf("Validate(boundary) = %v, want nil", err)
	}
}

// ---- MemStore in-process tests ---------

// TestConversationConfigStore_DefaultState_Empty
// pins the fresh-store baseline: a fresh
// MemStore has no configs yet
// (the brief's "fresh host has no conversation
// configs" rule). The store's Snapshot returns
// version=1 with configs={} so the wire always sees
// "{}" rather than "null".
//
// NewMemStore's default-
// fabrication path also drives the dispatcher's
// NewDispatcher nil-guard, so this test is the
// in-process mirror of the brief's "fresh store
// seeds empty state" line.
func TestConversationConfigStore_DefaultState_Empty(t *testing.T) {
	cs := NewMemStore(nil)
	state, err := cs.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != StateVersion {
		t.Errorf("Version = %d, want %d", state.Version, StateVersion)
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
	cs := NewMemStore(nil)
	created, err := cs.Upsert(context.Background(), "conv_abc", Config{
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
	cs := NewMemStore(nil)

	// First upsert: model = claude-3-7-sonnet,
	// effort = medium, max_turns = 50.
	created, err := cs.Upsert(context.Background(), "conv_abc", Config{
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
	created, err = cs.Upsert(context.Background(), "conv_abc", Config{
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
// FSStore so the future
// rc-conversation-config-read cycle can wire it.
func TestConversationConfigStore_Get_Missing(t *testing.T) {
	cs := NewMemStore(nil)
	c, ok := cs.Get(context.Background(), "conv_unknown")
	if ok {
		t.Errorf("Get(unknown) ok = true, want false")
	}
	if c.UpdatedAt.IsZero() == false {
		// Defensive: a zero-value
		// Config should be the
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
	cs := NewMemStore(nil)
	_, err := cs.Upsert(context.Background(), "", Config{Model: "x"})
	if !errors.Is(err, ErrPersistenceFailed) {
		t.Errorf("Upsert(\"\") err = %v, want ErrPersistenceFailed", err)
	}
	_, err = cs.Upsert(context.Background(), "   ", Config{Model: "x"})
	if !errors.Is(err, ErrPersistenceFailed) {
		t.Errorf("Upsert(\"   \") err = %v, want ErrPersistenceFailed", err)
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
	cs := NewMemStore(nil)
	_, err := cs.Upsert(context.Background(), "conv_abc", Config{
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
	s1.Configs["conv_NEW"] = Config{Model: "NEW"}

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
	original := map[string]Config{
		"conv_abc": {
			Model:        "claude-3-7-sonnet",
			AllowedTools: []string{"Read"},
			Metadata:     map[string]string{"k": "v"},
		},
	}
	cs := NewMemStoreWith(original, nil)
	// Mutate the input map after construction. Map
	// values are not addressable, so we replace
	// the row rather than mutate a field on it.
	abc := original["conv_abc"]
	abc.Model = "MUTATED"
	abc.AllowedTools[0] = "MUTATED_TOOL"
	original["conv_abc"] = abc
	original["conv_NEW"] = Config{Model: "NEW"}

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

// ---- FSStore on-disk tests -------------

// TestFSConversationConfigStore_RoundTrip pins the
// on-disk persistence: an Upsert writes the
// config to JSON; a fresh FSStore
// over the same path reads it back. The atomic
// temp+rename save path is exercised; the
// version stamp round-trips.
func TestFSConversationConfigStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversation_config.json")
	cs1 := NewFSStore(path, nil)
	created, err := cs1.Upsert(context.Background(), "conv_abc", Config{
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

	// Reload via a fresh FSStore
	// over the same path; verify the config
	// round-trips.
	cs2 := NewFSStore(path, nil)
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
	created, err = cs2.Upsert(context.Background(), "conv_abc", Config{
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
	cs3 := NewFSStore(path, nil)
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
// FSStore over a path that
// doesn't exist returns the empty state
// (version=1, configs={}).
func TestFSConversationConfigStore_FreshHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversation_config.json")
	cs := NewFSStore(path, nil)
	state, err := cs.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != StateVersion {
		t.Errorf("Version = %d, want %d", state.Version, StateVersion)
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
	cs := NewFSStore(path, nil)
	_, err := cs.Upsert(context.Background(), "conv_abc", Config{
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
// version > StateVersion
// returns an error (the operator needs to know
// the binary is too old to read the file).
func TestFSConversationConfigStore_VersionBump(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversation_config.json")
	// Hand-write a v2 file (newer than supported).
	rec := onDiskState{
		Version:   StateVersion + 1,
		Configs:   map[string]Config{},
		UpdatedAt: time.Now().UTC(),
	}
	data, _ := json.Marshal(rec)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cs := NewFSStore(path, nil)
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
	cs := NewFSStore(path, nil)
	_, err := cs.Snapshot(context.Background())
	if err == nil {
		t.Errorf("Snapshot on malformed file: err = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("Snapshot error = %q, want error mentioning parse", err)
	}
}

// TestConversationConfigChangedToWire_Pins pins
// the wire envelope the dispatcher emits. The
// response envelope is byte-for-byte identical to
// the conversation_config/changed broadcast
// notification — same helper, same shape.
func TestConversationConfigChangedToWire_Pins(t *testing.T) {
	c := Config{
		Model:       "claude-3-7-sonnet",
		ModelEffort: "medium",
		MaxTurns:    50,
		UpdatedAt:   time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	}
	got := changedToWire("conv_abc", true, c)
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
	cfg, ok := got["config"].(Config)
	if !ok {
		t.Fatalf("config is not a Config, got %T", got["config"])
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
	if e := mapError(nil, "x"); e != nil {
		t.Errorf("mapError(nil) = %v, want nil", e)
	}
	// Persistence failure → -32603.
	e := mapError(ErrPersistenceFailed, "upsert")
	if e == nil {
		t.Fatalf("mapError: nil envelope")
	}
	if e.Code != -32603 {
		t.Errorf("Code = %d, want -32603", e.Code)
	}
	dataStr, _ := e.Data.(string)
	if !strings.Contains(dataStr, "upsert") {
		t.Errorf("Data = %q, want mentioning upsert", e.Data)
	}
}
