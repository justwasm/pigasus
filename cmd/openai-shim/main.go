// Command openai-shim is an HTTP front-end that translates OpenAI-style
// requests (`POST /v1/chat/completions`, `GET /v1/models`) into a stream of
// chunks from a backend.Client. The point of the shim is that any
// OpenAI-compatible SDK (curl, openai-python, openai-node, open-webui, …)
	// can talk to a backend that only knows how to stream over gRPC.
//
// Architecture
// ----------------
//	HTTP request
//	  │
//	  ▼
//	translateOpenAI() ─→ agent.Loop ─→ backend.Client.Stream
//	                                            │
//	                                            ▼
//	                                      StreamRequest{…}
//	                                            │
//	                                        upstream call
//	                                            │
//	                                            ▼
//	                                    <-chan StreamChunk
//	                                            │
//	  ┌─────────────────────────────────────────┘
//	  ▼
//	openAIChunkHandler ─→ http.ResponseWriter (SSE or JSON)
//
// The same translation tables are reused in cmd/anthropic-shim (which
// additionally remaps OpenAI IDs to Anthropic IDs).
//
// Sandbox default backend is internal/backend.NewMock() because cloudaicompanion
// is unreachable from this pod. RealClient would be wired in when a licensed
// token is available — the wire format is identical.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
)

// ---- Static configuration ----------------------------------------------

// knownModels is the model list served from /v1/models and accepted on the
// chat completions endpoint. Order matches the cycle 9 binary.
var knownModels = []modelInfo{
	{ID: "claude-sonnet-4-6", OwnedBy: "anthropic", Object: "model", Created: 1_700_000_000},
	{ID: "claude-opus-4-6-thinking", OwnedBy: "anthropic", Object: "model", Created: 1_700_000_000},
	{ID: "gemini-3-flash", OwnedBy: "google", Object: "model", Created: 1_700_000_000},
	{ID: "gpt-oss-120b-medium", OwnedBy: "openai", Object: "model", Created: 1_700_000_000},
}

type modelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ---- main + flag/env plumbing ------------------------------------------

func main() {
	// CLI flags; each one also accepts an env var override (cycle 9 design).
	addr := flag.String("addr", envOr("OPENAI_SHIM_ADDR", ":19999"), "HTTP listen address (env OPENAI_SHIM_ADDR)")
	project := flag.String("project", envOr("OPENAI_SHIM_PROJECT", "cloudaicompanion-bucket-1307"), "Antigravity cloudaicompanion project ID (env OPENAI_SHIM_PROJECT)")
	proxyURL := flag.String("proxy-url", envOr("OPENAI_SHIM_PROXY", "socks5h://127.0.0.1:2081"), "SOCKS5/HTTP proxy URL (env OPENAI_SHIM_PROXY)")
	publicURL := flag.String("public-url", envOr("OPENAI_SHIM_PUBLIC_URL", ""), "Public URL printed on the root page (env OPENAI_SHIM_PUBLIC_URL)")
	tokenFile := flag.String("token-file", envOr("OPENAI_SHIM_TOKEN_FILE", "/workspace/.home/.gemini/antigravity-cli/antigravity-oauth-token"), "OAuth token JSON file (env OPENAI_SHIM_TOKEN_FILE)")
	upstream := flag.String("upstream", envOr("OPENAI_SHIM_UPSTREAM", "https://daily-cloudcode-pa.googleapis.com/v1internal:generateChat"), "Antigravity generateChat URL (env OPENAI_SHIM_UPSTREAM)")
	flag.Parse()

	// Boot-time configuration logging.
	version := "openai-shim-public/1.0.0"
	userAgent := "openai-shim-public/1.0.0 (sandbox-shim)"
	authMethod := "consumer"

	// Honor $OPENAI_SHIM_TOKEN as an inline token override.
	inlineToken := strings.TrimSpace(os.Getenv("OPENAI_SHIM_TOKEN"))
	tokens := backend.NewTokenStoreWithEnv(*tokenFile, inlineToken)
	if tok, err := tokens.AccessToken(); err != nil {
		log.Printf("[boot] token load failed: %v (continuing — /healthz will report token:false)", err)
	} else {
		log.Printf("[boot] token loaded (%d bytes, src=%s, expires=%s)", len(tok), tokens.Source(), tokens.ExpiresAt().Format(time.RFC3339))
	}

	// Backend wiring. NewMock is the default; a RealClient could replace it
	// when a licensed token + network path to cloudaicompanion are present.
	be := newBackendClient(*proxyURL, *project, *upstream, userAgent, authMethod, tokens)

	// Per-request SSE writer helper closure; captures nothing.
	svc := &server{
		version:   version,
		userAgent: userAgent,
		tokens:    tokens,
		backend:   be,
		rootMeta: rootMeta{
			Service:    version,
			Upstream:   *upstream,
			Proxy:      *proxyURL,
			Project:    *project,
			PublicURL:  ifEmpty(*publicURL, "http://"+*addr),
			ListenAddr: *addr,
		},
	}

	// Warn about the TUIC proxy being offline (cycle 9 behavior — useful for
	// the operator who hasn't started sing-box yet).
	if !proxyReachable(*proxyURL) {
		log.Printf("[boot] TUIC proxy not reachable at %s (continue; ss will use direct egress or fail)", *proxyURL)
	}

	// HTTP wiring.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", svc.handleChatCompletions)
	mux.HandleFunc("/v1/models", svc.handleModels)
	mux.HandleFunc("/healthz", svc.handleHealth)
	mux.HandleFunc("/", svc.routeRoot)

	handler := svc.corsMiddleware(svc.loggingMiddleware(mux))

	// Final boot log.
	log.Printf("[boot] openai-shim-public/1.0.0 listening on %s", *addr)
	log.Printf("[boot] upstream = %s", *upstream)
	log.Printf("[boot] proxy    = %s", *proxyURL)
	log.Printf("[boot] project  = %s", *project)
	log.Printf("[boot] public   = %s", ifEmpty(*publicURL, "http://"+*addr))
	log.Printf("[boot] models   = %s", joinModelIDs(knownModels))

	// Server lifecycle.
	srvHTTP := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srvHTTP.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
}

