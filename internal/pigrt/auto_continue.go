// auto_continue.go — recover a streaming / iteration-cap failure instead of
// reporting it.
//
// Cycle auto-continue-interrupt closes the "long tool-call chain interruption"
// pain point #2 (the other five are auto-compaction, premature finalisation,
// MaxTurns silent, token estimation drift, and a sub-tree). The model is left
// waiting forever when:
//
//  1. the upstream SSE stream closes without an end marker (provider-side
//     network drop, gateway timeout, proxy idle-evict). PiG folds this into
//     StopReasonError / StopReasonAborted on the last assistant message and
//     returns a nil Go error;
//  2. tool-iteration cap is hit (MaxTurns / pigMaxTurns). PiG returns
//     agent.ErrMaxTurnsReached.
//
// Instead of surfacing either as a failure, the runtime injects one of two
// recovery copies into the agent's transcript and calls Agent.ContinueMessages():
//
//   - stream-interrupt: the verbatim AGY recovery text
//     ("The stream was interrupted. Please continue the task you were working
//     on.", loadfile 0x33dc41b);
//   - iteration-cap: the verbatim AGY **Status** banner
//     ("**Status**: Cancelled after %d iterations", loadfile 0x33a3d44),
//     substituted with the iteration count.
//
// The recovery depth is bounded by AutoContMaxDepth; beyond it the runtime
// returns ErrAutoContMaxDepth so the caller (print path / TUI) can surface a
// "user input required" prompt. Per-Send depth is reset to 0 at the top of
// each Runtime.Run call so a successful user prompt does not carry failed
// recovery depth into the next turn.
//
// What this package deliberately does not change:
//
//   - the turn engine (still PiG's coding.Session, see session.Send
//     at coding/session.go:715);
//   - the transport (the gateway provider, the ErrorSlot out-of-band record;
//     pigrt.opts.ErrorSlot is still the path for the original
//     *backend.HTTPError);
//   - the event drain (runtime.drain still routes text deltas to onText —
//     this means an auto-continue turn's text streams through the same
//     delivery pipeline as a normal turn, so print-stdout stays byte-stable
//     cycle-on-cycle).
//
// Cycle auto-continue-interrupt writes one file in each of internal/pigrt and
// internal/agent and five + four tests respectively; see the deliverable in
// docs/cycles/auto-continue-interrupt/ for the trigger matrix and the recovery
// copy catalogue.
package pigrt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
)

// AutoContStreamInterrupted is the verbatim recovery copy AGY emits at
// loadfile offset 0x33dc41b (binary ~/.local/bin/agy, build 1.4.0). The model
// sees this exact text on a stream-interrupt failure; the print path
// stream-prints it as a marker line, the TUI surfaces it verbatim as a
// styled status banner.
//
// Source: docs/research/agy-feature-strings.md §3, evidence grade 【strings】.
const AutoContStreamInterrupted = "The stream was interrupted. Please continue the task you were working on."

// AutoContIterationCapFormat is the verbatim **Status** banner AGY emits at
// loadfile offset 0x33a3d44 with a single %d placeholder for the iteration
// count. fmt.Sprintf(AutoContIterationCapFormat, n) reproduces AGY's banner
// byte-for-byte (modulo the %d substitution).
//
// Source: docs/research/agy-feature-strings.md §3, evidence grade 【strings】.
const AutoContIterationCapFormat = "**Status**: Cancelled after %d iterations"

// AutoContMaxDepth bounds auto-continue recursion. Three consecutive
// stream-interrupt or iteration-cap failures in the same user-turn trip the
// "user input required" path; see ErrAutoContMaxDepth.
const AutoContMaxDepth = 3

// ErrAutoContMaxDepth is returned when auto-continue recurses AutoContMaxDepth
// times in one user-turn without settling. Callers surface this as a user
// prompt ("the model keeps failing — please rephrase") — it is the cycle
// auto-continue-interrupt give-up case the TUI banner ⎿ ↻ auto-continuing…
// resets after a human-types-in.
type ErrAutoContMaxDepth struct {
	Depth int
	Last  error
}

