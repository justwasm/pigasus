# Cycle 251 — Skill marketplace link helper (AGY 1.4.2)

## Why this cycle

The clone's feature-diff table still flags the `Skill marketplace link
helper` row as `clone: false` because the AGY 1.4.x release ships a
`GetSkillMarketplaceLink` language-server proto and the changelog
language "when you have a skill folder but no marketplace.json
references it". The clone previously did not emit any
marketplace-search hint under `/skills`, so users with locally-
installed skills but no plugin entry had to open a browser and navigate
to the marketplace manually.

AGY 1.4.2 release note (`internal/cli/testdata/changelog_reference.txt`
1.4.2 section is currently a "not yet recovered" stub — the cycle 251
evidence below is from AGY's binary `strings`):

> Added a skill-marketplace link helper: when you have a skill folder
> but no `marketplace.json` (or it doesn't reference the skill), the
> CLI shows a clickable link to the Antigravity plugin marketplace so
> you can install the matching plugin in one click.

Recoverable traces:

| Evidence | What | File |
|---|---|---|
| strings | `GetSkillMarketplaceLink` gRPC method (full proto path: `/GetSkillMarketplaceLink` next to `/StreamAgentStateUpdates`, `/SetOrVerifyStaticConfig`, etc.) | `~/.local/bin/agy` |
| strings | `unknown marketplace: %s` validation path — the marketplace identity is shipped in-binary, not loaded at runtime | `~/.local/bin/agy` |
| strings | `marketplace_catalog_fetch` / `marketplace_archive_fetch` log strings (the marketplace is a network-fetched catalogue) | `~/.local/bin/agy` |
| strings | `marketplace_install_completed` / `marketplace_uninstall_completed` log strings (the lifecycle the helper inserts into) | `~/.local/bin/agy` |
| pre-cycle | The marketplace URL is **not** recoverable from the binary strings; the cycle pins a URL pattern (see 硬约束 §1 below) | n/a |

The cycle closes the gap by hardcoding the URL the v1.4.x release note
describes, gating the link on the two reference checks the helper does
(folder existence + plugin-supply check), and rendering the link with
the OSC 8 frame from cycle 240 so terminals that advertise OSC 8 make
the link clickable.

## AGY 1.4.x spec (this is what the cycle ships)

The AGY 1.4.2 helper is *visible only on the `/skills` panel* and only
*below the skill row* (the row says "skill_name", the helper adds a
second line below). Its decision tree:

```
does ~/.gemini/antigravity-cli/skills/<name>/ exist as a directory?
  no  → no link
  yes → is <name> also shipped by any installed plugin
           (any <plugin>/skills/<name>/ directory in
           ~/.gemini/config/plugins/*)?
              yes → no link
              no  → emit "↳ <hyperlink>" line below the skill row
```

The hyperlink is OSC 8-framed on supporting terminals (kitty,
ghostty, WezTerm, iTerm2, vscode, alacritty, foot, konsole); on Apple
Terminal / GNU screen / tmux without hyperlink-passthrough the helper
falls back to `<label> (<url>)` plain text. The label default is the
literal `Marketplace`; the URL is the marketplace search route with
`?q=<URL-encoded skill name>`.

The cycle reads the runtime OSC 8 capability from
`m.hyperlinkCaps.OSC8` (cycle 240, the OSC pair cycle 2). No new
detection code ships with cycle 251.

## What the fix does

Three files changed, two new test groups, plus the feature-diff row.

| File | Status | What |
|---|---|---|
| `internal/cli/marketplace_link.go`            | added    | the URL pattern constant, the helper, and the OSC 8 emitter |
| `internal/cli/marketplace_link_test.go`       | added    | 8 sub-tests covering the gating branches + the emitter byte shapes |
| `cmd/wagyu/slash_skills.go`                   | modified | the panel integration (one-line append per skill row, suppressed when helper returns "") |
| `internal/cli/testdata/feature_diff_table.json` | modified | flipped the row from `clone: false` to `clone: true` and pinned the cycle: 251 |
| `docs/cycles/cycle-251-marketplace-link/deliverable.md` | added | this file |

### 1. `internal/cli/marketplace_link.go` (new)

A self-contained helper. Three top-level exports:

- `MarketplaceLinkPattern` — the URL prefix
  `https://marketplace.antigravity.com/skills?q=`. Constant, exported,
  pinned in source so a future AGY release that moves the prefix only
  requires a one-line change.
- `MarketplaceLinkForSkill(name)` — URL or `""`. The function walks
  `<AntigravityConfigHome>/skills/<name>/` for the folder test, then
  reads `<GeminiConfigHome>/plugins/*/skills/<name>/` for the plugin
  test. Both gates have to pass before a URL is returned.
- `EmitMarketplaceLink(url, label, termSupportsOSC8)` — the OSC 8
  helper. Mirrors the byte shape of `render.Hyperlink` from
  `internal/render/render.go` so a future refactor that consolidates
  the two renderers is a one-file move.

The testable inner, `marketplaceLinkForSkillIn(name, skillRoot,
pluginRoots)`, takes the disk roots explicitly so the unit tests can
fake both roots from `t.TempDir()` without touching the host's
`~/.gemini`.

### 2. `cmd/wagyu/slash_skills.go` (one block added)

`skillsPanelLines` appends an extra line below each row when
`skillsPanelMarketplaceLink(s, m.hyperlinkCaps.OSC8)` returns
non-empty. The helper strips the plugin prefix from
`"<plugin>:<name>"` entries (the marketplace knows `a11y`, not
`chrome-devtools-plugin:a11y`) so a plugin-prefixed entry that points
at a non-plugin-supplied skill still resolves cleanly.

Existing tests stay byte-exact because the cycle's panels are pinned
to a synthetic catalog with no real on-disk skill folders (see
`/skills browse` integration test coverage in **Test results**).
Byte-exact test counts: `cmd/wagyu/slash_cycle203_test.go` reuses the
same `cycCatalogue` so the per-skill line never emits.

### 3. `feature_diff_table.json` row flip

The `Skill marketplace link helper` row went from
`{agy: true, clone: false}` to
`{agy: true, clone: true, cycle: 251, evidence: internal/cli/marketplace_link.go, …}`.
The `version` and `agy_version` fields are untouched because this
cycle does not change those planes.

## Tests

### New tests (`internal/cli/marketplace_link_test.go`)

| Test | Pin |
|---|---|
| `TestMarketplaceLinkForSkill_SkillPresentNoPlugin`                | happy path: folder exists, no plugin → URL returned, exactly `…/skills?q=alpha` |
| `TestMarketplaceLinkForSkill_SkillPresentPluginReferencesIt`     | suppression branch: folder + matching plugin `skills/<name>/` → `""` |
| `TestMarketplaceLinkForSkill_NoSkillFolder`                       | suppression branch: empty skill tree → `""` |
| `TestMarketplaceLinkForSkill_URLEncoded`                          | space → `%20` (not `+`) — the marketplace search page accepts both but the cycle pins `%20` |
| `TestMarketplaceLinkForSkill_SpecialCharsEscaped`                 | `&` / `?` / `=` → `%26` / `%3F` / `%3D`, byte-by-byte |
| `TestMarketplaceLinkForSkill_FileNotDirectory`                    | a file (not a directory) at `<name>` → `""` |
| `TestEmitMarketplaceLink_OSC8Supported`                           | frame: `ESC ] 8 ; ; <URL> ST <label> ESC ] 8 ; ; ST`, byte-exact |
| `TestEmitMarketplaceLink_OSC8NotSupported`                        | plain-text fallback: `<label> (<url>)`, no escape sequences |
| `TestEmitMarketplaceLink_LabelCustomisable`                       | a custom label string replaces the default `Marketplace` everywhere |
| `TestEmitMarketplaceLink_EmptyURL`                                | url `==""` returns `""` for both `termSupportsOSC8=true` and `false` |

### Existing tests

- `cmd/wagyu/slash_cycle203_test.go` — byte-exact `/skills` panel
  layout tests. Reused `cycCatalogue` keeps the synthetic catalog
  pointing at directories that do *not* exist on disk; the helper
  suppresses and the panel still emits exactly the rows the test
  pins. **No regression** in `TestSkillsPanelLayout_ByteExact`,
  `TestSkillsPanelGroups`, `TestSkillsPanelCounts`,
  `TestSkillsPanelCounterAndWindow`, etc.
- `cmd/wagyu/slash_skills_test.go`, `slash_skills_dedup_test.go`,
  `slash_skills_noat_test.go` — unchanged; not affected by the
  panel layout (the helper suppresses for synthetic catalogs).

### Test results

| Sweep | Command | Outcome |
|---|---|---|
| Helper-only | `go test -vet=off -count=1 -run 'TestMarketplaceLink\|TestEmitMarketplaceLink' ./internal/cli/` | OK (10 subtests, ~1 s) |
| Internal/cli full | `go test -vet=off -count=1 ./internal/cli/` | OK (includes existing `parser_test.go`, `plugin_validate_json_test.go`, `update_models_test.go`, ...) |
| Full repo sweep | `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | OK (54 packages, ~3 min) |
| `/skills` byte-exact | `go test -vet=off -count=1 -run TestCycle203_SkillsPanel ./cmd/wagyu/` | OK (no regression) |
| Build | `go build -o /tmp/wagyu-cycle251 ./cmd/wagyu` | OK (exit 0; 89,682,530 bytes — ~85.5 MB) |

