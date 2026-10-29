// Package rchost — remote_control_serve.go: the user-facing
// `remote-control serve` subcommand handler.
package rchost

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
	"time"

	"github.com/justwasm/pigasus/agentbrowser"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/rcfrontend"
	"github.com/justwasm/pigasus/internal/rclink"
	"github.com/justwasm/pigasus/internal/rctunnel"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/originpolicy"
	"github.com/justwasm/pigasus/remotetransport"
)

const remoteControlServeAddrDefault = "0.0.0.0:8787"

// RunServe is the `remote-control serve` subcommand's Run handler.
func RunServe(j *cli.JetskiCLI, args cli.ServeArgs) error {
	if auth.IsBusinessAccount() {
		return cli.ExitCodeError(remoteControlADCExitCode, serveADCErrorMessageForCaller())
	}
	addr, err := validateServeArgs(args)
	if err != nil {
		return err
	}
	if err := prepareServeAuth(j, args); err != nil {
		return err
	}
	announceSettingsFile(j.ErrOut)

	// The label names the subcommand the user typed: "remote-control
	// serve" for the serve subcommand, and "remote-control start" for
	// the foreground path `start` takes where there is no launchd.
	label := strings.TrimSpace(args.Label)
	if label == "" {
		label = "remote-control serve"
	}

	instanceName := persistentrc.ResolveInstanceName(args.Name)
	// The instance id is resolved here, not inside the tunnel client:
	// the local server answers GetRemoteControlInfo with it, and the
	// Web UI turns it into the browser URL
	// "{companionOrigin}/r/{instanceId}".
	//
	// It is the reference's shape — "remote-control-<uuid>-v2" — and
	// it is persisted, so a restart keeps the same dashboard record.
	instanceID := persistentrc.InstanceID()
	_, activeAccount := accounts.Current()

	// Cycle rc-auth-bearer-required: the secret a client from
	// another machine has to present. The serve command refuses a
	// loopback bind on purpose, so the surface is reachable from the
	// network and the surface has to ask for a credential.
	token, warn, err := resolveRCToken()
	if err != nil {
		return cli.ExitCodeError(1, fmt.Sprintf("Error: could not mint the remote-control secret: %v", err))
	}
	if warn != "" {
		fmt.Fprintf(j.ErrOut, "remote-control serve: %s\n", warn)
	}

	disp, srv, err := buildServeStack(addr, instanceID, token, args.Desktop)
	if err != nil {
		return err
	}

	// Launch pure Go WebChannel client connecting to Google Cloud Tunnel (jetski-webchannel.googleapis.com).
	// Zero external binary dependencies; runs as an integral part of pigasus.
	//
	// The tunnel serves the inbound proxy requests against the local
	// HTTP handler — the reference serves its tunnel traffic against
	// its own language server handler, so a request that arrives over
	// the cloud tunnel and a request that arrives over the loopback
	// socket take the identical code path.
	tunnelClient, stopTunnel := startServeTunnel(rctunnel.ClientOptions{
		InstanceName: instanceName,
		InstanceID:   instanceID,
		Account:      activeAccount,
		Dispatcher:   disp,
		Handler:      srv.Handler(),
		LogWriter:    j.ErrOut,
		DebugForward: os.Getenv("PIGASUS_DEBUG_FORWARD") != "",
	}, j.ErrOut, label)
	defer stopTunnel()

	return runServeEventLoop(srv, tunnelClient, args.Name, addr, token, j.ErrOut, label, instanceID)
}

