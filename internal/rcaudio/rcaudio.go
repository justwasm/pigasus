// Package rcaudio — the Web UI's voice input: the audio session
// registry the LanguageServerService's audio methods call into.
//
// 【一手】 2026-10-27, measured against the reference's own
// language_server (a standalone `language_server` on 127.0.0.1:64019
// with `-csrf_token=…`), the voice surface is three methods:
//
//	StreamAudioTranscription — a Connect stream; the first frame is
//	                          {"ready":{"sessionId":"…"}}, then
//	                          {"transcription":{"text":"…"}} for the
//	                          partials, then
//	                          {"transcription":{"text":"…","isFinal":true}},
//	                          then {"complete":{}}
//	SendAudioChunk         — {"sessionId":…,"data":<base64>,"sequenceNumber":…}
//	                          → 200 {}
//	EndAudioSession        — {"sessionId":…}
//	                          → 200 {}
//
// The reference's own transcript of a synthetic sample (the file
// /tmp/speech.pcm, 16 kHz mono s16le) was:
//
//	{"ready":{"sessionId":"606015e5-cbee-447a-8b14-3d15517999dd"}}
//	{"transcription":{"text":"Hello"}}
//	{"transcription":{"text":"Hello"}}
//	{"transcription":{"text":"Hello terminal.","isFinal":true}}
//	{"complete":{}}
//
// The reference transcribes the audio with a *local* engine: the
// language_server answers the transcript without a cloud call. The
// clone has no local engine, so the transcription rides the Gemini
// transport the clone already speaks (the same inlineData part an
// image attachment uses — the CloudCode dialect writes the mime type
// and the bytes, and Gemini reads `audio/pcm;rate=16000` as raw
// PCM). The wire shape is the reference's either way.
//
// The difference is *when* the text arrives: the reference streams a
// partial hypothesis while the user is still speaking, the clone
// transcribes once the session ends (one model call instead of one
// per second of speech).
//
// Stdlib + internal/backend.
package rcaudio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// FrameKind names the oneof member a frame carries — the reference's
// StreamAudioTranscriptionResponse.message.
type FrameKind int

const (
	// FrameReady carries the session id the caller sends its chunks
	// to.
	FrameReady FrameKind = iota
	// FrameTranscription carries text: a partial one, or the final
	// one when IsFinal is set.
	FrameTranscription
	// FrameComplete ends the session.
	FrameComplete
)

// Frame is one message of the transcription stream.
type Frame struct {
	// Kind names the oneof member the frame carries.
	Kind FrameKind
	// SessionID is the payload of a FrameReady frame.
	SessionID string
	// Text is the payload of a FrameTranscription frame.
	Text string
	// IsFinal marks the last transcription of a session.
	IsFinal bool
}

// Transcriber turns a recorded audio buffer into text.
//
// The clone's own implementation asks the Gemini transport
// (cloudcode.go); a test answers a fixed string.
type Transcriber interface {
	// Transcribe returns the text of the audio. mimeType is the
	// recording's own mime type ("audio/pcm;rate=16000").
	Transcribe(ctx context.Context, audio []byte, mimeType string) (string, error)
}

// TranscriberFunc is a Transcriber built from a function.
type TranscriberFunc func(ctx context.Context, audio []byte, mimeType string) (string, error)

// Transcribe calls the function.
func (f TranscriberFunc) Transcribe(ctx context.Context, audio []byte, mimeType string) (string, error) {
	return f(ctx, audio, mimeType)
}

// Registry owns the live audio sessions.
type Registry struct {
	mu       sync.Mutex
	sessions map[string]*Session
	// transcriber turns a finished recording into text. nil → the
	// sessions complete without text.
	transcriber Transcriber
}

// NewRegistry returns an empty registry.
func NewRegistry(transcriber Transcriber) *Registry {
	return &Registry{sessions: map[string]*Session{}, transcriber: transcriber}
}

// Start opens a session and returns it.
//
// mimeType is the recording's own mime type; the reference's Web UI
// sends "audio/pcm;rate=16000".
func (r *Registry) Start(mimeType string) *Session {
	session := &Session{
		id:          newSessionID(),
		mimeType:    mimeType,
		transcriber: r.transcriber,
		frames:      make(chan Frame, 8),
	}
	r.mu.Lock()
	r.sessions[session.id] = session
	r.mu.Unlock()
	// The ready frame opens the stream: it names the session the
	// caller sends its chunks to.
	session.push(Frame{Kind: FrameReady, SessionID: session.id})
	return session
}

// Session returns the live session with that id.
func (r *Registry) Session(sessionID string) (*Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[sessionID]
	return session, ok
}

// Forget drops a session from the registry.
func (r *Registry) Forget(sessionID string) {
	r.mu.Lock()
	delete(r.sessions, sessionID)
	r.mu.Unlock()
}

// Session is one recording: the bytes the caller sent, and the frames
// the stream carries.
type Session struct {
	id          string
	mimeType    string
	transcriber Transcriber

	mu    sync.Mutex
	audio []byte
	ended bool

	frames    chan Frame
	closeOnce sync.Once
}

// ID is the session's identifier.
func (s *Session) ID() string { return s.id }

// MimeType is the recording's mime type.
func (s *Session) MimeType() string { return s.mimeType }

// Append adds recorded bytes to the session.
func (s *Session) Append(data []byte) {
	if len(data) == 0 {
		return
	}
	s.mu.Lock()
	s.audio = append(s.audio, data...)
	s.mu.Unlock()
}

// Audio is the recorded bytes so far.
func (s *Session) Audio() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.audio...)
}

// Frames is the channel the stream reads.
func (s *Session) Frames() <-chan Frame { return s.frames }

// End marks the recording finished and starts the final
// transcription.
//
// The call answers immediately: the reference's EndAudioSession
// answers `{}` and the final transcription arrives on the stream
// afterwards.
func (s *Session) End() {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	audio := append([]byte(nil), s.audio...)
	s.mu.Unlock()

	go s.finalize(audio)
}

// finalize transcribes the recording and pushes the closing frames.
func (s *Session) finalize(audio []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if s.transcriber != nil && len(audio) > 0 {
		if text, err := s.transcriber.Transcribe(ctx, audio, s.mimeType); err == nil {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				s.push(Frame{Kind: FrameTranscription, Text: trimmed, IsFinal: true})
			}
		}
	}
	s.push(Frame{Kind: FrameComplete})
	s.close()
}

// push hands a frame to the stream.
func (s *Session) push(frame Frame) {
	defer func() {
		// The stream may have gone away; a closed channel would
		// panic here.
		_ = recover()
	}()
	select {
	case s.frames <- frame:
	default:
	}
}

// close ends the stream.
func (s *Session) close() {
	s.closeOnce.Do(func() { close(s.frames) })
}

// newSessionID mints a uuid v4 shaped identifier, the shape the
// reference's session ids have.
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("audio-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}
