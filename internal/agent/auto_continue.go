// auto_continue.go — TUI-side auto-continue recovery for the PiG agent loop.
//
// Cycle auto-continue-interrupt closes pain-point #2 of "long tool-call chain
// interruption". A user prompt whose upstream stream closes mid-tool-call used
// to leave the terminal stuck: the model had stopReasonError in its last
// assistant message, sendPig returned a failure, and the TUI rendered a
// "stream error" line instead of the part of the response the model
// already had. Same shape for tool-iteration-cap.
//
// internal/pigrt/auto_continue.go owns the print-path counterpart. The
// recovery copy constants (AutoContStreamInterrupted /
// AutoContIterationCapFormat) are declared once, in internal/autocont, and
// both files alias them — so a cycle that wants to verify byte-match
// against the AGY verbatim strings (loadfile offsets 0x33dc41b / 0x33a3d44,
// see docs/research/agy-feature-strings.md §3) reads one constant, and a
// TUI/print divergence is impossible by construction.
//
// Trigger matrix — see the deliverable in
// docs/cycles/auto-continue-interrupt/ for the full version:
//
//	StopReason / error →  Action
//	StopReasonToolUse  →  no auto-continue — surface tool calls
//	StopReasonStop     →  no auto-continue — clean completion
//	StopReasonError    →  AUTO-CONTINUE (stream-interrupted recovery copy)
//	StopReasonAborted  →  AUTO-CONTINUE (stream-interrupted recovery copy)
//	StopReasonLength   →  AUTO-CONTINUE (stream-interrupted recovery copy)
//	ErrMaxTurnsReached →  AUTO-CONTINUE (iteration-cap **Status** banner)
//	user ctx cancel    →  no auto-continue — surface ctx.Err()
//	other error        →  no auto-continue — surface the error
//
// Depth is bounded by AutoContMaxDepth (=3). The TUI emits a transient
// "↻ auto-continuing…" banner before each auto-continue turn so the user
// has visual feedback during the recovery. The banner is a callback
// (Options.AutoContBanner) — keeping the styling decision inside cmd/wagyu
// means the agent package does not pull in lipgloss or colorprofile.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/autocont"
	"github.com/justwasm/pigasus/internal/backend"
)

// The recovery vocabulary is the reference's own bytes, and it is
// declared once, in internal/autocont. These aliases keep the TUI's
// historical names.
//
// Cycle 346: this file used to declare its own copy of each constant —
// two literal copies of a string that has to be byte-exact against the
// binary. The header comment above claimed "a TUI/print divergence is
// impossible by construction", but two declarations are not a
// construction; they are two chances to drift. The print path's copy
// (internal/pigrt) now aliases the same declarations.
const (
	// AutoContStreamInterrupted is the verbatim AGY recovery copy at
	// loadfile 0x33dc41b.
	AutoContStreamInterrupted = autocont.StreamInterrupted

	// AutoContIterationCapFormat is the verbatim AGY **Status** banner at
	// loadfile 0x33a3d44 with a single %d placeholder for the iteration
	// count.
	AutoContIterationCapFormat = autocont.IterationCapFormat

	// AutoContMaxDepth bounds recursion for generic failures (quota,
	// scope, auth, marshal errors, anything we cannot confidently fix by
	// retrying). Past this the loop returns ErrAutoContMaxDepth.
	AutoContMaxDepth = autocont.MaxDepth

	// AutoContMaxNetworkDepth caps auto-continue recursion for transient
	// network/socket errors (see IsTransientNetworkErr). See
	// autocont.MaxNetworkDepth for the reasoning behind the ceiling.
	AutoContMaxNetworkDepth = autocont.MaxNetworkDepth
)

// ErrAutoContMaxDepth is returned when auto-continue recurses
// AutoContMaxDepth times without settling. The TUI surfaces this as a
// user-input-required message; the print path maps it to a non-zero exit
// code (see the deliverable for the full contract).
type ErrAutoContMaxDepth struct {
	Depth int
	Last  error
}