// buildServeStack builds the session snapshot, the dispatcher and the
// local HTTP server the serve command runs.
//
// The token is the secret the surface demands from a client that is
// not on the loopback interface — cycle rc-auth-bearer-required. It
// goes to the transport (the gate) and to the web UI (the page the
// loopback peer is served).
//
// desktop asks for the reference's desktop UI: the dispatcher's
// Mendel read then carries the hub identity, so the page the daemon
// serves (and the dashboard's page behind the tunnel) is the
// desktop one. See desktopMendelUserAgent.
func buildServeStack(addr, instanceID, token string, desktop bool) (*remotecontrol.Dispatcher, *remotetransport.TCPServer, error) {
	cwd, _ := os.Getwd()
	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	sess.Workspace = cwd

	disp, err := buildServeDispatcher(cwd, addr, sess, instanceID, desktopMendelUserAgent(desktop))
	if err != nil {
		return nil, nil, err
	}
	srv, err := startServeServer(disp, addr, token)
	if err != nil {
		return nil, nil, err
	}
	disp.SetOriginPolicySetter(srv.SetOriginPolicy)
	return disp, srv, nil
}

// startServeTunnel starts the cloud tunnel client in the background
// and returns the client plus the function that stops it.
func startServeTunnel(opts rctunnel.ClientOptions, errOut io.Writer, label string) (*rctunnel.Client, func()) {
	client := rctunnel.NewClient(opts)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := client.Run(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintf(errOut, "%s: cloud tunnel client exited: %v\n", label, err)
		}
	}()
	return client, cancel
}

func validateServeArgs(args cli.ServeArgs) (string, error) {
	addr := strings.TrimSpace(args.Addr)
	if addr == "" {
		addr = remoteControlServeAddrDefault
	}
	if err := persistentrc.ValidateAddr(addr); err != nil {
		return "", cli.ExitCodeError(2, fmt.Sprintf("Error: %v", err))
	}
	if err := persistentrc.ValidateName(args.Name); err != nil {
		return "", cli.ExitCodeError(2, fmt.Sprintf("Error: %v", err))
	}
	return addr, nil
}

func prepareServeAuth(j *cli.JetskiCLI, args cli.ServeArgs) error {
	if !args.NoLogin {
		if err := ensureBearerTokenForServe(j.Out, j.ErrOut); err != nil {
			return cli.ExitCodeError(1, fmt.Sprintf("Error: %v", err))
		}
	}
	return nil
}

// activeAccountEmail returns the signed-in account's email address,
// or "" when no account is signed in. The Web UI shows the address in
// its account chip, and the clone's dispatcher surfaces it through
// GetUserStatus — so it has to be the real address, not a
// synthesised local one.
func activeAccountEmail() string {
	_, email := accounts.Current()
	return email
}

func buildServeDispatcher(cwd, addr string, sess *remotecontrol.SessionState, instanceID string, mendelUserAgent string) (*remotecontrol.Dispatcher, error) {
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
		DaemonMeta:              daemonMetaForCaller("tcp+ws", addr, "ephemeral-foreground"),
		MessageBackend:          buildHostMessageBackend(cwd, "", broker),
		Browser:                 broker,
		PermissionsStore:        buildHostPermissionStore(),
		PluginsStore:            buildHostPluginStore(),
		TriggersStore:           buildHostTriggerStore(),
		FastpushStore:           buildHostFastpushStore(),
		ResourceStore:           buildHostResourceStore(projects),
		ConversationConfigStore: buildHostConversationConfigStore(),
		TerminalsRegistry:       buildHostTerminalsRegistry(),
		WebTerminals:            buildHostWebTerminalsRegistry(),
		WebAudio:                buildHostWebAudioRegistry(),
		OriginPolicyStore:       buildHostOriginPolicyStore(),
		AccountEmail:            activeAccountEmail(),
		RemoteControlInstanceID: instanceID,
		MendelUserAgent:         mendelUserAgent,
	})
	if err != nil {
		return nil, cli.ExitCodeError(1, fmt.Sprintf("Error: dispatcher init failed: %v", err))
	}
	return disp, nil
}

