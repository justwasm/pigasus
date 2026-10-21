// Package remotetransport — origin_test.go:
// the WS-upgrade origin allowlist (cycle
// rc-auth-tokens). Three concerns:
//
//   - (*TCPServer).SetOriginPolicy +
//     OriginPolicySnapshot reflect the
//     active policy;
//   - (*TCPServer).handleOriginCheck
//     reads the policy under the
//     setOriginPolicyMu read lock and
//     returns a verdict;
//   - handleWS denies a non-allowlisted
//     Origin with HTTP 403 + JSON body
//     BEFORE the ws upgrade; the
//     pre-cycle "Origin: nil" (no header
//     at all) path still upgrades
//     (preserves the curl/wscat compat).
//
// Stdlib-only. No third-party deps in
// the tests beyond what the rest of the
// package already uses (coder/websocket +
// remotecontrol).
package remotetransport

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/justwasm/pigasus/internal/originpolicy"
)

// TestSetOriginPolicy_Roundtrip pins
// that the setter swaps the active
// allowlist atomically and the snapshot
// returns a defensive copy. A caller
// that mutates its slice after the Set
// call doesn't race an in-flight
// Allows() read.
func TestSetOriginPolicy_Roundtrip(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	policy := originpolicy.OriginPolicy{
		Patterns:  []string{"https://antigravity.google"},
		UpdatedAt: time.Now().UTC(),
	}
	srv.SetOriginPolicy(policy)
	got := srv.OriginPolicySnapshot()
	if len(got.Patterns) != 1 || got.Patterns[0] != "https://antigravity.google" {
		t.Errorf("OriginPolicySnapshot: got %v, want [https://antigravity.google]", got.Patterns)
	}
	// Defensive copy: a caller that mutates
	// its own slice after Set doesn't race
	// the server's internal copy.
	policy.Patterns[0] = "https://attacker.example"
	got2 := srv.OriginPolicySnapshot()
	if got2.Patterns[0] != "https://antigravity.google" {
		t.Errorf("Set must copy: got %v after caller mutation", got2.Patterns[0])
	}
}

// TestSetOriginPolicy_EmptyRestoresAllowAll
// pins the "empty patterns ⇒ allow all"
// semantic that matches
// DefaultOriginPolicy().Patterns=[].
func TestSetOriginPolicy_EmptyRestoresAllowAll(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	srv.SetOriginPolicy(originpolicy.OriginPolicy{
		Patterns: []string{"https://antigravity.google"},
	})
	if srv.OriginPolicySnapshot().Allows("https://example.com") {
		t.Fatalf("first snapshot should deny example.com; got allow")
	}
	srv.SetOriginPolicy(originpolicy.OriginPolicy{Patterns: []string{}})
	if !srv.OriginPolicySnapshot().Allows("https://example.com") {
		t.Errorf("empty patterns should allow all")
	}
}

// TestSetOriginPolicy_NilReceiver is the
// nil-safety guard the host wiring
// relies on when the host didn't bring
// up a TCPServer (Unix-socket path).
func TestSetOriginPolicy_NilReceiver(t *testing.T) {
	var srv *TCPServer
	srv.SetOriginPolicy(originpolicy.OriginPolicy{Patterns: []string{"https://antigravity.google"}})
	if got := srv.OriginPolicySnapshot(); len(got.Patterns) != 0 {
		t.Errorf("nil receiver should return zero value, got %v", got.Patterns)
	}
}

