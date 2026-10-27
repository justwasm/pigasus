// Package rchost — remote_control_foreground.go: the hidden
// `remote-control-foreground` subcommand the LaunchAgent plist invokes.
package rchost

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/justwasm/pigasus/agentbrowser"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/originpolicy"
	"github.com/justwasm/pigasus/remotetransport"
)

// RunForeground is the `remote-control-foreground` hidden subcommand's Run handler.
func RunForeground(j *cli.JetskiCLI, args []string) error {
	addr, name, logFile, model, desktop, err := parseForegroundFlags(args)
	if err != nil {
		return err
	}
	setupForegroundLogging(logFile, j.ErrOut)
	if err := validateForegroundArgs(name, addr); err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	sess.Workspace = cwd
	sess.Model = model

	// Cycle rc-auth-bearer-required: the secret a client from another
	// machine has to present. The persistent daemon's output goes to
	// a log file, so the operator is told where the secret is kept
	// rather than what it is.
	token, warn, err := resolveRCToken()
	if err != nil {
		return cli.ExitCodeError(1, fmt.Sprintf("Error: could not mint the remote-control secret: %v", err))
	}
	if warn != "" {
		fmt.Fprintf(j.ErrOut, "remote-control-foreground: %s\n", warn)
	}

	disp, err := buildForegroundDispatcher(cwd, model, addr, sess, desktop)
	if err != nil {
		return err
	}
	srv, err := startForegroundServer(disp, addr, token)
	if err != nil {
		return err
	}
	disp.SetOriginPolicySetter(srv.SetOriginPolicy)
	return runForegroundEventLoop(srv, name)
}

func parseForegroundFlags(args []string) (string, string, string, string, bool, error) {
	fs := flag.NewFlagSet("remote-control-foreground", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "", "Bind address for the JSON-RPC-over-WebSocket transport (host:port)")
	name := fs.String("name", "", "Machine instance label (matches the launchd label suffix)")
	logFile := fs.String("log-file", "", "Absolute log file path for stdout / stderr")
	model := fs.String("model", "", "Default model label forwarded to PiagentBackend; empty uses the host's resolved model")
	desktop := fs.Bool("desktop", false, "Serve the desktop UI: the Mendel read carries the hub identity")
	if err := fs.Parse(args); err != nil {
		return "", "", "", "", false, cli.ExitCodeError(2, fmt.Sprintf("Error: bad remote-control-foreground flags: %v", err))
	}
	return *addr, *name, *logFile, *model, *desktop, nil
}

func setupForegroundLogging(logFile string, errOut io.Writer) {
	if logFile != "" {
		if err := redirectToLogFile(logFile); err != nil {
			fmt.Fprintf(errOut, "remote-control-foreground: failed to open log file %s: %v\n", logFile, err)
		}
	}
}

func validateForegroundArgs(name, addr string) error {
	if name == "" {
		return cli.ExitCodeError(2, "Error: --name is required for remote-control-foreground")
	}
	if addr == "" {
		return cli.ExitCodeError(2, "Error: --addr is required for remote-control-foreground")
	}
	if err := persistentrc.ValidateAddr(addr); err != nil {
		return cli.ExitCodeError(2, fmt.Sprintf("Error: %v", err))
	}
	if err := persistentrc.ValidateName(name); err != nil {
		return cli.ExitCodeError(2, fmt.Sprintf("Error: %v", err))
	}
	return nil
}

// buildForegroundDispatcher builds the dispatcher the persistent
// daemon answers its RPCs with.
//
// desktop asks for the reference's desktop UI: the dispatcher's
// Mendel read then carries the hub identity, so the dashboard's page
// behind the tunnel is the desktop one. The flag arrives from the
// LaunchAgent plist's ProgramArguments (the persistentrc spec's
// Desktop field) or from the command line.
func buildForegroundDispatcher(cwd, model, addr string, sess *remotecontrol.SessionState, desktop bool) (*remotecontrol.Dispatcher, error) {
	fsProvider := buildHostFSProvider(nil, cwd)
	projects := buildHostProjectStore(fsProvider)
	// One broker for the whole daemon: the dispatcher's three
	// LanguageServerService RPCs and the turn's `webview_browser` tool
	// have to meet in the same instance.
	broker := agentbrowser.NewBroker()
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session:                 sess,
		Agents:                  remotecontrol.DefaultAgentLister,
		Reader:                  remotecontrol.ConvstoreOutputReader{},
		ListBackend:             buildHostListBackend(),
		FS:                      fsProvider,
		Projects:                projects,
		Conversations:           buildHostConversationStore(projects),
		AuthState:               authStateForCaller(),
		DaemonMeta:              daemonMetaForCaller("tcp+ws", addr, "persistent"),
		MessageBackend:          buildHostMessageBackend(cwd, model, broker),
		Browser:                 broker,
		ResourceStore:           buildHostResourceStore(projects),
		ConversationConfigStore: buildHostConversationConfigStore(),
		TerminalsRegistry:       buildHostTerminalsRegistry(),
		WebTerminals:            buildHostWebTerminalsRegistry(),
		WebAudio:                buildHostWebAudioRegistry(),
		OriginPolicyStore:       buildHostOriginPolicyStore(),
		MendelUserAgent:         desktopMendelUserAgent(desktop),
	})
	if err != nil {
		return nil, cli.ExitCodeError(1, fmt.Sprintf("Error: dispatcher init failed: %v", err))
	}
	return disp, nil
}

func startForegroundServer(disp *remotecontrol.Dispatcher, addr, token string) (*remotetransport.TCPServer, error) {
	seed := originpolicy.LoadOriginPolicyFromEnv()
	srv, err := remotetransport.NewTCPServer(context.Background(), disp, remotetransport.TCPConfig{
		Addr:         addr,
		Path:         "/rpc",
		OriginPolicy: &seed,
		CSRFToken:    token,
	})
	if err != nil {
		return nil, cli.ExitCodeError(1, fmt.Sprintf("Error: failed to bind %s: %v", addr, err))
	}
	return srv, nil
}

func runForegroundEventLoop(srv *remotetransport.TCPServer, name string) error {
	sigCh := make(chan os.Signal, 1)
	signalChProvider(sigCh)
	defer signal.Stop(sigCh)

	fmt.Fprintf(os.Stderr, "remote-control-foreground: listening on %s (path=/rpc, name=%s, antigravity_home=%s)\n",
		srv.HTTPURL(), name, auth.AntigravityHome())
	announceRCSecretPath(os.Stderr, "remote-control-foreground")

	sig := <-sigCh
	fmt.Fprintf(os.Stderr, "remote-control-foreground: caught signal %v, shutting down\n", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "remote-control-foreground: shutdown error: %v\n", err)
		return cli.ExitCodeError(1, fmt.Sprintf("Error: shutdown failed: %v", err))
	}
	return nil
}

func redirectToLogFile(logFile string) error {
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := unix.Dup2(int(f.Fd()), int(os.Stdout.Fd())); err != nil {
		_ = f.Close()
		return err
	}
	if err := unix.Dup2(int(f.Fd()), int(os.Stderr.Fd())); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()
	return nil
}

var signalChProvider = signalChFromNotify

func signalChFromNotify(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
}

var _ = convstore.ConversationBoundToProject