func startServeServer(disp *remotecontrol.Dispatcher, addr, token string) (*remotetransport.TCPServer, error) {
	seed := originpolicy.LoadOriginPolicyFromEnv()
	_, activeAccount := accounts.Current()
	cwd, _ := os.Getwd()
	webUI := rcfrontend.Handler(rcfrontend.Options{
		EnableTouchPatches: true,
		WorkspaceRoot:      cwd,
		AccountEmail:       activeAccount,
		CSRFToken:          token,
	})
	srv, err := remotetransport.NewTCPServer(context.Background(), disp, remotetransport.TCPConfig{
		Addr:         addr,
		Path:         "/rpc",
		OriginPolicy: &seed,
		WebUIHandler: webUI,
		CSRFToken:    token,
	})
	if err != nil {
		return nil, cli.ExitCodeError(1, fmt.Sprintf("Error: failed to bind %s: %v", addr, err))
	}
	return srv, nil
}

// runServeEventLoop waits for the interrupt signal and shuts the stack
// down.
//
// instanceID is the id the tunnel *registered* with. The link that is
// printed has to name that exact id: the cloud's registry keys its
// rows by it, so a link naming any other id resolves to "No instance
// found". It is passed in rather than re-derived — a second derivation
// would be a second id whenever the installation's uuid could not be
// persisted.
func runServeEventLoop(srv *remotetransport.TCPServer, tunnelClient *rctunnel.Client, name, addr, token string, errOut io.Writer, label, instanceID string) error {
	sigCh := make(chan os.Signal, 1)
	serveSignalChProvider(sigCh)
	defer signal.Stop(sigCh)

	if label == "" {
		label = "remote-control serve"
	}
	fmt.Fprintf(errOut, "%s: listening on %s (path=/rpc, name=%s, antigravity_home=%s)\n",
		label, srv.HTTPURL(), name, auth.AntigravityHome())
	if webURL := srv.WebURL(); webURL != "" {
		fmt.Fprintf(errOut, "%s: web UI available at %s\n", label, webURL)
	}
	// The link that reaches *this* instance from another device — the
	// account chooser wrapping the deep link, so the browser picks the
	// account the tunnel registered with. See internal/rclink.
	fmt.Fprintf(errOut, "%s: open %s on another device to take over.\n",
		label, rclink.BrowserURL(instanceID, ""))
	// Cycle rc-auth-bearer-required: the secret a client from another
	// machine has to present. The URL carries it, so the operator can
	// hand one line to somebody else.
	announceRCToken(errOut, label, addr, token)

	sig := <-sigCh
	fmt.Fprintf(errOut, "%s: caught signal %v, shutting down\n", label, sig)

	if tunnelClient != nil {
		tunnelClient.Stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(errOut, "remote-control serve: shutdown error: %v\n", err)
		return cli.ExitCodeError(1, fmt.Sprintf("Error: shutdown failed: %v", err))
	}
	return nil
}

func serveADCErrorMessageForCaller() string {
	return "error: remote-control serve cannot be used with Application Default Credentials " +
		"(AGY_ADC_AUTH) sign-in; Remote Control does not support ADC yet — " +
		"sign out with /logout and unset AGY_ADC_AUTH (or use a different sign-in method) and retry."
}

func ensureBearerTokenForServe(out, err io.Writer) error {
	if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil && tok.Token.AccessToken != "" {
		fmt.Fprintf(out, "using existing token at %s\n", auth.TokenFilePath())
		return nil
	}
	fmt.Fprintln(out, "no bearer token on disk; running OAuth login inline")
	fmt.Fprintln(out, "")
	return cli.RunDeviceFlowLogin(context.Background(), out, err)
}

func announceSettingsFile(out io.Writer) {
	settingsPath := filepath.Join(auth.AntigravityHome(), "settings.json")
	st, statErr := os.Stat(settingsPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			fmt.Fprintf(out, "settings.json: not found at %s (the dashboard will use defaults)\n", settingsPath)
			return
		}
		fmt.Fprintf(out, "settings.json: probe failed at %s: %v\n", settingsPath, statErr)
		return
	}
	fmt.Fprintf(out, "settings.json: loaded from %s (%d bytes)\n", settingsPath, st.Size())
}

var serveSignalChProvider = serveSignalChFromNotify

func serveSignalChFromNotify(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
}

var _ = flag.NewFlagSet
