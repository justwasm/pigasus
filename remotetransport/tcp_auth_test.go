// Package remotetransport — tcp_auth_test.go: the secret gate on
// the remote-control surface (cycle rc-auth-bearer-required).
//
// The surface is reachable from the network on purpose — the
// persistent daemon refuses a loopback bind — and the gate is what
// answers a client that is not on the loopback interface.
//
// The tests cover the four rules:
//
//   - no secret configured → every request is allowed (the pre-cycle
//     behaviour);
//   - the remote-control tunnel → allowed (the cloud authenticated
//     it);
//   - the loopback interface → allowed, unless the configuration asks
//     for the secret everywhere;
//   - every other request must present the secret.
package remotetransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// testSecret is the secret the tests configure. It is long enough to
// have the shape of a minted one.
const testSecret = "test-secret-0123456789abcdef"

// newAuthTestServer brings up a TCPServer with the given config.
//
// The config is the caller's, so a test can set CSRFToken and
// RequireTokenOnLoopback (the flag that lets a test on 127.0.0.1
// exercise the rules that apply to a remote peer).
func newAuthTestServer(t *testing.T, cfg TCPConfig) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.3.4-test")
	sess.Workspace = "/tmp/ws-workspace"
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session: sess,
		Agents:  remotecontrol.DefaultAgentLister,
		Reader:  remotecontrol.DefaultOutputReader{},
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	cfg.Addr = "127.0.0.1:0"
	cfg.Path = "/rpc"
	srv, err := NewTCPServer(context.Background(), disp, cfg)
	if err != nil {
		t.Fatalf("NewTCPServer: %v", err)
	}
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, teardown
}

// remoteRequest builds a request that came in from outside the
// loopback interface.
func remoteRequest() *http.Request {
	req := httptest.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "192.168.1.5:5555"
	return req
}

// loopbackRequest builds a request that came in over the loopback
// interface.
func loopbackRequest() *http.Request {
	req := httptest.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	return req
}

// TestHandleTokenCheck_NoSecretConfigured pins the pre-cycle
// behaviour: a server without a secret lets every request through.
func TestHandleTokenCheck_NoSecretConfigured(t *testing.T) {
	srv, done := newAuthTestServer(t, TCPConfig{})
	defer done()

	decision := srv.handleTokenCheck(remoteRequest())
	if !decision.Allowed {
		t.Errorf("a server without a secret denies a remote peer: %+v", decision)
	}
}

// TestHandleTokenCheck_TunnelRequest pins that the tunnel is exempt:
// the cloud authenticated that request.
func TestHandleTokenCheck_TunnelRequest(t *testing.T) {
	srv, done := newAuthTestServer(t, TCPConfig{CSRFToken: testSecret})
	defer done()

	req := remoteRequest()
	req.Header.Set("X-Jetski-Via-Remote-Control", "true")
	decision := srv.handleTokenCheck(req)
	if !decision.Allowed {
		t.Errorf("a tunnel request is denied: %+v", decision)
	}
}

// TestHandleTokenCheck_LoopbackPeer pins that the loopback interface
// is trusted by default, and that the configuration can turn that off.
func TestHandleTokenCheck_LoopbackPeer(t *testing.T) {
	srv, done := newAuthTestServer(t, TCPConfig{CSRFToken: testSecret})
	defer done()

	if decision := srv.handleTokenCheck(loopbackRequest()); !decision.Allowed {
		t.Errorf("a loopback peer is denied by default: %+v", decision)
	}

	strict, strictDone := newAuthTestServer(t, TCPConfig{
		CSRFToken:              testSecret,
		RequireTokenOnLoopback: true,
	})
	defer strictDone()

	if decision := strict.handleTokenCheck(loopbackRequest()); decision.Allowed {
		t.Errorf("a loopback peer is allowed when the secret is required everywhere")
	}
}

// TestHandleTokenCheck_RemotePeerNeedsSecret pins the rule that
// matters: a peer from outside the loopback interface has to present
// the secret.
func TestHandleTokenCheck_RemotePeerNeedsSecret(t *testing.T) {
	srv, done := newAuthTestServer(t, TCPConfig{CSRFToken: testSecret})
	defer done()

	t.Run("nothing presented", func(t *testing.T) {
		decision := srv.handleTokenCheck(remoteRequest())
		if decision.Allowed {
			t.Fatalf("a remote peer without a secret is allowed")
		}
		if decision.Reason != "no secret presented" {
			t.Errorf("reason = %q, want %q", decision.Reason, "no secret presented")
		}
	})

	t.Run("the wrong secret", func(t *testing.T) {
		req := remoteRequest()
		req.Header.Set("x-codeium-csrf-token", "wrong-secret-0123456789abcd")
		decision := srv.handleTokenCheck(req)
		if decision.Allowed {
			t.Fatalf("a remote peer with the wrong secret is allowed")
		}
		if decision.Reason != "secret mismatch" {
			t.Errorf("reason = %q, want %q", decision.Reason, "secret mismatch")
		}
	})

	t.Run("the right secret", func(t *testing.T) {
		req := remoteRequest()
		req.Header.Set("x-codeium-csrf-token", testSecret)
		decision := srv.handleTokenCheck(req)
		if !decision.Allowed {
			t.Fatalf("a remote peer with the right secret is denied: %+v", decision)
		}
	})
}

