package geminicode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// streamServer serves a fixed sequence of raw SSE lines and records the
// request bodies it saw.
type streamServer struct {
	t        *testing.T
	frames   []string
	bodies   []map[string]any
	status   int
	errBody  string
	requests int
}

func (s *streamServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.requests++
	if r.URL.Path != "/v1internal:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
		s.t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
		s.t.Errorf("Authorization = %q", got)
	}
	if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "antigravity/cli/1.4.4") {
		s.t.Errorf("User-Agent = %q", got)
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.t.Fatalf("decode body: %v", err)
	}
	s.bodies = append(s.bodies, body)
	if s.status != 0 && s.status != http.StatusOK {
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(s.errBody))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, f := range s.frames {
		_, _ = w.Write([]byte(f))
	}
}

func newTestClient(server *httptest.Server) *Client {
	return New(Config{
		Endpoint:       server.URL,
		Model:          "gemini-3.8-flash-high",
		AccessToken:    "test-token",
		SessionID:      42,
		HTTPClient:     server.Client(),
		ThinkingBudget: 1000,
	})
}

var textFrames = []string{
	"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hel\"}]}}],\"usageMetadata\":{\"promptTokenCount\":11,\"candidatesTokenCount\":1,\"totalTokenCount\":12}},\"traceId\":\"t\"}\n\n",
	"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"lo\"}]}}],\"usageMetadata\":{\"promptTokenCount\":11,\"candidatesTokenCount\":2,\"totalTokenCount\":13}},\"traceId\":\"t\"}\n\n",
	"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"thoughtSignature\":\"sig\",\"text\":\"\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":11,\"candidatesTokenCount\":2,\"totalTokenCount\":13}},\"traceId\":\"t\"}\n\n",
}

func collectChunks(t *testing.T, ch <-chan Chunk) []Chunk {
	t.Helper()
	var got []Chunk
	for c := range ch {
		got = append(got, c)
	}
	return got
}

