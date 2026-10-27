package agyproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/models"
)

// reqInfo is what the router learned about one request.
type reqInfo struct {
	model   string
	custom  bool
	tuned   models.Model
	wrapped bool
	inner   *geminicode.GenerateContentRequest
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
	var env geminicode.GenerateContentRequest
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

// passthrough forwards one RPC verbatim (method, headers, body, status,
// headers, body) and streams the response through without buffering it.
//
// The forwarding is httputil.ReverseProxy's — the standard library's
// reverse proxy. The clone's own copy loop was a re-implementation of
// it: the same hop-by-hop header list, the same 32 KiB copy buffer, the
// same flush after every chunk. The Rewrite hook keeps the three
// deviations the clone needs:
//
//   - the body is the buffer the router already read (the router
//     inspects it to decide whether the call targets a custom model),
//     so it is handed to the proxy rather than re-read from the wire;
//   - the client's Accept-Encoding is dropped, so the upstream's answer
//     arrives decoded and the capture files hold plain text;
//   - the client's forwarding headers are put back, because the
//     Rewrite hook strips them and the clone forwards the request as it
//     came in.
func (s *Server) passthrough(w http.ResponseWriter, r *http.Request, body []byte) {
	proxy := &httputil.ReverseProxy{
		Transport: s.client.Transport,
		// Flush after every chunk: the upstream streams SSE frames and
		// they have to reach agy as they arrive, not at EOF.
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			u := *s.upstream
			u.Path = joinPath(s.upstream.Path, pr.In.URL.Path)
			// The upstream's own query is replaced by the inbound one,
			// not appended to it.
			u.RawQuery = pr.In.URL.RawQuery
			pr.Out.URL = &u
			pr.Out.Host = u.Host

			pr.Out.Body = io.NopCloser(bytes.NewReader(body))
			pr.Out.ContentLength = int64(len(body))
			pr.Out.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(body)), nil
			}
			// The body may have been rewritten, so a copied Content-Length
			// would lie.
			pr.Out.Header.Del("Content-Length")
			// Let Go negotiate identity encoding: we forward the decoded
			// bytes and would otherwise have to rewrite Content-Encoding.
			pr.Out.Header.Del("Accept-Encoding")

			for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
				if vs := pr.In.Header.Values(h); len(vs) > 0 {
					pr.Out.Header.Del(h)
					for _, v := range vs {
						pr.Out.Header.Add(h, v)
					}
				}
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			s.logf("%s %s: upstream error: %v", r.Method, r.URL.Path, err)
			writeJSONError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
		},
	}
	proxy.ServeHTTP(w, r)
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

func isHopByHop(k string) bool {
	switch strings.ToLower(k) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailer", "transfer-encoding", "upgrade":
		return true
	}
	return false
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
