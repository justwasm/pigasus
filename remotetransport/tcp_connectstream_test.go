package remotetransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/originpolicy"
)

// newTestRPCServer starts a TCP server whose mux carries the
// Connect-RPC HTTP route, which the mux only registers when a Web UI
// handler is configured.
func newTestRPCServer(t *testing.T) (*TCPServer, func()) {
	t.Helper()
	sess := remotecontrol.NewSessionState("1.4.7-test")
	sess.Workspace = t.TempDir()
	disp, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session: sess,
		Agents:  remotecontrol.DefaultAgentLister,
		Reader:  remotecontrol.DefaultOutputReader{},
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	policy := originpolicy.OriginPolicy{Patterns: []string{"https://antigravity.google.com"}}
	srv, err := NewTCPServer(context.Background(), disp, TCPConfig{
		Addr:         "127.0.0.1:0",
		Path:         "/rpc",
		OriginPolicy: &policy,
		WebUIHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	})
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

func TestTCPServer_RPCRouteRejectsForeignOrigin(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/GetStatus"
	req, err := http.NewRequest(http.MethodPost, url, http.NoBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Origin", "https://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestTCPServer_RPCRouteAllowsRemoteControlRequest(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/GetStatus"
	req, err := http.NewRequest(http.MethodPost, url, http.NoBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Origin", "https://antigravity.google.com")
	req.Header.Set(headerViaRemoteControl, "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestTCPServer_RPCRouteStreamsConnectEnvelopes(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/GetStatus"
	req, err := http.NewRequest(http.MethodPost, url, http.NoBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(headerViaRemoteControl, "true")
	req.Header.Set(headerStreamingFlag, "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != contentTypeConnectJSON {
		t.Errorf("Content-Type = %q, want %q", got, contentTypeConnectJSON)
	}
	envelopes := parseConnectEnvelopes(t, readAll(t, resp))
	if len(envelopes) < 2 {
		t.Fatalf("got %d envelopes, want at least 2", len(envelopes))
	}
	if envelopes[0].flags != ConnectFlagMessage {
		t.Errorf("first envelope flags = %#x, want %#x", envelopes[0].flags, ConnectFlagMessage)
	}
	last := envelopes[len(envelopes)-1]
	if last.flags != ConnectFlagEndStream {
		t.Errorf("last envelope flags = %#x, want %#x", last.flags, ConnectFlagEndStream)
	}
}

type connectEnvelope struct {
	flags   byte
	payload []byte
}

// parseConnectEnvelopes splits a Connect streaming body into its
// envelopes: one flag byte, a four byte big-endian length, then the
// message.
func parseConnectEnvelopes(t *testing.T, body []byte) []connectEnvelope {
	t.Helper()
	var out []connectEnvelope
	for len(body) > 0 {
		if len(body) < 5 {
			t.Fatalf("truncated envelope header: %d bytes left", len(body))
		}
		size := int(body[1])<<24 | int(body[2])<<16 | int(body[3])<<8 | int(body[4])
		if len(body) < 5+size {
			t.Fatalf("truncated envelope payload: want %d bytes, have %d", size, len(body)-5)
		}
		out = append(out, connectEnvelope{flags: body[0], payload: body[5 : 5+size]})
		body = body[5+size:]
	}
	return out
}

func readAll(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf
		}
	}
}

var _ = httptest.NewRecorder
