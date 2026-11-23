package rcfrontend

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/justwasm/pigasus/internal/core/confhome"
)

const (
	embedAPIPath = "/__agy/api/embed"

	embedSandboxCSP = "default-src 'none'; script-src 'unsafe-inline' https://www.gstatic.com/antigravity/web/dev/tailwindcss.min.js; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; frame-ancestors 'self'"
)

var (
	errEmbedForbidden  = errors.New("embed path forbidden")
	errEmbedBadRequest = errors.New("invalid embed path")

	agentEmbedTagRe = regexp.MustCompile(`(?is)<agent-embed\b([^>]*?)(?:/>|>(?:[\s\S]*?</agent-embed>)?)`)
	embedSrcAttrRe  = regexp.MustCompile(`(?is)(?:^|\s)src\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>` + "`" + `]+))`)
	headOpenTagRe   = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)
	headCloseTagRe  = regexp.MustCompile(`(?i)</head>`)
)

// AgentEmbed represents an inline <agent-embed> tag extracted from markdown.
type AgentEmbed struct {
	Src string `json:"src"`
}

// ExtractAgentEmbeds parses <agent-embed src="..."></agent-embed> tags from markdown,
// supporting file:/// URIs and relative artifact paths.
func ExtractAgentEmbeds(markdown string) []AgentEmbed {
	matches := agentEmbedTagRe.FindAllStringSubmatch(markdown, -1)
	if len(matches) == 0 {
		return nil
	}
	var embeds []AgentEmbed
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		attrMatch := embedSrcAttrRe.FindStringSubmatch(m[1])
		if len(attrMatch) == 0 {
			continue
		}
		rawSrc := attrMatch[1]
		if rawSrc == "" {
			rawSrc = attrMatch[2]
		}
		if rawSrc == "" {
			rawSrc = attrMatch[3]
		}
		src := strings.TrimSpace(html.UnescapeString(rawSrc))
		if src == "" {
			continue
		}
		embeds = append(embeds, AgentEmbed{Src: src})
	}
	return embeds
}

// ResolveEmbedPath resolves a file:// URI or relative artifact path, evaluates symlinks,
// and strictly enforces that the resolved .html/.htm file resides inside one of allowedRoots.
func ResolveEmbedPath(src string, allowedRoots []string) (string, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return "", fmt.Errorf("%w: empty src", errEmbedBadRequest)
	}
	if strings.ContainsRune(src, '\x00') {
		return "", fmt.Errorf("%w: null byte in src", errEmbedForbidden)
	}

	rawPath := src
	if hasURIScheme(src) {
		u, err := url.Parse(src)
		if err != nil {
			return "", fmt.Errorf("%w: malformed URI: %v", errEmbedBadRequest, err)
		}
		if !strings.EqualFold(u.Scheme, "file") {
			return "", fmt.Errorf("%w: unsupported scheme %q", errEmbedBadRequest, u.Scheme)
		}
		if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
			return "", fmt.Errorf("%w: remote file host %q not allowed", errEmbedForbidden, u.Host)
		}
		rawPath = u.Path
	} else if idx := strings.IndexAny(rawPath, "?#"); idx >= 0 {
		rawPath = rawPath[:idx]
	}

	if rawPath == "" {
		return "", fmt.Errorf("%w: empty path", errEmbedBadRequest)
	}
	if strings.ContainsRune(rawPath, '\x00') {
		return "", fmt.Errorf("%w: null byte in path", errEmbedForbidden)
	}
	if hasTraversalSegment(src) || hasTraversalSegment(rawPath) {
		return "", fmt.Errorf("%w: path traversal not allowed", errEmbedForbidden)
	}
	if !isHTMLPath(rawPath) {
		return "", fmt.Errorf("%w: only .html and .htm files are allowed", errEmbedBadRequest)
	}

	evalRoots := evaluateAllowedRoots(allowedRoots)
	if len(evalRoots) == 0 {
		return "", fmt.Errorf("%w: no valid allowed roots", errEmbedForbidden)
	}

	nativePath := filepath.FromSlash(rawPath)
	if !filepath.IsAbs(nativePath) {
		var sawSymlinkEscape bool
		for _, root := range evalRoots {
			candidate := filepath.Join(root, nativePath)
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return "", fmt.Errorf("%w: %v", errEmbedForbidden, err)
			}
			if !isStrictlyInsideRoot(root, resolved) {
				sawSymlinkEscape = true
				continue
			}
			info, err := os.Stat(resolved)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return "", err
			}
			if info.IsDir() {
				return "", fmt.Errorf("%w: path is a directory", errEmbedBadRequest)
			}
			if !isHTMLPath(resolved) {
				return "", fmt.Errorf("%w: resolved target is not HTML", errEmbedBadRequest)
			}
			return resolved, nil
		}
		if sawSymlinkEscape {
			return "", fmt.Errorf("%w: symlink escapes allowed root", errEmbedForbidden)
		}
		return "", os.ErrNotExist
	}

	resolved, err := filepath.EvalSymlinks(nativePath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: path is a directory", errEmbedBadRequest)
	}
	if !isHTMLPath(resolved) {
		return "", fmt.Errorf("%w: resolved target is not HTML", errEmbedBadRequest)
	}

	for _, root := range evalRoots {
		if isStrictlyInsideRoot(root, resolved) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: path outside allowed roots", errEmbedForbidden)
}

