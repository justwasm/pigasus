// Package autocont — the auto-continue recovery vocabulary.
//
// Two turn engines recover from a broken turn: the TUI's agent.Loop
// (internal/agent) and the print path's coding.Session runtime
// (internal/pigrt). Both answer the same two questions with the same
// text, and both bound their recursion at the same depth.
//
// The text is not the clone's prose. It is the reference's own:
//
//   - StreamInterrupted is the verbatim recovery copy AGY emits at
//     loadfile offset 0x33dc41b (binary ~/.local/bin/agy, build 1.4.0).
//     The model sees this exact text on a stream-interrupt failure; the
//     print path stream-prints it as a marker line, the TUI surfaces it
//     verbatim as a styled status banner.
//   - IterationCapFormat is the verbatim **Status** banner AGY emits at
//     loadfile offset 0x33a3d44 with a single %d placeholder for the
//     iteration count. fmt.Sprintf(IterationCapFormat, n) reproduces
//     AGY's banner byte-for-byte (modulo the %d substitution).
//
// Source: docs/research/agy-feature-strings.md §3, evidence grade
// 【strings】.
//
// Cycle 346: both engines used to declare their own copy of these
// constants — two literal copies of a string that has to be byte-exact
// against the binary. The comment above the pair claimed "a TUI/print
// divergence is impossible by construction", but two declarations are
// not a construction; they are two chances to drift. There is one
// declaration now, and both engines alias it.
package autocont

const (
	// StreamInterrupted is the verbatim AGY recovery copy.
	StreamInterrupted = "The stream was interrupted. Please continue the task you were working on."

	// IterationCapFormat is the verbatim AGY **Status** banner for an
	// exhausted tool-iteration budget.
	IterationCapFormat = "**Status**: Cancelled after %d iterations"

	// MaxDepth bounds the recursion for generic failures (quota, scope,
	// auth, marshal errors, anything that cannot be fixed by retrying).
	//
	// Cycles auto-continue-interrupt (and its predecessors) picked 3 from
	// the AGY 1.2.13 wire — short enough to fail fast on permanent
	// failures, long enough to ride out a single transient hiccup. That
	// is the right ceiling for permanent errors and the wrong ceiling
	// for transient network/socket drops, which the changelog
	// (internal/cli/testdata/changelog_reference.txt:10) calls out as
	// "retrying automatically and reporting a network error if retries
	// fail". See MaxNetworkDepth.
	MaxDepth = 3

	// MaxNetworkDepth caps the recursion for transient network/socket
	// errors. It is much higher than MaxDepth because (a) socket
	// disconnects in long tool-call chains are common — every laptop
	// sleep, every wifi handoff, every short container restart can drop
	// a single frame, and (b) the recovery copy the loop appends is the
	// AGY verbatim string, so the user sees a consistent story
	// regardless of which branch serves them. Once this ceiling is
	// exhausted the loop returns its own "network failed" error so the
	// TUI / print path can render a clear banner with the last
	// underlying cause.
	//
	// Value picked at 8: enough to ride out several mid-task wifi
	// reconnects (typical retry-after-wifi-handoff sequence is 3-4).
	MaxNetworkDepth = 8
)

// IterationCount fishes the iteration count out of the error a turn
// failed with, so the **Status** banner can name it.
//
// The upstream shapes are the wraps PiG emits ("%w (%d turns)"), the
// clone's own wrap ("agent: exceeded tool-iteration budget (%d)"), and
// a plain sentence ("exceeded turn limit: 21 turns completed"). All
// three carry the count as a run of digits, so the count is the
// **largest** digit run in the message — not the concatenation of every
// digit it contains, which would read "budget (8) after 3 tries" as 83.
//
// A message with no digit yields 0, and the recovery copy formatted
// against 0 still works, which is what the cycle auto-continue-interrupt
// spec calls for.
func IterationCount(err error) int {
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
