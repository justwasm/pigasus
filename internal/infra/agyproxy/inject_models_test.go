package agyproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestHasUpstreamCatalog pins what may become the "last good" catalogue. A
// body without a non-empty models field is exactly the shape that wiped the
// vendor's model table when upstream returned it after a network reset.
func TestHasUpstreamCatalog(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"object catalogue", `{"models":{"gemini-3.8-flash-high":{}}}`, true},
		{"array catalogue", `{"models":[{"name":"models/gemini"}]}`, true},
		{"empty", ``, false},
		{"blank", "  \n", false},
		{"empty object", `{}`, false},
		{"empty models map", `{"models":{}}`, false},
		{"empty models array", `{"models":[]}`, false},
		{"error object", `{"error":{"code":500}}`, false},
		{"truncated", `{"models":`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasUpstreamCatalog([]byte(tc.raw)); got != tc.want {
				t.Errorf("hasUpstreamCatalog(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// TestFetchAvailableModelsServesLastGoodCatalogue pins the fix for the
// transient "unknown model key MODEL_PLACEHOLDER_M318" failure: once a good
// catalogue has been seen, a broken refresh answers with that catalogue plus
// the custom models instead of a custom-models-only document.
func TestFetchAvailableModelsServesLastGoodCatalogue(t *testing.T) {
	var broken atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if broken.Load() {
			// The observed failure: a 200 that carries no catalogue at all.
			_, _ = io.WriteString(w, `{}`)
			return
		}
		_, _ = io.WriteString(w, upstreamModels)
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL, miniMax())

	resp := post(t, s, "/v1internal:fetchAvailableModels", `{}`)
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Openagy-Upstream-Status"); got != "200" {
		t.Fatalf("first catalogue status = %q, want 200", got)
	}

	broken.Store(true)
	resp2 := post(t, s, "/v1internal:fetchAvailableModels", `{}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp2.StatusCode)
	}
	if got := resp2.Header.Get("X-Openagy-Upstream-Status"); got != "cached" {
		t.Errorf("status header = %q, want cached", got)
	}
	raw, _ := io.ReadAll(resp2.Body)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("cached body: %v", err)
	}
	ms, ok := doc["models"].([]any)
	if !ok {
		t.Fatalf("models = %#v, want the upstream array shape", doc["models"])
	}
	// Two upstream models plus the custom one; a custom-only fallback would
	// have exactly one and would drop M318 for the real Gemini id.
	if len(ms) != 3 {
		t.Fatalf("cached models = %d, want 3: %v", len(ms), ms)
	}
	var names []string
	for _, e := range ms {
		names = append(names, e.(map[string]any)["name"].(string))
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"models/gemini-2.5-pro", "models/gemini-2.5-flash", Placeholder(miniMax())} {
		if !strings.Contains(joined, want) {
			t.Errorf("cached catalogue lost %q: %v", want, names)
		}
	}

	// A later good fetch replaces the cache and is served directly again.
	broken.Store(false)
	resp3 := post(t, s, "/v1internal:fetchAvailableModels", `{}`)
	defer resp3.Body.Close()
	if got := resp3.Header.Get("X-Openagy-Upstream-Status"); got != "200" {
		t.Errorf("recovered status header = %q, want 200", got)
	}
}

// TestFetchAvailableModelsFailureModes pins that every way a refresh can go
// wrong falls back to the last good catalogue, not just a plain connection
// error.
func TestFetchAvailableModelsFailureModes(t *testing.T) {
	cases := []struct {
		name string
		fail func(http.ResponseWriter)
	}{
		{
			name: "empty 200",
			fail: func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{}`) },
		},
		{
			name: "500",
			fail: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":{"code":500}}`)
			},
		},
		{
			name: "truncated body",
			fail: func(w http.ResponseWriter) {
				w.Header().Set("Content-Length", "4096")
				_, _ = io.WriteString(w, `{"models":`)
				panic(http.ErrAbortHandler)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var broken atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if broken.Load() {
					tc.fail(w)
					return
				}
				_, _ = io.WriteString(w, upstreamModels)
			}))
			defer upstream.Close()

			s := newTestServer(t, upstream.URL, miniMax())
			resp := post(t, s, "/v1internal:fetchAvailableModels", `{}`)
			resp.Body.Close()
			if got := resp.Header.Get("X-Openagy-Upstream-Status"); got != "200" {
				t.Fatalf("prime status = %q", got)
			}

			broken.Store(true)
			resp2 := post(t, s, "/v1internal:fetchAvailableModels", `{}`)
			defer resp2.Body.Close()
			if got := resp2.Header.Get("X-Openagy-Upstream-Status"); got != "cached" {
				t.Errorf("status header = %q, want cached", got)
			}
			raw, _ := io.ReadAll(resp2.Body)
			if !strings.Contains(string(raw), "gemini-2.5-pro") {
				t.Errorf("cached body lost the upstream models: %s", raw)
			}
		})
	}
}

// TestFetchAvailableModelsNeverSucceeded pins the cold-start fallback: with no
// catalogue to remember, the custom models are still advertised (the proxy is
// useful while Google is unreachable), and the header says why.
func TestFetchAvailableModelsNeverSucceeded(t *testing.T) {
	s := newTestServer(t, "http://127.0.0.1:1", miniMax())
	resp := post(t, s, "/v1internal:fetchAvailableModels", `{}`)
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Openagy-Upstream-Status"); got != "error" {
		t.Errorf("status header = %q, want error", got)
	}
	raw, _ := io.ReadAll(resp.Body)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if models, ok := doc["models"].(map[string]any); !ok || len(models) != 1 {
		t.Errorf("fallback models = %#v, want the one custom entry", doc["models"])
	}
}
