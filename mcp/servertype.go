// servertype.go — the two server transports, and the members each one
// carries.
//
// A server is either a local process or a remote endpoint, and the two
// carry different members: `env` belongs to the process, `headers` to the
// request. A command that asks for the member of the other transport is
// refused rather than answered with an entry that carries a member the
// transport cannot use.
//
// The evidence
// ------------
//
// 【一手】 the reference binary (1.4.12), under a temporary HOME:
//
//	$ agy mcp add --env FOO=bar api https://example.com/mcp
//	Error: http server type does not accept --env flags          (exit 1)
//	$ agy mcp add --header "A: b" fs npx
//	Error: stdio server type does not accept --header flags       (exit 1)
//	$ agy mcp add --type sse fs npx
//	Error: invalid server type "sse": must be 'stdio' or 'http'   (exit 1)
//
// The type name is trimmed and lowercased before it is compared, so
// `--type HTTP` and `--type " stdio"` name the two documented
// transports; anything else is refused with the reference's own message,
// which echoes the value as it was typed.
//
// The transport is resolved by the endpoint first: an `http://` or
// `https://` target is an http server even when `--type stdio` says
// otherwise.
package mcp

import (
	"errors"
	"fmt"
	"strings"
)

// The two server transports the documented schema allows.
const (
	// ServerTypeStdio is a local process the client spawns.
	ServerTypeStdio = "stdio"
	// ServerTypeHTTP is a remote endpoint the client connects to.
	ServerTypeHTTP = "http"
)

// ParseServerType resolves the server type a command line asks for.
//
// The two documented names are the only ones accepted. The value is
// trimmed and lowercased first, so a differently spelled name is the
// name it spells; anything else is refused, and the error carries the
// reference's own message.
func ParseServerType(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ServerTypeStdio:
		return ServerTypeStdio, nil
	case ServerTypeHTTP:
		return ServerTypeHTTP, nil
	}
	return "", fmt.Errorf("invalid server type %q: must be 'stdio' or 'http'", raw)
}

// IsHTTPEndpoint reports whether a target names an http endpoint.
//
// The reference resolves the transport by the endpoint's shape, so an
// `http://` or `https://` target is an http server whatever the type
// flag says.
func IsHTTPEndpoint(target string) bool {
	return strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")
}

// ValidateTransportFlags refuses the flag/transport combinations the
// reference refuses: `--env` on an http server, and `--header` on a stdio
// server.
//
// The error carries the reference's own message; the caller prints it
// with the `Error: ` prefix.
func ValidateTransportFlags(kind string, hasEnv, hasHeaders bool) error {
	switch kind {
	case ServerTypeHTTP:
		if hasEnv {
			return errors.New("http server type does not accept --env flags")
		}
	case ServerTypeStdio:
		if hasHeaders {
			return errors.New("stdio server type does not accept --header flags")
		}
	}
	return nil
}
