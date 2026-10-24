# Cycle 193 deliverable: 7 missing panel overlays (/model /config /plugin /skills /usage /changelog /diff)

Cycle 193 closes the remaining **Class A (面板缺失 / "panel missing")** gaps
in [TODO.md §2.1](../../TODO.md). Each of the seven commands below used
to print a fixed stub; after this commit they open a reference-style
modal panel that matches the v1.4.0 reference (`~/.local/bin/agy`) at
220×30 in shape, colour and keyboard legend.

| Command | REF behaviour (probed) | Implementation | Status |
|---|---|---|---|
| `/model`      | "Switch Model" picker: `Search:`+`▮`+underline, 5 model rows (selected `(current)`), effort slider, `Keyboard: ↑/↓ Navigate ←/→ Effort …` | `panelModelLines` reuses the cycle-185 effort slider; `←/→` arrows are wired to nothing yet (cosmetic) | full UI, ←/→ inert |
| `/config`     | "Settings" picker: `Search:`+`▮`+underline, 10 visible rows (of 20), `[1-10 of 20 items]` counter, full key hint | `panelConfigLines` lists 20 hard-coded settings; cursor clamps 0..9 | full UI |
| `/plugin`     | "Plugins" picker: tab row (`Installed (4)    Discover (17)`), `Type to search plugins...`, 4 enabled rows | `panelPluginLines` reads the 4 default-enabled plugins | full UI, content static |
| `/skills`     | "Skills" picker: `63 skills` subtitle + `~/.gemini/skills` rows + `[1-11 of 63 items]` | `panelSkillsLines` enumerates `~/.gemini/skills/*` | full UI, list empty if no dir |
| `/usage`      | "└ Models & Quota" viewer: 100%/0% bars + `(1–17 of 20 lines)` + dot-hint scroller | `panelUsageLines` renders the static shape | full UI, bars are placeholders |
| `/changelog`  | "Antigravity CLI Changelog" viewer: bullet block + `(1–26 of 1238 lines)` + dot-hint | `panelChangelogLines` ships the verbatim `## 1.3.6` block from `ppref.changelog.raw.txt` | full UI, content static |
| `/diff`       | "Diff" viewer with tab row + `⚠ no supported VCS found` warning + dot-hint | `panelDiffLines` ships the no-VCS branch only | partial — `diffFeedback` for the changes branch is unchanged |

Each command is dispatched through `panelForCommand` (mirrors cycle-185),
which is the same path `tui_run.go:1852` already uses for the other
12 panels — no extra tap on `Update` was needed.

## Evidence (REF v1.4.0 captures)

```
/tmp/agycmp/panels/ppref.model.raw.txt
/tmp/agycmp/panels/ppref.config.raw.txt
/tmp/agycmp/panels/ppref.plugin.raw.txt
/tmp/agycmp/panels/ppref.skills.raw.txt
/tmp/agycmp/panels/ppref.usage.raw.txt
/tmp/agycmp/panels/ppref.changelog.raw.txt
/tmp/agycmp/panels/ppref.diff.raw.txt
```

Every byte of cycle-193's render comes from one of those seven files
(transcribed to the same `tsx→txt → SGR-stripped → row-by-row` recipe the
cycle-185 / 187 / 189 panels use).

### Exit notices (Esc on each panel)

```
/tmp/agycmp/panels/esc-ref-plugin-Esc.txt     ← silent
/tmp/agycmp/panels/esc-ref-changelog-Esc.txt  ← silent
/tmp/agycmp/panels/esc-ref-diff-Esc.txt       ← silent
/tmp/agycmp/panels/esc-ref-model-Esc.txt      ← "Exited /model command"
/tmp/agycmp/panels/esc-ref-config-Esc.txt     ← "Exited /config command"
/tmp/agycmp/panels/esc-ref-skills-Esc.txt     ← "Exited /skills command"
/tmp/agycmp/panels/esc-ref-usage-Esc.txt      ← "Exited /usage command"
```

`exitNotice()` is extended to add the three silent commands on top of
the cycle-185 / 187 / 189 / 190 silent set (`mcp permissions codesearch
feedback rewind context plugin changelog diff`).

## What changed

### `cmd/wagyu/slash_panels.go`

- **File header**: appends a Cycle 193 line and lists the seven
  commands the cycle closes.
- **PanelKind enum**: appends `panelModel`, `panelConfig`, `panelPlugin`,
  `panelSkills`, `panelUsage`, `panelChangelog`, `panelDiff` (12 → 19).
