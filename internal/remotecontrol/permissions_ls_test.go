// Tests for the two permission methods the LanguageServerService
// gained at agy 1.4.8 (GetPermissionSettings /
// UpdatePermissionSettings).
//
// The reference's own words are the oracle: the enum names come from
// the 1.4.8 descriptor (【一手】), and the answers are checked against
// the proto-JSON shapes the same descriptor describes.
//
// The vocabulary translation itself is the domain's fact, so its own
// test lives in internal/remotecontrol/permissions/vocabulary_test.go.
package remotecontrol

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/remotecontrol/permissions"
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
func lsPermissionState(t *testing.T, store permissions.Store) permissions.State {
	t.Helper()
	state, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	return state
}

// TestLSGetPermissionSettings_Default pins the answer a fresh host
// gives: the reference's own names, and an empty grants config.
func TestLSGetPermissionSettings_Default(t *testing.T) {
	store := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, store)
	res, errObj := dispatchPermissions(t, d, "GetPermissionSettings", map[string]any{})
	if errObj != nil {
		t.Fatalf("GetPermissionSettings: error = %#v", errObj)
	}
	if res["globalPreset"] != permissions.ReferencePresetDefault {
		t.Errorf("globalPreset = %v, want %q", res["globalPreset"], permissions.ReferencePresetDefault)
	}
	if res["globalArtifactReviewMode"] != permissions.ReferenceReviewAlways {
		t.Errorf("globalArtifactReviewMode = %v, want %q", res["globalArtifactReviewMode"], permissions.ReferenceReviewAlways)
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
	store := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, store)
	res, errObj := dispatchPermissions(t, d, "UpdatePermissionSettings", map[string]any{
		"preset":             permissions.ReferencePresetRequestReview,
		"artifactReviewMode": permissions.ReferenceReviewTurbo,
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
	if state.Preset != permissions.PresetStrict {
		t.Errorf("preset = %q, want %q", state.Preset, permissions.PresetStrict)
	}
	if state.Review != permissions.ReviewModeNever {
		t.Errorf("review = %q, want %q", state.Review, permissions.ReviewModeNever)
	}
	if len(state.Grants) != 1 || state.Grants[0].ToolName != "run_command" || state.Grants[0].Pattern != "go test*" {
		t.Fatalf("grants = %#v, want one run_command:go test* rule", state.Grants)
	}
	res, errObj = dispatchPermissions(t, d, "GetPermissionSettings", map[string]any{})
	if errObj != nil {
		t.Fatalf("GetPermissionSettings: error = %#v", errObj)
	}
	if res["globalPreset"] != permissions.ReferencePresetRequestReview {
		t.Errorf("globalPreset = %v, want %q", res["globalPreset"], permissions.ReferencePresetRequestReview)
	}
	if res["globalArtifactReviewMode"] != permissions.ReferenceReviewTurbo {
		t.Errorf("globalArtifactReviewMode = %v, want %q", res["globalArtifactReviewMode"], permissions.ReferenceReviewTurbo)
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
	store := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, store)
	if _, errObj := dispatchPermissions(t, d, "UpdatePermissionSettings", map[string]any{
		"artifact_review_mode": permissions.ReferenceReviewAuto,
	}); errObj != nil {
		t.Fatalf("UpdatePermissionSettings: error = %#v", errObj)
	}
	if got := lsPermissionState(t, store).Review; got != permissions.ReviewModeAuto {
		t.Errorf("review = %q, want %q", got, permissions.ReviewModeAuto)
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
			store := permissions.NewMemStore(nil)
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
	store := permissions.NewMemStore(nil)
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
	if res["globalPreset"] != permissions.ReferencePresetDefault {
		t.Errorf("globalPreset = %v, want %q", res["globalPreset"], permissions.ReferencePresetDefault)
	}
}
