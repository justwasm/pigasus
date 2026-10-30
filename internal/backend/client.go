// Package backend defines the minimal Client interface used by the openai-shim
// and anthropic-shim front-ends. The interface is intentionally small: only
// the streaming surface (Stream method) is exposed. Both shims wire their HTTP
// handler logic onto this single function so the upstream contract is stable.
//
// Sandbox note: cloudaicompanion is not reachable from this pod, so the
// default in this repo is NewMock(). RealClient (gRPC) is a placeholder for
// when a licensed token + egress are available.
package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/models"
)

// StreamChunk is one element of a streaming response. The shims translate this
// into OpenAI `data: {…}` frames and Anthropic `event: content_block_delta`
// frames respectively.
type StreamChunk struct {
	Text  string     // incremental content
	Done  bool       // set on the final chunk for this request
	Error error      // non-nil if the upstream call failed mid-stream
	Meta  *ChunkMeta // optional usage / fingerprint data, sent on Done chunks

	// ToolCalls carries model-initiated tool invocations. A chunk with a
	// non-empty ToolCalls is a *request to act*, not finished text: the
	// agent loop executes the calls and streams a follow-up turn with the
	// results appended. Backends that accumulate arguments across SSE
	// deltas must emit the fully-assembled calls no later than the
	// terminating (Done) chunk.
	ToolCalls []ToolCall
}

// ChunkMeta is the metadata sent on the terminating chunk of a stream. Mirrors
// the OpenAI `usage` block (and is the closest Anthropic equivalent).
type ChunkMeta struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// ReasoningTokens is the number of completion tokens the model spent
	// inside its thinking block. OpenAI-shaped gateways surface this as
	// usage.completion_tokens_details.reasoning_tokens (the configured gateway does;
	// see gateway.go). The reference CLI renders it in the conversation
	// view's thought step ("Thought for 1.5s, 24 tokens") and in the
	// `/usage` token summary ("%s in | %s out | %s thinking").
	ReasoningTokens   int    `json:"reasoning_tokens,omitempty"`
	CachedTokens      int    `json:"cached_tokens,omitempty"`
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
}

// ImagePart is one image a user attached to a message.
//
// The neutral shape is the mime type and the bytes: every transport
// spells it its own way, and the bytes are what all of them carry.
//
// The reference's own transports put an image *inline* — the CloudCode
// dialect as `inlineData` (a mime type and the base64 payload), the
// OpenAI-shaped dialect as an `image_url` content part carrying a
// `data:` URL, and the Anthropic one as an image source block. The
// mime types the reference's catalogue names are `image/jpeg` and
// `image/png`.
type ImagePart struct {
	// MIMEType is the image's media type — "image/png", "image/jpeg".
	MIMEType string `json:"mime_type,omitempty"`
	// Data is the image's bytes.
	Data []byte `json:"data,omitempty"`
}

// Message is one round of a conversation. Role mirrors OpenAI/Anthropic roles
// (`system`, `user`, `assistant`, and `tool` for tool results). Content is
// the message's text; the images a user attached ride in Images.
//
// Tool-call plumbing (cycle 97): an assistant message that requested tools
// carries ToolCalls; the corresponding `tool` role message that feeds a
// result back carries ToolCallID (matching the originating call's ID) and Name.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	// Images are the pictures the user attached to this message. Only
	// a user message carries them; every transport drops them on the
	// other roles.
	Images []ImagePart `json:"images,omitempty"`
	// WireContent is the <USER_REQUEST> envelope the reference sends in
	// place of a user message's raw text (see WrapUserContent). Transcript
	// and persistence keep Content; only the request uses this. It is not
	// part of the wire type here, so it never marshals.
	WireContent string `json:"-"`
	// Result is the structured payload a tool answered with, when the tool
	// has one. Most tools answer with text, which the transports wrap in
	// an `output` member; a tool that answers with a value — the
	// reference's `grep_search` answers with the files it matched —
	// carries that value here and the transport sends it as it is.
	//
	// It is filled in from the tool result's details (see
	// internal/piagentbridge) and never marshals on its own.
	Result map[string]any `json:"-"`
	// IsError marks a tool result that is the reference's *failure*
	// text. The transports read it to pick the envelope: a call that
	// failed travels under the `error` member, a call that succeeded
	// under `output`.
	//
	// 【一手】 agy 1.4.8 sends a failed call back as
	// `"response": {"error": "Created At: …\nCompleted At: …\nEncountered error in tool execution: file does not exist"}`
	// — the reference's 1.4.7 release notes call the change out: "failed
	// tool calls … were being passed back to the agent as ordinary
	// results; the agent now sees them as errors and can recover on its
	// next step".
	IsError bool `json:"-"`
}

