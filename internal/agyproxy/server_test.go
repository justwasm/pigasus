package agyproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/models"
)

// openAIStub is a stand-in for the configured gateway: it records the last request it saw
// and answers with a canned body/stream.
type openAIStub struct {
	srv      *httptest.Server
	lastBody []byte
	lastAuth string
}

func newOpenAIStub(t *testing.T, handler http.HandlerFunc) *openAIStub {
	t.Helper()
	s := &openAIStub{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.lastBody = body
		s.lastAuth = r.Header.Get("Authorization")
		handler(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// stubModel points a registry entry at the stub.
func stubModel(url string) models.Model {
	m := miniMax()
	m.Provider = "openai"
	m.APIURL = url
	m.APIKey = "test-key"
	return m
}

func newTestServer(t *testing.T, upstream string, custom ...models.Model) *Server {
	t.Helper()
	s, err := New(Config{Addr: "127.0.0.1:0", Upstream: upstream, Models: custom, Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func post(t *testing.T, s *Server, path string, body string) *http.Response {
	t.Helper()
	resp, err := http.Post("http://"+s.Addr()+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// ---- translation -------------------------------------------------------

func TestToChatRequestFull(t *testing.T) {
	raw := `{
	  "model": "MODEL_PLACEHOLDER_M400",
	  "request": {
	    "systemInstruction": {"parts": [{"text": "you are agy"}, {"text": ""}]},
	    "contents": [
	      {"role": "user", "parts": [{"text": "list files"}]},
	      {"role": "model", "parts": [{"text": "thinking", "thought": true},
	                                  {"functionCall": {"name": "ls", "args": {"dir": "/tmp"}}}]},
	      {"role": "user", "parts": [{"functionResponse": {"name": "ls", "response": {"files": ["a"]}}}]},
	      {"role": "model", "parts": [{"text": "two files"}]}
	    ],
	    "tools": [{"functionDeclarations": [
	      {"name": "ls", "description": "list", "parameters": {"type": "OBJECT",
	        "properties": {"dir": {"type": "STRING"}}}}]}],
	    "generationConfig": {"temperature": 0.25, "maxOutputTokens": 1234}
	  }
	}`
	var env geminicode.GenerateContentRequest
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Wrapped() {
		t.Fatal("envelope not detected as wrapped")
	}
	got := ToChatRequest(miniMax(), env.Inner())

	if got.SystemPrompt != "you are agy" {
		t.Errorf("SystemPrompt = %q", got.SystemPrompt)
	}
	if got.ExternalModel != "MiniMax-M3" {
		t.Errorf("ExternalModel = %q", got.ExternalModel)
	}
	if got.Temperature != 0.25 || got.MaxTokens != 1234 {
		t.Errorf("sampling = %v / %d", got.Temperature, got.MaxTokens)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("messages = %d, want 4: %+v", len(got.Messages), got.Messages)
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != "list files" {
		t.Errorf("msg0 = %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "assistant" || len(got.Messages[1].ToolCalls) != 1 {
		t.Fatalf("msg1 = %+v", got.Messages[1])
	}
	if c := got.Messages[1].ToolCalls[0]; c.Name != "ls" || c.Arguments != `{"dir":"/tmp"}` || c.ID == "" {
		t.Errorf("tool call = %+v", c)
	}
	// The thought part is dropped; only the call survives from that turn.
	if strings.Contains(got.Messages[1].Content, "thinking") {
		t.Errorf("thought text leaked: %q", got.Messages[1].Content)
	}
	if got.Messages[2].Role != "tool" || got.Messages[2].ToolCallID != got.Messages[1].ToolCalls[0].ID {
		t.Errorf("tool result not paired: %+v", got.Messages[2])
	}
	if !strings.Contains(got.Messages[2].Content, `"files"`) {
		t.Errorf("tool result content = %q", got.Messages[2].Content)
	}
	if got.Messages[3].Role != "assistant" || got.Messages[3].Content != "two files" {
		t.Errorf("msg3 = %+v", got.Messages[3])
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "ls" {
		t.Fatalf("tools = %+v", got.Tools)
	}
	// Gemini spells schema types in upper case; OpenAI needs lower case.
	if !strings.Contains(string(got.Tools[0].Parameters), `"object"`) ||
		!strings.Contains(string(got.Tools[0].Parameters), `"string"`) {
		t.Errorf("schema types not lowered: %s", got.Tools[0].Parameters)
	}
}

// ---- stream path -------------------------------------------------------

const sseTextThenTool = `data: {"choices":[{"delta":{"content":"I will run "}}]}

data: {"choices":[{"delta":{"content":"ls."}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"ls","arguments":"{\"dir\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"/tmp\"}"}}]},"finish_reason":"tool_calls"}]}

data: {"choices":[{"delta":{}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}

data: [DONE]

`

func TestCustomModelStreamRoundTrip(t *testing.T) {
	stub := newOpenAIStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseTextThenTool)
	})
	s := newTestServer(t, stub.srv.URL, stubModel(stub.srv.URL))

	req := `{"model":"` + Placeholder(miniMax()) + `","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`
	resp := post(t, s, "/v1internal:streamGenerateContent?alt=sse", req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `data: `) {
		t.Fatalf("not SSE:\n%s", body)
	}

	var texts []string
	var final geminicode.CloudCodeResponse
	frames := 0
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		frames++
		var cc geminicode.CloudCodeResponse
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cc); err != nil {
			t.Fatalf("frame %d not a CloudCode envelope: %v (%s)", frames, err, line)
		}
		final = cc
		for _, c := range cc.Response.Candidates {
			for _, p := range c.Content.Parts {
				if p.Text != "" {
					texts = append(texts, p.Text)
				}
			}
		}
	}
	if len(texts) != 2 || strings.Join(texts, "") != "I will run ls." {
		t.Fatalf("text deltas = %q", texts)
	}
	if frames < 3 {
		t.Fatalf("frames = %d, want >= 3 (two text chunks + final)", frames)
	}

	cand := final.Response.Candidates[0]
	if cand.FinishReason != "STOP" {
		t.Errorf("finishReason = %q, want STOP", cand.FinishReason)
	}
	var call *geminicode.FunctionCall
	for _, p := range cand.Content.Parts {
		if p.FunctionCall != nil {
			call = p.FunctionCall
		}
	}
	if call == nil {
		t.Fatalf("no functionCall in final frame: %+v", cand.Content.Parts)
	}
	if call.Name != "ls" || call.ID == "" {
		t.Errorf("functionCall = %+v", call)
	}
	var args map[string]any
	if err := json.Unmarshal(call.Args, &args); err != nil {
		t.Fatalf("unmarshal args: %v", err)
	}
	if got := args["dir"]; got != "/tmp" {
		t.Errorf("args = %+v, want dir=/tmp", args)
	}
	if final.Response.UsageMetadata == nil || final.Response.UsageMetadata.TotalTokenCount != 18 {
		t.Errorf("usage = %+v", final.Response.UsageMetadata)
	}

	// The provider must have received the OpenAI-shaped tools/messages, and
	// the request must be streaming and carry the external model name.
	if stub.lastAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", stub.lastAuth)
	}
	var sent models.ChatRequest
	var wire struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(stub.lastBody, &wire); err != nil {
		t.Fatal(err)
	}
	_ = sent
	if wire.Model != "MiniMax-M3" {
		t.Errorf("upstream model = %q, want MiniMax-M3", wire.Model)
	}
	if !wire.Stream {
		t.Error("upstream request was not stream:true")
	}
}

func TestCustomModelNonStreaming(t *testing.T) {
	stub := newOpenAIStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"pong",
		  "tool_calls":[{"id":"call_9","type":"function","function":{"name":"ping","arguments":"{}"}}]},
		  "finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	})
	s := newTestServer(t, stub.srv.URL, stubModel(stub.srv.URL))

	resp := post(t, s, "/v1internal:generateContent",
		`{"model":"custom-minimax-m3","request":{"contents":[{"role":"user","parts":[{"text":"ping"}]}]}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var cc geminicode.CloudCodeResponse
	if err := json.Unmarshal(raw, &cc); err != nil {
		t.Fatalf("body is not a CloudCode envelope: %v\n%s", err, raw)
	}
	if len(cc.Response.Candidates) != 1 {
		t.Fatalf("candidates = %+v", cc.Response.Candidates)
	}
	cand := cc.Response.Candidates[0]
	if cand.FinishReason != "STOP" {
		t.Errorf("finishReason = %q", cand.FinishReason)
	}
	var text string
	var call *geminicode.FunctionCall
	for _, p := range cand.Content.Parts {
		if p.Text != "" {
			text += p.Text
		}
		if p.FunctionCall != nil {
			call = p.FunctionCall
		}
	}
	if text != "pong" {
		t.Errorf("text = %q", text)
	}
	if call == nil || call.Name != "ping" || call.ID != "call_9" {
		t.Errorf("functionCall = %+v", call)
	}

	// A non-streaming upstream request must say stream:false.
	var wire struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(stub.lastBody, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Stream {
		t.Error("non-streaming call set stream:true upstream")
	}
}

func TestCustomModelBareGeminiEnvelope(t *testing.T) {
	// No `request` wrapper: the reply must stay bare for a plain Gemini call.
	stub := newOpenAIStub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"bare"},"finish_reason":"stop"}]}`)
	})
	s := newTestServer(t, stub.srv.URL, stubModel(stub.srv.URL))

	resp := post(t, s, "/v1internal:generateContent",
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"model":"MiniMax-M3"}`)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var bare geminicode.GenerateContentResponse
	if err := json.Unmarshal(raw, &bare); err != nil {
		t.Fatal(err)
	}
	if len(bare.Candidates) != 1 || bare.Candidates[0].Content.Parts[0].Text != "bare" {
		t.Fatalf("bare response = %s", raw)
	}
	if strings.Contains(string(raw), `"response"`) {
		t.Errorf("bare call got a CloudCode wrapper: %s", raw)
	}
}

func TestCustomModelUpstreamFailureIs502(t *testing.T) {
	stub := newOpenAIStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"nope"}`)
	})
	s := newTestServer(t, stub.srv.URL, stubModel(stub.srv.URL))
	resp := post(t, s, "/v1internal:generateContent", `{"model":"MiniMax-M3","contents":[]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != http.StatusBadGateway || env.Error.Message == "" {
		t.Fatalf("error envelope = %s", raw)
	}
}

func TestCountTokensAnsweredLocally(t *testing.T) {
	stub := newOpenAIStub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("countTokens must not reach the upstream")
	})
	s := newTestServer(t, stub.srv.URL, stubModel(stub.srv.URL))
	resp := post(t, s, "/v1internal:countTokens",
		`{"model":"MiniMax-M3","request":{"contents":[{"role":"user","parts":[{"text":"12345678"}]}]}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		TotalTokens int `json:"totalTokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.TotalTokens != 2 {
		t.Fatalf("totalTokens = %d, want 2 (8 chars / 4)", out.TotalTokens)
	}
}

// ---- passthrough -------------------------------------------------------

func TestPassthroughPreservesHeadersStatusAndBody(t *testing.T) {
	var gotAuth, gotGoog, gotUA string
	var gotPath, gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotGoog = r.Header.Get("x-goog-api-client")
		gotUA = r.Header.Get("User-Agent")
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, `{"weird":true}`)
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL, miniMax())
	req, err := http.NewRequest(http.MethodPost, "http://"+s.Addr()+"/v1internal:listExperiments?alt=json", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("x-goog-api-client", "gl-go/1.28")
	req.Header.Set("User-Agent", "agy/1.4.2")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", resp.StatusCode)
	}
	if resp.Header.Get("X-Upstream") != "yes" {
		t.Fatalf("upstream header not copied: %v", resp.Header)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"weird":true}` {
		t.Fatalf("body = %s", body)
	}
	if gotAuth != "Bearer tok" || gotGoog != "gl-go/1.28" || gotUA != "agy/1.4.2" {
		t.Fatalf("headers: auth=%q goog=%q ua=%q", gotAuth, gotGoog, gotUA)
	}
	if gotPath != "/v1internal:listExperiments" || gotQuery != "alt=json" {
		t.Fatalf("upstream path = %q?%q", gotPath, gotQuery)
	}
}

func TestPassthroughStreamsWithoutBuffering(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(w, "data: {\"n\":"+string(rune('0'+i))+"}\n\n")
			fl.Flush()
		}
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL, miniMax())
	// A non-custom model on the streaming path must go straight upstream.
	resp := post(t, s, "/v1internal:streamGenerateContent?alt=sse", `{"model":"gemini-2.5-pro","request":{}}`)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if got := strings.Count(string(raw), "data: "); got != 3 {
		t.Fatalf("frames = %d, want 3 (%q)", got, raw)
	}
}

