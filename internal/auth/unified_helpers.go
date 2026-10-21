// unified_helpers.go — small internal helpers used by unified_config.go
// (kept separate so antigravity_token.go stays untouched).
package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// urlEncode url-escapes a string for form encoding.
func urlEncode(s string) string { return url.QueryEscape(s) }

// httpNewRequest builds a new POST with body string.
func httpNewRequest(ctx context.Context, method, urlStr, body string) (*http.Request, error) {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	return http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
}

// httpDefaultClient returns a 30s-timeout http.Client.
func httpDefaultClient() *http.Client { return &http.Client{} }

// readAll reads all bytes from r.
func readAll(r io.Reader) []byte { b, _ := io.ReadAll(r); return b }

// jsonUnmarshal parses JSON.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
