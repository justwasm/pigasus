// tui_attach_test.go — the client's side of the address.
//
// The client dials the server by an address the user typed: a bare
// "host:port" is the common way to name one, and the websocket scheme
// is the only one the protocol speaks.
package cliapp

import "testing"

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
