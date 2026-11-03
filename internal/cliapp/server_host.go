// Package cliapp — server_host.go: the `pigasus server` subcommand.
//
// The server is the host half of the client/server split: it owns the
// workspace, the tools and the credentials, and it serves the turns a
// client asks for over a websocket.
//
// The server always demands a bearer token. It runs shell commands
// against its own workspace, so an unauthenticated peer is a peer with
// the machine's shell — the token is the only thing between them. A
// token is minted when the command line does not name one, and it is
// printed when the server starts.
//
// The bind address is the server's own fact: the loopback on a fixed
// port, because the server must not listen on the network unless the
// user asks for it. The host supplies the address, the host and the
// port the user gave; the server decides what they mean
// (internal/app/agentserver).
package cliapp

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/justwasm/pigasus/internal/app/agentserver"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/agents"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
)

// serverFlagSet builds the flag set the server's own options live in.
//
// The server is a command of its own, but the transport and the agent
// are resolved through the same flag names the root command uses, so
// the same helpers read them.
func serverFlagSet(args cli.ServerArgs) *flag.FlagSet {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.String("backend", strings.TrimSpace(args.Backend), "")
	fs.String("agent", strings.TrimSpace(args.Agent), "")
	return fs
}

// runServer hosts the agent and serves it over a websocket.
func runServer(j *cli.JetskiCLI, args cli.ServerArgs) error {
	addr := agentserver.ResolveAddr(args.Addr, args.Host, args.Port)

	workspace := strings.TrimSpace(args.Workspace)
	if workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("server: cannot read the working directory: %w", err)
		}
		workspace = wd
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("server: cannot resolve %q: %w", workspace, err)
	}

	model := strings.TrimSpace(args.Model)
	if model == "" {
		model = defaultModel
	}

	out := io.Writer(os.Stdout)
	if j != nil && j.Out != nil {
		out = j.Out
	}

	fs := serverFlagSet(args)
	srv, err := agentserver.New(agentserver.Config{
		Addr:            addr,
		Token:           strings.TrimSpace(args.Token),
		Workspace:       abs,
		Model:           model,
		Effort:          strings.TrimSpace(args.Effort),
		PermissionMode:  strings.TrimSpace(args.Mode),
		SkipPermissions: args.SkipPermissions,
		AddDirs:         args.AddDirs,
		ACPVersion:      strings.TrimSpace(args.ACPVersion),
		AgentBody:       agents.AgentBody(agents.CustomAgentFromFlags(fs)),
		BackendFor:      func(name string) backend.Client { return backendselect.Pick(fs, name, modelpicker.ResolveCatalogueID) },
		Log:             out,
	})
	if err != nil {
		return err
	}

	ln, err := srv.Listen()
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "pigasus server listening on ws://%s\n", srv.Addr())
	fmt.Fprintf(out, "protocols: acp (ws://%s/acp, default), agentlink (ws://%s/, legacy)\n", srv.Addr(), srv.Addr())
	fmt.Fprintf(out, "workspace: %s\n", abs)
	fmt.Fprintf(out, "model: %s\n", model)
	fmt.Fprintf(out, "token: %s\n", srv.Token())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Serve(ctx, ln)
}
