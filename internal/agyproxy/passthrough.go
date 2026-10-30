package agyproxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
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

// sanitizeUpstreamBody adjusts Gemini request payloads sent upstream if they contain
// historical tool calls without thought signatures or Gemini 3 thinking config conflicts,
// preserving all other fields and envelopes intact.
func sanitizeUpstreamBody(body []byte, info reqInfo) []byte {
	if info.inner == nil {
		return body
	}
	modSig := geminicode.InjectSkipThoughtSignatures(info.inner.Contents)
	modThink := geminicode.ResolveThinkingConfig(info.model, info.inner.GenerationConfig)
	if !modSig && !modThink {
		return body
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body
	}
	target := root
	if info.wrapped {
		if innerMap, ok := root["request"].(map[string]any); ok {
			target = innerMap
		}
	}
	if modSig {
		if b, err := json.Marshal(info.inner.Contents); err == nil {
			var raw any
			if json.Unmarshal(b, &raw) == nil {
				target["contents"] = raw
			}
		}
	}
	if modThink {
		if b, err := json.Marshal(info.inner.GenerationConfig); err == nil {
			var raw any
			if json.Unmarshal(b, &raw) == nil {
				target["generationConfig"] = raw
			}
		}
	}
	if updated, err := json.Marshal(root); err == nil {
		return updated
	}
	return body
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

// handleBalancedPassthrough executes one upstream generation call with multi-account
// load balancing. It selects an account via the account pool router, replaces the
// Authorization header with that account's token, and if upstream responds with a quota
// error (HTTP 429), excludes the exhausted account and retries with the next account up to
// max_attempts.
func (s *Server) handleBalancedPassthrough(w http.ResponseWriter, r *http.Request, body []byte, stream bool) {
	if !s.balanceActive() {
		s.passthrough(w, r, body)
		return
	}
	policy := s.effectivePolicy()
	if policy == nil || !policy.Enabled {
		s.passthrough(w, r, body)
		return
	}
	cfg := policy.Normalised()
	maxAttempts := cfg.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if !cfg.SwitchOnQuotaErrorEnabled() {
		maxAttempts = 1
	}
	if s.aggregateActive() {
		if cands, err := s.router.CandidateAccounts(r.Context()); err == nil && len(cands) > maxAttempts {
			maxAttempts = len(cands)
		}
	}

	var lastErrResp *http.Response
	var lastErrBody []byte

	for attempt := 0; attempt < maxAttempts; attempt++ {
		token, err := s.router.TokenFunc(r.Context())
		if err != nil {
			s.logf("[account-pool] token resolution error: %v", err)
			if auth := r.Header.Get("Authorization"); auth != "" {
				token = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		currentEmail := s.router.LastRouted()

		upstreamReq, err := s.upstreamRequest(r, body)
		if err != nil {
			s.logf("[account-pool] %s %s: upstream request creation error: %v", r.Method, r.URL.Path, err)
			writeJSONError(w, http.StatusBadGateway, "upstream request creation failed: "+err.Error())
			return
		}
		if token != "" {
			upstreamReq.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := s.client.Do(upstreamReq)
		if err != nil {
			s.logf("[account-pool] %s %s: upstream error: %v", r.Method, r.URL.Path, err)
			writeJSONError(w, http.StatusBadGateway, "upstream request failed: "+err.Error())
			return
		}

		if isQuotaStatus(resp.StatusCode) {
			errBytes, _ := readAll(resp.Body, maxRequestBody)
			resp.Body.Close()

			if isQuotaResponseBody(resp.StatusCode, errBytes) {
				s.logf("[account-pool] %s quota exceeded (%d); excluding and switching account (attempt %d/%d)",
					currentEmail, resp.StatusCode, attempt+1, maxAttempts)
				if currentEmail != "" {
					s.router.Exclude(currentEmail)
				}
				lastErrResp = resp
				lastErrBody = errBytes
				if attempt+1 < maxAttempts {
					continue
				}
				s.writeUpstreamResponse(w, lastErrResp, lastErrBody)
				return
			}

			s.writeUpstreamResponse(w, resp, errBytes)
			return
		}

		// Non-quota status (e.g. 200 OK, 400 Bad Request, etc.)
		if stream {
			handled := s.forwardStreamResponse(w, resp, currentEmail, attempt, maxAttempts, func() {
				if currentEmail != "" {
					s.router.Exclude(currentEmail)
				}
			})
			if handled {
				return
			}
			// Immediate stream quota error -> retry next account
			continue
		}

		s.forwardUnaryResponse(w, resp)
		return
	}

	if lastErrResp != nil {
		s.writeUpstreamResponse(w, lastErrResp, lastErrBody)
	}
}

func (s *Server) writeUpstreamResponse(w http.ResponseWriter, resp *http.Response, body []byte) {
	copyHeaders(w.Header(), resp.Header)
	w.Header().Del("Content-Encoding")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func (s *Server) forwardUnaryResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	body, _ := readAll(resp.Body, maxRequestBody)
	s.writeUpstreamResponse(w, resp, body)
}

func (s *Server) forwardStreamResponse(w http.ResponseWriter, resp *http.Response, currentEmail string, attempt, maxAttempts int, onImmediateQuota func()) bool {
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	var firstEvent bytes.Buffer
	var dataPayload strings.Builder
	var sawError *geminicode.StreamError
	hasEventData := false

	// Read initial lines until first event with data completes (\n\n) or stream ends.
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			firstEvent.Write(line)
			trimmed := strings.TrimRight(string(line), "\r\n")
			if trimmed == "" {
				if hasEventData {
					// End of first data-bearing event
					break
				}
				// Leading empty line before any event: keep buffering
			} else if strings.HasPrefix(trimmed, ":") {
				// SSE comment or keepalive ping: keep buffering
			} else {
				if strings.HasPrefix(trimmed, "data:") {
					hasEventData = true
					if dataPayload.Len() > 0 {
						dataPayload.WriteByte('\n')
					}
					dataPayload.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
				} else {
					hasEventData = true
				}
			}
		}
		if err != nil {
			break
		}
	}

	if dataPayload.Len() > 0 {
		sawError = geminicode.ParseFrameError(dataPayload.String())
	}

	// Immediate quota error on first frame: transparent failover if attempts remain
	if sawError != nil && isStreamQuotaError(sawError) && attempt+1 < maxAttempts {
		s.logf("[account-pool] %s stream quota exceeded (%d: %s); excluding and switching account (attempt %d/%d)",
			currentEmail, sawError.Code, sawError.Message, attempt+1, maxAttempts)
		if onImmediateQuota != nil {
			onImmediateQuota()
		}
		return false
	}

	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Del("Content-Length")
	w.Header().Del("Content-Encoding")
	w.WriteHeader(resp.StatusCode)

	flusher, isFlusher := w.(http.Flusher)
	if firstEvent.Len() > 0 {
		_, _ = w.Write(firstEvent.Bytes())
		if isFlusher {
			flusher.Flush()
		}
	}

	if sawError != nil && isStreamQuotaError(sawError) && currentEmail != "" {
		s.router.Exclude(currentEmail)
	}

	// Stream remaining bytes, monitoring for protocol error frames
	buf := make([]byte, 32*1024)
	var lineBuf bytes.Buffer
	var midStreamData strings.Builder
	for {
		n, err := br.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			_, _ = w.Write(chunk)
			if isFlusher {
				flusher.Flush()
			}

			// Scan complete lines in chunk for mid-stream error frames
			for _, b := range chunk {
				if b == '\n' {
					line := strings.TrimRight(lineBuf.String(), "\r\n")
					lineBuf.Reset()
					if line == "" {
						if midStreamData.Len() > 0 {
							payload := midStreamData.String()
							midStreamData.Reset()
							if sErr := geminicode.ParseFrameError(payload); sErr != nil && isStreamQuotaError(sErr) {
								if currentEmail != "" {
									s.router.Exclude(currentEmail)
									s.logf("[account-pool] %s mid-stream quota error; excluded for future turns", currentEmail)
								}
							}
						}
					} else if strings.HasPrefix(line, "data:") {
						if midStreamData.Len() > 0 {
							midStreamData.WriteByte('\n')
						}
						midStreamData.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
					}
				} else {
					lineBuf.WriteByte(b)
				}
			}
		}
		if err != nil {
			break
		}
	}
	if midStreamData.Len() > 0 {
		payload := midStreamData.String()
		if sErr := geminicode.ParseFrameError(payload); sErr != nil && isStreamQuotaError(sErr) {
			if currentEmail != "" {
				s.router.Exclude(currentEmail)
				s.logf("[account-pool] %s mid-stream quota error; excluded for future turns", currentEmail)
			}
		}
	}
	return true
}

func isQuotaStatus(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusForbidden
}

func isQuotaResponseBody(code int, body []byte) bool {
	if code == http.StatusTooManyRequests {
		return true
	}
	low := bytes.ToLower(body)
	return bytes.Contains(low, []byte("quota")) ||
		bytes.Contains(low, []byte("resource_exhausted"))
}

func isStreamQuotaError(sErr *geminicode.StreamError) bool {
	if sErr == nil {
		return false
	}
	if sErr.Code == 429 {
		return true
	}
	if strings.EqualFold(sErr.Status, "RESOURCE_EXHAUSTED") {
		return true
	}
	msg := strings.ToLower(sErr.Message)
	return strings.Contains(msg, "quota") || strings.Contains(msg, "resource_exhausted")
}
