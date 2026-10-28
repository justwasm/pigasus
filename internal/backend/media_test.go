// Package backend — media_test.go: the transports carry a picture.
//
// The neutral shape is the mime type and the bytes
// (backend.ImagePart); every transport spells it its own way:
//
//	CloudCode  {"inlineData":{"mimeType":"image/png","data":"<base64>"}}
//	OpenAI     {"type":"image_url","image_url":{"url":"data:image/png;base64,…"}}
//	Anthropic  {"type":"image","source":{"type":"base64","media_type":"image/png",…}}
//	Gemini     {"inlineData":{"mimeType":"image/png","data":"<base64>"}}
//
// 【一手】 the first and the last are the reference's own spelling —
// its JSON wire types (the geminicode package) name the member
// `inlineData` with a `mimeType` and a `data` member.
//
// 【一手】 the middle two are the shapes PiG's own dialects write
// (ai/openai.go, ai/anthropic.go) — the upstream the clone's gateway
// transport mirrors.
package backend

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/geminicode"
)

// mediaBytes is the picture the tests attach.
var mediaBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// mediaMessage is a user message with one picture in it.
func mediaMessage() Message {
	return Message{
		Role:    "user",
		Content: "what is this?",
		Images: []ImagePart{
			{MIMEType: "image/png", Data: mediaBytes},
		},
	}
}

// cloudCodeMediaMessage is the same message in the dialect's own terms.
func cloudCodeMediaMessage() geminicode.TurnMessage {
	return geminicode.TurnMessage{
		Role:    "user",
		Content: "what is this?",
		Images: []geminicode.TurnImage{
			{MIMEType: "image/png", Data: mediaBytes},
		},
	}
}

// TestTheCloudCodeRequestCarriesThePicture pins the CloudCode
// dialect's own spelling of an inline image.
func TestTheCloudCodeRequestCarriesThePicture(t *testing.T) {
	envelope := geminicode.BuildRequest(geminicode.Turn{
		Model:    "gemini-3.1-pro",
		Messages: []geminicode.TurnMessage{cloudCodeMediaMessage()},
	})
	if len(envelope.Request.Contents) != 1 {
		t.Fatalf("the request carries %d contents, want 1", len(envelope.Request.Contents))
	}
	parts := envelope.Request.Contents[0].Parts
	if len(parts) != 2 {
		t.Fatalf("the user content carries %d parts, want the text and the picture: %+v", len(parts), parts)
	}
	if parts[0].Text != "what is this?" {
		t.Errorf("the first part = %q, want the text", parts[0].Text)
	}
	if parts[1].InlineData == nil {
		t.Fatalf("the second part carries no inline data: %+v", parts[1])
	}
	if parts[1].InlineData.MIMEType != "image/png" {
		t.Errorf("the inline data's mime type = %q, want image/png", parts[1].InlineData.MIMEType)
	}
	if parts[1].InlineData.Data != base64.StdEncoding.EncodeToString(mediaBytes) {
		t.Errorf("the inline data = %q, want the picture's base64", parts[1].InlineData.Data)
	}

	// The wire spells it the way the reference does.
	raw, err := json.Marshal(parts[1])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"inlineData":{"mimeType":"image/png"`) {
		t.Errorf("the part on the wire = %s, want the reference's inlineData spelling", raw)
	}
}

// TestATextOnlyMessageKeepsItsShape pins that the change did not
// touch the messages that carry no picture.
func TestATextOnlyMessageKeepsItsShape(t *testing.T) {
	envelope := geminicode.BuildRequest(geminicode.Turn{
		Model:    "gemini-3.1-pro",
		Messages: []geminicode.TurnMessage{{Role: "user", Content: "hello"}},
	})
	parts := envelope.Request.Contents[0].Parts
	if len(parts) != 1 || parts[0].Text != "hello" || parts[0].InlineData != nil {
		t.Fatalf("a text-only message's parts = %+v, want the one text part", parts)
	}
}

// TestTheGatewayRequestCarriesThePicture pins the OpenAI dialect's
// content array.
func TestTheGatewayRequestCarriesThePicture(t *testing.T) {
	messages := formatOpenAIMessages(StreamRequest{Messages: []Message{mediaMessage()}})
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
	messages := formatOpenAIMessages(StreamRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
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
	messages := formatAnthropicMessages([]Message{mediaMessage()}, nil)
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

// TestTheGeminiRequestCarriesThePicture pins the Gemini dialect's
// inline data part.
func TestTheGeminiRequestCarriesThePicture(t *testing.T) {
	contents := geminiContentsFor(mediaMessage(), nil)
	if len(contents) != 1 {
		t.Fatalf("the request carries %d contents, want 1", len(contents))
	}
	parts := contents[0].Parts
	if len(parts) != 2 {
		t.Fatalf("the user content carries %d parts, want the text and the picture: %+v", len(parts), parts)
	}
	if parts[0].Text != "what is this?" {
		t.Errorf("the first part = %q, want the text", parts[0].Text)
	}
	if parts[1].InlineData == nil {
		t.Fatalf("the second part carries no inline data: %+v", parts[1])
	}
	if parts[1].InlineData.MIMEType != "image/png" {
		t.Errorf("the inline data's mime type = %q, want image/png", parts[1].InlineData.MIMEType)
	}
	if parts[1].InlineData.Data != base64.StdEncoding.EncodeToString(mediaBytes) {
		t.Errorf("the inline data = %q, want the picture's base64", parts[1].InlineData.Data)
	}
}

// TestAPictureWithoutTextIsAMessage pins the image-only case: the
// text part is dropped, so the request carries the picture alone.
func TestAPictureWithoutTextIsAMessage(t *testing.T) {
	contents := geminiContentsFor(Message{
		Role:   "user",
		Images: []ImagePart{{MIMEType: "image/png", Data: mediaBytes}},
	}, nil)
	if len(contents) != 1 {
		t.Fatalf("the request carries %d contents, want 1", len(contents))
	}
	parts := contents[0].Parts
	if len(parts) != 1 || parts[0].InlineData == nil {
		t.Fatalf("the user content's parts = %+v, want the picture alone", parts)
	}
}
