package agyproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/justwasm/pigasus/geminicode"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/infra/agyproxy/codec"
)

// Protocol conversions live in internal/infra/agyproxy/codec.

func setCORSHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Anthropic-Version, Anthropic-Dangerous-Beta")
	h.Set("Access-Control-Max-Age", "86400")
}

func (s *Server) handleCORSPreflight(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

// handleModelsList returns standard OpenAI model list catalog.
func (s *Server) handleModelsList(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for /v1/models")
		return
	}
	now := time.Now().Unix()
	data := []map[string]any{
		{"id": "gemini-3.8-flash-high", "object": "model", "created": now, "owned_by": "google"},
		{"id": "gemini-3.8-flash", "object": "model", "created": now, "owned_by": "google"},
		{"id": "gemini-3.1-pro-high", "object": "model", "created": now, "owned_by": "google"},
		{"id": "gemini-3.1-pro", "object": "model", "created": now, "owned_by": "google"},
		{"id": "gemini-3.8-flash-medium", "object": "model", "created": now, "owned_by": "google"},
		{"id": "gemini-3.8-flash-low", "object": "model", "created": now, "owned_by": "google"},
		{"id": "gemini-2.5-pro", "object": "model", "created": now, "owned_by": "google"},
		{"id": "gemini-2.5-flash", "object": "model", "created": now, "owned_by": "google"},
		{"id": "gpt-5", "object": "model", "created": now, "owned_by": "openai"},
		{"id": "gpt-5.5", "object": "model", "created": now, "owned_by": "openai"},
		{"id": "codex", "object": "model", "created": now, "owned_by": "openai"},
		{"id": "o3", "object": "model", "created": now, "owned_by": "openai"},
		{"id": "gpt-oss-120b-medium", "object": "model", "created": now, "owned_by": "openai"},
		{"id": "claude-sonnet-4-6", "object": "model", "created": now, "owned_by": "anthropic"},
		{"id": "claude-opus-4-6-thinking", "object": "model", "created": now, "owned_by": "anthropic"},
	}
	for _, m := range s.models() {
		data = append(data, map[string]any{
			"id":       m.Name,
			"object":   "model",
			"created":  now,
			"owned_by": "custom",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
	})
}

// ---- OpenAI Chat Completions ---------------------------------------------

var (
	mediaHTTPClient   = &http.Client{Timeout: 15 * time.Second}
	keepAliveInterval = 5 * time.Second
)

func fetchRemoteMedia(urlStr string) (string, []byte, error) {
	resp, err := mediaHTTPClient.Get(urlStr)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("remote media returned HTTP %d", resp.StatusCode)
	}
	mime := resp.Header.Get("Content-Type")
	if idx := strings.Index(mime, ";"); idx >= 0 {
		mime = strings.TrimSpace(mime[:idx])
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 25*1024*1024))
	if err != nil {
		return "", nil, err
	}
	if mime == "" || mime == "application/octet-stream" {
		mime = http.DetectContentType(data)
	}
	return mime, data, nil
}

func mimeForPath(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	case ".mp3":
		return "audio/mp3"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".m4a":
		return "audio/m4a"
	}
	detected := http.DetectContentType(data)
	if detected != "" && detected != "application/octet-stream" {
		return detected
	}
	return "application/octet-stream"
}

func resolveMedia(uriOrPath string) (string, []byte, error) {
	if strings.HasPrefix(uriOrPath, "http://") || strings.HasPrefix(uriOrPath, "https://") {
		return fetchRemoteMedia(uriOrPath)
	}
	// Check file:// or local filesystem path
	filePath := uriOrPath
	if strings.HasPrefix(filePath, "file://") {
		filePath = strings.TrimPrefix(filePath, "file://")
		if strings.HasPrefix(filePath, "localhost/") {
			filePath = strings.TrimPrefix(filePath, "localhost")
		}
		if unescaped, err := url.PathUnescape(filePath); err == nil {
			filePath = unescaped
		}
	} else if strings.HasPrefix(filePath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			filePath = filepath.Join(home, strings.TrimPrefix(filePath, "~/"))
		}
	}
	if filepath.IsAbs(filePath) {
		fi, err := os.Stat(filePath)
		if err == nil && !fi.IsDir() {
			if fi.Size() > 25*1024*1024 {
				return "", nil, fmt.Errorf("file %s exceeds 25MB limit", filePath)
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				return "", nil, err
			}
			mimeType := mimeForPath(filePath, data)
			return mimeType, data, nil
		}
	}
	return "", nil, fmt.Errorf("unrecognized media source: %s", uriOrPath)
}

