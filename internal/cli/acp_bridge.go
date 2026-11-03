// Package cli — acp_bridge.go: internal/cliapp → internal/cli callback for
// the `pigasus acp` subcommand.
package cli

import (
	"context"
	"io"
)

// ACPArgs holds the arguments passed to the acpHandler.
type ACPArgs struct {
	// Stdio is true when ACP should communicate over standard I/O (stdin/stdout).
	Stdio bool

	// Addr is the bind address (e.g. "127.0.0.1:0" or "127.0.0.1:7777").
	Addr string

	// Workspace is the root project workspace path to serve.
	Workspace string

	// Model is the model name.
	Model string

	// Effort is the reasoning effort.
	Effort string

	// Token is an optional bearer token.
	Token string

	// In is standard input.
	In io.Reader

	// Out is standard output.
	Out io.Writer

	// Err is diagnostics / stderr.
	Err io.Writer

	// Ctx is the cancellation context.
	Ctx context.Context

	// RawArgs are any unprocessed command-line arguments.
	RawArgs []string
}

// acpHandler is the handler implementation wired by cliapp.
var acpHandler func(j *JetskiCLI, args ACPArgs) error

// RegisterACPHandler registers the implementation for `pigasus acp`.
func RegisterACPHandler(fn func(j *JetskiCLI, args ACPArgs) error) {
	acpHandler = fn
}
