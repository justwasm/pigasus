package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed fixtures/models.json
var embeddedModelsJSON []byte

// CapturedExchange records one captured HTTP request and optional response.
type CapturedExchange struct {
	Timestamp  time.Time         `json:"timestamp"`
	ClientKey  string            `json:"clientKey"` // "agy" or "pigasus"
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body"`
	ParsedWire *WirePayload      `json:"parsedWire,omitempty"`
}

// ProxyServer runs a local CloudCode endpoint that captures wire payloads for inspection.
type ProxyServer struct {
	mu          sync.Mutex
	listener    net.Listener
	httpServer  *http.Server
	addr        string
	captureDir  string
	upstreamURL string
	exchanges   []*CapturedExchange
	clientExch  map[string][]*CapturedExchange
}

// NewProxyServer creates a proxy server on the given address.
func NewProxyServer(addr, captureDir, upstreamURL string) (*ProxyServer, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("wirealign server: listen failed on %s: %w", addr, err)
	}

	if captureDir != "" {
		_ = os.MkdirAll(captureDir, 0755)
	}

	ps := &ProxyServer{
		listener:    ln,
		addr:        ln.Addr().String(),
		captureDir:  captureDir,
		upstreamURL: upstreamURL,
		clientExch:  make(map[string][]*CapturedExchange),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", ps.handleDispatch)
	ps.httpServer = &http.Server{
		Handler: mux,
	}

	return ps, nil
}

// Start launches the HTTP server in a background goroutine.
func (ps *ProxyServer) Start() {
	go func() {
		_ = ps.httpServer.Serve(ps.listener)
	}()
}

// Close terminates the server.
func (ps *ProxyServer) Close() error {
	return ps.httpServer.Close()
}

// URL returns "http://127.0.0.1:PORT".
func (ps *ProxyServer) URL() string {
	return "http://" + ps.addr
}

// LatestExchange returns the latest captured exchange for a given client key ("agy" or "pigasus").
func (ps *ProxyServer) LatestExchange(clientKey string) *CapturedExchange {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	exchs := ps.clientExch[clientKey]
	if len(exchs) == 0 {
		return nil
	}
	return exchs[len(exchs)-1]
}

func (ps *ProxyServer) handleDispatch(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	clientKey := resolveClient(r, body)

	// Record exchange
	headerMap := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headerMap[k] = v[0]
		}
	}

	exch := &CapturedExchange{
		Timestamp: time.Now(),
		ClientKey: clientKey,
		Method:    r.Method,
		Path:      r.URL.Path,
		Headers:   headerMap,
		Body:      body,
	}

	if strings.Contains(r.URL.Path, "streamGenerateContent") || strings.Contains(r.URL.Path, "generateContent") {
		if payload, err := ParseWirePayload(body); err == nil {
			exch.ParsedWire = payload
		}
	}

	ps.mu.Lock()
	ps.exchanges = append(ps.exchanges, exch)
	ps.clientExch[clientKey] = append(ps.clientExch[clientKey], exch)
	ps.mu.Unlock()

	// Persist to disk if captureDir set
	if ps.captureDir != "" && exch.ParsedWire != nil {
		fname := fmt.Sprintf("%d_%s_streamGenerateContent.json", time.Now().UnixMilli(), clientKey)
		_ = os.WriteFile(filepath.Join(ps.captureDir, fname), body, 0644)
	}

	// 1. If upstream URL configured, forward request
	if ps.upstreamURL != "" {
		ps.forwardUpstream(w, r, body)
		return
	}

	// 2. Otherwise serve deterministic mock responses
	path := r.URL.Path
	switch {
	case strings.Contains(path, "streamGenerateContent"):
		ps.mockStreamGenerateContent(w, r, body, clientKey)
	case strings.Contains(path, "loadCodeAssist"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(DefaultMockCodeAssist())
	case strings.Contains(path, "onboardUser"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"done":true,"response":{"@type":"type.googleapis.com/google.internal.cloud.code.v1internal.OnboardUserResponse","cloudaicompanionProject":"aicode-consumers"}}`))
	case strings.Contains(path, "retrieveUserQuotaSummary"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"quotas":[]}`))
	case strings.Contains(path, "fetchAvailableModels"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(DefaultMockAvailableModels())
	case strings.Contains(path, "listExperiments"):
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"flags": []map[string]any{
				{
					"name":        "invoke-subagent-config",
					"stringValue": `{"enabled": true, "always_inherit_model": false}`,
				},
				{
					"name":      "enable-subagent-user-messaging",
					"boolValue": true,
				},
				{
					"name":      "enable-auto-permission-preset",
					"boolValue": true,
				},
				{
					"name":      "generic-system-prompt",
					"boolValue": true,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	default:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}
}

func (ps *ProxyServer) mockStreamGenerateContent(w http.ResponseWriter, r *http.Request, body []byte, clientKey string) {
	if payload, err := ParseWirePayload(body); err == nil && payload.SystemInstruction != "" {
		pDec := DeconstructPrompt(payload.SystemInstruction)
		tDec := DeconstructTools(payload.Tools)
		fmt.Printf("[wirealign:proxy] Intercepted %s: %d sections %v, %d tools -> generic=%v, sandboxed=%v\n",
			clientKey, len(pDec.SectionOrder), pDec.SectionOrder, tDec.Count, pDec.IsGenericPrompt, pDec.IsSandboxed)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Minimal STOP frame to complete turn immediately without delays or complex LLM simulation
	_, _ = fmt.Fprint(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"OK\"}]},\"finishReason\":\"STOP\"}]}}\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (ps *ProxyServer) forwardUpstream(w http.ResponseWriter, r *http.Request, body []byte) {
	targetURL := ps.upstreamURL + r.URL.RequestURI()
	req, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for k, vv := range r.Header {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func resolveClient(r *http.Request, body []byte) string {
	ua := r.UserAgent()
	if strings.Contains(strings.ToLower(ua), "pigasus") || strings.Contains(strings.ToLower(ua), "openagy") {
		return "pigasus"
	}
	if strings.Contains(string(body), `"userAgent":"openagy`) || strings.Contains(string(body), `"userAgent":"pigasus`) {
		return "pigasus"
	}
	return "agy"
}

// DefaultMockCodeAssist returns a valid Antigravity Pro tier response.
func DefaultMockCodeAssist() []byte {
	resp := map[string]any{
		"currentTier": map[string]any{
			"id":          "standard-tier",
			"name":        "Antigravity Pro",
			"description": "Google AI Pro (WireAlign)",
		},
		"paidTier": map[string]any{
			"id":          "standard-tier",
			"name":        "Antigravity Pro",
			"description": "Google AI Pro (WireAlign)",
		},
		"allowedTiers": []map[string]any{
			{
				"id":        "free-tier",
				"name":      "Antigravity",
				"isDefault": false,
			},
			{
				"id":        "standard-tier",
				"name":      "Antigravity Pro",
				"isDefault": true,
			},
		},
		"cloudaicompanionProject": "aicode-consumers",
		"gcpManaged":              false,
	}
	b, _ := json.Marshal(resp)
	return b
}

// DefaultMockAvailableModels returns standard CloudCode models catalog.
func DefaultMockAvailableModels() []byte {
	if len(embeddedModelsJSON) > 0 {
		return embeddedModelsJSON
	}
	return []byte(`{"models":{"gemini-3.8-flash-high":{"name":"models/gemini-3.8-flash-high","displayName":"Gemini 3.8 Flash (High)"}},"defaultAgentModelId":"gemini-3.8-flash-high"}`)
}