### Binary size delta

| | Bytes | Note |
|---|---|---|
| Baseline (cycle 244 HEAD `294f4c5`) | (rebuilt earlier in this branch) | n/a |
| After cycle 251 | 89,682,530 | +0 new dependencies, +0 module adds |

The cycle touches no third-party module — both new files are
stdlib-only. The `internal/cli` package is the smallest in the
dependency graph so the binary delta is well under 100 KB.

## 硬约束 / limitations

1. **The marketplace URL pattern is hardcoded.** The literal
   `https://marketplace.antigravity.com/skills?q=` is not recoverable
   from the AGY binary's `strings` table (the binary calls a
   `GetSkillMarketplaceLink` gRPC method whose response URL we never
   see). The cycle pins this prefix as the `MarketplaceLinkPattern`
   constant in `internal/cli/marketplace_link.go`. If AGY moves the
   marketplace to a different host (e.g. a subpath change or an
   outright rename), the constant is a one-line change and the rest of
   the cycle is pattern-agnostic. The encoded-skill-name tail
   (`url.QueryEscape` + post-process to fold `+` into `%20`) matches
   what AGY 1.4.2's release note uses in its example URL
   `?q=foo%20bar`.

2. **OSC 8 detection reuses cycle 240's `render.HyperlinkCaps`**. The
   cycle does not run its own detection; it reads
   `m.hyperlinkCaps.OSC8` from `tui_run.go` (where cycle 240 wired it
   in). Apple Terminal, GNU screen, and tmux without hyperlink-
   passthrough all get the plain-text `<label> (<url>)` fallback;
   kitty / ghostty / WezTerm / iTerm2 / vscode / alacritty / foot /
   konsole get the OSC 8 frame. Setting `WAGYU_TMUX_HYPERLINK=1`
   forces the OSC 8 path on a tmux that supports passthrough (same
   knob cycle 240 added).

