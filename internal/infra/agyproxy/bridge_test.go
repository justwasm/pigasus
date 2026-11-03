package agyproxy

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/geminicode"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
)

func TestAntigravityCloak(t *testing.T) {
	// Normal prompt untouched
	normal := "You are a helpful coding assistant."
	if got := antigravityCloak(normal); got != normal {
		t.Errorf("got %q, want %q", got, normal)
	}

	// Claude Code billing & identity triggers
	claudePrompt := "You are running with X-Anthropic-Billing-Header: test and Built on Anthropic's Claude Agent SDK."
	cloaked := antigravityCloak(claudePrompt)
	if strings.Contains(strings.ToLower(cloaked), "built on anthropic's claude agent sdk") {
		t.Errorf("cloaked prompt still contains exact blacklisted phrase: %q", cloaked)
	}
	if !strings.Contains(cloaked, "\u200b") {
		t.Errorf("cloaked prompt should contain zero-width space: %q", cloaked)
	}

	// Codex trigger
	codexPrompt := "You are Codex, an agent based on GPT-5."
	cloakedCodex := antigravityCloak(codexPrompt)
	if strings.Contains(strings.ToLower(cloakedCodex), "you are codex, an agent based on gpt-5") {
		t.Errorf("cloaked codex prompt still contains exact blacklisted phrase: %q", cloakedCodex)
	}
}

func TestModelsEndpoint(t *testing.T) {
	srv, err := New(Config{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	// 1. OPTIONS CORS preflight
	optReq, _ := http.NewRequest(http.MethodOptions, "http://"+addr+"/v1/models", nil)
	optResp, err := http.DefaultClient.Do(optReq)
	if err != nil {
		t.Fatalf("OPTIONS /v1/models: %v", err)
	}
	optResp.Body.Close()
	if optResp.StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", optResp.StatusCode)
	}
	if optResp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("CORS origin = %q, want *", optResp.Header.Get("Access-Control-Allow-Origin"))
	}

	// 2. GET /v1/models
	getReq, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/v1/models", nil)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
	var res struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&res); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if res.Object != "list" || len(res.Data) == 0 {
		t.Errorf("invalid models response: %+v", res)
	}
	foundSonnet := false
	for _, m := range res.Data {
		if m.ID == "claude-sonnet-4-6" {
			foundSonnet = true
		}
	}
	if !foundSonnet {
		t.Errorf("claude-sonnet-4-6 not found in models list: %+v", res.Data)
	}
}

func fakeCloudCodeSSE(w http.ResponseWriter, candidateParts []geminicode.Part, meta *geminicode.UsageMetadata) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	respObj := geminicode.GenerateContentResponse{
		Candidates: []geminicode.Candidate{
			{
				Content: geminicode.Content{
					Role:  "model",
					Parts: candidateParts,
				},
				FinishReason: "STOP",
			},
		},
		UsageMetadata: meta,
	}
	wrapped := map[string]any{
		"response": respObj,
	}
	data, _ := json.Marshal(wrapped)
	fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher != nil {
		flusher.Flush()
	}
}