// TestPassthroughDeliversFramesIncrementally pins the streaming
// contract the pass-through owes agy: a frame must reach the client
// when the upstream writes it, not when the upstream closes the
// response. The upstream holds the second frame back until the client
// has seen the first one, so a buffered proxy deadlocks here instead
// of quietly passing the byte-count assertion.
func TestPassthroughDeliversFramesIncrementally(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: 1\n\n")
		fl.Flush()
		select {
		case <-release:
		case <-time.After(10 * time.Second):
			t.Error("the first frame never reached the client")
		}
		_, _ = io.WriteString(w, "data: 2\n\n")
		fl.Flush()
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL, miniMax())
	resp := post(t, s, "/v1internal:streamGenerateContent?alt=sse", `{"model":"gemini-2.5-pro","request":{}}`)
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read the first frame: %v", err)
	}
	if line != "data: 1\n" {
		t.Fatalf("first line = %q, want %q", line, "data: 1\n")
	}
	close(release)
	rest, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("read the rest: %v", err)
	}
	if !strings.Contains(string(rest), "data: 2") {
		t.Fatalf("rest = %q, want it to hold the second frame", rest)
	}
}

func TestUpstreamUnreachable(t *testing.T) {
	// A closed port: 502 with a JSON error, never a hang.
	s := newTestServer(t, "http://127.0.0.1:1", miniMax())
	resp := post(t, s, "/v1internal:listExperiments", `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}

// ---- fetchAvailableModels ---------------------------------------------

func TestFetchAvailableModelsMergesAndSurvivesUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1internal:fetchAvailableModels" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, upstreamModels)
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL, miniMax())
	resp := post(t, s, "/v1internal:fetchAvailableModels", `{"project":"p"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Openagy-Upstream-Status") != "200" {
		t.Errorf("upstream status header = %q", resp.Header.Get("X-Openagy-Upstream-Status"))
	}
	raw, _ := io.ReadAll(resp.Body)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc["models"].([]any)) != 3 {
		t.Fatalf("models = %v", doc["models"])
	}

	// Now with upstream down: still 200, custom models only.
	down := newTestServer(t, "http://127.0.0.1:1", miniMax())
	resp2 := post(t, down, "/v1internal:fetchAvailableModels", `{}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status with upstream down = %d", resp2.StatusCode)
	}
	if resp2.Header.Get("X-Openagy-Upstream-Status") != "error" {
		t.Errorf("expected an error marker, got %q", resp2.Header.Get("X-Openagy-Upstream-Status"))
	}
	raw2, _ := io.ReadAll(resp2.Body)
	var doc2 map[string]any
	if err := json.Unmarshal(raw2, &doc2); err != nil {
		t.Fatal(err)
	}
	fallback, ok := doc2["models"].(map[string]any)
	if !ok || len(fallback) != 1 {
		t.Fatalf("fallback models = %#v", doc2["models"])
	}
}

// ---- capture -----------------------------------------------------------

func TestCaptureWritesRequestAndResponse(t *testing.T) {
	dir := t.TempDir()
	stub := newOpenAIStub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"pong"},"finish_reason":"stop"}]}`)
	})
	s, err := New(Config{
		Addr:       "127.0.0.1:0",
		Upstream:   stub.srv.URL,
		Models:     []models.Model{stubModel(stub.srv.URL)},
		CaptureDir: dir,
		Logf:       func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(); err != nil {
		t.Fatal(err)
	}
	resp := post(t, s, "/v1internal:generateContent",
		`{"model":"MiniMax-M3","request":{"systemInstruction":{"parts":[{"text":"SYS"}]},"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`)
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// One file per record, named <ms>-<seq>-<method>.json; the kind lives in
	// the body, so classify by reading each file.
	reqFile, respFile := "", ""
	for _, e := range readCaptures(t, dir) {
		switch e["kind"] {
		case "request":
			if reqFile == "" {
				reqFile = e["path"].(string)
			}
		case "response":
			if respFile == "" {
				respFile = e["path"].(string)
			}
		}
	}
	if reqFile == "" || respFile == "" {
		t.Fatalf("capture files missing: %v", names(entriesOf(t, dir)))
	}

	raw, err := os.ReadFile(reqFile)
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ts", "kind", "method", "httpMethod", "path", "model", "custom", "requestHeaders", "requestBody"} {
		if _, ok := rec[k]; !ok {
			t.Errorf("request capture missing key %q: %s", k, raw)
		}
	}
	if rec["kind"] != "request" || rec["custom"] != true {
		t.Errorf("kind/custom = %v/%v", rec["kind"], rec["custom"])
	}
	if rec["method"] != "generateContent" {
		t.Errorf("method = %v", rec["method"])
	}
	body := rec["requestBody"].(map[string]any)
	if _, ok := body["request"]; !ok {
		t.Errorf("raw body not preserved: %v", body)
	}

	raw, err = os.ReadFile(respFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec["kind"] != "response" || rec["status"].(float64) != 200 {
		t.Errorf("response capture = %v / %v", rec["kind"], rec["status"])
	}
	if _, ok := rec["responseBody"]; !ok {
		t.Error("response capture has no body")
	}
}

func TestCaptureStreamStoresConcatenatedSSE(t *testing.T) {
	dir := t.TempDir()
	stub := newOpenAIStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseTextThenTool)
	})
	s, err := New(Config{
		Addr:       "127.0.0.1:0",
		Upstream:   stub.srv.URL,
		Models:     []models.Model{stubModel(stub.srv.URL)},
		CaptureDir: dir,
		Logf:       func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(); err != nil {
		t.Fatal(err)
	}
	resp := post(t, s, "/v1internal:streamGenerateContent?alt=sse",
		`{"model":"MiniMax-M3","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}`)
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, e := range readCaptures(t, dir) {
		if e["kind"] != "response" {
			continue
		}
		body, _ := e["responseBody"].(string)
		if strings.Contains(body, "data: ") && strings.Contains(body, "functionCall") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no capture file held the raw SSE text (files: %v)", names(entriesOf(t, dir)))
	}
}

// readCaptures parses every capture file in dir, returning the raw JSON bodies
// plus the file path (so a failure can name the file).
func readCaptures(t *testing.T, dir string) []map[string]any {
	t.Helper()
	entries := entriesOf(t, dir)
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var rec map[string]any
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		rec["path"] = p
		out = append(out, rec)
	}
	return out
}

