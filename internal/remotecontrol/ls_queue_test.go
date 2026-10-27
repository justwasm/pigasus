package remotecontrol

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/justwasm/pigasus/lsproto"
)

// lsGateBackend is a MessageBackend whose turn parks until the test
// releases it. It is how the queue's tests hold a turn in flight
// while a second message arrives.
//
// Every turn records the prompt it was given, so a test can assert
// both how many turns ran and in which order.
type lsGateBackend struct {
	mu       sync.Mutex
	prompts  []string
	canceled int
	entered  chan string
	gate     chan struct{}
}

func newLSGateBackend() *lsGateBackend {
	return &lsGateBackend{
		entered: make(chan string, 8),
		gate:    make(chan struct{}),
	}
}

// Send parks until the gate opens or the turn's context dies.
func (b *lsGateBackend) Send(ctx context.Context, prompt MessagePrompt) (MessageResult, error) {
	b.mu.Lock()
	b.prompts = append(b.prompts, prompt.Text)
	b.mu.Unlock()
	select {
	case b.entered <- prompt.Text:
	default:
	}
	select {
	case <-b.gate:
		return MessageResult{Reply: "ok: " + prompt.Text}, nil
	case <-ctx.Done():
		b.mu.Lock()
		b.canceled++
		b.mu.Unlock()
		return MessageResult{}, ctx.Err()
	}
}

// release opens the gate: every turn that is waiting returns.
func (b *lsGateBackend) release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.gate:
	default:
		close(b.gate)
	}
}

// seen returns the prompts the turns were given, in order.
func (b *lsGateBackend) seen() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.prompts...)
}

// cancelCount returns how many turns ended because their context was
// cancelled.
func (b *lsGateBackend) cancelCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.canceled
}

// newLSQueueDispatcher builds a dispatcher whose cascades run on the
// given backend.
func newLSQueueDispatcher(t *testing.T, backend MessageBackend) *Dispatcher {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess, MessageBackend: backend})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// lsSendMessage sends a message into a conversation and returns the
// RPC's error (nil on success).
func lsSendMessage(t *testing.T, d *Dispatcher, cascadeID, text string, strategy int) error {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"cascadeId":        cascadeID,
		"items":            []any{map[string]any{"text": text}},
		"deliveryStrategy": strategy,
	})
	if err != nil {
		t.Errorf("marshal: %v", err)
		return err
	}
	_, err = d.DispatchDirect(context.Background(), "SendUserCascadeMessage", string(payload))
	return err
}

// lsStartConversation registers a conversation the way the Web UI
// does.
func lsStartConversation(t *testing.T, d *Dispatcher, cascadeID string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"cascadeId": cascadeID})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := d.DispatchDirect(context.Background(), "StartCascade", string(payload)); err != nil {
		t.Fatalf("StartCascade: %v", err)
	}
}

// lsWaitForPrompt waits until a turn has started with the given text.
func lsWaitForPrompt(t *testing.T, b *lsGateBackend, text string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-b.entered:
			if got == text {
				return
			}
		case <-deadline:
			t.Fatalf("no turn started with %q; the turns were %v", text, b.seen())
		}
	}
}

// TestAMessageThatArrivesDuringATurnWaitsForIt pins the queue.
//
// 【一手】 2026-10-26: a message sent while the agent was working
// started a *second* turn on the same cascade. The second turn's
// StartCancel fired the first turn's cancel func, so the first turn
// died with "context canceled" and the Web UI toasted "Failed to
// send".
//
// The reference's contract is the delivery strategy: the message waits
// for the turn in flight, and it runs after it.
func TestAMessageThatArrivesDuringATurnWaitsForIt(t *testing.T) {
	backend := newLSGateBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-queue")

	first := make(chan error, 1)
	go func() { first <- lsSendMessage(t, d, "conv-queue", "first", int(lsDeliveryWhenIdle)) }()
	lsWaitForPrompt(t, backend, "first")

	// The second message finds a turn in flight: it waits.
	if err := lsSendMessage(t, d, "conv-queue", "second", int(lsDeliveryWhenIdle)); err != nil {
		t.Fatalf("the queued message reported an error: %v", err)
	}
	if got := backend.seen(); len(got) != 1 {
		t.Fatalf("the queued message started a turn of its own: %v", got)
	}
	traj := d.trajectories.Get("conv-queue")
	pending := traj.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending messages = %d, want 1", len(pending))
	}
	if pending[0].Text != "second" {
		t.Errorf("the pending message is %q, want %q", pending[0].Text, "second")
	}

	// The turn in flight ends: the queue hands its message over.
	backend.release()
	select {
	case err := <-first:
		if err != nil {
			t.Fatalf("the first turn reported %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first turn never ended")
	}
	lsWaitForPrompt(t, backend, "second")

	got := backend.seen()
	if len(got) != 2 {
		t.Fatalf("the turns were %v, want two", got)
	}
	if got[0] != "first" || got[1] != "second" {
		t.Errorf("the turns were %v, want [first second]", got)
	}
	if n := backend.cancelCount(); n != 0 {
		t.Errorf("%d turns were cancelled, want none: a queued message does not interrupt the turn in flight", n)
	}
	if pending := traj.Pending(); len(pending) != 0 {
		t.Errorf("the queue still holds %d messages, want none", len(pending))
	}
}

