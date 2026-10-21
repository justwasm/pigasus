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

	"github.com/justwasm/pigasus/devutils/geminiwire"
	"github.com/justwasm/pigasus/internal/models"
)

// DefaultUpstream is the CloudCode endpoint agy talks to.
// Use daily-cloudcode-pa because prod cloudcode-pa returns 429 for this account
// while daily- returns 200 (matches internal/backend/cloudcode.go).
const DefaultUpstream = "https://daily-cloudcode-pa.googleapis.com"

// DefaultAddr binds an ephemeral loopback port.
const DefaultAddr = "127.0.0.1:0"

// maxRequestBody caps a single inbound body. agy's largest request is a long
// conversation with tool schemas; 64 MiB is far above anything real and still
// bounds memory.
const maxRequestBody = 64 << 20

// Config configures a Server. Zero values select the documented defaults.
type Config struct {
	Addr       string         // default DefaultAddr
	Upstream   string         // default DefaultUpstream
	Models     []models.Model // custom models to advertise/serve
	CaptureDir string         // "" disables capture
	Logf       func(string, ...any)
}

// Server is the proxy. Create with New, run with Start, stop with Close.
type Server struct {
	cfg      Config
	upstream *url.URL
	client   *http.Client
	logf     func(string, ...any)
	cap      *capture

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
		cfg.Upstream = DefaultUpstream
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
	s := &Server{cfg: cfg, upstream: u, client: client, logf: logf}
	if cfg.CaptureDir != "" {
		s.cap = newCapture(cfg.CaptureDir, logf)
	}
	return s, nil
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
		s.addr, s.upstream, len(s.cfg.Models), captureState(s.cfg.CaptureDir))
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
	})
	return s.closeErr
}

// ServeHTTP routes one inbound RPC.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	body, err := readAll(r.Body, maxRequestBody)
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	method := methodFromPath(r.URL.Path)
	lp := strings.ToLower(r.URL.Path)
	info := s.inspect(r.URL.Path, body)
	cw := newCaptureResponseWriter(w, s.cap != nil)
	if s.cap != nil {
		s.cap.write(captureRecord{
			TS:             started,
			Kind:           "request",
			Method:         method,
			HTTPMethod:     r.Method,
			Path:           r.URL.Path,
			Model:          info.model,
			Custom:         info.custom,
			RequestHeaders: r.Header.Clone(),
			RequestBody:    rawBody(body),
		})
	}

	switch {
	case strings.HasSuffix(lp, ":fetchavailablemodels"):
		s.handleFetchAvailableModels(cw, r, body)
	// The streaming case is tested first: ":streamgeneratecontent" does not
	// end in ":generatecontent", but keeping the order explicit documents the
	// precedence rather than relying on that.
	case strings.HasSuffix(lp, ":streamgeneratecontent"):
		if info.custom {
			s.handleCustom(cw, r, info.tuned, body, true, info.wrapped)
		} else {
			s.passthrough(cw, r, body)
		}
	case strings.HasSuffix(lp, ":generatecontent"):
		if info.custom {
			s.handleCustom(cw, r, info.tuned, body, false, info.wrapped)
		} else {
			s.passthrough(cw, r, body)
		}
	case strings.HasSuffix(lp, ":counttokens") && info.custom:
		s.handleCountTokens(cw, info.inner)
	default:
		s.passthrough(cw, r, body)
	}

	if s.cap != nil {
		s.cap.write(captureRecord{
			TS:             started,
			Kind:           "response",
			Method:         method,
			HTTPMethod:     r.Method,
			Path:           r.URL.Path,
			Model:          info.model,
			Custom:         info.custom,
			Status:         cw.statusCode(),
			RequestHeaders: r.Header.Clone(),
			RequestBody:    rawBody(body),
			ResponseBody:   rawBody(cw.captured()),
		})
	}
	s.logf("%s %s%s -> %d (%s)", r.Method, r.URL.Path, querySuffix(r.URL.RawQuery), cw.statusCode(), time.Since(started).Round(time.Millisecond))
}

// reqInfo is what the router learned about one request.
type reqInfo struct {
	model   string
	custom  bool
	tuned   models.Model
	wrapped bool
	inner   *geminiwire.GenerateContentRequest
}

