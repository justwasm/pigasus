// Package agyproxy is the local HTTP server that sits between the official
// `agy` CLI and the CloudCode upstream.
//
// agy 1.4.2 honours CLOUD_CODE_URL, so no binary patching is involved: the
// launcher exports CLOUD_CODE_URL=http://127.0.0.1:<port> and every CloudCode
// RPC arrives here as <base>/v1internal:<method>.
//
// Three jobs:
//
//  1. transparent pass-through to https://cloudcode-pa.googleapis.com, so
//     everything agy normally does keeps working byte-for-byte;
//  2. inject custom models into fetchAvailableModels and translate their
//     generateContent traffic to whichever provider internal/models speaks;
//  3. capture request/response pairs as JSON for reverse engineering.
package agyproxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/endpoints"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/models"
)

// DefaultAddr binds an ephemeral loopback port.
const DefaultAddr = "127.0.0.1:0"

// maxRequestBody caps a single inbound body. agy's largest request is a long
// conversation with tool schemas; 64 MiB is far above anything real and still
// bounds memory.
const maxRequestBody = 64 << 20

// Config configures a Server. Zero values select the documented defaults.
type Config struct {
	Addr       string         // default DefaultAddr
	Upstream   string         // default endpoints.DefaultCloudCode
	Models     []models.Model // custom models to advertise/serve
	CaptureDir string         // "" disables capture
	// CaptureMaxBytes is the ceiling the capture directory is held to.
	// A pointer, like fileConfig's booleans: nil takes the default
	// (DefaultCaptureMaxBytes), an explicit 0 means no ceiling at all.
	CaptureMaxBytes *int64
	Logf            func(string, ...any)
	Balance         bool                   // enables multi-account load balancing in proxy
	Aggregate       bool                   // enables virtual unified pool account aggregation
	Router          *accountrouting.Router // optional router (defaults to accountrouting.Shared())
}

// Server is the proxy. Create with New, run with Start, stop with Close.
type Server struct {
	cfg      Config
	upstream *url.URL
	client   *http.Client
	logf     func(string, ...any)
	cap      *capture
	router   *accountrouting.Router
	sockets  clientSockets

	quotaFetcher      func(ctx context.Context, email string) (*auth.QuotaSummary, error)
	codeAssistFetcher func(ctx context.Context, token string) (*auth.LoadCodeAssistResponse, error)

	httpSrv *http.Server
	ln      net.Listener
	addr    string

	closeOnce sync.Once
	closeErr  error
	closed    atomic.Bool
}

// New validates the configuration and prepares the HTTP client. It does not
// bind a socket; call Start for that.
func New(cfg Config) (*Server, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		cfg.Addr = DefaultAddr
	}
	if strings.TrimSpace(cfg.Upstream) == "" {
		cfg.Upstream = endpoints.Current().CloudCode
	}
	u, err := url.Parse(cfg.Upstream)
	if err != nil {
		return nil, fmt.Errorf("agyproxy: parse upstream %q: %w", cfg.Upstream, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("agyproxy: upstream %q needs a scheme and host", cfg.Upstream)
	}
	logf := cfg.Logf
	if logf == nil {
		l := log.New(os.Stderr, "agyproxy: ", 0)
		logf = l.Printf
	}
	// No Client.Timeout: a stream must be allowed to run as long as the model
	// takes. Per-phase timeouts are the guard rails.
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          64,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 5 * time.Minute,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	rtr := cfg.Router
	if rtr == nil {
		rtr = accountrouting.NewRouter()
	}
	s := &Server{cfg: cfg, upstream: u, client: client, logf: logf, router: rtr}
	s.router.SetPolicy(s.effectivePolicy)
	if cfg.CaptureDir != "" {
		max := DefaultCaptureMaxBytes
		if cfg.CaptureMaxBytes != nil {
			max = *cfg.CaptureMaxBytes
		}
		s.cap = newCapture(cfg.CaptureDir, logf, max)
	}
	return s, nil
}

// balanceEnabled reports whether the account balance feature flag is set.
func (s *Server) balanceEnabled() bool {
	if s.cfg.Balance || s.cfg.Aggregate {
		return true
	}
	for _, env := range []string{EnvBalance, EnvAccountPool, EnvAggregate, EnvAccountPoolAggregate} {
		if val, ok := os.LookupEnv(env); ok && strings.TrimSpace(val) != "" {
			if isTruthy(val) {
				return true
			}
			if isFalsy(val) {
				return false
			}
		}
	}
	if p := endpoints.CurrentAccountPool(); p != nil {
		if p.ProxyBalance || p.Aggregate || p.Strategy == accountpool.StrategyAggregate {
			return true
		}
	}
	return false
}

