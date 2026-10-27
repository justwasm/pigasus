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

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/models"
)

// DefaultUpstream is the CloudCode endpoint agy talks to.
// Use daily-cloudcode-pa because prod cloudcode-pa returns 429 for this account
// while daily- returns 200 (matches internal/backend/cloudcode.go).
//
// It is the compiled-in default. The live upstream is
// endpoints.Current().CloudCode — the configuration file may point the
// proxy at a local service.
const DefaultUpstream = endpoints.DefaultCloudCode

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
	info := s.inspect(r.URL.Path, body)
	cw := newCaptureResponseWriter(w, s.cap != nil)
	s.captureRequest(started, method, r, info, body)

	s.dispatch(cw, r, body, info)

	s.captureResponse(started, method, r, info, body, cw)
	s.logf("%s %s%s -> %d (%s)", r.Method, r.URL.Path, querySuffix(r.URL.RawQuery), cw.statusCode(), time.Since(started).Round(time.Millisecond))
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, body []byte, info reqInfo) {
	lp := strings.ToLower(r.URL.Path)
	switch {
	case strings.HasSuffix(lp, ":fetchavailablemodels"):
		s.handleFetchAvailableModels(w, r, body)
	// The streaming case is tested first: ":streamgeneratecontent" does not
	// end in ":generatecontent", but keeping the order explicit documents the
	// precedence rather than relying on that.
	case strings.HasSuffix(lp, ":streamgeneratecontent"):
		if info.custom {
			s.handleCustom(w, r, info.tuned, body, true, info.wrapped)
		} else {
			s.passthrough(w, r, body)
		}
	case strings.HasSuffix(lp, ":generatecontent"):
		if info.custom {
			s.handleCustom(w, r, info.tuned, body, false, info.wrapped)
		} else {
			s.passthrough(w, r, body)
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
