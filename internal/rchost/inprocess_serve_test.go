package rchost

import (
	"context"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
)

func TestStartInProcessServe(t *testing.T) {
	engine := &mockEngine{
		frames: []agentlink.Frame{
			{Kind: agentlink.KindText, Text: "in-process ok"},
		},
	}
	backend := NewEngineCascadeBackend(engine, "mock-agent")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	disp, stop, err := StartInProcessServe(ctx, backend, false, nil)
	if err != nil {
		t.Fatalf("StartInProcessServe failed: %v", err)
	}
	defer stop()

	if disp == nil {
		t.Fatal("expected non-nil dispatcher")
	}
}