// aggregateActive reports whether virtual unified account aggregation is active.
func (s *Server) aggregateActive() bool {
	if s.cfg.Aggregate {
		return true
	}
	for _, env := range []string{EnvAggregate, EnvAccountPoolAggregate} {
		if val, ok := os.LookupEnv(env); ok && strings.TrimSpace(val) != "" {
			if isTruthy(val) {
				return true
			}
			if isFalsy(val) {
				return false
			}
		}
	}
	if p := endpoints.CurrentAccountPool(); p != nil {
		if p.Aggregate || p.Strategy == accountpool.StrategyAggregate {
			return true
		}
	}
	return false
}

// effectivePolicy returns the account-routing policy from endpoints/config.json,
// filling in enabled state and sensible defaults for proxy balancing.
func (s *Server) effectivePolicy() *accountpool.Config {
	p := endpoints.CurrentAccountPool()
	if p == nil {
		t := true
		cp := &accountpool.Config{
			Enabled:            true,
			Strategy:           accountpool.DefaultStrategy,
			Threshold:          0.15,
			MaxAttempts:        3,
			SwitchOnQuotaError: &t,
		}
		if s.aggregateActive() {
			cp.Aggregate = true
			cp.Strategy = accountpool.StrategyAggregate
		}
		return cp
	}
	cp := *p
	cp.Enabled = true
	if cp.Strategy == "" {
		cp.Strategy = accountpool.DefaultStrategy
	}
	if s.aggregateActive() {
		cp.Aggregate = true
		if cp.Strategy == accountpool.DefaultStrategy {
			cp.Strategy = accountpool.StrategyAggregate
		}
	}
	if cp.MaxAttempts <= 1 && cp.SwitchOnQuotaErrorEnabled() {
		cp.MaxAttempts = 3
	}
	return &cp
}

// policy returns the account-routing policy from endpoints/config.json.
func (s *Server) policy() *accountpool.Config {
	return accountrouting.Policy()
}

// balanceActive reports whether balancing is active for this server.
func (s *Server) balanceActive() bool {
	return s.balanceEnabled()
}

// models returns configured custom models if explicitly supplied in Config.
// The proxy does not inject or take over catwalk providers from providers.json.
// It solely manages Google OAuth accounts (/accounts) load balancing, quota aggregation, and passthrough.
func (s *Server) models() []models.Model {
	return s.cfg.Models
}

// Start binds the listener and serves in the background. It returns the
// bound address in "127.0.0.1:PORT" form.
func (s *Server) Start() (string, error) {
	if s.addr != "" {
		return s.addr, nil
	}
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return "", fmt.Errorf("agyproxy: listen %s: %w", s.cfg.Addr, err)
	}
	s.ln = ln
	s.addr = ln.Addr().String()
	s.httpSrv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 30 * time.Second,
		// No WriteTimeout: SSE responses are long-lived.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logf("serve: %v", err)
		}
	}()
	s.logf("listening on http://%s (upstream %s, %d custom model(s), capture %s)",
		s.addr, s.upstream, len(s.models()), captureState(s.cfg.CaptureDir))
	return s.addr, nil
}

// Addr returns the bound address ("127.0.0.1:PORT") or "" before Start.
func (s *Server) Addr() string { return s.addr }

// Close stops the listener and flushes pending capture records.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		if s.httpSrv != nil {
			s.closeErr = s.httpSrv.Close()
		}
		if s.ln != nil {
			_ = s.ln.Close()
		}
		if s.cap != nil {
			s.cap.close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.sockets.drain(ctx)
		if s.router != nil && s.router != accountrouting.Shared() {
			s.router.Close()
		}
	})
	return s.closeErr
}

// ServeHTTP routes one inbound RPC.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	rd, err := decodeRequestBody(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "content-encoding error: "+err.Error())
		return
	}
	defer rd.Close()

	body, err := readAll(rd, maxRequestBody)
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	method := methodFromPath(r.URL.Path)
	info := s.inspect(r.URL.Path, body)
	cw := newCaptureResponseWriter(w, s.cap != nil)
	s.captureRequest(started, method, r, info, body)

	s.dispatch(cw, r, body, info)

	s.captureResponse(started, method, r, info, body, cw)
	accTag := ""
	if acc := cw.account(); acc != "" {
		accTag = " [" + acc + "]"
	}
	s.logf("%s %s%s%s -> %d (%s)", r.Method, r.URL.Path, querySuffix(r.URL.RawQuery), accTag, cw.statusCode(), time.Since(started).Round(time.Millisecond))
}

type accountRecorder interface {
	setAccount(string)
}

func recordAccount(w http.ResponseWriter, email string) {
	if email == "" {
		return
	}
	for curr := w; curr != nil; {
		if rec, ok := curr.(accountRecorder); ok {
			rec.setAccount(email)
			return
		}
		if u, ok := curr.(interface{ Unwrap() http.ResponseWriter }); ok {
			curr = u.Unwrap()
		} else {
			break
		}
	}
}

