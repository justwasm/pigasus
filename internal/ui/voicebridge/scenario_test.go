package voicebridge

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/infra/livevoice"
	voicebackend "github.com/justwasm/pigasus/internal/voice"
)

// scenarioEditor is the composer port: it records every write so a scenario
// can assert what the user would have seen in the input box.
type scenarioEditor struct {
	value   string
	inserts []string
}

func (e *scenarioEditor) Value() string         { return e.value }
func (e *scenarioEditor) SetValue(v string)     { e.value = v }
func (e *scenarioEditor) InsertString(s string) { e.inserts = append(e.inserts, s); e.value += s }

// scenarioSession is the transcript port: it records appended messages and
// the router the bridge installed.
type scenarioSession struct {
	messages []backend.Message
	router   LiveVoiceRouter
}

// SetRouter records the router the bridge installed.
func (s *scenarioSession) SetRouter(router LiveVoiceRouter) { s.router = router }

func (s *scenarioSession) Messages() []backend.Message { return s.messages }

func (s *scenarioSession) AppendMessage(msg backend.Message) {
	s.messages = append(s.messages, msg)
}

// scenarioSink records every progress event, in order.
type scenarioSink struct {
	events []string
}

func (s *scenarioSink) add(event string) { s.events = append(s.events, event) }

