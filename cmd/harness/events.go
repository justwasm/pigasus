// events.go — the PiG event stream, mapped onto the protocol's
// OutputEvents.
//
// # The mapping
//
//	PiG event                          OutputEvent
//	---------------------------------  ------------------------------------
//	user input                         StepUpdate{USER, DONE, text}
//	TurnStartEvent                    TrajectoryStateUpdate{RUNNING}
//	MessageUpdateEvent + text delta   StepUpdate{ACTIVE, text_delta, text}
//	MessageUpdateEvent + thinking     StepUpdate{ACTIVE, thinking_delta}
//	MessageEndEvent                   StepUpdate{ACTIVE, text} + StepUpdate{DONE, text} + usage
//	ToolExecutionStartEvent           StepUpdate{ACTIVE, <action>}
//	ToolExecutionEndEvent             StepUpdate{DONE, <action>}
//	CompactionStartEvent              StepUpdate{compaction}
//	AgentEndEvent                     TrajectoryStateUpdate{FULLY_IDLE}
//
// The tool action is the protocol's own vocabulary: `run_command` becomes
// an `ActionRunCommand`, `view_file` an `ActionViewFile`, and a tool the
// client declared becomes an `ActionCustomTool` carrying the wire
// `ToolCall` and `ToolResponse`.
//
// # The step index
//
// The protocol's `step_index` names a *logical* step, not a frame: the
// reference sends the model's text as three frames — the delta, the full
// text, and the done — all under one index, and the client's
// `_StepTracker` merges them because the key is
// `(trajectory_id, step_index)`.
//
// The reference's numbering, captured from the binary:
//
//	0   the user's message
//	1   the model's first action — its text, or a tool call
//	2   the next action
//
// So the index advances per action, not per frame.
//
// # The frames' shape
//
// Every step update the reference writes carries `thinking`,
// `thinking_delta`, `text_delta` and `text`, even when they are empty:
// the fields have explicit presence, and the reference sets them all. A
// frame that omits one of them is a different frame.
//
// The model's text targets the user (`TARGET_USER`) — the client's
// `is_complete_response` needs it — while a tool call targets the
// environment (`TARGET_ENVIRONMENT`).
//
// # Limitations
//
//   - The reference's `ActionEditFile` carries a diff block; this mapper
//     only fills in the file path. The diff itself is not reconstructed.
//   - `ActionListDirectory`'s per-entry results are not filled in: the
//     clone's tool returns text, not a structured listing.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/harnesstools"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// onEvent is the PiG event observer the runtime calls.
//
// The callback has no context of its own, so the running turn's context is
// used: only one turn runs at a time, and the turn's lifetime bounds the
// events it produces.
func (c *conversation) onEvent(ev piagent.AgentEvent) {
	ctx := c.currentContext()
	switch e := ev.(type) {
	case piagent.TurnStartEvent:
		// The running state is written by the turn loop, before the
		// user's message is echoed. The reference orders them that
		// way: `STATE_RUNNING`, then the user's step.
	case piagent.MessageUpdateEvent:
		switch inner := e.AssistantMessageEvent.(type) {
		case pi.TextDeltaEvent:
			c.emitTextDelta(inner.Delta)
		case pi.ThinkingDeltaEvent:
			c.emitThinkingDelta(inner.Delta)
		}
	case piagent.MessageEndEvent:
		c.emitModelMessage(e.Message)
		c.recordModelCall()
		if usage := messageUsage(e.Message); usage != nil {
			c.recordUsage(uint64(usage.Input), uint64(usage.Output))
			if len(messageToolCalls(e.Message)) > 0 {
				// The reference reports the usage of a response that
				// called a tool *after* the call has been dispatched:
				// the announcement, the start frame, and only then the
				// token count. The count waits for the call.
				c.holdUsage(usage)
			} else {
				c.emitUsage(usage)
			}
		}
	case piagent.ToolExecutionStartEvent:
		c.recordToolCall()
		c.emitToolStart(e)
		c.flushUsage()
	case piagent.ToolExecutionEndEvent:
		c.emitToolEnd(e)
		c.hooks.postTool(ctx, e)
		if e.IsError {
			c.hooks.onToolError(ctx, e)
		}
	case piagent.CompactionStartEvent:
		c.emitCompaction()
		c.hooks.onCompaction(ctx)
	case piagent.CompactionEndEvent:
		c.logger.Debug("harness: compaction finished", "reason", e.Reason, "aborted", e.Aborted)
	}
}

// onText is the text callback the runtime calls.
//
// The text is already carried by the message events, so this callback does
// nothing: emitting it twice would double the client's transcript.
func (c *conversation) onText(string) {}

// holdUsage keeps a model response's token usage until the call it made
// has been dispatched.
//
// The reference reports the usage of a response that called a tool after
// the call's own frames: the announcement, the start frame, and only then
// the token count. A response that called nothing reports its usage right
// away, with its text.
func (c *conversation) holdUsage(usage *pi.Usage) {
	c.pendingUsageMu.Lock()
	defer c.pendingUsageMu.Unlock()
	c.pendingUsage = usage
}

// flushUsage writes a held usage, if there is one.
//
// The call it waits for may have been denied before it started — a denied
// call has no start frame — so the count is written when the call is
// reported either way.
func (c *conversation) flushUsage() {
	if usage := c.takeUsage(); usage != nil {
		c.emitUsage(usage)
	}
}