func (e *ErrAutoContMaxDepth) Error() string {
	return fmt.Sprintf("agent: auto-continue reached max depth (%d); user input required (last error: %v)", e.Depth, e.Last)
}

// Unwrap preserves the original error chain so cmd/wagyu/api_error.go's
// errors.As classification still recovers *backend.HTTPError when the loop's
// last attempt was a gateway-level failure.
func (e *ErrAutoContMaxDepth) Unwrap() error { return e.Last }

// ErrAutoContNetworkFailed is returned when auto-continue retried
// AutoContMaxNetworkDepth times on a transient network/socket error
// (per IsTransientNetworkErr) without settling. Where
// ErrAutoContMaxDepth signals "give up, surface a user-input-required
// banner", ErrAutoContNetworkFailed signals "the network is the
// problem, the user wants to know they should check their
// connection". The Unwrap chain still feeds errors.As recoveries of
// *backend.HTTPError so the TUI's classification path is unchanged.
//
// AGY 1.4.x changelog_reference.txt:10: "reporting a network error if
// retries fail". The canonical user-visible copy from cmd/wagyu/tui
// is `Network error: %v (retried %d times)`; the print path maps to
// AGY_ERROR.network_error: { code: "NETWORK_DISCONNECT", last_error:
// "<verbatim>", attempts: N }.
type ErrAutoContNetworkFailed struct {
	Attempts int
	Last     error
}

func (e *ErrAutoContNetworkFailed) Error() string {
	return fmt.Sprintf("agent: network error after %d retries (last error: %v)", e.Attempts, e.Last)
}

// Unwrap preserves the original error chain so the upstream cause
// remains accessible via errors.As / errors.Is.
func (e *ErrAutoContNetworkFailed) Unwrap() error { return e.Last }

// autoContTracker records the per-Send recovery depth. The clone's Loop has
// at most one in-flight Send at a time (Looper.MaxTurns == 1 by default), so
// a plain int field is enough — the mutex is only there for the test
// fixture's concurrent probing.
type autoContTracker struct {
	mu    sync.Mutex
	depth int
}

func (t *autoContTracker) reset() {
	t.mu.Lock()
	t.depth = 0
	t.mu.Unlock()
}

func (t *autoContTracker) next() (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.depth >= AutoContMaxDepth {
		return t.depth, false
	}
	t.depth++
	return t.depth, true
}

// classifyInterruption inspects the (err, lastStopReason) pair and returns
// the recovery copy that should be appended to input.Messages, or "" if the
// failure is not auto-continueable.
//
// The matrix is the one at the package head. The "user ctx cancel" branch
// is detected by the caller before classifyInterruption is called — this
// helper assumes the ctx is still alive.
func classifyInterruption(err error, lastStopReason pi.StopReason) string {
	if errors.Is(err, piagent.ErrMaxTurnsReached) {
		return fmt.Sprintf(AutoContIterationCapFormat, autocont.IterationCount(err))
	}
	switch lastStopReason {
	case pi.StopReasonError, pi.StopReasonAborted, pi.StopReasonLength:
		return AutoContStreamInterrupted
	}
	return ""
}

// recoveryMessageFromMessages returns the last synthetic recovery copy in
// msgs, or "" if none was appended. The cycle auto-continue-interrupt
// tests use it to assert that the recovery message landed in the
// transcript at byte-fidelity to the AGY verbatim strings.
func recoveryMessageFromMessages(msgs []backend.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		text := msgs[i].Content
		if text == AutoContStreamInterrupted {
			return text
		}
		if len(text) >= len("**Status**: Cancelled after ") &&
			text[:len("**Status**: Cancelled after ")] == "**Status**: Cancelled after " {
			return text
		}
	}
	return ""
}

