package rcfrontend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/features/plugins/sidecars"
)

const (
	sidecarAPIPrefix = "/__agy/api/sidecar/"

	sidecarSandboxCSP = "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'self'"
)

var allowedSidecarAssetExts = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "application/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".svg":  "image/svg+xml",
	".png":  "image/png",
}

// SidecarManifest describes a resolved sidecar UI sandbox entry.
type SidecarManifest struct {
	SidecarID   string         `json:"sidecarId"`
	IframeURL   string         `json:"iframeUrl"`
	Sandbox     string         `json:"sandbox"`
	Permissions any            `json:"permissions,omitempty"`
	Config      map[string]any `json:"config"`
}

func (s *Server) lookupSidecar(sidecarID string) (map[string]any, string, error) {
	if s.opts.SidecarLookup != nil {
		return s.opts.SidecarLookup(sidecarID)
	}
	return sidecars.FindSidecarConfig(sidecarID)
}

func (s *Server) serveSidecarSandbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.pageAllowed(r) {
		writeTokenRequired(w, r)
		return
	}

	if r.URL.RawPath != "" && hasTraversalSegment(r.URL.RawPath) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if hasTraversalSegment(r.URL.Path) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, sidecarAPIPrefix)
	if rest == "" || strings.ContainsRune(rest, '\x00') || strings.ContainsRune(rest, '\\') {
		http.Error(w, "invalid sidecar path", http.StatusBadRequest)
		return
	}

	var sidecarID, assetPath string
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		sidecarID = rest[:slash]
		assetPath = rest[slash+1:]
	} else {
		sidecarID = rest
	}

	if !isValidSidecarID(sidecarID) {
		http.Error(w, "invalid sidecar id", http.StatusBadRequest)
		return
	}

	cfg, sidecarJSONPath, err := s.lookupSidecar(sidecarID)
	if err != nil || cfg == nil {
		http.Error(w, "sidecar not found", http.StatusNotFound)
		return
	}

	resolvedCfg := sidecars.ResolveSidecarTemplates(cfg, nil)

	if assetPath == "manifest" {
		s.serveSidecarManifest(w, r, sidecarID, resolvedCfg)
		return
	}

	if assetPath == "" {
		assetPath = "index.html"
	}
	if !isSafeSidecarAssetPath(assetPath) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	ext := strings.ToLower(filepath.Ext(assetPath))
	ctype, ok := allowedSidecarAssetExts[ext]
	if !ok {
		http.Error(w, "unsupported sidecar asset type", http.StatusForbidden)
		return
	}

	sidecarDir := filepath.Dir(sidecarJSONPath)
	evalRoot, err := filepath.EvalSymlinks(sidecarDir)
	if err != nil {
		http.Error(w, "sidecar directory unavailable", http.StatusNotFound)
		return
	}

	candidate := filepath.Join(evalRoot, filepath.FromSlash(assetPath))
	resolvedAsset, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "asset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if !isStrictlyInsideRoot(evalRoot, resolvedAsset) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	resolvedExt := strings.ToLower(filepath.Ext(resolvedAsset))
	if _, ok := allowedSidecarAssetExts[resolvedExt]; !ok {
		http.Error(w, "unsupported sidecar asset type", http.StatusForbidden)
		return
	}

	f, err := os.Open(resolvedAsset)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "asset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to open asset", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "asset not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if ext == ".html" {
		w.Header().Set("Content-Security-Policy", sidecarSandboxCSP)
	}
	http.ServeContent(w, r, filepath.Base(resolvedAsset), info.ModTime(), f)
}

func (s *Server) serveSidecarManifest(w http.ResponseWriter, r *http.Request, sidecarID string, resolvedCfg map[string]any) {
	var perms any
	if p, ok := resolvedCfg["agentPermissions"]; ok {
		perms = p
	} else if p, ok := resolvedCfg["agent_permissions"]; ok {
		perms = p
	}

	manifest := SidecarManifest{
		SidecarID:   sidecarID,
		IframeURL:   fmt.Sprintf("%s%s/index.html", sidecarAPIPrefix, url.PathEscape(sidecarID)),
		Sandbox:     "allow-scripts",
		Permissions: perms,
		Config:      resolvedCfg,
	}

	payload, err := json.Marshal(manifest)
	if err != nil {
		http.Error(w, "failed to encode manifest", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}

func isValidSidecarID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, "/\\\x00") {
		return false
	}
	return true
}

func isSafeSidecarAssetPath(assetPath string) bool {
	if assetPath == "" || strings.HasPrefix(assetPath, "/") || strings.ContainsAny(assetPath, "\\\x00") {
		return false
	}
	for _, seg := range strings.Split(assetPath, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