// TestNextInvocationWaitsForTheTurnLikeWhenIdle pins the other
// strategy.
//
// 【反编译】 2026-10-26, the reference's own executor — the rule, in
// agentexecutor.(*AgentExecutor).scheduleUserCascadeMessageRequest
// (0x101e35620, the IDE language server):
//
//	queue = (executorLoopStatus == RUNNING) || (deliveryStrategy == WHEN_IDLE && !fullyIdle)
//
// While the executor loop is RUNNING *every* message waits, so a
// NEXT_INVOCATION message waits like a WHEN_IDLE one: it does not
// interrupt the turn in flight, and it goes to the back of the queue.
//
// 【一手】 the same, live: against the reference's own language server,
// a NEXT_INVOCATION message sent during a running turn was queued and
// delivered when the turn ended — no interrupt, exactly like
// WHEN_IDLE.
func TestNextInvocationWaitsForTheTurnLikeWhenIdle(t *testing.T) {
	backend := newLSGateBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-next")

	first := make(chan error, 1)
	go func() { first <- lsSendMessage(t, d, "conv-next", "first", int(lsDeliveryWhenIdle)) }()
	lsWaitForPrompt(t, backend, "first")

	// A message that waits: it goes to the back of the queue.
	if err := lsSendMessage(t, d, "conv-next", "waiting", int(lsDeliveryWhenIdle)); err != nil {
		t.Fatalf("the queued message reported an error: %v", err)
	}
	// A message that asks for the next invocation: it waits too, and
	// it waits *behind* the message that was already queued.
	if err := lsSendMessage(t, d, "conv-next", "next", int(lsDeliveryNextInvocation)); err != nil {
		t.Fatalf("the queued message reported an error: %v", err)
	}
	if got := backend.seen(); len(got) != 1 {
		t.Fatalf("the queued message started a turn of its own: %v", got)
	}
	traj := d.trajectories.Get("conv-next")
	pending := traj.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending messages = %d, want 2", len(pending))
	}
	if pending[0].Text != "waiting" || pending[1].Text != "next" {
		t.Errorf("the queue holds %q then %q, want [waiting next]", pending[0].Text, pending[1].Text)
	}

	backend.release()
	select {
	case err := <-first:
		if err != nil {
			t.Fatalf("the turn reported %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never ended")
	}
	lsWaitForPrompt(t, backend, "waiting")
	lsWaitForPrompt(t, backend, "next")

	got := backend.seen()
	if len(got) != 3 {
		t.Fatalf("the turns were %v, want three", got)
	}
	if got[0] != "first" || got[1] != "waiting" || got[2] != "next" {
		t.Errorf("the turns were %v, want [first waiting next]", got)
	}
	if n := backend.cancelCount(); n != 0 {
		t.Errorf("%d turns were cancelled, want none: a message never interrupts the turn in flight", n)
	}
}

