// cloudcode.go — the official Google CloudCode transport.
//
// The clone's default transport is the agw.k0s.io OpenAI-shaped gateway, which
// only serves MiniMax. The user's own Google AI Pro subscription is reachable
// through the same endpoint the reference CLI uses:
//
//	POST https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse
//	Authorization: Bearer <~/.gemini/antigravity-cli/antigravity-oauth-token>
//
// The wire shape below is transcribed from live 1.4.3 traffic captured on
// 2026-10-20 (a successful Gemini 3.8 Flash turn, a successful Claude Sonnet
// 4.6 turn, and a tool-call round trip). Three details are load-bearing:
//
//   - the body is the wrapped envelope `{project, requestId, request, model,
//     userAgent, requestType}` — not a bare GenerateContentRequest;
//   - every frame is `data: {"response": {…}, "traceId": …}`;
//   - the reference echoes a tool result as a functionResponse inside a
//     *model*-role content, and replays the model's thoughtSignature on the
//     functionCall part. We mirror both.
package backend

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

const (
	// CloudCodeEndpoint is the endpoint the reference CLI talks to. The
	// wrapper's prod endpoint (cloudcode-pa.googleapis.com) answers 429 for
	// this account; daily-cloudcode-pa is what `agy` itself resolves to.
	CloudCodeEndpoint = "https://daily-cloudcode-pa.googleapis.com"

	// CloudCodeProject is the consumer project loadCodeAssist reports
	// (`"cloudaicompanionProject":"aicode-consumers"`).
	CloudCodeProject = "aicode-consumers"

	// cloudCodeUserAgent is byte-identical to the 1.4.4 CLI's header,
	// including the `cl=` build number.
	cloudCodeUserAgent = "antigravity/cli/1.4.4 (aidev_client; os_type=%s; arch=%s; cl=1003595037; auth_method=consumer)"
)

// CloudCodeConfig describes one official-model transport.
//
// Model is the wire id ("gemini-3.8-flash-high", "claude-sonnet-4-6"). When it
// is empty the request's own Model field is used, which lets a routing client
// share one CloudCodeClient across models.
type CloudCodeConfig struct {
	Endpoint string
	Project  string
	Model    string

	// TimeoutSeconds bounds the whole exchange. 0 means no timeout, which is
	// what the interactive and headless paths want; stream stalls are still
	// bounded by the caller.
	TimeoutSeconds int

	// AccessToken overrides the on-disk token (tests). TokenFunc overrides the
	// whole token source.
	AccessToken string
	TokenFunc   func(context.Context) (string, error)

	// HTTPClient overrides the transport (tests).
	HTTPClient *http.Client

	// MaxOutputTokens / ThinkingBudget override the per-model defaults below.
	MaxOutputTokens int
	ThinkingBudget  int

	// SessionID is the reference's sessionId field. 0 picks a random one.
	SessionID int64
}

// CloudCodeClient implements Client over the official CloudCode endpoint.
type CloudCodeClient struct {
	cfg       CloudCodeConfig
	http      *http.Client
	token     func(context.Context) (string, error)
	sessionID int64

	mu         sync.Mutex
	signatures map[string]string // tool call id -> thoughtSignature
}

