// Package rchost — remote_control_standalone.go: the dispatcher the
// standalone language server serves.
//
// `cmd/langserver` is the clone's rebuild of the Antigravity IDE's
// `language_server` binary (internal/lsserver). It answers the same
// `LanguageServerService` the web UI talks to, so it needs the same
// dispatcher the remote-control surface builds — the clone's own
// implementation, wired to the host's stores.
//
// The builder is exported here rather than reimplemented in
// internal/lsserver because the store wiring (the FS scope, the
// project store, the conversation store, the permission, plugin,
// trigger and fastpush stores) lives in this package and cannot move
// into internal/remotecontrol without an import-direction reversal —
// see remote_control_builders.go.
package rchost

import (
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// NewStandaloneDispatcher builds the dispatcher the standalone
// language server serves.
//
// mendelUserAgent is the identity the Mendel read carries: the hub's
// own for the IDE's client (the desktop feature set), the CLI's own
// for everything else.
func NewStandaloneDispatcher(cwd, addr, instanceID, mendelUserAgent string) (*remotecontrol.Dispatcher, error) {
	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	sess.Workspace = cwd
	return buildServeDispatcher(cwd, addr, sess, instanceID, mendelUserAgent)
}