func TestOpenAIChat_StreamingAndUnary(t *testing.T) {
	// Mock CloudCode upstream server
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "streamGenerateContent") {
			http.NotFound(w, r)
			return
		}
		parts := []geminicode.Part{
			{Text: "I am thinking about the problem...", Thought: true},
			{Text: "Here is the solution to your request."},
			{
				FunctionCall: &geminicode.FunctionCall{
					ID:   "call_abc123",
					Name: "run_command",
					Args: json.RawMessage(`{"CommandLine":"ls -l"}`),
				},
			},
		}
		fakeCloudCodeSSE(w, parts, &geminicode.UsageMetadata{
			PromptTokenCount:     50,
			CandidatesTokenCount: 20,
			TotalTokenCount:      70,
		})
	}))
	defer up.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: up.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	// 1. Streaming test
	streamReqBody := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []map[string]any{
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Run ls"},
		},
		"stream": true,
	}
	b, _ := json.Marshal(streamReqBody)
	resp, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /v1/chat/completions (stream): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("stream status = %d, body = %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	var streamOutput strings.Builder
	var sawThinking bool
	var sawContent bool
	var sawToolCall bool
	var sawDone bool

	for scanner.Scan() {
		line := scanner.Text()
		streamOutput.WriteString(line + "\n")
		if line == "data: [DONE]" {
			sawDone = true
			continue
		}
		if strings.HasPrefix(line, "data: {") {
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content"`
						ToolCalls        []struct {
							Function struct {
								Name string `json:"name"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err == nil {
				if len(chunk.Choices) > 0 {
					delta := chunk.Choices[0].Delta
					if delta.ReasoningContent != "" {
						sawThinking = true
					}
					if delta.Content != "" {
						sawContent = true
					}
					if len(delta.ToolCalls) > 0 && delta.ToolCalls[0].Function.Name == "run_command" {
						sawToolCall = true
					}
				}
			}
		}
	}

	if !sawThinking {
		t.Errorf("stream did not emit reasoning_content. Output:\n%s", streamOutput.String())
	}
	if !sawContent {
		t.Errorf("stream did not emit content. Output:\n%s", streamOutput.String())
	}
	if !sawToolCall {
		t.Errorf("stream did not emit tool call. Output:\n%s", streamOutput.String())
	}
	if !sawDone {
		t.Errorf("stream did not emit [DONE]. Output:\n%s", streamOutput.String())
	}

	// 2. Unary (non-streaming) test
	unaryReqBody := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
		"stream": false,
	}
	ub, _ := json.Marshal(unaryReqBody)
	uResp, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", bytes.NewReader(ub))
	if err != nil {
		t.Fatalf("POST /v1/chat/completions (unary): %v", err)
	}
	defer uResp.Body.Close()

	if uResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(uResp.Body)
		t.Fatalf("unary status = %d, body = %s", uResp.StatusCode, string(body))
	}

	var uRes struct {
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Role             string `json:"role"`
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(uResp.Body).Decode(&uRes); err != nil {
		t.Fatalf("decode unary: %v", err)
	}
	if uRes.Object != "chat.completion" || len(uRes.Choices) == 0 {
		t.Fatalf("unexpected unary payload: %+v", uRes)
	}
	msg := uRes.Choices[0].Message
	if msg.Content == "" {
		t.Errorf("expected content in unary message, got empty")
	}
	if msg.ReasoningContent == "" {
		t.Errorf("expected reasoning_content in unary message, got empty")
	}
	if len(msg.ToolCalls) == 0 || msg.ToolCalls[0].Function.Name != "run_command" {
		t.Errorf("expected tool call run_command in unary message, got %+v", msg.ToolCalls)
	}
}

func TestAnthropicMessages_StreamingAndUnary(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "streamGenerateContent") {
			http.NotFound(w, r)
			return
		}
		parts := []geminicode.Part{
			{Text: "Thinking deeply...", Thought: true},
			{Text: "Claude answer here."},
			{
				FunctionCall: &geminicode.FunctionCall{
					ID:   "tool_123",
					Name: "read_file",
					Args: json.RawMessage(`{"Path":"/etc/hosts"}`),
				},
			},
		}
		fakeCloudCodeSSE(w, parts, &geminicode.UsageMetadata{
			PromptTokenCount:     100,
			CandidatesTokenCount: 40,
			TotalTokenCount:      140,
		})
	}))
	defer up.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: up.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	// 1. Streaming Anthropic test
	streamReqBody := map[string]any{
		"model":  "claude-sonnet-4-6",
		"system": "Built on Anthropic's Claude Agent SDK", // Should be automatically cloaked
		"messages": []map[string]any{
			{"role": "user", "content": "Read hosts"},
		},
		"stream": true,
	}
	b, _ := json.Marshal(streamReqBody)
	resp, err := http.Post("http://"+addr+"/v1/messages", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /v1/messages (stream): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("stream status = %d, body = %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	var sawStart, sawThinking, sawText, sawToolUse, sawDelta, sawStop bool

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			evt := strings.TrimPrefix(line, "event: ")
			switch evt {
			case "message_start":
				sawStart = true
			case "message_delta":
				sawDelta = true
			case "message_stop":
				sawStop = true
			}
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if strings.Contains(data, `"thinking"`) || strings.Contains(data, `"thinking_delta"`) {
				sawThinking = true
			}
			if strings.Contains(data, `"text_delta"`) {
				sawText = true
			}
			if strings.Contains(data, `"tool_use"`) {
				sawToolUse = true
			}
		}
	}

	if !sawStart {
		t.Errorf("missing message_start event")
	}
	if !sawThinking {
		t.Errorf("missing thinking block / delta")
	}
	if !sawText {
		t.Errorf("missing text delta")
	}
	if !sawToolUse {
		t.Errorf("missing tool_use block")
	}
	if !sawDelta {
		t.Errorf("missing message_delta event")
	}
	if !sawStop {
		t.Errorf("missing message_stop event")
	}

	// 2. Unary Anthropic test
	unaryReqBody := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []map[string]any{
			{"role": "user", "content": "Hello"},
		},
		"stream": false,
	}
	ub, _ := json.Marshal(unaryReqBody)
	uResp, err := http.Post("http://"+addr+"/v1/messages", "application/json", bytes.NewReader(ub))
	if err != nil {
		t.Fatalf("POST /v1/messages (unary): %v", err)
	}
	defer uResp.Body.Close()

	if uResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(uResp.Body)
		t.Fatalf("unary status = %d, body = %s", uResp.StatusCode, string(body))
	}

	var uRes struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text,omitempty"`
			Thinking string `json:"thinking,omitempty"`
			Name     string `json:"name,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(uResp.Body).Decode(&uRes); err != nil {
		t.Fatalf("decode anthropic unary: %v", err)
	}
	if uRes.Type != "message" || len(uRes.Content) == 0 {
		t.Fatalf("unexpected anthropic unary payload: %+v", uRes)
	}
	if uRes.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", uRes.StopReason)
	}
}

func TestBridge_AggregateAccountFailover(t *testing.T) {
	// Account 1 hits 429 quota exhausted.
	// Account 2 succeeds with 200 OK.
	var callsTokenA, callsTokenB atomic.Int64

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authH := r.Header.Get("Authorization")
		if strings.Contains(authH, "token-a") {
			callsTokenA.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota)."}}`))
			return
		}
		if strings.Contains(authH, "token-b") {
			callsTokenB.Add(1)
			parts := []geminicode.Part{
				{Text: "Answer from account B"},
			}
			fakeCloudCodeSSE(w, parts, &geminicode.UsageMetadata{
				PromptTokenCount:     10,
				CandidatesTokenCount: 10,
				TotalTokenCount:      20,
			})
			return
		}
		http.Error(w, "unexpected token", http.StatusUnauthorized)
	}))
	defer up.Close()

	accA := accounts.Account{
		Email: "a@example.com",
		Token: testToken("token-a"),
		Quota: testQuotaSnapshot(0.0), // exhausted
	}
	accB := accounts.Account{
		Email: "b@example.com",
		Token: testToken("token-b"),
		Quota: testQuotaSnapshot(0.8), // available
	}
	setupTestEnvironment(t, map[string]any{
		"enabled":   true,
		"aggregate": true,
		"strategy":  "aggregate",
	}, accA, accB)

	router := newTestRouter(accA, accB)
	srv, err := New(Config{
		Addr:      "127.0.0.1:0",
		Upstream:  up.URL,
		Balance:   true,
		Aggregate: true,
		Router:    router,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	// Call OpenAI endpoint
	reqBody := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []map[string]any{
			{"role": "user", "content": "hi"},
		},
		"stream": false,
	}
	b, _ := json.Marshal(reqBody)
	resp, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 OK after failover, got %d: %s", resp.StatusCode, string(out))
	}

	var res struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Choices) == 0 || !strings.Contains(res.Choices[0].Message.Content, "Answer from account B") {
		t.Fatalf("expected answer from account B, got: %+v", res)
	}

	if callsTokenB.Load() == 0 {
		t.Errorf("token B was never called! callsA=%d, callsB=%d", callsTokenA.Load(), callsTokenB.Load())
	}
}

