// cloudcode.go — the official Google CloudCode transport.
//
// The clone's default transport is the configured OpenAI-shaped gateway, which
// only serves MiniMax. The user's own Google AI Pro subscription is reachable
// through the same endpoint the reference CLI uses:
//
//	POST https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse
//	Authorization: Bearer <~/.gemini/antigravity-cli/antigravity-oauth-token>
//
// The wire shape lives in the geminicode package, which owns the dialect
// end to end — the request envelope, the SSE frames, the thought-signature
// round trip and the Gemini Schema conversion. This file is the adapter:
// it translates the application's neutral turn into the dialect's turn and
// the dialect's chunks back into the application's chunks, and it owns the
// transport policy (retry a dropped connection, refresh an expired token,
// bound a stalled stream) that the dialect deliberately leaves to its
// caller.
//
// The wire shape was transcribed from live 1.4.3 traffic captured on
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
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/auth"
)

const (
	// CloudCodeEndpoint is the endpoint the reference CLI talks to. The
	// wrapper's prod endpoint (cloudcode-pa.googleapis.com) answers 429 for
	// this account; daily-cloudcode-pa is what `agy` itself resolves to.
	//
	// It is the compiled-in default. The live endpoint is
	// endpoints.Current().CloudCode — the configuration file may point
	// the transport somewhere else (a local service), and this constant
	// stays the name of what it falls back to.
	CloudCodeEndpoint = endpoints.DefaultCloudCode

	// CloudCodeProject is the consumer project loadCodeAssist reports
	// (`"cloudaicompanionProject":"aicode-consumers"`).
	CloudCodeProject = "aicode-consumers"
)

// CloudCodeUserAgent returns the header value the reference sends.
func CloudCodeUserAgent() string {
	return geminicode.UserAgent()
}

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

	// cc is the dialect client. It owns the wire types, the request
	// envelope and the thought signatures the model hands back with its
	// tool calls.
	cc *geminicode.Client
}

// NewCloudCodeWithConfig builds the transport. It never fails: a missing token
// surfaces on the first Stream, where the caller can show the reference's
// sign-in message.
func NewCloudCodeWithConfig(cfg CloudCodeConfig) *CloudCodeClient {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = strings.TrimSpace(os.Getenv("CLOUD_CODE_URL"))
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = endpoints.Current().CloudCode
	}
	if strings.TrimSpace(cfg.Project) == "" {
		cfg.Project = CloudCodeProject
	}
	c := &CloudCodeClient{cfg: cfg}
	c.http = cfg.HTTPClient
	if c.http == nil {
		c.http = &http.Client{}
	}
	if cfg.SessionID != 0 {
		c.sessionID = cfg.SessionID
	} else {
		c.sessionID = geminicode.NewSessionID()
	}
	c.token = initTokenSource(cfg)
	c.cc = geminicode.New(geminicode.Config{
		Endpoint:        cfg.Endpoint,
		Project:         cfg.Project,
		Model:           cfg.Model,
		HTTPClient:      c.http,
		SessionID:       cfg.SessionID,
		MaxOutputTokens: cfg.MaxOutputTokens,
		ThinkingBudget:  cfg.ThinkingBudget,
		TimeoutSeconds:  cfg.TimeoutSeconds,
		// The closure reads c.token on every call, so a test that swaps the
		// token source after the client was built still takes effect.
		TokenFunc: func(ctx context.Context) (string, error) { return c.token(ctx) },
	})
	return c
}

func initTokenSource(cfg CloudCodeConfig) func(context.Context) (string, error) {
	switch {
	case cfg.TokenFunc != nil:
		return cfg.TokenFunc
	case strings.TrimSpace(cfg.AccessToken) != "":
		tok := cfg.AccessToken
		return func(context.Context) (string, error) { return tok, nil }
	default:
		return defaultTokenSource()
	}
}

func defaultTokenSource() func(context.Context) (string, error) {
	cache := auth.NewUnifiedTokenCache()
	preferKeychain := runtime.GOOS == "darwin" && auth.HasKeychainToken()
	var kcMu sync.Mutex
	var kc *auth.AntigravityToken
	return func(ctx context.Context) (string, error) {
		if preferKeychain {
			if tok := readKeychainToken(ctx, &kcMu, &kc); tok != "" {
				return tok, nil
			}
		}
		tok, err := cache.Get(ctx)
		if tok != nil && tok.Token.AccessToken != "" {
			return tok.Token.AccessToken, nil
		}
		if err != nil {
			return "", err
		}
		return "", auth.ErrNoToken
	}
}