// TestHandleWS_UnauthorizedWithoutSecret pins the WS upgrade's
// answer to a client that presents no secret: HTTP 401 with the
// JSON body, and no upgrade.
func TestHandleWS_UnauthorizedWithoutSecret(t *testing.T) {
	srv, done := newAuthTestServer(t, TCPConfig{
		CSRFToken:              testSecret,
		RequireTokenOnLoopback: true,
	})
	defer done()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, srv.HTTPURL(), nil)
	if err == nil {
		_ = c.CloseNow()
		t.Fatalf("Dial without a secret should fail with 401; got success")
	}
	if resp == nil {
		t.Fatalf("Dial returned no http.Response")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Errorf("WWW-Authenticate = %q, want it to name Bearer", got)
	}
	assertDeniedBody(t, resp)
}

// TestHandleWS_AuthorizedWithSecret pins the happy path: a client
// that presents the secret upgrades and round-trips a request.
func TestHandleWS_AuthorizedWithSecret(t *testing.T) {
	srv, done := newAuthTestServer(t, TCPConfig{
		CSRFToken:              testSecret,
		RequireTokenOnLoopback: true,
	})
	defer done()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, srv.HTTPURL(), &websocket.DialOptions{
		HTTPHeader: http.Header{
			"x-codeium-csrf-token": []string{testSecret},
		},
	})
	if err != nil {
		t.Fatalf("Dial with the secret should succeed; got %v", err)
	}
	defer func() { _ = c.CloseNow() }()
	writeJSONRPC(t, c, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	})
	resp := readJSONRPC(t, c)
	if _, ok := resp["id"]; !ok {
		t.Errorf("missing id in response: %v", resp)
	}
}

// TestHandleRPCCall_UnauthorizedWithoutSecret pins the Connect-RPC
// path's answer: the same 401 + JSON body.
func TestHandleRPCCall_UnauthorizedWithoutSecret(t *testing.T) {
	srv, done := newAuthTestServer(t, TCPConfig{
		CSRFToken:              testSecret,
		RequireTokenOnLoopback: true,
	})
	defer done()

	req := httptest.NewRequest("POST", "/exa.language_server_pb.LanguageServerService/GetStatus", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleRPCCall(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v\nbody=%s", err, rr.Body.String())
	}
	if body["ok"] != false {
		t.Errorf("body.ok = %v, want false", body["ok"])
	}
	if body["denied"] != true {
		t.Errorf("body.denied = %v, want true", body["denied"])
	}
	if reason, _ := body["reason"].(string); reason == "" {
		t.Errorf("body.reason is empty")
	}
}

// TestHandleRPCCall_AuthorizedWithSecret pins that a client that
// presents the secret reaches the dispatcher.
func TestHandleRPCCall_AuthorizedWithSecret(t *testing.T) {
	srv, done := newAuthTestServer(t, TCPConfig{
		CSRFToken:              testSecret,
		RequireTokenOnLoopback: true,
	})
	defer done()

	req := httptest.NewRequest("POST", "/exa.language_server_pb.LanguageServerService/GetStatus", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-codeium-csrf-token", testSecret)
	rr := httptest.NewRecorder()
	srv.handleRPCCall(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d (%s)", rr.Code, http.StatusOK, rr.Body.String())
	}
}

// assertDeniedBody reads the 401 body and checks its shape.
func assertDeniedBody(t *testing.T, resp *http.Response) {
	t.Helper()
	defer resp.Body.Close()
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	var body map[string]any
	if err := json.Unmarshal(buf[:n], &body); err != nil {
		t.Fatalf("body not JSON: %v\nbody=%s", err, string(buf[:n]))
	}
	if body["ok"] != false {
		t.Errorf("body.ok = %v, want false", body["ok"])
	}
	if body["denied"] != true {
		t.Errorf("body.denied = %v, want true", body["denied"])
	}
	if reason, _ := body["reason"].(string); reason == "" {
		t.Errorf("body.reason is empty")
	}
}

// TestServeWebUI_GatesThePage pins that the Web UI's own paths are
// behind the same gate as the RPC surface: the page carries the
// secret, so a peer that may not have it must not get the page.
//
// The gate lives at the transport, so it covers every handler that
// serves the page — not only the clone's own one.
func TestServeWebUI_GatesThePage(t *testing.T) {
	srv, done := newAuthTestServer(t, TCPConfig{
		CSRFToken:              testSecret,
		RequireTokenOnLoopback: true,
		WebUIHandler:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("page")) }),
	})
	defer done()

	// No secret → the page is refused.
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	srv.newConnectRPCRouter(srv.cfg.WebUIHandler).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET / returned status %d, want 401", rr.Code)
	}

	// The secret in the query string → the page is served, and the
	// browser is handed the cookie that carries the secret from then
	// on.
	req2 := httptest.NewRequest("GET", "/?token="+testSecret, nil)
	rr2 := httptest.NewRecorder()
	srv.newConnectRPCRouter(srv.cfg.WebUIHandler).ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("GET /?token=... returned status %d, want 200", rr2.Code)
	}
	cookie := rr2.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Value != testSecret {
		t.Errorf("Set-Cookie = %v, want the secret", cookie)
	}

	// The cookie alone → the page is served too.
	req3 := httptest.NewRequest("GET", "/", nil)
	req3.AddCookie(&http.Cookie{Name: "csrfToken", Value: testSecret})
	rr3 := httptest.NewRecorder()
	srv.newConnectRPCRouter(srv.cfg.WebUIHandler).ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("GET / with the cookie returned status %d, want 200", rr3.Code)
	}
}
