package geminicode

import (
	"encoding/json"
	"strings"
)

// Part is one element of a content turn. Exactly one of the payload fields
// is expected, but Gemini tolerates mixtures (e.g. text + functionCall).
//
// The member order is the reference's own: text, thought, thoughtSignature,
// functionCall, functionResponse, inlineData. A part is the same shape in a
// request and in a response.
type Part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
	InlineData       *InlineData       `json:"inlineData,omitempty"`
	FileData         *FileData         `json:"fileData,omitempty"`
}

// FunctionCall is the model's request to invoke a tool.
//
// Args is a json.RawMessage so the model's own bytes survive the round trip:
// the reference replays the arguments it received, key order included, and
// re-encoding them through a map would change the request the model sees.
//
// ID is not part of upstream Gemini; agy sends one on replay and uses it to
// pair the later functionResponse.
type FunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// FunctionResponse is the client's answer to a FunctionCall.
//
// Response is the reference's own envelope: a successful call travels back
// under `output`, a failed one under `error` (agy 1.4.8). The body is the
// same string either way.
type FunctionResponse struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Response any    `json:"response"`
}

// InlineData is base64 payload (images, small files).
type InlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

// FileData references an uploaded file by URI.
type FileData struct {
	MIMEType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri,omitempty"`
}

// Content is one conversation turn. Role is "user" or "model".
type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// SystemInstruction is the agent's system prompt, always as text parts.
//
// The reference sends it with a role — `{"role":"user","parts":[…]}` —
// while the public Gemini API leaves the role out. The member is optional
// so both spellings parse.
type SystemInstruction struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts,omitempty"`
}

// FunctionDeclaration declares one callable function.
type FunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Tool groups declarations the way Gemini does (one entry may hold many).
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations"`
}

// GenerationConfig carries sampling parameters.
//
// The member order is the order the reference sends them in, and the first
// three are the ones the CloudCode surface actually uses: maxOutputTokens,
// temperature and thinkingConfig. The rest are the public Gemini spelling,
// accepted so a caller can reuse one struct for both surfaces.
//
// Temperature is a pointer because "unset" and "0" are different requests:
// the reference omits the member when it has no opinion.
type GenerationConfig struct {
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
	TopP            *float64        `json:"topP,omitempty"`
	TopK            int             `json:"topK,omitempty"`
	StopSequences   []string        `json:"stopSequences,omitempty"`
	CandidateCount  int             `json:"candidateCount,omitempty"`
}

// ThinkingConfig asks the model for its reasoning trace and bounds it.
//
// 【一手】 agy always asks for thoughts (includeThoughts is true on every
// request the clone sends) and budgets them per model — see Limits.
type ThinkingConfig struct {
	IncludeThoughts bool `json:"includeThoughts"`
	ThinkingBudget  int  `json:"thinkingBudget,omitempty"`
}

// GenerateContentRequest is both the outer CloudCode envelope and the inner
// Gemini request: the same type, nested.
//
// The member order is the reference's own, envelope members first and the
// inner members after. A request body therefore reads
//
//	{"project":…,"requestId":…,"request":{…},"model":…,"userAgent":…,"requestType":…}
//
// and the nested request reads
//
//	{"contents":[…],"systemInstruction":{…},"tools":[…],"labels":{…},
//	 "generationConfig":{…},"sessionId":"…"}
//
// The inner members are all omitempty, so the envelope level never emits
// them.
//
// The tolerated spellings — modelId, model_id and system_instruction — are
// the reference's own historical variants; the proxy accepts them because
// different builds of the reference have used different ones.
type GenerateContentRequest struct {
	// Envelope members.
	Project     string                  `json:"project,omitempty"`
	RequestID   string                  `json:"requestId,omitempty"`
	Request     *GenerateContentRequest `json:"request,omitempty"`
	Model       string                  `json:"model,omitempty"`
	UserAgent   string                  `json:"userAgent,omitempty"`
	RequestType string                  `json:"requestType,omitempty"`

	// Inner members.
	Contents               []Content          `json:"contents,omitempty"`
	SystemInstruction      *SystemInstruction `json:"systemInstruction,omitempty"`
	SystemInstructionSnake *SystemInstruction `json:"system_instruction,omitempty"`
	Tools                  []Tool             `json:"tools,omitempty"`
	Labels                 map[string]string  `json:"labels,omitempty"`
	GenerationConfig       *GenerationConfig  `json:"generationConfig,omitempty"`
	SessionID              string             `json:"sessionId,omitempty"`

	// Tolerated spellings.
	ModelID      string `json:"modelId,omitempty"`
	ModelIDSnake string `json:"model_id,omitempty"`
}

// Inner returns the Gemini request, unwrapping the CloudCode envelope.
// It never returns nil for a non-nil receiver.
func (r *GenerateContentRequest) Inner() *GenerateContentRequest {
	if r == nil {
		return &GenerateContentRequest{}
	}
	if r.Request != nil {
		return r.Request
	}
	return r
}

// Wrapped reports whether the envelope had a `request` wrapper.
func (r *GenerateContentRequest) Wrapped() bool {
	return r != nil && r.Request != nil
}

// ModelName returns the model identifier from either envelope level, and from
// any of the three spellings agy has used over its lifetime.
func (r *GenerateContentRequest) ModelName() string {
	if r == nil {
		return ""
	}
	for _, s := range []string{r.Model, r.ModelID, r.ModelIDSnake} {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	if r.Request != nil {
		return r.Request.ModelName()
	}
	return ""
}

// SystemText flattens the system instruction to plain text. agy always uses
// camelCase; snake_case is accepted because the reference proxy did.
func (r *GenerateContentRequest) SystemText() string {
	if r == nil {
		return ""
	}
	si := r.SystemInstruction
	if si == nil {
		si = r.SystemInstructionSnake
	}
	if si == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range si.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// Candidate is one completion alternative.
type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
	Index        int     `json:"index"`
}

// UsageMetadata mirrors Gemini's token accounting.
//
// ThoughtsTokenCount is the reasoning the model spent inside its thinking
// block; the reference renders it as "Thought for 1.5s, 24 tokens".
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount,omitempty"`
}

// GenerateContentResponse is the bare Gemini response.
type GenerateContentResponse struct {
	Candidates    []Candidate    `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string         `json:"modelVersion,omitempty"`
	ResponseID    string         `json:"responseId,omitempty"`
}

// CloudCodeResponse is the wrapped CloudCode response.
type CloudCodeResponse struct {
	Response GenerateContentResponse `json:"response"`
	TraceID  string                  `json:"traceId"`
	Metadata map[string]any          `json:"metadata"`
}

// NewResponse builds a one-candidate response with the given parts.
func NewResponse(parts []Part, finishReason string) GenerateContentResponse {
	if parts == nil {
		parts = []Part{}
	}
	return GenerateContentResponse{
		Candidates: []Candidate{{
			Content:      Content{Role: "model", Parts: parts},
			FinishReason: finishReason,
			Index:        0,
		}},
	}
}
