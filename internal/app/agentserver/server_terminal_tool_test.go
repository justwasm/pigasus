package agentserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
)

func TestAgentServer_TerminalToolAndSkillAvailable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	t.Setenv("ZDOTDIR", t.TempDir())
	t.Setenv("ENV", "")
	t.Setenv("BASH_ENV", "")
	t.Setenv("PIGASUS_TERMINAL_SOCKET", "")
	t.Setenv("PIGASUS_TERMINAL_SESSION", "")
	home := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", home)
	ws := t.TempDir()

	srv, err := New(Config{
		Workspace:      ws,
		Model:          "gemini-2.5-pro",
		PermissionMode: "plan",
	})
	if err != nil {
		t.Fatalf("New server: %v", err)
	}

	sess, err := srv.sessionForID("conv-term-1", ws, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("sessionForID: %v", err)
	}
	if sess.Close != nil {
		defer sess.Close()
	}

	wantSkillPath := filepath.Join(home, "builtin-skills", "terminal", "SKILL.md")
	if !strings.Contains(sess.Prompt, wantSkillPath) {
		t.Fatalf("session prompt missing terminal skill path %q", wantSkillPath)
	}

	foundDecl := false
	for _, d := range sess.Tools {
		if d.Name == "terminal" {
			foundDecl = true
			break
		}
	}
	if !foundDecl {
		t.Fatal("session Tools missing terminal declaration")
	}

	ctx := context.Background()
	out, err := sess.Runner.Run(ctx, backend.ToolCall{
		Name:      "terminal",
		Arguments: json.RawMessage(`{"action":"new","args":{"name":"srv1","command":"printf 'SERVER_%s\\n' TERM_OK","wait_ms":200}}`),
	})
	if err != nil {
		t.Fatalf("Runner.Run new: %v", err)
	}
	if !strings.Contains(out, "srv1") {
		t.Fatalf("unexpected new output:\n%s", out)
	}

	deadline := time.Now().Add(25 * time.Second)
	lastRetry := time.Now()
	for {
		capOut, err := sess.Runner.Run(ctx, backend.ToolCall{
			Name:      "terminal",
			Arguments: json.RawMessage(`{"action":"capture","args":{"id":"srv1"}}`),
		})
		if err != nil {
			t.Fatalf("Runner.Run capture: %v", err)
		}
		if strings.Contains(capOut, "SERVER_TERM_OK") {
			break
		}
		if time.Since(lastRetry) >= 2*time.Second {
			lastRetry = time.Now()
			_, _ = sess.Runner.Run(ctx, backend.ToolCall{
				Name:      "terminal",
				Arguments: json.RawMessage(`{"action":"send","args":{"id":"srv1","input":"printf 'SERVER_%s\\n' TERM_OK\n","wait_ms":100}}`),
			})
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for SERVER_TERM_OK:\n%s", capOut)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
