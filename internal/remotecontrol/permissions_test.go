// Package remotecontrol — permissions_test.go: the dispatcher's
// round-trip against the permissions domain.
//
// The domain's own tests live in
// internal/remotecontrol/permissions; this file exercises the wire
// the dispatcher produces.
//
// Wire-level tests live in
// remotetransport/tcp_test.go (mirroring
// the project's split: in-process envelopes vs.
// wire round-trips).
package remotecontrol

import (
	"context"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/remotecontrol/permissions"
)

// newPermissionsDispatcher returns a Dispatcher
// with the supplied permissions.Store pre-installed.
// The helper mirrors newProjectsDispatcher.
func newPermissionsDispatcher(t *testing.T, ps permissions.Store) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:          sess,
		PermissionsStore: ps,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchPermissions sends a Set/Add/Remove
// request through the dispatcher and returns the
// (result, error) pair. Mirrors dispatchProjects.
func dispatchPermissions(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	if params != nil {
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

// ---- dispatcher envelope tests ------------------------------

// TestSetRemoteAgentPermissionPreset_Ok pins the
// happy-path SetPreset envelope: result carries
// the post-state (preset updated, review stable,
// grants stable).
func TestSetRemoteAgentPermissionPreset_Ok(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	res, errObj := dispatchPermissions(t, d, "SetRemoteAgentPermissionPreset", map[string]any{
		"preset": "strict",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if res["preset"] != "strict" {
		t.Errorf("result.preset = %v, want strict", res["preset"])
	}
	if res["review"] != string(permissions.DefaultReviewMode) {
		t.Errorf("result.review = %v, want %v", res["review"], permissions.DefaultReviewMode)
	}
	if v, _ := res["version"].(int); v != permissions.StateWireFormat {
		t.Errorf("result.version = %v, want %d", res["version"], permissions.StateWireFormat)
	}
}

// TestSetRemoteAgentPermissionPreset_BadParams pins
// the unknown-preset → -32602 path.
func TestSetRemoteAgentPermissionPreset_BadParams(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "SetRemoteAgentPermissionPreset", map[string]any{
		"preset": "ULTRA-STRICT",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
}

// TestSetRemoteAgentPermissionPreset_NoStore pins
// the nil-store → -32603 path. (NewDispatcher
// fabricates a default, so we explicitly build a
// Dispatcher with no PermissionsStore — using the
// package's nil-friendly NewDispatcher default.)
func TestSetRemoteAgentPermissionPreset_NoStore(t *testing.T) {
	// Build a Config that explicitly says "no
	// PermissionsStore". NewDispatcher's nil-guard
	// fabricates a fresh permissions.MemStore, but for
	// the negative test we want to verify the
	// "store reports broken state" path. Use a
	// minimal sentinel by overriding the field after
	// construction.
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.PermissionsStore = nil
	_, errObj := dispatchPermissions(t, d, "SetRemoteAgentPermissionPreset", map[string]any{
		"preset": "strict",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %d, want -32603", codeAsInt(t, errObj["code"]))
	}
}

// TestSetRemoteAgentArtifactReviewMode_Ok pins the
// happy path for SetReview.
func TestSetRemoteAgentArtifactReviewMode_Ok(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	res, errObj := dispatchPermissions(t, d, "SetRemoteAgentArtifactReviewMode", map[string]any{
		"mode": "never",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if res["review"] != "never" {
		t.Errorf("result.review = %v, want never", res["review"])
	}
}

// TestSetRemoteAgentArtifactReviewMode_BadParams
// pins the unknown-mode → -32602 path.
func TestSetRemoteAgentArtifactReviewMode_BadParams(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "SetRemoteAgentArtifactReviewMode", map[string]any{
		"mode": "definitely-not-a-mode",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
}

// TestAddRemoteAgentPermissionGrants_Ok pins the
// happy-path Add: result carries the new grants
// array with the added entries.
func TestAddRemoteAgentPermissionGrants_Ok(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	res, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install"},
		"reason":        "USER approved",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	grants, ok := res["grants"].([]any)
	if !ok {
		t.Fatalf("grants = %T, want []any: %#v", res["grants"], res)
	}
	if len(grants) != 1 {
		t.Fatalf("len(grants) = %d, want 1", len(grants))
	}
	g0 := grants[0].(map[string]any)
	if g0["tool_name"] != "bash" {
		t.Errorf("grants[0].tool_name = %v, want bash", g0["tool_name"])
	}
	if g0["pattern"] != "npm install" {
		t.Errorf("grants[0].pattern = %v, want npm install", g0["pattern"])
	}
	if g0["reason"] != "USER approved" {
		t.Errorf("grants[0].reason = %v, want \"USER approved\"", g0["reason"])
	}
}

// TestAddRemoteAgentPermissionGrants_BatchOk pins
// the multi-entry Add path.
func TestAddRemoteAgentPermissionGrants_BatchOk(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	res, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{
			"bash:npm install",
			"write_file:*.go",
			"read_file:/etc/hosts",
		},
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	grants := res["grants"].([]any)
	if len(grants) != 3 {
		t.Errorf("len(grants) = %d, want 3", len(grants))
	}
}

// TestAddRemoteAgentPermissionGrants_Empty pins
// the empty-tool_patterns → -32602 path.
func TestAddRemoteAgentPermissionGrants_Empty(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
}

// TestAddRemoteAgentPermissionGrants_Malformed pins
// the malformed "<tool_name>:<pattern>" → -32602
// path.
func TestAddRemoteAgentPermissionGrants_Malformed(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"missing-colon"},
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
}

// TestAddRemoteAgentPermissionGrants_Duplicate pins
// the duplicate-key → -32014 path.
func TestAddRemoteAgentPermissionGrants_Duplicate(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	if _, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install"},
	}); errObj != nil {
		t.Fatalf("first add err = %#v", errObj)
	}
	_, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install"},
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32014 {
		t.Errorf("code = %d, want -32014 (Permission grant exists)", codeAsInt(t, errObj["code"]))
	}
}

// TestRemoveRemoteAgentPermissionGrants_Ok pins
// the happy-path Remove.
func TestRemoveRemoteAgentPermissionGrants_Ok(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	if _, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install", "bash:pnpm install"},
	}); errObj != nil {
		t.Fatalf("seed add err = %#v", errObj)
	}
	res, errObj := dispatchPermissions(t, d, "RemoveRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install"},
	})
	if errObj != nil {
		t.Fatalf("remove err = %#v", errObj)
	}
	grants := res["grants"].([]any)
	if len(grants) != 1 {
		t.Errorf("len(grants) = %d, want 1", len(grants))
	}
	if g0 := grants[0].(map[string]any); g0["pattern"] != "pnpm install" {
		t.Errorf("remaining grant pattern = %v, want pnpm install", g0["pattern"])
	}
}

// TestRemoveRemoteAgentPermissionGrants_Empty pins
// the empty-tool_patterns → -32602 path.
func TestRemoveRemoteAgentPermissionGrants_Empty(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "RemoveRemoteAgentPermissionGrants", map[string]any{})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
}

// TestRemoveRemoteAgentPermissionGrants_Malformed
// pins the malformed-key → -32602 path with the
// verbatim entry echoed in `data`.
func TestRemoveRemoteAgentPermissionGrants_Malformed(t *testing.T) {
	ps := permissions.NewMemStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "RemoveRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"missing-colon"},
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
	if data, _ := errObj["data"].(string); !strings.Contains(data, "missing-colon") {
		t.Errorf("data = %q, want it to contain the malformed entry", data)
	}
}

// TestRemoveRemoteAgentPermissionGrants_NoStore pins
// the nil-store → -32603 path.
func TestRemoveRemoteAgentPermissionGrants_NoStore(t *testing.T) {
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.PermissionsStore = nil
	_, errObj := dispatchPermissions(t, d, "RemoveRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install"},
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %d, want -32603", codeAsInt(t, errObj["code"]))
	}
}
