package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MockChunk represents one streaming frame emitted by the mock server.
type MockChunk struct {
	Thought      string        `json:"thought,omitempty"`
	Text         string        `json:"text,omitempty"`
	ToolCall     *ToolCallSpec `json:"tool_call,omitempty"`
	FinishReason string        `json:"finish_reason,omitempty"`
	Delay        time.Duration `json:"delay_ms,omitempty"`

	// Token counters
	PromptTokens    int `json:"prompt_tokens,omitempty"`
	CandidateTokens int `json:"candidate_tokens,omitempty"`
	ThoughtTokens   int `json:"thought_tokens,omitempty"`
}

// ToolCallSpec defines a tool invocation from the model.
type ToolCallSpec struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
	ID   string         `json:"id,omitempty"`
}

// MockTurn represents one expected LLM response turn, composed of one or more streaming chunks.
type MockTurn struct {
	Chunks []MockChunk `json:"chunks"`
}

// RecordedRequest tracks an inbound HTTP request for inspection and assertions.
type RecordedRequest struct {
	Time   time.Time         `json:"time"`
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Header map[string]string `json:"header"`
	Body   string            `json:"body"`
}

// MockServer provides the local HTTP service serving deterministic CloudCode endpoints.
type MockServer struct {
	mu           sync.Mutex
	listener     net.Listener
	httpServer   *http.Server
	addr         string
	turns        []MockTurn
	clientTurns  map[string]int // client identifier -> current turn index
	requests     []RecordedRequest
	upStreamURL  string
	verboseLogs  bool
}

// NewMockServer creates a new mock server.
func NewMockServer(addr string, verbose bool) (*MockServer, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("mockserver: listen failed on %s: %w", addr, err)
	}

	s := &MockServer{
		listener:    ln,
		addr:        ln.Addr().String(),
		clientTurns: make(map[string]int),
		verboseLogs: verbose,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleDispatch)
	s.httpServer = &http.Server{
		Handler: mux,
	}

	return s, nil
}

// Start begins serving in a background goroutine.
func (s *MockServer) Start() {
	go func() {
		_ = s.httpServer.Serve(s.listener)
	}()
}

// URL returns the base HTTP URL ("http://127.0.0.1:PORT").
func (s *MockServer) URL() string {
	return "http://" + s.addr
}

// Close stops the mock server.
func (s *MockServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

// AddTurn appends a planned turn to the mock server queue.
func (s *MockServer) AddTurn(turn MockTurn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns = append(s.turns, turn)
}

// Reset clears turns, client progress, and recorded requests.
func (s *MockServer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns = nil
	s.clientTurns = make(map[string]int)
	s.requests = nil
}

// Requests returns a copy of all recorded requests.
func (s *MockServer) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RecordedRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *MockServer) logf(format string, args ...any) {
	if s.verboseLogs {
		fmt.Printf("[tuialign:mock] "+format+"\n", args...)
	}
}

