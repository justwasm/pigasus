package livechat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/geminicode"
)

type ChatRequest struct {
	AuthMode   string `json:"auth_mode"`   // "oauth", "api_key", "proxy"
	Token      string `json:"token"`       // Google OAuth Bearer Token
	APIKey     string `json:"api_key"`     // Google AI Studio Gemini API Key
	ProxyURL   string `json:"proxy_url"`   // Local Pigasus proxy base URL
	Model      string `json:"model"`       // Model identifier e.g. "gemini-3.8-flash"
	Prompt     string `json:"prompt"`      // User text prompt
	SystemInst string `json:"system_inst"` // Optional system instruction
	Stream     bool   `json:"stream"`      // Stream response using SSE
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

	switch strings.ToLower(req.AuthMode) {
	case "oauth", "bearer":
		return handleOAuthChat(ctx, w, req, model)
	case "api_key", "gemini_key", "aistudio":
		return handleAPIKeyChat(ctx, w, req, model)
	case "proxy":
		return handleProxyChat(ctx, w, req, model)
	default:
		// Smart fallback: if APIKey provided use api_key, if Token provided use oauth, if ProxyURL provided use proxy
		if req.APIKey != "" {
			return handleAPIKeyChat(ctx, w, req, model)
		}
		if req.Token != "" {
			return handleOAuthChat(ctx, w, req, model)
		}
		if req.ProxyURL != "" {
			return handleProxyChat(ctx, w, req, model)
		}
		return errors.New("no valid authentication provided: specify OAuth token, Gemini API Key, or Pigasus proxy")
	}
}

// handleOAuthChat communicates with CloudCode API using OAuth Bearer token
func handleOAuthChat(ctx context.Context, w http.ResponseWriter, req *ChatRequest, model string) error {
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return errors.New("missing Google OAuth bearer token")
	}

	wireModel := normalizeCloudCodeModel(model)

	turn := geminicode.Turn{
		Model:       wireModel,
		Project:     geminicode.DefaultProject,
		RequestType: "agent",
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
		return fmt.Errorf("marshal cloudcode request: %w", err)
	}

	apiEndpoint := endpoints.Current().CloudCode
	if apiEndpoint == "" {
		apiEndpoint = endpoints.DefaultCloudCode
	}
	targetURL := strings.TrimRight(apiEndpoint, "/") + geminicode.StreamPath

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Goog-Auth-Scope", "https://www.googleapis.com/auth/cloud-platform")
	httpReq.Header.Set("User-Agent", geminicode.UserAgent())

	resp, err := webHTTPClient.Do(httpReq)
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
		apiKey = endpoints.CurrentGeminiAPIKey()
	}
	if apiKey == "" {
		return errors.New("missing Gemini API key")
	}

	cleanModel := strings.TrimPrefix(model, "models/")
	// If model is Gemini 3.8 Flash, AI Studio maps to gemini-2.5-flash / gemini-2.0-flash
	if strings.HasPrefix(cleanModel, "gemini-3.8") {
		cleanModel = "gemini-2.5-flash"
	}
	targetURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", cleanModel, apiKey)

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
		proxyURL = "http://127.0.0.1:8085"
	}

	targetURL := strings.TrimRight(proxyURL, "/") + "/v1internal:streamGenerateContent?alt=sse"

	wireModel := normalizeCloudCodeModel(model)
	turn := geminicode.Turn{
		Model:       wireModel,
		Project:     geminicode.DefaultProject,
		RequestType: "agent",
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
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	decoder := geminicode.NewDecoder(nil)
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		for _, chunk := range decoder.Feed(line) {
			if chunk.Text != "" {
				data, _ := json.Marshal(ChatResponse{Text: chunk.Text})
				fmt.Fprintf(w, "data: %s\n\n", data)
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
	}

	finalChunks, _ := decoder.Finish(scanner.Err())
	for _, chunk := range finalChunks {
		if chunk.Text != "" {
			data, _ := json.Marshal(ChatResponse{Text: chunk.Text})
			fmt.Fprintf(w, "data: %s\n\n", data)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	fmt.Fprintf(w, "data: {\"done\":true}\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	return scanner.Err()
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
