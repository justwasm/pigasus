package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
)

func (s *server) routeRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.corsHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "") {
		s.handleRoot(w, r)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no route for %s", r.URL.Path))
}

func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	manifest := map[string]any{
		"service":     s.rootMeta.Service,
		"upstream":    s.rootMeta.Upstream,
		"proxy":       s.rootMeta.Proxy,
		"project":     s.rootMeta.Project,
		"public_url":  s.rootMeta.PublicURL,
		"listen_addr": s.rootMeta.ListenAddr,
		"endpoints": []string{
			"GET  " + s.rootMeta.PublicURL + "/v1/models",
			"POST " + s.rootMeta.PublicURL + "/v1/messages",
			"GET  " + s.rootMeta.PublicURL + "/healthz",
		},
		"example_curl_message": `curl -sX POST ` + s.rootMeta.PublicURL + `/v1/messages -H 'content-type: application/json' -H 'anthropic-version: 2023-06-01' -d '{"model":"claude-sonnet-4-6","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}'`,
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /healthz")
		return
	}
	_, tokErr := s.tokens.AccessToken()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     tokErr == nil,
		"token":  tokErr == nil,
		"src":    s.tokens.Source(),
		"exp":    formatTime(s.tokens.ExpiresAt()),
		"time":   time.Now().Format(time.RFC3339),
		"models": len(knownModels),
	})
}

func (s *server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /v1/models")
		return
	}
	out := struct {
		Data []map[string]string `json:"data"`
	}{Data: make([]map[string]string, 0, len(knownModels))}
	for _, id := range knownModels {
		out.Data = append(out.Data, map[string]string{
			"type": "model",
			"name": id,
			"id":   id,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.corsHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required for /v1/messages")
		return
	}
	s.corsHeaders(w)
	req, ok := parseMessagesRequest(w, r)
	if !ok {
		return
	}

	msgs, sysPrompt := translateMessagesToBackend(req)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	id := "msg_" + newID()
	if req.Stream {
		s.streamAnthropic(ctx, w, req, id, sysPrompt, msgs)
		return
	}
	s.nonStreamAnthropic(ctx, w, req, id, sysPrompt, msgs)
}

func parseMessagesRequest(w http.ResponseWriter, r *http.Request) (messagesRequest, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_failed", err.Error())
		return messagesRequest{}, false
	}
	defer r.Body.Close()

	var req messagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON: "+err.Error())
		return req, false
	}
	if strings.TrimSpace(req.Model) == "" {
		writeError(w, http.StatusBadRequest, "missing_model", "`model` field is required")
		return req, false
	}
	if !modelKnown(req.Model) {
		writeError(w, http.StatusBadRequest, "unknown_model", fmt.Sprintf("unknown model %q", req.Model))
		return req, false
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "no_messages", "`messages` must contain at least one message")
		return req, false
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 1024
	}
	return req, true
}

func translateMessagesToBackend(req messagesRequest) ([]backend.Message, string) {
	var sys string
	if s := strings.TrimSpace(req.System); s != "" {
		sys = s
	}
	out := make([]backend.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		out = append(out, backend.Message{
			Role:    m.Role,
			Content: flattenContent(m.Content),
		})
	}
	return out, sys
}

func flattenContent(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, blk := range v {
			m, ok := blk.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "text":
				if s, ok := m["text"].(string); ok {
					b.WriteString(s)
				}
			case "image":
				b.WriteString("[image]")
			case "tool_use":
				b.WriteString("[tool_use]")
			case "tool_result":
				b.WriteString("[tool_result]")
			}
		}
		return b.String()
	}
	return ""
}

type anthropicEmitter func(event string, payload any) error

func (s *server) streamAnthropic(ctx context.Context, w http.ResponseWriter, req messagesRequest, id, sysPrompt string, msgs []backend.Message) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	emit := func(event string, payload any) error {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	emitStreamPrologue(emit, id, req.Model)
	var usage *backend.ChunkMeta
	handler := createStreamHandler(emit, &usage)

	loop, err := agent.New(agent.Options{
		Backend:      s.backend,
		Handler:      handler,
		SystemPrompt: sysPrompt,
		DefaultModel: req.Model,
		MaxTurns:     1,
	})
	if err != nil {
		_ = emit("error", map[string]any{"type": "error", "error": map[string]any{"type": "internal_error", "message": err.Error()}})
		return
	}
	defer loop.Close()
	_ = loop.Send(ctx, agent.TurnInput{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Messages:    msgs,
	})
	emitStreamEpilogue(emit, usage)
}

func emitStreamPrologue(emit anthropicEmitter, id, model string) {
	_ = emit("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":      id,
			"type":    "message",
			"role":    "assistant",
			"model":   model,
			"content": []any{},
		},
	})
	_ = emit("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	})
	_ = emit("ping", map[string]any{"type": "ping"})
}

func createStreamHandler(emit anthropicEmitter, usage **backend.ChunkMeta) func(backend.StreamChunk) error {
	return func(chunk backend.StreamChunk) error {
		if chunk.Error != nil {
			_ = emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": chunk.Error.Error()},
			})
			return nil
		}
		if chunk.Text != "" {
			return emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": chunk.Text},
			})
		}
		if chunk.Done {
			*usage = chunk.Meta
		}
		return nil
	}
}

func emitStreamEpilogue(emit anthropicEmitter, usage *backend.ChunkMeta) {
	_ = emit("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	})
	md := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
	}
	if usage != nil {
		md["usage"] = map[string]any{
			"input_tokens":  usage.PromptTokens,
			"output_tokens": usage.CompletionTokens,
		}
	}
	_ = emit("message_delta", md)
	_ = emit("message_stop", map[string]any{"type": "message_stop"})
}

func (s *server) nonStreamAnthropic(ctx context.Context, w http.ResponseWriter, req messagesRequest, id, sysPrompt string, msgs []backend.Message) {
	var acc strings.Builder
	var usage *backend.ChunkMeta
	loop, err := agent.New(agent.Options{
		Backend: s.backend,
		Handler: func(chunk backend.StreamChunk) error {
			acc.WriteString(chunk.Text)
			if chunk.Meta != nil {
				usage = chunk.Meta
			}
			return nil
		},
		SystemPrompt: sysPrompt,
		DefaultModel: req.Model,
		MaxTurns:     1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "agent_init", err.Error())
		return
	}
	defer loop.Close()
	_ = loop.Send(ctx, agent.TurnInput{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Messages:    msgs,
	})

	resp := map[string]any{
		"id":            id,
		"type":          "message",
		"role":          "assistant",
		"model":         req.Model,
		"content":       []map[string]string{{"type": "text", "text": acc.String()}},
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
	}
	if usage != nil {
		resp["usage"] = map[string]any{
			"input_tokens":  usage.PromptTokens,
			"output_tokens": usage.CompletionTokens,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