// takeUsage hands over the held usage, if there is one.
func (c *conversation) takeUsage() *pi.Usage {
	c.pendingUsageMu.Lock()
	defer c.pendingUsageMu.Unlock()
	usage := c.pendingUsage
	c.pendingUsage = nil
	return usage
}

// recordModelCall charges one model call against the budget.
//
// The manager exists only after the conversation was initialised; an event
// that arrives before that is not charged.
func (c *conversation) recordModelCall() {
	if c.budget == nil {
		return
	}
	c.budget.RecordModelCall()
}

// recordToolCall charges one tool call against the budget.
func (c *conversation) recordToolCall() {
	if c.budget == nil {
		return
	}
	c.budget.RecordToolCall()
}

// recordUsage charges the tokens of one model response against the budget.
func (c *conversation) recordUsage(input, output uint64) {
	if c.budget == nil {
		return
	}
	c.budget.RecordUsage(input, output)
}

// emitUsage reports a turn's token usage.
//
// The protocol's `UsageUpdate` carries the conversation's running total
// plus a per-trajectory breakdown. The reference's frames report the
// running total — the captured frames go 18, 36, 54, each one the sum of
// the responses so far — so the clone accumulates too.
//
// The reference sets `thoughts_token_count` even when the model reported
// no thinking, so the field is always present.
func (c *conversation) emitUsage(usage *pi.Usage) {
	if usage == nil {
		return
	}
	meta := &pb.UsageMetadata{}
	meta.SetPromptTokenCount(c.addUsage(usagePrompt, uint64(usage.Input)))
	meta.SetCachedContentTokenCount(c.addUsage(usageCached, uint64(usage.CacheRead)))
	meta.SetCandidatesTokenCount(c.addUsage(usageCandidates, uint64(usage.Output)))
	meta.SetTotalTokenCount(c.addUsage(usageTotal, uint64(usage.TotalTokens)))
	thoughts := uint64(0)
	if usage.Reasoning != nil {
		thoughts = uint64(*usage.Reasoning)
	}
	meta.SetThoughtsTokenCount(c.addUsage(usageThoughts, thoughts))
	entry := &pb.TrajectoryUsageEntry{}
	entry.SetTrajectoryId(c.trajectoryID)
	entry.SetUsage(meta)
	update := &pb.UsageUpdate{}
	update.SetTotal(meta)
	update.SetAgents([]*pb.TrajectoryUsageEntry{entry})
	ev := &pb.OutputEvent{}
	ev.SetUsageUpdate(update)
	c.emit(c.currentContext(), ev)
}

// The counters the running token total keeps.
const (
	usagePrompt = iota
	usageCached
	usageCandidates
	usageTotal
	usageThoughts
	// usageCount is how many counters there are.
	usageCount
)

// addUsage adds to one counter of the conversation's running token count
// and returns the counter's new value.
func (c *conversation) addUsage(counter int, count uint64) uint64 {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	if counter < 0 || counter >= usageCount {
		return 0
	}
	c.usageTotal[counter] += count
	return c.usageTotal[counter]
}

// emitRunning reports that the trajectory is running.
func (c *conversation) emitRunning() {
	c.emitState(pb.TrajectoryStateUpdate_STATE_RUNNING, pb.TrajectoryStateUpdate_STOP_REASON_UNSPECIFIED)
}

// emitIdle reports that the trajectory went idle, with an optional stop
// reason.
func (c *conversation) emitIdle(reason pb.TrajectoryStateUpdate_StopReason) {
	c.emitState(pb.TrajectoryStateUpdate_STATE_FULLY_IDLE, reason)
}

// emitFailed reports that the run failed.
//
// The reference's trajectory carries the reason when the run fails: the
// model used up its retries and the harness gave up.
//
// The turn's context is cancelled when the run fails — the reference halts
// the turn — so the frame is written with a context of its own: the reason
// is the last thing the client hears, and it has to hear it.
func (c *conversation) emitFailed(reason string) {
	update := &pb.TrajectoryStateUpdate{}
	update.SetTrajectoryId(c.trajectoryID)
	update.SetState(pb.TrajectoryStateUpdate_STATE_FULLY_IDLE)
	update.SetError(reason)
	ev := &pb.OutputEvent{}
	ev.SetTrajectoryStateUpdate(update)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.currentContext()), 5*time.Second)
	defer cancel()
	c.emit(ctx, ev)
}

// emitState writes one TrajectoryStateUpdate.
//
// The stop reason is only written when it says something: the field has
// explicit presence, and the reference leaves it unset for an ordinary
// turn.
func (c *conversation) emitState(state pb.TrajectoryStateUpdate_State, reason pb.TrajectoryStateUpdate_StopReason) {
	update := &pb.TrajectoryStateUpdate{}
	update.SetTrajectoryId(c.trajectoryID)
	update.SetState(state)
	if reason != pb.TrajectoryStateUpdate_STOP_REASON_UNSPECIFIED {
		update.SetStopReason(reason)
	}
	ev := &pb.OutputEvent{}
	ev.SetTrajectoryStateUpdate(update)
	c.emit(c.currentContext(), ev)
}

