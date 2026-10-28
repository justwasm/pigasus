// Package backend — gemini.go: the Gemini API transport.
//
// # What this is
//
// A `backend.Client` that speaks the Gemini API's `streamGenerateContent`
// dialect: an HTTP POST that carries a `GenerateContentRequest` JSON body
// and answers with an SSE stream of `GenerateContentResponse` chunks.
//
// # Why the clone needs it
//
// The `google-antigravity` SDKs do not spawn the harness with credentials in
// the environment. They hand it a `HarnessConfig` whose `models` field
// carries, per model, the **endpoint** the harness must dial:
//
//	models[i].gemini_api_endpoint{base_url, http_headers, api_key, options}
//	models[i].vertex_endpoint{base_url, http_headers, project, location, api_key, options}
//	models[i].gemma_endpoint{base_url}
//	models[i].custom_endpoint{backend_type, config_json}
//
// The reference harness reads that field and talks to the Gemini API
// directly (`google.golang.org/genai`). The clone had no Gemini-shaped
// transport — its native provider is OpenAI-shaped and its other client is
// the Antigravity gRPC (CloudCode) — so a harness that honoured the SDK's
// model configuration had to be given one. That is this file.
//
// # The wire
//
//	POST {base_url}/{api_version}/models/{model}:streamGenerateContent?alt=sse
//	Content-Type: application/json
//	X-Goog-Api-Key: {api_key}
//
// The body is a `GenerateContentRequest`: `contents`, `systemInstruction`,
// `generationConfig`, `tools` and `toolConfig`. The response is an SSE
// stream whose `data:` payloads are `GenerateContentResponse` objects.
//
// # Evidence
//
// The request shape below was captured against the reference binary
// (`out/localharness/`, cycle 301): the harness was pointed at a local
// fake endpoint and the request it sent was recorded verbatim. The
// captured body carried `contents`, `generationConfig.maxOutputTokens`,
// `sessionId`, `systemInstruction` and `toolConfig.functionCallingConfig`
// — and `tools` was absent because the config advertised no tools.
package backend

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/justwasm/pigasus/geminicode"
)

// GeminiAPIVersion is the API version segment the reference harness used.
const GeminiAPIVersion = "v1beta"

// GeminiAPIClientHeader is the value the reference put in
// `X-Goog-Api-Client`.
//
// The reference's value came from the `google.golang.org/genai` SDK it
// embeds, which builds the header from three parts and then appends the
// binary's own build stamp:
//
//	google-genai-sdk/1.74.0 gl-go/go1.28-20260921-RC02 cl/985595000 +644cd61a4a X:fieldtrack,boringcrypto,simd
//
// The first three are fixed — the SDK version, the Go runtime and the
// SDK's client version. The last two are the binary's own: the VCS
// revision it was built from and the Go experiments it was built with.
// The clone reproduces the shape; its own revision and experiments are
// its own.
var GeminiAPIClientHeader = "google-genai-sdk/1.74.0 gl-go/" + runtime.Version() +
	" cl/" + genaiClientVersion + buildStamp()

// genaiClientVersion is the `google.golang.org/genai` SDK's own client
// version. It is a constant of that SDK's release, not of the binary that
// embeds it.
const genaiClientVersion = "985595000"

// buildStamp renders the genai SDK's build stamp: the VCS revision the
// binary was built from, and the Go experiments it was built with.
//
// The SDK appends `+<revision>` for the revision and ` X:<experiments>`
// for the experiment list. A binary built outside a checkout carries
// neither.
func buildStamp() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var revision, experiments string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
			if len(revision) > 10 {
				revision = revision[:10]
			}
		case "GOEXPERIMENT":
			experiments = setting.Value
		}
	}
	var b strings.Builder
	if revision != "" {
		// The reference's stamp is separated from the client version by
		// a space: `cl/985595000 +644cd61a4a`.
		b.WriteString(" +")
		b.WriteString(revision)
	}
	if experiments != "" {
		b.WriteString(" X:")
		b.WriteString(experiments)
	}
	return b.String()
}

