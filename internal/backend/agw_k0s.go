// Package backend — agw_k0s.go: a direct, registry-free Client that
// talks to https://agw.k0s.io/v1/chat/completions.
//
// Background: the cycle 33 registry-backed path (`NewRealClient` ->
// models.Registry -> models.Provider) is the right answer for a stable
// user-managed config of multiple providers, but it has two rough
// edges in the dev sandbox:
//
//  1. The registry has to be set up by the user (write a models.json
//     file with the agw entry). That's a chicken-and-egg problem when
//     you're just trying to verify the agent loop wires up correctly.
//  2. The 7-provider Provider interface pulls in the entire models
//     package (validate, retry, registry, Anthropic, Google, ...)
//     when all we want is a single OpenAI-shaped HTTPS POST.
//
// This file is the cycle 40 escape hatch: NewAgwK0s() returns a
// backend.Client that just talks to the agw.k0s.io endpoint with the
// documented Bearer "-" dev sentinel. It is selected via
// `OPENAGY_BACKEND=agw` so the rest of the code path (mock, registry
// providers) keeps working unchanged. Default remains mock — the
// `--print` behaviour without OPENAGY_BACKEND is exactly what cycle 33
// shipped.
//
// The wire shape we send / receive is OpenAI-compatible Chat
// Completions with the standard "data: {…}\n\n" SSE framing. We reuse
// the same JSON tags the upstream gateway returns, so the parser is
// robust against the "reasoning_tokens" subfield agw.k0s.io adds
// (the openAI-shape parser in models/provider.go handles the same
// payload, this is the simpler one-file cousin).
package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// AgwTimeoutUnlimited disables the agw client's per-request timeout so the
// caller's context is the only bound. The headless (`-p` / `--prompt`) path
// uses it: the reference changed the default headless timeout from 5 minutes to
// unlimited, so a long agent turn must not be cut off by the transport.
const AgwTimeoutUnlimited = -1

// AgwK0sConfig is the small knob set we expose. Defaults are picked so
// a zero-value struct is usable in the dev sandbox:
//
//	cfg := AgwK0sConfig{} // URL, ExternalModel, APIKey all default
type AgwK0sConfig struct {
	// URL is the full chat completions endpoint. Defaults to
	// "https://agw.k0s.io/v1/chat/completions". Override for local
	// proxies / dev mirrors.
	URL string
	// APIKey is the bearer credential. Defaults to "-" (the public dev
	// sentinel that agw.k0s.io accepts and ignores). Override with
	// OPENAGY_AGW_KEY for a private tenant token.
	APIKey string
	// ExternalModel is the upstream model id (e.g. "MiniMax-M3").
	// Defaults to "MiniMax-M3".
	ExternalModel string
	// TimeoutSeconds is the per-request timeout. 0 uses the 60s default; a
	// negative value (AgwTimeoutUnlimited) disables the client timeout so the
	// caller's context is the only bound.
	TimeoutSeconds int
	// HTTPClient is an optional pre-built client (mostly for tests). If
	// nil we build a default one with the Timeout applied.
	HTTPClient *http.Client
}

// Defaults returns a config with all defaults filled in. Useful as
// `AgwK0sConfig{}.WithDefaults()` style.
func (c AgwK0sConfig) withDefaults() AgwK0sConfig {
	if c.URL == "" {
		c.URL = "https://agw.k0s.io/v1/chat/completions"
		// Allow a local proxy / dev mirror without recompiling, mirroring
		// OPENAGY_AGW_KEY. The parity harnesses use this to point the CLI at
		// a scripted endpoint and exercise the failure paths.
		if u := strings.TrimSpace(os.Getenv("OPENAGY_AGW_URL")); u != "" {
			c.URL = u
		}
	}
	if c.APIKey == "" {
		// Allow env override so a real tenant can be plugged in without
		// recompiling. "-" is the public dev sentinel.
		if k := strings.TrimSpace(os.Getenv("OPENAGY_AGW_KEY")); k != "" {
			c.APIKey = k
		} else {
			c.APIKey = "-"
		}
	}
	if c.ExternalModel == "" {
		c.ExternalModel = "MiniMax-M3"
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 60
	}
	return c
}

