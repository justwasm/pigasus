// rcaudio_test.go: the audio session's rules.
//
// The expectations are the reference's own answers, measured against
// the reference's language_server on 2026-10-27 (see rcaudio.go's
// header for the captures).
package rcaudio

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// cannedTranscriber answers a fixed text and records what it was
// asked.
type cannedTranscriber struct {
	text string
	err  error
	seen []byte
}

func (c *cannedTranscriber) Transcribe(_ context.Context, audio []byte, _ string) (string, error) {
	c.seen = append(c.seen, audio...)
	return c.text, c.err
}

// TestTheSessionCarriesTheReferenceFrames pins the frame sequence: the
// ready frame, the final transcription, the complete frame.
func TestTheSessionCarriesTheReferenceFrames(t *testing.T) {
	transcriber := &cannedTranscriber{text: "Hello terminal."}
	registry := NewRegistry(transcriber)
	session := registry.Start("audio/pcm;rate=16000")

	if session.ID() == "" {
		t.Fatal("the session has no id")
	}
	session.Append([]byte{1, 2, 3})
	session.End()

	var frames []Frame
	deadline := time.After(5 * time.Second)
	for {
		select {
		case frame, ok := <-session.Frames():
			if !ok {
				// The stream is done; check the frames.
				if len(frames) != 3 {
					t.Fatalf("the stream carried %d frames, want 3", len(frames))
				}
				if frames[0].Kind != FrameReady || frames[0].SessionID != session.ID() {
					t.Fatalf("the first frame is %+v, want the ready frame", frames[0])
				}
				if frames[1].Kind != FrameTranscription ||
					frames[1].Text != "Hello terminal." || !frames[1].IsFinal {
					t.Fatalf("the second frame is %+v, want the final transcription", frames[1])
				}
				if frames[2].Kind != FrameComplete {
					t.Fatalf("the third frame is %+v, want the complete frame", frames[2])
				}
				if string(transcriber.seen) != "\x01\x02\x03" {
					t.Fatalf("the transcriber saw %q, want the recorded bytes", transcriber.seen)
				}
				return
			}
			frames = append(frames, frame)
		case <-deadline:
			t.Fatalf("the stream did not end; got %d frames", len(frames))
		}
	}
}

// TestAFailingTranscriberStillCompletes pins the failure path: a
// transcription that fails ends the session with the complete frame
// and no text, the way a session with no engine ends.
func TestAFailingTranscriberStillCompletes(t *testing.T) {
	registry := NewRegistry(&cannedTranscriber{err: errors.New("no engine")})
	session := registry.Start("audio/pcm;rate=16000")
	session.Append([]byte{1})
	session.End()

	var frames []Frame
	for frame := range session.Frames() {
		frames = append(frames, frame)
	}
	if len(frames) != 2 || frames[0].Kind != FrameReady || frames[1].Kind != FrameComplete {
		t.Fatalf("the stream carried %+v, want the ready and the complete frame", frames)
	}
}

// TestTheRegistryFindsAndForgetsSessions pins the registry's rules.
func TestTheRegistryFindsAndForgetsSessions(t *testing.T) {
	registry := NewRegistry(nil)
	session := registry.Start("")
	if _, ok := registry.Session(session.ID()); !ok {
		t.Fatal("the session is not in the registry")
	}
	if _, ok := registry.Session("no-such-session"); ok {
		t.Fatal("an unknown session is in the registry")
	}
	registry.Forget(session.ID())
	if _, ok := registry.Session(session.ID()); ok {
		t.Fatal("the forgotten session is still in the registry")
	}
}

// TestEndIsIdempotent pins the end: a session that is ended twice
// transcribes once.
func TestEndIsIdempotent(t *testing.T) {
	transcriber := &cannedTranscriber{text: "once"}
	registry := NewRegistry(transcriber)
	session := registry.Start("")
	session.End()
	session.End()
	for range session.Frames() {
	}
	if len(transcriber.seen) != 0 {
		t.Fatalf("the transcriber saw %q, want nothing", transcriber.seen)
	}
}

// TestTheFrameNamesAreStable pins the frame kinds' numbering: the
// wire depends on the names, not the numbers, but a rename that
// shifts them would change the answers.
func TestTheFrameNamesAreStable(t *testing.T) {
	if FrameReady != 0 || FrameTranscription != 1 || FrameComplete != 2 {
		t.Fatalf("the frame kinds are %d/%d/%d", FrameReady, FrameTranscription, FrameComplete)
	}
}

// TestTheSessionIDLooksLikeTheReferences pins the id's shape: the
// reference's session ids are uuid v4 strings.
func TestTheSessionIDLooksLikeTheReferences(t *testing.T) {
	registry := NewRegistry(nil)
	id := registry.Start("").ID()
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		t.Fatalf("the session id is %q, want a uuid", id)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(`{"id":"`+id+`"}`), &out); err != nil {
		t.Fatalf("the id is not JSON-clean: %v", err)
	}
}
