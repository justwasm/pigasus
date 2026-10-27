// Package remotecontrol — ls_page.go: the page of a conversation's
// steps the Antigravity Web UI asks for when it opens a conversation.
//
// 【一手】 2026-10-26, from the CLI web bundle (cli-1.4.7-main.js):
// the frontend's conversation view asks for a conversation's steps
// through RequestAgentStatePageUpdate — the request carries the
// conversation id, a subscriber id and the page bounds.
//
// The response carries no fields at all
// (exa.jetski_cortex_pb.AgentStatePageUpdateResponse is empty): the
// steps themselves arrive through the StreamAgentStateUpdates stream,
// which the frontend keeps open for the conversation.
//
// So the clone answers the request by loading the conversation's
// stored steps from the store and seeding the conversation's
// trajectory with them; the stream then carries them.
package remotecontrol

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/lsproto"
)

// lsStoredSteps loads a conversation's stored steps as the proto-JSON
// the Web UI renders.
//
// The reference's steps are binary protobuf blobs — a
// gemini_coder.Step — and the clone has the schema (the descriptor set
// the frontend bundle carries), so it decodes them into the very shape
// the reference sent: the model's thinking, the usage metadata and the
// user config all survive.
//
// A blob the clone cannot decode is skipped: the conversation is then
// shown without that step, which is better than a broken step.
func lsStoredSteps(id string) []map[string]any {
	rows, err := convstore.StoredStepPayloads(id)
	if err != nil {
		log.Printf("remotecontrol: load steps %s: %v", id, err)
		return nil
	}
	codec, err := lsproto.Shared()
	if err != nil {
		log.Printf("remotecontrol: proto codec: %v", err)
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if len(row.Payload) == 0 {
			continue
		}
		raw, err := codec.DecodeStep(row.Payload)
		if err != nil {
			continue
		}
		var step map[string]any
		if err := json.Unmarshal(raw, &step); err != nil {
			continue
		}
		// The step's type and status come from the row: the decoded
		// blob spells them as the enum's names and the frame wants the
		// numbers.
		step["type"] = int(row.Type)
		step["status"] = int(row.Status)
		out = append(out, step)
	}
	return out
}

// handleLSRequestAgentStatePageUpdate answers the Web UI's page
// request for a conversation.
//
// The response is empty — the steps travel through the state stream.
func (d *Dispatcher) handleLSRequestAgentStatePageUpdate(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		ConversationID string `json:"conversationId"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	id := strings.TrimSpace(req.ConversationID)
	if id == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "conversationId is required"}, true
	}
	if steps := lsStoredSteps(id); len(steps) > 0 {
		d.trajectories.Get(id).Seed(steps)
	}
	return map[string]any{}, nil, true
}
