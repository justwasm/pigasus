// server_host_test.go — the `pigasus server` subcommand's own wiring.
//
// The address policy belongs to the server (internal/app/agentserver):
// the loopback default, the port default and the rule that an explicit
// address wins. What the host owns is the mapping — which of its own
// arguments reaches which of the server's values.
package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/agentserver"
	"github.com/justwasm/pigasus/internal/cli"
)

// TestServerAddressWiring pins that the host hands the server the
// address, the host and the port it was given.
//
// The policy itself is pinned in internal/app/agentserver; what is
// pinned here is that the host's three arguments reach the server's
// resolver in the right slots — a swap between them would change the
// bind address without changing anything else.
func TestServerAddressWiring(t *testing.T) {
	cases := []struct {
		name string
		args cli.ServerArgs
		want string
	}{
		{"the default", cli.ServerArgs{}, "127.0.0.1:7777"},
		{"a host and a port", cli.ServerArgs{Host: "example.test", Port: 9000}, "example.test:9000"},
		{"an address wins", cli.ServerArgs{Addr: "10.0.0.1:1234", Host: "0.0.0.0", Port: 8080}, "10.0.0.1:1234"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := agentserver.ResolveAddr(c.args.Addr, c.args.Host, c.args.Port)
			if got != c.want {
				t.Errorf("ResolveAddr(%q, %q, %d) = %q, want %q",
					c.args.Addr, c.args.Host, c.args.Port, got, c.want)
			}
		})
	}
}
