// Tests for the rc-status-tool cycle's new JSON-RPC methods.
//
// Four methods this cycle adds:
//
//   - RemoteAgentErrorStatus — per-cascade bounded error ring.
//   - SetRemoteControlSession — binds a session id to the
//     dispatcher's AuthState.
//   - remoteControlVersion — version / build_label / daemon_sha.
//
// Note: rc-status-tool originally shipped a fifth method
// (EnsureRemoteAgentACEVM, parity stub). Cycle rc-ace-real
// removed it; the test that previously pinned the stub
// (TestEnsureRemoteAgentACE_Stub) became TestEnsureRemoteAgentACEVM_Removed
// and asserts the dispatcher returns -32601 "Method not found".
//
// Plus the rich-shape rewrite of the existing `status` MCP tool
// (the dispatcher-level coverage is in daemon_test.go's
// TestStatusToolReturnsSnapshot; this file exercises the
// daemon-meta + auth-state paths independently).
//
// The tests use (*Dispatcher).Dispatch directly so they pin the
// in-process surface without dragging in the Unix-socket or the
// WS transport; the wire-level surface is covered by the WS
// round-trip tests in internal/remotetransport/tcp_test.go.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// richShapeReq marshals a minimal JSON-RPC request for
// `tools/call` → `status` so the in-process tests below can
// decode the response envelope without repeating the literal in
// every test body.
func richShapeReq(t *testing.T) []byte {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "status",
			"arguments": map[string]any{},
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// decodeRichShape runs the rich-shape status dispatch and
// returns the decoded inner snapshot object. Returns nil + t.Fatal
// on any decode failure so callers can use plain dereferencing
// in their body.
func decodeRichShape(t *testing.T, d *Dispatcher) map[string]any {
	t.Helper()
	resp := d.Dispatch(context.Background(), richShapeReq(t))
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("status: no result, got %#v", resp)
	}
	text := extractStatusText(t, res)
	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("status returned isError: %s", text)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(text), &snap); err != nil {
		t.Fatalf("status text is not JSON: %v\n%s", err, text)
	}
	return snap
}

// extractStatusText pulls the single text field out of the
// MCP-shaped content envelope. The dispatcher in-process API
// returns the content slice as either `[]any` (when the
// underlying value went through a generic-cast) or as
// `[]map[string]any` (the literal type the handler emits).
// Both come back with a single text-bearing entry whose
// .text is the JSON-serialised snapshot; we read it either
// way.
func extractStatusText(t *testing.T, res map[string]any) string {
	t.Helper()
	raw, ok := res["content"]
	if !ok {
		t.Fatalf("status: content missing, got %#v", res)
	}
	entries := asSliceOfMaps(t, raw)
	if len(entries) == 0 {
		t.Fatalf("status: empty content, got %#v", res)
	}
	text, _ := entries[0]["text"].(string)
	return text
}

// asSliceOfMaps converts whatever slice is in `raw` to
// []map[string]any. The dispatcher's handlers emit typed
// slices (e.g. `[]map[string]any`, `[]CascadeError`); the
// wire-level test path sees these as `[]any` after the JSON
// round-trip. This helper handles both so the same
// assertion works at either layer.
func asSliceOfMaps(t *testing.T, raw any) []map[string]any {
	t.Helper()
	switch v := raw.(type) {
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, e := range v {
			m, _ := e.(map[string]any)
			out = append(out, m)
		}
		return out
	case []map[string]any:
		return v
	}
	t.Fatalf("status: content type %T, want slice of objects", raw)
	return nil
}

// asAnySlice converts a typed slice in `raw` to []any. The
// dispatcher's handlers emit typed slices (e.g.
// `[]CascadeError`); when called in-process those slices
// don't match the `[]any` assertion a JSON-decoded wire
// result makes. This helper lets a test assert "this is a
// slice of N entries" without naming the concrete element
// type.
func asAnySlice(t *testing.T, raw any) []any {
	t.Helper()
	switch v := raw.(type) {
	case []any:
		return v
	case []CascadeError:
		out := make([]any, 0, len(v))
		for _, e := range v {
			out = append(out, map[string]any{
				"at":      e.At,
				"method":  e.Method,
				"message": e.Message,
				"code":    e.Code,
			})
		}
		return out
	case []string:
		out := make([]any, 0, len(v))
		for _, e := range v {
			out = append(out, e)
		}
		return out
	}
	t.Fatalf("slice type %T, want []any / []CascadeError / []string", raw)
	return nil
}

