// Package remotecontrol — ls_queue.go: the per-conversation delivery
// queue, and the step the Web UI renders on the queued card.
//
// The reference runs one turn per conversation. A message that arrives
// while a turn is in flight does not start a second one: the server
// holds it, and the delivery strategy says what happens next.
//
// 【一手】 2026-10-26, from the reference's own Web UI bundle
// (internal/rcfrontend/assets/main.js): the server's pending messages
// are the messages it is holding, and the frontend renders them from
// the state frame's `pendingAgentMessages`:
//
//	function u6b(a,b){var c=[];for(let e of a)if((e.deliveryStrategy===b||e.deliveryStrategy===V.MessageDeliveryStrategy.UNSPECIFIED||e.deliveryStrategy===void 0)&&e.payload.case==="stepPayload")try{let f=Kc(ke.StepSchema,e.payload.value);f.step.case==="userInput"&&c.push({msgId:e.id,step:f.step.value,metadata:f.metadata,deliveryStrategy:e.deliveryStrategy})}catch{}return c}
//
// So a pending message carries its step as *bytes* — the frontend
// decodes it with its own Step schema — and the strategy the request
// carried.
//
// The clone's bug this queue fixes: the strategy was not read at all,
// and a second message started a *second concurrent turn* on the same
// cascade. The second turn's StartCancel fired the first turn's cancel
// func, so the first turn died with "context canceled" and the Web UI
// toasted "Failed to send".
package remotecontrol

import (
	"context"
	"fmt"
	"time"
)

// lsQueuedMessage is one message the conversation has accepted but has
// not run yet.
type lsQueuedMessage struct {
	// ID identifies the message in the server's pending list.
	ID string
	// Text is the message the user sent.
	Text string
	// Images are the pictures the user attached to the message. They
	// ride with it: a message that waits in the queue waits with its
	// pictures.
	Images []MessageImage
	// Strategy is the delivery strategy the request carried.
	Strategy lsDeliveryStrategy
	// Step is the serialised gemini_coder.Step the Web UI decodes off
	// the pending message (see lsEncodeUserInputStep).
	Step []byte
}

// Admit decides what happens to a message that arrives for the
// conversation.
//
// It returns true when the caller owns the turn and has to run the
// message now, and false when the message waits in the queue: a
// conversation runs one turn at a time, so a message that arrives
// while a turn is in flight waits for it.
//
// 【反编译】 2026-10-26, the reference's own executor — the rule, in
// agentexecutor.(*AgentExecutor).scheduleUserCascadeMessageRequest
// (0x101e35620, the IDE language server):
//
//	queue = (executorLoopStatus == RUNNING) || (deliveryStrategy == WHEN_IDLE && !fullyIdle)
//
// While the executor loop is RUNNING *every* message waits — the
// delivery strategy does not change that. The reference's setting copy
// ("Interrupt the agent and send immediately.") promises an interrupt
// the reference does not deliver, and the reference's own frontend
// sides with the executor: its pending predicate answers "this message
// is pending" for a NEXT_INVOCATION message exactly while the executor
// loop is RUNNING.
//
// The queue is FIFO, like the reference's: the reference's changelog
// says a message sent while earlier ones are still queued "goes to the
// agent together with them, in order".
func (t *lsTrajectory) Admit(msg lsQueuedMessage) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		// The conversation is idle: the caller runs the message.
		t.running = true
		t.interrupted = false
		t.mutateLocked()
		return true
	}
	t.queue = append(t.queue, msg)
	t.mutateLocked()
	return false
}

// Interrupt cancels the turn in flight and marks it interrupted.
//
// The interrupted turn's error is not a failure: the message that
// interrupted it is why the turn ended, and the Web UI shows the turn
// as interrupted rather than as an error.
//
// The delivery strategy does *not* use this: the reference queues every
// message while its executor loop is running (§2.7 of the cycle-322
// report). The only caller is the "Send all (N)" button — the
// reference's own `SendAllQueuedMessages`, which the reference's
// language server answers with "deprecated" and the clone implements.
//
// It reports whether a turn was actually interrupted: a message that
// arrives when the conversation is already idle interrupts nothing.
func (t *lsTrajectory) Interrupt() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	cancel := t.turnCancel
	if cancel != nil {
		t.interrupted = true
	}
	t.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// BeginTurn installs the cancel func of the turn that is about to run,
// so a message that interrupts it can stop it.
func (t *lsTrajectory) BeginTurn(cancel context.CancelFunc) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.turnCancel = cancel
	t.mu.Unlock()
}

