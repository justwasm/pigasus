// ls_audio.go: the Web UI's voice input.
//
// 【一手】 2026-10-27, measured against the reference's own
// language_server (a standalone `language_server` on 127.0.0.1:64019
// with `-csrf_token=…`), the voice surface is three methods:
//
//	StreamAudioTranscription — a Connect stream. The first frame is
//	                          {"ready":{"sessionId":"…"}}, then
//	                          {"transcription":{"text":"…"}} for the
//	                          partials, then
//	                          {"transcription":{"text":"…","isFinal":true}},
//	                          then {"complete":{}}, and the stream ends.
//
//	SendAudioChunk {"sessionId":…,"data":<base64>,"sequenceNumber":…}
//	              → 200 {}
//
//	EndAudioSession {"sessionId":…}
//	              → 200 {}
//
// A session that is not known answers `not_found`:
//
//	404 {"code":"not_found","message":"not_found: … not found"}
//
// The frames are the reference's StreamAudioTranscriptionResponse:
// the `message` oneof written the way proto-JSON writes it — the
// member name at the top level. The Web UI reads them through
// protobuf-es, whose JavaScript object shape is `{case:"ready",
// value:{sessionId}}`; the transport flattens that into the
// canonical form here.
package remotecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/justwasm/pigasus/internal/rcaudio"
)

// lsAudioStreamRequest is the reference's StreamAudioTranscriptionRequest.
type lsAudioStreamRequest struct {
	MimeType       string `json:"mimeType"`
	PreCursorText  string `json:"preCursorText"`
	PostCursorText string `json:"postCursorText"`
	CascadeID      string `json:"cascadeId"`
	Continuous     bool   `json:"continuous"`
}

// lsAudioChunkRequest is the reference's SendAudioChunkRequest.
type lsAudioChunkRequest struct {
	SessionID      string `json:"sessionId"`
	Data           string `json:"data"`
	SequenceNumber int64  `json:"sequenceNumber"`
}

// lsAudioEndRequest is the reference's EndAudioSessionRequest.
type lsAudioEndRequest struct {
	SessionID string `json:"sessionId"`
}

// lsStreamAudioTranscription serves the Web UI's
// StreamAudioTranscription.
//
// The stream opens with the ready frame — the session id the caller
// sends its chunks to — and carries the transcriptions until the
// session ends.
func (d *Dispatcher) lsStreamAudioTranscription(ctx context.Context, payload string, emit func(json.RawMessage) error) (bool, error) {
	var req lsAudioStreamRequest
	if len(payload) > 0 {
		if err := json.Unmarshal([]byte(payload), &req); err != nil {
			return true, err
		}
	}
	registry := d.cfg.WebAudio
	if registry == nil {
		return false, nil
	}
	session := registry.Start(req.MimeType)
	for frame := range session.Frames() {
		if err := emit(lsAudioFrame(frame)); err != nil {
			registry.Forget(session.ID())
			return true, err
		}
	}
	registry.Forget(session.ID())
	return true, nil
}

// lsSendAudioChunk answers the Web UI's SendAudioChunk.
//
// The bytes are appended to the recording; the answer is empty, the
// way the reference answers it.
func (d *Dispatcher) lsSendAudioChunk(params json.RawMessage) (any, *rpcErrorBody) {
	var req lsAudioChunkRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	registry := d.cfg.WebAudio
	if registry == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "audio is not wired"}
	}
	session, ok := registry.Session(req.SessionID)
	if !ok {
		return nil, lsAudioNotFound(req.SessionID)
	}
	var data []byte
	if req.Data != "" {
		decoded, err := base64.StdEncoding.DecodeString(req.Data)
		if err != nil {
			return nil, invalidParams(lsInvalidBytesField("SendAudioChunkRequest", "data", req.Data))
		}
		data = decoded
	}
	session.Append(data)
	return map[string]any{}, nil
}

// lsEndAudioSession answers the Web UI's EndAudioSession.
//
// The answer is empty and comes immediately; the final transcription
// arrives on the stream afterwards, the way the reference answers it.
func (d *Dispatcher) lsEndAudioSession(params json.RawMessage) (any, *rpcErrorBody) {
	var req lsAudioEndRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	registry := d.cfg.WebAudio
	if registry == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "audio is not wired"}
	}
	session, ok := registry.Session(req.SessionID)
	if !ok {
		return nil, lsAudioNotFound(req.SessionID)
	}
	session.End()
	return map[string]any{}, nil
}

// lsAudioFrame renders one frame the way the reference's proto-JSON
// does.
//
// The members are written in the reference's field order (the text
// before the flag), so the answer reads the way the reference's does.
func lsAudioFrame(frame rcaudio.Frame) json.RawMessage {
	switch frame.Kind {
	case rcaudio.FrameReady:
		return lsAudioJSON(lsAudioReadyFrame{Ready: lsAudioReady{SessionID: frame.SessionID}})
	case rcaudio.FrameTranscription:
		return lsAudioJSON(lsAudioTranscriptionFrame{Transcription: lsAudioTranscription{
			Text:    frame.Text,
			IsFinal: frame.IsFinal,
		}})
	default:
		return lsAudioJSON(lsAudioCompleteFrame{Complete: lsAudioComplete{}})
	}
}

// lsAudioReadyFrame is the frame that names the session.
type lsAudioReadyFrame struct {
	Ready lsAudioReady `json:"ready"`
}

// lsAudioReady is the payload of the ready frame.
type lsAudioReady struct {
	SessionID string `json:"sessionId"`
}

// lsAudioTranscriptionFrame is the frame that carries text.
type lsAudioTranscriptionFrame struct {
	Transcription lsAudioTranscription `json:"transcription"`
}

// lsAudioTranscription is the payload of a transcription frame.
type lsAudioTranscription struct {
	Text    string `json:"text"`
	IsFinal bool   `json:"isFinal,omitempty"`
}

// lsAudioCompleteFrame is the frame that ends the session.
type lsAudioCompleteFrame struct {
	Complete lsAudioComplete `json:"complete"`
}

// lsAudioComplete is the payload of the complete frame.
type lsAudioComplete struct{}

// lsAudioJSON renders a value as compact JSON.
func lsAudioJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}

// lsAudioNotFound is the answer for a session that is not known.
//
// 【一手】 2026-10-27, the reference's own answer:
//
//	404 {"code":"not_found","message":"not_found: audio session not found: <id>"}
func lsAudioNotFound(sessionID string) *rpcErrorBody {
	return &rpcErrorBody{
		Code:    -32018,
		Message: "not_found: audio session not found: " + sessionID,
	}
}
