package rcfrontend

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/rctunnel/patches"
)

//go:embed assets/*
var rawAssets embed.FS

var (
	assetsOnce sync.Once
	assetsFS   fs.FS
)

// FS returns the sub-filesystem containing all frontend assets.
func FS() fs.FS {
	assetsOnce.Do(func() {
		sub, err := fs.Sub(rawAssets, "assets")
		if err != nil {
			panic(fmt.Sprintf("rcfrontend: fs.Sub: %v", err))
		}
		assetsFS = sub
	})
	return assetsFS
}

// Options configures the web UI frontend handler.
type Options struct {
	EnableTouchPatches bool
	WorkspaceRoot      string
	ProductName        string
	// CSRFToken is the secret the page carries and the transport's
	// gate expects.
	//
	// Cycle rc-auth-bearer-required. Empty (the default) means "no
	// secret": the page is served to every peer and the transport
	// lets every request through — the pre-cycle behaviour. A server
	// that wires a secret hands it to the loopback and to the
	// tunnel, and hands a remote peer back only the secret that peer
	// already presented.
	//
	// The bundled UI reads the secret from
	// `window.__APP_CONFIG__.csrfToken` and sends it on every request
	// under the header `x-codeium-csrf-token`.
	CSRFToken    string
	AppVersion   string
	AccountEmail string
	RPCHandler   http.Handler

	// DisableAccountSwitcher turns off the top-right account widget and
	// the account API behind it.
	//
	// The flag is negative on purpose: the widget is the web UI's
	// mirror of the TUI's /accounts panel, and the panel is always
	// available. A positive flag would have to be set by every front-end
	// that serves the UI, and the one that is easiest to forget is the
	// one that would silently lose the feature.
	DisableAccountSwitcher bool

	// ReferencePage serves the reference's own IDE page.
	//
	// The reference's language server serves a page whose title is
	// "Antigravity" and whose configuration script is the
	// reference's one-line form:
	//
	//	<script>window.__APP_CONFIG__ = {"productName":"antigravity-ide",
	//	 "csrfToken":"…","appVersion":"2.26.0","releaseChannel":"stable",
	//	 "devMode":false};</script>
	//
	// The embedded page is the CLI's own ("Jetski Web"), and the
	// clone's own configuration script is a multi-line one with the
	// account switcher's fields. The standalone language server
	// (internal/lsserver) is the reference's replica and turns this
	// on.
	ReferencePage bool
}

// Server serves the embedded Jetski Web UI.
type Server struct {
	opts Options
	fsys fs.FS

	modTime time.Time

	mu            sync.RWMutex
	patchedHTML   []byte
	patchedMainJS []byte
	mainJSETag    string

	// acct is the account switcher's state: the token the page carries
	// and the sign-ins in flight. It has its own lock, so a registry
	// write never blocks a page load.
	acct accountsState
}

// NewServer constructs a new web UI Server.
//
// The CSRF token is *not* defaulted to a constant: a constant is
// public, and the secret is the one thing on this surface that must
// not be. An empty token means "no secret configured" — the
// pre-cycle behaviour, where every peer is served the page.
func NewServer(opts Options) *Server {
	if opts.ProductName == "" {
		opts.ProductName = "antigravity"
	}
	if opts.AppVersion == "" {
		opts.AppVersion = "1.4.7"
	}
	return &Server{
		opts:    opts,
		fsys:    FS(),
		modTime: time.Now(),
		acct:    newAccountsState(),
	}
}

