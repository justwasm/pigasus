package remotecontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestReferenceRPCMethodsMatchFixture pins ReferenceRPCMethods to the
// method list recovered from agy 1.4.6's embedded FileDescriptorProto. If
// this fails, either the fixture was refreshed against a new reference
// release (update the slice) or the slice was edited by hand (don't).
func TestReferenceRPCMethodsMatchFixture(t *testing.T) {
	f, err := os.Open("testdata/jetski_remote_agent_proxy_1.4.6.txt")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	var want []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			t.Fatalf("malformed fixture line %q", line)
		}
		want = append(want, parts[0])
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	if len(want) != 77 {
		t.Fatalf("fixture has %d RPCs, want 77", len(want))
	}
	if len(ReferenceRPCMethods) != len(want) {
		t.Fatalf("ReferenceRPCMethods has %d entries, fixture has %d", len(ReferenceRPCMethods), len(want))
	}
	for i := range want {
		if ReferenceRPCMethods[i] != want[i] {
			t.Fatalf("RPC %d: got %q, want %q", i, ReferenceRPCMethods[i], want[i])
		}
	}
}

// TestReferenceAliasesDispatch asserts that every alias names a real clone
// handler: dispatching the reference name must never answer -32601. Handler
// validation errors (-32602) are expected for empty params and are fine —
// the point is that the method was routed.
func TestReferenceAliasesDispatch(t *testing.T) {
	d, err := NewDispatcher(Config{Session: NewSessionState("1.4.4-test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	for reference, canonical := range referenceRPCAliases {
		raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"` + reference + `","params":{}}`)
		resp := d.Dispatch(context.Background(), raw)
		if resp == nil {
			t.Fatalf("%s: nil response (treated as a notification?)", reference)
		}
		if e, ok := resp["error"].(map[string]any); ok {
			if code, _ := e["code"].(float64); code == -32601 {
				t.Errorf("%s (%s): method not found — alias target is not routed", reference, canonical)
			}
		}
	}
}

// TestReferenceAliasInverseIsInjective guards ReferenceRPCForCloneMethod:
// two reference RPCs must not claim the same clone handler, otherwise the
// parity report would silently pick one.
func TestReferenceAliasInverseIsInjective(t *testing.T) {
	seen := map[string]string{}
	for reference, canonical := range referenceRPCAliases {
		if prev, ok := seen[canonical]; ok {
			t.Fatalf("clone method %q claimed by both %q and %q", canonical, prev, reference)
		}
		seen[canonical] = reference
		if got := ReferenceRPCForCloneMethod(canonical); got != reference {
			t.Fatalf("ReferenceRPCForCloneMethod(%q) = %q, want %q", canonical, got, reference)
		}
	}
}

// TestReferenceAliasParams covers the two request fields whose reference
// spelling differs from the clone's: StartCascade's project_id and
// SendUserCascadeMessage's message_text.
func TestReferenceAliasParams(t *testing.T) {
	p, rpcErr := decodeStartRemoteAgentCascadeParams(json.RawMessage(`{"project_id":"proj-1"}`))
	if rpcErr != nil {
		t.Fatalf("start decode: %+v", rpcErr)
	}
	if p.ProjectID != "proj-1" {
		t.Fatalf("project_id alias: got %q", p.ProjectID)
	}

	s, rpcErr := decodeSendRemoteAgentCascadeMessageParams(json.RawMessage(`{"cascade_id":"c1","message_text":"hi"}`))
	if rpcErr != nil {
		t.Fatalf("send decode: %+v", rpcErr)
	}
	if s.Text != "hi" {
		t.Fatalf("message_text alias: got %q", s.Text)
	}

	// An explicit clone-spelling value still wins.
	s, rpcErr = decodeSendRemoteAgentCascadeMessageParams(json.RawMessage(`{"cascade_id":"c1","text":"a","message_text":"b"}`))
	if rpcErr != nil {
		t.Fatalf("send decode: %+v", rpcErr)
	}
	if s.Text != "a" {
		t.Fatalf("explicit text lost: got %q", s.Text)
	}
}
