package livechat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/geminicode"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
)

type ChatRequest struct {
	AuthMode      string `json:"auth_mode"`      // "aggregate", "oauth", "api_key", "proxy"
	Token         string `json:"token"`          // Google OAuth Bearer Token (optional in aggregate mode)
	APIKey        string `json:"api_key"`        // Google AI Studio Gemini API Key (optional)
	ProxyURL      string `json:"proxy_url"`      // Local Pigasus proxy base URL (optional)
	Model         string `json:"model"`          // Model identifier e.g. "gemini-3.8-flash"
	Prompt        string `json:"prompt"`         // User text prompt
	SystemInst    string `json:"system_inst"`    // Optional system instruction
	ThinkingLevel string `json:"thinking_level"` // Optional thinking level ("low", "medium", "high", "none")
	Stream        bool   `json:"stream"`         // Stream response using SSE
}

type ChatResponse struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
	Done  bool   `json:"done"`
}

var webHTTPClient = &http.Client{
	Timeout: 90 * time.Second,
}

func normalizeCloudCodeModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" || model == "gemini-3.8-flash" {
		return "gemini-3.8-flash-high"
	}
	return model
}

// HandleChat processes an incoming text chat request and writes output to the client.
func HandleChat(ctx context.Context, w http.ResponseWriter, req *ChatRequest) error {
	if req.Prompt == "" {
		return errors.New("empty prompt")
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "gemini-3.8-flash"
	}

	switch strings.ToLower(strings.TrimSpace(req.AuthMode)) {
	case "aggregate", "auto":
		return handleAggregateChat(ctx, w, req, model)
	case "oauth", "bearer":
		if strings.TrimSpace(req.Token) == "" {
			return handleAggregateChat(ctx, w, req, model)
		}
		return handleAggregateChat(ctx, w, req, model)
	case "api_key", "gemini_key", "aistudio":
		return handleAPIKeyChat(ctx, w, req, model)
	case "proxy":
		return handleProxyChat(ctx, w, req, model)
	default:
		// Smart fallback: explicit APIKey -> api_key; explicit ProxyURL without pool -> proxy; otherwise aggregate pool
		if strings.TrimSpace(req.APIKey) != "" {
			return handleAPIKeyChat(ctx, w, req, model)
		}
		if strings.TrimSpace(req.ProxyURL) != "" && strings.TrimSpace(req.Token) == "" {
			return handleProxyChat(ctx, w, req, model)
		}
		return handleAggregateChat(ctx, w, req, model)
	}
}

func buildCloudCodeChatBody(req *ChatRequest, model string) ([]byte, error) {
	wireModel := normalizeCloudCodeModel(model)
	turn := geminicode.Turn{
		Model:         wireModel,
		Project:       geminicode.DefaultProject,
		RequestType:   "agent",
		ThinkingLevel: strings.TrimSpace(req.ThinkingLevel),
		Messages: []geminicode.TurnMessage{
			{Role: "user", Content: req.Prompt},
		},
	}
	if req.SystemInst != "" {
		turn.SystemPrompt = req.SystemInst
	}
	cloudCodeReq := geminicode.BuildRequest(turn)
	bodyBytes, err := json.Marshal(cloudCodeReq)
	if err != nil {
		return nil, fmt.Errorf("marshal cloudcode request: %w", err)
	}
	return bodyBytes, nil
}

func doCloudCodeChatRequest(ctx context.Context, baseEndpoint, token string, bodyBytes []byte) (*http.Response, error) {
	targetURL := strings.TrimRight(baseEndpoint, "/") + geminicode.StreamPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Goog-Auth-Scope", "https://www.googleapis.com/auth/cloud-platform")
	httpReq.Header.Set("User-Agent", geminicode.UserAgent())
	return webHTTPClient.Do(httpReq)
}