func (s *Server) handleOpenAIChat(w http.ResponseWriter, r *http.Request, body []byte) {
	setCORSHeaders(w)
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required for /v1/chat/completions")
		return
	}

	var req codec.OpenAIChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON: "+err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeOpenAIError(w, http.StatusBadRequest, "missing_messages", "`messages` array cannot be empty")
		return
	}

	turn := codec.OpenAIToTurn(req, codec.Media{Resolve: resolveMedia, DetectMIME: http.DetectContentType}, strconv.FormatInt(geminicode.NewSessionID(), 10), geminicode.NewRequestID())
	turn.SystemPrompt = cloakSystemPrompt(turn.SystemPrompt)
	canonicalModel := turn.Model
	family := accountrouting.ModelFamily(canonicalModel)

	ctx := r.Context()
	resp, currentEmail, err := s.executeBridgeRequest(ctx, turn, r.Header.Get("Authorization"))
	recordAccount(w, currentEmail)
	if err != nil {
		accTag := ""
		if currentEmail != "" {
			accTag = " [" + currentEmail + "]"
		}
		s.logf("[bridge]%s upstream error: %v", accTag, err)
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", formatClientErrorMessage(currentEmail, err))
		return
	}
	defer resp.Body.Close()

	reqID := "chatcmpl-" + newUUIDv4()
	if req.Stream {
		s.streamOpenAI(w, r, resp, canonicalModel, reqID, currentEmail, family)
		return
	}
	s.unaryOpenAI(w, resp, canonicalModel, reqID, currentEmail)
}

func (s *Server) streamOpenAI(w http.ResponseWriter, r *http.Request, resp *http.Response, model, reqID, currentEmail, family string) {
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	setCORSHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	decoder := geminicode.NewDecoder(nil)

	converter := codec.NewOpenAIStream(reqID, model, time.Now().Unix())

	emitChunk := func(chunkObj map[string]any) {
		data, _ := json.Marshal(chunkObj)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	linesChan := make(chan string, 32)
	go func() {
		defer close(linesChan)
		for scanner.Scan() {
			linesChan <- scanner.Text()
		}
	}()

	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// SSE keep-alive heartbeat comment during reasoning / silent wait
			_, _ = fmt.Fprint(w, ": keep-alive\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		case textLine, ok := <-linesChan:
			if !ok {
				goto OpenAIDone
			}
			chunks := decoder.Feed(textLine)
			for _, c := range chunks {
				if c.Error != nil {
					accTag := ""
					if currentEmail != "" {
						accTag = " [" + currentEmail + "]"
					}
					s.logf("[bridge]%s in-stream error: %v", accTag, c.Error)
					if sErr, ok := c.Error.(*geminicode.StreamError); ok {
						if isAcc, isGlobal, reason := isStreamAccountError(sErr); isAcc {
							if isGlobal {
								if strings.Contains(strings.ToLower(reason), "terms of service") || strings.Contains(strings.ToLower(reason), "disabled") {
									s.router.Quarantine(currentEmail, reason)
								} else {
									s.router.Exclude(currentEmail)
								}
							} else {
								s.router.ExcludeForModel(currentEmail, family)
							}
							s.logf("[bridge] %s in-stream %s; excluded for future turns", currentEmail, reason)
						}
					}
				}
				for _, chunk := range converter.Feed(c) {
					emitChunk(chunk)
				}
			}
		}
	}

OpenAIDone:
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) unaryOpenAI(w http.ResponseWriter, resp *http.Response, model, reqID, currentEmail string) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	decoder := geminicode.NewDecoder(nil)

	accumulator := &codec.Accumulator{}
	for scanner.Scan() {
		for _, chunk := range decoder.Feed(scanner.Text()) {
			accumulator.Feed(chunk)
		}
	}
	result := accumulator.Completion()

	if result.Error != nil && result.Text == "" && len(result.ToolCalls) == 0 {
		accTag := ""
		if currentEmail != "" {
			accTag = " [" + currentEmail + "]"
		}
		s.logf("[bridge]%s upstream stream error: %v", accTag, result.Error)
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", formatClientErrorMessage(currentEmail, result.Error))
		return
	}

	out := codec.OpenAICompletion(reqID, model, time.Now().Unix(), result.Text, result.Thinking, result.ToolCalls, result.Meta)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// ---- Anthropic Messages ---------------------------------------------------

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request, body []byte) {
	setCORSHeaders(w)
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "invalid_request_error", "POST required for /v1/messages")
		return
	}

	var req codec.AnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "`messages` array cannot be empty")
		return
	}

	turn := codec.AnthropicToTurn(req, codec.Media{Resolve: resolveMedia, DetectMIME: http.DetectContentType}, strconv.FormatInt(geminicode.NewSessionID(), 10), geminicode.NewRequestID())
	turn.SystemPrompt = cloakSystemPrompt(turn.SystemPrompt)
	canonicalModel := turn.Model
	family := accountrouting.ModelFamily(canonicalModel)

	ctx := r.Context()
	resp, currentEmail, err := s.executeBridgeRequest(ctx, turn, r.Header.Get("Authorization"))
	recordAccount(w, currentEmail)
	if err != nil {
		accTag := ""
		if currentEmail != "" {
			accTag = " [" + currentEmail + "]"
		}
		s.logf("[bridge]%s upstream error: %v", accTag, err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", formatClientErrorMessage(currentEmail, err))
		return
	}
	defer resp.Body.Close()

	msgID := newAnthropicID()
	if req.Stream {
		s.streamAnthropic(w, r, resp, canonicalModel, msgID, currentEmail, family)
		return
	}
	s.unaryAnthropic(w, resp, canonicalModel, msgID, currentEmail)
}

