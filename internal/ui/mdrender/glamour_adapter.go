// glamour_adapter.go — charm.land/glamour/v2 v2.0.1 wrappers for the
// hand-rolled internal/mdrender package.
//
// Why glamour (cycle: AGY 1.4.2 markdown rendering alignment). The
// recovery notes from out/local-macos/strings-goldmark.txt show the
// reference embeds github.com/yuin/goldmark; the 1.4.2 release note
// is explicit:
//
//	"Improved markdown rendering by upgrading glamour to v2.0.1 for
//	 cleaner headings and block padding."
//
// The binary confirms 152 glamour pclntab symbols including
// google3/third_party/golang/charm_land/glamour/v/v2/... runtime
// symbols. The hand-rolled mdrender walker produces visibly
// different output (cycle 175 visual diffs against the rebuilt
// transcript). This file is the four public renderers the clone's
// callers consume, all built on top of glamour:
//
//	ANSI  — chat-transcript rendering (cmd/wagyu/conversation.go).
//	        ANSI bold / italic / link styling; what the cycle-155
//	        hand-rolled pipeline produced, now via glamour.
//	Plain — Transcript passthrough / file-link rendering / --print
//	        path. Same structure as ANSI but no colour, with the
//	        cycle 175 plain-mode "**bold**" → "*bold*" marker
//	        preservation (the legacy walker emits literal "*bold*"
//	        when colour mode is off; glamour would drop the markers
//	        entirely).
//	User — Render-as-typed for render_user_text.go. Preserves soft
//	        line breaks so a user typing two lines sees two lines
//	        (crush's WithPreservedNewLines).
//	Quiet — OAuth device-flow prompt. No colour, no styling —
//	        equivalent to internal/cli/login_prompt.go's plain
//	        mdrender.Render call.
//
// The four variants are memoized per (variant, width) — a glamour
// TermRenderer carries goldmark BlockStack state across Render and
// is NOT safe for concurrent calls; the per-renderer mutex pattern
// is borrowed from charmbracelet/crush's common.MarkdownRenderer.
//
// Fallback: any glamour error falls through to the legacy
// mdrender.Render / RenderANSI so the user always sees something
// rendered instead of an empty transcript (cycle 1.4.2 "fallback to
// raw text with warning banner" spirit). Pin in:
//
//	TestGlamour_ANSIFallbackOnParserError  and
//	TestGlamour_*_ErrorContract tests below.
package mdrender

import (
	"fmt"
	"os"
	"strconv"
	"sync"

	"charm.land/glamour/v2"
	"golang.org/x/term"
)

// Renderer encapsulates a glamour markdown rendering pipeline configured for
// a specific variant and terminal width.
type Renderer struct {
	variant mdVariant
	width   int
}

// NewANSI returns a Renderer configured for the ANSI chat-transcript palette.
func NewANSI(width int) *Renderer {
	return &Renderer{variant: variantANSI, width: width}
}

// NewPlain returns a Renderer configured for plain unstyled markdown.
func NewPlain(width int) *Renderer {
	return &Renderer{variant: variantPlain, width: width}
}

// NewUser returns a Renderer configured for user-typed markdown with soft line breaks preserved.
func NewUser(width int) *Renderer {
	return &Renderer{variant: variantUser, width: width}
}

// NewQuiet returns a Renderer configured for the quiet/OAuth device-flow prompt.
func NewQuiet(width int) *Renderer {
	return &Renderer{variant: variantQuiet, width: width}
}

// Render renders src according to the renderer's configured variant and width.
func (r *Renderer) Render(src string) (string, error) {
	switch r.variant {
	case variantANSI:
		return ANSI(src, r.width)
	case variantPlain:
		return Plain(src, r.width)
	case variantUser:
		return User(src, r.width)
	case variantQuiet:
		return Quiet(src, r.width)
	default:
		return ANSI(src, r.width)
	}
}

// mdVariant is the discriminator for the four flavours of renderer
// this package exposes. Each variant has its own ansi.StyleConfig
// (and, for User, its own WithPreservedNewLines flag), so caches
// are keyed per-variant per-width — same shape as crush's
// common.MarkdownRenderer (lines 28-34 of that file).
type mdVariant int

const (
	variantANSI mdVariant = iota
	variantPlain
	variantUser
	variantQuiet
)

// variantCount is the number of distinct renderer flavours. Caches
// are sized once at init so the lookup table never grows.
const variantCount = 4

// mdCacheMu guards mdCache (the four per-variant renderer maps).
// The same lock that crush holds — concurrent tests can race for a
// newly-minted TermRenderer, but once mint finishes the underlying
// renderer's per-instance mutex serialises Render calls.
var mdCacheMu sync.Mutex

// mdCache is the per-variant, per-width glamour TermRenderer pool.
// Nil entries are filled lazily on first Render for that key, and
// invalidated wholesale by InvalidateCache.
var mdCache [variantCount]map[int]*glamour.TermRenderer

func init() {
	for i := range mdCache {
		mdCache[i] = map[int]*glamour.TermRenderer{}
	}
}

// InvalidateCache drops every cached renderer. Call after a style
// change so subsequent renders mint TermRenderers with the new
// ansi.StyleConfig. Held under mdCacheMu so the reset is atomic
// from concurrent Render calls.
func InvalidateCache() {
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	for i := range mdCache {
		mdCache[i] = map[int]*glamour.TermRenderer{}
	}
}

