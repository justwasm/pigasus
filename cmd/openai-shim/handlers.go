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
	if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "") {
		s.handleRoot(w, r)
		return
	}
	if r.Method == http.MethodOptions {
		s.corsHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no route for %s", r.URL.Path))
}

func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /")
		return
	}
	manifest := map[string]any{
		"service":     s.rootMeta.Service,
		"upstream":    s.rootMeta.Upstream,
		"proxy":       s.rootMeta.Proxy,
		"project":     s.rootMeta.Project,
		"public_url":  s.rootMeta.PublicURL,
		"listen_addr": s.rootMeta.ListenAddr,
		"endpoints": []string{
			"GET  " + s.rootMeta.PublicURL + "/v1/models",
			"POST " + s.rootMeta.PublicURL + "/v1/chat/completions",
			"GET  " + s.rootMeta.PublicURL + "/healthz",
		},
		"example_curl_chat": `curl -sX POST ` + s.rootMeta.PublicURL + `/v1/chat/completions -H 'content-type: application/json' -d '{"model":"gemini-3-flash","messages":[{"role":"user","content":"hi"}]}'`,
		"openai_sdk_python": map[string]any{
			"base_url": s.rootMeta.PublicURL + "/v1",
			"api_key":  "ignored",
			"example":  `import openai; c = openai.OpenAI(base_url="` + s.rootMeta.PublicURL + `/v1", api_key="ignored"); c.chat.completions.create(model="gemini-3-flash", messages=[{"role":"user","content":"hi"}]).choices[0].message.content`,
		},
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /healthz")
		return
	}
	_, tokErr := s.tokens.AccessToken()
	status := map[string]any{
		"ok":     tokErr == nil,
		"token":  tokErr == nil,
		"src":    s.tokens.Source(),
		"exp":    formatTime(s.tokens.ExpiresAt()),
		"time":   time.Now().Format(time.RFC3339),
		"models": len(knownModels),
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /v1/models")
		return
	}
	out := struct {
		Object string      `json:"object"`
		Data   []modelInfo `json:"data"`
	}{Object: "list", Data: knownModels}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.corsHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required for /v1/chat/completions")
		return
	}
	s.corsHeaders(w)
	req, ok := parseChatCompletionsRequest(w, r)
	if !ok {
		return
	}

	msgs, systemPrompt := translateOpenAIMessages(req.Messages)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	id := newUUIDv4()
	created := time.Now().Unix()

	if req.Stream {
		s.streamChatCompletions(ctx, w, req, msgs, systemPrompt, id, created)
		return
	}
	s.singleChatCompletions(ctx, w, req, msgs, systemPrompt, id, created)
}

func parseChatCompletionsRequest(w http.ResponseWriter, r *http.Request) (chatCompletionsRequest, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_failed", err.Error())
		return chatCompletionsRequest{}, false
	}
	defer r.Body.Close()

	var req chatCompletionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON: "+err.Error())
		return req, false
	}
	if strings.TrimSpace(req.Model) == "" {
		writeError(w, http.StatusBadRequest, "missing_model", "`model` field is required")
		return req, false
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "no_messages", "`messages` must contain at least one message")
		return req, false
	}
	if !modelKnown(req.Model) {
		writeError(w, http.StatusBadRequest, "unknown_model", fmt.Sprintf("unknown model %q", req.Model))
		return req, false
	}
	return req, true
}

func translateOpenAIMessages(reqMsgs []openAIReqMsg) ([]backend.Message, string) {
	msgs := make([]backend.Message, 0, len(reqMsgs))
	var systemPrompt string
	for _, m := range reqMsgs {
		if m.Role == "system" {
			systemPrompt = m.Content
			continue
		}
		msgs = append(msgs, backend.Message{Role: m.Role, Content: m.Content})
	}
	return msgs, systemPrompt
}

func (s *server) streamChatCompletions(ctx context.Context, w http.ResponseWriter, req chatCompletionsRequest, msgs []backend.Message, systemPrompt, id string, created int64) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	handler := func(chunk backend.StreamChunk) error {
		emit := formatOpenAIChunk(chunk, id, req.Model, created)
		data, err := json.Marshal(emit)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	loop, err := agent.New(agent.Options{
		Backend:      s.backend,
		Handler:      handler,
		SystemPrompt: systemPrompt,
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
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func formatOpenAIChunk(chunk backend.StreamChunk, id, model string, created int64) openAIChunk {
	emit := openAIChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model}
	if chunk.Error != nil {
		emit.Choices = []openAIChoice{{Index: 0, FinishReason: "error", Message: openAIMsg{Role: "assistant", Content: chunk.Error.Error()}}}
	} else if chunk.Text != "" {
		emit.Choices = []openAIChoice{{Index: 0, Delta: openAIMsg{Role: "assistant", Content: chunk.Text}}}
	} else {
		emit.Choices = []openAIChoice{{Index: 0, Delta: openAIMsg{}}}
	}
	if chunk.Done {
		emit.Choices[0].FinishReason = "stop"
		if chunk.Meta != nil {
			emit.Usage = &openAIUsage{
				PromptTokens:     chunk.Meta.PromptTokens,
				CompletionTokens: chunk.Meta.CompletionTokens,
				TotalTokens:      chunk.Meta.TotalTokens,
			}
			emit.SystemFingerprint = chunk.Meta.SystemFingerprint
		}
	}
	return emit
}

func (s *server) singleChatCompletions(ctx context.Context, w http.ResponseWriter, req chatCompletionsRequest, msgs []backend.Message, systemPrompt, id string, created int64) {
	var acc strings.Builder
	var meta *backend.ChunkMeta
	loop, err := agent.New(agent.Options{
		Backend: s.backend,
		Handler: func(chunk backend.StreamChunk) error {
			acc.WriteString(chunk.Text)
			if chunk.Meta != nil {
				meta = chunk.Meta
			}
			return nil
		},
		SystemPrompt: systemPrompt,
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
	resp := buildSingleResponse(id, req.Model, created, acc.String(), meta)
	writeJSON(w, http.StatusOK, resp)
}

func buildSingleResponse(id, model string, created int64, content string, meta *backend.ChunkMeta) openAIResponse {
	resp := openAIResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []openAIChoice{{
			Index: 0,
			Message: openAIMsg{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: "stop",
		}},
	}
	if meta != nil {
		resp.Usage = openAIUsage{
			PromptTokens:     meta.PromptTokens,
			CompletionTokens: meta.CompletionTokens,
			TotalTokens:      meta.TotalTokens,
		}
		resp.SystemFingerprint = meta.SystemFingerprint
	}
	return resp
}