// TestTheStateFrameCarriesTheQueuedMessages pins what the Web UI
// renders the queued card from.
//
// 【一手】 2026-10-26, from the reference's own Web UI bundle: the
// pending messages arrive in the state frame's
// `pendingAgentMessagesUpdate`, each one carrying its step as bytes
// and the strategy the request asked for.
func TestTheStateFrameCarriesTheQueuedMessages(t *testing.T) {
	backend := newLSGateBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-frame")

	first := make(chan error, 1)
	go func() { first <- lsSendMessage(t, d, "conv-frame", "first", int(lsDeliveryWhenIdle)) }()
	lsWaitForPrompt(t, backend, "first")
	if err := lsSendMessage(t, d, "conv-frame", "waiting", int(lsDeliveryWhenIdle)); err != nil {
		t.Fatalf("the queued message reported an error: %v", err)
	}

	traj := d.trajectories.Get("conv-frame")
	frame := lsAgentStateFrame(traj.Snapshot())
	update, _ := frame["update"].(map[string]any)
	pending, _ := update["pendingAgentMessagesUpdate"].(map[string]any)
	messages, _ := pending["pendingAgentMessages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("the frame carries %d pending messages, want 1: %v", len(messages), pending)
	}
	msg, _ := messages[0].(map[string]any)
	if msg["id"] == "" {
		t.Errorf("the pending message has no id: %v", msg)
	}
	if got := msg["deliveryStrategy"]; got != int(lsDeliveryWhenIdle) {
		t.Errorf("deliveryStrategy = %v, want %d", got, lsDeliveryWhenIdle)
	}
	// proto-JSON spells a oneof by the member's own name: the payload
	// is the step's bytes, base64-encoded.
	value, _ := msg["stepPayload"].(string)
	if value == "" {
		t.Errorf("the pending message carries no step: %v", msg)
	}
	// The conversation is not idle while a message waits.
	if update["fullyIdle"] != false {
		t.Errorf("fullyIdle = %v, want false while a message waits", update["fullyIdle"])
	}

	backend.release()
	<-first
}

// TestTheDeliveryStrategyReadsEveryForm pins the wire forms of the
// strategy: proto-JSON spells an enum by its name, and the frontend
// sends the number.
func TestTheDeliveryStrategyReadsEveryForm(t *testing.T) {
	cases := []struct {
		raw  string
		want lsDeliveryStrategy
	}{
		{`2`, lsDeliveryWhenIdle},
		{`"2"`, lsDeliveryWhenIdle},
		{`"MESSAGE_DELIVERY_STRATEGY_WHEN_IDLE"`, lsDeliveryWhenIdle},
		{`"WHEN_IDLE"`, lsDeliveryWhenIdle},
		{`1`, lsDeliveryNextInvocation},
		{`"MESSAGE_DELIVERY_STRATEGY_NEXT_INVOCATION"`, lsDeliveryNextInvocation},
		{`0`, lsDeliveryUnspecified},
		{`null`, lsDeliveryUnspecified},
		{`"MESSAGE_DELIVERY_STRATEGY_UNSPECIFIED"`, lsDeliveryUnspecified},
	}
	for _, tc := range cases {
		var got lsDeliveryStrategy
		if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
			t.Errorf("%s: %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s decoded to %d, want %d", tc.raw, got, tc.want)
		}
	}
}

// TestTheQueuedStepDecodesAsTheReferenceBuildsIt pins the bytes the
// Web UI decodes off a pending message: the frontend decodes them
// with its own Step schema, so the step has to be a real
// gemini_coder.Step.
func TestTheQueuedStepDecodesAsTheReferenceBuildsIt(t *testing.T) {
	step := lsEncodeUserInputStep("hello there", true, time.Unix(1700000000, 0))
	fields := lsDecodeProtoFields(step)

	if got := lsDecodeProtoVarint(fields, 1); got != lsStepTypeUserInput {
		t.Errorf("the step's type is %d, want %d", got, lsStepTypeUserInput)
	}
	if got := lsDecodeProtoVarint(fields, 4); got != lsStepStatusQueued {
		t.Errorf("the step's status is %d, want %d", got, lsStepStatusQueued)
	}
	metadata := lsDecodeProtoBytes(fields, 5)
	if len(metadata) == 0 {
		t.Fatalf("the step carries no metadata")
	}
	if got := lsDecodeProtoVarint(lsDecodeProtoFields(metadata), 3); got != lsStepSourceUserExplicit {
		t.Errorf("the metadata's source is %d, want %d", got, lsStepSourceUserExplicit)
	}
	userInput := lsDecodeProtoBytes(fields, 19)
	if len(userInput) == 0 {
		t.Fatalf("the step carries no user input")
	}
	if got := lsDecodeProtoVarint(lsDecodeProtoFields(userInput), 6); got != 1 {
		t.Errorf("the user input's is_queued_message is %d, want 1", got)
	}
	item := lsDecodeProtoBytes(lsDecodeProtoFields(userInput), 3)
	if got := string(lsDecodeProtoBytes(lsDecodeProtoFields(item), 1)); got != "hello there" {
		t.Errorf("the user input's text is %q, want %q", got, "hello there")
	}
}

