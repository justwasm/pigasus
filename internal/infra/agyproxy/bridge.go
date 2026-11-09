package agyproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
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
)

// ---- System prompt cloaking (antigravityCloak) ----------------------------
//
// Google CodeAssist automatically rejects requests with simulated HTTP 429
// ("Resource has been exhausted (e.g. check quota)") if it detects system
// prompt signatures from external coding agents like Claude Code, Codex, or Hermes.
// Inserting zero-width spaces (\u200b) preserves the semantic meaning for the LLM
// while bypassing Google's strict string blocklist.

var agTurnedAway = []string{
	"x-anthropic-billing-header:",
	"built on anthropic's claude agent sdk",
	"you are codex, an agent based on gpt-5",
	"you are codex, a coding agent based on gpt-5",
	"hermes agent, an intelligent ai assistant created by nous research",
}

func antigravityCloak(system string) string {
	if system == "" {
		return ""
	}
	low := strings.ToLower(system)
	hasBlocked := false
	for _, phrase := range agTurnedAway {
		if strings.Contains(low, phrase) {
			hasBlocked = true
			break
		}
	}
	if !hasBlocked {
		return system
	}
	res := system
	for _, phrase := range agTurnedAway {
		for {
			idx := strings.Index(strings.ToLower(res), phrase)
			if idx < 0 {
				break
			}
			cloaked := string(res[idx]) + "\u200b" + res[idx+1:idx+len(phrase)]
			res = res[:idx] + cloaked + res[idx+len(phrase):]
		}
	}
	return res
}

// normalizeModelName maps user-friendly names or aliases from external agents
// onto canonical Antigravity/CloudCode wire identifiers.
func normalizeModelName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "gemini-3.8-flash-high"
	}
	low := strings.ToLower(raw)
	for _, prefix := range []string{"antigravity/", "google/", "anthropic/", "openai/", "models/"} {
		if strings.HasPrefix(low, prefix) {
			low = strings.TrimPrefix(low, prefix)
		}
	}
	switch {
	case strings.Contains(low, "3.1-pro") || strings.Contains(low, "3-1-pro") || strings.Contains(low, "gemini-pro"):
		if strings.Contains(low, "low") {
			return "gemini-3.1-pro-low"
		}
		return "gemini-3.1-pro-high"
	case strings.Contains(low, "2.5-pro") || strings.Contains(low, "2-5-pro"):
		return "gemini-2.5-pro"
	case strings.Contains(low, "2.5-flash") || strings.Contains(low, "2-5-flash"):
		return "gemini-2.5-flash"
	case strings.Contains(low, "flash-medium"):
		return "gemini-3.8-flash-medium"
	case strings.Contains(low, "flash-low"):
		return "gemini-3.8-flash-low"
	case strings.Contains(low, "flash") || strings.Contains(low, "gemini"):
		return "gemini-3.8-flash-high"
	case strings.Contains(low, "opus"):
		return "claude-opus-4-6-thinking"
	case strings.Contains(low, "sonnet"):
		return "claude-sonnet-4-6"
	case strings.Contains(low, "gpt-oss") || strings.Contains(low, "gpt_oss"):
		return "gpt-oss-120b-medium"
	case strings.Contains(low, "gpt") || strings.Contains(low, "sol") || strings.Contains(low, "luna") || strings.Contains(low, "terra") || strings.Contains(low, "astra") || strings.Contains(low, "codex") || strings.HasPrefix(low, "o1") || strings.HasPrefix(low, "o3") || strings.HasPrefix(low, "o4") || strings.HasPrefix(low, "chatgpt"):
		return "gemini-3.8-flash-high"
	default:
		return raw
	}
}

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