// handleAggregateChat routes CloudCode chat requests through the quota-aware
// account pool, automatically backing off and failing over across healthy
// accounts on 503 (MODEL_CAPACITY_EXHAUSTED), 429, 401/403, or immediate SSE errors.
func handleAggregateChat(ctx context.Context, w http.ResponseWriter, req *ChatRequest, model string) error {
	bodyBytes, err := buildCloudCodeChatBody(req, model)
	if err != nil {
		return err
	}

	apiEndpoint := strings.TrimSpace(endpoints.Current().CloudCode)
	if apiEndpoint == "" {
		apiEndpoint = endpoints.DefaultCloudCode
	}
	sandboxEndpoint := strings.TrimSpace(endpoints.Current().CloudCodeSandbox)

	router := getAggregateRouter()
	policy := effectiveAggregatePolicy()
	maxAttempts := policy.MaxAttempts
	if maxAttempts < 3 {
		maxAttempts = 3
	}
	if cands, err := router.CandidateAccounts(ctx); err == nil && len(cands) > maxAttempts {
		maxAttempts = len(cands)
	}

	family := accountrouting.ModelFamily(normalizeCloudCodeModel(model))
	explicitToken := strings.TrimSpace(req.Token)
	var lastErr error
	var lastToken string
	var lastEmail string
	var lastKind aggregateFailureKind
	var lastReason string
	triedExplicit := false
	failedTokens := make(map[string]bool)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		var token, email string
		if explicitToken != "" && !triedExplicit && strings.EqualFold(strings.TrimSpace(req.AuthMode), "oauth") {
			token = explicitToken
			triedExplicit = true
		} else {
			res, routeErr := router.RouteTurn(ctx, family)
			if routeErr == nil && strings.TrimSpace(res.AccessToken) != "" {
				candTok := strings.TrimSpace(res.AccessToken)
				candEmail := strings.TrimSpace(res.Email)
				if failedTokens[candTok] && candEmail != "" && lastKind != aggFailureNone {
					applyAggregateFailure(router, candEmail, family, lastKind, lastReason)
					if res2, err2 := router.RouteTurn(ctx, family); err2 == nil && strings.TrimSpace(res2.AccessToken) != "" {
						candTok = strings.TrimSpace(res2.AccessToken)
						candEmail = strings.TrimSpace(res2.Email)
					}
				}
				token = candTok
				email = candEmail
			} else if explicitToken != "" && !triedExplicit {
				token = explicitToken
				triedExplicit = true
			} else if tok, tokErr := GetExistingActiveToken(ctx); tokErr == nil && tok != nil && strings.TrimSpace(tok.Token.AccessToken) != "" {
				token = strings.TrimSpace(tok.Token.AccessToken)
				email = strings.TrimSpace(tok.UserEmail())
			} else if routeErr != nil && lastErr == nil {
				lastErr = routeErr
			}
		}

		if token == "" {
			break
		}
		lastToken = token
		lastEmail = email

		resp, reqErr := doCloudCodeChatRequest(ctx, apiEndpoint, token, bodyBytes)
		if reqErr != nil {
			lastErr = fmt.Errorf("cloudcode request failed: %w", reqErr)
			if attempt+1 < maxAttempts {
				if waitErr := waitAggregateBackoff(ctx, attempt, ""); waitErr != nil {
					return waitErr
				}
				continue
			}
			break
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			raw, _ := io.ReadAll(resp.Body)
			retryAfter := resp.Header.Get("Retry-After")
			resp.Body.Close()

			lastErr = fmt.Errorf("cloudcode error HTTP %d: %s", resp.StatusCode, string(raw))
			kind, reason := classifyAggregateFailure(resp.StatusCode, raw)
			if kind != aggFailureNone {
				lastKind = kind
				lastReason = reason
				failedTokens[token] = true
				applyAggregateFailure(router, email, family, kind, reason)
				if attempt+1 < maxAttempts {
					log.Printf("[livechat-web] aggregate failover (%s, HTTP %d, account=%q, attempt %d/%d): %s",
						reason, resp.StatusCode, email, attempt+1, maxAttempts, model)
					if kind == aggFailureCapacityOrQuota {
						if waitErr := waitAggregateBackoff(ctx, attempt, retryAfter); waitErr != nil {
							return waitErr
						}
					}
					continue
				}
			}
			// Non-account error (e.g. 400 Bad Request) or final attempt
			if kind == aggFailureNone {
				return lastErr
			}
			break
		}

		canRetry := attempt+1 < maxAttempts
		retryKind, retryReason, streamErr := streamCloudCodeSSEResponseWithFailover(w, resp.Body, canRetry, func(kind aggregateFailureKind, reason string) {
			applyAggregateFailure(router, email, family, kind, reason)
		})
		resp.Body.Close()
		if retryKind != aggFailureNone && canRetry {
			lastErr = streamErr
			lastKind = retryKind
			lastReason = retryReason
			failedTokens[token] = true
			applyAggregateFailure(router, email, family, retryKind, retryReason)
			log.Printf("[livechat-web] aggregate stream failover (%s, account=%q, attempt %d/%d): %s",
				retryReason, email, attempt+1, maxAttempts, model)
			if retryKind == aggFailureCapacityOrQuota {
				if waitErr := waitAggregateBackoff(ctx, attempt, ""); waitErr != nil {
					return waitErr
				}
			}
			continue
		}
		return streamErr
	}

	// Sandbox mirror fallback if primary CloudCode endpoint failed with 503/5xx/capacity
	if lastToken != "" && sandboxEndpoint != "" && !strings.EqualFold(strings.TrimRight(sandboxEndpoint, "/"), strings.TrimRight(apiEndpoint, "/")) &&
		(apiEndpoint == endpoints.DefaultCloudCode || apiEndpoint == endpoints.Current().CloudCode) {
		if resp, sErr := doCloudCodeChatRequest(ctx, sandboxEndpoint, lastToken, bodyBytes); sErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				log.Printf("[livechat-web] aggregate sandbox mirror succeeded (account=%q, model=%s)", lastEmail, model)
				_, _, streamErr := streamCloudCodeSSEResponseWithFailover(w, resp.Body, false, nil)
				return streamErr
			}
		}
	}

	// Fallback to machine Gemini API key or configured proxy if OAuth pool is empty or exhausted
	if strings.TrimSpace(req.APIKey) != "" || currentGeminiAPIKeyFn() != "" {
		log.Printf("[livechat-web] aggregate pool exhausted/unavailable (%v); falling back to Gemini API key", lastErr)
		return handleAPIKeyChat(ctx, w, req, model)
	}
	if strings.TrimSpace(req.ProxyURL) != "" {
		return handleProxyChat(ctx, w, req, model)
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("no healthy accounts or credentials available in aggregate pool")
}

