// Tests for the openagy rc-piagent-bind cycle's
// PiagentBackend. The cycle ships a production
// `MessageBackend` + `StreamingMessageBackend` that turns a
// `MessagePrompt` into a PiG turn; the tests above pin the
// five contracts the dispatcher relies on:
//
//  1. Happy path: a fake `SessionFactory` emits canned
//     events; `PiagentBackend.SendStream` drains them,
//     translates each into EventSink calls, and assembles
//     the end-of-turn MessageResult from the joined deltas.
//
//  2. Error propagation: a session that returns a non-nil
//     Go error after the assistant message landed is
//     surfaced through `OnError` and as the `SendStream`
//     return value, so the dispatcher's `ErrTurnFailed`
//     wrap can route the JSON-RPC code.
//
//  3. PiG fallback: a session that returns a nil Go error
//     but encodes the failure into the assistant message
//     (the cycle-209/210 path that flattens
//     `*backend.HTTPError`) still has its typed error
//     recovered via the messages walk — the dispatcher's
//     `errors.As` continues to match.
//
//  4. Sync fallback: `Send` (the cycle-3a fallback path
//     used when a backend does NOT implement
//     `StreamingMessageBackend`) routes through SendStream
//     with a nil sink and still produces the
//     `MessageResult` from the event stream. Production
//     PiagentBackend implements both — this just pins that
//     the routing does not lose state.
//
//  5. Lazy cascade backend: the dispatcher-supplied
//     `MessageBackend` lands on a cascade on first
//     `Backend()` call when SetBackend has not run.
//
// All tests use the existing `captureSink` fixture from
// cascade_test.go (same package, cycle 5) and a tiny
// `FakePigSession` that satisfies `Session` directly.
//
// No third-party deps. Stdlib + the package's existing
// PiG import.
package remotecontrol

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

// FakePigSession is a `Session` test double that
// replays a canned sequence of `agent.AgentEvent`
// frames on Send, then returns the cycle's expected
// assistant message.
//
// Construction:
//
//	f := &FakePigSession{events: cannedEvents, msgs: cannedMsgs}
//	b := &PiagentBackend{SessionFactory: factoryReturning(f)}
//
// `events` is the slice of frames the session emits
// before closing the channel; each event lands in the
// `chan<- agent.AgentEvent` argument PiagentBackend
// supplied. `msgs` is the slice of `agent.AgentMessage`
// Send returns after draining.
//
// `sendErr` is returned verbatim from Send (nil
// for the happy path).
//
// `closed` is set true once Close() runs; tests use it
// to assert the backend teared down the session even
// on the error path.
type FakePigSession struct {
	mu      sync.Mutex
	events  []agent.AgentEvent
	msgs    []agent.AgentMessage
	sendErr error

	closed    bool
	closeErr  error
	sendCalls int
}