// newStep builds a step update with the protocol's shape fields set.
//
// The four text fields are always written, even empty: they have explicit
// presence, and the reference sets them all on every frame it sends.
func newStep(source pb.StepUpdate_Source, target pb.StepUpdate_Target, state pb.StepUpdate_State) *pb.StepUpdate {
	update := &pb.StepUpdate{}
	update.SetSource(source)
	update.SetTarget(target)
	update.SetState(state)
	update.SetThinking("")
	update.SetThinkingDelta("")
	update.SetTextDelta("")
	update.SetText("")
	return update
}

// emitUserInput echoes the user's message.
//
// The reference writes the message the client sent as a step of its own,
// before the model's answer: source `SOURCE_USER`, target `TARGET_MODEL`,
// the raw text in both `text_delta` and `text`.
func (c *conversation) emitUserInput(text string) {
	if text == "" {
		return
	}
	index := c.beginStep()
	update := newStep(pb.StepUpdate_SOURCE_USER, pb.StepUpdate_TARGET_MODEL, pb.StepUpdate_STATE_DONE)
	update.SetTextDelta(text)
	update.SetText(text)
	c.emitStep(index, update)
}

// emitTextDelta writes one text delta.
//
// The reference sends the delta *and* the text accumulated so far, so a
// client that missed a frame still has the whole message.
func (c *conversation) emitTextDelta(delta string) {
	if delta == "" {
		return
	}
	index := c.modelStepIndex()
	accumulated := c.appendText(delta)
	update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_USER, pb.StepUpdate_STATE_ACTIVE)
	update.SetTextDelta(delta)
	update.SetText(accumulated)
	c.emitStep(index, update)
}

// emitThinkingDelta writes one thinking delta.
func (c *conversation) emitThinkingDelta(delta string) {
	if delta == "" {
		return
	}
	index := c.modelStepIndex()
	update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_USER, pb.StepUpdate_STATE_ACTIVE)
	update.SetThinkingDelta(delta)
	c.emitStep(index, update)
}

// emitText writes the model's finished message.
//
// The reference sends it twice: once as an active step carrying the full
// text, and once as the step's done. Both frames share the step index,
// so the client folds them into one step.
func (c *conversation) emitText(text string) {
	if text == "" {
		// A message with no text is a tool call. The tool's own step
		// carries the announcement, so there is nothing to write.
		c.closeModelStep()
		return
	}
	index := c.modelStepIndex()

	active := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_USER, pb.StepUpdate_STATE_ACTIVE)
	active.SetText(text)
	c.emitStep(index, active)

	done := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_USER, pb.StepUpdate_STATE_DONE)
	done.SetText(text)
	c.emitStep(index, done)

	c.closeModelStep()
}

// emitStepError reports a failure that ended the turn.
func (c *conversation) emitStepError(message string) {
	if message == "" {
		message = "the turn failed"
	}
	index := c.beginStep()
	update := newStep(pb.StepUpdate_SOURCE_SYSTEM, pb.StepUpdate_TARGET_MODEL, pb.StepUpdate_STATE_ERROR)
	update.SetErrorMessage(message)
	c.emitStep(index, update)
}

// emitCompaction reports that the context was compacted.
func (c *conversation) emitCompaction() {
	index := c.beginStep()
	update := newStep(pb.StepUpdate_SOURCE_SYSTEM, pb.StepUpdate_TARGET_MODEL, pb.StepUpdate_STATE_ACTIVE)
	update.SetCompaction(&pb.ActionCompaction{})
	c.emitStep(index, update)
}

// emitStep writes one StepUpdate under the given step index.
func (c *conversation) emitStep(index uint32, update *pb.StepUpdate) {
	update.SetCascadeId(c.cascadeID)
	update.SetTrajectoryId(c.trajectoryID)
	update.SetStepIndex(index)
	ev := &pb.OutputEvent{}
	ev.SetStepUpdate(update)
	c.emit(c.currentContext(), ev)
}

// emit writes one OutputEvent, swallowing the write error.
//
// A failed write means the connection is gone; the read loop will notice
// and tear the conversation down. Logging from here would be noise.
func (c *conversation) emit(ctx context.Context, ev *pb.OutputEvent) {
	if err := c.send(ctx, ev); err != nil {
		c.logger.Debug("harness: write failed", "error", err)
	}
}

// emitSessionEnd acknowledges the client's session-end request.
//
// The SDK's `disconnect()` sends `InputEvent.session_end_request` and then
// *blocks* on `session_end_done`, an event only this frame sets:
//
//	await self._send_session_end_request()
//	await self._processor.session_end_done.wait()
//
// A harness that never answers leaves the client hanging on close. The
// frame carries one boolean: whether the session ended cleanly.
func (c *conversation) emitSessionEnd(ctx context.Context, success bool) {
	ev := &pb.OutputEvent{}
	ev.SetSessionEndResponse(success)
	c.emit(ctx, ev)
}

// toolExecutionText is the text the reference puts on a tool call's step.
//
// The captured frames all carry the same phrase, whatever the tool is:
// the model's message to the user is "Tool execution".
const toolExecutionText = "Tool execution"

