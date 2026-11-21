package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/justwasm/pigasus/endpoints"
)

var (
	portFlag     = flag.Int("port", 8090, "HTTP server port to listen on")
	proxyFlag    = flag.String("proxy", "", "Default Pigasus proxy URL (e.g. http://127.0.0.1:8085)")
	noBrowserFlag = flag.Bool("no-browser", false, "Do not automatically open browser on startup")
)

func main() {
	flag.Parse()

	defaultProxy := *proxyFlag
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

	// 1. Static Web Assets
	webDir := resolveWebDir()
	log.Printf("[demo] Serving web UI from: %s", webDir)
	fs := http.FileServer(http.Dir(webDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Disable caching for all web assets in development mode (app.js, style.css, index.html)
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		fs.ServeHTTP(w, r)
	})

	// 2. OAuth API Endpoints
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
			"default_proxy": defaultProxy,
			"has_api_key":   hasKey,
			"masked_api_key": maskedKey,
			"generative_language_url": endpoints.Current().GenerativeLanguage,
			"cloudcode_url": endpoints.Current().CloudCode,
		})
	})

	// 3. Unified Text Chat API
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
			log.Printf("[demo] chat handler error: %v", err)
			// If headers haven't flushed, write json error
			if !headerWritten(w) {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
			}
		}
	})

	// 4. WebSocket Live Chat Bridge
	mux.HandleFunc("/api/live-ws", HandleLiveWebSocket)

	// Start HTTP Server with port collision handling
	addr := fmt.Sprintf("127.0.0.1:%d", *portFlag)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Attempt port 0 (dynamic port)
		log.Printf("[demo] Port %d unavailable (%v), trying dynamic port...", *portFlag, err)
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatalf("[demo] Failed to bind server: %v", err)
		}
	}
	defer ln.Close()

	actualPort := ln.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

	log.Printf("==================================================================")
	log.Printf(" Pigasus Google OAuth & LiveChat Web Demo")
	log.Printf(" Local URL:       %s", url)
	log.Printf(" Default Proxy:   %s", defaultProxy)
	log.Printf(" Controls:        Ctrl+C to stop")
	log.Printf("==================================================================")

	if !*noBrowserFlag && os.Getenv("OPENAGY_NO_BROWSER") == "" {
		go openBrowser(url)
	}

	srv := &http.Server{
		Handler:      mux,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[demo] server error: %v", err)
		}
	}()

	<-stop
	log.Printf("\n[demo] Shutting down demo server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
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

func headerWritten(w http.ResponseWriter) bool {
	// Simple flusher inspection
	if _, ok := w.(http.Flusher); ok {
		// Header likely already flushed if streaming
		return false
	}
	return false
}

func resolveWebDir() string {
	candidates := []string{
		"demo/web",
		"web",
		"../web",
		filepath.Join(os.Getenv("OPENAGY_ROOT"), "demo/web"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(filepath.Join(c, "index.html")); err == nil && !fi.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	// Fallback to demo/web relative to binary or cwd
	return "demo/web"
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
