// ls_audio_test.go: the Web UI's voice surface at the dispatcher's
// level.
//
// The expectations are the reference's own answers, measured against
// the reference's language_server on 2026-10-27 (see ls_audio.go's
// header for the captures).
package remotecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/rcaudio"
)

// newAudioDispatcher builds a dispatcher whose Web UI's voice surface
// is wired to a registry with a canned transcriber.
func newAudioDispatcher(t *testing.T, transcriber rcaudio.Transcriber) *Dispatcher {
	t.Helper()
	disp, err := NewDispatcher(Config{
		Session:  NewSessionState("1.4.7"),
		WebAudio: rcaudio.NewRegistry(transcriber),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return disp
}

// TestTheVoiceStreamCarriesTheReferenceFrames pins the frame
// sequence: the ready frame, the final transcription, the complete
// frame.
func TestTheVoiceStreamCarriesTheReferenceFrames(t *testing.T) {
	disp := newAudioDispatcher(t, rcaudio.TranscriberFunc(
		func(_ context.Context, audio []byte, _ string) (string, error) {
			if len(audio) == 0 {
				return "", errors.New("no audio")
			}
			return "Hello terminal.", nil
		}))

	frames := make(chan string, 8)
	go func() {
		_, _ = disp.DispatchStream(context.Background(), "StreamAudioTranscription",
			`{"mimeType":"audio/pcm;rate=16000","preCursorText":"","postCursorText":"","cascadeId":"","continuous":false}`,
			func(raw json.RawMessage) error {
				frames <- string(raw)
				return nil
			})
	}()

	var ready string
	select {
	case ready = <-frames:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not open")
	}
	if !strings.Contains(ready, `"ready"`) || !strings.Contains(ready, `"sessionId"`) {
		t.Fatalf("the first frame is %s, want the ready frame", ready)
	}
	var answer struct {
		Ready struct {
			SessionID string `json:"sessionId"`
		} `json:"ready"`
	}
	if err := json.Unmarshal([]byte(ready), &answer); err != nil {
		t.Fatalf("the ready frame is not JSON: %v", err)
	}
	sessionID := answer.Ready.SessionID
	if sessionID == "" {
		t.Fatalf("the ready frame has no session id: %s", ready)
	}

	// The chunk is accepted with an empty answer.
	body, err := disp.DispatchDirect(context.Background(), "SendAudioChunk",
		`{"sessionId":"`+sessionID+`","data":"`+
			base64.StdEncoding.EncodeToString([]byte{1, 2, 3})+`","sequenceNumber":0}`)
	if err != nil {
		t.Fatalf("SendAudioChunk: %v", err)
	}
	if strings.TrimSpace(body) != "{}" {
		t.Fatalf("the chunk answer is %s, want {}", body)
	}

	// The session ends with an empty answer too.
	body, err = disp.DispatchDirect(context.Background(), "EndAudioSession",
		`{"sessionId":"`+sessionID+`"}`)
	if err != nil {
		t.Fatalf("EndAudioSession: %v", err)
	}
	if strings.TrimSpace(body) != "{}" {
		t.Fatalf("the end answer is %s, want {}", body)
	}

	// The transcription and the complete frame arrive on the stream.
	var seen []string
	deadline := time.After(10 * time.Second)
	for len(seen) < 2 {
		select {
		case frame := <-frames:
			seen = append(seen, frame)
		case <-deadline:
			t.Fatalf("the stream carried %v, want the transcription and the complete frame", seen)
		}
	}
	if !strings.Contains(seen[0], `"transcription"`) ||
		!strings.Contains(seen[0], `"isFinal":true`) ||
		!strings.Contains(seen[0], "Hello terminal.") {
		t.Fatalf("the transcription frame is %s", seen[0])
	}
	if !strings.Contains(seen[1], `"complete"`) {
		t.Fatalf("the last frame is %s, want the complete frame", seen[1])
	}
}

// TestAnUnknownSessionAnswersNotFound pins the error: a session that
// is not there answers with the not-found code.
func TestAnUnknownSessionAnswersNotFound(t *testing.T) {
	disp := newAudioDispatcher(t, nil)
	for _, method := range []string{"SendAudioChunk", "EndAudioSession"} {
		_, err := disp.DispatchDirect(context.Background(), method,
			`{"sessionId":"no-such-session","data":"aGk=","sequenceNumber":0}`)
		if err == nil {
			t.Fatalf("%s answered, want a not-found", method)
		}
		var coded *rpcCodedError
		if !errors.As(err, &coded) {
			t.Fatalf("the error is %T, want a coded error", err)
		}
		if coded.Code != -32018 {
			t.Fatalf("the code is %d, want -32018", coded.Code)
		}
	}
}
