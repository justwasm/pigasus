// Package cli — server_bridge.go: internal/cliapp → internal/cli callback
// for the `pigasus server` subcommand.
//
// The server is the host half of the client/server split: it owns the
// workspace, the tools and the credentials, and it serves the turns a
// client asks for over a websocket.
//
// The parser-level half lives in internal/cli (the spec is registered
// next to the `web` sibling), and the handler body lives in
// internal/cliapp — it needs the clone's own session setup, which is
// where the tools, the permission policy and the prompt are built.
package cli

// ServerArgs is the typed argument set passed to the serverHandler.
type ServerArgs struct {
	// Addr is the bind address (host:port, e.g. "127.0.0.1:7777").
	Addr string

	// Port is an optional port override (e.g. 7777, 8080).
	Port int

	// Host is an optional host interface (e.g. "0.0.0.0" or "127.0.0.1").
	Host string

	// Workspace is the root project workspace path to serve.
	Workspace string

	// Model is the model the served turns run under.
	Model string

	// Effort is the reasoning effort the served turns ask for.
	Effort string

	// Mode is the session's permission mode: "auto", "plan" or
	// "acceptEdits". Empty means the configured default.
	Mode string

	// SkipPermissions runs the served tools without the permission gate.
	SkipPermissions bool

	// Token is the bearer token a client has to present. Empty means
	// the server mints one.
	Token string

	// Agent is the custom agent the served turns run under. Empty means
	// the default agent.
	Agent string

	// Backend is the transport the served turns run on ("gateway",
	// "cloudcode", "shim"). Empty means the configured default.
	Backend string

	// AddDirs are extra workspaces the served tools may act on.
	AddDirs []string

	// ACPVersion is the ACP protocol version to serve ("auto", "1", "2").
	ACPVersion string
}

// serverHandler is the handler implementation wired by cliapp.
var serverHandler func(j *JetskiCLI, args ServerArgs) error

// RegisterServerHandler registers the implementation for `pigasus server`.
func RegisterServerHandler(fn func(j *JetskiCLI, args ServerArgs) error) {
	serverHandler = fn
}
