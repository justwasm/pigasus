package main

// slash_skills_dedup.go — cycle skill-discovery-already-discovered:
//
// content-hash deduplication for the skills catalog. AGY 1.2.7 closed a
// gap where the same SKILL.md, discovered through both a plugin's
// skills/ directory and a project-level `.claude/skills/` folder (or any
// other second root), appeared under two different names in /skills —
// once with the plugin prefix ("<plugin>:<name>") and once as a bare
// shared entry. The 1.2.7 fix collapses duplicates so each skill appears
// once, identified by the SHA-256 of its SKILL.md bytes, with its
// declared (frontmatter `name:`) name.
//
// The dedup pass sits at the end of printSlashSkillInfos (print_slash.go),
// the single enumerator the /skills picker, /skills JSON, the /<skill>
// router, and the print-mode `agy -p /skills` command all share — so
// every surface picks up the fix together.
//
// ── Evidence grades ──────────────────────────────────────────────────
//
// The dedup rule itself is 【推测】: AGY 1.2.7's release note says the
// skill "now show[s] the skill's declared name only, deduplicated by
// hash" but does not name the hash function. SHA-256 is the standard
// choice: short enough to track in a map, long enough that collisions
// in a personal skills tree (~10�² entries) are not a practical concern.
// Hashing the raw bytes — rather than the parsed frontmatter — is
// deliberate: it lets two SKILL.md files at different paths be detected
// as the same skill even when one has only a directory-derived name
// (no frontmatter `name:`) and the other has a declared `name:`, because
// the dedup keeps the surviving entry's declared name.
//
// The first-encountered-wins rule for the surviving entry's metadata
// (path, plugin, builtin, description) is 【推测】 too. The reference does
// not document a priority between roots, so we keep the discovery order
// (builtin → shared → plugins, alphabetical within plugins), which
// matches the natural reading: a builtin skill shipped with the binary
// outranks a user override, and a user override outranks a plugin's
// bundled copy.

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// skillContentHash returns the SHA-256 hex digest of the file at path,
// or "" when the file cannot be read. Two SKILL.md files with identical
// bytes (frontmatter + body) hash equal; callers use this to detect
// "the same skill discovered through a second path".
//
// Hashing the raw bytes — not the parsed frontmatter — is what makes
// the rule robust to formatting changes: a SKILL.md with a
// directory-derived name (no `name:` frontmatter) and a SKILL.md with
// `name: foo` but otherwise identical body still hash equal, so the
// dedup collapses them and the surviving entry keeps its declared name.
func skillContentHash(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// dedupeSkillInfosByHash folds skills that share the same SKILL.md
// content into a single entry. First-encountered wins for path, plugin,
// builtin and description (those are the fields the reference exposes
// to downstream consumers); the surviving entry's Name is already the
// declared frontmatter name because the discovery layer in
// printSlashSkillInfos reads frontmatter before falling back to the
// directory name. An unreadable SKILL.md (hash == "") is kept without
// dedup so a transient I/O error does not silently delete a skill.
//
// The result preserves the input order so the caller's sort by display
// name is unaffected — `dedupeSkillInfosByHash(sortByName(skills))` and
// `sortByName(dedupeSkillInfosByHash(skills))` produce the same set,
// only differing in order.
func dedupeSkillInfosByHash(skills []jsonSkillInfo) []jsonSkillInfo {
	seen := make(map[string]int, len(skills))
	out := make([]jsonSkillInfo, 0, len(skills))
	for _, s := range skills {
		h := skillContentHash(s.Path)
		if h == "" {
			// Unreadable: keep without dedup. The hash would otherwise
			// match every other unreadable file (all map to ""), which
			// would collapse genuinely-distinct skills into one.
			out = append(out, s)
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = len(out)
		out = append(out, s)
	}
	return out
}
