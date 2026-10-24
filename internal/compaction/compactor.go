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
func ComputeCompactTokens(beforeTokens, inputTokens, afterTokens, outputTokens, overhead int, summary string) (before, after, sumTokens int) {
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
		a = overhead + st
	}
	return b, a, st
}
