package terminal

import (
	"context"
	"encoding/json"
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
	deadline := time.Now().Add(10 * time.Second)
	var capOut string
	for {
		res, err := tool.Run(ctx, json.RawMessage(`{"action":"capture","args":{"id":"work"}}`))
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		capOut = res.(string)
		if strings.Contains(capOut, "TOOL_NEW_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for TOOL_NEW_OK in capture:\n%s", capOut)
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Flat args compatibility: send command.
	if _, err := tool.Run(ctx, json.RawMessage(`{"action":"send","id":"work","text":"printf 'TOOL_%s\\n' SEND_OK","keys":["Enter"],"wait_ms":200}`)); err != nil {
		t.Fatalf("send: %v", err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		res, err := tool.Run(ctx, json.RawMessage(`{"action":"capture","args":{"id":"work"}}`))
		if err != nil {
			t.Fatalf("capture after send: %v", err)
		}
		if strings.Contains(res.(string), "TOOL_SEND_OK") {
			break
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

	// Kill.
	res, err = tool.Run(ctx, json.RawMessage(`{"action":"kill","args":{"id":"work"}}`))
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !strings.Contains(res.(string), "Closed terminal") {
		t.Fatalf("unexpected kill output: %v", res)
	}
}
