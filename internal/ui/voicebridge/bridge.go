// Package voicebridge owns the voice session state machine that used to sit
// on the TUI's RootModel: the live-session lifecycle, dictation capture,
// microphone mute and the Thinker hand-off bookkeeping.
//
// The module is host-free in the same sense as the composer's selection
// module. It never imports Bubble Tea or the TUI host: every side effect it
// needs — reporting progress, writing the composer, recording the transcript,
// dialing the microphone, resolving credentials — arrives as a narrow port or
// an injectable function whose default preserves the shipped behaviour.
//
// The host (internal/tui) keeps the effects it alone can perform and
// delegates the rules here; the tests drive the same rules with fakes.
package voicebridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/infra/livevoice"
	"github.com/justwasm/pigasus/internal/rcaudio"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
	voicebackend "github.com/justwasm/pigasus/internal/voice"
)

// DefaultVoice is the persona used when the session carries none.
const DefaultVoice = "Finn"

// ProgressSink receives the live-session events the host renders.
//
// The module never touches a program or a transcript row: it describes what
// happened and the host decides where it is painted.
type ProgressSink interface {
	// UserSpeechInterim carries the in-flight recognition preview.
	UserSpeechInterim(text string)
	// UserSpeech carries a committed user utterance.
	UserSpeech(text string)
	// AgentSpeech carries the agent's complete spoken reply.
	AgentSpeech(text string)
	// AgentSpeechDelta carries one streamed chunk of the reply.
	AgentSpeechDelta(delta string)
	// Interrupted reports that the agent was barged into.
	Interrupted()
	// ThinkerHandoff reports a task handed to the Thinker agent.
	ThinkerHandoff(invocationID, task string)
	// ThinkerDone reports a Thinker turn's terminal state. A non-nil err
	// means the turn failed; answer is then empty.
	ThinkerDone(invocationID, answer string, err error)
	// LiveError echoes a live-session failure into the conversation.
	LiveError(err error)
}

// InputEditor is the composer port the bridge writes drafts into.
type InputEditor interface {
	Value() string
	SetValue(string)
	InsertString(string)
}

// SessionPort is the transcript port the bridge reads and appends to.
type SessionPort interface {
	Messages() []backend.Message
	AppendMessage(msg backend.Message)
}

// LiveVoiceRouter is the subset of the session's live router the bridge
// installs. The session's own router satisfies it.
type LiveVoiceRouter interface {
	IsActive() bool
	SendText(text string) error
}

// ThinkerRunner executes one Thinker turn and returns its final answer.
type ThinkerRunner func(ctx context.Context, task string, onProgress func(string)) (string, error)

// LiveSession is the live-call session the bridge drives. The concrete
// *voice.NativeLiveSession satisfies it.
type LiveSession interface {
	LiveVoiceRouter
	Start(ctx context.Context, cfg voicebackend.LiveSessionConfig) error
	HangUp() error
	SetDuplex(duplex bool)
	ToggleMute() bool
	IsMuted() bool
	Speaking() bool
	IsActive() bool
	ConversationHistory() []voicebackend.LiveTurnRecord
	SetConversationHistory(turns []voicebackend.LiveTurnRecord)
	SendWaitingOnUser(question string) error
	SendTaskStopped() error
	SendThinkerNote(note string) error
	SendThinkerAnswer(answer string) error
	CompleteHandoff(id string, result any) error
}

// Transcriber turns captured PCM into text.
type Transcriber interface {
	Transcribe(ctx context.Context, audio []byte, mimeType string) (string, error)
}

