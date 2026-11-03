// Package cli — tui_bridge.go: internal/cliapp → internal/cli callback for
// the `pigasus tui` subcommand.
//
// The interactive session is the client half of the client/server split.
// Without --server it is the ordinary local session; with it, the turn
// runs on the server and the session renders the server's frames.
package cli

// TUIArgs is the typed argument set passed to the tuiHandler.
type TUIArgs struct {
	// Server is the address of the pigasus server to attach to
	// ("ws://host:port"). Empty means the local session.
	Server string

	// Token is the bearer token the server demands.
	Token string

	// Protocol is the wire protocol to speak ("agentlink" or "acp").
	Protocol string

	// Backend is the agent backend to run ("local", "codex", etc.).
	Backend string

	// ACPVersion is the ACP protocol version to negotiate ("auto", "1", "2").
	ACPVersion string

	// ACPCmd is an optional custom command to spawn an external ACP agent over stdio.
	ACPCmd string

	// ACPManifest is an optional path to an agent.json manifest file or folder.
	ACPManifest string

	// Args holds the arguments the subcommand does not own: the
	// session's own flags, which the session parses.
	Args []string
}

// tuiHandler is the handler implementation wired by cliapp.
var tuiHandler func(j *JetskiCLI, args TUIArgs) error

// RegisterTUIHandler registers the implementation for `pigasus tui`.
func RegisterTUIHandler(fn func(j *JetskiCLI, args TUIArgs) error) {
	tuiHandler = fn
}
