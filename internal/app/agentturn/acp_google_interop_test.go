package agentturn

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestGoogleOfficialACPInterop(t *testing.T) {
	serverBin := "/tmp/agy-extracted/agy_acp_server.par"
	if _, err := os.Stat(serverBin); os.IsNotExist(err) {
		t.Skip("Google official agy_acp_server.par not found at /tmp/agy-extracted/agy_acp_server.par")
	}

	geminiHome := t.TempDir()
	acpDir := filepath.Join(geminiHome, "antigravity-acp")
	_ = os.MkdirAll(acpDir, 0o755)
	_ = os.WriteFile(filepath.Join(acpDir, "settings.json"), []byte(`{"auth": {"type": "gemini-api-key"}}`), 0o644)

	cmd := exec.Command(serverBin)
	cmd.Env = append(os.Environ(), "GEMINI_HOME="+geminiHome, "GEMINI_API_KEY=dummy-test-key-for-acp-interop")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	remote, err := NewACPRemote(ctx, stdin, stdout, nil, t.TempDir(), "1")
	if err != nil {
		t.Fatalf("NewACPRemote against Google official ACP server failed: %v", err)
	}
	defer remote.Close()

	if remote.sessionV1 == nil || remote.sessionV1.ID == "" {
		t.Fatalf("expected active v1 session from Google official ACP server")
	}
	t.Logf("Successfully connected to Google official ACP server! Session ID: %s, Protocol V1 active", remote.sessionV1.ID)
}
