// Tests for the openagy rc-cascade-start cycle. Covers the
// dispatcher's cascade surface: the StartRemoteAgentCascade
// handler, the SendRemoteAgentCascadeMessage handler, the
// CancelRemoteAgentCascade handler and the streaming sink.
//
// The cascade domain's own tests — the registry, the cascade
// value type, the stub backend and the streaming pipeline —
// live in internal/remotecontrol/cascades, next to the code
// they exercise (cycle 379's rule: a test that constructs the
// subject directly lives with the capability that owns it).
//
// The end-to-end "StartRemoteAgentCascade over the wire" tests
// live next to the TCP+WS transport (remotetransport/tcp_test.go)
// — the same split rc-ws-transport established for "test the
// daemon's Unix-socket surface here, test the WS adapter's surface
// next to the transport".
package remotecontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// TestDispatcher_StartRemoteAgentCascade_OK covers the
// dispatcher's StartRemoteAgentCascade handler at the in-process
// level — no Unix socket, no ws adapter, just Dispatcher.Dispatch.
// Complements the WS-level tests in remotetransport
// (which pin the wire envelope byte-for-byte) by exercising
// the param-validation paths cheaply. The dispatcher is built
// the same way daemon_test.go builds one: NewDispatcher with a
// fresh SessionState.
func TestDispatcher_StartRemoteAgentCascade_OK(t *testing.T) {
	sess := NewSessionState("1.3.4-test")
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.Backend = "gateway"

	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{
		"jsonrpc":"2.0",
		"id":1,
		"method":"StartRemoteAgentCascade",
		"params":{"clientInfo":{"name":"unit","version":"0"},"agent":"explorer"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result, got %#v", resp)
	}
	if res["cascade_id"] == "" {
		t.Errorf("cascade_id empty")
	}
	if res["model"] != sess.Model {
		t.Errorf("model = %v, want %q", res["model"], sess.Model)
	}
	if res["backend"] != sess.Backend {
		t.Errorf("backend = %v, want %q", res["backend"], sess.Backend)
	}
	if got := d.Cascades().Len(); got != 1 {
		t.Errorf("Cascades().Len() = %d, want 1", got)
	}
}

// TestDispatcher_StartRemoteAgentCascade_CreatedAtRFC3339Nano
// pins the rc-polish (G15) fix: the response.created_at field
// must be parseable as RFC3339 (Nano). The pre-fix implementation
// at dispatch.go:1234 passed the formatted timestamp string back
// into Format as if it were a layout constant, producing garbage
// like "18186-100-108T180:08:180.78Z". The current implementation
// uses time.RFC3339Nano directly. Pinning the parse here keeps
// the regression from sneaking back in.
//
// The test injects a deterministic CreatedAt via SessionState
// and verifies the response string parses back to the same
// instant (modulo the layout resolution precision).

// TestDispatcher_StartRemoteAgentCascade_CreatedAtRFC3339Nano
// pins the rc-polish (G15) fix: the response.created_at field
// must be parseable as RFC3339 (Nano). The pre-fix implementation
// at dispatch.go:1234 passed the formatted timestamp string back
// into Format as if it were a layout constant, producing garbage
// like "18186-100-108T180:08:180.78Z". The current implementation
// uses time.RFC3339Nano directly. Pinning the parse here keeps
// the regression from sneaking back in.
//
// The test injects a deterministic CreatedAt via SessionState
// and verifies the response string parses back to the same
// instant (modulo the layout resolution precision).
func TestDispatcher_StartRemoteAgentCascade_CreatedAtRFC3339Nano(t *testing.T) {
	sess := NewSessionState("1.3.4-test")
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.Backend = "gateway"

	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{
		"jsonrpc":"2.0",
		"id":1,
		"method":"StartRemoteAgentCascade",
		"params":{"clientInfo":{"name":"unit","version":"0"}}
	}`)
	resp := d.Dispatch(context.Background(), req)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result, got %#v", resp)
	}
	createdAt, _ := res["created_at"].(string)
	if createdAt == "" {
		t.Fatalf("response.created_at empty")
	}
	// Must parse as RFC3339Nano. The pre-fix code's garbage
	// strings would fail this parse with a clear error.
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		t.Errorf("response.created_at = %q is not RFC3339Nano-parseable: %v", createdAt, err)
	}
	// And must round-trip to a recent instant (NewCascade
	// stamps time.Now().UTC() unless opts.Now is set).
	if delta := time.Since(parsed); delta < 0 || delta > 30*time.Second {
		t.Errorf("response.created_at = %q parsed to %v which is %v from now; want within 30s of now", createdAt, parsed, delta)
	}
	// Reject the literal garbage value the pre-fix code emitted —
	// if someone reintroduces the bug and someone else's test
	// mask drops the parse step, this still fails fast.
	if strings.Contains(createdAt, "18186") {
		t.Errorf("response.created_at = %q looks like pre-fix garbage (contains %q)", createdAt, "18186")
	}
}

// TestDispatcher_StartRemoteAgentCascade_ParamsBackendWins
// pins the rc-polish (G16) fix: when the dashboard pins
// `params.backend`, that value must be honored over the host
// session's backend and must round-trip into the cascade's
// BackendLabel. Pre-fix, the params struct had no Backend
// field, so the dashboard's choice was silently dropped.

// TestDispatcher_StartRemoteAgentCascade_ParamsBackendWins
// pins the rc-polish (G16) fix: when the dashboard pins
// `params.backend`, that value must be honored over the host
// session's backend and must round-trip into the cascade's
// BackendLabel. Pre-fix, the params struct had no Backend
// field, so the dashboard's choice was silently dropped.
func TestDispatcher_StartRemoteAgentCascade_ParamsBackendWins(t *testing.T) {
	sess := NewSessionState("1.3.4-test")
	sess.Model = "Gemini 3.8 Flash (Medium)"
	// Host session wants gateway; the dashboard wants shim.
	sess.Backend = "gateway"

	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{
		"jsonrpc":"2.0",
		"id":1,
		"method":"StartRemoteAgentCascade",
		"params":{"clientInfo":{"name":"unit","version":"0"},"backend":"shim"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result, got %#v", resp)
	}
	if res["backend"] != "shim" {
		t.Errorf("response.backend = %v, want shim (params.backend should win)", res["backend"])
	}
	cid, _ := res["cascade_id"].(string)
	if cid == "" {
		t.Fatalf("response.cascade_id empty")
	}
	c, ok := d.Cascades().Get(cid)
	if !ok || c == nil {
		t.Fatalf("cascade %q not in registry", cid)
	}
	if c.BackendLabel != "shim" {
		t.Errorf("cascade.BackendLabel = %q, want shim (params.backend should round-trip into the cascade)", c.BackendLabel)
	}
}

// TestDispatcher_StartRemoteAgentCascade_ParamsBackendEmptyFallsBack
// pins the inverse path of the G16 fix: when the dashboard
// omits `params.backend` (or sends ""), the host session's
// backend is the source of truth. Pre-fix the response always
// echoed "" because no per-cascade override ever existed; the
// cycle-15 verifier's known-gap note ("host session, params.backend
// is not parsed") was on this exact behaviour.

// TestDispatcher_StartRemoteAgentCascade_ParamsBackendEmptyFallsBack
// pins the inverse path of the G16 fix: when the dashboard
// omits `params.backend` (or sends ""), the host session's
// backend is the source of truth. Pre-fix the response always
// echoed "" because no per-cascade override ever existed; the
// cycle-15 verifier's known-gap note ("host session, params.backend
// is not parsed") was on this exact behaviour.
func TestDispatcher_StartRemoteAgentCascade_ParamsBackendEmptyFallsBack(t *testing.T) {
	sess := NewSessionState("1.3.4-test")
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.Backend = "gateway"

	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{
		"jsonrpc":"2.0",
		"id":1,
		"method":"StartRemoteAgentCascade",
		"params":{"clientInfo":{"name":"unit","version":"0"}}
	}`)
	resp := d.Dispatch(context.Background(), req)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result, got %#v", resp)
	}
	if res["backend"] != "gateway" {
		t.Errorf("response.backend = %v, want gateway (host session backend when params.backend is empty)", res["backend"])
	}
}