func hasURIScheme(s string) bool {
	colon := strings.IndexByte(s, ':')
	if colon <= 0 {
		return false
	}
	// Avoid treating Windows drive letters like C:\ as a URI scheme.
	if colon == 1 && len(s) > 2 && (s[2] == '\\' || s[2] == '/') {
		return false
	}
	for i := 0; i < colon; i++ {
		c := s[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isAlpha {
			return false
		}
		if !isAlpha && !isDigit && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func hasTraversalSegment(p string) bool {
	if idx := strings.IndexAny(p, "?#"); idx >= 0 {
		p = p[:idx]
	}
	normalized := strings.ReplaceAll(p, "\\", "/")
	for _, seg := range strings.Split(normalized, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func isHTMLPath(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".html" || ext == ".htm"
}

func evaluateAllowedRoots(roots []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(roots))
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		eval, err := filepath.EvalSymlinks(abs)
		if err != nil {
			continue
		}
		info, err := os.Stat(eval)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, ok := seen[eval]; ok {
			continue
		}
		seen[eval] = struct{}{}
		out = append(out, eval)
	}
	return out
}

func isStrictlyInsideRoot(evalRoot, resolvedPath string) bool {
	rel, err := filepath.Rel(evalRoot, resolvedPath)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

type themeToken struct {
	name  string
	value string
}

var darkThemeTokens = []themeToken{
	{"--background", "#070708"},
	{"--content", "#070708"},
	{"--card", "#131317"},
	{"--sidebar", "#111114"},
	{"--card-border", "rgb(255 255 255 / 0.08)"},
	{"--border", "rgb(255 255 255 / 0.06)"},
	{"--foreground", "#FFFFFF"},
	{"--muted-foreground", "#87888F"},
	{"--placeholder", "#636470"},
	{"--code-foreground", "#d7ba7d"},
	{"--primary", "#2F71E3"},
	{"--primary-foreground", "#FFFFFF"},
	{"--secondary", "rgb(255 255 255 / 0.075)"},
	{"--secondary-foreground", "#BCBDC6"},
	{"--accent", "#2F71E3"},
	{"--link", "#60a5fa"},
	{"--success", "#4ade80"},
	{"--warning", "#fbbf24"},
	{"--error", "#f87171"},
	{"--chart-1", "#60a5fa"},
	{"--chart-2", "#34d399"},
	{"--chart-3", "#a78bfa"},
	{"--chart-4", "#fbbf24"},
	{"--chart-5", "#f87171"},
	{"--chart-6", "#fb923c"},
}

var lightThemeTokens = []themeToken{
	{"--background", "#FFFFFF"},
	{"--content", "#FFFFFF"},
	{"--card", "#FFFFFF"},
	{"--sidebar", "#FAFAFA"},
	{"--card-border", "rgb(10 10 12 / 0.07)"},
	{"--border", "rgb(10 10 12 / 0.06)"},
	{"--foreground", "#0A0A0C"},
	{"--muted-foreground", "#71717A"},
	{"--placeholder", "#8E8E95"},
	{"--code-foreground", "#795e26"},
	{"--primary", "#2F75C3"},
	{"--primary-foreground", "#FFFFFF"},
	{"--secondary", "rgb(10 10 12 / 0.06)"},
	{"--secondary-foreground", "#3F3F46"},
	{"--accent", "#2F75C3"},
	{"--link", "#2563eb"},
	{"--success", "#16a34a"},
	{"--warning", "#d97706"},
	{"--error", "#dc2626"},
	{"--chart-1", "#2563eb"},
	{"--chart-2", "#059669"},
	{"--chart-3", "#7c3aed"},
	{"--chart-4", "#d97706"},
	{"--chart-5", "#dc2626"},
	{"--chart-6", "#ea580c"},
}

// InjectGenerativeUITheme injects the semantic CSS variables documented in
// generative_ui/SKILL.md, flush container defaults, and live themechange listener support.
func InjectGenerativeUITheme(htmlBytes []byte, theme string) []byte {
	mode := "dark"
	tokens := darkThemeTokens
	if strings.EqualFold(strings.TrimSpace(theme), "light") {
		mode = "light"
		tokens = lightThemeTokens
	}

	var cssVars strings.Builder
	for i, tok := range tokens {
		if i > 0 {
			cssVars.WriteByte(' ')
		}
		cssVars.WriteString(tok.name)
		cssVars.WriteString(": ")
		cssVars.WriteString(tok.value)
		cssVars.WriteByte(';')
	}

	injection := fmt.Sprintf(
		`<style data-host-theme>:root { %s } html, body { margin: 0; padding: 0; background: transparent; color: var(--foreground); font-size: 14px; color-scheme: %s; }</style>`+
			`<script>(function(){var mode=%q;var other=mode==="light"?"dark":"light";var cl=document.documentElement.classList;cl.remove(other);cl.add(mode);var t=window.tailwind=window.tailwind||{};if(!t.config){t.config={darkMode:"class"};}window.addEventListener("message",function(event){if(event.source!==window.parent||!event.data||event.data.type!=="THEME_SYNC"||typeof event.data.cssVariables!=="object"||!event.data.cssVariables)return;var vars=event.data.cssVariables;var root=document.documentElement;Object.keys(vars).forEach(function(k){if(/^--[a-zA-Z0-9-]+$/.test(k)&&typeof vars[k]==="string"&&!/[;{}\r\n]|<\/style/i.test(vars[k])){root.style.setProperty(k,vars[k]);}});var nextMode=vars["--theme-type"]==="light"?"light":(vars["--theme-type"]==="dark"?"dark":mode);var nextOther=nextMode==="light"?"dark":"light";root.classList.remove(nextOther);root.classList.add(nextMode);window.dispatchEvent(new CustomEvent("themechange",{detail:{themeMode:nextMode,cssVariables:vars}}));});})();</script>`,
		cssVars.String(), mode, mode,
	)

	if loc := headOpenTagRe.FindIndex(htmlBytes); loc != nil {
		var out bytes.Buffer
		out.Grow(len(htmlBytes) + len(injection) + 2)
		out.Write(htmlBytes[:loc[1]])
		out.WriteByte('\n')
		out.WriteString(injection)
		out.Write(htmlBytes[loc[1]:])
		return out.Bytes()
	}
	if loc := headCloseTagRe.FindIndex(htmlBytes); loc != nil {
		var out bytes.Buffer
		out.Grow(len(htmlBytes) + len(injection) + 2)
		out.Write(htmlBytes[:loc[0]])
		out.WriteString(injection)
		out.WriteByte('\n')
		out.Write(htmlBytes[loc[0]:])
		return out.Bytes()
	}

	var out bytes.Buffer
	out.Grow(len(htmlBytes) + len(injection) + 1)
	out.WriteString(injection)
	out.WriteByte('\n')
	out.Write(htmlBytes)
	return out.Bytes()
}

func (s *Server) embedAllowedRoots() []string {
	var roots []string
	if s.opts.WorkspaceRoot != "" {
		roots = append(roots, s.opts.WorkspaceRoot)
	}
	if antigravityHome := confhome.Antigravity(); antigravityHome != "" {
		roots = append(roots, filepath.Join(antigravityHome, "brain"))
	}
	roots = append(roots, s.opts.EmbedRoots...)
	return roots
}

func (s *Server) serveEmbed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.pageAllowed(r) {
		writeTokenRequired(w, r)
		return
	}

	src := r.URL.Query().Get("src")
	if strings.TrimSpace(src) == "" {
		http.Error(w, "missing src query parameter", http.StatusBadRequest)
		return
	}

	resolved, err := ResolveEmbedPath(src, s.embedAllowedRoots())
	if err != nil {
		switch {
		case errors.Is(err, errEmbedForbidden):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, errEmbedBadRequest):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, os.ErrNotExist):
			http.Error(w, "embed file not found", http.StatusNotFound)
		default:
			http.Error(w, "unable to resolve embed path", http.StatusInternalServerError)
		}
		return
	}

	raw, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "embed file not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to read embed file", http.StatusInternalServerError)
		return
	}

	theme := r.URL.Query().Get("theme")
	themed := InjectGenerativeUITheme(raw, theme)

	w.Header().Set("Content-Security-Policy", embedSandboxCSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(themed)
	}
}