// GeminiUserAgent is the default User-Agent of the Gemini transport.
var GeminiUserAgent = "pigasus-harness"

// GeminiDefaultMaxOutputTokens is the `generationConfig.maxOutputTokens`
// the reference sent when the endpoint config did not name one. The
// reference's captured request carried 65535.
const GeminiDefaultMaxOutputTokens = 65535

// GeminiConfig is the knob set for one Gemini-API endpoint.
type GeminiConfig struct {
	// BaseURL is the endpoint root, without the API version. The reference
	// appends `/{api_version}/models/{model}:streamGenerateContent`.
	BaseURL string
	// APIKey is sent as `X-Goog-Api-Key`.
	APIKey string
	// Model is the wire model id (`gemini-3-pro-preview`, …).
	Model string
	// Headers are extra request headers. They are applied *before* the
	// transport's own headers, so a caller cannot accidentally clobber
	// `X-Goog-Api-Key` — that would silently break authentication.
	Headers map[string]string
	// APIVersion overrides the `/v1beta` segment.
	APIVersion string
	// ThinkingLevel maps to `generationConfig.thinkingConfig.thinkingLevel`.
	ThinkingLevel string
	// MaxOutputTokens overrides `generationConfig.maxOutputTokens`.
	MaxOutputTokens int
	// SessionID is sent as the request's `sessionId`. Empty mints a random
	// one per request, which is what the reference did.
	SessionID string
	// UserAgent is the request's `User-Agent`.
	UserAgent string
	// TimeoutSeconds bounds one HTTP request. Zero uses the transport's
	// default; a negative value disables the client-side timeout.
	TimeoutSeconds int
	// HTTPClient overrides the HTTP client (tests inject a stub server).
	HTTPClient *http.Client

	// VertexProject and VertexLocation switch the endpoint into Vertex AI
	// mode: the path becomes
	// `/{version}/projects/{project}/locations/{location}/publishers/google/models/{model}`.
	VertexProject  string
	VertexLocation string
	// JSONSchemaTools sends every advertised schema as
	// `parametersJsonSchema` instead of the API's OpenAPI subset.
	//
	// The reference's harness declares its tools with a
	// `google.protobuf.Struct`, so its schemas always travel under
	// `parametersJsonSchema` — including the schema the client names for
	// the `finish` tool, whose shape says nothing about the dialect it
	// belongs to. The CLI's declarations are the OpenAPI subset, and they
	// travel under `parameters`.
	JSONSchemaTools bool
}

// withDefaults fills in the values that can be defaulted.
func (c GeminiConfig) withDefaults() GeminiConfig {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if c.APIVersion == "" {
		c.APIVersion = GeminiAPIVersion
	}
	if c.MaxOutputTokens <= 0 {
		c.MaxOutputTokens = GeminiDefaultMaxOutputTokens
	}
	if c.UserAgent == "" {
		c.UserAgent = GeminiUserAgent
	}
	return c
}

// GeminiClient is a backend.Client that streams from one Gemini API
// endpoint. It is safe for concurrent Stream calls: each call owns its own
// HTTP request and its own goroutine.
type GeminiClient struct {
	cfg     GeminiConfig
	httpDo  func(*http.Request) (*http.Response, error)
	baseURL string

	// modeMu guards functionCallingMode.
	modeMu sync.RWMutex
	// functionCallingMode is the mode the request's toolConfig carries.
	// It is "AUTO" unless a caller turns the function calling off.
	functionCallingMode string
}

// SetFunctionCallingMode sets the mode the next request's toolConfig
// carries.
//
// The reference turns the function calling off when the model has one
// retry left: the request carries "NONE", so the model answers with text
// instead of another call. An empty mode means the default, "AUTO".
func (c *GeminiClient) SetFunctionCallingMode(mode string) {
	c.modeMu.Lock()
	defer c.modeMu.Unlock()
	c.functionCallingMode = strings.TrimSpace(mode)
}

