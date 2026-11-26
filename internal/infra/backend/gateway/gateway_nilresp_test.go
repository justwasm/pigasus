package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

// TestReadGatewayBodyOnceNilResponseIsAConnectionDrop pins the crash guard for
// every protocol: a mid-stream reopen that yields no response leaves the
// pipeline holding nil, and the reader must classify that as a connection drop
// (so the retry budget absorbs it) rather than dereference resp.Body.
//
// The panic this prevents:
//
//	panic: runtime error: invalid memory address or nil pointer dereference
//	gateway.(*GatewayClient).readOpenAIBody(...)  gateway_stream.go:233
func TestReadGatewayBodyOnceNilResponseIsAConnectionDrop(t *testing.T) {
	protocols := []struct {
		name string
		cfg  GatewayConfig
	}{
		{"openai", GatewayConfig{URL: "https://example.invalid/v1", Type: GatewayProtocolOpenAI}},
		{"anthropic", GatewayConfig{URL: "https://example.invalid/v1", Type: GatewayProtocolAnthropic}},
		{"responses", GatewayConfig{URL: "https://example.invalid/v1", Type: GatewayProtocolResponses}},
	}

	for _, tc := range protocols {
		t.Run(tc.name, func(t *testing.T) {
			client, ok := NewGatewayWithConfig(tc.cfg).(*GatewayClient)
			if !ok {
				t.Fatalf("NewGatewayWithConfig returned %T, want *GatewayClient", NewGatewayWithConfig(tc.cfg))
			}

			out := make(chan backend.StreamChunk, 4)
			var err error
			// The regression: this used to fault on resp.Body.
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("readGatewayBodyOnce panicked on a nil response: %v", r)
					}
				}()
				err = client.readGatewayBodyOnce(context.Background(), nil, "test-model", out)
			}()

			if err == nil {
				t.Fatal("readGatewayBodyOnce(nil resp) = nil error, want a connection-dropped error")
			}
			if !retry.IsConnectionDropped(err) {
				t.Fatalf("error = %v, want a connection-dropped error so the retry budget absorbs it", err)
			}
			if got := len(out); got != 0 {
				t.Fatalf("emitted %d chunk(s) for a missing response, want none", got)
			}
		})
	}
}

// TestReadGatewayBodyOnceNilResponseKeepsTheEndpointInTheError makes sure the
// guard reports the endpoint it was talking to, so the retry log line and the
// user-facing error still name the failing hop.
func TestReadGatewayBodyOnceNilResponseKeepsTheEndpointInTheError(t *testing.T) {
	client := NewGatewayWithConfig(GatewayConfig{
		URL:  "https://example.invalid/v1",
		Type: GatewayProtocolOpenAI,
	}).(*GatewayClient)

	out := make(chan backend.StreamChunk, 4)
	err := client.readGatewayBodyOnce(context.Background(), nil, "test-model", out)
	var dropped *retry.ConnectionDroppedError
	if !errors.As(err, &dropped) {
		t.Fatalf("error = %v, want *retry.ConnectionDroppedError", err)
	}
	if dropped.URL != client.baseURL {
		t.Errorf("dropped.URL = %q, want %q", dropped.URL, client.baseURL)
	}
	if closed, ok := any(out).(interface{ Len() int }); ok && closed.Len() != 0 {
		t.Errorf("emitted %d chunk(s), want none", closed.Len())
	}
}

// compile-time assertion kept honest by the test file's use of the constants.
var _ = http.StatusOK
