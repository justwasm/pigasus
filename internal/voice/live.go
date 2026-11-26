// Package voice implements Antigravity parity for real-time voice mode.
package voice

import (
	"context"
	"time"
)

// LiveToolCall represents a tool or function invocation requested by the live talker model.
type LiveToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// LiveServerEvent is an event emitted by an upstream live voice session.
type LiveServerEvent struct {
	// Audio contains 24kHz 16-bit PCM mono audio bytes produced by the model.
	Audio []byte

	// VoiceTranscript contains the text of what the model is speaking.
	VoiceTranscript string

	// UserSpeech contains the recognized transcription of what the user said.
	UserSpeech string

	// Interim indicates whether the UserSpeech is an interim (partial) recognition.
	Interim bool

	// Interrupted is true if the model was interrupted by the user (barge-in).
	Interrupted bool

	// TurnComplete indicates the model has finished its speaking turn.
	TurnComplete bool

	// ToolCalls contains tool/function calls emitted by the live talker (e.g. ThinkerHandoff).
	ToolCalls []LiveToolCall
}

// LiveSessionConfig configures an individual live voice streaming session.
type LiveSessionConfig struct {
	CascadeID    string
	Voice        string
	SampleRate   int
	Model        string
	SystemPrompt string
	APIKey       string
	Endpoint     string
	QuotaProject string
	// DirectTools exposes direct local/web tools (search_web, fetch_url,
	// run_command, read_file) alongside the Thinker-Talker declarations.
	DirectTools bool
}

// LiveSession is a full-duplex live audio streaming session with an upstream AI engine.
type LiveSession interface {
	// SendAudio sends a chunk of raw 16kHz 16-bit PCM audio from the user's microphone.
	SendAudio(pcm16k []byte) error

	// SendText sends a text message into the live session.
	SendText(text string) error

	// SendToolResponse sends the execution result of a tool call back to the live session.
	SendToolResponse(id string, name string, result any) error

	// SendThinkerNote sends a background task progress note to the live talker.
	SendThinkerNote(note string) error

	// SendThinkerAnswer sends the final answer of a Thinker task to the live talker.
	SendThinkerAnswer(answer string) error

	// SendWaitingOnUser notifies the live talker that the background task is waiting for user confirmation or input.
	SendWaitingOnUser(question string) error

	// SendTaskStopped notifies the live talker that the background task was stopped or cancelled.
	SendTaskStopped() error

	// SendMedia sends a real-time visual image or video frame (e.g. image/jpeg, image/png) into the live session.
	SendMedia(mimeType string, data []byte) error

	// Receive waits for and returns the next event from the engine.
	// Returns an error (e.g. io.EOF or context.Canceled) when the session ends.
	Receive() (*LiveServerEvent, error)

	// Close terminates the session cleanly.
	Close() error
}

// HandshakeAwareSession can optionally be implemented by LiveSessions to report
// immediate upstream handshake failures (e.g. gRPC or WebSocket auth/permission rejections).
type HandshakeAwareSession interface {
	WaitHandshake(timeout time.Duration) error
}

// LiveBackend creates full-duplex real-time voice sessions.
type LiveBackend interface {
	// CreateSession starts a new live session with the backend.
	CreateSession(ctx context.Context, cfg LiveSessionConfig) (LiveSession, error)
}
