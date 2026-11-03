// Package voice owns the timing primitive for /voice dictation sessions.
//
// Cycle voice-dictation-3m30s-cap (AGY 1.2.8 fix) closes the
// "voice dictation sessions longer than 4 minutes failing with a
// deadline error and erasing the drafted transcription" gap. The
// reference binary used a 4-minute cap that sat right at the upstream
// transcription gRPC deadline, so sessions that crossed it were killed
// by the deadline and any partial transcription was discarded with the
// session. This package replaces that cap with a shorter one and
// guarantees the drafted transcription is preserved when the recorder
// auto-stops.
//
// The package is stdlib-only — no gRPC, no audio backend. The Recorder
// is the *timing primitive* a future cycle wires into the /voice slash
// command and the TUI's f5 binding. Today the recorder is exercised by
// the unit tests; the slash command still surfaces the cycle-167
// permission-denied stub verbatim.
//
// Constants:
//   - MaxRecordingDuration — the upper bound. 3 minutes 30 seconds
//     (210 s). Shorter than the upstream transcription deadline so a
//     recorder auto-stops before the gRPC context expires.
//
// Recorder state machine:
//
//	StateIdle ── Start ──▶ StateRecording ── Stop ──▶ StateFinalized
//	                          │
//	                          └── elapsed ≥ MaxRecordingDuration ──▶ StateAutoStopped
//
// Auto-stopped sessions are final: their drafted transcription is
// preserved and Snapshot() returns it intact. Manual Stop() rejects
// sessions that are not currently recording (returns ErrNotRecording)
// so a caller that hits the f5 binding twice does not silently
// truncate the draft.
package voice

import (
	"errors"
	"sync"
	"time"
)

// MaxRecordingDuration is the upper bound for a single voice dictation
// recording. Sessions that reach this elapsed time are auto-stopped
// without losing the drafted transcription.
//
// 3 minutes 30 seconds (210 seconds) is deliberately shorter than the
// upstream transcription gRPC deadline the reference binary uses. The
// pre-cycle cap sat at 4 minutes, which pushed sessions past the
// deadline and produced the deadline error the changelog describes.
const MaxRecordingDuration = 3*time.Minute + 30*time.Second

// State values a Recorder can carry during its lifetime.
//
//   - StateIdle        — recorder is fresh or has just been reset; no
//     recording in progress.
//   - StateRecording   — Start() succeeded and the recorder is
//     accepting draft appends and time elapses.
//   - StateFinalized   — Stop() ended the recording normally.
//   - StateAutoStopped — elapsed time crossed MaxRecordingDuration and
//     the recorder stopped itself. The draft is
//     still in Snapshot() so the caller can render
//     it in the prompt.
const (
	StateIdle        = "idle"
	StateRecording   = "recording"
	StateFinalized   = "finalized"
	StateAutoStopped = "auto-stopped"
)

// Stop-reason values a Recorder records on the Snapshot. Both manual
// and auto-stop paths set StopReason so a future caller can branch on
// it (e.g. emit a different transcript hint for auto-stopped sessions
// than for user-stopped ones).
const (
	ReasonUserStopped        = "user-stopped"
	ReasonMaxDurationReached = "max-recording-duration-reached"
)

// Errors the Recorder methods can return. Sentinel errors so callers
// can errors.Is() against them without retyping the message.
var (
	// ErrNotIdle is returned by Start when the recorder is not in
	// StateIdle. A caller that wants to reuse a finalized recorder
	// must Reset it first; otherwise the Start call protects the
	// caller from accidentally overwriting a draft.
	ErrNotIdle = errors.New("voice: recorder is not idle")

	// ErrNotRecording is returned by AppendDraft and Stop when the
	// recorder is not currently recording. The double-bind on the
	// f5 key produces this error path; the TUI is expected to
	// ignore it.
	ErrNotRecording = errors.New("voice: recorder is not recording")

	// ErrEmptyID is returned by NewRecorder when the caller passes
	// an empty session id. The id is what /copy voice and the
	// future transcript card key off; an empty id would silently
	// shadow an earlier session's draft.
	ErrEmptyID = errors.New("voice: empty session id")
)

// Recorder is one voice dictation recording session.
//
// Fields are mutated from the caller's goroutine (AppendDraft, Tick,
// Stop) and read from the TUI render goroutine after the recorder
// settles. mu guards the read/write window.
//
// NewRecorder must be used to construct one — the zero value is
// intentionally not useful.
type Recorder struct {
	mu        sync.Mutex
	id        string
	state     string
	draft     string
	elapsed   time.Duration
	reason    string
	maxDur    time.Duration
	nowFunc   func() time.Duration
	startAt   time.Duration
	stoppedAt time.Duration
}