// finishText is the text the reference puts on a `finish` call's step.
//
// The `finish` call is the one tool call that is an answer rather than an
// action: the reference says what happened instead of "Tool execution",
// and it targets the user rather than the environment.
const finishText = "Finished"

// stepTarget reports the target of a tool call's frames.
//
// The reference's tool calls target the environment; the `finish` call
// targets the user, because it ends the interaction with an answer.
func stepTarget(name string) pb.StepUpdate_Target {
	if name == "finish" {
		return pb.StepUpdate_TARGET_USER
	}
	return pb.StepUpdate_TARGET_ENVIRONMENT
}

// toolStepText reports the text a tool call's frames carry.
func toolStepText(name string) string {
	if name == "finish" {
		return finishText
	}
	return toolExecutionText
}

// emitModelMessage reports the model's finished message.
//
// A message with text is the model's answer: the reference sends it twice
// — once as an active step carrying the full text, and once as the step's
// done.
//
// A message with a tool call is not an answer but a *call*, and the
// reference renders it as the call's announcement: an active step whose
// text is "Tool execution" and whose action is the call. The
// announcement opens the step the call's later frames reuse.
func (c *conversation) emitModelMessage(msg piagent.AgentMessage) {
	calls := messageToolCalls(msg)
	if len(calls) == 0 {
		c.emitText(messageText(msg))
		return
	}
	if text := messageText(msg); text != "" {
		c.emitText(text)
	}
	for _, call := range calls {
		c.announceToolCall(call)
	}
}

// announceToolCall reports a tool call the model asked for.
//
// The frame comes *before* the turn's usage: the reference writes the
// model's message — which is the call — first, and the token count after
// it.
//
// The client's policy is asked *before* the call is announced: the
// reference never announces a call it is going to deny, and reports the
// denial on the step the announcement would have used.
func (c *conversation) announceToolCall(call pi.ToolCall) {
	args, err := json.Marshal(call.Arguments)
	if err != nil {
		args = json.RawMessage("{}")
	}
	index := c.beginStep()
	c.rememberToolArgs(call.Name, args)
	c.rememberToolStep(call.Name, index)

	verdict := c.evaluateToolCallOnce(c.currentContext(), call.ID, call.Name, args)
	if verdict.Invalid {
		// The reference never announces a call it refuses: the model
		// produced a call the harness cannot run, and the reference
		// reports that on the step the announcement would have used.
		return
	}
	if verdict.Blocked {
		c.emitDeniedCall(index, verdict)
		return
	}
	if call.Name == harnesstools.ToolAskQuestion {
		// The questions API is the GAOS one, and this dialect does not
		// carry it: the reference does not announce a call it cannot
		// run, and reports the failure on the step the announcement
		// would have used.
		verdict.Cancelled = true
		c.rememberVerdict(call.ID, verdict)
		return
	}

	update := newStep(pb.StepUpdate_SOURCE_MODEL, stepTarget(call.Name), pb.StepUpdate_STATE_ACTIVE)
	update.SetTextDelta(toolStepText(call.Name))
	update.SetText(toolStepText(call.Name))
	applyToolAction(update, call.Name, args, nil, nil, false)
	c.emitStep(index, update)
}

// emitDeniedCall reports a call the client refused.
//
// The reference writes one frame for it: the step fails, the failure
// targets the user, and the text says the call was denied by the pre-tool
// hook. The call itself is never announced.
//
//	state      STATE_ERROR
//	source     SOURCE_MODEL
//	target     TARGET_USER
//	errorMessage  the reason
//	text       the reason ("denied by pre-tool hook: the reason")
//	error      {errorMessage: the text, httpCode: 0}
func (c *conversation) emitDeniedCall(index uint32, verdict callVerdict) {
	c.emitStep(index, deniedStep(verdict.Reason))
}

// deniedStep builds the frame a refused call writes.
//
// The reference's frame says why the call did not run and quotes the
// step's own failure:
//
//	the reason ("denied by pre-tool hook: the reason")
func deniedStep(reason string) *pb.StepUpdate {
	text := harnesstools.DeniedFrame(reason)
	update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_USER, pb.StepUpdate_STATE_ERROR)
	update.SetErrorMessage(reason)
	update.SetTextDelta(text)
	update.SetText(text)
	action := &pb.ActionError{}
	action.SetErrorMessage(text)
	action.SetHttpCode(0)
	update.SetError(action)
	return update
}

// invalidStep builds the frame a call the reference refused writes.
//
// The frame says the model produced an invalid tool call and quotes the
// reason. It comes from the *system*, not from the model: the reference
// reports its own refusal, not the model's answer.
//
//	state      STATE_ERROR
//	source     SOURCE_SYSTEM
//	target     TARGET_USER
//	errorMessage  The model produced an invalid tool call.
//	text       The model produced an invalid tool call. ("the reason")
//	error      {errorMessage: the text, httpCode: 0}
//
// The frame carries no action: the call never ran, so there is nothing
// to report.
func invalidStep(message, reason string) *pb.StepUpdate {
	if message == "" {
		message = harnesstools.InvalidMessage
	}
	text := harnesstools.InvalidFrame(message, reason)
	update := newStep(pb.StepUpdate_SOURCE_SYSTEM, pb.StepUpdate_TARGET_USER, pb.StepUpdate_STATE_ERROR)
	update.SetErrorMessage(message)
	update.SetTextDelta(text)
	update.SetText(text)
	action := &pb.ActionError{}
	action.SetErrorMessage(text)
	action.SetHttpCode(0)
	update.SetError(action)
	return update
}