func TestOpenAIChat_ToolResponseInfersName(t *testing.T) {
	var capturedEnvelope geminicode.GenerateContentRequest
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedEnvelope)
		fakeCloudCodeSSE(w, []geminicode.Part{{Text: "All done."}}, nil)
	}))
	defer up.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: up.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	// OpenAI request with tool message that has NO "name" field (like @ai-sdk/openai-compatible)
	reqBody := map[string]any{
		"model": "gemini-3.8-flash-high",
		"messages": []map[string]any{
			{"role": "user", "content": "read the file"},
			{
				"role": "assistant",
				"tool_calls": []map[string]any{
					{
						"id":   "call_read_1",
						"type": "function",
						"function": map[string]any{
							"name":      "read_file",
							"arguments": `{"path":"main.go"}`,
						},
					},
				},
			},
			{
				"role":         "tool",
				"tool_call_id": "call_read_1",
				// Notice: name is omitted intentionally!
				"content": "package main",
			},
		},
	}
	b, _ := json.Marshal(reqBody)
	resp, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, string(out))
	}

	var foundRespName string
	if capturedEnvelope.Request != nil {
		for _, c := range capturedEnvelope.Request.Contents {
			for _, p := range c.Parts {
				if p.FunctionResponse != nil {
					foundRespName = p.FunctionResponse.Name
				}
			}
		}
	}
	if foundRespName != "read_file" {
		t.Errorf("captured FunctionResponse.Name = %q, want %q", foundRespName, "read_file")
	}
}

