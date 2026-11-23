//go:build unix

package perfrecorder

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDumpOnSignal(t *testing.T) {
	dir := t.TempDir()
	dumped := make(chan string, 1)
	errCh := make(chan error, 1)

	r := New(WithOnDump(func(path string, err error) {
		if err != nil {
			errCh <- err
			return
		}
		dumped <- path
	}))
	r.RecordUpdate("tea.KeyMsg", 5*time.Millisecond)
	r.SampleRuntime()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := r.DumpOnSignal(ctx, dir, syscall.SIGUSR1)
	defer stop()

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess: %v", err)
	}
	if err := proc.Signal(syscall.SIGUSR1); err != nil {
		t.Fatalf("Signal(SIGUSR1): %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("DumpOnSignal callback error: %v", err)
	case path := <-dumped:
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "pigasus-perf-") || !strings.HasSuffix(base, ".zip") {
			t.Fatalf("unexpected bundle filename %q", base)
		}
		zr, err := zip.OpenReader(path)
		if err != nil {
			t.Fatalf("open dumped zip %q: %v", path, err)
		}
		_ = zr.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for DumpOnSignal bundle")
	}
}
