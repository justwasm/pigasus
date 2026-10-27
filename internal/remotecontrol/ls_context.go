// Package remotecontrol — ls_context.go: the conversation's earlier
// turns, replayed into the model's context.
//
// # Why the Web UI needs this
//
// The Antigravity Web UI's composer sends one message at a time. The
// message before it is not in the prompt the server receives, so a
// server that runs every turn on a fresh session answers each message
// as if it were the first one: every message is its own context.
//
// The reference's own language server does not have that problem. Its
// CascadeConfig carries `useDbTrajectory` — "load the trajectory from
// the db" — and the trajectory is the conversation's own record, so
// the model sees the earlier turns before the new prompt.
//
// 【一手】 2026-10-26: the reference's own language server answers a
// message sent into a conversation it did not start, and the
// conversation file grows by two steps — the message and the answer —
// with the earlier steps still in place. Those earlier steps are the
// context the next message is answered against.
//
// # What the clone replays
//
// The conversation store is the source of truth: the same file the
// Web UI's sidebar reads, and the same file the turn is appended to.
// The caps are the clone's own — convstore.DefaultHistoryTurns and
// convstore.DefaultHistoryChars, the ones the --continue path uses.
//
// The tool calls are not replayed. The clone's stored step shape does
// not carry the reference's tool-result format, and half a transcript
// is worse than the prose.
package remotecontrol

import (
	"github.com/justwasm/pigasus/internal/convstore"
)

// lsConversationHistory returns the conversation's earlier turns, in
// the order the model should see them.
//
// A conversation that does not exist — the first turn of a new one —
// has no history. A store that cannot be read is not an error either:
// the turn runs without the earlier turns rather than not at all.
func lsConversationHistory(id string) []MessageHistoryTurn {
	if id == "" {
		return nil
	}
	path := convstore.FindConversation(id)
	if path == "" {
		return nil
	}
	conv, err := convstore.LoadConversation(path)
	if err != nil || conv == nil {
		return nil
	}
	turns := conv.LastChatTurnsBudget(convstore.DefaultHistoryTurns, convstore.DefaultHistoryChars)
	if len(turns) == 0 {
		return nil
	}
	out := make([]MessageHistoryTurn, 0, len(turns))
	for _, turn := range turns {
		out = append(out, MessageHistoryTurn{Role: turn.Role, Text: turn.Text})
	}
	return out
}