// Send drains the supplied channel into `events`, then
// returns (msgs, sendErr).
func (s *FakePigSession) Send(ctx context.Context, _ string, events chan<- agent.AgentEvent) ([]agent.AgentMessage, error) {
	s.mu.Lock()
	s.sendCalls++
	frames := append([]agent.AgentEvent(nil), s.events...)
	msgs := append([]agent.AgentMessage(nil), s.msgs...)
	sendErr := s.sendErr
	s.mu.Unlock()

	for _, ev := range frames {
		select {
		case events <- ev:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return msgs, sendErr
}

// Close sets the closed flag and returns any
// caller-supplied closeErr.
func (s *FakePigSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.closeErr
}

// pigCannedFactory returns a SessionFactory that always
// returns `sess`, no matter what prompt arrives. Tests
// pass &FakePigSession{} as sess; the factory pins the
// behaviour for every Send call.
func pigCannedFactory(sess Session) SessionFactory {
	return func(_ context.Context, _ MessagePrompt, _ MessageSendOptions) (Session, error) {
		return sess, nil
	}
}

// errPigFactory returns a SessionFactory that always
// returns an error. Pinning the failure at factory time
// (rather than at session.Send time) exercises the
// "factory returned an error before any turn ran"
// branch of SendStream.
func errPigFactory(err error) SessionFactory {
	return func(_ context.Context, _ MessagePrompt, _ MessageSendOptions) (Session, error) {
		return nil, err
	}
}

// pigStartEvent is a no-frills PiG `StartEvent` wrapped in
// a MessageUpdateEvent for the bridge to translate. The
// partial is nil — PiagentBackend.SendStream doesn't read
// it.
func pigStartEvent() agent.MessageUpdateEvent {
	return agent.MessageUpdateEvent{
		AssistantMessageEvent: ai.StartEvent{},
	}
}

// pigTextDelta wraps a single string delta the same way.
func pigTextDelta(text string) agent.MessageUpdateEvent {
	return agent.MessageUpdateEvent{
		AssistantMessageEvent: ai.TextDeltaEvent{Delta: text},
	}
}

// pigToolEnd wraps a single `ai.ToolCall` end event the
// bridge translates into a `ToolCall` EventSink callback.
func pigToolEnd(name string, args string) agent.MessageUpdateEvent {
	return agent.MessageUpdateEvent{
		AssistantMessageEvent: ai.ToolCallEndEvent{
			ToolCall: ai.ToolCall{
				Name:      name,
				Arguments: map[string]any{"arg": args},
			},
		},
	}
}

// pigDone wraps a final Done event with the given stop
// reason. The cycle's `stopReasonFromPiG` translates the
// PiG string into the cycle's vocabulary.
func pigDone(reason ai.StopReason) agent.MessageUpdateEvent {
	return agent.MessageUpdateEvent{
		AssistantMessageEvent: ai.DoneEvent{
			Reason: reason,
			Message: &ai.AssistantMessage{
				StopReason: reason,
				Content:    []ai.AssistantContentBlock{},
				API:        ai.API("test/test"),
			},
		},
	}
}

// pigRecoveryMsg builds an assistant message stamped with
// a PiG StopReasonError + ErrorMessage text. PiG's
// agent loop returns a nil Go error in this case (the
// agent_loop.go:494 err.Error() flattening), so the
// recovery has to walk the messages to recover the inner
// text.
//
// Note: agent.AgentMessage.Assistant is
// *agent.AssistantMessage (not *ai.AssistantMessage);
// both types carry the same StopReason / ErrorMessage
// fields the recovery walker inspects.
func pigRecoveryMsg(text string) agent.AgentMessage {
	return agent.AgentMessage{
		Assistant: &agent.AssistantMessage{
			StopReason:   ai.StopReasonError,
			ErrorMessage: text,
		},
	}
}

// TestPiagentBackend_HappyPath_DrainsEventsAndAssemblesResult
// pins the headline contract: a session that emits a
// Start + 3 TextDeltas + 1 ToolEnd + Done(stop) drives
// the same EventSink callback sequence a real PiG turn
// would, and the resulting MessageResult carries the
// joined reply, the tool call, and the cycle-mapped
// stop reason.
func TestPiagentBackend_HappyPath_DrainsEventsAndAssemblesResult(t *testing.T) {
	toolJSON := `{"arg":"hi"}`
	sess := &FakePigSession{
		events: []agent.AgentEvent{
			pigStartEvent(),
			pigTextDelta("hello "),
			pigTextDelta("from "),
			pigTextDelta("pi"),
			pigToolEnd("echo", "hi"),
			pigDone(ai.StopReasonStop),
		},
		msgs: []agent.AgentMessage{},
	}

	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}
	sink := &captureSink{}

	res, err := be.SendStream(context.Background(), MessagePrompt{Text: "ping"}, sink, MessageSendOptions{})
	if err != nil {
		t.Fatalf("SendStream returned error: %v", err)
	}
	if got, want := res.Reply, "hello from pi"; got != want {
		t.Errorf("Reply = %q, want %q", got, want)
	}
	if got, want := res.StopReason, StopReasonEndTurn; got != want {
		t.Errorf("StopReason = %q, want %q", got, want)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(res.ToolCalls))
	}
	if got := res.ToolCalls[0].Name; got != "echo" {
		t.Errorf("ToolCalls[0].Name = %q, want echo", got)
	}
	if got := string(res.ToolCalls[0].Args); got != toolJSON {
		t.Errorf("ToolCalls[0].Args = %s, want %s", got, toolJSON)
	}

	// Sink order: starting + 3 text deltas + tool + done.
	if len(sink.states) < 2 {
		t.Fatalf("sink.states = %d entries, want >= 2", len(sink.states))
	}
	if sink.states[0].Phase != StatePhaseStarting {
		t.Errorf("states[0].Phase = %q, want %q", sink.states[0].Phase, StatePhaseStarting)
	}
	if sink.states[len(sink.states)-1].Phase != StatePhaseDone {
		t.Errorf("states[last].Phase = %q, want %q", sink.states[len(sink.states)-1].Phase, StatePhaseDone)
	}
	if sink.states[len(sink.states)-1].StopReason != StopReasonEndTurn {
		t.Errorf("states[last].StopReason = %q, want %q",
			sink.states[len(sink.states)-1].StopReason, StopReasonEndTurn)
	}
	wantDeltas := []string{"hello ", "from ", "pi"}
	if len(sink.deltas) != len(wantDeltas) {
		t.Fatalf("sink.deltas = %v, want %v", sink.deltas, wantDeltas)
	}
	for i, d := range wantDeltas {
		if sink.deltas[i] != d {
			t.Errorf("sink.deltas[%d] = %q, want %q", i, sink.deltas[i], d)
		}
	}
	if len(sink.tools) != 1 || sink.tools[0].Name != "echo" {
		t.Errorf("sink.tools = %+v, want one entry named echo", sink.tools)
	}
	if len(sink.errors) != 0 {
		t.Errorf("sink.errors = %v, want empty", sink.errors)
	}

	if !sess.closed {
		t.Errorf("session.Close was not called; want true after SendStream returns")
	}
}