// Ports are the host seams the bridge needs. Every field is optional: a nil
// port answers with the zero value, which keeps the module usable on its own.
type Ports struct {
	// Progress receives the live-session events. Optional.
	Progress ProgressSink
	// Input is the composer. Optional.
	Input InputEditor
	// Session is the transcript. Optional.
	Session SessionPort
	// SetLiveRouter installs the live router, or clears it when router is
	// nil. Optional: without it the session keeps no router.
	SetLiveRouter func(router LiveVoiceRouter)
	// Cwd is the active workspace, named in the live system prompt.
	// Optional.
	Cwd func() string
	// FallbackCwd answers the workspace when Cwd is empty; the host passes
	// os.Getwd. Leaving it nil keeps an unnamed workspace, which is what a
	// host-free caller wants.
	FallbackCwd func() string
	// RunThinker runs a Thinker turn. A nil runner makes a hand-off
	// report a failure to the sink.
	RunThinker ThinkerRunner
	// ModelLabel labels the turn model in the wire envelope. Defaults to
	// catalog.FormatModelLabel.
	ModelLabel func(string) string
}

// Env is the process environment the bridge reads. Tests replace it; the
// default reads the real one.
type Env struct {
	// MicAddr is the remote microphone server ($ANTIGRAVITY_MIC).
	MicAddr string
	// ForceAPIKey forces the Gemini API-key transport
	// ($OPENAGY_VOICE_FORCE_API_KEY).
	ForceAPIKey bool
	// AltBackend overrides the live transport
	// ($OPENAGY_VOICE_ALT_BACKEND).
	AltBackend string
	// MockVoice marks a mock live session ($PIGASUS_MOCK_VOICE or
	// $OPENAGY_MOCK_VOICE).
	MockVoice bool
}

// Config collects the injectable I/O. The zero value is usable: every
// function falls back to the shipped default, so a host that changes
// nothing keeps the previous behaviour.
type Config struct {
	// Recorder owns dictation capture. The zero Recorder uses the real
	// microphone and net.DialTimeout.
	Recorder Recorder
	// Env samples the environment. Defaults to reading the process one.
	Env func() Env
	// LoadToken resolves the unified credential. Defaults to
	// auth.LoadTokenUnified.
	LoadToken func() (*auth.AntigravityToken, error)
	// APIKey resolves the Gemini API key. Defaults to
	// endpoints.CurrentGeminiAPIKey.
	APIKey func() string
	// VoiceEndpoint is the resolved live endpoint. Defaults to
	// endpoints.Current().Voice.
	VoiceEndpoint func() string
	// DefaultVoiceEndpoint is the shipped live endpoint. Defaults to
	// endpoints.DefaultVoice.
	DefaultVoiceEndpoint func() string
	// LiveModel is the configured live model. Defaults to
	// endpoints.CurrentGeminiLiveModel.
	LiveModel func() string
	// DefaultLiveModel is the fallback live model. Defaults to
	// voicebackend.DefaultGeminiLiveModel.
	DefaultLiveModel string
	// ResolveBackend resolves the live transport. Defaults to
	// livevoice.ResolveBackend.
	ResolveBackend func(livevoice.Options) (voicebackend.LiveBackend, error)
	// NewLiveSession builds the live session. Defaults to
	// voicebackend.NewNativeLiveSession.
	NewLiveSession func(voicebackend.LiveBackend, voicebackend.NativeLiveHandler) LiveSession
	// NewTranscriber builds the dictation transcriber. Defaults to
	// rcaudio.NewCloudCodeTranscriber.
	NewTranscriber func() Transcriber
}

func (c Config) env() Env {
	if c.Env != nil {
		return c.Env()
	}
	return ReadEnv()
}

func (c Config) loadToken() (*auth.AntigravityToken, error) {
	if c.LoadToken != nil {
		return c.LoadToken()
	}
	return auth.LoadTokenUnified()
}

func (c Config) apiKey() string {
	if c.APIKey != nil {
		return c.APIKey()
	}
	return endpoints.CurrentGeminiAPIKey()
}

func (c Config) voiceEndpoint() string {
	if c.VoiceEndpoint != nil {
		return c.VoiceEndpoint()
	}
	return endpoints.Current().Voice
}

