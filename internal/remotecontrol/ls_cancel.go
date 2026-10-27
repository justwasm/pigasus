// Package remotecontrol — ls_cancel.go: the Web UI's cancel RPCs.
//
// 【一手】 2026-10-26, from the IDE web bundle
// (internal/rcfrontend/assets/main.js): the chat area's Stop button
// calls the bundle's `cancelCascadeInvocation` hook — the
// LanguageServerService's `CancelCascadeInvocation`:
//
//	cancelCascadeInvocation:async(...p)=>{var r=d,t=d;try{t(await r(k(...p)))}finally{r()}}
//	k=async p=>g(r=>r.cancelCascadeInvocation(q(lga,p)))
//
// and the button hands it the cascade id and asks for the background
// tasks to die with the turn:
//
//	{cascadeId:b,killBackgroundTasks:!0}
//
// The clone answered the *legacy* `CancelRemoteAgentCascade` (the
// dashboard's method) and none of the IDE dialect's three cancel
// methods, so the button's RPC came back "Method not found" and the
// turn ran on.
//
// The three methods and their proto shapes (the FileDescriptorProto set
// in lsproto/descriptor/language_server.protoset):
//
//	CancelCascadeInvocation{cascade_id, kill_background_tasks, notify_parent}
//	  → CancelCascadeInvocationResponse{}        (no fields)
//	ForceStopCascadeTree{conversation_id}
//	  → ForceStopCascadeTreeResponse{stopped_conversation_ids}
//	CancelCascadeSteps{cascade_id, step_indices}
//	  → CancelCascadeStepsResponse{}             (no fields)
//
// 【一手】 the reference's own answer for a cascade it does not know: a
// 500 with `{"code":"unknown","message":"cascade not found (error ID: …)"}`.
// The clone answers its own error envelope instead (-32004), the way
// its other cascade methods do.
package remotecontrol

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/tools"
)

// The statuses the cancel puts on a step.
const (
	// lsStepStatusCanceled is CORTEX_STEP_STATUS_CANCELED — the status
	// of a step the user cancelled.
	//
	// 【一手】 the IDE web bundle renders a step with this status as
	// "Canceled" (`b===V.CortexStepStatus.CANCELED` in the tool-call
	// renderer, and the badge reads "CANCELED").
	lsStepStatusCanceled = 6
	// lsStepStatusInterrupted is CORTEX_STEP_STATUS_INTERRUPTED — the
	// status of a turn that was interrupted rather than finished.
	//
	// 【一手】 the IDE web bundle's badge reads "INTERRUPTED" for it.
	lsStepStatusInterrupted = 12
)

// routeLSCancelMethods handles the IDE dialect's three cancel methods.
//
// It is consulted before the alias table (reference_surface.go), so the
// IDE's own names win over the clone's legacy ones — the legacy
// `CancelRemoteAgentCascade` answers with its own envelope and stays
// where it is.
func (d *Dispatcher) routeLSCancelMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "CancelCascadeInvocation":
		return d.handleLSCancelCascadeInvocation(ctx, params)
	case "ForceStopCascadeTree":
		return d.handleLSForceStopCascadeTree(params)
	case "CancelCascadeSteps":
		return d.handleLSCancelCascadeSteps(params)
	}
	return nil, nil, false
}

// handleLSCancelCascadeInvocation stops the turn in flight.
//
// The request is the Web UI's Stop button:
//
//	{"cascadeId":"<id>","killBackgroundTasks":true}
//
// The answer is the empty message the proto declares
// (CancelCascadeInvocationResponse has no fields).
func (d *Dispatcher) handleLSCancelCascadeInvocation(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody, bool) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}, true
	}
	fields, err := lsParams(params)
	if err != nil {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
	}
	cascadeID := lsStringField(fields, "cascadeId", "cascade_id")
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascadeId is required"}, true
	}
	c, ok := d.lsCascadeFor(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}, true
	}
	d.lsCancelCascade(c, "user requested")
	// The button asks for the background tasks to die with the turn.
	if lsBoolField(fields, "killBackgroundTasks", "kill_background_tasks") {
		tools.KillConversationTasks(cascadeID)
	}
	// notify_parent tells a parent cascade that its child was
	// cancelled. The clone has no cascade tree — no subagent runs under
	// a cascade — so there is no parent to tell.
	return map[string]any{}, nil, true
}

// handleLSForceStopCascadeTree stops the conversation's turn.
//
// The request names the conversation, the answer lists the conversations
// that were stopped.
func (d *Dispatcher) handleLSForceStopCascadeTree(params json.RawMessage) (any, *rpcErrorBody, bool) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}, true
	}
	fields, err := lsParams(params)
	if err != nil {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
	}
	conversationID := lsStringField(fields, "conversationId", "conversation_id")
	if conversationID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "conversationId is required"}, true
	}
	c, ok := d.lsCascadeFor(conversationID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: conversationID}, true
	}
	d.lsCancelCascade(c, "user requested")
	return map[string]any{
		"stoppedConversationIds": []string{conversationID},
	}, nil, true
}

// handleLSCancelCascadeSteps cancels the steps the Web UI named.
//
// 【一手】 the bundle's "Cancel All Tasks" button (the background-tasks
// panel) hands over the step indices of the running background tasks:
//
//	{cascadeId:p,stepIndices:r}
//
// A step that names a background task is killed; every step that was
// named is marked cancelled, because that is what the button asked for
// and the Web UI renders a cancelled step as "Canceled".
func (d *Dispatcher) handleLSCancelCascadeSteps(params json.RawMessage) (any, *rpcErrorBody, bool) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}, true
	}
	fields, err := lsParams(params)
	if err != nil {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
	}
	cascadeID := lsStringField(fields, "cascadeId", "cascade_id")
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascadeId is required"}, true
	}
	c, ok := d.lsCascadeFor(cascadeID)
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}, true
	}
	d.lsCancelSteps(d.trajectories.Get(c.ID), lsUint32Fields(fields, "stepIndices", "step_indices"))
	return map[string]any{}, nil, true
}

