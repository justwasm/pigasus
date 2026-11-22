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
	"os"
	"sync"

	"github.com/justwasm/pigasus/internal/rcaudio"
	voiceviews "github.com/justwasm/pigasus/internal/ui/views/voice"
	"github.com/justwasm/pigasus/internal/voice"
)

// AudioModel owns the dictation surface. connect is the host's transport
// hook; the surface calls it when the user starts dictation.
type AudioModel struct {
	*voiceviews.Surface
	connect     func() error
	muted       bool
	thinking    bool
	duplex      bool
	voice       string
	volume      string
	speed       string
	speaker     string
	liveSession *voice.NativeLiveSession

	// Dictation recording state
	dictCapture *voice.AudioCapture
	dictRemote  *voice.RemoteAudioStream
	dictMu      sync.Mutex
	dictPCM     []byte
	transcriber rcaudio.Transcriber
}

func (a *AudioModel) startDictationCapture() error {
	a.dictMu.Lock()
	a.dictPCM = nil
	a.dictMu.Unlock()

	if micAddr := os.Getenv("ANTIGRAVITY_MIC"); micAddr != "" {
		stream, err := voice.DialRemoteAudio(micAddr, func(pcm []byte) {
			a.dictMu.Lock()
			a.dictPCM = append(a.dictPCM, pcm...)
			a.dictMu.Unlock()
		})
		if err == nil {
			a.dictRemote = stream
			return nil
		}
	}

	cap, err := voice.NewAudioCapture(16000, func(pcm []byte) {
		a.dictMu.Lock()
		a.dictPCM = append(a.dictPCM, pcm...)
		a.dictMu.Unlock()
	})
	if err != nil {
		return err
	}
	a.dictCapture = cap
	return cap.Start()
}

func (a *AudioModel) stopDictationCapture() []byte {
	if a.dictRemote != nil {
		_ = a.dictRemote.Close()
		a.dictRemote = nil
	}
	if a.dictCapture != nil {
		_ = a.dictCapture.Stop()
		_ = a.dictCapture.Close()
		a.dictCapture = nil
	}
	a.dictMu.Lock()
	defer a.dictMu.Unlock()
	pcm := a.dictPCM
	a.dictPCM = nil
	return pcm
}

func (a *AudioModel) cancelDictationCapture() {
	if a.dictRemote != nil {
		_ = a.dictRemote.Close()
		a.dictRemote = nil
	}
	if a.dictCapture != nil {
		_ = a.dictCapture.Stop()
		_ = a.dictCapture.Close()
		a.dictCapture = nil
	}
	a.dictMu.Lock()
	a.dictPCM = nil
	a.dictMu.Unlock()
}
