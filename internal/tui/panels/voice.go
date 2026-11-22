package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/voice"
)

// VoiceHost defines host seams for voice dictation surface.
type VoiceHost interface {
	ConnectVoice() error
	InsertDraft(text string)
	SlashRespond(cmd, msg string, extra ...string)
	TerminalWidth() int
	StartLiveVoice() error
	StopLiveVoice() error
	ToggleLiveMute() bool
	IsLiveMuted() bool
	IsLiveSpeaking() bool
	IsPromptEmpty() bool
	IsLiveVoiceAvailable() bool
	IsLiveThinking() bool
	FinishDictation() tea.Cmd
	CancelDictation()
}

// NewVoiceSurface builds the voice dictation surface with the host's seams.
func NewVoiceSurface(h VoiceHost) *voice.Surface {
	return voice.New(voice.Seams{
		Connect:     h.ConnectVoice,
		InsertDraft: h.InsertDraft,
		Respond: func(msg string, extra ...string) {
			h.SlashRespond("/voice", msg, extra...)
		},
		Width:           h.TerminalWidth,
		StartLive:       h.StartLiveVoice,
		StopLive:        h.StopLiveVoice,
		ToggleMute:      h.ToggleLiveMute,
		IsMuted:         h.IsLiveMuted,
		IsSpeaking:      h.IsLiveSpeaking,
		PromptEmpty:     h.IsPromptEmpty,
		IsLiveAvailable: h.IsLiveVoiceAvailable,
		IsThinking:      h.IsLiveThinking,
		FinishDictation: h.FinishDictation,
		CancelDictation: h.CancelDictation,
	})
}
