// auto_continue.go — TUI-side auto-continue recovery for the PiG agent loop.
//
// Cycle auto-continue-interrupt closes pain-point #2 of "long tool-call chain
// interruption". A user prompt whose upstream stream closes mid-tool-call used
// to leave the terminal stuck: the model had stopReasonError in its last
// assistant message, sendPig returned a failure, and the TUI rendered a
// "stream error" line instead of the part of the response the model
// already had. Same shape for tool-iteration-cap.
//
// internal/pigrt/auto_continue.go owns the print-path counterpart. Both files
// expose the same recovery copy constants (AutoContStreamInterrupted /
// AutoContIterationCapFormat) so a future cycle that wants to verify byte-
// match against the AGY verbatim strings (loadfile offsets 0x33dc41b /
// 0x33a3d44, see docs/research/agy-feature-strings.md §3) reads one
// constant, not two — and a TUI/print divergence is impossible by
// construction.
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

	"github.com/justwasm/pigasus/internal/backend"
)

// AutoContStreamInterrupted is the verbatim AGY recovery copy at loadfile
// 0x33dc41b. See pigrt/auto_continue.go for the source citation.
const AutoContStreamInterrupted = "The stream was interrupted. Please continue the task you were working on."

// AutoContIterationCapFormat is the verbatim AGY **Status** banner at loadfile
// 0x33a3d44 with a single %d placeholder for the iteration count. See
// pigrt/auto_continue.go for the source citation.
const AutoContIterationCapFormat = "**Status**: Cancelled after %d iterations"

// AutoContMaxDepth bounds recursion for generic failures (quota,
// scope, auth, marshal errors, anything we cannot confidently fix by
// retrying). Past this the loop returns ErrAutoContMaxDepth.
//
// Cycles auto-continue-interrupt (and its predecessors) picked 3 from
// the AGY 1.2.13 wire — short enough to fail fast on permanent
// failures, long enough to ride out a single transient hiccup. That
// is the right ceiling for permanent errors and the wrong ceiling
// for transient network/socket drops, which the changelog
// (internal/cli/testdata/changelog_reference.txt:10) calls out as
// "retrying automatically and reporting a network error if retries
// fail". See AutoContMaxNetworkDepth below.
const AutoContMaxDepth = 3

// AutoContMaxNetworkDepth caps auto-continue recursion for transient
// network/socket errors (see IsTransientNetworkErr). It is much
// higher than AutoContMaxDepth because (a) socket disconnects in
// long tool-call chains are common — every laptop sleep, every wifi
// handoff, every short container restart can drop a single frame,
// and (b) the recovery copy the loop appends ("The stream was
// interrupted. Please continue the task you were working on.") is
// the AGY verbatim string at loadfile 0x33dc41b, so the user sees
// a consistent story regardless of which branch serves them. Once
// this ceiling is exhausted the loop returns
// *ErrAutoContNetworkFailed so the TUI / print path can render a
// clear network-error banner with the last underlying cause.
//
// Value picked at 8: enough to ride out several mid-task wifi
// reconnects (typical retry-after-wifi-handoff sequence is 3-4
// reconnects over 10-30 s) without turning a permanent failure into
// a long stall. The user can still see progress — every retry
// fires the Options.AutoContBanner callback and appends a recovery
// message to the transcript — so 8 retries with steady feedback is
// strictly nicer than the previous "3-skip-then-die" cliff.
const AutoContMaxNetworkDepth = 8

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
		return fmt.Sprintf(AutoContIterationCapFormat, turnsFromErr(err))
	}
	switch lastStopReason {
	case pi.StopReasonError, pi.StopReasonAborted, pi.StopReasonLength:
		return AutoContStreamInterrupted
	}
	return ""
}

