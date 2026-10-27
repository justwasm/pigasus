// Package remotetransport — tcp_auth.go: the secret the
// remote-control surface demands from a client that did not come in
// over the loopback interface.
//
// Cycle rc-auth-bearer-required.
//
// The surface this file guards is reachable from the network on
// purpose — `pigasus remote-control serve` and `pigasus web` both bind
// 0.0.0.0 — and until this cycle nothing on it asked for a
// credential. The Origin allowlist was the only gate, and the
// allowlist admits an empty Origin, which is exactly what curl,
// wscat and a python client send. A host that could reach the port
// could therefore drive the agent.
//
// The rule this file implements:
//
//   - a request that arrived through the remote-control tunnel is
//     trusted — the cloud authenticated it, and it never crossed the
//     socket this gate guards;
//   - a request from the loopback interface is trusted — it is the
//     operator's own machine, and the peer address is the one part of
//     a request a client cannot forge;
//   - every other request must present the secret.
//
// The secret itself lives in rctoken; this file only decides
// who has to present it.
package remotetransport

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/justwasm/pigasus/rctoken"
)

// AuthDecision is the verdict (*TCPServer).handleTokenCheck returns.
//
// Allowed:true falls through to the normal request path;
// Allowed:false is the 401 + JSON-body reject the callers write.
//
// Reason is the human-readable verdict the wire envelope echoes. It
// names the rule that decided the request — "loopback peer",
// "remote-control tunnel", "no secret presented", "secret mismatch" —
// and never carries the secret itself.
type AuthDecision struct {
	Allowed bool
	Reason  string
}

// handleTokenCheck decides whether a request may use the
// remote-control surface.
//
// The order of the rules is the order of trust:
//
//  1. no secret configured → every request is allowed. A server that
//     was wired without a secret keeps its pre-cycle behaviour, so
//     the Unix-socket rig and the tests are unaffected.
//  2. the remote-control tunnel → allowed. The cloud's WebChannel
//     client authenticated the request with Google's own bearer
//     token before the relay handed it to this process.
//  3. the loopback interface → allowed, unless the configuration asks
//     for the secret everywhere (TCPConfig.RequireTokenOnLoopback).
//  4. otherwise the request must present the secret.
//
// The function is nil-safe: a nil receiver allows the request (the
// pre-cycle "no server, no gate" semantic), and a nil request is
// treated as a request that presented nothing.
func (s *TCPServer) handleTokenCheck(r *http.Request) AuthDecision {
	if s == nil {
		return AuthDecision{Allowed: true, Reason: "no server"}
	}
	expected := s.cfg.CSRFToken
	if expected == "" {
		return AuthDecision{Allowed: true, Reason: "no secret configured"}
	}
	if rctoken.IsTunnelRequest(r) {
		return AuthDecision{Allowed: true, Reason: "remote-control tunnel"}
	}
	if !s.cfg.RequireTokenOnLoopback && rctoken.IsLoopbackPeer(r) {
		return AuthDecision{Allowed: true, Reason: "loopback peer"}
	}
	presented := s.presentedToken(r)
	if presented == "" {
		return AuthDecision{Allowed: false, Reason: "no secret presented"}
	}
	if !rctoken.Verify(expected, presented) {
		return AuthDecision{Allowed: false, Reason: "secret mismatch"}
	}
	return AuthDecision{Allowed: true, Reason: "secret accepted"}
}

// presentedToken reads the secret a request presents.
//
// The reference's language server reads it from one place only — the
// `x-codeium-csrf-token` header. The clone reads four channels
// because the remote-control page and the cloud tunnel need them;
// TCPConfig.ReferenceTokenGate narrows it back to the reference's
// one.
func (s *TCPServer) presentedToken(r *http.Request) string {
	if s != nil && s.cfg.ReferenceTokenGate {
		if r == nil {
			return ""
		}
		return strings.TrimSpace(r.Header.Get(rctoken.HeaderName))
	}
	return rctoken.Presented(r)
}

// writeTokenUnauthorized writes the HTTP 401 + JSON body the callers
// return when handleTokenCheck denies a request.
//
// The body is the same shape as the Origin deny body
// (writeOriginForbidden), so a client that reads either envelope
// finds the same fields:
//
//	{"ok":false,"denied":true,"reason":"<human-readable>"}
//
// The `WWW-Authenticate` header names the standard scheme, so a
// client that understands bearer authentication knows what to send.
//
// The reference's language server writes the Connect-RPC error body
// instead — `{"code":"unauthenticated","message":"unauthenticated:
// missing CSRF token"}` — and carries no `WWW-Authenticate` header.
// TCPConfig.ReferenceTokenGate selects that body.
func writeTokenUnauthorized(w http.ResponseWriter, d AuthDecision) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("WWW-Authenticate", `Bearer realm="remote-control"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"ok":false,"denied":true,"reason":%q}`+"\n", d.Reason)
}

// writeReferenceTokenUnauthorized writes the reference's own 401 body.
//
// The reference's language server answers a denied request with the
// Connect-RPC error body and with no `WWW-Authenticate` header.
func writeReferenceTokenUnauthorized(w http.ResponseWriter, d AuthDecision) {
	detail := "invalid CSRF token"
	if d.Reason == "no secret presented" {
		detail = "missing CSRF token"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"code":"unauthenticated","message":%q}`, "unauthenticated: "+detail)
}

// writeTokenUnauthorizedFor writes the 401 body of a denied request.
//
// The reference's gate writes the Connect-RPC error body; the clone's
// own gate writes its own envelope. TCPConfig.ReferenceTokenGate
// picks between them.
func (s *TCPServer) writeTokenUnauthorizedFor(w http.ResponseWriter, d AuthDecision) {
	if s != nil && s.cfg.ReferenceTokenGate {
		writeReferenceTokenUnauthorized(w, d)
		return
	}
	writeTokenUnauthorized(w, d)
}