// FunctionCallingMode returns the mode the next request's toolConfig
// carries.
func (c *GeminiClient) FunctionCallingMode() string {
	c.modeMu.RLock()
	defer c.modeMu.RUnlock()
	return c.functionCallingMode
}

// functionCallingModeOrDefault returns the mode to put on the wire.
func (c *GeminiClient) functionCallingModeOrDefault() string {
	if mode := c.FunctionCallingMode(); mode != "" {
		return mode
	}
	return "AUTO"
}

// NewGemini returns a Client for the endpoint named by the configuration.
func NewGemini(cfg GeminiConfig) Client {
	return NewGeminiWithConfig(cfg)
}

// NewGeminiWithConfig returns a Client for the supplied config. A non-nil
// HTTPClient is preserved (tests inject a stub server).
func NewGeminiWithConfig(cfg GeminiConfig) Client {
	cfg = cfg.withDefaults()
	c := &GeminiClient{cfg: cfg, baseURL: cfg.BaseURL}
	switch {
	case cfg.HTTPClient != nil:
		c.httpDo = cfg.HTTPClient.Do
	case cfg.TimeoutSeconds < 0:
		c.httpDo = (&http.Client{}).Do
	case cfg.TimeoutSeconds > 0:
		c.httpDo = (&http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}).Do
	default:
		c.httpDo = (&http.Client{}).Do
	}
	return c
}

// Endpoint returns a short, loggable description of where this client is
// pointing. The API key is not echoed.
func (c *GeminiClient) Endpoint() string {
	return fmt.Sprintf("%s model=%s", c.baseURL, c.cfg.Model)
}

// Stream performs one `streamGenerateContent` call and translates the SSE
// stream into the clone's neutral chunk channel.
func (c *GeminiClient) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	if strings.TrimSpace(c.cfg.BaseURL) == "" {
		return nil, fmt.Errorf("backend gemini: base url is required")
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = c.cfg.Model
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("backend gemini: model is required")
	}

	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("backend gemini: encode request: %w", err)
	}
	// The reference's body ends with a newline: the genai SDK writes it
	// with a `json.Encoder`, which appends one.
	body = append(body, '\n')

	out := make(chan StreamChunk, 16)
	go func() {
		defer close(out)
		if err := c.run(ctx, body, out); err != nil {
			out <- StreamChunk{Error: err, Done: true}
		}
	}()
	return out, nil
}

// run opens the request and consumes the response body.
func (c *GeminiClient) run(ctx context.Context, body []byte, out chan<- StreamChunk) error {
	var lastErr error
	for attempt := 0; attempt <= MaxConnectionRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(NextConnectionRetryDelay(attempt - 1)):
			}
		}
		err := c.runOnce(ctx, body, out)
		if err == nil {
			return nil
		}
		if !IsConnectionDropped(err) {
			return err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = &ConnectionDroppedError{URL: c.baseURL, Err: fmt.Errorf("max retries exceeded")}
	}
	return lastErr
}

// runOnce performs one HTTP attempt and consumes the SSE body.
func (c *GeminiClient) runOnce(ctx context.Context, body []byte, out chan<- StreamChunk) error {
	endpoint := c.endpointURL()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("content-type", "application/json")
	for name, value := range c.cfg.Headers {
		httpReq.Header.Set(name, value)
	}
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("X-Goog-Api-Key", c.cfg.APIKey)
	}
	httpReq.Header.Set("X-Goog-Api-Client", GeminiAPIClientHeader)
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)

	resp, err := c.httpDo(httpReq)
	if err != nil {
		return ClassifyTransportError(err, endpoint)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		rawBody, _ := io.ReadAll(resp.Body)
		return &HTTPError{StatusCode: resp.StatusCode, Body: rawBody, URL: endpoint}
	}
	return c.readBody(ctx, resp, out)
}