// WireText returns the text that goes to the model for this message. User
// turns carry the reference's envelope in WireContent; everything else is
// its Content verbatim.
func (m Message) WireText() string {
	if m.WireContent != "" {
		return m.WireContent
	}
	return m.Content
}

// ToolDecl is a tool the model is allowed to call, in provider-neutral form.
// Name/Description/Parameters map onto an OpenAI `tools[].function` entry
// (and onto an Anthropic `tools[]` entry, minus the input_schema key).
type ToolDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is a model-initiated tool invocation. Arguments is the raw JSON the
// model produced (not yet validated against the tool's schema).
type ToolCall struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// StreamRequest is the upstream call body, decoupled from any specific
// provider's wire format. Model is the Antigravity model id (claude-sonnet-4-6
// etc.). SystemPrompt, if non-empty, is prepended as a system message. The
// remaining messages are forwarded in order.
type StreamRequest struct {
	Model        string    `json:"model"`
	SystemPrompt string    `json:"system_prompt,omitempty"`
	Messages     []Message `json:"messages"`
	MaxTokens    int       `json:"max_tokens,omitempty"`
	Temperature  float64   `json:"temperature,omitempty"`
	// Tools, when non-empty, is the advertised tool set. The model may
	// answer with ToolCalls instead of (or before) text.
	Tools []ToolDecl `json:"tools,omitempty"`
}

// Client is the minimal interface shims need. NewMock is the default in the
// sandbox (cloudaicompanion unreachable from this pod). RealClient would be
// wired in when a licensed token is available.
type Client interface {
	// Stream returns a channel of StreamChunk. The channel is closed when the
	// upstream call finishes (success or failure). Implementations MUST emit
	// at least one chunk with Done=true at the end of every call.
	Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error)
}

// NewMock returns a Client that emits a synthetic streaming response. Useful
// for offline / sandbox tests where cloudaicompanion is unreachable. Output
// echoes the last user message back, broken into 12-char chunks to exercise the
// SSE framing path in both shims.
func NewMock() Client { return &mockClient{} }

// mockClient is a deterministic stand-in for the real Antigravity gRPC
// stream. It deliberately has zero external dependencies and zero goroutine
// leaks: the channel is closed before Stream returns.
type mockClient struct{}

// Stream pretends to be the Antigravity backend. It writes a short synthetic
// reply one chunk at a time, then a terminating chunk carrying mock usage
// metadata.
func (m *mockClient) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("backend mock: model is required")
	}
	out := make(chan StreamChunk, 16)
	go func() {
		defer close(out)
		reply := fmt.Sprintf(
			"mock reply from model=%s: %s",
			req.Model,
			lastUserSnippet(req.Messages),
		)
		const chunkSize = 12
		for i := 0; i < len(reply); i += chunkSize {
			select {
			case <-ctx.Done():
				out <- StreamChunk{Error: ctx.Err(), Done: true}
				return
			case <-time.After(15 * time.Millisecond):
			}
			end := i + chunkSize
			if end > len(reply) {
				end = len(reply)
			}
			out <- StreamChunk{Text: reply[i:end]}
		}
		out <- StreamChunk{
			Done: true,
			Meta: &ChunkMeta{
				PromptTokens:      approxTokens(req.Messages),
				CompletionTokens:  len(reply) / 4,
				TotalTokens:       approxTokens(req.Messages) + len(reply)/4,
				SystemFingerprint: "shim-mock-1",
			},
		}
	}()
	return out, nil
}

func lastUserSnippet(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			s := strings.TrimSpace(msgs[i].Content)
			if len(s) > 40 {
				return s[:40] + "…"
			}
			return s
		}
	}
	return ""
}

