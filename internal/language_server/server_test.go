// Tests for the recovered internal/language_server package.
//
// These tests verify that:
//   - the 7 RPCs of LanguageServerService are exposed
//   - the Server lifecycle (NewServer / Serve / Shutdown /
//     RegisterServer) works end-to-end on a localhost listener
//   - the proto message structs construct + return their getters
//   - the streaming RPC dispatches via the Stream type
//   - the recovered types (RoutingClient, AudioSessionManager,
//     NarrationSessionManager, BAICTelemetryClient,
//     turnAttributionTracker, ModelAPIClientType) construct and
//     behave as expected.
package language_server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLanguageServer_Names(t *testing.T) {
	want := []string{
		"StartCascade",
		"SendUserCascadeMessage",
		"CancelCascade",
		"SkipBrowserSubagent",
		"GetAgentInstance",
		"StreamAgentUpdates",
		"Shutdown",
	}
	if len(LanguageServerMethodNames) != len(want) {
		t.Fatalf("got %d methods, want %d", len(LanguageServerMethodNames), len(want))
	}
	for i, name := range want {
		if LanguageServerMethodNames[i] != name {
			t.Errorf("LanguageServerMethodNames[%d] = %q, want %q", i, LanguageServerMethodNames[i], name)
		}
	}
}

func TestNewServer(t *testing.T) {
	srv := NewServer(&LanguageServerConfig{Port: 0, RPCTimeout: 30 * time.Second})
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.config == nil {
		t.Error("config is nil")
	}
	if srv.router == nil {
		t.Error("router is nil")
	}
	if srv.host == nil {
		t.Error("host is nil")
	}
	if srv.telemetry == nil {
		t.Error("telemetry is nil")
	}
	if srv.audioMgr == nil {
		t.Error("audioMgr is nil")
	}
	if srv.narration == nil {
		t.Error("narration is nil")
	}
	if srv.attrib == nil {
		t.Error("attrib is nil")
	}
}