func (s *scenarioSink) UserSpeechInterim(text string) { s.add("interim:" + text) }
func (s *scenarioSink) UserSpeech(text string)        { s.add("speech:" + text) }
func (s *scenarioSink) AgentSpeech(text string)       { s.add("agent:" + text) }
func (s *scenarioSink) AgentSpeechDelta(delta string) { s.add("delta:" + delta) }
func (s *scenarioSink) Interrupted()                  { s.add("interrupted") }
func (s *scenarioSink) ThinkerHandoff(id, task string) {
	s.add("handoff:" + id + ":" + task)
}
func (s *scenarioSink) ThinkerDone(id, answer string, err error) {
	s.add("thinkerdone:" + id + ":" + answer + ":" + errString(err))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func (s *scenarioSink) LiveError(err error) { s.add("error:" + err.Error()) }

// fakeCapture is the local microphone: it hands the scenario synthetic PCM.
type fakeCapture struct {
	onAudio func([]byte)
	pcm     []byte
	started bool
	closed  bool
}

func (c *fakeCapture) Start() error { c.started = true; return nil }
func (c *fakeCapture) Stop() error  { return nil }
func (c *fakeCapture) Close() error { c.closed = true; return nil }

// fakeConn is the dial probe's connection.
type fakeConn struct{ closed bool }

func (c *fakeConn) Read([]byte) (int, error)         { return 0, nil }
func (c *fakeConn) Write([]byte) (int, error)        { return 0, nil }
func (c *fakeConn) Close() error                     { c.closed = true; return nil }
func (c *fakeConn) LocalAddr() net.Addr              { return nil }
func (c *fakeConn) RemoteAddr() net.Addr             { return nil }
func (c *fakeConn) SetDeadline(time.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(time.Time) error { return nil }

// fakeSession is the live session: it records what the bridge asked of it.
type fakeSession struct {
	started    bool
	hungUp     bool
	duplex     bool
	muted      bool
	speaking   bool
	active     bool
	history    []voicebackend.LiveTurnRecord
	notes      []string
	answers    []string
	stopped    int
	handoffs   map[string]any
	waiting    []string
	routerText []string
	sent       []string
}

func (s *fakeSession) SendText(text string) error {
	s.sent = append(s.sent, text)
	return nil
}

func (s *fakeSession) Start(ctx context.Context, cfg voicebackend.LiveSessionConfig) error {
	s.started = true
	s.active = true
	return nil
}
func (s *fakeSession) HangUp() error                                      { s.hungUp = true; s.active = false; return nil }
func (s *fakeSession) SetDuplex(d bool)                                   { s.duplex = d }
func (s *fakeSession) ToggleMute() bool                                   { s.muted = !s.muted; return s.muted }
func (s *fakeSession) IsMuted() bool                                      { return s.muted }
func (s *fakeSession) Speaking() bool                                     { return s.speaking }
func (s *fakeSession) IsActive() bool                                     { return s.active }
func (s *fakeSession) ConversationHistory() []voicebackend.LiveTurnRecord { return s.history }
func (s *fakeSession) SetConversationHistory(turns []voicebackend.LiveTurnRecord) {
	s.history = turns
}
func (s *fakeSession) SendWaitingOnUser(q string) error { s.waiting = append(s.waiting, q); return nil }
func (s *fakeSession) SendTaskStopped() error           { s.stopped++; return nil }
func (s *fakeSession) SendThinkerNote(n string) error   { s.notes = append(s.notes, n); return nil }
func (s *fakeSession) SendThinkerAnswer(a string) error { s.answers = append(s.answers, a); return nil }
func (s *fakeSession) CompleteHandoff(id string, result any) error {
	if s.handoffs == nil {
		s.handoffs = map[string]any{}
	}
	s.handoffs[id] = result
	return nil
}

// ports assembles the scenario's seams with the current runner.
func (sc *scenario) ports() Ports {
	return Ports{
		Progress: sc.sink,
		Input:    sc.editor,
		Session:  sc.session,
		Cwd:      func() string { return "/work" },
		SetLiveRouter: func(router LiveVoiceRouter) {
			sc.session.SetRouter(router)
		},
		RunThinker: sc.runner,
	}
}

// scenario builds a bridge whose every dependency is a fake.
type scenario struct {
	bridge      *Model
	editor      *scenarioEditor
	session     *scenarioSession
	sink        *scenarioSink
	live        *fakeSession
	capture     *fakeCapture
	probed      []string
	dialErr     error
	env         Env
	runner      ThinkerRunner
	transcribed []byte
}

func newScenario(t *testing.T) *scenario {
	t.Helper()
	sc := &scenario{
		editor:  &scenarioEditor{},
		session: &scenarioSession{},
		sink:    &scenarioSink{},
		live:    &fakeSession{active: true},
		capture: &fakeCapture{},
		env:     Env{MockVoice: true},
	}
	sc.bridge = New(Ports{
		Progress: sc.sink,
		Input:    sc.editor,
		Session:  sc.session,
		Cwd:      func() string { return "/work" },
		SetLiveRouter: func(router LiveVoiceRouter) {
			sc.session.SetRouter(router)
		},
	}, Config{
		Env: func() Env { return sc.env },
		Recorder: Recorder{
			Dial: func(network, addr string, timeout time.Duration) (net.Conn, error) {
				sc.probed = append(sc.probed, addr)
				if sc.dialErr != nil {
					return nil, sc.dialErr
				}
				return &fakeConn{}, nil
			},
			NewAudioCapture: func(rate int, onAudio func([]byte)) (capture, error) {
				sc.capture.onAudio = onAudio
				return sc.capture, nil
			},
		},
		LoadToken: func() (*auth.AntigravityToken, error) { return nil, errors.New("no token") },
		ResolveBackend: func(livevoice.Options) (voicebackend.LiveBackend, error) {
			return nil, nil
		},
		NewLiveSession: func(voicebackend.LiveBackend, voicebackend.NativeLiveHandler) LiveSession {
			return sc.live
		},
	})
	return sc
}

func TestScenarioConnectRejectsUnreachableMic(t *testing.T) {
	// Scenario: a configured remote microphone that refuses the dial fails the
	// connect before the local device opens, with the reference's wording.
	sc := newScenario(t)
	sc.env.MicAddr = "127.0.0.1:5"
	sc.dialErr = errors.New("connection refused")

	err := sc.bridge.Connect()
	if err == nil || !strings.Contains(err.Error(), "cannot reach the mic server at 127.0.0.1:5 (tcp)") {
		t.Fatalf("connect error = %v, want the mic-server failure", err)
	}
	if sc.capture.started {
		t.Fatal("the local device opened after the remote probe failed")
	}
}

func TestScenarioDictationTranscribesBufferedPCM(t *testing.T) {
	// Scenario: dictation records synthetic PCM, and finishing returns a
	// command that transcribes it and answers the surface's message.
	sc := newScenario(t)
	sc.bridge.SetTranscriber(TranscriberFunc(func(ctx context.Context, audio []byte, mime string) (string, error) {
		sc.transcribed = append([]byte(nil), audio...)
		return "hello from the microphone", nil
	}))

	if err := sc.bridge.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	sc.capture.onAudio([]byte{0x01, 0x02})
	sc.capture.onAudio([]byte{0x03})

	dictation := sc.bridge.FinishDictation()
	if dictation == nil {
		t.Fatal("finishing an empty recording returned a command")
	}
	done := dictation()
	if done.Err != nil {
		t.Fatalf("transcription: %v", done.Err)
	}
	if done.Text != "hello from the microphone" {
		t.Fatalf("text = %q", done.Text)
	}
	if len(sc.transcribed) != 3 {
		t.Fatalf("transcribed %d bytes, want 3", len(sc.transcribed))
	}
}

func TestScenarioDictationCancelDropsRecording(t *testing.T) {
	// Scenario: cancelling discards the buffer, so finishing has nothing to
	// transcribe and returns no command.
	sc := newScenario(t)
	if err := sc.bridge.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	sc.capture.onAudio([]byte{0x01})
	sc.bridge.CancelDictation()

	if dictation := sc.bridge.FinishDictation(); dictation != nil {
		t.Fatal("cancelled dictation still produced a transcription command")
	}
}

func TestScenarioMuteTogglesWithAndWithoutSession(t *testing.T) {
	// Scenario: mute flips locally while idle and follows the session once one
	// is live.
	sc := newScenario(t)
	if sc.bridge.Muted() {
		t.Fatal("a fresh bridge reports muted")
	}
	if !sc.bridge.ToggleMute() || !sc.bridge.Muted() {
		t.Fatal("the first toggle did not mute")
	}
	if sc.bridge.ToggleMute() || sc.bridge.Muted() {
		t.Fatal("the second toggle did not unmute")
	}

	sc.bridge.SetLive(sc.live)
	if !sc.bridge.ToggleMute() || !sc.live.muted {
		t.Fatal("the session's mute did not follow the toggle")
	}
	if sc.bridge.Muted() != sc.live.IsMuted() {
		t.Fatal("the bridge and the session disagree on mute")
	}
}

func TestScenarioLiveStartSeedsHistoryAndRouter(t *testing.T) {
	// Scenario: starting a live session installs the router, replays the
	// transcript into the session and carries the workspace into the prompt.
	sc := newScenario(t)
	sc.session.messages = []backend.Message{
		{Role: "user", Content: "my code is 42"},
		{Role: "assistant", Content: "noted"},
		{Role: "user", Content: "   "},
	}
	sc.bridge.SetVoice("Zephyr")

	if err := sc.bridge.StartLive(context.Background()); err != nil {
		t.Fatalf("start live: %v", err)
	}
	if !sc.live.started || !sc.live.duplex == sc.bridge.Duplex() {
		t.Fatalf("session started=%v duplex=%v", sc.live.started, sc.live.duplex)
	}
	if got := sc.session.router; got != LiveVoiceRouter(sc.live) {
		t.Fatalf("router = %v, want the live session", got)
	}
	if len(sc.live.history) != 2 || sc.live.history[0].Content != "my code is 42" {
		t.Fatalf("history = %+v, want the two non-empty turns", sc.live.history)
	}
}

func TestScenarioLiveStopClearsRouterAndHangsUp(t *testing.T) {
	// Scenario: hanging up clears the router first, so a typed prompt can no
	// longer reach the dead session.
	sc := newScenario(t)
	if err := sc.bridge.StartLive(context.Background()); err != nil {
		t.Fatalf("start live: %v", err)
	}
	if err := sc.bridge.StopLive(); err != nil {
		t.Fatalf("stop live: %v", err)
	}
	if !sc.live.hungUp {
		t.Fatal("the session was not hung up")
	}
	if sc.session.router != nil {
		t.Fatalf("router = %v, want nil after hang-up", sc.session.router)
	}
	if sc.bridge.LiveActive() {
		t.Fatal("the bridge still reports a live session")
	}
}

func TestScenarioThinkerHandoffRunsAndReports(t *testing.T) {
	// Scenario: a hand-off runs the host's thinker, streams the answer into
	// the session and reports the terminal state to the sink.
	sc := newScenario(t)
	sc.runner = func(ctx context.Context, task string, onProgress func(string)) (string, error) {
		onProgress("step one")
		return "all done", nil
	}
	sc.bridge.SetPorts(sc.ports())
	sc.live.active = true
	sc.bridge.AdoptLive(sc.live)

	answer, err := sc.bridge.RunThinker(context.Background(), "inv-1", "check the tests")
	if err != nil || answer != "all done" {
		t.Fatalf("run thinker = (%q, %v)", answer, err)
	}
	if len(sc.live.notes) != 1 || sc.live.notes[0] != "step one" {
		t.Fatalf("notes = %v", sc.live.notes)
	}
	if got := sc.live.handoffs["inv-1"].(map[string]any)["answer"]; got != "all done" {
		t.Fatalf("handoff result = %v", got)
	}
	if len(sc.sink.events) == 0 || sc.sink.events[len(sc.sink.events)-1] != "thinkerdone:inv-1:all done:" {
		t.Fatalf("sink events = %v", sc.sink.events)
	}
}

func TestScenarioThinkerCancelStopsSessionAndReports(t *testing.T) {
	// Scenario: cancelling an armed hand-off stops the session's task, marks
	// the hand-off cancelled and reports a cancellation to the sink.
	sc := newScenario(t)
	sc.live.active = true
	sc.bridge.AdoptLive(sc.live)

	ctx, cancel := context.WithCancel(context.Background())
	sc.bridge.SetThinkerCancel("inv-2", cancel)

	activeID, cancelled := sc.bridge.CancelThinker("")
	if !cancelled || activeID != "inv-2" {
		t.Fatalf("cancel = (%q, %v), want (inv-2, true)", activeID, cancelled)
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("thinker context = %v, want cancelled", ctx.Err())
	}
	if sc.live.stopped != 1 {
		t.Fatalf("SendTaskStopped calls = %d, want 1", sc.live.stopped)
	}
	if got := sc.live.handoffs["inv-2"].(map[string]any)["cancelled"]; got != true {
		t.Fatalf("handoff result = %v", got)
	}
	if last := sc.sink.events[len(sc.sink.events)-1]; !strings.HasPrefix(last, "thinkerdone:inv-2::") {
		t.Fatalf("last sink event = %q", last)
	}
}

func TestScenarioThinkerFailureReportsError(t *testing.T) {
	// Scenario: a failed hand-off marks the hand-off failed in the session and
	// reports the error, so the caller can speak it.
	sc := newScenario(t)
	sc.runner = func(ctx context.Context, task string, onProgress func(string)) (string, error) {
		return "", errors.New("boom")
	}
	sc.bridge.SetPorts(sc.ports())
	sc.live.active = true
	sc.bridge.AdoptLive(sc.live)

	if _, err := sc.bridge.RunThinker(context.Background(), "inv-3", "task"); err == nil {
		t.Fatal("expected the failure to surface")
	}
	if got := sc.live.handoffs["inv-3"].(map[string]any)["error"]; got != "boom" {
		t.Fatalf("handoff result = %v, want the error", got)
	}
	if len(sc.live.notes) == 0 || !strings.Contains(sc.live.notes[0], "boom") {
		t.Fatalf("notes = %v, want the failure note", sc.live.notes)
	}
	if last := sc.sink.events[len(sc.sink.events)-1]; last != "thinkerdone:inv-3::boom" {
		t.Fatalf("last sink event = %q", last)
	}
}

func TestScenarioHandlerForwardsSessionEvents(t *testing.T) {
	// Scenario: the live session's callbacks reach the sink in order, with the
	// committed utterance clearing the interim preview first.
	sc := newScenario(t)
	handler := &BridgeHandler{Bridge: sc.bridge}

	handler.OnUserSpeech("thinking aloud", true)
	handler.OnUserSpeech("list the files", false)
	handler.OnAgentSpeechDelta("here")
	handler.OnAgentSpeech("here you go")
	handler.OnInterrupted()

	want := []string{"interim:thinking aloud", "interim:", "speech:list the files", "delta:here", "agent:here you go", "interrupted"}
	if strings.Join(sc.sink.events, "|") != strings.Join(want, "|") {
		t.Fatalf("events = %v, want %v", sc.sink.events, want)
	}
}

func TestScenarioHandlerRewritesAuthRejection(t *testing.T) {
	// Scenario: the transport's scope rejection is reported in the reference's
	// own words, so the user is told which variable to set.
	sc := newScenario(t)
	handler := &BridgeHandler{Bridge: sc.bridge}
	handler.OnError(errors.New("insufficient authentication scopes"))

	if len(sc.sink.events) != 1 || !strings.Contains(sc.sink.events[0], "GEMINI_API_KEY") {
		t.Fatalf("events = %v, want the rewritten guidance", sc.sink.events)
	}
}

func TestScenarioInsertDraftAppendsToNonEmptyComposer(t *testing.T) {
	// Scenario: a draft lands in an empty composer as the value, and appends at
	// the cursor once the user has typed something.
	sc := newScenario(t)
	sc.bridge.InsertDraft("first")
	if sc.editor.value != "first" || len(sc.editor.inserts) != 0 {
		t.Fatalf("value = %q inserts = %v", sc.editor.value, sc.editor.inserts)
	}
	sc.bridge.InsertDraft(" second")
	if sc.editor.value != "first second" || len(sc.editor.inserts) != 1 {
		t.Fatalf("value = %q inserts = %v", sc.editor.value, sc.editor.inserts)
	}
	if sc.bridge.PromptEmpty() {
		t.Fatal("a non-empty composer reports empty")
	}
}

func TestScenarioLiveUnavailableWithoutCredentials(t *testing.T) {
	// Scenario: with no credential, no API key, the shipped endpoint and no
	// mock switch, live mode is refused before a transport is resolved.
	sc := newScenario(t)
	sc.env = Env{}
	sc.bridge.SetConfig(Config{
		Env:                  func() Env { return sc.env },
		LoadToken:            func() (*auth.AntigravityToken, error) { return nil, errors.New("no token") },
		APIKey:               func() string { return "" },
		VoiceEndpoint:        func() string { return "wss://default" },
		DefaultVoiceEndpoint: func() string { return "wss://default" },
		Recorder:             Recorder{},
	})

	if sc.bridge.Available() {
		t.Fatal("live mode reported available with no credentials")
	}
	if err := sc.bridge.StartLive(context.Background()); err == nil {
		t.Fatal("expected StartLive to refuse")
	}
}

// compile-time proof the fakes satisfy the ports.
var (
	_ ProgressSink = (*scenarioSink)(nil)
	_ InputEditor  = (*scenarioEditor)(nil)
	_ SessionPort  = (*scenarioSession)(nil)
	_ LiveSession  = (*fakeSession)(nil)
	_ capture      = (*fakeCapture)(nil)
)