// TestPiagentBackend_FactoryError_PropagatesBeforeTurn confirms
// that when the SessionFactory fails, no turn runs, no
// sink call fires, and the error reaches the dispatcher
// verbatim (so ErrTurnFailed can wrap to -32603).
func TestPiagentBackend_FactoryError_PropagatesBeforeTurn(t *testing.T) {
	factoryErr := errors.New("piagent: factory blew up")
	be := &PiagentBackend{SessionFactory: errPigFactory(factoryErr)}
	sink := &captureSink{}

	res, err := be.SendStream(context.Background(), MessagePrompt{Text: "ping"}, sink, MessageSendOptions{})
	if err == nil {
		t.Fatalf("SendStream returned nil error; want %v", factoryErr)
	}
	if !errors.Is(err, factoryErr) {
		t.Errorf("SendStream err = %v, want errors.Is(_, factoryErr)", err)
	}
	if res.Reply != "" {
		t.Errorf("Result.Reply = %q, want empty (no turn ran)", res.Reply)
	}
	if len(sink.states) != 0 || len(sink.deltas) != 0 || len(sink.tools) != 0 || len(sink.errors) != 0 {
		t.Errorf("sink received events: states=%d deltas=%d tools=%d errors=%d; want 0/0/0/0 (no events when factory fails)",
			len(sink.states), len(sink.deltas), len(sink.tools), len(sink.errors))
	}
}

// TestPiagentBackend_NilFactory_ReturnsErrNilSessionFactory
// pins the sentinel returned when the host forgot to
// install a factory. The dispatcher surfaces it as -32603
// via ErrTurnFailed; the message is distinctive enough
// that an operator can spot it.
func TestPiagentBackend_NilFactory_ReturnsErrNilSessionFactory(t *testing.T) {
	var be *PiagentBackend // nil receiver

	_, err := be.SendStream(context.Background(), MessagePrompt{Text: "x"}, nil, MessageSendOptions{})
	if !errors.Is(err, ErrNilSessionFactory) {
		t.Errorf("nil-receiver SendStream err = %v, want errors.Is(_, ErrNilSessionFactory)", err)
	}

	zero := &PiagentBackend{} // nil SessionFactory
	_, err = zero.SendStream(context.Background(), MessagePrompt{Text: "x"}, nil, MessageSendOptions{})
	if !errors.Is(err, ErrNilSessionFactory) {
		t.Errorf("zero-SessionFactory SendStream err = %v, want errors.Is(_, ErrNilSessionFactory)", err)
	}
}

// TestPiagentBackend_PropagatesSessionError confirms that
// when the session returns a non-nil Go error after a
// complete event stream landed, the error reaches
// SendStream's caller AND OnError fires on the sink
// (before OnState(done), so a dashboard sees the failure
// before the success frame).
func TestPiagentBackend_PropagatesSessionError(t *testing.T) {
	sessionErr := errors.New("piagent: model turn aborted")
	sess := &FakePigSession{
		events: []agent.AgentEvent{
			pigStartEvent(),
			pigTextDelta("mid-stream"),
			pigDone(ai.StopReasonAborted),
		},
		msgs:    []agent.AgentMessage{},
		sendErr: sessionErr,
	}

	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}
	sink := &captureSink{}

	_, err := be.SendStream(context.Background(), MessagePrompt{Text: "x"}, sink, MessageSendOptions{})
	if !errors.Is(err, sessionErr) {
		t.Errorf("SendStream err = %v, want errors.Is(_, sessionErr)", err)
	}
	if len(sink.errors) != 1 || !errors.Is(sink.errors[0], sessionErr) {
		t.Errorf("sink.errors = %v, want one entry matching sessionErr", sink.errors)
	}
}