// emitToolStart reports a tool call that started running.
//
// The frame keeps the step index the announcement used: the client folds
// the announcement, the start, the result and the done into one step.
func (c *conversation) emitToolStart(e piagent.ToolExecutionStartEvent) {
	c.recordToolCall()
	if c.callCancelled(e.ToolCallID) {
		// A question the harness could not ask is reported when the call
		// ends: the reference writes one frame for it, and no start.
		return
	}
	if c.callDenied(e.ToolCallID) {
		// A denied call was reported when the denial was known: it
		// never started, so it has no frame of its own.
		return
	}
	if c.callInvalid(e.ToolCallID) {
		// A call the reference refused was reported when the refusal
		// was known: it never ran, so it has no frame of its own.
		return
	}
	c.rememberToolArgs(e.ToolName, e.Args)
	index := c.toolStep(e.ToolName)
	update := newStep(pb.StepUpdate_SOURCE_MODEL, stepTarget(e.ToolName), pb.StepUpdate_STATE_ACTIVE)
	update.SetText(toolStepText(e.ToolName))
	applyToolAction(update, e.ToolName, e.Args, nil, nil, false)
	c.emitStep(index, update)
}

// emitToolEnd reports a tool call's outcome.
//
// The reference writes two frames: one while the call is still active —
// the tool's output has arrived — and one that closes the step. Both keep
// the step index the call started with.
//
// Two of the reference's tools write one more active frame before the step
// closes; see extraToolFrame.
func (c *conversation) emitToolEnd(e piagent.ToolExecutionEndEvent) {
	if c.callCancelled(e.ToolCallID) {
		// The reference reports a question it could not ask as one
		// failed step: the questions were cancelled, and the frame says
		// so. The token count of the response that asked for it came
		// before this frame — the call never started, so there was no
		// start frame to wait for.
		c.emitStep(c.toolStep(e.ToolName), cancelledQuestionStep())
		c.forgetVerdict(e.ToolCallID)
		return
	}
	if c.callInvalid(e.ToolCallID) {
		// The reference reports the refusal in one frame, and the token
		// count of the response that made the call comes *before* it —
		// the call never started, so there was no start frame to wait
		// for.
		verdict, _ := c.verdictFor(e.ToolCallID)
		c.flushUsage()
		c.emitStep(c.toolStep(e.ToolName), invalidStep(verdict.InvalidMessage, verdict.Reason))
		c.forgetVerdict(e.ToolCallID)
		if c.invalidFailureReason() != "" {
			// The model used up its retries: the reference gives up,
			// and the run ends with the reason.
			c.halt()
		}
		return
	}
	if c.callDenied(e.ToolCallID) {
		// The denial was reported when it was known. The token count
		// of the response that asked for the call follows it.
		c.flushUsage()
		c.forgetVerdict(e.ToolCallID)
		return
	}
	index := c.toolStep(e.ToolName)
	args := c.toolArgsOf(e.ToolName)
	outcome := c.outcomeOf(e.ToolName)
	// A call that ran starts the refusal counter over — it worked or it
	// failed: the reference counts the refusals in a row, and a call that
	// was not refused breaks the row.
	c.resetInvalidRetries()

	result := newStep(pb.StepUpdate_SOURCE_MODEL, stepTarget(e.ToolName), pb.StepUpdate_STATE_ACTIVE)
	result.SetText(toolStepText(e.ToolName))
	applyToolAction(result, e.ToolName, args, nil, outcome, false)
	c.emitStep(index, result)

	if extraToolFrame(e.ToolName, outcome) {
		extra := newStep(pb.StepUpdate_SOURCE_MODEL, stepTarget(e.ToolName), pb.StepUpdate_STATE_ACTIVE)
		extra.SetText(toolStepText(e.ToolName))
		applyToolAction(extra, e.ToolName, args, nil, outcome, extraFrameCarriesDiff(e.ToolName))
		c.emitStep(index, extra)
	}

	done := newStep(pb.StepUpdate_SOURCE_MODEL, stepTarget(e.ToolName), pb.StepUpdate_STATE_DONE)
	if e.IsError {
		done.SetState(pb.StepUpdate_STATE_ERROR)
		done.SetErrorMessage(toolErrorMessage(outcome, e))
	}
	done.SetText(toolStepText(e.ToolName))
	applyToolAction(done, e.ToolName, args, &e, outcome, true)
	c.emitStep(index, done)
	c.forgetVerdict(e.ToolCallID)

	// A call that never started — a denied one — is reported here, and
	// the token count of the response that asked for it follows.
	c.flushUsage()
}

// callDenied reports whether the client refused a call.
//
// The verdict is remembered when the call was announced: the reference
// asks the client before it announces the call, so the answer is known
// before the call runs.
func (c *conversation) callDenied(callID string) bool {
	verdict, ok := c.verdictFor(callID)
	return ok && verdict.Blocked
}