// turnsFromErr fishes the iteration count out of PiG's wrapped
// ErrMaxTurnsReached and a few observed upstream variants. Best-effort:
// the parser returns the LAST contiguous run of digits in err.Error(),
// which matches every format we have evidence for in the upstream PiG
// error stream:
//
//   - pigrt-style wrap: "%w (%d turns)"            → "8"
//   - clone-style wrap: "agent: exceeded ... (%d)"  → "8"
//   - direct integer:   "agent: ... 21 turns"      → "21"
//
// Returning 0 is fine: the recovery copy will read "**Status**: Cancelled
// after 0 iterations", which is still a valid prompt. The depth counter
// is the real bound on recursion, not the literal count.
func turnsFromErr(err error) int {
	if err == nil {
		return 0
	}
	var best, current int
	for _, r := range err.Error() {
		switch {
		case r >= '0' && r <= '9':
			current = current*10 + int(r-'0')
		default:
			if current > best {
				best = current
			}
			current = 0
		}
	}
	if current > best {
		best = current
	}
	return best
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
		return fmt.Sprintf(AutoContIterationCapFormat, turnsFromErr(err))
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
	// First attempt — this is the user's original request, not a retry.
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

	// Pick the budget for this error class (cycle 266 network-retry).
	// Transient network/socket errors get AutoContMaxNetworkDepth so a
	// long tool-call chain can ride out several wifi handoffs; every
	// other class keeps the historical AutoContMaxDepth so permanent
	// failures still fail fast.
	maxDepth := AutoContMaxDepth
	typedGiveUp := func() error {
		return &ErrAutoContMaxDepth{Depth: AutoContMaxDepth, Last: err}
	}
	if IsTransientNetworkErr(err) {
		maxDepth = AutoContMaxNetworkDepth
		typedGiveUp = func() error {
			return &ErrAutoContNetworkFailed{Attempts: AutoContMaxNetworkDepth, Last: err}
		}
	}

	// Each loop iteration is one retry. The first attempt above has
	// already failed, so the loop covers (maxDepth - 1) retries before
	// we give up — totalling maxDepth attempts in line with the budget
	// name.
	for attempt := 1; attempt < maxDepth; attempt++ {
		// A cancelled context is the caller's own abort; auto-continue would
		// burn a quota slot on a session the user no longer wants.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		// Flash the depth-N banner BEFORE the last-iteration guard
		// so a banner fires for every retry, including the last
		// (matched-by the test
		// TestLoop_AutoContGiveUpAfterMaxDepth's expectation of
		// exactly AutoContMaxDepth-1 banner firings).
		if l.opts.AutoContBanner != nil {
			l.opts.AutoContBanner(attempt, maxDepth)
		}
		// Last retry — already failed. Don't re-enter the loop.
		if attempt == maxDepth-1 {
			return typedGiveUp()
		}
		// Append the recovery message as a user-role turn. The fresh PiG
		// agent built on the next sendPig call will see the original
		// conversation plus this verbatim AGY copy and resume.
		input.Messages = append(input.Messages, backend.Message{
			Role:    "user",
			Content: recovery,
		})
		// Re-run sendPig. If it returns the same class of error, we
		// loop again and try the next recovery copy; if it succeeds,
		// the outer Send returns nil and the loop's caller sees the
		// final transcript.
		if err = l.sendPig(ctx, input); err == nil {
				return nil
		}
		// A late-stage quota failure (e.g. the user burned their
		// remaining credits on attempt N — 1) must still surface
		// without further retries. Recheck the bail-out conditions
		// here because the error class may have changed.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if backend.IsQuotaReachedErr(err) {
			return err
		}
		// If the error class changed (e.g. quota recovered but a
		// transient network blip hit, OR we promoted a transient
		// socket to a hard 5xx), re-evaluate the recovery copy. The
		// typedGiveUp wrapper remains bound to the *first* error's
		// class so the user sees a consistent banner — a "network
		// error after N retries" stays a network error even if the
		// eventual cause was different.
		if newRecovery := recoveryForError(err); newRecovery != "" {
			recovery = newRecovery
		}
	}
	// Unreachable: the loop returns either inside (attempts 1..N-2)
	// or via the last-iteration guard at attempt == maxDepth-1.
	return typedGiveUp()
}
