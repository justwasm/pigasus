package voice

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/endpoints"
	"google.golang.org/genai"
)

const (
	// DefaultGeminiLiveEndpoint is the public WebSocket endpoint for Gemini Multimodal Live.
	DefaultGeminiLiveEndpoint = endpoints.DefaultVoice

	// DefaultGeminiLiveModel is the standard multimodal live model.
	DefaultGeminiLiveModel = "models/gemini-3.8-live"
)

// VoicePersona represents a voice option in Antigravity / Pigasus.
type VoicePersona struct {
	Name        string
	Description string
	Tone        string
	GeminiVoice string
}

// CanonicalVoicePersonas lists the 9 canonical Antigravity personas.
var CanonicalVoicePersonas = []VoicePersona{
	{Name: "Fola", Description: "Clear and friendly (Default)", Tone: "Female", GeminiVoice: "Aoede"},
	{Name: "Arlo", Description: "Confident and clear", Tone: "Male", GeminiVoice: "Puck"},
	{Name: "Cleo", Description: "Warm and engaging", Tone: "Female", GeminiVoice: "Kore"},
	{Name: "Finn", Description: "Warm and approachable", Tone: "Male", GeminiVoice: "Fenrir"},
	{Name: "Gero", Description: "Clear and friendly", Tone: "Male", GeminiVoice: "Puck"},
	{Name: "Lora", Description: "Clear and friendly", Tone: "Female", GeminiVoice: "Aoede"},
	{Name: "Neno", Description: "Deep and velvety", Tone: "Male", GeminiVoice: "Charon"},
	{Name: "Olin", Description: "Light and precise", Tone: "Male", GeminiVoice: "Fenrir"},
	{Name: "Tari", Description: "Confident and clear", Tone: "Female", GeminiVoice: "Kore"},
}

// GeminiPrebuiltVoices lists direct prebuilt voices supported by Gemini Live API.
var GeminiPrebuiltVoices = []string{
	"Aoede", "Puck", "Kore", "Fenrir", "Charon", "Zephyr", "Leda", "Orus",
}

// MapVoiceName maps Antigravity voice personas (Fola, Arlo, Cleo, Finn, etc.)
// and direct Gemini voice names (Aoede, Puck, Kore, Fenrir, Charon, Zephyr, etc.)
// to the canonical Gemini Live prebuilt voice identifier.
func MapVoiceName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "fola", "lora", "aoede":
		return "Aoede"
	case "arlo", "gero", "puck":
		return "Puck"
	case "cleo", "tari", "kore":
		return "Kore"
	case "finn", "olin", "fenrir":
		return "Fenrir"
	case "neno", "charon":
		return "Charon"
	case "zephyr":
		return "Zephyr"
	case "leda":
		return "Leda"
	case "orus":
		return "Orus"
	default:
		return "Aoede"
	}
}

// FindVoicePersona looks up a voice persona by name (Antigravity persona or Gemini voice).
func FindVoicePersona(name string) (VoicePersona, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, p := range CanonicalVoicePersonas {
		if strings.ToLower(p.Name) == lower {
			return p, true
		}
	}
	for _, g := range GeminiPrebuiltVoices {
		if strings.ToLower(g) == lower {
			return VoicePersona{
				Name:        g,
				Description: "Gemini Live Prebuilt Voice",
				GeminiVoice: g,
			}, true
		}
	}
	return VoicePersona{}, false
}

// GeminiLiveBackend implements LiveBackend backed by Google Gemini Multimodal Live API
// using the official google.golang.org/genai SDK.
type GeminiLiveBackend struct {
	apiKey      string
	bearerToken string
	endpoint    string
	model       string
}

// NewGeminiLiveBackend creates a GeminiLiveBackend with the given API key.
func NewGeminiLiveBackend(apiKey string) *GeminiLiveBackend {
	model := endpoints.CurrentGeminiLiveModel()
	if model == "" {
		model = DefaultGeminiLiveModel
	}
	return &GeminiLiveBackend{
		apiKey:   apiKey,
		endpoint: endpoints.Current().Voice,
		model:    model,
	}
}

