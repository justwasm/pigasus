// Package models — provider.go
//
// This file implements the seven providers documented in the SPEC:
//
//   openai       — POST <api_url> (OpenAI Chat Completions, supports stream SSE)
//   anthropic    — POST <api_url> (Anthropic Messages, supports stream SSE)
//   ollama       — POST <api_url> (Ollama /v1/chat/completions, OpenAI-shape)
//   google       — Gemini native (passthrough only, mock in sandbox; see notes)
//   openrouter   — OpenAI-compatible
//   agw          — agw.k0s.io (treated as a hostile/dirty OpenAI endpoint; we
//                  send a Bearer "-" sentinel and accept whatever comes back)
//   custom       — OpenAI-compatible, user-configured
//
// All providers satisfy Provider:
//
//	type Provider interface {
//	    Chat(ctx, req ChatRequest) (ChatResponse, error)
//	    Stream(ctx, req ChatRequest) (<-chan StreamEvent, error)
//	}
//
// ChatRequest and ChatResponse are the wire-neutral types — they look like
// our internal `backend.StreamRequest` / `backend.StreamChunk`, with two
// extra fields (ExternalModel, Reasoning) the provider layer knows about.
//
// The translation rules are kept inline (not abstracted into an interface
// seam) because the formats are already 90% identical and an abstraction
// would just hide the differences without reducing code. The seven
// constructors below are the entire surface.

package models

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatRequest is the provider-neutral input. We re-use the backend.Message
// shape so callers can pass the same []Message the shim built.
type ChatRequest struct {
	// ExternalModel overrides Model.ExternalModelName for this turn.
	// Empty means "use the registry value".
	ExternalModel string
	// SystemPrompt is prepended as a system role. Empty ⇒ no system msg.
	SystemPrompt string
	// Messages is the conversation tail (user / assistant only; the system
	// message has been hoisted out into SystemPrompt by the caller).
	Messages []ChatMessage
	// MaxTokens is optional. 0 ⇒ let the provider default.
	MaxTokens int
	// Temperature is optional. 0 ⇒ let the provider default.
	Temperature float64
	// Stream is set by Stream; the Chat method ignores it.
	Stream bool
	// ReasoningEffort is the OpenAI o-series hint. "" ⇒ don't send.
	ReasoningEffort string
	// Tools lists the functions the model may call (cycle 274). Nil means
	// "no tools", which keeps every pre-274 caller byte-identical on the wire.
	Tools []ToolSpec
}

// ChatMessage is a single conversation turn. Role is "user" or "assistant";
// "system" is hoisted out into ChatRequest.SystemPrompt.
//
// ToolCalls / ToolCallID are only meaningful on assistant / tool messages
// respectively and are omitted from the wire when empty.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ChatResponse is the non-streaming reply. Content is the assembled text;
// Usage mirrors the OpenAI usage block (or its Anthropic / Gemini shape).
type ChatResponse struct {
	Content      string
	Model        string
	FinishReason string
	PromptTokens int
	OutputTokens int
	TotalTokens  int
	// ToolCalls holds any function calls the model asked for (cycle 274).
	ToolCalls []ToolCall
	Raw       []byte // raw upstream body, for debugging
}

// StreamEvent is one element of a streaming response. We mirror the
// backend.StreamChunk shape so a single bridge function can lift provider
// events into the existing agent.Loop machinery.
type StreamEvent struct {
	Text  string
	Done  bool
	Error error
	// ToolCalls is set when the model asked to call one or more functions.
	// Providers that stream partial arguments (OpenAI) accumulate them and
	// emit a single event with the finished calls just before Done.
	ToolCalls []ToolCall
	// Meta is set on the final chunk (Done=true) and mirrors the
	// backend.ChunkMeta fields we care about.
	Meta *StreamMeta
}

// StreamMeta is the metadata we forward on the terminal chunk.
type StreamMeta struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Provider is the seven-impl interface. Chat is used by the CLI smoke test
// and by anything that doesn't need SSE; Stream is what the TUI / shims use.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
	// Name is the provider handle from the registry ("openai", "agw", ...).
	// Returned for log lines and `openagy-models test` output.
	Name() string
}

