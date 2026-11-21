package livechat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/voice"
)

func runWebServer(port int, defaultProxy string, noBrowser bool, customWebDir string, workspace string) error {
	if defaultProxy == "" {
		if envProxy := os.Getenv("OPENAGY_VOICE_ALT_BACKEND"); envProxy != "" {
			defaultProxy = envProxy
		} else if envProxy := os.Getenv("PIGASUS_VOICE_ALT_BACKEND"); envProxy != "" {
			defaultProxy = envProxy
		} else {
			defaultProxy = "http://127.0.0.1:8085"
		}
	}

	mux := http.NewServeMux()

	// 1. Static Web Assets (Embedded or Custom Directory)
	webFS, sourceDesc := resolveWebFileSystem(customWebDir)
	log.Printf("[livechat-web] Serving Web UI from %s", sourceDesc)
	fileServer := http.FileServer(webFS)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Disable caching for live app development
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		fileServer.ServeHTTP(w, r)
	})

	// 2. OAuth Endpoints
	mux.HandleFunc("/api/auth/start", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sess, err := globalAuthStore.createSession()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"session_id": sess.ID,
			"auth_url":   sess.AuthURL,
			"state":      sess.State,
			"created_at": sess.CreatedAt,
		})
	})

	mux.HandleFunc("/api/auth/exchange", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			SessionID string `json:"session_id"`
			Code      string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request json: "+err.Error())
			return
		}

		tok, err := ExchangeToken(r.Context(), req.SessionID, req.Code)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}

		email := tok.UserEmail()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"access_token":  tok.Token.AccessToken,
			"refresh_token": tok.Token.RefreshToken,
			"token_type":    tok.Token.TokenType,
			"expiry":        tok.Token.Expiry,
			"email":         email,
			"auth_method":   tok.AuthMethod,
		})
	})

	mux.HandleFunc("/api/auth/existing", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		tok, err := GetExistingActiveToken(r.Context())
		if err != nil || tok == nil || tok.Token.AccessToken == "" {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"available": false,
				"message":   "no active CLI token found on machine",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"available":     true,
			"access_token":  tok.Token.AccessToken,
			"refresh_token": tok.Token.RefreshToken,
			"token_type":    tok.Token.TokenType,
			"expiry":        tok.Token.Expiry,
			"email":         tok.UserEmail(),
		})
	})

	mux.HandleFunc("/api/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request json: "+err.Error())
			return
		}

		tok, err := RefreshExistingToken(r.Context(), req.RefreshToken)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"access_token":  tok.Token.AccessToken,
			"refresh_token": tok.Token.RefreshToken,
			"token_type":    tok.Token.TokenType,
			"expiry":        tok.Token.Expiry,
			"email":         tok.UserEmail(),
		})
	})

	// 3. System Config Endpoint
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		existingKey := endpoints.CurrentGeminiAPIKey()
		hasKey := existingKey != ""
		maskedKey := ""
		if hasKey && len(existingKey) > 8 {
			maskedKey = existingKey[:4] + "..." + existingKey[len(existingKey)-4:]
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"default_proxy":           defaultProxy,
			"has_api_key":             hasKey,
			"masked_api_key":          maskedKey,
			"generative_language_url": endpoints.Current().GenerativeLanguage,
			"cloudcode_url":           endpoints.Current().CloudCode,
			"workspace":               workspace,
		})
	})

	// 4. Unified Text Chat API
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request json: "+err.Error())
			return
		}

		if req.ProxyURL == "" {
			req.ProxyURL = defaultProxy
		}

		if err := HandleChat(r.Context(), w, &req); err != nil {
			log.Printf("[livechat-web] chat error: %v", err)
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		}
	})

	// 5. WebSocket Live Chat Bridge
	mux.HandleFunc("/api/live-ws", HandleLiveWebSocket)

	// 6. Session Persistence API (Parity with TUI disk storage)
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		ws := strings.TrimSpace(r.URL.Query().Get("workspace"))
		if ws == "" {
			ws = workspace
		}

		switch r.Method {
		case http.MethodGet:
			id := strings.TrimSpace(r.URL.Query().Get("id"))
			if id != "" {
				sess, err := voice.FindLiveSession(id, ws)
				if err != nil {
					writeJSONError(w, http.StatusNotFound, "session not found: "+err.Error())
					return
				}
				writeJSON(w, http.StatusOK, sess)
				return
			}

			// List sessions
			summaries, err := voice.ListLiveSessions(ws)
			if err != nil || len(summaries) == 0 {
				summaries, _ = voice.ListLiveSessions()
			}
			if summaries == nil {
				summaries = []voice.LiveSessionSummary{}
			}
			writeJSON(w, http.StatusOK, summaries)

		case http.MethodPost:
			var data voice.LiveSessionData
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid session json: "+err.Error())
				return
			}
			if data.ID == "" {
				data.ID = voice.GenerateLiveSessionID()
			}
			if data.Workspace == "" {
				data.Workspace = ws
			}
			data.UpdatedAt = time.Now()
			if data.CreatedAt.IsZero() {
				data.CreatedAt = time.Now()
			}
			if err := voice.SaveLiveSession(&data); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "failed to save session: "+err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"status": "saved",
				"id":     data.ID,
			})

		case http.MethodDelete:
			id := strings.TrimSpace(r.URL.Query().Get("id"))
			if id == "" {
				var req struct {
					ID string `json:"id"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				id = strings.TrimSpace(req.ID)
			}
			if id == "" {
				writeJSONError(w, http.StatusBadRequest, "missing session id to delete")
				return
			}
			if err := voice.DeleteLiveSession(id); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "failed to delete session: "+err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"status": "deleted",
				"id":     id,
			})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// 7. Web Search API (Parity with TUI /search command)
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			var body struct {
				Query string `json:"query"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			query = strings.TrimSpace(body.Query)
		}

		if query == "" {
			writeJSONError(w, http.StatusBadRequest, "missing search query")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()

		result, err := performWebSearch(ctx, query, "")
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"query": query,
				"error": err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"query":  query,
			"result": result,
		})
	})

	// Bind HTTP Listener
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("[livechat-web] Port %d unavailable (%v), selecting dynamic port...", port, err)
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("failed to bind server: %w", err)
		}
	}
	defer ln.Close()

	actualPort := ln.Addr().(*net.TCPAddr).Port
	localURL := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

	log.Printf("==================================================================")
	log.Printf(" 🎙️  Pigasus LiveChat Embedded Web UI Server")
	log.Printf(" Local Web URL:   %s", localURL)
	log.Printf(" Default Proxy:   %s", defaultProxy)
	log.Printf(" Workspace:       %s", workspace)
	log.Printf(" Asset Source:    %s", sourceDesc)
	log.Printf(" Controls:        Ctrl+C to stop")
	log.Printf("==================================================================")

	if !noBrowser && os.Getenv("OPENAGY_NO_BROWSER") == "" {
		go openBrowser(localURL)
	}

	srv := &http.Server{
		Handler:      mux,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	serverErrCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	select {
	case err := <-serverErrCh:
		return err
	case <-stop:
		log.Printf("\n[livechat-web] Shutting down Web UI server gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

func enableCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-goog-api-key, X-Goog-Auth-Scope")
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": message,
	})
}

func openBrowser(url string) {
	time.Sleep(350 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
