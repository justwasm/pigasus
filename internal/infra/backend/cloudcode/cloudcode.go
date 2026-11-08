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
package cloudcode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/auth/credential"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

const (
	// CloudCodeProject is the consumer project loadCodeAssist reports
	// (`"cloudaicompanionProject":"aicode-consumers"`).
	CloudCodeProject = "aicode-consumers"
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

// CloudCodeClient implements backend.Client over the official CloudCode endpoint.
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
		return credential.DefaultTokenSource()
	}
}

// Model returns the wire model this client was built for.
func (c *CloudCodeClient) Model() string { return c.cfg.Model }

// Stream implements backend.Client. HTTP failures are returned synchronously so the
// caller keeps the *apierror.HTTPError classification; everything after the
// response opens arrives on the channel, ending in exactly one Done chunk.
func (c *CloudCodeClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
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
	out := make(chan backend.StreamChunk, 16)
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
func (c *CloudCodeClient) turn(req backend.StreamRequest, model string) geminicode.Turn {
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
		content := m.Content
		if m.WireContent != "" {
			content = m.WireContent
		} else if m.Role == "user" && !strings.HasPrefix(strings.TrimSpace(content), "<USER_REQUEST>") && !strings.HasPrefix(content, "Step Id:") {
			content = backend.WrapUserContent(content, model, false, time.Now())
		}
		msg := geminicode.TurnMessage{
			Role:       m.Role,
			Content:    content,
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
func emitChunk(ctx context.Context, out chan<- backend.StreamChunk, chunk geminicode.Chunk) bool {
	chunkErr := chunk.Error
	if chunkErr != nil {
		chunkErr = toBackendError(chunkErr, "")
	}
	mapped := backend.StreamChunk{Text: chunk.Text, Done: chunk.Done, Error: chunkErr}
	if len(chunk.ToolCalls) > 0 {
		mapped.ToolCalls = make([]backend.ToolCall, 0, len(chunk.ToolCalls))
		for _, tc := range chunk.ToolCalls {
			mapped.ToolCalls = append(mapped.ToolCalls, backend.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
	}
	if chunk.Meta != nil {
		mapped.Meta = &backend.ChunkMeta{
			PromptTokens:     chunk.Meta.PromptTokens,
			CompletionTokens: chunk.Meta.CompletionTokens,
			TotalTokens:      chunk.Meta.TotalTokens,
			ReasoningTokens:  chunk.Meta.ReasoningTokens,
			CachedTokens:     chunk.Meta.CachedContentTokens,
		}
	}
	select {
	case <-ctx.Done():
		out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
		return false
	case out <- mapped:
		return true
	}
}

// WireToolsJSON renders the tool declarations exactly as the CloudCode
// request sends them, so /context's "System tools" row can be
// len(tools JSON)/3 rather than an approximation over the untransformed
// catalogue (cycle 292).
func WireToolsJSON(tools []backend.ToolDecl) []byte {
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
//   - a dropped connection becomes its typed retry.ConnectionDroppedError;
//   - a cancellation stays a cancellation;
//   - anything else — the token source's own error — keeps the transport's
//     prefix.
func toBackendError(err error, url string) error {
	if err == nil {
		return nil
	}
	var he *geminicode.HTTPError
	if errors.As(err, &he) {
		hErr := apierror.NewHTTPError(he.StatusCode, he.Body, he.URL)
		hErr.Header = he.Header
		return hErr
	}
	var se *geminicode.StreamError
	if errors.As(err, &se) {
		code := se.Code
		if code == 0 {
			code = 500
		}
		return apierror.NewHTTPError(code, []byte(se.Error()), url)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	classified := retry.ClassifyTransportError(err, url)
	if classified != err {
		return classified
	}
	return fmt.Errorf("cloudcode: %w", err)
}
