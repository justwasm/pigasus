// Package refbackend — assets.go: the proxy that serves the
// reference's *own* frontend assets.
//
// `pigasus web --reference-backend` answers the Web UI's RPCs with a
// reference `language_server`. The page has to come from the same
// place: the reference's index.html carries its own
// `window.__APP_CONFIG__` (its csrf token, its app version, its
// product name) and its own bundle, and serving the clone's copy of the
// page next to the reference's answers mixes two products.
//
// 【一手】 2026-10-26, the reference's own HTTP answers (a standalone
// `language_server` on 127.0.0.1:8802):
//
//	GET /                      → 200 text/html      3870 bytes
//	GET /index.html            → 200 text/html      3870 bytes
//	GET /main.js               → 200 text/javascript  10461461 bytes
//	GET /jetbox.css            → 200 text/css           95487 bytes
//	GET /compiled_tailwind.css → 200 text/css          249475 bytes
//	GET /prism_bundle.js       → 200 text/javascript   141963 bytes
//	GET /diff_worker.js        → 200 text/javascript     9952 bytes
//	GET /symbols-icons/x.svg   → 200 text/html          3870 bytes
//	GET /unknown               → 200 text/html          3870 bytes
//
// The last two are the reference's SPA fallback: a path it does not
// know answers with its index.html, not with a 404. The proxy
// reproduces that answer verbatim — the page the browser gets is the
// page the reference would have given it.
//
// One thing does not come from the reference: the *transport* the page
// runs on. The reference's bundle defaults to the fetch transport,
// which hangs the page in a browser (internal/refbackend/transport.go
// has the measurement), so the proxy answers the document with a
// redirect to the WebSocket transport. The bytes are still the
// reference's own.
//
// The bundle itself carries an ETag (`Etag: W/"9fa115-bb169a04402c03c6"`
// for /main.js) and answers `If-None-Match` with a 304; the proxy
// passes both through, so a reload does not re-download ten megabytes.
package refbackend

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// assetsTimeout bounds one fetch from the reference. The bundle is ten
// megabytes; a minute is the ceiling before the fetch is a failure.
const assetsTimeout = 60 * time.Second

// Assets serves the reference's frontend assets.
//
// It is an http.Handler: the web server hands it every request that is
// not one of the clone's own endpoints.
type Assets struct {
	base   string
	client *http.Client

	mu    sync.Mutex
	cache map[string]*assetBody
}

// assetBody is one fetched asset.
type assetBody struct {
	body          []byte
	contentType   string
	etag          string
	lastModified  string
	cacheControl  string
	contentLength int64
}

// NewAssets returns the asset proxy for the reference at base. The base
// may be "host:port", "http://host:port" or "https://host:port".
func NewAssets(base string) (*Assets, error) {
	normalized, err := NormalizeBase(base)
	if err != nil {
		return nil, err
	}
	return &Assets{
		base:   normalized,
		client: &http.Client{Timeout: assetsTimeout},
		cache:  map[string]*assetBody{},
	}, nil
}

// ServeHTTP fetches the asset from the reference and answers with it.
//
// Only GET and HEAD are served: an asset is read-only. A request for a
// path the proxy cannot map is a 400 — the reference's own answers are
// forwarded, but the proxy does not invent paths.
func (a *Assets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a == nil {
		http.Error(w, "reference backend: no assets", http.StatusBadGateway)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	target, err := a.assetPath(r)
	if err != nil {
		http.Error(w, "reference backend: "+err.Error(), http.StatusBadRequest)
		return
	}
	key := target
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	body, cached := a.lookup(key)
	if !cached {
		body, err = a.fetch(r.Context(), target, r.URL.RawQuery)
		if err != nil {
			http.Error(w, "reference backend: "+err.Error(), http.StatusBadGateway)
			return
		}
		a.store(key, body)
	}
	// The document must run on the WebSocket transport: on the fetch
	// transport the app's own six long-lived streams take every
	// connection Chrome keeps per origin and the send never leaves the
	// browser (internal/refbackend/transport.go).
	if location := transportRedirect(r, body.contentType); location != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, location, http.StatusFound)
		return
	}
	a.write(w, r, body)
}

// assetPath maps the request onto the reference's path.
//
// The query string rides along: the reference's own assets do not take
// one, but the request may carry a cache buster, and dropping it would
// make the proxy answer a different question than the browser asked.
func (a *Assets) assetPath(r *http.Request) (string, error) {
	clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	if clean == "." || strings.Contains(clean, "..") {
		return "", errors.New("bad asset path")
	}
	return clean, nil
}

// lookup returns the cached body for a path, if there is one.
func (a *Assets) lookup(target string) (*assetBody, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	body, ok := a.cache[target]
	return body, ok
}

// store caches a fetched body.
//
// The HTML document is not cached: it carries the reference's live
// csrf token and app version, and a cached copy would outlive the
// reference that issued it. Everything else is immutable for the life
// of the reference, and the bundle is ten megabytes.
func (a *Assets) store(target string, body *assetBody) {
	if body == nil || isHTML(body.contentType) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cache[target] = body
}

// isHTML reports whether a content type is the reference's HTML
// document (or its SPA fallback).
func isHTML(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/html")
}

// fetch asks the reference for one asset.
func (a *Assets) fetch(ctx context.Context, target, rawQuery string) (*assetBody, error) {
	url := a.base + target
	if rawQuery != "" {
		url += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	return &assetBody{
		body:          raw,
		contentType:   resp.Header.Get("Content-Type"),
		etag:          resp.Header.Get("ETag"),
		lastModified:  resp.Header.Get("Last-Modified"),
		cacheControl:  resp.Header.Get("Cache-Control"),
		contentLength: int64(len(raw)),
	}, nil
}

// CSRFToken returns the secret the reference's own page carries.
//
// A web server that serves the reference's page has to accept the same
// secret: the page's own requests carry it, and a transport that
// expects a different one denies the browser's own calls.
//
// It returns "" when the reference cannot be reached or its document
// carries no secret.
func (a *Assets) CSRFToken() string {
	if a == nil {
		return ""
	}
	return discoverCSRFToken(context.Background(), a.client, a.base)
}

// write answers the request with the fetched body.
func (a *Assets) write(w http.ResponseWriter, r *http.Request, body *assetBody) {
	if body == nil {
		http.Error(w, "reference backend: empty asset", http.StatusBadGateway)
		return
	}
	if body.etag != "" {
		w.Header().Set("ETag", body.etag)
		if etagMatches(r.Header.Get("If-None-Match"), body.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	if body.contentType != "" {
		w.Header().Set("Content-Type", body.contentType)
	}
	if body.cacheControl != "" {
		w.Header().Set("Cache-Control", body.cacheControl)
	}
	if body.lastModified != "" {
		w.Header().Set("Last-Modified", body.lastModified)
	}
	w.Header().Set("Content-Length", strconv.FormatInt(body.contentLength, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.body)
	}
}

// etagMatches compares a client's If-None-Match with the asset's ETag.
//
// The reference's own ETag is weak (`W/"9fa115-bb169a04402c03c6"`), so
// the comparison is on the opaque part: a weak and a strong validator
// for the same bytes are the same answer.
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" || etag == "" {
		return false
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if stripWeak(candidate) == stripWeak(etag) {
			return true
		}
	}
	return false
}

// stripWeak drops the weak-validator prefix and the quotes of an ETag.
func stripWeak(etag string) string {
	etag = strings.TrimSpace(etag)
	etag = strings.TrimPrefix(etag, "W/")
	etag = strings.TrimPrefix(etag, "w/")
	return strings.Trim(etag, `"`)
}
