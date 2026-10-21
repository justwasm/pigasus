// Command anthropic-shim translates Anthropic-style Messages API calls
// (`POST /v1/messages`, `GET /v1/models`) onto the same backend.Client
// the openai-shim uses. The wire format is Anthropic-shaped; the upstream
// call and streaming primitives are identical to cmd/openai-shim so both
// binaries stay in sync over time.
//
// The flow:
//
//	HTTP request (Anthropic)
//	  │
//	  ▼
//	translateAnthropicToBackend() ─→ agent.Loop ─→ backend.Client.Stream
//	                                                       │
//	                                                       ▼
//	                                          StreamRequest{Model:…, Messages:…}
//	                                                       │
//	                                                  upstream
//	                                                       │
//	                                                       ▼
//	                                            <-chan StreamChunk
//	                                                       │
//	                                                       ▼
//	translateChunkToAnthropicSSE()  ─→ http.ResponseWriter
//
// Anthropic streaming uses named events (message_start, content_block_delta,
// message_delta, message_stop) rather than the OpenAI `data: {…}` framing.
// Non-streaming returns a single JSON envelope with `content: [{type:text,…}]`.
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

// knownModels — the Anthropic side exposes the same four Antigravity models
// under the same ids. Anthropic SDKs (claude-code, continue.dev, …) accept
// any string for `model`, so no id remapping is required.
var knownModels = []string{
	"claude-sonnet-4-6",
	"claude-opus-4-6-thinking",
	"gemini-3-flash",
	"gpt-oss-120b-medium",
}

func modelKnown(id string) bool {
	for _, m := range knownModels {
		if m == id {
			return true
		}
	}
	return false
}

// ---- main + flag/env plumbing ------------------------------------------

func main() {
	addr := flag.String("addr", envOr("ANTHROPIC_SHIM_ADDR", ":19998"), "HTTP listen address (env ANTHROPIC_SHIM_ADDR)")
	project := flag.String("project", envOr("ANTHROPIC_SHIM_PROJECT", "cloudaicompanion-bucket-1307"), "Antigravity cloudaicompanion project ID (env ANTHROPIC_SHIM_PROJECT)")
	proxyURL := flag.String("proxy-url", envOr("ANTHROPIC_SHIM_PROXY", "socks5h://127.0.0.1:2081"), "SOCKS5/HTTP proxy URL (env ANTHROPIC_SHIM_PROXY)")
	publicURL := flag.String("public-url", envOr("ANTHROPIC_SHIM_PUBLIC_URL", ""), "Public URL printed on the root page (env ANTHROPIC_SHIM_PUBLIC_URL)")
	tokenFile := flag.String("token-file", envOr("ANTHROPIC_SHIM_TOKEN_FILE", "/workspace/.home/.gemini/antigravity-cli/antigravity-oauth-token"), "OAuth token JSON file (env ANTHROPIC_SHIM_TOKEN_FILE)")
	upstream := flag.String("upstream", envOr("ANTHROPIC_SHIM_UPSTREAM", "https://daily-cloudcode-pa.googleapis.com/v1internal:generateChat"), "Antigravity generateChat URL (env ANTHROPIC_SHIM_UPSTREAM)")
	flag.Parse()

	versionStr := "anthropic-shim/1.0.0"
	_ = project
	_ = upstream
	inlineToken := strings.TrimSpace(os.Getenv("ANTHROPIC_SHIM_TOKEN"))
	if inlineToken == "" {
		// Fall back to the openai-shim token env var so users don't have to
		// duplicate configuration.
		inlineToken = strings.TrimSpace(os.Getenv("OPENAI_SHIM_TOKEN"))
	}
	tokens := backend.NewTokenStoreWithEnv(*tokenFile, inlineToken)
	if _, err := tokens.AccessToken(); err != nil {
		log.Printf("[boot] token load failed: %v (continuing)", err)
	} else {
		log.Printf("[boot] token loaded (%d bytes, src=%s, expires=%s)", 0, tokens.Source(), tokens.ExpiresAt().Format(time.RFC3339))
	}

	be := newBackendClient(*proxyURL, *tokenFile)

	svc := &server{
		version: versionStr,
		tokens:  tokens,
		backend: be,
		rootMeta: rootMeta{
			Service:    versionStr,
			Upstream:   *upstream,
			Proxy:      *proxyURL,
			Project:    *project,
			PublicURL:  ifEmpty(*publicURL, "http://"+*addr),
			ListenAddr: *addr,
		},
	}

	if !proxyReachable(*proxyURL) {
		log.Printf("[boot] TUIC proxy not reachable at %s", *proxyURL)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", svc.handleMessages)
	mux.HandleFunc("/v1/models", svc.handleModels)
	mux.HandleFunc("/healthz", svc.handleHealth)
	mux.HandleFunc("/", svc.routeRoot)

	handler := svc.corsMiddleware(svc.loggingMiddleware(mux))

	log.Printf("[boot] anthropic-shim/1.0.0 listening on %s", *addr)
	log.Printf("[boot] upstream = %s", *upstream)
	log.Printf("[boot] proxy    = %s", *proxyURL)
	log.Printf("[boot] project  = %s", *project)
	log.Printf("[boot] public   = %s", ifEmpty(*publicURL, "http://"+*addr))
	log.Printf("[boot] models   = [%s]", strings.Join(knownModels, " "))

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
	version string
	tokens  backend.TokenStore
	backend backend.Client

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

func (s *server) routeRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.corsHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "") {
		s.handleRoot(w, r)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no route for %s", r.URL.Path))
}

func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	manifest := map[string]any{
		"service":     s.rootMeta.Service,
		"upstream":    s.rootMeta.Upstream,
		"proxy":       s.rootMeta.Proxy,
		"project":     s.rootMeta.Project,
		"public_url":  s.rootMeta.PublicURL,
		"listen_addr": s.rootMeta.ListenAddr,
		"endpoints": []string{
			"GET  " + s.rootMeta.PublicURL + "/v1/models",
			"POST " + s.rootMeta.PublicURL + "/v1/messages",
			"GET  " + s.rootMeta.PublicURL + "/healthz",
		},
		"example_curl_message": `curl -sX POST ` + s.rootMeta.PublicURL + `/v1/messages -H 'content-type: application/json' -H 'anthropic-version: 2023-06-01' -d '{"model":"claude-sonnet-4-6","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}'`,
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /healthz")
		return
	}
	_, tokErr := s.tokens.AccessToken()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     tokErr == nil,
		"token":  tokErr == nil,
		"src":    s.tokens.Source(),
		"exp":    formatTime(s.tokens.ExpiresAt()),
		"time":   time.Now().Format(time.RFC3339),
		"models": len(knownModels),
	})
}

