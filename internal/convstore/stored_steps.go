// Package convstore — stored_steps.go: the raw protobuf payloads of a
// stored conversation.
//
// The reference writes every step of a conversation as a binary
// protobuf blob — a gemini_coder.Step — and keeps the richer fields
// in it: the model's thinking, the usage metadata, the user config.
// The clone's own convstore.Step holds the text and little else, so a
// reader that wants to render a stored conversation exactly as the
// reference rendered it needs the blob itself.
package convstore

import (
	"sort"
)

// StoredStep is one row of a conversation's steps table.
type StoredStep struct {
	// Idx is the step's index in the trajectory.
	Idx int64
	// Type is the numeric step type — 14 for a user input, 15 for a
	// model response, 132 for a tool call.
	Type int64
	// Status is the numeric step status — 3 for DONE.
	Status int64
	// Payload is the raw protobuf blob the reference wrote.
	Payload []byte
}

// StoredStepPayloads reads the rows of a conversation's steps table,
// oldest first.
//
// A conversation that does not exist has no steps; that is not an
// error.
func StoredStepPayloads(id string) ([]StoredStep, error) {
	if id == "" {
		return nil, nil
	}
	path := ConversationPath(id)
	if !HasConversation(id) {
		return nil, nil
	}
	db, err := Open(path)
	if err != nil {
		if isUnreadableStore(err) {
			return nil, nil
		}
		return nil, err
	}
	defer db.Close()
	var out []StoredStep
	if err := db.Scan("steps", func(r *Row) error {
		out = append(out, StoredStep{
			Idx:     r.Int("idx"),
			Type:    r.Int("step_type"),
			Status:  r.Int("status"),
			Payload: r.Bytes("step_payload"),
		})
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Idx < out[j].Idx })
	return out, nil
}
