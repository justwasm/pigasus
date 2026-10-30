package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// geminiServer is a stub Gemini endpoint: it records the request it
// received and replies with the SSE frames it was given.
type geminiServer struct {
	server *httptest.Server
	path   string
	header http.Header
	body   []byte
}

func newGeminiServer(t *testing.T, sse string) *geminiServer {
	t.Helper()
	g := &geminiServer{}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.path = r.URL.RequestURI()
		g.header = r.Header.Clone()
		buf := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(buf)
		}
		g.body = buf
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	t.Cleanup(g.server.Close)
	return g
}

func TestGeminiStreamRequestShape(t *testing.T) {
	sse := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}],\"role\":\"model\"}}]}\n\n" +
		"data: {\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":4,\"totalTokenCount\":7}}\n\n"
	g := newGeminiServer(t, sse)

	client := NewGeminiWithConfig(GeminiConfig{
		BaseURL: g.server.URL,
		APIKey:  "secret-key",
		Model:   "gemini-3-pro-preview",
		Headers: map[string]string{"X-Custom": "yes"},
	})

	chunks, err := client.Stream(context.Background(), StreamRequest{
		Model:        "gemini-3-pro-preview",
		SystemPrompt: "you are antigravity",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		Tools: []ToolDecl{{Name: "read_file", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var text string
	var meta *ChunkMeta
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("chunk error: %v", chunk.Error)
		}
		text += chunk.Text
		if chunk.Done {
			meta = chunk.Meta
		}
	}
	if text != "hi" {
		t.Fatalf("text = %q, want %q", text, "hi")
	}
	if meta == nil || meta.PromptTokens != 3 || meta.TotalTokens != 7 {
		t.Fatalf("meta = %+v, want prompt 3 / total 7", meta)
	}

	if want := "/v1beta/models/gemini-3-pro-preview:streamGenerateContent?alt=sse"; g.path != want {
		t.Fatalf("path = %q, want %q", g.path, want)
	}
	if got := g.header.Get("X-Goog-Api-Key"); got != "secret-key" {
		t.Fatalf("X-Goog-Api-Key = %q", got)
	}
	if got := g.header.Get("X-Custom"); got != "yes" {
		t.Fatalf("X-Custom = %q", got)
	}
	if got := g.header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}

	var body map[string]any
	if err := json.Unmarshal(g.body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	si, ok := body["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatalf("systemInstruction missing: %s", g.body)
	}
	if si["role"] != "user" {
		t.Fatalf("systemInstruction.role = %v, want user", si["role"])
	}
	contents, ok := body["contents"].([]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("contents = %v", body["contents"])
	}
	first := contents[0].(map[string]any)
	if first["role"] != "user" {
		t.Fatalf("contents[0].role = %v", first["role"])
	}
	parts := first["parts"].([]any)
	if len(parts) != 1 || parts[0].(map[string]any)["text"] != "hello" {
		t.Fatalf("contents[0].parts = %v", parts)
	}
	gen, ok := body["generationConfig"].(map[string]any)
	if !ok || gen["maxOutputTokens"] != float64(GeminiDefaultMaxOutputTokens) {
		t.Fatalf("generationConfig = %v", body["generationConfig"])
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v", body["tools"])
	}
	decls := tools[0].(map[string]any)["functionDeclarations"].([]any)
	if len(decls) != 1 || decls[0].(map[string]any)["name"] != "read_file" {
		t.Fatalf("functionDeclarations = %v", decls)
	}
	toolConfig, ok := body["toolConfig"].(map[string]any)
	if !ok {
		t.Fatalf("toolConfig missing: %s", g.body)
	}
	fcc, ok := toolConfig["functionCallingConfig"].(map[string]any)
	if !ok || fcc["mode"] != "AUTO" {
		t.Fatalf("functionCallingConfig = %v", toolConfig["functionCallingConfig"])
	}
	if _, ok := body["sessionId"].(string); !ok {
		t.Fatalf("sessionId missing: %s", g.body)
	}
}

func TestGeminiStreamToolCall(t *testing.T) {
	sse := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"read_file\",\"args\":{\"path\":\"a.txt\"}}}],\"role\":\"model\"}}]}\n\n"
	g := newGeminiServer(t, sse)

	client := NewGeminiWithConfig(GeminiConfig{BaseURL: g.server.URL, Model: "m"})
	chunks, err := client.Stream(context.Background(), StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var calls []ToolCall
	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("chunk error: %v", chunk.Error)
		}
		calls = append(calls, chunk.ToolCalls...)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].Name != "read_file" {
		t.Fatalf("call name = %q", calls[0].Name)
	}
	if calls[0].ID == "" {
		t.Fatalf("call has no id")
	}
	if string(calls[0].Arguments) != `{"path":"a.txt"}` {
		t.Fatalf("call args = %s", calls[0].Arguments)
	}
}

func TestGeminiStreamHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer server.Close()

	client := NewGeminiWithConfig(GeminiConfig{BaseURL: server.URL, Model: "m"})
	chunks, err := client.Stream(context.Background(), StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var got error
	for chunk := range chunks {
		if chunk.Error != nil {
			got = chunk.Error
		}
	}
	if got == nil {
		t.Fatalf("expected an error chunk")
	}
	var httpErr *HTTPError
	if !asHTTPError(got, &httpErr) {
		t.Fatalf("error = %T (%v), want *HTTPError", got, got)
	}
	if httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", httpErr.StatusCode)
	}
	if !strings.Contains(string(httpErr.Body), "bad key") {
		t.Fatalf("body = %s", httpErr.Body)
	}
}

func TestGeminiVertexURL(t *testing.T) {
	g := newGeminiServer(t, "")
	client := NewGeminiWithConfig(GeminiConfig{
		BaseURL:        g.server.URL,
		Model:          "gemini-3-pro-preview",
		VertexProject:  "proj",
		VertexLocation: "us-central1",
	})
	chunks, err := client.Stream(context.Background(), StreamRequest{Model: "gemini-3-pro-preview"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range chunks {
	}
	want := "/v1beta/projects/proj/locations/us-central1/publishers/google/models/gemini-3-pro-preview:streamGenerateContent?alt=sse"
	if g.path != want {
		t.Fatalf("path = %q, want %q", g.path, want)
	}
}

// The reference's body ends with a newline: the genai SDK writes it with
// a `json.Encoder`, which appends one.
func TestGeminiBodyEndsWithANewline(t *testing.T) {
	g := newGeminiServer(t, "")
	client := NewGeminiWithConfig(GeminiConfig{BaseURL: g.server.URL, Model: "gemini-3-pro-preview"})
	chunks, err := client.Stream(context.Background(), StreamRequest{
		Model:    "gemini-3-pro-preview",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range chunks {
	}
	if len(g.body) == 0 {
		t.Fatal("the body is empty")
	}
	if g.body[len(g.body)-1] != '\n' {
		t.Errorf("the body does not end with a newline: %q", g.body[len(g.body)-10:])
	}
}

// The reference's body is keyed alphabetically, top level and inside the
// declarations: the capture shows `contents`, `generationConfig`,
// `sessionId`, `systemInstruction`, `toolConfig`, `tools` in that order,
// and `description`, `name`, `parametersJsonSchema` inside each
// declaration.
func TestGeminiBodyIsKeyedAlphabetically(t *testing.T) {
	g := newGeminiServer(t, "")
	client := NewGeminiWithConfig(GeminiConfig{BaseURL: g.server.URL, Model: "gemini-3-pro-preview"})
	chunks, err := client.Stream(context.Background(), StreamRequest{
		Model:        "gemini-3-pro-preview",
		SystemPrompt: "you are antigravity",
		Messages:     []Message{{Role: "user", Content: "hello"}},
		Tools: []ToolDecl{{
			Name:        "view_file",
			Description: "view a file",
			Parameters:  json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range chunks {
	}
	body := string(g.body)
	top := []string{`"contents"`, `"generationConfig"`, `"sessionId"`, `"systemInstruction"`, `"toolConfig"`, `"tools"`}
	at := -1
	for _, key := range top {
		next := strings.Index(body, key)
		if next < 0 {
			t.Fatalf("the body has no %s: %s", key, body)
		}
		if next <= at {
			t.Errorf("%s is out of order in the body: %s", key, body)
		}
		at = next
	}
	decl := []string{`"description"`, `"name"`, `"parametersJsonSchema"`}
	at = -1
	for _, key := range decl {
		next := strings.Index(body, key)
		if next < 0 {
			t.Fatalf("the declaration has no %s: %s", key, body)
		}
		if next <= at {
			t.Errorf("%s is out of order in the declaration: %s", key, body)
		}
		at = next
	}
}

// The harness advertises plain JSON Schema, and the API carries that in
// its own `parametersJsonSchema` field. The OpenAPI subset stays in
// `parameters`.
func TestGeminiJSONSchemaGoesToParametersJSONSchema(t *testing.T) {
	g := newGeminiServer(t, "")
	client := NewGeminiWithConfig(GeminiConfig{BaseURL: g.server.URL, Model: "gemini-3-pro-preview"})
	chunks, err := client.Stream(context.Background(), StreamRequest{
		Model: "gemini-3-pro-preview",
		Tools: []ToolDecl{
			{
				Name:       "view_file",
				Parameters: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`),
			},
			{
				Name:       "read_file",
				Parameters: json.RawMessage(`{"type":"OBJECT","properties":{"path":{"type":"STRING"}}}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range chunks {
	}
	var body map[string]any
	if err := json.Unmarshal(g.body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	decls := body["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)
	first := decls[0].(map[string]any)
	if _, ok := first["parametersJsonSchema"]; !ok {
		t.Errorf("the JSON-Schema declaration has no parametersJsonSchema: %v", first)
	}
	if _, ok := first["parameters"]; ok {
		t.Errorf("the JSON-Schema declaration also carries parameters: %v", first)
	}
	second := decls[1].(map[string]any)
	if _, ok := second["parameters"]; !ok {
		t.Errorf("the OpenAPI declaration has no parameters: %v", second)
	}
	if _, ok := second["parametersJsonSchema"]; ok {
		t.Errorf("the OpenAPI declaration also carries parametersJsonSchema: %v", second)
	}
}

// The request's toolConfig carries the function calling mode.
//
// The reference turns the function calling off when the model has one
// retry left: the request carries "NONE", and the declarations stay on
// the wire — only the mode changes.
func TestGeminiFunctionCallingMode(t *testing.T) {
	sse := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}],\"role\":\"model\"}}]}\n\n"
	g := newGeminiServer(t, sse)

	client := NewGeminiWithConfig(GeminiConfig{
		BaseURL: g.server.URL,
		APIKey:  "secret-key",
		Model:   "gemini-3-pro-preview",
	})
	gemini, ok := client.(*GeminiClient)
	if !ok {
		t.Fatalf("client = %T, want *GeminiClient", client)
	}

	mode := func() string {
		t.Helper()
		chunks, err := client.Stream(context.Background(), StreamRequest{
			Model:    "gemini-3-pro-preview",
			Messages: []Message{{Role: "user", Content: "hello"}},
			Tools:    []ToolDecl{{Name: "read_file", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`)}},
		})
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		for range chunks {
		}
		var body map[string]any
		if err := json.Unmarshal(g.body, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		toolConfig, ok := body["toolConfig"].(map[string]any)
		if !ok {
			t.Fatalf("the body carries no toolConfig: %s", g.body)
		}
		calling, ok := toolConfig["functionCallingConfig"].(map[string]any)
		if !ok {
			t.Fatalf("the toolConfig carries no functionCallingConfig: %s", g.body)
		}
		got, _ := calling["mode"].(string)
		return got
	}

	if got := mode(); got != "AUTO" {
		t.Errorf("mode = %q, want AUTO", got)
	}

	gemini.SetFunctionCallingMode("NONE")
	if got := mode(); got != "NONE" {
		t.Errorf("mode = %q, want NONE", got)
	}
	if got := gemini.FunctionCallingMode(); got != "NONE" {
		t.Errorf("FunctionCallingMode = %q, want NONE", got)
	}

	gemini.SetFunctionCallingMode("")
	if got := mode(); got != "AUTO" {
		t.Errorf("mode = %q, want AUTO", got)
	}
}

// asHTTPError is errors.As specialised to *HTTPError, kept local so the
// test reads without importing errors.
func asHTTPError(err error, target **HTTPError) bool {
	for err != nil {
		if he, ok := err.(*HTTPError); ok {
			*target = he
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

func TestGeminiStreamInStreamError(t *testing.T) {
	sse := "data: {\"error\":{\"code\":429,\"message\":\"Resource has been exhausted (e.g. check quota)\",\"status\":\"RESOURCE_EXHAUSTED\"}}\n\n"
	g := newGeminiServer(t, sse)

	client := NewGeminiWithConfig(GeminiConfig{BaseURL: g.server.URL, Model: "m"})
	chunks, err := client.Stream(context.Background(), StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotErr error
	var doneCount int
	for chunk := range chunks {
		if chunk.Done {
			doneCount++
		}
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}
	if doneCount != 1 {
		t.Errorf("doneCount = %d, want exactly 1", doneCount)
	}
	if gotErr == nil {
		t.Fatalf("expected error chunk, got nil")
	}
	var he *HTTPError
	if !asHTTPError(gotErr, &he) {
		t.Fatalf("expected *HTTPError, got %T (%v)", gotErr, gotErr)
	}
	if he.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", he.StatusCode)
	}
}

func TestGeminiStreamToolCallNamespaceStripping(t *testing.T) {
	sse := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"default_api:run_command\",\"args\":{\"CommandLine\":\"echo 1\"}}}],\"role\":\"model\"}}]}\n\n"
	g := newGeminiServer(t, sse)

	client := NewGeminiWithConfig(GeminiConfig{BaseURL: g.server.URL, Model: "m"})
	chunks, err := client.Stream(context.Background(), StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var calls []ToolCall
	for chunk := range chunks {
		calls = append(calls, chunk.ToolCalls...)
	}
	if len(calls) != 1 {
		t.Fatalf("calls len = %d, want 1", len(calls))
	}
	if calls[0].Name != "run_command" {
		t.Errorf("call.Name = %q, want run_command (namespace stripped)", calls[0].Name)
	}
}

func TestGeminiStreamCachedTokens(t *testing.T) {
	sse := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"cached\"}],\"role\":\"model\"}}]}\n\n" +
		"data: {\"usageMetadata\":{\"promptTokenCount\":150,\"candidatesTokenCount\":10,\"totalTokenCount\":160,\"cachedContentTokenCount\":120}}\n\n"
	g := newGeminiServer(t, sse)

	client := NewGeminiWithConfig(GeminiConfig{BaseURL: g.server.URL, Model: "m"})
	chunks, err := client.Stream(context.Background(), StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var meta *ChunkMeta
	for chunk := range chunks {
		if chunk.Done && chunk.Meta != nil {
			meta = chunk.Meta
		}
	}
	if meta == nil {
		t.Fatalf("expected meta")
	}
	if meta.CachedTokens != 120 {
		t.Errorf("CachedTokens = %d, want 120", meta.CachedTokens)
	}
}
