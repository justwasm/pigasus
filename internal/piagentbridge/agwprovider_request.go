package piagentbridge

import (
	"encoding/json"
	"fmt"
	"strings"

	pi "github.com/MichaelKinsy/PiG/ai"
)

// ─── request shape ───────────────────────────────────────────────────────────
//
// chatRequest and friends mirror the OpenAI Chat-Completions body the agw
// gateway accepts. The struct tags here are the contract; buildChatRequest
// is the only writer; the gateway is the only reader.

type chatRequest struct {
	Model         string         `json:"model"`
	Messages      []chatMessage  `json:"messages"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	Tools         []chatTool     `json:"tools,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatTool struct {
	Type     string          `json:"type"`
	Function chatToolFnShape `json:"function"`
}

type chatToolFnShape struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

type chatToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// buildChatRequest converts a PiG transcript into the agw-shaped JSON body.
// It is the request-side mirror of streamState.consume; the response-side
// decoder and the request-side builder must agree on the wire format, which
// is why both halves live in this package.
func buildChatRequest(model string, transcript pi.TranscriptContext, opts pi.StreamOptions) ([]byte, error) {
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("agw: model is required")
	}
	request := newChatRequest(model, opts)
	appendTranscriptToChat(&request, transcript)
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("agw: transcript has no messages")
	}
	return json.Marshal(request)
}

// newChatRequest fills the transcript-independent request fields: the model
// id, the streaming flags, max-tokens, and the temperature (when set).
//
// Temperature is sent only when non-zero OR TemperatureSet: PiG's
// StreamOptions.Temperature is 0 both for "unset" and for "the user
// explicitly asked for 0.0", and the agw gateway rounds 0 differently
// from "not present".
func newChatRequest(model string, opts pi.StreamOptions) chatRequest {
	request := chatRequest{
		Model:         model,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		MaxTokens:     opts.MaxTokens,
	}
	if opts.Temperature != 0 || opts.TemperatureSet {
		temperature := opts.Temperature
		request.Temperature = &temperature
	}
	return request
}

// appendTranscriptToChat walks the transcript message-by-message, mirroring
// stream.go's ToStreamRequest but emitting the agw-shaped chatMessage /
// chatTool structures. Image blocks and thinking blocks have no agw
// counterpart and are dropped, which matches backend.AgwK0sClient.
func appendTranscriptToChat(request *chatRequest, transcript pi.TranscriptContext) {
	for _, message := range transcript.Messages() {
		switch m := message.(type) {
		case pi.SystemMessage:
			if text := systemText(m); text != "" {
				request.Messages = append(request.Messages, chatMessage{Role: "system", Content: text})
			}
			appendToolDeclsToChat(request, m.ToolsAdded)
		case pi.UserMessage:
			request.Messages = append(request.Messages, chatMessage{Role: "user", Content: userText(m)})
		case pi.AssistantMessage:
			request.Messages = append(request.Messages, buildChatAssistantMessage(m))
		case pi.ToolResultMessage:
			request.Messages = append(request.Messages, chatMessage{
				Role:       "tool",
				Content:    toolResultText(m),
				ToolCallID: m.ToolCallID,
				Name:       m.ToolName,
			})
		}
	}
}

// appendToolDeclsToChat converts PiG tool schemas into the agw tool shape.
// A failed marshal or an empty parameter set yields the canonical
// `{"type":"object","properties":{}}` stub — the gateway accepts the tool
// as advertised but treats all calls as parameterless.
func appendToolDeclsToChat(request *chatRequest, decls []pi.ToolSchema) {
	for _, decl := range decls {
		parameters, err := json.Marshal(decl.Parameters)
		if err != nil || len(decl.Parameters) == 0 {
			parameters = []byte(`{"type":"object","properties":{}}`)
		}
		request.Tools = append(request.Tools, chatTool{
			Type: "function",
			Function: chatToolFnShape{
				Name:        decl.Name,
				Description: decl.Description,
				Parameters:  parameters,
			},
		})
	}
}

// buildChatAssistantMessage converts one PiG assistant turn into the agw
// assistant message: the joined text plus the tool-call fragments the turn
// produced. Image blocks and thinking blocks drop on the floor (no agw
// representation); the helper is also the only place outside
// agwprovider.go's translate that knows the tool-call argument-shape order.
func buildChatAssistantMessage(m pi.AssistantMessage) chatMessage {
	out := chatMessage{Role: "assistant", Content: assistantText(m.Content)}
	for _, block := range m.Content {
		call, ok := block.(pi.ToolCall)
		if !ok {
			continue
		}
		arguments, err := json.Marshal(call.Arguments)
		if err != nil {
			arguments = []byte("{}")
		}
		entry := chatToolCall{ID: call.ID, Type: "function"}
		entry.Function.Name = call.Name
		entry.Function.Arguments = string(arguments)
		out.ToolCalls = append(out.ToolCalls, entry)
	}
	return out
}
