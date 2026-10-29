package compaction

import (
	"context"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
)

// ManualCompactor is the seam for the /compact slash command.
type ManualCompactor interface {
	Compact(ctx context.Context, customInstructions string) error
	IsCompacting() bool
	SubscribeManual(listener func(piagent.AgentEvent)) func()
}

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

// ManualDoneMsg reports the result of a manual compaction task.
type ManualDoneMsg struct {
	TokensBefore       int
	TokensAfter        int
	Reason             string
	Aborted            bool
	Err                error
	CustomInstructions string
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
func ComputeCompactTokens(beforeTokens, inputTokens, afterTokens, outputTokens, overhead int, summary string, carriedTokens int) (before, after, sumTokens int) {
	b := beforeTokens
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
