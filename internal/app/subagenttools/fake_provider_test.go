package subagenttools

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/justwasm/pigasus/actor"
)

type scriptedProvider struct {
	Response string
	Calls    []scriptedCall
	FailWith error
	mu       sync.Mutex
}

type scriptedCall struct {
	LastText string
}

type scriptedStream struct {
	chunks []actor.StreamChunk
	idx    int
}

func (s *scriptedStream) Recv() (actor.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return actor.StreamChunk{}, io.EOF
	}
	c := s.chunks[s.idx]
	s.idx++
	return c, nil
}

func (s *scriptedStream) Close() error {
	return nil
}

func (p *scriptedProvider) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	resp := p.Response
	if resp == "" {
		resp = "stubbed subagent reply"
	}
	if p.FailWith != nil {
		err := p.FailWith
		p.mu.Unlock()
		return nil, err
	}
	lastText := ""
	if len(req.Messages) > 0 {
		lastText = req.Messages[len(req.Messages)-1].Content
	}
	p.Calls = append(p.Calls, scriptedCall{LastText: lastText})
	p.mu.Unlock()

	return &scriptedStream{
		chunks: []actor.StreamChunk{{Delta: resp, FinishReason: "stop"}},
	}, nil
}

func (p *scriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.Calls)
}

func TestScriptedProviderSatisfiesInterface(t *testing.T) {
	var _ actor.Provider = (*scriptedProvider)(nil)

	p := &scriptedProvider{Response: "hello-world"}
	stream, err := p.Generate(context.Background(), actor.GenerateRequest{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if chunk.Delta != "hello-world" {
		t.Fatalf("chunk.Delta = %q, want %q", chunk.Delta, "hello-world")
	}
	if p.callCount() != 1 {
		t.Fatalf("callCount = %d, want 1", p.callCount())
	}
}

func TestScriptedProviderFailWith(t *testing.T) {
	p := &scriptedProvider{FailWith: errors.New("provider offline")}
	stream, err := p.Generate(context.Background(), actor.GenerateRequest{})
	if err == nil {
		t.Fatal("Generate returned nil error")
	}
	if stream != nil {
		t.Fatalf("expected nil stream on failure, got %v", stream)
	}
	if err.Error() != "provider offline" {
		t.Fatalf("err.Error() = %q, want %q", err.Error(), "provider offline")
	}
}
