package voice

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/voice/voicepb"
)

const (
	// DefaultAICodeEndpoint is the canonical Google Remote Agent Proxy gRPC endpoint.
	DefaultAICodeEndpoint = endpoints.DefaultVoiceAICode

	// DefaultAICodeMethod is the RPC path for StreamVoice in v1alpha.
	DefaultAICodeMethod = "/google.gca.aicode.v1alpha.JetskiRemoteAgentProxyService/StreamVoice"

	// DefaultAICodeUserAgent matches the reference Antigravity language_server client.
	DefaultAICodeUserAgent = "antigravity/2.32.2 (darwin; arm64)"

	// DefaultAICodeClientVersion is the reference client version.
	DefaultAICodeClientVersion = "2.32.2"

	// DefaultAICodeLSVersion is the reference language server version.
	DefaultAICodeLSVersion = "2.32.2"
)

// AICodeStream abstracts the bi-directional gRPC stream for testing and mocking.
type AICodeStream interface {
	SendMsg(m any) error
	RecvMsg(m any) error
	CloseSend() error
}

type aicodeTokenAuth struct {
	token string
}

func (t aicodeTokenAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + t.token,
	}, nil
}

func (t aicodeTokenAuth) RequireTransportSecurity() bool {
	return true
}

// AICodeLiveBackend implements LiveBackend backed by Google's AICode Remote Agent Proxy (RAP).
type AICodeLiveBackend struct {
	mu                    sync.RWMutex
	endpoint              string
	userAgent             string
	clientVersion         string
	languageServerVersion string
	accessToken           string
	fallback              LiveBackend

	// streamFactory allows tests to inject custom streams without making network calls.
	streamFactory func(ctx context.Context, endpoint string, token string) (AICodeStream, func(), error)
}

// NewAICodeLiveBackend creates a new backend using Google OAuth Bearer credentials.
func NewAICodeLiveBackend(accessToken string) *AICodeLiveBackend {
	ep := endpoints.Current().VoiceAICode
	if ep == "" {
		ep = DefaultAICodeEndpoint
	}
	return &AICodeLiveBackend{
		endpoint:              ep,
		userAgent:             DefaultAICodeUserAgent,
		clientVersion:         DefaultAICodeClientVersion,
		languageServerVersion: DefaultAICodeLSVersion,
		accessToken:           accessToken,
	}
}

// NewAICodeLiveBackendWithFallback creates a backend with a secondary fallback LiveBackend.
func NewAICodeLiveBackendWithFallback(accessToken string, fallback LiveBackend) *AICodeLiveBackend {
	b := NewAICodeLiveBackend(accessToken)
	b.fallback = fallback
	return b
}

// SetEndpoint sets the gRPC target endpoint (default: aicode.googleapis.com:443).
func (b *AICodeLiveBackend) SetEndpoint(ep string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.endpoint = ep
}

// Endpoint returns the gRPC target endpoint.
func (b *AICodeLiveBackend) Endpoint() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.endpoint
}

// SetAccessToken sets or refreshes the OAuth access token.
func (b *AICodeLiveBackend) SetAccessToken(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.accessToken = token
}

// AccessToken returns the current OAuth access token.
func (b *AICodeLiveBackend) AccessToken() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.accessToken
}

// SetFallback sets the secondary fallback LiveBackend.
func (b *AICodeLiveBackend) SetFallback(fb LiveBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fallback = fb
}

// Fallback returns the secondary fallback LiveBackend.
func (b *AICodeLiveBackend) Fallback() LiveBackend {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.fallback
}

// SetStreamFactory injects a custom stream creator for unit testing.
func (b *AICodeLiveBackend) SetStreamFactory(fn func(ctx context.Context, endpoint string, token string) (AICodeStream, func(), error)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.streamFactory = fn
}