func (e *ErrAutoContMaxDepth) Error() string {
	return fmt.Sprintf("pigrt: auto-continue reached max depth (%d); user input required (last error: %v)", e.Depth, e.Last)
}

// Unwrap exposes the original error to errors.Is / errors.As — the cycle
// 183 *backend.HTTPError classification in cmd/wagyu/api_error.go has to see
// the typed error so the AGY_ERROR envelope and exit code 3 still fire when
// the original was a gateway failure.
func (e *ErrAutoContMaxDepth) Unwrap() error { return e.Last }

// classifyInterruption reports the auto-continue recovery that fits the
// given (msgs, runErr) pair, or "" if the failure is not auto-continueable.
//
// The matrix:
//
//	ErrMaxTurnsReached                              → AutoContIterationCapFormat
//	last.Assistant.StopReason ∈ {Error,Aborted,Length} → AutoContStreamInterrupted
//	last.Assistant.StopReason ∈ {Stop,ToolUse}       → ""   (clean completion)
//	user ctx.Cancel (ctx.Err != nil)                  → ""   (caller checks first)
//	other                                             → ""
//
// Note the early test order: a typed iteration-cap error wins over the
// tail assistant's StopReason so a Continue() that resets the budget to
// 0 doesn't get spuriously surfaced with the stream-interrupted copy.
//
// A failure can reach this helper in two distinct shapes:
//
//   - Session.Send returned a non-nil err (typical, the runtime got an
//     upstream-side failure code);
//   - Session.Send returned nil but the last assistant message carries
//     StopReasonError / Aborted / Length (PiG encodes the same kind of
//     failure into a message and reports the run as done-with-error —
//     see agent_loop.go:494). The matrix must catch both.
func classifyInterruption(msgs []agent.AgentMessage, runErr error) string {
	if errors.Is(runErr, agent.ErrMaxTurnsReached) {
		return fmt.Sprintf(AutoContIterationCapFormat, iterationCountFromErr(runErr))
	}
	last := lastAssistant(msgs)
	if last == nil {
		return ""
	}
	switch last.StopReason {
	case ai.StopReasonError, ai.StopReasonAborted, ai.StopReasonLength:
		return AutoContStreamInterrupted
	}
	return ""
}

// lastAssistant returns the tail assistant message of msgs (or nil).
func lastAssistant(msgs []agent.AgentMessage) *agent.AssistantMessage {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Assistant != nil {
			return msgs[i].Assistant
		}
	}
	return nil
}

// iterationCountFromErr extracts the iteration count from the message PiG
// embeds in agent.ErrMaxTurnsReached. The exact text is "agent: exceeded
// tool-iteration budget (%d)" (or "%d" prefix in upstream variants), so we
// fish for the integer after the last parenthesis.
//
// Returning 0 is fine: the recovery copy will read "**Status**: Cancelled
// after 0 iterations", which is still a valid prompt. The depth counter is
// the real bound on recursion, not the literal count.
func iterationCountFromErr(err error) int {
	if err == nil {
		return 0
	}
	// Parse "budget (N)" or "after N iterations" whichever upstream emits.
	// The clone's own loop returns fmt.Errorf("agent: exceeded tool-iteration
	// budget (%d)", maxTurns) when the budget is exceeded; runtime.runPig's
	// path bubbles agent.ErrMaxTurnsReached up, so we unwrap via errors.As in
	// the TUI path's recursion. The recovery copy formatted against 0 still
	// works, which is what the cycle auto-continue-interrupt spec calls for.
	var last int
	for _, r := range err.Error() {
		if r >= '0' && r <= '9' {
			last = last*10 + int(r-'0')
		}
	}
	return last
}

// appendRecoveryMessage returns a copy of msgs with a synthetic user message
// carrying the recovery text appended at the tail. Continue() rejects tail
// assistant messages; the synthetic user tail is what lets the next Continue
// actually run the loop.
func appendRecoveryMessage(msgs []agent.AgentMessage, recovery string) []agent.AgentMessage {
	out := make([]agent.AgentMessage, 0, len(msgs)+1)
	out = append(out, msgs...)
	out = append(out, agent.AgentMessage{User: &agent.UserMessage{
		Role:      "user",
		Content:   ai.UserText(recovery),
		Timestamp: time.Now().UnixMilli(),
	}})
	return out
}