func entriesOf(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// ---- config plumbing ---------------------------------------------------

func TestNewValidatesUpstream(t *testing.T) {
	if _, err := New(Config{Upstream: "not a url"}); err == nil {
		t.Fatal("want an error for an upstream without a scheme/host")
	}
}

func TestStartIsIdempotentAndAddrReported(t *testing.T) {
	s, err := New(Config{Upstream: "http://127.0.0.1:1", Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	if first != second || s.Addr() != first {
		t.Fatalf("addr drift: %q %q %q", first, second, s.Addr())
	}
	if !strings.HasPrefix(first, "127.0.0.1:") {
		t.Fatalf("addr = %q", first)
	}
}

func TestContextCancellationStopsStream(t *testing.T) {
	stub := newOpenAIStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseTextThenTool)
	})
	prov, err := models.NewProvider(stubModel(stub.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch, err := prov.Stream(ctx, models.ChatRequest{Stream: true})
	if err != nil {
		return // pre-stream failure is an acceptable shape
	}
	for range ch {
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestTranslateTools_MagpieNormalization(t *testing.T) {
	tools := []geminicode.Tool{
		{
			FunctionDeclarations: []geminicode.FunctionDeclaration{
				{
					Name:        "default_api:custom_tool",
					Description: "a tool with non-standard schema",
					Parameters: map[string]any{
						"type": "OBJECT",
						"properties": map[string]any{
							"flag": true,
							"tags": map[string]any{
								"type": "ARRAY",
							},
						},
					},
				},
			},
		},
	}

	specs := translateTools(tools)
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Name != "custom_tool" {
		t.Errorf("tool name = %q, want custom_tool", specs[0].Name)
	}

	var parsed map[string]any
	if err := json.Unmarshal(specs[0].Parameters, &parsed); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if parsed["type"] != "object" {
		t.Errorf("type = %v, want object", parsed["type"])
	}

	props, ok := parsed["properties"].(map[string]any)
	if !ok {
		t.Fatalf("missing properties map")
	}

	// flag: true should have been converted to type: string
	flagProp, ok := props["flag"].(map[string]any)
	if !ok || flagProp["type"] != "string" {
		t.Errorf("flag prop = %+v, want type: string", props["flag"])
	}

	// tags should have items: {type: string}
	tagsProp, ok := props["tags"].(map[string]any)
	if !ok || tagsProp["type"] != "array" {
		t.Errorf("tags prop = %+v, want type: array", props["tags"])
	}
	if items, ok := tagsProp["items"].(map[string]any); !ok || items["type"] != "string" {
		t.Errorf("tags.items = %+v, want type: string", tagsProp["items"])
	}

	parts := toolCallParts([]models.ToolCall{{Name: "custom_tool", Arguments: "{}"}})
	if len(parts) != 1 || parts[0].ThoughtSignature != geminicode.SkipThoughtSignature {
		t.Errorf("expected ThoughtSignature %q on toolCallParts", geminicode.SkipThoughtSignature)
	}
}

func TestSanitizeUpstreamBody_ThoughtSignatureAndThinkingConfig(t *testing.T) {
	raw := `{
		"model": "gemini-3.8-flash",
		"request": {
			"contents": [
				{
					"role": "model",
					"parts": [
						{"functionCall": {"name": "run_cmd", "args": {}}}
					]
				}
			],
			"generationConfig": {
				"thinkingConfig": {
					"includeThoughts": true,
					"thinkingBudget": 2048
				}
			}
		}
	}`

	var env geminicode.GenerateContentRequest
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}

	info := reqInfo{
		model:   env.ModelName(),
		wrapped: env.Wrapped(),
		inner:   env.Inner(),
	}

	sanitized := sanitizeUpstreamBody([]byte(raw), info)

	var res map[string]any
	if err := json.Unmarshal(sanitized, &res); err != nil {
		t.Fatal(err)
	}

	reqMap, ok := res["request"].(map[string]any)
	if !ok {
		t.Fatalf("missing request map in %s", string(sanitized))
	}

	// Check thoughtSignature was injected into functionCall part
	contents := reqMap["contents"].([]any)
	parts := contents[0].(map[string]any)["parts"].([]any)
	part0 := parts[0].(map[string]any)
	if part0["thoughtSignature"] != geminicode.SkipThoughtSignature {
		t.Errorf("expected thoughtSignature %q, got %v", geminicode.SkipThoughtSignature, part0["thoughtSignature"])
	}

	// Check thinkingBudget was pruned and thinkingLevel set to high
	genCfg := reqMap["generationConfig"].(map[string]any)
	tc := genCfg["thinkingConfig"].(map[string]any)
	if tc["thinkingLevel"] != "high" {
		t.Errorf("expected thinkingLevel 'high', got %v", tc["thinkingLevel"])
	}
	if b, ok := tc["thinkingBudget"].(float64); ok && b != 0 {
		t.Errorf("expected thinkingBudget 0 or pruned, got %v", b)
	}
}

func TestToChatRequest_HardenAndNormalizeToolNames(t *testing.T) {
	req := &geminicode.GenerateContentRequest{
		Contents: []geminicode.Content{
			{
				Role: "model",
				Parts: []geminicode.Part{
					{
						FunctionCall: &geminicode.FunctionCall{
							Name: "default_api:run_command",
							Args: json.RawMessage(`{"CommandLine":"ls -la"}`),
						},
					},
				},
			},
			{
				Role: "user",
				Parts: []geminicode.Part{
					{
						FunctionResponse: &geminicode.FunctionResponse{
							Name:     "default_api:run_command",
							Response: "file.txt",
						},
					},
				},
			},
		},
		Tools: []geminicode.Tool{
			{
				FunctionDeclarations: []geminicode.FunctionDeclaration{
					{
						Name:        "default_api:run_command",
						Description: "Execute a command",
					},
				},
			},
		},
	}

	m := models.Model{Name: "claude-3-5-sonnet", ExternalModelName: "claude-3-5-sonnet-20241022"}
	chatReq := ToChatRequest(m, req)

	// HardenContents must have prepended a user message because history started with a model turn
	if len(chatReq.Messages) < 3 {
		t.Fatalf("expected at least 3 messages after hardening, got %d", len(chatReq.Messages))
	}
	if chatReq.Messages[0].Role != "user" {
		t.Errorf("expected first message to be user role, got %s", chatReq.Messages[0].Role)
	}

	// The assistant turn tool call name must be normalized
	modelMsg := chatReq.Messages[1]
	if modelMsg.Role != "assistant" {
		t.Errorf("expected second message to be assistant role, got %s", modelMsg.Role)
	}
	if len(modelMsg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(modelMsg.ToolCalls))
	}
	if modelMsg.ToolCalls[0].Name != "run_command" {
		t.Errorf("expected tool call name 'run_command', got %q", modelMsg.ToolCalls[0].Name)
	}

	// The tool response message tool call ID must match
	toolMsg := chatReq.Messages[2]
	if toolMsg.Role != "tool" {
		t.Errorf("expected third message to be tool role, got %s", toolMsg.Role)
	}
	if toolMsg.ToolCallID != modelMsg.ToolCalls[0].ID {
		t.Errorf("tool response ToolCallID %q != model ToolCall ID %q", toolMsg.ToolCallID, modelMsg.ToolCalls[0].ID)
	}

	// Tools list must also be normalized
	if len(chatReq.Tools) != 1 || chatReq.Tools[0].Name != "run_command" {
		t.Errorf("expected declared tool name 'run_command', got %+v", chatReq.Tools)
	}
}