func (s *MockServer) handleDispatch(w http.ResponseWriter, r *http.Request) {
	bodyBytes, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	s.mu.Lock()
	rec := RecordedRequest{
		Time:   time.Now(),
		Method: r.Method,
		Path:   r.URL.Path,
		Header: make(map[string]string),
		Body:   string(bodyBytes),
	}
	for k := range r.Header {
		rec.Header[k] = r.Header.Get(k)
	}
	s.requests = append(s.requests, rec)
	s.mu.Unlock()

	path := r.URL.Path

	// Control endpoints
	if strings.HasPrefix(path, "/_mock/") {
		s.handleControl(w, r, bodyBytes)
		return
	}

	s.logf("%s %s (%d bytes)", r.Method, path, len(bodyBytes))

	// CloudCode service routing
	switch {
	case strings.HasSuffix(path, ":loadCodeAssist"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(DefaultSynthesizedCodeAssist())

	case strings.HasSuffix(path, ":onboardUser"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(DefaultOnboardUser())

	case strings.HasSuffix(path, ":retrieveUserQuotaSummary"), strings.HasSuffix(path, ":retrieveUserQuota"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(DefaultQuotaSummary())

	case strings.HasSuffix(path, ":fetchAvailableModels"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(DefaultModelsJSON())

	case strings.HasSuffix(path, ":fetchUserInfo"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(DefaultUserInfo(""))

	case strings.HasSuffix(path, ":streamGenerateContent"):
		s.handleStreamGenerateContent(w, r, bodyBytes)

	case strings.HasSuffix(path, ":countTokens"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalTokens":10}`))

	default:
		// Default stub 200 OK for other metadata RPCs (listExperiments, writeTrajectoryAcls, fetchAdminControls, etc.)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
}

func (s *MockServer) handleControl(w http.ResponseWriter, r *http.Request, body []byte) {
	switch r.URL.Path {
	case "/_mock/health":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	case "/_mock/reset":
		s.Reset()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	case "/_mock/turn":
		var turn MockTurn
		if err := json.Unmarshal(body, &turn); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.AddTurn(turn)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	case "/_mock/history":
		w.Header().Set("Content-Type", "application/json")
		reqs := s.Requests()
		_ = json.NewEncoder(w).Encode(reqs)
	default:
		http.NotFound(w, r)
	}
}

func (s *MockServer) resolveClientKey(r *http.Request, body []byte) string {
	// Try parsing sessionId or requestId from request payload
	var doc struct {
		SessionID string `json:"sessionId"`
		Request   struct {
			SessionID string `json:"sessionId"`
			RequestID string `json:"requestId"`
		} `json:"request"`
	}
	if err := json.Unmarshal(body, &doc); err == nil {
		if doc.Request.SessionID != "" {
			return doc.Request.SessionID
		}
		if doc.SessionID != "" {
			return doc.SessionID
		}
		if doc.Request.RequestID != "" {
			return doc.Request.RequestID
		}
	}
	// Fall back to RemoteAddr
	return r.RemoteAddr
}

func (s *MockServer) handleStreamGenerateContent(w http.ResponseWriter, r *http.Request, body []byte) {
	clientKey := s.resolveClientKey(r, body)
	snippet := string(body)
	if len(snippet) > 300 {
		snippet = snippet[:300]
	}
	s.logf("[streamGenerateContent] resolved clientKey=%q, User-Agent=%q, snippet=%s", clientKey, r.UserAgent(), snippet)

	// Background title generator shouldn't consume user turn mocks
	if strings.Contains(string(body), "conversation title generator") || strings.Contains(string(body), `"requestId":"checkpoint/`) {
		s.logf("[%s] answering background title generator request", clientKey)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		titleFrame := buildCandidateFrame("", "Greeting and Alignment", nil, "STOP", 5, 3, 0)
		_, _ = w.Write(FormatSSEChunk(titleFrame))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	s.mu.Lock()
	turnIdx := s.clientTurns[clientKey]
	var currentTurn *MockTurn
	if turnIdx < len(s.turns) {
		t := s.turns[turnIdx]
		currentTurn = &t
		s.clientTurns[clientKey] = turnIdx + 1
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	if currentTurn == nil || len(currentTurn.Chunks) == 0 {
		s.logf("[%s] no mock turn available for turn #%d, sending default fallback reply", clientKey, turnIdx)
		// Default fallback response
		fallback := buildCandidateFrame("", "OK (mock response from tuialign)", nil, "STOP", 10, 5, 0)
		_, _ = w.Write(FormatSSEChunk(fallback))
		if canFlush {
			flusher.Flush()
		}
		return
	}

	s.logf("[%s] streaming turn #%d with %d chunks", clientKey, turnIdx, len(currentTurn.Chunks))

	for i, chunk := range currentTurn.Chunks {
		if chunk.Delay > 0 {
			time.Sleep(chunk.Delay)
		}

		frame := buildCandidateFrame(
			chunk.Thought,
			chunk.Text,
			chunk.ToolCall,
			chunk.FinishReason,
			chunk.PromptTokens,
			chunk.CandidateTokens,
			chunk.ThoughtTokens,
		)

		_, _ = w.Write(FormatSSEChunk(frame))
		if canFlush {
			flusher.Flush()
		}
		s.logf("[%s] sent chunk #%d (thought=%d chars, text=%d chars, stop=%s)",
			clientKey, i, len(chunk.Thought), len(chunk.Text), chunk.FinishReason)
	}
}

func buildCandidateFrame(thought, text string, toolCall *ToolCallSpec, finishReason string, promptTokens, candidateTokens, thoughtTokens int) []byte {
	var parts []map[string]any

	if thought != "" {
		parts = append(parts, map[string]any{
			"thought": true,
			"text":    thought,
		})
	}

	if toolCall != nil {
		id := toolCall.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", time.Now().UnixNano()%1000000)
		}
		parts = append(parts, map[string]any{
			"thoughtSignature": "tuialign_sig_" + id,
			"functionCall": map[string]any{
				"name": toolCall.Name,
				"args": toolCall.Args,
				"id":   id,
			},
		})
	}

	if text != "" {
		parts = append(parts, map[string]any{
			"text": text,
		})
	}

	// Always ensure at least one part exists
	if len(parts) == 0 {
		parts = append(parts, map[string]any{
			"text": "",
		})
	}

	cand := map[string]any{
		"content": map[string]any{
			"role":  "model",
			"parts": parts,
		},
		"index": 0,
	}
	if finishReason != "" {
		cand["finishReason"] = finishReason
	}

	totalTokens := promptTokens + candidateTokens + thoughtTokens
	usage := map[string]any{
		"promptTokenCount":     promptTokens,
		"candidatesTokenCount": candidateTokens,
		"thoughtsTokenCount":   thoughtTokens,
		"totalTokenCount":      totalTokens,
	}

	resp := map[string]any{
		"response": map[string]any{
			"candidates":    []any{cand},
			"usageMetadata": usage,
		},
		"traceId": fmt.Sprintf("tuialign_trace_%d", time.Now().UnixMilli()),
	}

	b, _ := json.Marshal(resp)
	return b
}