// lsDecodeProtoFields splits a proto message into its fields: the
// field number and the raw value.
func lsDecodeProtoFields(buf []byte) map[int][]byte {
	out := map[int][]byte{}
	i := 0
	for i < len(buf) {
		key, n := lsDecodeVarint(buf[i:])
		i += n
		field := int(key >> 3)
		switch key & 7 {
		case 0:
			start := i
			_, n := lsDecodeVarint(buf[i:])
			i += n
			out[field] = buf[start:i]
		case 2:
			length, n := lsDecodeVarint(buf[i:])
			i += n
			out[field] = buf[i : i+int(length)]
			i += int(length)
		case 5:
			i += 4
		case 1:
			i += 8
		default:
			return out
		}
	}
	return out
}

// lsDecodeProtoVarint reads one varint field.
func lsDecodeProtoVarint(fields map[int][]byte, field int) uint64 {
	raw, ok := fields[field]
	if !ok {
		return 0
	}
	value, _ := lsDecodeVarint(raw)
	return value
}

// lsDecodeProtoBytes reads one length-delimited field.
func lsDecodeProtoBytes(fields map[int][]byte, field int) []byte {
	return fields[field]
}

// lsDecodeVarint reads one base-128 varint.
func lsDecodeVarint(buf []byte) (uint64, int) {
	var value uint64
	var shift uint
	for i := 0; i < len(buf); i++ {
		b := buf[i]
		value |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return value, i + 1
		}
		shift += 7
	}
	return value, len(buf)
}

// TestThePendingMessagesSurviveTheProtobufBridge pins the wire shape
// of the queued messages against the reference's own schema.
//
// The Connect bridge re-encodes the state frame with protojson before
// it reaches the browser: a field the schema does not know is dropped,
// so a payload spelled in any other way than proto-JSON spells it
// simply disappears — and the Web UI renders no queued card at all.
//
// 【一手】 the schema is the reference's own: the descriptor set the
// frontend bundle embeds (lsproto/descriptor/language_server.protoset).
func TestThePendingMessagesSurviveTheProtobufBridge(t *testing.T) {
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	md, ok := codec.OutputMessage("StreamAgentStateUpdates")
	if !ok {
		t.Fatal("the schema does not know StreamAgentStateUpdates")
	}

	msg := lsQueuedMessage{ID: "conv-q1", Text: "hello there", Strategy: lsDeliveryWhenIdle}
	msg.Step = lsEncodeUserInputStep(msg.Text, true, time.Unix(1700000000, 0))
	snap := lsSnapshot{
		ConversationID: "conv",
		TrajectoryID:   "traj",
		Pending:        []lsQueuedMessage{msg},
	}
	raw, err := json.Marshal(lsAgentStateFrame(snap))
	if err != nil {
		t.Fatalf("marshal the frame: %v", err)
	}
	body, err := codec.EncodeResponse("StreamAgentStateUpdates", raw)
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	decoded := dynamicpb.NewMessage(md)
	if err := proto.Unmarshal(body, decoded); err != nil {
		t.Fatalf("unmarshal the frame: %v", err)
	}

	update := decoded.Get(fdByName(md, "update")).Message()
	if !update.IsValid() {
		t.Fatal("the frame carries no update")
	}
	updateMD := update.Descriptor()
	pendingUpdate := update.Get(fdByName(updateMD, "pending_agent_messages_update")).Message()
	if !pendingUpdate.IsValid() {
		t.Fatal("the frame carries no pending messages update")
	}
	messages := pendingUpdate.Get(fdByName(pendingUpdate.Descriptor(), "pending_agent_messages")).List()
	if messages.Len() != 1 {
		t.Fatalf("the frame carries %d pending messages, want 1", messages.Len())
	}
	one := messages.Get(0).Message()
	oneMD := one.Descriptor()
	if got := one.Get(fdByName(oneMD, "id")).String(); got != "conv-q1" {
		t.Errorf("the pending message's id is %q, want %q", got, "conv-q1")
	}
	if got := one.Get(fdByName(oneMD, "delivery_strategy")).Enum(); got != protoreflect.EnumNumber(lsDeliveryWhenIdle) {
		t.Errorf("the pending message's strategy is %d, want %d", got, lsDeliveryWhenIdle)
	}
	payload := one.Get(fdByName(oneMD, "step_payload")).Bytes()
	if len(payload) == 0 {
		t.Fatal("the pending message carries no step")
	}
	if !strings.Contains(string(payload), "hello there") {
		t.Errorf("the step does not carry the message text: %q", payload)
	}
}

// fdByName finds a field of a message by its protobuf name.
func fdByName(md protoreflect.MessageDescriptor, name string) protoreflect.FieldDescriptor {
	fd := md.Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		fd = md.Fields().ByJSONName(name)
	}
	return fd
}

