package terminalmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIPCAndMCP_RoundTripAndSelfCapture(t *testing.T) {
	useFastTestShell(t)
	ws := t.TempDir()
	mgr := New(ws)
	defer mgr.CloseAll()

	sockDir, err := os.MkdirTemp("/tmp", "pgt-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(sockDir)
	sockPath := filepath.Join(sockDir, "term.sock")
	srv, err := StartIPCServer(sockPath, mgr)
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	defer srv.Close()

	ctx := context.Background()
	created, err := mgr.New(ctx, NewOptions{
		Name:    "tui-tab",
		Command: "printf 'SELF_TUI_%s\\n' VISIBLE",
		WaitMs:  intPtr(200),
	})
	if err != nil {
		t.Fatalf("mgr.New: %v", err)
	}

	t.Setenv(EnvSocket, sockPath)
	t.Setenv(EnvSessionID, created.Session.ID)

	resolved, cleanup := ResolveBackend(ws, nil)
	defer cleanup()
	client, ok := resolved.(*IPCClient)
	if !ok {
		t.Fatalf("ResolveBackend returned %T, want *IPCClient", resolved)
	}

	// Wait for marker output via IPC capture("self")
	deadline := time.Now().Add(10 * time.Second)
	for {
		capRes, err := client.Capture(ctx, CaptureOptions{ID: "self"})
		if err != nil {
			t.Fatalf("client.Capture(self): %v", err)
		}
		if !capRes.Session.Self {
			t.Fatalf("expected Session.Self=true on self capture, got %+v", capRes.Session)
		}
		if strings.Contains(strings.Join(capRes.Screen, "\n"), "SELF_TUI_VISIBLE") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for SELF_TUI_VISIBLE in %v", capRes.Screen)
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Rename & Switch via IPC
	renamed, err := client.Rename("self", "pigasus-main")
	if err != nil {
		t.Fatalf("client.Rename: %v", err)
	}
	if renamed.Title != "pigasus-main" {
		t.Fatalf("renamed.Title = %q, want pigasus-main", renamed.Title)
	}

	// MCP stdio round-trip: initialize, tools/list, tools/call(terminal_capture self), resources/read(terminal://self/screen)
	reqs := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"terminal_capture","arguments":{"id":"self"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"terminal://self/screen"}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := ServeMCP(ctx, strings.NewReader(reqs), &out, client, created.Session.ID); err != nil {
		t.Fatalf("ServeMCP: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 MCP responses (notification skipped), got %d:\n%s", len(lines), out.String())
	}
	var r3 struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &r3); err != nil {
		t.Fatalf("unmarshal r3: %v", err)
	}
	if r3.Result.IsError || len(r3.Result.Content) == 0 || !strings.Contains(r3.Result.Content[0].Text, "SELF_TUI_VISIBLE") {
		t.Fatalf("unexpected MCP terminal_capture(self) response: %s", lines[2])
	}
	if !strings.Contains(lines[3], "SELF_TUI_VISIBLE") {
		t.Fatalf("unexpected MCP resources/read(terminal://self/screen) response: %s", lines[3])
	}
}

func intPtr(v int) *int { return &v }