// TestDispatcher_StartRemoteAgentCascade_InvalidNumericModel
// covers the dispatcher-level shape of the wire-level test in
// remotetransport: a numeric "model" field must yield -32602.
// Goes through Dispatch directly so we lock the behavior
// independently of the ws adapter.

// TestDispatcher_StartRemoteAgentCascade_InvalidNumericModel
// covers the dispatcher-level shape of the wire-level test in
// remotetransport: a numeric "model" field must yield -32602.
// Goes through Dispatch directly so we lock the behavior
// independently of the ws adapter.
func TestDispatcher_StartRemoteAgentCascade_InvalidNumericModel(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"StartRemoteAgentCascade","params":{"model":42}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
	if got := d.Cascades().Len(); got != 0 {
		t.Errorf("Cascades().Len() = %d, want 0 (invalid params must not register)", got)
	}
}

// TestDispatcher_StartRemoteAgentCascade_MalformedJSON covers
// the parse-error path: invalid JSON must yield -32700 and the
// cascade registry must NOT see a partial insert.

// TestDispatcher_StartRemoteAgentCascade_MalformedJSON covers
// the parse-error path: invalid JSON must yield -32700 and the
// cascade registry must NOT see a partial insert.
func TestDispatcher_StartRemoteAgentCascade_MalformedJSON(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	resp := d.Dispatch(context.Background(), []byte(`not-json`))
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32700 {
		t.Errorf("error.code = %d, want -32700", got)
	}
	if got := d.Cascades().Len(); got != 0 {
		t.Errorf("Cascades().Len() = %d, want 0 (parse error must not register)", got)
	}
}