// newStatusDispatcher brings up a *Dispatcher with a fresh
// SessionState carrying the test's intent. Used by the rich-
// shape + new-RPC tests, which need direct Dispatch access —
// the daemon-level helper (newTestDaemon) returns a *Daemon
// with an unexported dispatcher field, which isn't reachable
// from these tests in this package.
func newStatusDispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.3.4-test")
	sess.Workspace = "/tmp/workspace"
	sess.Backend = "gateway"
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.SetConversationID("conv-abc")
	sess.SetTurnOutput("hello from the host")
	d, err := NewDispatcher(Config{
		Session: sess,
		Agents:  DefaultAgentLister,
		Reader:  DefaultOutputReader{},
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// TestRichStatusShape_DaemonMeta exercises the daemon block of
// the rich status tool independently of the rest of the
// snapshot: when Config.DaemonMeta is set, the daemon block
// surfaces its Transport/Addr/Mode verbatim. This pins the
// path the foreground daemon's wiring (cmd/wagyu/
// remote_control_foreground.go) takes.
func TestRichStatusShape_DaemonMeta(t *testing.T) {
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session: sess,
		DaemonMeta: &DaemonMeta{
			Transport: "tcp+ws",
			Addr:      "0.0.0.0:8787",
			Mode:      "persistent",
		},
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	snap := decodeRichShape(t, d)
	daemon, ok := snap["daemon"].(map[string]any)
	if !ok {
		t.Fatalf("rich.daemon missing or not an object: %v", snap["daemon"])
	}
	if daemon["transport"] != "tcp+ws" {
		t.Errorf("daemon.transport = %v, want tcp+ws", daemon["transport"])
	}
	if daemon["addr"] != "0.0.0.0:8787" {
		t.Errorf("daemon.addr = %v, want 0.0.0.0:8787", daemon["addr"])
	}
	if daemon["mode"] != "persistent" {
		t.Errorf("daemon.mode = %v, want persistent", daemon["mode"])
	}
}

// TestRichStatusShape_AuthStateInitial covers the
// "freshly-installed AuthState" path: the dispatcher reads
// method="google", session_id="<supplied>", user_id="<opt>",
// scopes=["read","write"], expires_at=<rfc3339> verbatim.
//
// Without an AuthState the dispatcher fabricates a zero
// snapshot and the auth block emits method="none" + empty
// fields + scopes=[] + expires_at=null. Pre-installed
// AuthState round-trips through Snapshot's defensive copy
// (so a concurrent Set from a dashboard doesn't mutate the
// snapshot's underlying slice).
func TestRichStatusShape_AuthStateInitial(t *testing.T) {
	sess := NewSessionState("")
	expires := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	auth := NewAuthState(
		AuthMethodGoogle,
		"sess-abc",
		"alice@example.com",
		[]string{"read", "write"},
		expires,
	)
	d, err := NewDispatcher(Config{
		Session:    sess,
		AuthState:  auth,
		DaemonMeta: &DaemonMeta{Transport: "unix", Mode: "in-process"},
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	snap := decodeRichShape(t, d)
	a, ok := snap["auth"].(map[string]any)
	if !ok {
		t.Fatalf("rich.auth missing: %v", snap["auth"])
	}
	if a["method"] != "google" {
		t.Errorf("auth.method = %v, want google", a["method"])
	}
	if a["session_id"] != "sess-abc" {
		t.Errorf("auth.session_id = %v", a["session_id"])
	}
	if a["user_id"] != "alice@example.com" {
		t.Errorf("auth.user_id = %v", a["user_id"])
	}
	scopes := asAnySlice(t, a["scopes"])
	if len(scopes) != 2 {
		t.Errorf("auth.scopes = %v, want 2 entries", a["scopes"])
	} else if scopes[0] != "read" || scopes[1] != "write" {
		t.Errorf("auth.scopes = %v, want [read, write]", scopes)
	}
	gotExp, _ := a["expires_at"].(string)
	if gotExp == "" {
		t.Errorf("auth.expires_at = %v, want RFC3339 string", a["expires_at"])
	} else if !strings.Contains(gotExp, "2026-11-01T12:00:00Z") {
		t.Errorf("auth.expires_at = %v, want 2026-11-01T12:00:00Z prefix", gotExp)
	}
}

// TestRichStatusShape_NilAuthStateEmpty covers the dispatcher
// fabricating a zero AuthSnapshot when no AuthState was
// installed. The auth block falls back to method="none", empty
// fields, scopes=[] (not null), expires_at=null. The cascade /
// daemon / pid-style fields round-trip regardless.
func TestRichStatusShape_NilAuthStateEmpty(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	snap := decodeRichShape(t, d)
	a, _ := snap["auth"].(map[string]any)
	if a["method"] != AuthMethodNone {
		t.Errorf("auth.method = %v, want %q", a["method"], AuthMethodNone)
	}
	if got := a["session_id"]; got != "" {
		t.Errorf("auth.session_id = %v, want empty", got)
	}
	scopes := asAnySlice(t, a["scopes"])
	if len(scopes) != 0 {
		t.Errorf("auth.scopes has %d entries, want 0", len(scopes))
	}
}

// TestRemoteAgentErrorStatus_Empty covers the bounded-error-ring
// read path for a freshly-started cascade with no recorded
// errors. The response is a JSON object with errors=[]
// (not null), even though no recordError call has happened.
func TestRemoteAgentErrorStatus_Empty(t *testing.T) {
	d := newStatusDispatcher(t)

	cid, _ := startDispatcherCascade(t, d)

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"RemoteAgentErrorStatus","params":{"cascade_id":"` + cid + `"}}`)
	resp := d.Dispatch(context.Background(), req)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("RemoteAgentErrorStatus: no result, got %#v", resp)
	}
	if res["cascade_id"] != cid {
		t.Errorf("result.cascade_id = %v, want %q", res["cascade_id"], cid)
	}
	errs := asAnySlice(t, res["errors"])
	if len(errs) != 0 {
		t.Errorf("result.errors has %d entries, want 0", len(errs))
	}
}

// TestRemoteAgentErrorStatus_AfterError exercises the
// recordError → ErrorsSnapshot round-trip. A cascade whose
// backend returned an error must surface exactly the recorded
// entry on a subsequent RemoteAgentErrorStatus call, with
// code / method / message preserved verbatim.
func TestRemoteAgentErrorStatus_AfterError(t *testing.T) {
	d := newStatusDispatcher(t)

	cid, c := startDispatcherCascade(t, d)
	c.recordError("SendRemoteAgentCascadeMessage", errors.New("backend tripped"), -32603, time.Now().UTC())

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"RemoteAgentErrorStatus","params":{"cascade_id":"` + cid + `"}}`)
	resp := d.Dispatch(context.Background(), req)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("RemoteAgentErrorStatus: no result, got %#v", resp)
	}
	errs := asAnySlice(t, res["errors"])
	if len(errs) != 1 {
		t.Fatalf("result.errors has %d entries, want 1", len(errs))
	}
	entry, _ := errs[0].(map[string]any)
	if entry["method"] != "SendRemoteAgentCascadeMessage" {
		t.Errorf("entry.method = %v", entry["method"])
	}
	if entry["message"] != "backend tripped" {
		t.Errorf("entry.message = %v", entry["message"])
	}
	// The dispatcher returns []CascadeError as a typed
	// slice in-process, so entry["code"] is int — wire
	// tests see float64. Accept either so the assertion
	// works at both layers.
	switch v := entry["code"].(type) {
	case int:
		if v != -32603 {
			t.Errorf("entry.code = %d, want -32603", v)
		}
	case int64:
		if int(v) != -32603 {
			t.Errorf("entry.code = %d, want -32603", v)
		}
	case float64:
		if int(v) != -32603 {
			t.Errorf("entry.code = %v, want -32603", v)
		}
	default:
		t.Errorf("entry.code = %T (%v), want int or float64", v, v)
	}
	// `at` is time.Time in-process (the handler returns
	// []CascadeError as a typed slice); on the wire it is
	// an RFC3339Nano string after a round-trip. Accept
	// either so the in-process test path keeps simple
	// assertions.
	switch v := entry["at"].(type) {
	case time.Time:
		if v.IsZero() {
			t.Errorf("entry.at = zero time, want non-zero")
		}
	case string:
		if v == "" {
			t.Errorf("entry.at = empty string, want RFC3339Nano")
		}
	default:
		t.Errorf("entry.at = %T (%v), want time.Time or string", v, v)
	}
}

// TestRemoteAgentErrorStatus_UnknownCascade pins the brief's
// -32004 envelope path: an unknown cascade_id must not panic
// or yield 0-entries; the JSON-RPC error.code is the
// ErrCodeCascadeNotFound constant the dashboards filter on.
func TestRemoteAgentErrorStatus_UnknownCascade(t *testing.T) {
	d := newStatusDispatcher(t)

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"RemoteAgentErrorStatus","params":{"cascade_id":"cas_doesnotexist"}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("unknown cascade: no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d", got, ErrCodeCascadeNotFound)
	}
}

