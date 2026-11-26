package terminalmgr

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/rcterminal"
)

func useFastTestShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Setenv("SHELL", "/bin/sh")
	}
	// Shell startup and tool discovery must use fixture state, never an
	// operator's profile or a terminal session owned by the parent process.
	t.Setenv("ZDOTDIR", t.TempDir())
	t.Setenv("ENV", "")
	t.Setenv("BASH_ENV", "")
	t.Setenv("PIGASUS_TERMINAL_SOCKET", "")
	t.Setenv("PIGASUS_TERMINAL_SESSION", "")
}

func waitForScreenContains(t *testing.T, mgr *Manager, id, substr string, timeout time.Duration) CaptureResult {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		res, err := mgr.Capture(context.Background(), CaptureOptions{ID: id})
		if err == nil && strings.Contains(strings.Join(res.Screen, "\n"), substr) {
			return res
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q in screen; last=%v err=%v", substr, res.Screen, err)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func TestManager_LifecycleAndTmuxWorkflow(t *testing.T) {
	useFastTestShell(t)
	ws := t.TempDir()
	mgr := New(ws)
	defer mgr.CloseAll()

	ctx := context.Background()
	wait := 100
	res, err := mgr.New(ctx, NewOptions{
		Name:    "dev",
		Cwd:     ws,
		Command: "printf 'READY_%s\\n' MARKER",
		Rows:    12,
		Cols:    60,
		WaitMs:  &wait,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if res.Session.Name != "dev" {
		t.Fatalf("Name = %q, want dev", res.Session.Name)
	}
	waitForScreenContains(t, mgr, "dev", "READY_MARKER", 10*time.Second)

	// List should include the named session.
	list := mgr.List()
	if len(list) != 1 || list[0].ID != res.Session.ID || list[0].Name != "dev" {
		t.Fatalf("List = %+v", list)
	}

	// Send a command using text + Enter key.
	if _, err := mgr.Send(ctx, SendOptions{
		ID:     "dev",
		Text:   "printf 'SECOND_%s\\n' LINE",
		Keys:   []string{"Enter"},
		WaitMs: &wait,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitForScreenContains(t, mgr, "dev", "SECOND_LINE", 10*time.Second)

	// Resize should update dimensions.
	resized, err := mgr.Resize("dev", 20, 90)
	if err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if resized.Rows != 20 || resized.Cols != 90 {
		t.Fatalf("resized = %dx%d, want 20x90", resized.Rows, resized.Cols)
	}

	// Generate scrollback and verify capture scrollback.
	if _, err := mgr.Send(ctx, SendOptions{
		ID:     "dev",
		Text:   "i=1; while [ $i -le 30 ]; do printf 'SB_ROW_%d\\n' $i; i=$((i+1)); done",
		Keys:   []string{"Enter"},
		WaitMs: &wait,
	}); err != nil {
		t.Fatalf("Send scrollback loop: %v", err)
	}
	waitForScreenContains(t, mgr, "dev", "SB_ROW_30", 10*time.Second)
	capRes, err := mgr.Capture(ctx, CaptureOptions{ID: "dev", Scrollback: 50})
	if err != nil {
		t.Fatalf("Capture scrollback: %v", err)
	}
	if len(capRes.Scrollback) == 0 {
		t.Fatalf("expected non-empty scrollback lines")
	}

	// Start a blocking sleep and interrupt it with Ctrl+C.
	if _, err := mgr.Send(ctx, SendOptions{
		ID:     "dev",
		Text:   "sleep 30",
		Keys:   []string{"Enter"},
		WaitMs: &wait,
	}); err != nil {
		t.Fatalf("Send sleep: %v", err)
	}
	if _, err := mgr.Send(ctx, SendOptions{
		ID:     "dev",
		Keys:   []string{"Ctrl+C"},
		WaitMs: &wait,
	}); err != nil {
		t.Fatalf("Send Ctrl+C: %v", err)
	}
	if _, err := mgr.Send(ctx, SendOptions{
		ID:     "dev",
		Text:   "printf 'AFTER_%s\\n' SIGINT",
		Keys:   []string{"Enter"},
		WaitMs: &wait,
	}); err != nil {
		t.Fatalf("Send after Ctrl+C: %v", err)
	}
	waitForScreenContains(t, mgr, "dev", "AFTER_SIGINT", 10*time.Second)

	// Kill session.
	if _, err := mgr.Kill("dev"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if got := mgr.List(); len(got) != 0 {
		t.Fatalf("List after Kill = %+v, want empty", got)
	}
}

func TestManager_SyncFromSharedRegistry(t *testing.T) {
	useFastTestShell(t)
	ws := t.TempDir()
	reg := rcterminal.NewRegistry()
	reg.SetWorkspace(ws)
	mgr := NewWithRegistry(reg)
	defer mgr.CloseAll()

	// Simulate TUI /terminal creating a session directly in the shared registry.
	info, err := reg.Create(rcterminal.CreateRequest{Workspace: ws})
	if err != nil {
		t.Fatalf("reg.Create: %v", err)
	}
	if err := reg.Write(info.TerminalID, []byte("printf 'SHARED_%s\\n' REG_OK\r")); err != nil {
		t.Fatalf("reg.Write: %v", err)
	}

	waitForScreenContains(t, mgr, info.TerminalID, "SHARED_REG_OK", 10*time.Second)
	list := mgr.List()
	if len(list) != 1 || list[0].ID != info.TerminalID {
		t.Fatalf("List = %+v, want %s", list, info.TerminalID)
	}

	// Closing directly in registry should prune from Manager on SyncFromRegistry/List.
	if err := reg.Close(info.TerminalID, true); err != nil {
		t.Fatalf("reg.Close: %v", err)
	}
	if got := mgr.List(); len(got) != 0 {
		t.Fatalf("List after reg.Close = %+v, want empty", got)
	}
}

func TestEncodeKeys(t *testing.T) {
	got, err := EncodeKeys([]string{"Ctrl+C", "Enter", "Up", "Alt+x", "PgDn"}, false)
	if err != nil {
		t.Fatalf("EncodeKeys: %v", err)
	}
	want := "\x03\r\x1b[A\x1bx\x1b[6~"
	if string(got) != want {
		t.Fatalf("EncodeKeys = %q, want %q", string(got), want)
	}

	appGot, err := EncodeKeys([]string{"Up", "Home"}, true)
	if err != nil {
		t.Fatalf("EncodeKeys app mode: %v", err)
	}
	if string(appGot) != "\x1bOA\x1bOH" {
		t.Fatalf("EncodeKeys app mode = %q, want %q", string(appGot), "\x1bOA\x1bOH")
	}

	if _, err := EncodeKeys([]string{"NoSuchKeyName"}, false); err == nil {
		t.Fatal("expected error for unsupported key name")
	}
	_ = fmt.Sprintf("ok")
}
