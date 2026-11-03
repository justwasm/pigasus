package rcmesh

import (
	"encoding/json"
	"testing"
)

func TestSignalingEnvelopeUsesRawNestedJSON(t *testing.T) {
	// These are recovered Go struct tags, not proto-JSON's camelCase names.
	message := SignalingEnvelope{MessageID: "m", SenderID: "browser", RecipientID: "host", PayloadType: "offer", Payload: json.RawMessage(`{"type":"offer","sdp":"v=0\r\n"}`)}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"message_id":"m","sender_id":"browser","recipient_id":"host","payload_type":"offer","payload":{"type":"offer","sdp":"v=0\r\n"}}`
	if string(encoded) != want {
		t.Fatalf("wire changed: %s", encoded)
	}
}