// TestDispatcher_CascadesAccessor covers the public accessor
// the brief asks for. A nil dispatcher must return nil (no
// panic), and a freshly-built dispatcher must return a
// non-nil registry.

// TestDispatcher_CascadesAccessor covers the public accessor
// the brief asks for. A nil dispatcher must return nil (no
// panic), and a freshly-built dispatcher must return a
// non-nil registry.
func TestDispatcher_CascadesAccessor(t *testing.T) {
	var d *Dispatcher
	if got := d.Cascades(); got != nil {
		t.Errorf("nil dispatcher Cascades() = %v, want nil", got)
	}

	sess := NewSessionState("")
	built, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if built.Cascades() == nil {
		t.Errorf("NewDispatcher defaulted to a nil registry")
	}
	if got := built.Cascades().Max(); got != cascades.RegistryDefaultMax {
		t.Errorf("Cascades().Max() = %d, want %d", got, cascades.RegistryDefaultMax)
	}
}

// TestDispatcher_InjectedRegistry covers the brief's "host
// passes a fresh registry into the daemon / dispatcher"
// affordance: a Config.Cascades value must be the same
// instance the dispatcher surfaces through Cascades(). The
// host wiring uses this to pin a custom cap; cycle
// rc-cascade-start's TCP test does exactly that.

// TestDispatcher_InjectedRegistry covers the brief's "host
// passes a fresh registry into the daemon / dispatcher"
// affordance: a Config.Cascades value must be the same
// instance the dispatcher surfaces through Cascades(). The
// host wiring uses this to pin a custom cap; cycle
// rc-cascade-start's TCP test does exactly that.
func TestDispatcher_InjectedRegistry(t *testing.T) {
	sess := NewSessionState("")
	reg := cascades.NewRegistry(8)
	d, err := NewDispatcher(Config{Session: sess, Cascades: reg})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if d.Cascades() != reg {
		t.Errorf("Cascades() did not return the injected registry")
	}
	if got := d.Cascades().Max(); got != 8 {
		t.Errorf("Cascades().Max() = %d, want 8", got)
	}
}

// ----- cycle rc-cascade-message tests -----
//
// The tests below exercise the cascade.Send path (the
// in-process face of SendRemoteAgentCascadeMessage) and the
// dispatcher's envelope mapping. Wire-level (over the WS
// adapter) coverage lives in
// remotetransport/tcp_test.go next to the rc-
// cascade-start round-trip tests.
//
// The tests use StubBackend as a fixture; no real PiG
// session is involved. StubBackend's fields (Reply,
// ConversationID, ToolCalls, StopReason, Err, Sleep) are
// the test's surface — set what you need; the un-configured
// fields take a zero-value default.