// handleOAuthChat communicates with CloudCode API using a single OAuth Bearer token.
func handleOAuthChat(ctx context.Context, w http.ResponseWriter, req *ChatRequest, model string) error {
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return errors.New("missing Google OAuth bearer token")
	}

	bodyBytes, err := buildCloudCodeChatBody(req, model)
	if err != nil {
		return err
	}

	apiEndpoint := endpoints.Current().CloudCode
	if apiEndpoint == "" {
		apiEndpoint = endpoints.DefaultCloudCode
	}

	resp, err := doCloudCodeChatRequest(ctx, apiEndpoint, token, bodyBytes)
	if err != nil {
		return fmt.Errorf("cloudcode request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudcode error HTTP %d: %s", resp.StatusCode, string(raw))
	}

	return streamCloudCodeSSEResponse(w, resp.Body)
}

// handleAPIKeyChat communicates with Google AI Studio Gemini API
func handleAPIKeyChat(ctx context.Context, w http.ResponseWriter, req *ChatRequest, model string) error {
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		apiKey = currentGeminiAPIKeyFn()
	}
	if apiKey == "" {
		return errors.New("missing Gemini API key")
	}

	cleanModel := strings.TrimPrefix(model, "models/")
	// If model is Gemini 3.8 Flash, AI Studio maps to gemini-2.5-flash / gemini-2.0-flash
	if strings.HasPrefix(cleanModel, "gemini-3.8") {
		cleanModel = "gemini-2.5-flash"
	}
	baseURL := strings.TrimRight(strings.TrimSpace(endpoints.Current().GenerativeLanguage), "/")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	targetURL := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", baseURL, cleanModel, apiKey)

	contents := []map[string]interface{}{
		{
			"role": "user",
			"parts": []map[string]interface{}{
				{"text": req.Prompt},
			},
		},
	}

	payload := map[string]interface{}{
		"contents": contents,
	}
	if req.SystemInst != "" {
		payload["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": req.SystemInst},
			},
		}
	}

	bodyBytes, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := webHTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("gemini api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gemini api error HTTP %d: %s", resp.StatusCode, string(raw))
	}

	return streamGeminiSSEResponse(w, resp.Body)
}

// handleProxyChat forwards the chat request to a local Pigasus proxy
func handleProxyChat(ctx context.Context, w http.ResponseWriter, req *ChatRequest, model string) error {
	proxyURL := strings.TrimSpace(req.ProxyURL)
	if proxyURL == "" {
		return errors.New("proxy mode selected but no Pigasus proxy URL is configured")
	}

	targetURL := strings.TrimRight(proxyURL, "/") + "/v1internal:streamGenerateContent?alt=sse"

	wireModel := normalizeCloudCodeModel(model)
	turn := geminicode.Turn{
		Model:         wireModel,
		Project:       geminicode.DefaultProject,
		RequestType:   "agent",
		ThinkingLevel: strings.TrimSpace(req.ThinkingLevel),
		Messages: []geminicode.TurnMessage{
			{Role: "user", Content: req.Prompt},
		},
	}
	if req.SystemInst != "" {
		turn.SystemPrompt = req.SystemInst
	}

	cloudCodeReq := geminicode.BuildRequest(turn)
	bodyBytes, _ := json.Marshal(cloudCodeReq)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", geminicode.UserAgent())

	if req.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.Token)
	}
	if req.APIKey != "" {
		httpReq.Header.Set("x-goog-api-key", req.APIKey)
	}

	resp, err := webHTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("pigasus proxy request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pigasus proxy error HTTP %d: %s", resp.StatusCode, string(raw))
	}

	return streamCloudCodeSSEResponse(w, resp.Body)
}

