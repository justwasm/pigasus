// addr.go — the server's bind address.
//
// The address the server binds on is the server's own fact: the
// loopback on a fixed port, because the server runs shell commands
// against its own workspace and must not listen on the network unless
// the user asks for it.
//
// The host supplies the three values the user gave — the address, the
// host and the port — and the server decides what they mean. The
// function takes plain values, so the package never depends on the
// host's own argument types.
package agentserver

import (
	"fmt"
	"strings"
)

const (
	// DefaultHost is the interface the server binds when nothing else
	// is asked for.
	//
	// The loopback is the default on purpose: the server runs shell
	// commands against its own workspace, so it does not listen on the
	// network unless the user asks for it.
	DefaultHost = "127.0.0.1"

	// DefaultPort is the port the server binds when nothing else is
	// asked for.
	DefaultPort = 7777
)

// ResolveAddr maps the server's address options onto a bind address.
//
// An explicit address wins: it names the whole thing, host and port
// together. Otherwise the host and the port are filled in one by one,
// each falling back to its default when it was not given.
func ResolveAddr(addr, host string, port int) string {
	if a := strings.TrimSpace(addr); a != "" {
		return a
	}
	h := strings.TrimSpace(host)
	if h == "" {
		h = DefaultHost
	}
	p := port
	if p <= 0 {
		p = DefaultPort
	}
	return fmt.Sprintf("%s:%d", h, p)
}