// TestTheSendAllButtonReleasesTheQueue pins the RPC the Web UI's
// "Send all (N)" button calls.
//
// 【一手】 the button is the reference's own SendAllQueuedMessages
// method: the app's bundle calls it with the cascade id and the
// cascade config.
func TestTheSendAllButtonReleasesTheQueue(t *testing.T) {
	backend := newLSGateBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-sendall")

	first := make(chan error, 1)
	go func() { first <- lsSendMessage(t, d, "conv-sendall", "first", int(lsDeliveryWhenIdle)) }()
	lsWaitForPrompt(t, backend, "first")
	if err := lsSendMessage(t, d, "conv-sendall", "waiting", int(lsDeliveryWhenIdle)); err != nil {
		t.Fatalf("the queued message reported an error: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"cascadeId": "conv-sendall"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := d.DispatchDirect(context.Background(), "SendAllQueuedMessages", string(payload)); err != nil {
		t.Fatalf("SendAllQueuedMessages: %v", err)
	}

	backend.release()
	select {
	case err := <-first:
		if err != nil {
			t.Fatalf("the interrupted turn reported %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the interrupted turn never ended")
	}
	lsWaitForPrompt(t, backend, "waiting")
	if got := backend.seen(); len(got) != 2 || got[1] != "waiting" {
		t.Errorf("the turns were %v, want [first waiting]", got)
	}
}

// TestTheQueuedMessageCanBeDropped pins the RPC the Web UI's card
// calls when the user throws a queued message away.
func TestTheQueuedMessageCanBeDropped(t *testing.T) {
	backend := newLSGateBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-delete")

	first := make(chan error, 1)
	go func() { first <- lsSendMessage(t, d, "conv-delete", "first", int(lsDeliveryWhenIdle)) }()
	lsWaitForPrompt(t, backend, "first")
	if err := lsSendMessage(t, d, "conv-delete", "second", int(lsDeliveryWhenIdle)); err != nil {
		t.Fatalf("the queued message reported an error: %v", err)
	}
	if err := lsSendMessage(t, d, "conv-delete", "third", int(lsDeliveryWhenIdle)); err != nil {
		t.Fatalf("the queued message reported an error: %v", err)
	}

	traj := d.trajectories.Get("conv-delete")
	if pending := traj.Pending(); len(pending) != 2 {
		t.Fatalf("pending messages = %d, want 2", len(pending))
	}
	payload, err := json.Marshal(map[string]any{"cascadeId": "conv-delete", "stepIndex": 0})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := d.DispatchDirect(context.Background(), "DeleteQueuedUserInputStep", string(payload)); err != nil {
		t.Fatalf("DeleteQueuedUserInputStep: %v", err)
	}
	pending := traj.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending messages = %d, want 1", len(pending))
	}
	if pending[0].Text != "third" {
		t.Errorf("the remaining message is %q, want %q", pending[0].Text, "third")
	}

	backend.release()
	<-first
}

// TestTheQueuedMessageIsNotTheUsersOwnStep pins that the pending
// message's step is the message itself — the frontend matches the
// server's pending messages against the request it sent by comparing
// the step's items.
//
// The step the card carries is the *queued* step: QUEUED, and flagged
// with the reference's own is_queued_message. The conversation's
// trajectory gets the user's own step — DONE, and not flagged — so
// the two are different steps.
func TestTheQueuedMessageIsNotTheUsersOwnStep(t *testing.T) {
	msg := lsQueuedMessage{ID: "conv-q1", Text: "hello", Strategy: lsDeliveryWhenIdle}
	msg.Step = lsEncodeUserInputStep(msg.Text, true, time.Now())
	if len(msg.Step) == 0 {
		t.Fatal("the pending message carries no step")
	}
	if !strings.Contains(string(msg.Step), "hello") {
		t.Errorf("the step does not carry the message text")
	}
	if got := lsDecodeProtoVarint(lsDecodeProtoFields(msg.Step), 4); got != lsStepStatusQueued {
		t.Errorf("the pending step's status is %d, want %d", got, lsStepStatusQueued)
	}
	own := lsUserInputStep(msg.Text)
	if got := own["status"]; got != lsStepStatusDone {
		t.Errorf("the user's own step's status is %v, want %d", got, lsStepStatusDone)
	}
	userInput, _ := own["userInput"].(map[string]any)
	if userInput == nil {
		t.Fatalf("the user's own step carries no user input: %v", own)
	}
	if _, ok := userInput["isQueuedMessage"]; ok {
		t.Errorf("the user's own step is flagged as a queued message: %v", userInput)
	}
}