// NewGeminiLiveBackendWithBearer creates a GeminiLiveBackend using an OAuth Bearer token.
func NewGeminiLiveBackendWithBearer(bearerToken string) *GeminiLiveBackend {
	model := endpoints.CurrentGeminiLiveModel()
	if model == "" {
		model = DefaultGeminiLiveModel
	}
	return &GeminiLiveBackend{
		bearerToken: bearerToken,
		endpoint:    endpoints.Current().Voice,
		model:       model,
	}
}

// SetBearerToken sets or updates the OAuth Bearer token.
func (b *GeminiLiveBackend) SetBearerToken(token string) {
	b.bearerToken = token
}

// BearerToken returns the current OAuth Bearer token, if configured.
func (b *GeminiLiveBackend) BearerToken() string {
	return b.bearerToken
}

// APIKey returns the current Gemini API key, if configured.
func (b *GeminiLiveBackend) APIKey() string {
	return b.apiKey
}

// SetEndpoint overrides the WebSocket endpoint.
func (b *GeminiLiveBackend) SetEndpoint(ep string) {
	b.endpoint = ep
}

// SetModel overrides the default live model.
func (b *GeminiLiveBackend) SetModel(model string) {
	b.model = model
}

// CreateSession establishes a live full-duplex session with Gemini Live API using google.golang.org/genai.
func (b *GeminiLiveBackend) CreateSession(ctx context.Context, cfg LiveSessionConfig) (LiveSession, error) {
	key := cfg.APIKey
	if key == "" {
		key = b.apiKey
	}
	if key == "" && b.bearerToken != "" {
		key = b.bearerToken
	}

	endpoint := b.endpoint
	if cfg.Endpoint != "" {
		endpoint = cfg.Endpoint
	}

	if key == "" && (endpoint == "" || endpoint == endpoints.DefaultVoice) {
		return nil, fmt.Errorf("gemini live: API key or OAuth bearer token is required")
	}

	clientCfg := &genai.ClientConfig{
		APIKey:  key,
		Backend: genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{
			APIVersion: "v1alpha",
		},
	}

	if b.bearerToken != "" {
		if clientCfg.HTTPOptions.Headers == nil {
			clientCfg.HTTPOptions.Headers = make(map[string][]string)
		}
		clientCfg.HTTPOptions.Headers["Authorization"] = []string{"Bearer " + b.bearerToken}
	}

	if endpoint != "" && endpoint != endpoints.DefaultVoice {
		clientCfg.HTTPOptions.BaseURL = endpoint
	}

	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("gemini live: failed to create genai client: %w", err)
	}

	voiceName := MapVoiceName(cfg.Voice)
	modelName := b.model
	if cfg.Model != "" {
		modelName = cfg.Model
	}
	if !strings.HasPrefix(modelName, "models/") {
		modelName = "models/" + modelName
	}

	liveCfg := &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
		SpeechConfig: &genai.SpeechConfig{
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
					VoiceName: voiceName,
				},
			},
		},
		InputAudioTranscription:  &genai.AudioTranscriptionConfig{},
		OutputAudioTranscription: &genai.AudioTranscriptionConfig{},
		Tools: []*genai.Tool{
			{
				FunctionDeclarations: []*genai.FunctionDeclaration{
					{
						Name:        "search_web",
						Description: "Search the web for real-time information, documentation, news, or general web queries.",
						Parameters: &genai.Schema{
							Type: genai.TypeObject,
							Properties: map[string]*genai.Schema{
								"query": {
									Type:        genai.TypeString,
									Description: "The search query string to look up.",
								},
								"domain": {
									Type:        genai.TypeString,
									Description: "Optional specific website domain to filter results (e.g. 'go.dev', 'github.com').",
								},
							},
							Required: []string{"query"},
						},
					},
					{
						Name:        "fetch_url",
						Description: "Fetch the text or markdown content of an HTTP or HTTPS web URL from the internet (e.g. GitHub changelogs, web documentation, release notes, APIs).",
						Parameters: &genai.Schema{
							Type: genai.TypeObject,
							Properties: map[string]*genai.Schema{
								"url": {
									Type:        genai.TypeString,
									Description: "The full HTTP or HTTPS URL to fetch.",
								},
							},
							Required: []string{"url"},
						},
					},
					{
						Name:        "run_command",
						Description: "Execute a shell command in the local workspace terminal (e.g. 'cal', 'curl -s https://...', 'git status', 'ls -la', 'go test'). Returns stdout and stderr.",
						Parameters: &genai.Schema{
							Type: genai.TypeObject,
							Properties: map[string]*genai.Schema{
								"command": {
									Type:        genai.TypeString,
									Description: "The exact command string to execute in the shell.",
								},
							},
							Required: []string{"command"},
						},
					},
					{
						Name:        "read_file",
						Description: "Read the contents of a local file in the workspace.",
						Parameters: &genai.Schema{
							Type: genai.TypeObject,
							Properties: map[string]*genai.Schema{
								"path": {
									Type:        genai.TypeString,
									Description: "Relative or absolute path of the file to read.",
								},
							},
							Required: []string{"path"},
						},
					},
					{
						Name:        "delegate_to_thinker",
						Description: "Delegate a complex coding, software engineering, terminal execution, file inspection, or debugging task to the background Thinker agent.",
						Parameters: &genai.Schema{
							Type: genai.TypeObject,
							Properties: map[string]*genai.Schema{
								"task": {
									Type:        genai.TypeString,
									Description: "Detailed and actionable task description for the Thinker agent.",
								},
							},
							Required: []string{"task"},
						},
					},
				},
			},
		},
	}

	if strings.Contains(modelName, "extended-thinking") {
		liveCfg.ThinkingConfig = &genai.ThinkingConfig{
			ThinkingBudget: genai.Ptr(int32(1024)),
		}
	}

	if cfg.SystemPrompt != "" {
		liveCfg.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{
				{Text: cfg.SystemPrompt},
			},
		}
	}

	sessCtx, cancel := context.WithCancel(ctx)
	session, err := client.Live.Connect(sessCtx, modelName, liveCfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gemini live: failed to connect: %w", err)
	}

	sess := &GeminiLiveSession{
		session:  session,
		ctx:      sessCtx,
		cancel:   cancel,
		eventsCh: make(chan *LiveServerEvent, 64),
		closedCh: make(chan struct{}),
	}

	go sess.readLoop()
	return sess, nil
}