// TestDispatcher_SendRemoteAgentCascadeMessage_OK covers
// the dispatcher's SendRemoteAgentCascadeMessage handler
// at the in-process level — same pattern as the
// StartRemoteAgentCascade dispatcher-level test, but
// for the message-send path. Complements the WS-level
// tests in remotetransport/tcp_test.go.
func TestDispatcher_SendRemoteAgentCascadeMessage_OK(t *testing.T) {
	be := &cascades.StubBackend{
		Reply:          "answer",
		ConversationID: "conv-disp",
		StopReason:     cascades.StopReasonEndTurn,
	}
	sess := NewSessionState("1.3.4-test")
	sess.Model = "Gemini 3.8 Flash (Medium)"
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// Start a cascade so the registry holds it. Model is
	// resolved from the host's SessionState here (mirrors
	// the rc-cascade-start test).
	startReq := []byte(`{
		"jsonrpc":"2.0","id":1,"method":"StartRemoteAgentCascade",
		"params":{"clientInfo":{"name":"u","version":"0"}}
	}`)
	startResp := d.Dispatch(context.Background(), startReq)
	startRes, ok := startResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Start: no result, got %#v", startResp)
	}
	cid, _ := startRes["cascade_id"].(string)
	if cid == "" {
		t.Fatalf("Start: cascade_id empty")
	}

	// Wire the backend onto the cascade we just registered.
	regCascade, _ := d.Cascades().Get(cid)
	regCascade.SetBackend(be)

	// Now send the message. Use the cascade_id from Start.
	msgReq := []byte(fmt.Sprintf(`{
		"jsonrpc":"2.0","id":2,"method":"SendRemoteAgentCascadeMessage",
		"params":{"cascade_id":%q,"text":"hello"}
	}`, cid))
	msgResp := d.Dispatch(context.Background(), msgReq)
	if errObj, ok := msgResp["error"]; ok {
		t.Fatalf("Send: error = %#v", errObj)
	}
	res, ok := msgResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Send: no result, got %#v", msgResp)
	}
	if res["reply"] != "answer" {
		t.Errorf("Send: reply = %v, want answer", res["reply"])
	}
	if res["conversationId"] != "conv-disp" {
		t.Errorf("Send: conversationId = %v, want conv-disp", res["conversationId"])
	}
	if res["stopReason"] != cascades.StopReasonEndTurn {
		t.Errorf("Send: stopReason = %v, want %s", res["stopReason"], cascades.StopReasonEndTurn)
	}
	// toolCalls is always a JSON array, not null; the
	// handler explicitly normalises nil to [].
	if res["toolCalls"] == nil {
		t.Errorf("Send: toolCalls = nil; want []")
	}
}

// TestDispatcher_SendRemoteAgentCascadeMessage_NoCascadeID
// covers the "-32602 when cascade_id is missing or empty"
// rule. Both the absent-field and empty-string paths must
// produce the same error envelope.

// TestDispatcher_SendRemoteAgentCascadeMessage_NoCascadeID
// covers the "-32602 when cascade_id is missing or empty"
// rule. Both the absent-field and empty-string paths must
// produce the same error envelope.
func TestDispatcher_SendRemoteAgentCascadeMessage_NoCascadeID(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	cases := map[string]string{
		"missing field": `{"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage","params":{"text":"hi"}}`,
		"empty string":  `{"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage","params":{"cascade_id":"","text":"hi"}}`,
		"whitespace":    `{"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage","params":{"cascade_id":"   ","text":"hi"}}`,
		"empty params":  `{"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage","params":{}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := d.Dispatch(context.Background(), []byte(body))
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("no error envelope, got %#v", resp)
			}
			if got := errorCode(errObj); got != -32602 {
				t.Errorf("error.code = %d, want -32602", got)
			}
		})
	}
}

// TestDispatcher_SendRemoteAgentCascadeMessage_NoText covers
// the matching "text is required" rule.

// TestDispatcher_SendRemoteAgentCascadeMessage_NoText covers
// the matching "text is required" rule.
func TestDispatcher_SendRemoteAgentCascadeMessage_NoText(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage","params":{"cascade_id":"cas_x","text":""}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
}

// TestDispatcher_SendRemoteAgentCascadeMessage_BadEffort
// pins the effort-validation rule. The dispatcher rejects
// any effort value not in DefaultEffortValues with -32602
// before the cascade is even looked up — a buggy dashboard
// gets a clear error rather than silently defaulting to
// the cascade's pinned effort.

// TestDispatcher_SendRemoteAgentCascadeMessage_BadEffort
// pins the effort-validation rule. The dispatcher rejects
// any effort value not in DefaultEffortValues with -32602
// before the cascade is even looked up — a buggy dashboard
// gets a clear error rather than silently defaulting to
// the cascade's pinned effort.
func TestDispatcher_SendRemoteAgentCascadeMessage_BadEffort(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	cases := map[string]string{
		"bogus":     `{"cascade_id":"cas_x","text":"hi","effort":"bogus"}`,
		"x-high":    `{"cascade_id":"cas_x","text":"hi","effort":"x-high"}`,
		"uppercase": `{"cascade_id":"cas_x","text":"hi","effort":"MEDIUM"}`,
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage","params":` + params + `}`
			resp := d.Dispatch(context.Background(), []byte(body))
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("no error envelope, got %#v", resp)
			}
			if got := errorCode(errObj); got != -32602 {
				t.Errorf("error.code = %d, want -32602", got)
			}
		})
	}
}

