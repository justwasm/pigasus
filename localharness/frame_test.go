// frame_test.go — the protojson frame dialect.
//
// The dialect is what the shipped Python SDK speaks, so the tests pin the
// *shape* of the JSON rather than its bytes: `protojson` deliberately
// varies its whitespace, so a byte comparison would be flaky by design.
package localharness

import (
	"encoding/json"
	"testing"

	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// TestMarshalFrameDialect pins the protojson dialect the websocket frames
// use.
//
// 【一手】 The dialect comes from the shipped SDK's
// `google/antigravity/proto/localharness_pb2.py`, which serialises with
// `json_format.MessageToJson` — camelCase field names, int64 as a string,
// int32 as a number, enums as their names, a `oneof` flattened into the
// parent.
func TestMarshalFrameDialect(t *testing.T) {
	step := &pb.StepUpdate{}
	step.SetTextDelta("hello")
	step.SetState(pb.StepUpdate_STATE_ACTIVE)
	step.SetStepIndex(3)

	ev := &pb.OutputEvent{}
	ev.SetSeqNum(7)
	ev.SetTimestampMicros(1234567890)
	ev.SetStepUpdate(step)

	data, err := MarshalFrame(ev)
	if err != nil {
		t.Fatalf("MarshalFrame: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("the frame is not JSON: %v (%s)", err, data)
	}

	// int64 fields are JSON strings.
	seq, ok := raw["seqNum"].(string)
	if !ok {
		t.Fatalf("seqNum is %T, want a string: %s", raw["seqNum"], data)
	}
	if seq != "7" {
		t.Errorf("seqNum = %q, want %q", seq, "7")
	}
	stamp, ok := raw["timestampMicros"].(string)
	if !ok {
		t.Fatalf("timestampMicros is %T, want a string: %s", raw["timestampMicros"], data)
	}
	if stamp != "1234567890" {
		t.Errorf("timestampMicros = %q, want %q", stamp, "1234567890")
	}

	// The oneof member is flattened under its own name.
	update, ok := raw["stepUpdate"].(map[string]any)
	if !ok {
		t.Fatalf("stepUpdate is %T, want an object: %s", raw["stepUpdate"], data)
	}
	// uint32 fields are JSON numbers.
	index, ok := update["stepIndex"].(float64)
	if !ok {
		t.Fatalf("stepIndex is %T, want a number: %s", update["stepIndex"], data)
	}
	if index != 3 {
		t.Errorf("stepIndex = %v, want 3", index)
	}
	// Enums are their names.
	if state := update["state"]; state != "STATE_ACTIVE" {
		t.Errorf("state = %v, want STATE_ACTIVE", state)
	}
	if delta := update["textDelta"]; delta != "hello" {
		t.Errorf("textDelta = %v, want %q", delta, "hello")
	}
}

// TestMarshalFrameInt32IsANumber pins the other half of the numeric
// dialect: int32 and uint32 are numbers, not strings.
func TestMarshalFrameInt32IsANumber(t *testing.T) {
	cfg := &pb.OutputConfig{}
	cfg.SetPort(8080)

	data, err := MarshalFrame(cfg)
	if err != nil {
		t.Fatalf("MarshalFrame: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("the frame is not JSON: %v (%s)", err, data)
	}
	port, ok := raw["port"].(float64)
	if !ok {
		t.Fatalf("port is %T, want a number: %s", raw["port"], data)
	}
	if port != 8080 {
		t.Errorf("port = %v, want 8080", port)
	}
}

// TestUnmarshalFrameDiscardsUnknownFields pins the tolerant decode.
//
// The descriptor this build speaks was recovered from a shipped SDK, not
// from the harness binary's own schema. A field the harness adds must not
// break the connection.
func TestUnmarshalFrameDiscardsUnknownFields(t *testing.T) {
	payload := []byte(`{"haltRequest":true,"somethingNew":{"a":1}}`)

	ev := &pb.InputEvent{}
	if err := UnmarshalFrame(payload, ev); err != nil {
		t.Fatalf("UnmarshalFrame: %v", err)
	}
	if !ev.GetHaltRequest() {
		t.Error("the known field was lost")
	}
}

// TestUnmarshalFrameRejectsGarbage pins that a malformed frame is an
// error, not a silently empty message.
func TestUnmarshalFrameRejectsGarbage(t *testing.T) {
	ev := &pb.InputEvent{}
	if err := UnmarshalFrame([]byte("{not json"), ev); err == nil {
		t.Fatal("UnmarshalFrame accepted a malformed frame")
	}
}

// TestFrameRoundTrip pins that a message survives the codec unchanged.
func TestFrameRoundTrip(t *testing.T) {
	original := &pb.InputEvent{}
	tool := &pb.ToolResponse{}
	tool.SetId("call-1")
	tool.SetResponseJson(`{"ok":true}`)
	original.SetToolResponse(tool)

	data, err := MarshalFrame(original)
	if err != nil {
		t.Fatalf("MarshalFrame: %v", err)
	}
	decoded := &pb.InputEvent{}
	if err := UnmarshalFrame(data, decoded); err != nil {
		t.Fatalf("UnmarshalFrame: %v", err)
	}
	if got := decoded.GetToolResponse().GetId(); got != "call-1" {
		t.Errorf("the tool response id = %q, want %q", got, "call-1")
	}
	if got := decoded.GetToolResponse().GetResponseJson(); got != `{"ok":true}` {
		t.Errorf("the tool response payload = %q, want %q", got, `{"ok":true}`)
	}
}
