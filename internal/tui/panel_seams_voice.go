package tui

import (
	"context"
	"os"
	"strconv"
	"strings"

	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
	"github.com/justwasm/pigasus/internal/ui/voicebridge"
	voicebackend "github.com/justwasm/pigasus/internal/voice"
)

// panel_seams_voice.go — the host half of the voice bridge.
//
// The rules live in internal/ui/voicebridge: the live-session lifecycle, the
// dictation recorder, the microphone mute and the Thinker hand-off
// bookkeeping. What stays here is what only the host can do — reach the turn
// engine, paint the transcript, own the composer — plus the thin delegations
// the voice surface and the slash commands call.
//
// The bridge is built lazily, so a session that never opens the voice surface
// never allocates one.

// voicePorts assembles the host seams the bridge asks for.
//
// Every closure reads the model at call time, so a bridge built before the
// composer or the transcript exists still writes to the live one.
func (m *RootModel) voicePorts() voicebridge.Ports {
	return voicebridge.Ports{
		Progress: voiceProgressSink{m: m},
		Input:    (*composerTextarea)(&m.prompt.input),
		Session:  voiceSessionPort{state: &m.session},
		SetLiveRouter: func(router voicebridge.LiveVoiceRouter) {
			if dom := m.session.Domain(); dom != nil {
				if router == nil {
					dom.SetLiveVoiceRouter(nil)
					return
				}
				dom.SetLiveVoiceRouter(liveRouter{router: router})
			}
		},
		Cwd: func() string { return m.session.cwd },
		FallbackCwd: func() string {
			wd, _ := os.Getwd()
			return wd
		},
		RunThinker: func(ctx context.Context, task string, onProgress func(string)) (string, error) {
			return m.RunThinkerTaskWithContext(ctx, task, onProgress)
		},
	}
}

// composerTextarea adapts the composer widget to the bridge's input port.
type composerTextarea tas.Model

func (t *composerTextarea) Value() string         { return (*tas.Model)(t).Value() }
func (t *composerTextarea) SetValue(v string)     { (*tas.Model)(t).SetValue(v) }
func (t *composerTextarea) InsertString(s string) { (*tas.Model)(t).InsertString(s) }

// voiceSessionPort adapts the session state to the bridge's transcript port.
type voiceSessionPort struct{ state *sessionState }

func (p voiceSessionPort) Messages() []backend.Message { return p.state.messages() }

func (p voiceSessionPort) AppendMessage(msg backend.Message) { p.state.appendMessage(msg) }

// liveRouter adapts the bridge's narrow router to the session broker's.
type liveRouter struct{ router voicebridge.LiveVoiceRouter }

func (r liveRouter) IsActive() bool             { return true }
func (r liveRouter) SendText(text string) error { return r.router.SendText(text) }

// voiceProgressSink is the host's ProgressSink: it turns the bridge's events
// into the program messages the transcript already understands.
type voiceProgressSink struct{ m *RootModel }

func (s voiceProgressSink) send(msg tea.Msg) {
	if s.m == nil || s.m.turn.prog == nil {
		return
	}
	s.m.turn.prog.Send(msg)
}

func (s voiceProgressSink) UserSpeechInterim(text string) {
	s.send(voiceUserSpeechInterimMsg{text: text})
}

func (s voiceProgressSink) UserSpeech(text string) { s.send(voiceUserSpeechMsg{text: text}) }

func (s voiceProgressSink) AgentSpeech(text string) { s.send(voiceAgentDoneMsg{text: text}) }

func (s voiceProgressSink) AgentSpeechDelta(delta string) {
	s.send(voiceAgentDeltaMsg{delta: delta})
}

func (s voiceProgressSink) Interrupted() { s.send(voiceInterruptedMsg{}) }

func (s voiceProgressSink) ThinkerHandoff(invocationID, task string) {
	s.send(voiceThinkerHandoffMsg{invocationID: invocationID, task: task})
}

func (s voiceProgressSink) ThinkerDone(invocationID, answer string, err error) {
	s.send(voiceThinkerDoneMsg{invocationID: invocationID, answer: answer, err: err})
}