func TestOpenAIChat_MultimodalAttachments(t *testing.T) {
	var capturedEnvelope geminicode.GenerateContentRequest
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedEnvelope)
		fakeCloudCodeSSE(w, []geminicode.Part{{Text: "I see the image and PDF."}}, nil)
	}))
	defer up.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: up.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	reqBody := map[string]any{
		"model": "gemini-3.8-flash-high",
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "Analyze this:"},
					map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAACklEQVR4nGNiAAAABgADNjd8qAAAAABJRU5ErkJggg==",
						},
					},
					map[string]any{
						"type":     "file",
						"mimeType": "application/pdf",
						"data":     "data:application/pdf;base64,JVBERi0xLjQKJcOkw7zDtsOfCjEgMCBvYmoKPDwKL1R5cGUgL0NhdGFsb2cKPj4KZW5kb2Jq",
					},
				},
			},
		},
	}
	b, _ := json.Marshal(reqBody)
	resp, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, string(out))
	}

	var inlineMimes []string
	if capturedEnvelope.Request != nil && len(capturedEnvelope.Request.Contents) > 0 {
		for _, p := range capturedEnvelope.Request.Contents[0].Parts {
			if p.InlineData != nil {
				inlineMimes = append(inlineMimes, p.InlineData.MIMEType)
			}
		}
	}
	if len(inlineMimes) != 2 || inlineMimes[0] != "image/png" || inlineMimes[1] != "application/pdf" {
		t.Errorf("inlineMimes = %v, want [image/png application/pdf]", inlineMimes)
	}
}