// NewProvider is the single entry point used by the registry, the CLI, and
// the agent loop. It maps the registry's `provider` string to one of the
// seven concrete implementations below. The HTTP client is shared and
// configured once per process.
func NewProvider(m Model) (Provider, error) {
	timeout := time.Duration(m.Timeout()) * time.Second
	httpClient := &http.Client{
		Timeout: timeout,
	}
	switch strings.ToLower(m.Provider) {
	case "openai", "openrouter", "ollama", "custom":
		// All four speak the OpenAI chat completions shape. The wire is
		// identical; the only difference is default headers (OpenRouter wants
		// an HTTP-Referer for ranking, ollama wants no auth, custom is
		// unrestricted). The case label preserves the original provider name
		// so Name() can echo it back.
		return &openAIShapeProvider{
			model:      m,
			httpClient: httpClient,
			label:      strings.ToLower(m.Provider),
		}, nil
	case "anthropic":
		return &anthropicProvider{
			model:      m,
			httpClient: httpClient,
		}, nil
	case "agw":
		// agw.k0s.io — Bearer "-" dev sentinel; we still go through the
		// OpenAI shape because the gateway's /v1/chat/completions endpoint
		// is OpenAI-compatible. (The Anthropic-shape is also exposed at
		// /anthropic/v1/messages but we let users point api_url there if
		// they want it.)
		return &openAIShapeProvider{
			model:      m,
			httpClient: httpClient,
			label:      "agw",
		}, nil
	case "google":
		// Gemini native would need a separate Gemini client. We don't ship
		// one yet — GoogleProvider returns a friendly error so the caller
		// knows to switch to the openai or anthropic shape.
		return &googleProvider{model: m, httpClient: httpClient}, nil
	default:
		return nil, fmt.Errorf("models: unknown provider %q", m.Provider)
	}
}

// ---- OpenAI / OpenRouter / Ollama / Custom / Agw ------------------------

// openAIShapeProvider handles the four OpenAI-shaped providers plus agw.
// "label" is what's returned by Name() so logs can distinguish "openai"
// from "openrouter" even though they share the wire.
type openAIShapeProvider struct {
	model      Model
	httpClient *http.Client
	label      string
}

// Name implements Provider.
func (p *openAIShapeProvider) Name() string { return p.label }

