// Package rchost — web_handlers.go: reusable http.Handler constructors for
// `pigasus web`, providing standalone UI-only, API-only, and combined web handlers.
package rchost

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/infra/refbackend"
	"github.com/justwasm/pigasus/internal/rcfrontend"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/originpolicy"
	"github.com/justwasm/pigasus/remotetransport"
)

// WebHandlerOptions configures the HTTP handlers for pigasus web.
type WebHandlerOptions struct {
	Workspace        string
	AccountEmail     string
	CSRFToken        string
	ReferenceBackend string
	Desktop          bool
}

// resolveOptions normalizes and fills default values for WebHandlerOptions.
func resolveOptions(opts WebHandlerOptions) (WebHandlerOptions, error) {
	cwd := strings.TrimSpace(opts.Workspace)
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return opts, fmt.Errorf("web handler: cannot get working directory: %w", err)
		}
		cwd = wd
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return opts, fmt.Errorf("web handler: resolve %q: %w", cwd, err)
	}
	opts.Workspace = abs

	if opts.AccountEmail == "" {
		_, opts.AccountEmail = accounts.Current()
	}
	return opts, nil
}

// NewUIHandler creates an http.Handler that serves ONLY the frontend static assets
// (HTML, CSS, JS, icons, touch patches) without initializing an agent backend loop.
func NewUIHandler(opts WebHandlerOptions) (http.Handler, error) {
	resolved, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}

	webUI := rcfrontend.Handler(rcfrontend.Options{
		EnableTouchPatches: true,
		WorkspaceRoot:      resolved.Workspace,
		AccountEmail:       resolved.AccountEmail,
		CSRFToken:          resolved.CSRFToken,
	})

	if ref := strings.TrimSpace(resolved.ReferenceBackend); ref != "" {
		base, _, err := startReferenceBackend(ref)
		if err == nil && base != "" {
			if assets, err := refbackend.NewAssets(base); err == nil {
				webUI = newReferenceAssetsHandler(webUI, assets)
			}
		}
	}

	return webUI, nil
}

// NewAPIHandler creates an http.Handler that serves ONLY the LanguageServerService
// Connect-RPC and WebSocket backend APIs, returning 404 for UI asset requests.
// The caller should invoke the returned cleanup function when shutting down to
// release any spawned reference processes.
func NewAPIHandler(opts WebHandlerOptions) (http.Handler, func(), error) {
	resolved, err := resolveOptions(opts)
	if err != nil {
		return nil, nil, err
	}

	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	sess.Workspace = resolved.Workspace

	disp, refProc, _, err := buildWebDispatcher(resolved.Workspace, "", sess, resolved.ReferenceBackend, resolved.Desktop)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		if refProc != nil {
			refProc.Stop()
		}
	}

	seed := originpolicy.OriginPolicy{}
	handler, err := remotetransport.NewHandler(disp, remotetransport.TCPConfig{
		Path:         "/rpc",
		OriginPolicy: &seed,
		WebUIHandler: nil, // API-only mode
		CSRFToken:    resolved.CSRFToken,
	})
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	return handler, cleanup, nil
}

// NewWebHandler creates an http.Handler that serves both the LanguageServerService
// Connect-RPC/WebSocket APIs and the frontend static UI assets.
func NewWebHandler(opts WebHandlerOptions) (http.Handler, func(), error) {
	resolved, err := resolveOptions(opts)
	if err != nil {
		return nil, nil, err
	}

	uiHandler, err := NewUIHandler(resolved)
	if err != nil {
		return nil, nil, err
	}

	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	sess.Workspace = resolved.Workspace

	disp, refProc, _, err := buildWebDispatcher(resolved.Workspace, "", sess, resolved.ReferenceBackend, resolved.Desktop)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		if refProc != nil {
			refProc.Stop()
		}
	}

	seed := originpolicy.OriginPolicy{}
	handler, err := remotetransport.NewHandler(disp, remotetransport.TCPConfig{
		Path:         "/rpc",
		OriginPolicy: &seed,
		WebUIHandler: uiHandler,
		CSRFToken:    resolved.CSRFToken,
	})
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	return handler, cleanup, nil
}