// ---- server: shared state across handlers -----------------------------

type server struct {
	version   string
	userAgent string
	tokens    backend.TokenStore
	backend   backend.Client

	rootMeta rootMeta
}

type rootMeta struct {
	Service    string
	Upstream   string
	Proxy      string
	Project    string
	PublicURL  string
	ListenAddr string
}

// ---- handlers ---------------------------------------------------------

// routeRoot dispatches `/` (the self-documenting JSON
// manifest) and 404s anything else.
func (s *server) routeRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "") {
		s.handleRoot(w, r)
		return
	}
	if r.Method == http.MethodOptions {
		// CORS preflight falls through the same OPTIONS handler the middleware
		// already installs, but keeping it here means a bare OPTIONS to /
		// still gets the headers.
		s.corsHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no route for %s", r.URL.Path))
}

// handleRoot renders the self-documenting JSON manifest that the cycle 9
// public landing page links to.
func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /")
		return
	}
	manifest := map[string]any{
		"service":     s.rootMeta.Service,
		"upstream":    s.rootMeta.Upstream,
		"proxy":       s.rootMeta.Proxy,
		"project":     s.rootMeta.Project,
		"public_url":  s.rootMeta.PublicURL,
		"listen_addr": s.rootMeta.ListenAddr,
		"endpoints": []string{
			"GET  " + s.rootMeta.PublicURL + "/v1/models",
			"POST " + s.rootMeta.PublicURL + "/v1/chat/completions",
			"GET  " + s.rootMeta.PublicURL + "/healthz",
		},
		"example_curl_chat": `curl -sX POST ` + s.rootMeta.PublicURL + `/v1/chat/completions -H 'content-type: application/json' -d '{"model":"gemini-3-flash","messages":[{"role":"user","content":"hi"}]}'`,
		"openai_sdk_python": map[string]any{
			"base_url": s.rootMeta.PublicURL + "/v1",
			"api_key":  "ignored",
			"example":  `import openai; c = openai.OpenAI(base_url="` + s.rootMeta.PublicURL + `/v1", api_key="ignored"); c.chat.completions.create(model="gemini-3-flash", messages=[{"role":"user","content":"hi"}]).choices[0].message.content`,
		},
	}
	writeJSON(w, http.StatusOK, manifest)
}

// handleHealth renders the JSON status block the operator scripts use.
func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /healthz")
		return
	}
	_, tokErr := s.tokens.AccessToken()
	exp := s.tokens.ExpiresAt()
	src := s.tokens.Source()
	status := map[string]any{
		"ok":    tokErr == nil,
		"token": tokErr == nil,
		"src":   src,
		"exp":   formatTime(exp),
		"time":  time.Now().Format(time.RFC3339),
		"models": len(knownModels),
	}
	writeJSON(w, http.StatusOK, status)
}

