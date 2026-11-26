package geminicode

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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
// 【一手】 the 1.5.3 CLI's header, recovered from live agy request capture.
const userAgentTemplate = "antigravity/cli/1.5.3 (aidev_client; os_type=%s; arch=%s; cl=1016078699; auth_method=consumer)"

// UserAgent returns the header value the reference sends.
func UserAgent() string {
	return fmt.Sprintf(userAgentTemplate, runtime.GOOS, runtime.GOARCH)
}

// StreamPath is the RPC the clone calls for a turn's inference. The
// `alt=sse` query is what makes the endpoint answer with an event stream
// instead of one JSON document.
const StreamPath = "/v1internal:streamGenerateContent?alt=sse"

// GenerateContentPath is the unary RPC used for non-streaming calls such as image generation.
const GenerateContentPath = "/v1internal:generateContent"

// FetchAvailableModelsPath is the unary RPC returning available model IDs including imageGenerationModelIds.
const FetchAvailableModelsPath = "/v1internal:fetchAvailableModels"

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

	// Keep discovery local to this connection, project and credential. Only the
	// most recent successful, nonempty catalogue is retained; failures may retry.
	imageModelsKey imageModelsCacheKey
	imageModels    []string
}

type imageModelsCacheKey struct {
	endpoint string
	project  string
	token    [sha256.Size]byte
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
	return c.OpenAt(ctx, turn, c.cfg.Endpoint)
}

// OpenAt performs the request against a specific endpoint base URL and returns the live response.
func (c *Client) OpenAt(ctx context.Context, turn Turn, endpoint string) (*http.Response, error) {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = c.cfg.Endpoint
	}
	url := strings.TrimRight(endpoint, "/") + StreamPath
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
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: raw, URL: url, Header: resp.Header}
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

func (c *Client) postUnary(ctx context.Context, endpoint, path string, payload any) ([]byte, error) {
	if c.cfg.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	raw, _, err := c.postUnaryWithToken(ctx, endpoint, path, payload, token)
	return raw, err
}

// The returned token identifies the credential actually used after a reactive refresh.
func (c *Client) postUnaryWithToken(ctx context.Context, endpoint, path string, payload any, token string) ([]byte, string, error) {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = c.cfg.Endpoint
	}
	url := strings.TrimRight(endpoint, "/") + path
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, token, fmt.Errorf("geminicode: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, token, fmt.Errorf("geminicode: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, token, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		if fresh, terr := c.token(ctx); terr == nil && fresh != token {
			_ = resp.Body.Close()
			req2, rerr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if rerr != nil {
				return nil, token, fmt.Errorf("geminicode: build request: %w", rerr)
			}
			req2.Header = req.Header.Clone()
			token = fresh
			req2.Header.Set("Authorization", "Bearer "+token)
			resp, err = c.http.Do(req2)
			if err != nil {
				return nil, token, err
			}
		}
	}
	defer resp.Body.Close()
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, token, &HTTPError{StatusCode: resp.StatusCode, Body: raw, URL: url, Header: resp.Header}
	}
	if rerr != nil {
		return nil, token, rerr
	}
	if len(raw) > 32<<20 {
		return nil, token, fmt.Errorf("geminicode: unary response exceeds 32 MiB")
	}
	return raw, token, nil
}

// FetchAvailableModels resolves /v1internal:fetchAvailableModels on the configured endpoint.
func (c *Client) FetchAvailableModels(ctx context.Context, project string) (*FetchAvailableModelsResponse, error) {
	return c.FetchAvailableModelsAt(ctx, project, c.cfg.Endpoint)
}

// FetchAvailableModelsAt resolves /v1internal:fetchAvailableModels on a specific endpoint.
// Nonempty results are cached per client, endpoint, project and access token.
func (c *Client) FetchAvailableModelsAt(ctx context.Context, project, endpoint string) (*FetchAvailableModelsResponse, error) {
	if c.cfg.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	project = strings.TrimSpace(project)
	if project == "" {
		project = c.cfg.Project
	}
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		endpoint = strings.TrimRight(strings.TrimSpace(c.cfg.Endpoint), "/")
	}
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	key := imageModelsCacheKey{endpoint: endpoint, project: project, token: sha256.Sum256([]byte(token))}
	c.mu.Lock()
	if c.imageModelsKey == key && len(c.imageModels) > 0 {
		out := &FetchAvailableModelsResponse{ImageGenerationModelIDs: append([]string(nil), c.imageModels...)}
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()
	raw, usedToken, err := c.postUnaryWithToken(ctx, endpoint, FetchAvailableModelsPath, FetchAvailableModelsRequest{Project: project}, token)
	if err != nil {
		return nil, err
	}
	key.token = sha256.Sum256([]byte(usedToken))
	var out FetchAvailableModelsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("geminicode: decode fetchAvailableModels: %w", err)
	}
	for _, id := range out.ImageGenerationModelIDs {
		if strings.TrimSpace(id) != "" {
			c.mu.Lock()
			c.imageModelsKey = key
			c.imageModels = append([]string(nil), out.ImageGenerationModelIDs...)
			c.mu.Unlock()
			break
		}
	}
	return &out, nil
}

// GenerateImage performs a non-streaming /v1internal:generateContent image generation call.
func (c *Client) GenerateImage(ctx context.Context, req ImageGenerationRequest) (ImageGenerationResult, error) {
	return c.GenerateImageAt(ctx, req, c.cfg.Endpoint)
}

// GenerateImageAt performs a non-streaming /v1internal:generateContent image generation call against endpoint.
func (c *Client) GenerateImageAt(ctx context.Context, req ImageGenerationRequest, endpoint string) (ImageGenerationResult, error) {
	if strings.TrimSpace(req.Project) == "" {
		req.Project = c.cfg.Project
	}
	wireReq, err := BuildImageGenerationRequest(req)
	if err != nil {
		return ImageGenerationResult{}, err
	}
	raw, err := c.postUnary(ctx, endpoint, GenerateContentPath, wireReq)
	if err != nil {
		return ImageGenerationResult{}, err
	}
	var envelope struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ImageGenerationResult{}, fmt.Errorf("geminicode: decode generateContent: %w", err)
	}
	if len(envelope.Response) > 0 {
		raw = envelope.Response
	}
	var resp *GenerateContentResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ImageGenerationResult{}, fmt.Errorf("geminicode: decode generateContent: %w", err)
	}
	return ExtractGeneratedImage(resp)
}
