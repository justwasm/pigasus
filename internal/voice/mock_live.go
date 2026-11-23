package voice

import (
	"context"
	"io"
	"sync"
)

// MockLiveBackend provides an in-memory live voice backend for tests and offline mode.
type MockLiveBackend struct {
	mu           sync.Mutex
	lastSession  *MockLiveSession
	customEvents []*LiveServerEvent
	autoEcho     bool
	failAttempts int
	failErr      error
	createdCount int
}

// NewMockLiveBackend creates a new MockLiveBackend.
func NewMockLiveBackend() *MockLiveBackend {
	return &MockLiveBackend{}
}

// SetAutoEcho enables or disables automatic simulated responses on SendAudio (for tests).
func (b *MockLiveBackend) SetAutoEcho(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.autoEcho = enabled
}

// SetCustomEvents sets events that newly created sessions will emit.
func (b *MockLiveBackend) SetCustomEvents(events []*LiveServerEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.customEvents = events
}

// SetFailAttempts configures the next N CreateSession calls to return err.
func (b *MockLiveBackend) SetFailAttempts(n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failAttempts = n
	b.failErr = err
}

// CreatedCount returns the total number of sessions successfully created.
func (b *MockLiveBackend) CreatedCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.createdCount
}

// LastSession returns the most recently created session.
func (b *MockLiveBackend) LastSession() *MockLiveSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastSession
}

// CreateSession implements LiveBackend.
func (b *MockLiveBackend) CreateSession(ctx context.Context, cfg LiveSessionConfig) (LiveSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.failAttempts > 0 {
		b.failAttempts--
		return nil, b.failErr
	}
	b.createdCount++

	sessCtx, cancel := context.WithCancel(ctx)
	s := &MockLiveSession{
		cfg:        cfg,
		ctx:        sessCtx,
		cancel:     cancel,
		inAudio:    make([][]byte, 0),
		inText:     make([]string, 0),
		inToolResp: make(map[string]any),
		eventsCh:   make(chan *LiveServerEvent, 64),
		closedCh:   make(chan struct{}),
		autoEcho:   b.autoEcho,
	}

	if len(b.customEvents) > 0 {
		for _, ev := range b.customEvents {
			s.eventsCh <- ev
		}
	}

	b.lastSession = s
	return s, nil
}

// MockLiveSession is a mock implementation of LiveSession.
type MockLiveSession struct {
	mu               sync.Mutex
	cfg              LiveSessionConfig
	ctx              context.Context
	cancel           context.CancelFunc
	inAudio          [][]byte
	inText           []string
	inToolResp       map[string]any
	inNotes          []string
	inAnswers        []string
	inWaitingOnUser  []string
	taskStoppedCount int
	eventsCh         chan *LiveServerEvent
	closedCh         chan struct{}
	closed           bool
	speaking         bool
	autoEcho         bool
	injectErr        error
}

// InjectedTexts returns all text messages sent to this mock session.
func (s *MockLiveSession) InjectedTexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]string, len(s.inText))
	copy(res, s.inText)
	return res
}

// InjectError simulates an unexpected network drop delivering err to Receive().
func (s *MockLiveSession) InjectError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.injectErr = err
	s.closed = true
	close(s.closedCh)
}

// SendAudio records audio. When autoEcho is enabled (for tests), it simulates user speech and barge-in.
func (s *MockLiveSession) SendAudio(pcm16k []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return io.ErrClosedPipe
	}
	s.inAudio = append(s.inAudio, append([]byte(nil), pcm16k...))

	if !s.autoEcho {
		return nil
	}

	// If the model was speaking and new audio arrives, simulate barge-in interruption!
	if s.speaking {
		s.speaking = false
		select {
		case s.eventsCh <- &LiveServerEvent{Interrupted: true}:
		default:
		}
		return nil
	}

	// If we accumulated enough audio and no custom events were pre-queued, emit a simulated reply.
	if len(s.inAudio) >= 1 && len(s.eventsCh) == 0 {
		s.speaking = true
		pcm24k := generateSilence24k(200)
		select {
		case s.eventsCh <- &LiveServerEvent{
			UserSpeech: "Hello agent",
			Interim:    false,
		}:
		default:
		}
		select {
		case s.eventsCh <- &LiveServerEvent{
			VoiceTranscript: "I hear you clearly.",
			Audio:           pcm24k,
			TurnComplete:    true,
		}:
		default:
		}
	}

	return nil
}

// SendText accepts text input.
func (s *MockLiveSession) SendText(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return io.ErrClosedPipe
	}
	s.inText = append(s.inText, text)

	pcm24k := generateSilence24k(200)
	select {
	case s.eventsCh <- &LiveServerEvent{
		VoiceTranscript: "Acknowledged: " + text,
		Audio:           pcm24k,
		TurnComplete:    true,
	}:
	default:
	}

	return nil
}

