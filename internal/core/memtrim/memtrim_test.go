package memtrim

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTrimMemory_Cooldown(t *testing.T) {
	var gcCount, osCount atomic.Int32

	oldGC := runtimeGCSeam
	oldOS := freeOSMemorySeam
	runtimeGCSeam = func() { gcCount.Add(1) }
	freeOSMemorySeam = func() { osCount.Add(1) }
	defer func() {
		runtimeGCSeam = oldGC
		freeOSMemorySeam = oldOS
	}()

	ResetStats()
	SetCooldown(200 * time.Millisecond)

	// Rapid calls in succession
	for i := 0; i < 10; i++ {
		TrimMemory()
	}

	// Give background goroutine a moment to finish
	time.Sleep(30 * time.Millisecond)

	stats := GetStats()
	if stats.TotalTrims != 1 {
		t.Fatalf("TotalTrims = %d, want 1", stats.TotalTrims)
	}
	if stats.SkippedTrims != 9 {
		t.Fatalf("SkippedTrims = %d, want 9", stats.SkippedTrims)
	}

	// After cooldown, should allow another trim
	time.Sleep(210 * time.Millisecond)
	if !TrimSync() {
		t.Fatal("expected trim after cooldown to succeed")
	}

	stats = GetStats()
	if stats.TotalTrims != 2 {
		t.Fatalf("TotalTrims after cooldown = %d, want 2", stats.TotalTrims)
	}
}

func TestTrimMemory_Force(t *testing.T) {
	var gcCount atomic.Int32

	oldGC := runtimeGCSeam
	runtimeGCSeam = func() { gcCount.Add(1) }
	defer func() { runtimeGCSeam = oldGC }()

	ResetStats()
	SetCooldown(1 * time.Hour)

	if !TrimWithOptions(Options{Sync: true}) {
		t.Fatal("initial trim should succeed")
	}

	// Second attempt without force should fail due to 1-hour cooldown
	if TrimWithOptions(Options{Sync: true}) {
		t.Fatal("second immediate trim should be throttled")
	}

	// Second attempt with Force should succeed
	if !TrimWithOptions(Options{Sync: true, Force: true}) {
		t.Fatal("forced trim should succeed regardless of cooldown")
	}

	if gcCount.Load() != 2 {
		t.Fatalf("gcCount = %d, want 2", gcCount.Load())
	}
}

func TestTrimMemory_SyncExecution(t *testing.T) {
	executed := false
	oldOS := freeOSMemorySeam
	freeOSMemorySeam = func() { executed = true }
	defer func() { freeOSMemorySeam = oldOS }()

	ResetStats()
	SetCooldown(10 * time.Millisecond)

	ok := TrimWithOptions(Options{Sync: true, Force: true})
	if !ok {
		t.Fatal("expected trim to execute")
	}
	if !executed {
		t.Fatal("expected synchronous execution to have completed before return")
	}
}

func TestRelease_And_After(t *testing.T) {
	var mu sync.Mutex
	called := 0

	oldOS := freeOSMemorySeam
	freeOSMemorySeam = func() {
		mu.Lock()
		defer mu.Unlock()
		called++
	}
	defer func() { freeOSMemorySeam = oldOS }()

	cancel := After(20 * time.Millisecond)
	defer cancel()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := called
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	n := called
	mu.Unlock()
	if n == 0 {
		t.Fatal("After timer did not execute")
	}
}

func TestAfter_Cancel(t *testing.T) {
	var mu sync.Mutex
	called := 0

	oldOS := freeOSMemorySeam
	freeOSMemorySeam = func() {
		mu.Lock()
		defer mu.Unlock()
		called++
	}
	defer func() { freeOSMemorySeam = oldOS }()

	cancel := After(50 * time.Millisecond)
	cancel()

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	n := called
	mu.Unlock()
	if n != 0 {
		t.Fatalf("Release executed despite cancel: %d", n)
	}
}
