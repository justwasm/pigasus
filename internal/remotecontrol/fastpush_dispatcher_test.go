// Package remotecontrol — fastpush_dispatcher_test.go: the
// dispatcher's round-trip against the fastpush domain.
//
// The store's own tests live in the domain package
// (internal/remotecontrol/fastpush); this file exercises the
// wire the dispatcher produces — the handler, the envelope and
// the notification, through the dispatcher's own route table.
//
// Wire-level tests live in
// remotetransport/tcp_test.go (mirroring the
// project's split: in-process envelopes vs. wire round-
// trips).
package remotecontrol

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
	"github.com/justwasm/pigasus/internal/remotecontrol/fastpush"
	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// newFastpushDispatcher returns a Dispatcher with the
// supplied fastpush.Store pre-installed. The helper
// mirrors newPluginsDispatcher /
// newTriggersDispatcher.
//
// When ps is nil, NewDispatcher fabricates a fresh
// empty MemStore (the nil-guard pattern
// rc-fastpush-toggle adds). Tests that want to assert
// "no store installed" pass nil and exercise the
// -32603 path; tests that want a specific store pass
// it directly.
//
// The helper also pre-registers a cascade
// "cas_known" in the dispatcher's cascades.Registry so
// happy-path SetEnabled calls land on a real cascade
// rather than the dispatcher's -32004 unknown-
// cascade guard. Tests that exercise the
// -32004 path either pre-clear the registry or pass a
// different (or absent) cascade id.
func newFastpushDispatcher(t *testing.T, ps fastpush.Store) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:       sess,
		FastpushStore: ps,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	// Pre-register a cascade so happy-path
	// SetEnabled calls land on a real cascade
	// rather than the dispatcher's -32004
	// unknown-cascade guard. The cascade is
	// metadata-only; tests don't exercise
	// SendMessage / Cancel paths.
	c, err := cascades.New(cascades.Opts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := d.cascades.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return d
}

// registerFastpushCascade registers a fresh
// cascade in the dispatcher's registry and returns
// its id. Useful for tests that want to exercise
// the SetEnabled happy path against an explicit
// cascade id (rather than the default
// "cas_known" the newFastpushDispatcher
// helper pre-registers).
func registerFastpushCascade(t *testing.T, d *Dispatcher) string {
	t.Helper()
	c, err := cascades.New(cascades.Opts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := d.cascades.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return c.ID
}

// dispatchFastpush sends a SetRemoteAgentFastpushEnabled
// request through the dispatcher and returns the
// (result, error) pair. Mirrors dispatchPlugins /
// dispatchTriggers.
//
// nil params marshals to JSON `null` so the
// dispatcher's "len(params) > 0" guards see a present
// (but null) params block; jsonRPCFor takes a byte
// slice and appends verbatim, so we mirror the
// convention `[]byte("null")` rather than a missing-
// key shape.
func dispatchFastpush(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
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

// ---- dispatcher round-trip tests -------------------------

// TestSetRemoteAgentFastpushEnabled_Ok pins the
// happy-path round-trip through the dispatcher:
// fresh store, post-state reflects the toggle,
// wire envelope carries push_delivery:"deferred" as
// the explicit sentinel.
//
// The test installs a checkCascade closure that
// accepts every cascade_id (mirrors the production
// seam built in cmd/wagyu/remote_control.go's
// buildHostFastpushStore — every cascade created
// via StartRemoteAgentCascade lands in the
// dispatcher's registry before the toggle call,
// so the production checkCascade returns true).
func TestSetRemoteAgentFastpushEnabled_Ok(t *testing.T) {
	check := func(string) bool { return true }
	ps := fastpush.NewMemStore(nil, check)
	d := newFastpushDispatcher(t, ps)
	cid := registerFastpushCascade(t, d)

	res, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
		"cascade_id": cid,
		"enabled":    true,
	})
	if errObj != nil {
		t.Fatalf("SetRemoteAgentFastpushEnabled: error = %#v", errObj)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = false, want true")
	}
	if got, _ := res["cascade_id"].(string); got != cid {
		t.Errorf("result.cascade_id = %q, want %q", got, cid)
	}
	if got, _ := res["enabled"].(bool); !got {
		t.Errorf("result.enabled = false, want true")
	}
	if got, _ := res["channel"].(string); got != fastpush.ChannelName {
		t.Errorf("result.channel = %q, want %q", got, fastpush.ChannelName)
	}
	if got, _ := res["push_delivery"].(string); got != fastpush.PushDeliveryDeferred {
		t.Errorf("result.push_delivery = %q, want %q (deferred sentinel)", got, fastpush.PushDeliveryDeferred)
	}
	if got, _ := res["updated_at"].(string); got == "" {
		t.Errorf("result.updated_at missing")
	}

	// Post-state must reflect the toggle.
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Settings) != 1 {
		t.Fatalf("len(state.Settings) = %d, want 1", len(state.Settings))
	}
	if !state.Settings[0].Enabled {
		t.Errorf("state.Settings[0].Enabled = false, want true")
	}
}

// TestSetRemoteAgentFastpushEnabled_ToggleOff pins
// the toggle-off path: a second SetEnabled with
// enabled=false flips the row to false (upsert,
// not append). The wire envelope reflects the
// post-state.
func TestSetRemoteAgentFastpushEnabled_ToggleOff(t *testing.T) {
	check := func(string) bool { return true }
	ps := fastpush.NewMemStore(nil, check)
	d := newFastpushDispatcher(t, ps)
	cid := registerFastpushCascade(t, d)

	// First set: enabled=true.
	if _, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
		"cascade_id": cid,
		"enabled":    true,
	}); errObj != nil {
		t.Fatalf("first Set: error = %#v", errObj)
	}
	// Second set: enabled=false (toggle off).
	res, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
		"cascade_id": cid,
		"enabled":    false,
	})
	if errObj != nil {
		t.Fatalf("second Set: error = %#v", errObj)
	}
	if got, _ := res["enabled"].(bool); got {
		t.Errorf("result.enabled = true, want false (toggle off)")
	}

	// Slice should still hold one row.
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Settings) != 1 {
		t.Fatalf("len(state.Settings) = %d, want 1 (upsert)", len(state.Settings))
	}
	if state.Settings[0].Enabled {
		t.Errorf("state.Settings[0].Enabled = true, want false")
	}
}

