// openai.go — OpenAI-shaped provider implementation.
//
// Handles openai, openrouter, ollama, and custom providers that speak
// the OpenAI Chat Completions wire dialect.
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

// openAIShapeProvider handles the four OpenAI-shaped providers.
// "label" is what's returned by Name() so logs can distinguish "openai"
// from "openrouter" even though they share the wire.
type openAIShapeProvider struct {
	model      Model
	httpClient *http.Client
	label      string
}

// Name implements Provider.
func (p *openAIShapeProvider) Name() string { return p.label }

type openAIRequestBody struct {
	Model           string            `json:"model"`
	Messages        []openAIMessage   `json:"messages"`
	Stream          bool              `json:"stream,omitempty"`
	MaxTokens       int               `json:"max_tokens,omitempty"`
	Temperature     float64           `json:"temperature,omitempty"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	StreamOptions   *openAIStreamOpts `json:"stream_options,omitempty"`
	Tools           []openAITool      `json:"tools,omitempty"`
}

type openAIStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type openAIChoiceDelta struct {
	Delta struct {
		Content   string                `json:"content"`
		ToolCalls []openAIToolCallDelta `json:"tool_calls"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

type openAIResponseBody struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat implements Provider.
func (p *openAIShapeProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body := p.buildBody(req, false)
	resp, err := p.doPost(ctx, body)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return ChatResponse{Raw: raw}, &UpstreamError{Status: resp.StatusCode, Body: truncate(string(raw), 240)}
	}
	var parsed openAIResponseBody
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ChatResponse{Raw: raw}, fmt.Errorf("decode: %w", err)
	}
	out := ChatResponse{
		Model:        parsed.Model,
		Raw:          raw,
		PromptTokens: parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		TotalTokens:  parsed.Usage.TotalTokens,
	}
	if len(parsed.Choices) > 0 {
		out.Content = parsed.Choices[0].Message.Content
		out.FinishReason = parsed.Choices[0].FinishReason
		out.ToolCalls = toolCallsFromWire(parsed.Choices[0].Message.ToolCalls)
	}
	return out, nil
}

// Stream implements Provider.
func (p *openAIShapeProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body := p.buildBody(req, true)
	resp, err := p.doPost(ctx, body)
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

func (p *openAIShapeProvider) runStreamLoop(ctx context.Context, body io.ReadCloser, out chan<- StreamEvent) {
	defer close(out)
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var meta *StreamMeta
	partial := map[int]*ToolCall{}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		if p.processSSEFrame(ctx, payload, out, partial, &meta) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		out <- StreamEvent{Error: fmt.Errorf("sse read: %w", err), Done: true}
		return
	}
	flushToolCalls(ctx, out, partial)
	out <- StreamEvent{Done: true, Meta: meta}
}

func (p *openAIShapeProvider) processSSEFrame(
	ctx context.Context,
	payload string,
	out chan<- StreamEvent,
	partial map[int]*ToolCall,
	meta **StreamMeta,
) bool {
	var envelope struct {
		Choices []openAIChoiceDelta `json:"choices"`
		Usage   *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		select {
		case <-ctx.Done():
			out <- StreamEvent{Error: ctx.Err(), Done: true}
		case out <- StreamEvent{Error: fmt.Errorf("sse decode: %w", err), Done: true}:
		}
		return true
	}
	if envelope.Usage != nil {
		*meta = &StreamMeta{
			PromptTokens:     envelope.Usage.PromptTokens,
			CompletionTokens: envelope.Usage.CompletionTokens,
			TotalTokens:      envelope.Usage.TotalTokens,
		}
	}
	return p.handleChoices(ctx, envelope.Choices, out, partial)
}

func (p *openAIShapeProvider) handleChoices(
	ctx context.Context,
	choices []openAIChoiceDelta,
	out chan<- StreamEvent,
	partial map[int]*ToolCall,
) bool {
	for _, c := range choices {
		for _, tc := range c.Delta.ToolCalls {
			cur, ok := partial[tc.Index]
			if !ok {
				cur = &ToolCall{}
				partial[tc.Index] = cur
			}
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			if tc.Function.Name != "" {
				cur.Name += tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				cur.Arguments += tc.Function.Arguments
			}
		}
		if c.Delta.Content != "" {
			select {
			case <-ctx.Done():
				out <- StreamEvent{Error: ctx.Err(), Done: true}
				return true
			case out <- StreamEvent{Text: c.Delta.Content}:
			}
		}
		if c.FinishReason != "" {
			flushToolCalls(ctx, out, partial)
		}
	}
	return false
}

func flushToolCalls(ctx context.Context, out chan<- StreamEvent, partial map[int]*ToolCall) {
	if len(partial) == 0 {
		return
	}
	calls := make([]ToolCall, 0, len(partial))
	for i := 0; i < len(partial); i++ {
		c, ok := partial[i]
		if !ok || c.Name == "" {
			continue
		}
		calls = append(calls, *c)
	}
	for k := range partial {
		delete(partial, k)
	}
	if len(calls) == 0 {
		return
	}
	select {
	case <-ctx.Done():
		out <- StreamEvent{Error: ctx.Err(), Done: true}
	case out <- StreamEvent{ToolCalls: calls}:
	}
}

func (p *openAIShapeProvider) buildBody(req ChatRequest, stream bool) openAIRequestBody {
	msgs := make([]openAIMessage, 0, len(req.Messages)+1)
	if sp := strings.TrimSpace(req.SystemPrompt); sp != "" {
		msgs = append(msgs, openAIMessage{Role: "system", Content: sp})
	}
	for _, m := range req.Messages {
		wire := openAIMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			wire.ToolCalls = append(wire.ToolCalls, toolCallToWire(tc))
		}
		msgs = append(msgs, wire)
	}
	ext := req.ExternalModel
	if ext == "" {
		ext = p.model.ExternalModelName
	}
	body := openAIRequestBody{
		Model:           ext,
		Messages:        msgs,
		Stream:          stream,
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		ReasoningEffort: req.ReasoningEffort,
	}
	for _, t := range req.Tools {
		params := t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		body.Tools = append(body.Tools, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	if stream {
		body.StreamOptions = &openAIStreamOpts{IncludeUsage: true}
	}
	return body
}

func toolCallToWire(tc ToolCall) openAIToolCall {
	args := tc.Arguments
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	w := openAIToolCall{ID: tc.ID, Type: "function"}
	w.Function.Name = tc.Name
	w.Function.Arguments = args
	return w
}

func toolCallsFromWire(in []openAIToolCall) []ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(in))
	for _, tc := range in {
		out = append(out, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	return out
}

func (p *openAIShapeProvider) doPost(ctx context.Context, body openAIRequestBody) (*http.Response, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.model.APIURL, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	switch p.label {
	case "ollama":
	default:
		if key := strings.TrimSpace(p.model.APIKey); key != "" {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
	}
	if p.label == "openrouter" {
		httpReq.Header.Set("HTTP-Referer", "https://github.com/justwasm/pigasus")
		httpReq.Header.Set("X-Title", "openagy (recovered)")
	}
	return p.httpClient.Do(httpReq)
}