// NewCloudCodeWithConfig builds the transport. It never fails: a missing token
// surfaces on the first Stream, where the caller can show the reference's
// sign-in message.
func NewCloudCodeWithConfig(cfg CloudCodeConfig) *CloudCodeClient {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		// The reference honours CLOUD_CODE_URL; mirror it so the endpoint can
		// be pointed at a capture proxy without patching the binary.
		cfg.Endpoint = strings.TrimSpace(os.Getenv("CLOUD_CODE_URL"))
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = CloudCodeEndpoint
	}
	if strings.TrimSpace(cfg.Project) == "" {
		cfg.Project = CloudCodeProject
	}
	c := &CloudCodeClient{
		cfg:        cfg,
		signatures: make(map[string]string),
	}
	c.http = cfg.HTTPClient
	if c.http == nil {
		c.http = &http.Client{}
	}
	if cfg.SessionID != 0 {
		c.sessionID = cfg.SessionID
	} else {
		c.sessionID = randomSessionID()
	}
	switch {
	case cfg.TokenFunc != nil:
		c.token = cfg.TokenFunc
	case strings.TrimSpace(cfg.AccessToken) != "":
		tok := cfg.AccessToken
		c.token = func(context.Context) (string, error) { return tok, nil }
	default:
		// macOS: the reference stores the live token in the login
		// keychain ("gemini"/"antigravity") and refreshes it there. The
		// on-disk copies can be stale or test fixtures, so prefer the
		// keychain when one exists, exactly like agy does.
		cache := auth.NewUnifiedTokenCache()
		preferKeychain := runtime.GOOS == "darwin" && auth.HasKeychainToken()
		var kcMu sync.Mutex
		var kc *auth.AntigravityToken
		c.token = func(ctx context.Context) (string, error) {
			if preferKeychain {
				kcMu.Lock()
				if kc == nil {
					kc, _ = auth.LoadKeychainToken()
				}
				tok := kc
				kcMu.Unlock()
				if tok != nil && tok.Token.AccessToken != "" {
					if updated, _, err := tok.RefreshIfNeeded(ctx); err == nil && updated != nil {
						kcMu.Lock()
						kc = updated
						kcMu.Unlock()
						tok = updated
					}
					return tok.Token.AccessToken, nil
				}
			}
			tok, err := cache.Get(ctx)
			// A failed refresh still leaves the stale access token in
			// hand; let the server decide instead of refusing the turn.
			if tok != nil && tok.Token.AccessToken != "" {
				return tok.Token.AccessToken, nil
			}
			if err != nil {
				return "", err
			}
			return "", auth.ErrNoToken
		}
	}
	return c
}

// CloudCodeUserAgent returns the header value the reference sends.
func CloudCodeUserAgent() string {
	return fmt.Sprintf(cloudCodeUserAgent, runtime.GOOS, runtime.GOARCH)
}

// Model returns the wire model this client was built for.
func (c *CloudCodeClient) Model() string { return c.cfg.Model }

// Stream implements Client. HTTP failures are returned synchronously so the
// caller keeps the *backend.HTTPError classification; everything after the
// response opens arrives on the channel, ending in exactly one Done chunk.
func (c *CloudCodeClient) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	model := strings.TrimSpace(c.cfg.Model)
	if model == "" {
		model = strings.TrimSpace(req.Model)
	}
	if model == "" {
		return nil, fmt.Errorf("cloudcode: empty model")
	}
	token, err := c.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloudcode: %w", err)
	}
	body, err := json.Marshal(c.buildEnvelope(req, model))
	if err != nil {
		return nil, fmt.Errorf("cloudcode: marshal: %w", err)
	}
	streamCtx := ctx
	var cancel context.CancelFunc
	if c.cfg.TimeoutSeconds > 0 {
		streamCtx, cancel = context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	}
	resp, err := c.openWithRetry(streamCtx, body, token)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	out := make(chan StreamChunk, 16)
	go func() {
		defer close(out)
		if cancel != nil {
			defer cancel()
		}
		c.runCloudCodeStreamBodyWithRetry(streamCtx, resp, body, token, out)
	}()
	return out, nil
}

