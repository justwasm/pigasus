// Package rewind owns the /rewind policy.
//
// # What the policy is
//
// A rewind takes a conversation back to an earlier turn. Three
// questions decide it:
//
//   - which messages does a turn own?
//   - how many messages does a rewind to that turn drop?
//   - what does the transcript say afterwards?
//
// All three are facts about the *session*, not about the picker that
// names the turn. The picker names a step; this package decides what
// that name means. So the policy lives here, in the session layer,
// below the view.
//
// # Why it is one package
//
// The policy existed twice. One copy sat in the ui package
// (internal/ui/views/rewindpanel): it popped the trailing
// user/assistant pair from a *local copy* of the session slice and
// never wrote the slice back, so it could not truncate the caller's
// session — the bare /rewind command reported a rewind that never
// happened. The other copy sat inline in the host
// (internal/cliapp/rewind_host.go), where it truncated the session for
// real.
//
// Two copies of one policy drift, and the copy that could not
// truncate was the one the bare command used. Both now name this
// package.
//
// # The two operations
//
//   - ToStep rewinds to a *picked* step. The picker's cursor is the
//     step's index, newest first.
//   - PopTail drops the trailing assistant/user pair. The bare
//     /rewind command names no step, so it drops the conversation's
//     last turn.
//
// They are two operations over one session, so they are two functions
// in one owner rather than one function pretending to be both.
package rewind

import (
	"fmt"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// Step is one user-anchored turn of a conversation.
//
// UserIdx is the session index of the user message that anchors the
// step; EndIdx is the slice index AFTER the last message the step
// owns (one past the trailing assistant / tool-result chain);
// Content is the anchoring user message's text.
//
// The step names the messages it owns and carries the turn's text; how
// a turn is *labelled* in a row is the viewer's business.
type Step struct {
	UserIdx int
	EndIdx  int
	Content string
}

// Steps breaks a session into turn steps starting with a user message.
//
// The list is newest first: index 0 is the conversation's latest turn.
// A session with no user message has no step at all.
func Steps(messages []backend.Message) []Step {
	var steps []Step
	// Walk backwards so we know the most-recent end index first; capture
	// the index AFTER the trailing assistant (or after the user message
	// itself when no assistant has replied yet). Then sweep back: every
	// time we encounter a user-role message we record it as a step whose
	// "end" boundary is the immediately preceding user's start.
	if len(messages) == 0 {
		return nil
	}
	end := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			steps = append(steps, Step{
				UserIdx: i,
				EndIdx:  end,
				Content: messages[i].Content,
			})
			end = i
		}
	}
	// The `end = i` above resets to *before* the user message, so the
	// remaining assistant/tool messages before the first user message are
	// silently dropped from the picker — that matches REF, which doesn't
	// surface orphan leading assistant rows. The result is newest-first.
	return steps
}

// Result is the outcome of a rewind.
type Result struct {
	// Session is the conversation after the rewind. It is the caller's
	// own slice, truncated — the caller assigns it back, so the
	// truncation reaches the session the next turn reads.
	Session []backend.Message

	// Removed is the number of messages the rewind dropped.
	Removed int

	// Step is the 1-based number of the step the conversation was
	// rewound to. It is 0 when the rewind named no step (the bare
	// command's tail pop).
	Step int
}

// ToStep rewinds a session to the picked step.
//
// index is the step's index in the newest-first list Steps returns. An
// index outside the list is clamped to the newest step — the current
// turn — and a rewind to the current turn drops nothing, so the
// session comes back unchanged.
func ToStep(messages []backend.Message, index int) Result {
	steps := Steps(messages)
	if len(steps) == 0 {
		// Empty form — there is no step to rewind to.
		return Result{Session: messages}
	}
	cur := index
	if cur < 0 || cur >= len(steps) {
		cur = 0
	}
	// The picked step's NEW end boundary — keep everything up to and
	// including the step's trailing assistant / tool-result chain (one
	// past the user message). EndIdx is the slice index immediately
	// after the last message the step owns (the trailing assistant
	// message in the simple user/assistant case).
	target := steps[cur].EndIdx
	removed := len(messages) - target
	if removed <= 0 {
		// Already at or before the picked step (the cursor is on the
		// current turn, which is the default). No-op: no truncation.
		return Result{Session: messages, Step: cur + 1}
	}
	return Result{
		Session: messages[:target],
		Removed: removed,
		Step:    cur + 1,
	}
}

// PopTail drops the trailing assistant/user pair from a session.
//
// This is the bare /rewind command's operation: it names no step, so
// it drops the conversation's last turn. The pair is popped by role —
// the trailing assistant first, then the trailing user — so a turn the
// model has not answered yet drops only its own user message.
//
// The result carries the truncated session, so the caller's session
// really shrinks. The copy this operation replaces truncated a local
// slice and never wrote it back.
func PopTail(messages []backend.Message) Result {
	if len(messages) == 0 {
		return Result{Session: messages}
	}
	removed := 0
	end := len(messages)
	if messages[end-1].Role == "assistant" {
		end--
		removed++
	}
	if end > 0 && messages[end-1].Role == "user" {
		end--
		removed++
	}
	return Result{Session: messages[:end], Removed: removed}
}

// StepEcho renders the transcript's line for a rewind to a picked step.
//
// The wording is the reference's own: "Rewound to step N (M message(s)
// removed)". The current-turn form — "Rewound to step N (current, no
// messages removed)" — is the wording for a picker that sat on the
// turn the conversation is already at; it is kept verbatim so the
// text does not drift.
func StepEcho(r Result) string {
	if r.Step == 1 {
		return fmt.Sprintf("Rewound to step %d (current, no messages removed)", r.Step)
	}
	return fmt.Sprintf("Rewound to step %d (%d message(s) removed)", r.Step, r.Removed)
}

// TailEcho renders the transcript's line for the bare command's rewind.
//
// A rewind that dropped nothing has nothing to say, so the line is the
// command's own "Nothing to rewind."
func TailEcho(r Result) string {
	if r.Removed == 0 {
		return "Nothing to rewind."
	}
	return fmt.Sprintf("Rewound %d message(s) from the session tail", r.Removed)
}
