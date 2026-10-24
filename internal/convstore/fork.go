// Package convstore fork.go — session forking and persistence helpers.
package convstore

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
)

// NewUUIDv4 makes a v4 UUID (8-4-4-4-12 hex digits).
func NewUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hx := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hx[0:8], hx[8:12], hx[12:16], hx[16:20], hx[20:32]), nil
}

// SessionToSteps converts an in-memory session into the convstore Step shape.
// Empty assistant messages are elided, and the last assistant message has
// trailing blank lines trimmed.
func SessionToSteps(sess []backend.Message) []Step {
	var out []Step
	lastAssistant := -1
	for i, m := range sess {
		switch m.Role {
		case "user":
			out = append(out, Step{
				Idx: int64(i), Type: StepUserInput, Status: 3,
				Text: m.Content,
			})
		case "assistant":
			if strings.TrimSpace(m.Content) == "" {
				continue
			}
			out = append(out, Step{
				Idx: int64(i), Type: StepModelResponse, Status: 3,
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

// ForkConversation duplicates the live session into a fresh convstore database.
// Returns the new UUID (empty on failure) and feedback message for the user.
func ForkConversation(sess []backend.Message, currentID string) (string, string) {
	return ForkConversationSteps(SessionToSteps(sess), currentID)
}

// ForkConversationSteps duplicates a conversation's trajectory into a
// fresh convstore database.
//
// The reference's fork copies the trajectory, tool calls included: the
// fork is the same conversation under a new id, so the state the calls
// produced has to come along (see internal/rehydration).
func ForkConversationSteps(steps []Step, currentID string) (string, string) {
	if len(steps) == 0 {
		return "", "Error: no active conversation to fork"
	}
	id, err := NewUUIDv4()
	if err != nil {
		return "", "  Failed to fork: " + err.Error()
	}
	dir := filepath.Join(StoreDir(), "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "  Failed to fork: " + err.Error()
	}
	path := filepath.Join(dir, id+".db")
	if err := WriteConversation(path, id, steps); err != nil {
		return "", "  Failed to fork: " + err.Error()
	}
	if currentID != "" {
		return id, fmt.Sprintf("Forked conversation.\nTo go back, use: /resume %s", currentID)
	}
	return id, "Forked conversation."
}
