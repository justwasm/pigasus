// media_test.go — the gateway's two dialects carry a picture.
//
// The neutral shape is the mime type and the bytes
// (backend.ImagePart); the OpenAI and the Anthropic dialects spell it
// their own way:
//
//	OpenAI     {"type":"image_url","image_url":{"url":"data:image/png;base64,…"}}
//	Anthropic  {"type":"image","source":{"type":"base64","media_type":"image/png",…}}
//
// 【一手】 both are the shapes PiG's own dialects write (ai/openai.go,
// ai/anthropic.go) — the upstream the clone's gateway transport
// mirrors.
package gateway

import (
	"encoding/base64"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// mediaBytes is the picture the tests attach.
var mediaBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// mediaMessage is a user message with one picture in it.
func mediaMessage() backend.Message {
	return backend.Message{
		Role:    "user",
		Content: "what is this?",
		Images: []backend.ImagePart{
			{MIMEType: "image/png", Data: mediaBytes},
		},
	}
}

// TestTheGatewayRequestCarriesThePicture pins the OpenAI dialect's
// content array.
func TestTheGatewayRequestCarriesThePicture(t *testing.T) {
	messages := formatOpenAIMessages(backend.StreamRequest{Messages: []backend.Message{mediaMessage()}})
	if len(messages) != 1 {
		t.Fatalf("the request carries %d messages, want 1", len(messages))
	}
	parts, ok := messages[0].Content.([]gatewayContentPart)
	if !ok {
		t.Fatalf("the message's content = %T, want a content array", messages[0].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("the content array holds %d parts, want the text and the picture: %+v", len(parts), parts)
	}
	if parts[0].Type != "text" || parts[0].Text != "what is this?" {
		t.Errorf("the first part = %+v, want the text", parts[0])
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil {
		t.Fatalf("the second part = %+v, want an image_url part", parts[1])
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(mediaBytes)
	if parts[1].ImageURL.URL != want {
		t.Errorf("the image url = %q, want %q", parts[1].ImageURL.URL, want)
	}
}

// TestATextOnlyGatewayMessageKeepsItsString pins that a message with
// no picture still carries a plain string: the OpenAI dialect spells
// a text-only message's content as a string, and the clone's own
// transcripts do too.
func TestATextOnlyGatewayMessageKeepsItsString(t *testing.T) {
	messages := formatOpenAIMessages(backend.StreamRequest{Messages: []backend.Message{{Role: "user", Content: "hello"}}})
	if len(messages) != 1 {
		t.Fatalf("the request carries %d messages, want 1", len(messages))
	}
	text, ok := messages[0].Content.(string)
	if !ok || text != "hello" {
		t.Fatalf("the message's content = %#v, want the string %q", messages[0].Content, "hello")
	}
}

// TestTheAnthropicRequestCarriesThePicture pins the Messages
// dialect's image block.
func TestTheAnthropicRequestCarriesThePicture(t *testing.T) {
	messages := formatAnthropicMessages([]backend.Message{mediaMessage()}, nil)
	if len(messages) != 1 {
		t.Fatalf("the request carries %d messages, want 1", len(messages))
	}
	blocks := messages[0].Content
	if len(blocks) != 2 {
		t.Fatalf("the message holds %d blocks, want the text and the picture: %+v", len(blocks), blocks)
	}
	if blocks[0].Type != "text" || blocks[0].Text != "what is this?" {
		t.Errorf("the first block = %+v, want the text", blocks[0])
	}
	if blocks[1].Type != "image" || blocks[1].Source == nil {
		t.Fatalf("the second block = %+v, want an image block", blocks[1])
	}
	if blocks[1].Source.Type != "base64" {
		t.Errorf("the source's type = %q, want base64", blocks[1].Source.Type)
	}
	if blocks[1].Source.MediaType != "image/png" {
		t.Errorf("the source's media type = %q, want image/png", blocks[1].Source.MediaType)
	}
	if blocks[1].Source.Data != base64.StdEncoding.EncodeToString(mediaBytes) {
		t.Errorf("the source's data = %q, want the picture's base64", blocks[1].Source.Data)
	}
}