// GeminiLiveSession manages the WebSocket connection with Gemini Live using google.golang.org/genai.
type GeminiLiveSession struct {
	session  *genai.Session
	ctx      context.Context
	cancel   context.CancelFunc
	writeMu  sync.Mutex
	eventsCh chan *LiveServerEvent
	closedCh chan struct{}
	closed   bool
	closeErr error
	mu       sync.Mutex
}

func (s *GeminiLiveSession) readLoop() {
	var readErr error
	defer func() {
		_ = s.closeWithError(readErr)
	}()

	for {
		msg, err := s.session.Receive()
		if err != nil {
			readErr = err
			return
		}

		var toolCalls []LiveToolCall
		if msg.ToolCall != nil && len(msg.ToolCall.FunctionCalls) > 0 {
			for _, fc := range msg.ToolCall.FunctionCalls {
				toolCalls = append(toolCalls, LiveToolCall{
					ID:   fc.ID,
					Name: fc.Name,
					Args: fc.Args,
				})
			}
		}

		if msg.ServerContent != nil {
			sc := msg.ServerContent
			if sc.Interrupted {
				select {
				case s.eventsCh <- &LiveServerEvent{Interrupted: true}:
				case <-s.closedCh:
					return
				}
			}

			hasPartText := false
			if sc.ModelTurn != nil {
				for _, part := range sc.ModelTurn.Parts {
					if part.FunctionCall != nil {
						toolCalls = append(toolCalls, LiveToolCall{
							ID:   part.FunctionCall.ID,
							Name: part.FunctionCall.Name,
							Args: part.FunctionCall.Args,
						})
					}
					if part.Text != "" {
						hasPartText = true
						select {
						case s.eventsCh <- &LiveServerEvent{VoiceTranscript: part.Text}:
						case <-s.closedCh:
							return
						}
					}
					if part.InlineData != nil && len(part.InlineData.Data) > 0 {
						select {
						case s.eventsCh <- &LiveServerEvent{Audio: part.InlineData.Data}:
						case <-s.closedCh:
							return
						}
					}
				}
			}

			if sc.InputTranscription != nil && sc.InputTranscription.Text != "" {
				select {
				case s.eventsCh <- &LiveServerEvent{UserSpeech: sc.InputTranscription.Text, Interim: false}:
				case <-s.closedCh:
					return
				}
			}

			if sc.InterimInputTranscription != nil && sc.InterimInputTranscription.Text != "" {
				select {
				case s.eventsCh <- &LiveServerEvent{UserSpeech: sc.InterimInputTranscription.Text, Interim: true}:
				case <-s.closedCh:
					return
				}
			}

			if sc.OutputTranscription != nil && sc.OutputTranscription.Text != "" && !hasPartText {
				select {
				case s.eventsCh <- &LiveServerEvent{VoiceTranscript: sc.OutputTranscription.Text}:
				case <-s.closedCh:
					return
				}
			}

			if sc.TurnComplete {
				select {
				case s.eventsCh <- &LiveServerEvent{TurnComplete: true}:
				case <-s.closedCh:
					return
				}
			}
		}

		if len(toolCalls) > 0 {
			select {
			case s.eventsCh <- &LiveServerEvent{ToolCalls: toolCalls}:
			case <-s.closedCh:
				return
			}
		}
	}
}

