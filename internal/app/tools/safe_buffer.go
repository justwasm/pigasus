// safe_buffer.go — the mutex-guarded buffer every capture in the tool
// surface writes through.
//
// Cycle 379: this type used to live in internal/app/tools/shell/support.go
// and was reached from three packages at once — the shell capability wrote
// into it, the command capability read out of it, and the tool contract
// handed it to a caller. A type that three packages need is not the
// shell's type; it is the contract's type, so it moved down to the
// package that owns the contract.
//
// SafeBuffer wraps a bytes.Buffer with a mutex so concurrent writes
// from os/exec's stdout/stderr goroutines and reads from status() do
// not race. The pre-cycle-tasks-panel bytes.Buffer-as-Reader pattern
// was racy under -race (cmd.Wait → cmd.writerDescriptor writes the
// buffer while the panel-render goroutine calls t.out.String()).

package tools

import (
	"bytes"
	"sync"
)

type SafeBuffer struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	appendOnce sync.Once
}

// Write appends p to the buffer under the lock.
func (b *SafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// WriteString writes s to the buffer as if it were a single Write call.
// Cycle 258 added this method so recordFinalOutput can append the exit
// reason without allocating a []byte for the literal — same trick as
// bytes.Buffer.WriteString.
func (b *SafeBuffer) WriteString(s string) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.WriteString(s)
}

// WriteByte appends a single byte to the buffer. Cycle 258 added this
// method for the "\n" terminator recordFinalOutput writes after the
// pending output and the exit reason.
func (b *SafeBuffer) WriteByte(c byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.WriteByte(c)
}

// String returns the buffer's contents under the lock.
func (b *SafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// AppendOnce runs fn at most once for the lifetime of the buffer.
//
// It is the door onto the buffer's appendOnce latch, which the shell
// capability needs: the task log is written by two racing paths (the
// cmd.Wait goroutine and the kill path) and only the first one may
// record the final output.
func (b *SafeBuffer) AppendOnce(fn func()) {
	if b == nil || fn == nil {
		return
	}
	b.appendOnce.Do(fn)
}