// Handler returns an http.Handler that serves the embedded Jetski Web UI.
func Handler(opts Options) http.Handler {
	return NewServer(opts)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cleanPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))

	// API endpoints expected by web bundle & touch patches
	if cleanPath == "/__agy/api/signin/status" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		email := s.opts.AccountEmail
		// The shared credential is the truth about who is signed in:
		// the option is what the server was told at start-up, and a
		// switch changes the credential without changing the server.
		if _, live := accounts.Current(); live != "" {
			email = live
		}
		if email == "" {
			email = "user@example.com"
		}
		_, _ = fmt.Fprintf(w, `{"signedIn":true,"needsSignIn":false,"email":%q}`+"\n", email)
		return
	}
	if cleanPath == "/__agy/api/debug/log" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}` + "\n"))
		return
	}
	if cleanPath == "/__agy/signin" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// The account switcher's API. It answers POST as well as GET: the
	// remote-control tunnel turns every proxied request into a POST
	// (internal/rctunnel/httpproxy.go), so a caller that reaches the
	// server through the cloud tunnel must not get a 405 for it.
	if !s.opts.DisableAccountSwitcher && strings.HasPrefix(cleanPath, accountsAPIPath) {
		s.serveAccountsAPI(w, r, cleanPath)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if s.opts.RPCHandler != nil {
			s.opts.RPCHandler.ServeHTTP(w, r)
			return
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Root or index.html
	if cleanPath == "/" || cleanPath == "/index.html" {
		s.serveIndexHTML(w, r)
		return
	}

	// 2. Main JavaScript bundle with touch patches
	if cleanPath == "/main.js" {
		s.serveMainJS(w, r)
		return
	}

	// 3. Static asset from embedded assets
	fsPath := strings.TrimPrefix(cleanPath, "/")
	f, err := s.fsys.Open(fsPath)
	if err == nil {
		defer f.Close()
		stat, err := f.Stat()
		if err == nil && !stat.IsDir() {
			s.serveStaticFile(w, r, fsPath, f, stat)
			return
		}
	}

	// 4. SPA fallback: if client is requesting a page route, serve index.html
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		s.serveIndexHTML(w, r)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) serveIndexHTML(w http.ResponseWriter, r *http.Request) {
	// The page carries the secret, so a peer that may not have it
	// does not get the page at all — cycle rc-auth-bearer-required.
	if !s.pageAllowed(r) {
		writeTokenRequired(w, r)
		return
	}

	cached, err := s.indexHTML()
	if err != nil {
		http.Error(w, "index.html not found", http.StatusNotFound)
		return
	}

	// The secret is per-request, so the body is per-request too: the
	// cached page carries a placeholder and the value is substituted
	// on the way out. The ETag follows the substituted body.
	body := s.applyRequestToken(cached, r)
	etag := etagFor(body)

	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// indexHTML returns the cached page — the raw index.html with the
// config script and the touch patches applied.
//
// The cached bytes carry the csrfToken *placeholder*: the secret is
// substituted per request (see applyRequestToken), because the page
// is served to peers with different rights.
func (s *Server) indexHTML() ([]byte, error) {
	s.mu.RLock()
	htmlData := s.patchedHTML
	s.mu.RUnlock()
	if htmlData != nil {
		return htmlData, nil
	}
	htmlData, err := s.buildIndexHTML()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.patchedHTML = htmlData
	s.mu.Unlock()
	return htmlData, nil
}

// buildIndexHTML builds the page from the embedded index.html.
func (s *Server) buildIndexHTML() ([]byte, error) {
	raw, err := fs.ReadFile(s.fsys, "index.html")
	if err != nil {
		return nil, err
	}
	raw = s.injectConfigScript(raw)
	if !s.opts.EnableTouchPatches {
		return raw, nil
	}
	patched, _ := patches.Apply(patches.HTML, raw, patches.Options{
		MobileUX:      true,
		WorkspaceRoot: s.opts.WorkspaceRoot,
	})
	return patched, nil
}

// injectConfigScript puts the config script into the page's head.
//
// The script carries the csrfToken *placeholder* — the secret is
// substituted per request, because the page is served to peers with
// different rights.
func (s *Server) injectConfigScript(raw []byte) []byte {
	if s.opts.ReferencePage {
		return s.injectReferenceConfigScript(raw)
	}
	// Inject window.__APP_CONFIG__ required by main.js
	configScript := fmt.Sprintf(`<script>
