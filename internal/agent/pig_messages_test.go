package agent

import (
	"context"
	"encoding/base64"
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/piagentbridge"
)

// TestPigMessagesKeepsThePlainTextArm pins that a user turn with no
// picture is still the union's plain-text arm.
//
// The picture branch must not disturb the text-only turn: every byte the
// clone sent before it learned to attach images has to keep going out the
// same way.
func TestPigMessagesKeepsThePlainTextArm(t *testing.T) {
	messages := pigMessages([]backend.Message{{Role: "user", Content: "hello"}})
	if len(messages) != 1 || messages[0].User == nil {
		t.Fatalf("messages = %+v, want one user message", messages)
	}
	text, ok := messages[0].User.Content.(pi.UserText)
	if !ok {
		t.Fatalf("content is %T, want pi.UserText", messages[0].User.Content)
	}
	if string(text) != "hello" {
		t.Errorf("content = %q, want %q", string(text), "hello")
	}
}

// TestPigMessagesCarriesTheAttachedImage pins that a picture the user
// attached reaches PiG as an image block.
//
// agy 1.4.9's image pasting attaches a picture to the turn; the clone
// used to drop it here, so the picture never left the TUI.
func TestPigMessagesCarriesTheAttachedImage(t *testing.T) {
	payload := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	messages := pigMessages([]backend.Message{{
		Role:    "user",
		Content: "what is this?",
		Images: []backend.ImagePart{
			{MIMEType: "image/png", Data: payload},
		},
	}})
	if len(messages) != 1 || messages[0].User == nil {
		t.Fatalf("messages = %+v, want one user message", messages)
	}
	blocks, ok := messages[0].User.Content.(pi.UserContentBlocks)
	if !ok {
		t.Fatalf("content is %T, want pi.UserContentBlocks", messages[0].User.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2 (the text and the picture)", len(blocks))
	}
	text, ok := blocks[0].(pi.TextContent)
	if !ok {
		t.Fatalf("blocks[0] is %T, want pi.TextContent", blocks[0])
	}
	if text.Text != "what is this?" {
		t.Errorf("the text block holds %q, want %q", text.Text, "what is this?")
	}
	image, ok := blocks[1].(pi.ImageContent)
	if !ok {
		t.Fatalf("blocks[1] is %T, want pi.ImageContent", blocks[1])
	}
	if image.MimeType != "image/png" {
		t.Errorf("mime type = %q, want %q", image.MimeType, "image/png")
	}
	if image.Data != base64.StdEncoding.EncodeToString(payload) {
		t.Errorf("the picture's payload = %q, want the base64 of the bytes", image.Data)
	}
}

// TestPigMessagesDropsAnEmptyPicture pins that a picture with no bytes is
// not sent: the model would answer a broken image with a guess.
func TestPigMessagesDropsAnEmptyPicture(t *testing.T) {
	messages := pigMessages([]backend.Message{{
		Role:    "user",
		Content: "hello",
		Images:  []backend.ImagePart{{MIMEType: "image/png"}},
	}})
	text, ok := messages[0].User.Content.(pi.UserText)
	if !ok {
		t.Fatalf("content is %T, want pi.UserText", messages[0].User.Content)
	}
	if string(text) != "hello" {
		t.Errorf("content = %q, want %q", string(text), "hello")
	}
}

// TestAttachedPictureReachesTheTransport walks the whole leg: the clone's
// own message shape goes into PiG's transcript and comes back out as the
// transport's image part.
//
// This is the link that was broken. Everything downstream of PiG — the
// CloudCode `inlineData` block, the gateway's `image_url` data URL, the
// Anthropic image source — reads backend.Message.Images, and this test
// says the picture survives the trip in both directions.
func TestAttachedPictureReachesTheTransport(t *testing.T) {
	payload := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01}
	messages := pigMessages([]backend.Message{{
		Role:    "user",
		Content: "what is this?",
		Images: []backend.ImagePart{
			{MIMEType: "image/png", Data: payload},
			{MIMEType: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff}},
		},
	}})

	transcript := pi.NormalizeContext(pi.Context{
		Messages: piagent.ConvertToLLM(messages),
	})
	request := piagentbridge.ToStreamRequest(transcript, "m", pi.StreamOptions{})
	if len(request.Messages) != 1 {
		t.Fatalf("request carries %d messages, want 1", len(request.Messages))
	}
	got := request.Messages[0]
	if got.Content != "what is this?" {
		t.Errorf("the message's text = %q, want %q", got.Content, "what is this?")
	}
	if len(got.Images) != 2 {
		t.Fatalf("the message carries %d pictures, want 2", len(got.Images))
	}
	if got.Images[0].MIMEType != "image/png" {
		t.Errorf("pictures[0] mime = %q, want %q", got.Images[0].MIMEType, "image/png")
	}
	if string(got.Images[0].Data) != string(payload) {
		t.Errorf("pictures[0] = %v, want the bytes it went in with", got.Images[0].Data)
	}
	if got.Images[1].MIMEType != "image/jpeg" {
		t.Errorf("pictures[1] mime = %q, want %q", got.Images[1].MIMEType, "image/jpeg")
	}
	if len(got.Images[1].Data) != 3 {
		t.Errorf("pictures[1] holds %d bytes, want 3", len(got.Images[1].Data))
	}
}

// TestAttachedPictureReachesTheGatewayRequest is the end-to-end leg: the
// picture the user attached in the TUI is in the request body the gateway
// receives.
//
// The chain is the clone's own: the TUI puts the picture in the user
// message (internal/cliapp/tui_prompt_submit.go), the turn becomes PiG's
// transcript (pigMessages), the derived provider writes the OpenAI-shaped
// content array, and the picture rides in it as a `data:` URL.
func TestAttachedPictureReachesTheGatewayRequest(t *testing.T) {
	payload := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01}
	gateway := newScriptedGateway(t, []string{gatewayText("a picture"), gatewayFinish("stop"), "[DONE]"})
	client := &countingGatewayBackend{cfg: backend.GatewayConfig{
		URL:            gateway.server.URL,
		APIKey:         "test",
		ExternalModel:  "MiniMax-M3",
		TimeoutSeconds: 5,
	}}

	l, err := New(Options{
		Backend:      client,
		DefaultModel: "MiniMax-M3",
		Handler:      func(chunk backend.StreamChunk) error { return nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{
			Role:    "user",
			Content: "what is this?",
			Images: []backend.ImagePart{
				{MIMEType: "image/png", Data: payload},
			},
		}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	requests := gateway.requests()
	if len(requests) != 1 {
		t.Fatalf("gateway requests = %d, want 1", len(requests))
	}
	messages, _ := requests[0]["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("the request carries %d messages, want 1", len(messages))
	}
	user, _ := messages[0].(map[string]any)
	parts, _ := user["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("the user message carries %d content parts, want 2 (the text and the picture)", len(parts))
	}
	picture, _ := parts[1].(map[string]any)
	if picture["type"] != "image_url" {
		t.Fatalf("the second part is %v, want an image_url part", picture["type"])
	}
	url, _ := picture["image_url"].(map[string]any)
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
	if url["url"] != want {
		t.Errorf("the picture's url = %v, want %q", url["url"], want)
	}
}