func (c Config) defaultVoiceEndpoint() string {
	if c.DefaultVoiceEndpoint != nil {
		return c.DefaultVoiceEndpoint()
	}
	return endpoints.DefaultVoice
}

func (c Config) liveModel() string {
	if c.LiveModel != nil {
		return c.LiveModel()
	}
	return endpoints.CurrentGeminiLiveModel()
}

func (c Config) resolveBackend(opts livevoice.Options) (voicebackend.LiveBackend, error) {
	if c.ResolveBackend != nil {
		return c.ResolveBackend(opts)
	}
	return livevoice.ResolveBackend(opts)
}

func (c Config) newLiveSession(b voicebackend.LiveBackend, h voicebackend.NativeLiveHandler) LiveSession {
	if c.NewLiveSession != nil {
		return c.NewLiveSession(b, h)
	}
	return voicebackend.NewNativeLiveSession(b, h)
}

func (c Config) newTranscriber() Transcriber {
	if c.NewTranscriber != nil {
		return c.NewTranscriber()
	}
	return rcaudio.NewCloudCodeTranscriber()
}

func (c Config) fallbackLiveModel() string {
	if c.DefaultLiveModel != "" {
		return c.DefaultLiveModel
	}
	return voicebackend.DefaultGeminiLiveModel
}

// Model is the voice bridge's state: the live session, the microphone mute,
// the dictation recorder and the Thinker hand-off bookkeeping.
//
// Its zero value is ready for use: a Model without ports connects, records
// and reports nothing, and every transition stays observable through the
// returned values.
type Model struct {
	ports  Ports
	config Config

	recorder Recorder
	pcm      PCM
	device   device

	mu          sync.Mutex
	live        LiveSession
	muted       bool
	thinking    bool
	duplex      bool
	voice       string
	transcriber Transcriber

	thinkerMu     sync.Mutex
	thinkerCancel context.CancelFunc
	thinkerInvID  string
	thinkerSent   bool
}

// New builds a bridge. Ports and Config may be replaced afterwards.
func New(ports Ports, config Config) *Model {
	m := &Model{ports: ports, config: config}
	m.recorder = config.Recorder
	return m
}

// SetPorts replaces the host seams.
func (m *Model) SetPorts(ports Ports) { m.ports = ports }

// SetConfig replaces the injectable I/O and re-adopts its recorder.
func (m *Model) SetConfig(config Config) {
	m.config = config
	m.recorder = config.Recorder
}

// SetTranscriber overrides the dictation transcriber.
func (m *Model) SetTranscriber(t Transcriber) { m.transcriber = t }

// SetVoice sets the persona name used by the next live session.
func (m *Model) SetVoice(name string) { m.voice = name }

// Voice returns the configured persona name.
func (m *Model) Voice() string { return m.voice }

// SetDuplex sets the duplex preference and applies it to the live session.
func (m *Model) SetDuplex(duplex bool) {
	m.mu.Lock()
	m.duplex = duplex
	m.mu.Unlock()
	if sess := m.Live(); sess != nil {
		sess.SetDuplex(duplex)
	}
}

// Duplex reports the duplex preference.
func (m *Model) Duplex() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.duplex
}

// SetLive installs the active live session, or clears it when sess is nil.
//
// Installing applies the duplex preference and points the session's live
// router at the session, so a typed prompt in the composer reaches it.
func (m *Model) SetLive(sess LiveSession) {
	m.mu.Lock()
	m.live = sess
	m.mu.Unlock()
	if sess == nil {
		return
	}
	sess.SetDuplex(m.Duplex())
	if m.ports.SetLiveRouter != nil {
		m.ports.SetLiveRouter(sess)
	}
}

// AdoptLive installs a session the host already started: the duplex
// preference and the router are applied, but Start is not called again.
func (m *Model) AdoptLive(sess LiveSession) { m.SetLive(sess) }

// Live returns the active live session, or nil.
func (m *Model) Live() LiveSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.live
}