// endpointURL builds the request URL.
func (c *GeminiClient) endpointURL() string {
	version := c.cfg.APIVersion
	if c.cfg.VertexProject != "" {
		location := c.cfg.VertexLocation
		if location == "" {
			location = "us-central1"
		}
		return fmt.Sprintf(
			"%s/%s/projects/%s/locations/%s/publishers/google/models/%s:streamGenerateContent?alt=sse",
			c.baseURL, version, c.cfg.VertexProject, location, c.cfg.Model,
		)
	}
	return fmt.Sprintf("%s/%s/models/%s:streamGenerateContent?alt=sse", c.baseURL, version, c.cfg.Model)
}

// readBody consumes the SSE stream and pushes the translated chunks.
func (c *GeminiClient) readBody(ctx context.Context, resp *http.Response, out chan<- StreamChunk) error {
	idleR := NewIdleTimeoutReader(resp.Body, StreamIdleTimeout)
	scanner := bufio.NewScanner(idleR)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<22)

	var meta *ChunkMeta
	calls := newGeminiToolAccumulator()

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var chunk geminiResponse
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			classified := ClassifyTransportError(fmt.Errorf("backend gemini: sse decode: %w", err), c.baseURL)
			out <- StreamChunk{Error: classified, Done: true}
			return classified
		}
		if usage := chunk.UsageMetadata; usage != nil {
			meta = &ChunkMeta{
				PromptTokens:     usage.PromptTokenCount,
				CompletionTokens: usage.CandidatesTokenCount,
				TotalTokens:      usage.TotalTokenCount,
				ReasoningTokens:  usage.ThoughtsTokenCount,
			}
		}
		if err := c.emitChunk(ctx, chunk, calls, out); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		classified := ClassifyTransportError(fmt.Errorf("backend gemini: sse read: %w", err), c.baseURL)
		out <- StreamChunk{Error: classified, Done: true}
		return classified
	}
	if !calls.emit(ctx, out) {
		return nil
	}
	out <- StreamChunk{Done: true, Meta: meta}
	return nil
}

// emitChunk translates one Gemini response chunk into the neutral chunk
// channel.
func (c *GeminiClient) emitChunk(ctx context.Context, chunk geminiResponse, calls *geminiToolAccumulator, out chan<- StreamChunk) error {
	for _, candidate := range chunk.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.FunctionCall != nil {
				calls.add(part.FunctionCall)
				continue
			}
			if part.Text == "" {
				continue
			}
			select {
			case <-ctx.Done():
				out <- StreamChunk{Error: ctx.Err(), Done: true}
				return ctx.Err()
			case out <- StreamChunk{Text: part.Text}:
			}
		}
	}
	return nil
}

// geminiToolAccumulator collects the function calls of one response.
//
// The Gemini API has no tool-call ids: a functionCall carries only a name
// and an argument object. The clone's tool loop correlates a call with its
// result by id, so the transport mints one and remembers the name that goes
// with it.
//
// The reference mints a UUID (v4) per call, and the answer carries the
// same id back — the captured request body reads:
//
//	"functionCall":     {"args": {…}, "id": "17f025ce-…", "name": "view_file"}
//	"functionResponse": {"id": "17f025ce-…", "name": "view_file", "response": {…}}
//
// The id is minted here rather than by the caller, so the call and its
// answer always carry the same one.
type geminiToolAccumulator struct {
	order []int
	byIdx map[int]*geminiFunctionCall
}

func newGeminiToolAccumulator() *geminiToolAccumulator {
	return &geminiToolAccumulator{byIdx: map[int]*geminiFunctionCall{}}
}

func (a *geminiToolAccumulator) add(call *geminiFunctionCall) {
	idx := len(a.order)
	a.byIdx[idx] = call
	a.order = append(a.order, idx)
}

func (a *geminiToolAccumulator) empty() bool { return len(a.order) == 0 }