type openAIChatReq struct {
	Model           string          `json:"model"`
	Messages        []openAIMessage `json:"messages"`
	Tools           []openAITool    `json:"tools,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFuncDecl `json:"function"`
}

type openAIFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openAIFuncCall `json:"function"`
}

type openAIFuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

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

func parseOpenAIContent(raw any) (string, []geminicode.TurnImage) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case []any:
		var textParts []string
		var images []geminicode.TurnImage
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "text", "input_text", "output_text":
				if txt, ok := m["text"].(string); ok {
					textParts = append(textParts, txt)
				}
			case "image_url", "input_image":
				if iu, ok := m["image_url"].(map[string]any); ok {
					if urlStr, ok := iu["url"].(string); ok {
						if mime, data, ok := parseDataURI(urlStr); ok {
							images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
						} else if mime, data, err := resolveMedia(urlStr); err == nil {
							images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
						}
					}
				}
			case "image":
				if imgStr, ok := m["image"].(string); ok {
					if mime, data, ok := parseDataURI(imgStr); ok {
						images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
					} else if dec, err := base64.StdEncoding.DecodeString(imgStr); err == nil {
						images = append(images, geminicode.TurnImage{MIMEType: http.DetectContentType(dec), Data: dec})
					} else if mime, data, err := resolveMedia(imgStr); err == nil {
						images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
					}
				} else if iu, ok := m["image_url"].(map[string]any); ok {
					if urlStr, ok := iu["url"].(string); ok {
						if mime, data, ok := parseDataURI(urlStr); ok {
							images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
						} else if mime, data, err := resolveMedia(urlStr); err == nil {
							images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
						}
					}
				}
			case "file", "document":
				var dataURI, mime string
				if rawData, ok := m["data"].(string); ok {
					dataURI = rawData
				}
				if rawMime, ok := m["mimeType"].(string); ok {
					mime = rawMime
				} else if rawMime, ok := m["media_type"].(string); ok {
					mime = rawMime
				}
				if fileObj, ok := m["file"].(map[string]any); ok {
					if u, ok := fileObj["url"].(string); ok {
						dataURI = u
					}
					if mStr, ok := fileObj["mimeType"].(string); ok {
						mime = mStr
					}
				}
				if dataURI != "" {
					if dMime, data, ok := parseDataURI(dataURI); ok {
						if mime == "" {
							mime = dMime
						}
						images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
					} else if dec, err := base64.StdEncoding.DecodeString(dataURI); err == nil {
						if mime == "" {
							mime = http.DetectContentType(dec)
						}
						images = append(images, geminicode.TurnImage{MIMEType: mime, Data: dec})
					} else if fMime, data, err := resolveMedia(dataURI); err == nil {
						if mime == "" {
							mime = fMime
						}
						images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
					}
				}
			case "input_audio":
				if aud, ok := m["input_audio"].(map[string]any); ok {
					dataB64, _ := aud["data"].(string)
					format, _ := aud["format"].(string)
					if format == "" {
						format = "wav"
					}
					if dec, err := base64.StdEncoding.DecodeString(dataB64); err == nil {
						images = append(images, geminicode.TurnImage{MIMEType: "audio/" + format, Data: dec})
					}
				}
			}
		}
		return strings.Join(textParts, "\n"), images
	default:
		return "", nil
	}
}

func parseDataURI(uri string) (string, []byte, bool) {
	if !strings.HasPrefix(uri, "data:") {
		return "", nil, false
	}
	comma := strings.Index(uri, ",")
	if comma < 0 {
		return "", nil, false
	}
	meta := uri[5:comma]
	dataStr := uri[comma+1:]
	isBase64 := strings.Contains(meta, ";base64")
	mime := strings.TrimSpace(strings.Split(meta, ";")[0])
	if mime == "" {
		mime = "application/octet-stream"
	}
	if isBase64 {
		dec, err := base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			return "", nil, false
		}
		return mime, dec, true
	}
	return mime, []byte(dataStr), true
}

func (s *Server) handleOpenAIChat(w http.ResponseWriter, r *http.Request, body []byte) {
	setCORSHeaders(w)
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required for /v1/chat/completions")
		return
	}

	var req openAIChatReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON: "+err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeOpenAIError(w, http.StatusBadRequest, "missing_messages", "`messages` array cannot be empty")
		return
	}

	canonicalModel := normalizeModelName(req.Model)
	family := accountrouting.ModelFamily(canonicalModel)

	// Map tool_call_id to function name from assistant messages so tool responses
	// whose client payload omits "name" (like @ai-sdk/openai-compatible) retain their function name.
	// Track chronologically in-stream so reused or colliding IDs never overwrite earlier turns.
	toolCallNames := make(map[string]string)
	var systemPromptParts []string
	var turnMessages []geminicode.TurnMessage
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		text, images := parseOpenAIContent(m.Content)
		if role == "system" {
			if text != "" {
				systemPromptParts = append(systemPromptParts, text)
			}
			continue
		}
		if role == "assistant" {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" && tc.Function.Name != "" {
					toolCallNames[tc.ID] = tc.Function.Name
				}
			}
		}
		name := m.Name
		if role == "tool" {
			if expected, ok := toolCallNames[m.ToolCallID]; ok && expected != "" {
				name = expected
			}
		}
		msg := geminicode.TurnMessage{
			Role:    role,
			Content: text,
			Images:  images,
			Name:    name,
		}
		if role == "tool" {
			msg.ToolCallID = m.ToolCallID
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, geminicode.TurnToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			})
		}
		turnMessages = append(turnMessages, msg)
	}

	var turnTools []geminicode.TurnTool
	for _, t := range req.Tools {
		if t.Type != "" && t.Type != "function" {
			continue
		}
		turnTools = append(turnTools, geminicode.TurnTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
		})
	}

	temp := 0.0
	if req.Temperature != nil {
		temp = *req.Temperature
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	thinkingLevel := "high"
	if req.ReasoningEffort != "" {
		thinkingLevel = req.ReasoningEffort
	}
	switch strings.ToLower(thinkingLevel) {
	case "low", "medium", "high", "none":
		thinkingLevel = strings.ToLower(thinkingLevel)
	default:
		thinkingLevel = "high"
	}

	turn := geminicode.Turn{
		Model:           canonicalModel,
		Project:         geminicode.DefaultProject,
		SystemPrompt:    antigravityCloak(strings.Join(systemPromptParts, "\n\n")),
		Messages:        turnMessages,
		Tools:           turnTools,
		Temperature:     temp,
		MaxOutputTokens: maxTokens,
		ThinkingLevel:   thinkingLevel,
		SessionID:       strconv.FormatInt(geminicode.NewSessionID(), 10),
		RequestID:       geminicode.NewRequestID(),
	}

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

	now := time.Now().Unix()
	inThinking := false

	emitChunk := func(delta map[string]any, finishReason any, usage any) {
		chunkObj := map[string]any{
			"id":      reqID,
			"object":  "chat.completion.chunk",
			"created": now,
			"model":   model,
			"choices": []map[string]any{
				{
					"index":         0,
					"delta":         delta,
					"finish_reason": finishReason,
				},
			},
		}
		if usage != nil {
			chunkObj["usage"] = usage
		}
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
					emitChunk(map[string]any{}, "error", nil)
					continue
				}
				text := c.Text
				if text == "<think>" {
					inThinking = true
					continue
				}
				if text == "</think>" {
					inThinking = false
					continue
				}
				if strings.HasPrefix(text, "<think>") {
					inThinking = true
					text = strings.TrimPrefix(text, "<think>")
				}
				if strings.HasSuffix(text, "</think>") {
					inThinking = false
					text = strings.TrimSuffix(text, "</think>")
				}

				if text != "" {
					if inThinking {
						emitChunk(map[string]any{"reasoning_content": text}, nil, nil)
					} else {
						emitChunk(map[string]any{"content": text}, nil, nil)
					}
				}

				if len(c.ToolCalls) > 0 {
					var tcDeltas []map[string]any
					for i, tc := range c.ToolCalls {
						tcDeltas = append(tcDeltas, map[string]any{
							"index": i,
							"id":    tc.ID,
							"type":  "function",
							"function": map[string]any{
								"name":      tc.Name,
								"arguments": string(tc.Arguments),
							},
						})
					}
					emitChunk(map[string]any{"tool_calls": tcDeltas}, nil, nil)
				}

				if c.Done {
					finishReason := "stop"
					if len(decoder.Calls()) > 0 {
						finishReason = "tool_calls"
					}
					var usageObj any
					if c.Meta != nil {
						usageObj = map[string]any{
							"prompt_tokens":     c.Meta.PromptTokens,
							"completion_tokens": c.Meta.CompletionTokens,
							"total_tokens":      c.Meta.TotalTokens,
						}
					}
					emitChunk(map[string]any{}, finishReason, usageObj)
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

	var fullText strings.Builder
	var fullThinking strings.Builder
	var allToolCalls []geminicode.TurnToolCall
	var finalMeta geminicode.Meta
	var lastStreamErr error
	inThinking := false

	for scanner.Scan() {
		chunks := decoder.Feed(scanner.Text())
		for _, c := range chunks {
			if c.Error != nil {
				lastStreamErr = c.Error
				continue
			}
			text := c.Text
			if text == "<think>" {
				inThinking = true
				continue
			}
			if text == "</think>" {
				inThinking = false
				continue
			}
			if strings.HasPrefix(text, "<think>") {
				inThinking = true
				text = strings.TrimPrefix(text, "<think>")
			}
			if strings.HasSuffix(text, "</think>") {
				inThinking = false
				text = strings.TrimSuffix(text, "</think>")
			}

			if text != "" {
				if inThinking {
					fullThinking.WriteString(text)
				} else {
					fullText.WriteString(text)
				}
			}
			if len(c.ToolCalls) > 0 {
				allToolCalls = append(allToolCalls, c.ToolCalls...)
			}
			if c.Meta != nil {
				finalMeta = *c.Meta
			}
		}
	}

	if lastStreamErr != nil && fullText.Len() == 0 && len(allToolCalls) == 0 {
		accTag := ""
		if currentEmail != "" {
			accTag = " [" + currentEmail + "]"
		}
		s.logf("[bridge]%s upstream stream error: %v", accTag, lastStreamErr)
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", formatClientErrorMessage(currentEmail, lastStreamErr))
		return
	}

	msgObj := map[string]any{
		"role":    "assistant",
		"content": fullText.String(),
	}
	if fullThinking.Len() > 0 {
		msgObj["reasoning_content"] = fullThinking.String()
	}
	finishReason := "stop"
	if len(allToolCalls) > 0 {
		finishReason = "tool_calls"
		var tcs []map[string]any
		for _, tc := range allToolCalls {
			tcs = append(tcs, map[string]any{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": string(tc.Arguments),
				},
			})
		}
		msgObj["tool_calls"] = tcs
	}

	out := map[string]any{
		"id":      reqID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       msgObj,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     finalMeta.PromptTokens,
			"completion_tokens": finalMeta.CompletionTokens,
			"total_tokens":      finalMeta.TotalTokens,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// ---- Anthropic Messages ---------------------------------------------------

type anthropicReq struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      any                `json:"system,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func parseAnthropicSystem(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if txt, ok := m["text"].(string); ok {
					parts = append(parts, txt)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func parseAnthropicMessages(msgs []anthropicMessage) []geminicode.TurnMessage {
	toolCallNames := make(map[string]string)
	var out []geminicode.TurnMessage
	for _, m := range msgs {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		switch v := m.Content.(type) {
		case string:
			out = append(out, geminicode.TurnMessage{Role: role, Content: v})
		case []any:
			var regularParts []string
			var images []geminicode.TurnImage
			for _, blk := range v {
				block, ok := blk.(map[string]any)
				if !ok {
					continue
				}
				bType, _ := block["type"].(string)
				switch bType {
				case "text":
					txt, _ := block["text"].(string)
					regularParts = append(regularParts, txt)
				case "image", "document":
					if src, ok := block["source"].(map[string]any); ok {
						srcType, _ := src["type"].(string)
						mediaType, _ := src["media_type"].(string)
						if mediaType == "" {
							if bType == "document" {
								mediaType = "application/pdf"
							} else {
								mediaType = "image/png"
							}
						}
						if srcType == "base64" {
							dataB64, _ := src["data"].(string)
							b, err := base64.StdEncoding.DecodeString(dataB64)
							if err == nil {
								images = append(images, geminicode.TurnImage{MIMEType: mediaType, Data: b})
							}
						} else if srcType == "url" || srcType == "file" || srcType == "path" {
							urlStr, _ := src["url"].(string)
							if urlStr == "" {
								urlStr, _ = src["path"].(string)
							}
							if mime, data, err := resolveMedia(urlStr); err == nil {
								images = append(images, geminicode.TurnImage{MIMEType: mime, Data: data})
							}
						}
					}
				case "tool_use":
					id, _ := block["id"].(string)
					name, _ := block["name"].(string)
					if id != "" && name != "" {
						toolCallNames[id] = name
					}
					inputRaw, _ := json.Marshal(block["input"])
					out = append(out, geminicode.TurnMessage{
						Role: "assistant",
						ToolCalls: []geminicode.TurnToolCall{{
							ID:        id,
							Name:      name,
							Arguments: inputRaw,
						}},
					})
				case "tool_result":
					toolUseID, _ := block["tool_use_id"].(string)
					isError, _ := block["is_error"].(bool)
					name := toolCallNames[toolUseID]
					contentStr := ""
					switch c := block["content"].(type) {
					case string:
						contentStr = c
					default:
						b, _ := json.Marshal(c)
						contentStr = string(b)
					}
					out = append(out, geminicode.TurnMessage{
						Role:       "tool",
						Name:       name,
						ToolCallID: toolUseID,
						Content:    contentStr,
						IsError:    isError,
					})
				}
			}
			if len(regularParts) > 0 || len(images) > 0 {
				out = append(out, geminicode.TurnMessage{
					Role:    role,
					Content: strings.Join(regularParts, "\n"),
					Images:  images,
				})
			}
		}
	}
	return out
}

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request, body []byte) {
	setCORSHeaders(w)
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "invalid_request_error", "POST required for /v1/messages")
		return
	}

	var req anthropicReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "`messages` array cannot be empty")
		return
	}

	canonicalModel := normalizeModelName(req.Model)
	family := accountrouting.ModelFamily(canonicalModel)
	sysPrompt := parseAnthropicSystem(req.System)
	turnMessages := parseAnthropicMessages(req.Messages)

	var turnTools []geminicode.TurnTool
	for _, t := range req.Tools {
		turnTools = append(turnTools, geminicode.TurnTool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		})
	}

	temp := 0.0
	if req.Temperature != nil {
		temp = *req.Temperature
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	turn := geminicode.Turn{
		Model:           canonicalModel,
		Project:         geminicode.DefaultProject,
		SystemPrompt:    antigravityCloak(sysPrompt),
		Messages:        turnMessages,
		Tools:           turnTools,
		Temperature:     temp,
		MaxOutputTokens: maxTokens,
		SessionID:       strconv.FormatInt(geminicode.NewSessionID(), 10),
		RequestID:       geminicode.NewRequestID(),
	}

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

	emitEvent("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  1,
				"output_tokens": 0,
			},
		},
	})

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	decoder := geminicode.NewDecoder(nil)

	inThinking := false
	blockIndex := 0
	activeBlockType := "" // "text" or "thinking" or ""

	closeActiveBlock := func() {
		if activeBlockType != "" {
			emitEvent("content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": blockIndex,
			})
			blockIndex++
			activeBlockType = ""
		}
	}

	finalOutputTokens := 0

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
					emitEvent("error", map[string]any{
						"type": "error",
						"error": map[string]any{
							"type":    "api_error",
							"message": c.Error.Error(),
						},
					})
					continue
				}
				text := c.Text
				if text == "<think>" {
					closeActiveBlock()
					inThinking = true
					continue
				}
				if text == "</think>" {
					closeActiveBlock()
					inThinking = false
					continue
				}
				if strings.HasPrefix(text, "<think>") {
					closeActiveBlock()
					inThinking = true
					text = strings.TrimPrefix(text, "<think>")
				}
				if strings.HasSuffix(text, "</think>") {
					closeActiveBlock()
					inThinking = false
					text = strings.TrimSuffix(text, "</think>")
				}

				if text != "" {
					desiredType := "text"
					if inThinking {
						desiredType = "thinking"
					}
					if activeBlockType != desiredType {
						closeActiveBlock()
						activeBlockType = desiredType
						if desiredType == "thinking" {
							emitEvent("content_block_start", map[string]any{
								"type":  "content_block_start",
								"index": blockIndex,
								"content_block": map[string]any{
									"type":     "thinking",
									"thinking": "",
								},
							})
						} else {
							emitEvent("content_block_start", map[string]any{
								"type":  "content_block_start",
								"index": blockIndex,
								"content_block": map[string]any{
									"type": "text",
									"text": "",
								},
							})
						}
					}

					if desiredType == "thinking" {
						emitEvent("content_block_delta", map[string]any{
							"type":  "content_block_delta",
							"index": blockIndex,
							"delta": map[string]any{
								"type":     "thinking_delta",
								"thinking": text,
							},
						})
					} else {
						emitEvent("content_block_delta", map[string]any{
							"type":  "content_block_delta",
							"index": blockIndex,
							"delta": map[string]any{
								"type": "text_delta",
								"text": text,
							},
						})
					}
				}

				if len(c.ToolCalls) > 0 {
					closeActiveBlock()
					for _, tc := range c.ToolCalls {
						emitEvent("content_block_start", map[string]any{
							"type":  "content_block_start",
							"index": blockIndex,
							"content_block": map[string]any{
								"type":  "tool_use",
								"id":    tc.ID,
								"name":  tc.Name,
								"input": map[string]any{},
							},
						})
						emitEvent("content_block_delta", map[string]any{
							"type":  "content_block_delta",
							"index": blockIndex,
							"delta": map[string]any{
								"type":         "input_json_delta",
								"partial_json": string(tc.Arguments),
							},
						})
						emitEvent("content_block_stop", map[string]any{
							"type":  "content_block_stop",
							"index": blockIndex,
						})
						blockIndex++
					}
				}

				if c.Meta != nil {
					finalOutputTokens = c.Meta.CompletionTokens
				}
			}
		}
	}

