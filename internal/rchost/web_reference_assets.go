// Package rchost — web_reference_assets.go: the web UI handler for the
// `--reference-backend` mode.
//
// In that mode the *page* comes from the reference as well: the
// reference's own index.html (with its own `window.__APP_CONFIG__` — its
// csrf token, its app version) and the reference's own bundle. Serving
// the clone's copy of the page next to the reference's answers would
// mix two products.
//
// Two paths stay with the clone:
//
//   - `/__agy/*` — the endpoints the bundle itself calls: the sign-in
//     status, the debug log, the account switcher's API. They belong
//     to the process the browser talks to, not to the reference.
//   - everything else — the reference's assets.
package rchost

import (
	"net/http"
	"strings"
)

// cloneEndpointPrefix is the path prefix the clone's own endpoints live
// under.
const cloneEndpointPrefix = "/__agy/"

// referenceAssetsHandler serves the reference's own frontend assets and
// keeps the clone's own endpoints.
type referenceAssetsHandler struct {
	clone  http.Handler
	assets http.Handler
}

// newReferenceAssetsHandler composes the two handlers.
func newReferenceAssetsHandler(clone, assets http.Handler) http.Handler {
	return &referenceAssetsHandler{clone: clone, assets: assets}
}

// ServeHTTP routes the request: the clone's own endpoints to the clone,
// everything else to the reference.
func (h *referenceAssetsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "no handler", http.StatusInternalServerError)
		return
	}
	if strings.HasPrefix(r.URL.Path, cloneEndpointPrefix) {
		if h.clone != nil {
			h.clone.ServeHTTP(w, r)
			return
		}
		http.Error(w, "no handler", http.StatusInternalServerError)
		return
	}
	if h.assets == nil {
		http.Error(w, "no reference assets", http.StatusBadGateway)
		return
	}
	h.assets.ServeHTTP(w, r)
}
