package tui

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	terminaltool "github.com/justwasm/pigasus/internal/app/tools/terminal"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
)

func TestTerminalTool_TUISharedRegistryAndNoApproval(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	ws := t.TempDir()
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", ws, true)
	defer m.CloseAllTerminals()

	catalog := surface.RuntimeCatalog(ws)
	decls := m.registerTerminalTool(catalog, toolrunner.AdvertisedToolDecls("", catalog))

	foundDecl := false
	for _, d := range decls {
		if d.Name == terminaltool.ToolName {
			foundDecl = true
			break
		}
	}
	if !foundDecl {
		t.Fatal("registerTerminalTool did not append terminal declaration")
	}

	// Even under Strict + Plan mode with sandbox enabled, terminal must run without approval or denial.
	askCalled := false
	runner := &permissionRunner{
		inner: &toolrunner.RegistryRunner{Reg: catalog},
		policy: &permissions.Policy{
			Mode:           permissions.ModePlan,
			ToolPermission: permissions.ToolPermissionStrict,
		},
		sandbox: sandbox.Sandbox{Enabled: true, Cwd: ws},
		ask: func(ctx context.Context, call backend.ToolCall) (bool, error) {
			askCalled = true
			return false, nil
		},
	}

	ctx := context.Background()
	out, err := runner.Run(ctx, backend.ToolCall{
		Name:      "terminal",
		Arguments: json.RawMessage(`{"action":"new","args":{"name":"tui-shared","command":"printf 'TUI_%s\\n' SHARED_OK","wait_ms":200}}`),
	})
	if err != nil {
		t.Fatalf("terminal new: %v", err)
	}
	if askCalled {
		t.Fatal("terminal tool triggered approval prompt; expected no approval")
	}
	if len(runner.Denials) != 0 {
		t.Fatalf("unexpected denials: %v", runner.Denials)
	}
	if !strings.Contains(out, "tui-shared") {
		t.Fatalf("unexpected new output:\n%s", out)
	}

	// Verify /terminal's TerminalService sees the exact same session.
	terminals, err := m.terminalService().ListTerminals(ctx)
	if err != nil {
		t.Fatalf("ListTerminals: %v", err)
	}
	if len(terminals) != 1 {
		t.Fatalf("ListTerminals = %+v, want 1 shared session", terminals)
	}

	// Poll capture until shell prints marker.
	deadline := time.Now().Add(10 * time.Second)
	for {
		capOut, err := runner.Run(ctx, backend.ToolCall{
			Name:      "terminal",
			Arguments: json.RawMessage(`{"action":"capture","args":{"id":"tui-shared"}}`),
		})
		if err != nil {
			t.Fatalf("terminal capture: %v", err)
		}
		if strings.Contains(capOut, "TUI_SHARED_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for TUI_SHARED_OK:\n%s", capOut)
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Verify TUI step title formatting distinguishes terminal tool from bash alias.
	if got := tuitools.ToolStepTitle("terminal", json.RawMessage(`{"action":"capture","args":{"id":"tui-shared"}}`)); got != "Terminal(capture tui-shared)" {
		t.Fatalf("ToolStepTitle = %q, want Terminal(capture tui-shared)", got)
	}
	if got := tuitools.ToolStepTitle("terminal", json.RawMessage(`{"command":"ls"}`)); got != "Bash(ls)" {
		t.Fatalf("ToolStepTitle bash alias = %q, want Bash(ls)", got)
	}
}
