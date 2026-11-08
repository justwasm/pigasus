package mockremoteagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

// TestTerminalRoundTrip drives the proxy's real PTY through the client:
// create, list, send input, stream the output, then close on exit.
func TestTerminalRoundTrip(t *testing.T) {
	if openPTYUnsupported() {
		t.Skip("terminals are not supported on this platform")
	}
	dir := t.TempDir()
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	srv.service.cascades.CreateSession("c1", "", "m", []string{"file://" + dir}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL()})

	info, err := client.CreateTerminal(ctx, "c1", "req-1")
	if err != nil {
		t.Fatalf("CreateTerminal: %v", err)
	}
	if info.TerminalID == "" || info.Cwd != dir || info.Pid == 0 {
		t.Fatalf("terminal = %+v", info)
	}
	list, err := client.ListTerminals(ctx, "c1")
	if err != nil || len(list) != 1 || list[0].TerminalID != info.TerminalID {
		t.Fatalf("ListTerminals = %+v, %v", list, err)
	}

	events, err := client.StreamTerminalOutput(ctx, info.TerminalID, 0)
	if err != nil {
		t.Fatalf("StreamTerminalOutput: %v", err)
	}
	if _, err := client.SendTerminalInput(ctx, info.TerminalID, []byte("echo terminal-ok\n"), "", 0); err != nil {
		t.Fatalf("SendTerminalInput: %v", err)
	}

	var output strings.Builder
	deadline := time.After(10 * time.Second)
	for !strings.Contains(output.String(), "terminal-ok") {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("stream closed before the command output; got %q", output.String())
			}
			output.Write(event.Output)
		case <-deadline:
			t.Fatalf("timed out; output so far %q", output.String())
		}
	}

	if _, err := client.SendTerminalInput(ctx, info.TerminalID, []byte("exit\n"), "", 0); err != nil {
		t.Fatalf("SendTerminalInput(exit): %v", err)
	}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("stream closed without an exit frame")
			}
			if event.ExitCode != nil {
				if *event.ExitCode != 0 {
					t.Errorf("exit code = %d", *event.ExitCode)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for the exit frame")
		}
	}
}

// TestCloseTerminalKillsThePTY covers closeTerminal.
func TestCloseTerminalKillsThePTY(t *testing.T) {
	if openPTYUnsupported() {
		t.Skip("terminals are not supported on this platform")
	}
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL()})
	info, err := client.CreateTerminal(ctx, "", "")
	if err != nil {
		t.Fatalf("CreateTerminal: %v", err)
	}
	if err := client.CloseTerminal(ctx, info.TerminalID); err != nil {
		t.Fatalf("CloseTerminal: %v", err)
	}
	list, err := client.ListTerminals(ctx, "")
	if err != nil {
		t.Fatalf("ListTerminals: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("terminals = %+v", list)
	}
	if err := client.CloseTerminal(ctx, "missing"); err == nil {
		t.Error("CloseTerminal on an unknown id should fail")
	}
}