func TestClient_TextStream(t *testing.T) {
	srv := &streamServer{t: t, frames: textFrames}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := newTestClient(server)

	ch, err := c.Stream(context.Background(), Turn{
		SystemPrompt: "be brief",
		Messages:     []TurnMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collectChunks(t, ch)
	var text strings.Builder
	var done *Chunk
	for i := range chunks {
		if chunks[i].Text != "" {
			text.WriteString(chunks[i].Text)
		}
		if chunks[i].Done {
			done = &chunks[i]
		}
	}
	if text.String() != "Hello" {
		t.Errorf("text = %q, want Hello", text.String())
	}
	if done == nil {
		t.Fatal("no Done chunk")
	}
	if done.Meta == nil || done.Meta.PromptTokens != 11 || done.Meta.TotalTokens != 13 {
		t.Errorf("meta = %+v", done.Meta)
	}

	verifyTextStreamEnvelope(t, srv.bodies[0])
}

func verifyTextStreamEnvelope(t *testing.T, body map[string]any) {
	t.Helper()
	if body["model"] != "gemini-3.8-flash-high" || body["project"] != "aicode-consumers" ||
		body["userAgent"] != "antigravity" || body["requestType"] != "agent" {
		t.Errorf("envelope = %#v", body)
	}
	inner := body["request"].(map[string]any)
	sys := inner["systemInstruction"].(map[string]any)
	if sys["role"] != "user" {
		t.Errorf("systemInstruction role = %v", sys["role"])
	}
	contents := inner["contents"].([]any)
	if len(contents) != 1 || contents[0].(map[string]any)["role"] != "user" {
		t.Errorf("contents = %#v", contents)
	}
	gen := inner["generationConfig"].(map[string]any)
	if gen["maxOutputTokens"] != float64(65536) {
		t.Errorf("maxOutputTokens = %v", gen["maxOutputTokens"])
	}
	if _, ok := inner["sessionId"]; !ok {
		t.Errorf("sessionId missing: %#v", inner)
	}
}

func TestClient_ToolCallAndSignatureReplay(t *testing.T) {
	srv := &streamServer{t: t, frames: []string{
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"thoughtSignature\":\"SIG-1\",\"functionCall\":{\"name\":\"run_command\",\"args\":{\"CommandLine\":\"echo hi\"},\"id\":\"call_1\"}}]}}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":4,\"totalTokenCount\":9}}}\n\n",
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":4,\"totalTokenCount\":9}}}\n\n",
	}}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := newTestClient(server)

	ch, err := c.Stream(context.Background(), Turn{
		Messages: []TurnMessage{{Role: "user", Content: "run it"}},
		Tools: []TurnTool{{
			Name:        "run_command",
			Description: "run",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"CommandLine":{"type":"string"}},"required":["CommandLine"],"additionalProperties":false}`),
		}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var call *TurnToolCall
	for chunk := range ch {
		if len(chunk.ToolCalls) > 0 {
			call = &chunk.ToolCalls[0]
		}
	}
	if call == nil || call.ID != "call_1" || call.Name != "run_command" {
		t.Fatalf("call = %+v", call)
	}

	// Second turn: the assistant call plus the tool result are replayed.
	ch, err = c.Stream(context.Background(), Turn{
		Messages: []TurnMessage{
			{Role: "user", Content: "run it"},
			{Role: "assistant", ToolCalls: []TurnToolCall{*call}},
			{Role: "tool", ToolCallID: "call_1", Name: "run_command", Content: "hi\n"},
		},
	})
	if err != nil {
		t.Fatalf("Stream 2: %v", err)
	}
	collectChunks(t, ch)

	verifyReplayContents(t, srv.bodies[1])
	verifyReplaySchema(t, srv.bodies[0])
}

func verifyReplayContents(t *testing.T, body map[string]any) {
	t.Helper()
	inner := body["request"].(map[string]any)
	contents := inner["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents = %d, want 3", len(contents))
	}
	asst := contents[1].(map[string]any)
	if asst["role"] != "model" {
		t.Errorf("assistant role = %v", asst["role"])
	}
	part := asst["parts"].([]any)[0].(map[string]any)
	if part["thoughtSignature"] != "SIG-1" {
		t.Errorf("thoughtSignature = %v", part["thoughtSignature"])
	}
	toolMsg := contents[2].(map[string]any)
	if toolMsg["role"] != "model" {
		t.Errorf("tool result role = %v, want model", toolMsg["role"])
	}
	fr := toolMsg["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "run_command" || fr["id"] != "call_1" {
		t.Errorf("functionResponse = %#v", fr)
	}
	if out := fr["response"].(map[string]any)["output"]; out != "hi\n" {
		t.Errorf("output = %q", out)
	}
}

func verifyReplaySchema(t *testing.T, body map[string]any) {
	t.Helper()
	firstInner := body["request"].(map[string]any)
	tools := firstInner["tools"].([]any)
	decls := tools[0].(map[string]any)["functionDeclarations"].([]any)
	params := decls[0].(map[string]any)["parameters"].(map[string]any)
	if params["type"] != "OBJECT" {
		t.Errorf("schema type = %v, want OBJECT", params["type"])
	}
	if _, ok := params["additionalProperties"]; ok {
		t.Errorf("additionalProperties survived: %#v", params)
	}
	prop := params["properties"].(map[string]any)["CommandLine"].(map[string]any)
	if prop["type"] != "STRING" {
		t.Errorf("property type = %v, want STRING", prop["type"])
	}
}

func TestClient_HTTPErrorIsSynchronous(t *testing.T) {
	srv := &streamServer{
		t:       t,
		status:  http.StatusTooManyRequests,
		errBody: `{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`,
	}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := newTestClient(server)

	ch, err := c.Stream(context.Background(), Turn{Messages: []TurnMessage{{Role: "user", Content: "hi"}}})
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

func TestClient_BareResponseFrame(t *testing.T) {
	srv := &streamServer{t: t, frames: []string{
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"bare\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"totalTokenCount\":3}}\n\n",
	}}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := newTestClient(server)
	ch, err := c.Stream(context.Background(), Turn{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text string
	sawDone := false
	for chunk := range ch {
		text += chunk.Text
		if chunk.Done {
			sawDone = true
		}
	}
	if text != "bare" || !sawDone {
		t.Errorf("text=%q done=%v", text, sawDone)
	}
}

func TestClient_ThinkingIsWrappedForSanitizer(t *testing.T) {
	srv := &streamServer{t: t, frames: []string{
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"thought\":true,\"text\":\"let me \"}]}}],\"usageMetadata\":{\"promptTokenCount\":5,\"totalTokenCount\":5}}}\n\n",
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"thought\":true,\"text\":\"compute\"}]}}],\"usageMetadata\":{\"promptTokenCount\":5,\"totalTokenCount\":5}}}\n\n",
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"42\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":1,\"totalTokenCount\":6}}}\n\n",
	}}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := newTestClient(server)

	ch, err := c.Stream(context.Background(), Turn{Messages: []TurnMessage{{Role: "user", Content: "q"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	for chunk := range ch {
		text.WriteString(chunk.Text)
	}
	if got, want := text.String(), "<think>let me compute</think>42"; got != want {
		t.Fatalf("streamed text = %q, want %q", got, want)
	}
}

func TestClient_ThinkingClosesBeforeToolCall(t *testing.T) {
	srv := &streamServer{t: t, frames: []string{
		"data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"thought\":true,\"text\":\"need a tool\"},{\"functionCall\":{\"name\":\"run_command\",\"args\":{},\"id\":\"c1\"}}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"totalTokenCount\":5}}}\n\n",
	}}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := newTestClient(server)

	ch, err := c.Stream(context.Background(), Turn{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	var calls int
	for chunk := range ch {
		text.WriteString(chunk.Text)
		calls += len(chunk.ToolCalls)
	}
	if got, want := text.String(), "<think>need a tool</think>"; got != want {
		t.Fatalf("streamed text = %q, want %q", got, want)
	}
	if calls != 1 {
		t.Fatalf("tool calls = %d, want 1", calls)
	}
}

func TestClient_DuplicateToolNamesDeduped(t *testing.T) {
	srv := &streamServer{t: t, frames: textFrames}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := newTestClient(server)

	ch, err := c.Stream(context.Background(), Turn{
		Tools: []TurnTool{
			{Name: "invoke_subagent", Parameters: json.RawMessage(`{"type":"object"}`)},
			{Name: "invoke_subagent", Parameters: json.RawMessage(`{"type":"object"}`)},
			{Name: "run_command", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	collectChunks(t, ch)
	inner := srv.bodies[0]["request"].(map[string]any)
	decls := inner["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)
	if len(decls) != 2 {
		t.Fatalf("declarations = %d, want 2 (duplicate invoke_subagent dropped)", len(decls))
	}
	if decls[0].(map[string]any)["name"] != "invoke_subagent" || decls[1].(map[string]any)["name"] != "run_command" {
		t.Errorf("declarations = %#v", decls)
	}
}

func TestClient_ModelFromRequest(t *testing.T) {
	srv := &streamServer{t: t, frames: textFrames}
	server := httptest.NewServer(srv)
	defer server.Close()
	c := New(Config{
		Endpoint:    server.URL,
		AccessToken: "test-token",
		HTTPClient:  server.Client(),
	})
	ch, err := c.Stream(context.Background(), Turn{Model: "claude-sonnet-4-6"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	collectChunks(t, ch)
	if got := srv.bodies[0]["model"]; got != "claude-sonnet-4-6" {
		t.Errorf("model = %v", got)
	}
	labels := srv.bodies[0]["request"].(map[string]any)["labels"].(map[string]any)
	if labels["used_claude"] != "true" {
		t.Errorf("labels = %#v", labels)
	}
}

// TestSchemaProtoIntStrings pins the round trip the tools package
// performs for PiG: minLength/minItems are numbers in PiG's copy (draft
// 2020-12 requires integers) and protojson strings on the wire — the
// reference sends `"minLength":"1"`.
func TestSchemaProtoIntStrings(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"Message":{"type":"string","minLength":1}},"required":["Message"],"minItems":2}`)
	got := Schema(raw)
	if got["type"] != "OBJECT" {
		t.Errorf("type = %v, want OBJECT", got["type"])
	}
	if got["minItems"] != "2" {
		t.Errorf("minItems = %#v, want the protojson string %q", got["minItems"], "2")
	}
	props, _ := got["properties"].(map[string]any)
	msg, _ := props["Message"].(map[string]any)
	if msg["minLength"] != "1" {
		t.Errorf("minLength = %#v, want the protojson string %q", msg["minLength"], "1")
	}
}

func TestDecoder_InStreamError(t *testing.T) {
	dec := NewDecoder(nil)
	chunks := dec.Feed(`data: {"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota)","status":"RESOURCE_EXHAUSTED"}}`)
	if len(chunks) != 1 {
		t.Fatalf("chunks len = %d, want 1", len(chunks))
	}
	if chunks[0].Error == nil {
		t.Fatalf("expected error in chunk, got nil")
	}
	if !strings.Contains(chunks[0].Error.Error(), "Resource has been exhausted") {
		t.Errorf("unexpected error: %v", chunks[0].Error)
	}
	if !chunks[0].Done {
		t.Errorf("expected Done chunk on in-stream error")
	}

	closing, ferr := dec.Finish(nil)
	if ferr == nil || !strings.Contains(ferr.Error(), "Resource has been exhausted") {
		t.Errorf("Finish err = %v, want stream error", ferr)
	}
	_ = closing
}

func TestDecoder_FinishReason(t *testing.T) {
	dec := NewDecoder(nil)
	chunks := dec.Feed(`data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"safe text"}]},"finishReason":"SAFETY"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}},"traceId":"t"}`)
	fin, err := dec.Finish(nil)
	if err != nil {
		t.Fatalf("Finish error: %v", err)
	}
	chunks = append(chunks, fin...)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}
	last := chunks[len(chunks)-1]
	if last.Meta == nil || last.Meta.FinishReason != "SAFETY" {
		t.Errorf("FinishReason = %v, want SAFETY", last.Meta)
	}
}

func TestHardenContents(t *testing.T) {
	// Leading model turn gets prepended with user context
	leading := []Content{
		{Role: "model", Parts: []Part{{Text: "I thought about it"}}},
		{Role: "user", Parts: []Part{{Text: "next question"}}},
	}
	hardened := HardenContents(leading)
	if len(hardened) != 3 || hardened[0].Role != "user" || hardened[1].Role != "model" {
		t.Errorf("leading model turn hardening failed: %v", hardened)
	}

	// Trailing assistant message (without tool response) gets appended with Continue.
	trailing := []Content{
		{Role: "user", Parts: []Part{{Text: "hi"}}},
		{Role: "model", Parts: []Part{{Text: "hello"}}},
	}
	hardenedTrailing := HardenContents(trailing)
	if len(hardenedTrailing) != 3 || hardenedTrailing[2].Role != "user" || hardenedTrailing[2].Parts[0].Text != "Continue." {
		t.Errorf("trailing model turn hardening failed: %v", hardenedTrailing)
	}

	// Trailing tool response does not get appended
	toolResp := []Content{
		{Role: "user", Parts: []Part{{Text: "hi"}}},
		{Role: "model", Parts: []Part{{FunctionResponse: &FunctionResponse{ID: "1", Name: "t", Response: map[string]any{"output": "ok"}}}}},
	}
	hardenedTool := HardenContents(toolResp)
	if len(hardenedTool) != 2 {
		t.Errorf("tool response should not be appended: len = %d", len(hardenedTool))
	}
}

func TestBuildRequest_ThinkingLevel(t *testing.T) {
	req := BuildRequest(Turn{
		Model:         "gemini-3.8-flash-high",
		ThinkingLevel: "high",
		Messages:      []TurnMessage{{Role: "user", Content: "think"}},
	})
	gc := req.Request.GenerationConfig
	if gc == nil || gc.ThinkingConfig == nil {
		t.Fatalf("expected ThinkingConfig")
	}
	if gc.ThinkingConfig.ThinkingLevel != "high" {
		t.Errorf("ThinkingLevel = %q, want high", gc.ThinkingConfig.ThinkingLevel)
	}
	if gc.ThinkingConfig.ThinkingBudget != 0 {
		t.Errorf("ThinkingBudget = %d, want 0 (omitted) when ThinkingLevel is active", gc.ThinkingConfig.ThinkingBudget)
	}

	raw, err := json.Marshal(gc.ThinkingConfig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "thinkingBudget") {
		t.Errorf("thinkingBudget must be omitted in json: %s", raw)
	}
}

func TestNormalizeToolName(t *testing.T) {
	cases := map[string]string{
		"default_api:run_command": "run_command",
		"default_api.view_file":   "view_file",
		"run_command":             "run_command",
		"custom_tool":             "custom_tool",
		"default_api:":            "default_api:",
	}
	for in, want := range cases {
		if got := NormalizeToolName(in); got != want {
			t.Errorf("NormalizeToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGeminiGeneration(t *testing.T) {
	cases := map[string]int{
		"gemini-3.8-flash":       3,
		"gemini-3-pro":           3,
		"models/gemini-4-flash":  4,
		"gemini-2.5-pro":         2,
		"claude-3-5-sonnet-2024": 0,
		"gpt-4o":                 0,
	}
	for in, want := range cases {
		if got := GeminiGeneration(in); got != want {
			t.Errorf("GeminiGeneration(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestHardenContents_LeadingToolResponse(t *testing.T) {
	// A history opening with a tool response (model role) must be prepended with a user turn
	leadingTool := []Content{
		{Role: "model", Parts: []Part{{FunctionResponse: &FunctionResponse{ID: "1", Name: "t", Response: map[string]any{"output": "ok"}}}}},
		{Role: "user", Parts: []Part{{Text: "next"}}},
	}
	hardened := HardenContents(leadingTool)
	if len(hardened) != 3 || hardened[0].Role != "user" || hardened[1].Role != "model" {
		t.Fatalf("expected user turn before leading tool response: %+v", hardened)
	}
}

func TestIsSafetyFinishReason(t *testing.T) {
	for _, reason := range []string{"SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII", "IMAGE_SAFETY", "safety"} {
		if !IsSafetyFinishReason(reason) {
			t.Errorf("IsSafetyFinishReason(%q) = false, want true", reason)
		}
	}
	for _, reason := range []string{"STOP", "MAX_TOKENS", "RECITATION", ""} {
		if IsSafetyFinishReason(reason) {
			t.Errorf("IsSafetyFinishReason(%q) = true, want false", reason)
		}
	}
}

func TestDecoder_CachedContentTokens(t *testing.T) {
	dec := NewDecoder(nil)
	chunks := dec.Feed(`data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":5,"totalTokenCount":105,"cachedContentTokenCount":80}}}`)
	if len(chunks) == 0 {
		t.Fatalf("expected chunks")
	}
	last := chunks[len(chunks)-1]
	if last.Meta == nil {
		t.Fatalf("expected meta")
	}
	if last.Meta.CachedContentTokens != 80 {
		t.Errorf("CachedContentTokens = %d, want 80", last.Meta.CachedContentTokens)
	}
}

func TestInjectSkipThoughtSignatures(t *testing.T) {
	contents := []Content{
		{
			Role: "model",
			Parts: []Part{
				{
					FunctionCall: &FunctionCall{ID: "call_1", Name: "run_command"},
				},
				{
					ThoughtSignature: "existing_sig",
					FunctionCall:     &FunctionCall{ID: "call_2", Name: "view_file"},
				},
				{
					Text: "just text",
				},
			},
		},
	}

	modified := InjectSkipThoughtSignatures(contents)
	if !modified {
		t.Errorf("expected modified = true")
	}
	if contents[0].Parts[0].ThoughtSignature != SkipThoughtSignature {
		t.Errorf("call_1 expected signature %q, got %q", SkipThoughtSignature, contents[0].Parts[0].ThoughtSignature)
	}
	if contents[0].Parts[1].ThoughtSignature != "existing_sig" {
		t.Errorf("call_2 signature clobbered: got %q", contents[0].Parts[1].ThoughtSignature)
	}
}

func TestResolveThinkingConfig(t *testing.T) {
	// Gemini 3+ with budget should prune budget and default to "high"
	gc := &GenerationConfig{
		ThinkingConfig: &ThinkingConfig{
			IncludeThoughts: true,
			ThinkingBudget:  2048,
		},
	}
	mod := ResolveThinkingConfig("gemini-3.8-flash", gc)
	if !mod {
		t.Errorf("expected modified = true")
	}
	if gc.ThinkingConfig.ThinkingLevel != "high" {
		t.Errorf("expected thinkingLevel 'high', got %q", gc.ThinkingConfig.ThinkingLevel)
	}
	if gc.ThinkingConfig.ThinkingBudget != 0 {
		t.Errorf("expected thinkingBudget 0, got %d", gc.ThinkingConfig.ThinkingBudget)
	}

	// Gemini 3+ without thinkingConfig should get one with high
	gcNil := &GenerationConfig{}
	mod = ResolveThinkingConfig("gemini-3-pro", gcNil)
	if !mod || gcNil.ThinkingConfig == nil || gcNil.ThinkingConfig.ThinkingLevel != "high" {
		t.Errorf("expected thinkingConfig with level 'high', got %+v", gcNil.ThinkingConfig)
	}

	// Explicit thinkingLevel should prune budget on any model
	gcCustom := &GenerationConfig{
		ThinkingConfig: &ThinkingConfig{
			ThinkingLevel:  "low",
			ThinkingBudget: 512,
		},
	}
	mod = ResolveThinkingConfig("gemini-2.5-flash", gcCustom)
	if !mod || gcCustom.ThinkingConfig.ThinkingBudget != 0 || gcCustom.ThinkingConfig.ThinkingLevel != "low" {
		t.Errorf("expected budget pruned to 0, got %+v", gcCustom.ThinkingConfig)
	}
}