// SendToolResponse records the response to a tool call.
func (s *MockLiveSession) SendToolResponse(id string, name string, result any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return io.ErrClosedPipe
	}
	s.inToolResp[id] = result
	return nil
}

// ToolResponses returns a copy of tool responses sent to this mock session.
func (s *MockLiveSession) ToolResponses() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make(map[string]any, len(s.inToolResp))
	for k, v := range s.inToolResp {
		res[k] = v
	}
	return res
}

// InNotes returns a copy of thinker progress notes sent to this mock session.
func (s *MockLiveSession) InNotes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]string, len(s.inNotes))
	copy(res, s.inNotes)
	return res
}

// InAnswers returns a copy of final thinker answers sent to this mock session.
func (s *MockLiveSession) InAnswers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]string, len(s.inAnswers))
	copy(res, s.inAnswers)
	return res
}

// InWaitingOnUser returns a copy of waiting-on-user prompts sent to this mock session.
func (s *MockLiveSession) InWaitingOnUser() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]string, len(s.inWaitingOnUser))
	copy(res, s.inWaitingOnUser)
	return res
}

// TaskStoppedCount returns the number of SendTaskStopped calls recorded by this mock session.
func (s *MockLiveSession) TaskStoppedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taskStoppedCount
}

// SendThinkerNote records a thinker note and simulates a speech update.
func (s *MockLiveSession) SendThinkerNote(note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return io.ErrClosedPipe
	}
	s.inNotes = append(s.inNotes, note)
	select {
	case s.eventsCh <- &LiveServerEvent{
		VoiceTranscript: "Working on it: " + note,
		Audio:           generateSilence24k(150),
		TurnComplete:    true,
	}:
	default:
	}
	return nil
}

// SendThinkerAnswer records the final thinker answer and simulates conclusion speech.
func (s *MockLiveSession) SendThinkerAnswer(answer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return io.ErrClosedPipe
	}
	s.inAnswers = append(s.inAnswers, answer)
	select {
	case s.eventsCh <- &LiveServerEvent{
		VoiceTranscript: "Completed: " + answer,
		Audio:           generateSilence24k(200),
		TurnComplete:    true,
	}:
	default:
	}
	return nil
}

// SendWaitingOnUser records a prompt asking the user for confirmation.
func (s *MockLiveSession) SendWaitingOnUser(question string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return io.ErrClosedPipe
	}
	s.inWaitingOnUser = append(s.inWaitingOnUser, question)
	s.inNotes = append(s.inNotes, "[waiting on the user] "+question)
	select {
	case s.eventsCh <- &LiveServerEvent{
		VoiceTranscript: "I need your confirmation: " + question,
		Audio:           generateSilence24k(150),
		TurnComplete:    true,
	}:
	default:
	}
	return nil
}

// SendTaskStopped notifies the live talker that the active thinker task was stopped.
func (s *MockLiveSession) SendTaskStopped() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return io.ErrClosedPipe
	}
	s.taskStoppedCount++
	s.inNotes = append(s.inNotes, "[stopped]")
	select {
	case s.eventsCh <- &LiveServerEvent{
		VoiceTranscript: "Task has been stopped.",
		Audio:           generateSilence24k(150),
		TurnComplete:    true,
	}:
	default:
	}
	return nil
}

// SendMedia records a real-time visual image or video frame.
func (s *MockLiveSession) SendMedia(mimeType string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return io.ErrClosedPipe
	}
	return nil
}

// Receive returns the next simulated event.
func (s *MockLiveSession) Receive() (*LiveServerEvent, error) {
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case <-s.closedCh:
		s.mu.Lock()
		injErr := s.injectErr
		s.mu.Unlock()
		if injErr != nil {
			return nil, injErr
		}
		return nil, io.EOF
	case ev, ok := <-s.eventsCh:
		if !ok {
			return nil, io.EOF
		}
		return ev, nil
	}
}

// Close closes the session.
func (s *MockLiveSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	s.cancel()
	close(s.closedCh)
	return nil
}

// Config returns the session configuration.
func (s *MockLiveSession) Config() LiveSessionConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Emit sends an event into the session's receive queue.
func (s *MockLiveSession) Emit(ev *LiveServerEvent) {
	s.eventsCh <- ev
}

// InAudioCount returns the count of received audio buffers.
func (s *MockLiveSession) InAudioCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.inAudio)
}

// generateSilence24k creates raw 16-bit little-endian PCM silence (zeros) at 24000Hz for durationMs.
func generateSilence24k(durationMs int) []byte {
	const sampleRate = 24000
	numSamples := (sampleRate * durationMs) / 1000
	return make([]byte, numSamples*2)
}