// LiveError echoes a live-session failure into the conversation and drops the
// session that failed. The session hangs itself up on the way out.
func (s voiceProgressSink) LiveError(err error) {
	if s.m == nil || err == nil {
		return
	}
	s.m.slashRespond("/voice", "Live error: "+err.Error())
	s.m.audio.forgetLiveSession()
	if s.m.audio.Surface != nil && s.m.audio.Surface.Active() {
		s.m.audio.Surface.Discard()
	}
}

// tuiLiveHandler adapts the bridge's handler to a concrete live session, so
// the host (and its tests) can build a session around the bridge. The model
// field stays for the callers that read it; every event goes to the bridge.
type tuiLiveHandler struct {
	m       *RootModel
	handler voicebackend.NativeLiveHandler
}

// NewLiveHandler returns the handler the bridge installs on a live session.
func (m *RootModel) NewLiveHandler() *tuiLiveHandler {
	return &tuiLiveHandler{m: m, handler: &voicebridge.BridgeHandler{Bridge: m.voiceBridge()}}
}

func (h *tuiLiveHandler) OnUserSpeech(text string, interim bool) {
	if h == nil || h.handler == nil {
		return
	}
	h.handler.OnUserSpeech(text, interim)
}

func (h *tuiLiveHandler) OnAgentSpeech(text string) { h.handler.OnAgentSpeech(text) }

func (h *tuiLiveHandler) OnAgentSpeechDelta(delta string) {
	if h == nil || h.handler == nil {
		return
	}
	h.handler.OnAgentSpeechDelta(delta)
}

func (h *tuiLiveHandler) OnTurnComplete() { h.handler.OnTurnComplete() }

func (h *tuiLiveHandler) OnInterrupted() { h.handler.OnInterrupted() }

func (h *tuiLiveHandler) OnError(err error) { h.handler.OnError(err) }

func (h *tuiLiveHandler) OnClose() { h.handler.OnClose() }

func (h *tuiLiveHandler) OnThinkerHandoff(invocationID string, task string) {
	if h == nil || h.handler == nil {
		return
	}
	h.handler.OnThinkerHandoff(invocationID, task)
}

func (h *tuiLiveHandler) OnToolCall(call voicebackend.LiveToolCall) {
	if h == nil || h.handler == nil {
		return
	}
	h.handler.OnToolCall(call)
}

func (h *tuiLiveHandler) OnReconnecting(attempt int, maxAttempts int, err error) {
	if h == nil || h.handler == nil {
		return
	}
	h.handler.OnReconnecting(attempt, maxAttempts, err)
}

func (h *tuiLiveHandler) OnReconnected() { h.handler.OnReconnected() }

// OnThinkerCancel keeps the optional ThinkerCancelHandler contract.
func (h *tuiLiveHandler) OnThinkerCancel(invocationID string) {
	if h == nil || h.handler == nil {
		return
	}
	if c, ok := h.handler.(voicebackend.ThinkerCancelHandler); ok {
		c.OnThinkerCancel(invocationID)
	}
}

// voiceBridge returns the session's voice bridge, building it on first use.
//
// The ports are re-assembled on every call, so a bridge built before the
// composer or the transcript exists still writes to the live ones.
func (m *RootModel) voiceBridge() *voicebridge.Model {
	if m == nil {
		return nil
	}
	if m.audio.bridge == nil {
		m.audio.bridge = voicebridge.New(m.voicePorts(), voicebridge.Config{})
	} else {
		m.audio.bridge.SetPorts(m.voicePorts())
	}
	return m.audio.bridge
}

// Voice
func (m *RootModel) InsertDraft(text string) { m.voiceBridge().InsertDraft(text) }

// ConnectVoice establishes dictation capture. A host-installed connect seam
// wins, so an embedder keeps ownership of the transport.
func (m *RootModel) ConnectVoice() error {
	if m.audio.connect != nil {
		return m.audio.connect()
	}
	return m.voiceBridge().Connect()
}

