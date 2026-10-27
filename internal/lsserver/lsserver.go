// Package lsserver is the standalone language-server process: the
// clone's rebuild of the Antigravity IDE's `language_server` binary.
//
// The reference is the second binary of the family this repository
// clones (out/ide-language-server/): the Antigravity IDE ships it,
// built from the same monorepo commit as the `agy` CLI. It owns the
// IDE's whole product surface — the web UI, the `LanguageServerService`
// the page talks to, the model catalogue, the conversations — and it
// is the process the IDE spawns, reads its port from and then talks
// Connect-RPC to.
//
// What this package reproduces, and what it does not:
//
//   - the flag surface, byte for byte (`--help`);
//   - the `-stamp` answer;
//   - the startup log, line for line;
//   - the on-disk layout: the two config roots, the migration
//     marker, the bundled skills, the discovery file;
//   - the two listeners (plain HTTP and TLS) and the routes on them:
//     the Connect-RPC service, the WebSocket endpoint, the web UI,
//     `/healthz`;
//   - the mocked catalogue `-use_mocked_data=true` asks for.
//
// The clone's own agent kernel answers the service — the Web UI's own
// RPCs live in internal/remotecontrol — so the catalogue and the
// conversations are the clone's, not a second implementation.
//
// Evidence grade: 一手 for everything below, captured against the
// reference binary on this machine (out/langserver/).
package lsserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/google/uuid"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/rcfrontend"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/remotetransport"
)

// Version is the reference language server's own version.
//
// The reference prints it in the startup log and carries it in the
// discovery file. The clone reports the reference's version, the way
// `cli.ReferenceVersion` reports the reference CLI's version.
const Version = "1.11.0"

// lsVersion is the version the process reports.
//
// The reference's own version is the default; `--override_ide_version`
// wins over it, which is how the IDE reports its own version (the
// IDE passes `--override_ide_version 2.26.0`, and the reference then
// logs "Language server version: 2.26.0").
func lsVersion(f *Flags) string {
	if v := f.OverrideIDEVersion; v != "" {
		return v
	}
	return Version
}

// ideVersion is the version the reference's web page reports.
//
// 【一手】 the page's `appVersion` is the `--override_ide_version`
// value, and "0.0.0.0" when that flag was not passed — *not* the
// language server's own version, which the log line reports.
func ideVersion(f *Flags) string {
	if v := f.OverrideIDEVersion; v != "" {
		return v
	}
	return "0.0.0.0"
}

// Run starts the language server and blocks until ctx is done.
//
// The order of the log lines is the reference's: the process announces
// itself, caps GOMAXPROCS, names its version, says which host it
// will listen on, then reports the two ports, then writes the
// discovery file and reports that it is up.
func Run(ctx context.Context, f *Flags, stdout, stderr io.Writer) error {
	started := time.Now()
	log := NewLogger(stderr)
	const thread = 1

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Infof(thread, locStartPID, "Starting language server process with pid %d", os.Getpid())
	applyGoMaxProcs(f.LimitGoMaxProcs)
	log.Infof(thread, locGoMaxProcs, "Setting GOMAXPROCS to %d", runtime.GOMAXPROCS(0))
	log.Infof(thread, locVersion, "Language server version: %s", lsVersion(f))
	log.Infof(thread, locListenHost, "Language server will attempt to listen on host localhost")

	layout, err := ResolveLayout(f)
	if err != nil {
		return err
	}
	applyAppDataHome(layout)

	disp, err := buildDispatcher(f, layout)
	if err != nil {
		return err
	}

	servers, err := startListeners(ctx, disp, f, layout)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = servers.Shutdown(shutdownCtx)
	}()

	log.Infof(thread, locListenHTTPS, "Language server listening on %s port at %d for HTTPS (gRPC)", portKind(f.HTTPSServerPort), servers.HTTPSPort())
	log.Infof(thread, locListenHTTP, "Language server listening on %s port at %d for HTTP", portKind(f.HTTPServerPort), servers.HTTPPort())

	if err := layout.Prepare(log, thread); err != nil {
		return err
	}
	if err := writeDiscovery(f, layout, servers, log, thread); err != nil {
		return err
	}
	log.Infof(thread, locInitialized, "initialized server successfully in %s", time.Since(started))

	return waitForShutdown(ctx, cancel, f)
}

// startListeners brings the two listeners up.
//
// The reference asks the secret of every service call once the IDE
// configured one — loopback included — and serves its page without it.
func startListeners(ctx context.Context, disp remotetransport.Dispatcher, f *Flags, layout Layout) (*Servers, error) {
	return StartServers(ctx, disp, ServerOptions{
		HTTPAddr:                  listenAddr(f.HTTPServerPort),
		HTTPSAddr:                 listenAddr(f.HTTPSServerPort),
		WebUI:                     webUIHandler(f, layout),
		Healthz:                   healthzHandler(),
		CSRFToken:                 f.CSRFToken,
		RequireTokenOnLoopback:    f.CSRFToken != "",
		AllowUnauthenticatedWebUI: true,
		ReferenceTokenGate:        true,
	})
}