// TestDispatcher_SendRemoteAgentCascadeMessage_NotFound
// covers the -32004 envelope path: the cascade id is
// well-formed but no cascade is registered under it. The
// dispatcher surfaces this with the brief's custom code.

// TestDispatcher_SendRemoteAgentCascadeMessage_NotFound
// covers the -32004 envelope path: the cascade id is
// well-formed but no cascade is registered under it. The
// dispatcher surfaces this with the brief's custom code.
func TestDispatcher_SendRemoteAgentCascadeMessage_NotFound(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	req := []byte(`{
		"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage",
		"params":{"cascade_id":"cas_doesnotexist","text":"hi"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != rpc.ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d (Cascade not found)", got, rpc.ErrCodeCascadeNotFound)
	}
}

// TestDispatcher_SendRemoteAgentCascadeMessage_Closed
// covers the -32005 envelope path: the cascade is
// registered but has been marked closed. The dispatcher
// surfaces this with the brief's custom code.

// TestDispatcher_SendRemoteAgentCascadeMessage_Closed
// covers the -32005 envelope path: the cascade is
// registered but has been marked closed. The dispatcher
// surfaces this with the brief's custom code.
func TestDispatcher_SendRemoteAgentCascadeMessage_Closed(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// Start a cascade, then mark it closed.
	startReq := []byte(`{"jsonrpc":"2.0","id":1,"method":"StartRemoteAgentCascade","params":{"clientInfo":{"name":"u","version":"0"}}}`)
	startResp := d.Dispatch(context.Background(), startReq)
	startRes, _ := startResp["result"].(map[string]any)
	cid, _ := startRes["cascade_id"].(string)
	if cid == "" {
		t.Fatalf("Start: cascade_id empty")
	}
	regCas, _ := d.Cascades().Get(cid)
	regCas.MarkClosed()

	req := []byte(fmt.Sprintf(`{
		"jsonrpc":"2.0","id":2,"method":"SendRemoteAgentCascadeMessage",
		"params":{"cascade_id":%q,"text":"hi"}
	}`, cid))
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != rpc.ErrCodeCascadeClosed {
		t.Errorf("error.code = %d, want %d (Cascade closed)", got, rpc.ErrCodeCascadeClosed)
	}
}

// TestDispatcher_SendRemoteAgentCascadeMessage_PropagatesError
// covers the -32603 envelope path when the backend itself
// fails: the registry wraps the error in ErrTurnFailed and
// the dispatcher's default branch surfaces it as -32603.

// TestDispatcher_SendRemoteAgentCascadeMessage_PropagatesError
// covers the -32603 envelope path when the backend itself
// fails: the registry wraps the error in ErrTurnFailed and
// the dispatcher's default branch surfaces it as -32603.
func TestDispatcher_SendRemoteAgentCascadeMessage_PropagatesError(t *testing.T) {
	be := &cascades.StubBackend{Err: errors.New("backend failed")}
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	reg, cid := registerCascadeWithBackend(t, be)
	// Inject the cascade we built into the dispatcher's
	// registry — the dispatcher's default registry is
	// empty otherwise.
	if err := d.cascades.Register(registeredCascade(t, reg, cid)); err != nil {
		t.Fatalf("Register on the dispatcher's registry: %v", err)
	}

	req := []byte(fmt.Sprintf(`{
		"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage",
		"params":{"cascade_id":%q,"text":"hi"}
	}`, cid))
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32603 {
		t.Errorf("error.code = %d, want -32603 (Internal error)", got)
	}
}

// ----- cycle rc-cascade-stream tests -----
//
// The tests below exercise the streaming surface the
// rc-cascade-stream cycle adds on top of the cycle-3a
// synchronous path. The streaming seam itself — the
// registry's SendStream entry point and the test doubles that
// drive it — lives in internal/remotecontrol/cascades, next to
// the code it exercises; what stays here is the dispatcher's
// own envelope. Wire-level coverage that pins the JSON envelope
// shape lives next to the transport in remotetransport/tcp_test.go.

// TestDispatcher_SendRemoteAgentCascadeMessage_Timeout
// covers the -32010 envelope path: the backend sleeps
// longer than the configured timeoutMs; the registry maps
// the ctx.DeadlineExceeded to ErrTurnTimeout and the
// dispatcher surfaces it as -32010.
func TestDispatcher_SendRemoteAgentCascadeMessage_Timeout(t *testing.T) {
	be := &cascades.StubBackend{Sleep: 200 * time.Millisecond}
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	reg, cid := registerCascadeWithBackend(t, be)
	if err := d.cascades.Register(registeredCascade(t, reg, cid)); err != nil {
		t.Fatalf("Register on the dispatcher's registry: %v", err)
	}

	req := []byte(fmt.Sprintf(`{
		"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage",
		"params":{"cascade_id":%q,"text":"hi","timeoutMs":30}
	}`, cid))
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != rpc.ErrCodeTurnTimeout {
		t.Errorf("error.code = %d, want %d (Turn timeout)", got, rpc.ErrCodeTurnTimeout)
	}
}

// errorCode reads the "code" field out of a JSON-RPC error
// envelope. The dispatcher in-process API returns it as int;
// wire tests go through a JSON round-trip and see it as
// float64. This helper accepts both so the same assertion
// works at either layer.

// TestConnNotifierSink_NilSafe covers the brief's "no panic
// on nil receiver" rule applied to the streaming sink. All
// five OnXxx methods must accept a nil receiver and a nil
// captured cascade without crashing — production code never
// passes these, but a future test or a deferred teardown
// that nulls the dispatcher must not crash the sink path.
func TestConnNotifierSink_NilSafe(t *testing.T) {
	var s *ConnNotifierSink
	s.OnState(&cascades.Cascade{ID: "x"}, cascades.StateFrame{Phase: cascades.StatePhaseDone}) // must not panic
	s.OnTextDelta(&cascades.Cascade{ID: "x"}, "x")                                             // must not panic
	s.OnToolCall(&cascades.Cascade{ID: "x"}, cascades.ToolCall{Name: "x"})                     // must not panic
	s.OnError(&cascades.Cascade{ID: "x"}, errors.New("x"))                                     // must not panic
	s.OnUserInteraction(&cascades.Cascade{ID: "x"}, &cascades.PendingInteraction{ID: "x"})     // must not panic

	// nil cascade captured (zero-value sink) — also no-op.
	s2 := NewConnNotifierSink(nil, "", nil)
	s2.OnState(nil, cascades.StateFrame{Phase: cascades.StatePhaseDone})
	s2.OnTextDelta(nil, "x")
	s2.OnToolCall(nil, cascades.ToolCall{Name: "x"})
	s2.OnError(nil, errors.New("x"))
	s2.OnUserInteraction(nil, &cascades.PendingInteraction{ID: "x"})
}

// TestDispatcher_WithConnID pins the public interface
// method the remotetransport package uses to thread a
// per-connection connID through the dispatcher's request
// context. The dispatcher is built without a notifier here
// so the test asserts only the WithConnID seam; cycle-5
// wire tests in remotetransport assert the end-to-end
// effect (notifications actually arrive on the right
// connection).

// TestDispatcher_WithConnID pins the public interface
// method the remotetransport package uses to thread a
// per-connection connID through the dispatcher's request
// context. The dispatcher is built without a notifier here
// so the test asserts only the WithConnID seam; cycle-5
// wire tests in remotetransport assert the end-to-end
// effect (notifications actually arrive on the right
// connection).
func TestDispatcher_WithConnID(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ctx := context.Background()
	out := d.WithConnID(ctx, "ws-123")
	if out == ctx {
		t.Errorf("WithConnID returned the input ctx; want a derived ctx")
	}
	if got := connIDFromContext(out); got != "ws-123" {
		t.Errorf("connIDFromContext(WithConnID(ctx, ws-123)) = %q, want ws-123", got)
	}
	if got := connIDFromContext(ctx); got != "" {
		t.Errorf("connIDFromContext(input ctx) = %q, want empty (input ctx has no connID)", got)
	}

	// Nil-dispatcher is also safe — the package documents
	// the transport always passes non-nil, but a defensive
	// nil-check matters when an in-process test driver
	// forgets to inject a dispatcher.
	var nilD *Dispatcher
	if got := nilD.WithConnID(ctx, "ws-zzz"); got == nil {
		t.Errorf("nil dispatcher WithConnID returned nil ctx; want input ctx")
	}
	if got := connIDFromContext(ctx); got != "" {
		t.Errorf("connIDFromContext(orig ctx) mutated to %q", got)
	}
}

// TestDispatcher_SendRemoteAgentCascadeMessage_Timeout
// covers the -32010 envelope path: the backend sleeps
// longer than the configured timeoutMs; the registry maps
// the ctx.DeadlineExceeded to ErrTurnTimeout and the
// dispatcher surfaces it as -32010.

// errorCode reads the "code" field out of a JSON-RPC error
// envelope. The dispatcher in-process API returns it as int;
// wire tests go through a JSON round-trip and see it as
// float64. This helper accepts both so the same assertion
// works at either layer.
func errorCode(env map[string]any) int {
	v, ok := env["code"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// ----- cycle rc-cancel-cascade tests -----
//
// In-process coverage for (*Cascade).Cancel, the
// (*cascades.Registry).SendStream ctx-derivation, the
// (*Cascade).recordCancel error-ring hook, and the
// dispatcher's CancelRemoteAgentCascade handler.
// Wire-level coverage (full WS round-trip) lives next
// to the transport in remotetransport/tcp_test.go.

// TestCascade_Cancel_Idempotent pins the headline
// ask of the rc-cancel-cascade cycle's cascade
// accessor: Cancel() returns true iff it actually
// fired a per-turn cancel func. A second call after
// the first returns false (idempotent).
//
// The test installs a cancel func via StartCancel,
// then calls Cancel twice. First call returns true;
// second returns false. IsCancelled is true after
// both calls; the cascade's cancelled flag is
// sticky.

// TestDispatcher_CancelRemoteAgentCascade_BadParams covers
// the brief's "empty cascade_id" path: -32602 Invalid
// params. The dispatcher surface.
func TestDispatcher_CancelRemoteAgentCascade_BadParams(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// Empty cascade_id
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"CancelRemoteAgentCascade","params":{"cascade_id":""}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (Invalid params)", got)
	}

	// Missing cascade_id entirely
	req2 := []byte(`{"jsonrpc":"2.0","id":1,"method":"CancelRemoteAgentCascade","params":{}}`)
	resp2 := d.Dispatch(context.Background(), req2)
	errObj2, ok := resp2["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope for empty params, got %#v", resp2)
	}
	if got := errorCode(errObj2); got != -32602 {
		t.Errorf("empty-params error.code = %d, want -32602 (Invalid params)", got)
	}
}

// TestDispatcher_CancelRemoteAgentCascade_NotFound
// covers the brief's "unknown cascade" path: -32004
// Cascade not found.

// TestDispatcher_CancelRemoteAgentCascade_NotFound
// covers the brief's "unknown cascade" path: -32004
// Cascade not found.
func TestDispatcher_CancelRemoteAgentCascade_NotFound(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"CancelRemoteAgentCascade","params":{"cascade_id":"cas_does_not_exist"}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != rpc.ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d (Cascade not found)", got, rpc.ErrCodeCascadeNotFound)
	}
}

// TestDispatcher_CancelRemoteAgentCascade_Idempotent
// covers the in-process idempotency path: cancel twice
// on the same dispatcher; the second response carries
// already_cancelled:true.
//
// The test installs a cancel func via the registry's
// StartCancel so the first call fires.

// TestDispatcher_CancelRemoteAgentCascade_Idempotent
// covers the in-process idempotency path: cancel twice
// on the same dispatcher; the second response carries
// already_cancelled:true.
//
// The test installs a cancel func via the registry's
// StartCancel so the first call fires.
func TestDispatcher_CancelRemoteAgentCascade_Idempotent(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// Manually mint and register a cascade so the
	// dispatcher has something to look up.
	c, err := cascades.New(cascades.Opts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := d.cascades.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Install a per-turn cancel via StartCancel so
	// the first Cancel fires.
	c.StartCancel(context.Background())

	req := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"CancelRemoteAgentCascade","params":{"cascade_id":%q}}`, c.ID))
	resp1 := d.Dispatch(context.Background(), req)
	res1, ok := resp1["result"].(map[string]any)
	if !ok {
		t.Fatalf("first response has no result: %#v", resp1)
	}
	if got, _ := res1["ok"].(bool); !got {
		t.Errorf("first ok = %v; want true", res1["ok"])
	}
	if got, _ := res1["already_cancelled"].(bool); got {
		t.Errorf("first already_cancelled = true; want false")
	}
	if _, ok := res1["cancelled_at"].(string); !ok {
		t.Errorf("first cancelled_at missing or non-string: %#v", res1["cancelled_at"])
	}

	// Second cancel: no cancel func to fire (StartCancel
	// was not re-called), so already_cancelled should be
	// true.
	resp2 := d.Dispatch(context.Background(), req)
	res2, ok := resp2["result"].(map[string]any)
	if !ok {
		t.Fatalf("second response has no result: %#v", resp2)
	}
	if got, _ := res2["ok"].(bool); !got {
		t.Errorf("second ok = %v; want true", res2["ok"])
	}
	if got, _ := res2["already_cancelled"].(bool); !got {
		t.Errorf("second already_cancelled = false; want true (idempotent)")
	}
	if _, ok := res2["cancelled_at"].(string); !ok {
		t.Errorf("second cancelled_at missing or non-string: %#v", res2["cancelled_at"])
	}
}

