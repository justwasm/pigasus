// Package refbackend — csrf.go: the secret the reference's RPCs demand.
//
// 【一手】 2026-10-26, the reference's own answers (a standalone
// `language_server` on 127.0.0.1:8802, `-csrf_token=ref-csrf-token`):
//
//	POST /exa.language_server_pb.LanguageServerService/CancelCascadeInvocation
//	  without the secret → 401 {"code":"unauthenticated","message":"unauthenticated: missing CSRF token"}
//	  with the secret    → 500 {"code":"unknown","message":"cascade not found (error ID: …)"}
//
// So the proxy has to present the secret, or every RPC it forwards is
// rejected. The secret is the one the reference's own page carries: the
// browser learns it from the page's `window.__APP_CONFIG__` and
// presents it with every call, and so does the proxy.
//
// The proxy learns it from the same place — the reference's index
// document — so a reference the operator started with its own secret
// works without the operator telling the proxy anything.
package refbackend

import (
	"context"
	"io"
	"net/http"
	"regexp"
)

const (
	// csrfHeader is the reference's own header name. The web bundle
	// sends the secret in it (`e["x-codeium-csrf-token"]=[b]`).
	csrfHeader = "x-codeium-csrf-token"

	// csrfMaxDocument bounds the index document the proxy reads. The
	// document is three kilobytes; the bound is there so a hostile
	// endpoint cannot make the proxy read without end.
	csrfMaxDocument = 1 << 20
)

// csrfTokenPattern finds the secret in the reference's config script.
//
// 【一手】 the reference's own index document:
//
//	<script>window.__APP_CONFIG__ = {"productName":"antigravity","csrfToken":"ref-csrf-token","appVersion":"2.26.0",…};</script>
var csrfTokenPattern = regexp.MustCompile(`"csrfToken"\s*:\s*"([^"]*)"`)

// discoverCSRFToken reads the secret off the reference's own index
// document.
//
// It returns "" when the reference cannot be reached or its document
// carries no secret: a reference that does not want a secret is a
// reference the proxy can talk to without one.
func discoverCSRFToken(ctx context.Context, client *http.Client, base string) string {
	if client == nil || base == "" {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, csrfMaxDocument))
	if err != nil {
		return ""
	}
	match := csrfTokenPattern.FindSubmatch(raw)
	if match == nil {
		return ""
	}
	return string(match[1])
}