func (s *Server) streamAnthropic(w http.ResponseWriter, r *http.Request, resp *http.Response, model, msgID, currentEmail, family string) {
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	setCORSHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	emitEvent := func(event string, payload any) {
		data, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	converter := &codec.AnthropicStream{}
	start := converter.Start(msgID, model)
	emitEvent(start.Name, start.Payload)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	decoder := geminicode.NewDecoder(nil)

	linesChan := make(chan string, 32)
	go func() {
		defer close(linesChan)
		for scanner.Scan() {
			linesChan <- scanner.Text()
		}
	}()

	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Anthropic SSE ping event
			_, _ = fmt.Fprintf(w, "event: ping\ndata: {\"type\": \"ping\"}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		case textLine, ok := <-linesChan:
			if !ok {
				goto AnthropicDone
			}
			chunks := decoder.Feed(textLine)
			for _, c := range chunks {
				if c.Error != nil {
					accTag := ""
					if currentEmail != "" {
						accTag = " [" + currentEmail + "]"
					}
					s.logf("[bridge]%s in-stream error: %v", accTag, c.Error)
					if sErr, ok := c.Error.(*geminicode.StreamError); ok {
						if isAcc, isGlobal, reason := isStreamAccountError(sErr); isAcc {
							if isGlobal {
								if strings.Contains(strings.ToLower(reason), "terms of service") || strings.Contains(strings.ToLower(reason), "disabled") {
									s.router.Quarantine(currentEmail, reason)
								} else {
									s.router.Exclude(currentEmail)
								}
							} else {
								s.router.ExcludeForModel(currentEmail, family)
							}
							s.logf("[bridge] %s in-stream %s; excluded for future turns", currentEmail, reason)
						}
					}
				}
				for _, event := range converter.Feed(c) {
					emitEvent(event.Name, event.Payload)
				}
			}
		}
	}

AnthropicDone:
	for _, event := range converter.Finish(len(decoder.Calls()) > 0) {
		emitEvent(event.Name, event.Payload)
	}
}

func (s *Server) unaryAnthropic(w http.ResponseWriter, resp *http.Response, model, msgID, currentEmail string) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	decoder := geminicode.NewDecoder(nil)

	accumulator := &codec.Accumulator{}
	for scanner.Scan() {
		for _, chunk := range decoder.Feed(scanner.Text()) {
			accumulator.Feed(chunk)
		}
	}
	result := accumulator.Completion()

	if result.Error != nil && result.Text == "" && len(result.ToolCalls) == 0 {
		accTag := ""
		if currentEmail != "" {
			accTag = " [" + currentEmail + "]"
		}
		s.logf("[bridge]%s upstream stream error: %v", accTag, result.Error)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", formatClientErrorMessage(currentEmail, result.Error))
		return
	}

	out := codec.AnthropicMessageResponse(msgID, model, result.Text, result.Thinking, result.ToolCalls, result.Meta)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// ---- Upstream Execution & Account Pool Balancing -------------------------