// AgwK0sClient is a backend.Client that streams from agw.k0s.io. It is
// safe for concurrent Stream() calls (each call spawns its own
// goroutine and its own HTTP request).
type AgwK0sClient struct {
	cfg     AgwK0sConfig
	httpDo  func(*http.Request) (*http.Response, error) // overridable for tests
	baseURL string                                      // for status / logging
	apiKey  string                                      // for status / logging (sentinel OK to surface)
	model   string
}

// NewAgwK0s returns a Client ready to talk to agw.k0s.io with the dev
// sentinel. Defaults to MiniMax-M3 / 60s timeout. To customise, build
// an AgwK0sConfig and call NewAgwK0sWithConfig.
func NewAgwK0s() Client {
	return NewAgwK0sWithConfig(AgwK0sConfig{})
}

// NewAgwK0sWithConfig returns a Client with the supplied config. Zero
// values trigger defaults; a non-nil HTTPClient is preserved (used by
// tests to inject a stub server).
func NewAgwK0sWithConfig(cfg AgwK0sConfig) Client {
	cfg = cfg.withDefaults()
	c := &AgwK0sClient{
		cfg:     cfg,
		baseURL: cfg.URL,
		apiKey:  cfg.APIKey,
		model:   cfg.ExternalModel,
	}
	if cfg.HTTPClient != nil {
		c.httpDo = cfg.HTTPClient.Do
	} else if cfg.TimeoutSeconds < 0 {
		// Cycle 117: no client-side timeout, so the request is bounded only
		// by the caller's context. The headless path uses this because the
		// reference's default headless timeout is unlimited.
		c.httpDo = (&http.Client{}).Do
	} else {
		c.httpDo = (&http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}).Do
	}
	return c
}

// Endpoint returns a short, loggable description of where this client
// is pointing. Used by /healthz and operator tooling. The API key is
// not echoed (we only return the model + a tag for the key kind).
func (c *AgwK0sClient) Endpoint() string {
	keyTag := "sentinel(-)"
	if c.apiKey != "-" {
		keyTag = "tenant"
	}
	return fmt.Sprintf("agw.k0s.io model=%s key=%s", c.model, keyTag)
}

// AgwConfig returns the resolved configuration this client is using, defaults
// included. It exists so a caller that wants to reach agw by another route can
// reproduce this client's decisions exactly — notably ExternalModel, which
// overrides whatever model the request asked for, and the timeout.
//
// The returned value is a copy; mutating it does not reconfigure the client.
func (c *AgwK0sClient) AgwConfig() AgwK0sConfig { return c.cfg }

// AgwSystemFingerprint is the value this client stamps on every usage record.
// The host is a constant rather than derived from cfg.URL, because that is what
// the recovered client does: a parity harness pointed at a local mirror still
// reports "agw.k0s.io/<model>".
func AgwSystemFingerprint(externalModel string) string {
	return "agw.k0s.io/" + externalModel
}

// AgwUserAgent is the identity this transport presents upstream. It is a
// package constant because more than one route reaches agw now, and they must
// claim the same identity: the server sees the connector, not the caller.
const AgwUserAgent = "openagy-recovered/1 (agw-k0s; cycle 40)"