// CreateSession establishes a bi-directional live session with Google's AICode proxy.
func (b *AICodeLiveBackend) CreateSession(ctx context.Context, cfg LiveSessionConfig) (LiveSession, error) {
	b.mu.RLock()
	endpoint := b.endpoint
	token := b.accessToken
	clientVer := b.clientVersion
	lsVer := b.languageServerVersion
	userAgent := b.userAgent
	factory := b.streamFactory
	fallback := b.fallback
	b.mu.RUnlock()

	if token == "" && factory == nil {
		if fallback != nil {
			return fallback.CreateSession(ctx, cfg)
		}
		return nil, errors.New("aicode live: no OAuth access token provided")
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	var stream AICodeStream
	var cleanupFn func()

	if factory != nil {
		s, cleanup, err := factory(sessionCtx, endpoint, token)
		if err != nil {
			cancel()
			if fallback != nil {
				return fallback.CreateSession(ctx, cfg)
			}
			return nil, fmt.Errorf("aicode live: stream factory failed: %w", err)
		}
		stream = s
		cleanupFn = cleanup
	} else {
		conn, err := grpc.NewClient(
			endpoint,
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
				ServerName: "aicode.googleapis.com",
			})),
			grpc.WithPerRPCCredentials(aicodeTokenAuth{token: token}),
		)
		if err != nil {
			cancel()
			if fallback != nil {
				return fallback.CreateSession(ctx, cfg)
			}
			return nil, fmt.Errorf("aicode live: grpc dial failed: %w", err)
		}

		streamDesc := &grpc.StreamDesc{
			StreamName:    "StreamVoice",
			ServerStreams: true,
			ClientStreams: true,
		}

		md := metadata.Pairs(
			"user-agent", userAgent,
			"x-goog-api-client", "gl-go/1.27.4 gccl/"+clientVer,
		)
		outgoingCtx := metadata.NewOutgoingContext(sessionCtx, md)

		st, err := conn.NewStream(outgoingCtx, streamDesc, DefaultAICodeMethod)
		if err != nil {
			_ = conn.Close()
			cancel()
			if fallback != nil {
				return fallback.CreateSession(ctx, cfg)
			}
			return nil, fmt.Errorf("aicode live: NewStream failed: %w", err)
		}

		stream = st
		cleanupFn = func() {
			_ = conn.Close()
		}
	}

	cascadeID := cfg.CascadeID
	if cascadeID == "" {
		cascadeID = uuid.New().String()
	}

	voiceName := cfg.Voice
	if voiceName == "" {
		voiceName = "Finn"
	}

	sampleRate := cfg.SampleRate
	if sampleRate <= 0 {
		sampleRate = 16000
	}

	setupReq := &voicepb.StreamVoiceRequest{
		Message: &voicepb.VoiceClientMessage{
			Kind: &voicepb.VoiceClientMessage_Setup_{
				Setup: &voicepb.VoiceClientMessage_Setup{
					CascadeId:             cascadeID,
					Voice:                 voiceName,
					SampleRateHertz:       int32(sampleRate),
					PlaybackSpeed:         1.0,
					RuntimeEnvironment:    voicepb.RuntimeEnvironment_RUNTIME_ENVIRONMENT_ELECTRON,
					Backend:               voicepb.BackendType_BACKEND_TYPE_CLOUD,
					ClientVersion:         clientVer,
					LanguageServerVersion: lsVer,
					SpeakProgress:         true,
				},
			},
		},
	}

	if err := stream.SendMsg(setupReq); err != nil {
		if cleanupFn != nil {
			cleanupFn()
		}
		cancel()
		if fallback != nil {
			return fallback.CreateSession(ctx, cfg)
		}
		return nil, fmt.Errorf("aicode live: send setup failed: %w", err)
	}

	sess := &AICodeLiveSession{
		ctx:      sessionCtx,
		cancel:   cancel,
		cfg:      cfg,
		stream:   stream,
		cleanup:  cleanupFn,
		fallback: fallback,
		eventsCh: make(chan *LiveServerEvent, 64),
		closedCh: make(chan struct{}),
		earlyBuf: make([][]byte, 0, 8),
	}

	go sess.readLoop()

	return sess, nil
}

