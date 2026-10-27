// Package remotecontrol — ls_interaction_rpc.go: the Web UI's answer
// RPC.
//
// 【一手】 2026-10-26, from the IDE web bundle (main.js): the card
// sends the answer as
//
//	HandleCascadeUserInteraction{cascadeId, interaction}
//
// where the interaction is an exa.cortex_pb.CascadeUserInteraction —
// the trajectory id, the step index, and the oneof member the user
// answered. The procedure is the IDE's own
// `exa.language_server_pb.LanguageServerService/HandleCascadeUserInteraction`.
//
// The handler finds the pending interaction by the step index the
// answer names, resolves it, and the waiting tool continues.
//
// The reference's own gRPC surface has a method of the same name
// (reference_surface.go aliases it to the clone's legacy
// HandleRemoteAgentCascadeUserInteraction), but the IDE dialect is the
// one the Web UI speaks — and the IDE dialect is routed first.
package remotecontrol

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// lsHandleCascadeUserInteraction answers the Web UI's answer RPC.
//
// The answer names the trajectory and the step, not the interaction
// id: the frontend reads both off the step's metadata
// (`metadata.sourceTrajectoryStepInfo`).
func (d *Dispatcher) lsHandleCascadeUserInteraction(params json.RawMessage) (any, *rpcErrorBody, bool) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}, true
	}
	var req struct {
		CascadeID   string          `json:"cascadeId"`
		Interaction json.RawMessage `json:"interaction"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	cascadeID := strings.TrimSpace(req.CascadeID)
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascadeId is required"}, true
	}
	c, ok := d.cascades.Get(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}, true
	}
	cue, err := lsDecodeCascadeUserInteraction(req.Interaction)
	if err != nil {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
	}
	intr, ok := c.Pending().GetByStep(cue.TrajectoryID, cue.StepIndex)
	if !ok {
		return nil, &rpcErrorBody{
			Code:    ErrCodeInteractionNotFound,
			Message: "Interaction not found",
			Data:    cue.TrajectoryID + ":" + strconv.Itoa(cue.StepIndex),
		}, true
	}
	now := time.Now().UTC()
	if err := c.Pending().Resolve(intr.ID, InteractionResult{Response: req.Interaction, DecidedAt: now}); err != nil {
		if errors.Is(err, ErrInteractionNotFound) {
			return nil, &rpcErrorBody{Code: ErrCodeInteractionNotFound, Message: "Interaction not found", Data: intr.ID}, true
		}
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: err.Error()}, true
	}
	return map[string]any{}, nil, true
}