func (m *RootModel) FinishDictation() tea.Cmd {
	m.audio.syncBridgeFromHost()
	dictation := m.voiceBridge().FinishDictation()
	if dictation == nil {
		return nil
	}
	return func() tea.Msg { return dictation() }
}

func (m *RootModel) CancelDictation() { m.voiceBridge().CancelDictation() }

func (m *RootModel) IsLiveVoiceAvailable() bool { return m.voiceBridge().Available() }

func (m *RootModel) StartLiveVoice() error {
	m.audio.syncBridgeFromHost()
	return m.voiceBridge().StartLive(context.Background())
}

func (m *RootModel) StopLiveVoice() error {
	if m.session.Domain() != nil {
		m.session.Domain().SetLiveVoiceRouter(nil)
	}
	return m.voiceBridge().StopLive()
}

func (m *RootModel) ToggleLiveMute() bool {
	m.audio.syncBridgeFromHost()
	return m.voiceBridge().ToggleMute()
}

func (m *RootModel) IsLiveMuted() bool { return m.voiceBridge().Muted() }

func (m *RootModel) IsLiveSpeaking() bool { return m.voiceBridge().Speaking() }

func (m *RootModel) IsPromptEmpty() bool { return m.voiceBridge().PromptEmpty() }

// IsLiveThinking reads the host's own flag: the Thinker hand-off bookkeeping
// in voice_live_stream.go flips it around the turn it drives.
func (m *RootModel) IsLiveThinking() bool { return m.audio.thinking }

// SetLiveDuplex records the duplex preference and applies it to the session.
// The audio model keeps the knob because the slash commands read and report
// it; the bridge keeps the session's copy in step.
func (m *RootModel) SetLiveDuplex(duplex bool) {
	m.audio.duplex = duplex
	m.voiceBridge().SetDuplex(duplex)
}

// LiveDuplex reports the duplex preference.
func (m *RootModel) LiveDuplex() bool { return m.audio.duplex }

// SetLiveVoice records the persona the next live session uses.
func (m *RootModel) SetLiveVoice(name string) {
	m.audio.voice = name
	m.voiceBridge().SetVoice(name)
}

// LiveVoice returns the configured persona.
func (m *RootModel) LiveVoice() string { return m.audio.voice }

// adoptLiveSession points the bridge and the audio model's mirror at a
// session the host already started. Installing nil clears the mirror too, so
// a dropped session is never left behind for the host files that read it.
func (m *RootModel) adoptLiveSession(sess voicebridge.LiveSession) {
	if sess == nil {
		m.audio.liveSession = nil
		m.voiceBridge().SetLive(nil)
		return
	}
	m.audio.liveSession, _ = sess.(*voicebackend.NativeLiveSession)
	m.voiceBridge().AdoptLive(sess)
}

// syncBridgeFromHost pushes the knobs the slash commands still own — the
// dictation transcriber, the persona and the duplex preference — into the
// bridge before it starts a session.
func (a *AudioModel) syncBridgeFromHost() {
	if a == nil || a.bridge == nil {
		return
	}
	a.bridge.SetTranscriber(a.transcriber)
	if a.voice != "" {
		a.bridge.SetVoice(a.voice)
	}
	a.bridge.SetDuplex(a.duplex)
}

func (m *RootModel) LiveSession() voicebridge.LiveSession { return m.voiceBridge().Live() }

// SetLiveSession installs a live session the host built itself; the remote
// host and the tests do this. The audio model keeps a typed mirror because
// the rest of the host reads it.
func (m *RootModel) SetLiveSession(sess voicebridge.LiveSession) { m.adoptLiveSession(sess) }

// setLiveSession installs an already-built session without starting it, so a
// host that dialled the transport itself keeps ownership of the dial.
func (m *RootModel) setLiveSession(sess *voicebackend.NativeLiveSession) {
	m.adoptLiveSession(sess)
}

// forgetLiveSession drops the session the bridge holds. The rest of the host
// reads the audio model's mirror, so it is cleared too.
func (a *AudioModel) forgetLiveSession() {
	if a == nil {
		return
	}
	if a.bridge != nil {
		a.bridge.SetLive(nil)
	}
	a.setLiveSession(nil)
}

