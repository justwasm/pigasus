// Package backend — gateway_request.go: request construction for the
// gateway transport dialects.
package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// gatewayRequestBody is the OpenAI-compatible shape we POST.
type gatewayRequestBody struct {
	Model           string             `json:"model"`
	Messages        []gatewayMessage   `json:"messages"`
	Stream          bool               `json:"stream,omitempty"`
	MaxTokens       int                `json:"max_tokens,omitempty"`
	Temperature     float64            `json:"temperature,omitempty"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
	StreamOptions   *gatewayStreamOpts `json:"stream_options,omitempty"`
	Tools           []gatewayTool      `json:"tools,omitempty"`
}

type gatewayStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type gatewayTool struct {
	Type     string             `json:"type"` // always "function"
	Function gatewayToolFnShape `json:"function"`
}

type gatewayToolFnShape struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type gatewayMessage struct {
	Role string `json:"role"`
	// Content is the message's text — or, for a message that carries an
	// image, the content *array* the OpenAI dialect spells its parts
	// with. The key is the same either way; only the JSON type changes.
	Content    any               `json:"content"`
	ToolCalls  []gatewayToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

// gatewayContentPart is one part of a message's content array.
//
// 【一手】 the shape is the OpenAI Chat Completions one, and it is the
// shape PiG's own OpenAI dialect writes (ai/openai.go): a text part and
// an image part whose url is a `data:` URL carrying the base64 payload.
type gatewayContentPart struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *gatewayImageURL `json:"image_url,omitempty"`
}

// gatewayImageURL is the image part's payload.
type gatewayImageURL struct {
	URL string `json:"url"`
}

type gatewayToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// buildRequest renders the wire-neutral request in the configured protocol.
func (c *GatewayClient) buildRequest(req StreamRequest, externalModel string) ([]byte, error) {
	if c.cfg.protocol() == GatewayProtocolAnthropic {
		return c.buildAnthropicRequest(req, externalModel)
	}
	return c.buildOpenAIRequest(req, externalModel)
}

// buildOpenAIRequest renders the Chat Completions body.
func (c *GatewayClient) buildOpenAIRequest(req StreamRequest, externalModel string) ([]byte, error) {
	body := gatewayRequestBody{
		Model:       externalModel,
		Messages:    formatOpenAIMessages(req),
		Stream:      true,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		// The reasoning level the model runs at — the tier of a model
		// that declares tiers. It rides the model the client was built
		// for, not the turn: the turn's own level is PiG's business.
		ReasoningEffort: strings.TrimSpace(c.cfg.ReasoningEffort),
		StreamOptions: &gatewayStreamOpts{
			IncludeUsage: true,
		},
		Tools: formatOpenAITools(req),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("gateway: marshal: %w", err)
	}
	return raw, nil
}

func formatOpenAIMessages(req StreamRequest) []gatewayMessage {
	msgs := make([]gatewayMessage, 0, len(req.Messages)+1)
	if sp := strings.TrimSpace(req.SystemPrompt); sp != "" {
		msgs = append(msgs, gatewayMessage{Role: "system", Content: sp})
	}
	for _, m := range req.Messages {
		am := gatewayMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if parts := formatOpenAIContentParts(m); parts != nil {
			am.Content = parts
		}
		for _, tc := range m.ToolCalls {
			am.ToolCalls = append(am.ToolCalls, gatewayToolCallFrom(tc))
		}
		msgs = append(msgs, am)
	}
	return msgs
}

// formatOpenAIContentParts renders the content array of a message that
// carries an image, and nil for every other message.
//
// The text comes first and the image after it; a message with no text
// carries the image alone, which is what PiG's own dialect does (it
// drops an empty text part so an image-only message stays valid).
func formatOpenAIContentParts(m Message) []gatewayContentPart {
	if len(m.Images) == 0 {
		return nil
	}
	parts := make([]gatewayContentPart, 0, len(m.Images)+1)
	if m.Content != "" {
		parts = append(parts, gatewayContentPart{Type: "text", Text: m.Content})
	}
	for _, img := range m.Images {
		if len(img.Data) == 0 {
			continue
		}
		parts = append(parts, gatewayContentPart{
			Type: "image_url",
			ImageURL: &gatewayImageURL{URL: "data:" + img.MIMEType +
				";base64," + base64.StdEncoding.EncodeToString(img.Data)},
		})
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func formatOpenAITools(req StreamRequest) []gatewayTool {
	var tools []gatewayTool
	for _, t := range req.Tools {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		tools = append(tools, gatewayTool{
			Type: "function",
			Function: gatewayToolFnShape{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return tools
}

// applyProtocolHeaders sets the credential and version headers the
// configured protocol expects.
func (c *GatewayClient) applyProtocolHeaders(req *http.Request) {
	if c.cfg.protocol() == GatewayProtocolAnthropic {
		if c.apiKey != "" {
			req.Header.Set("x-api-key", c.apiKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
		return
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// gatewayToolCallFrom converts a wire-neutral ToolCall into the OpenAI shape.
func gatewayToolCallFrom(tc ToolCall) gatewayToolCall {
	out := gatewayToolCall{ID: tc.ID, Type: "function"}
	out.Function.Name = tc.Name
	args := string(tc.Arguments)
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	out.Function.Arguments = args
	return out
}
