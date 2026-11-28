package slashactions_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/slashactions"
)

// TestGoalPromptCarriesTheTokensTheLoopScansFor pins the one property of the
// /goal body that has a live consequence: the prompt must instruct the model
// to emit the exact marker tokens that convstore scans for.
//
// The goal loop reads a reply and looks for convstore.GoalCompleteToken /
// GoalCancelledToken. If the prompt told the model to send anything else — a
// different spelling, a paraphrase, or nothing at all — the loop would keep
// waiting for a signal the model was never asked to produce, and /goal would
// never terminate. The text itself is pinned byte-for-byte by wirealign; this
// test guards the coupling between that text and the code that consumes it,
// which a byte pin cannot see.
func TestGoalPromptCarriesTheTokensTheLoopScansFor(t *testing.T) {
	body := slashactions.GoalPrompt()

	for _, tok := range []string{convstore.GoalCompleteToken, convstore.GoalCancelledToken} {
		if !strings.Contains(body, tok) {
			t.Errorf("/goal body does not contain %q, which convstore scans for; a reply "+
				"following the prompt would never move the goal loop out of Active", tok)
		}
	}
}
