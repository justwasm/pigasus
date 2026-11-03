// Package remotecontrol — origin_policy_store_test.go: the
// dispatcher's harness for the origin-policy domain.
//
// The store's own tests live in the domain package
// (internal/remotecontrol/origins); this file carries the
// helpers that build a dispatcher around a store and drive one
// method through it.
//
// Wire-level tests for the
// SetRemoteControlOriginPatterns RPC live in
// remotetransport/tcp_test.go (mirroring
// the project's split: in-process envelopes vs.
// wire round-trips).
package remotecontrol

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/remotecontrol/origins"
)

// newOriginPolicyDispatcher returns a Dispatcher with the
// supplied origins.Store pre-installed. Mirrors
// newPluginsDispatcher / newFastpushDispatcher.
//
// When os nil, NewDispatcher fabricates a fresh
// empty MemOriginPolicyStore (the nil-guard pattern
// rc-auth-tokens adds). Tests that want to assert
// "no store installed" pass nil and exercise the
// -32603 path; tests that want a specific store
// pass it directly.
func newOriginPolicyDispatcher(t *testing.T, os origins.Store) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:           sess,
		OriginPolicyStore: os,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchOriginPolicy posts one JSON-RPC
// request to the dispatcher and returns the
// (result, error) pair. Mirrors
// dispatchPlugins / dispatchFastpush.
//
// nil params marshals to JSON `null` so the
// dispatcher's "len(params) > 0" guards see a
// present (but null) params block; jsonRPCFor
// takes a byte slice and appends verbatim, so
// we mirror the convention `[]byte("null")`
// rather than a missing-key shape.
func dispatchOriginPolicy(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	switch v := params.(type) {
	case nil:
		raw = []byte("null")
	case []byte:
		raw = v
	default:
		var err error
		raw, err = jsonMarshal(v)
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