// callCancelled reports whether the call is a question the harness could
// not ask.
//
// Such a call runs and fails: the reference's model sees the failed step,
// and the frame says the questions were cancelled.
func (c *conversation) callCancelled(callID string) bool {
	verdict, ok := c.verdictFor(callID)
	return ok && verdict.Cancelled
}

// callInvalid reports whether the model produced a call the reference
// refuses.
//
// The call never runs: the reference refuses it before it dispatches the
// tool, and it is reported in one frame when the refusal is known.
func (c *conversation) callInvalid(callID string) bool {
	verdict, ok := c.verdictFor(callID)
	return ok && verdict.Invalid
}

// cancelledQuestionStep builds the frame a question the harness could not
// ask writes.
//
//	state      STATE_ERROR
//	source     SOURCE_MODEL
//	target     TARGET_USER
//	errorMessage  Questions cancelled.
//	text       Questions cancelled.
//
// The frame carries no error action: the reference's does not either.
func cancelledQuestionStep() *pb.StepUpdate {
	update := newStep(pb.StepUpdate_SOURCE_MODEL, pb.StepUpdate_TARGET_USER, pb.StepUpdate_STATE_ERROR)
	update.SetErrorMessage(harnesstools.QuestionsCancelled)
	update.SetTextDelta(harnesstools.QuestionsCancelled)
	update.SetText(harnesstools.QuestionsCancelled)
	return update
}

// outcomeOf returns what a tool produced, when it ran.
func (c *conversation) outcomeOf(name string) *harnesstools.Outcome {
	outcome, ok := c.outcomes.get(name)
	if !ok {
		return nil
	}
	return &outcome
}

// extraToolFrame reports whether the reference writes one more active frame
// before the step closes.
//
// The reference writes one frame per internal event, and two of the tools
// produce one more event than the others:
//
//   - a command that printed something reports its output as an event of
//     its own. A command that printed nothing does not, and its step has
//     the same three active frames as every other tool's;
//   - an edit reports its diff as an event of its own, and the diff is on
//     that frame as well as on the frame that closes the step.
//
// The counts were read off the reference's own frames (一手 — cycle 307's
// differential capture): three active frames for a view, a listing, a
// search and a name search; four for a command that printed something and
// for an edit.
func extraToolFrame(name string, outcome *harnesstools.Outcome) bool {
	switch name {
	case "run_command":
		return outcome != nil && outcome.HasOutput
	case "replace_file_content", "multi_replace_file_content", "edit_file":
		return outcome != nil && len(outcome.Diff) > 0
	}
	return false
}

// extraFrameCarriesDiff reports whether the extra frame carries the edit's
// diff block.
//
// Only an edit's extra frame does: the reference reports the diff on the
// frame that announces the change and on the frame that closes the step. A
// created file gets the diff on the closing frame alone.
func extraFrameCarriesDiff(name string) bool {
	switch name {
	case "replace_file_content", "multi_replace_file_content", "edit_file":
		return true
	}
	return false
}

