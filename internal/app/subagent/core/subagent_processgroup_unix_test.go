//go:build !windows && !js

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
	"time"
)

// The shell and its child both retain the captured output descriptors. Waiting
// for the command therefore also verifies that termination reached the child.
func startProcessGroupSubagent(t *testing.T, s *Subagent, wait func(*Subagent) error) (<-chan struct{}, *os.File) {
	t.Helper()
	if s.Cmd.SysProcAttr == nil || !s.Cmd.SysProcAttr.Setpgid {
		t.Fatal("subagent command must request its own process group before Start")
	}
	ready, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ready.Close(); _ = writer.Close() })
	s.Cmd.ExtraFiles = []*os.File{writer}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	childPID := 0
	done := make(chan struct{})
	go func() {
		_ = wait(s)
		close(done)
	}()
	t.Cleanup(func() {
		// Only signal known individual processes during failure cleanup. Even a
		// regression in Setpgid must never signal the test runner's group.
		if childPID > 0 {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
		}
		_ = s.Cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("subagent did not finish during cleanup")
		}
	})
	if err := ready.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fscan(ready, &childPID); err != nil {
		t.Fatalf("reading descendant PID: %v", err)
	}
	for _, pid := range []int{s.Cmd.Process.Pid, childPID} {
		pgid, err := syscall.Getpgid(pid)
		if err != nil {
			t.Fatalf("Getpgid(%d): %v", pid, err)
		}
		if pgid != s.Cmd.Process.Pid || pgid == syscall.Getpgrp() {
			t.Fatalf("PID %d group = %d, want isolated group %d", pid, pgid, s.Cmd.Process.Pid)
		}
	}
	return done, ready
}

func TestSubagentProcessGroup_KillAndStopDescendants(t *testing.T) {
	for _, action := range []string{"kill", "stop"} {
		t.Run(action, func(t *testing.T) {
			s := NewSubagent("sh", "-c", `sleep 30 & printf '%s\n' "$!" >&3; wait`)
			done, _ := startProcessGroupSubagent(t, s, func(s *Subagent) error { return s.Cmd.Wait() })
			var err error
			if action == "kill" {
				err = s.Kill()
			} else {
				err = s.Stop()
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("termination left a descendant holding the output pipes open")
			}
		})
	}
}

func TestSubagentProcessGroup_ContextCancellation(t *testing.T) {
	s := NewSubagent("sh", "-c", `sleep 30 & printf '%s\n' "$!" >&3; wait`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitErr := make(chan error, 1)
	done, ready := startProcessGroupSubagent(t, s, func(s *Subagent) error {
		err := s.Wait(ctx)
		waitErr <- err
		return err
	})
	cancel()
	select {
	case <-done:
		if err := <-waitErr; !errors.Is(err, context.Canceled) {
			t.Fatalf("Wait error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after cancellation")
	}
	if _, err := io.ReadAll(ready); err != nil {
		t.Fatalf("waiting for descendant pipe EOF after cancellation: %v", err)
	}
}

func TestSubagentProcessGroup_Resume(t *testing.T) {
	s := NewSubagent("sleep", "30")
	if s.Cmd.SysProcAttr == nil || !s.Cmd.SysProcAttr.Setpgid {
		t.Fatal("initial command is not isolated")
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	first := s.Cmd
	t.Cleanup(func() { _ = first.Process.Kill() })
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Cmd.Process.Kill(); _ = s.Cmd.Wait() })
	if s.Cmd == first {
		t.Fatal("Resume reused the stopped command")
	}
	pgid, err := syscall.Getpgid(s.Cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if pgid != s.Cmd.Process.Pid || pgid == syscall.Getpgrp() {
		t.Fatalf("resumed process group = %d, want isolated group %d", pgid, s.Cmd.Process.Pid)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
}
