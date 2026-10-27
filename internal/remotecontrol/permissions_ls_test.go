// Tests for the two permission methods the LanguageServerService
// gained at agy 1.4.8 (GetPermissionSettings /
// UpdatePermissionSettings) and for the vocabulary translation they
// speak.
//
// The reference's own words are the oracle: the enum names come from
// the 1.4.8 descriptor (【一手】), and the answers are checked against
// the proto-JSON shapes the same descriptor describes.
package remotecontrol

import (
	"context"
	"testing"
)

// lsErrorCode reads the numeric code out of a JSON-RPC error
// envelope. The envelope carries it as an int; a decoded float64 is
// accepted too.
func lsErrorCode(errObj map[string]any) int {
	switch code := errObj["code"].(type) {
	case int:
		return code
	case float64:
		return int(code)
	}
	return 0
}

// lsPermissionState reads the store's state directly.
func lsPermissionState(t *testing.T, store PermissionStore) PermissionState {
	t.Helper()
	state, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	return state
}

// TestLSPermissionVocabulary pins the translation table: every enum
// name the reference has must land on a preset the clone models, and
// the clone's own three presets must render to the reference's names.
func TestLSPermissionVocabulary(t *testing.T) {
	referencePresets := []string{
		referencePresetUnspecified,
		referencePresetDefault,
		referencePresetRequestReview,
		referencePresetTurbo,
		referencePresetVetted,
		referencePresetNone,
		referencePresetAuto,
	}
	for _, name := range referencePresets {
		preset, ok := presetFromReferenceName(name)
		if !ok {
			t.Errorf("presetFromReferenceName(%q) has no counterpart", name)
			continue
		}
		if !IsValidPreset(preset) {
			t.Errorf("presetFromReferenceName(%q) = %q, which is not one of the clone's presets", name, preset)
		}
	}
	referenceReviews := []string{
		referenceReviewUnspecified,
		referenceReviewAlways,
		referenceReviewTurbo,
		referenceReviewAuto,
	}
	for _, name := range referenceReviews {
		mode, ok := reviewFromReferenceName(name)
		if !ok {
			t.Errorf("reviewFromReferenceName(%q) has no counterpart", name)
			continue
		}
		if !IsValidReviewMode(mode) {
			t.Errorf("reviewFromReferenceName(%q) = %q, which is not one of the clone's modes", name, mode)
		}
	}
	// The three the clone models have an exact counterpart.
	for preset, want := range map[PermissionPreset]string{
		PermissionPresetDefault:    referencePresetDefault,
		PermissionPresetStrict:     referencePresetRequestReview,
		PermissionPresetPermissive: referencePresetNone,
	} {
		if got := referencePresetName(preset); got != want {
			t.Errorf("referencePresetName(%q) = %q, want %q", preset, got, want)
		}
	}
	for mode, want := range map[ArtifactReviewMode]string{
		ArtifactReviewModeAlways: referenceReviewAlways,
		ArtifactReviewModeNever:  referenceReviewTurbo,
		ArtifactReviewModeAuto:   referenceReviewAuto,
	} {
		if got := referenceReviewName(mode); got != want {
			t.Errorf("referenceReviewName(%q) = %q, want %q", mode, got, want)
		}
	}
	// A name the reference does not have is refused, not guessed.
	if _, ok := presetFromReferenceName("AGENT_PERMISSION_PRESET_NOPE"); ok {
		t.Error("presetFromReferenceName accepted a name the reference does not have")
	}
	if _, ok := reviewFromReferenceName("ARTIFACT_REVIEW_MODE_NOPE"); ok {
		t.Error("reviewFromReferenceName accepted a name the reference does not have")
	}
}

// TestLSGetPermissionSettings_Default pins the answer a fresh host
// gives: the reference's own names, and an empty grants config.
func TestLSGetPermissionSettings_Default(t *testing.T) {
	store := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, store)
	res, errObj := dispatchPermissions(t, d, "GetPermissionSettings", map[string]any{})
	if errObj != nil {
		t.Fatalf("GetPermissionSettings: error = %#v", errObj)
	}
	if res["globalPreset"] != referencePresetDefault {
		t.Errorf("globalPreset = %v, want %q", res["globalPreset"], referencePresetDefault)
	}
	if res["globalArtifactReviewMode"] != referenceReviewAlways {
		t.Errorf("globalArtifactReviewMode = %v, want %q", res["globalArtifactReviewMode"], referenceReviewAlways)
	}
	grants, ok := res["globalGrants"].(map[string]any)
	if !ok {
		t.Fatalf("globalGrants = %#v, want an object", res["globalGrants"])
	}
	if len(grants) != 0 {
		t.Errorf("globalGrants = %#v, want an empty object", grants)
	}
}