func formatPermissionForVoice(req permissionprompt.Request) string {
	tool := strings.TrimSpace(req.Tool)
	cmd := strings.TrimSpace(req.Command)
	desc := strings.TrimSpace(req.Description)
	switch {
	case tool != "" && cmd != "":
		return "Approve " + tool + ": " + cmd + "?"
	case desc != "" && desc != tool:
		return "Approve " + tool + " (" + desc + ")?"
	case tool != "":
		return "Approve " + tool + "?"
	default:
		return "Approve tool execution?"
	}
}

func (m *RootModel) notifyVoiceWaitingOnUser(question string) {
	m.voiceBridge().NotifyWaitingOnUser(question)
}

// RunThinkerTask executes a Thinker turn using the active turn engine and tools.
func (m *RootModel) RunThinkerTask(task string, onProgress func(string)) (string, error) {
	return m.RunThinkerTaskWithContext(context.Background(), task, onProgress)
}

// RunThinkerTaskWithContext executes a Thinker turn using the active turn engine and tools under ctx.
func (m *RootModel) RunThinkerTaskWithContext(ctx context.Context, task string, onProgress func(string)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	engine := NewEngineBridge(BridgeOptions{
		Engine:    m.turnEngine(),
		Program:   m.turn.prog,
		PromptMgr: DefaultPermissionPrompts,
		OnUsage:   m.recordTurnUsage,
		OnFrame: func(f agentlink.Frame) {
			if f.Tool != nil && onProgress != nil {
				onProgress(voicebackend.HumanizeTool(f.Tool.Name, f.Tool.Args))
			}
		},
		OnPermissionPrompt: func(req permissionprompt.Request) {
			m.notifyVoiceWaitingOnUser(formatPermissionForVoice(req))
		},
		HookStart:    m.hookSessionStart,
		HookPre:      m.hookPreInvocation,
		HookStop:     m.hookStop,
		HookPost:     m.hookPostInvocation,
		SessionEpoch: func() uint64 { return m.session.epoch() },
	})

	turnCtx, endTurn := engine.turns.Begin(ctx)
	defer endTurn()
	turnCtx = interaction.WithHost(turnCtx, m.turnInteractionHost())

	bridgeVoice := m.voiceBridge()
	content := bridgeVoice.ThinkerContext(task)
	userMsg := bridgeVoice.WireMessage(content, m.turnModel(), m.session.wireSettingsPending)

	msgs := append([]backend.Message(nil), m.SessionMessages()...)
	msgs = append(msgs, userMsg)

	finalAnswer, runErr := thinkerAnswer(engine.runTurnOn(turnCtx, m.turnModel(), msgs))
	if runErr != nil {
		return "", runErr
	}
	bridgeVoice.FinishThinkerTurn(userMsg, finalAnswer)
	return finalAnswer, nil
}

// thinkerAnswer reduces the bridge's terminal message to the answer text.
func thinkerAnswer(rawMsg tea.Msg) (string, error) {
	done, ok := rawMsg.(toolsteps.TurnDoneMsg)
	if !ok {
		return "Task completed.", nil
	}
	if done.Err != nil {
		return "", done.Err
	}
	if done.Reply != "" {
		return done.Reply, nil
	}
	if len(done.Steps) > 0 {
		lastStep := done.Steps[len(done.Steps)-1]
		if lastStep.Result != "" {
			return lastStep.Result, nil
		}
		return "Completed " + strconv.Itoa(len(done.Steps)) + " tool steps", nil
	}
	return "Task completed successfully.", nil
}

// deadcode: staged (voice connection seam tested in slash_content_test.go)
func (m *RootModel) connectVoice() error { return m.ConnectVoice() }

func (m *RootModel) newVoiceSurface() *voice.Surface { return panels.NewVoiceSurface(m) }

// compile-time proof that the host answers the voice seams and the sink.
var (
	_ panels.VoiceHost         = (*RootModel)(nil)
	_ voicebridge.ProgressSink = voiceProgressSink{}
)