// SendAudio sends a chunk of raw 16kHz 16-bit PCM mono audio.
func (s *GeminiLiveSession) SendAudio(pcm16k []byte) error {
	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return io.ErrClosedPipe
	}
	s.mu.Unlock()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.session.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{
			MIMEType: "audio/pcm;rate=16000",
			Data:     pcm16k,
		},
	})
}

// SendText sends text turns to Gemini Live.
func (s *GeminiLiveSession) SendText(text string) error {
	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return io.ErrClosedPipe
	}
	s.mu.Unlock()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.session.SendClientContent(genai.LiveClientContentInput{
		Turns: []*genai.Content{
			{
				Role: genai.RoleUser,
				Parts: []*genai.Part{
					{Text: text},
				},
			},
		},
		TurnComplete: genai.Ptr(true),
	})
}

// SendToolResponse sends the execution result of a function/tool back to Gemini Live.
func (s *GeminiLiveSession) SendToolResponse(id string, name string, result any) error {
	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return io.ErrClosedPipe
	}
	s.mu.Unlock()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.session.SendToolResponse(genai.LiveToolResponseInput{
		FunctionResponses: []*genai.FunctionResponse{
			{
				ID:   id,
				Name: name,
				Response: map[string]any{
					"output": result,
				},
			},
		},
	})
}

// SendThinkerNote sends an asynchronous progress note from the Thinker agent to the Talker model.
func (s *GeminiLiveSession) SendThinkerNote(note string) error {
	prompt := fmt.Sprintf("[Thinker Note: %s. Briefly update the user with natural speech if helpful.]", note)
	return s.SendText(prompt)
}

// SendThinkerAnswer delivers the finalized answer from the Thinker agent to the Talker model.
func (s *GeminiLiveSession) SendThinkerAnswer(answer string) error {
	prompt := fmt.Sprintf("[Thinker Finished: %s. Please summarize and speak the result to the user.]", answer)
	return s.SendText(prompt)
}

// Receive returns the next LiveServerEvent.
func (s *GeminiLiveSession) Receive() (*LiveServerEvent, error) {
	select {
	case <-s.ctx.Done():
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closeErr != nil {
			return nil, s.closeErr
		}
		return nil, s.ctx.Err()
	case <-s.closedCh:
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closeErr != nil {
			return nil, s.closeErr
		}
		return nil, io.EOF
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
	}
}

func (s *GeminiLiveSession) closeWithError(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	if err != nil && s.closeErr == nil {
		s.closeErr = err
	}
	s.cancel()
	close(s.closedCh)
	return s.session.Close()
}

// Close closes the session.
func (s *GeminiLiveSession) Close() error {
	return s.closeWithError(nil)
}