// getRenderer returns the renderer for (variant, width), minting it
// on first call and caching the result. The returned renderer is
// shared across callers; per-renderer concurrency is serialised by
// the caller via the locker returned by lockRenderer.
func getRenderer(v mdVariant, width int) (*glamour.TermRenderer, error) {
	widthVal := width
	if widthVal <= 0 {
		widthVal = 80 // safety net — glamour default
	}
	mdCacheMu.Lock()
	if r, ok := mdCache[v][widthVal]; ok {
		mdCacheMu.Unlock()
		return r, nil
	}
	mdCacheMu.Unlock()

	cfg := styleConfigFor(v)
	opts := []glamour.TermRendererOption{
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(widthVal),
	}
	if v == variantANSI || v == variantUser {
		opts = append(opts, glamour.WithPreservedNewLines())
	}
	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return nil, fmt.Errorf("mdrender: glamour NewTermRenderer: %w", err)
	}

	mdCacheMu.Lock()
	mdCache[v][widthVal] = r
	mdCacheMu.Unlock()
	return r, nil
}

// rendererLocksMu guards rendererLocks (common.LockMarkdownRenderer).
var (
	rendererLocksMu sync.Mutex
	rendererLocks   = map[*glamour.TermRenderer]*sync.Mutex{}
)

// lockRenderer returns the per-renderer mutex serialising concurrent
// Render calls on a shared TermRenderer. Tests parallelise over the
// four variants; the production TUI Update loop is single-threaded
// but the lock is harmless there.
func lockRenderer(r *glamour.TermRenderer) *sync.Mutex {
	rendererLocksMu.Lock()
	defer rendererLocksMu.Unlock()
	if mu, ok := rendererLocks[r]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	rendererLocks[r] = mu
	return mu
}

// renderWithFallthrough is the implementation shared by ANSI /
// Plain / User / Quiet. It mints / retrieves the renderer, locks
// the per-renderer mutex around the single Render call (so parallel
// tests do not corrupt goldmark's BlockStack), and falls back to
// the legacy walker on any glamour error.
func renderWithFallthrough(v mdVariant, src string, width int) (string, error) {
	if src == "" {
		return "", nil
	}
	r, err := getRenderer(v, width)
	if err != nil {
		return "", err
	}
	mu := lockRenderer(r)
	mu.Lock()
	out, gErr := r.Render(src)
	mu.Unlock()
	if gErr != nil {
		return "", fmt.Errorf("mdrender: glamour: %w", gErr)
	}
	return transformRenderOutput(v, out), nil
}

// currentVariant returns the glamour variant the cycle-232 /
// cycle-243 mid-stream glamour pass renders against.
func currentVariant() string {
	return "ansi"
}

// currentWidth returns the terminal width the cycle-232 /
// cycle-243 mid-stream glamour pass uses, and the width the package
// applies to every Render call that does NOT receive an explicit
// width argument.
func currentWidth() int {
	if pinned := readPinnedWidth(); pinned > 0 {
		return pinned
	}
	if cols := os.Getenv("COLUMNS"); cols != "" {
		if n, err := strconv.Atoi(cols); err == nil && n > 0 {
			return n
		}
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 0
}

// pinnedWidthMu guards pinnedWidth (last value passed to SetCurrentWidth).
var (
	pinnedWidthMu sync.RWMutex
	pinnedWidth   = 0
)

// readPinnedWidth atomically reads the package-pinned width.
func readPinnedWidth() int {
	pinnedWidthMu.RLock()
	defer pinnedWidthMu.RUnlock()
	return pinnedWidth
}

// CurrentWidth returns the package's current view of the terminal
// width, resolved via the same chain currentWidth uses.
func CurrentWidth() int {
	return currentWidth()
}

// SetCurrentWidth pins the package-wide terminal width to width.
// The TUI's tea.WindowSizeMsg handler wires this in so every
// downstream glamour render renders at the actual terminal width.
func SetCurrentWidth(width int) {
	pinnedWidthMu.Lock()
	defer pinnedWidthMu.Unlock()
	if width <= 0 {
		pinnedWidth = 0
	} else {
		pinnedWidth = width
	}
}

// ANSI renders src as Markdown with colour, suitable for the chat
// transcript and the step-detail panel. Width is the terminal
// width; <= 0 falls back to glamour's default (80).
func ANSI(src string, width int) (string, error) {
	out, err := renderWithFallthrough(variantANSI, src, width)
	if err != nil {
		buf := newPooledBuffer()
		if wErr := RenderANSI(src, buf); wErr != nil {
			return src, nil // absolute last resort — pass through.
		}
		return buf.String(), nil
	}
	return out, nil
}

// Plain renders src as Markdown without colour, suitable for piped
// transcripts, file-link rendering, and the --print path. Same
// fallback contract as ANSI.
func Plain(src string, width int) (string, error) {
	out, err := renderWithFallthrough(variantPlain, src, width)
	if err != nil {
		fb, rErr := Render(src)
		if rErr != nil {
			return src, nil
		}
		return fb, nil
	}
	return out, nil
}

// User renders src as Markdown for user-typed echoes
// (cmd/wagyu/render_user_text.go). Same as ANSI but glamour is
// configured with WithPreservedNewLines so a lone "\n" in the
// source renders as a real line break rather than being collapsed
// by CommonMark's paragraph rule.
func User(src string, width int) (string, error) {
	out, err := renderWithFallthrough(variantUser, src, width)
	if err != nil {
		buf := newPooledBuffer()
		if wErr := RenderANSI(src, buf); wErr != nil {
			return src, nil
		}
		return buf.String(), nil
	}
	return out, nil
}

// Quiet renders src as Markdown without colour or styling, suitable
// for the OAuth device-flow prompt (internal/cli/login_prompt.go).
func Quiet(src string, width int) (string, error) {
	out, err := renderWithFallthrough(variantQuiet, src, width)
	if err != nil {
		fb, rErr := Render(src)
		if rErr != nil {
			return src, nil
		}
		return fb, nil
	}
	return out, nil
}