// FinishTurn ends the turn that just ran.
//
// It reports whether the turn was interrupted — so the caller knows
// the turn's error is not a failure — and hands back the next queued
// message, if any. The conversation goes idle only when the queue is
// empty: a message that is waiting keeps the conversation busy, which
// is what the Web UI's "fully idle" flag reports.
func (t *lsTrajectory) FinishTurn() (interrupted bool, next lsQueuedMessage, hasNext bool) {
	if t == nil {
		return false, lsQueuedMessage{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turnCancel = nil
	interrupted = t.interrupted
	t.interrupted = false
	if len(t.queue) == 0 {
		t.running = false
		t.mutateLocked()
		return interrupted, lsQueuedMessage{}, false
	}
	next = t.queue[0]
	t.queue = t.queue[1:]
	t.mutateLocked()
	return interrupted, next, true
}

// Pending returns the messages the conversation is holding, oldest
// first.
func (t *lsTrajectory) Pending() []lsQueuedMessage {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pendingLocked()
}

// DropPending removes the queued message at the given index.
//
// The index is the position in the queue — the same position the
// pendingAgentMessages update's indices carry, so the card the Web UI
// renders and the message the user deleted are the same one. It
// reports whether a message was dropped.
func (t *lsTrajectory) DropPending(index int) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if index < 0 || index >= len(t.queue) {
		return false
	}
	t.queue = append(t.queue[:index], t.queue[index+1:]...)
	t.mutateLocked()
	return true
}

// DropPendingByID removes the queued message with the given id.
//
// The id is the one the pendingAgentMessages update carries, and the
// one the Web UI hands back when the user deletes a queued card
// (the `messageId` of its DeleteAgentMessage request). The index is
// not stable across a queue that grew or shrank, the id is.
//
// It reports whether a message was dropped.
func (t *lsTrajectory) DropPendingByID(id string) bool {
	if t == nil || id == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, msg := range t.queue {
		if msg.ID != id {
			continue
		}
		t.queue = append(t.queue[:i], t.queue[i+1:]...)
		t.mutateLocked()
		return true
	}
	return false
}

// pendingLocked copies the queue. The caller must hold t.mu.
func (t *lsTrajectory) pendingLocked() []lsQueuedMessage {
	if len(t.queue) == 0 {
		return nil
	}
	out := make([]lsQueuedMessage, len(t.queue))
	copy(out, t.queue)
	return out
}

// nextPendingID mints the id of a pending message. The frontend keys
// its queued card by the id and hands it back when the user deletes
// the message, so it has to be stable for the life of the message.
func (t *lsTrajectory) nextPendingID() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pendingSeq++
	return fmt.Sprintf("%s-q%d", t.conversationID, t.pendingSeq)
}

// lsEncodeUserInputStep serialises the gemini_coder.Step the Web UI
// decodes off a pending agent message.
//
// The pending message's payload is the step's *bytes*, not its
// proto-JSON form: the frontend decodes it with its own Step schema
// (`Kc(ke.StepSchema, e.payload.value)`), and proto-JSON spells a
// bytes field as base64.
//
// 【一手】 2026-10-26, from the Web UI's own bundle — the shape the
// frontend builds for its optimistic copy, which the server's step has
// to match:
//
//	q(ke.StepSchema,{type:V.CortexStepType.USER_INPUT,
//	                metadata:q(V.CortexStepMetadataSchema,{sourceTrajectoryStepInfo:…,createdAt:…}),
//	                step:{case:"userInput",value:q(V.CortexStepUserInputSchema,{items:…,media:…})}})
//
// The clone's step carries the fields the *server* side of the
// reference sets as well: the status (QUEUED — the message is waiting),
// the metadata source (USER_EXPLICIT) and the
// `is_queued_message` flag the reference's CortexStepUserInput has for
// exactly this case.
//
// 【反编译】 the field numbers, from the FileDescriptorProto set
// embedded in the same bundle:
//
//	gemini_coder.Step: type=1, status=4, metadata=5, user_input=19
//	exa.cortex_pb.CortexStepUserInput: items=3, is_queued_message=6
//	exa.codeium_common_pb.TextOrScopeItem: text=1
//	exa.cortex_pb.CortexStepMetadata: created_at=1, source=3
//	exa.cortex_pb.CortexStepType.USER_INPUT = 14
//	exa.cortex_pb.CortexStepStatus.QUEUED = 11
//	exa.cortex_pb.CortexStepSource.USER_EXPLICIT = 4
func lsEncodeUserInputStep(text string, queued bool, createdAt time.Time) []byte {
	item := lsProtoBytes(nil, 1, []byte(text))
	userInput := lsProtoBytes(nil, 3, item)
	if queued {
		userInput = lsProtoVarint(userInput, 6, 1)
	}
	metadata := lsProtoBytes(nil, 1, lsProtoTimestamp(createdAt))
	metadata = lsProtoVarint(metadata, 3, lsStepSourceUserExplicit)
	step := lsProtoVarint(nil, 1, lsStepTypeUserInput)
	step = lsProtoVarint(step, 4, lsStepStatusQueued)
	step = lsProtoBytes(step, 5, metadata)
	step = lsProtoBytes(step, 19, userInput)
	return step
}

// lsProtoTimestamp encodes a google.protobuf.Timestamp: seconds=1,
// nanos=2.
func lsProtoTimestamp(t time.Time) []byte {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	out := lsProtoVarint(nil, 1, uint64(utc.Unix()))
	if nanos := uint64(utc.Nanosecond()); nanos != 0 {
		out = lsProtoVarint(out, 2, nanos)
	}
	return out
}

// lsProtoVarint appends a varint field.
func lsProtoVarint(b []byte, field int, value uint64) []byte {
	b = lsAppendVarint(b, uint64(field)<<3)
	return lsAppendVarint(b, value)
}

// lsProtoBytes appends a length-delimited field.
func lsProtoBytes(b []byte, field int, value []byte) []byte {
	b = lsAppendVarint(b, uint64(field)<<3|2)
	b = lsAppendVarint(b, uint64(len(value)))
	return append(b, value...)
}

// lsAppendVarint appends one base-128 varint.
func lsAppendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}