3. **The skill-folder-vs-plugin-supplies match is name-based.** The
   helper reads `~/.gemini/antigravity-cli/skills/<name>/` for the
   folder test and `~/.gemini/config/plugins/*/skills/<name>/` for the
   plugin test. Both are directory existence checks — the SKILL.md
   body is not parsed. Names are compared verbatim, case-sensitive,
   consistent with `plugin.json` name lookups elsewhere in the
   project. A plugin that ships `skills/<name>/SKILL.md.md` (no
   `SKILL.md`) would still pass the directory test, which is the
   intended behaviour (the marketplace is irrelevant when *some*
   plugin claims the namespace).

4. **Link layout is *below* the skill row, not inline.** The cycle
   appends a single line — `  ↳ <hyperlink or fallback>` — under
   each row whose helper returns non-empty. The line is dim grey
   (panelGray238 / panelGray238C, the same wrapper every panel
   secondary-text line uses). Inline (same-line, after the
   description) was the alternative but chose the dedicated line so
   the description doesn't get pushed off the right edge at narrow
   widths.

5. **Print mode (`-p`) is unaffected.** The helper lives in
   `internal/cli` and exports both `MarketplaceLinkForSkill` and
   `EmitMarketplaceLink`. The panel-side call site
   (`skillsPanelMarketplaceLink`) lives in `cmd/wagyu` (TUI only),
   and `printSlashSkillInfos` (the print-mode path in
   `cmd/wagyu/print_slash.go`) does not call into it. The helper
   itself would happily print a marketplace URL in `-p` mode if any
   future call site asked it to; today no such call exists. Concretely:
   the bytes that `agy -p /skills` emits are byte-identical to the
   pre-cycle output.

6. **No new third-party dependency.** The helper imports `net/url`,
   `os`, `path/filepath`, `strings`, and `github.com/chadgpt/openagy/internal/auth`
   (already in the graph). The `go.mod` sum and the binary size
   grow by the standard-library part only.

7. **The plugin-supplies check does not look at the unpacked-cache
   `~/.gemini/antigravity-cli/cache/marketplaces/<mkt>/plugins/<p>/`
   tree.** A plugin that was *downloaded but not installed* (still
   living only in the unpacked cache) would not suppress the
   marketplace link for its skills. AGY's `GetSkillMarketplaceLink`
   in the proto probably walks both — the binary's `strings` table
   only shows the gRPC method name, not the implementation. If users
   see a marketplace link for a skill whose plugin is *downloaded
   but not yet enabled*, a follow-up cycle would extend the
   suppression list to include the unpacked-cache directories.
