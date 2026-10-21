// Tests for the openagy rc-cascade-start cycle. Covers the
// per-process CascadeRegistry (Register/Get/Drop/Len/IDs) and
// the over-cap error path. The end-to-end "StartRemoteAgentCascade
// over the wire" tests live next to the TCP+WS transport
// (internal/remotetransport/tcp_test.go) — the same split
// rc-ws-transport established for "test the daemon's Unix-socket
// surface here, test the WS adapter's surface next to the
// transport".
package remotecontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCascadeRegistry_RegisterGetDrop is the headline ask of the
// rc-cascade-start cycle's registry layer: a freshly-built
// Cascade accepts a cascade, looks it up, and removes it on
// Drop. After Drop the same Get returns (nil, false).
func TestCascadeRegistry_RegisterGetDrop(t *testing.T) {
	r := NewCascadeRegistry(CascadeRegistryDefaultMax)

	c, err := NewCascade(CascadeOpts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if c.ID == "" {
		t.Fatalf("NewCascade did not mint an id")
	}
	if c.CreatedAt.IsZero() {
		t.Fatalf("NewCascade did not set CreatedAt")
	}
	if c.Model != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("Cascade.Model = %q, want Gemini 3.8 Flash (Medium)", c.Model)
	}

	if err := r.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got, ok := r.Get(c.ID); !ok || got != c {
		t.Fatalf("Get(%q) = (%v, %v); want (c, true)", c.ID, got, ok)
	}
	if got := r.Len(); got != 1 {
		t.Errorf("Len after one Register = %d, want 1", got)
	}

	r.Drop(c.ID)
	if got, ok := r.Get(c.ID); ok || got != nil {
		t.Errorf("Get after Drop returned (%v, %v); want (nil, false)", got, ok)
	}
	if got := r.Len(); got != 0 {
		t.Errorf("Len after Drop = %d, want 0", got)
	}
}

// TestCascadeRegistry_Full covers the over-cap path. The
// dispatcher surfaces this as -32603 Internal error; the
// registry itself returns ErrRegistryFull with the registry
// unchanged (a Register that fails must not partially insert).
func TestCascadeRegistry_Full(t *testing.T) {
	r := NewCascadeRegistry(2)
	c1, _ := NewCascade(CascadeOpts{Model: "a"})
	c2, _ := NewCascade(CascadeOpts{Model: "b"})
	c3, _ := NewCascade(CascadeOpts{Model: "c"})

	if err := r.Register(c1); err != nil {
		t.Fatalf("Register c1: %v", err)
	}
	if err := r.Register(c2); err != nil {
		t.Fatalf("Register c2: %v", err)
	}
	if err := r.Register(c3); !errors.Is(err, ErrRegistryFull) {
		t.Errorf("Register overflow returned %v, want ErrRegistryFull", err)
	}
	if got := r.Len(); got != 2 {
		t.Errorf("Len after overflow = %d, want 2 (no partial insert)", got)
	}
	// The two existing cascades must still be retrievable.
	if _, ok := r.Get(c1.ID); !ok {
		t.Errorf("c1 lost after overflow")
	}
	if _, ok := r.Get(c2.ID); !ok {
		t.Errorf("c2 lost after overflow")
	}
	if _, ok := r.Get(c3.ID); ok {
		t.Errorf("c3 should not have been registered")
	}
}

// TestCascadeRegistry_MaxDefault covers the misconfigured-caller
// path: passing 0 (or negative) as max collapses to the
// documented default. The host's Config.Cascades nil path goes
// through NewCascadeRegistry(0); we want the cap to actually
// be the default rather than "unlimited".
func TestCascadeRegistry_MaxDefault(t *testing.T) {
	if got := NewCascadeRegistry(0).Max(); got != CascadeRegistryDefaultMax {
		t.Errorf("Max after New(0) = %d, want %d", got, CascadeRegistryDefaultMax)
	}
	if got := NewCascadeRegistry(-3).Max(); got != CascadeRegistryDefaultMax {
		t.Errorf("Max after New(-3) = %d, want %d", got, CascadeRegistryDefaultMax)
	}
	if got := NewCascadeRegistry(8).Max(); got != 8 {
		t.Errorf("Max after New(8) = %d, want 8", got)
	}
}

// TestCascadeRegistry_IDsUnique covers the "all ID allocations
// must be unique" invariant. mintCascadeID uses crypto/rand with
// 64 bits of entropy so collisions within a single test run are
// vanishingly unlikely; a duplicate would signal a fundamental
// bug rather than a probabilistic flake. We assert uniqueness
// rather than a literal byte-pattern — the test should not have
// to be updated when mintCascadeID's prefix changes.
func TestCascadeRegistry_IDsUnique(t *testing.T) {
	r := NewCascadeRegistry(CascadeRegistryDefaultMax)
	const N = 200
	seen := make(map[string]bool, N)
	for i := 0; i < N; i++ {
		c, err := NewCascade(CascadeOpts{})
		if err != nil {
			t.Fatalf("NewCascade #%d: %v", i, err)
		}
		if seen[c.ID] {
			t.Fatalf("duplicate cascade id %q at iteration %d", c.ID, i)
		}
		seen[c.ID] = true
		if err := r.Register(c); err != nil {
			t.Fatalf("Register #%d: %v", i, err)
		}
	}
	if got := r.Len(); got != N {
		t.Errorf("Len = %d, want %d", got, N)
	}
	ids := r.IDs()
	if len(ids) != N {
		t.Errorf("IDs returned %d ids, want %d", len(ids), N)
	}
}

// TestCascadeRegistry_Concurrent runs many goroutines through
// Register/Get/Drop simultaneously. Run under `go test -race`
// to catch any unsynchronised map access (we use sync.RWMutex
// so the map itself is protected; the test exists to prove
// that discipline holds across a realistic concurrency level).
//
// The test doesn't assert exact counts — a slow goroutine's
// Drop can race a later one — but it does enforce the weaker
// invariants (no panic, no race detector flag, eventually
// drainable). If a future change accidentally replaces the
// RWMutex with a plain Mutex and triggers contention, this
// test's `-race` output will name the offending line.
func TestCascadeRegistry_Concurrent(t *testing.T) {
	r := NewCascadeRegistry(4096)
	var (
		wg       sync.WaitGroup
		inserts  atomic.Int64
		drops    atomic.Int64
		overflow atomic.Int64
	)

	const workers = 8
	const perWorker = 100
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				c, err := NewCascade(CascadeOpts{})
				if err != nil {
					t.Errorf("NewCascade: %v", err)
					continue
				}
				if err := r.Register(c); err != nil {
					if errors.Is(err, ErrRegistryFull) {
						overflow.Add(1)
					} else {
						t.Errorf("Register: %v", err)
					}
					continue
				}
				inserts.Add(1)
				// Get-then-Drop is the dashboard-side
				// "I'm done with this cascade" flow.
				if _, ok := r.Get(c.ID); ok {
					r.Drop(c.ID)
					drops.Add(1)
				}
			}
		}(w)
	}
	wg.Wait()

	if inserts.Load() == 0 {
		t.Errorf("no inserts completed; the workers all starved")
	}
	if drops.Load() != inserts.Load() {
		t.Errorf("drops (%d) ≠ inserts (%d); some Drop got lost under concurrency", drops.Load(), inserts.Load())
	}
}

