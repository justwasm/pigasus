package livechat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/infra/livevoice"
	"github.com/justwasm/pigasus/internal/voice"
)

// oauthLiveBackendFactory creates the LiveBackend used when bridging OAuth Bearer
// tokens over /api/live-ws without an external proxy. Overridden in unit tests.
var oauthLiveBackendFactory = func(token string) voice.LiveBackend {
	return voice.NewAICodeLiveBackend(token)
}

// currentGeminiAPIKeyFn resolves the machine's configured Gemini API key.
// Overridden in unit tests to isolate from host ~/.openagy/config.json.
var currentGeminiAPIKeyFn = endpoints.CurrentGeminiAPIKey

// HandleLiveWebSocket bridges the browser's WebSocket connection to Gemini Live
// (directly via API key, via OAuth LiveBackend bridge, or via an explicit Pigasus proxy),
// injecting auth credentials that browsers cannot attach to native WebSockets.
func HandleLiveWebSocket(w http.ResponseWriter, r *http.Request) {
	handleLiveWebSocketWithDefaultProxy("")(w, r)
}

func handleLiveWebSocketWithDefaultProxy(defaultProxy string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		mode := strings.ToLower(strings.TrimSpace(query.Get("mode")))
		token := strings.TrimSpace(query.Get("token"))
		apiKey := strings.TrimSpace(query.Get("key"))
		proxyURL := strings.TrimSpace(query.Get("proxy"))

		if mode == "proxy" && proxyURL == "" {
			proxyURL = strings.TrimSpace(defaultProxy)
		}

		// If explicit proxy mode is requested, require a proxy URL.
		if mode == "proxy" && proxyURL == "" {
			_ = clientConn.Close(websocket.StatusPolicyViolation, "proxy mode selected but no Pigasus proxy URL is configured")
			return
		}

		// Aggregate mode (or unspecified mode with no explicit client credentials):
		// Route transparently via the shared quota-aware multi-account OAuth pool
		// with automatic failover & backoff, falling back to proxy or Gemini API key.
		if mode == "aggregate" || mode == "auto" || (mode == "" && token == "" && apiKey == "" && proxyURL == "") {
			bridgeAggregateLiveSession(r.Context(), clientConn, defaultProxy, token, apiKey)
			return
		}

		// Resolve machine credentials when omitted by the browser client.
		if mode != "proxy" && proxyURL == "" {
			if mode != "api_key" && token == "" {
				if tok, err := GetExistingActiveToken(r.Context()); err == nil && tok != nil && tok.Token.AccessToken != "" {
					token = tok.Token.AccessToken
				}
			}
			// Match TUI livechat & legacy Web UI behavior: if the machine has a configured
			// Gemini API key (~/.openagy/config.json or $GEMINI_API_KEY), prefer it for
			// Gemini Live WebSocket unless the user explicitly selected proxy mode.
			if apiKey == "" {
				apiKey = currentGeminiAPIKeyFn()
			}
			// If mode was unspecified and neither token nor apiKey was found, fall back to configured defaultProxy if any.
			if mode == "" && token == "" && apiKey == "" && defaultProxy != "" {
				proxyURL = strings.TrimSpace(defaultProxy)
			}
		}

		// OAuth Bearer token without explicit proxy or API key:
		// Bridge directly via Pigasus's native LiveBackend (AICode gRPC + Gemini Live Bearer fallback)
		// instead of requiring a separate proxy listening on 127.0.0.1:8085.
		if proxyURL == "" && apiKey == "" && (mode == "oauth" || token != "") {
			if token == "" {
				_ = clientConn.Close(websocket.StatusPolicyViolation, "missing Google OAuth bearer token; sign in under Settings -> Google OAuth or run `pigasus login`")
				return
			}
			bridgeOAuthLiveSession(r.Context(), clientConn, token)
			return
		}

		upstreamURL, reqHeaders, err := resolveUpstream(proxyURL, token, apiKey)
		if err != nil {
			log.Printf("[live-ws] resolve upstream failed: %v", err)
			_ = clientConn.Close(websocket.StatusPolicyViolation, err.Error())
			return
		}

		if !bridgeRawWebSocket(r.Context(), clientConn, upstreamURL, reqHeaders, nil) {
			_ = clientConn.Close(websocket.StatusBadGateway, "upstream dial failed")
		}
	}
}