// lsCancelCascade stops the turn in flight.
//
// Two cancels have to fire, and they cancel two different things:
//
//   - the trajectory's turn cancel — the context the turn itself runs
//     under. The trajectory marks the turn interrupted, so the Web UI
//     shows it as stopped and not as a failure.
//   - the cascade's per-turn cancel — the cascade's own "the user
//     pressed stop" signal, the one the legacy dashboard's cancel
//     fires.
//
// The first one stops the turn; the second one is the cascade's own
// bookkeeping.
func (d *Dispatcher) lsCancelCascade(c *Cascade, reason string) bool {
	if c == nil {
		return false
	}
	interrupted := false
	if traj := d.trajectories.Get(c.ID); traj != nil {
		interrupted = traj.Interrupt()
		// The card of a question the turn is waiting for goes away
		// with the turn: a step that waits for an answer nobody is
		// going to give is not a waiting step.
		lsCancelWaitingSteps(traj)
	}
	fired := c.Cancel(reason)
	if fired {
		c.recordCancel(reason, time.Now().UTC())
	}
	return interrupted || fired
}

// lsCancelSteps cancels the steps the Web UI named.
//
// It returns the ids of the background tasks it killed.
func (d *Dispatcher) lsCancelSteps(traj *lsTrajectory, indices []uint32) []string {
	if traj == nil {
		return nil
	}
	snap := traj.Snapshot()
	var killed []string
	for _, index := range indices {
		if int(index) >= len(snap.Steps) {
			continue
		}
		step := lsCopyStep(snap.Steps[index])
		step["status"] = lsStepStatusCanceled
		traj.Update(int(index), step)
		if id := lsBackgroundTaskID(step); id != "" {
			if err := tools.KillBackgroundTask(id); err == nil {
				killed = append(killed, id)
			}
		}
	}
	return killed
}

// lsCancelWaitingSteps marks the steps that wait for the user as
// cancelled.
func lsCancelWaitingSteps(traj *lsTrajectory) {
	if traj == nil {
		return
	}
	snap := traj.Snapshot()
	for i, step := range snap.Steps {
		if lsStepStatusOf(step) != lsStepStatusWaiting {
			continue
		}
		cancelled := lsCopyStep(step)
		cancelled["status"] = lsStepStatusCanceled
		traj.Update(i, cancelled)
	}
}

// lsBackgroundTaskID reads the background task id off a tool-call step.
//
// 【一手】 the reference's launch acknowledgement names the task:
//
//	Tool is running as a background task with task id: <conversation-id>/task-<n>
//
// (internal/tools/background_task.go, taskLaunchResult).
func lsBackgroundTaskID(step map[string]any) string {
	generic, _ := step["generic"].(map[string]any)
	if generic == nil {
		return ""
	}
	result, _ := generic["result"].(map[string]any)
	if result == nil {
		return ""
	}
	text, _ := result["result"].(string)
	const marker = "background task with task id: "
	idx := strings.Index(text, marker)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(marker):]
	if end := strings.IndexAny(rest, "\r\n"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

// lsStepStatusOf reads a step's status.
func lsStepStatusOf(step map[string]any) int {
	status, _ := step["status"].(int)
	return status
}

// lsCascadeFor finds the cascade a cancel names.
//
// The Web UI mints one uuid and uses it for both the cascade id and
// the conversation id, so the registry's key is the id the cancel
// carries. A conversation id is looked up too: the cascade's own
// conversation id is what its first turn produced, and a caller that
// knows only the conversation has to find the cascade as well.
func (d *Dispatcher) lsCascadeFor(id string) (*Cascade, bool) {
	if d == nil || d.cascades == nil || id == "" {
		return nil, false
	}
	if c, ok := d.cascades.Get(id); ok {
		return c, true
	}
	for _, cascadeID := range d.cascades.IDs() {
		c, ok := d.cascades.Get(cascadeID)
		if !ok {
			continue
		}
		if c.ConvID() == id {
			return c, true
		}
	}
	return nil, false
}

// ConvID returns the cascade's conversation id.
//
// The field is written under the cascade's lock when a turn produces
// one, so the reader takes the lock too.
func (c *Cascade) ConvID() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ConversationID
}

// Interrupted reports whether the turn in flight was interrupted.
//
// The trajectory's sink reads it when the turn ends: a turn the user
// stopped is not a turn that finished.
func (t *lsTrajectory) Interrupted() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.interrupted
}

// lsParams decodes the request's fields.
//
// The IDE's methods take proto-JSON, so a field may arrive in the
// camelCase spelling or in the proto's own snake_case spelling; both
// are read.
func lsParams(params json.RawMessage) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	if len(params) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(params, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]json.RawMessage{}
	}
	return out, nil
}

// lsStringField reads a string field in either spelling.
func lsStringField(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// lsBoolField reads a boolean field in either spelling.
func lsBoolField(fields map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value bool
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return false
}

// lsUint32Fields reads a repeated unsigned field in either spelling.
func lsUint32Fields(fields map[string]json.RawMessage, names ...string) []uint32 {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value []uint32
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return nil
}
