// slash_helpers.go — cycle 167 helpers that close the cycle-77 stub
// bodies. Each function does the smallest useful thing for the
// command that invokes it; the references for the recovered messages
// are inline so future cycles know where to tighten them.

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
)

// rewindSession pops the trailing user/assistant pair from sess and
// returns the number of messages removed. The reference's /rewind is
// a tail clip rather than a full rollback (verified by replaying a
// captured transcript against a fresh session).
func rewindSession(sess []backend.Message) int {
	if len(sess) == 0 {
		return 0
	}
	removed := 0
	// Pop at most one assistant + one user from the tail.
	if len(sess) > 0 && sess[len(sess)-1].Role == "assistant" {
		sess = sess[:len(sess)-1]
		removed++
	}
	if len(sess) > 0 && sess[len(sess)-1].Role == "user" {
		sess = sess[:len(sess)-1]
		removed++
	}
	return removed
}

// compactSession drops the older half of sess so the next model call
// sees a shorter history. Returns the number of messages removed.
// The reference keeps the most recent N turns; we keep the recent
// half. Transcript rows are NOT trimmed — only the wire history.
func compactSession(sess []backend.Message) int {
	if len(sess) < 4 {
		return 0
	}
	keep := len(sess) / 2
	drop := len(sess) - keep
	if drop <= 0 {
		return 0
	}
	copy(sess, sess[drop:])
	return drop
}

// forkConversation duplicates the live session into a fresh
// convstore database under a new UUID. Returns the new id ("" on
// failure) and a feedback message. The clone writes the same SQLite
// schema the reference uses so the resulting file passes
// LoadConversation unmodified on the next /resume.
//
// When the active conversation is unnamed (the startup session) we
// still write a fresh db so the user has something to resume later,
// but we keep the original conversationID unchanged so any in-flight
// references stay valid.
//
// The empty-session feedback text was recovered from REF v1.4.0:
//   > /fork
//     ⎿  Error: no active conversation to fork
// (/tmp/agycmp/panels/ppref.fork.raw.txt)
func forkConversation(sess []backend.Message, currentID string) (string, string) {
	if len(sess) == 0 {
		return "", "Error: no active conversation to fork"
	}
	id, err := newUUIDv4()
	if err != nil {
		return "", "  Failed to fork: " + err.Error()
	}
	dir := filepath.Join(convstore.StoreDir(), "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "  Failed to fork: " + err.Error()
	}
	path := filepath.Join(dir, id+".db")
	steps := sessionToSteps(sess)
	if err := convstore.WriteConversation(path, id, steps); err != nil {
		return "", "  Failed to fork: " + err.Error()
	}
	if currentID != "" {
		return id, fmt.Sprintf("Forked conversation.\nTo go back, use: /resume %s", currentID)
	}
	return id, "Forked conversation."
}

// sessionToSteps converts an in-memory session into the convstore
// step shape that WriteConversation emits. Each session message
// becomes one Step with the matching Type constant; tool calls and
// other shapes from a forked session are dropped (we don't have
// the protobuf to embed). Returns an empty slice if sess is empty.
//
// Empty/whitespace-only assistant messages (the model returned no
// content) are dropped so they do not bloat the conversation DB with
// blank rows that --continue would otherwise have to filter out at
// replay time. AGY 1.2.13: a streamed reply whose chunks were all
// empty used to land in conversations/<id>.db as a step with
// Text == " ", which then re-appeared as a blank scrollback row in
// every resumed session.
//
// Cycle trim-trailing-blank-messages: the LAST assistant step's
// Text is run through backend.TrimTrailingBlankLines so the
// trailing "\n\n" the streaming APIs commonly emit does not leak
// into the persisted conversation DB. We only trim the tail
// because internal whitespace in earlier chunks is part of the
// model's paragraph structure — collapsing it would corrupt
// mid-stream output. Mid-stream trimming belongs to the
// sanitizer, not this projection.
func sessionToSteps(sess []backend.Message) []convstore.Step {
	var out []convstore.Step
	lastAssistant := -1
	for i, m := range sess {
		switch m.Role {
		case "user":
			out = append(out, convstore.Step{
				Idx: int64(i), Type: convstore.StepUserInput, Status: 3,
				Text: m.Content,
			})
		case "assistant":
			// AGY 1.2.13: drop empty/whitespace-only assistant replies.
			// Tool calls ride on separate rows (StepToolCall) which are
			// not produced here — they go through the liveSteps path —
			// so dropping a text-less assistant step never loses a tool
			// call, only a blank bubble.
			if strings.TrimSpace(m.Content) == "" {
				continue
			}
			out = append(out, convstore.Step{
				Idx: int64(i), Type: convstore.StepModelResponse, Status: 3,
				Text: m.Content,
			})
			lastAssistant = len(out) - 1
		}
	}
	if lastAssistant >= 0 {
		out[lastAssistant].Text = backend.TrimTrailingBlankLines(out[lastAssistant].Text)
	}
	return out
}

// printSlashKeybindings is the /keybindings panel content. The
// reference's /keybindings tab on the /help overlay lists the same
// shortcuts in a slightly different visual layout; we render the
// same content via a multi-line text block to keep parity without
// re-implementing the panel.
func printSlashKeybindings() string {
	var b strings.Builder
	b.WriteString("Keyboard shortcuts:\n")
	for _, line := range []string{
		"  Enter            Submit prompt",
		"  Shift+Enter      Newline (multiline prompt)",
		"  Ctrl+C × 2        Exit",
		"  Ctrl+L            Toggle alternate screen",
		"  Ctrl+O            Expand / collapse tool calls",
		"  Ctrl+T            Edit prompt in $EDITOR",
		"  Up / Down         History navigation",
		"  PageUp / PageDown Transcript scroll",
		"  /                 Open command picker",
		"  Esc               Close picker / overlay",
		"  Tab               Complete command",
	} {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// newConversationID makes a v4 UUID. The reference uses the same
// shape (8-4-4-4-12 hex digits); we mirror that so the convstore
// loader (case-sensitive) finds our newly forked db.
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hex := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex[0:8], hex[8:12], hex[12:16], hex[16:20], hex[20:32]), nil
}