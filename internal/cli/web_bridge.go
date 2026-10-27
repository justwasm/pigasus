// Package cli — web_bridge.go: internal/cliapp → internal/cli callback for the
// standalone `pigasus web` subcommand.
package cli

// WebArgs is the typed argument set passed to the webHandler.
type WebArgs struct {
	// Addr is the bind address (host:port, e.g. "0.0.0.0:8787", "127.0.0.1:8787", or ":8787").
	Addr string

	// Port is an optional port override (e.g. 8787, 8080).
	Port int

	// Host is an optional host interface (e.g. "0.0.0.0" or "127.0.0.1").
	Host string

	// Workspace is the root project workspace path to serve.
	Workspace string

	// Open indicates whether to automatically launch the default browser.
	Open bool

	// ReferenceBackend makes the web server proxy its RPCs to a
	// reference `language_server` instead of answering them itself.
	//
	// The value is either an endpoint ("host:port", "http://host:port")
	// of a running reference, or the path of a reference binary to
	// start. Empty means the clone answers the RPCs itself.
	ReferenceBackend string

	// Desktop asks the web UI for the reference's *desktop* UI: the
	// voice input, the jetbox terminal, the redesigned aux pane.
	//
	// The page and the bundle are the same either way — the switch
	// is the identity the page's Mendel read carries: the backend
	// serves a different flag set per product, and the desktop
	// features are in the hub's set.
	//
	// 【一手】 measured 2026-10-27 — the same token, the same body,
	// the same endpoint, only the User-Agent differing:
	// `antigravity/hub/…` answers enable-audio-transcription: true,
	// `antigravity/cli/…` answers it false. See internal/auth.
	Desktop bool
}

// webHandler is the handler implementation wired by rchost/cliapp.
var webHandler func(j *JetskiCLI, args WebArgs) error

// RegisterWebHandler registers the implementation for `pigasus web`.
func RegisterWebHandler(fn func(j *JetskiCLI, args WebArgs) error) {
	webHandler = fn
}

// EnsureWebHandlerRegistered reports whether a web handler is registered.
func EnsureWebHandlerRegistered() bool {
	return webHandler != nil
}