// SetThinking records whether a Thinker hand-off is in flight.
func (m *Model) SetThinking(thinking bool) {
	m.mu.Lock()
	m.thinking = thinking
	m.mu.Unlock()
}

// Thinking reports whether a Thinker hand-off is in flight.
func (m *Model) Thinking() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.thinking
}

// InsertDraft writes text into the composer: the value when the composer is
// empty, an insertion at the cursor otherwise.
func (m *Model) InsertDraft(text string) {
	editor := m.ports.Input
	if editor == nil {
		return
	}
	if editor.Value() == "" {
		editor.SetValue(text)
		return
	}
	editor.InsertString(text)
}

// PromptEmpty reports whether the composer holds only whitespace.
func (m *Model) PromptEmpty() bool {
	editor := m.ports.Input
	if editor == nil {
		return true
	}
	return strings.TrimSpace(editor.Value()) == ""
}

// Connect establishes dictation capture. A configured remote microphone is
// probed first, so an unreachable one fails before the local device opens.
// A capture failure is advisory: the session continues without audio.
func (m *Model) Connect() error {
	env := m.config.env()
	if err := m.recorder.ProbeMic(env.MicAddr); err != nil {
		return fmt.Errorf("cannot reach the mic server at %s (tcp): %w", env.MicAddr, err)
	}
	dev, err := m.recorder.Start(env.MicAddr, &m.pcm)
	if err != nil {
		return err
	}
	m.device = dev
	return nil
}

// SetDictationPCM seeds the recording buffer, so a test can drive the
// transcription path without a microphone.
func (m *Model) SetDictationPCM(pcm []byte) {
	m.pcm.mu.Lock()
	m.pcm.data = append([]byte(nil), pcm...)
	m.pcm.mu.Unlock()
}

// DictationPCM returns the buffered recording.
func (m *Model) DictationPCM() []byte {
	m.pcm.mu.Lock()
	defer m.pcm.mu.Unlock()
	return append([]byte(nil), m.pcm.data...)
}

// StopDictation ends capture and answers the recorded PCM.
func (m *Model) StopDictation() []byte {
	m.recorder.Stop(m.device)
	m.device = device{}
	return m.pcm.take()
}

// FinishDictation stops capture and returns the transcription command.
//
// The command — not this call — performs the I/O, so the caller hands it
// straight to the update loop. An empty recording yields a nil command.
func (m *Model) FinishDictation() Dictation {
	pcm := m.StopDictation()
	if len(pcm) == 0 {
		return nil
	}
	transcriber := m.transcriber
	if transcriber == nil {
		transcriber = m.config.newTranscriber()
	}
	return func() voice.DictationDoneMsg {
		text, err := Transcribe(context.Background(), transcriber, pcm)
		return voice.DictationDoneMsg{Text: text, Err: err}
	}
}

// CancelDictation ends capture and drops the recording.
func (m *Model) CancelDictation() {
	m.recorder.Stop(m.device)
	m.device = device{}
	m.pcm.drop()
}

// Available reports whether a live session can be established here: a
// credential, an API key, a custom endpoint or the mock switch each answer
// yes.
func (m *Model) Available() bool {
	if tok, err := m.config.loadToken(); err == nil && tok != nil && tok.Token.AccessToken != "" {
		return true
	}
	if m.config.apiKey() != "" {
		return true
	}
	if m.config.voiceEndpoint() != m.config.defaultVoiceEndpoint() {
		return true
	}
	return m.config.env().MockVoice
}

