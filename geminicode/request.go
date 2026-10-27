package geminicode

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Turn is one model turn, in the dialect's own terms.
//
// The package carries its own turn types rather than the application's
// neutral ones: a dialect library that depends on its host cannot be
// reused. internal/backend converts between the two.
type Turn struct {
	// Model is the wire model id — "gemini-3.8-flash-high",
	// "claude-sonnet-4-6".
	Model string
	// Project is the consumer project loadCodeAssist reports
	// ("aicode-consumers").
	Project string
	// SystemPrompt is prepended as a system instruction.
	SystemPrompt string
	// Messages are the conversation so far, in order.
	Messages []TurnMessage
	// Tools is the advertised tool set.
	Tools []TurnTool
	// Temperature is the sampling temperature. Zero means "no opinion" and
	// the member is omitted from the request.
	Temperature float64
	// MaxOutputTokens and ThinkingBudget override the per-model defaults.
	MaxOutputTokens int
	ThinkingBudget  int
	// SessionID is the reference's sessionId member. Zero picks a random
	// one.
	SessionID string
	// RequestID is the reference's requestId member. Empty picks a random
	// one; set it to keep the id stable across a retry.
	RequestID string
	// Signatures maps a tool call id to the thoughtSignature the model
	// returned with that call. The reference replays the signature on the
	// functionCall part of the next turn — Gemini 3 rejects the turn
	// without it.
	Signatures map[string]string
}

// TurnMessage is one round of a conversation.
//
// Role mirrors OpenAI/Anthropic roles: "system", "user", "assistant" and
// "tool" for a tool result. A tool result carries ToolCallID (matching the
// originating call) and Name.
type TurnMessage struct {
	Role       string
	Content    string
	ToolCalls  []TurnToolCall
	ToolCallID string
	Name       string
	// Images are the pictures the user attached. Only a user message
	// carries them.
	Images []TurnImage
	// IsError marks a tool result that carries the reference's *failure*
	// text. The transport reads it to pick the envelope: a call that
	// failed travels back under the `error` member, a call that succeeded
	// under `output`.
	IsError bool
}

// TurnToolCall is a model-initiated tool invocation. Arguments is the raw
// JSON the model produced, not yet validated against the tool's schema.
type TurnToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// TurnTool is a tool the model is allowed to call.
type TurnTool struct {
	Name        string
	Description string
	// Parameters is a provider-neutral JSON Schema. Schema converts it to
	// the Gemini spelling.
	Parameters json.RawMessage
}

// TurnImage is a picture the user attached.
type TurnImage struct {
	MIMEType string
	Data     []byte
}

// BuildRequest renders one turn into the request body `agy` sends.
//
// The result is the wrapped envelope: the inner Gemini request under a
// `request` member, with the envelope's own members around it.
func BuildRequest(t Turn) GenerateContentRequest {
	contents, system := buildContents(t.Messages, t.SystemPrompt, t.Signatures)
	inner := GenerateContentRequest{Contents: contents}
	if system != "" {
		inner.SystemInstruction = &SystemInstruction{Role: "user", Parts: []Part{{Text: system}}}
	}
	if decls := BuildTools(t.Tools); len(decls) > 0 {
		inner.Tools = []Tool{{FunctionDeclarations: decls}}
	}
	maxOut, budget := Limits(t.Model, t.MaxOutputTokens, t.ThinkingBudget)
	temp := t.Temperature
	inner.GenerationConfig = &GenerationConfig{
		MaxOutputTokens: maxOut,
		ThinkingConfig:  &ThinkingConfig{IncludeThoughts: true, ThinkingBudget: budget},
	}
	if temp != 0 {
		inner.GenerationConfig.Temperature = &temp
	}
	inner.Labels = ModelLabels(t.Model)
	inner.SessionID = t.SessionID

	env := GenerateContentRequest{
		Project:     t.Project,
		RequestID:   t.RequestID,
		Request:     &inner,
		Model:       t.Model,
		UserAgent:   "antigravity",
		RequestType: "agent",
	}
	if env.RequestID == "" {
		env.RequestID = NewRequestID()
	}
	return env
}

// BuildTools turns the provider-neutral catalogue into the CloudCode
// function declarations, dropping blanks and duplicates in first-seen order.
//
// Gemini rejects the whole request for a duplicate function declaration
// ("Duplicate function declaration found: …"); the gateway tolerated the
// clone's overlapping catalog.
func BuildTools(tools []TurnTool) []FunctionDeclaration {
	decls := make([]FunctionDeclaration, 0, len(tools))
	seen := make(map[string]bool, len(tools))
	for _, t := range tools {
		if strings.TrimSpace(t.Name) == "" || seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		decl := FunctionDeclaration{Name: t.Name, Description: t.Description}
		if len(t.Parameters) > 0 {
			decl.Parameters = Schema(t.Parameters)
		}
		decls = append(decls, decl)
	}
	return decls
}

// ToolsJSON renders the tool declarations exactly as BuildRequest sends
// them, so a caller that reports the size of the tool surface (the
// reference's own /context panel does) can measure the wire and not an
// approximation over the untransformed catalogue.
func ToolsJSON(tools []TurnTool) []byte {
	decls := BuildTools(tools)
	if len(decls) == 0 {
		return nil
	}
	b, err := json.Marshal([]Tool{{FunctionDeclarations: decls}})
	if err != nil {
		return nil
	}
	return b
}

