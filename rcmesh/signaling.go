package rcmesh

import "encoding/json"

// SignalingEnvelope is JSON embedded inside SignalingMessageDetails.payload
// (a string in api.proto). It is distinct from the outer proto-JSON request:
// these four IDs/tags use snake_case and payload is raw JSON, not base64.
// Recovered from the agy 1.4.11 Go type at 0x10988b010.
type SignalingEnvelope struct {
	MessageID   string          `json:"message_id,omitempty"`
	SenderID    string          `json:"sender_id,omitempty"`
	RecipientID string          `json:"recipient_id,omitempty"`
	PayloadType string          `json:"payload_type,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// SessionDescription matches Pion's custom SDPType JSON encoding. The wire
// carries "offer" or "answer", despite SDPType's underlying Go integer kind.
type SessionDescription struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}