func streamCloudCodeSSEResponse(w http.ResponseWriter, r io.Reader) error {
	_, _, err := streamCloudCodeSSEResponseWithFailover(w, r, false, nil)
	return err
}

func streamCloudCodeSSEResponseWithFailover(
	w http.ResponseWriter,
	r io.Reader,
	canRetry bool,
	onMidStreamErr func(kind aggregateFailureKind, reason string),
) (aggregateFailureKind, string, error) {
	br := bufio.NewReader(r)
	var firstLines []string
	var firstData strings.Builder
	hasEventData := false

	// Buffer the first SSE event before committing HTTP 200 headers so immediate
	// quota/capacity/auth error frames can transparently fail over to another account.
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\r\n")
			firstLines = append(firstLines, trimmed)
			if trimmed == "" {
				if hasEventData {
					break
				}
			} else if !strings.HasPrefix(trimmed, ":") {
				if strings.HasPrefix(trimmed, "data:") {
					hasEventData = true
					if firstData.Len() > 0 {
						firstData.WriteByte('\n')
					}
					firstData.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
				} else {
					hasEventData = true
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				if canRetry && !hasEventData {
					return aggFailureCapacityOrQuota, "stream read error", err
				}
			}
			break
		}
	}

	if firstData.Len() > 0 {
		if sErr := geminicode.ParseFrameError(firstData.String()); sErr != nil {
			kind, reason := classifyAggregateStreamError(sErr)
			if kind != aggFailureNone {
				if canRetry {
					return kind, reason, fmt.Errorf("cloudcode stream error %d (%s): %s", sErr.Code, sErr.Status, sErr.Message)
				}
				if onMidStreamErr != nil {
					onMidStreamErr(kind, reason)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	decoder := geminicode.NewDecoder(nil)
	emitChunks := func(chunks []geminicode.Chunk) {
		for _, chunk := range chunks {
			if chunk.Text != "" {
				data, _ := json.Marshal(ChatResponse{Text: chunk.Text})
				fmt.Fprintf(w, "data: %s\n\n", data)
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
	}

	for _, line := range firstLines {
		emitChunks(decoder.Feed(line))
	}

	scanner := bufio.NewScanner(br)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var midData strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if midData.Len() > 0 {
				if sErr := geminicode.ParseFrameError(midData.String()); sErr != nil && onMidStreamErr != nil {
					if kind, reason := classifyAggregateStreamError(sErr); kind != aggFailureNone {
						onMidStreamErr(kind, reason)
					}
				}
				midData.Reset()
			}
		} else if strings.HasPrefix(line, "data:") {
			if midData.Len() > 0 {
				midData.WriteByte('\n')
			}
			midData.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		emitChunks(decoder.Feed(line))
	}

	finalChunks, _ := decoder.Finish(scanner.Err())
	emitChunks(finalChunks)

	fmt.Fprintf(w, "data: {\"done\":true}\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	return aggFailureNone, "", scanner.Err()
}

func streamGeminiSSEResponse(w http.ResponseWriter, r io.Reader) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	scanner := bufio.NewScanner(r)
	inThought := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataJSON := strings.TrimPrefix(line, "data: ")
			var geminiResp struct {
				Candidates []struct {
					Content struct {
						Parts []struct {
							Text    string `json:"text"`
							Thought bool   `json:"thought"`
						} `json:"parts"`
					} `json:"content"`
				} `json:"candidates"`
			}
			if err := json.Unmarshal([]byte(dataJSON), &geminiResp); err == nil {
				for _, cand := range geminiResp.Candidates {
					for _, part := range cand.Content.Parts {
						if part.Thought && !inThought {
							inThought = true
							out, _ := json.Marshal(ChatResponse{Text: "<think>"})
							fmt.Fprintf(w, "data: %s\n\n", out)
							if flusher != nil {
								flusher.Flush()
							}
						} else if !part.Thought && inThought {
							inThought = false
							out, _ := json.Marshal(ChatResponse{Text: "</think>"})
							fmt.Fprintf(w, "data: %s\n\n", out)
							if flusher != nil {
								flusher.Flush()
							}
						}
						if part.Text != "" {
							out, _ := json.Marshal(ChatResponse{Text: part.Text})
							fmt.Fprintf(w, "data: %s\n\n", out)
							if flusher != nil {
								flusher.Flush()
							}
						}
					}
				}
			}
		}
	}

	if inThought {
		out, _ := json.Marshal(ChatResponse{Text: "</think>"})
		fmt.Fprintf(w, "data: %s\n\n", out)
		if flusher != nil {
			flusher.Flush()
		}
	}

	fmt.Fprintf(w, "data: {\"done\":true}\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	return scanner.Err()
}
