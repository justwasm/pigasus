package compaction

import (
	"time"
)

// GenericDoneMsg reports the result of a generic compaction task.
type GenericDoneMsg struct {
	Summary            string
	StatsBefore        Stats
	StatsAfter         Stats
	InputTokens        int
	OutputTokens       int
	Overhead           int
	Cancelled          bool
	Err                error
	CustomInstructions string
	Took               time.Duration
}

// ComputeCompactTokens calculates before, after, and summary token counts.
//
// carriedTokens is the size of what the summary *carries into the next
// prompt* beside the summary itself — the resume block's header and its
// "# User Requests" list.
//
// 【一手】 agy 1.4.10: "Improved the after-compaction size shown in the
// compaction marker and in `/context` to also count what the summary
// carries into the next prompt, such as your earlier requests and
// artifact, subagent, and running-task snapshots, so it is closer to
// the real size; the `(N summary)` label still shows the summary
// alone."
//
// So the *after* figure counts the base context, the summary **and**
// the carried content; the summary figure — the one the "(N summary)"
// label shows — is the summary alone, unchanged.
//
// toolTokens is the size of the tool results the transcript count cannot
// see. The model's request carries them — they travel as the trajectory's
// tool messages, not as conversation text — so leaving them out made the
// "before" figure disagree with what /context reports for the same
// conversation. They count on the *before* side only: the summary replaces
// them, so the after figure has none.
func ComputeCompactTokens(beforeTokens, toolTokens, inputTokens, afterTokens, outputTokens, overhead int, summary string, carriedTokens int) (before, after, sumTokens int) {
	b := beforeTokens + toolTokens
	if overhead > 0 {
		b = overhead + b
	}
	if inputTokens > 0 {
		b = inputTokens
	}
	st := EstimateTokens(summary)
	if outputTokens > 0 {
		st = outputTokens
	}
	a := afterTokens
	if overhead > 0 {
		a = overhead + st + carriedTokens
	}
	return b, a, st
}