// recoveryForError picks the right verbatim copy for the err returned by
// sendPig. The simpler version of classifyInterruption — the agent wrapper
// does not see lastStopReason because PiG encoded it inside the runner
// and the wrapper replaces the agent from the outside, so the err alone
// drives the choice.
//
// Iteration-cap (ErrMaxTurnsReached → wrapped with "(%d turns)") gets the
// Status banner; everything else gets the stream-interrupted copy. A
// caller-side ctx.Err() is caught earlier and never reaches this helper.
func recoveryForError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, piagent.ErrMaxTurnsReached) {
		return fmt.Sprintf(AutoContIterationCapFormat, autocont.IterationCount(err))
	}
	return AutoContStreamInterrupted
}

// sendPigAuto wraps sendPig with auto-continue recovery. It is the path
// Loop.Send dispatches to when Options.AutoCont is set.
//
// Strategy: on a non-nil sendPig error, classify it (iteration cap vs.
// generic stream failure), append the verbatim AGY recovery copy as a
// synthetic user message to input.Messages, fire the
// Options.AutoContBanner callback so the TUI can flash a transient
// "↻ auto-continuing…" status line, and re-run sendPig. Each retry is a
// fresh PiG agent — sendPig builds a new piagent.Agent per call by
// design (no state worth carrying between Sends) — so the cost is one
// extra upstream request per recovery attempt, with the conversation
// re-played in full. This is acceptable because auto-continue is the
// catastrophic path the rest of the turn protected against, and the
// second upstream request usually succeeds.
//
// Retry budget — picked per error class (cycle 266 network-retry):
//
//	IsTransientNetworkErr(err) → AutoContMaxNetworkDepth (=8) attempts.
//	Other errors               → AutoContMaxDepth (=3) attempts.
//
// Once the budget for that class is exhausted, the loop wraps the
// original error: ErrAutoContMaxDepth for the generic path,
// ErrAutoContNetworkFailed for the network path. Both errors expose
// the underlying cause via errors.As / errors.Is, so the TUI's
// classification in cmd/wagyu/api_error.go continues to recover
// *backend.HTTPError and friends.
//
// Cycle auto-continue-interrupt: doc'd in the package head; see the
// classify / ErrMaxTurnsReached lineage in pig.go and api_error.go.
// Cycle 266 network-retry: closed the AGY 1.4.x line in
// changelog_reference.txt:10 ("socket disconnect errors ... retrying
// automatically and reporting a network error if retries fail").
func (l *Loop) sendPigAuto(ctx context.Context, input TurnInput) error {
	err := l.sendPig(ctx, input)
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if backend.IsQuotaReachedErr(err) {
		return err
	}
	recovery := recoveryForError(err)
	if recovery == "" {
		return err
	}

	maxDepth, typedGiveUp := selectAutoContBudget(err)
	return l.runAutoContLoop(ctx, input, recovery, maxDepth, typedGiveUp, err)
}

func selectAutoContBudget(err error) (int, func() error) {
	if IsTransientNetworkErr(err) {
		return AutoContMaxNetworkDepth, func() error {
			return &ErrAutoContNetworkFailed{Attempts: AutoContMaxNetworkDepth, Last: err}
		}
	}
	return AutoContMaxDepth, func() error {
		return &ErrAutoContMaxDepth{Depth: AutoContMaxDepth, Last: err}
	}
}

func (l *Loop) runAutoContLoop(ctx context.Context, input TurnInput, recovery string, maxDepth int, typedGiveUp func() error, err error) error {
	for attempt := 1; attempt < maxDepth; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if l.opts.AutoContBanner != nil {
			l.opts.AutoContBanner(attempt, maxDepth)
		}
		if attempt == maxDepth-1 {
			return typedGiveUp()
		}
		input.Messages = append(input.Messages, backend.Message{Role: "user", Content: recovery})
		if err = l.sendPig(ctx, input); err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if backend.IsQuotaReachedErr(err) {
			return err
		}
		if newRecovery := recoveryForError(err); newRecovery != "" {
			recovery = newRecovery
		}
	}
	return typedGiveUp()
}
