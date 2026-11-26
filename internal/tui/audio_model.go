package tui

// audio_model.go — the dictation sub-model.
//
// AudioModel owns the voice-dictation surface and the transport seam that
// connects it. The surface lives in internal/ui/views/voice and owns its
// phase machine, recorder and cancellation; this model is the root's owner
// for it, so the surface is not a loose field on the viewport bucket.
//
// The voice session state machine itself lives in internal/ui/voicebridge.
// What stays here is the surface, the knobs the slash commands own (persona,
// volume, speed, duplex) and a typed mirror of the active live session for
// the host files that read it. bridge is built lazily by voiceBridge().
//
// Upstream: google3/third_party/jetski/cli/model/model.AudioModel.

import (
	"github.com/justwasm/pigasus/internal/rcaudio"
	voiceviews "github.com/justwasm/pigasus/internal/ui/views/voice"
	"github.com/justwasm/pigasus/internal/ui/voicebridge"
	"github.com/justwasm/pigasus/internal/voice"
)

// AudioModel owns the dictation surface. connect is the host's transport
// hook; the surface calls it when the user starts dictation.
type AudioModel struct {
	*voiceviews.Surface
	connect     func() error
	bridge      *voicebridge.Model
	muted       bool
	thinking    bool
	duplex      bool
	voice       string
	volume      string
	speed       string
	speaker     string
	liveSession *voice.NativeLiveSession

	transcriber rcaudio.Transcriber
}

// setLiveSession installs the typed mirror of the bridge's active session.
func (a *AudioModel) setLiveSession(sess *voice.NativeLiveSession) {
	if a != nil {
		a.liveSession = sess
	}
}
