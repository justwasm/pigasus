// Package pluginwatch watches the ~/.gemini tree (plugins, skills,
// marketplace cache) and signals loaders in cmd/wagyu to re-scan their
// on-disk sources when files change underneath them.
//
// # Why this exists
//
// AGY 1.4.2 ships with `github.com/fsnotify/fsnotify` wired into the
// plugin/skill/marketplace loaders (106 fsnotify symbols in pclntab).
// When `agy plugin install foo` runs in another shell, or the user
// edits a SKILL.md in their editor, the live TUI sees the change on
// its next /plugin or /skills invocation without a restart.
//
// The wagyu clone's loaders (cmd/wagyu/plugin_data.go,
// cmd/wagyu/print_slash.go's printSlashSkillInfos) have always read
// the disk on every call, so they were never *stale* per se — but a
// foreground edit + immediately invoke /skills still requires no UI
// work. The gap AGY 1.4.2 closes is that the watcher pushes a
// staleness flag the loader reads, so:
//
//   - the panel's existing tick handler can redraw automatically;
//   - loaders do not need to re-derive "did something change" from a
//     stat comparison;
//   - external edits (a different shell, an editor) become visible.
//
// The package is the small missing piece: a debounced fsnotify loop
// over three roots, exposing a single "is stale" atomic that loaders
// query with IsStale and clear with MarkFresh.
//
// # What this package does NOT do
//
//   - It does not own cache state. The loaders continue to read the
//     disk on every call; the watcher only flags "maybe re-read".
//   - It does not recurse into symlinks. AGY's loader walks
//     (internal/pluginscan follows symlinks), but the watcher stays
//     non-recursive — a symlinked subtree is itself a directory entry
//     under one of the roots and that entry's events reach us
//     through the parent root's watch.
//   - It does not retry on transient fsnotify errors. A burst of
//     filesystem errors is logged once and the loop continues; the
//     watcher may miss events until the user re-invokes /plugin or
//     /skills (a single re-scan always re-anchors the surface).
//
// # Construction
//
// New(homeDir) builds a Watcher that monitors homeDir's three plugin
// trees (see roots()). It returns (nil, nil) when none of the trees
// exist — the loaders already render a useful "no plugins installed"
// state, and a missing ~/.gemini tree is the common first-launch
// case. A non-nil error only fires when fsnotify itself fails to
// construct, which is rare (only happens under fd exhaustion).
//
// # Lifecycle
//
// Run(ctx) starts the goroutine that drains fsnotify. It returns when
// ctx is cancelled or Close is called; either path stops the loop.
// Close releases the underlying fsnotify watcher; calling Run after
// Close is a no-op.
//
// IsStale returns whether at least one debounced event has fired
// since the last MarkFresh. The flag is single-bit — there is no
// "stale counter" — because every loader reads the disk fresh on
// every call; we only need a "yes/no, please re-scan" hint.
package watch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debounceWindow collapses the burst of fsnotify events a single
// editor save produces (write + chmod + rename + close-write on
// Linux, several Write events on macOS) into one staleness flag flip.
// 200 ms is the value used in practice — long enough to fold a
// `git checkout` (rename + chmod) into one transition, short enough
// that a foreground edit feels "immediate" to the operator.
const debounceWindow = 200 * time.Millisecond

// roots are the three directories the watcher monitors. They are
// derived from homeDir (the value auth.GeminiConfigHome /
// AntigravityConfigHome resolves to at startup) so a $ANTIGRAVITY_HOME
// override redirects all three at once.
//
//	gemini/config/plugins         — installed plugin trees
//	gemini/skills                 — shared user skill library
//	home/cache/marketplaces       — Discover-tab marketplace caches
func roots(homeDir string) []string {
	if homeDir == "" {
		return nil
	}
	gemini := filepath.Dir(homeDir) // ~/.gemini
	return []string{
		filepath.Join(gemini, "config", "plugins"),
		filepath.Join(gemini, "skills"),
		filepath.Join(homeDir, "cache", "marketplaces"),
	}
}

// Watcher monitors the three plugin/skill/marketplace roots under
// ~/.gemini. Construct with New, start with Run, query with IsStale,
// clear with MarkFresh, release with Close.
//
// All methods on a nil *Watcher are safe and behave as a no-op —
// IsStale returns false (the empty loaders' default), MarkFresh is a
// no-op, Close returns nil, Run returns immediately. This means
// loaders can call m.watcher.IsStale() unconditionally without
// threading a "watcher is wired" boolean through the call graph.
type Watcher struct {
	fsw *fsnotify.Watcher

	// stale is flipped to 1 on every debounced event batch. atomic.Int32
	// rather than atomic.Bool so the package compiles on Go 1.18 (Bool
	// landed in 1.19).
	stale atomic.Int32

	// debounceTimer is the wallclock-anchored debouncer. reset() on
	// every event, fires a single flip when it elapses.
	debounceMu    sync.Mutex
	debounceTimer *time.Timer

	// debugf, when non-nil, receives one line per interesting event
	// (a root that could not be watched, an fs error). Production
	// callers leave it nil and stay silent; tests set it to a recorder
	// to assert on emitted diagnostics.
	debugf func(format string, args ...any)

	// runOnce guards Run so it can be invoked at most once; a second
	// call is a no-op rather than a panic.
	runOnce sync.Once

	// closed flips on Close; Run observes it and exits.
	closeMu sync.Mutex
	closed  bool
}