// TestHandleWS_Forbidden_DenyOrigin
// confirms handleWS denies a non-
// allowlisted origin with HTTP 403 +
// JSON body BEFORE the ws upgrade
// runs. The ws client receives an
// http.StatusForbidden response, NOT
// a 101 Switching Protocols.
//
// Cycle rc-auth-tokens.
func TestHandleWS_Forbidden_DenyOrigin(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	// Install an allowlist that
	// permits only the canonical
	// antigravity hosts.
	srv.SetOriginPolicy(originpolicy.OriginPolicy{
		Patterns: []string{"https://antigravity.google"},
	})

	// Dial ws with an Origin header
	// the allowlist doesn't permit.
	// coder/websocket's Dial lets the
	// caller set the Origin header.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, srv.HTTPURL(), &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin": []string{"https://attacker.example"},
		},
	})
	if err == nil {
		_ = c.CloseNow()
		t.Fatalf("Dial should fail with 403; got success")
	}
	if resp == nil {
		t.Fatalf("Dial returned no http.Response")
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	body := make([]byte, 256)
	n, _ := resp.Body.Read(body)
	resp.Body.Close()
	bodyStr := string(body[:n])
	// The JSON body must carry the
	// decision shape the brief pins:
	// {ok:false, denied:true, origin, reason}.
	var bodyMap map[string]any
	if err := json.Unmarshal(body[:n], &bodyMap); err != nil {
		t.Fatalf("body not JSON: %v\nbody=%s", err, bodyStr)
	}
	if bodyMap["ok"] != false {
		t.Errorf("body.ok = %v, want false", bodyMap["ok"])
	}
	if bodyMap["denied"] != true {
		t.Errorf("body.denied = %v, want true", bodyMap["denied"])
	}
	if bodyMap["origin"] != "https://attacker.example" {
		t.Errorf("body.origin = %v, want https://attacker.example", bodyMap["origin"])
	}
	if reason, _ := bodyMap["reason"].(string); !strings.Contains(reason, "attacker.example") {
		t.Errorf("body.reason = %q, want substring 'attacker.example'", reason)
	}
}

// TestHandleWS_Allow_AllowlistedOrigin
// confirms the happy path: an origin in
// the allowlist upgrades normally. The
// ws client receives a 101 and a
// JSON-RPC request can be round-tripped.
func TestHandleWS_Allow_AllowlistedOrigin(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	srv.SetOriginPolicy(originpolicy.OriginPolicy{
		Patterns: []string{"https://antigravity.google"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, srv.HTTPURL(), &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin": []string{"https://antigravity.google"},
		},
	})
	if err != nil {
		t.Fatalf("Dial should succeed for allowlisted origin; got %v", err)
	}
	defer func() {
		_ = c.CloseNow()
	}()
	// Round-trip one JSON-RPC request
	// to prove the dispatcher is wired.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	})
	resp := readJSONRPC(t, c)
	if _, ok := resp["id"]; !ok {
		t.Errorf("missing id in response: %v", resp)
	}
}

// TestHandleWS_Allow_NoOriginHeader pins
// the pre-cycle compat path. A request
// with no Origin header upgrades
// normally regardless of the allowlist
// (matches OriginPolicy.Allows("") ==
// true).
func TestHandleWS_Allow_NoOriginHeader(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	srv.SetOriginPolicy(originpolicy.OriginPolicy{
		Patterns: []string{"https://antigravity.google"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, srv.HTTPURL(), nil)
	if err != nil {
		t.Fatalf("Dial with no Origin should succeed (non-browser caller); got %v", err)
	}
	defer func() {
		_ = c.CloseNow()
	}()
	// Round-trip a JSON-RPC ping to
	// confirm the dispatcher is wired.
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	})
	_ = readJSONRPC(t, c)
}

// TestHandleWS_Allow_EmptyPolicy pins
// the "no policy configured ⇒ allow
// all" pre-cycle semantic. A freshly-
// constructed TCPServer (no SetOriginPolicy
// call) upgrades every dial.
func TestHandleWS_Allow_EmptyPolicy(t *testing.T) {
	srv, _, done := newTestTCPServer(t)
	defer done()

	// Deliberately do NOT call
	// SetOriginPolicy — the
	// pre-cycle "no policy ⇒ allow
	// all" semantic must hold.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, srv.HTTPURL(), &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin": []string{"https://anything.example"},
		},
	})
	if err != nil {
		t.Fatalf("Dial with no policy set should succeed; got %v", err)
	}
	defer func() {
		_ = c.CloseNow()
	}()
}
