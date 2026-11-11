package remotetransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/justwasm/pigasus/originpolicy"
)

type mockRPCDispatcher struct {
	accepted         uint64
	notifier         func(ctx context.Context, connID string, frame []byte) error
	broadcaster      func(ctx context.Context, method string, params any) error
	onDispatch       func(ctx context.Context, raw []byte) map[string]any
	onDispatchDirect func(ctx context.Context, method string, payloadText string) (string, error)
}

func (m *mockRPCDispatcher) Dispatch(ctx context.Context, raw []byte) map[string]any {
	if m.onDispatch != nil {
		return m.onDispatch(ctx, raw)
	}
	var req map[string]any
	_ = json.Unmarshal(raw, &req)
	id := req["id"]
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"ok": true}}
}

func (m *mockRPCDispatcher) DispatchDirect(ctx context.Context, method string, payloadText string) (string, error) {
	if m.onDispatchDirect != nil {
		return m.onDispatchDirect(ctx, method, payloadText)
	}
	switch method {
	case "GetStatus":
		return `{"status":{"message":"ready"}}`, nil
	case "JetboxSubscribeToState":
		return `{"appState":{"agentOnboardingCompleted":"AGENT_ONBOARDING_STATE_COMPLETED"},"userConfig":{}}`, nil
	case "GetLocalUserInfo":
		return `{"username":"testuser"}`, nil
	case "NoSuchMethod":
		return "", errors.New("method NoSuchMethod not found")
	default:
		return `{}`, nil
	}
}

func (m *mockRPCDispatcher) Accepted() uint64    { return m.accepted }
func (m *mockRPCDispatcher) MarkAccepted()       { m.accepted++ }
func (m *mockRPCDispatcher) NotifyConnected()    {}
func (m *mockRPCDispatcher) NotifyDisconnected() {}
func (m *mockRPCDispatcher) Notify(ctx context.Context, connID, method string, params any) error {
	if m.notifier != nil {
		return m.notifier(ctx, connID, nil)
	}
	return nil
}
func (m *mockRPCDispatcher) SetNotifier(fn func(ctx context.Context, connID string, frame []byte) error) {
	m.notifier = fn
}
func (m *mockRPCDispatcher) WithConnID(ctx context.Context, connID string) context.Context {
	return ctx
}
func (m *mockRPCDispatcher) SetBroadcaster(fn func(ctx context.Context, method string, params any) error) {
	m.broadcaster = fn
}

func newTestRPCServer(t testing.TB) (*TCPServer, func()) {
	t.Helper()
	disp := &mockRPCDispatcher{}
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

func newTestTCPServer(t *testing.T) (*TCPServer, *mockRPCDispatcher, func()) {
	t.Helper()
	disp := &mockRPCDispatcher{}
	srv, err := NewTCPServer(context.Background(), disp, TCPConfig{
		Addr: "127.0.0.1:0",
		Path: "/rpc",
	})
	if err != nil {
		t.Fatalf("NewTCPServer: %v", err)
	}
	teardown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return srv, disp, teardown
}

func writeJSONRPC(t *testing.T, c *websocket.Conn, req map[string]any) {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

func readJSONRPC(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, raw, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}
	return resp
}
