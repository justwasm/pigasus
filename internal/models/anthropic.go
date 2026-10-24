// anthropic.go — Anthropic provider implementation.
//
// Speaks the Anthropic Messages wire dialect (/v1/messages).
package models

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// anthropicProvider talks to /v1/messages.
type anthropicProvider struct {
	model      Model
	httpClient *http.Client
}

// Name implements Provider.
func (p *anthropicProvider) Name() string { return "anthropic" }

type anthropicRequestBody struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Stream      bool               `json:"stream,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Chat implements Provider.
func (p *anthropicProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body := p.buildBody(req, false)
	buf, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.model.APIURL, bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", p.model.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return ChatResponse{Raw: raw}, &UpstreamError{Status: resp.StatusCode, Body: truncate(string(raw), 240)}
	}
	var parsed anthropicResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ChatResponse{Raw: raw}, fmt.Errorf("decode: %w", err)
	}
	var content string
	for _, c := range parsed.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}
	return ChatResponse{
		Content:      content,
		Model:        parsed.Model,
		FinishReason: parsed.StopReason,
		PromptTokens: parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		TotalTokens:  parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
		Raw:          raw,
	}, nil
}

// Stream implements Provider.
func (p *anthropicProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body := p.buildBody(req, true)
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.model.APIURL, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", p.model.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &UpstreamError{Status: resp.StatusCode, Body: truncate(string(raw), 240)}
	}
	out := make(chan StreamEvent, 16)
	go p.runStreamLoop(ctx, resp.Body, out)
	return out, nil
}

func (p *anthropicProvider) runStreamLoop(ctx context.Context, body io.ReadCloser, out chan<- StreamEvent) {
	defer close(out)
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var eventType string
	var meta *StreamMeta
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			payload := strings.TrimPrefix(line, "data: ")
			if p.handleEvent(ctx, eventType, payload, out, &meta) {
				return
			}
			eventType = ""
		}
	}
	if err := scanner.Err(); err != nil {
		out <- StreamEvent{Error: fmt.Errorf("sse read: %w", err), Done: true}
		return
	}
	out <- StreamEvent{Done: true, Meta: meta}
}

func (p *anthropicProvider) handleEvent(
	ctx context.Context,
	eventType, payload string,
	out chan<- StreamEvent,
	meta **StreamMeta,
) bool {
	switch eventType {
	case "content_block_delta":
		return emitContentDelta(ctx, payload, out)
	case "message_delta":
		updateMessageDelta(payload, meta)
	case "message_stop":
		out <- StreamEvent{Done: true, Meta: *meta}
		return true
	case "error":
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(payload), &e)
		out <- StreamEvent{Error: fmt.Errorf("anthropic: %s", e.Error.Message), Done: true}
		return true
	}
	return false
}

func emitContentDelta(ctx context.Context, payload string, out chan<- StreamEvent) bool {
	var d struct {
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(payload), &d); err == nil && d.Delta.Text != "" {
		select {
		case <-ctx.Done():
			out <- StreamEvent{Error: ctx.Err(), Done: true}
			return true
		case out <- StreamEvent{Text: d.Delta.Text}:
		}
	}
	return false
}

func updateMessageDelta(payload string, meta **StreamMeta) {
	var d struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &d); err == nil {
		*meta = &StreamMeta{
			PromptTokens:     d.Usage.InputTokens,
			CompletionTokens: d.Usage.OutputTokens,
			TotalTokens:      d.Usage.InputTokens + d.Usage.OutputTokens,
		}
	}
}

func (p *anthropicProvider) buildBody(req ChatRequest, stream bool) anthropicRequestBody {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	msgs := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "system" {
			continue
		}
		msgs = append(msgs, anthropicMessage{Role: m.Role, Content: m.Content})
	}
	ext := req.ExternalModel
	if ext == "" {
		ext = p.model.ExternalModelName
	}
	return anthropicRequestBody{
		Model:       ext,
		Messages:    msgs,
		System:      req.SystemPrompt,
		MaxTokens:   maxTokens,
		Stream:      stream,
		Temperature: req.Temperature,
	}
}