// applyToolAction sets the protocol's action on a step update.
//
// The mapping is by tool name: the reference's own names
// (`run_command`, `view_file`, `write_to_file`, …) map onto the matching
// `Action*` message, and a tool the client declared maps onto
// `ActionCustomTool`, which carries the wire `ToolCall` and `ToolResponse`
// verbatim.
//
// The path fields carry a `file://` URI, not a bare path — the reference
// writes `file:///path/to/file` and the client resolves it as a link.
//
// end is the call's outcome, and only the frame that closes the step
// carries it: the reference's done frame reports the range the tool
// actually read, while the announcement, the start and the result frames
// report the range the call asked for.
//
// outcome is what the tool produced, when the frame is written after the
// call ran. The reference's closing frame carries it — a command's exit
// code and output, an edit's diff, a listing's entries, a search's match
// count, a name search's names.
//
// diff says whether the frame carries an edit's diff block. The reference
// puts the diff on the frame that reports the edit and on the frame that
// closes the step; a created file gets it only on the closing one.
func applyToolAction(update *pb.StepUpdate, name string, args json.RawMessage, end *piagent.ToolExecutionEndEvent, outcome *harnesstools.Outcome, diff bool) {
	switch name {
	case "run_command":
		action := &pb.ActionRunCommand{}
		action.SetCommandLine(argString(args, "CommandLine", "command", "command_line"))
		action.SetWorkingDir(argString(args, "Cwd", "cwd", "working_dir"))
		if end != nil && outcome != nil && !outcome.Failed {
			action.SetExitCode(int32(outcome.ExitCode))
			// The reference reports the output even when the command
			// printed nothing: the field is there, empty.
			action.SetCombinedOutput(outcome.Output)
		}
		update.SetRunCommand(action)
	case "view_file", "read_file":
		action := &pb.ActionViewFile{}
		action.SetFilePath(fileURI(argString(args, "AbsolutePath", "path", "file_path")))
		first, last := viewFileWindow(args)
		action.SetStartLine(uint32(first))
		action.SetEndLine(uint32(last))
		if offset := argInt(args, "ContentOffset", "content_offset"); offset != 0 {
			action.SetContentOffset(int32(offset))
		}
		if end != nil {
			// The done frame carries the window the tool actually
			// showed, not the window the call asked for.
			if shown, shownLast, ok := shownLines(toolResultText(end.Result)); ok {
				action.SetStartLine(uint32(shown - 1))
				action.SetEndLine(uint32(shownLast - 1))
			}
		}
		update.SetViewFile(action)
	case "write_to_file", "write_file", "create_file":
		// The reference reports a written file as an *edit*, not as a
		// creation: the action is an `editFile` whose diff block is the
		// file's new lines.
		action := &pb.ActionEditFile{}
		action.SetFilePath(fileURI(argString(args, "TargetFile", "AbsolutePath", "path", "file_path")))
		if diff && outcome != nil {
			action.SetDiffBlock(diffBlocks(outcome.Diff))
		}
		update.SetEditFile(action)
	case "edit_file", "replace_file_content", "multi_replace_file_content", "sed_file":
		action := &pb.ActionEditFile{}
		action.SetFilePath(fileURI(argString(args, "TargetFile", "AbsolutePath", "path", "file_path")))
		if diff && outcome != nil {
			action.SetDiffBlock(diffBlocks(outcome.Diff))
		}
		update.SetEditFile(action)
	case "list_dir", "list_directory":
		action := &pb.ActionListDirectory{}
		action.SetDirectoryPath(fileURI(argString(args, "DirectoryPath", "path", "directory_path")))
		if end != nil && outcome != nil {
			action.SetResults(listResults(outcome.Entries))
		}
		update.SetListDirectory(action)
	case "grep_search", "search_directory":
		action := &pb.ActionSearchDirectory{}
		action.SetDirectoryPath(fileURI(argString(args, "SearchPath", "DirectoryPath", "path", "directory_path")))
		action.SetQuery(argString(args, "Query", "pattern", "query"))
		// The reference reports the match count on every frame, zero
		// included: the field is there before the search has run, and
		// only the frame that closes the step knows the count.
		matches := 0
		if end != nil && outcome != nil {
			matches = outcome.Matches
		}
		action.SetNumResults(int32(matches))
		update.SetSearchDirectory(action)
	case "find_by_name", "find_file":
		action := &pb.ActionFindFile{}
		action.SetDirectoryPath(fileURI(argString(args, "SearchDirectory", "DirectoryPath", "path", "directory_path")))
		action.SetQuery(argString(args, "Pattern", "pattern", "name", "query"))
		// The reference reports the names it found on the frame that
		// closes the step, and only there — a step that failed has no
		// names to report, and the field is left out.
		if end != nil && outcome != nil && !outcome.Failed {
			action.SetOutput(strings.Join(outcome.Found, "\n"))
		}
		update.SetFindFile(action)
	case "search_web", "web_search":
		action := &pb.ActionSearchWeb{}
		action.SetQuery(argString(args, "query", "pattern"))
		update.SetSearchWeb(action)
	case "generate_image":
		action := &pb.ActionGenerateImage{}
		action.SetPrompt(argString(args, "prompt", "description"))
		update.SetGenerateImage(action)
	case "call_mcp_tool":
		action := &pb.ActionMcpTool{}
		action.SetServerName(argString(args, "server_name", "serverName"))
		action.SetToolName(argString(args, "tool_name", "toolName"))
		action.SetArgumentsJson(argString(args, "arguments", "arguments_json"))
		update.SetMcpTool(action)
	case "invoke_subagent":
		update.SetInvokeSubagent(&pb.ActionInvokeSubagent{})
	case "finish":
		// The reference reports the `finish` call as its own action, and
		// the action carries the model's own arguments: the summary the
		// model wrote is the conversation's last word.
		action := &pb.ActionFinish{}
		action.SetOutputString(string(args))
		update.SetFinish(action)
	default:
		// A tool the client declared: the wire call and its answer
		// travel inside the action.
		action := &pb.ActionCustomTool{}
		call := &pb.ToolCall{}
		call.SetName(name)
		call.SetArgumentsJson(string(args))
		action.SetToolCall(call)
		if end != nil {
			response := &pb.ToolResponse{}
			response.SetId(name)
			response.SetResponseJson(toolResultText(end.Result))
			action.SetToolResponse(response)
		}
		update.SetCustomTool(action)
	}
}

// fileURI renders an absolute path as a `file://` URI.
//
// The reference writes the path fields as URIs, and the client renders
// them as links. A path that is already a URI is left alone.
func fileURI(path string) string {
	if path == "" {
		return ""
	}
	if strings.Contains(path, "://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		return path
	}
	return "file://" + path
}

// diffBlocks converts a computed diff into the protocol's diff blocks.
//
// The reference's edit action carries the change as a range of the old file
// and the lines that replace it, each marked with what happened to it.
func diffBlocks(blocks []harnesstools.DiffBlock) []*pb.ActionEditFile_DiffBlock {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]*pb.ActionEditFile_DiffBlock, 0, len(blocks))
	for _, block := range blocks {
		lines := make([]*pb.ActionEditFile_DiffLine, 0, len(block.Lines))
		for _, line := range block.Lines {
			action := pb.ActionEditFile_DiffLine_LINE_ACTION_NONE
			switch line.Action {
			case harnesstools.LineInsert:
				action = pb.ActionEditFile_DiffLine_LINE_ACTION_INSERT
			case harnesstools.LineDelete:
				action = pb.ActionEditFile_DiffLine_LINE_ACTION_DELETE
			}
			text := line.Text
			lines = append(lines, pb.ActionEditFile_DiffLine_builder{
				Text:   &text,
				Action: &action,
			}.Build())
		}
		start := int32(block.StartLine)
		end := int32(block.EndLine)
		out = append(out, pb.ActionEditFile_DiffBlock_builder{
			StartLine: &start,
			EndLine:   &end,
			Lines:     lines,
		}.Build())
	}
	return out
}

