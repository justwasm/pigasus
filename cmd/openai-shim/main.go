// Command openai-shim is an HTTP front-end that translates OpenAI-style
// requests (`POST /v1/chat/completions`, `GET /v1/models`) into a stream of
// chunks from a backend.Client.
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
	// Termux/Android: /etc/resolv.conf points at a dead loopback
	// nameserver, so the default upstream would never resolve.
	"github.com/justwasm/pigasus/endpoints"
	_ "github.com/justwasm/pigasus/internal/dns"
)

var knownModels = []modelInfo{
	{ID: "claude-sonnet-4-6", OwnedBy: "anthropic", Object: "model", Created: 1_700_000_000},
	{ID: "claude-opus-4-6-thinking", OwnedBy: "anthropic", Object: "model", Created: 1_700_000_000},
	{ID: "gemini-3-flash", OwnedBy: "google", Object: "model", Created: 1_700_000_000},
	{ID: "gpt-oss-120b-medium", OwnedBy: "openai", Object: "model", Created: 1_700_000_000},
}

type shimFlags struct {
	addr      *string
	project   *string
	proxyURL  *string
	publicURL *string
	tokenFile *string
	upstream  *string
}

func parseShimFlags() shimFlags {
	f := shimFlags{
		addr:      flag.String("addr", envOr("OPENAI_SHIM_ADDR", ":19999"), "HTTP listen address"),
		project:   flag.String("project", envOr("OPENAI_SHIM_PROJECT", "cloudaicompanion-bucket-1307"), "Project ID"),
		proxyURL:  flag.String("proxy-url", envOr("OPENAI_SHIM_PROXY", "socks5h://127.0.0.1:2081"), "Proxy URL"),
		publicURL: flag.String("public-url", envOr("OPENAI_SHIM_PUBLIC_URL", ""), "Public URL"),
		tokenFile: flag.String("token-file", envOr("OPENAI_SHIM_TOKEN_FILE", "/workspace/.home/.gemini/antigravity-cli/antigravity-oauth-token"), "Token file"),
		upstream:  flag.String("upstream", envOr("OPENAI_SHIM_UPSTREAM", endpoints.Current().CloudCode+"/v1internal:generateChat"), "Upstream URL"),
	}
	flag.Parse()
	return f
}

func main() {
	f := parseShimFlags()
	version := "openai-shim-public/1.0.0"
	userAgent := "openai-shim-public/1.0.0 (sandbox-shim)"
	authMethod := "consumer"

	inlineToken := strings.TrimSpace(os.Getenv("OPENAI_SHIM_TOKEN"))
	tokens := backend.NewTokenStoreWithEnv(*f.tokenFile, inlineToken)
	logTokenState(tokens)

	be := newBackendClient(*f.proxyURL, *f.project, *f.upstream, userAgent, authMethod, tokens)
	svc := &server{
		version:   version,
		userAgent: userAgent,
		tokens:    tokens,
		backend:   be,
		rootMeta: rootMeta{
			Service:    version,
			Upstream:   *f.upstream,
			Proxy:      *f.proxyURL,
			Project:    *f.project,
			PublicURL:  ifEmpty(*f.publicURL, "http://"+*f.addr),
			ListenAddr: *f.addr,
		},
	}

	checkProxyHealth(*f.proxyURL)
	handler := setupMux(svc)
	logBootInfo(*f.addr, *f.upstream, *f.proxyURL, *f.project, *f.publicURL)

	srvHTTP := &http.Server{
		Addr:              *f.addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srvHTTP.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
}

func logTokenState(tokens backend.TokenStore) {
	if tok, err := tokens.AccessToken(); err != nil {
		log.Printf("[boot] token load failed: %v (continuing — /healthz will report token:false)", err)
	} else {
		log.Printf("[boot] token loaded (%d bytes, src=%s, expires=%s)", len(tok), tokens.Source(), tokens.ExpiresAt().Format(time.RFC3339))
	}
}

func checkProxyHealth(proxyURL string) {
	if !proxyReachable(proxyURL) {
		log.Printf("[boot] TUIC proxy not reachable at %s (continue; ss will use direct egress or fail)", proxyURL)
	}
}

func setupMux(svc *server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", svc.handleChatCompletions)
	mux.HandleFunc("/v1/models", svc.handleModels)
	mux.HandleFunc("/healthz", svc.handleHealth)
	mux.HandleFunc("/", svc.routeRoot)
	return svc.corsMiddleware(svc.loggingMiddleware(mux))
}

func logBootInfo(addr, upstream, proxyURL, project, publicURL string) {
	log.Printf("[boot] openai-shim-public/1.0.0 listening on %s", addr)
	log.Printf("[boot] upstream = %s", upstream)
	log.Printf("[boot] proxy    = %s", proxyURL)
	log.Printf("[boot] project  = %s", project)
	log.Printf("[boot] public   = %s", ifEmpty(publicURL, "http://"+addr))
	log.Printf("[boot] models   = %s", joinModelIDs(knownModels))
}

type server struct {
	version   string
	userAgent string
	tokens    backend.TokenStore
	backend   backend.Client
	rootMeta  rootMeta
}

type rootMeta struct {
	Service    string
	Upstream   string
	Proxy      string
	Project    string
	PublicURL  string
	ListenAddr string
}

func newBackendClient(proxyURL, upstream, up string, userAgent, authMethod string, tokens backend.TokenStore) backend.Client {
	_ = proxyURL
	_ = up
	_ = userAgent
	_ = authMethod
	_ = tokens
	return backend.NewMock()
}