window.__APP_CONFIG__ = {
  productName: %q,
  csrfToken: %q,
  appVersion: %q,
  devMode: false,
  releaseChannel: "stable",
  ideName: "antigravity",
  ideVersion: "2.24.0",
  isRemoteControl: false
};
</script>`, s.opts.ProductName, csrfTokenPlaceholder, s.opts.AppVersion)

	// The account switcher rides along with the config script: it
	// needs the server's per-process token, and the config script
	// is where the page's own settings live.
	if !s.opts.DisableAccountSwitcher {
		configScript += "\n" + accountsWidgetHTML(s.acct.token)
	}

	if bytes.Contains(raw, []byte("<head>")) {
		return bytes.Replace(raw, []byte("<head>"), []byte("<head>\n    "+configScript), 1)
	}
	if bytes.Contains(raw, []byte("</head>")) {
		return bytes.Replace(raw, []byte("</head>"), []byte("    "+configScript+"\n  </head>"), 1)
	}
	return raw
}

// injectReferenceConfigScript puts the reference's own configuration
// script into the page's head and gives the page the reference's own
// title.
//
// The reference's script is one line and carries five fields, in the
// reference's order. The script sits directly under `<head>`, with no
// indentation — the reference's own bytes.
func (s *Server) injectReferenceConfigScript(raw []byte) []byte {
	config := fmt.Sprintf(
		`{"productName":%q,"csrfToken":%q,"appVersion":%q,"releaseChannel":"stable","devMode":false}`,
		s.opts.ProductName, csrfTokenPlaceholder, s.opts.AppVersion)
	script := "<script>window.__APP_CONFIG__ = " + config + ";</script>"
	raw = bytes.Replace(raw, []byte("<title>Jetski Web</title>"), []byte("<title>Antigravity</title>"), 1)
	if bytes.Contains(raw, []byte("<head>")) {
		return bytes.Replace(raw, []byte("<head>"), []byte("<head>\n"+script), 1)
	}
	if bytes.Contains(raw, []byte("</head>")) {
		return bytes.Replace(raw, []byte("</head>"), []byte(script+"\n  </head>"), 1)
	}
	return raw
}

func (s *Server) serveMainJS(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	jsData := s.patchedMainJS
	etag := s.mainJSETag
	s.mu.RUnlock()

	if jsData == nil {
		raw, err := fs.ReadFile(s.fsys, "main.js")
		if err != nil {
			http.Error(w, "main.js not found", http.StatusNotFound)
			return
		}

		if s.opts.EnableTouchPatches {
			patched, _ := patches.Apply(patches.MainJS, raw, patches.Options{
				MobileUX:      true,
				WorkspaceRoot: s.opts.WorkspaceRoot,
			})
			jsData = patched
		} else {
			jsData = raw
		}

		h := sha256.Sum256(jsData)
		etag = `"` + hex.EncodeToString(h[:8]) + `"`

		s.mu.Lock()
		s.patchedMainJS = jsData
		s.mainJSETag = etag
		s.mu.Unlock()
	}

	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(jsData)
	}
}

func (s *Server) serveStaticFile(w http.ResponseWriter, r *http.Request, fsPath string, f fs.File, stat fs.FileInfo) {
	ctype := mime.TypeByExtension(path.Ext(fsPath))
	if ctype == "" {
		switch path.Ext(fsPath) {
		case ".js", ".mjs":
			ctype = "application/javascript; charset=utf-8"
		case ".css":
			ctype = "text/css; charset=utf-8"
		case ".svg":
			ctype = "image/svg+xml"
		case ".png":
			ctype = "image/png"
		case ".mp3":
			ctype = "audio/mpeg"
		case ".json":
			ctype = "application/json"
		default:
			ctype = "application/octet-stream"
		}
	}

	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "public, max-age=86400")

	// If seeker is supported, use http.ServeContent for range requests
	if rseeker, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, path.Base(fsPath), stat.ModTime(), rseeker)
		return
	}

	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, f)
	}
}
