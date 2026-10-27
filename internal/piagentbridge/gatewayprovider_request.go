package piagentbridge

import (
	"encoding/json"
	"fmt"
	"strings"

	pi "github.com/MichaelKinsy/PiG/ai"
)

// ─── request shape ───────────────────────────────────────────────────────────
//
// chatRequest and friends mirror the OpenAI Chat-Completions body the gateway
// gateway accepts. The struct tags here are the contract; buildChatRequest
// is the only writer; the gateway is the only reader.

type chatRequest struct {
	Model         string         `json:"model"`
	Messages      []chatMessage  `json:"messages"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	// ReasoningEffort is the reasoning level the model runs at — the
	// tier of a model that declares tiers. It is absent when the model
	// says nothing: the endpoint then picks its own default.
	ReasoningEffort string     `json:"reasoning_effort,omitempty"`
	Tools           []chatTool `json:"tools,omitempty"`
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
	Role string `json:"role"`
	// Content is the message's text — or, for a message that carries
	// an image, the content *array* the OpenAI dialect spells its
	// parts with. The key is the same either way; only the JSON type
	// changes.
	Content    any            `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

// chatContentPart is one part of a message's content array.
//
// 【一手】 the shape is the OpenAI Chat Completions one, and it is the
// shape PiG's own OpenAI dialect writes (ai/openai.go): a text part and
// an image part whose url is a `data:` URL carrying the base64
// payload.
type chatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *chatImageURL `json:"image_url,omitempty"`
}

// chatImageURL is the image part's payload.
type chatImageURL struct {
	URL string `json:"url"`
}

type chatToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// buildChatRequest converts a PiG transcript into the gateway-shaped JSON body.
// It is the request-side mirror of streamState.consume; the response-side
// decoder and the request-side builder must agree on the wire format, which
// is why both halves live in this package.
func buildChatRequest(model, effort string, transcript pi.TranscriptContext, opts pi.StreamOptions) ([]byte, error) {
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("gateway: model is required")
	}
	request := newChatRequest(model, effort, opts)
	appendTranscriptToChat(&request, transcript)
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("gateway: transcript has no messages")
	}
	return json.Marshal(request)
}

// newChatRequest fills the transcript-independent request fields: the model
// id, the streaming flags, max-tokens, the temperature (when set) and the
// reasoning level (when the model has one).
//
// Temperature is sent only when non-zero OR TemperatureSet: PiG's
// StreamOptions.Temperature is 0 both for "unset" and for "the user
// explicitly asked for 0.0", and the gateway rounds 0 differently
// from "not present".
func newChatRequest(model, effort string, opts pi.StreamOptions) chatRequest {
	request := chatRequest{
		Model:           model,
		Stream:          true,
		StreamOptions:   &streamOptions{IncludeUsage: true},
		MaxTokens:       opts.MaxTokens,
		ReasoningEffort: strings.TrimSpace(effort),
	}
	if opts.Temperature != 0 || opts.TemperatureSet {
		temperature := opts.Temperature
		request.Temperature = &temperature
	}
	return request
}

// appendTranscriptToChat walks the transcript message-by-message, mirroring
// stream.go's ToStreamRequest but emitting the gateway-shaped chatMessage /
// chatTool structures. Thinking blocks have no gateway counterpart and are
// dropped; image blocks are not — they ride along in the user message's
// content array, the way the OpenAI dialect spells a picture.
func appendTranscriptToChat(request *chatRequest, transcript pi.TranscriptContext) {
	for _, message := range transcript.Messages() {
		switch m := message.(type) {
		case pi.SystemMessage:
			if text := systemText(m); text != "" {
				request.Messages = append(request.Messages, chatMessage{Role: "system", Content: text})
			}
			appendToolDeclsToChat(request, m.ToolsAdded)
		case pi.UserMessage:
			request.Messages = append(request.Messages, chatMessage{Role: "user", Content: userContent(m)})
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

// userContent renders a user message's content: the plain text for a
// message with no picture, and the content array for a message that
// carries one.
//
// A text-only message keeps the string form, so the request the clone
// sends today is the request the gateway sees today.
func userContent(m pi.UserMessage) any {
	parts := userContentParts(m)
	if len(parts) == 0 {
		return userText(m)
	}
	return parts
}

// userContentParts renders a user message's blocks as the OpenAI
// content array: the text first, the pictures after it.
//
// 【一手】 the shape is the OpenAI Chat Completions one, and it is the
// shape PiG's own OpenAI dialect writes (ai/openai.go) — an empty text
// part is dropped so an image-only message stays valid, and an image
// part's url is a `data:` URL carrying the base64 payload.
//
// The clone's own transport writes the same array
// (backend.formatOpenAIContentParts), so a picture reaches a gateway
// the same way whichever transport carries it.
func userContentParts(m pi.UserMessage) []chatContentPart {
	content, ok := m.Content.(pi.UserContentBlocks)
	if !ok {
		return nil
	}
	var images []chatContentPart
	for _, block := range content {
		image, ok := block.(pi.ImageContent)
		if !ok || image.Data == "" {
			continue
		}
		images = append(images, chatContentPart{
			Type:     "image_url",
			ImageURL: &chatImageURL{URL: "data:" + image.MimeType + ";base64," + image.Data},
		})
	}
	if len(images) == 0 {
		return nil
	}
	parts := make([]chatContentPart, 0, len(images)+1)
	if text := userText(m); text != "" {
		parts = append(parts, chatContentPart{Type: "text", Text: text})
	}
	return append(parts, images...)
}

// appendToolDeclsToChat converts PiG tool schemas into the gateway tool shape.
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

// buildChatAssistantMessage converts one PiG assistant turn into the gateway
// assistant message: the joined text plus the tool-call fragments the turn
// produced. Image blocks and thinking blocks drop on the floor (no gateway
// representation); the helper is also the only place outside
// gatewayprovider.go's translate that knows the tool-call argument-shape order.
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