func (s *Server) resolveAccount(currentEmail, authHeader string) string {
	if currentEmail != "" {
		return currentEmail
	}
	if em := emailFromBearer(authHeader); em != "" {
		return em
	}
	if _, em := accounts.Current(); em != "" {
		return em
	}
	if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil {
		if em := tok.UserEmail(); em != "" {
			return em
		}
	}
	return ""
}

func emailFromBearer(authHeader string) string {
	token := strings.TrimPrefix(strings.TrimSpace(authHeader), "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) >= 2 {
		data, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			data, err = base64.URLEncoding.DecodeString(parts[1])
			if err != nil {
				data = nil
			}
		}
		if data != nil {
			var claims struct {
				Email string `json:"email"`
			}
			if err := json.Unmarshal(data, &claims); err == nil && claims.Email != "" {
				return claims.Email
			}
		}
	}
	return ""
}

func decodeRequestBody(r *http.Request) (io.ReadCloser, error) {
	if r.Body == nil {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	enc := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding")))
	switch enc {
	case "", "identity":
		return r.Body, nil
	case "zstd":
		d, err := zstd.NewReader(r.Body, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(maxRequestBody))
		if err != nil {
			return nil, err
		}
		r.Header.Del("Content-Encoding")
		return d.IOReadCloser(), nil
	case "gzip", "x-gzip":
		g, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		r.Header.Del("Content-Encoding")
		return g, nil
	default:
		return r.Body, nil
	}
}

// SetQuotaFetcher overrides the quota summary retrieval logic (used in tests).
func (s *Server) SetQuotaFetcher(fn func(ctx context.Context, email string) (*auth.QuotaSummary, error)) {
	s.quotaFetcher = fn
}

// SetCodeAssistFetcher overrides the code assist retrieval logic (used in tests).
func (s *Server) SetCodeAssistFetcher(fn func(ctx context.Context, token string) (*auth.LoadCodeAssistResponse, error)) {
	s.codeAssistFetcher = fn
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, body []byte, info reqInfo) {
	if r.Method == http.MethodOptions {
		s.handleCORSPreflight(w, r)
		return
	}

	lp := strings.TrimSuffix(strings.ToLower(r.URL.Path), "/")
	switch {
	case lp == "/v1/models" || lp == "/models":
		s.handleModelsList(w, r)
	case lp == "/v1/chat/completions" || lp == "/chat/completions":
		s.handleOpenAIChat(w, r, body)
	case lp == "/v1/messages" || lp == "/messages":
		s.handleAnthropicMessages(w, r, body)
	case lp == "/v1/responses" || lp == "/responses":
		if wantsSocket(r) {
			s.serveResponsesSocket(w, r)
			return
		}
		if r.Method == http.MethodGet {
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				writeOpenAIError(w, http.StatusUpgradeRequired, "upgrade_required", "WebSocket upgrade failed")
				return
			}
			w.Header().Set("Allow", "POST")
			writeOpenAIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required for /v1/responses")
			return
		}
		s.handleResponses(w, r, body)
	case strings.HasSuffix(lp, ":fetchavailablemodels"):
		s.handleFetchAvailableModels(w, r, body)
	case strings.HasSuffix(lp, ":retrieveuserquotasummary"), strings.HasSuffix(lp, ":retrieveuserquota"):
		if s.aggregateActive() {
			s.handleAggregatedQuota(w, r, body)
		} else {
			s.passthrough(w, r, body)
		}
	case strings.HasSuffix(lp, ":loadcodeassist"):
		if s.aggregateActive() {
			s.handleAggregatedCodeAssist(w, r, body)
		} else {
			s.passthrough(w, r, body)
		}
	// The streaming case is tested first: ":streamgeneratecontent" does not
	// end in ":generatecontent", but keeping the order explicit documents the
	// precedence rather than relying on that.
	case strings.HasSuffix(lp, ":streamgeneratecontent"):
		if info.custom {
			s.handleCustom(w, r, info.tuned, body, true, info.wrapped)
		} else {
			body = sanitizeUpstreamBody(body, info)
			if s.balanceActive() {
				s.handleBalancedPassthrough(w, r, body, true, info.model)
			} else {
				s.passthrough(w, r, body)
			}
		}
	case strings.HasSuffix(lp, ":generatecontent"):
		if info.custom {
			s.handleCustom(w, r, info.tuned, body, false, info.wrapped)
		} else {
			body = sanitizeUpstreamBody(body, info)
			if s.balanceActive() {
				s.handleBalancedPassthrough(w, r, body, false, info.model)
			} else {
				s.passthrough(w, r, body)
			}
		}
	case strings.HasSuffix(lp, ":counttokens") && info.custom:
		s.handleCountTokens(w, info.inner)
	default:
		s.passthrough(w, r, body)
	}
}

func querySuffix(q string) string {
	if q == "" {
		return ""
	}
	return "?" + q
}
