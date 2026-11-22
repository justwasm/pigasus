// Package memtrim provides smooth, active memory trimming and OS release
// for long tasks and large outputs across all architecture tiers.
//
// Background:
// The Go runtime retains allocated heap pages for future allocations rather
// than immediately returning them to the operating system. During large file
// reads, long-running agent workflows, or large tool outputs, temporary peak
// allocations create residual RSS memory inflation.
//
// memtrim provides rate-limited, smooth micro-releases (runtime.GC followed
// by debug.FreeOSMemory) to return physical memory to the OS without introducing
// stop-the-world GC thrashing or latency spikes.
package memtrim

import (
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultCooldown is the default minimum duration between consecutive memory
// trims to avoid GC thrashing under rapid task completion.
const DefaultCooldown = 1 * time.Second

// Delay is the standard delay before start-up memory is released.
const Delay = 15 * time.Second

var (
	// freeOSMemorySeam is replaced during unit tests.
	freeOSMemorySeam = debug.FreeOSMemory
	// runtimeGCSeam is replaced during unit tests.
	runtimeGCSeam = runtime.GC

	mu             sync.Mutex
	lastTrimTime   time.Time
	cooldownPeriod = DefaultCooldown

	totalTrims   atomic.Uint64
	skippedTrims atomic.Uint64
)

// Options customizes the behavior of TrimWithOptions.
type Options struct {
	// Sync runs the GC and OS memory release synchronously on the caller's goroutine.
	// When false (default), the release runs smoothly in a background goroutine.
	Sync bool

	// Force ignores the cooldown check and triggers a trim immediately.
	Force bool

	// Cooldown overrides the default cooldown interval for this invocation.
	Cooldown time.Duration
}

// Stats captures telemetry on memory trimming activity.
type Stats struct {
	TotalTrims   uint64
	SkippedTrims uint64
	LastTrimTime time.Time
}

// SetCooldown globally configures the rate-limiting interval between trims.
func SetCooldown(d time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	cooldownPeriod = d
}

// ResetStats resets the telemetry counters and timestamp.
func ResetStats() {
	mu.Lock()
	defer mu.Unlock()
	lastTrimTime = time.Time{}
	totalTrims.Store(0)
	skippedTrims.Store(0)
}

// GetStats returns current memory trim telemetry.
func GetStats() Stats {
	mu.Lock()
	defer mu.Unlock()
	return Stats{
		TotalTrims:   totalTrims.Load(),
		SkippedTrims: skippedTrims.Load(),
		LastTrimTime: lastTrimTime,
	}
}

// TrimMemory performs a smooth, non-blocking active micro-release suitable
// for calling immediately after long tasks or large outputs complete.
//
// If called within the cooldown period of a previous trim, it safely skips
// execution to prevent GC pause thrashing.
func TrimMemory() {
	TrimWithOptions(Options{})
}

// TrimSync performs an immediate synchronous memory trim, respecting cooldown.
// Returns true if the trim executed, false if skipped by cooldown.
func TrimSync() bool {
	return TrimWithOptions(Options{Sync: true})
}

// TrimWithOptions initiates a memory trim with custom options.
// Returns true if trim executed or was scheduled, false if skipped.
func TrimWithOptions(opts Options) bool {
	mu.Lock()
	now := time.Now()
	cd := cooldownPeriod
	if opts.Cooldown > 0 {
		cd = opts.Cooldown
	}

	if !opts.Force && !lastTrimTime.IsZero() && now.Sub(lastTrimTime) < cd {
		skippedTrims.Add(1)
		mu.Unlock()
		return false
	}

	lastTrimTime = now
	mu.Unlock()

	totalTrims.Add(1)

	execute := func() {
		runtimeGCSeam()
		freeOSMemorySeam()
	}

	if opts.Sync {
		execute()
	} else {
		go execute()
	}

	return true
}

// Release hands the memory the process is not using back to the OS immediately.
func Release() {
	mu.Lock()
	lastTrimTime = time.Now()
	mu.Unlock()
	totalTrims.Add(1)

	runtimeGCSeam()
	freeOSMemorySeam()
}

// After schedules Release to run delay from now.
// The returned function cancels the scheduled execution.
func After(delay time.Duration) (cancel func()) {
	if delay <= 0 {
		Release()
		return func() {}
	}
	timer := time.AfterFunc(delay, Release)
	return func() {
		timer.Stop()
	}
}