func (s *Server) executeBridgeRequest(ctx context.Context, turn geminicode.Turn, clientAuthHeader string) (*http.Response, string, error) {
	family := accountrouting.ModelFamily(turn.Model)
	upstreamURL := joinPath(s.upstream.String(), geminicode.StreamPath)

	maxAttempts := 1
	if s.balanceActive() {
		policy := s.effectivePolicy()
		if policy != nil && policy.RoutingEnabled() {
			cfg := policy.Normalised()
			maxAttempts = cfg.MaxAttempts
			if s.aggregateActive() {
				if cands, err := s.router.CandidateAccounts(ctx); err == nil && len(cands) > maxAttempts {
					maxAttempts = len(cands)
				}
			}
		}
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	var lastAttemptEmail string
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var token, currentEmail string
		if s.balanceActive() {
			res, err := s.router.RouteTurn(ctx, family)
			if err == nil {
				token = res.AccessToken
				currentEmail = res.Email
			}
		}
		if token == "" {
			if strings.HasPrefix(clientAuthHeader, "Bearer ya29.") {
				token = strings.TrimPrefix(clientAuthHeader, "Bearer ")
			} else if tok, err := auth.LoadAntigravityToken(); err == nil && tok != nil && tok.Token.AccessToken != "" {
				token = tok.Token.AccessToken
			}
		}
		if currentEmail == "" {
			currentEmail = s.resolveAccount(currentEmail, clientAuthHeader)
		}
		lastAttemptEmail = currentEmail

		reqEnv := geminicode.BuildRequest(turn)
		bodyBytes, err := json.Marshal(reqEnv)
		if err != nil {
			return nil, lastAttemptEmail, fmt.Errorf("marshal request: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, lastAttemptEmail, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("User-Agent", geminicode.UserAgent())
		if token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+token)
		}

		if s.cap != nil {
			s.cap.write(captureRecord{
				TS:             time.Now(),
				Kind:           "request",
				Method:         "bridge_upstream",
				HTTPMethod:     http.MethodPost,
				Path:           upstreamURL,
				Model:          turn.Model,
				RequestBody:    rawBody(bodyBytes),
				RequestHeaders: httpReq.Header.Clone(),
			})
		}

		resp, err := s.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			errBytes, _ := readAll(resp.Body, maxRequestBody)
			resp.Body.Close()
			if s.cap != nil {
				s.cap.write(captureRecord{
					TS:           time.Now(),
					Kind:         "response",
					Method:       "bridge_upstream",
					HTTPMethod:   http.MethodPost,
					Path:         upstreamURL,
					Model:        turn.Model,
					Status:       resp.StatusCode,
					ResponseBody: rawBody(errBytes),
				})
			}

			kind, reasonText := classifyAccountFailure(resp.StatusCode, errBytes)
			if kind != failureNone {
				reason, _, appealURL := extractErrorInfo(errBytes)
				appealText := ""
				if appealURL != "" {
					appealText = fmt.Sprintf(" | appeal: %s", appealURL)
				}
				reasonDesc := reasonText
				if reason != "" {
					reasonDesc = fmt.Sprintf("%s: %s", reasonText, reason)
				}

				if kind == failureQuotaExceeded {
					if currentEmail != "" {
						s.router.ExcludeForModel(currentEmail, family)
					}
				} else if kind == failureAccountDisabled {
					if currentEmail != "" {
						s.router.Quarantine(currentEmail, reasonDesc)
					}
				} else {
					if currentEmail != "" {
						s.router.Exclude(currentEmail)
					}
				}

				if attempt+1 < maxAttempts {
					s.logf("[bridge] %s %s (%d)%s; excluding and retrying (attempt %d/%d)",
						currentEmail, reasonDesc, resp.StatusCode, appealText, attempt+1, maxAttempts)
					lastErr = fmt.Errorf("upstream rejected with status %d: %s", resp.StatusCode, string(errBytes))
					continue
				}

				s.logf("[bridge] %s %s (%d)%s; pool exhausted (attempt %d/%d)",
					currentEmail, reasonDesc, resp.StatusCode, appealText, attempt+1, maxAttempts)
				return nil, currentEmail, fmt.Errorf("upstream rejected with status %d: %s", resp.StatusCode, string(errBytes))
			}

			return nil, currentEmail, fmt.Errorf("upstream error (%d): %s", resp.StatusCode, string(errBytes))
		}

		return resp, currentEmail, nil
	}

	if lastErr != nil {
		return nil, lastAttemptEmail, lastErr
	}
	return nil, lastAttemptEmail, fmt.Errorf("no available accounts in pool")
}

// Helpers for JSON errors

func writeOpenAIError(w http.ResponseWriter, status int, code, msg string) {
	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "invalid_request_error",
			"code":    code,
		},
	})
}

func writeAnthropicError(w http.ResponseWriter, status int, errType, msg string) {
	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errType,
			"message": msg,
		},
	})
}

func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
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

func newAnthropicID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "msg_000000000000"
	}
	return "msg_" + hex.EncodeToString(b[:])
}