// TestRemoteAgentErrorStatus_NoCascadeID pins the brief's
// -32602 envelope path: absent cascade_id is Invalid params.
func TestRemoteAgentErrorStatus_NoCascadeID(t *testing.T) {
	d := newStatusDispatcher(t)

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"RemoteAgentErrorStatus","params":{}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no-cascade-id: no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
}

// TestEnsureRemoteAgentACEVM_Removed pins the cycle rc-ace-real
// contract: the dispatcher no longer registers a method named
// "EnsureRemoteAgentACEVM", so any caller that posts it gets the
// JSON-RPC 2.0 standard envelope -32601 "Method not found" with
// the method name in `data`. This replaces the rc-status-tool
// parity-stub test (TestEnsureRemoteAgentACE_Stub) which asserted
// a canned {ensured,version,capabilities} envelope; the cycle
// rationale is in docs/cycles/rc-ace-real/deliverable.md §
// "Option B chosen — why a local probe is not honest".
//
// In-process: the dispatcher's error envelope carries code as
// int (the wire JSON round-trip decodes it as float64; the
// shared errorCode helper accepts both, defined in
// cascade_test.go:1596).
func TestEnsureRemoteAgentACEVM_Removed(t *testing.T) {
	d := newStatusDispatcher(t)

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"EnsureRemoteAgentACEVM","params":{}}`)
	resp := d.Dispatch(context.Background(), req)
	if _, hasResult := resp["result"]; hasResult {
		t.Fatalf("EnsureRemoteAgentACEVM: still has result envelope, want -32601; got %#v", resp)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("EnsureRemoteAgentACEVM: no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32601 {
		t.Errorf("error.code = %d, want -32601", got)
	}
	if msg, _ := errObj["message"].(string); msg != "Method not found" {
		t.Errorf("error.message = %v, want \"Method not found\"", msg)
	}
	if data, _ := errObj["data"].(string); data != "EnsureRemoteAgentACEVM" {
		t.Errorf("error.data = %v, want \"EnsureRemoteAgentACEVM\"", data)
	}
}

// TestSetRemoteControlSession_Roundtrip exercises the
// session-binding path: a Set call followed by a Status call
// must surface the new session_id on the wire. The handler
// always fabricates a non-nil AuthState when the dispatcher
// had none installed, so the in-process test path uses
// `d.cfg.AuthState == nil` to also exercise that branch.
func TestSetRemoteControlSession_Roundtrip(t *testing.T) {
	d := newStatusDispatcher(t)

	// Initial state: no AuthState installed. The rich status
	// tool fabricates a zero snapshot.
	pre := decodeRichShape(t, d)
	preAuth, _ := pre["auth"].(map[string]any)
	if preAuth["session_id"] != "" {
		t.Fatalf("baseline auth.session_id = %v, want empty", preAuth["session_id"])
	}
	if preAuth["method"] != AuthMethodNone {
		t.Fatalf("baseline auth.method = %v, want %q", preAuth["method"], AuthMethodNone)
	}

	setReq := []byte(`{
		"jsonrpc":"2.0",
		"id":2,
		"method":"SetRemoteControlSession",
		"params":{"session_id":"sess-fresh","user_id":"bob","scopes":["read","admin"]}
	}`)
	resp := d.Dispatch(context.Background(), setReq)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("SetRemoteControlSession: no result, got %#v", resp)
	}
	if res["session_id"] != "sess-fresh" {
		t.Errorf("Set echo session_id = %v", res["session_id"])
	}
	if res["user_id"] != "bob" {
		t.Errorf("Set echo user_id = %v", res["user_id"])
	}

	// Follow-up status: the dispatched AuthState now
	// reflects the Set call. The rich snapshot reads
	// method="google" because Set always sets it (matches
	// the handler's `AuthMethodGoogle` constant).
	post := decodeRichShape(t, d)
	postAuth, _ := post["auth"].(map[string]any)
	if postAuth["method"] != AuthMethodGoogle {
		t.Errorf("post auth.method = %v, want %q", postAuth["method"], AuthMethodGoogle)
	}
	if postAuth["session_id"] != "sess-fresh" {
		t.Errorf("post auth.session_id = %v", postAuth["session_id"])
	}
	if postAuth["user_id"] != "bob" {
		t.Errorf("post auth.user_id = %v", postAuth["user_id"])
	}
	postScopes := asAnySlice(t, postAuth["scopes"])
	if len(postScopes) != 2 {
		t.Fatalf("post auth.scopes = %v, want 2 entries", postAuth["scopes"])
	}
}

// TestSetRemoteControlSession_NoSessionID pins the brief's
// -32602 envelope path: a Set call without session_id is
// rejected.
func TestSetRemoteControlSession_NoSessionID(t *testing.T) {
	d := newStatusDispatcher(t)

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"SetRemoteControlSession","params":{}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no-session-id: no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
}

// TestSetRemoteControlSession_BadExpiresAt pins the brief's
// -32602 envelope path for an unparseable expires_at: a Set
// call with an RFC3339-violating expires_at must be rejected
// rather than silently coerced.
func TestSetRemoteControlSession_BadExpiresAt(t *testing.T) {
	d := newStatusDispatcher(t)

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"SetRemoteControlSession","params":{"session_id":"s","expires_at":"not-rfc3339"}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("bad-expires-at: no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
}

// TestRemoteControlVersion covers the dedicated version probe:
// when Config.Commit / Config.BuildLabel are set the handler
// surfaces them verbatim; when empty, the response is the
// "unknown" sentinel. Session.Version wins over the constant
// when populated, so a dashboard's "what's running" query sees
// the host's resolved CLI version, not the dispatcher's
// static-server version.
func TestRemoteControlVersion(t *testing.T) {
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:    sess,
		Commit:     "deadbeef1234567890abcdef1234567890abcdef",
		BuildLabel: "release",
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	resp := d.Dispatch(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"remoteControlVersion"}`))
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("remoteControlVersion: no result, got %#v", resp)
	}
	if res["version"] != "1.4.0" {
		t.Errorf("result.version = %v, want 1.4.0 (Session.Version wins)", res["version"])
	}
	if res["build_label"] != "release" {
		t.Errorf("result.build_label = %v, want release", res["build_label"])
	}
	if res["daemon_sha"] != "deadbeef1234567890abcdef1234567890abcdef" {
		t.Errorf("result.daemon_sha = %v", res["daemon_sha"])
	}
}

