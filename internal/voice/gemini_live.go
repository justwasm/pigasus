package voice

import (
	"context"
	"fmt"
	"io"
	"net/url"
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

// NewGeminiLiveBackendWithEndpoint creates a GeminiLiveBackend pointing at a custom endpoint.
func NewGeminiLiveBackendWithEndpoint(apiKey string, endpoint string) *GeminiLiveBackend {
	b := NewGeminiLiveBackend(apiKey)
	if endpoint != "" {
		b.SetEndpoint(endpoint)
	}
	return b
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

// Endpoint returns the current WebSocket endpoint.
func (b *GeminiLiveBackend) Endpoint() string {
	return b.endpoint
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

	isCustomProxy := endpoint != "" && endpoint != endpoints.DefaultVoice
	if key == "" && !isCustomProxy {
		return nil, fmt.Errorf("gemini live: API key or OAuth bearer token is required")
	}
	// For custom local proxies (e.g. pigasus proxy), supply a placeholder key if none is given,
	// because google.golang.org/genai requires a non-empty APIKey to construct the WebSocket URL.
	if key == "" && isCustomProxy {
		key = "alt-backend"
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

	if isCustomProxy {
		// Clean and normalize endpoint for genai:
		// 1. Strip the /ws/... RPC suffix because genai's Live.Connect appends it internally.
		// 2. Ensure scheme is ws:// (if http/ws) or wss:// (if https/wss) so genai does not force TLS on local loopback proxies.
		cleaned := endpoint
		if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
			scheme := "ws"
			if u.Scheme == "https" || u.Scheme == "wss" {
				scheme = "wss"
			}
			p := u.Path
			if idx := strings.Index(p, "/ws/"); idx >= 0 {
				p = p[:idx]
			}
			u.Scheme = scheme
			u.Path = p
			cleaned = u.String()
		}
		clientCfg.HTTPOptions.BaseURL = cleaned
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
				FunctionDeclarations: geminiLiveFunctionDeclarationsForConfig(cfg),
			},
		},
	}

	if strings.Contains(modelName, "extended-thinking") {
		liveCfg.ThinkingConfig = &genai.ThinkingConfig{
			ThinkingLevel:  genai.ThinkingLevelLow,
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

			if sc.ModelTurn != nil {
				for _, part := range sc.ModelTurn.Parts {
					if part.FunctionCall != nil {
						toolCalls = append(toolCalls, LiveToolCall{
							ID:   part.FunctionCall.ID,
							Name: part.FunctionCall.Name,
							Args: part.FunctionCall.Args,
						})
					}
					if part.Text != "" && !part.Thought {
						select {
						case s.eventsCh <- &LiveServerEvent{VoiceTranscript: part.Text}:
						case <-s.closedCh:
							return
						}
					}
					if part.AudioTranscription != nil && part.AudioTranscription.Text != "" {
						select {
						case s.eventsCh <- &LiveServerEvent{VoiceTranscript: part.AudioTranscription.Text}:
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

			if sc.OutputTranscription != nil && sc.OutputTranscription.Text != "" {
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
	prompt := fmt.Sprintf("[task progress] %s", note)
	return s.SendText(prompt)
}

// SendThinkerAnswer delivers the finalized answer from the Thinker agent to the Talker model.
func (s *GeminiLiveSession) SendThinkerAnswer(answer string) error {
	prompt := fmt.Sprintf("[answer] %s", answer)
	return s.SendText(prompt)
}

// SendWaitingOnUser sends a prompt asking the user for confirmation or input.
func (s *GeminiLiveSession) SendWaitingOnUser(question string) error {
	prompt := fmt.Sprintf("[waiting on the user] %s", question)
	return s.SendText(prompt)
}

// SendTaskStopped notifies the live talker that the active thinker task was stopped.
func (s *GeminiLiveSession) SendTaskStopped() error {
	return s.SendText("[stopped]")
}

// SendMedia sends a visual image or video frame into the live session.
func (s *GeminiLiveSession) SendMedia(mimeType string, data []byte) error {
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
		Media: &genai.Blob{
			MIMEType: mimeType,
			Data:     data,
		},
	})
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

// LiveToolSpec defines a single-source specification for a Gemini Live function tool,
// pairing its wire FunctionDeclaration with its system-prompt usage hint.
type LiveToolSpec struct {
	Name        string
	PromptHint  string
	DirectOnly  bool
	Declaration *genai.FunctionDeclaration
}

var geminiLiveToolSpecs = []LiveToolSpec{
	{
		Name:       "call_thinker_agent",
		PromptHint: "Call this for complex multi-step coding or analysis tasks.",
		DirectOnly: false,
		Declaration: &genai.FunctionDeclaration{
			Name:        "call_thinker_agent",
			Description: "Tool for code, files, profile/desk, calendar/reminders, docs/chat, pricing, weather, web, accounts, screen/links. Never call on cues ([task progress], [waiting on the user], answer/error, [stopped]/[stop failed], [The user just sent in chat]), open [waiting on the user] (questions about the choice, constraints, approvals), or local tools; skip chitchat, thanks, remarks, declines, resuming speech, mic tests, settings, omitted-part reads, and fresh-call status. Otherwise never refuse, say cannot/unable, cite missing tools/access, or repeat refusals on pushback/retries/calendar/desk/link/how-to asks (commit and call, even on open calls), and call on context-covered asks needing detail. Speak first (no agent, thinker, or tool mentions): verified fact (no UUIDs/guesses/general knowledge/refusals) in two sentences (present tense, subject first, no context note, plus varied 'I'll' commitment), else one warm 'I'll' sentence (never 'I will'/'I am') varying clause ('To <user goal>, I'll...', 'I'll' + diverse verb + target without 'the', or brief ack + 'I'll...') and verb (no -ing openers, execution verbs, promises, vague pronouns; edits/reminders/sends/approvals as lookups; pushback: ONLY one positive commitment), and emit the call same turn right after speaking (never narrating it, silent, stopping, or uncalled).",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"query": {
						Type:        genai.TypeString,
						Description: "Full request with context details, lookups read-only.",
					},
				},
				Required: []string{"query"},
			},
		},
	},
	{
		Name:       "cancel_thinker_agent",
		PromptHint: "Call this to cancel an active background Thinker task when the user asks to stop or abort it.",
		DirectOnly: false,
		Declaration: &genai.FunctionDeclaration{
			Name:        "cancel_thinker_agent",
			Description: "Cancels the active task. Call immediately silently with zero words beforehand only to cancel, drop, or ignore the entire running task (never on a bare stop, wait, or hold-on request; a stop that names the task or its work does cancel; to stop one branch, narrow, or redirect work, call the thinker instead; to stop updates, stop repeating, or stay quiet until done, call set_progress_updates with enabled=false instead so the task keeps running); confirm in future tense on return, stopped only on [stopped].",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
			},
		},
	},
	{
		Name:       "set_progress_updates",
		PromptHint: "Call this to enable or disable spoken progress updates for an active background task without cancelling it.",
		DirectOnly: false,
		Declaration: &genai.FunctionDeclaration{
			Name:        "set_progress_updates",
			Description: "Enables or disables spoken progress updates for the active background task without cancelling it.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"enabled": {
						Type:        genai.TypeBoolean,
						Description: "Whether spoken progress updates are enabled.",
					},
				},
				Required: []string{"enabled"},
			},
		},
	},
	{
		Name:       "hang_up",
		PromptHint: "Call this to end the voice call when the user explicitly asks to hang up or says goodbye.",
		DirectOnly: false,
		Declaration: &genai.FunctionDeclaration{
			Name:        "hang_up",
			Description: "Ends the voice call and hangs up. Call with no arguments only on explicit hang-up requests, clear end-of-call farewells, or an [idle] note that says to hang up, not mid-call thanks: speak one brief farewell first in the same turn, call this function, and stay silent afterward. Active chat tasks keep running.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
			},
		},
	},
	{
		Name:       "search_web",
		PromptHint: "Call this to search the web for up-to-date information, documentation, news, or general web queries.",
		DirectOnly: true,
		Declaration: &genai.FunctionDeclaration{
			Name:        "search_web",
			Description: "Search the web for up-to-date information, definitions, documentation, news, or general knowledge queries.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"query": {
						Type:        genai.TypeString,
						Description: "The search query string.",
					},
					"domain": {
						Type:        genai.TypeString,
						Description: "Optional domain filter (e.g. github.com).",
					},
				},
				Required: []string{"query"},
			},
		},
	},
	{
		Name:       "fetch_url",
		PromptHint: "Call this whenever the user gives an HTTP/HTTPS link or asks to read documentation, GitHub files, or changelogs.",
		DirectOnly: true,
		Declaration: &genai.FunctionDeclaration{
			Name:        "fetch_url",
			Description: "Fetch and read the text content of an HTTP or HTTPS URL (documentation, GitHub files, changelogs, web pages).",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"url": {
						Type:        genai.TypeString,
						Description: "The HTTP or HTTPS URL to fetch.",
					},
				},
				Required: []string{"url"},
			},
		},
	},
	{
		Name:       "run_command",
		PromptHint: "Call this whenever the user asks to run terminal commands (e.g. 'cal', 'curl', 'git status', 'ls -la', 'go test'). Pass the exact command string.",
		DirectOnly: true,
		Declaration: &genai.FunctionDeclaration{
			Name:        "run_command",
			Description: "Execute a shell command in the workspace terminal and return its output.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"command": {
						Type:        genai.TypeString,
						Description: "The exact shell command to execute.",
					},
				},
				Required: []string{"command"},
			},
		},
	},
	{
		Name:       "read_file",
		PromptHint: "Call this to inspect local repository files.",
		DirectOnly: true,
		Declaration: &genai.FunctionDeclaration{
			Name:        "read_file",
			Description: "Read the contents of a file in the local workspace.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"path": {
						Type:        genai.TypeString,
						Description: "Path to the file to read.",
					},
				},
				Required: []string{"path"},
			},
		},
	},
}