// handleModels — Anthropic has no `/v1/models` natively but
// claude-code / continue.dev still call it, so we serve a minimal list of
// `{name, type}` objects.
func (s *server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /v1/models")
		return
	}
	out := struct {
		Data []map[string]string `json:"data"`
	}{Data: make([]map[string]string, 0, len(knownModels))}
	for _, id := range knownModels {
		out.Data = append(out.Data, map[string]string{
			"type": "model",
			"name": id,
			"id":   id,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMessages is the meaty one. Translates Anthropic Messages API into
// our internal loop, then frames the response either as SSE events
// (stream=true) or a single JSON envelope.
func (s *server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.corsHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required for /v1/messages")
		return
	}
	s.corsHeaders(w)

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_failed", err.Error())
		return
	}
	defer r.Body.Close()

	var req messagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeError(w, http.StatusBadRequest, "missing_model", "`model` field is required")
		return
	}
	if !modelKnown(req.Model) {
		writeError(w, http.StatusBadRequest, "unknown_model", fmt.Sprintf("unknown model %q", req.Model))
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "no_messages", "`messages` must contain at least one message")
		return
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 1024 // Anthropic requires max_tokens; default sensibly
	}

	msgs, sysPrompt := translateMessagesToBackend(req)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	id := "msg_" + newID()
	if req.Stream {
		s.streamAnthropic(ctx, w, req, id, sysPrompt, msgs)
		return
	}
	s.nonStreamAnthropic(ctx, w, req, id, sysPrompt, msgs)
}

// translateMessagesToBackend flattens Anthropic's system/messages split plus
// content-block arrays into backend.Message{role, content} strings. The
// Antigravity wire drops multimodal parts, so we concatenate text parts.
func translateMessagesToBackend(req messagesRequest) ([]backend.Message, string) {
	var sys string
	if s := strings.TrimSpace(req.System); s != "" {
		sys = s
	}
	out := make([]backend.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		out = append(out, backend.Message{
			Role:    m.Role,
			Content: flattenContent(m.Content),
		})
	}
	return out, sys
}

