package piagentbridge

import (
	"context"
	"testing"

	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
)

// TestBackendProviderModel pins the ai.Provider surface BackendProvider
// presents to PiG: the clone's provider id (so a session it drives is
// indistinguishable from one on the native gateway provider), and a model whose id
// is the caller's label rather than the wire model.
func TestBackendProviderModel(t *testing.T) {
	p := NewBackendProvider(backend.NewMock(), "MiniMax-M3")
	if p.ID() != ProviderID {
		t.Fatalf("ID() = %q, want %q", p.ID(), ProviderID)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	model := p.Model("claude-sonnet-4-6")
	if model == nil || model.ID != "claude-sonnet-4-6" {
		t.Fatalf("Model label = %+v, want the label", model)
	}
	if model.ProviderMeta.ProviderID != ProviderID {
		t.Fatalf("model provider meta = %+v", model.ProviderMeta)
	}
	if model.Provider == nil {
		t.Fatal("model has no provider")
	}
	if empty := p.Model(""); empty.ID != "MiniMax-M3" {
		t.Fatalf("empty label = %q, want the wire model", empty.ID)
	}
}

// TestBackendProviderStreamsText proves the provider speaks PiG's event
// vocabulary: the mock backend's chunks come out as a Start/Text/Done stream.
func TestBackendProviderStreamsText(t *testing.T) {
	p := NewBackendProvider(backend.NewMock(), "MiniMax-M3")
	ctx := context.Background()
	stream, err := p.Stream(ctx, transcriptOf(pi.UserText("hi")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	text := ""
	done := false
	for event := range stream.Events(ctx) {
		switch e := event.(type) {
		case pi.TextDeltaEvent:
			text += e.Delta
		case pi.DoneEvent:
			done = true
		}
	}
	if text == "" {
		t.Fatal("no text deltas")
	}
	if !done {
		t.Fatal("stream never ended cleanly")
	}
}