// TestLSGetPermissionSettings_AfterUpdate pins the round trip: what
// the update writes is what the read reports, in the reference's
// vocabulary.
func TestLSGetPermissionSettings_AfterUpdate(t *testing.T) {
	store := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, store)
	res, errObj := dispatchPermissions(t, d, "UpdatePermissionSettings", map[string]any{
		"preset":             referencePresetRequestReview,
		"artifactReviewMode": referenceReviewTurbo,
		"grants": map[string]any{
			"allow": []string{"run_command:go test*"},
		},
	})
	if errObj != nil {
		t.Fatalf("UpdatePermissionSettings: error = %#v", errObj)
	}
	if len(res) != 0 {
		t.Errorf("UpdatePermissionSettings answered %#v, want the empty message", res)
	}
	state := lsPermissionState(t, store)
	if state.Preset != PermissionPresetStrict {
		t.Errorf("preset = %q, want %q", state.Preset, PermissionPresetStrict)
	}
	if state.Review != ArtifactReviewModeNever {
		t.Errorf("review = %q, want %q", state.Review, ArtifactReviewModeNever)
	}
	if len(state.Grants) != 1 || state.Grants[0].ToolName != "run_command" || state.Grants[0].Pattern != "go test*" {
		t.Fatalf("grants = %#v, want one run_command:go test* rule", state.Grants)
	}
	res, errObj = dispatchPermissions(t, d, "GetPermissionSettings", map[string]any{})
	if errObj != nil {
		t.Fatalf("GetPermissionSettings: error = %#v", errObj)
	}
	if res["globalPreset"] != referencePresetRequestReview {
		t.Errorf("globalPreset = %v, want %q", res["globalPreset"], referencePresetRequestReview)
	}
	if res["globalArtifactReviewMode"] != referenceReviewTurbo {
		t.Errorf("globalArtifactReviewMode = %v, want %q", res["globalArtifactReviewMode"], referenceReviewTurbo)
	}
	grants, _ := res["globalGrants"].(map[string]any)
	allow, _ := grants["allow"].([]string)
	if len(allow) != 1 || allow[0] != "run_command:go test*" {
		t.Errorf("globalGrants.allow = %#v, want [run_command:go test*]", allow)
	}
}

// TestLSUpdatePermissionSettings_SnakeCase pins that the proto field
// name is accepted as well as the JSON name.
func TestLSUpdatePermissionSettings_SnakeCase(t *testing.T) {
	store := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, store)
	if _, errObj := dispatchPermissions(t, d, "UpdatePermissionSettings", map[string]any{
		"artifact_review_mode": referenceReviewAuto,
	}); errObj != nil {
		t.Fatalf("UpdatePermissionSettings: error = %#v", errObj)
	}
	if got := lsPermissionState(t, store).Review; got != ArtifactReviewModeAuto {
		t.Errorf("review = %q, want %q", got, ArtifactReviewModeAuto)
	}
}

// TestLSUpdatePermissionSettings_Rejects pins the three refusals: an
// unknown preset, a grant rule the clone cannot parse, and a deny /
// ask rule the clone's allow-only grant model cannot hold.
func TestLSUpdatePermissionSettings_Rejects(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
	}{
		{"unknown preset", map[string]any{"preset": "AGENT_PERMISSION_PRESET_NOPE"}},
		{"unknown review mode", map[string]any{"artifactReviewMode": "ARTIFACT_REVIEW_MODE_NOPE"}},
		{"malformed grant", map[string]any{"grants": map[string]any{"allow": []string{"no-colon"}}}},
		{"deny rule", map[string]any{"grants": map[string]any{"deny": []string{"run_command:rm -rf*"}}}},
		{"ask rule", map[string]any{"grants": map[string]any{"ask": []string{"run_command:git*"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemPermissionStore(nil)
			d := newPermissionsDispatcher(t, store)
			_, errObj := dispatchPermissions(t, d, "UpdatePermissionSettings", tc.params)
			if errObj == nil {
				t.Fatalf("UpdatePermissionSettings(%v) answered, want an error", tc.params)
			}
			if code := lsErrorCode(errObj); code != -32602 {
				t.Errorf("error code = %v, want -32602 (%#v)", errObj["code"], errObj)
			}
		})
	}
}

// TestLSUpdatePermissionSettings_ReplacesGrants pins that the update
// carries the whole grant list: a second update replaces the first.
func TestLSUpdatePermissionSettings_ReplacesGrants(t *testing.T) {
	store := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, store)
	if _, errObj := dispatchPermissions(t, d, "UpdatePermissionSettings", map[string]any{
		"grants": map[string]any{"allow": []string{"run_command:git status"}},
	}); errObj != nil {
		t.Fatalf("UpdatePermissionSettings: error = %#v", errObj)
	}
	if _, errObj := dispatchPermissions(t, d, "UpdatePermissionSettings", map[string]any{
		"grants": map[string]any{"allow": []string{"run_command:git log"}},
	}); errObj != nil {
		t.Fatalf("UpdatePermissionSettings: error = %#v", errObj)
	}
	state := lsPermissionState(t, store)
	if len(state.Grants) != 1 || state.Grants[0].Pattern != "git log" {
		t.Errorf("grants = %#v, want only run_command:git log", state.Grants)
	}
}

// TestLSPermissionMethods_DefaultStore pins what a dispatcher that
// was not given a store answers: the dispatcher installs an in-memory
// one, so the two methods answer the defaults rather than failing.
func TestLSPermissionMethods_DefaultStore(t *testing.T) {
	d, err := NewDispatcher(Config{Session: NewSessionState("1.4.0")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	res, errObj := dispatchPermissions(t, d, "GetPermissionSettings", map[string]any{})
	if errObj != nil {
		t.Fatalf("GetPermissionSettings: error = %#v", errObj)
	}
	if res["globalPreset"] != referencePresetDefault {
		t.Errorf("globalPreset = %v, want %q", res["globalPreset"], referencePresetDefault)
	}
}
