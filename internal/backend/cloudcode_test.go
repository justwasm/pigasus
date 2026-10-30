package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The dialect's own byte-level assertions live in the geminicode package —
// that is where the wire shapes are defined. What is pinned here is the
// adapter: the application's neutral turn reaches the wire, and the
// dialect's chunks come back as the application's chunks.

// adapterServer serves a fixed sequence of raw SSE lines and records the
// request bodies it saw.
type adapterServer struct {
	t      *testing.T
	frames []string
	bodies []map[string]any
}

func (s *adapterServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.t.Fatalf("decode body: %v", err)
	}
	s.bodies = append(s.bodies, body)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, f := range s.frames {
		_, _ = w.Write([]byte(f))
	}
}

func newAdapterClient(server *httptest.Server) *CloudCodeClient {
	return NewCloudCodeWithConfig(CloudCodeConfig{
		Endpoint:    server.URL,
		Model:       "gemini-3.8-flash-high",
		AccessToken: "test-token",
		SessionID:   42,
		HTTPClient:  server.Client(),
	})
}

// TestCloudCode_AdapterCarriesTheNeutralTurn pins that the application's
// neutral turn reaches the dialect's request envelope.
func TestCloudCode_AdapterCarriesTheNeutralTurn(t *testing.T) {
	srv := &adapterServer{t: t, frames: []string{
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"totalTokenCount\":4}}}\n\n",
	}}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := newAdapterClient(server)

	ch, err := c.Stream(context.Background(), StreamRequest{
		SystemPrompt: "be brief",
		Messages:     []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	var done *StreamChunk
	for chunk := range ch {
		text.WriteString(chunk.Text)
		if chunk.Done {
			chunk := chunk
			done = &chunk
		}
	}
	if text.String() != "ok" {
		t.Errorf("text = %q, want ok", text.String())
	}
	if done == nil || done.Meta == nil || done.Meta.TotalTokens != 4 {
		t.Fatalf("done = %+v", done)
	}
	if got := srv.bodies[0]["model"]; got != "gemini-3.8-flash-high" {
		t.Errorf("model = %v", got)
	}
	if got := srv.bodies[0]["project"]; got != CloudCodeProject {
		t.Errorf("project = %v, want %q", got, CloudCodeProject)
	}
	inner := srv.bodies[0]["request"].(map[string]any)
	if _, ok := inner["sessionId"]; !ok {
		t.Errorf("sessionId missing: %#v", inner)
	}
}

// TestCloudCode_AdapterMapsTheToolCall pins that a tool call comes back
// as the application's ToolCall, with its id.
func TestCloudCode_AdapterMapsTheToolCall(t *testing.T) {
	srv := &adapterServer{t: t, frames: []string{
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"name\":\"run_command\",\"args\":{\"CommandLine\":\"echo hi\"},\"id\":\"call_1\"}}]}}],\"usageMetadata\":{\"totalTokenCount\":9}}}\n\n",
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"totalTokenCount\":9}}}\n\n",
	}}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := newAdapterClient(server)

	ch, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "run it"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var call *ToolCall
	for chunk := range ch {
		if len(chunk.ToolCalls) > 0 {
			call = &chunk.ToolCalls[0]
		}
	}
	if call == nil || call.ID != "call_1" || call.Name != "run_command" {
		t.Fatalf("call = %+v", call)
	}
	if got := string(call.Arguments); got != `{"CommandLine":"echo hi"}` {
		t.Errorf("arguments = %q, want the model's own bytes", got)
	}
}

// TestCloudCode_AdapterMapsTheHTTPError pins that a non-2xx answer keeps
// the application's own error type, so the headless error reporter can
// classify it.
func TestCloudCode_AdapterMapsTheHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`))
	}))
	defer server.Close()
	c := newAdapterClient(server)

	ch, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("want synchronous error")
	}
	if ch != nil {
		t.Errorf("channel = %v, want nil", ch)
	}
	he, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("error type = %T, want *HTTPError", err)
	}
	if he.StatusCode != 429 || !strings.Contains(string(he.Body), "Resource has been exhausted") {
		t.Errorf("HTTPError = %#v", he)
	}
}

func TestCloudCode_AdapterInStreamError(t *testing.T) {
	srv := &adapterServer{t: t, frames: []string{
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"starting...\"}]}}]}}\n\n",
		"data: {\"error\":{\"code\":429,\"message\":\"Resource has been exhausted (e.g. check quota).\",\"status\":\"RESOURCE_EXHAUSTED\"}}\n\n",
	}}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := newAdapterClient(server)

	ch, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var doneCount int
	var gotErr error
	for chunk := range ch {
		if chunk.Done {
			doneCount++
		}
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}
	if doneCount != 1 {
		t.Errorf("doneCount = %d, want exactly 1 (no duplicate Done chunk)", doneCount)
	}
	if gotErr == nil {
		t.Fatalf("expected error chunk")
	}
	he, ok := gotErr.(*HTTPError)
	if !ok {
		t.Fatalf("error type = %T, want *HTTPError", gotErr)
	}
	if he.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", he.StatusCode)
	}
}