// openAIRequestBody is the wire shape POSTed to /v1/chat/completions.
// Kept private because the public ChatRequest is wire-neutral.
type openAIRequestBody struct {
	Model               string         `json:"model"`
	Messages            []openAIMessage `json:"messages"`
	Stream              bool           `json:"stream,omitempty"`
	MaxTokens           int            `json:"max_tokens,omitempty"`
	Temperature         float64        `json:"temperature,omitempty"`
	ReasoningEffort     string         `json:"reasoning_effort,omitempty"`
	StreamOptions       *openAIStreamOpts `json:"stream_options,omitempty"`
	Tools               []openAITool   `json:"tools,omitempty"`
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

// openAITool is the function-declaration envelope: {"type":"function", …}.
type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// openAIToolCall is one finished call as it appears on a response message.
type openAIToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// openAIToolCallDelta is the streaming variant: id/name arrive once, then
// fragments of `arguments` accumulate across frames.
type openAIToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// openAIResponseBody is the non-streaming reply. We only decode the fields
// we actually surface; the rest is preserved in Raw.
type openAIResponseBody struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int `json:"index"`
		Message      openAIMessage `json:"message"`
		FinishReason string `json:"finish_reason"`
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
		return ChatResponse{Raw: raw}, fmt.Errorf("upstream %d: %s", resp.StatusCode, truncate(string(raw), 240))
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

// Stream implements Provider. The SSE framing is identical to what
// cmd/openai-shim reads from the upstream cloudaicompanion endpoint, so
// the same parsing rules apply: "data: {…}\n\n" deltas, terminating
// "data: [DONE]\n\n".
func (p *openAIShapeProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body := p.buildBody(req, true)
	resp, err := p.doPost(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, truncate(string(raw), 240))
	}
	out := make(chan StreamEvent, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		// 1 MiB max line — long completions can blow past the default 64 KiB.
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		var meta *StreamMeta
		// Partial tool calls keyed by their stream index: id/name arrive once,
		// arguments arrive in fragments. They are flushed as one event when the
		// provider reports a finish reason (cycle 274).
		var partial = map[int]*ToolCall{}
		flushToolCalls := func() {
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
			partial = map[int]*ToolCall{}
			if len(calls) == 0 {
				return
			}
			select {
			case <-ctx.Done():
				out <- StreamEvent{Error: ctx.Err(), Done: true}
			case out <- StreamEvent{ToolCalls: calls}:
			}
		}
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			var envelope struct {
				Choices []struct {
					Delta struct {
						Content   string                `json:"content"`
						ToolCalls []openAIToolCallDelta `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
				// Bad frame — emit as an error chunk and keep reading so a
				// single bad frame doesn't kill the whole stream.
				select {
				case <-ctx.Done():
					out <- StreamEvent{Error: ctx.Err(), Done: true}
					return
				case out <- StreamEvent{Error: fmt.Errorf("sse decode: %w", err), Done: true}:
				}
				return
			}
			if envelope.Usage != nil {
				meta = &StreamMeta{
					PromptTokens:     envelope.Usage.PromptTokens,
					CompletionTokens: envelope.Usage.CompletionTokens,
					TotalTokens:      envelope.Usage.TotalTokens,
				}
			}
			for _, c := range envelope.Choices {
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
						return
					case out <- StreamEvent{Text: c.Delta.Content}:
					}
				}
				// finish_reason "tool_calls" (or a provider that ends a tool
				// turn with "stop") is our cue that the arguments are complete.
				if c.FinishReason != "" {
					flushToolCalls()
				}
			}
		}
		if err := scanner.Err(); err != nil {
			out <- StreamEvent{Error: fmt.Errorf("sse read: %w", err), Done: true}
			return
		}
		// Providers that never send finish_reason still deserve their calls.
		flushToolCalls()
		out <- StreamEvent{Done: true, Meta: meta}
	}()
	return out, nil
}

// buildBody assembles the OpenAI request body. Stream flag is plumbed in
// so the same body builder serves both Chat and Stream.
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
		// Ask the provider to include a usage block on the final SSE frame
		// so we can forward token counts to the TUI / shim. OpenAI,
		// OpenRouter, and agw all honor this; ollama ignores it.
		body.StreamOptions = &openAIStreamOpts{IncludeUsage: true}
	}
	return body
}

// toolCallToWire converts the neutral ToolCall to the OpenAI message shape.
// An empty Arguments string is normalised to "{}" because some gateways
// reject a null arguments field on replay.
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

// toolCallsFromWire is the inverse of toolCallToWire.
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

// doPost is the shared HTTP POST. Headers are provider-specific:
//   - agw.k0s.io wants "Authorization: Bearer -" (the dev sentinel literally
//     is the string "-")
//   - openai / openrouter / custom want "Authorization: Bearer <key>"
//   - ollama doesn't want any auth header at all
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
		// No auth header. Ollama 0.x and 1.x both accept no-auth.
	case "agw":
		// Dev sentinel. The gateway ignores the value; we send "-" so logs
		// and packet captures show "this is the dev tenant" not "this is
		// a leaked real token".
		httpReq.Header.Set("Authorization", "Bearer "+p.model.APIKey)
	default:
		if key := strings.TrimSpace(p.model.APIKey); key != "" {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
	}
	if p.label == "openrouter" {
		// OpenRouter ranks apps by HTTP-Referer. We don't have one to send
		// honestly, so we send the GitHub repo as a courtesy.
		httpReq.Header.Set("HTTP-Referer", "https://github.com/justwasm/pigasus")
		httpReq.Header.Set("X-Title", "openagy (recovered)")
	}
	return p.httpClient.Do(httpReq)
}

// ---- Anthropic ----------------------------------------------------------

// anthropicProvider talks to /v1/messages. Two important differences from
// OpenAI: the path is /v1/messages (not /v1/chat/completions) and the auth
// header is x-api-key, not Authorization.
type anthropicProvider struct {
	model      Model
	httpClient *http.Client
}

// Name implements Provider.
func (p *anthropicProvider) Name() string { return "anthropic" }

// anthropicRequestBody is the wire shape for /v1/messages.
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
		return ChatResponse{Raw: raw}, fmt.Errorf("upstream %d: %s", resp.StatusCode, truncate(string(raw), 240))
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

// Stream implements Provider. Anthropic uses named SSE events
// (event: content_block_delta / message_stop) instead of OpenAI's "data: …".
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
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, truncate(string(raw), 240))
	}
	out := make(chan StreamEvent, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
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
				switch eventType {
				case "content_block_delta":
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
							return
						case out <- StreamEvent{Text: d.Delta.Text}:
						}
					}
				case "message_delta":
					var d struct {
						Usage struct {
							InputTokens  int `json:"input_tokens"`
							OutputTokens int `json:"output_tokens"`
						} `json:"usage"`
					}
					if err := json.Unmarshal([]byte(payload), &d); err == nil {
						meta = &StreamMeta{
							PromptTokens:     d.Usage.InputTokens,
							CompletionTokens: d.Usage.OutputTokens,
							TotalTokens:      d.Usage.InputTokens + d.Usage.OutputTokens,
						}
					}
				case "message_stop":
					out <- StreamEvent{Done: true, Meta: meta}
					return
				case "error":
					var e struct {
						Error struct {
							Message string `json:"message"`
						} `json:"error"`
					}
					_ = json.Unmarshal([]byte(payload), &e)
					out <- StreamEvent{Error: fmt.Errorf("anthropic: %s", e.Error.Message), Done: true}
					return
				}
				eventType = "" // reset for next frame
			}
		}
		if err := scanner.Err(); err != nil {
			out <- StreamEvent{Error: fmt.Errorf("sse read: %w", err), Done: true}
			return
		}
		out <- StreamEvent{Done: true, Meta: meta}
	}()
	return out, nil
}

func (p *anthropicProvider) buildBody(req ChatRequest, stream bool) anthropicRequestBody {
	// Anthropic requires non-empty messages and a max_tokens cap. If the
	// caller didn't set one, default to 1024 — better than the API rejecting
	// the call.
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	msgs := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		// Anthropic doesn't allow system in the messages array; it lives in
		// the top-level system field. The caller is expected to have hoisted
		// it, but we drop any straggler system messages to be safe.
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

// ---- Google (passthrough placeholder) -----------------------------------

// googleProvider is a placeholder. The cycle 33 spec calls for "passthrough
// (mock in sandbox)" — we keep the type so the registry's NewProvider switch
// is exhaustive, but the actual Gemini native client would need a
// non-trivial body builder (Gemini uses a completely different envelope).
// For now, Chat returns a friendly error that points users at the
// openai-compatible workaround.
type googleProvider struct {
	model      Model
	httpClient *http.Client
}

// Name implements Provider.
func (p *googleProvider) Name() string { return "google" }

// Chat implements Provider. The Gemini native client is not implemented in
// cycle 33; use the openai provider with an api_url pointing at a
// Gemini-compatible OpenAI proxy.
func (p *googleProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, errors.New(
		"google: Gemini native client is not implemented in this build. " +
			"Use provider=openai with api_url pointing at a Gemini-compatible OpenAI proxy " +
			"(e.g. https://generativelanguage.googleapis.com/v1beta/openai/chat/completions).")
}

// Stream implements Provider.
func (p *googleProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	out := make(chan StreamEvent, 1)
	out <- StreamEvent{
		Error: errors.New(
			"google: Gemini native client is not implemented in this build. " +
				"Use provider=openai with api_url pointing at a Gemini-compatible OpenAI proxy."),
		Done: true,
	}
	close(out)
	return out, nil
}

// ---- shared helpers -----------------------------------------------------

// truncate keeps error messages short enough to fit in a single log line.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
