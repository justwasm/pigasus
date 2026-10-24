// cloudcode.go — the official Google CloudCode transport.
//
// The clone's default transport is the configured OpenAI-shaped gateway, which
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
	"context"
	"encoding/json"
	"fmt"
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
	c.token = initTokenSource(cfg)
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