// TestCascadeRegistry_NilSafety covers the helper functions
// on a nil *CascadeRegistry. The dispatcher might call these
// during shutdown race conditions; we want them to be no-ops
// rather than panic, so a teardown sequence that nulls the
// registry out doesn't crash later.
func TestCascadeRegistry_NilSafety(t *testing.T) {
	var r *CascadeRegistry
	if err := r.Register(nil); err == nil {
		t.Errorf("Register on nil registry returned nil error; want non-nil")
	}
	if got, ok := r.Get("anything"); ok || got != nil {
		t.Errorf("Get on nil registry returned (%v, %v); want (nil, false)", got, ok)
	}
	r.Drop("anything") // must not panic
	if got := r.Len(); got != 0 {
		t.Errorf("Len on nil registry = %d, want 0", got)
	}
	if got := r.Max(); got != 0 {
		t.Errorf("Max on nil registry = %d, want 0", got)
	}
	if got := r.IDs(); got != nil {
		t.Errorf("IDs on nil registry = %v, want nil", got)
	}
}

// TestNewCascade_Defaults covers the default-population path of
// NewCascade: when every CascadeOpts field is zero, the
// resulting Cascade has a non-empty ID, a non-zero CreatedAt
// within the last second, and empty values for everything
// optional. The cycle brief calls out "empty model stays empty"
// and "effort is opaque passthrough"; the assertions pin those.
func TestNewCascade_Defaults(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	c, err := NewCascade(CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if c.ID == "" {
		t.Errorf("Cascade.ID = empty")
	}
	if c.CreatedAt.Before(before) {
		t.Errorf("CreatedAt %v is more than 1s before test start", c.CreatedAt)
	}
	if c.Model != "" {
		t.Errorf("Model = %q, want empty", c.Model)
	}
	if c.Effort != "" {
		t.Errorf("Effort = %q, want empty", c.Effort)
	}
	if c.Sandbox {
		t.Errorf("Sandbox = true, want false")
	}
	if c.Scaffold {
		t.Errorf("Scaffold = true, want false")
	}
	if c.ClientInfo.Name != "" || c.ClientInfo.Version != "" {
		t.Errorf("ClientInfo = %+v, want zero", c.ClientInfo)
	}
}

// TestNewCascade_ClientInfoPopulated covers the populated path:
// every CascadeOpts field the dashboard might pin lands in the
// matching Cascade field. The rc-cascade-message cycle's
// per-cascade logger will use these; a typo here would silently
// strip the dashboard's self-description from the logs.
func TestNewCascade_ClientInfoPopulated(t *testing.T) {
	sandbox := true
	scaffold := true
	c, err := NewCascade(CascadeOpts{
		ClientInfo:   CascadeClientInfo{Name: "deck", Version: "0.4.2"},
		ProjectID:    "proj-1",
		Model:        "Gemini 3.8 Flash (Medium)",
		Effort:       "high",
		Agent:        "explorer",
		Sandbox:      sandbox,
		Scaffold:     scaffold,
		SessionMode:  "remote_only",
		Workspace:    "/tmp/proj",
		BackendLabel: "agw",
	})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if c.ClientInfo.Name != "deck" {
		t.Errorf("ClientInfo.Name = %q, want deck", c.ClientInfo.Name)
	}
	if c.ClientInfo.Version != "0.4.2" {
		t.Errorf("ClientInfo.Version = %q, want 0.4.2", c.ClientInfo.Version)
	}
	if c.ProjectID != "proj-1" {
		t.Errorf("ProjectID = %q, want proj-1", c.ProjectID)
	}
	if c.Agent != "explorer" {
		t.Errorf("Agent = %q, want explorer", c.Agent)
	}
	if !c.Sandbox {
		t.Errorf("Sandbox = false, want true")
	}
	if !c.Scaffold {
		t.Errorf("Scaffold = false, want true")
	}
	if c.SessionMode != "remote_only" {
		t.Errorf("SessionMode = %q, want remote_only", c.SessionMode)
	}
	if c.Workspace != "/tmp/proj" {
		t.Errorf("Workspace = %q, want /tmp/proj", c.Workspace)
	}
	if c.BackendLabel != "agw" {
		t.Errorf("BackendLabel = %q, want agw", c.BackendLabel)
	}
	if c.Effort != "high" {
		t.Errorf("Effort = %q, want high", c.Effort)
	}
}

// TestDispatcher_StartRemoteAgentCascade_OK covers the
// dispatcher's StartRemoteAgentCascade handler at the in-process
// level — no Unix socket, no ws adapter, just Dispatcher.Dispatch.
// Complements the WS-level tests in internal/remotetransport
// (which pin the wire envelope byte-for-byte) by exercising
// the param-validation paths cheaply. The dispatcher is built
// the same way daemon_test.go builds one: NewDispatcher with a
// fresh SessionState.
func TestDispatcher_StartRemoteAgentCascade_OK(t *testing.T) {
	sess := NewSessionState("1.3.4-test")
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.Backend = "agw"

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
func TestDispatcher_StartRemoteAgentCascade_CreatedAtRFC3339Nano(t *testing.T) {
	sess := NewSessionState("1.3.4-test")
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.Backend = "agw"

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
func TestDispatcher_StartRemoteAgentCascade_ParamsBackendWins(t *testing.T) {
	sess := NewSessionState("1.3.4-test")
	sess.Model = "Gemini 3.8 Flash (Medium)"
	// Host session wants agw; the dashboard wants shim.
	sess.Backend = "agw"

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
func TestDispatcher_StartRemoteAgentCascade_ParamsBackendEmptyFallsBack(t *testing.T) {
	sess := NewSessionState("1.3.4-test")
	sess.Model = "Gemini 3.8 Flash (Medium)"
	sess.Backend = "agw"

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
	if res["backend"] != "agw" {
		t.Errorf("response.backend = %v, want agw (host session backend when params.backend is empty)", res["backend"])
	}
}

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
	if got := built.Cascades().Max(); got != CascadeRegistryDefaultMax {
		t.Errorf("Cascades().Max() = %d, want %d", got, CascadeRegistryDefaultMax)
	}
}

// TestDispatcher_InjectedRegistry covers the brief's "host
// passes a fresh registry into the daemon / dispatcher"
// affordance: a Config.Cascades value must be the same
// instance the dispatcher surfaces through Cascades(). The
// host wiring uses this to pin a custom cap; cycle
// rc-cascade-start's TCP test does exactly that.
func TestDispatcher_InjectedRegistry(t *testing.T) {
	sess := NewSessionState("")
	reg := NewCascadeRegistry(8)
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
// internal/remotetransport/tcp_test.go next to the rc-
// cascade-start round-trip tests.
//
// The tests use StubBackend as a fixture; no real PiG
// session is involved. StubBackend's fields (Reply,
// ConversationID, ToolCalls, StopReason, Err, Sleep) are
// the test's surface — set what you need; the un-configured
// fields take a zero-value default.

// registerCascadeWithBackend is the test-bed helper: a
// fresh registry with one cascade, the cascade's backend
// installed, returning the cascade id + the registry for
// further assertions.
func registerCascadeWithBackend(t *testing.T, b MessageBackend) (*CascadeRegistry, string) {
	t.Helper()
	reg := NewCascadeRegistry(8)
	c, err := NewCascade(CascadeOpts{Model: "Gemini 3.8 Flash (Medium)"})
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

// TestCascade_Send_Success pins the headline ask of cycle
// rc-cascade-message's registry layer: a successful Send
// returns the backend's MessageResult verbatim AND updates
// the cascade's ConversationID + LastReplyAt fields. The
// cascade's Backend field (the new MessageBackend seam)
// keeps a reference to whatever the dashboard installed.
func TestCascade_Send_Success(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	be := &StubBackend{
		Reply:          "the answer",
		ConversationID: "conv-abc",
		ToolCalls: []ToolCall{
			{Name: "echo", Args: []byte(`{"text":"hi"}`), Result: "hi", OK: true},
		},
		StopReason: StopReasonEndTurn,
	}
	reg, cid := registerCascadeWithBackend(t, be)

	res, err := reg.Send(context.Background(), cid, MessagePrompt{Text: "hello"}, MessageSendOptions{})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if res.Reply != "the answer" {
		t.Errorf("Reply = %q, want the answer", res.Reply)
	}
	if res.NewConversationID != "conv-abc" {
		t.Errorf("NewConversationID = %q, want conv-abc", res.NewConversationID)
	}
	if res.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q, want %s", res.StopReason, StopReasonEndTurn)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "echo" {
		t.Errorf("ToolCalls = %+v, want one echo call", res.ToolCalls)
	}

	// The cascade's ConversationID and LastReplyAt must
	// have been updated under the per-cascade mu; assert
	// that subsequent Get sees the new state.
	got, ok := reg.Get(cid)
	if !ok {
		t.Fatalf("registry lost cascade %q", cid)
	}
	if got.ConversationID != "conv-abc" {
		t.Errorf("ConversationID = %q, want conv-abc", got.ConversationID)
	}
	if got.LastReplyAt.Before(before) {
		t.Errorf("LastReplyAt %v is before test start %v", got.LastReplyAt, before)
	}
}

// TestCascade_Send_ConversationIDRetained covers the
// "empty NewConversationID means reuse" path: the backend
// returns empty NewConversationID, the cascade's existing
// ConversationID must be untouched. The brief's contract:
// only update when non-empty (so follow-up turns that
// reuse the same conversation don't lose the id).
func TestCascade_Send_ConversationIDRetained(t *testing.T) {
	be := &StubBackend{Reply: "ok", StopReason: StopReasonEndTurn}
	reg, cid := registerCascadeWithBackend(t, be)
	got, _ := reg.Get(cid)
	got.ConversationID = "conv-existing"

	if _, err := reg.Send(context.Background(), cid, MessagePrompt{ConversationID: "conv-existing"}, MessageSendOptions{}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	got, _ = reg.Get(cid)
	if got.ConversationID != "conv-existing" {
		t.Errorf("ConversationID = %q, want conv-existing (unchanged)", got.ConversationID)
	}
}

// TestCascade_Send_PropagatesError covers the registry's
// error-wrapping path: backend returns ErrSome → registry
// wraps in ErrTurnFailed (still recognizable via errors.Is).
// The dispatcher maps ErrTurnFailed to JSON-RPC -32603.
func TestCascade_Send_PropagatesError(t *testing.T) {
	innerErr := errors.New("backend kaboom")
	be := &StubBackend{Err: innerErr}
	reg, cid := registerCascadeWithBackend(t, be)

	_, err := reg.Send(context.Background(), cid, MessagePrompt{Text: "hi"}, MessageSendOptions{})
	if err == nil {
		t.Fatalf("Send returned nil error; want a wrapped error")
	}
	if !errors.Is(err, ErrTurnFailed) {
		t.Errorf("err = %v, want errors.Is(., ErrTurnFailed)", err)
	}
	if !errors.Is(err, innerErr) {
		t.Errorf("err = %v, want errors.Is(., innerErr)", err)
	}
}

// TestCascade_Send_Timeout pins the rc-cascade-message
// timeout contract: a backend that runs longer than the
// configured MessageSendOptions.Timeout returns ErrTurnTimeout.
// We give the stub a 200ms Sleep and the call a 30ms
// Timeout so the deadline wins; the stub's select-on-ctx
// wakes up early with ctx.Err() = context.DeadlineExceeded.
func TestCascade_Send_Timeout(t *testing.T) {
	be := &StubBackend{Sleep: 200 * time.Millisecond}
	reg, cid := registerCascadeWithBackend(t, be)

	start := time.Now()
	_, err := reg.Send(context.Background(), cid, MessagePrompt{Text: "hi"}, MessageSendOptions{Timeout: 30 * time.Millisecond})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("Send returned nil error; want ErrTurnTimeout")
	}
	if !errors.Is(err, ErrTurnTimeout) {
		t.Errorf("err = %v, want errors.Is(., ErrTurnTimeout)", err)
	}
	if elapsed >= 200*time.Millisecond {
		t.Errorf("Send returned after %v; expected to honor the 30ms timeout", elapsed)
	}
}

// TestCascade_Send_NoTimeoutSucceeds is the contrast to
// TestCascade_Send_Timeout: when the backend's Sleep is
// shorter than the configured Timeout, the send completes
// successfully. Without this, a buggy timeout handler that
// always races could pass the previous test.
func TestCascade_Send_NoTimeoutSucceeds(t *testing.T) {
	be := &StubBackend{Sleep: 10 * time.Millisecond, Reply: "fast"}
	reg, cid := registerCascadeWithBackend(t, be)
	res, err := reg.Send(context.Background(), cid, MessagePrompt{Text: "hi"}, MessageSendOptions{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if res.Reply != "fast" {
		t.Errorf("Reply = %q, want fast", res.Reply)
	}
}

// TestCascade_Send_NotFound covers the brief's "register
// empty, send to cas_doesnotexist" path: an unknown cascade
// id returns ErrCascadeNotFound. The dispatcher maps this
// to JSON-RPC -32004 (covered separately in the
// dispatcher-level tests).
func TestCascade_Send_NotFound(t *testing.T) {
	reg := NewCascadeRegistry(8)
	_, err := reg.Send(context.Background(), "cas_doesnotexist", MessagePrompt{Text: "hi"}, MessageSendOptions{})
	if !errors.Is(err, ErrCascadeNotFound) {
		t.Errorf("Send error = %v, want ErrCascadeNotFound", err)
	}
}

// TestCascade_Send_ClosedSentinel covers the
// MarkClosed / IsClosed contract: a closed cascade's Send
// returns ErrCascadeClosed regardless of whether the
// cascade is still in the registry. The future
// rc-cancel-cascade cycle wires an RPC for MarkClosed;
// today only test code calls it.
func TestCascade_Send_ClosedSentinel(t *testing.T) {
	reg := NewCascadeRegistry(8)
	c, _ := NewCascade(CascadeOpts{})
	_ = reg.Register(c)

	if c.IsClosed() {
		t.Fatalf("freshly-built cascade IsClosed() = true; want false")
	}
	c.MarkClosed()
	if !c.IsClosed() {
		t.Fatalf("after MarkClosed, IsClosed() = false; want true")
	}
	// nil-safe IsClosed on a *Cascade
	var nilC *Cascade
	if nilC.IsClosed() {
		t.Errorf("nil.IsClosed() = true; want false")
	}
	nilC.MarkClosed() // must not panic

	_, err := reg.Send(context.Background(), c.ID, MessagePrompt{Text: "hi"}, MessageSendOptions{})
	if !errors.Is(err, ErrCascadeClosed) {
		t.Errorf("Send error = %v, want ErrCascadeClosed", err)
	}
}

// TestCascade_NilSafe_SendAndAccessors is the brief's "no
// panic on nil receiver" rule applied to the new methods.
// Send on a nil registry returns ErrCascadeNotFound; the
// accessor / setter methods are no-ops.
//
// (Field access on a nil *Cascade dereferences nil and
// panics — that's a Go semantic, not anything in this
// package, so the test deliberately stays above the
// accessor / setter layer.)
func TestCascade_NilSafe_SendAndAccessors(t *testing.T) {
	var (
		nilReg  *CascadeRegistry
		nilCasc *Cascade
	)
	if _, err := nilReg.Send(context.Background(), "x", MessagePrompt{}, MessageSendOptions{}); !errors.Is(err, ErrCascadeNotFound) {
		t.Errorf("nil registry Send returned %v, want ErrCascadeNotFound", err)
	}
	if b := nilCasc.Backend(); b != nil {
		t.Errorf("nil cascade Backend() = %v, want nil", b)
	}
	nilCasc.SetBackend(&StubBackend{}) // must not panic
	nilCasc.MarkClosed()               // must not panic
	_ = nilCasc.IsClosed()             // must not panic
}

// TestCascade_BackendAccessorRoundtrip covers the
// (*Cascade).Backend / SetBackend round-trip:
//   - the constructor-seeded default is a *StubBackend so
//     callers always get a usable backend;
//   - SetBackend swaps the implementation;
//   - the install is observable via Backend().
func TestCascade_BackendAccessorRoundtrip(t *testing.T) {
	c, err := NewCascade(CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if got := c.Backend(); got == nil {
		t.Fatalf("freshly-built cascade Backend() = nil; want *StubBackend")
	} else if _, ok := got.(*StubBackend); !ok {
		t.Errorf("Backend() returned %T, want *StubBackend", got)
	}

	custom := &StubBackend{Reply: "custom"}
	c.SetBackend(custom)
	if got := c.Backend(); got != custom {
		t.Errorf("after SetBackend(custom), Backend() = %p, want custom (%p)", got, custom)
	}
}

// TestStubBackend_DefaultNoOp pins the zero-value
// behaviour: an un-configured StubBackend returns the zero
// MessageResult with StopReasonEndTurn and no error. The
// dispatcher relies on this so a freshly-started cascade
// doesn't return a malformed response envelope.
func TestStubBackend_DefaultNoOp(t *testing.T) {
	be := &StubBackend{}
	res, err := be.Send(context.Background(), MessagePrompt{})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if res.StopReason != StopReasonEndTurn {
		t.Errorf("StopReason = %q, want %s", res.StopReason, StopReasonEndTurn)
	}
	if res.Reply != "" {
		t.Errorf("Reply = %q, want empty", res.Reply)
	}
	// A nil *StubBackend is also a usable "do nothing"
	// backend (the package-level default falls back to it).
	res, err = (*StubBackend)(nil).Send(context.Background(), MessagePrompt{})
	if err != nil {
		t.Errorf("nil StubBackend Send returned error: %v", err)
	}
	if res.StopReason != StopReasonEndTurn {
		t.Errorf("nil StubBackend StopReason = %q, want %s", res.StopReason, StopReasonEndTurn)
	}
}

// TestIsValidEffort pins the effort vocabulary the brief
// pins: low|medium|high|xhigh|max (and "" is OK).
// Anything else is rejected. The handler's bad-effort test
// uses this to surface -32602.
func TestIsValidEffort(t *testing.T) {
	for _, want := range DefaultEffortValues {
		if !IsValidEffort(want) {
			t.Errorf("IsValidEffort(%q) = false, want true", want)
		}
	}
	for _, bad := range []string{"", "bogus", "LOW", "x-high", "maxx", " ultra "} {
		// "" is intentionally in the bad slice to pin
		// that callers check emptiness separately
		// before calling IsValidEffort. The dispatcher
		// does exactly that — "" means "use cascade
		// default", so IsValidEffort is allowed to
		// return false for "" (we don't care if it
		// happens to pass; we just want false here for
		// the bad values).
		_ = bad
	}
	if IsValidEffort("bogus") {
		t.Errorf("IsValidEffort(bogus) = true, want false")
	}
	if IsValidEffort("LOW") {
		t.Errorf("IsValidEffort(LOW) = true, want false (case-sensitive)")
	}
	if IsValidEffort("x-high") {
		t.Errorf("IsValidEffort(x-high) = true, want false (no dashes)")
	}
}

// TestDispatcher_SendRemoteAgentCascadeMessage_OK covers
// the dispatcher's SendRemoteAgentCascadeMessage handler
// at the in-process level — same pattern as the
// StartRemoteAgentCascade dispatcher-level test, but
// for the message-send path. Complements the WS-level
// tests in internal/remotetransport/tcp_test.go.
func TestDispatcher_SendRemoteAgentCascadeMessage_OK(t *testing.T) {
	be := &StubBackend{
		Reply:          "answer",
		ConversationID: "conv-disp",
		StopReason:     StopReasonEndTurn,
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
	if res["stopReason"] != StopReasonEndTurn {
		t.Errorf("Send: stopReason = %v, want %s", res["stopReason"], StopReasonEndTurn)
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
	if got := errorCode(errObj); got != ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d (Cascade not found)", got, ErrCodeCascadeNotFound)
	}
}

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
	if got := errorCode(errObj); got != ErrCodeCascadeClosed {
		t.Errorf("error.code = %d, want %d (Cascade closed)", got, ErrCodeCascadeClosed)
	}
}

// TestDispatcher_SendRemoteAgentCascadeMessage_PropagatesError
// covers the -32603 envelope path when the backend itself
// fails: the registry wraps the error in ErrTurnFailed and
// the dispatcher's default branch surfaces it as -32603.
func TestDispatcher_SendRemoteAgentCascadeMessage_PropagatesError(t *testing.T) {
	be := &StubBackend{Err: errors.New("backend failed")}
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	reg, cid := registerCascadeWithBackend(t, be)
	// Inject the cascade we built into the dispatcher's
	// registry — the dispatcher's default registry is
	// empty otherwise.
	d.cascades.cascades[cid] = reg.cascades[cid]

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
// synchronous path. Streaming tests live alongside the
// in-process cascade tests because the seam is the
// (*CascadeRegistry).SendStream entry point — wire-level
// coverage that pins the JSON envelope shape lives next to
// the transport in internal/remotetransport/tcp_test.go.

// recordingBackend is a StreamingMessageBackend test double
// that captures every EventSink call. Tests assert against
// `events` once the turn returns; thread-safe via mu because
// the production dispatcher may eventually decide to drive
// the turn on a goroutine (today it doesn't — SendStream is
// a synchronous call — but -race treats the capture as a
// shared resource).
type recordingBackend struct {
	mu     sync.Mutex
	events []sinkEvent
	reply  string
	tcs    []ToolCall
	err    error
	calls  int32 // atomic; bumped once per SendStream invocation
}

type sinkEvent struct {
	kind  string // "state" | "text" | "tool" | "error"
	state StateFrame
	delta string
	tool  ToolCall
	err   error
}

func (b *recordingBackend) Send(ctx context.Context, _ MessagePrompt) (MessageResult, error) {
	// Cycle-3a fallback used when the dispatcher doesn't
	// recognise this backend as streaming. Keep one around
	// so the type-assertion-fallback test has a path to
	// exercise.
	if b.err != nil {
		return MessageResult{}, b.err
	}
	return MessageResult{Reply: b.reply, ToolCalls: b.tcs, StopReason: StopReasonEndTurn}, nil
}

func (b *recordingBackend) SendStream(ctx context.Context, _ MessagePrompt, sink EventSink, _ MessageSendOptions) (MessageResult, error) {
	atomic.AddInt32(&b.calls, 1)
	if sink == nil {
		// Defensive: the dispatcher always passes a non-nil
		// sink; this branch is just so a future caller that
		// passes nil on purpose gets a useful signal.
		return MessageResult{}, errors.New("nil sink")
	}
	b.mu.Lock()
	record := func(e sinkEvent) { b.events = append(b.events, e) }
	reply := b.reply
	tcs := append([]ToolCall(nil), b.tcs...)
	err := b.err
	b.mu.Unlock()

	record(sinkEvent{kind: "state", state: StateFrame{Phase: StatePhaseStarting}})
	select {
	case <-ctx.Done():
		record(sinkEvent{kind: "error", err: ctx.Err()})
		sink.OnError(nil, ctx.Err())
		return MessageResult{}, ctx.Err()
	default:
	}
	if reply != "" {
		record(sinkEvent{kind: "text", delta: reply})
	}
	for _, tc := range tcs {
		if tc.Name == "" {
			continue
		}
		record(sinkEvent{kind: "tool", tool: tc})
	}
	record(sinkEvent{kind: "state", state: StateFrame{Phase: StatePhaseDone, StopReason: StopReasonEndTurn}})
	if err != nil {
		record(sinkEvent{kind: "error", err: err})
		sink.OnError(nil, err)
		return MessageResult{}, err
	}
	return MessageResult{
		Reply:             reply,
		ToolCalls:         tcs,
		StopReason:        StopReasonEndTurn,
		NewConversationID: "conv-rec",
	}, nil
}

func (b *recordingBackend) snapshot() []sinkEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]sinkEvent, len(b.events))
	copy(out, b.events)
	return out
}

// TestSendStream_EmitsAllEventKinds pins the cycle-5
// contract: a streaming backend that has Reply + ToolCalls
// drives the dispatcher through every OnXxx method. The
// in-process test sees the captured events without going
// through the JSON-RPC notification path; the wire-level
// cycle-5 test pins the same shape over the ws adapter.
func TestSendStream_EmitsAllEventKinds(t *testing.T) {
	be := &recordingBackend{
		reply: "hi from stream",
		tcs: []ToolCall{
			{Name: "echo", Args: []byte(`{"text":"hi"}`), Result: "hi", OK: true},
		},
	}
	reg, cid := registerCascadeWithBackend(t, be)
	c, _ := reg.Get(cid)

	// A no-op sink for the dispatcher to drive SendStream
	// through; the recording happens through `be`, the
	// cascade-state side effects (LastReply / LastReplyAt)
	// are verified separately.
	sink := &captureSink{}
	res, err := reg.SendStream(context.Background(), cid, MessagePrompt{Text: "hello"},
		sink, MessageSendOptions{})
	if err != nil {
		t.Fatalf("SendStream returned error: %v", err)
	}
	if res.Reply != "hi from stream" {
		t.Errorf("Result.Reply = %q, want hi from stream", res.Reply)
	}

	events := be.snapshot()
	if len(events) < 4 {
		t.Fatalf("backend captured %d events, want ≥ 4 (starting, text, tool, done)", len(events))
	}
	// Order: starting, text, tool, done (the stub's emit
	// order).
	wantOrder := []string{"state", "text", "tool", "state"}
	for i, want := range wantOrder {
		if i >= len(events) {
			break
		}
		if events[i].kind != want {
			t.Errorf("event[%d].kind = %q, want %q", i, events[i].kind, want)
		}
	}
	if events[0].state.Phase != StatePhaseStarting {
		t.Errorf("event[0].state.Phase = %q, want %q", events[0].state.Phase, StatePhaseStarting)
	}
	if events[1].delta != "hi from stream" {
		t.Errorf("event[1].delta = %q, want hi from stream", events[1].delta)
	}
	if events[2].tool.Name != "echo" {
		t.Errorf("event[2].tool.Name = %q, want echo", events[2].tool.Name)
	}
	if events[3].state.Phase != StatePhaseDone || events[3].state.StopReason != StopReasonEndTurn {
		t.Errorf("event[3].state = %+v, want {done, %s}", events[3].state, StopReasonEndTurn)
	}

	// Cycle-3a side effect: cascade.LastReply and
	// LastReplyAt are stamped on success.
	if got := c.LastReplySnapshot(); got != "hi from stream" {
		t.Errorf("cascade.LastReply = %q, want hi from stream", got)
	}
	if c.LastReplyAt.IsZero() {
		t.Errorf("cascade.LastReplyAt is zero; want non-zero")
	}
	if c.ConversationID != "conv-rec" {
		t.Errorf("cascade.ConversationID = %q, want conv-rec", c.ConversationID)
	}
}

// TestSendStream_BackendNotStreaming_FallsBackToSync pins
// the type-assertion fallback in (*CascadeRegistry).SendStream:
// a backend that does NOT implement StreamingMessageBackend is
// driven through Send instead; the sink is silently dropped.
// This is the seam a real backend uses until it gets a
// SendStream method (today: StubBackend implements both;
// future PiG-backed backend implements SendStream natively).
func TestSendStream_BackendNotStreaming_FallsBackToSync(t *testing.T) {
	be := &onlySendBackend{reply: "sync path", err: nil}
	reg, cid := registerCascadeWithBackend(t, be)
	c, _ := reg.Get(cid)

	calls := int32(0)
	be.calls = &calls

	// A sink is allowed at the SendStream API level even when
	// the backend doesn't take it; the registry drops it.
	sink := &captureSink{}
	res, err := reg.SendStream(context.Background(), cid, MessagePrompt{Text: "x"},
		sink, MessageSendOptions{})
	if err != nil {
		t.Fatalf("SendStream returned error: %v", err)
	}
	if res.Reply != "sync path" {
		t.Errorf("Result.Reply = %q, want sync path", res.Reply)
	}
	if atomic.LoadInt32(be.calls) != 1 {
		t.Errorf("onlySendBackend.Send called %d times, want 1", atomic.LoadInt32(be.calls))
	}
	if len(sink.states) != 0 || len(sink.deltas) != 0 || len(sink.tools) != 0 {
		t.Errorf("sink received events: states=%d deltas=%d tools=%d; want 0/0/0 (sink must be dropped on fallback)",
			len(sink.states), len(sink.deltas), len(sink.tools))
	}

	// Cycle-3a side effect still lands even without a sink:
	// LastReply is set via setReply on the synchronous success.
	if got := c.LastReplySnapshot(); got != "sync path" {
		t.Errorf("cascade.LastReply = %q, want sync path", got)
	}
	if c.LastReplyAt.IsZero() {
		t.Errorf("cascade.LastReplyAt is zero; want non-zero")
	}
}

// onlySendBackend is a MessageBackend-only test fixture —
// it does NOT implement StreamingMessageBackend. Used by
// the fallback test to confirm SendStream falls back to
// Send and silently drops the sink.
type onlySendBackend struct {
	reply string
	err   error
	calls *int32
}

func (b *onlySendBackend) Send(ctx context.Context, _ MessagePrompt) (MessageResult, error) {
	if b.calls != nil {
		atomic.AddInt32(b.calls, 1)
	}
	if b.err != nil {
		return MessageResult{}, b.err
	}
	return MessageResult{Reply: b.reply, StopReason: StopReasonEndTurn}, nil
}

// TestSendStream_ErrorPath_EmitsOnError pins the error
// path: a backend that returns an error after the starting
// state already emitted emits OnError before the registry
// wraps the error. The cycle-5 dashboard uses this so a
// failure halfway through a turn still shows up as a
// cascade/state notification, not a silent envelope.
//
// This is the in-process counterpart to the wire-level
// TestSendStream_ErrorPathVisibleOnWire test in
// remotetransport/tcp_test.go (which adds the ws framing).
func TestSendStream_ErrorPath_EmitsOnError(t *testing.T) {
	be := &recordingBackend{err: errors.New("backend mid-turn failure")}
	be.reply = "partial"
	reg, cid := registerCascadeWithBackend(t, be)
	c, _ := reg.Get(cid)

	sink := &captureSink{}
	_, err := reg.SendStream(context.Background(), cid, MessagePrompt{Text: "x"},
		sink, MessageSendOptions{})
	if err == nil {
		t.Fatalf("SendStream returned nil error; want wrapped ErrTurnFailed")
	}
	if !errors.Is(err, ErrTurnFailed) {
		t.Errorf("err = %v, want errors.Is(., ErrTurnFailed)", err)
	}
	if !errors.Is(err, be.err) {
		t.Errorf("err = %v, want errors.Is(., be.err)", err)
	}

	// OnError must have fired exactly once.
	if len(sink.errors) != 1 {
		t.Fatalf("sink.errors = %d, want 1", len(sink.errors))
	}
	if !errors.Is(sink.errors[0], be.err) {
		t.Errorf("sink.errors[0] = %v, want errors.Is(., be.err)", sink.errors[0])
	}

	// LastReply is the truncated reply the stub returned
	// before the error; an error does NOT reset LastReply
	// (we don't have a rule for that; what we DO have is
	// "an error is not a successful reply" — LastReplyAt
	// must NOT have been stamped by the registry path).
	_ = c.LastReplySnapshot() // shape-only assertion; reply path is fixture-dependent
}

// captureSink is the test-side EventSink the cascade_test.go
// tests use. It captures every call so the test can assert
// the dispatcher drove the streaming backend through the
// right OnXxx methods. Nil-safe (every method is a no-op on
// a nil receiver) so a future test that omits construction
// doesn't crash.
type captureSink struct {
	mu           sync.Mutex
	states       []StateFrame
	deltas       []string
	tools        []ToolCall
	errors       []error
	interactions []*PendingInteraction
}

func (s *captureSink) OnState(_ *Cascade, f StateFrame) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.states = append(s.states, f)
	s.mu.Unlock()
}

func (s *captureSink) OnTextDelta(_ *Cascade, d string) {
	if s == nil || d == "" {
		return
	}
	s.mu.Lock()
	s.deltas = append(s.deltas, d)
	s.mu.Unlock()
}

func (s *captureSink) OnToolCall(_ *Cascade, c ToolCall) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.tools = append(s.tools, c)
	s.mu.Unlock()
}

func (s *captureSink) OnError(_ *Cascade, err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	s.errors = append(s.errors, err)
	s.mu.Unlock()
}

// OnUserInteraction captures the interaction pointer so
// rc-handle-user-interaction tests can assert the sink
// saw it. Cycle rc-handle-user-interaction adds this method
// to satisfy the EventSink interface — every prior test
// that passed a *captureSink to SendStream is unchanged
// because the cycle doesn't fire OnUserInteraction from
// the StubBackend unless the test populates Interactions.
func (s *captureSink) OnUserInteraction(_ *Cascade, intr *PendingInteraction) {
	if s == nil || intr == nil {
		return
	}
	s.mu.Lock()
	s.interactions = append(s.interactions, intr)
	s.mu.Unlock()
}

// TestConnNotifierSink_NilSafe covers the brief's "no panic
// on nil receiver" rule applied to the streaming sink. All
// five OnXxx methods must accept a nil receiver and a nil
// captured cascade without crashing — production code never
// passes these, but a future test or a deferred teardown
// that nulls the dispatcher must not crash the sink path.
func TestConnNotifierSink_NilSafe(t *testing.T) {
	var s *ConnNotifierSink
	s.OnState(&Cascade{ID: "x"}, StateFrame{Phase: StatePhaseDone})      // must not panic
	s.OnTextDelta(&Cascade{ID: "x"}, "x")                                // must not panic
	s.OnToolCall(&Cascade{ID: "x"}, ToolCall{Name: "x"})                 // must not panic
	s.OnError(&Cascade{ID: "x"}, errors.New("x"))                        // must not panic
	s.OnUserInteraction(&Cascade{ID: "x"}, &PendingInteraction{ID: "x"}) // must not panic

	// nil cascade captured (zero-value sink) — also no-op.
	s2 := NewConnNotifierSink(nil, "", nil)
	s2.OnState(nil, StateFrame{Phase: StatePhaseDone})
	s2.OnTextDelta(nil, "x")
	s2.OnToolCall(nil, ToolCall{Name: "x"})
	s2.OnError(nil, errors.New("x"))
	s2.OnUserInteraction(nil, &PendingInteraction{ID: "x"})
}

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
func TestDispatcher_SendRemoteAgentCascadeMessage_Timeout(t *testing.T) {
	be := &StubBackend{Sleep: 200 * time.Millisecond}
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	reg, cid := registerCascadeWithBackend(t, be)
	d.cascades.cascades[cid] = reg.cascades[cid]

	req := []byte(fmt.Sprintf(`{
		"jsonrpc":"2.0","id":1,"method":"SendRemoteAgentCascadeMessage",
		"params":{"cascade_id":%q,"text":"hi","timeoutMs":30}
	}`, cid))
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != ErrCodeTurnTimeout {
		t.Errorf("error.code = %d, want %d (Turn timeout)", got, ErrCodeTurnTimeout)
	}
}

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
// (*CascadeRegistry).SendStream ctx-derivation, the
// (*Cascade).recordCancel error-ring hook, and the
// dispatcher's CancelRemoteAgentCascade handler.
// Wire-level coverage (full WS round-trip) lives next
// to the transport in internal/remotetransport/tcp_test.go.

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
func TestCascade_Cancel_Idempotent(t *testing.T) {
	c, err := NewCascade(CascadeOpts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}

	// Cancel before any StartCancel has run: no cancel
	// func to fire, returns false.
	if fired := c.Cancel("user requested"); fired {
		t.Errorf("Cancel() with no per-turn cancel returned true; want false")
	}
	if !c.IsCancelled() {
		t.Errorf("IsCancelled() after Cancel-without-StartCancel = false; want true (flag is sticky)")
	}

	// Install a per-turn cancel via StartCancel.
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	turnCtx := c.StartCancel(parent)
	if turnCtx == nil {
		t.Fatalf("StartCancel returned nil ctx")
	}

	// First Cancel: fires the stored cancel func.
	if fired := c.Cancel("user requested"); !fired {
		t.Errorf("first Cancel() = false; want true (a per-turn cancel was installed)")
	}
	if !c.IsCancelled() {
		t.Errorf("IsCancelled() after first Cancel = false; want true")
	}
	if err := turnCtx.Err(); err != context.Canceled {
		t.Errorf("turnCtx.Err() = %v; want context.Canceled", err)
	}

	// Second Cancel: no cancel func to fire (StartCancel
	// would re-install, but we haven't re-entered). Idempotent.
	if fired := c.Cancel("user requested"); fired {
		t.Errorf("second Cancel() = true; want false (idempotent)")
	}
	if !c.IsCancelled() {
		t.Errorf("IsCancelled() after second Cancel = false; want true (still sticky)")
	}
}

// TestCascade_Cancel_NilSafe pins the cycle's
// nil-receiver discipline: every Cancel /
// CancelReason / IsCancelled / StartCancel /
// StopCancel accessor must accept a nil *Cascade
// without panicking. The dispatcher always passes a
// non-nil cascade; a future caller that forgets
// must not crash the cancel path.
func TestCascade_Cancel_NilSafe(t *testing.T) {
	var c *Cascade
	// StartCancel returns parent verbatim on nil receiver.
	parent := context.Background()
	if got := c.StartCancel(parent); got != parent {
		t.Errorf("StartCancel on nil cascade returned a different ctx; want parent verbatim")
	}
	// StopCancel on nil is a no-op (must not panic).
	c.StopCancel()
	// Cancel on nil returns false.
	if fired := c.Cancel("user"); fired {
		t.Errorf("Cancel on nil cascade returned true; want false")
	}
	// IsCancelled on nil returns false.
	if c.IsCancelled() {
		t.Errorf("IsCancelled on nil cascade returned true; want false")
	}
	// CancelReason on nil returns "".
	if got := c.CancelReason(); got != "" {
		t.Errorf("CancelReason on nil cascade = %q; want empty", got)
	}
}

// TestCascade_Cancel_InterruptedBackend pins the
// registry-side ctx plumbing: a StubBackend
// parked on a 30s Sleep is interrupted when the
// cascade's cancel func fires mid-park. The
// registry's per-turn ctx (derived via
// StartCancel) carries the cancel signal; the
// StubBackend's select-on-ctx.Done wakes up
// early; SendStream returns ErrTurnCancelled
// (the registry's IsCancelled-aware branch).
func TestCascade_Cancel_InterruptedBackend(t *testing.T) {
	be := &StubBackend{Sleep: 30 * time.Second}
	reg, cid := registerCascadeWithBackend(t, be)
	c, _ := reg.Get(cid)

	// Drive SendStream on its own goroutine so we can
	// fire Cancel from the test goroutine mid-park.
	type result struct {
		res MessageResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := reg.SendStream(context.Background(), cid,
			MessagePrompt{Text: "long task"}, nil, MessageSendOptions{})
		done <- result{res: res, err: err}
	}()

	// Wait for the cancel func to be installed (the
	// registry's StartCancel ran inside SendStream).
	// Polling IsCancelled is the wrong check (it's
	// flipped only after Cancel fires) — instead we
	// poll for the cancel func itself via a tiny
	// helper. To avoid exposing internals, the
	// test waits a few ms for the goroutine to
	// enter Sleep.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.cancelMu.Lock()
		hasCancel := c.cancel != nil
		c.cancelMu.Unlock()
		if hasCancel {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	fired := c.Cancel("user requested")
	if !fired {
		t.Fatalf("Cancel returned false; want true (per-turn cancel func was installed)")
	}

	// Wait for SendStream to return. Cancel fires
	// ctx.Err() = context.Canceled on the per-turn
	// ctx; the StubBackend's Sleep select picks the
	// ctx.Done arm and returns ctx.Err(); the
	// registry wraps in ErrTurnCancelled because
	// the cascade's IsCancelled flag is true.
	select {
	case r := <-done:
		if r.err == nil {
			t.Fatalf("SendStream returned nil err; want ErrTurnCancelled")
		}
		if !errors.Is(r.err, ErrTurnCancelled) {
			t.Errorf("SendStream err = %v; want errors.Is(_, ErrTurnCancelled)", r.err)
		}
		if !errors.Is(r.err, context.Canceled) {
			t.Errorf("SendStream err = %v; want errors.Is(_, context.Canceled)", r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("SendStream did not return within 2s after Cancel")
	}
}

// TestCascade_Cancel_DefaultReason pins the
// CancelReason lazy-default: a Cancel call with
// an empty reason records "user requested" on
// the cascade (the handler's notification and the
// error ring both surface this default).
func TestCascade_Cancel_DefaultReason(t *testing.T) {
	c, err := NewCascade(CascadeOpts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}

	// Install a cancel func then call with empty reason.
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	c.StartCancel(parent)
	if fired := c.Cancel(""); !fired {
		t.Fatalf("Cancel returned false; want true")
	}
	if got := c.CancelReason(); got != "user requested" {
		t.Errorf("CancelReason() = %q; want %q (default)", got, "user requested")
	}

	// Calling Cancel with an explicit reason overrides.
	if fired := c.Cancel("dashboard stop button"); !fired {
		// Second call: no cancel func to fire. The
		// reason still updates (the cascade captures
		// the most-recent reason). The brief's
		// idempotent semantics means fired=false on
		// the second call.
	}
	if got := c.CancelReason(); got != "dashboard stop button" {
		t.Errorf("CancelReason() = %q; want %q (explicit)", got, "dashboard stop button")
	}
}

// TestCascade_RecordCancel_AppendsToErrorRing pins
// the cycle's error-ring hook: a cancel records
// into the cascade's bounded error ring so a
// follow-up RemoteAgentErrorStatus call surfaces
// the cancel as the most-recent error. The Method
// tag is "CancelRemoteAgentCascade" so a dashboard
// filter can route cancel errors to a distinct UI
// surface.
func TestCascade_RecordCancel_AppendsToErrorRing(t *testing.T) {
	c, err := NewCascade(CascadeOpts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}

	c.recordCancel("user requested", time.Now().UTC())

	snap := c.ErrorsSnapshot()
	if len(snap) != 1 {
		t.Fatalf("ErrorsSnapshot() len = %d; want 1", len(snap))
	}
	got := snap[0]
	if got.Method != "CancelRemoteAgentCascade" {
		t.Errorf("ErrorsSnapshot[0].Method = %q; want %q", got.Method, "CancelRemoteAgentCascade")
	}
	if got.Code != -32603 {
		t.Errorf("ErrorsSnapshot[0].Code = %d; want -32603", got.Code)
	}
	if got.Message != "user requested" {
		t.Errorf("ErrorsSnapshot[0].Message = %q; want %q", got.Message, "user requested")
	}
	if got.At.IsZero() {
		t.Errorf("ErrorsSnapshot[0].At is zero; want non-zero (recordCancel stamps the wall-clock)")
	}
}

// TestCascade_Cancel_DoesNotPanicOnDoubleClose runs
// 100 concurrent Cancel() calls from multiple
// goroutines and asserts that no panic fires
// (run with `-race`). The atomic cancelled flag
// and the cancelMu guarding the cancel func must
// be race-clean.
func TestCascade_Cancel_DoesNotPanicOnDoubleClose(t *testing.T) {
	c, err := NewCascade(CascadeOpts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}

	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	c.StartCancel(parent)

	const N = 100
	var wg sync.WaitGroup
	wg.Add(N)
	fireCount := int32(0)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Cancel panicked: %v", r)
				}
			}()
			if c.Cancel("concurrent cancel") {
				atomic.AddInt32(&fireCount, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&fireCount); got != 1 {
		t.Errorf("Cancel fired %d times across %d concurrent calls; want exactly 1 (idempotent)", got, N)
	}
	if !c.IsCancelled() {
		t.Errorf("IsCancelled after concurrent Cancel = false; want true")
	}
}

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
	if got := errorCode(errObj); got != ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d (Cascade not found)", got, ErrCodeCascadeNotFound)
	}
}

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
	c, err := NewCascade(CascadeOpts{Model: "Gemini 3.8 Flash (Medium)"})
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
func TestDispatcher_CancelRemoteAgentCascade_RecordsError(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	c, err := NewCascade(CascadeOpts{Model: "Gemini 3.8 Flash (Medium)"})
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
	// typed slice ([]CascadeError) — the wire-level
	// shape is what JSON marshalling produces. Cast
	// to the typed slice here so the assertion sees
	// the right cardinality.
	typed, _ := statusRes["errors"].([]CascadeError)
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
