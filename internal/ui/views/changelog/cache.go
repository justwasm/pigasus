// cache.go — the CLI's own cached copy of the published release notes.
//
// The reference fetches the published release notes and keeps them at
// ~/.gemini/antigravity-cli/cache/CHANGELOG.md; when the network is not
// reachable it falls back to that copy, and when there is no copy either,
// to the release notes embedded in the binary.
//
// 【docs】 official-docs/cli/commands/changelog.md:11 — "the CLI fetches the
// latest published release notes and caches them locally at
// ~/.gemini/antigravity-cli/cache/CHANGELOG.md. If you're working offline or
// behind a restricted network, the CLI automatically falls back to your
// cached copy or the release notes embedded in your installed agy binary."
//
// 【strings】 the reference's own strings: "Downloading changelog from
// %s", "No changelog found at %s" and "failed to read cache and no
// fallback available: %w. See online changelog at: %s" — the three
// messages of its fetch-and-fallback ladder. The surviving pruned index of
// that table is out/local-macos-1.4.11/strings-index.txt (the full dump
// is retired); it carries the first two messages verbatim and the head of
// the third ("failed to read cache and no fallback available").
//
// # The fetch is not here
//
// The clone *reads* the cache; it does not fill it. The reference's fetch is
// a separate step with its own trigger (the /changelog command), and the two
// candidate URLs its binary carries —
// "https://raw.githubusercontent.com/google-antigravity/antigravity-cli/
// refs/heads/main/CHANGELOG.md" and
// "https://exafunction.github.io/public/changelog/%d.%d.md" — do not say
// which one it uses, so the copy that lands in the cache is the reference's
// own. The panel's document is rebuilt on every draw, so an implicit fetch
// here would put network I/O in the render path.
//
// # Where the path lives
//
// The path is the CLI's own home plus "cache/CHANGELOG.md", and the home is
// `internal/core/confhome`'s fact (it owns ~/.gemini/antigravity-cli), so
// this package asks it for the directory instead of carrying a second copy
// of the shape.

package changelog

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/core/confhome"
)

// cacheFile is the file name of the cached release notes.
const cacheFile = "CHANGELOG.md"

// cachePath returns the path of the cached release notes.
//
// 【docs】 official-docs/cli/commands/changelog.md:11 — the cache lives at
// ~/.gemini/antigravity-cli/cache/CHANGELOG.md.
func cachePath() string {
	return filepath.Join(confhome.Antigravity(), "cache", cacheFile)
}

// cacheEntry is one cached document and the file state it was read from.
type cacheEntry struct {
	mtime time.Time
	size  int64
	text  string
	ok    bool
}

var (
	cacheMu   sync.Mutex
	cacheMemo = map[string]cacheEntry{}
)

// cachedMarkdown returns the cached release notes and whether the cache
// held a document.
//
// The file is read once per (mtime, size): the panel's document is rebuilt
// on every draw, and the cache changes only when the reference rewrites it.
func cachedMarkdown() (string, bool) {
	path := cachePath()
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if entry, seen := cacheMemo[path]; seen &&
		entry.mtime.Equal(info.ModTime()) && entry.size == info.Size() {
		return entry.text, entry.ok
	}
	entry := cacheEntry{mtime: info.ModTime(), size: info.Size()}
	if data, rerr := os.ReadFile(path); rerr == nil {
		entry.text = string(data)
		// An empty (or blank) cache file is no cache: the ladder falls
		// through to the next rung rather than showing an empty document.
		entry.ok = strings.TrimSpace(entry.text) != ""
	}
	cacheMemo[path] = entry
	return entry.text, entry.ok
}
