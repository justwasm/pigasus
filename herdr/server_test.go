package herdr_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/justwasm/pigasus/herdr"
)

func TestHerdrServer_JSONRPC(t *testing.T) {
	mgr := herdr.NewManager(24, 80)
	defer mgr.CloseAll()

	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("hs-%d.sock", time.Now().UnixNano()))
	srv, err := herdr.NewServer(sockPath, mgr)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Close()

	// 1. Add mock panes
	p1, err := mgr.AddPane("worker-1", "", herdr.WithMockSession("$ "))
	if err != nil {
		t.Fatalf("failed to add pane 1: %v", err)
	}
	_, err = mgr.AddPane("worker-2", "", herdr.WithMockSession("$ "))
	if err != nil {
		t.Fatalf("failed to add pane 2: %v", err)
	}

	// 2. ping
	respBytes, err := herdr.CallSocket(sockPath, "system.ping", nil)
	if err != nil {
		t.Fatalf("system.ping failed: %v", err)
	}
	var pingResp struct {
		Result struct {
			Type   string `json:"type"`
			Server string `json:"server"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBytes, &pingResp); err != nil {
		t.Fatalf("unmarshal ping response: %v", err)
	}
	if pingResp.Result.Type != "pong" || pingResp.Result.Server != "pigasus-herdr" {
		t.Fatalf("unexpected ping result: %+v", pingResp)
	}

	// 3. pane.list
	respBytes, err = herdr.CallSocket(sockPath, "pane.list", nil)
	if err != nil {
		t.Fatalf("pane.list failed: %v", err)
	}
	type listResult struct {
		Panes []herdr.PaneSummary `json:"panes"`
	}
	res, err := herdr.DecodeResponse[listResult](respBytes)
	if err != nil {
		t.Fatalf("decode pane.list result: %v", err)
	}
	if len(res.Panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(res.Panes))
	}

	// 4. pane.report_agent
	msg := "Working on test suite"
	reportParams := map[string]any{
		"pane_id": p1.ID,
		"agent":   "crush",
		"state":   "working",
		"message": msg,
	}
	respBytes, err = herdr.CallSocket(sockPath, "pane.report_agent", reportParams)
	if err != nil {
		t.Fatalf("pane.report_agent failed: %v", err)
	}
	agent, astate, amsg := p1.AgentState()
	if agent != "crush" || astate != "working" || amsg != msg {
		t.Fatalf("expected agent crush/working/%s, got %s/%s/%s", msg, agent, astate, amsg)
	}

	// 5. pane.get
	respBytes, err = herdr.CallSocket(sockPath, "pane.get", map[string]any{"pane_id": p1.ID})
	if err != nil {
		t.Fatalf("pane.get failed: %v", err)
	}
	paneRes, err := herdr.DecodeResponse[herdr.PaneSummary](respBytes)
	if err != nil {
		t.Fatalf("decode pane.get: %v", err)
	}
	if paneRes.PaneID != p1.ID || paneRes.Agent != "crush" || paneRes.AgentState != "working" {
		t.Fatalf("unexpected pane summary: %+v", paneRes)
	}

	// 6. pane.report_metadata (change title)
	respBytes, err = herdr.CallSocket(sockPath, "pane.report_metadata", map[string]any{
		"pane_id": p1.ID,
		"title":   "crush-build",
	})
	if err != nil {
		t.Fatalf("pane.report_metadata failed: %v", err)
	}
	if p1.Title != "crush-build" {
		t.Fatalf("expected title crush-build, got %s", p1.Title)
	}

	// 7. agent.prompt (tincan doorbell)
	var notifiedTarget, notifiedMsg string
	srv.SetOnNotify(func(target, message string) {
		notifiedTarget = target
		notifiedMsg = message
	})
	respBytes, err = herdr.CallSocket(sockPath, "agent.prompt", map[string]any{
		"target":  "crush-build",
		"message": "Doorbell: new review requested",
	})
	if err != nil {
		t.Fatalf("agent.prompt failed: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if notifiedTarget != "crush-build" || notifiedMsg != "Doorbell: new review requested" {
		t.Fatalf("unexpected notify callback: target=%s msg=%s", notifiedTarget, notifiedMsg)
	}

	// 8. agent.prompt on missing agent -> returns agent_not_found
	respBytes, err = herdr.CallSocket(sockPath, "agent.prompt", map[string]any{
		"target":  "non-existent-agent",
		"message": "hello",
	})
	if err != nil {
		t.Fatalf("call socket error: %v", err)
	}
	_, err = herdr.DecodeResponse[map[string]any](respBytes)
	if !errors.Is(err, herdr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing agent, got: %v", err)
	}

	// 9. pane.release_agent
	respBytes, err = herdr.CallSocket(sockPath, "pane.release_agent", map[string]any{"pane_id": p1.ID})
	if err != nil {
		t.Fatalf("pane.release_agent failed: %v", err)
	}
	agent, astate, _ = p1.AgentState()
	if agent != "" || astate != "" {
		t.Fatalf("expected cleared agent, got %s / %s", agent, astate)
	}
}

func TestHerdrModel_SocketLifecycle(t *testing.T) {
	model := herdr.NewModel(24, 80)
	defer model.Close()

	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("hm-%d.sock", time.Now().UnixNano()))
	srv, err := model.StartSocketServer(sockPath)
	if err != nil {
		t.Fatalf("StartSocketServer failed: %v", err)
	}
	if srv.SocketPath() != sockPath {
		t.Fatalf("expected sockPath %s, got %s", sockPath, srv.SocketPath())
	}

	p, err := model.SpawnMockPane("test-tab", "welcome to test pane")
	if err != nil {
		t.Fatalf("SpawnMockPane failed: %v", err)
	}

	// Verify env info
	sp, _, _ := p.SocketInfo()
	if sp != sockPath {
		t.Fatalf("expected pane socket path %s, got %s", sockPath, sp)
	}

	// Test prompt delivery via client
	cli := herdr.NewClient("")
	cli.Runner = &mockCLIProxy{sockPath: sockPath}
	err = cli.Prompt(context.Background(), p.ID, "Unit test message")
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
}

type mockCLIProxy struct {
	sockPath string
}

func (m *mockCLIProxy) Run(_ context.Context, args ...string) ([]byte, []byte, error) {
	if len(args) >= 4 && args[0] == "agent" && args[1] == "prompt" {
		resp, err := herdr.CallSocket(m.sockPath, "agent.prompt", map[string]any{
			"target":  args[2],
			"message": args[3],
		})
		return resp, nil, err
	}
	return nil, nil, nil
}