func (a *geminiToolAccumulator) list() []ToolCall {
	out := make([]ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		call := a.byIdx[idx]
		if call == nil || call.Name == "" {
			continue
		}
		args := call.Args
		if len(args) == 0 {
			args = map[string]any{}
		}
		raw, err := json.Marshal(args)
		if err != nil {
			raw = []byte("{}")
		}
		out = append(out, ToolCall{
			ID:        newToolCallID(),
			Name:      call.Name,
			Arguments: raw,
		})
	}
	return out
}

func (a *geminiToolAccumulator) emit(ctx context.Context, out chan<- StreamChunk) bool {
	if a.empty() {
		return true
	}
	select {
	case <-ctx.Done():
		out <- StreamChunk{Error: ctx.Err(), Done: true}
		return false
	case out <- StreamChunk{ToolCalls: a.list()}:
		return true
	}
}

// newToolCallID mints the id a tool call carries.
//
// The reference mints a UUID (version 4) per call, and the answer echoes
// it. The clone's own id scheme ("call-1", "call-2") was a placeholder —
// the id goes on the wire, and the wire carries a UUID.
func newToolCallID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// A failure here means the system entropy source is gone. The id
		// only has to be unique within one conversation, so the counter
		// fallback keeps the loop working.
		return fmt.Sprintf("call-%d", toolCallFallback.Add(1))
	}
	// Version 4, variant 1.
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

// toolCallFallback numbers the ids minted when the entropy source fails.
var toolCallFallback atomic.Uint64

// geminiRequest is the `GenerateContentRequest` body.
// The request body's fields are declared in alphabetical order.
//
// The reference's body comes out of a map, and Go's `encoding/json`
// writes a map's keys sorted — so the reference's wire order is
// alphabetical, and the clone's is too. The order is not semantic, but a
// byte-comparing fake endpoint sees it.
type geminiRequest struct {
	Contents          []geminiContent   `json:"contents"`
	GenerationConfig  *geminiGenConfig  `json:"generationConfig,omitempty"`
	SessionID         string            `json:"sessionId,omitempty"`
	SystemInstruction *geminiContent    `json:"systemInstruction,omitempty"`
	ToolConfig        *geminiToolConfig `json:"toolConfig,omitempty"`
	Tools             []geminiTool      `json:"tools,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}

type geminiPart struct {
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
	Text             string                  `json:"text,omitempty"`
	// InlineData carries an image the user attached: the mime type and
	// the base64 payload, in one part. The spelling is the reference's
	// own — its JSON wire types (the geminicode package) name the member
	// `inlineData` with a `mimeType` and a `data` member.
	InlineData *geminiInlineData `json:"inlineData,omitempty"`
}

// geminiInlineData is the payload of an inline image part.
type geminiInlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

// geminiFunctionCall is the model half of a tool call.
//
// The fields are in the order the reference writes them: the body is keyed
// alphabetically at every level, so a part reads `args`, `id`, `name`.
type geminiFunctionCall struct {
	Args map[string]any `json:"args,omitempty"`
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
}

// geminiFunctionResponse is the harness's half of a tool call.
//
// The reference answers with the same id the call carried, so the API can
// pair them.
type geminiFunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response,omitempty"`
}