// runAutoContCycle drives one auto-continue attempt on the runtime's session.
// It pushes the synthetic recovery message onto the agent's in-memory
// transcript, calls Agent.ContinueMessages(ctx), and returns the messages the
// continued turn produced plus the (typed) error PiG returned.
//
// The function is recursive: a ContinueMessages() that ends on a SECOND
// stream-interrupt will recurse with depth+1. The depth ceiling lives in the
// caller — Run decides whether to call this at all.
func (r *Runtime) runAutoContCycle(ctx context.Context, msgs []agent.AgentMessage, recovery string) ([]agent.AgentMessage, error) {
	if r.session == nil || r.session.Agent() == nil {
		// Nothing we can do without a live agent. Surface the original
		// failure rather than silently dropping it.
		return msgs, errors.New("pigrt: auto-continue: no live session")
	}
	updated := appendRecoveryMessage(msgSlice(r.session.Agent().MessagesSnapshot()), recovery)
	r.session.Agent().SetMessages(updated)
	continued, err := r.session.Agent().ContinueMessages(ctx)
	if err != nil {
		return continued, err
	}
	// Mirror the RunWith tail: a cancelled context outranks whatever the
	// provider reported on the way out. --print-timeout depends on this.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return continued, ctxErr
	}
	if r.session != nil {
		_ = r.session.FlushEvents(ctx)
	}
	return continued, nil
}

// maybeAutoContinue inspects (msgs, runErr) returned by session.Send. If the
// failure is auto-continueable (StopReasonError/Aborted/Length or
// agent.ErrMaxTurnsReached) and depth < AutoContMaxDepth, it patches the
// transcript with the appropriate recovery copy and recurses via
// runAutoContCycle. A clean stop or a non-auto-continue failure returns the
// arguments unchanged.
//
// Recovery depth is capped per user-turn: each Run() call initialises the
// counter (see Runtime.RunWith). The cap prevents an unbroken recovery loop
// from holding the terminal hostage.
//
// At give-up (depth == AutoContMaxDepth), the Last error preserved through
// the recursion is the original Session.Send return — but PiG sometimes
// flattens the typed *backend.HTTPError to its message and returns it nil,
// so the chain walks a nil runErr path. cmd/wagyu/api_error.go classifies
func slotErr(err error, slot *ErrorSlot) error {
	if err == nil && slot != nil {
		return slot.Err()
	}
	return err
}

// a turn with errors.As(err, &he) to recover the AGY_ERROR envelope; to
// keep that contract we also look in r.opts.ErrorSlot, which is filled by
// RecordProvider every time PiG's Stream returns a typed error.
func (r *Runtime) maybeAutoContinue(ctx context.Context, msgs []agent.AgentMessage, runErr error, depth int) ([]agent.AgentMessage, error) {
	// ctx already cancelled is the user's own abort — auto-continue would
	// burn a quota slot on a session they no longer want. Hand the ctx
	// error back.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return msgs, ctxErr
	}
	// Changelog 1.3.3: "reaching your individual quota is reported right
	// away instead of being retried". A *backend.HTTPError flagged as
	// "individual quota reached" must surface immediately.
	if backend.IsQuotaReachedErr(runErr) {
		return msgs, runErr
	}
	if depth >= AutoContMaxDepth {
		last := slotErr(runErr, r.opts.ErrorSlot)
		return msgs, &ErrAutoContMaxDepth{Depth: depth, Last: last}
	}
	recovery := classifyInterruption(msgs, runErr)
	if recovery == "" {
		return msgs, runErr
	}
	continued, err := r.runAutoContCycle(ctx, msgs, recovery)
	if err != nil {
		return continued, err
	}
	err = slotErr(err, r.opts.ErrorSlot)
	return r.maybeAutoContinue(ctx, continued, err, depth+1)
}

// msgSlice returns a fresh copy of msgs. It exists so the snapshot we
// install on the agent never aliases the caller's slice.
func msgSlice(msgs []agent.AgentMessage) []agent.AgentMessage {
	out := make([]agent.AgentMessage, len(msgs))
	copy(out, msgs)
	return out
}
