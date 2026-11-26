package codec

import (
	"encoding/json"
	"strings"

	"github.com/justwasm/pigasus/geminicode"
)

// Media supplies the host's media-loading and MIME-detection capabilities.
// A zero value supports inline data URIs and explicitly typed base64 content
// without accessing the network, filesystem, or ambient configuration.
type Media struct {
	Resolve    func(uriOrPath string) (mime string, data []byte, err error)
	DetectMIME func(data []byte) string
}

func (m Media) detectMIME(data []byte) string {
	if m.DetectMIME != nil {
		return m.DetectMIME(data)
	}
	return "application/octet-stream"
}

// OpenAIChatRequest is the Chat Completions request shape handled by the proxy.
type OpenAIChatRequest struct {
	Model           string          `json:"model"`
	Messages        []OpenAIMessage `json:"messages"`
	Tools           []OpenAITool    `json:"tools,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
}

// OpenAIMessage is one Chat Completions message.
type OpenAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// OpenAITool is a Chat Completions function tool.
type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFuncDecl `json:"function"`
}

// OpenAIFuncDecl describes a function offered to the model.
type OpenAIFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// OpenAIToolCall is a function call emitted by an assistant message.
type OpenAIToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function OpenAIFuncCall `json:"function"`
}

// OpenAIFuncCall carries the emitted function name and JSON arguments.
type OpenAIFuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// AnthropicRequest is the Messages API request shape handled by the proxy.
type AnthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []AnthropicMessage `json:"messages"`
	System      any                `json:"system,omitempty"`
	Tools       []AnthropicTool    `json:"tools,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
}

// AnthropicMessage is one Messages API input message.
type AnthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// AnthropicTool is a Messages API tool declaration.
type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// OpenAIToTurn converts a decoded Chat Completions request into a CloudCode
// turn. Session and request IDs are supplied by the transport host. The system
// prompt is returned verbatim; cloaking is the proxy host's responsibility.
func OpenAIToTurn(req OpenAIChatRequest, media Media, sessionID, requestID string) geminicode.Turn {
	canonicalModel := NormalizeModelName(req.Model)
	toolCallNames := make(map[string]string)
	var systemPromptParts []string
	var turnMessages []geminicode.TurnMessage
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		text, images := ParseOpenAIContent(m.Content, media)
		if role == "system" {
			if text != "" {
				systemPromptParts = append(systemPromptParts, text)
			}
			continue
		}
		if role == "assistant" {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" && tc.Function.Name != "" {
					toolCallNames[tc.ID] = tc.Function.Name
				}
			}
		}
		name := m.Name
		if role == "tool" {
			if expected, ok := toolCallNames[m.ToolCallID]; ok && expected != "" {
				name = expected
			}
		}
		msg := geminicode.TurnMessage{Role: role, Content: text, Images: images, Name: name}
		if role == "tool" {
			msg.ToolCallID = m.ToolCallID
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, geminicode.TurnToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: json.RawMessage(tc.Function.Arguments)})
		}
		turnMessages = append(turnMessages, msg)
	}
	var turnTools []geminicode.TurnTool
	for _, tool := range req.Tools {
		if tool.Type != "" && tool.Type != "function" {
			continue
		}
		turnTools = append(turnTools, geminicode.TurnTool{Name: tool.Function.Name, Description: tool.Function.Description, Parameters: tool.Function.Parameters})
	}
	temp := 0.0
	if req.Temperature != nil {
		temp = *req.Temperature
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	thinkingLevel := "high"
	if req.ReasoningEffort != "" {
		thinkingLevel = req.ReasoningEffort
	}
	switch strings.ToLower(thinkingLevel) {
	case "low", "medium", "high", "none":
		thinkingLevel = strings.ToLower(thinkingLevel)
	default:
		thinkingLevel = "high"
	}
	return geminicode.Turn{
		Model: canonicalModel, Project: geminicode.DefaultProject,
		SystemPrompt:    strings.Join(systemPromptParts, "\n\n"),
		Messages:        turnMessages, Tools: turnTools, Temperature: temp,
		MaxOutputTokens: maxTokens, ThinkingLevel: thinkingLevel,
		SessionID: sessionID, RequestID: requestID,
	}
}

// AnthropicToTurn converts a decoded Messages API request into a CloudCode turn.
// The system prompt is returned verbatim; cloaking is the host's responsibility.
func AnthropicToTurn(req AnthropicRequest, media Media, sessionID, requestID string) geminicode.Turn {
	var tools []geminicode.TurnTool
	for _, tool := range req.Tools {
		tools = append(tools, geminicode.TurnTool{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema})
	}
	temp := 0.0
	if req.Temperature != nil {
		temp = *req.Temperature
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	return geminicode.Turn{
		Model: NormalizeModelName(req.Model), Project: geminicode.DefaultProject,
		SystemPrompt:    ParseAnthropicSystem(req.System),
		Messages:        ParseAnthropicMessages(req.Messages, media), Tools: tools,
		Temperature: temp, MaxOutputTokens: maxTokens,
		SessionID: sessionID, RequestID: requestID,
	}
}