func TestAnthropicMessages_MultimodalDocument(t *testing.T) {
	var capturedEnvelope geminicode.GenerateContentRequest
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedEnvelope)
		fakeCloudCodeSSE(w, []geminicode.Part{{Text: "Document processed."}}, nil)
	}))
	defer up.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: up.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	reqBody := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "Here is a PDF:"},
					map[string]any{
						"type": "document",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "application/pdf",
							"data":       "JVBERi0xLjQKJcOkw7zDtsOfCjEgMCBvYmoKPDwKL1R5cGUgL0NhdGFsb2cKPj4KZW5kb2Jq",
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(reqBody)
	resp, err := http.Post("http://"+addr+"/v1/messages", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST /v1/messages: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, string(out))
	}

	var inlineMimes []string
	if capturedEnvelope.Request != nil && len(capturedEnvelope.Request.Contents) > 0 {
		for _, p := range capturedEnvelope.Request.Contents[0].Parts {
			if p.InlineData != nil {
				inlineMimes = append(inlineMimes, p.InlineData.MIMEType)
			}
		}
	}
	if len(inlineMimes) != 1 || inlineMimes[0] != "application/pdf" {
		t.Errorf("inlineMimes = %v, want [application/pdf]", inlineMimes)
	}
}

func TestOpenAIChat_RemoteImageURL(t *testing.T) {
	fakeImageBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(fakeImageBytes)
	}))
	defer imageServer.Close()

	var capturedEnvelope geminicode.GenerateContentRequest
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedEnvelope)
		fakeCloudCodeSSE(w, []geminicode.Part{{Text: "Fetched."}}, nil)
	}))
	defer up.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: up.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	reqBody := map[string]any{
		"model": "gemini-3.8-flash-high",
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": imageServer.URL + "/test.png",
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(reqBody)
	resp, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	var inlineData *geminicode.InlineData
	if capturedEnvelope.Request != nil && len(capturedEnvelope.Request.Contents) > 0 {
		for _, p := range capturedEnvelope.Request.Contents[0].Parts {
			if p.InlineData != nil {
				inlineData = p.InlineData
			}
		}
	}
	if inlineData == nil || inlineData.MIMEType != "image/png" || inlineData.Data != base64.StdEncoding.EncodeToString(fakeImageBytes) {
		t.Errorf("expected inlineData with image/png, got %+v", inlineData)
	}
}

func TestBridge_LocalFilePathMedia(t *testing.T) {
	// Create a temporary PNG file and PDF file on disk
	fakePng := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01")
	tmpPng, err := os.CreateTemp("", "test-*.png")
	if err != nil {
		t.Fatalf("create temp png: %v", err)
	}
	defer os.Remove(tmpPng.Name())
	_, _ = tmpPng.Write(fakePng)
	_ = tmpPng.Close()

	fakePdf := []byte("%PDF-1.5 test content")
	tmpPdf, err := os.CreateTemp("", "test-*.pdf")
	if err != nil {
		t.Fatalf("create temp pdf: %v", err)
	}
	defer os.Remove(tmpPdf.Name())
	_, _ = tmpPdf.Write(fakePdf)
	_ = tmpPdf.Close()

	var capturedEnvelope geminicode.GenerateContentRequest
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedEnvelope)
		fakeCloudCodeSSE(w, []geminicode.Part{{Text: "Media processed"}}, nil)
	}))
	defer up.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: up.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	// Test 1: OpenAI chat completions with file:// URI and absolute path
	reqBody := map[string]any{
		"model": "gemini-3.8-flash-high",
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": "file://" + tmpPng.Name(),
						},
					},
					map[string]any{
						"type": "document",
						"data": tmpPdf.Name(),
					},
				},
			},
		},
	}
	b, _ := json.Marshal(reqBody)
	resp, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	var foundPng, foundPdf bool
	req := capturedEnvelope.Request
	if req == nil {
		req = &capturedEnvelope
	}
	if len(req.Contents) > 0 {
		for _, p := range req.Contents[0].Parts {
			if p.InlineData != nil {
				if p.InlineData.MIMEType == "image/png" && p.InlineData.Data == base64.StdEncoding.EncodeToString(fakePng) {
					foundPng = true
				}
				if p.InlineData.MIMEType == "application/pdf" && p.InlineData.Data == base64.StdEncoding.EncodeToString(fakePdf) {
					foundPdf = true
				}
			}
		}
	}
	if !foundPng {
		t.Errorf("did not find local png inline data in request")
	}
	if !foundPdf {
		t.Errorf("did not find local pdf inline data in request")
	}
}

