package geminicode

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
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
	ThinkingLevel   string
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
	contents = HardenContents(contents)
	inner := GenerateContentRequest{Contents: contents}
	if system != "" {
		inner.SystemInstruction = &SystemInstruction{Role: "user", Parts: []Part{{Text: system}}}
	}
	if decls := BuildTools(t.Tools); len(decls) > 0 {
		inner.Tools = []Tool{{FunctionDeclarations: decls}}
	}
	maxOut, budget := Limits(t.Model, t.MaxOutputTokens, t.ThinkingBudget)
	temp := t.Temperature
	tc := &ThinkingConfig{IncludeThoughts: true}
	level := strings.TrimSpace(t.ThinkingLevel)
	if level == "" && GeminiGeneration(t.Model) >= 3 {
		level = "high"
	}
	if level != "" {
		tc.ThinkingLevel = level
	} else {
		tc.ThinkingBudget = budget
	}
	inner.GenerationConfig = &GenerationConfig{
		MaxOutputTokens: maxOut,
		ThinkingConfig:  tc,
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

var geminiVersion = regexp.MustCompile(`(?:^|/)gemini-(\d+)`)

// GeminiGeneration returns the major generation of a Gemini model name
// (e.g. 3 for "gemini-3.8-flash", 4 for "gemini-4-flash"), or 0 if unknown.
func GeminiGeneration(model string) int {
	m := geminiVersion.FindStringSubmatch(strings.ToLower(model))
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// HardenContents ensures conversation contents conform to Gemini's role sequence
// constraints, matching the reference Gemini CLI hardenHistory behavior learned
// from magpie:
//  1. Gemini turns away requests whose first turn is a model turn (e.g. after history
//     trimming or compaction) with 400 "The first turn must be a user turn". Prepend
//     a synthetic user turn.
//  2. Gemini turns away requests whose last turn is an assistant turn (without function
//     response) with 400 "Requests ending with a model turn are not supported".
//     Append a synthetic user turn asking to continue.
func HardenContents(contents []Content) []Content {
	if len(contents) == 0 {
		return contents
	}
	out := contents
	if out[0].Role == "model" {
		out = append([]Content{{Role: "user", Parts: []Part{{Text: "[Continuing from previous AI thoughts...]"}}}}, out...)
	}
	if n := len(out); n > 0 && out[n-1].Role == "model" && !hasFunctionResponse(out[n-1]) {
		out = append(out, Content{Role: "user", Parts: []Part{{Text: "Continue."}}})
	}
	return out
}

func hasFunctionResponse(c Content) bool {
	for _, p := range c.Parts {
		if p.FunctionResponse != nil {
			return true
		}
	}
	return false
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
//
// 【一手】 agy 1.4.9: a tool call whose result never arrived, and a result
// whose call is gone — "such as after older history was trimmed" — are
// left out of the request. The reference's release notes name the failure
// it fixes: "Fixed conversations getting stuck failing on every turn after
// a tool result was left without its matching tool call …; unmatched tool
// calls and results are now left out of the request." The API pairs the
// two halves itself, so either half on its own fails the request.
func buildContents(messages []TurnMessage, initialSystem string, signatures map[string]string) ([]Content, string) {
	system := strings.TrimSpace(initialSystem)
	paired := pairedToolCallIDs(messages)
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
			if part := buildAssistantContent(m, signatures, paired); len(part.Parts) > 0 {
				contents = append(contents, part)
			}
		case "tool":
			if !paired[m.ToolCallID] {
				continue
			}
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

// pairedToolCallIDs returns the ids of the tool calls that have both a
// call and a result in this conversation.
//
// An id that appears on only one side is an orphan: a call the agent never
// answered (or whose answer was trimmed away) and a result whose call is
// gone. 【一手】 agy 1.4.9 — both are left out of the request.
func pairedToolCallIDs(messages []TurnMessage) map[string]bool {
	var calls, results []string
	for _, m := range messages {
		switch strings.ToLower(m.Role) {
		case "assistant":
			for _, tc := range m.ToolCalls {
				calls = append(calls, tc.ID)
			}
		case "tool":
			results = append(results, m.ToolCallID)
		}
	}
	return PairedToolCallIDs(calls, results)
}

// PairedToolCallIDs returns the ids that appear in both lists — the tool
// calls that have a result and the results that have a call.
//
// It is the one place the pairing rule lives: the CloudCode dialect
// (geminicode) and the Gemini API-key dialect (internal/backend) both
// drop the halves that do not match, and both call this.
func PairedToolCallIDs(calls, results []string) map[string]bool {
	called := make(map[string]bool, len(calls))
	for _, id := range calls {
		called[id] = true
	}
	answered := make(map[string]bool, len(results))
	for _, id := range results {
		answered[id] = true
	}
	paired := make(map[string]bool, len(called))
	for id := range called {
		if answered[id] {
			paired[id] = true
		}
	}
	return paired
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
//
// A call with no result in the conversation is left out — 【一手】 agy
// 1.4.9 — so the request never carries half a pair.
func buildAssistantContent(m TurnMessage, signatures map[string]string, paired map[string]bool) Content {
	parts := make([]Part, 0, len(m.ToolCalls)+1)
	if m.Content != "" {
		parts = append(parts, Part{Text: m.Content})
	}
	for _, tc := range m.ToolCalls {
		if paired != nil && !paired[tc.ID] {
			continue
		}
		parts = append(parts, callPart(tc, signatures))
	}
	return Content{Role: "model", Parts: parts}
}

// SkipThoughtSignature is the sentinel Google documents for tool calls
// in history that have no thought signature: "skip_thought_signature_validator".
// Gemini 3 rejects history containing tool calls without a signature unless
// this validator bypass is provided (learned from magpie).
const SkipThoughtSignature = "skip_thought_signature_validator"

// InjectSkipThoughtSignatures scans conversation contents and injects
// SkipThoughtSignature ("skip_thought_signature_validator") on any historical
// FunctionCall parts that lack a ThoughtSignature. Returns true if any part was modified.
func InjectSkipThoughtSignatures(contents []Content) bool {
	modified := false
	for i := range contents {
		for j := range contents[i].Parts {
			p := &contents[i].Parts[j]
			if p.FunctionCall != nil && p.ThoughtSignature == "" {
				p.ThoughtSignature = SkipThoughtSignature
				modified = true
			}
		}
	}
	return modified
}

// ResolveThinkingConfig resolves any conflict between thinkingLevel and thinkingBudget.
// For Gemini 3+ models, thinkingBudget is pruned and thinkingLevel defaults to "high"
// if not set. If thinkingLevel is present, thinkingBudget is always pruned (set to 0).
// Returns true if gc was modified.
func ResolveThinkingConfig(model string, gc *GenerationConfig) bool {
	if gc == nil {
		return false
	}
	gen := GeminiGeneration(model)
	if gc.ThinkingConfig == nil {
		if gen >= 3 {
			gc.ThinkingConfig = &ThinkingConfig{
				IncludeThoughts: true,
				ThinkingLevel:   "high",
			}
			return true
		}
		return false
	}
	modified := false
	tc := gc.ThinkingConfig
	level := strings.TrimSpace(tc.ThinkingLevel)
	if level == "" && gen >= 3 {
		level = "high"
		tc.ThinkingLevel = level
		modified = true
	}
	if level != "" && tc.ThinkingBudget != 0 {
		tc.ThinkingBudget = 0
		modified = true
	}
	return modified
}

// callPart builds a functionCall part and replays the thoughtSignature
// Gemini 3 returned with it, or passes SkipThoughtSignature if unrecorded.
func callPart(tc TurnToolCall, signatures map[string]string) Part {
	args := json.RawMessage(tc.Arguments)
	if len(args) == 0 || !json.Valid(args) {
		args = json.RawMessage(`{}`)
	}
	part := Part{FunctionCall: &FunctionCall{ID: tc.ID, Name: tc.Name, Args: args}}
	if signatures != nil && signatures[tc.ID] != "" {
		part.ThoughtSignature = signatures[tc.ID]
	} else {
		part.ThoughtSignature = SkipThoughtSignature
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
