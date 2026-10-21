// Package geminiwire holds the JSON wire types for the CloudCode
// `generateContent` family as spoken by agy 1.4.2.
//
// Two envelopes exist and they nest:
//
//	POST /v1internal:generateContent
//	{"model":"…","project":"…","requestId":"…",
//	 "request":{"contents":[…],"systemInstruction":{…},"tools":[…],
//	            "generationConfig":{…}}}
//
//	200 OK
//	{"response":{"candidates":[…],"usageMetadata":{…}},"traceId":"…","metadata":{}}
//
// Whether the `request`/`response` wrapper is present differs between the
// CloudCode surface (`/v1internal:…`, wrapped) and the plain Gemini surface
// (`/v1beta/models/…`, bare), so every helper here accepts both. Unknown
// fields are not preserved: these types exist for translation, not for
// pass-through (pass-through uses the original bytes).
package geminiwire

import "strings"

// Part is one element of a content turn. Exactly one of the payload fields is
// expected, but Gemini tolerates mixtures (e.g. text + functionCall).
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
type FunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
	// ID is not part of upstream Gemini; agy sends one on replay and uses it
	// to pair the later functionResponse. We echo whatever the provider gave
	// us so the pairing survives.
	ID string `json:"id,omitempty"`
}

// FunctionResponse is the client's answer to a FunctionCall.
type FunctionResponse struct {
	Name     string `json:"name"`
	Response any    `json:"response,omitempty"`
	ID       string `json:"id,omitempty"`
}

// InlineData is base64 payload (images, small files).
type InlineData struct {
	MIMEType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

// FileData references an uploaded file by URI.
type FileData struct {
	MIMEType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri,omitempty"`
}

// Content is one conversation turn. Role is "user" or "model".
type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts,omitempty"`
}

// SystemInstruction is the agent's system prompt, always as text parts.
type SystemInstruction struct {
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
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// GenerationConfig carries sampling parameters.
type GenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	TopK            int      `json:"topK,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
	CandidateCount  int      `json:"candidateCount,omitempty"`
}

// GenerateContentRequest is both the outer CloudCode envelope and the inner
// Gemini request. Request is non-nil exactly when the caller used the
// CloudCode shape.
type GenerateContentRequest struct {
	Model        string                  `json:"model,omitempty"`
	ModelID      string                  `json:"modelId,omitempty"`
	ModelIDSnake string                  `json:"model_id,omitempty"`
	Project      string                  `json:"project,omitempty"`
	RequestID    string                  `json:"requestId,omitempty"`
	Request      *GenerateContentRequest `json:"request,omitempty"`

	SystemInstruction      *SystemInstruction `json:"systemInstruction,omitempty"`
	SystemInstructionSnake *SystemInstruction `json:"system_instruction,omitempty"`

	Contents         []Content         `json:"contents,omitempty"`
	Tools            []Tool            `json:"tools,omitempty"`
	GenerationConfig *GenerationConfig `json:"generationConfig,omitempty"`
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
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
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