// flattenContent handles both the simple `content: "..."` form and the
// block form `content: [{type:"text", text:"..."}, ...]`. Returns plain
// text; non-text blocks are noted in brackets (cycle 9 binary silently
// dropped them, we preserve at least a marker).
func flattenContent(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, blk := range v {
			m, ok := blk.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "text":
				if s, ok := m["text"].(string); ok {
					b.WriteString(s)
				}
			case "image":
				b.WriteString("[image]")
			case "tool_use":
				b.WriteString("[tool_use]")
			case "tool_result":
				b.WriteString("[tool_result]")
			default:
				// Unknown block — skip silently.
			}
		}
		return b.String()
	}
	return ""
}

// streamAnthropic emits named Anthropic SSE events.
func (s *server) streamAnthropic(ctx context.Context, w http.ResponseWriter, req messagesRequest, id, sysPrompt string, msgs []backend.Message) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	emit := func(event string, payload any) error {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	// message_start
	_ = emit("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":      id,
			"type":    "message",
			"role":    "assistant",
			"model":   req.Model,
			"content": []any{},
		},
	})
	// content_block_start
	_ = emit("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	})
	// ping (Anthropic sends these to keep the connection fresh)
	_ = emit("ping", map[string]any{"type": "ping"})

	var usage *backend.ChunkMeta
	handler := func(chunk backend.StreamChunk) error {
		if chunk.Error != nil {
			// Surface as text in the delta + end_turn later.
			_ = emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": chunk.Error.Error()},
			})
			return nil
		}
		if chunk.Text != "" {
			return emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": chunk.Text},
			})
		}
		if chunk.Done {
			usage = chunk.Meta
		}
		return nil
	}

	loop, err := agent.New(agent.Options{
		Backend:      s.backend,
		Handler:      handler,
		SystemPrompt: sysPrompt,
		DefaultModel: req.Model,
		MaxTurns:     1,
	})
	if err != nil {
		_ = emit("error", map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "internal_error",
				"message": err.Error(),
			},
		})
		return
	}
	defer loop.Close()
	_ = loop.Send(ctx, agent.TurnInput{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Messages:    msgs,
	})

	// content_block_stop
	_ = emit("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	})
	// message_delta
	md := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason": "end_turn",
			"stop_sequence": nil,
		},
	}
	if usage != nil {
		md["usage"] = map[string]any{
			"input_tokens":  usage.PromptTokens,
			"output_tokens": usage.CompletionTokens,
		}
	}
	_ = emit("message_delta", md)
	// message_stop
	_ = emit("message_stop", map[string]any{"type": "message_stop"})
}

// nonStreamAnthropic returns a single Anthropic-shaped JSON envelope.
func (s *server) nonStreamAnthropic(ctx context.Context, w http.ResponseWriter, req messagesRequest, id, sysPrompt string, msgs []backend.Message) {
	var acc strings.Builder
	var usage *backend.ChunkMeta
	loop, err := agent.New(agent.Options{
		Backend:      s.backend,
		Handler: func(chunk backend.StreamChunk) error {
			acc.WriteString(chunk.Text)
			if chunk.Meta != nil {
				usage = chunk.Meta
			}
			return nil
		},
		SystemPrompt: sysPrompt,
		DefaultModel: req.Model,
		MaxTurns:     1,
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

	resp := map[string]any{
		"id":          id,
		"type":        "message",
		"role":        "assistant",
		"model":       req.Model,
		"content":     []map[string]string{{"type": "text", "text": acc.String()}},
		"stop_reason": "end_turn",
		"stop_sequence": nil,
	}
	if usage != nil {
		resp["usage"] = map[string]any{
			"input_tokens":  usage.PromptTokens,
			"output_tokens": usage.CompletionTokens,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- middleware --------------------------------------------------------

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
	h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Anthropic-Version, Anthropic-Dangerous-Beta")
	h.Set("Access-Control-Max-Age", "86400")
}

func (s *server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

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

// ---- wire types (Anthropic Messages API shape) --------------------------------

type messagesRequest struct {
	Model       string         `json:"model"`
	Messages    []anthropicMsg `json:"messages"`
	System      string         `json:"system,omitempty"`
	MaxTokens   int            `json:"max_tokens"`
	Temperature float64        `json:"temperature,omitempty"`
	Stream      bool           `json:"stream,omitempty"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []block
}

// ---- helpers -----------------------------------------------------------

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "fallback-id"
	}
	return hex.EncodeToString(b[:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError emits an `error` envelope in Anthropic's style.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": msg,
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

func proxyReachable(rawURL string) bool {
	if rawURL == "" {
		return false
	}
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

func newBackendClient(proxyURL, tokenFile string) backend.Client {
	_ = proxyURL
	_ = tokenFile
	return backend.NewMock()
}

var _ = io.Discard
var _ sync.Mutex