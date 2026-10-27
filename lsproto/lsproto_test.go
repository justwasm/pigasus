package lsproto

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// newTestCodec builds a codec and fails the test if the embedded
// descriptor set does not load.
func newTestCodec(t *testing.T) *Codec {
	t.Helper()
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestServiceMethodCount pins the size of the surface the web UI
// speaks. The reference's LanguageServerService declares 351 RPCs.
func TestServiceMethodCount(t *testing.T) {
	c := newTestCodec(t)
	if got := c.MethodCount(); got != 351 {
		t.Fatalf("MethodCount = %d, want 351", got)
	}
}

// TestSharedIsCached checks that the process-wide codec is built once.
func TestSharedIsCached(t *testing.T) {
	a, err := Shared()
	if err != nil {
		t.Fatalf("Shared: %v", err)
	}
	b, err := Shared()
	if err != nil {
		t.Fatalf("Shared: %v", err)
	}
	if a != b {
		t.Fatal("Shared returned two different codecs")
	}
}

// TestDecodeRequest covers the inbound direction: a binary protobuf
// body becomes the proto-JSON the dispatcher expects.
func TestDecodeRequest(t *testing.T) {
	c := newTestCodec(t)
	md, ok := c.InputMessage("GetStatus")
	if !ok {
		t.Fatal("GetStatus is not in the descriptor set")
	}
	msg := dynamicpb.NewMessage(md)
	metaFd := md.Fields().ByName("metadata")
	msg.Set(metaFd, protoreflect.ValueOfMessage(dynamicpb.NewMessage(metaFd.Message())))
	body, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := c.DecodeRequest("GetStatus", body)
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, out)
	}
	if _, ok := got["metadata"]; !ok {
		t.Fatalf("decoded request has no metadata key: %s", out)
	}
}

// TestDecodeEmptyRequest checks that a body-less Connect request — the
// shape of GetMendelFlags — decodes to an empty object.
func TestDecodeEmptyRequest(t *testing.T) {
	c := newTestCodec(t)
	out, err := c.DecodeRequest("GetMendelFlags", nil)
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if string(out) != "{}" {
		t.Fatalf("DecodeRequest = %s, want {}", out)
	}
}

// TestEncodeResponse covers the outbound direction: the dispatcher's
// proto-JSON answer becomes a binary protobuf body.
func TestEncodeResponse(t *testing.T) {
	c := newTestCodec(t)
	answer := `{"webrtcMeshReady":true,"experimentConfig":{"experiments":[{"key":"a"}]}}`
	body, err := c.EncodeResponse("GetMendelFlags", []byte(answer))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("EncodeResponse produced an empty body")
	}
	round, err := c.DecodeResponse("GetMendelFlags", body)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(round, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, round)
	}
	if got["webrtcMeshReady"] != true {
		t.Fatalf("webrtcMeshReady = %v, want true (%s)", got["webrtcMeshReady"], round)
	}
}

// TestEncodeDropsUnknownFields checks the lenient path: a key the
// schema does not know is dropped, the known fields survive.
func TestEncodeDropsUnknownFields(t *testing.T) {
	c := newTestCodec(t)
	answer := `{"webrtcMeshReady":true,"bogusKey":123}`
	body, err := c.EncodeResponse("GetMendelFlags", []byte(answer))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	round, err := c.DecodeResponse("GetMendelFlags", body)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(round, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, round)
	}
	if _, ok := got["bogusKey"]; ok {
		t.Fatalf("unknown key survived: %s", round)
	}
	if got["webrtcMeshReady"] != true {
		t.Fatalf("webrtcMeshReady = %v, want true (%s)", got["webrtcMeshReady"], round)
	}
}

// TestEncodeDropsBadField checks that a field whose JSON type does
// not fit the schema is dropped without taking the message with it.
func TestEncodeDropsBadField(t *testing.T) {
	c := newTestCodec(t)
	answer := `{"webrtcMeshReady":"yes"}`
	body, err := c.EncodeResponse("GetMendelFlags", []byte(answer))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	round, err := c.DecodeResponse("GetMendelFlags", body)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(round, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, round)
	}
	if _, ok := got["webrtcMeshReady"]; ok {
		t.Fatalf("bad field survived: %s", round)
	}
}

