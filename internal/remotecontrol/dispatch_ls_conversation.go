// Package remotecontrol — dispatch_ls_conversation.go: the
// conversation batch of the LanguageServerService — the methods that
// answer *about* a conversation rather than about its trajectory.
//
// The workbench asks about a conversation from two sides: the
// conversation list (SearchConversations,
// GetAllCascadeTrajectories) and the conversation itself
// (GetConversationMetadata, GetTrajectoryTurns,
// DeleteCascadeTrajectory).
//
// 【一手】 2026-10-27, measured against the reference binary in its
// mocked standalone mode (out/langserver/coverage-351.json,
// out/langserver/conversation.json):
//
//   - the reference answers an empty map for GetAllCascadeTrajectories
//     and an empty list for SearchConversations — its mocked store
//     holds no conversation;
//   - the reference rejects a missing argument with
//     "invalid_argument: conversation id is required" and an unknown
//     conversation with "not_found: trajectory not found";
//   - the reference answers GetConversationMetadata,
//     LoadTrajectory, GetTrajectoryTurns and GetTrajectoryFileDiffs
//     with an empty body when the arguments are there.
//
// The clone answers its own conversation store: the same store the
// /resume picker reads and the sidebar lists.
package remotecontrol

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
)

// routeLanguageServerConversationMethods handles the conversation
// batch — the methods that answer about a conversation.
//
// It is a *read* side with one exception (DeleteCascadeTrajectory),
// and it reads the clone's own conversation store.
func (d *Dispatcher) routeLanguageServerConversationMethods(method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "SearchConversations":
		return d.handleLSSearchConversations(params)
	case "GetConversationMetadata":
		return d.handleLSGetConversationMetadata(params)
	case "GetTrajectoryTurns":
		return d.handleLSGetTrajectoryTurns(params)
	case "GetAllCascadeTrajectories":
		return d.handleLSGetAllCascadeTrajectories(params)
	case "DeleteCascadeTrajectory":
		return d.handleLSDeleteCascadeTrajectory(params)
	}
	return nil, nil, false
}

// handleLSSearchConversations answers the conversation list.
//
// 【一手】 2026-10-27: the reference answers the results the query
// matched. The clone searches its own conversation store (the same
// store the /resume picker reads), newest first.
func (d *Dispatcher) handleLSSearchConversations(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	metas, err := convstore.ListConversations()
	if err != nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: err.Error()}, true
	}
	results := lsSearchResults(metas, req.Query, req.Limit)
	// proto-JSON omits a member that carries its default, so a
	// conversation list that is empty answers {} — the reference's
	// own answer for a store that holds nothing.
	answer := map[string]any{}
	if len(results) > 0 {
		answer["results"] = results
	}
	return answer, nil, true
}

// lsSearchResults is the conversation list the query matched.
//
// The query matches the title, case-insensitively; an empty query
// matches every conversation. The limit caps the list — the
// reference's own default is small, and the workbench sends one.
func lsSearchResults(metas []convstore.ConversationMeta, query string, limit int) []map[string]any {
	if limit <= 0 {
		limit = 50
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	out := make([]map[string]any, 0, len(metas))
	for _, meta := range metas {
		if needle != "" && !strings.Contains(strings.ToLower(meta.Title), needle) {
			continue
		}
		entry := map[string]any{
			"cascadeId":   meta.ID,
			"title":       meta.Title,
			"matchSource": "MATCH_SOURCE_TITLE",
		}
		if meta.Project != "" {
			entry["workspaceName"] = meta.Project
		}
		if !meta.ModTime.IsZero() {
			entry["lastModifiedTime"] = meta.ModTime.UTC().Format(time.RFC3339)
		}
		out = append(out, entry)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// handleLSGetConversationMetadata answers the metadata of a
// conversation.
//
// 【反编译】 exa.language_server_pb.GetConversationMetadataResponse:
// metadata=1 (a CortexTrajectoryMetadata), num_steps=2,
// trajectory_id=3. The metadata carries the workspace the
// conversation ran in and when it started.
//
// 【一手】 2026-10-27: the reference rejects a missing conversation id
// with "invalid_argument: conversation id is required" and answers an
// empty body for a conversation it has.
func (d *Dispatcher) handleLSGetConversationMetadata(params json.RawMessage) (any, *rpcErrorBody, bool) {
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
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "conversation id is required"}, true
	}
	snap, ok := d.lsSnapshot(id)
	if !ok {
		return nil, &rpcErrorBody{Code: -32019, Message: "not_found", Data: "trajectory not found"}, true
	}
	metadata := map[string]any{}
	if ws := d.lsWorkspace(); ws != "" {
		metadata["workspaceUris"] = []string{lsNormalizedURI(ws)}
	}
	if !snap.CreatedAt.IsZero() {
		metadata["createdAt"] = snap.CreatedAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"metadata":    metadata,
		"numSteps":    len(snap.Steps),
		"trajectoryId": snap.TrajectoryID,
	}, nil, true
}

// handleLSGetTrajectoryTurns answers the turns of a conversation.
//
// 【反编译】 exa.language_server_pb.GetTrajectoryTurnsRequest:
// cascade_id=1, step_indices=2, trajectory_verbosity=3. The response
// carries a list of exa.jetski_cortex_pb.TurnSummary —
// start_step_index, first_step, response_preview.
//
// A turn starts at a user-input step and runs until the next one, so
// the turns of a conversation are its user messages and the replies
// that followed them.
func (d *Dispatcher) handleLSGetTrajectoryTurns(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		CascadeID   string   `json:"cascadeId"`
		StepIndices []uint32 `json:"stepIndices"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	cascadeID := strings.TrimSpace(req.CascadeID)
	snap, ok := d.lsSnapshot(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: -32019, Message: "not_found", Data: "trajectory not found"}, true
	}
	turns := lsTrajectoryTurns(snap.Steps, req.StepIndices)
	return map[string]any{"turns": turns}, nil, true
}

// handleLSGetAllCascadeTrajectories answers the summary of every
// conversation the clone knows.
//
// 【反编译】 exa.language_server_pb.GetAllCascadeTrajectoriesResponse:
// trajectory_summaries=1, a map of the conversation id to an
// exa.jetski_cortex_pb.CascadeTrajectorySummary.
//
// 【一手】 2026-10-27: the reference answers an empty map — its mocked
// store holds no conversation.
func (d *Dispatcher) handleLSGetAllCascadeTrajectories(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		ExcludeSubtrajectories bool `json:"excludeSubtrajectories"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	summaries := map[string]any{}
	for _, snap := range d.lsSnapshots() {
		summaries[snap.ConversationID] = lsCascadeSummary(snap)
	}
	// proto-JSON omits an empty map: a store that holds no
	// conversation answers {} — the reference's own answer.
	answer := map[string]any{}
	if len(summaries) > 0 {
		answer["trajectorySummaries"] = summaries
	}
	return answer, nil, true
}

// handleLSDeleteCascadeTrajectory drops a conversation's trajectory.
//
// 【一手】 2026-10-27: the reference answers an empty message for a
// cascade id it does not know, and the same for one it does — the
// delete is the caller's own bookkeeping.
func (d *Dispatcher) handleLSDeleteCascadeTrajectory(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		CascadeID string `json:"cascadeId"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	cascadeID := strings.TrimSpace(req.CascadeID)
	if cascadeID != "" && d != nil && d.trajectories != nil {
		d.trajectories.Delete(cascadeID)
	}
	return map[string]any{}, nil, true
}