// inspect parses the body only for the RPCs that carry a model. The path test
// is case-insensitive on purpose: agy spells the RPC `generateContent`
// (lower-case g after the colon) but the envelope has also been seen as
// `GenerateContent`, and getting this wrong silently turns every custom-model
// call into a pass-through.
func (s *Server) inspect(path string, body []byte) reqInfo {
	p := strings.ToLower(path)
	if !strings.Contains(p, "generatecontent") && !strings.HasSuffix(p, ":counttokens") {
		return reqInfo{}
	}
	var env geminiwire.GenerateContentRequest
	if err := json.Unmarshal(body, &env); err != nil {
		return reqInfo{}
	}
	name := env.ModelName()
	info := reqInfo{model: name, wrapped: env.Wrapped(), inner: env.Inner()}
	if m, ok := s.matchCustom(name); ok {
		info.custom = true
		info.tuned = m
	}
	return info
}

// ---- routing helpers ----------------------------------------------------

// methodFromPath turns "/v1internal:streamGenerateContent" into
// "streamGenerateContent". Capture filenames use it.
func methodFromPath(path string) string {
	p := strings.TrimPrefix(path, "/")
	if i := strings.LastIndex(p, ":"); i >= 0 {
		if m := sanitizeName(p[i+1:]); m != "" {
			return m
		}
	}
	if m := sanitizeName(p); m != "" {
		return m
	}
	return "root"
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func querySuffix(q string) string {
	if q == "" {
		return ""
	}
	return "?" + q
}

func captureState(dir string) string {
	if dir == "" {
		return "off"
	}
	return dir
}

// ---- HTTP plumbing ------------------------------------------------------

// passthrough forwards one RPC verbatim (method, headers, body, status,
// headers, body) and streams the response through without buffering it.
func (s *Server) passthrough(w http.ResponseWriter, r *http.Request, body []byte) {
	req, err := s.upstreamRequest(r, body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.logf("%s %s: upstream error: %v", r.Method, r.URL.Path, err)
		writeJSONError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	streamCopy(w, resp.Body)
}

func (s *Server) upstreamRequest(r *http.Request, body []byte) (*http.Request, error) {
	u := *s.upstream
	u.Path = joinPath(s.upstream.Path, r.URL.Path)
	u.RawQuery = r.URL.RawQuery
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, u.String(), rdr)
	if err != nil {
		return nil, err
	}
	copyHeaders(req.Header, r.Header)
	// The body may have been rewritten, so a copied Content-Length would lie.
	req.Header.Del("Content-Length")
	// Let Go negotiate identity encoding: we forward the decoded bytes and
	// would otherwise have to rewrite Content-Encoding.
	req.Header.Del("Accept-Encoding")
	req.Header.Del("Host")
	req.Host = u.Host
	return req, nil
}

func joinPath(base, p string) string {
	switch {
	case base == "":
		return p
	case strings.HasSuffix(base, "/") && strings.HasPrefix(p, "/"):
		return base + p[1:]
	case !strings.HasSuffix(base, "/") && !strings.HasPrefix(p, "/"):
		return base + "/" + p
	default:
		return base + p
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for k, vv := range src {
		if isHopByHop(k) || strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func isHopByHop(k string) bool {
	switch strings.ToLower(k) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailer", "transfer-encoding", "upgrade":
		return true
	}
	return false
}

// streamCopy copies src to w flushing after every chunk so SSE frames reach
// agy as they arrive instead of at EOF.
func streamCopy(w http.ResponseWriter, src io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func readAll(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("body too large")
	}
	return b, nil
}

// writeJSONError emits a Gemini-shaped error envelope, which both agy and the
// CloudCode clients know how to display.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": msg,
			"status":  http.StatusText(status),
		},
	})
}

// ---- capture-aware ResponseWriter --------------------------------------

// captureResponseWriter remembers the status and a bounded copy of the body so
// ServeHTTP can persist the exchange after the handler returns. When capture is
// off it is a transparent wrapper.
type captureResponseWriter struct {
	http.ResponseWriter
	status  int
	limited bool
	buf     bytes.Buffer
}

// captureBodyLimit bounds how much of a response is stored in memory. A turn
// with a large tool result can exceed it; the capture is truncated, the request
// is not.
const captureBodyLimit = 16 << 20

func newCaptureResponseWriter(w http.ResponseWriter, capture bool) *captureResponseWriter {
	return &captureResponseWriter{ResponseWriter: w, limited: capture}
}

func (c *captureResponseWriter) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *captureResponseWriter) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.limited && c.buf.Len() < captureBodyLimit {
		room := captureBodyLimit - c.buf.Len()
		if room > len(p) {
			room = len(p)
		}
		c.buf.Write(p[:room])
	}
	return c.ResponseWriter.Write(p)
}

// Flush keeps SSE working through the wrapper.
func (c *captureResponseWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *captureResponseWriter) statusCode() int {
	if c.status == 0 {
		return http.StatusOK
	}
	return c.status
}

func (c *captureResponseWriter) captured() []byte { return c.buf.Bytes() }