// writeDiscovery writes the discovery file in the persistent mode.
//
// The reference writes it only in that mode, and reports the path it
// wrote.
func writeDiscovery(f *Flags, layout Layout, servers *Servers, log *Logger, thread int) error {
	if !f.PersistentMode {
		return nil
	}
	path, err := layout.WriteDiscovery(f.WorkspaceID, Discovery{
		PID:       os.Getpid(),
		HTTPSPort: servers.HTTPSPort(),
		HTTPPort:  servers.HTTPPort(),
		LSPPort:   f.LSPPort,
		LSVersion: lsVersion(f),
		CSRFToken: f.CSRFToken,
	})
	if err != nil {
		return err
	}
	log.Infof(thread, locDiscoveryFile, "Wrote discovery file: %s", path)
	return nil
}

// buildDispatcher builds the service the server answers with.
//
// The clone's own `LanguageServerService` does the work; the language
// server wraps it with the answers the reference derives from its
// flags and with the mocked catalogue.
func buildDispatcher(f *Flags, layout Layout) (remotetransport.Dispatcher, error) {
	cwd, _ := os.Getwd()
	mendel := ""
	if f.SubclientType == "hub" {
		mendel = auth.MendelHubUserAgent(cli.ReferenceVersion)
	}
	inner, err := rchost.NewStandaloneDispatcher(cwd, listenAddr(f.HTTPServerPort), "", mendel)
	if err != nil {
		return nil, err
	}
	return NewDispatcher(inner, f, layout, f.UseMockedData), nil
}

// webUIHandler returns the handler that serves the web UI.
//
// The reference serves the page when the process is the IDE's hub
// client (`--subclient_type hub`): the IDE loads it from
// `https://127.0.0.1:<port>/`. Every other mode answers 404 on every
// path but the service itself.
func webUIHandler(f *Flags, layout Layout) http.Handler {
	if f.SubclientType != "hub" {
		return http.HandlerFunc(notFound)
	}
	cwd, _ := os.Getwd()
	_, account := accounts.Current()
	return rcfrontend.Handler(rcfrontend.Options{
		// The reference's own page: no touch patches, the
		// "Antigravity" title and the one-line configuration script.
		// The account switcher is the clone's own widget and the
		// reference has none.
		EnableTouchPatches:     false,
		WorkspaceRoot:          cwd,
		ProductName:            "antigravity-ide",
		AppVersion:             ideVersion(f),
		AccountEmail:           account,
		CSRFToken:              f.CSRFToken,
		ReferencePage:          true,
		DisableAccountSwitcher: true,
	})
}

// notFound is the reference's answer for a path it does not serve.
func notFound(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "404 page not found", http.StatusNotFound)
}

// healthzHandler answers the reference's /healthz route.
//
// The reference's answer carries the process's instance id and the
// word "ok".
func healthzHandler() http.Handler {
	instanceID := uuid.NewString()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"instanceId":%q,"status":"ok"}`, instanceID)
	})
}

// applyGoMaxProcs caps GOMAXPROCS at the flag's value.
//
// The reference's message names the value it settled on.
func applyGoMaxProcs(limit int) {
	if limit <= 0 {
		return
	}
	if runtime.GOMAXPROCS(0) > limit {
		runtime.GOMAXPROCS(limit)
	}
}

// listenAddr is the bind address for a port. Port 0 means "the OS
// picks one", which is the reference's own meaning for it.
func listenAddr(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}

// portKind names the port the way the reference's log does.
//
// A port the operator asked for is a *fixed* port, a port the OS
// picked is a *random* one. The reference's flag help says the same:
// "0 means random".
//
// 【一手】 the reference's two lines:
//
//	server.go:687] Language server listening on fixed port at 8802 for HTTPS (gRPC)
//	server.go:687] Language server listening on random port at 55660 for HTTPS (gRPC)
func portKind(port int) string {
	if port == 0 {
		return "random"
	}
	return "fixed"
}

// applyAppDataHome points the clone's own home at the language
// server's application data root.
//
// The reference keeps its whole state under the app data dir — the
// conversations, the brain, the projects — so `-gemini_dir` moves all
// of it. The clone's home is the same knob (`ANTIGRAVITY_HOME`), and
// it is only set when the environment did not already choose one.
func applyAppDataHome(layout Layout) {
	if os.Getenv("ANTIGRAVITY_HOME") != "" {
		return
	}
	_ = os.Setenv("ANTIGRAVITY_HOME", layout.AppDataDir)
}

// waitForShutdown blocks until the process is asked to stop.
//
// Two signals end the process: the context (a signal handler in the
// caller) and, with `--exit_on_stdin_close`, the end of standard
// input. The second one is how the IDE ties the language server's
// lifetime to its own pipe — the reference's own help says so.
func waitForShutdown(ctx context.Context, cancel context.CancelFunc, f *Flags) error {
	if f.ExitOnStdinClose {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			cancel()
		}()
	}
	<-ctx.Done()
	return nil
}