func (c *CloudCodeClient) openWithRetry(ctx context.Context, body []byte, token string) (*http.Response, error) {
	for attempt := 0; attempt <= MaxConnectionRetries; attempt++ {
		resp, err := c.open(ctx, body, token)
		if err == nil {
			return resp, nil
		}
		if !IsConnectionDropped(err) || attempt >= MaxConnectionRetries {
			return nil, err
		}
		delay := NextConnectionRetryDelay(attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, &ConnectionDroppedError{Err: ErrConnectionDropped, URL: c.cfg.Endpoint}
}

func (c *CloudCodeClient) runCloudCodeStreamBodyWithRetry(ctx context.Context, resp *http.Response, body []byte, token string, out chan<- StreamChunk) {
	defer func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()
	for attempt := 0; ; attempt++ {
		err := c.readBodyOnce(ctx, resp.Body, out)
		if err == nil {
			return
		}
		if !IsConnectionDropped(err) {
			return
		}
		if attempt >= MaxConnectionRetries {
			out <- StreamChunk{Error: err, Done: true}
			return
		}
		_ = resp.Body.Close()
		delay := NextConnectionRetryDelay(attempt)
		select {
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
			return
		case <-time.After(delay):
		}
		freshToken := token
		if t, terr := c.token(ctx); terr == nil && t != "" {
			freshToken = t
		}
		newResp, openErr := c.open(ctx, body, freshToken)
		if openErr != nil {
			if !IsConnectionDropped(openErr) {
				out <- StreamChunk{Error: openErr, Done: true}
				return
			}
			resp = nil
			continue
		}
		resp = newResp
	}
}

func (c *CloudCodeClient) open(ctx context.Context, body []byte, token string) (*http.Response, error) {
	url := strings.TrimRight(c.cfg.Endpoint, "/") + "/v1internal:streamGenerateContent?alt=sse"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cloudcode: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", CloudCodeUserAgent())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, ClassifyTransportError(err, url)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// One reactive retry: the token may have expired between the cache
		// read and the request. The unified cache refreshes inside Get.
		_ = resp.Body.Close()
		if fresh, terr := c.token(ctx); terr == nil && fresh != token {
			req2, rerr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if rerr != nil {
				return nil, fmt.Errorf("cloudcode: build request: %w", rerr)
			}
			req2.Header = req.Header.Clone()
			req2.Header.Set("Authorization", "Bearer "+fresh)
			resp, err = c.http.Do(req2)
			if err != nil {
				return nil, ClassifyTransportError(err, url)
			}
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, NewHTTPError(resp.StatusCode, raw, url)
	}
	return resp, nil
}

// ---- request envelope -------------------------------------------------------

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
// ("Duplicate function declaration found: …"); the agw gateway tolerated the
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
	system := strings.TrimSpace(req.SystemPrompt)
	var contents []ccContent
	for _, m := range req.Messages {
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
			parts := make([]ccPart, 0, len(m.ToolCalls)+1)
			if m.Content != "" {
				parts = append(parts, ccPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				parts = append(parts, c.callPart(tc))
			}
			if len(parts) > 0 {
				contents = append(contents, ccContent{Role: "model", Parts: parts})
			}
		case "tool":
			// The reference puts the response in a model-role content of its
			// own — one content per result (capture 1792446067516-052).
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
	if isClaudeModel(model) {
		inner.Labels = map[string]string{
			"used_claude":              "true",
			"used_claude_conservative": "true",
			"used_non_gemini_model":    "true",
		}
	} else if !strings.HasPrefix(model, "gemini") {
		inner.Labels = map[string]string{"used_non_gemini_model": "true"}
	}
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

// ---- response stream -------------------------------------------------------

type ccFrame struct {
	Response json.RawMessage `json:"response"`
}

type ccResponse struct {
	Candidates []struct {
		Content struct {
			Role  string   `json:"role"`
			Parts []ccPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

func (c *CloudCodeClient) readBodyOnce(ctx context.Context, body io.ReadCloser, out chan<- StreamChunk) error {
	idleR := NewIdleTimeoutReader(body, StreamIdleTimeout)
	scanner := bufio.NewScanner(idleR)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var calls []ToolCall
	seenCalls := map[string]bool{}
	var meta ChunkMeta
	var finished bool
	var sawFinish bool
	// CloudCode streams reasoning as separate `thought:true` parts, while the
	// clone's transcript pipeline (internal/agent.Sanitizer) recognises the
	// reference's inline `<think>…</think>` convention — that is how MiniMax's
	// gateway exposes reasoning. Wrap the thought parts in the same markers so
	// the TUI renders "Thought for …" and print mode still strips them.
	// internal/agent imports this package, so the constants are literals here.
	const thinkOpen, thinkClose = "<think>", "</think>"
	inThought := false
	closeThought := func() {
		if !inThought {
			return
		}
		inThought = false
		select {
		case <-ctx.Done():
		case out <- StreamChunk{Text: thinkClose}:
		}
	}

	emit := func(chunk StreamChunk) bool {
		select {
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
			return false
		case out <- chunk:
			return true
		}
	}
	final := func() {
		if finished {
			return
		}
		closeThought()
		finished = true
		chunk := StreamChunk{Done: true, ToolCalls: calls}
		if meta != (ChunkMeta{}) {
			chunk.Meta = &meta
		}
		select {
		case out <- chunk:
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
		}
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ")]}'") {
			continue
		}
		payload := line
		if strings.HasPrefix(payload, "data:") {
			payload = strings.TrimSpace(strings.TrimPrefix(payload, "data:"))
		}
		if payload == "" || payload == "[DONE]" {
			if payload == "[DONE]" {
				final()
				return nil
			}
			continue
		}
		resp, ok := parseCloudCodePayload(payload)
		if !ok {
			continue
		}
		for _, cand := range resp.Candidates {
			for _, part := range cand.Content.Parts {
				if part.FunctionCall != nil {
					closeThought()
					call := ToolCall{Name: part.FunctionCall.Name, Arguments: part.FunctionCall.Args}
					call.ID = part.FunctionCall.ID
					if call.ID == "" {
						call.ID = fmt.Sprintf("call_%d", len(calls)+1)
					}
					if len(call.Arguments) == 0 || !json.Valid(call.Arguments) {
						call.Arguments = json.RawMessage(`{}`)
					}
					if seenCalls[call.ID] {
						continue
					}
					seenCalls[call.ID] = true
					calls = append(calls, call)
					if part.ThoughtSignature != "" {
						c.mu.Lock()
						c.signatures[call.ID] = part.ThoughtSignature
						c.mu.Unlock()
					}
					continue
				}
				// Thought parts are reasoning, wrapped for the transcript's
				// `<think>` sanitizer (see the note above).
				if part.Thought {
					if part.Text == "" {
						continue
					}
					if !inThought {
						inThought = true
						if !emit(StreamChunk{Text: thinkOpen}) {
							return nil
						}
					}
					if !emit(StreamChunk{Text: part.Text}) {
						return nil
					}
					continue
				}
				if part.Text == "" {
					continue
				}
				closeThought()
				if !emit(StreamChunk{Text: part.Text}) {
					return nil
				}
			}
			if cand.FinishReason != "" {
				sawFinish = true
			}
		}
		if u := resp.UsageMetadata; u.TotalTokenCount > 0 {
			meta = ChunkMeta{
				PromptTokens:     u.PromptTokenCount,
				CompletionTokens: u.CandidatesTokenCount,
				TotalTokens:      u.TotalTokenCount,
				ReasoningTokens:  u.ThoughtsTokenCount,
			}
		}
		if sawFinish {
			final()
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		closeThought()
		classified := ClassifyTransportError(err, c.cfg.Endpoint)
		if IsConnectionDropped(classified) {
			return classified
		}
		select {
		case out <- StreamChunk{Error: classified, Done: true}:
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
		}
		return classified
	}
	if !sawFinish && len(calls) == 0 {
		closeThought()
		return &ConnectionDroppedError{Err: io.ErrUnexpectedEOF, URL: c.cfg.Endpoint}
	}
	final()
	return nil
}

// parseCloudCodePayload accepts both the wrapped frame the CloudCode endpoint
// sends and a bare inner response (some intermediaries strip the wrapper).
func parseCloudCodePayload(payload string) (ccResponse, bool) {
	var frame ccFrame
	if err := json.Unmarshal([]byte(payload), &frame); err == nil && len(frame.Response) > 0 {
		var out ccResponse
		if err := json.Unmarshal(frame.Response, &out); err == nil {
			return out, true
		}
	}
	var out ccResponse
	if err := json.Unmarshal([]byte(payload), &out); err == nil && (len(out.Candidates) > 0 || out.UsageMetadata.TotalTokenCount > 0) {
		return out, true
	}
	return ccResponse{}, false
}

// ---- helpers ---------------------------------------------------------------

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
