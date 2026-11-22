package tui

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLocalTerminalService_Lifecycle(t *testing.T) {
	dir := t.TempDir()
	svc := NewLocalTerminalService(dir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Create a terminal session in temp workspace
	info, err := svc.CreateTerminal(ctx, dir)
	if err != nil {
		t.Fatalf("CreateTerminal: %v", err)
	}
	if info.TerminalID == "" {
		t.Fatalf("expected non-empty TerminalID")
	}
	if info.PID <= 0 {
		t.Fatalf("expected valid PID, got %d", info.PID)
	}

	// 2. List terminals
	list, err := svc.ListTerminals(ctx)
	if err != nil {
		t.Fatalf("ListTerminals: %v", err)
	}
	if len(list) != 1 || list[0].TerminalID != info.TerminalID {
		t.Fatalf("ListTerminals returned %+v, want terminal %s", list, info.TerminalID)
	}

	// 3. Stream terminal output
	events, err := svc.StreamTerminal(ctx, info.TerminalID, 0)
	if err != nil {
		t.Fatalf("StreamTerminal: %v", err)
	}

	// 4. Send input command
	cmdText := "echo PTY_TEST_OK\n"
	if err := svc.SendTerminalInput(ctx, info.TerminalID, []byte(cmdText)); err != nil {
		t.Fatalf("SendTerminalInput: %v", err)
	}

	// 5. Read output from stream until we see our echoed marker
	found := false
	deadline := time.Now().Add(5 * time.Second)
	var outputCollected []string
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-events:
			if !ok {
				break
			}
			if len(ev.Output) > 0 {
				str := string(ev.Output)
				outputCollected = append(outputCollected, str)
				if strings.Contains(str, "PTY_TEST_OK") {
					found = true
					break
				}
			}
		case <-time.After(100 * time.Millisecond):
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("expected 'PTY_TEST_OK' in terminal output, collected:\n%s", strings.Join(outputCollected, ""))
	}

	// 6. Resize
	if err := svc.Resize(ctx, info.TerminalID, 30, 100); err != nil {
		t.Errorf("Resize: %v", err)
	}

	// 7. Close terminal
	if err := svc.CloseTerminal(ctx, info.TerminalID); err != nil {
		t.Fatalf("CloseTerminal: %v", err)
	}

	listAfter, err := svc.ListTerminals(ctx)
	if err != nil {
		t.Fatalf("ListTerminals after close: %v", err)
	}
	if len(listAfter) != 0 {
		t.Errorf("expected 0 terminals after close, got %d", len(listAfter))
	}
}
