package geminicode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// userAgentTemplate is the User-Agent the reference sends, byte for byte,
// including the `cl=` build number.
//
// 【一手】 the 1.4.4 CLI's header, recovered from the reference's own
// request capture.
const userAgentTemplate = "antigravity/cli/1.4.4 (aidev_client; os_type=%s; arch=%s; cl=1003595037; auth_method=consumer)"

// UserAgent returns the header value the reference sends.
func UserAgent() string {
	return fmt.Sprintf(userAgentTemplate, runtime.GOOS, runtime.GOARCH)
}

// StreamPath is the RPC the clone calls for a turn's inference. The
// `alt=sse` query is what makes the endpoint answer with an event stream
// instead of one JSON document.
const StreamPath = "/v1internal:streamGenerateContent?alt=sse"

// DefaultProject is the consumer project the reference's loadCodeAssist
// reports ("cloudaicompanionProject":"aicode-consumers").
const DefaultProject = "aicode-consumers"

// Config describes one CloudCode connection.
type Config struct {
	// Endpoint is the host the RPC goes to. The reference resolves it to
	// daily-cloudcode-pa.googleapis.com; a local service can take its
	// place.
	Endpoint string
	// Project is the consumer project loadCodeAssist reports
	// ("aicode-consumers").
	Project string
	// Model is the wire model id. When it is empty the request's own model
	// is used, which lets one client serve several models.
	Model string

	// HTTPClient overrides the transport.
	HTTPClient *http.Client

	// AccessToken overrides the token source. TokenFunc overrides the
	// whole source.
	AccessToken string
	TokenFunc   func(context.Context) (string, error)

	// SessionID is the reference's sessionId member. Zero picks a random
	// one.
	SessionID int64

	// MaxOutputTokens and ThinkingBudget override the per-model defaults.
	MaxOutputTokens int
	ThinkingBudget  int
	ThinkingLevel   string

	// TimeoutSeconds bounds the whole exchange. Zero means no timeout.
	TimeoutSeconds int

	// UserAgent overrides the header. Empty uses the reference's own.
	UserAgent string
}

// Client speaks the CloudCode dialect over one endpoint.
//
// It performs one request per call and reports what happened; the retry
// policy, the idle timeout and the token refresh policy belong to the
// caller.
type Client struct {
	cfg       Config
	http      *http.Client
	token     func(context.Context) (string, error)
	sessionID int64

	mu         sync.Mutex
	signatures map[string]string // tool call id -> thoughtSignature
}

// New builds a client. It never fails: a missing token surfaces on the
// first call, where the caller can show its own sign-in message.
func New(cfg Config) *Client {
	if strings.TrimSpace(cfg.Project) == "" {
		cfg.Project = DefaultProject
	}
	c := &Client{cfg: cfg, signatures: make(map[string]string)}
	c.http = cfg.HTTPClient
	if c.http == nil {
		c.http = &http.Client{}
	}
	if cfg.SessionID != 0 {
		c.sessionID = cfg.SessionID
	} else {
		c.sessionID = NewSessionID()
	}
	switch {
	case cfg.TokenFunc != nil:
		c.token = cfg.TokenFunc
	case strings.TrimSpace(cfg.AccessToken) != "":
		tok := cfg.AccessToken
		c.token = func(context.Context) (string, error) { return tok, nil }
	default:
		c.token = func(context.Context) (string, error) {
			return "", fmt.Errorf("geminicode: no access token configured")
		}
	}
	return c
}

// SessionID returns the session id this client stamps its requests with.
func (c *Client) SessionID() int64 { return c.sessionID }

// Signatures returns a copy of the thought signatures the client collected.
//
// The reference replays a signature on the functionCall part of the next
// turn; Gemini 3 rejects the turn without it.
func (c *Client) Signatures() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.signatures))
	for k, v := range c.signatures {
		out[k] = v
	}
	return out
}

func (c *Client) setSignature(id, signature string) {
	c.mu.Lock()
	c.signatures[id] = signature
	c.mu.Unlock()
}

