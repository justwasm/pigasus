package terminal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/infra/terminalmgr"
)

func TestTool_MinimalDeclAndActions(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	decl := MinimalDecl()
	if decl.Name != "terminal" {
		t.Fatalf("decl.Name = %q, want terminal", decl.Name)
	}
	if strings.Contains(decl.Description, "capture") || strings.Contains(decl.Description, "Ctrl+C") {
		t.Fatalf("MinimalDecl description should stay minimal, got %q", decl.Description)
	}

	ws := t.TempDir()
	mgr := terminalmgr.New(ws)
	defer mgr.CloseAll()
	tool := &Tool{Manager: mgr}

	ctx := context.Background()

	// Empty list.
	out, err := tool.Run(ctx, json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out.(string), "No active terminal sessions") {
		t.Fatalf("unexpected list output: %v", out)
	}

	// Nested args: new session.
	out, err = tool.Run(ctx, json.RawMessage(`{"action":"new","args":{"name":"work","command":"printf 'TOOL_%s\\n' NEW_OK","wait_ms":200}}`))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if !strings.Contains(out.(string), "work") {
		t.Fatalf("new output missing session name: %v", out)
	}

	// Poll capture until shell prints marker.
	deadline := time.Now().Add(25 * time.Second)
	var capOut string
	lastRetry := time.Now()
	for {
		res, err := tool.Run(ctx, json.RawMessage(`{"action":"capture","args":{"id":"work"}}`))
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		capOut = res.(string)
		if strings.Contains(capOut, "TOOL_NEW_OK") {
			break
		}
		if time.Since(lastRetry) >= 2*time.Second {
			lastRetry = time.Now()
			_, _ = tool.Run(ctx, json.RawMessage(`{"action":"send","args":{"id":"work","text":"printf 'TOOL_%s\\n' NEW_OK\n","keys":["Enter"]}}`))
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for TOOL_NEW_OK in capture:\n%s", capOut)
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Flat args compatibility: send command.
	if _, err := tool.Run(ctx, json.RawMessage(`{"action":"send","id":"work","text":"printf 'TOOL_%s\\n' SEND_OK\n","keys":["Enter"],"wait_ms":200}`)); err != nil {
		t.Fatalf("send: %v", err)
	}
	deadline = time.Now().Add(25 * time.Second)
	lastRetry = time.Now()
	for {
		res, err := tool.Run(ctx, json.RawMessage(`{"action":"capture","args":{"id":"work"}}`))
		if err != nil {
			t.Fatalf("capture after send: %v", err)
		}
		if strings.Contains(res.(string), "TOOL_SEND_OK") {
			break
		}
		if time.Since(lastRetry) >= 2*time.Second {
			lastRetry = time.Now()
			_, _ = tool.Run(ctx, json.RawMessage(`{"action":"send","id":"work","text":"printf 'TOOL_%s\\n' SEND_OK\n","keys":["Enter"]}}`))
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for TOOL_SEND_OK:\n%s", res.(string))
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Resize.
	res, err := tool.Run(ctx, json.RawMessage(`{"action":"resize","args":{"id":"work","rows":30,"cols":100}}`))
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if !strings.Contains(res.(string), "30x100") {
		t.Fatalf("unexpected resize output: %v", res)
	}

	// Rename & Switch.
	res, err = tool.Run(ctx, json.RawMessage(`{"action":"rename","args":{"id":"work","title":"build-tab"}}`))
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !strings.Contains(res.(string), `to "build-tab"`) {
		t.Fatalf("unexpected rename output: %v", res)
	}
	res, err = tool.Run(ctx, json.RawMessage(`{"action":"switch","args":{"id":"build-tab"}}`))
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if !strings.Contains(res.(string), "Switched active terminal") {
		t.Fatalf("unexpected switch output: %v", res)
	}

	// Kill.
	res, err = tool.Run(ctx, json.RawMessage(`{"action":"kill","args":{"id":"build-tab"}}`))
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !strings.Contains(res.(string), "Closed terminal") {
		t.Fatalf("unexpected kill output: %v", res)
	}
}

func TestTool_ResolveBackendIPCAndSelfCapture(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	ws := t.TempDir()
	hostMgr := terminalmgr.New(ws)
	defer hostMgr.CloseAll()

	ctx := context.Background()
	created, err := hostMgr.New(ctx, terminalmgr.NewOptions{
		Name:    "pigasus-tab",
		Command: "printf 'SELF_TUI_%s\\n' VISIBLE",
		WaitMs:  intPtr(250),
	})
	if err != nil {
		t.Fatalf("hostMgr.New: %v", err)
	}

	sockDir, err := os.MkdirTemp("/tmp", "pgt-tool-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(sockDir)
	sockPath := filepath.Join(sockDir, "term.sock")

	srv, err := terminalmgr.StartIPCServer(sockPath, hostMgr)
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	defer srv.Close()

	t.Setenv(terminalmgr.EnvSocket, sockPath)
	t.Setenv(terminalmgr.EnvSessionID, created.Session.ID)

	backend, closeFn := terminalmgr.ResolveBackend(ws, nil)
	defer closeFn()
	tool := &Tool{Manager: backend}

	deadline := time.Now().Add(10 * time.Second)
	for {
		res, err := tool.Run(ctx, json.RawMessage(`{"action":"capture","args":{"id":"self"}}`))
		if err != nil {
			t.Fatalf("capture self: %v", err)
		}
		out := res.(string)
		if strings.Contains(out, "SELF_TUI_VISIBLE") && strings.Contains(out, "self=true") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for SELF_TUI_VISIBLE in self capture:\n%s", out)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func intPtr(v int) *int { return &v }