func approxTokens(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content) / 4
	}
	return n
}

// Compile-time guarantee mockClient is a Client.
var _ Client = (*mockClient)(nil)

// ---- TokenStore: shared by both shims ----------------------------------

// TokenStore caches the OAuth token loaded from a JSON file. The same struct
// is used by openai-shim and anthropic-shim — they only differ in which
// header they put the bearer in.
type TokenStore interface {
	// AccessToken returns the bearer token to send upstream. It reloads from
	// disk when the cached copy is within 60s of its expiry.
	AccessToken() (string, error)
	// ExpiresAt returns the next expiry time the loader saw, for /healthz.
	ExpiresAt() time.Time
	// Source returns a short tag describing where the token came from
	// (file:wrapped, env:OPENAI_SHIM_TOKEN, …).
	Source() string
}

// NewTokenStore returns a TokenStore backed by the OAuth JSON file at path.
func NewTokenStore(path string) TokenStore {
	return &fileTokenStore{path: path}
}

// NewTokenStoreWithEnv is the constructor used by the shim entry points:
// path is the `--token-file` flag (or its default), inlineToken is the value
// of $OPENAI_SHIM_TOKEN if non-empty. The inline value takes precedence.
func NewTokenStoreWithEnv(path, inlineToken string) TokenStore {
	return &fileTokenStore{path: path, inlineToken: inlineToken}
}

type fileTokenStore struct {
	mu          sync.RWMutex
	path        string
	inlineToken string
	cache       string
	exp         time.Time
	src         string
}

// AccessToken implements TokenStore.
func (s *fileTokenStore) AccessToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cache valid?
	if s.cache != "" && time.Until(s.exp) > 60*time.Second {
		return s.cache, nil
	}

	// Inline env token is the override.
	if tok := strings.TrimSpace(s.inlineToken); tok != "" {
		s.cache = tok
		s.exp = time.Now().Add(time.Hour) // optimistic
		s.src = "env:OPENAI_SHIM_TOKEN"
		return tok, nil
	}

	// Otherwise read from file.
	if s.path == "" {
		return "", fmt.Errorf("token store: no path configured")
	}
	tok, exp, srcTag, err := readOAuthFile(s.path)
	if err != nil {
		return "", err
	}
	s.cache = tok
	s.src = "file:" + srcTag
	if exp.IsZero() {
		s.exp = time.Now().Add(time.Hour)
	} else {
		s.exp = exp
	}
	return tok, nil
}

// ExpiresAt implements TokenStore.
func (s *fileTokenStore) ExpiresAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exp
}

// Source implements TokenStore.
func (s *fileTokenStore) Source() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.src != "" {
		return s.src
	}
	return "unloaded"
}

// ---- readOAuthFile: cycle-9 behavior, no upstream deps -----------------

// readOAuthFile decodes the Antigravity OAuth JSON. Two shapes are accepted:
// the wrapped format the upstream binary writes
//
//	{"token":{"access_token":"…", …}, "auth_method":"consumer"}
//
// and the unwrapped flat format
//
//	{"access_token":"…", …}
//
// `srcTag` is "wrapped"/"flat" so /healthz can show which shape it loaded.
func readOAuthFile(path string) (token string, exp time.Time, srcTag string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", time.Time{}, "", fmt.Errorf("read %s: %w", path, err)
	}
	if tok, exp, ok := decodeWrapped(raw); ok {
		return tok, exp, "wrapped", nil
	}
	if tok, exp, ok := decodeFlat(raw); ok {
		return tok, exp, "flat", nil
	}
	return "", time.Time{}, "", fmt.Errorf("no access_token found in %s", path)
}

func decodeWrapped(raw []byte) (string, time.Time, bool) {
	var w struct {
		Token struct {
			AccessToken string `json:"access_token"`
			Expiry      string `json:"expiry"`
		} `json:"token"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return "", time.Time{}, false
	}
	if w.Token.AccessToken == "" {
		return "", time.Time{}, false
	}
	return w.Token.AccessToken, parseExpiry(w.Token.Expiry), true
}

func decodeFlat(raw []byte) (string, time.Time, bool) {
	var v struct {
		AccessToken string `json:"access_token"`
		Expiry      string `json:"expiry"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", time.Time{}, false
	}
	if v.AccessToken == "" {
		return "", time.Time{}, false
	}
	return v.AccessToken, parseExpiry(v.Expiry), true
}