// TestRemoteControlVersion_Unknown covers the empty-Config
// path: when Commit / BuildLabel are zero the handler emits
// "unknown" so a dashboard's renderer never has to nil-check.
func TestRemoteControlVersion_Unknown(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	resp := d.Dispatch(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"remoteControlVersion"}`))
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("remoteControlVersion: no result, got %#v", resp)
	}
	if res["version"] != ServerVersion {
		t.Errorf("empty-Config: version = %v, want %q (ServerVersion fallback)", res["version"], ServerVersion)
	}
	if res["build_label"] != "unknown" {
		t.Errorf("build_label = %v, want unknown", res["build_label"])
	}
	if res["daemon_sha"] != "unknown" {
		t.Errorf("daemon_sha = %v, want unknown", res["daemon_sha"])
	}
}

// startDispatcherCascade is a tiny test helper that registers
// a fresh cascade on the dispatcher's registry so the error-
// ring tests have something to look up. The dispatcher is the
// same one newTestDaemon builds; the cascade comes from
// d.cfg.Session-cascades() which is built with the same
// CascadeRegistry that NewDispatcher wired.
//
// Returns (cascade_id, *Cascade) so callers can poke the
// per-cascade state (e.g. recordError) directly without going
// through a backend.
func startDispatcherCascade(t *testing.T, d *Dispatcher) (string, *Cascade) {
	t.Helper()
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"StartRemoteAgentCascade","params":{"clientInfo":{"name":"status-test","version":"0"}}}`)
	resp := d.Dispatch(context.Background(), req)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Start: no result, got %#v", resp)
	}
	cid, _ := res["cascade_id"].(string)
	if cid == "" {
		t.Fatalf("Start: cascade_id empty")
	}
	c, ok := d.Cascades().Get(cid)
	if !ok || c == nil {
		t.Fatalf("registry.Get(%q) returned (%v, %v)", cid, c, ok)
	}
	return cid, c
}