// StartLive resolves a transport, builds a session and starts it.
//
// The session's system prompt carries the workspace, the persona rules and
// the tail of the active conversation, so the voice agent picks up where the
// text chat left off.
func (m *Model) StartLive(ctx context.Context) error {
	if !m.Available() {
		return errors.New("Live mode isn't available in this session.")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	env := m.config.env()

	voiceName := m.voice
	if voiceName == "" {
		voiceName = DefaultVoice
	}
	liveModel := m.config.liveModel()
	if liveModel == "" {
		liveModel = m.config.fallbackLiveModel()
	}
	history := m.history()

	b, err := m.config.resolveBackend(livevoice.Options{
		ForceAPIKey: env.ForceAPIKey,
		AltBackend:  env.AltBackend,
	})
	if err != nil {
		return errors.New("Live mode isn't available in this session. Sign in with 'pigasus login' or configure a Gemini API key.")
	}

	sess := m.config.newLiveSession(b, &BridgeHandler{Bridge: m})
	if turns := LiveTurns(history); len(turns) > 0 {
		sess.SetConversationHistory(turns)
	}
	if err := sess.Start(ctx, voicebackend.LiveSessionConfig{
		Voice:        voiceName,
		Model:        liveModel,
		SystemPrompt: m.systemPrompt(history),
	}); err != nil {
		return err
	}
	m.SetLive(sess)
	return nil
}

// StopLive clears the router and hangs up the session.
func (m *Model) StopLive() error {
	if m.ports.SetLiveRouter != nil {
		m.ports.SetLiveRouter(nil)
	}
	if sess := m.Live(); sess != nil {
		_ = sess.HangUp()
		m.SetLive(nil)
	}
	return nil
}

// ToggleMute flips the microphone mute and reports the new state.
func (m *Model) ToggleMute() bool {
	if sess := m.Live(); sess != nil {
		muted := sess.ToggleMute()
		m.mu.Lock()
		m.muted = muted
		m.mu.Unlock()
		return muted
	}
	m.mu.Lock()
	m.muted = !m.muted
	muted := m.muted
	m.mu.Unlock()
	return muted
}

// Muted reports whether the microphone is muted.
func (m *Model) Muted() bool {
	if sess := m.Live(); sess != nil {
		return sess.IsMuted()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.muted
}

// Speaking reports whether the agent is speaking.
func (m *Model) Speaking() bool {
	if sess := m.Live(); sess != nil {
		return sess.Speaking()
	}
	return false
}

// LiveActive reports whether a live session is established and running.
func (m *Model) LiveActive() bool {
	sess := m.Live()
	return sess != nil && sess.IsActive()
}

// NotifyWaitingOnUser tells the caller what the agent is blocked on, when a
// live session can carry it.
func (m *Model) NotifyWaitingOnUser(question string) {
	sess := m.Live()
	if sess == nil || !sess.IsActive() {
		return
	}
	question = strings.TrimSpace(question)
	if question == "" {
		question = "Waiting for your confirmation."
	}
	_ = sess.SendWaitingOnUser(question)
}

// SetThinkerCancel arms the cancellation of the Thinker turn identified by
// invID. A previously armed turn is cancelled, so only one runs at a time.
func (m *Model) SetThinkerCancel(invID string, cancel context.CancelFunc) {
	m.thinkerMu.Lock()
	prev := m.thinkerCancel
	m.thinkerCancel = cancel
	m.thinkerInvID = invID
	m.thinkerSent = false
	m.thinkerMu.Unlock()
	if prev != nil {
		prev()
	}
}

// ClearThinkerCancel drops the cancellation for invID and reports whether a
// terminal message was already sent for it.
func (m *Model) ClearThinkerCancel(invID string) bool {
	m.thinkerMu.Lock()
	defer m.thinkerMu.Unlock()
	sent := m.thinkerSent
	if m.thinkerInvID == invID || invID == "" {
		m.thinkerCancel = nil
		m.thinkerInvID = ""
		m.thinkerSent = false
	}
	return sent
}

// CancelThinker cancels the armed Thinker turn and tells the caller what the
// agent hears. It answers the cancelled invocation and whether a turn was
// actually running.
func (m *Model) CancelThinker(invocationID string) (activeID string, cancelled bool) {
	m.thinkerMu.Lock()
	cancel := m.thinkerCancel
	activeID = m.thinkerInvID
	if cancel != nil {
		m.thinkerSent = true
		cancelled = true
	}
	m.thinkerMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if invocationID == "" {
		invocationID = activeID
	}
	if sess := m.Live(); sess != nil && sess.IsActive() {
		_ = sess.SendTaskStopped()
		if invocationID != "" {
			_ = sess.CompleteHandoff(invocationID, map[string]any{"cancelled": true})
		}
	}
	if cancelled {
		if sink := m.sink(); sink != nil {
			sink.ThinkerDone(invocationID, "", context.Canceled)
		}
	}
	return activeID, cancelled
}

// systemPrompt builds the live session's opening instructions: the persona
// rules, the workspace and the tail of the active conversation, so the voice
// agent picks up where the text chat left off.
func (m *Model) systemPrompt(history []backend.Message) string {
	workspace := ""
	if m.ports.Cwd != nil {
		workspace = m.ports.Cwd()
	}
	if workspace == "" && m.ports.FallbackCwd != nil {
		workspace = m.ports.FallbackCwd()
	}

	prompt := fmt.Sprintf(`You are Antigravity's real-time spoken voice agent running in workspace: %s.


Primary behavioral guidelines:
1. Spoken persona: Speak concisely, naturally, warmly, and helpfully in 1-2 spoken sentences. Never read out raw markdown tables, verbose code blocks, URLs, or UUIDs.
2. Thinker agent handoff: For any coding, file inspection, editing, terminal command execution, tests, git operations, web searches, or complex analysis tasks, you MUST call 'call_thinker_agent'.
3. Never refuse a request or claim you lack tools or capabilities. Speak one brief commitment sentence first (e.g. "To check the files, I'll examine the workspace directory." or "好的，我来帮你查看项目下的文件。"), and in the SAME turn call 'call_thinker_agent' with the user's full request and context.
4. When Thinker progress notes or answers arrive, summarize the key takeaway to the user naturally in 1-2 spoken sentences.
5. If the user explicitly asks to hang up or say goodbye, speak a brief farewell and call 'hang_up'.`, workspace)

	var hist strings.Builder
	if len(history) > 0 {
		hist.WriteString("\n\nActive conversation history prior to starting this voice session:\n")
		start := 0
		if len(history) > 30 {
			start = len(history) - 30
		}
		for _, msg := range history[start:] {
			role := "User"
			if msg.Role == "assistant" {
				role = "Assistant"
			}
			if content := strings.TrimSpace(msg.Content); content != "" {
				fmt.Fprintf(&hist, "%s: %s\n", role, content)
			}
		}
	}
	return prompt + hist.String()
}

// ThinkerContext builds the prompt a Thinker turn runs with: the voice
// instructions, the recent spoken turns and the user's request.
func (m *Model) ThinkerContext(task string) string {
	var talkerCtx strings.Builder
	if sess := m.Live(); sess != nil {
		for _, t := range sess.ConversationHistory() {
			role := strings.ToLower(t.Role)
			if strings.Contains(role, "user") {
				role = "user"
			} else {
				role = "talker"
			}
			fmt.Fprintf(&talkerCtx, "%s: %q\n", role, t.Content)
		}
	}
	return fmt.Sprintf(thinkerPromptTemplate, talkerCtx.String(), task)
}

// WireMessage builds the transcript entry a Thinker turn appends.
func (m *Model) WireMessage(content, model string, wireSettingsPending bool) backend.Message {
	label := m.ports.ModelLabel
	if label == nil {
		label = catalog.FormatModelLabel
	}
	return backend.Message{
		Role:        "user",
		Content:     content,
		WireContent: backend.WrapUserContent(content, label(model), wireSettingsPending, time.Now()),
	}
}

// FinishThinkerTurn records the Thinker turn in the transcript.
func (m *Model) FinishThinkerTurn(userMsg backend.Message, answer string) {
	if m.ports.Session == nil {
		return
	}
	m.ports.Session.AppendMessage(userMsg)
	m.ports.Session.AppendMessage(backend.Message{Role: "assistant", Content: answer})
}

// RunThinker runs a Thinker turn through the host's runner, streaming
// progress notes into the live session and reporting the outcome to the sink.
func (m *Model) RunThinker(ctx context.Context, invocationID, task string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runner := m.ports.RunThinker
	if runner == nil {
		err := errors.New("voicebridge: no thinker runner configured")
		m.thinkerDone(invocationID, "", err)
		return "", err
	}
	sess := m.Live()
	result, err := runner(ctx, task, func(note string) {
		if sess != nil && sess.IsActive() {
			_ = sess.SendThinkerNote(note)
		}
	})
	if err != nil {
		m.finishThinkerError(sess, invocationID, err)
		return "", err
	}
	if sess != nil && sess.IsActive() {
		if invocationID != "" {
			_ = sess.CompleteHandoff(invocationID, map[string]any{"answer": result})
		}
		_ = sess.SendThinkerAnswer(result)
	}
	m.thinkerDone(invocationID, result, nil)
	return result, nil
}

func (m *Model) finishThinkerError(sess LiveSession, invocationID string, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		if sess != nil && sess.IsActive() {
			_ = sess.SendTaskStopped()
			if invocationID != "" {
				_ = sess.CompleteHandoff(invocationID, map[string]any{"cancelled": true})
			}
		}
	case sess != nil && sess.IsActive():
		_ = sess.SendThinkerNote("Task failed: " + err.Error())
		if invocationID != "" {
			_ = sess.CompleteHandoff(invocationID, map[string]any{"error": err.Error()})
		}
	}
	m.thinkerDone(invocationID, "", err)
}

func (m *Model) thinkerDone(invocationID, answer string, err error) {
	if sink := m.sink(); sink != nil {
		sink.ThinkerDone(invocationID, answer, err)
	}
}

// LiveTurns converts transcript messages into the live session's
// rehydration records, dropping empty content.
func LiveTurns(msgs []backend.Message) []voicebackend.LiveTurnRecord {
	var turns []voicebackend.LiveTurnRecord
	for _, msg := range msgs {
		role := "User"
		if msg.Role == "assistant" {
			role = "Agent"
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		turns = append(turns, voicebackend.LiveTurnRecord{Role: role, Content: content})
	}
	return turns
}

func (m *Model) history() []backend.Message {
	if m == nil || m.ports.Session == nil {
		return nil
	}
	return m.ports.Session.Messages()
}

func (m *Model) sink() ProgressSink {
	if m == nil {
		return nil
	}
	return m.ports.Progress
}

const thinkerPromptTemplate = `<voice_instructions>
With <talker_agent_context>, a live voice handles casual chat, sends tasks here, and speaks your reply. Without it, reply in markdown.

1. Share one persona with the voice: continue from its last line with no filler, restatements, or mentions of the voice or instructions, correcting any hedge or small-talk slip cleanly.
2. Fulfill any running task and the new request unless it cancels, replaces, or narrows the first; on stop, cancel, or ignore, stop immediately and reply briefly.
3. Act only on the current request, using prior lines only as context.

<talker_agent_context> lists timestamped user and talker lines oldest first (trailing hyphen marks cutoffs), matched in order to attached audio/wav clips. Check audio if speech looks misheard or clipped, honor self-corrections, and ask one short question instead of guessing unclear targets. Claim finished or logged only after tool success, treat the chat screen as a live canvas (rendering interactive generative UI, visuals, diagrams, tables, cards, code, printouts, or clickable links directly on screen whenever asked), and state key facts in brief prose with requested lists, shortlinks, URLs, slash commands, caveats, options, and colleague names.
</voice_instructions>
<talker_agent_context>
%s</talker_agent_context>
User request: %s`
