// Command harness — a clone of Google Antigravity's closed-source
// `localharness` agent backend.
//
// # What this is
//
// The `agy` CLI and the `google-antigravity` SDKs do not talk to a model
// directly. They spawn a closed-source Go binary (`localharness`) that
// owns the agent loop, the tools, the budget and the lifecycle hooks, and
// drive it over a two-phase transport:
//
//	phase 1 — stdio handshake
//	    client  --[4-byte LE length][InputConfig protobuf]-->  harness stdin
//	    client  <--[4-byte LE length][OutputConfig protobuf]--  harness stdout
//
//	phase 2 — websocket
//	    client  --ws://<host>:<port>/ , header x-goog-api-key: <key]--> harness
//	    client  <--[protojson OutputEvent]--   harness
//	    client  --[protojson InputEvent]-->    harness
//
// This command is that binary, reimplemented on top of the clone's own
// packages. It is not a reimplementation of the *agent loop*: the loop, the
// tools, the transports, the system prompt, the permission policy, the
// budget manager and the lifecycle-hook model all come from the packages
// the CLI already uses.
//
//	what                        comes from
//	--------------------------  ------------------------------------------
//	turn engine                 internal/pigrt (PiG's coding.Session)
//	provider / transport        internal/backend + internal/piagentbridge
//	tool catalogue              internal/tools (the reference's own names)
//	tool surface to the model   internal/tools.ModelDeclarations
//	permission policy           internal/permissions
//	budget manager              budget
//	lifecycle-hook model        lifecyclehooks
//	wire protocol               localharness
//
// # What it is not
//
// It is not a byte-for-byte clone of the reference binary. The reference's
// own agent loop, its cortex engine and its planner are not reproduced;
// the clone's PiG-based runtime stands in for them. The *protocol* is the
// part this command reproduces, and the evidence for it lives in
// `out/localharness/`.
//
// # stdout is the protocol
//
// The handshake's `OutputConfig` frame is written to stdout, and nothing
// else may be. Every diagnostic goes to stderr. A stray `fmt.Println` in
// this package would desynchronise the client, which is reading a
// length-prefixed frame off the same stream.
//
// # Usage
//
//	harness [--model NAME] [--backend gateway|cloudcode|shim]
//	        [--workspace DIR] [--agent-dir DIR] [--host ADDR] [--port N]
//
// The client is expected to write the `InputConfig` frame to stdin
// immediately; the harness blocks on that read.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "harness: %v\n", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, opts, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "harness: %v\n", err)
		os.Exit(1)
	}
}

// run is main's body with its streams injected, so a test can drive the
// whole handshake over a pipe instead of a child process.
func run(ctx context.Context, opts options, in io.Reader, out io.Writer) error {
	return serve(ctx, opts, in, out)
}