// listResults converts a directory listing into the protocol's entries.
//
// The reference's listing carries each entry's name and its size — the
// size as a number, which the protocol's JSON writes as a string.
func listResults(entries []harnesstools.Entry) []*pb.ActionListDirectory_Result {
	if len(entries) == 0 {
		return nil
	}
	out := make([]*pb.ActionListDirectory_Result, 0, len(entries))
	for _, entry := range entries {
		size, err := strconv.ParseUint(entry.Size, 10, 64)
		if err != nil {
			size = 0
		}
		name := entry.Name
		result := pb.ActionListDirectory_Result_builder{Name: &name}.Build()
		if entry.IsDirectory {
			isDir := true
			result = pb.ActionListDirectory_Result_builder{
				IsDirectory: &isDir,
				Name:        &name,
			}.Build()
		} else {
			result.SetFileSize(size)
		}
		out = append(out, result)
	}
	return out
}

// viewFileMaxLines is the number of lines a view_file call may ask for.
//
// The tool's own description says "You can view at most 800 lines at a
// time", and the reference's captured call for a file with no line
// arguments asks for exactly that window.
const viewFileMaxLines = 800

// viewFileWindow is the line window a view_file call asks for, as the
// action reports it.
//
// The reference reports the window as 0-based line indices: a call with
// neither `StartLine` nor `EndLine` asks for lines 1..800 (1-indexed, as
// the tool's description says), which is 0..799 — and that is what the
// captured frames carry.
func viewFileWindow(args json.RawMessage) (first, last int) {
	first, last = 0, viewFileMaxLines-1
	if value, ok := argIntOK(args, "StartLine", "start_line"); ok && value > 0 {
		first = value - 1
	}
	if value, ok := argIntOK(args, "EndLine", "end_line"); ok && value > 0 {
		last = value - 1
	}
	if last < first {
		last = first
	}
	return first, last
}

// shownLines reads the line window a tool reported in its result.
//
// The clone's view_file result carries "Showing lines A to B" with 1-based
// indices; the action reports the same window as 0-based indices, so the
// done frame's range is the one the tool actually showed.
func shownLines(text string) (first, last int, ok bool) {
	const marker = "Showing lines "
	at := strings.Index(text, marker)
	if at < 0 {
		return 0, 0, false
	}
	var a, b int
	if _, err := fmt.Sscanf(text[at+len(marker):], "%d to %d", &a, &b); err != nil {
		return 0, 0, false
	}
	return a, b, true
}

// argString reads the first string-valued argument among keys.
func argString(args json.RawMessage, keys ...string) string {
	if len(args) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return ""
	}
	for _, key := range keys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return ""
}

// argInt reads the first integer-valued argument among keys.
func argInt(args json.RawMessage, keys ...string) int {
	value, _ := argIntOK(args, keys...)
	return value
}

// argIntOK reads the first integer-valued argument among keys and reports
// whether one was there.
//
// The difference matters for the optional arguments: an absent `EndLine`
// and an `EndLine` of zero are not the same request.
func argIntOK(args json.RawMessage, keys ...string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return 0, false
	}
	for _, key := range keys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, true
		}
	}
	return 0, false
}

// messageText renders an assistant message's text blocks.
func messageText(msg piagent.AgentMessage) string {
	if msg.Assistant == nil {
		return ""
	}
	var b strings.Builder
	for _, block := range msg.Assistant.Content {
		if text, ok := block.(pi.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// messageToolCalls returns the tool calls an assistant message carries.
func messageToolCalls(msg piagent.AgentMessage) []pi.ToolCall {
	if msg.Assistant == nil {
		return nil
	}
	var out []pi.ToolCall
	for _, block := range msg.Assistant.Content {
		if call, ok := block.(pi.ToolCall); ok {
			out = append(out, call)
		}
	}
	return out
}

// messageUsage returns an assistant message's token usage.
func messageUsage(msg piagent.AgentMessage) *pi.Usage {
	if msg.Assistant == nil {
		return nil
	}
	return msg.Assistant.Usage
}

// lastAssistantText returns the text of the last assistant message of a
// turn.
func lastAssistantText(msgs []piagent.AgentMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if text := messageText(msgs[i]); text != "" {
			return text
		}
	}
	return ""
}

// toolResultText renders a tool result as text.
func toolResultText(result piagent.AgentToolResult) string {
	var b strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(pi.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// toolErrorMessage is the sentence a failed call's frame carries.
//
// The reference puts the *tool's own* sentence there — "Cannot view file
// file://… which does not exist." — and not the whole result text. A
// failure the reference's wording is not known for keeps the error
// itself.
func toolErrorMessage(outcome *harnesstools.Outcome, e piagent.ToolExecutionEndEvent) string {
	if outcome != nil && outcome.ErrorMessage != "" {
		return outcome.ErrorMessage
	}
	return toolResultText(e.Result)
}
