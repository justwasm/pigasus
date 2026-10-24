package agyproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/justwasm/pigasus/internal/geminiwire"
	"github.com/justwasm/pigasus/internal/models"
)

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
