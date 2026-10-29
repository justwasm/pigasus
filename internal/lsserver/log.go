// Package lsserver — log.go: the reference's log line format.
//
// The reference logs through Google's glog before `google.Init` runs,
// and its bootstrap prints a fixed prefix in front of every line:
//
//	ERROR: logging before google.Init: I1027 04:10:50.508996       1 server.go:1710] Starting language server process with pid 34930
//
// The line itself is glog's header — severity letter, the date, the
// time with microseconds, the thread id in a seven-wide field, and
// the source position — followed by the message.
//
// The clone reproduces the format and the messages. The source
// positions are the reference's own (`server.go:1710` and friends),
// not the clone's: the whole point of the replica is that a log
// diff against the reference is empty.
//
// Evidence grade: 一手 — captured from the reference binary on this
// machine (out/langserver/startup.log).
package lsserver

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// bootstrapPrefix is the reference's pre-Init logging prefix. It is
// on every line the reference writes before `google.Init` — which is
// every line the process writes at startup, because the reference
// never logs again after that.
const bootstrapPrefix = "ERROR: logging before google.Init: "

// The reference's source positions. They are part of the log line the
// operator reads and the evidence diff compares, so they are carried
// verbatim rather than replaced with the clone's own.
const (
	locStartPID      = "server.go:1798"
	locGoMaxProcs    = "server.go:574"
	locVersion       = "server.go:1854"
	locListenHost    = "server.go:742"
	locListenHTTPS   = "server.go:757"
	locListenHTTP    = "server.go:765"
	locDiscoveryFile = "server.go:3115"
	locInitialized   = "server.go:2935"
	locMigration     = "migrate.go:50"
	locMigrationDone = "migrate.go:158"
)

// Logger writes the reference's log lines.
//
// The thread id in glog's header is the thread that wrote the line.
// The reference's startup lines all come from its main thread and
// carry the id 1; the background lines carry the id of the thread
// that wrote them. The clone's startup lines are written from the
// main goroutine and carry 1, and the background lines take the next
// id from the counter.
type Logger struct {
	mu sync.Mutex
	w  io.Writer

	thread atomic.Int64
}

// NewLogger returns a Logger writing to w.
func NewLogger(w io.Writer) *Logger {
	l := &Logger{w: w}
	l.thread.Store(1)
	return l
}

// NextThread reserves the next thread id and returns it. A background
// goroutine calls it once and uses the returned id for every line it
// writes.
func (l *Logger) NextThread() int {
	return int(l.thread.Add(1))
}

// Infof writes one INFO line in the reference's format.
func (l *Logger) Infof(thread int, loc, format string, args ...any) {
	l.write('I', thread, loc, fmt.Sprintf(format, args...))
}

// Warningf writes one WARN line in the reference's format.
func (l *Logger) Warningf(thread int, loc, format string, args ...any) {
	l.write('W', thread, loc, fmt.Sprintf(format, args...))
}

// Errorf writes one ERROR line in the reference's format.
func (l *Logger) Errorf(thread int, loc, format string, args ...any) {
	l.write('E', thread, loc, fmt.Sprintf(format, args...))
}

// write emits one line: the bootstrap prefix, glog's header and the
// message.
func (l *Logger) write(severity byte, thread int, loc, msg string) {
	now := time.Now()
	_, month, day := now.Date()
	hour, minute, second := now.Clock()
	line := fmt.Sprintf("%s%c%02d%02d %02d:%02d:%02d.%06d %7d %s] %s\n",
		bootstrapPrefix,
		severity, int(month), day,
		hour, minute, second, now.Nanosecond()/1000,
		thread, loc, msg)

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.w, line)
}
