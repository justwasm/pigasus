package panels

import (
	"github.com/justwasm/pigasus/internal/ui/views/voice"
)

// VoiceHost defines host seams for voice dictation surface.
type VoiceHost interface {
	ConnectVoice() error
	InsertDraft(text string)
	SlashRespond(cmd, msg string, extra ...string)
	TerminalWidth() int
}

// NewVoiceSurface builds the voice dictation surface with the host's seams.
func NewVoiceSurface(h VoiceHost) *voice.Surface {
	return voice.New(voice.Seams{
		Connect:     h.ConnectVoice,
		InsertDraft: h.InsertDraft,
		Respond: func(msg string, extra ...string) {
			h.SlashRespond("/voice", msg, extra...)
		},
		Width: h.TerminalWidth,
	})
}
