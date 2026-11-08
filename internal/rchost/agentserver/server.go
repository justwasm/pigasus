// Package agentserver runs the local Antigravity remote-agent proxy.
//
// The same server backs both entry points:
//
//	pigasus agent-server [flags]
//	mock-remote-agent [flags]   (standalone compatibility binary)
//
// It speaks the REST/SSE dialect agy's --remote-agent client uses, and by
// default executes turns through Pigasus's own agent runtime.
package agentserver

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/infra/mockremoteagent"
)

// Config is the agent-server's command line.
type Config struct {
	// Addr is the bind address ("127.0.0.1:8080").
	Addr string
	// Model is the Pigasus-side model that executes the turn. It accepts
	// a reference id/label or a model configured in providers.json.
	Model string
	// Workspace overrides the workspace URI the remote client sends.
	Workspace string
	// CloudCodeEndpoint overrides the reference endpoint for reference
	// models.
	CloudCodeEndpoint string
	// RealAgent runs the full Pigasus agent; false keeps the protocol
	// stub used by wire tests.
	RealAgent bool
	// Verbose logs every HTTP/RPC request.
	Verbose bool
}

// DefaultModel is the model the server runs when --model is not given.
func DefaultModel() string {
	if m := strings.TrimSpace(os.Getenv("OPENAGY_MODEL")); m != "" {
		return m
	}
	return "gemini-3.8-flash-high"
}

// newFlagSet defines the agent-server flags. ParseConfig and Usage share it
// so the help text cannot drift from the parser.
func newFlagSet(out io.Writer, cfg *Config) *flag.FlagSet {
	if out == nil {
		out = io.Discard
	}
	fs := flag.NewFlagSet("agent-server", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&cfg.Addr, "addr", "127.0.0.1:8080", "bind address for the agent server")
	fs.StringVar(&cfg.Model, "model", DefaultModel(), "Pigasus-side model for agent execution (reference id/label or a configured model)")
	fs.StringVar(&cfg.Workspace, "workspace", "", "override workspace directory (defaults to client URI or cwd)")
	fs.StringVar(&cfg.CloudCodeEndpoint, "cloudcode-endpoint", "", "CloudCode endpoint for reference models (default: daily-cloudcode-pa.googleapis.com)")
	fs.BoolVar(&cfg.RealAgent, "real-agent", true, "enable full Pigasus agent runtime (set false for the simulated turn stub)")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "log all HTTP/RPC requests")
	return fs
}

// ParseConfig parses the agent-server flags.
func ParseConfig(args []string, out io.Writer) (Config, error) {
	cfg := Config{}
	fs := newFlagSet(out, &cfg)
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Start builds and starts the server. The caller owns Close.
func Start(cfg Config) (*mockremoteagent.Server, error) {
	// Clear loopback intercept env vars for the server process so its
	// internal backends connect to their true upstreams.
	if u := os.Getenv("CLOUD_CODE_URL"); isLoopback(u, cfg.Addr) {
		_ = os.Unsetenv("CLOUD_CODE_URL")
	}
	if u := os.Getenv("AICODE_ENDPOINT_URL"); isLoopback(u, cfg.Addr) {
		_ = os.Unsetenv("AICODE_ENDPOINT_URL")
	}

	opts := []mockremoteagent.Option{mockremoteagent.WithAddr(cfg.Addr)}
	if cfg.Verbose {
		opts = append(opts, mockremoteagent.WithRequestLogger(func(r *http.Request, method string, body []byte) {
			log.Printf("[MockServer] %s %s (headers: %v, method: %s, body: %s)", r.Method, r.URL.RequestURI(), r.Header, method, string(body))
		}))
	}
	if cfg.RealAgent {
		opts = append(opts, mockremoteagent.WithCustomTurnHandler(
			NewPigasusAgentTurnHandler(AgentHandlerConfig{
				Model:             cfg.Model,
				Workspace:         cfg.Workspace,
				CloudCodeEndpoint: cfg.CloudCodeEndpoint,
			}),
		))
	}
	return mockremoteagent.NewServer(opts...)
}

// Serve starts the server, prints the client instructions, and blocks until
// ctx is cancelled.
func Serve(ctx context.Context, out io.Writer, cfg Config) error {
	if out == nil {
		out = io.Discard
	}
	srv, err := Start(cfg)
	if err != nil {
		return fmt.Errorf("failed to start mock remote agent server: %w", err)
	}
	defer srv.Close()

	mode := "Real Pigasus Agent (rchost/actor/tools)"
	if !cfg.RealAgent {
		mode = "Simulated Protocol Stub"
	}
	fmt.Fprintf(out, "Mock Remote Agent Server running at %s\n", srv.URL())
	fmt.Fprintf(out, "Execution Engine: %s (model: %s)\n", mode, cfg.Model)
	fmt.Fprintln(out, "Ready to accept agy --remote-agent client requests on:")
	fmt.Fprintf(out, "  %s/google.gca.aicode.v1main.JetskiRemoteAgentProxyService/\n", srv.URL())
	fmt.Fprintln(out, "Press Ctrl+C to stop.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Launch the reference client with BOTH variables set:")
	fmt.Fprintf(out, "  env AICODE_ENDPOINT_URL=%s CLOUD_CODE_URL=%s agy --remote-agent\n", srv.URL(), srv.URL())
	fmt.Fprintln(out)
	fmt.Fprintln(out, "CLOUD_CODE_URL is required: without it agy resolves the cloud project")
	fmt.Fprintln(out, "against daily-cloudcode-pa.googleapis.com and reports")
	fmt.Fprintln(out, "\"cloud conversations are not enabled for this user\".")
	fmt.Fprintf(out, "pigasus --remote-agent only needs AICODE_ENDPOINT_URL=%s\n", srv.URL())

	<-ctx.Done()
	fmt.Fprintln(out, "\nShutting down Mock Remote Agent Server...")
	return nil
}

// RunCLI is the subcommand/standalone entry point: parse, serve, and turn
// Ctrl+C into a clean shutdown.
func RunCLI(out io.Writer, args []string) error {
	cfg, err := ParseConfig(args, out)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Serve(ctx, out, cfg)
}

// Main runs the standalone compatibility binary and returns its exit code.
func Main(args []string) int {
	if err := RunCLI(os.Stdout, args); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, err)
		}
		return 1
	}
	return 0
}

// Usage is the subcommand help block.
func Usage(command string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s [flags]\n\n", command)
	b.WriteString("Run the local Antigravity remote-agent proxy. The server speaks the\n")
	b.WriteString("google.gca.aicode.v1main.JetskiRemoteAgentProxyService REST/SSE dialect and\n")
	b.WriteString("executes turns through Pigasus's agent runtime by default.\n\nFlags:\n")
	var cfg Config
	newFlagSet(&b, &cfg).PrintDefaults()
	return b.String()
}

// isLoopback reports whether url points at the local server address.
func isLoopback(url, addr string) bool {
	if url == "" {
		return false
	}
	return strings.Contains(url, addr) ||
		strings.Contains(url, "127.0.0.1") ||
		strings.Contains(url, "localhost")
}

func init() {
	subcommands.RegisterSubcommand(cliflags.SubcommandSpec{
		Name:    "agent-server",
		Aliases: []string{"mock-remote-agent"},
		Short:   "Run the local Antigravity remote-agent proxy",
		HelpFn:  func(prog string) string { return Usage(prog + " agent-server") },
		Run:     RunCLI,
	})
}