// DefaultTokenSource returns the transport's own token source — the one a
// client without an explicit token uses.
//
// It is exported so that a wrapper around the transport (the account
// router) can answer with *exactly* the same credential when its policy
// is off: the wrapper must not become a second implementation of the
// credential cascade, or the two would drift.
func DefaultTokenSource() func(context.Context) (string, error) {
	return defaultTokenSource()
}

func readKeychainToken(ctx context.Context, mu *sync.Mutex, kc **auth.AntigravityToken) string {
	mu.Lock()
	if *kc == nil {
		*kc, _ = auth.LoadKeychainToken()
	}
	tok := *kc
	mu.Unlock()
	if tok == nil || tok.Token.AccessToken == "" {
		return ""
	}
	if updated, _, err := tok.RefreshIfNeeded(ctx); err == nil && updated != nil {
		mu.Lock()
		*kc = updated
		mu.Unlock()
		tok = updated
	}
	return tok.Token.AccessToken
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
	turn := c.turn(req, model)
	streamCtx := ctx
	var cancel context.CancelFunc
	if c.cfg.TimeoutSeconds > 0 {
		streamCtx, cancel = context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	}
	resp, err := c.openWithRetry(streamCtx, turn)
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
		c.runCloudCodeStreamBodyWithRetry(streamCtx, resp, turn, out)
	}()
	return out, nil
}

// turn translates the application's neutral turn into the dialect's turn.
func (c *CloudCodeClient) turn(req StreamRequest, model string) geminicode.Turn {
	turn := geminicode.Turn{
		Model:           model,
		Project:         c.cfg.Project,
		SystemPrompt:    req.SystemPrompt,
		Temperature:     req.Temperature,
		MaxOutputTokens: c.cfg.MaxOutputTokens,
		ThinkingBudget:  c.cfg.ThinkingBudget,
		SessionID:       strconv.FormatInt(c.sessionID, 10),
		// The request id is minted once, so a retry of a dropped connection
		// re-sends the same body.
		RequestID: geminicode.NewRequestID(),
	}
	for _, m := range req.Messages {
		msg := geminicode.TurnMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
			IsError:    m.IsError,
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, geminicode.TurnToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
		for _, img := range m.Images {
			msg.Images = append(msg.Images, geminicode.TurnImage{
				MIMEType: img.MIMEType,
				Data:     img.Data,
			})
		}
		turn.Messages = append(turn.Messages, msg)
	}
	for _, t := range req.Tools {
		turn.Tools = append(turn.Tools, geminicode.TurnTool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	return turn
}

// emitChunk translates one dialect chunk into the application's chunk and
// hands it to the caller. It reports whether the stream may continue.
func emitChunk(ctx context.Context, out chan<- StreamChunk, chunk geminicode.Chunk) bool {
	mapped := StreamChunk{Text: chunk.Text, Done: chunk.Done, Error: chunk.Error}
	if len(chunk.ToolCalls) > 0 {
		mapped.ToolCalls = make([]ToolCall, 0, len(chunk.ToolCalls))
		for _, tc := range chunk.ToolCalls {
			mapped.ToolCalls = append(mapped.ToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
	}
	if chunk.Meta != nil {
		mapped.Meta = &ChunkMeta{
			PromptTokens:     chunk.Meta.PromptTokens,
			CompletionTokens: chunk.Meta.CompletionTokens,
			TotalTokens:      chunk.Meta.TotalTokens,
			ReasoningTokens:  chunk.Meta.ReasoningTokens,
		}
	}
	select {
	case <-ctx.Done():
		out <- StreamChunk{Error: ctx.Err(), Done: true}
		return false
	case out <- mapped:
		return true
	}
}

// WireToolsJSON renders the tool declarations exactly as the CloudCode
// request sends them, so /context's "System tools" row can be
// len(tools JSON)/3 rather than an approximation over the untransformed
// catalogue (cycle 292).
func WireToolsJSON(tools []ToolDecl) []byte {
	turns := make([]geminicode.TurnTool, 0, len(tools))
	for _, t := range tools {
		turns = append(turns, geminicode.TurnTool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	return geminicode.ToolsJSON(turns)
}

// toBackendError maps the dialect's failures onto the application's error
// types, so the caller keeps the classification the headless error reporter
// and the TUI's error surface branch on.
//
//   - an upstream status becomes the application's HTTPError, with the raw
//     body;
//   - a dropped connection becomes its typed ConnectionDroppedError;
//   - a cancellation stays a cancellation;
//   - anything else — the token source's own error — keeps the transport's
//     prefix.
func toBackendError(err error, url string) error {
	if err == nil {
		return nil
	}
	var he *geminicode.HTTPError
	if errors.As(err, &he) {
		return NewHTTPError(he.StatusCode, he.Body, he.URL)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	classified := ClassifyTransportError(err, url)
	if classified != err {
		return classified
	}
	return fmt.Errorf("cloudcode: %w", err)
}
