package agent

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

// TestLoop_ChunkFingerprintSurvivesPiG covers the third item. backend/gateway.go
// stamps SystemFingerprint on its usage record and cmd/openai-shim echoes it;
// ai.Usage has no field for it, so it only survives if it is carried beside the
// chunks. The mock stamps a different value, which also pins that the value
// comes off the wire rather than being derived from the provider.
func TestLoop_ChunkFingerprintSurvivesPiG(t *testing.T) {
	var meta *backend.ChunkMeta
	l, err := New(Options{
		Backend:      backend.NewMock(),
		DefaultModel: "MiniMax-M3",
		Handler: func(chunk backend.StreamChunk) error {
			if chunk.Done {
				meta = chunk.Meta
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if meta == nil {
		t.Fatal("done chunk carried no metadata")
	}
	if got := meta.SystemFingerprint; got != "shim-mock-1" {
		t.Errorf("SystemFingerprint = %q, want the transport's own value", got)
	}
	if meta.PromptTokens == 0 {
		t.Error("PromptTokens = 0; the usage record was dropped")
	}
}

// TestLoop_NativeProviderStampsTheGatewayFingerprint is the other half: with no
// usage record to read, the native gateway provider supplies the same value
// backend.GatewayClient would have.
func TestLoop_NativeProviderStampsTheGatewayFingerprint(t *testing.T) {
	gateway := newScriptedGateway(t, []string{
		gatewayText("hi"),
		gatewayUsage(7, 3, 0),
		gatewayFinish("stop"),
		"[DONE]",
	})

	var meta *backend.ChunkMeta
	l, err := New(Options{
		Provider:     gateway.provider(t),
		DefaultModel: "MiniMax-M3",
		Handler: func(chunk backend.StreamChunk) error {
			if chunk.Done {
				meta = chunk.Meta
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if meta == nil {
		t.Fatal("done chunk carried no metadata")
	}
	want := backend.GatewaySystemFingerprint(gateway.server.URL, "MiniMax-M3")
	if got := meta.SystemFingerprint; got != want {
		t.Errorf("SystemFingerprint = %q, want %q", got, want)
	}
	if meta.PromptTokens != 7 || meta.CompletionTokens != 3 {
		t.Errorf("tokens = %d/%d, want 7/3", meta.PromptTokens, meta.CompletionTokens)
	}
}

// TestLoop_NoUsageMeansNoMetadata pins the guard backend.GatewayClient has: its
// ChunkMeta is built inside the usage branch, so a transport that reports no
// metadata at all yields no metadata — not an empty one carrying a fingerprint.
func TestLoop_NoUsageMeansNoMetadata(t *testing.T) {
	var done bool
	var meta *backend.ChunkMeta
	l, err := New(Options{
		Backend:      &bareClient{text: "hi"},
		DefaultModel: "MiniMax-M3",
		Handler: func(chunk backend.StreamChunk) error {
			if chunk.Done {
				done, meta = true, chunk.Meta
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !done {
		t.Fatal("Handler never saw the done chunk")
	}
	if meta != nil {
		t.Errorf("meta = %+v, want nil without a usage record", meta)
	}
}

// bareClient answers with text and closes, carrying no metadata at all: the
// shape a gateway that ignores stream_options.include_usage produces.
type bareClient struct{ text string }

func (c *bareClient) Stream(ctx context.Context, _ backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	out := make(chan backend.StreamChunk, 2)
	out <- backend.StreamChunk{Text: c.text}
	out <- backend.StreamChunk{Done: true}
	close(out)
	return out, nil
}
