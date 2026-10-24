# Cycle skill-discovery-already-discovered — skills panel dedup by content hash

## Gap

AGY 1.2.7:

> Fixed the same skill being listed under multiple names in the
> skills panel when discovered through both a plugin's skills
> directory and a project-level `.claude/skills/` folder; skill
> listings now show the skill's declared name only, deduplicated by
> hash.

The clone's `printSlashSkillInfos` (the single enumerator behind
`/skills`, the `/skills` JSON payload, the `/<skill>` router, and
`agy -p /skills`) walked three roots — the builtin bundle,
`~/.gemini/skills`, and `~/.gemini/config/plugins/<plugin>/skills` —
and accumulated every result without deduplicating. A SKILL.md
present under both a plugin's `skills/` directory and a project-level
`.claude/skills/` folder would appear twice: once as
`<plugin>:<declared-name>` and once as `<declared-name>`. The gap
fixes that.

## Scope

Close the gap on the clone, mirroring AGY 1.2.7's release note:

1. **`cmd/wagyu/slash_skills_dedup.go`** (new) — `skillContentHash`
   (SHA-256 hex of a SKILL.md's bytes) and `dedupeSkillInfosByHash`
   (single-pass fold by content hash, first-encountered wins,
   unreadable files kept without dedup so an I/O error never
   silently deletes a skill).
2. **`cmd/wagyu/print_slash.go`** — `printSlashSkillInfos` calls
   `dedupeSkillInfosByHash` after collecting and sorting the
   discoveries, so every surface that reads the catalog picks up
   the fix in lockstep. The file comment records the rule and
   notes that the dedup is what would collapse duplicates if a
   future release adds `.claude/skills/` (or any other second
   root) to the enumerator.
3. **`cmd/wagyu/slash_skills_dedup_test.go`** (new) — eight unit
   tests cover the dedup primitive, the catalog wiring, and the
   user-visible outcome of the gap fix.

## Evidence grade

The dedup rule is 【推测】 — AGY 1.2.7's release note does not name
the hash function. SHA-256 is the standard choice: short enough to
key a map, long enough that collisions in a personal skills tree
(≤10² entries) are not a practical concern. Hashing the raw file
bytes (not the parsed frontmatter) is deliberate: it lets a SKILL.md
with a directory-derived name (no frontmatter `name:`) and a SKILL.md
with `name: foo` but otherwise identical body still hash equal, so
the dedup collapses them and the surviving entry keeps its declared
name. The first-encountered-wins rule for path / plugin / builtin /
description is also 【推测】; the reference does not document a
priority between roots, so we keep the discovery order (builtin →
shared → plugins, alphabetical within plugins).

## Tests

`go test -vet=off -count=1 -run 'TestSkill|TestDedup|TestDiscovery|TestPanel' ./cmd/wagyu/ ./internal/.../` — 1.527 s, all green.

The new suite (slash_skills_dedup_test.go) adds eight tests:

| Test | Pins |
|---|---|
| `TestDedup_SameSkillViaPluginAndProject` | The gap case: same SKILL.md discovered twice (plugin + shared) collapses to one entry. First-encountered wins for path / plugin. |
| `TestDedup_DifferentSkillsBothKept` | Negative case: distinct content hashes don't merge. |
| `TestDedup_HashCollisionHandled` | Two unreadable SKILL.md paths both hash to ""; the dedup must NOT collapse them (otherwise every I/O error becomes a silent data-loss event). |
| `TestDedup_DeclaredNameWinsOverFilename` | The "declared name only" clause of the changelog: the surviving entry's declared name (frontmatter `name:` minus plugin prefix) is what the listing shows. |
| `TestDedup_MultipleSkillHashesDeduped` | Bulk case: 6 entries over 3 SKILL.md files (one 3-way dup, one unique, one 2-way dup) → 3 entries. |
| `TestDedup_FirstEncounteredWinsForMetadata` | Surviving entry keeps the first-encountered's path / plugin / description; the rule does NOT blend fields across discoveries. |
| `TestSkillContentHash_Deterministic` | The hash primitive: identical bytes → identical hashes (64 hex chars = SHA-256); different bytes → different hashes; empty / missing paths → "". |
| `TestPrintSlashSkillInfos_DedupApplied` | Integration: a SKILL.md planted under BOTH a plugin's `skills/` directory and the shared `~/.gemini/skills` root surfaces as a single entry in the catalog the `/skills` panel reads. |

## Constraints honoured

- `go build -o /tmp/wagyu-sda ./cmd/wagyu` — exit 0, ~73 MB binary.
- `gofmt -l cmd/wagyu/slash_skills_dedup.go cmd/wagyu/slash_skills_dedup_test.go cmd/wagyu/print_slash.go` — empty output.
- Pre-existing not-clean files (`picker_view.go`, `tui_v135_test.go`, the 50 files under `internal/`) untouched.
- `internal/convstore/sqlite.go` — not touched.
- One commit on `feature/skill-discovery-already-discovered` (not pushed).
- AGENTS.md convention: `out/...` reports are zh-CN, this repo's docs/cycles are English.

## Limitations / 存疑

- **The changelog text is verbatim the gap prompt.** AGENTS.md records
  that the reference's 1.2.7 section does not include the "deduplicated
  by hash" line (the shipped `internal/cli/testdata/changelog_reference.txt`
  is the v1.4.0 binary's 69-section blob and stops at 1.0.0 — it does
  cover 1.2.7 but the bullet list there has no entry that names skills
  dedup). The gap description supplied for this cycle is therefore a
  recovery from an undocumented reference behaviour, not a direct
  reproduction of the released changelog line. The implementation
  matches the gap wording exactly; if the reference's actual rule
  differs (e.g. SHA-1 instead of SHA-256, or normalization of
  whitespace before hashing) the result is still correct in spirit
  but the byte-level contract may need adjusting.
- **The clone does not actually walk `.claude/skills/`** — the existing
  enumerator only reads the builtin bundle, the shared root, and the
  plugin skills/ trees. The dedup is wired in regardless, so adding a
  `.claude/skills/` root in a future cycle will pick up dedup
  automatically; until then, the user-visible behaviour of `/skills`
  on this machine is unchanged (no duplicate content was present in
  the test fixture). `TestPrintSlashSkillInfos_DedupApplied`
  exercises the dedup by planting the same content under a plugin's
  `skills/` and the shared root, which is the same shape as the
  plugin-vs-`.claude/skills` overlap the changelog describes, just
  routed through two existing roots.
- **`first-encountered-wins` is a tie-breaker guess.** The reference
  might prefer the highest-priority root (builtin > plugin > shared)
  instead of strict discovery order. With a clean test tree (no
  duplicates) this does not affect behaviour; with duplicates, the
  result is one entry per skill either way and only the metadata
  (path / plugin / builtin / description) of the surviving entry
  differs.
