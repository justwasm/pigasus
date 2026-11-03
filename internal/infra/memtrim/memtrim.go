// Package memtrim returns the memory the process held at start-up to
// the operating system.
//
// 【一手】 agy 1.4.10, the vendor's own release notes: "Improved memory
// use shortly after startup: the CLI now returns about 100 MB of
// memory left over from startup to the operating system 15 seconds
// after the interactive UI opens; print and headless modes are
// unaffected."
//
// The interactive UI is where the memory is: the start-up path builds
// the tool catalogue, the plugin registry, the prompt and the banner
// before the first frame, and most of it is dead once the first turn
// is on screen. The heap keeps that memory, though — the Go runtime
// holds freed pages for the next allocation instead of handing them
// back — so the process looks like it is holding a hundred megabytes it
// is not using.
//
// runtime/debug.FreeOSMemory is the call that gives the pages back: it
// runs a collection and then returns as much of the freed heap to the
// operating system as it can.
//
// The trim is **delayed**: doing it while the first frame is being
// built would cost the start-up its own latency, and the memory is
// wanted exactly once the UI is up.
package memtrim

import (
	"runtime/debug"
	"time"
)

// Delay is how long after the interactive UI opens the trim runs.
//
// 【一手】 the 1.4.10 note: "15 seconds after the interactive UI
// opens".
const Delay = 15 * time.Second

// freeOSMemory is the seam the tests replace: the real call is
// runtime/debug.FreeOSMemory.
var freeOSMemory = debug.FreeOSMemory

// Release hands the memory the process is not using back to the
// operating system.
func Release() {
	freeOSMemory()
}

// After schedules Release to run delay from now.
//
// The returned function cancels the schedule; it is safe to call more
// than once, and it is what the caller defers so a session that ends
// before the delay does not leave a timer behind.
//
// A delay of zero or less runs the release immediately — on the
// calling goroutine, so a caller that asks for it gets it before the
// call returns.
func After(delay time.Duration) (cancel func()) {
	if delay <= 0 {
		Release()
		return func() {}
	}
	timer := time.AfterFunc(delay, Release)
	return func() { timer.Stop() }
}