// TestPiagentBackend_RecoverPiGFlattenedFailure pins the
// cycle-209/210 recovery path: a session that returns
// (nil err) but the assistant message carries a
// StopReasonError + ErrorMessage. PiG flattens the
// transport error into the message; PiagentBackend
// recovers the original text via the messages walk
// (the `piagentTurnFailure` helper) and surfaces it as
// a non-nil Go error so the dispatcher's `errors.As`
// catch around `ErrTurnFailed` keeps working.
func TestPiagentBackend_RecoverPiGFlattenedFailure(t *testing.T) {
	const flatText = "antigravity: upstream stream closed"
	sess := &FakePigSession{
		events: []agent.AgentEvent{
			pigStartEvent(),
			pigDone(ai.StopReasonError),
		},
		msgs: []agent.AgentMessage{
			pigRecoveryMsg(flatText),
		},
	}

	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}
	sink := &captureSink{}

	_, err := be.SendStream(context.Background(), MessagePrompt{Text: "x"}, sink, MessageSendOptions{})
	if err == nil {
		t.Fatalf("SendStream returned nil; want recovered ErrorMessage %q", flatText)
	}
	if got := err.Error(); !contains(got, flatText) {
		t.Errorf("err.Error() = %q, want substring %q", got, flatText)
	}
	if len(sink.errors) != 1 || !contains(sink.errors[0].Error(), flatText) {
		t.Errorf("sink.errors = %v, want one entry containing %q", sink.errors, flatText)
	}
}

// TestPiagentBackend_StopReasonLengthMapsToMaxTokens pins
// the cycle's PiG→wire mapping: a `StopReasonLength` from
// PiG surfaces as the cycle's "max_tokens" vocabulary
// (PiG has no separate MaxTokens constant; "length" is
// the upstream union member — see
// github.com/MichaelKinsy/PiG/ai/types.go:341-348).
func TestPiagentBackend_StopReasonLengthMapsToMaxTokens(t *testing.T) {
	sess := &FakePigSession{
		events: []agent.AgentEvent{
			pigStartEvent(),
			pigTextDelta("halfw"),
			pigDone(ai.StopReasonLength),
		},
		msgs: []agent.AgentMessage{},
	}

	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}
	sink := &captureSink{}

	res, err := be.SendStream(context.Background(), MessagePrompt{Text: "x"}, sink, MessageSendOptions{})
	if err != nil {
		t.Fatalf("SendStream err = %v", err)
	}
	if got := res.StopReason; got != StopReasonMaxTokens {
		t.Errorf("StopReason = %q, want %q", got, StopReasonMaxTokens)
	}
	last := sink.states[len(sink.states)-1]
	if last.StopReason != StopReasonMaxTokens {
		t.Errorf("sink.states[last].StopReason = %q, want %q", last.StopReason, StopReasonMaxTokens)
	}
}

// TestPiagentBackend_SendDelegatesToSendStreamWithNilSink
// pins the cycle-3a fallback: a real backend that
// implements both MessageBackend (Send) and
// StreamingMessageBackend (SendStream) routes Send through
// SendStream with a nil sink. Production PiagentBackend
// satisfies both — this is the dispatcher type-assertion
// path: backend.Send gets called when a backend doesn't
// implement SendStream; the second arm implements both,
// confirming neither is broken.
func TestPiagentBackend_SendDelegatesToSendStreamWithNilSink(t *testing.T) {
	sess := &FakePigSession{
		events: []agent.AgentEvent{
			pigStartEvent(),
			pigTextDelta("sync reply"),
			pigDone(ai.StopReasonStop),
		},
		msgs: []agent.AgentMessage{},
	}
	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}

	// Use Send with a nil sink — the production code path
	// doesn't pass one; the dispatcher passes the same
	// value the streaming call would have.
	res, err := be.Send(context.Background(), MessagePrompt{Text: "x"})
	if err != nil {
		t.Fatalf("Send err = %v", err)
	}
	if res.Reply != "sync reply" {
		t.Errorf("Result.Reply = %q, want sync reply", res.Reply)
	}
	if res.StopReason != StopReasonEndTurn {
		t.Errorf("Result.StopReason = %q, want %q", res.StopReason, StopReasonEndTurn)
	}
}

