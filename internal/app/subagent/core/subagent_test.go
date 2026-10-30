package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewSubagent(t *testing.T) {
	s := NewSubagent("echo", "hello")

	if s.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if !strings.HasPrefix(s.ID, "sub-") {
		t.Errorf("expected ID to start with 'sub-', got %q", s.ID)
	}
	if s.Name != "echo" {
		t.Errorf("expected Name=echo, got %q", s.Name)
	}
	if s.State != "pending" {
		t.Errorf("expected State=pending, got %q", s.State)
	}
	if s.Cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	if s.Err != nil {
		t.Errorf("expected nil Err before Start, got %v", s.Err)
	}
	if s.Result != "" {
		t.Errorf("expected empty Result before Start, got %q", s.Result)
	}
	if s.stdinWriter == nil {
		t.Error("expected stdinWriter wired by NewSubagent")
	}
	if s.stdoutBuf == nil {
		t.Error("expected stdout pipe wired by NewSubagent")
	}
}

func TestSubagentStartWait(t *testing.T) {
	s := NewSubagent("echo", "hello")
	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Wait(ctx); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	if s.State != "done" {
		t.Errorf("expected State=done after Wait, got %q", s.State)
	}
	if s.Err != nil {
		t.Errorf("expected nil Err on successful exit, got %v", s.Err)
	}
	if s.Result != "hello\n" {
		t.Errorf("expected Result=%q, got %q", "hello\n", s.Result)
	}

	// MarshalJSON should at minimum round-trip the ID/Name without error.
	b, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	if !strings.Contains(string(b), `"id":"`+s.ID+`"`) {
		t.Errorf("MarshalJSON missing id, got %s", b)
	}
	if !strings.Contains(string(b), `"state":"done"`) {
		t.Errorf("MarshalJSON missing state=done, got %s", b)
	}
}

func TestSubagentSend(t *testing.T) {
	s := NewSubagent("cat")
	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := s.Send("hi\n"); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Give `cat` a moment to echo back through the captured stdout pipe.
	time.Sleep(150 * time.Millisecond)

	if err := s.Kill(); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.Wait(ctx)

	if !strings.Contains(s.Result, "hi") {
		t.Errorf("expected Result to contain %q, got %q", "hi", s.Result)
	}
	if s.State != "killed" {
		if s.State != "killed" && s.State != "failed" {
		t.Errorf("expected State=killed or failed after Kill, got %q", s.State)
	}
	}
}