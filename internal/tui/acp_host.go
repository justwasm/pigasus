// Package cliapp — acp_host.go: the `pigasus acp` subcommand host implementation.
//
// Implements the Agent Client Protocol (ACP) server over stdio or TCP,
// conforming to the official specification and Google's antigravity-acp registry entry:
// https://github.com/agentclientprotocol/registry/blob/main/antigravity-acp/agent.json
package tui

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/justwasm/pigasus/internal/app/agentserver"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/backend"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
)

type acpHostOpts struct {
	stdio           bool
	addr            string
	port            int
	workspace       string
	model           string
	effort          string
	token           string
	skipPermissions bool
}

func parseACPFlags(rawArgs []string) acpHostOpts {
	fs := flag.NewFlagSet("acp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts acpHostOpts
	fs.BoolVar(&opts.stdio, "stdio", false, "Serve ACP over standard I/O")
	fs.StringVar(&opts.addr, "addr", "", "TCP bind address (e.g. 127.0.0.1:0)")
	fs.StringVar(&opts.addr, "acp-addr", "", "TCP bind address alias")
	fs.IntVar(&opts.port, "port", 0, "TCP port override")
	fs.StringVar(&opts.workspace, "workspace", "", "Root workspace directory")
	fs.StringVar(&opts.workspace, "w", "", "Root workspace directory alias")
	fs.StringVar(&opts.model, "model", "", "Model name")
	fs.StringVar(&opts.effort, "effort", "", "Reasoning effort")
	fs.StringVar(&opts.token, "token", "", "Bearer token")
	fs.BoolVar(&opts.skipPermissions, "dangerously-skip-permissions", false, "Auto-approve all tool permission requests")
	fs.BoolVar(&opts.skipPermissions, "skip-permissions", false, "Auto-approve all tool permission requests alias")

	// Google's ACP registry entry specifies "--uid=" on Linux
	var uid string
	fs.StringVar(&uid, "uid", "", "User identity token (Google ACP compat)")

	_ = fs.Parse(rawArgs)
	return opts
}

// runAcpHandler hosts the ACP agent over stdio or TCP.
func runAcpHandler(j *cli.JetskiCLI, args cli.ACPArgs) error {
	opts := parseACPFlags(args.RawArgs)

	if args.Addr != "" && opts.addr == "" {
		opts.addr = args.Addr
	}
	if args.Model != "" && opts.model == "" {
		opts.model = args.Model
	}
	if args.Workspace != "" && opts.workspace == "" {
		opts.workspace = args.Workspace
	}

	workspace := strings.TrimSpace(opts.workspace)
	if workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("acp: cannot read working directory: %w", err)
		}
		workspace = wd
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("acp: cannot resolve workspace %q: %w", workspace, err)
	}

	model := strings.TrimSpace(opts.model)
	if model == "" {
		model = defaultModel
	}

	in := args.In
	if in == nil {
		in = os.Stdin
	}
	out := args.Out
	if out == nil {
		out = os.Stdout
	}
	errOut := args.Err
	if errOut == nil {
		errOut = os.Stderr
	}

	ctx := args.Ctx
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
	}

	// In stdio mode, log output MUST go to stderr so it does not pollute the JSON-RPC wire on stdout.
	logWriter := errOut

	srv, err := agentserver.New(agentserver.Config{
		Workspace:       absWorkspace,
		Model:           model,
		Effort:          strings.TrimSpace(opts.effort),
		Token:           strings.TrimSpace(opts.token),
		SkipPermissions: opts.skipPermissions,
		BackendFor:      func(name string) backend.Client { return backendselect.Pick(nil, name, modelpicker.ResolveCatalogueID) },
		Log:             logWriter,
	})
	if err != nil {
		return fmt.Errorf("acp: create server: %w", err)
	}

	// If a network address or port is requested, listen on TCP.
	if opts.addr != "" || opts.port > 0 {
		addr := opts.addr
		if opts.port > 0 {
			addr = fmt.Sprintf("127.0.0.1:%d", opts.port)
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("acp: listen %s: %w", addr, err)
		}
		defer ln.Close()

		fmt.Fprintf(errOut, "acp listening: %s\n", ln.Addr().String())

		go func() {
			<-ctx.Done()
			ln.Close()
		}()

		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			go func(c net.Conn) {
				_ = srv.ServeACPConn(ctx, c)
			}(conn)
		}
	}

	// Default to stdio mode (the standard invocation by Zed and other ACP clients).
	return srv.ServeACP(ctx, out, in)
}

// RunAcpStandalone runs the ACP server standalone (e.g. for external wrappers or test helpers).
func RunAcpStandalone(rawArgs []string, in io.Reader, out, errOut io.Writer) error {
	return runAcpHandler(nil, cli.ACPArgs{
		In:      in,
		Out:     out,
		Err:     errOut,
		RawArgs: rawArgs,
	})
}