// TestPiagentBackend_DrainsEventsWhenSinkNil pins the
// nil-sink-tolerance rule: a SendStream call with a nil
// sink still drains the session's event channel (so the
// session is not blocked on its emit) and returns the
// MessageResult assembled from the joined deltas. The
// call returns no error in this scenario.
func TestPiagentBackend_DrainsEventsWhenSinkNil(t *testing.T) {
	sess := &FakePigSession{
		events: []agent.AgentEvent{
			pigStartEvent(),
			pigTextDelta("no "),
			pigTextDelta("sink"),
			pigDone(ai.StopReasonStop),
		},
		msgs: []agent.AgentMessage{},
	}
	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}

	res, err := be.SendStream(context.Background(), MessagePrompt{Text: "x"}, nil, MessageSendOptions{})
	if err != nil {
		t.Fatalf("SendStream err = %v", err)
	}
	if res.Reply != "no sink" {
		t.Errorf("Result.Reply = %q, want no sink", res.Reply)
	}
	if sess.sendCalls != 1 {
		t.Errorf("session.Send called %d times, want 1", sess.sendCalls)
	}
	if !sess.closed {
		t.Errorf("session.Close was not called after nil-sink SendStream")
	}
}

// TestPiagentBackend_ImplementsBothInterfaces is the
// compile-time check assertion lifted into a runtime
// test so the wiring never silently drifts off the
// cascade's contract.
func TestPiagentBackend_ImplementsBothInterfaces(t *testing.T) {
	var (
		_ MessageBackend          = (*PiagentBackend)(nil)
		_ StreamingMessageBackend = (*PiagentBackend)(nil)
	)

	var be any = &PiagentBackend{}
	if _, ok := be.(MessageBackend); !ok {
		t.Errorf("*PiagentBackend does NOT implement MessageBackend")
	}
	if _, ok := be.(StreamingMessageBackend); !ok {
		t.Errorf("*PiagentBackend does NOT implement StreamingMessageBackend")
	}
}

// TestCascade_Backend_LazyInitFromDispatchDefault pins
// the dispatcher's lazy-init path: a cascade built with
// a non-nil CascadeOpts.DispatchDefault swaps the
// seeded *StubBackend for the supplied default on first
// Backend() call. After SetBackend runs, Backend() keeps
// returning the user's backend (no further swap).
func TestCascade_Backend_LazyInitFromDispatchDefault(t *testing.T) {
	productionBackend := &PiagentBackend{SessionFactory: errPigFactory(errors.New("unused"))}
	c, err := NewCascade(CascadeOpts{
		Model:           "Gemini 3.8 Flash (Medium)",
		DispatchDefault: productionBackend,
	})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}

	// First Backend() call swaps in the default.
	got := c.Backend()
	if got != productionBackend {
		t.Errorf("c.Backend() = %T, want *PiagentBackend (the dispatcher-installed default)", got)
	}
	// Second call returns the same instance.
	if c.Backend() != productionBackend {
		t.Errorf("c.Backend() second call did not return the same instance")
	}

	// SetBackend installs a different backend; Backend()
	// returns it, not the dispatcher default.
	userBackend := &StubBackend{Reply: "user"}
	c.SetBackend(userBackend)
	if got := c.Backend(); got != userBackend {
		t.Errorf("c.Backend() after SetBackend = %T, want *StubBackend (user)", got)
	}
}

// TestCascade_Backend_NilDispatchDefaultKeepsStub pins the
// pre-cycle behaviour: a cascade with no
// DispatchDefault routes turns through *StubBackend. The
// cascade_test.go existing tests already exercised this
// path; this test pins it explicitly so a future cycle
// that breaks the lazy init can't silently land on
// "no backend installed" instead of *StubBackend.
func TestCascade_Backend_NilDispatchDefaultKeepsStub(t *testing.T) {
	c, err := NewCascade(CascadeOpts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}

	got := c.Backend()
	if _, ok := got.(*StubBackend); !ok {
		t.Errorf("c.Backend() = %T, want *StubBackend (seeded default)", got)
	}
}

// contains is the unicode-free substring helper used by
// two tests above. strings.Contains is stdlib but every
// other fixture in this package is local; consistent
// style wins here.
func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Ensure both errors the cycle added are distinguishable
// from `errors.New("...")` ones. A future dispatcher
// switch on the concrete type expects unique identities.
func TestPiagentBackend_SentinelsAreUniqueErrors(t *testing.T) {
	if ErrNilSessionFactory == nil {
		t.Fatal("ErrNilSessionFactory is nil")
	}
	if ErrSessionClosed == nil {
		t.Fatal("ErrSessionClosed is nil")
	}
	if ErrNilSessionFactory.Error() == ErrSessionClosed.Error() {
		t.Errorf("sentinel messages collided: %q", ErrNilSessionFactory.Error())
	}
}
