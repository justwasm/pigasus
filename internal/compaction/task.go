package compaction

import (
	"context"
	"errors"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
)

// RunGenericTask executes ComputeSummary and emits a GenericDoneMsg on onDone.
func RunGenericTask(
	ctx context.Context,
	backendClient backend.Client,
	model, systemPrompt string,
	overhead int,
	sessSnap []backend.Message,
	customInstructions string,
	started time.Time,
	onDone func(GenericDoneMsg),
) {
	summary, err := ComputeSummary(ctx, backendClient, model, systemPrompt, sessSnap)

	var msg GenericDoneMsg
	msg.CustomInstructions = customInstructions
	msg.Overhead = overhead
	switch {
	case errors.Is(err, ErrCancelled):
		msg.Cancelled = true
		msg.StatsBefore = summary.StatsBefore
	case errors.Is(err, ErrEmptyTranscript):
		return
	case err != nil:
		msg.Err = err
		msg.StatsBefore = summary.StatsBefore
	default:
		msg.Summary = summary.Text
		msg.StatsBefore = summary.StatsBefore
		msg.StatsAfter = summary.StatsAfter
		msg.InputTokens = summary.InputTokens
		msg.OutputTokens = summary.OutputTokens
	}
	msg.Took = time.Since(started)

	if onDone != nil {
		onDone(msg)
	}
}