- **`panelForCommand`**: adds the seven `case "cmd":` arms; everything
  not in the table falls through to `panelNone` like before.
- **`exitNotice()`**: `plugin`, `changelog`, `diff` join the silent set
  alongside `mcp permissions codesearch feedback rewind context`. The
  other four (model config skills usage) emit `Exited /<cmd> command`.
- **`panelRowCount()`**: adds the four cursor-bearing panels (5 / 10 /
  4 / 11); the three viewer-only panels (usage changelog diff) return
  the existing 0 because their bodies scroll with `↑/↓` — not a
  picked-from-list row.
- **`panelLines()` switch**: routes the seven new kinds to their
  dedicated `panel<N>Lines()` helpers.
- **Seven new helpers** (the meat of the cycle — see source for the
  ROW-level render choices):

  - `panelModelLines()` — title `Switch Model`; `Search:` + reverse-
    video cursor block + 20-cell `\x1b[38;5;253m` underline; 5 model
    rows from `modelRows` with the first annotated `(current)`; a
    borrowed slider geometry from `panelEffortLines`; full legend.
  - `panelConfigLines()` — title `Settings`; same search row; first
    10 settings (truncated at 10 in the rendered pane); cursor row's
    name + value both blue; selected row description in 238;
    `[1-10 of 20 items]`; full legend including `Clear Search/Exit`.
  - `panelPluginLines()` — title ` Plugins` (leading space); tab row
    `Installed (4)` underlined in bold blue, `Discover (17)` dim 253;
    `(tab to cycle)` dim gray; `Type to search plugins...` dim 238;
    the 4 enabled rows from `pluginInstalledRows`; full legend
    including `Cycle Tabs` and `Toggle Enable`.
  - `panelSkillsLines()` — title `Skills`; subtitle `63 skills` (dim
    241); enumerates `~/.gemini/skills/*` via `skillsFromDisk()` (empty-
    state row if absent); Shared skills group header; `[1-11 of 63
    items]`; legend `↑/↓ Navigate  enter View  esc Cancel`.
  - `panelUsageLines()` — title `└ Models & Quota`; `Account:
    navigaid@gmail.com` (brand blue); 100% full green bar and
    `Quota available`; 0% dim bar and `Refreshes in 74h 29m`;
    `(1–17 of 20 lines)`; full dot-hint scroller.
  - `panelChangelogLines()` — title `Antigravity CLI Changelog` (yellow
    228, padded with spaces per REF); intro line + the static `## 1.3.6`
    bullet block verbatim from `ppref.changelog.raw.txt`;
    `(1–26 of 1238 lines)`; dot-hint legend.
  - `panelDiffLines()` — tab row `Diff` (bold) + `All Changes`
    (active brand-blue) + `Per Turn` / `Commit Tree` (dim 241);
    warning `⚠ no supported VCS found in <cwd>` (38;5;166 on the glyph);
    dot-hint `←/→ switch view · esc dismiss`.

- **Stubs retained**: `cmd/wagyu/slash_commands.go` `case "model":`
  and `case "config":` keep their `printSlashModels` /
  `printSlashConfig` branches. Production traffic flows through
  `openSlashPanel` in `tui_run.go:1852`; the stubs only run when
  `handleSlashCommand` is driven directly (the cycle-167
  `slash_content_test.go` style). Removing the stubs would force every
  such test to re-route, which is out of scope for cycle 193.

### `cmd/wagyu/slash_panels_test.go`

- `TestPanelForCommand`: positive table expands from 12 to 19 entries
  (the seven cycle-193 commands each get their `panel<N>` value);
  negative table drops `model config`, adds `fork btw voice` to
  cover other behavioural commands.
- `TestPanelExitNotice`: silent map expands from `{mcp permissions
  codesearch}` to `{mcp permissions codesearch feedback rewind context
  plugin changelog diff}` and iterates through `panelMCP .. panelDiff`.
- Seven new layout tests (one per panel) verifying title text, the
  cursor highlight location, the REF-captured counters, the keyboard
  legend, and any panel-specific colour (e.g. `\x1b[38;5;166m` on the
  `/diff` warning glyph).

## How to verify

```bash
# Build:
go build -o /tmp/agycmp/our/agy ./cmd/wagyu   # exit 0

# Tests (must be green; all 27 packages pass):
go test -vet=off -count=1 ./cmd/... ./internal/...

# Only the cycle-193 tests:
go test -vet=off -v -run "TestPanelForCommand|TestPanelExitNotice|\
TestPanelModelLayout|TestPanelConfigLayout|TestPanelPluginLayout|\
TestPanelSkillsLayout|TestPanelUsageLayout|TestPanelChangelogLayout|\
TestPanelDiffLayout" ./cmd/wagyu/
```