// buildContents renders the conversation into the inner request's contents
// and returns the system prompt separately — the reference carries the
// system instruction as its own member, not as a leading content.
func buildContents(messages []TurnMessage, initialSystem string, signatures map[string]string) ([]Content, string) {
	system := strings.TrimSpace(initialSystem)
	var contents []Content
	for _, m := range messages {
		switch strings.ToLower(m.Role) {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				if system != "" {
					system += "\n\n"
				}
				system += m.Content
			}
		case "user":
			if parts := buildUserParts(m); len(parts) > 0 {
				contents = append(contents, Content{Role: "user", Parts: parts})
			}
		case "assistant":
			if part := buildAssistantContent(m, signatures); len(part.Parts) > 0 {
				contents = append(contents, part)
			}
		case "tool":
			contents = append(contents, Content{
				Role: "model",
				Parts: []Part{{
					FunctionResponse: &FunctionResponse{
						ID:       m.ToolCallID,
						Name:     m.Name,
						Response: toolResponse(m),
					},
				}},
			})
		}
	}
	return contents, system
}

// toolResponse picks the envelope a tool's answer travels in.
//
// 【一手】 agy 1.4.8: a call that failed is sent back under `error`, a
// call that succeeded under `output`. The body is the same string in
// both cases — the reference's failure text carries the two stamp lines
// and the "Encountered error in tool execution: …" sentence:
//
//	"response": {"error": "Created At: …\nCompleted At: …\nEncountered error in tool execution: file does not exist"}
//
// Before 1.4.8 the reference sent both under `output`, and its release
// notes call the change out: "failed tool calls … were being passed back
// to the agent as ordinary results; the agent now sees them as errors and
// can recover on its next step".
func toolResponse(m TurnMessage) map[string]any {
	if m.IsError {
		return map[string]any{"error": m.Content}
	}
	return map[string]any{"output": m.Content}
}

// buildUserParts renders one user message: its text first, then the
// images the user attached.
//
// A user message with no text is still a part — an empty text part —
// unless it carries tool calls: the reference sends the text part even
// when the text is empty, and it skips the message altogether when the
// only thing it holds is a tool call.
func buildUserParts(m TurnMessage) []Part {
	parts := make([]Part, 0, len(m.Images)+1)
	if m.Content != "" || len(m.ToolCalls) == 0 {
		parts = append(parts, Part{Text: m.Content})
	}
	for _, img := range m.Images {
		if len(img.Data) == 0 {
			continue
		}
		parts = append(parts, Part{InlineData: &InlineData{
			MIMEType: img.MIMEType,
			Data:     base64.StdEncoding.EncodeToString(img.Data),
		}})
	}
	return parts
}

// buildAssistantContent renders one assistant message: its text, then the
// tool calls it asked for.
//
// The reference replays the model's thoughtSignature on the functionCall
// part. The signature is keyed by call id, which survives the trip through
// the caller's own message type, so the caller passes the map it collected
// from the previous turn.
func buildAssistantContent(m TurnMessage, signatures map[string]string) Content {
	parts := make([]Part, 0, len(m.ToolCalls)+1)
	if m.Content != "" {
		parts = append(parts, Part{Text: m.Content})
	}
	for _, tc := range m.ToolCalls {
		parts = append(parts, callPart(tc, signatures))
	}
	return Content{Role: "model", Parts: parts}
}

// callPart builds a functionCall part and replays the thoughtSignature
// Gemini 3 returned with it.
func callPart(tc TurnToolCall, signatures map[string]string) Part {
	args := json.RawMessage(tc.Arguments)
	if len(args) == 0 || !json.Valid(args) {
		args = json.RawMessage(`{}`)
	}
	part := Part{FunctionCall: &FunctionCall{ID: tc.ID, Name: tc.Name, Args: args}}
	if signatures != nil {
		part.ThoughtSignature = signatures[tc.ID]
	}
	return part
}

// ModelLabels returns the labels the reference attaches to a request.
//
// A Claude model is marked with three members, any other non-Gemini model
// with one. A Gemini model carries no labels at all.
func ModelLabels(model string) map[string]string {
	if IsClaudeModel(model) {
		return map[string]string{
			"used_claude":              "true",
			"used_claude_conservative": "true",
			"used_non_gemini_model":    "true",
		}
	}
	if !strings.HasPrefix(model, "gemini") {
		return map[string]string{"used_non_gemini_model": "true"}
	}
	return nil
}

// Limits returns the output-token and thinking budgets for a model.
//
// The Claude budgets are the reference's own; a non-zero override wins over
// both.
func Limits(model string, maxOutputTokens, thinkingBudget int) (maxOut, budget int) {
	if IsClaudeModel(model) {
		maxOut, budget = 64000, 1024
	} else {
		maxOut, budget = 65536, 1000
	}
	if maxOutputTokens > 0 {
		maxOut = maxOutputTokens
	}
	if thinkingBudget > 0 {
		budget = thinkingBudget
	}
	return maxOut, budget
}

// IsClaudeModel reports whether a wire model id names a Claude model.
func IsClaudeModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "claude")
}

// NewRequestID returns a request id in the reference's own shape.
func NewRequestID() string {
	return fmt.Sprintf("agent/%s/%d/%s/1", randomHex(16), time.Now().UnixMilli(), randomHex(16))
}

// NewSessionID returns a session id in the reference's own shape: a
// negative random 64-bit number.
func NewSessionID() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return -1
	}
	id := int64(binary.LittleEndian.Uint64(b[:]))
	if id > 0 {
		id = -id
	}
	if id == 0 {
		id = -1
	}
	return id
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}