// TestSetRemoteAgentFastpushEnabled_BadParams pins
// the -32602 envelopes:
//
//   - empty cascade_id → -32602 "Invalid params";
//   - whitespace-only cascade_id → -32602;
//   - missing enabled → -32602;
//   - non-string cascade_id → -32602;
//   - wrong-type enabled → -32602.
//
// Mirrors the rc-plugins / rc-triggers bad-params
// test pattern: a closed table of input shapes
// the brief explicitly enumerates.
//
// Note: the "missing cascade_id" case doesn't appear
// in the table — the dispatcher's `cascade_id absent`
// rule lands as -32602 with the same data text as
// "empty cascade_id", so the dispatcher short-circuits
// on the empty-string check before the registry lookup.
// The table focuses on the unique data shapes.
func TestSetRemoteAgentFastpushEnabled_BadParams(t *testing.T) {
	check := func(string) bool { return true }
	ps := fastpush.NewMemStore(nil, check)
	d := newFastpushDispatcher(t, ps)
	cid := registerFastpushCascade(t, d)

	cases := []struct {
		label  string
		params any
	}{
		{"empty cascade_id", map[string]any{"cascade_id": "", "enabled": true}},
		{"whitespace cascade_id", map[string]any{"cascade_id": "   ", "enabled": true}},
		{"missing enabled", map[string]any{"cascade_id": cid}},
		{"non-string cascade_id", map[string]any{"cascade_id": 42, "enabled": true}},
		{"null enabled", map[string]any{"cascade_id": cid, "enabled": nil}},
		{"wrong-type enabled", map[string]any{"cascade_id": cid, "enabled": "yes"}},
		{"missing cascade_id", map[string]any{"enabled": true}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", tc.params)
			if errObj == nil {
				t.Fatalf("%s: no error envelope", tc.label)
			}
			if codeAsInt(t, errObj["code"]) != -32602 {
				t.Errorf("%s: code = %d, want -32602", tc.label, codeAsInt(t, errObj["code"]))
			}
		})
	}
}

// TestSetRemoteAgentFastpushEnabled_CascadeNotFound
// pins the unknown-cascade → -32004 envelope. The
// store's checkCascade closure returns false; the
// dispatcher surfaces the same lifecycle code
// SendRemoteAgentCascadeMessage uses.
func TestSetRemoteAgentFastpushEnabled_CascadeNotFound(t *testing.T) {
	check := func(id string) bool { return false }
	ps := fastpush.NewMemStore(nil, check)
	d := newFastpushDispatcher(t, ps)

	_, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
		"cascade_id": "cas_unknown",
		"enabled":    true,
	})
	if errObj == nil {
		t.Fatalf("SetRemoteAgentFastpushEnabled: no error envelope")
	}
	if codeAsInt(t, errObj["code"]) != rpc.ErrCodeCascadeNotFound {
		t.Errorf("code = %d, want %d (Cascade not found)", codeAsInt(t, errObj["code"]), rpc.ErrCodeCascadeNotFound)
	}
	if got, _ := errObj["message"].(string); got != "Cascade not found" {
		t.Errorf("message = %q, want Cascade not found", got)
	}
}

// TestSetRemoteAgentFastpushEnabled_NoStore pins
// the no-store-installed → -32603 envelope. The
// test passes a nil fastpush.Store to
// NewDispatcher — but the nil-fabrication kicks in
// and installs a fresh empty MemStore. So
// to exercise the negative path, the test builds
// a dispatcher manually and clears the field after
// construction.
func TestSetRemoteAgentFastpushEnabled_NoStore(t *testing.T) {
	d := newFastpushDispatcher(t, nil)
	d.cfg.FastpushStore = nil

	_, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
		"cascade_id": "cas_abc",
		"enabled":    true,
	})
	if errObj == nil {
		t.Fatalf("SetRemoteAgentFastpushEnabled: no error envelope")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %d, want -32603", codeAsInt(t, errObj["code"]))
	}
}

// TestSetRemoteAgentFastpushEnabled_PushDeliverySentinel
// pins the wire envelope's explicit
// push_delivery:"deferred" sentinel on every
// successful call. The brief calls this out
// specifically: dashboards must not assume actual
// delivery is wired.
//
// The test exercises both toggle states (true /
// false) and asserts the sentinel appears
// verbatim in both.
func TestSetRemoteAgentFastpushEnabled_PushDeliverySentinel(t *testing.T) {
	check := func(string) bool { return true }
	ps := fastpush.NewMemStore(nil, check)
	d := newFastpushDispatcher(t, ps)
	cid := registerFastpushCascade(t, d)

	for _, enabled := range []bool{true, false} {
		res, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
			"cascade_id": cid,
			"enabled":    enabled,
		})
		if errObj != nil {
			t.Fatalf("Set(enabled=%v): error = %#v", enabled, errObj)
		}
		if got, _ := res["push_delivery"].(string); got != fastpush.PushDeliveryDeferred {
			t.Errorf("enabled=%v: push_delivery = %q, want %q", enabled, got, fastpush.PushDeliveryDeferred)
		}
	}
}