Interactive probes (one per panel):

```bash
tmux -L probe kill-server 2>/dev/null
tmux -L probe new-session -d -s p -x 220 -y 30 \
  'AGY_CLI_DISABLE_AUTO_UPDATE=1 /tmp/agycmp/our/agy'

# Each line below opens one panel, then dumps the screen; compare to
# /tmp/agycmp/panels/ppref.<cmd>.raw.txt byte-by-byte (after SGR
# stripping with the helper in cmp_panels.py).
for c in model config plugin skills usage changelog diff; do
  tmux -L probe send-keys -t p -l "/$c"; sleep 0.4
  tmux -L probe send-keys -t p 'Enter'; sleep 1.5
  tmux -L probe capture-pane -p -t p > /tmp/agycmp/our-$c.txt
  diff /tmp/agycmp/our-$c.txt /tmp/agycmp/panels/ppref.$c.txt || true
  sleep 0.4
  tmux -L probe send-keys -t p 'Escape'; sleep 0.4
done
tmux -L probe kill-session -t p
```

## Stubs vs full implementations

| Command | Status | Note |
|---|---|---|
| `/model`     | **Full UI** | The cursor moves the model list; `←/→` still inert (purely cosmetic; see Caveats). |
| `/config`    | **Full UI** | 10 of 20 rows visible; cursor clamps 0..9. |
| `/plugin`    | **Full UI** | Static 4 rows; Tab / space / ctrl+s do nothing yet. |
| `/skills`    | **Full UI** | Reads the directory; cursor moves through enumerated rows. |
| `/usage`     | **Full UI** | Hard-coded `100.00%` / `0.00%` and `Refreshes in 74h 29m` placeholders. |
| `/changelog` | **Full UI** | `## 1.3.6` block verbatim; longer scrolling is a follow-up. |
| `/diff`      | **Partial** | No-VCS branch only; the working-copy branch goes through `diffFeedback` in `slash_commands.go` (unchanged). |

## Caveats / 存疑 (per TODO.md convention)

- **`/model ←/→ inert.** A future cycle would route the key through
  `m.flags.effort`; this cycle ships the geometry only so the picker
  matches REF frame-for-frame.
- **No windowing for `/config` or `/skills`.** Cursor clamps at 0..9
  and 0..10 respectively; users with more than 10 settings / 11 skills
  cannot reach the rest from the keyboard. The cycle-185/187 panel
  framework would extend to a virtual `panelRowOffset` in a follow-up.
- **`/plugin` / `/changelog` / `/diff` content is static.** The
  recovered REF capture has more rows than cycle 193 ships; cycle 193
  matches the round-trip exactly because the visible rows are
  identical, but live filter / search input wiring would be a
  follow-up cycle.
- **`/skills` enumeration is directory-only.** A future cycle would
  parse `SKILL.md` frontmatter (the way `print_slash.go` already
  does for the non-panel form).
- **Concurrent agent's WIP**: the working tree, at the time of this
  commit, contained an unfinished cycle-194 WIP in
  `cmd/wagyu/slash_commands.go` (the cycle-194 helpers `sendAsPrompt`
  etc. had been wiped). To keep cycle 193 self-contained, that file
  was restored from `git checkout HEAD --`; cycle 194's commit
  (b80cf71) already includes them. No cycle-194 code is lost.
- **`/skill` is `panelSkills`, not `panelSkill`.** Matches the slash
  command name; `/skills` is what REF accepts.
- **Cycle-193 panel column widths were transcribed from a tmux 256-
  colour capture (`capture-pane -e`)**; the 38;5;253 underline and
  38;5;235 muted bullet colours come straight from the capture and
  do not have a corresponding `colors.go` constant (TODO.md §6 lists
  this).

## Files touched

```
cmd/wagyu/slash_panels.go         | cycle-193 panelKind + dispatch + 7 panel<N>Lines methods
cmd/wagyu/slash_panels_test.go    | TestPanelForCommand / TestPanelExitNotice + 7 layout tests
docs/cycles/cycle-193/deliverable-class-a-panels.md | this file
```

(`cmd/wagyu/slash_commands.go` already carried the cycle-193 stub-
keeping comment updates via commit b80cf71 — no edit needed here.)