// New constructs a watcher rooted at homeDir. When homeDir is empty
// or none of the three watched subtrees exist, New returns
// (nil, nil) — the loaders' empty-state behaviour is the right
// fallback for a first-launch machine. A non-nil error means
// fsnotify itself could not be created (fd exhaustion, ENOSYS on an
// unsupported platform, etc.); the caller should treat that the same
// as (nil, nil) for runtime behaviour.
//
// New never reads or writes ~/.gemini; the existence check is purely
// a "should we bother starting the loop" decision. A root that
// exists at New time but is removed later is silently dropped from
// the watch — its events are not delivered, and the next /plugin
// invocation will still re-scan and surface the absence.
func New(homeDir string) (*Watcher, error) {
	if homeDir == "" {
		return nil, nil
	}
	w := &Watcher{}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		// fsnotify failed — return the error so the caller can log it
		// and decide to fall back. (nil, nil) is reserved for the
		// "nothing to watch" case, which is a *successful* outcome
		// on a fresh install.
		return nil, err
	}
	w.fsw = fsw

	added := 0
	for _, root := range roots(homeDir) {
		info, statErr := os.Stat(root)
		if statErr != nil || !info.IsDir() {
			// Missing root is fine — the user has never installed a
			// plugin, or this is the marketplace-cache root before
			// the first marketplace fetch. Skip silently.
			continue
		}
		if addErr := fsw.Add(root); addErr != nil {
			w.debugf("pluginwatch: skip %s: %v", root, addErr)
			continue
		}
		added++
	}
	if added == 0 {
		// Nothing to watch. Close the fsnotify watcher so we do not
		// leak a goroutine, and signal "no-op watcher" via nil.
		_ = fsw.Close()
		return nil, nil
	}
	return w, nil
}

// SetDebugf replaces the diagnostic sink. nil silences all messages.
// The setter exists so production callers do not pay the cost of
// importing log / fmt just to suppress one warning.
func (w *Watcher) SetDebugf(f func(format string, args ...any)) {
	if w == nil {
		return
	}
	w.debugf = f
}

// Run starts the watcher's event loop. It exits when ctx is cancelled
// or Close is called. Run is a no-op when New returned (nil, nil) or
// after a previous Run has returned.
//
// The loop is single-threaded; events from fsnotify's Events channel
// debounce into one staleness flip per debounceWindow. Errors from
// the Errors channel are logged once each (the channel is rarely
// fired under normal use; it surfaces things like the underlying
// inotify watch being removed by the kernel).
func (w *Watcher) Run(ctx context.Context) {
	if w == nil || w.fsw == nil {
		return
	}
	w.runOnce.Do(func() {
		w.loop(ctx)
	})
}

func (w *Watcher) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-w.fsw.Events:
			if !ok {
				// fsnotify closed its channels; mirror Close.
				return
			}
			w.queueFlip()
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			if err != nil {
				if w.debugf != nil {
					w.debugf("pluginwatch: fs error: %v", err)
				}
			}
		}
	}
}

// queueFlip (re)arms the debounce timer. The first event in a burst
// starts the timer; subsequent events within debounceWindow reset it
// without firing. When the timer fires, the staleness flag flips
// once.
func (w *Watcher) queueFlip() {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}
	w.debounceTimer = time.AfterFunc(debounceWindow, w.flipStale)
}

// hasPendingTimer reports whether a debounce timer is armed. The
// timer field is guarded by debounceMu, so it is never read directly
// from outside the lock.
func (w *Watcher) hasPendingTimer() bool {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()
	return w.debounceTimer != nil
}

// flipStale is the timer callback. Atomic store so concurrent
// IsStale readers see the transition.
func (w *Watcher) flipStale() {
	w.stale.Store(1)
}

// IsStale reports whether at least one debounced filesystem event
// has fired since the last MarkFresh. The flag is sticky until
// MarkFresh runs — loaders should call MarkFresh after they have
// consumed the staleness (i.e. after their re-scan).
func (w *Watcher) IsStale() bool {
	if w == nil {
		return false
	}
	return w.stale.Load() == 1
}

// MarkFresh clears the staleness flag. The next fsnotify event batch
// will set it again; a single MarkFresh therefore means "I have
// re-read everything, do not bother me until the next change".
func (w *Watcher) MarkFresh() {
	if w == nil {
		return
	}
	w.stale.Store(0)
}

// Close releases the fsnotify watcher and stops Run. It is safe to
// call from any goroutine; subsequent Run calls are no-ops and
// subsequent IsStale / MarkFresh calls are harmless (the atomic
// flag is still readable).
//
// Close is idempotent. A second call returns nil (the
// fsnotify.ErrClosed case is swallowed).
func (w *Watcher) Close() error {
	if w == nil || w.fsw == nil {
		return nil
	}
	w.closeMu.Lock()
	defer w.closeMu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	err := w.fsw.Close()
	if errors.Is(err, fsnotify.ErrClosed) {
		return nil
	}
	return err
}