// handleModels serves the OpenAI `GET /v1/models` payload.
func (s *server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /v1/models")
		return
	}
	out := struct {
		Object string      `json:"object"`
		Data   []modelInfo `json:"data"`
	}{Object: "list", Data: knownModels}
	writeJSON(w, http.StatusOK, out)
}

// handleChatCompletions is the meaty one: parses an OpenAI chat completions
// request, hands it to agent.Loop, and streams the response back as either
// Server-Sent Events (stream=true) or a single JSON envelope (stream=false).
func (s *server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.corsHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required for /v1/chat/completions")
		return
	}
	// Always advertise CORS headers on the real reply.
	s.corsHeaders(w)

	// Read body (cap at 4 MiB; cycle 9 binary used 1 MiB but large system
	// prompts do happen).
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_failed", err.Error())
		return
	}
	defer r.Body.Close()

	var req chatCompletionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeError(w, http.StatusBadRequest, "missing_model", "`model` field is required")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "no_messages", "`messages` must contain at least one message")
		return
	}
	if !modelKnown(req.Model) {
		writeError(w, http.StatusBadRequest, "unknown_model", fmt.Sprintf("unknown model %q", req.Model))
		return
	}

	// Translate OpenAI messages to backend messages; preserve ordering, drop
	// tool messages (not supported upstream).
	msgs := make([]backend.Message, 0, len(req.Messages))
	var systemPrompt string
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemPrompt = m.Content
			continue
		}
		msgs = append(msgs, backend.Message{Role: m.Role, Content: m.Content})
	}

	// Build the agent loop with a per-request handler.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	id := newUUIDv4()
	created := time.Now().Unix()

	if req.Stream {
		// SSE path.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		handler := func(chunk backend.StreamChunk) error {
			emit := openAIChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: req.Model}
			if chunk.Error != nil {
				emit.Choices = []openAIChoice{{Index: 0, FinishReason: "error", Message: openAIMsg{Role: "assistant", Content: chunk.Error.Error()}}}
			} else if chunk.Text != "" {
				emit.Choices = []openAIChoice{{Index: 0, Delta: openAIMsg{Role: "assistant", Content: chunk.Text}}}
			} else {
				emit.Choices = []openAIChoice{{Index: 0, Delta: openAIMsg{}}}
			}
			if chunk.Done {
				emit.Choices[0].FinishReason = "stop"
				if chunk.Meta != nil {
					emit.Usage = &openAIUsage{
						PromptTokens:     chunk.Meta.PromptTokens,
						CompletionTokens: chunk.Meta.CompletionTokens,
						TotalTokens:      chunk.Meta.TotalTokens,
					}
					emit.SystemFingerprint = chunk.Meta.SystemFingerprint
				}
			}
			data, err := json.Marshal(emit)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return err
			}
			if flusher != nil {
				flusher.Flush()
			}
			return nil
		}
		loop, err := agent.New(agent.Options{
			Backend:       s.backend,
			Handler:       handler,
			SystemPrompt:  systemPrompt,
			DefaultModel:  req.Model,
			MaxTurns:      1,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "agent_init", err.Error())
			return
		}
		defer loop.Close()
		_ = loop.Send(ctx, agent.TurnInput{
			Model:       req.Model,
			MaxTokens:   req.MaxTokens,
			Temperature: req.Temperature,
			Messages:    msgs,
		})
		// Send the trailing [DONE] marker.
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	// Non-streaming: collect all chunks and emit one OpenAI envelope.
	var acc strings.Builder
	var meta *backend.ChunkMeta
	loop, err := agent.New(agent.Options{
		Backend:      s.backend,
		Handler: func(chunk backend.StreamChunk) error {
			acc.WriteString(chunk.Text)
			if chunk.Meta != nil {
				meta = chunk.Meta
			}
			return nil
		},
		SystemPrompt: systemPrompt,
		DefaultModel: req.Model,
		MaxTurns:     1,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "agent_init", err.Error())
		return
	}
	defer loop.Close()
	if err := loop.Send(ctx, agent.TurnInput{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Messages:    msgs,
	}); err != nil {
		// Even on error, surface what we got so clients see something useful.
	}

	resp := openAIResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   req.Model,
		Choices: []openAIChoice{{
			Index: 0,
				Message: openAIMsg{
					Role:    "assistant",
					Content: acc.String(),
				},
				FinishReason: "stop",
			}},
	}
	if meta != nil {
		resp.Usage = openAIUsage{
			PromptTokens:     meta.PromptTokens,
			CompletionTokens: meta.CompletionTokens,
			TotalTokens:      meta.TotalTokens,
		}
		resp.SystemFingerprint = meta.SystemFingerprint
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- middleware --------------------------------------------------------

// corsMiddleware writes CORS headers on every response and short-circuits
// OPTIONS preflight with 204. This is the same shape the cycle 9 binary used.
func (s *server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.corsHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) corsHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
	h.Set("Access-Control-Max-Age", "86400")
}

