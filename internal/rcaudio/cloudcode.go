// cloudcode.go: the Gemini-backed transcriber.
//
// The reference transcribes with a local engine (see rcaudio.go's
// header). The clone has none, so the audio rides the Gemini
// transport the clone already speaks: the CloudCode dialect writes
// the recording as an `inlineData` part — a mime type and the
// base64 payload — and Gemini reads `audio/pcm;rate=16000` as raw
// PCM.
//
// 【一手】 2026-10-27, the transport answered a synthetic sample
// (16 kHz mono s16le, "Hello terminal, this is a test.") with
// "Hello terminal, this is a test." — the same words the
// reference's own engine produced ("Hello terminal.").
package rcaudio

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
)

// DefaultTranscriptionModel is the model the transcriber asks when
// nothing else is configured.
//
// 【一手】 2026-10-27: this model answered a transcription request
// with the sample's own words.
const DefaultTranscriptionModel = "gemini-3.8-flash-high"

// EnvTranscriptionModel is the env var that overrides the model.
const EnvTranscriptionModel = "OPENAGY_AUDIO_MODEL"

// transcriptionPrompt asks for the words and nothing else.
const transcriptionPrompt = "Transcribe the attached audio verbatim. " +
	"Reply with the transcription text only."

// CloudCodeTranscriber transcribes a recording with the Gemini
// transport.
type CloudCodeTranscriber struct {
	// Model is the model the request asks for. Empty picks
	// DefaultTranscriptionModel.
	Model string
	// Client is the transport. nil → one is built on demand.
	Client *backend.CloudCodeClient
}

// NewCloudCodeTranscriber returns a transcriber for the configured
// model.
//
// The model comes from $OPENAGY_AUDIO_MODEL, falling back to
// DefaultTranscriptionModel.
func NewCloudCodeTranscriber() *CloudCodeTranscriber {
	model := strings.TrimSpace(os.Getenv(EnvTranscriptionModel))
	if model == "" {
		model = DefaultTranscriptionModel
	}
	return &CloudCodeTranscriber{Model: model}
}

// Transcribe asks the model for the recording's words.
func (t *CloudCodeTranscriber) Transcribe(ctx context.Context, audio []byte, mimeType string) (string, error) {
	if len(audio) == 0 {
		return "", errors.New("rcaudio: the recording is empty")
	}
	model := strings.TrimSpace(t.Model)
	if model == "" {
		model = DefaultTranscriptionModel
	}
	if mimeType == "" {
		mimeType = "audio/pcm;rate=16000"
	}
	client := t.Client
	if client == nil {
		client = backend.NewCloudCodeWithConfig(backend.CloudCodeConfig{Model: model})
	}
	chunks, err := client.Stream(ctx, backend.StreamRequest{
		Model: model,
		Messages: []backend.Message{{
			Role:    "user",
			Content: transcriptionPrompt,
			// The recording rides in the byte-carrying part: the
			// CloudCode dialect writes it as `inlineData`, which is
			// what the mime type names.
			Images: []backend.ImagePart{{
				MIMEType: mimeType,
				Data:     audio,
			}},
		}},
	})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for chunk := range chunks {
		if chunk.Error != nil {
			return out.String(), chunk.Error
		}
		out.WriteString(chunk.Text)
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return "", errors.New("rcaudio: the model answered nothing")
	}
	return text, nil
}