// TestEncodeAcceptsBothSpellings checks that a dispatcher answer may
// use either the JSON name or the protobuf name.
func TestEncodeAcceptsBothSpellings(t *testing.T) {
	c := newTestCodec(t)
	body, err := c.EncodeResponse("GetMendelFlags", []byte(`{"webrtc_mesh_ready":true}`))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	round, err := c.DecodeResponse("GetMendelFlags", body)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(round, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, round)
	}
	if got["webrtcMeshReady"] != true {
		t.Fatalf("webrtcMeshReady = %v, want true (%s)", got["webrtcMeshReady"], round)
	}
}

// TestEncodeServerConfiguration covers the nested message the web UI
// fetches first.
func TestEncodeServerConfiguration(t *testing.T) {
	c := newTestCodec(t)
	answer := `{"config":{"devMode":true,"standalone":true,"workspaceId":"ws-1","maxNumTrackedWorkspaces":7}}`
	body, err := c.EncodeResponse("GetServerConfiguration", []byte(answer))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	round, err := c.DecodeResponse("GetServerConfiguration", body)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	var got struct {
		Config struct {
			DevMode                 bool   `json:"devMode"`
			Standalone              bool   `json:"standalone"`
			WorkspaceID             string `json:"workspaceId"`
			MaxNumTrackedWorkspaces int32  `json:"maxNumTrackedWorkspaces"`
		} `json:"config"`
	}
	if err := json.Unmarshal(round, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, round)
	}
	if !got.Config.DevMode || !got.Config.Standalone {
		t.Fatalf("config = %+v, want devMode and standalone set", got.Config)
	}
	if got.Config.WorkspaceID != "ws-1" {
		t.Fatalf("workspaceId = %q, want ws-1", got.Config.WorkspaceID)
	}
	if got.Config.MaxNumTrackedWorkspaces != 7 {
		t.Fatalf("maxNumTrackedWorkspaces = %d, want 7", got.Config.MaxNumTrackedWorkspaces)
	}
}

// TestEncodeEmptyListIsSafe is a regression test: assigning an unset
// list to a dynamic message panics ("assigning invalid type
// dynamicpb.emptyList"), and the panic used to take the whole daemon
// down from the tunnel's goroutine.
func TestEncodeEmptyListIsSafe(t *testing.T) {
	c := newTestCodec(t)
	body, err := c.EncodeResponse("ListProjects", []byte(`{"projectIds":[]}`))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	round, err := c.DecodeResponse("ListProjects", body)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(round, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, round)
	}
}

// TestEncodeEmptyListOfMessages is the same regression with a repeated
// *message* field, which is the shape the panic was reported on.
func TestEncodeEmptyListOfMessages(t *testing.T) {
	c := newTestCodec(t)
	body, err := c.EncodeResponse("SubscribeToPlugins", []byte(`{"plugins":[]}`))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	if _, err := c.DecodeResponse("SubscribeToPlugins", body); err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
}

// TestEncodeNonEmptyListKeepsItsElements checks that a populated
// repeated field still survives the copy.
func TestEncodeNonEmptyListKeepsItsElements(t *testing.T) {
	c := newTestCodec(t)
	body, err := c.EncodeResponse("ListProjects", []byte(`{"projectIds":["a","b"]}`))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	round, err := c.DecodeResponse("ListProjects", body)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	var got struct {
		ProjectIDs []string `json:"projectIds"`
	}
	if err := json.Unmarshal(round, &got); err != nil {
		t.Fatalf("decode json: %v (%s)", err, round)
	}
	if len(got.ProjectIDs) != 2 || got.ProjectIDs[0] != "a" {
		t.Fatalf("projectIds = %v, want [a b]", got.ProjectIDs)
	}
}

// TestUnknownMethod checks that an unknown method is an error rather
// than a silent empty message.
func TestUnknownMethod(t *testing.T) {
	c := newTestCodec(t)
	if c.HasMethod("NoSuchMethod") {
		t.Fatal("HasMethod(NoSuchMethod) = true")
	}
	if _, err := c.EncodeResponse("NoSuchMethod", []byte(`{}`)); err == nil {
		t.Fatal("EncodeResponse on an unknown method returned no error")
	}
	if _, err := c.DecodeRequest("NoSuchMethod", nil); err == nil {
		t.Fatal("DecodeRequest on an unknown method returned no error")
	}
}

// TestOutputMessageIsCallable checks that a method's output type
// resolves — the schema closure is complete.
func TestOutputMessageIsCallable(t *testing.T) {
	c := newTestCodec(t)
	md, ok := c.OutputMessage("HandleStreamingCommand")
	if !ok {
		t.Skip("HandleStreamingCommand is not in the descriptor set")
	}
	if md.FullName() == "" {
		t.Fatal("output message has no name")
	}
	var _ protoreflect.MessageDescriptor = md
}