// loggingMiddleware prints one line per request with method, path, status and
// duration. Uses a statusRecorder to capture the status code.
func (s *server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

// statusRecorder wraps http.ResponseWriter so loggingMiddleware can read the
// status code after the handler ran.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(p)
}

// ---- wire types (OpenAI compatibility) ---------------------------------

type chatCompletionsRequest struct {
	Model       string         `json:"model"`
	Messages    []openAIReqMsg `json:"messages"`
	Stream      bool           `json:"stream"`
	MaxTokens   int            `json:"max_tokens,omitempty"`
	Temperature float64        `json:"temperature,omitempty"`
}

type openAIReqMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

type openAIResponse struct {
	ID                string      `json:"id"`
	Object            string      `json:"object"`
	Created           int64       `json:"created"`
	Model             string      `json:"model"`
	Choices           []openAIChoice `json:"choices"`
	Usage             openAIUsage `json:"usage"`
	SystemFingerprint string      `json:"system_fingerprint,omitempty"`
}

type openAIChunk struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []openAIChoice `json:"choices"`
	Usage             *openAIUsage   `json:"usage,omitempty"`
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
}

type openAIChoice struct {
	Index        int     `json:"index"`
	Message      openAIMsg `json:"message,omitempty"`
	Delta        openAIMsg `json:"delta,omitempty"`
	FinishReason string  `json:"finish_reason"`
}

type openAIMsg struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ---- helpers -----------------------------------------------------------

// newUUIDv4 generates a random hex string shaped like a UUID v4. We don't
// import google/uuid because we want this binary to have zero non-stdlib
// deps for the sandbox build.
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	hexBuf := make([]byte, 36)
	hex.Encode(hexBuf[0:8], b[0:4])
	hexBuf[8] = '-'
	hex.Encode(hexBuf[9:13], b[4:6])
	hexBuf[13] = '-'
	hex.Encode(hexBuf[14:18], b[6:8])
	hexBuf[18] = '-'
	hex.Encode(hexBuf[19:23], b[8:10])
	hexBuf[23] = '-'
	hex.Encode(hexBuf[24:36], b[10:16])
	return string(hexBuf)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "invalid_request_error",
			"code":    code,
		},
	})
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func ifEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func joinModelIDs(ms []modelInfo) string {
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	return "[" + strings.Join(ids, " ") + "]"
}

func modelKnown(id string) bool {
	for _, m := range knownModels {
		if m.ID == id {
			return true
		}
	}
	return false
}

// proxyReachable does a quick TCP probe on a `socks5h://` or `http://` URL.
// Returns true if the URL parses and the dial succeeds. Used for the
// `[boot] TUIC proxy not reachable` warning.
func proxyReachable(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	// We only do a hostname:port probe; full URL parsing isn't required for
	// the warning.
	host := rawURL
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	conn, err := net.DialTimeout("tcp", host, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ---- backend wiring ----------------------------------------------------

// newBackendClient returns the Client to install. We always return NewMock
// in this repo (cloudaicompanion unreachable from this pod). The arguments
// are kept so a future RealClient can be wired in without changing callers.
func newBackendClient(proxyURL, upstream, up string, userAgent, authMethod string, tokens backend.TokenStore) backend.Client {
	// Future: if licensed token + network egress are present, use a real
	// gRPC streaming client. For now: mock.
	_ = proxyURL
	_ = up
	_ = userAgent
	_ = authMethod
	_ = tokens
	return backend.NewMock()
}

// Compile-time check we haven't accidentally shadowed io.Discard.
var _ = io.Discard

// Suppress unused import warnings in builds that trim features.
var _ sync.Mutex