type geminiGenConfig struct {
	MaxOutputTokens int                   `json:"maxOutputTokens,omitempty"`
	Temperature     *float64              `json:"temperature,omitempty"`
	ThinkingConfig  *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiThinkingConfig struct {
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type geminiFunctionDeclaration struct {
	Description string          `json:"description,omitempty"`
	Name        string          `json:"name"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	// ParametersJSONSchema carries a JSON Schema (draft 2020-12) as the
	// API's own `parametersJsonSchema` field.
	//
	// The two fields are not interchangeable: `parameters` is the API's
	// OpenAPI subset — uppercase types, no `$schema`, no
	// `additionalProperties` — and the reference's harness advertises
	// plain JSON Schema. The captured request carries every declaration
	// under `parametersJsonSchema`.
	ParametersJSONSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

type geminiToolConfig struct {
	FunctionCallingConfig *geminiFunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

type geminiFunctionCallingConfig struct {
	Mode string `json:"mode,omitempty"`
}

// geminiResponse is the `GenerateContentResponse` chunk shape.
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Role  string       `json:"role"`
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		TotalTokenCount         int `json:"totalTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

// buildRequest translates the clone's neutral request into the Gemini
// request body.
func (c *GeminiClient) buildRequest(req StreamRequest) geminiRequest {
	body := geminiRequest{
		GenerationConfig: &geminiGenConfig{
			MaxOutputTokens: c.cfg.MaxOutputTokens,
		},
		ToolConfig: &geminiToolConfig{
			FunctionCallingConfig: &geminiFunctionCallingConfig{Mode: c.functionCallingModeOrDefault()},
		},
		SessionID: c.sessionID(),
	}
	if c.cfg.ThinkingLevel != "" {
		body.GenerationConfig.ThinkingConfig = &geminiThinkingConfig{ThinkingLevel: c.cfg.ThinkingLevel}
	}
	if req.Temperature != 0 {
		temperature := req.Temperature
		body.GenerationConfig.Temperature = &temperature
	}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		body.SystemInstruction = &geminiContent{
			Role:  "user",
			Parts: []geminiPart{{Text: req.SystemPrompt}},
		}
	}
	paired := geminicode.PairedToolCallIDs(
		geminiCallIDs(req.Messages), geminiResultIDs(req.Messages))
	for _, message := range req.Messages {
		body.Contents = append(body.Contents, geminiContentsFor(message, paired)...)
	}
	if len(req.Tools) > 0 {
		decls := make([]geminiFunctionDeclaration, 0, len(req.Tools))
		for _, tool := range req.Tools {
			decl := geminiFunctionDeclaration{
				Name:        tool.Name,
				Description: tool.Description,
			}
			if c.cfg.JSONSchemaTools || isJSONSchema(tool.Parameters) {
				decl.ParametersJSONSchema = tool.Parameters
			} else {
				decl.Parameters = tool.Parameters
			}
			decls = append(decls, decl)
		}
		body.Tools = []geminiTool{{FunctionDeclarations: decls}}
	}
	return body
}

// isJSONSchema reports whether a schema is a plain JSON Schema rather
// than the API's OpenAPI subset.
//
// The two differ in the ways that matter: a JSON Schema names its
// dialect (`$schema`) and closes itself (`additionalProperties`), and
// its types are lowercase. The OpenAPI subset has neither.
func isJSONSchema(schema json.RawMessage) bool {
	if len(schema) == 0 {
		return false
	}
	var probe struct {
		Schema               *string         `json:"$schema"`
		AdditionalProperties json.RawMessage `json:"additionalProperties"`
	}
	if err := json.Unmarshal(schema, &probe); err != nil {
		return false
	}
	return probe.Schema != nil || len(probe.AdditionalProperties) > 0
}

// geminiCallIDs collects the ids of the tool calls the conversation holds.
func geminiCallIDs(messages []Message) []string {
	var ids []string
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, call := range m.ToolCalls {
			ids = append(ids, call.ID)
		}
	}
	return ids
}

// geminiResultIDs collects the ids of the tool results the conversation holds.
func geminiResultIDs(messages []Message) []string {
	var ids []string
	for _, m := range messages {
		if m.Role != "tool" {
			continue
		}
		ids = append(ids, m.ToolCallID)
	}
	return ids
}

// geminiContentsFor maps one neutral message onto Gemini contents.
//
// The clone's roles are `user`, `assistant` and `tool`; Gemini's are `user`
// and `model`.
//
// A tool call travels as a `functionCall` part inside a `model` content, and
// the answer as a `functionResponse` part — which the reference also puts
// in a `model` content, not a `user` one. The captured request body reads:
//
//	{"parts": [{"functionCall": {"args": {…}, "id": "…", "name": "view_file"}}], "role": "model"}
//	{"parts": [{"functionResponse": {"id": "…", "name": "view_file", "response": {…}}}], "role": "model"}
//
// The answer carries the id the call carried, so the API can pair them.
//
// 【一手】 agy 1.4.9: a tool call with no result and a result with no call
// are left out of the request — the pairing rule the CloudCode dialect
// applies too, and the one implementation of it lives in geminicode.
func geminiContentsFor(message Message, paired map[string]bool) []geminiContent {
	switch message.Role {
	case "assistant":
		parts := make([]geminiPart, 0, len(message.ToolCalls)+1)
		if text := message.WireText(); text != "" {
			parts = append(parts, geminiPart{Text: text})
		}
		for _, call := range message.ToolCalls {
			if paired != nil && !paired[call.ID] {
				continue
			}
			args := map[string]any{}
			if len(call.Arguments) > 0 {
				_ = json.Unmarshal(call.Arguments, &args)
			}
			parts = append(parts, geminiPart{
				FunctionCall: &geminiFunctionCall{Args: args, ID: call.ID, Name: call.Name},
			})
		}
		if len(parts) == 0 {
			return nil
		}
		return []geminiContent{{Role: "model", Parts: parts}}
	case "tool":
		if paired != nil && !paired[message.ToolCallID] {
			return nil
		}
		name := message.Name
		if name == "" {
			name = message.ToolCallID
		}
		return []geminiContent{{
			Role: "model",
			Parts: []geminiPart{{
				FunctionResponse: &geminiFunctionResponse{
					ID:       message.ToolCallID,
					Name:     name,
					Response: toolResponseFor(message),
				},
			}},
		}}
	default:
		text := message.WireText()
		if text == "" && len(message.Images) == 0 {
			return nil
		}
		parts := make([]geminiPart, 0, len(message.Images)+1)
		if text != "" {
			parts = append(parts, geminiPart{Text: text})
		}
		for _, img := range message.Images {
			if len(img.Data) == 0 {
				continue
			}
			parts = append(parts, geminiPart{InlineData: &geminiInlineData{
				MIMEType: img.MIMEType,
				Data:     base64.StdEncoding.EncodeToString(img.Data),
			}})
		}
		if len(parts) == 0 {
			return nil
		}
		return []geminiContent{{Role: "user", Parts: parts}}
	}
}

// toolResponseFor returns the payload a tool's answer carries.
//
// Most tools answer with text, which the API expects wrapped in an
// `output` member. A tool that answers with a structured value — the
// reference's `grep_search` answers with the files it matched — carries
// that value as it is.
func toolResponseFor(message Message) map[string]any {
	// 【一手】 agy 1.4.8: a failed call travels under `error`, a
	// successful one under `output`. The Gemini API-key dialect is the
	// reference's own path, so it takes the same envelope as the
	// CloudCode one.
	if message.IsError {
		return map[string]any{"error": message.Content}
	}
	if len(message.Result) > 0 {
		return message.Result
	}
	return map[string]any{"output": message.Content}
}

// sessionID returns the request's `sessionId`, minting a random one when
// the config does not name it.
func (c *GeminiClient) sessionID() string {
	if c.cfg.SessionID != "" {
		return c.cfg.SessionID
	}
	return geminiSessionID()
}

// geminiSessionID mints a random int64 rendered as a decimal string —
// the shape the reference sent.
func geminiSessionID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "0"
	}
	value := int64(binary.LittleEndian.Uint64(buf[:]))
	return fmt.Sprintf("%d", value)
}

// goRuntimeTag renders the Go runtime version for the client header, in the
// `1.27.3` shape — the `go` prefix stripped.
func goRuntimeTag() string {
	return strings.TrimPrefix(runtime.Version(), "go")
}