// agwRequestBody is the OpenAI-compatible shape we POST. Kept private
// because the public StreamRequest is wire-neutral.
type agwRequestBody struct {
	Model         string         `json:"model"`
	Messages      []agwMessage   `json:"messages"`
	Stream        bool           `json:"stream,omitempty"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
	Temperature   float64        `json:"temperature,omitempty"`
	StreamOptions *agwStreamOpts `json:"stream_options,omitempty"`
	// Tools is the OpenAI function-calling advertisement. Empty omits the
	// key entirely so the request is byte-identical to the pre-cycle-97
	// body when no tools are configured.
	Tools []agwTool `json:"tools,omitempty"`
}

type agwStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

// agwTool is one element of the OpenAI `tools` array.
type agwTool struct {
	Type     string         `json:"type"` // always "function"
	Function agwToolFnShape `json:"function"`
}

type agwToolFnShape struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// agwMessage is one conversation turn. The tool fields are populated only on
// assistant messages that requested tools and on `tool` role result messages.
type agwMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCalls  []agwToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

// agwToolCall is one model-emitted tool invocation in OpenAI shape.
type agwToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// agwSSEChunk is the subset of the gateway's chunk shape that we
// actually consume. agw.k0s.io sometimes adds extra fields
// (reasoning_tokens, prompt_tokens_details, service_tier, ...) — we
// ignore them via `json:",omitempty"` on our write side and tolerate
// them on the read side by decoding into a struct with only the
// fields we use.
type agwSSEChunk struct {
	Choices []struct {
		Delta        agwDelta `json:"delta"`
		FinishReason string   `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		// CompletionTokensDetails carries the gateway's reasoning-token
		// breakdown (agw.k0s.io: {"reasoning_tokens": N}). The reference
		// CLI shows this count in its thought step; we surface it on
		// ChunkMeta.ReasoningTokens.
		CompletionTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

// agwDelta is the streaming `delta` object. Tool calls arrive fragmented:
// each SSE frame carries an `index`, an optional `id`/`name` on the first
// fragment, and a chunk of the JSON `arguments` string that must be
// concatenated across frames.
type agwDelta struct {
	Content   string `json:"content"`
	ToolCalls []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// agwToolAccumulator reassembles fragmented tool calls. Calls are keyed by
// their `index` (stable within a response) and returned in index order.
type agwToolAccumulator struct {
	order []int
	byIdx map[int]*agwToolCall
}

func newAGWToolAccumulator() *agwToolAccumulator {
	return &agwToolAccumulator{byIdx: map[int]*agwToolCall{}}
}

func (a *agwToolAccumulator) add(index int, id, name, argsFragment string) {
	tc, ok := a.byIdx[index]
	if !ok {
		tc = &agwToolCall{}
		a.byIdx[index] = tc
		a.order = append(a.order, index)
	}
	if id != "" {
		tc.ID = id
	}
	if name != "" {
		tc.Function.Name = name
	}
	tc.Function.Arguments += argsFragment
}

func (a *agwToolAccumulator) empty() bool { return len(a.order) == 0 }

// list returns the assembled calls in first-seen order, dropping any entry
// that never received a name (a malformed fragment).
func (a *agwToolAccumulator) list() []ToolCall {
	out := make([]ToolCall, 0, len(a.order))
	for _, i := range a.order {
		tc := a.byIdx[i]
		if tc.Function.Name == "" {
			continue
		}
		args := tc.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		out = append(out, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(args),
		})
	}
	return out
}

// Stream implements backend.Client. It translates the wire-neutral
// StreamRequest into an agw.k0s.io OpenAI-shaped POST, parses the
// server-sent event stream, and emits one StreamChunk per non-empty
// delta.content. A terminating chunk with Done=true is emitted when
// the server signals [DONE] (or the stream ends naturally). A usage
// frame, if present, is attached to the terminating chunk as Meta.
//
// Synchronous failure modes (marshal, request setup, HTTP status)
// return an error directly — the channel contract is
// "Stream() returns (channel, nil) for the streaming path, (nil,
// err) for setup-time failures." The synchronous phase is the
// HTTP-request open: it returns *HTTPError directly for 4xx/5xx
// (so cmd/wagyu/api_error.go and the parity tests can classify the
// failure), and it returns *ConnectionDroppedError after the retry
// budget is exhausted.
//
// AGY 1.3.1: a connection drop on the HTTP-request layer (dial
// failure, TLS handshake, broken pipe before the first byte) is
// retried up to MaxConnectionRetries with the schedule in
// gemini_api_key_retry.go. A non-retryable failure (4xx/5xx, marshal
// error) returns the error directly. Mid-stream connection drops
// (a body read failure after the headers arrived) trigger the
// async body retry loop in runAgwStreamBodyWithRetry, which closes
// the dead response and re-opens via openAgwOnce — the caller sees a
// single continuous chunk stream and never a "you cancelled it"
// surface.
//
// The retry budget is split: open-phase drops burn their own
// MaxConnectionRetries, and each body retry is itself an
// open-then-read attempt that may surface further drops from
// openAgwOnce. A pathological "drop-everywhere" cycle therefore
// caps at a few hundred milliseconds of test time and 2 *
// MaxConnectionRetries server attempts worst-case; in the
// realistic AGY 1.3.1 trigger (a single mid-stream drop) the
// caller sees one open + body, one reconnect, one body.
func (c *AgwK0sClient) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	// Translate wire-neutral -> agw body. ExternalModel is the only
	// one we care about: req.Model is whatever the upstream caller
	// put there (often the default "claude-4.5-sonnet" placeholder),
	// NOT the agw-side model id. The cycle 33 NewRealClient path
	// takes the same approach: it ignores req.Model and uses the
	// registry entry's ExternalModelName. We mirror that here so the
	// cycle 40 direct path behaves the same way the cycle 33
	// registry-backed path does. To override the model, build the
	// client with AgwK0sConfig{ExternalModel: "..."}.
	externalModel := c.model
	if strings.TrimSpace(externalModel) == "" {
		// Belt and braces: withDefaults guarantees c.model is
		// non-empty, but if a caller constructs an AgwK0sClient
		// directly (bypassing the constructors) we still want a
		// sensible fallback.
		externalModel = "MiniMax-M3"
	}
	msgs := make([]agwMessage, 0, len(req.Messages)+1)
	if sp := strings.TrimSpace(req.SystemPrompt); sp != "" {
		msgs = append(msgs, agwMessage{Role: "system", Content: sp})
	}
	for _, m := range req.Messages {
		am := agwMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			am.ToolCalls = append(am.ToolCalls, agwToolCallFrom(tc))
		}
		msgs = append(msgs, am)
	}
	// Advertised tools: only present when the caller configured some, so a
	// no-tools request serialises exactly as it did before cycle 97.
	var tools []agwTool
	for _, t := range req.Tools {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		tools = append(tools, agwTool{
			Type: "function",
			Function: agwToolFnShape{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	body := agwRequestBody{
		Model:       externalModel,
		Messages:    msgs,
		Stream:      true,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		StreamOptions: &agwStreamOpts{
			IncludeUsage: true,
		},
		Tools: tools,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("agw: marshal: %w", err)
	}

	// Open the HTTP response synchronously. Connection drops
	// (dial/TLS/broken-pipe before the first byte) are retried
	// transparently by openAgwWithRetry. 4xx/5xx, marshal, and any
	// other non-drop failure returns the error directly so the
	// existing TestAgwK0sErrorStatus contract keeps working.
	resp, err := c.openAgwWithRetry(ctx, raw)
	if err != nil {
		return nil, err
	}

	out := make(chan StreamChunk, 16)
	go func() {
		defer close(out)
		c.runAgwStreamBodyWithRetry(ctx, resp, raw, externalModel, out)
	}()
	return out, nil
}

// openAgwWithRetry performs one synchronous HTTP open and retries on
// connection drops only. It is the boundary between
// pre-stream (synchronous, can return an error) and post-stream
// (asynchronous, only chunk-based errors).
//
// Return contract:
//
//   - response, nil                — 2xx response is open and ready
//     for body reads; caller takes
//     ownership of resp.Body and
//     must Close it.
//   - nil, *HTTPError              — 4xx/5xx from the gateway, surfaced
//     synchronously so the existing
//     TestAgwK0sErrorStatus contract
//     keeps working.
//   - nil, *ConnectionDroppedError — the retry budget is exhausted,
//     surfaced synchronously.
//   - nil, ctx.Err()               — caller cancelled before any
//     attempt could complete.
//   - nil, other error             — non-drop, non-retryable failure
//     (network weirdness the
//     classifier did not match).
func (c *AgwK0sClient) openAgwWithRetry(ctx context.Context, raw []byte) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= MaxConnectionRetries; attempt++ {
		if attempt > 0 {
			delay := NextConnectionRetryDelay(attempt - 1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, err := c.openAgwOnce(ctx, raw)
		if err == nil {
			return resp, nil
		}
		// Non-retryable (4xx/5xx *HTTPError, parse, classify-found-nothing) is
		// returned synchronously without retrying. This is the contract the
		// existing TestAgwK0sErrorStatus relies on.
		if !IsConnectionDropped(err) {
			return nil, err
		}
		lastErr = err
	}
	// Budget exhausted: surface the final error.
	if lastErr == nil {
		lastErr = &ConnectionDroppedError{URL: c.baseURL, Err: errors.New("max retries exceeded")}
	}
	return nil, lastErr
}

// openAgwOnce performs one HTTP attempt: build request, execute,
// surface 4xx/5xx as *HTTPError (reading the body so the
// api_error.go classifier has the upstream message), and classify
// transport-level failures as *ConnectionDroppedError so the
// retry loop can match.
func (c *AgwK0sClient) openAgwOnce(ctx context.Context, raw []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	// Be a good citizen: identify ourselves.
	httpReq.Header.Set("User-Agent", AgwUserAgent)

	resp, err := c.httpDo(httpReq)
	if err != nil {
		return nil, ClassifyTransportError(err, c.baseURL)
	}
	if resp.StatusCode >= 400 {
		rawBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// Keep the raw body so the headless error reporter can classify the
		// failure and extract the user-facing message (see http_error.go).
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: rawBody, URL: c.baseURL}
	}
	return resp, nil
}

// runAgwStreamBodyWithRetry owns the async body read and the
// mid-stream retry loop. Each iteration is one open + body pass:
// the initial response came from openAgwWithRetry (synchronous);
// subsequent responses come from openAgwOnce (single attempt), so
// the total retry attempts end up bounded by MaxConnectionRetries
// for the body's own loop plus whatever openAgwWithRetry spent on
// the initial dial.
//
// Channel contract for this goroutine:
//
//   - On a clean Done (server sent [DONE] or closed cleanly with a
//     non-empty last frame), one terminal chunk with Done=true is
//     emitted.
//   - On a non-drop failure (decode error, ctx cancel inside the
//     scanner loop) one terminal chunk with Done=true and Error set
//     is emitted; this goroutine then returns.
//   - On a connection drop mid-stream, NO terminal chunk is emitted.
//     The retry loop sleeps for the schedule, closes the dead
//     response, re-opens, and continues. The user-visible behavior
//     is a single continuous chunk stream — the partial text from
//     the failed attempt plus the retry's text plus the terminal
//     Done — instead of a "you cancelled it" surface.
//   - When the body's own retry budget is exhausted, a final
//     terminal chunk with Done=true and Error set to the last
//     classified drop is emitted.
func (c *AgwK0sClient) runAgwStreamBodyWithRetry(ctx context.Context, resp *http.Response, raw []byte, externalModel string, out chan<- StreamChunk) {
	// Defensive: if a caller forgot to Close the body, do it on exit.
	defer func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()
	for attempt := 0; ; attempt++ {
		err := c.readAgwBodyOnce(ctx, resp, externalModel, out)
		if err == nil {
			// readAgwBodyOnce already emitted the terminal Done.
			return
		}
		if !IsConnectionDropped(err) {
			// Non-drop: readAgwBodyOnce emitted the terminal
			// Error+Done chunk. Do not emit twice; do not retry.
			return
		}
		if attempt >= MaxConnectionRetries {
			out <- StreamChunk{Error: err, Done: true}
			return
		}
		// Sleep for the schedule, then re-open. The new attempt's
		// body read is independent of the failed response; the
		// retry budget for the body's loop is MaxConnectionRetries.
		_ = resp.Body.Close()
		delay := NextConnectionRetryDelay(attempt)
		select {
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
			return
		case <-time.After(delay):
		}
		newResp, openErr := c.openAgwOnce(ctx, raw)
		if openErr != nil {
			// openErr is classified. A drop here still counts as a
			// retryable failure; we surface it as the next attempt's
			// outcome instead of looping (openAgwOnce has no retry
			// budget of its own — that lives in openAgwWithRetry
			// for the initial open). For a body retry this means:
			// one body failure = one re-open attempt, no chained
			// re-tries inside openAgwOnce.
			out <- StreamChunk{Error: openErr, Done: true}
			return
		}
		resp = newResp
	}
}

// readAgwBodyOnce reads one response body to completion. Returns nil
// on success (the terminal Done chunk has already been emitted);
// returns a classified error otherwise.
//
// Channel terminal-chunk ownership:
//
//   - nil return (success)        — terminal Done already emitted.
//   - non-drop error return       — terminal Error+Done already
//     emitted by the path that
//     produced the failure.
//   - *ConnectionDroppedError     — NO terminal chunk emitted; the
//     retry wrapper in
//     runAgwStreamBodyWithRetry is
//     the single owner of the
//     "we gave up" terminal chunk
//     for connection drops.
func (c *AgwK0sClient) readAgwBodyOnce(ctx context.Context, resp *http.Response, externalModel string, out chan<- StreamChunk) error {
	// Read body until terminal chunk / error with 5-minute stall protection (AGY 1.3.6).
	idleR := NewIdleTimeoutReader(resp.Body, StreamIdleTimeout)
	scanner := bufio.NewScanner(idleR)
	// 1 MiB max line — long completions can blow past the default 64 KiB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var meta *ChunkMeta
	acc := newAGWToolAccumulator()
	// emitToolCalls flushes any assembled tool calls as a single
	// non-terminal chunk. Safe to call when there are none.
	emitToolCalls := func() bool {
		if acc.empty() {
			return true
		}
		select {
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
			return false
		case out <- StreamChunk{ToolCalls: acc.list()}:
			return true
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			if payload == "[DONE]" {
				if !emitToolCalls() {
					return nil
				}
				out <- StreamChunk{Done: true, Meta: meta}
				return nil
			}
			continue
		}
		var chunk agwSSEChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Bad frame — surface as an error chunk and stop. A
			// single bad frame from a hostile / dirty upstream
			// shouldn't be silently dropped, and continuing to
			// read after a decode error is rarely useful (the
			// next frame is likely also malformed).
			select {
			case <-ctx.Done():
				out <- StreamChunk{Error: ctx.Err(), Done: true}
			default:
				out <- StreamChunk{Error: fmt.Errorf("agw: sse decode: %w", err), Done: true}
			}
			return fmt.Errorf("agw: sse decode: %w", err)
		}
		if chunk.Usage != nil {
			meta = &ChunkMeta{
				PromptTokens:      chunk.Usage.PromptTokens,
				CompletionTokens:  chunk.Usage.CompletionTokens,
				TotalTokens:       chunk.Usage.TotalTokens,
				ReasoningTokens:   chunk.Usage.CompletionTokensDetails.ReasoningTokens,
				SystemFingerprint: AgwSystemFingerprint(externalModel),
			}
		}
		for _, choice := range chunk.Choices {
			for _, tc := range choice.Delta.ToolCalls {
				acc.add(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
			}
			if choice.Delta.Content == "" {
				continue
			}
			select {
			case <-ctx.Done():
				out <- StreamChunk{Error: ctx.Err(), Done: true}
				return ctx.Err()
			case out <- StreamChunk{Text: choice.Delta.Content}:
			}
		}
	}
	if err := scanner.Err(); err != nil {
		// AGY 1.3.1: classify the read error. A network-level
		// drop is returned as *ConnectionDroppedError; the retry
		// wrapper is the single owner of the "we gave up" terminal
		// chunk for drops, so we do NOT emit a chunk here for
		// drops. For non-drops (decode, ctx cancel surfacing as a
		// read failure), emit the terminal chunk so the caller's
		// range loop unblocks.
		classified := ClassifyTransportError(fmt.Errorf("agw: sse read: %w", err), c.baseURL)
		if !IsConnectionDropped(classified) {
			out <- StreamChunk{Error: classified, Done: true}
		}
		return classified
	}
	// Server closed the stream without [DONE] — flush any assembled
	// tool calls, then emit a final chunk so the loop's contract
	// (always Done=true at the end) is satisfied.
	if !emitToolCalls() {
		return nil
	}
	out <- StreamChunk{Done: true, Meta: meta}
	return nil
}

// agwToolCallFrom converts a wire-neutral ToolCall into the OpenAI shape we
// echo back on the following assistant message so the model can correlate the
// tool result with its request.
func agwToolCallFrom(tc ToolCall) agwToolCall {
	out := agwToolCall{ID: tc.ID, Type: "function"}
	out.Function.Name = tc.Name
	args := string(tc.Arguments)
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	out.Function.Arguments = args
	return out
}

// truncateAGW is a small helper that keeps error messages short enough
// to fit in a single log line. Mirrors models.truncate but kept inline
// so the file is self-contained.
func truncateAGW(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Compile-time check AgwK0sClient satisfies Client.
var _ Client = (*AgwK0sClient)(nil)