// AICodeLiveSession represents an active full-duplex session with AICode StreamVoice.
type AICodeLiveSession struct {
	ctx             context.Context
	cancel          context.CancelFunc
	cfg             LiveSessionConfig
	stream          AICodeStream
	cleanup         func()
	fallback        LiveBackend
	fallbackSession LiveSession

	mu       sync.Mutex
	writeMu  sync.Mutex
	closed   bool
	closeErr error
	closedCh chan struct{}
	eventsCh chan *LiveServerEvent

	// earlyBuf retains initial audio chunks in case fallback activation is required.
	earlyBuf     [][]byte
	fallbackOnce sync.Once
}

func (s *AICodeLiveSession) readLoop() {
	var fallbackActive bool
	defer func() {
		if fallbackActive {
			return
		}
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			close(s.closedCh)
		}
		s.mu.Unlock()
	}()

	isFirst := true

	for {
		var resp voicepb.StreamVoiceResponse
		err := s.stream.RecvMsg(&resp)
		if err != nil {
			if isFirst && s.fallback != nil {
				// Immediate auth or permission error on initial receive: activate fallback session
				if s.tryActivateFallback(err) {
					fallbackActive = true
					return
				}
			}
			s.closeWithError(err)
			return
		}

		isFirst = false
		s.mu.Lock()
		s.earlyBuf = nil // clear early buffer once first packet is confirmed
		s.mu.Unlock()

		msg := resp.GetMessage()
		if msg == nil {
			continue
		}

		ev := &LiveServerEvent{}
		hasEvent := false

		if audio := msg.GetVoiceAudio(); len(audio) > 0 {
			ev.Audio = audio
			hasEvent = true
		}
		if transcript := msg.GetVoiceTranscript(); transcript != nil && transcript.Text != "" {
			ev.VoiceTranscript = transcript.Text
			hasEvent = true
		}
		if speech := msg.GetUserSpeech(); speech != nil && speech.Text != "" {
			ev.UserSpeech = speech.Text
			ev.Interim = speech.Interim
			hasEvent = true
		}
		if clear := msg.GetClearAudio(); clear != nil {
			ev.Interrupted = true
			hasEvent = true
		}
		if status := msg.GetStatus(); status != nil {
			if status.State == voicepb.VoiceServerMessage_SessionStatus_STOPPING ||
				status.State == voicepb.VoiceServerMessage_SessionStatus_FAILED {
				if status.Message != "" {
					s.closeWithError(fmt.Errorf("voice session status: %s (%v)", status.Message, status.FailureReason))
					return
				}
			}
		}

		if hasEvent {
			select {
			case s.eventsCh <- ev:
			case <-s.closedCh:
				return
			case <-s.ctx.Done():
				return
			}
		}
	}
}

// tryActivateFallback switches the session over to the fallback backend if upstream rejected.
func (s *AICodeLiveSession) tryActivateFallback(upstreamErr error) bool {
	var activated bool
	s.fallbackOnce.Do(func() {
		fbSess, err := s.fallback.CreateSession(s.ctx, s.cfg)
		if err != nil {
			return
		}

		s.mu.Lock()
		s.fallbackSession = fbSess
		buf := s.earlyBuf
		s.earlyBuf = nil
		s.mu.Unlock()

		// Replay buffered early mic audio to fallback session
		for _, chunk := range buf {
			_ = fbSess.SendAudio(chunk)
		}

		// Forward events from fallback session to caller
		go func() {
			for {
				ev, err := fbSess.Receive()
				if err != nil {
					s.closeWithError(err)
					return
				}
				select {
				case s.eventsCh <- ev:
				case <-s.closedCh:
					return
				case <-s.ctx.Done():
					return
				}
			}
		}()

		activated = true
	})

	return activated
}

// SendAudio sends a chunk of raw 16kHz 16-bit PCM mono audio.
func (s *AICodeLiveSession) SendAudio(pcm16k []byte) error {
	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return io.ErrClosedPipe
	}

	if s.fallbackSession != nil {
		fb := s.fallbackSession
		s.mu.Unlock()
		return fb.SendAudio(pcm16k)
	}

	// Buffer initial chunks until first successful receive confirms backend availability
	if len(s.earlyBuf) < 16 {
		chunkCopy := make([]byte, len(pcm16k))
		copy(chunkCopy, pcm16k)
		s.earlyBuf = append(s.earlyBuf, chunkCopy)
	}
	s.mu.Unlock()

	req := &voicepb.StreamVoiceRequest{
		Message: &voicepb.VoiceClientMessage{
			Kind: &voicepb.VoiceClientMessage_MicAudio{
				MicAudio: pcm16k,
			},
		},
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.stream.SendMsg(req)
}

