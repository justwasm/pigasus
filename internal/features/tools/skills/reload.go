// reload.go — the `/skills reload` verb: the loaded catalogue and the
// re-scan from disk.
//
// The reference lists the skills it has loaded and answers `/skills
// reload` with a re-read of them.
//
// 【strings】 agy 1.4.x: the binary's string table carries the literal
// "Reload skills from disk" next to "/skills reload". The wording that
// follows that line is *not* recovered — the clone keeps the listing the
// bare command prints (【推测】).
//
// The reload is best-effort: a root that cannot be read keeps the entries
// the previous scan found for it, so a read error never drops a loaded
// skill. The reload never panics — an unreadable root is an empty root,
// not a crash.
package skills

import (
	"sort"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/internal/core/confhome"
)

// ReloadMessage is the reference's own line for the `/skills reload` verb.
//
// 【strings】 agy 1.4.x: the string table carries the literal
// "Reload skills from disk" next to "/skills reload".
const ReloadMessage = "Reload skills from disk"

// IsReload reports whether a `/skills` argument asks for the reload.
//
// The verb is matched case-insensitively and after trimming, like the
// reference's other subcommand verbs (the /statusline verbs are matched
// the same way).
//
// The verb lives here, with the reload it selects: the slash-command
// vocabulary owns the *commands*, and it has no second notion of a
// command's arguments.
func IsReload(args string) bool {
	return strings.EqualFold(strings.TrimSpace(args), "reload")
}

// loadedRoot is one root's entries, as the last scan read them.
type loadedRoot struct {
	// path is the root's directory — the unit's identity.
	path string
	// entries are the skills the last scan found under it.
	entries []jsonSkillInfo
}

// loader holds the entries the last scan read, per root, in discovery
// order.
//
// The state is process-wide on purpose: the reload is a command, and the
// catalogue it keeps is the one the next reload falls back to. The bare
// listing never reads it — the listing always scans the disk, so a skill
// installed while the session runs shows up at once.
type loader struct {
	mu    sync.Mutex
	roots []loadedRoot
}

// setRoot replaces a root's entries, or appends the root when it is new.
func (l *loader) setRoot(path string, entries []jsonSkillInfo) {
	for i := range l.roots {
		if l.roots[i].path == path {
			l.roots[i].entries = entries
			return
		}
	}
	l.roots = append(l.roots, loadedRoot{path: path, entries: entries})
}

// skillsLoader is the loader the `/skills reload` verb drives.
var skillsLoader = &loader{}

// Reload re-scans the skill roots from disk and returns the catalogue that
// is now loaded.
//
// The reload is best-effort: a root that cannot be read keeps the entries
// the previous scan found for it, so a read error never drops a loaded
// skill. A root that is gone from disk (an uninstalled plugin) is dropped.
func Reload() []SkillInfo {
	skillsLoader.mu.Lock()
	defer skillsLoader.mu.Unlock()

	var found []jsonSkillInfo
	covered := map[string]bool{}
	for _, unit := range scanUnits(confhome.Antigravity()) {
		entries, ok := unit.read()
		if !ok {
			// The root could not be read: keep what was loaded from it.
			for _, r := range skillsLoader.roots {
				if unit.covers(r.path) {
					covered[r.path] = true
					found = append(found, r.entries...)
				}
			}
			continue
		}
		covered[unit.path] = true
		skillsLoader.setRoot(unit.path, entries)
		found = append(found, entries...)
	}
	// A root no unit covers any more (an uninstalled plugin) is dropped.
	kept := make([]loadedRoot, 0, len(skillsLoader.roots))
	for _, r := range skillsLoader.roots {
		if covered[r.path] {
			kept = append(kept, r)
		}
	}
	skillsLoader.roots = kept

	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	return dedupeByHash(found)
}
