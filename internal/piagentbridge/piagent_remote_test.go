package piagentbridge

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/tools"
)

// TestTheRemoteTurnAdvertisesTheReferenceToolSurface checks that a
// remote-control turn offers the model the reference's own tool
// declarations.
//
// 【一手】 2026-10-26: the request the remote-control path sent to the
// model carried an empty `tools` array — `SkipBuiltinTools` had removed
// PiG's own tools and nothing had replaced them — while the system
// prompt (the reference's, byte for byte) describes run_command,
// search_web and the file tools. A model that reads the prompt and
// finds no declaration for them has no way to act: it thinks about the
// tools it does not have, and the turn ends with the reasoning alone.
func TestTheRemoteTurnAdvertisesTheReferenceToolSurface(t *testing.T) {
	client := &fakeClient{turns: [][]backend.StreamChunk{{
		{Text: "ok", Done: true},
	}}}
	factory := NewRemoteCascadeSession(RemoteCascadeOptions{
		Backend:   client,
		Workspace: t.TempDir(),
		AgentDir:  t.TempDir(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := factory(ctx, remotecontrol.MessagePrompt{
		Text:  "what is the weather in Beijing?",
		Model: "claude-sonnet-4-6",
	}, remotecontrol.MessageSendOptions{})
	if err != nil {
		t.Fatalf("the session factory: %v", err)
	}
	defer func() { _ = sess.Close() }()

	events := make(chan piagent.AgentEvent, 64)
	if _, err := sess.Send(ctx, "what is the weather in Beijing?", events); err != nil {
		t.Fatalf("Send: %v", err)
	}

	requests := client.requests()
	if len(requests) == 0 {
		t.Fatal("the model was never called")
	}
	advertised := map[string]bool{}
	for _, decl := range requests[0].Tools {
		advertised[decl.Name] = true
	}
	for _, want := range tools.ModelDeclarations() {
		if !advertised[want.Name] {
			t.Errorf("the turn does not advertise %q", want.Name)
		}
	}
	if len(requests[0].Tools) != len(tools.ModelDeclarations()) {
		t.Errorf("the turn advertises %d tools, the reference sends %d: %+v",
			len(requests[0].Tools), len(tools.ModelDeclarations()), requests[0].Tools)
	}
}

// TestTheRemoteSessionReplaysTheConversation checks that the turns
// before this one reach the model.
//
// The Web UI's composer sends one message at a time, and the session
// the factory mints is a fresh one, so without the replay every
// message would be answered as if it were the first one.
func TestTheRemoteSessionReplaysTheConversation(t *testing.T) {
	client := &fakeClient{turns: [][]backend.StreamChunk{{
		{Text: "ok", Done: true},
	}}}
	factory := NewRemoteCascadeSession(RemoteCascadeOptions{
		Backend:   client,
		Workspace: t.TempDir(),
		AgentDir:  t.TempDir(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := factory(ctx, remotecontrol.MessagePrompt{
		Text:  "the second question",
		Model: "claude-sonnet-4-6",
		History: []remotecontrol.MessageHistoryTurn{
			{Role: "user", Text: "the first question"},
			{Role: "assistant", Text: "the first answer"},
		},
	}, remotecontrol.MessageSendOptions{})
	if err != nil {
		t.Fatalf("the session factory: %v", err)
	}
	defer func() { _ = sess.Close() }()

	events := make(chan piagent.AgentEvent, 64)
	if _, err := sess.Send(ctx, "the second question", events); err != nil {
		t.Fatalf("Send: %v", err)
	}

	requests := client.requests()
	if len(requests) == 0 {
		t.Fatal("the model was never called")
	}
	var sawUser, sawAssistant bool
	for _, message := range requests[0].Messages {
		switch message.Role {
		case "user":
			if strings.Contains(message.WireText(), "the first question") {
				sawUser = true
			}
		case "assistant":
			if strings.Contains(message.Content, "the first answer") {
				sawAssistant = true
			}
		}
	}
	if !sawUser {
		t.Errorf("the request does not carry the earlier question: %+v", requests[0].Messages)
	}
	if !sawAssistant {
		t.Errorf("the request does not carry the earlier answer: %+v", requests[0].Messages)
	}
}

// TestTheRemoteSessionWithoutAHistory checks that a turn with nothing
// before it asks the model exactly one question.
func TestTheRemoteSessionWithoutAHistory(t *testing.T) {
	client := &fakeClient{turns: [][]backend.StreamChunk{{
		{Text: "ok", Done: true},
	}}}
	factory := NewRemoteCascadeSession(RemoteCascadeOptions{
		Backend:   client,
		Workspace: t.TempDir(),
		AgentDir:  t.TempDir(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := factory(ctx, remotecontrol.MessagePrompt{
		Text:  "the only question",
		Model: "claude-sonnet-4-6",
	}, remotecontrol.MessageSendOptions{})
	if err != nil {
		t.Fatalf("the session factory: %v", err)
	}
	defer func() { _ = sess.Close() }()

	events := make(chan piagent.AgentEvent, 64)
	if _, err := sess.Send(ctx, "the only question", events); err != nil {
		t.Fatalf("Send: %v", err)
	}

	requests := client.requests()
	if len(requests) == 0 {
		t.Fatal("the model was never called")
	}
	for _, message := range requests[0].Messages {
		if message.Role == "user" && !strings.Contains(message.WireText(), "the only question") {
			t.Errorf("the request carries an unexpected user turn: %+v", message)
		}
	}
}

// TestTheRemoteSessionCarriesTheAttachedPicture pins the last hop of
// the Web UI's media surface: a picture the user attached reaches the
// model with the turn.
//
// The picture rides the user message as PiG's own image block — the
// shape PiG's BuildUserContent builds — and the bridge carries it in
// the neutral message's Images.
func TestTheRemoteSessionCarriesTheAttachedPicture(t *testing.T) {
	client := &fakeClient{turns: [][]backend.StreamChunk{{
		{Text: "ok", Done: true},
	}}}
	factory := NewRemoteCascadeSession(RemoteCascadeOptions{
		Backend:   client,
		Workspace: t.TempDir(),
		AgentDir:  t.TempDir(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A real one-pixel PNG: PiG's own image policy decodes and
	// re-encodes an attachment, and a body that is not an image at
	// all is dropped from the turn.
	picture, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC")
	if err != nil {
		t.Fatalf("decode the picture: %v", err)
	}
	sess, err := factory(ctx, remotecontrol.MessagePrompt{
		Text:  "what is this?",
		Model: "claude-sonnet-4-6",
		Images: []remotecontrol.MessageImage{
			{MIMEType: "image/png", Data: picture},
		},
	}, remotecontrol.MessageSendOptions{})
	if err != nil {
		t.Fatalf("the session factory: %v", err)
	}
	defer func() { _ = sess.Close() }()

	events := make(chan piagent.AgentEvent, 64)
	if _, err := sess.Send(ctx, "what is this?", events); err != nil {
		t.Fatalf("Send: %v", err)
	}

	requests := client.requests()
	if len(requests) == 0 {
		t.Fatal("the model was never called")
	}
	var images []backend.ImagePart
	for _, message := range requests[0].Messages {
		if message.Role == "user" {
			images = message.Images
		}
	}
	if len(images) != 1 {
		t.Fatalf("the request carries %d images, want 1: %+v", len(images), requests[0].Messages)
	}
	if images[0].MIMEType != "image/png" {
		t.Errorf("the image's mime type = %q, want image/png", images[0].MIMEType)
	}
	if len(images[0].Data) != len(picture) {
		t.Errorf("the image holds %d bytes, want %d", len(images[0].Data), len(picture))
	}
}
