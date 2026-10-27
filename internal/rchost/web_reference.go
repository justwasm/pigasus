// Package rchost — web_reference.go: the `pigasus web --reference-backend`
// wiring.
//
// The clone answers the Web UI's LanguageServerService calls itself (see
// internal/remotecontrol). With a reference backend the web server
// proxies every call to a reference `language_server` instead, so the UI
// shows the reference's own answers and the two implementations can be
// compared side by side.
//
// The reference endpoint is either a running instance ("host:port") or
// the path of a reference binary, which the web server starts for the
// lifetime of the command.
package rchost

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/refbackend"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/originpolicy"
	"github.com/justwasm/pigasus/remotetransport"
)

// originPolicySetter is the optional capability the clone's own
// dispatcher offers: the transport's origin-policy setter. The
// reference proxy does not enforce an origin policy — the reference
// does its own — so the web server only wires it when the dispatcher
// has it.
type originPolicySetter interface {
	SetOriginPolicySetter(func(originpolicy.OriginPolicy))
}

// buildWebDispatcher returns the dispatcher the web server answers its
// RPCs with, the reference process it owns (nil when the clone
// answers the calls itself), and the reference's base URL (empty when
// there is no reference backend).
//
// desktop asks for the reference's *desktop* UI: the page's Mendel
// read then carries the hub identity, and the backend serves the flag
// set that carries the desktop features — the voice input, the jetbox
// terminal, the aux pane redesign. See internal/auth's package comment
// for the measurement.
func buildWebDispatcher(cwd, addr string, sess *remotecontrol.SessionState, referenceBackend string, desktop bool) (remotetransport.Dispatcher, *refbackend.Process, string, error) {
	ref := strings.TrimSpace(referenceBackend)
	if ref == "" {
		disp, err := buildServeDispatcher(cwd, addr, sess, "", desktopMendelUserAgent(desktop))
		if err != nil {
			return nil, nil, "", err
		}
		return disp, nil, "", nil
	}
	base, proc, err := startReferenceBackend(ref)
	if err != nil {
		return nil, nil, "", cli.ExitCodeError(1, fmt.Sprintf("Error: reference backend: %v", err))
	}
	disp, err := refbackend.New(base)
	if err != nil {
		if proc != nil {
			proc.Stop()
		}
		return nil, nil, "", cli.ExitCodeError(1, fmt.Sprintf("Error: reference backend: %v", err))
	}
	return disp, proc, base, nil
}

// startReferenceBackend resolves the --reference-backend value into a
// base URL. A path to a binary starts the reference; anything else is
// the endpoint of a running one.
func startReferenceBackend(value string) (string, *refbackend.Process, error) {
	if refbackend.IsBinaryPath(value) {
		proc, err := refbackend.Spawn(value)
		if err != nil {
			return "", nil, err
		}
		return proc.Base(), proc, nil
	}
	base, err := refbackend.NormalizeBase(value)
	if err != nil {
		return "", nil, err
	}
	return base, nil, nil
}

// desktopMendelUserAgent returns the identity the web server's Mendel
// read carries.
//
// Empty — the CLI's own — is the default: the clone's page is the
// CLI's page, with the CLI's feature set. The desktop mode asks for
// the hub's set, which is the one the reference's own page gets.
//
// 【一手】 2026-10-27: the backend answers a different flag set per
// product. See internal/auth's package comment.
func desktopMendelUserAgent(desktop bool) string {
	if !desktop {
		return ""
	}
	return auth.MendelHubUserAgent(cli.ReferenceVersion)
}
