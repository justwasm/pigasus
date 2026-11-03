package agyproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCloudCodeServer returns a mock CloudCode streaming backend.
func fakeCloudCodeServer(t *testing.T, replyText string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		// Emit CloudCode SSE chunk
		envelope := map[string]any{
			"response": map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"role": "model",
							"parts": []map[string]any{
								{"text": replyText},
							},
						},
						"finishReason": "STOP",
					},
				},
				"usageMetadata": map[string]any{
					"promptTokenCount":     10,
					"candidatesTokenCount": 5,
					"totalTokenCount":      15,
				},
			},
		}
		b, _ := json.Marshal(envelope)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestResponsesHTTP_GET_MethodNotAllowed(t *testing.T) {
	srv, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	for _, path := range []string{"/v1/responses", "/responses"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: got status %d, want 405", path, rec.Code)
		}
		if rec.Header().Get("Allow") != "POST" {
			t.Errorf("%s: got Allow %q, want POST", path, rec.Header().Get("Allow"))
		}
	}
}

func TestResponsesHTTP_POST_Unary(t *testing.T) {
	backend := fakeCloudCodeServer(t, "Hello from Codex Responses API!")
	srv, err := New(Config{Upstream: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	reqBody := `{
		"model": "gpt-5",
		"input": "hi there",
		"stream": false
	}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["object"] != "response" {
		t.Errorf("got object %v, want response", resp["object"])
	}
	if resp["status"] != "completed" {
		t.Errorf("got status %v, want completed", resp["status"])
	}

	output, ok := resp["output"].([]any)
	if !ok || len(output) == 0 {
		t.Fatalf("output empty or not list: %v", resp["output"])
	}

	first := output[0].(map[string]any)
	if first["type"] != "message" {
		t.Errorf("got output item type %v, want message", first["type"])
	}
}

func TestResponsesHTTP_POST_Stream(t *testing.T) {
	backend := fakeCloudCodeServer(t, "Streaming response text")
	srv, err := New(Config{Upstream: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	reqBody := `{
		"model": "claude-sonnet-4-6",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "tell me something"}]}
		],
		"stream": true
	}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, body: %s", rec.Code, rec.Body.String())
	}

	raw := rec.Body.String()
	if !strings.Contains(raw, "event: response.created") {
		t.Errorf("missing response.created in stream: %s", raw)
	}
	if !strings.Contains(raw, "event: response.output_item.added") {
		t.Errorf("missing response.output_item.added in stream: %s", raw)
	}
	if !strings.Contains(raw, "event: response.output_text.delta") {
		t.Errorf("missing response.output_text.delta in stream: %s", raw)
	}
	if !strings.Contains(raw, "Streaming response text") {
		t.Errorf("missing text delta content in stream: %s", raw)
	}
	if !strings.Contains(raw, "event: response.completed") {
		t.Errorf("missing response.completed in stream: %s", raw)
	}
}

func TestResponsesHTTP_PreviousResponseID(t *testing.T) {
	backend := fakeCloudCodeServer(t, "Answer 1")
	srv, err := New(Config{Upstream: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	// Turn 1
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"question 1","stream":false}`))
	srv.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("turn 1: %d %s", rec1.Code, rec1.Body.String())
	}

	var resp1 map[string]any
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)
	id1, _ := resp1["id"].(string)
	if id1 == "" {
		t.Fatalf("missing id in turn 1: %v", resp1)
	}

	// Turn 2 continues with previous_response_id
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt-5","previous_response_id":%q,"input":"question 2","stream":false}`, id1)))
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("turn 2: %d %s", rec2.Code, rec2.Body.String())
	}

	// Turn with invalid previous_response_id returns 400
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","previous_response_id":"resp_unknown_12345","input":"question 3","stream":false}`))
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown previous_response_id, got %d", rec3.Code)
	}
}

func TestResponsesHTTP_WithCustomTool(t *testing.T) {
	// Fake backend that emits a tool call for apply_patch
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		envelope := map[string]any{
			"response": map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"role": "model",
							"parts": []map[string]any{
								{
									"functionCall": map[string]any{
										"name": "apply_patch",
										"args": map[string]any{
											"input": "*** a.txt\n--- b.txt\n",
										},
									},
								},
							},
						},
						"finishReason": "STOP",
					},
				},
			},
		}
		b, _ := json.Marshal(envelope)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()

	srv, err := New(Config{Upstream: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	reqBody := `{
		"model": "gpt-5",
		"input": "apply the diff",
		"tools": [
			{"type": "custom", "name": "apply_patch", "format": {"type": "text"}}
		],
		"stream": true
	}`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "custom_tool_call") {
		t.Errorf("expected custom_tool_call in stream, got: %s", body)
	}
	if !strings.Contains(body, "apply_patch") {
		t.Errorf("expected apply_patch in stream, got: %s", body)
	}
}