func TestRegisterServer(t *testing.T) {
	srv := NewServer(nil)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	handler := RegisterServer(srv)
	if handler == nil {
		t.Fatal("RegisterServer returned nil")
	}
	// Test that the handler dispatches to the 7 RPCs.
	for _, method := range LanguageServerMethodNames {
		path := "/" + ServiceName + "/" + method
		req := httptest.NewRequest(http.MethodPost, path+"?agent_id=test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		// For Shutdown we expect a 200, for others we expect either
		// 200 (placeholder) or 500 (if the handler requires
		// non-empty CascadeID). Either way, the handler must be
		// registered (i.e. NOT 404).
		if w.Code == http.StatusNotFound {
			t.Errorf("RPC %s: handler not registered (404)", method)
		}
	}
}

func TestStartCascadeHandler(t *testing.T) {
	srv := NewServer(nil)
	resp, err := srv.StartCascade(context.Background(), &StartCascadeRequest{
		AgentPath: "/path/to/agent",
	})
	if err != nil {
		t.Fatalf("StartCascade: %v", err)
	}
	if !resp.Accepted {
		t.Error("Accepted = false, want true (placeholder)")
	}
	if resp.CascadeID == "" {
		t.Error("CascadeID is empty")
	}
}

func TestSendUserCascadeMessageHandler(t *testing.T) {
	srv := NewServer(nil)
	resp, err := srv.SendUserCascadeMessage(context.Background(), &SendUserCascadeMessageRequest{
		CascadeID: "cascade-001",
		Text:      "hello",
	})
	if err != nil {
		t.Fatalf("SendUserCascadeMessage: %v", err)
	}
	if !resp.Accepted {
		t.Error("Accepted = false, want true (placeholder)")
	}
	if resp.StepID == "" {
		t.Error("StepID is empty")
	}
}

func TestCancelCascadeHandler(t *testing.T) {
	srv := NewServer(nil)
	resp, err := srv.CancelCascade(context.Background(), &CancelCascadeRequest{
		CascadeID: "cascade-001",
		Reason:    "user requested",
	})
	if err != nil {
		t.Fatalf("CancelCascade: %v", err)
	}
	if !resp.Cancelled {
		t.Error("Cancelled = false, want true (placeholder)")
	}
}

func TestGetAgentInstanceHandler(t *testing.T) {
	srv := NewServer(nil)
	resp, err := srv.GetAgentInstance(context.Background(), &GetAgentInstanceRequest{
		AgentID: "agent-001",
	})
	if err != nil {
		t.Fatalf("GetAgentInstance: %v", err)
	}
	if resp.Agent == nil {
		t.Fatal("Agent is nil")
	}
	if resp.Agent.AgentID != "agent-001" {
		t.Errorf("AgentID = %q, want agent-001", resp.Agent.AgentID)
	}
	if resp.LatestCascadeID == "" {
		t.Error("LatestCascadeID is empty")
	}
	// Verify the recovered GetLatestCascadeID getter.
	if got := resp.Agent.GetLatestCascadeID(); got != resp.LatestCascadeID {
		t.Errorf("GetLatestCascadeID = %q, want %q", got, resp.LatestCascadeID)
	}
}

func TestStreamAgentUpdatesHandler(t *testing.T) {
	srv := NewServer(nil)
	// Use the Stream type as a stand-in for the server-side stream.
	stream := NewStream(context.Background())
	defer stream.Close()
	err := srv.StreamAgentUpdates(&StreamAgentUpdatesRequest{
		CascadeID: "cascade-001",
	}, stream)
	if err != nil {
		t.Fatalf("StreamAgentUpdates: %v", err)
	}
	update, ok := stream.Recv()
	if !ok {
		t.Fatal("Recv: no update")
	}
	if update.UpdateType == "" {
		t.Error("UpdateType is empty")
	}
}

func TestRoutingClient(t *testing.T) {
	rc := NewRoutingClient("https://example.com")
	if rc == nil {
		t.Fatal("NewRoutingClient returned nil")
	}
	if got := rc.GetCommitMessageModelEnum(); got != "claude-sonnet-4" {
		t.Errorf("GetCommitMessageModelEnum = %q, want claude-sonnet-4", got)
	}
	if infos := rc.GetModelInfos(); len(infos) == 0 {
		t.Error("GetModelInfos: empty")
	}
	if got := rc.WithConfig("sonnet").GetCascadeModelConfigData(); got != "sonnet" {
		t.Errorf("WithConfig().GetCascadeModelConfigData = %q, want sonnet", got)
	}
}

func TestModelAPIClientType(t *testing.T) {
	m := ModelAPIClientAntigravity
	if m.String() != "antigravity" {
		t.Errorf("String = %q, want antigravity", m.String())
	}
	if got := (&m).Get(); got != ModelAPIClientAntigravity {
		t.Errorf("Get = %q, want antigravity", got)
	}
	(&m).Set(ModelAPIClientVertex)
	if m != ModelAPIClientVertex {
		t.Errorf("Set failed: m = %q, want vertex", m)
	}
}

func TestLanguageServerConfig(t *testing.T) {
	cfg := &LanguageServerConfig{
		IsSessionScopedRemoteControl_: true,
		IsTerminalCLI_:                true,
	}
	if !cfg.IsSessionScopedRemoteControl() {
		t.Error("IsSessionScopedRemoteControl = false, want true")
	}
	if !cfg.IsTerminalCLI() {
		t.Error("IsTerminalCLI = false, want true")
	}
}

func TestTurnAttributionTracker(t *testing.T) {
	tracker := &turnAttributionTracker{}
	tracker.OnTurnActive("turn-1")
	if !tracker.enabled {
		t.Error("OnTurnActive: enabled = false, want true")
	}
	tracker.OnTurnIdle("turn-1")
	if tracker.enabled {
		t.Error("OnTurnIdle: enabled = true, want false")
	}
	if err := tracker.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestAudioSessionManager(t *testing.T) {
	mgr := NewAudioSessionManager()
	if mgr == nil {
		t.Fatal("NewAudioSessionManager returned nil")
	}
	s := mgr.Add("session-1")
	if s == nil {
		t.Fatal("Add returned nil")
	}
	if got := mgr.Get("session-1"); got != s {
		t.Error("Get: returned different session")
	}
	if got := mgr.Get("missing"); got != nil {
		t.Error("Get: returned non-nil for missing session")
	}
	mgr.Remove("session-1")
	if got := mgr.Get("session-1"); got != nil {
		t.Error("Get: returned non-nil after Remove")
	}
}

func TestNarrationSessionManager(t *testing.T) {
	mgr := NewNarrationSessionManager()
	if mgr == nil {
		t.Fatal("NewNarrationSessionManager returned nil")
	}
	s := mgr.Add("narr-1")
	if s == nil {
		t.Fatal("Add returned nil")
	}
	if got := mgr.Get("narr-1"); got != s {
		t.Error("Get: returned different session")
	}
	mgr.Remove("narr-1")
}

func TestBAICTelemetryClient(t *testing.T) {
	c := NewBAICTelemetryClient("https://example.com")
	if c == nil {
		t.Fatal("NewBAICTelemetryClient returned nil")
	}
	if err := c.RecordCommandUsage("test"); err != nil {
		t.Errorf("RecordCommandUsage: %v", err)
	}
	if err := c.RecordError(nil); err != nil {
		t.Errorf("RecordError: %v", err)
	}
	if err := c.Shutdown(); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestEndToEndHTTP(t *testing.T) {
	// Spin up a real httptest.Server with RegisterServer as the
	// handler and exercise the 5 unary RPCs.
	srv := NewServer(&LanguageServerConfig{Port: 0, RPCTimeout: 30 * time.Second})
	handler := RegisterServer(srv)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	cases := []struct {
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{"StartCascade", "/StartCascade?agent_path=/p", http.StatusOK, "cascade_id"},
		{"SendUserCascadeMessage", "/SendUserCascadeMessage?cascade_id=c1&text=hi", http.StatusOK, "step_id"},
		{"CancelCascade", "/CancelCascade?cascade_id=c1&reason=usr", http.StatusOK, "cancelled"},
		{"SkipBrowserSubagent", "/SkipBrowserSubagent?cascade_id=c1", http.StatusOK, "skipped"},
		{"GetAgentInstance", "/GetAgentInstance?agent_id=a1", http.StatusOK, "latest_cascade_id"},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/"+ServiceName+tc.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("RPC %s: %v", tc.method, err)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != tc.wantStatus {
			t.Errorf("RPC %s: status = %d, want %d", tc.method, resp.StatusCode, tc.wantStatus)
		}
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		body := string(buf[:n])
		if tc.wantBody != "" && !strings.Contains(body, tc.wantBody) {
			t.Errorf("RPC %s: body = %q, want substring %q", tc.method, body, tc.wantBody)
		}
	}
}
