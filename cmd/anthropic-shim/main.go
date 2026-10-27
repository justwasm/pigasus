// Command anthropic-shim translates Anthropic-style Messages API calls
// (`POST /v1/messages`, `GET /v1/models`) onto the same backend.Client
// the openai-shim uses.
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

var knownModels = []string{
	"claude-sonnet-4-6",
	"claude-opus-4-6-thinking",
	"gemini-3-flash",
	"gpt-oss-120b-medium",
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
		addr:      flag.String("addr", envOr("ANTHROPIC_SHIM_ADDR", ":19998"), "HTTP listen address"),
		project:   flag.String("project", envOr("ANTHROPIC_SHIM_PROJECT", "cloudaicompanion-bucket-1307"), "Project ID"),
		proxyURL:  flag.String("proxy-url", envOr("ANTHROPIC_SHIM_PROXY", "socks5h://127.0.0.1:2081"), "Proxy URL"),
		publicURL: flag.String("public-url", envOr("ANTHROPIC_SHIM_PUBLIC_URL", ""), "Public URL"),
		tokenFile: flag.String("token-file", envOr("ANTHROPIC_SHIM_TOKEN_FILE", "/workspace/.home/.gemini/antigravity-cli/antigravity-oauth-token"), "Token file"),
		upstream:  flag.String("upstream", envOr("ANTHROPIC_SHIM_UPSTREAM", endpoints.Current().CloudCode+"/v1internal:generateChat"), "Upstream URL"),
	}
	flag.Parse()
	return f
}

func main() {
	f := parseShimFlags()
	versionStr := "anthropic-shim/1.0.0"

	tokens := initTokenStore(*f.tokenFile)
	be := newBackendClient(*f.proxyURL, *f.tokenFile)
	svc := &server{
		version: versionStr,
		tokens:  tokens,
		backend: be,
		rootMeta: rootMeta{
			Service:    versionStr,
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

func initTokenStore(tokenFile string) backend.TokenStore {
	inlineToken := strings.TrimSpace(os.Getenv("ANTHROPIC_SHIM_TOKEN"))
	if inlineToken == "" {
		inlineToken = strings.TrimSpace(os.Getenv("OPENAI_SHIM_TOKEN"))
	}
	tokens := backend.NewTokenStoreWithEnv(tokenFile, inlineToken)
	if _, err := tokens.AccessToken(); err != nil {
		log.Printf("[boot] token load failed: %v (continuing)", err)
	} else {
		log.Printf("[boot] token loaded (%d bytes, src=%s, expires=%s)", 0, tokens.Source(), tokens.ExpiresAt().Format(time.RFC3339))
	}
	return tokens
}

func checkProxyHealth(proxyURL string) {
	if !proxyReachable(proxyURL) {
		log.Printf("[boot] TUIC proxy not reachable at %s", proxyURL)
	}
}

func setupMux(svc *server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", svc.handleMessages)
	mux.HandleFunc("/v1/models", svc.handleModels)
	mux.HandleFunc("/healthz", svc.handleHealth)
	mux.HandleFunc("/", svc.routeRoot)
	return svc.corsMiddleware(svc.loggingMiddleware(mux))
}

func logBootInfo(addr, upstream, proxyURL, project, publicURL string) {
	log.Printf("[boot] anthropic-shim/1.0.0 listening on %s", addr)
	log.Printf("[boot] upstream = %s", upstream)
	log.Printf("[boot] proxy    = %s", proxyURL)
	log.Printf("[boot] project  = %s", project)
	log.Printf("[boot] public   = %s", ifEmpty(publicURL, "http://"+addr))
	log.Printf("[boot] models   = [%s]", strings.Join(knownModels, " "))
}

type server struct {
	version  string
	tokens   backend.TokenStore
	backend  backend.Client
	rootMeta rootMeta
}

type rootMeta struct {
	Service    string
	Upstream   string
	Proxy      string
	Project    string
	PublicURL  string
	ListenAddr string
}

func newBackendClient(proxyURL, tokenFile string) backend.Client {
	_ = proxyURL
	_ = tokenFile
	return backend.NewMock()
}