func TestBridge_StreamingKeepAlive(t *testing.T) {
	origInterval := keepAliveInterval
	keepAliveInterval = 30 * time.Millisecond
	defer func() { keepAliveInterval = origInterval }()

	// Upstream flushes headers, then pauses 80ms before sending data, so keep-alive heartbeat fires.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(80 * time.Millisecond)
		fakeCloudCodeSSE(w, []geminicode.Part{{Text: "Hello after thinking"}}, nil)
	}))
	defer up.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: up.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	// 1. OpenAI stream test
	reqBody := map[string]any{
		"model":  "gemini-3.8-flash-high",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	b, _ := json.Marshal(reqBody)
	resp, err := http.Post("http://"+addr+"/v1/chat/completions", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST OpenAI: %v", err)
	}
	defer resp.Body.Close()

	rawStream, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	streamStr := string(rawStream)
	if !strings.Contains(streamStr, ": keep-alive") {
		t.Errorf("expected ': keep-alive' comment in stream, got:\n%s", streamStr)
	}

	// 2. Anthropic stream test
	reqAnthropic := map[string]any{
		"model":  "claude-3-7-sonnet",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}
	bA, _ := json.Marshal(reqAnthropic)
	respA, err := http.Post("http://"+addr+"/v1/messages", "application/json", bytes.NewReader(bA))
	if err != nil {
		t.Fatalf("POST Anthropic: %v", err)
	}
	defer respA.Body.Close()

	rawStreamA, err := io.ReadAll(respA.Body)
	if err != nil {
		t.Fatalf("read stream A: %v", err)
	}
	streamStrA := string(rawStreamA)
	if !strings.Contains(streamStrA, "event: ping") {
		t.Errorf("expected 'event: ping' in stream, got:\n%s", streamStrA)
	}
}

func TestNormalizeModelName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "gemini-3.8-flash-high"},
		{"gemini-3.8-flash", "gemini-3.8-flash-high"},
		{"gemini-3.8-flash-medium", "gemini-3.8-flash-medium"},
		{"gemini-3.1-pro-high", "gemini-3.1-pro-high"},
		{"gemini-3.1-pro-low", "gemini-3.1-pro-low"},
		{"gemini-2.5-pro", "gemini-2.5-pro"},
		{"gemini-2.5-flash", "gemini-2.5-flash"},
		{"gpt-6.1-sol", "gemini-3.8-flash-high"},
		{"gpt-6-astra", "gemini-3.8-flash-high"},
		{"gpt-5.6-luna", "gemini-3.8-flash-high"},
		{"gpt-5.6-terra", "gemini-3.8-flash-high"},
		{"gpt-4o", "gemini-3.8-flash-high"},
		{"sol", "gemini-3.8-flash-high"},
		{"luna", "gemini-3.8-flash-high"},
		{"o1-preview", "gemini-3.8-flash-high"},
		{"o3-mini", "gemini-3.8-flash-high"},
		{"o4-mini", "gemini-3.8-flash-high"},
		{"gpt-oss-120b-medium", "gpt-oss-120b-medium"},
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
	}
	for _, tc := range cases {
		got := normalizeModelName(tc.in)
		if got != tc.want {
			t.Errorf("normalizeModelName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
