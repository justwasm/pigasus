package agyproxy

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// isLiveWebSocket reports whether an inbound request is a Gemini Multimodal Live
// WebSocket connection (BidiGenerateContent).
func isLiveWebSocket(r *http.Request) bool {
	if !wantsSocket(r) {
		return false
	}
	path := strings.ToLower(r.URL.Path)
	return strings.Contains(path, "bidigeneratecontent") ||
		strings.HasPrefix(path, "/ws/google.ai.generativelanguage") ||
		strings.HasPrefix(path, "/ws/google.cloud.aiplatform") ||
		path == "/v1/live" || path == "/live"
}

// liveSocket manages the active connection pair for a proxied Gemini Live session.
type liveSocket struct {
	client   *websocket.Conn
	upstream *websocket.Conn
	closed   atomic.Bool
}

func (s *liveSocket) goingAway() {
	if s.closed.CompareAndSwap(false, true) {
		if s.client != nil {
			_ = s.client.Close(websocket.StatusGoingAway, "proxy is restarting")
		}
		if s.upstream != nil {
			_ = s.upstream.Close(websocket.StatusGoingAway, "proxy is restarting")
		}
	}
}

func (s *liveSocket) closeNow() {
	s.closed.Store(true)
	if s.client != nil {
		s.client.CloseNow()
	}
	if s.upstream != nil {
		s.upstream.CloseNow()
	}
}

// liveUpstreamURL resolves the target upstream WebSocket URL for Gemini Live.
func (s *Server) liveUpstreamURL(r *http.Request) string {
	upstreamBase := endpoints.Current().GenerativeLanguage
	// If a custom upstream is configured on Server (e.g. in test harness with httptest.Server):
	if s.upstream != nil && s.upstream.Host != "" && !strings.Contains(s.upstream.Host, "googleapis.com") {
		upstreamBase = s.upstream.String()
	}
	if upstreamBase == "" {
		upstreamBase = endpoints.DefaultGenerativeLanguage
	}

	u, err := url.Parse(upstreamBase)
	if err != nil || u.Host == "" {
		u, _ = url.Parse(endpoints.DefaultGenerativeLanguage)
	}

	scheme := "wss"
	if u.Scheme == "http" || u.Scheme == "ws" {
		scheme = "ws"
	}

	path := r.URL.Path
	if path == "/v1/live" || path == "/live" {
		path = "/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent"
	}

	return (&url.URL{
		Scheme:   scheme,
		Host:     u.Host,
		Path:     path,
		RawQuery: r.URL.RawQuery,
	}).String()
}

// serveLiveSocket accepts a Gemini Multimodal Live WebSocket client connection,
// establishes an upstream full-duplex session, and forwards audio frames and events.
func (s *Server) serveLiveSocket(w http.ResponseWriter, r *http.Request) {
	clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode:    websocket.CompressionNoContextTakeover,
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logf("live ws: accept failed: %v", err)
		return
	}
	defer clientConn.CloseNow()

	apiKey := r.Header.Get("x-goog-api-key")
	if apiKey == "" {
		apiKey = r.URL.Query().Get("key")
	}
	authHeader := r.Header.Get("Authorization")

	// If client provided a placeholder key or omitted credentials, inject from the proxy's store.
	if apiKey == "" || apiKey == "alt-backend" || apiKey == "proxy" {
		if k := endpoints.CurrentGeminiAPIKey(); k != "" {
			apiKey = k
		} else if authHeader == "" {
			if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil && tok.Token.AccessToken != "" {
				authHeader = "Bearer " + tok.Token.AccessToken
			}
		}
	}

	upstreamURL := s.liveUpstreamURL(r)

	upstreamHeader := make(http.Header)
	for k, vv := range r.Header {
		lk := strings.ToLower(k)
		if hopHeader(lk) || strings.HasPrefix(lk, "sec-websocket-") {
			continue
		}
		for _, v := range vv {
			upstreamHeader.Add(k, v)
		}
	}
	if apiKey != "" && apiKey != "alt-backend" && apiKey != "proxy" {
		upstreamHeader.Set("x-goog-api-key", apiKey)
	}
	if authHeader != "" {
		upstreamHeader.Set("Authorization", authHeader)
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	upstreamConn, _, err := websocket.Dial(ctx, upstreamURL, &websocket.DialOptions{
		HTTPHeader:      upstreamHeader,
		CompressionMode: websocket.CompressionNoContextTakeover,
	})
	if err != nil {
		s.logf("live ws: dial upstream %s failed: %v", upstreamURL, err)
		_ = clientConn.Close(websocket.StatusBadGateway, "upstream dial failed: "+err.Error())
		return
	}
	defer upstreamConn.CloseNow()

	ls := &liveSocket{client: clientConn, upstream: upstreamConn}
	if !s.sockets.addLive(ls) {
		_ = clientConn.Close(websocket.StatusGoingAway, "proxy is restarting")
		return
	}
	defer s.sockets.removeLive(ls)

	s.logf("live ws: connected client -> %s", upstreamURL)

	var clientFrames, upstreamFrames atomic.Int64
	var clientBytes, upstreamBytes atomic.Int64

	var wg sync.WaitGroup
	wg.Add(2)

	// Forward client -> upstream
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			msgType, data, err := clientConn.Read(ctx)
			if err != nil {
				return
			}
			clientFrames.Add(1)
			clientBytes.Add(int64(len(data)))
			if err := upstreamConn.Write(ctx, msgType, data); err != nil {
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
				return
			}
			upstreamFrames.Add(1)
			upstreamBytes.Add(int64(len(data)))
			if err := clientConn.Write(ctx, msgType, data); err != nil {
				return
			}
		}
	}()

	wg.Wait()
	s.logf("live ws: closed (client: %d frames, %d bytes; upstream: %d frames, %d bytes)",
		clientFrames.Load(), clientBytes.Load(), upstreamFrames.Load(), upstreamBytes.Load())
}