func bridgeRawWebSocket(parentCtx context.Context, clientConn *websocket.Conn, upstreamURL string, reqHeaders http.Header, initialFrame []byte) bool {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
	upstreamConn, _, err := websocket.Dial(dialCtx, upstreamURL, &websocket.DialOptions{
		HTTPHeader:      reqHeaders,
		CompressionMode: websocket.CompressionNoContextTakeover,
	})
	dialCancel()
	if err != nil {
		log.Printf("[live-ws] upstream dial failed (%s): %v", upstreamURL, err)
		return false
	}
	defer upstreamConn.CloseNow()
	// Disable default 32KB read limit to allow Gemini Live multimodal audio chunks (up to 32MB)
	upstreamConn.SetReadLimit(32 << 20)

	if len(initialFrame) > 0 {
		if err := upstreamConn.Write(ctx, websocket.MessageText, initialFrame); err != nil {
			log.Printf("[live-ws] write initial setup frame failed (%s): %v", upstreamURL, err)
			return false
		}
	}

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
	return true
}

type browserLiveClientMsg struct {
	Setup *struct {
		Model            string `json:"model"`
		GenerationConfig *struct {
			SpeechConfig *struct {
				VoiceConfig *struct {
					PrebuiltVoiceConfig *struct {
						VoiceName string `json:"voiceName"`
					} `json:"prebuiltVoiceConfig"`
				} `json:"voiceConfig"`
			} `json:"speechConfig"`
		} `json:"generationConfig"`
		SystemInstruction *struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
	} `json:"setup"`
	RealtimeInput *struct {
		Audio *struct {
			MimeType string `json:"mimeType"`
			Data     string `json:"data"`
		} `json:"audio"`
		MediaChunks []struct {
			MimeType string `json:"mimeType"`
			Data     string `json:"data"`
		} `json:"mediaChunks"`
		AudioStreamEnd bool `json:"audioStreamEnd"`
	} `json:"realtimeInput"`
	ClientContent *struct {
		Turns []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"turns"`
		TurnComplete bool `json:"turnComplete"`
	} `json:"clientContent"`
}

func readInitialLiveSetup(ctx context.Context, clientConn *websocket.Conn) ([]byte, voice.LiveSessionConfig, bool) {
	setupCtx, setupCancel := context.WithTimeout(ctx, 15*time.Second)
	_, rawSetup, err := clientConn.Read(setupCtx)
	setupCancel()
	if err != nil {
		log.Printf("[live-ws] failed reading initial setup frame: %v", err)
		_ = clientConn.Close(websocket.StatusPolicyViolation, "expected initial setup message")
		return nil, voice.LiveSessionConfig{}, false
	}

	var setupMsg browserLiveClientMsg
	if err := json.Unmarshal(rawSetup, &setupMsg); err != nil || setupMsg.Setup == nil {
		_ = clientConn.Close(websocket.StatusPolicyViolation, "invalid initial setup payload")
		return nil, voice.LiveSessionConfig{}, false
	}

	cfg := voice.LiveSessionConfig{
		SampleRate: 16000,
		Model:      strings.TrimSpace(setupMsg.Setup.Model),
	}
	if setupMsg.Setup.GenerationConfig != nil &&
		setupMsg.Setup.GenerationConfig.SpeechConfig != nil &&
		setupMsg.Setup.GenerationConfig.SpeechConfig.VoiceConfig != nil &&
		setupMsg.Setup.GenerationConfig.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig != nil {
		cfg.Voice = strings.TrimSpace(setupMsg.Setup.GenerationConfig.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName)
	}
	if setupMsg.Setup.SystemInstruction != nil {
		var parts []string
		for _, p := range setupMsg.Setup.SystemInstruction.Parts {
			if strings.TrimSpace(p.Text) != "" {
				parts = append(parts, p.Text)
			}
		}
		cfg.SystemPrompt = strings.Join(parts, "\n")
	}
	return rawSetup, cfg, true
}

func bridgeAggregateLiveSession(parentCtx context.Context, clientConn *websocket.Conn, defaultProxy string, fallbackToken string, fallbackAPIKey string) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	rawSetup, cfg, ok := readInitialLiveSetup(ctx, clientConn)
	if !ok {
		return
	}

	liveSess, lastErr := livevoice.OpenPoolOAuthSession(ctx, cfg, livevoice.Options{
		BearerToken:  fallbackToken,
		OAuthFactory: oauthLiveBackendFactory,
		Logf:         log.Printf,
	})
	if liveSess != nil {
		bridgeLiveSessionLoop(ctx, cancel, clientConn, liveSess, cfg, nil)
		return
	}

	// Fallback to defaultProxy if configured
	if strings.TrimSpace(defaultProxy) != "" {
		if upstreamURL, reqHeaders, err := resolveUpstream(defaultProxy, "", ""); err == nil {
			if bridgeRawWebSocket(ctx, clientConn, upstreamURL, reqHeaders, rawSetup) {
				return
			}
		}
	}

	// Fallback to client-provided Gemini API key if given
	if clientKey := strings.TrimSpace(fallbackAPIKey); clientKey != "" {
		if upstreamURL, reqHeaders, err := resolveUpstream("", "", clientKey); err == nil {
			if bridgeRawWebSocket(ctx, clientConn, upstreamURL, reqHeaders, rawSetup) {
				return
			}
		}
	}

	// Fallback to machine Gemini API key if configured
	if machineKey := currentGeminiAPIKeyFn(); machineKey != "" {
		if upstreamURL, reqHeaders, err := resolveUpstream("", "", machineKey); err == nil {
			if bridgeRawWebSocket(ctx, clientConn, upstreamURL, reqHeaders, rawSetup) {
				return
			}
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no healthy accounts or credentials available in aggregate pool")
	}
	log.Printf("[live-ws] aggregate live session failed: %v", lastErr)
	reason := fmt.Sprintf("Aggregate live session failed: %v", lastErr)
	if strings.Contains(reason, "PermissionDenied") || strings.Contains(reason, "permission denied") || strings.Contains(reason, "insufficient authentication scopes") {
		reason = "OAuth accounts lack Gemini Live voice permission; configure gemini_api_key in Settings -> Gemini API Key or ~/.openagy/config.json"
	}
	_ = clientConn.Close(websocket.StatusPolicyViolation, truncateWSReason(reason))
}

func bridgeOAuthLiveSession(parentCtx context.Context, clientConn *websocket.Conn, token string) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	_, cfg, ok := readInitialLiveSetup(ctx, clientConn)
	if !ok {
		return
	}

	liveSess, err := livevoice.CreateSessionWithProbe(
		ctx,
		oauthLiveBackendFactory(token),
		cfg,
		livevoice.DefaultCreateTimeout,
		livevoice.DefaultHandshakeProbe,
	)
	if err != nil {
		log.Printf("[live-ws] OAuth live backend CreateSession failed: %v", err)
		_ = clientConn.Close(websocket.StatusBadGateway, truncateWSReason(fmt.Sprintf("OAuth live session failed: %v", err)))
		return
	}

	bridgeLiveSessionLoop(ctx, cancel, clientConn, liveSess, cfg, nil)
}

func bridgeLiveSessionLoop(
	ctx context.Context,
	cancel context.CancelFunc,
	clientConn *websocket.Conn,
	liveSess voice.LiveSession,
	cfg voice.LiveSessionConfig,
	onUpstreamErr func(error),
) {
	defer liveSess.Close()

	if err := clientConn.Write(ctx, websocket.MessageText, []byte(`{"setupComplete":{}}`)); err != nil {
		return
	}

	log.Printf("[live-ws] OAuth live session bridged (model=%q voice=%q)", cfg.Model, cfg.Voice)

	var wg sync.WaitGroup
	wg.Add(2)

	// Forward browser WebSocket -> voice.LiveSession
	go func() {
		defer wg.Done()
		for {
			_, data, err := clientConn.Read(ctx)
			if err != nil {
				cancel()
				return
			}
			var msg browserLiveClientMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg.RealtimeInput != nil {
				if msg.RealtimeInput.Audio != nil && msg.RealtimeInput.Audio.Data != "" {
					if pcm, err := base64.StdEncoding.DecodeString(msg.RealtimeInput.Audio.Data); err == nil && len(pcm) > 0 {
						if err := liveSess.SendAudio(pcm); err != nil {
							// Give the Receive goroutine a brief window to surface the real upstream error
							// and send a proper WebSocket close frame instead of dropping with 1006.
							time.AfterFunc(500*time.Millisecond, cancel)
							return
						}
					}
				}
				for _, mc := range msg.RealtimeInput.MediaChunks {
					if mc.Data == "" {
						continue
					}
					if raw, err := base64.StdEncoding.DecodeString(mc.Data); err == nil && len(raw) > 0 {
						if err := liveSess.SendMedia(mc.MimeType, raw); err != nil {
							time.AfterFunc(500*time.Millisecond, cancel)
							return
						}
					}
				}
			}
			if msg.ClientContent != nil {
				var texts []string
				for _, turn := range msg.ClientContent.Turns {
					for _, p := range turn.Parts {
						if p.Text != "" {
							texts = append(texts, p.Text)
						}
					}
				}
				if len(texts) > 0 {
					if err := liveSess.SendText(strings.Join(texts, "\n")); err != nil {
						time.AfterFunc(500*time.Millisecond, cancel)
						return
					}
				}
			}
		}
	}()

	// Forward voice.LiveSession -> browser WebSocket
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			ev, err := liveSess.Receive()
			if err != nil {
				if ctx.Err() == nil && err != io.EOF {
					log.Printf("[live-ws] OAuth live upstream closed: %v", err)
					if onUpstreamErr != nil {
						onUpstreamErr(err)
					}
					errStr := err.Error()
					if strings.Contains(errStr, "PermissionDenied") || strings.Contains(errStr, "Unauthenticated") {
						_ = clientConn.Close(websocket.StatusPolicyViolation, truncateWSReason("OAuth Live Voice permission denied; configure gemini_api_key in ~/.openagy/config.json or Settings -> Gemini API Key"))
					} else {
						_ = clientConn.Close(websocket.StatusBadGateway, truncateWSReason(errStr))
					}
				}
				return
			}
			if ev == nil {
				continue
			}
			for _, payload := range encodeLiveServerEvent(ev) {
				if err := clientConn.Write(ctx, websocket.MessageText, payload); err != nil {
					return
				}
			}
		}
	}()

	wg.Wait()
	log.Printf("[live-ws] OAuth live session terminated")
}

func truncateWSReason(s string) string {
	const maxBytes = 120
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes-3] + "..."
}

func encodeLiveServerEvent(ev *voice.LiveServerEvent) [][]byte {
	if ev == nil {
		return nil
	}
	var frames [][]byte

	if ev.Interrupted {
		if b, err := json.Marshal(map[string]any{
			"serverContent": map[string]any{
				"interrupted": true,
			},
		}); err == nil {
			frames = append(frames, b)
		}
	}

	if len(ev.Audio) > 0 {
		if b, err := json.Marshal(map[string]any{
			"serverContent": map[string]any{
				"modelTurn": map[string]any{
					"parts": []map[string]any{
						{
							"inlineData": map[string]any{
								"mimeType": "audio/pcm;rate=24000",
								"data":     base64.StdEncoding.EncodeToString(ev.Audio),
							},
						},
					},
				},
			},
		}); err == nil {
			frames = append(frames, b)
		}
	}

	if ev.VoiceTranscript != "" {
		if b, err := json.Marshal(map[string]any{
			"serverContent": map[string]any{
				"outputTranscription": map[string]any{
					"text": ev.VoiceTranscript,
				},
			},
		}); err == nil {
			frames = append(frames, b)
		}
	}

	if ev.UserSpeech != "" {
		key := "inputTranscription"
		if ev.Interim {
			key = "interimInputTranscription"
		}
		if b, err := json.Marshal(map[string]any{
			"serverContent": map[string]any{
				key: map[string]any{
					"text": ev.UserSpeech,
				},
			},
		}); err == nil {
			frames = append(frames, b)
		}
	}

	if ev.TurnComplete {
		if b, err := json.Marshal(map[string]any{
			"serverContent": map[string]any{
				"turnComplete": true,
			},
		}); err == nil {
			frames = append(frames, b)
		}
	}

	return frames
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

	// 2. Direct Gemini Live endpoint with explicit API Key
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

	// 3. Direct Gemini Live endpoint with OAuth Bearer token (no local 8085 proxy required)
	if token != "" {
		base := endpoints.Current().Voice
		if base == "" {
			base = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent"
		}
		headers.Set("Authorization", "Bearer "+token)
		return base, headers, nil
	}

	// 4. Fallback to machine Gemini API key if neither explicit key nor token was provided
	if machineKey := currentGeminiAPIKeyFn(); machineKey != "" {
		base := endpoints.Current().Voice
		if base == "" {
			base = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent"
		}
		u, err := url.Parse(base)
		if err != nil {
			return "", nil, err
		}
		q := u.Query()
		q.Set("key", machineKey)
		u.RawQuery = q.Encode()
		return u.String(), headers, nil
	}

	return "", nil, fmt.Errorf("no credentials provided (neither token, api key, nor proxy)")
}