// SendText injects a user transcript / text into the live session.
func (s *AICodeLiveSession) SendText(text string) error {
	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return io.ErrClosedPipe
	}

	if s.fallbackSession != nil {
		fb := s.fallbackSession
		s.mu.Unlock()
		return fb.SendText(text)
	}
	s.mu.Unlock()

	req := &voicepb.StreamVoiceRequest{
		Message: &voicepb.VoiceClientMessage{
			Kind: &voicepb.VoiceClientMessage_UserTranscript_{
				UserTranscript: &voicepb.VoiceClientMessage_UserTranscript{
					Text:    text,
					IsFinal: true,
				},
			},
		},
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.stream.SendMsg(req)
}

// SendToolResponse sends the result of a tool execution back into the session.
func (s *AICodeLiveSession) SendToolResponse(id string, name string, result any) error {
	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return io.ErrClosedPipe
	}

	if s.fallbackSession != nil {
		fb := s.fallbackSession
		s.mu.Unlock()
		return fb.SendToolResponse(id, name, result)
	}
	s.mu.Unlock()

	// Represent tool return as text update for Talker model
	resBytes, _ := json.Marshal(result)
	msg := fmt.Sprintf("[Tool %s completed: %s]", name, string(resBytes))
	return s.SendText(msg)
}

// SendThinkerNote sends an asynchronous progress note from the Thinker agent to the Talker model.
func (s *AICodeLiveSession) SendThinkerNote(note string) error {
	s.mu.Lock()
	if s.fallbackSession != nil {
		fb := s.fallbackSession
		s.mu.Unlock()
		return fb.SendThinkerNote(note)
	}
	s.mu.Unlock()

	prompt := fmt.Sprintf("[Thinker Note: %s. Briefly update the user with natural speech if helpful.]", note)
	return s.SendText(prompt)
}

// SendThinkerAnswer delivers the finalized answer from the Thinker agent to the Talker model.
func (s *AICodeLiveSession) SendThinkerAnswer(answer string) error {
	s.mu.Lock()
	if s.fallbackSession != nil {
		fb := s.fallbackSession
		s.mu.Unlock()
		return fb.SendThinkerAnswer(answer)
	}
	s.mu.Unlock()

	prompt := fmt.Sprintf("[Thinker Finished: %s. Please summarize and speak the result to the user.]", answer)
	return s.SendText(prompt)
}

// Receive waits for and returns the next LiveServerEvent.
func (s *AICodeLiveSession) Receive() (*LiveServerEvent, error) {
	// First check if an event is already queued
	select {
	case ev, ok := <-s.eventsCh:
		if ok {
			return ev, nil
		}
	default:
	}

	select {
	case ev, ok := <-s.eventsCh:
		if !ok {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.closeErr != nil {
				return nil, s.closeErr
			}
			return nil, io.EOF
		}
		return ev, nil
	case <-s.ctx.Done():
		select {
		case ev, ok := <-s.eventsCh:
			if ok {
				return ev, nil
			}
		default:
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closeErr != nil {
			return nil, s.closeErr
		}
		return nil, s.ctx.Err()
	case <-s.closedCh:
		select {
		case ev, ok := <-s.eventsCh:
			if ok {
				return ev, nil
			}
		default:
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closeErr != nil {
			return nil, s.closeErr
		}
		return nil, io.EOF
	}
}

// Close terminates the live voice session.
func (s *AICodeLiveSession) Close() error {
	return s.closeWithError(nil)
}

func (s *AICodeLiveSession) closeWithError(err error) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if err != nil && s.closeErr == nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		s.closeErr = err
	}
	fb := s.fallbackSession
	s.mu.Unlock()

	s.cancel()

	if s.cleanup != nil {
		s.cleanup()
	}

	if fb != nil {
		_ = fb.Close()
	}

	return nil
}