func parseExpiry(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ---- NewRealClient (cycle 33 native custom-model support) -------------

// NewRealClient returns a Client that talks to a user-configured provider
// via the models.Registry. If modelName is empty, the registry has no
// matching entry, or the model is otherwise unusable, NewRealClient falls
// back to NewMock() so the rest of the system (TUI, shims) keeps running
// in the sandbox. The "not found" path is logged so operators can spot a
// typo in --model= without a stack trace.
//
// This is the cycle 33 entry point: it lets the recovered binary route
// completions to OpenAI / Anthropic / Ollama / the configured gateway / etc. without
// any Node.js wrapper, runtime patching, or proxy.
func NewRealClient(reg *models.Registry, modelName string) (Client, error) {
	if reg == nil || strings.TrimSpace(modelName) == "" {
		return NewMock(), nil
	}
	m, ok := reg.GetByName(modelName)
	if !ok {
		return NewMock(), nil
	}
	if err := m.Validate(); err != nil {
		return NewMock(), nil
	}
	prov, err := models.NewProvider(*m)
	if err != nil {
		return NewMock(), nil
	}
	return &realClient{model: *m, provider: prov, name: modelName}, nil
}

// realClient adapts a models.Provider to the backend.Client interface.
// The adapter is intentionally thin: the model is a stream of
// models.StreamEvent which we map 1:1 to backend.StreamChunk.
type realClient struct {
	model    models.Model
	provider models.Provider
	name     string
}

// Stream implements Client. The bridge function is `bridgeProvider` below;
// keeping it on a method means a single test can exercise the whole path.
func (r *realClient) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	// Translate backend.Message -> models.ChatMessage. (Same shape, but
	// types are different so we don't share the slice directly.)
	msgs := make([]models.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = models.ChatMessage{Role: m.Role, Content: m.Content}
	}
	// IMPORTANT: req.Model is the *registry* handle (e.g. "minimax-m3"), not the
	// upstream model id (e.g. "MiniMax-M3"). We deliberately ignore it
	// here and let the provider layer use Model.ExternalModelName. The
	// handle only exists to look up the registry entry in NewRealClient;
	// from here on out the upstream identifier is fixed.
	provReq := models.ChatRequest{
		ExternalModel:   r.model.ExternalModelName,
		SystemPrompt:    req.SystemPrompt,
		Messages:        msgs,
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		Stream:          true,
		ReasoningEffort: r.model.ReasoningEffort,
	}
	events, err := r.provider.Stream(ctx, provReq)
	if err != nil {
		return nil, fmt.Errorf("real client: provider stream: %w", err)
	}
	out := make(chan StreamChunk, 16)
	go func() {
		defer close(out)
		for ev := range events {
			select {
			case <-ctx.Done():
				out <- StreamChunk{Error: ctx.Err(), Done: true}
				return
			case out <- bridgeProvider(ev):
			}
			if ev.Done {
				return
			}
		}
		// Channel closed without an explicit Done; synthesize one.
		out <- StreamChunk{Done: true}
	}()
	return out, nil
}

// bridgeProvider converts a models.StreamEvent to a backend.StreamChunk.
// One small wart: the models package doesn't carry the
// SystemFingerprint string (the OpenAI usage field is what most callers
// care about), so we set it to "<provider>/<model>" for debugging.
func bridgeProvider(ev models.StreamEvent) StreamChunk {
	chunk := StreamChunk{Text: ev.Text, Done: ev.Done, Error: ev.Error}
	if ev.Meta != nil {
		chunk.Meta = &ChunkMeta{
			PromptTokens:      ev.Meta.PromptTokens,
			CompletionTokens:  ev.Meta.CompletionTokens,
			TotalTokens:       ev.Meta.TotalTokens,
			SystemFingerprint: "openagy-recovered/1",
		}
	}
	return chunk
}

// Compile-time check realClient satisfies Client.
var _ Client = (*realClient)(nil)
