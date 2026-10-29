package memtrim

import (
	"sync"
	"testing"
	"time"
)

// TestAfter_ReleasesAfterTheDelay pins the 1.4.10 rule: the memory is
// given back 15 seconds after the UI opens, not at once.
func TestAfter_ReleasesAfterTheDelay(t *testing.T) {
	var mu sync.Mutex
	called := 0
	freeOSMemory = func() {
		mu.Lock()
		defer mu.Unlock()
		called++
	}
	defer func() { freeOSMemory = func() {} }()

	cancel := After(20 * time.Millisecond)
	defer cancel()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := called
		mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the memory was not released")
}

// TestAfter_CancelStopsTheRelease pins the other half: a session that
// ends before the delay does not leave a timer behind that would trim
// a process that has already moved on.
func TestAfter_CancelStopsTheRelease(t *testing.T) {
	var mu sync.Mutex
	called := 0
	freeOSMemory = func() {
		mu.Lock()
		defer mu.Unlock()
		called++
	}
	defer func() { freeOSMemory = func() {} }()

	cancel := After(50 * time.Millisecond)
	cancel()
	cancel() // cancelling twice is not an error

	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	n := called
	mu.Unlock()
	if n != 0 {
		t.Errorf("the memory was released %d time(s) after the cancel", n)
	}
}

// TestAfter_ZeroDelayReleasesAtOnce pins the edge: a delay of zero
// releases before the call returns.
func TestAfter_ZeroDelayReleasesAtOnce(t *testing.T) {
	var mu sync.Mutex
	called := 0
	freeOSMemory = func() {
		mu.Lock()
		defer mu.Unlock()
		called++
	}
	defer func() { freeOSMemory = func() {} }()

	cancel := After(0)
	defer cancel()

	mu.Lock()
	n := called
	mu.Unlock()
	if n != 1 {
		t.Errorf("the release ran %d time(s), want 1", n)
	}
}

// TestDelayIsFifteenSeconds pins the number the upstream note names.
func TestDelayIsFifteenSeconds(t *testing.T) {
	if Delay != 15*time.Second {
		t.Errorf("Delay = %v, want 15s", Delay)
	}
}
