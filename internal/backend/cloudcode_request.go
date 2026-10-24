package backend

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ccEnvelope struct {
	Project     string    `json:"project"`
	RequestID   string    `json:"requestId"`
	Request     ccRequest `json:"request"`
	Model       string    `json:"model"`
	UserAgent   string    `json:"userAgent"`
	RequestType string    `json:"requestType"`
}

type ccRequest struct {
	Contents          []ccContent       `json:"contents"`
	SystemInstruction *ccContent        `json:"systemInstruction,omitempty"`
	Tools             []ccTool          `json:"tools,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	GenerationConfig  *ccGenConfig      `json:"generationConfig,omitempty"`
	SessionID         string            `json:"sessionId,omitempty"`
}

type ccContent struct {
	Role  string   `json:"role"`
	Parts []ccPart `json:"parts"`
}

// ccPart is both the request and response part shape. Omitempty keeps the
// emitted JSON to exactly the keys the reference sends.
type ccPart struct {
	Text             string           `json:"text,omitempty"`
	Thought          bool             `json:"thought,omitempty"`
	ThoughtSignature string           `json:"thoughtSignature,omitempty"`
	FunctionCall     *ccFunctionCall  `json:"functionCall,omitempty"`
	FunctionResponse *ccFunctionReply `json:"functionResponse,omitempty"`
}

type ccFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type ccFunctionReply struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type ccTool struct {
	FunctionDeclarations []ccFunctionDecl `json:"functionDeclarations"`
}

type ccFunctionDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ccGenConfig struct {
	MaxOutputTokens int               `json:"maxOutputTokens,omitempty"`
	Temperature     float64           `json:"temperature,omitempty"`
	ThinkingConfig  *ccThinkingConfig `json:"thinkingConfig,omitempty"`
}

type ccThinkingConfig struct {
	IncludeThoughts bool `json:"includeThoughts"`
	ThinkingBudget  int  `json:"thinkingBudget,omitempty"`
}

// buildToolDecls turns the provider-neutral catalogue into the CloudCode
// function declarations, dropping blanks and duplicates in first-seen order.
// Gemini rejects the whole request for a duplicate function declaration
// ("Duplicate function declaration found: …"); the gateway tolerated the
// clone's overlapping catalog.
func buildToolDecls(tools []ToolDecl) []ccFunctionDecl {
	decls := make([]ccFunctionDecl, 0, len(tools))
	seen := make(map[string]bool, len(tools))
	for _, t := range tools {
		if strings.TrimSpace(t.Name) == "" || seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		decl := ccFunctionDecl{Name: t.Name, Description: t.Description}
		if len(t.Parameters) > 0 {
			decl.Parameters = geminiToolSchema(t.Parameters)
		}
		decls = append(decls, decl)
	}
	return decls
}

// WireToolsJSON renders the tool declarations exactly as buildEnvelope sends
// them, so /context's "System tools" row can be len(tools JSON)/3 rather than
// an approximation over the untransformed catalogue (cycle 292).
func WireToolsJSON(tools []ToolDecl) []byte {
	decls := buildToolDecls(tools)
	if len(decls) == 0 {
		return nil
	}
	b, err := json.Marshal([]ccTool{{FunctionDeclarations: decls}})
	if err != nil {
		return nil
	}
	return b
}

func (c *CloudCodeClient) buildEnvelope(req StreamRequest, model string) ccEnvelope {
	contents, system := c.buildContents(req.Messages, req.SystemPrompt)
	inner := ccRequest{Contents: contents}
	if system != "" {
		inner.SystemInstruction = &ccContent{Role: "user", Parts: []ccPart{{Text: system}}}
	}
	if decls := buildToolDecls(req.Tools); len(decls) > 0 {
		inner.Tools = []ccTool{{FunctionDeclarations: decls}}
	}
	maxOut, budget := c.limits(model)
	inner.GenerationConfig = &ccGenConfig{
		MaxOutputTokens: maxOut,
		Temperature:     req.Temperature,
		ThinkingConfig:  &ccThinkingConfig{IncludeThoughts: true, ThinkingBudget: budget},
	}
	inner.Labels = buildModelLabels(model)
	inner.SessionID = fmt.Sprintf("%d", c.sessionID)
	return ccEnvelope{
		Project:     c.cfg.Project,
		RequestID:   newCloudCodeRequestID(),
		Request:     inner,
		Model:       model,
		UserAgent:   "antigravity",
		RequestType: "agent",
	}
}

func (c *CloudCodeClient) buildContents(messages []Message, initialSystem string) ([]ccContent, string) {
	system := strings.TrimSpace(initialSystem)
	var contents []ccContent
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
			if m.Content != "" || len(m.ToolCalls) == 0 {
				contents = append(contents, ccContent{
					Role:  "user",
					Parts: []ccPart{{Text: m.Content}},
				})
			}
		case "assistant":
			if part := c.buildAssistantContent(m); len(part.Parts) > 0 {
				contents = append(contents, part)
			}
		case "tool":
			contents = append(contents, ccContent{
				Role: "model",
				Parts: []ccPart{{
					FunctionResponse: &ccFunctionReply{
						ID:       m.ToolCallID,
						Name:     m.Name,
						Response: map[string]any{"output": m.Content},
					},
				}},
			})
		}
	}
	return contents, system
}

func (c *CloudCodeClient) buildAssistantContent(m Message) ccContent {
	parts := make([]ccPart, 0, len(m.ToolCalls)+1)
	if m.Content != "" {
		parts = append(parts, ccPart{Text: m.Content})
	}
	for _, tc := range m.ToolCalls {
		parts = append(parts, c.callPart(tc))
	}
	return ccContent{Role: "model", Parts: parts}
}

func buildModelLabels(model string) map[string]string {
	if isClaudeModel(model) {
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

// callPart builds a functionCall part and replays the thoughtSignature Gemini 3
// returned with it. The signature is keyed by call id, which survives the trip
// through backend.Message.
func (c *CloudCodeClient) callPart(tc ToolCall) ccPart {
	args := json.RawMessage(tc.Arguments)
	if len(args) == 0 || !json.Valid(args) {
		args = json.RawMessage(`{}`)
	}
	part := ccPart{FunctionCall: &ccFunctionCall{ID: tc.ID, Name: tc.Name, Args: args}}
	c.mu.Lock()
	part.ThoughtSignature = c.signatures[tc.ID]
	c.mu.Unlock()
	return part
}

func (c *CloudCodeClient) limits(model string) (maxOut, budget int) {
	if isClaudeModel(model) {
		maxOut, budget = 64000, 1024
	} else {
		maxOut, budget = 65536, 1000
	}
	if c.cfg.MaxOutputTokens > 0 {
		maxOut = c.cfg.MaxOutputTokens
	}
	if c.cfg.ThinkingBudget > 0 {
		budget = c.cfg.ThinkingBudget
	}
	return maxOut, budget
}

func isClaudeModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "claude")
}

func newCloudCodeRequestID() string {
	return fmt.Sprintf("agent/%s/%d/%s/1", randomHex(16), time.Now().UnixMilli(), randomHex(16))
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}

func randomSessionID() int64 {
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
