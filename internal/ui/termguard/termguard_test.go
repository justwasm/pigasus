package termguard

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestGuard_EmitsResetEscapeSequence(t *testing.T) {
	buf := &bytes.Buffer{}
	g := Capture()
	g.SetWriter(buf)
	g.Restore()

	if !strings.Contains(buf.String(), "\x1b[0m") {
		t.Error("expected reset sequence to contain SGR reset (\\x1b[0m)")
	}
	if !strings.Contains(buf.String(), "\x1b[?25h") {
		t.Error("expected reset sequence to restore cursor visibility (\\x1b[?25h)")
	}
	if !strings.Contains(buf.String(), "\x1b[?1047l") {
		t.Error("expected reset sequence to exit alternate screen without restoring cursor (\\x1b[?1047l)")
	}
	if strings.Contains(buf.String(), "\x1b[?1049l") {
		t.Error("reset sequence must NOT restore cursor position (\\x1b[?1049l)")
	}
	if strings.Contains(buf.String(), "\x1b[?1048l") || strings.Contains(buf.String(), "\x1b[u") {
		t.Error("reset sequence must NOT restore cursor position")
	}
	if !strings.Contains(buf.String(), "\x1b[?1000l") {
		t.Error("expected reset sequence to disable mouse tracking (\\x1b[?1000l)")
	}

	// Idempotency: second call must be a no-op
	lenBefore := buf.Len()
	g.Restore()
	if buf.Len() != lenBefore {
		t.Errorf("second Restore() modified buffer: got len %d, want %d", buf.Len(), lenBefore)
	}
}

func TestGuard_ConcurrentRestore(t *testing.T) {
	buf := &bytes.Buffer{}
	g := Capture()
	g.SetWriter(buf)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Restore()
		}()
	}
	wg.Wait()

	if buf.String() != ResetEscapeSequence {
		t.Errorf("expected exactly one reset sequence under concurrent calls, got: %q", buf.String())
	}
}

func TestRun_NormalExecution(t *testing.T) {
	executed := false
	err := Run(func() error {
		executed = true
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if !executed {
		t.Error("expected fn to be executed")
	}
}

func TestRun_NormalError(t *testing.T) {
	wantErr := errors.New("custom failure")
	err := Run(func() error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("got %v, want %v", err, wantErr)
	}
}

func TestRun_PanicRecovery(t *testing.T) {
	err := Run(func() error {
		panic("simulated fatal crash in TUI")
	})
	if err == nil {
		t.Fatal("expected non-nil error after panic, got nil")
	}
	if !strings.Contains(err.Error(), "simulated fatal crash in TUI") {
		t.Errorf("error %q should contain panic message", err.Error())
	}
}
