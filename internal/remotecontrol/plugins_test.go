// Package remotecontrol — plugins_test.go: the dispatcher's
// round-trip against the plugins domain.
//
// The domain's own tests live in internal/remotecontrol/plugins;
// this file exercises the wire the dispatcher produces.
//
// Wire-level round-trips (ws adapter + JSON marshalling)
// live in remotetransport/tcp_test.go, mirroring the split
// the rc-file-ops cycle established.
package remotecontrol

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/remotecontrol/plugins"
)

// newPluginsDispatcher returns a Dispatcher with
// the supplied plugins.Store pre-installed. The
// helper mirrors newPermissionsDispatcher.
func newPluginsDispatcher(t *testing.T, ps plugins.Store) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:      sess,
		PluginsStore: ps,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchPlugins sends a List / Disconnect
// request through the dispatcher and returns the
// (result, error) pair. Mirrors dispatchPermissions.
//
// nil params marshals to JSON `null` so the
// dispatcher's "len(params) > 0" guards see a
// present (but null) params block; jsonRPCFor
// takes a byte slice and appends verbatim, so we
// mirror the convention `[]byte("null")` rather
// than a missing-key shape — the brief pins
// `{"method": "ListRemoteAgentPlugins"}` as a
// params-less call that's accepted by the
// dispatcher (List has no required inputs).
func dispatchPlugins(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
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

// TestListRemoteAgentPlugins_Ok pins the happy
// path: a fresh plugins.MemStore receives a
// List call; the dispatcher returns the
// fixture-seeded state with three plugins,
// sorted by plugin_id ascending, in the
// brief's `{ok:true, plugins:[...]}` envelope.
func TestListRemoteAgentPlugins_Ok(t *testing.T) {
	ps := plugins.NewMemStore(nil)
	d := newPluginsDispatcher(t, ps)
	res, errObj := dispatchPlugins(t, d, "ListRemoteAgentPlugins", nil)
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if got := res["ok"]; got != true {
		t.Errorf("result.ok = %v, want true", got)
	}
	plugins, ok := res["plugins"].([]any)
	if !ok {
		t.Fatalf("plugins = %T, want []any: %#v", res["plugins"], res)
	}
	if len(plugins) != 3 {
		t.Fatalf("len(plugins) = %d, want 3", len(plugins))
	}
	// Sorted: claude-code-bridge, github-copilot-bridge, mcp-bridge.
	wantIDs := []string{"claude-code-bridge", "github-copilot-bridge", "mcp-bridge"}
	for i, want := range wantIDs {
		p, _ := plugins[i].(map[string]any)
		if got, _ := p["plugin_id"].(string); got != want {
			t.Errorf("plugins[%d].plugin_id = %q, want %q", i, got, want)
		}
		// Every plugin carries the
		// connected_at timestamp and a
		// non-empty capability list.
		if got, _ := p["connected_at"].(string); got == "" {
			t.Errorf("plugins[%d].connected_at missing or empty: %#v", i, p["connected_at"])
		}
		caps, _ := p["capabilities"].([]any)
		if len(caps) == 0 {
			t.Errorf("plugins[%d].capabilities is empty", i)
		}
	}
}

// TestListRemoteAgentPlugins_OkWithParams
// pins the spec's "params are ignored" path:
// the dispatcher accepts a non-null params
// object on List (returns the same envelope).
func TestListRemoteAgentPlugins_OkWithParams(t *testing.T) {
	ps := plugins.NewMemStore(nil)
	d := newPluginsDispatcher(t, ps)
	res, errObj := dispatchPlugins(t, d, "ListRemoteAgentPlugins", map[string]any{
		"unused_key": "irrelevant",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if got := res["ok"]; got != true {
		t.Errorf("result.ok = %v, want true", got)
	}
	plugins, ok := res["plugins"].([]any)
	if !ok || len(plugins) != 3 {
		t.Errorf("plugins = %#v, want 3 entries", res["plugins"])
	}
}

// TestDisconnectRemoteAgentPlugin_Ok pins the
// happy-path Disconnect: a fresh fixture-
// seeded plugins.MemStore receives a Disconnect
// for "mcp-bridge", and the response envelope
// is the brief's lean `{ok:true, plugin_id,
// disconnected_at}` shape. The full post-
// mutation state (mcp-bridge gone) is verified
// via a follow-up List call and via the
// plugins/changed broadcast pinned by the
// wire-level test in remotetransport/.
func TestDisconnectRemoteAgentPlugin_Ok(t *testing.T) {
	ps := plugins.NewMemStore(nil)
	d := newPluginsDispatcher(t, ps)
	res, errObj := dispatchPlugins(t, d, "DisconnectRemoteAgentPlugin", map[string]any{
		"plugin_id": "mcp-bridge",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if got := res["ok"]; got != true {
		t.Errorf("result.ok = %v, want true", got)
	}
	if got, _ := res["plugin_id"].(string); got != "mcp-bridge" {
		t.Errorf("result.plugin_id = %q, want mcp-bridge", got)
	}
	if got, _ := res["disconnected_at"].(string); got == "" {
		t.Errorf("result.disconnected_at missing or empty: %#v", res["disconnected_at"])
	}
	// Sanity: the post-mutation state is
	// observable via List (the dispatcher
	// doesn't fabricate the response; the
	// store actually mutated).
	listRes, listErr := dispatchPlugins(t, d, "ListRemoteAgentPlugins", nil)
	if listErr != nil {
		t.Fatalf("list err = %#v", listErr)
	}
	plugins := listRes["plugins"].([]any)
	if len(plugins) != 2 {
		t.Fatalf("post-Disconnect len(plugins) = %d, want 2", len(plugins))
	}
	for _, raw := range plugins {
		p, _ := raw.(map[string]any)
		if got, _ := p["plugin_id"].(string); got == "mcp-bridge" {
			t.Errorf("mcp-bridge still in list.plugins after Disconnect: %#v", p)
		}
	}
}

// TestDisconnectRemoteAgentPlugin_NotFound
// pins the unknown-id → -32015 path.
func TestDisconnectRemoteAgentPlugin_NotFound(t *testing.T) {
	ps := plugins.NewMemStore(nil)
	d := newPluginsDispatcher(t, ps)
	_, errObj := dispatchPlugins(t, d, "DisconnectRemoteAgentPlugin", map[string]any{
		"plugin_id": "no-such-plugin",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32015 {
		t.Errorf("code = %d, want -32015 (Plugin not found)", codeAsInt(t, errObj["code"]))
	}
	if msg, _ := errObj["message"].(string); msg != "Plugin not found" {
		t.Errorf("message = %q, want %q", msg, "Plugin not found")
	}
}

// TestDisconnectRemoteAgentPlugin_BadParams
// pins the empty / non-string plugin_id →
// -32602 path (the "dashboard bug" envelope).
func TestDisconnectRemoteAgentPlugin_BadParams(t *testing.T) {
	cases := []struct {
		label  string
		params map[string]any
	}{
		{"empty string", map[string]any{"plugin_id": ""}},
		{"whitespace-only", map[string]any{"plugin_id": "   "}},
		{"missing key", map[string]any{}},
		{"non-string number", map[string]any{"plugin_id": 42}},
		{"non-string bool", map[string]any{"plugin_id": true}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			ps := plugins.NewMemStore(nil)
			d := newPluginsDispatcher(t, ps)
			_, errObj := dispatchPlugins(t, d, "DisconnectRemoteAgentPlugin", tc.params)
			if errObj == nil {
				t.Fatalf("expected error envelope, got result")
			}
			if codeAsInt(t, errObj["code"]) != -32602 {
				t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
			}
		})
	}
}

// TestDisconnectRemoteAgentPlugin_NoStore pins
// the nil-store → -32603 path.
func TestDisconnectRemoteAgentPlugin_NoStore(t *testing.T) {
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.PluginsStore = nil
	_, errObj := dispatchPlugins(t, d, "DisconnectRemoteAgentPlugin", map[string]any{
		"plugin_id": "claude-code-bridge",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %d, want -32603", codeAsInt(t, errObj["code"]))
	}
}

// TestListRemoteAgentPlugins_NoStore pins the
// nil-store → -32603 path on the read-side
// (the dispatcher fabricates a
// plugins.MemStore by default, but tests can
// clear the field after construction).
func TestListRemoteAgentPlugins_NoStore(t *testing.T) {
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.PluginsStore = nil
	_, errObj := dispatchPlugins(t, d, "ListRemoteAgentPlugins", nil)
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %d, want -32603", codeAsInt(t, errObj["code"]))
	}
}