// TestDispatcher_CancelRemoteAgentCascade_RecordsError
// pins the brief's "after cancel,
// RemoteAgentErrorStatus returns the cancel reason
// in the ring" rule: a successful cancel records
// an entry the dashboard's error-status probe can
// surface.

// TestDispatcher_CancelRemoteAgentCascade_RecordsError
// pins the brief's "after cancel,
// RemoteAgentErrorStatus returns the cancel reason
// in the ring" rule: a successful cancel records
// an entry the dashboard's error-status probe can
// surface.
func TestDispatcher_CancelRemoteAgentCascade_RecordsError(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	c, err := cascades.New(cascades.Opts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := d.cascades.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.StartCancel(context.Background())

	// Cancel via the dispatcher.
	req := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"CancelRemoteAgentCascade","params":{"cascade_id":%q}}`, c.ID))
	resp := d.Dispatch(context.Background(), req)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result, got %#v", resp)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("ok = %v; want true", res["ok"])
	}

	// Now probe RemoteAgentErrorStatus on the same
	// cascade. The cancel should be the most-recent
	// entry in the ring.
	statusReq := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"RemoteAgentErrorStatus","params":{"cascade_id":%q}}`, c.ID))
	statusResp := d.Dispatch(context.Background(), statusReq)
	statusRes, ok := statusResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no status result, got %#v", statusResp)
	}
	// The dispatcher's in-process API returns the
	// typed slice ([]cascades.ErrorEntry) — the wire-level
	// shape is what JSON marshalling produces. Cast
	// to the typed slice here so the assertion sees
	// the right cardinality.
	typed, _ := statusRes["errors"].([]cascades.ErrorEntry)
	if len(typed) != 1 {
		t.Fatalf("errors len = %d; want 1 (the cancel); full resp = %#v", len(typed), statusRes)
	}
	got := typed[0]
	if got.Method != "CancelRemoteAgentCascade" {
		t.Errorf("errors[0].Method = %q; want %q", got.Method, "CancelRemoteAgentCascade")
	}
	if got.Message != "user requested" {
		t.Errorf("errors[0].Message = %q; want %q", got.Message, "user requested")
	}
	if got.Code != -32603 {
		t.Errorf("errors[0].Code = %d; want -32603", got.Code)
	}
	if got.At.IsZero() {
		t.Errorf("errors[0].At is zero; want non-zero (recordCancel stamps the wall-clock)")
	}
}

// registerCascadeWithBackend is the test-bed helper: a
// fresh registry with one cascade, the cascade's backend
// installed, returning the cascade id + the registry for
// further assertions.
func registerCascadeWithBackend(t *testing.T, b cascades.Backend) (*cascades.Registry, string) {
	t.Helper()
	reg := cascades.NewRegistry(8)
	c, err := cascades.New(cascades.Opts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if b != nil {
		c.SetBackend(b)
	}
	if err := reg.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return reg, c.ID
}

// registeredCascade returns the cascade a test registered under
// id in reg. The registry's map is the registry's own — a test
// that wants a cascade in the dispatcher's registry registers it
// there instead of reaching into the map.
func registeredCascade(t *testing.T, reg *cascades.Registry, id string) *cascades.Cascade {
	t.Helper()
	c, ok := reg.Get(id)
	if !ok {
		t.Fatalf("registry lost cascade %q", id)
	}
	return c
}