func geminiLiveFunctionDeclarationsForConfig(cfg LiveSessionConfig) []*genai.FunctionDeclaration {
	decls := make([]*genai.FunctionDeclaration, 0, len(geminiLiveToolSpecs))
	for _, spec := range geminiLiveToolSpecs {
		if spec.DirectOnly && !cfg.DirectTools {
			continue
		}
		decls = append(decls, spec.Declaration)
	}
	return decls
}

func geminiLiveFunctionDeclarations() []*genai.FunctionDeclaration {
	return geminiLiveFunctionDeclarationsForConfig(LiveSessionConfig{DirectTools: false})
}

// GeminiLiveToolNames returns the declared Gemini Live tool names in declaration order.
func GeminiLiveToolNames(directTools bool) []string {
	decls := geminiLiveFunctionDeclarationsForConfig(LiveSessionConfig{DirectTools: directTools})
	names := make([]string, 0, len(decls))
	for _, d := range decls {
		names = append(names, d.Name)
	}
	return names
}

// DirectToolPromptLines returns formatted system-prompt bullet lines generated from
// the single-source Gemini Live tool specification for DirectTools: true.
func DirectToolPromptLines() string {
	var lines []string
	for _, spec := range geminiLiveToolSpecs {
		hint := strings.TrimSpace(spec.PromptHint)
		if hint == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("   - '%s': %s", spec.Name, hint))
	}
	return strings.Join(lines, "\n")
}
