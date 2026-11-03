// thought.go — the turn's thought row.
//
// The reference shows one thought row per turn, not one per delta: the
// thought the model streamed is the first row of the turn's block.

package toolsteps

import (
	"strings"
	"time"
)

// AppendThoughtStep puts the turn's thought in front of its steps.
//
// The thought is empty when the model never thought, and the turn's
// steps are then left alone.
func AppendThoughtStep(steps []Step, thought string, dur time.Duration, tokens int) []Step {
	if thought == "" {
		return steps
	}
	return append([]Step{{
		Kind:   KindThought,
		Name:   "thinking",
		Detail: strings.TrimSpace(thought),
		Dur:    dur,
		Tokens: tokens,
	}}, steps...)
}