// Request renders the request body this client would send for a turn. It
// is exported so a caller can measure or log the exact bytes.
func (c *Client) Request(turn Turn) GenerateContentRequest {
	if turn.Model == "" {
		turn.Model = c.cfg.Model
	}
	if turn.Project == "" {
		turn.Project = c.cfg.Project
	}
	if turn.SessionID == "" {
		turn.SessionID = strconv.FormatInt(c.sessionID, 10)
	}
	if turn.MaxOutputTokens == 0 {
		turn.MaxOutputTokens = c.cfg.MaxOutputTokens
	}
	if turn.ThinkingBudget == 0 {
		turn.ThinkingBudget = c.cfg.ThinkingBudget
	}
	if turn.ThinkingLevel == "" {
		turn.ThinkingLevel = c.cfg.ThinkingLevel
	}
	turn.Signatures = c.Signatures()
	return BuildRequest(turn)
}

// Open performs the request and returns the live response.
//
// A non-2xx answer is returned as an *HTTPError with the raw body, so the
// caller can classify it (the reference distinguishes a quota exhaustion
// from an expired sign-in). A transport failure is returned raw, for the
// same reason.
func (c *Client) Open(ctx context.Context, turn Turn) (*http.Response, error) {
	url := strings.TrimRight(c.cfg.Endpoint, "/") + StreamPath
	body, err := json.Marshal(c.Request(turn))
	if err != nil {
		return nil, fmt.Errorf("geminicode: marshal: %w", err)
	}
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("geminicode: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// One reactive retry: the token may have expired between the cache
		// read and the request.
		_ = resp.Body.Close()
		if fresh, terr := c.token(ctx); terr == nil && fresh != token {
			req2, rerr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if rerr != nil {
				return nil, fmt.Errorf("geminicode: build request: %w", rerr)
			}
			req2.Header = req.Header.Clone()
			req2.Header.Set("Authorization", "Bearer "+fresh)
			resp, err = c.http.Do(req2)
			if err != nil {
				return nil, err
			}
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: raw, URL: url}
	}
	return resp, nil
}

// Stream performs one request and decodes the answer.
//
// It is the convenience form of Open plus Decode. A caller that wants to
// retry a dropped connection drives the Decoder itself instead.
func (c *Client) Stream(ctx context.Context, turn Turn) (<-chan Chunk, error) {
	streamCtx := ctx
	var cancel context.CancelFunc
	if c.cfg.TimeoutSeconds > 0 {
		streamCtx, cancel = context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	}
	resp, err := c.Open(streamCtx, turn)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	out := make(chan Chunk, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		if cancel != nil {
			defer cancel()
		}
		dec := NewDecoder(c)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for scanner.Scan() {
			for _, chunk := range dec.Feed(scanner.Text()) {
				if !emit(ctx, out, chunk) {
					return
				}
			}
		}
		chunks, ferr := dec.Finish(scanner.Err())
		for _, chunk := range chunks {
			if !emit(ctx, out, chunk) {
				return
			}
		}
		if ferr != nil {
			emit(ctx, out, Chunk{Error: ferr, Done: true})
		}
	}()
	return out, nil
}

// Decode reads an SSE body and returns the chunks it produced.
//
// The raw error the stream ended with — if any — arrives as the Error of
// the last chunk.
func (c *Client) Decode(ctx context.Context, body io.Reader) <-chan Chunk {
	out := make(chan Chunk, 16)
	go func() {
		defer close(out)
		dec := NewDecoder(c)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for scanner.Scan() {
			for _, chunk := range dec.Feed(scanner.Text()) {
				if !emit(ctx, out, chunk) {
					return
				}
			}
		}
		chunks, err := dec.Finish(scanner.Err())
		for _, chunk := range chunks {
			if !emit(ctx, out, chunk) {
				return
			}
		}
		if err != nil {
			emit(ctx, out, Chunk{Error: err, Done: true})
		}
	}()
	return out
}

// emit sends one chunk, honouring the context's cancellation.
func emit(ctx context.Context, out chan<- Chunk, chunk Chunk) bool {
	select {
	case <-ctx.Done():
		select {
		case out <- Chunk{Error: ctx.Err(), Done: true}:
		case <-ctx.Done():
		}
		return false
	case out <- chunk:
		return true
	}
}

func (c *Client) userAgent() string {
	if ua := strings.TrimSpace(c.cfg.UserAgent); ua != "" {
		return ua
	}
	return UserAgent()
}
