package livechat

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/justwasm/pigasus/endpoints"
)

// HandleLiveWebSocket bridges the browser's WebSocket connection to Gemini Live
// (either directly or via Pigasus proxy), injecting auth credentials that browsers
// cannot attach to native WebSockets.
func HandleLiveWebSocket(w http.ResponseWriter, r *http.Request) {
	clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionNoContextTakeover,
		OriginPatterns:     []string{"*"},
	})
	if err != nil {
		log.Printf("[live-ws] accept client connection failed: %v", err)
		return
	}
	defer clientConn.CloseNow()
	// Disable default 32KB read limit to allow audio/image streaming frames (up to 32MB)
	clientConn.SetReadLimit(32 << 20)

	query := r.URL.Query()
	token := strings.TrimSpace(query.Get("token"))
	apiKey := strings.TrimSpace(query.Get("key"))
	proxyURL := strings.TrimSpace(query.Get("proxy"))

	upstreamURL, reqHeaders, err := resolveUpstream(proxyURL, token, apiKey)
	if err != nil {
		log.Printf("[live-ws] resolve upstream failed: %v", err)
		_ = clientConn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
	upstreamConn, _, err := websocket.Dial(dialCtx, upstreamURL, &websocket.DialOptions{
		HTTPHeader:      reqHeaders,
		CompressionMode: websocket.CompressionNoContextTakeover,
	})
	dialCancel()
	if err != nil {
		log.Printf("[live-ws] upstream dial failed (%s): %v", upstreamURL, err)
		_ = clientConn.Close(websocket.StatusBadGateway, fmt.Sprintf("upstream dial failed: %v", err))
		return
	}
	defer upstreamConn.CloseNow()
	// Disable default 32KB read limit to allow Gemini Live multimodal audio chunks (up to 32MB)
	upstreamConn.SetReadLimit(32 << 20)

	log.Printf("[live-ws] session bridged: client <-> %s", upstreamURL)

	var wg sync.WaitGroup
	wg.Add(2)

	// Forward client -> upstream
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			msgType, data, err := clientConn.Read(ctx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("[live-ws] client disconnected: %v (code %v)", err, websocket.CloseStatus(err))
				}
				return
			}
			if err := upstreamConn.Write(ctx, msgType, data); err != nil {
				if ctx.Err() == nil {
					log.Printf("[live-ws] write to upstream failed: %v", err)
				}
				return
			}
		}
	}()

	// Forward upstream -> client
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			msgType, data, err := upstreamConn.Read(ctx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("[live-ws] upstream closed: %v (code %v)", err, websocket.CloseStatus(err))
				}
				return
			}
			if err := clientConn.Write(ctx, msgType, data); err != nil {
				if ctx.Err() == nil {
					log.Printf("[live-ws] write to client failed: %v", err)
				}
				return
			}
		}
	}()

	wg.Wait()
	log.Printf("[live-ws] session terminated")
}

func resolveUpstream(proxyURL, token, apiKey string) (string, http.Header, error) {
	headers := make(http.Header)

	// 1. If Pigasus Proxy is specified explicitly
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Host == "" {
			return "", nil, fmt.Errorf("invalid pigasus proxy url: %s", proxyURL)
		}
		scheme := "ws"
		if u.Scheme == "https" || u.Scheme == "wss" {
			scheme = "wss"
		}
		target := fmt.Sprintf("%s://%s/v1/live", scheme, u.Host)
		if token != "" {
			headers.Set("Authorization", "Bearer "+token)
		}
		if apiKey != "" {
			headers.Set("x-goog-api-key", apiKey)
		}
		return target, headers, nil
	}

	// 2. Direct Gemini Live endpoint (Google AI Studio / GenerativeLanguage)
	// If API key not passed in request, look up existing machine key
	if apiKey == "" {
		apiKey = endpoints.CurrentGeminiAPIKey()
	}

	if apiKey != "" {
		base := endpoints.Current().Voice
		if base == "" {
			base = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent"
		}
		u, err := url.Parse(base)
		if err != nil {
			return "", nil, err
		}
		q := u.Query()
		q.Set("key", apiKey)
		u.RawQuery = q.Encode()
		return u.String(), headers, nil
	}

	// 3. If only OAuth token is available, check if local Pigasus proxy (8085) is listening
	if token != "" {
		// Test if 127.0.0.1:8085 is listening
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:8085", 500*time.Millisecond); err == nil {
			conn.Close()
			target := "ws://127.0.0.1:8085/v1/live"
			headers.Set("Authorization", "Bearer "+token)
			log.Printf("[live-ws] routing OAuth token to local Pigasus proxy at %s", target)
			return target, headers, nil
		}
		return "", nil, fmt.Errorf("Google GenerativeLanguage Live requires a Gemini API key or a running Pigasus proxy (http://127.0.0.1:8085) when using OAuth Bearer tokens")
	}

	return "", nil, fmt.Errorf("no credentials provided (neither token, api key, nor proxy)")
}
