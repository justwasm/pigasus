// history.go seeds a PiG session with the clone's conversation store.
//
// The reference lets `-p -c` continue an earlier conversation: the model sees
// the prior user/assistant turns before the new prompt. On the clone's old
// loop that was a straight replay of backend.Messages. PiG's session owns its
// transcript instead, so the equivalent is to append the same turns to its
// session log before the first send — the session re-projects the log before
// every provider request (coding/session_transcript.go:prepareRequest), so the
// appended turns reach the model exactly like a resumed session's would.
//
// The clone's store is the source of truth, not PiG's own session files, and a
// print run writes nothing to disk (NoSession), so this is a one-way copy.
package pigrt

import (
	"fmt"
	"strings"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// HistoryTurn is one turn to replay ahead of the new prompt, in the clone's
// neutral shape. It mirrors convstore.ChatTurn without importing that package.
type HistoryTurn struct {
	// Role is "user" or "assistant" (case-insensitive). Anything else is
	// treated as a user turn, matching the old loop's replay.
	Role string
	// Text is the turn's plain text.
	Text string
}

// SeedHistory appends turns to the session's log so the next prompt sees them
// as context. It is a no-op for an empty slice.
func (r *Runtime) SeedHistory(turns []HistoryTurn) error {
	if len(turns) == 0 || r.session == nil {
		return nil
	}
	manager := r.session.SessionManager()
	if manager == nil {
		return fmt.Errorf("pigrt: session has no session manager")
	}
	for _, turn := range turns {
		msg, err := historyMessage(turn)
		if err != nil {
			return err
		}
		if _, err := manager.AppendMessage(msg); err != nil {
			return fmt.Errorf("pigrt: seed history: %w", err)
		}
	}
	return nil
}

// historyMessage builds the AgentMessage for one replayed turn.
func historyMessage(turn HistoryTurn) (agent.AgentMessage, error) {
	now := time.Now().UnixMilli()
	if strings.EqualFold(strings.TrimSpace(turn.Role), "assistant") {
		return agent.AgentMessage{Assistant: &agent.AssistantMessage{
			Role: "assistant",
			Content: []ai.AssistantContentBlock{
				ai.TextContent{Text: turn.Text},
			},
			Timestamp:  now,
			StopReason: ai.StopReasonStop,
			Provider:   "agw",
		}}, nil
	}
	return agent.AgentMessage{User: &agent.UserMessage{
		Role:      "user",
		Content:   ai.UserText(turn.Text),
		Timestamp: now,
	}}, nil
}
