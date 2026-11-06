// tui_attach_test.go — the client's side of the address.
//
// The client dials the server by an address the user typed: a bare
// "host:port" is the common way to name one, and the websocket scheme
// is the only one the protocol speaks.
package tui

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
)

// TestNormalizeServerAddr pins that a bare "host:port" — the common way
// to name a server — becomes a websocket URL, and that a URL the user
// wrote is left alone.
func TestNormalizeServerAddr(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"127.0.0.1:7777", "ws://127.0.0.1:7777"},
		{"  127.0.0.1:7777  ", "ws://127.0.0.1:7777"},
		{"ws://127.0.0.1:7777", "ws://127.0.0.1:7777"},
		{"wss://example.test/agent", "wss://example.test/agent"},
		{"http://example.test", "http://example.test"},
	}
	for _, c := range cases {
		if got := normalizeServerAddr(c.in); got != c.want {
			t.Errorf("normalizeServerAddr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestResolveAttachProtocol pins protocol dispatch based on URL path suffix and --protocol flag.
func TestResolveAttachProtocol(t *testing.T) {
	cases := []struct {
		addr  string
		proto string
		want  string
	}{
		// Explicit ahp protocol flag
		{"ws://127.0.0.1:8080", "ahp", "ahp"},
		{"ws://127.0.0.1:8080", "AHP", "ahp"},
		{"ws://127.0.0.1:8080/", "ahp", "ahp"},
		// URL ending in /ahp overrides default or empty protocol
		{"ws://127.0.0.1:8080/ahp", "", "ahp"},
		{"ws://127.0.0.1:8080/ahp", "acp", "ahp"},
		{"ws://127.0.0.1:8080/ahp/", "acp", "ahp"},
		{"127.0.0.1:8080/ahp", "", "ahp"},
		{"ws://127.0.0.1:8080/ahp?token=secret", "", "ahp"},
		{"ws://127.0.0.1:8080/ahp?session=123", "acp", "ahp"},
		// URL ending in /acp or acp flag
		{"ws://127.0.0.1:8080/acp", "", "acp"},
		{"ws://127.0.0.1:8080/acp?token=secret", "", "acp"},
		{"ws://127.0.0.1:8080", "acp", "acp"},
		{"ws://127.0.0.1:8080", "", "acp"},
		// Agentlink
		{"ws://127.0.0.1:8080", "agentlink", "agentlink"},
		{"ws://127.0.0.1:8080/", "agentlink", "agentlink"},
	}
	for _, c := range cases {
		if got := resolveAttachProtocol(c.addr, c.proto); got != c.want {
			t.Errorf("resolveAttachProtocol(%q, %q) = %q, want %q", c.addr, c.proto, got, c.want)
		}
	}
}

// TestRunAttachedTUI_DispatchesToAHP verifies that runAttachedTUI properly delegates to runAttachedTUI_AHP.
func TestRunAttachedTUI_DispatchesToAHP(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var out bytes.Buffer

	// Case 1: --protocol ahp
	err := runAttachedTUI(&out, fs, cli.TUIArgs{
		Server:   "127.0.0.1:59999",
		Protocol: "ahp",
	})
	if err == nil || !strings.Contains(err.Error(), "tui (ahp): cannot reach the server") {
		t.Errorf("expected tui (ahp) connection error, got: %v", err)
	}

	// Case 2: URL ending in /ahp with default protocol
	err = runAttachedTUI(&out, fs, cli.TUIArgs{
		Server:   "ws://127.0.0.1:59999/ahp",
		Protocol: "acp", // default value passed by CLI parser
	})
	if err == nil || !strings.Contains(err.Error(), "tui (ahp): cannot reach the server") {
		t.Errorf("expected tui (ahp) connection error for /ahp suffix, got: %v", err)
	}
}
