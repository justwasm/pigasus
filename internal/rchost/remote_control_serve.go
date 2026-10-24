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

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/originpolicy"
	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/remotetransport"
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

	cwd, _ := os.Getwd()
	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	sess.Workspace = cwd

	disp, err := buildServeDispatcher(cwd, addr, sess)
	if err != nil {
		return err
	}
	srv, err := startServeServer(disp, addr)
	if err != nil {
		return err
	}
	disp.SetOriginPolicySetter(srv.SetOriginPolicy)
	return runServeEventLoop(srv, args.Name, j.ErrOut)
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

func buildServeDispatcher(cwd, addr string, sess *remotecontrol.SessionState) (*remotecontrol.Dispatcher, error) {
	fsProvider := buildHostFSProvider(nil, cwd)
	projects := buildHostProjectStore(fsProvider)
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
		MessageBackend:          buildHostMessageBackend(cwd, ""),
		PermissionsStore:        buildHostPermissionStore(),
		PluginsStore:            buildHostPluginStore(),
		TriggersStore:           buildHostTriggerStore(),
		FastpushStore:           buildHostFastpushStore(),
		ResourceStore:           buildHostResourceStore(projects),
		ConversationConfigStore: buildHostConversationConfigStore(),
		TerminalsRegistry:       buildHostTerminalsRegistry(),
		OriginPolicyStore:       buildHostOriginPolicyStore(),
	})
	if err != nil {
		return nil, cli.ExitCodeError(1, fmt.Sprintf("Error: dispatcher init failed: %v", err))
	}
	return disp, nil
}

func startServeServer(disp *remotecontrol.Dispatcher, addr string) (*remotetransport.TCPServer, error) {
	seed := originpolicy.LoadOriginPolicyFromEnv()
	srv, err := remotetransport.NewTCPServer(context.Background(), disp, remotetransport.TCPConfig{
		Addr:         addr,
		Path:         "/rpc",
		OriginPolicy: &seed,
	})
	if err != nil {
		return nil, cli.ExitCodeError(1, fmt.Sprintf("Error: failed to bind %s: %v", addr, err))
	}
	return srv, nil
}

func runServeEventLoop(srv *remotetransport.TCPServer, name string, errOut io.Writer) error {
	sigCh := make(chan os.Signal, 1)
	serveSignalChProvider(sigCh)
	defer signal.Stop(sigCh)

	fmt.Fprintf(errOut, "remote-control serve: listening on %s (path=/rpc, name=%s, antigravity_home=%s)\n",
		srv.HTTPURL(), name, auth.AntigravityHome())

	sig := <-sigCh
	fmt.Fprintf(errOut, "remote-control serve: caught signal %v, shutting down\n", sig)

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
