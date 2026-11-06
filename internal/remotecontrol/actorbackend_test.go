package remotecontrol

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

type FakeRemoteSession struct {
	mu        sync.Mutex
	events    []SessionEvent
	msgs      []SessionMessage
	sendErr   error
	closed    bool
	closeErr  error
	sendCalls int
}

type FakePigSession = FakeRemoteSession

func (s *FakeRemoteSession) Send(ctx context.Context, _ string, events chan<- SessionEvent) ([]SessionMessage, error) {
	s.mu.Lock()
	s.sendCalls++
	frames := append([]SessionEvent(nil), s.events...)
	msgs := append([]SessionMessage(nil), s.msgs...)
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

func (s *FakeRemoteSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.closeErr
}

func cannedRemoteFactory(sess Session) SessionFactory {
	return func(_ context.Context, _ cascades.Prompt, _ cascades.SendOptions) (Session, error) {
		return sess, nil
	}
}

func errRemoteFactory(err error) SessionFactory {
	return func(_ context.Context, _ cascades.Prompt, _ cascades.SendOptions) (Session, error) {
		return nil, err
	}
}

func remoteStartEvent() SessionEvent {
	return SessionEvent{Type: SessionEventStart}
}

func remoteTextDelta(text string) SessionEvent {
	return SessionEvent{Type: SessionEventTextDelta, Delta: text}
}

func remoteToolEnd(name string, args string) SessionEvent {
	return SessionEvent{
		Type: SessionEventToolCall,
		ToolCall: cascades.ToolCall{
			Name: name,
			Args: []byte(`{"arg":"` + args + `"}`),
			OK:   true,
		},
	}
}

var pigCannedFactory = cannedRemoteFactory
var errPigFactory = errRemoteFactory
var pigStartEvent = remoteStartEvent
var pigTextDelta = remoteTextDelta
var pigToolEnd = remoteToolEnd

func pigDone(reason string) SessionEvent {
	return SessionEvent{
		Type:       SessionEventDone,
		StopReason: reason,
	}
}

func pigRecoveryMsg(text string) SessionMessage {
	return SessionMessage{
		Role:         "assistant",
		StopReason:   "error",
		ErrorMessage: text,
	}
}

func TestPiagentBackend_HappyPath_DrainsEventsAndAssemblesResult(t *testing.T) {
	toolJSON := `{"arg":"hi"}`
	sess := &FakePigSession{
		events: []SessionEvent{
			pigStartEvent(),
			pigTextDelta("hello "),
			pigTextDelta("from "),
			pigTextDelta("pi"),
			pigToolEnd("echo", "hi"),
			pigDone("stop"),
		},
		msgs: []SessionMessage{},
	}

	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}
	sink := &captureSink{}

	res, err := be.SendStream(context.Background(), cascades.Prompt{Text: "ping"}, sink, cascades.SendOptions{})
	if err != nil {
		t.Fatalf("SendStream returned error: %v", err)
	}
	if got, want := res.Reply, "hello from pi"; got != want {
		t.Errorf("Reply = %q, want %q", got, want)
	}
	if got, want := res.StopReason, cascades.StopReasonEndTurn; got != want {
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

	if len(sink.states) < 2 {
		t.Fatalf("sink.states = %d entries, want >= 2", len(sink.states))
	}
	if sink.states[0].Phase != cascades.StatePhaseStarting {
		t.Errorf("states[0].Phase = %q, want %q", sink.states[0].Phase, cascades.StatePhaseStarting)
	}
	if sink.states[len(sink.states)-1].Phase != cascades.StatePhaseDone {
		t.Errorf("states[last].Phase = %q, want %q", sink.states[len(sink.states)-1].Phase, cascades.StatePhaseDone)
	}
	if sink.states[len(sink.states)-1].StopReason != cascades.StopReasonEndTurn {
		t.Errorf("states[last].StopReason = %q, want %q",
			sink.states[len(sink.states)-1].StopReason, cascades.StopReasonEndTurn)
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

func TestPiagentBackend_FactoryError_PropagatesBeforeTurn(t *testing.T) {
	factoryErr := errors.New("piagent: factory blew up")
	be := &PiagentBackend{SessionFactory: errPigFactory(factoryErr)}
	sink := &captureSink{}

	res, err := be.SendStream(context.Background(), cascades.Prompt{Text: "ping"}, sink, cascades.SendOptions{})
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

func TestPiagentBackend_NilFactory_ReturnsErrNilSessionFactory(t *testing.T) {
	var be *PiagentBackend // nil receiver

	_, err := be.SendStream(context.Background(), cascades.Prompt{Text: "x"}, nil, cascades.SendOptions{})
	if !errors.Is(err, ErrNilSessionFactory) {
		t.Errorf("nil-receiver SendStream err = %v, want errors.Is(_, ErrNilSessionFactory)", err)
	}

	zero := &PiagentBackend{} // nil SessionFactory
	_, err = zero.SendStream(context.Background(), cascades.Prompt{Text: "x"}, nil, cascades.SendOptions{})
	if !errors.Is(err, ErrNilSessionFactory) {
		t.Errorf("zero-SessionFactory SendStream err = %v, want errors.Is(_, ErrNilSessionFactory)", err)
	}
}

func TestPiagentBackend_PropagatesSessionError(t *testing.T) {
	sessionErr := errors.New("piagent: model turn aborted")
	sess := &FakePigSession{
		events: []SessionEvent{
			pigStartEvent(),
			pigTextDelta("mid-stream"),
			pigDone("aborted"),
		},
		msgs:    []SessionMessage{},
		sendErr: sessionErr,
	}

	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}
	sink := &captureSink{}

	_, err := be.SendStream(context.Background(), cascades.Prompt{Text: "x"}, sink, cascades.SendOptions{})
	if !errors.Is(err, sessionErr) {
		t.Errorf("SendStream err = %v, want errors.Is(_, sessionErr)", err)
	}
	if len(sink.errors) != 1 || !errors.Is(sink.errors[0], sessionErr) {
		t.Errorf("sink.errors = %v, want one entry matching sessionErr", sink.errors)
	}
}

func TestPiagentBackend_RecoverPiGFlattenedFailure(t *testing.T) {
	const flatText = "antigravity: upstream stream closed"
	sess := &FakePigSession{
		events: []SessionEvent{
			pigStartEvent(),
			pigDone("error"),
		},
		msgs: []SessionMessage{
			pigRecoveryMsg(flatText),
		},
	}

	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}
	sink := &captureSink{}

	_, err := be.SendStream(context.Background(), cascades.Prompt{Text: "x"}, sink, cascades.SendOptions{})
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

func TestPiagentBackend_StopReasonLengthMapsToMaxTokens(t *testing.T) {
	sess := &FakePigSession{
		events: []SessionEvent{
			pigStartEvent(),
			pigTextDelta("halfw"),
			pigDone("length"),
		},
		msgs: []SessionMessage{},
	}

	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}
	sink := &captureSink{}

	res, err := be.SendStream(context.Background(), cascades.Prompt{Text: "x"}, sink, cascades.SendOptions{})
	if err != nil {
		t.Fatalf("SendStream err = %v", err)
	}
	if got := res.StopReason; got != cascades.StopReasonMaxTokens {
		t.Errorf("StopReason = %q, want %q", got, cascades.StopReasonMaxTokens)
	}
	last := sink.states[len(sink.states)-1]
	if last.StopReason != cascades.StopReasonMaxTokens {
		t.Errorf("sink.states[last].StopReason = %q, want %q", last.StopReason, cascades.StopReasonMaxTokens)
	}
}

func TestPiagentBackend_SendDelegatesToSendStreamWithNilSink(t *testing.T) {
	sess := &FakePigSession{
		events: []SessionEvent{
			pigStartEvent(),
			pigTextDelta("sync reply"),
			pigDone("stop"),
		},
		msgs: []SessionMessage{},
	}
	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}

	res, err := be.Send(context.Background(), cascades.Prompt{Text: "x"})
	if err != nil {
		t.Fatalf("Send err = %v", err)
	}
	if res.Reply != "sync reply" {
		t.Errorf("Result.Reply = %q, want sync reply", res.Reply)
	}
	if res.StopReason != cascades.StopReasonEndTurn {
		t.Errorf("Result.StopReason = %q, want %q", res.StopReason, cascades.StopReasonEndTurn)
	}
}

func TestPiagentBackend_DrainsEventsWhenSinkNil(t *testing.T) {
	sess := &FakePigSession{
		events: []SessionEvent{
			pigStartEvent(),
			pigTextDelta("no "),
			pigTextDelta("sink"),
			pigDone("stop"),
		},
		msgs: []SessionMessage{},
	}
	be := &PiagentBackend{SessionFactory: pigCannedFactory(sess)}

	res, err := be.SendStream(context.Background(), cascades.Prompt{Text: "x"}, nil, cascades.SendOptions{})
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

func TestPiagentBackend_ImplementsBothInterfaces(t *testing.T) {
	var (
		_ cascades.Backend          = (*PiagentBackend)(nil)
		_ cascades.StreamingBackend = (*PiagentBackend)(nil)
	)

	var be any = &PiagentBackend{}
	if _, ok := be.(cascades.Backend); !ok {
		t.Errorf("*PiagentBackend does NOT implement MessageBackend")
	}
	if _, ok := be.(cascades.StreamingBackend); !ok {
		t.Errorf("*PiagentBackend does NOT implement cascades.StreamingBackend")
	}
}

func TestCascade_Backend_LazyInitFromDispatchDefault(t *testing.T) {
	productionBackend := &PiagentBackend{SessionFactory: errPigFactory(errors.New("unused"))}
	c, err := cascades.New(cascades.Opts{
		Model:           "Gemini 3.8 Flash (Medium)",
		DispatchDefault: productionBackend,
	})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}

	got := c.Backend()
	if got != productionBackend {
		t.Errorf("c.Backend() = %T, want *PiagentBackend (the dispatcher-installed default)", got)
	}
	if c.Backend() != productionBackend {
		t.Errorf("c.Backend() second call did not return the same instance")
	}

	userBackend := &cascades.StubBackend{Reply: "user"}
	c.SetBackend(userBackend)
	if got := c.Backend(); got != userBackend {
		t.Errorf("c.Backend() after SetBackend = %T, want *StubBackend (user)", got)
	}
}

func TestCascade_Backend_NilDispatchDefaultKeepsStub(t *testing.T) {
	c, err := cascades.New(cascades.Opts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}

	got := c.Backend()
	if _, ok := got.(*cascades.StubBackend); !ok {
		t.Errorf("c.Backend() = %T, want *StubBackend (seeded default)", got)
	}
}

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
