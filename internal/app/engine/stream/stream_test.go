package stream_test

import (
	"context"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/modular"
	"github.com/justwasm/pigasus/internal/modular/engines/stream"
)

func TestStreamEngineRegistration(t *testing.T) {
	eng, ok := modular.GetEngine("stream")
	if !ok || eng == nil {
		t.Fatalf("expected stream engine to be registered")
	}
	if eng.Name() != "stream" {
		t.Fatalf("expected stream, got %s", eng.Name())
	}
}

func TestStreamEngineExecution(t *testing.T) {
	eng := stream.NewEngine()

	var chunks []string
	input := modular.TurnInput{
		Prompt: "hello",
		Model:  "mock-model",
		OnChunk: func(c backend.StreamChunk) error {
			if c.Text != "" {
				chunks = append(chunks, c.Text)
			}
			return nil
		},
	}

	res, err := eng.Send(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected Send error: %v", err)
	}
	if res.TurnCount == 0 {
		t.Fatalf("expected TurnCount > 0")
	}
	combined := strings.Join(chunks, "")
	if len(combined) == 0 {
		t.Fatalf("expected streamed chunks")
	}
}