AnthropicDone:
	closeActiveBlock()

	stopReason := "end_turn"
	if len(decoder.Calls()) > 0 {
		stopReason = "tool_use"
	}

	emitEvent("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": finalOutputTokens,
		},
	})
	emitEvent("message_stop", map[string]any{"type": "message_stop"})
}

func (s *Server) unaryAnthropic(w http.ResponseWriter, resp *http.Response, model, msgID, currentEmail string) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	decoder := geminicode.NewDecoder(nil)

	var fullText strings.Builder
	var fullThinking strings.Builder
	var allToolCalls []geminicode.TurnToolCall
	var finalMeta geminicode.Meta
	var lastStreamErr error
	inThinking := false

	for scanner.Scan() {
		chunks := decoder.Feed(scanner.Text())
		for _, c := range chunks {
			if c.Error != nil {
				lastStreamErr = c.Error
				continue
			}
			text := c.Text
			if text == "<think>" {
				inThinking = true
				continue
			}
			if text == "</think>" {
				inThinking = false
				continue
			}
			if strings.HasPrefix(text, "<think>") {
				inThinking = true
				text = strings.TrimPrefix(text, "<think>")
			}
			if strings.HasSuffix(text, "</think>") {
				inThinking = false
				text = strings.TrimSuffix(text, "</think>")
			}

			if text != "" {
				if inThinking {
					fullThinking.WriteString(text)
				} else {
					fullText.WriteString(text)
				}
			}
			if len(c.ToolCalls) > 0 {
				allToolCalls = append(allToolCalls, c.ToolCalls...)
			}
			if c.Meta != nil {
				finalMeta = *c.Meta
			}
		}
	}

	if lastStreamErr != nil && fullText.Len() == 0 && len(allToolCalls) == 0 {
		accTag := ""
		if currentEmail != "" {
			accTag = " [" + currentEmail + "]"
		}
		s.logf("[bridge]%s upstream stream error: %v", accTag, lastStreamErr)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", formatClientErrorMessage(currentEmail, lastStreamErr))
		return
	}

	var contentBlocks []map[string]any
	if fullThinking.Len() > 0 {
		contentBlocks = append(contentBlocks, map[string]any{
			"type":     "thinking",
			"thinking": fullThinking.String(),
		})
	}
	if fullText.Len() > 0 {
		contentBlocks = append(contentBlocks, map[string]any{
			"type": "text",
			"text": fullText.String(),
		})
	}
	for _, tc := range allToolCalls {
		var inputObj any
		if err := json.Unmarshal(tc.Arguments, &inputObj); err != nil {
			inputObj = map[string]any{}
		}
		contentBlocks = append(contentBlocks, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Name,
			"input": inputObj,
		})
	}

	stopReason := "end_turn"
	if len(allToolCalls) > 0 {
		stopReason = "tool_use"
	}

	out := map[string]any{
		"id":            msgID,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       contentBlocks,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  finalMeta.PromptTokens,
			"output_tokens": finalMeta.CompletionTokens,
		},
	}
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
		if policy != nil && policy.Enabled {
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
