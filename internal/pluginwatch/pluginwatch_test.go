// pluginwatch_test.go — coverage for the four behaviours the package
// is required to provide:
//
//  1. a write under a watched root flips IsStale (TestWatcher_DetectsNewFile);
//  2. a burst of events within debounceWindow collapses to a single
//     staleness transition (TestWatcher_Debounce);
//  3. New is robust to a missing ~/.gemini tree (TestWatcher_MissingRootIsOK);
//  4. Close stops the goroutine cleanly (TestWatcher_CloseStopsRun).
//
// Run:
//
//	go test -vet=off -count=1 ./internal/pluginwatch/
package pluginwatch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls cond up to timeout, returning true the first time it
// returns true. The watcher tests need this — fsnotify event delivery
// is asynchronous and the debounce window adds another 200 ms on top.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// TestWatcher_DetectsNewFile: write a file under a watched root and
// observe IsStale transitioning to true.
func TestWatcher_DetectsNewFile(t *testing.T) {
	root := t.TempDir()
	// Set up a synthetic ~/.gemini layout: the watcher only adds a
	// watch for the three plugin roots, so we have to populate all
	// three so the watcher sees at least one valid root and
	// constructs.
	gemini := filepath.Dir(root)
	pluginsDir := filepath.Join(gemini, "config", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(gemini, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marketDir := filepath.Join(root, "cache", "marketplaces")
	if err := os.MkdirAll(marketDir, 0o755); err != nil {
		t.Fatal(err)
	}

	w, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if w == nil {
		t.Fatal("New returned (nil, nil) but the roots exist")
	}
	t.Cleanup(func() { _ = w.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go w.Run(ctx)

	if w.IsStale() {
		t.Fatal("IsStale should be false immediately after Run")
	}

	// fsnotify fires on the directory for a Create event; the
	// debounce timer then runs for 200 ms before IsStale flips.
	if err := os.WriteFile(filepath.Join(pluginsDir, "demo.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, w.IsStale) {
		t.Fatal("IsStale did not flip after a Create event")
	}

	w.MarkFresh()
	if w.IsStale() {
		t.Fatal("IsStale should be false after MarkFresh")
	}
}

// TestWatcher_Debounce: a burst of events within debounceWindow
// collapses to one flip. We measure by hooking a custom debugf and
// counting the (re)arms of the timer — the underlying fsnotify
// doesn't expose a flip counter, but the debounceMu serialisation
// means each queueFlip runs while holding the lock. The simpler
// observable is "exactly one transition from fresh → stale":
// MarkFresh between events should remain false until the burst ends.
func TestWatcher_Debounce(t *testing.T) {
	root := t.TempDir()
	gemini := filepath.Dir(root)
	pluginsDir := filepath.Join(gemini, "config", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(gemini, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marketDir := filepath.Join(root, "cache", "marketplaces")
	if err := os.MkdirAll(marketDir, 0o755); err != nil {
		t.Fatal(err)
	}

	w, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if w == nil {
		t.Fatal("New returned (nil, nil) but the roots exist")
	}
	t.Cleanup(func() { _ = w.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go w.Run(ctx)

	// Sleep until the watcher is settled (no pending timer), so the
	// burst we emit starts from a known-fresh state.
	if !waitFor(t, 500*time.Millisecond, func() bool { return !w.hasPendingTimer() }) {
		t.Fatal("watcher never reached the no-pending-timer idle state")
	}

	// Emit five events 50 ms apart. With debounceWindow = 200 ms the
	// timer is reset every 50 ms and never fires during the burst;
	// IsStale must remain false throughout.
	var transitions atomic.Int32
	w.MarkFresh()
	for i := 0; i < 5; i++ {
		if w.IsStale() {
			transitions.Add(1)
			w.MarkFresh()
		}
		time.Sleep(50 * time.Millisecond)
		// Each touch creates a fresh event on the watched dir.
		if err := os.WriteFile(filepath.Join(pluginsDir, "touch"), []byte{byte(i)}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := transitions.Load(); got != 0 {
		t.Errorf("expected zero transitions during burst, got %d", got)
	}
	// After the burst ends the timer fires once → exactly one
	// transition.
	if !waitFor(t, 2*time.Second, w.IsStale) {
		t.Fatal("expected exactly one transition after burst end")
	}
}

// TestWatcher_MissingRootIsOK: a non-existent or empty home dir
// must not produce an error from New — it should be (nil, nil) so
// the loader's empty-state code path takes over.
func TestWatcher_MissingRootIsOK(t *testing.T) {
	cases := map[string]string{
		"empty string":     "",
		"non-existent dir": "/this/does/not/exist/somewhere/deep",
	}
	for name, dir := range cases {
		t.Run(name, func(t *testing.T) {
			w, err := New(dir)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if w != nil {
				t.Errorf("New should have returned (nil, nil), got watcher=%v", w)
				_ = w.Close()
			}
			// All methods on a nil receiver must be safe.
			if w.IsStale() {
				t.Error("nil Watcher IsStale should be false")
			}
			w.MarkFresh() // must not panic
			if err := w.Close(); err != nil {
				t.Errorf("nil Watcher Close returned %v, want nil", err)
			}
			w.Run(context.Background()) // must not panic
		})
	}

	// Also cover the "home dir exists but none of the three roots do"
	// case — New should still return (nil, nil) without error.
	t.Run("home exists, no roots", func(t *testing.T) {
		emptyHome := t.TempDir()
		w, err := New(emptyHome)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if w != nil {
			t.Errorf("New should have returned (nil, nil) for empty home, got watcher=%v", w)
			_ = w.Close()
		}
	})
}

// TestWatcher_CloseStopsRun: starting Run then closing should not
// leak a goroutine. We allow a delta of ±2 because the Go runtime
// may have transient system goroutines (sweep, scavenger, etc.).
func TestWatcher_CloseStopsRun(t *testing.T) {
	root := t.TempDir()
	gemini := filepath.Dir(root)
	for _, sub := range []string{
		filepath.Join(gemini, "config", "plugins"),
		filepath.Join(gemini, "skills"),
		filepath.Join(root, "cache", "marketplaces"),
	} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Build and run several watchers; each one must stop cleanly.
	runtime.GC()
	baseline := runtime.NumGoroutine()
	for i := 0; i < 4; i++ {
		w, err := New(root)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if w == nil {
			t.Fatal("New returned (nil, nil) but the roots exist")
		}
		ctx, cancel := context.WithCancel(context.Background())
		go w.Run(ctx)
		cancel()
		if err := w.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}

	// Allow the watchers' goroutines to drain. 500 ms is generous;
	// the loop body is non-blocking.
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	final := runtime.NumGoroutine()
	if delta := final - baseline; delta > 2 || delta < -2 {
		t.Errorf("goroutine count drifted after Close: baseline=%d final=%d (delta=%d)",
			baseline, final, final-baseline)
	}
}
