package tui

// audio_model.go — the dictation sub-model.
//
// AudioModel owns the voice-dictation surface and the transport seam that
// connects it. The surface lives in internal/ui/views/voice and owns its
// phase machine, recorder and cancellation; this model is the root's owner
// for it, so the surface is not a loose field on the viewport bucket.
//
// Upstream: google3/third_party/jetski/cli/model/model.AudioModel.

import (
	voiceviews "github.com/justwasm/pigasus/internal/ui/views/voice"
	"github.com/justwasm/pigasus/internal/voice"
)

// AudioModel owns the dictation surface. connect is the host's transport
// hook; the surface calls it when the user starts dictation.
type AudioModel struct {
	*voiceviews.Surface
	connect     func() error
	muted       bool
	duplex      bool
	voice       string
	volume      string
	speed       string
	speaker     string
	liveSession *voice.NativeLiveSession
}
