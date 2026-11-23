package rcfrontend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/confhome"
)

func TestExtractAgentEmbeds(t *testing.T) {
	md := `
Here is a chart:

<agent-embed src="file:///Users/dev/.gemini/antigravity-cli/brain/conv-1/chart.html"></agent-embed>

And a relative widget with single quotes and extra attrs:
<agent-embed class="compact" src='widgets/summary.htm' />

Empty src should be ignored:
<agent-embed src="   "></agent-embed>
<agent-embed></agent-embed>

Another relative path:
<AGENT-EMBED src="nested/panel.html">fallback</AGENT-EMBED>
`
	got := ExtractAgentEmbeds(md)
	want := []AgentEmbed{
		{Src: "file:///Users/dev/.gemini/antigravity-cli/brain/conv-1/chart.html"},
		{Src: "widgets/summary.htm"},
		{Src: "nested/panel.html"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractAgentEmbeds() = %#v, want %#v", got, want)
	}
}

func TestResolveEmbedPath_SecurityAndRoots(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()

	validHTML := filepath.Join(root, "sub", "widget.html")
	if err := os.MkdirAll(filepath.Dir(validHTML), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validHTML, []byte("<html><body>ok</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	nonHTML := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(nonHTML, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}

	outsideHTML := filepath.Join(outsideDir, "secret.html")
	if err := os.WriteFile(outsideHTML, []byte("<html>secret</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	symlinkEscape := filepath.Join(root, "escape.html")
	if err := os.Symlink(outsideHTML, symlinkEscape); err != nil {
		t.Fatal(err)
	}

	evalValid, err := filepath.EvalSymlinks(validHTML)
	if err != nil {
		t.Fatal(err)
	}

	// Valid file:// URI
	got, err := ResolveEmbedPath("file://"+validHTML, []string{root})
	if err != nil {
		t.Fatalf("ResolveEmbedPath(file://) unexpected error: %v", err)
	}
	if got != evalValid {
		t.Fatalf("ResolveEmbedPath(file://) = %q, want %q", got, evalValid)
	}

	// Valid relative path
	gotRel, err := ResolveEmbedPath("sub/widget.html", []string{root})
	if err != nil {
		t.Fatalf("ResolveEmbedPath(relative) unexpected error: %v", err)
	}
	if gotRel != evalValid {
		t.Fatalf("ResolveEmbedPath(relative) = %q, want %q", gotRel, evalValid)
	}

	// Rejections
	cases := []struct {
		name string
		src  string
	}{
		{"traversal relative", "../etc/passwd"},
		{"traversal html", "sub/../../outside.html"},
		{"traversal file uri", "file://" + root + "/../etc/passwd.html"},
		{"outside root", "file://" + outsideHTML},
		{"etc passwd", "file:///etc/passwd"},
		{"symlink escape", "file://" + symlinkEscape},
		{"symlink escape relative", "escape.html"},
		{"non-html file", "file://" + nonHTML},
		{"http scheme", "https://example.com/widget.html"},
		{"javascript scheme", "javascript:alert(1)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if p, err := ResolveEmbedPath(tc.src, []string{root}); err == nil {
				t.Fatalf("expected error for %q, got path %q", tc.src, p)
			}
		})
	}
}

func TestInjectGenerativeUITheme_VariablesAndListener(t *testing.T) {
	requiredVars := []string{
		"--background", "--content", "--card", "--sidebar",
		"--card-border", "--border", "--foreground", "--muted-foreground",
		"--placeholder", "--code-foreground", "--primary", "--primary-foreground",
		"--secondary", "--secondary-foreground", "--accent", "--link",
		"--success", "--warning", "--error",
		"--chart-1", "--chart-2", "--chart-3", "--chart-4", "--chart-5", "--chart-6",
	}

	raw := []byte("<!DOCTYPE html><html><head><title>Widget</title></head><body><div>hi</div></body></html>")

	for _, theme := range []string{"dark", "light"} {
		out := string(InjectGenerativeUITheme(raw, theme))
		for _, v := range requiredVars {
			if !strings.Contains(out, v+":") {
				t.Errorf("theme %q missing CSS variable %s", theme, v)
			}
		}
		if !strings.Contains(out, "margin: 0; padding: 0; background: transparent;") {
			t.Errorf("theme %q missing flush container defaults", theme)
		}
		if !strings.Contains(out, "themechange") {
			t.Errorf("theme %q missing themechange listener support", theme)
		}
		if !strings.Contains(out, `var mode="`+theme+`"`) {
			t.Errorf("theme %q missing mode %q in script", theme, theme)
		}
	}
}

func TestServeEmbed_HTTPAndCSPAndAuth(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()

	widgetPath := filepath.Join(ws, "widget.html")
	if err := os.WriteFile(widgetPath, []byte("<!DOCTYPE html><html><head></head><body><p>Hello</p></body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(outside, "evil.html")
	if err := os.WriteFile(outsidePath, []byte("<html><body>evil</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(ws, "link.html")
	if err := os.Symlink(outsidePath, symlinkPath); err != nil {
		t.Fatal(err)
	}

	// Also verify brain dir is allowed by default.
	brainDir := filepath.Join(confhome.Antigravity(), "brain", "conv-test")
	if err := os.MkdirAll(brainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	brainWidget := filepath.Join(brainDir, "brain.html")
	if err := os.WriteFile(brainWidget, []byte("<html><head></head><body>brain</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(Options{
		WorkspaceRoot: ws,
		CSRFToken:     "secret-token-123",
	})

	// 1. Remote peer without token is rejected (401).
	reqUnauth := httptest.NewRequest(http.MethodGet, "/__agy/api/embed?src=file://"+widgetPath+"&theme=light", nil)
	reqUnauth.RemoteAddr = "203.0.113.10:12345"
	rrUnauth := httptest.NewRecorder()
	srv.ServeHTTP(rrUnauth, reqUnauth)
	if rrUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated remote embed status = %d, want 401", rrUnauth.Code)
	}

	// 2. Remote peer with valid token succeeds with CSP + themed HTML.
	reqOK := httptest.NewRequest(http.MethodGet, "/__agy/api/embed?src=file://"+widgetPath+"&theme=light&token=secret-token-123", nil)
	reqOK.RemoteAddr = "203.0.113.10:12345"
	rrOK := httptest.NewRecorder()
	srv.ServeHTTP(rrOK, reqOK)
	if rrOK.Code != http.StatusOK {
		t.Fatalf("authenticated embed status = %d, want 200 (body=%s)", rrOK.Code, rrOK.Body.String())
	}
	wantCSP := "default-src 'none'; script-src 'unsafe-inline' https://www.gstatic.com/antigravity/web/dev/tailwindcss.min.js; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; frame-ancestors 'self'"
	if gotCSP := rrOK.Header().Get("Content-Security-Policy"); gotCSP != wantCSP {
		t.Errorf("CSP = %q, want %q", gotCSP, wantCSP)
	}
	if ct := rrOK.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	body := rrOK.Body.String()
	if !strings.Contains(body, "--background: #FFFFFF;") || !strings.Contains(body, "themechange") {
		t.Errorf("response missing injected light theme or themechange listener")
	}

	// 3. Brain directory embed succeeds on loopback.
	reqBrain := httptest.NewRequest(http.MethodGet, "/__agy/api/embed?src=file://"+brainWidget+"&theme=dark", nil)
	reqBrain.RemoteAddr = "127.0.0.1:54321"
	rrBrain := httptest.NewRecorder()
	srv.ServeHTTP(rrBrain, reqBrain)
	if rrBrain.Code != http.StatusOK {
		t.Fatalf("brain embed status = %d, want 200", rrBrain.Code)
	}

	// 4. Traversal and symlink escape rejected with 403.
	for _, badSrc := range []string{
		"../etc/passwd",
		"file://" + outsidePath,
		"file://" + symlinkPath,
	} {
		reqBad := httptest.NewRequest(http.MethodGet, "/__agy/api/embed?src="+badSrc, nil)
		reqBad.RemoteAddr = "127.0.0.1:54321"
		rrBad := httptest.NewRecorder()
		srv.ServeHTTP(rrBad, reqBad)
		if rrBad.Code != http.StatusForbidden {
			t.Errorf("bad src %q returned status %d, want 403", badSrc, rrBad.Code)
		}
	}
}

func TestSidecarSandbox_ManifestAndAssetsAndTraversal(t *testing.T) {
	sidecarDir := t.TempDir()
	outsideDir := t.TempDir()

	sidecarJSONPath := filepath.Join(sidecarDir, "sidecar.json")
	if err := os.WriteFile(sidecarJSONPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sidecarDir, "index.html"), []byte("<html><body>sidecar ui</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sidecarDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sidecarDir, "assets", "app.js"), []byte("console.log('ok');"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sidecarDir, "assets", "style.css"), []byte("body{color:red}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sidecarDir, "assets", "data.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sidecarDir, "assets", "icon.svg"), []byte(`<svg></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sidecarDir, "assets", "logo.png"), []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sidecarDir, "secret.sh"), []byte("#!/bin/sh\necho secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	outsideJS := filepath.Join(outsideDir, "leak.js")
	if err := os.WriteFile(outsideJS, []byte("leak()"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideJS, filepath.Join(sidecarDir, "escape.js")); err != nil {
		t.Fatal(err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer(Options{
		CSRFToken: "sidecar-secret",
		SidecarLookup: func(sidecarID string) (map[string]any, string, error) {
			if sidecarID != "demo-app" {
				return nil, "", os.ErrNotExist
			}
			return map[string]any{
				"display_name": "Demo App",
				"has_web_ui":   true,
				"agentPermissions": map[string]any{
					"workspaceUris": []any{"file://${HOME}/worktrees"},
					"accessGrants":  []any{"read_file(${HOME}/.config/demo)"},
				},
			}, sidecarJSONPath, nil
		},
	})

	// 1. Unauthenticated remote request rejected.
	reqUnauth := httptest.NewRequest(http.MethodGet, "/__agy/api/sidecar/demo-app/manifest", nil)
	reqUnauth.RemoteAddr = "203.0.113.10:12345"
	rrUnauth := httptest.NewRecorder()
	srv.ServeHTTP(rrUnauth, reqUnauth)
	if rrUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated manifest status = %d, want 401", rrUnauth.Code)
	}

	// 2. Manifest returns resolved permissions and sandbox="allow-scripts" iframe URL.
	reqManifest := httptest.NewRequest(http.MethodGet, "/__agy/api/sidecar/demo-app/manifest?token=sidecar-secret", nil)
	reqManifest.RemoteAddr = "203.0.113.10:12345"
	rrManifest := httptest.NewRecorder()
	srv.ServeHTTP(rrManifest, reqManifest)
	if rrManifest.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, want 200 (body=%s)", rrManifest.Code, rrManifest.Body.String())
	}
	var manifest SidecarManifest
	if err := json.Unmarshal(rrManifest.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if manifest.SidecarID != "demo-app" {
		t.Errorf("manifest.SidecarID = %q, want demo-app", manifest.SidecarID)
	}
	if manifest.Sandbox != "allow-scripts" {
		t.Errorf("manifest.Sandbox = %q, want allow-scripts", manifest.Sandbox)
	}
	if manifest.IframeURL != "/__agy/api/sidecar/demo-app/index.html" {
		t.Errorf("manifest.IframeURL = %q, want /__agy/api/sidecar/demo-app/index.html", manifest.IframeURL)
	}
	perms, ok := manifest.Permissions.(map[string]any)
	if !ok {
		t.Fatalf("manifest.Permissions type = %T, want map[string]any", manifest.Permissions)
	}
	uris := perms["workspaceUris"].([]any)
	if uris[0] != "file://"+home+"/worktrees" {
		t.Errorf("resolved workspaceUris[0] = %v, want file://%s/worktrees", uris[0], home)
	}

	// 3. Static assets served with expected Content-Type.
	assetCases := []struct {
		path     string
		wantType string
		wantBody string
	}{
		{"/__agy/api/sidecar/demo-app/index.html", "text/html", "sidecar ui"},
		{"/__agy/api/sidecar/demo-app/assets/app.js", "application/javascript", "console.log('ok')"},
		{"/__agy/api/sidecar/demo-app/assets/style.css", "text/css", "body{color:red}"},
		{"/__agy/api/sidecar/demo-app/assets/data.json", "application/json", `{"ok":true}`},
		{"/__agy/api/sidecar/demo-app/assets/icon.svg", "image/svg+xml", "<svg></svg>"},
		{"/__agy/api/sidecar/demo-app/assets/logo.png", "image/png", ""},
	}
	for _, tc := range assetCases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.RemoteAddr = "127.0.0.1:54321"
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", tc.path, rr.Code)
			continue
		}
		if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.wantType) {
			t.Errorf("GET %s Content-Type = %q, want prefix %q", tc.path, ct, tc.wantType)
		}
		if tc.wantBody != "" && !strings.Contains(rr.Body.String(), tc.wantBody) {
			t.Errorf("GET %s body = %q, want substring %q", tc.path, rr.Body.String(), tc.wantBody)
		}
	}

	// 4. Traversal, symlink escape, and disallowed extensions rejected with 403.
	for _, badPath := range []string{
		"/__agy/api/sidecar/demo-app/../../etc/passwd",
		"/__agy/api/sidecar/demo-app/assets/../../outside.js",
		"/__agy/api/sidecar/demo-app/escape.js",
		"/__agy/api/sidecar/demo-app/secret.sh",
	} {
		req := httptest.NewRequest(http.MethodGet, badPath, nil)
		req.RemoteAddr = "127.0.0.1:54321"
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("GET %s status = %d, want 403", badPath, rr.Code)
		}
	}
}
