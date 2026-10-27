// server.go — the harness's stdio handshake, listener and flags.
//
// # The handshake
//
// The client writes the `InputConfig` frame to stdin and waits for the
// `OutputConfig` frame on stdout. The harness therefore reads stdin first,
// binds its listener, and only then writes stdout — the port has to be in
// the frame the client is blocking on.
//
// # stdout discipline
//
// The `OutputConfig` frame is the *only* thing this process may write to
// stdout. The client parses that stream as length-prefixed frames, so a
// stray newline would be read as the first byte of a length prefix and
// desynchronise the connection for good. Every diagnostic goes to stderr.
//
// # Limitations
//
//   - One connection is served at a time. The reference harness may
//     multiplex; nothing here does.
//   - The `InputConfig.env` map is *not* applied to the harness process.
//     It travels to the client-side tools in the reference; here it is
//     logged and otherwise ignored.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/localharness"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// options is the harness's configuration. Every field has a default, so an
// empty options is usable.
type options struct {
	// model is the model the harness serves: a catalogue label or slug, or
	// the name of a model the user configured. Empty takes the model from
	// the client's HarnessConfig, then the reference's default.
	model string

	// backend selects the transport: "gateway", "cloudcode" or "shim". Empty
	// lets the shared dispatch decide (a catalogue model goes to CloudCode,
	// a configured model to its own endpoint).
	backend string

	// workspace is the directory the tools operate in. Empty uses the
	// process's working directory.
	workspace string

	// workspaceExplicit records that the operator named the workspace on
	// the command line. It wins over the client's `workspaces` field.
	workspaceExplicit bool

	// agentDir is the agy config home. Empty uses the reference's
	// ~/.gemini/antigravity-cli.
	agentDir string

	// agentDirExplicit records that the operator named the config home on
	// the command line. It wins over the client's `app_data_dir`.
	agentDirExplicit bool

	// host is the address the websocket binds. Empty binds the loopback
	// interface only.
	host string

	// port is the TCP port the websocket binds. Zero picks a free one.
	port int

	// timeout is the gateway request timeout in seconds. Zero means 60
	// seconds (the transport's own default); a negative value disables it,
	// which is what a server wants: one model call may legitimately take
	// minutes.
	timeout int

	// verbose turns the protocol log on.
	verbose bool
}

// parseFlags parses the command line.
//
// The flag set writes to stderr: stdout belongs to the protocol.
func parseFlags(args []string, errOut io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet("harness", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&opts.model, "model", "", "the model to serve (catalogue label or slug; default: the client's HarnessConfig, else the reference's default)")
	fs.StringVar(&opts.backend, "backend", "", "the transport to use: gateway|cloudcode|shim")
	fs.StringVar(&opts.workspace, "workspace", "", "the directory the tools operate in (default: the client's workspaces, else the working directory)")
	fs.StringVar(&opts.agentDir, "agent-dir", "", "the agy config home (default: the client's app_data_dir, else ~/.gemini/antigravity-cli)")
	fs.StringVar(&opts.host, "host", "", "the address the websocket binds (default: the client's InputConfig, else loopback)")
	fs.IntVar(&opts.port, "port", 0, "the port the websocket binds (default: the client's InputConfig, else a free one)")
	fs.IntVar(&opts.timeout, "timeout", -1, "the gateway request timeout in seconds (-1 disables it)")
	fs.BoolVar(&opts.verbose, "verbose", false, "log the protocol to stderr")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	opts.workspace = strings.TrimSpace(opts.workspace)
	opts.workspaceExplicit = opts.workspace != ""
	if opts.workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			return options{}, fmt.Errorf("resolve working directory: %w", err)
		}
		opts.workspace = wd
	}
	if abs, err := filepath.Abs(opts.workspace); err == nil {
		opts.workspace = abs
	}
	opts.model = strings.TrimSpace(opts.model)
	opts.backend = strings.TrimSpace(opts.backend)
	opts.host = strings.TrimSpace(opts.host)
	opts.agentDir = strings.TrimSpace(opts.agentDir)
	opts.agentDirExplicit = opts.agentDir != ""
	if opts.agentDir == "" {
		// The same home the CLI uses: it is where mcp.json, the settings
		// and the skills live.
		opts.agentDir = pigrt.DefaultAgentHome()
	}
	return opts, nil
}

// serve runs the harness: handshake, listen, serve.
//
// It returns when the context is cancelled or the listener fails.
func serve(ctx context.Context, opts options, in io.Reader, out io.Writer) error {
	logger := newLogger(opts.verbose)

	// Phase 1, client to harness: the InputConfig.
	input, err := localharness.ReadInputConfig(in)
	if err != nil {
		return fmt.Errorf("read input config: %w", err)
	}
	if ci := input.GetClientInfo(); ci != nil {
		logger.Debug("harness: client", "language", ci.GetLanguage(), "version", ci.GetVersion(), "os", ci.GetOs())
	}
	if env := input.GetEnv(); len(env) > 0 {
		logger.Debug("harness: client env entries", "count", len(env))
		// The reference applies the client's environment entries to the
		// harness process, so the tools and the model transport see
		// them. This process exists for that one client, so the clone
		// does the same.
		applyEnv(envFromConfig(input))
	}

	host := opts.host
	if host == "" {
		host = strings.TrimSpace(input.GetBindAddress())
	}
	if host == "" || host == "localhost" {
		// The client dials "localhost" and then "127.0.0.1"; binding the
		// numeric address makes the second candidate the one that answers.
		host = "127.0.0.1"
	}
	port := opts.port
	if port == 0 {
		port = int(input.GetPort())
	}

	srv, err := localharness.Listen(localharness.ServerConfig{
		BindAddress: host,
		Port:        port,
		Logger:      logger,
	})
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	// Phase 1, harness to client: the OutputConfig.
	cfg := &pb.OutputConfig{}
	cfg.SetPort(int32(srv.Port()))
	cfg.SetApiKey(srv.APIKey())
	if err := localharness.WriteOutputConfig(out, cfg); err != nil {
		return fmt.Errorf("write output config: %w", err)
	}
	logger.Debug("harness: listening", "addr", srv.Addr().String())

	return srv.Serve(ctx, func(ctx context.Context, conn *localharness.ServerConn) error {
		conv := newConversation(conn, opts, logger, input)
		return conv.serve(ctx)
	})
}

// newLogger builds the process logger. It always writes to stderr: stdout
// carries the protocol.
func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