// NewRecorder returns a fresh Recorder in StateIdle. id is the session
// identifier the caller can use to key transcript cards; an empty id
// is rejected so the same Recorder value cannot silently overwrite an
// earlier session.
//
// maxDur defaults to MaxRecordingDuration when zero. Tests can pass a
// shorter duration to exercise the auto-stop path without waiting for
// the real wall clock.
//
// nowFunc is the clock the Recorder reads to stamp Start and Stop
// times. It defaults to a wall-clock reader when nil. Tests inject a
// deterministic function so they can verify Start() / Stop()
// timestamps without sleeping.
func NewRecorder(id string, maxDur time.Duration, nowFunc func() time.Duration) (*Recorder, error) {
	if id == "" {
		return nil, ErrEmptyID
	}
	if maxDur <= 0 {
		maxDur = MaxRecordingDuration
	}
	if nowFunc == nil {
		nowFunc = defaultNow
	}
	return &Recorder{
		id:      id,
		state:   StateIdle,
		maxDur:  maxDur,
		nowFunc: nowFunc,
	}, nil
}

// defaultNow returns time.Since in nanoseconds as a time.Duration, the
// same shape the injected nowFunc uses in the tests. Pulling it
// through a single function lets a caller pass any monotonic clock
// without Recorder caring whether it is wall-clock or synthetic.
func defaultNow() time.Duration {
	return time.Duration(time.Now().UnixNano())
}

// Start begins a new recording session. The recorder must be in
// StateIdle; calling Start on a non-idle recorder returns ErrNotIdle
// without mutating any field. The start timestamp is recorded via the
// injected clock so test runs can verify ordering.
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != StateIdle {
		return ErrNotIdle
	}
	r.state = StateRecording
	r.draft = ""
	r.elapsed = 0
	r.reason = ""
	r.startAt = r.nowFunc()
	r.stoppedAt = 0
	return nil
}

// AppendDraft adds a transcription delta to the session. The recorder
// must be in StateRecording; any other state returns ErrNotRecording
// without mutating the draft.
//
// AppendDraft concatenates — multiple deltas combine into the same
// draft string. The recorder does not interpret the text, so leading
// or trailing whitespace is preserved verbatim.
func (r *Recorder) AppendDraft(text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != StateRecording {
		return ErrNotRecording
	}
	r.draft += text
	return nil
}

// Tick advances the recorder's internal elapsed counter by d. If the
// new elapsed crosses maxDur the recorder transitions to
// StateAutoStopped, the draft is preserved verbatim, and Reason is
// set to ReasonMaxDurationReached.
//
// Tick is the path tests use to drive the auto-stop transition
// without sleeping for 3.5 minutes. The real recorder loops in a
// goroutine that calls Tick(loopDelta) on a timer; the behaviour is
// identical because Tick is the only entry point that advances the
// counter.
//
// Tick on a recorder that is not in StateRecording is a no-op — the
// method is the auto-stop path's only safe call shape and silently
// ignoring off-state ticks keeps the timer goroutine idempotent under
// shutdown.
func (r *Recorder) Tick(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != StateRecording {
		return
	}
	r.elapsed += d
	if r.elapsed >= r.maxDur {
		r.reason = ReasonMaxDurationReached
		r.state = StateAutoStopped
		r.stoppedAt = r.nowFunc()
	}
}

// Stop ends the recording manually. The recorder must be in
// StateRecording; any other state returns ErrNotRecording without
// mutating any field. On success the recorder transitions to
// StateFinalized and the draft is preserved.
func (r *Recorder) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != StateRecording {
		return ErrNotRecording
	}
	r.reason = ReasonUserStopped
	r.state = StateFinalized
	r.stoppedAt = r.nowFunc()
	return nil
}

// Reset returns the recorder to StateIdle. Any draft is discarded.
// Reset is the only path that drops a draft — manual Stop() and the
// auto-stop path both preserve the draft so the caller can still
// render it.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = StateIdle
	r.draft = ""
	r.elapsed = 0
	r.reason = ""
	r.startAt = 0
	r.stoppedAt = 0
}

// Snapshot returns a copy of the recorder's mutable fields under mu.
// TUI render code should use this rather than touching the fields
// directly so a concurrent Tick() can keep advancing the elapsed
// counter without racing the read.
//
// The returned values are independent copies — mutating them does
// not change the recorder.
func (r *Recorder) Snapshot() (state, draft, reason string, elapsed time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, r.draft, r.reason, r.elapsed
}

// StartedAt returns the clock reading the recorder stamped at Start.
// Zero before Start and after Reset.
func (r *Recorder) StartedAt() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startAt
}

// StoppedAt returns the clock reading the recorder stamped at the
// most recent Stop / auto-stop transition. Zero before the first
// transition and after Reset.
func (r *Recorder) StoppedAt() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stoppedAt
}

// ID returns the session id passed to NewRecorder. Stable for the
// lifetime of the Recorder value.
func (r *Recorder) ID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.id
}
