// Package remotecontrol — media_wire_test.go: the media RPCs on the
// wire.
//
// The Web UI speaks Connect-RPC in its v2 "binary unary" dialect: the
// request body is a binary protobuf and the answer has to be one too.
// The dispatcher, on the other hand, speaks JSON — the bridge between
// the two is `lsproto`.
//
// This test pins the media half of that bridge: the request the
// frontend sends (a `Media` message with the picture's bytes inline)
// decodes to the shape the handler reads, and the handler's answer
// encodes back into the schema's response message.
//
// 【一手】 the schema is the reference's own descriptor closure
// (out/ide-language-server/protos/language_server.protoset), and the
// frontend's request is what its own bundle builds:
//
//	{media:{mimeType:"image/png",payload:{case:"inlineData",value:…}}}
//
// — the bytes ride the `inline_data` member of the `payload` oneof
// (field 2 of exa.codeium_common_pb.Media).
package remotecontrol

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/lsproto"
)

// mediaRequestBytes is a binary-protobuf
// SaveMediaAsArtifactRequest with one picture in it:
//
//	SaveMediaAsArtifactRequest { media = 1 : Media }
//	Media { mime_type = 1, inline_data = 2 }
func mediaRequestBytes() []byte {
	media := []byte{0x0a, 0x09}
	media = append(media, []byte("image/png")...)
	media = append(media, 0x12, 0x03)
	media = append(media, []byte{0x01, 0x02, 0x03}...)
	body := []byte{0x0a, byte(len(media))}
	return append(body, media...)
}

// TestTheMediaRequestDecodesToTheShapeTheHandlerReads pins the
// request's half of the bridge.
func TestTheMediaRequestDecodesToTheShapeTheHandlerReads(t *testing.T) {
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	out, err := codec.DecodeRequest("SaveMediaAsArtifact", mediaRequestBytes())
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	var decoded struct {
		Media struct {
			MIMEType   string `json:"mimeType"`
			InlineData string `json:"inlineData"`
		} `json:"media"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if decoded.Media.MIMEType != "image/png" {
		t.Errorf("the media's mime type = %q, want image/png", decoded.Media.MIMEType)
	}
	if decoded.Media.InlineData == "" {
		t.Errorf("the media carries no inline data: %s", out)
	}
	// The bytes the handler reads are the picture's own.
	images := messageImagesFromMedia([]lsMediaItem{{
		MIMEType:   decoded.Media.MIMEType,
		InlineData: decoded.Media.InlineData,
	}})
	if len(images) != 1 || len(images[0].Data) != 3 {
		t.Fatalf("the decoded media read %d images of %d bytes, want one of three",
			len(images), len(images[0].Data))
	}
}

// TestTheMediaAnswersEncodeIntoTheSchema pins the answer's half of
// the bridge: the uri and the success flag are the schema's own
// fields, and the answer the frontend decodes is the one the
// dispatcher produced.
func TestTheMediaAnswersEncodeIntoTheSchema(t *testing.T) {
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	answer, err := codec.EncodeResponse("SaveMediaAsArtifact",
		[]byte(`{"uri":"remote-artifact://abc"}`))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	back, err := codec.DecodeResponse("SaveMediaAsArtifact", answer)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	var decoded struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(back, &decoded); err != nil {
		t.Fatalf("unmarshal %s: %v", back, err)
	}
	if decoded.URI != "remote-artifact://abc" {
		t.Errorf("the answer's uri = %q, want the artifact's uri", decoded.URI)
	}

	answer, err = codec.EncodeResponse("DeleteMediaArtifact", []byte(`{"success":true}`))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	back, err = codec.DecodeResponse("DeleteMediaArtifact", answer)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	var deleted struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(back, &deleted); err != nil {
		t.Fatalf("unmarshal %s: %v", back, err)
	}
	if !deleted.Success {
		t.Errorf("the answer = %s, want a success", back)
	}
}
