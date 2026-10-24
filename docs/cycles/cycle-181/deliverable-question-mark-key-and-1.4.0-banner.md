# Cycle 181 — `?` opens help overlay + 1.4.0 banner + 34-entry shortcuts

## Why

The `~/.local/bin/agy` parity target moved from 1.3.6 to **1.4.0** (the
installed binary self-updated). Three visible TUI gaps remained:

1. The clone's banner and `/help → general` panel still read
   "Antigravity CLI 1.3.6" / "Version 1.3.6". REF reads `1.4.0`.
2. The clone's status-row hint `? for shortcuts` was a **lie** — pressing
   `?` on an idle prompt just typed the literal `?` into the input.
   REF v1.4.0 opens the help overlay.
3. While closing gap #2 we discovered the SHORTCUTS tab was missing the
   "alt+m / Send with opposite Queued Messages setting" row — REF ships
   34 entries, the clone had 33.

Closing these is the natural next step after cycle 179 (sticky user-
prompt header): they are *visible the moment the user opens the binary*,
no new feature work required.

## What changes

### `internal/cli/version.go`

- `ReferenceVersion`: `"1.3.6"` → `"1.4.0"`. The constant is the single
  source of truth — every user-visible version string reads it.
- Package-doc comment updated to mention the 1.4.0 bump.

### `cmd/wagyu/tui_run.go`

- New keybinding in `Update()`'s `tea.KeyMsg` arm: `?` on an **empty**
  input opens the help overlay on the **SHORTCUTS tab** (`helpTab = 2`).
  Conditions: `key.Code == '?'`, `key.Mod == 0`, `m.input.Value() == ""`,
  `!m.helpMode`, `!m.picker.Visible()`, `!m.resumeMode`. Otherwise the
  rune falls through to the textarea and is typed as a literal `?`.
- Cleared `m.helpPage`, `m.helpCursor`, `m.input`, `m.historyIdx`,
  `m.historyEdit` — same teardown the `/help` slash handler already
  performs.
- Comment notes that `/help` keeps `helpTab = 0` (general) while `?`
  defaults to `helpTab = 2` (shortcuts) — the two entry points have
  different defaults in REF.

### `cmd/wagyu/helpShortcuts` (in `tui_run.go`)

- Added the missing entry `"  alt+m  Send with opposite Queued Messages setting"`
  between `alt+j` and `alt+v`. The list grew from 33 to **34 entries**.
- Cycle-137 comments updated to reflect the new total (33 → 34).

### `cmd/wagyu/tui_run_test.go`

- `Test_HelpGeneralPanelStyling`: the asserted "Version 1.3.6" styling
  block is now `"Version 1.4.0"`.
- New `Test_QuestionMarkOpensHelpOverlay` test covers:
  1. `?` on empty input opens overlay on `helpTab=2` (SHORTCUTS).
  2. Esc dismisses (re-uses the cycle-83 esc path).
  3. Typing "hello" then `?` appends the literal `?` and does NOT open
     the overlay (cursor stays in input, not in help mode).
  4. After clearing the input, `?` re-opens the overlay on SHORTCUTS.
  5. `/help` slash command still lands on `helpTab=0` (GENERAL).

### `internal/cli/testdata/changelog_reference.txt` + constants

Captured from `~/.local/bin/agy changelog` (the reference binary's
release-notes block). The previously-embedded blob was 150,756 bytes
from v1.3.6; the v1.4.0 binary prints **153,988 bytes / 915 lines**
starting at `1.3.6:` (the binary ships one release *behind* — there
is no `1.4.0:` section). Constants in `changelog_data_test.go` updated:

| Constant | Before | After |
|---|---|---|
| `referenceChangelogByteLen` | 150,756 | 153,988 |
| `referenceChangelogLines`   | 900     | 915     |
| `referenceChangelogSections`| 69      | 70 (1.3.6→1.0.0 still) |
| `referenceChangelogBullets` | 763     | 776     |
| `referenceChangelogSHA256`  | `f60d668e…` | `423ba8bd…` |

`TestChangelog_Shape` updated to assert "starts with 1.3.6:" and "no
1.4.0: section" — the v1.3.6 assertion was the old parity baseline,
now superseded.

All 5 changelog tests pass, including the byte-for-byte check against
the live `~/.local/bin/agy changelog`.

## Verification

```
$ go test ./cmd/... ./internal/...
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.487s
ok  	github.com/chadgpt/openagy/internal/cli	... (all tests)
```

Side-by-side REF vs OUR check at 220×30:

```
$ python3 /tmp/agycmp/full_diff.py
REF: 25 lines, OUR: 25 lines
=== PIXEL-PERFECT MATCH ===
```

The help overlay body rendered after pressing `?` (idle prompt,
shortcuts tab, page 1) is byte-identical between REF and OUR —
including the `Keyboard Shortcuts` heading, the `/keybindings to-
customize` sub-label, all 18 visible binding rows (`> /` through
`ctrl+y  Yank (paste from kill ring)`), the `[1-18 of 34 items]`
indicator, and the `Keyboard: ↑/↓ Navigate  ←/→ Switch View  esc Close`
footer hint.

## Hard constraints / 存疑 / limitations

- **Pagination model divergence (cycle 137)**: REF v1.4.0 uses
  `Right` / `Left` on the COMMANDS and SHORTCUTS tabs to **cycle tabs**
  (commands → shortcuts → general → commands), not to advance pages.
  Our clone still uses `Right` / `Left` to **cycle pages** within the
  current tab (cycle 137). Page-level jumps in REF use `Ctrl+End` /
  `Ctrl+Home`; ours uses `Right` / `Left`. The page-1 layout matches;
  this divergence surfaces only when the user tries to advance pages
  or wrap tabs. **Tracked as a separate cycle** (next).
- 【推测】REF may also have changed the `?` keybinding in 1.4.0 from
  1.3.6; the 1.3.6 capture was on a build that does not match the
  current binary. The cycle-181 test pins the **v1.4.0** behaviour
  (empty-input `?` opens SHORTCUTS; with-text `?` is a literal rune),
  so a future version bump will need to re-verify both halves.
- 【推测】The new `alt+m  Send with opposite Queued Messages setting`
  row may have a slightly different description in REF — the live
  capture was matched by scrolling, not by a side-by-side string
  diff. If REF later prints e.g. "alt+m  Toggle Queued Messages" the
  helper-text won't match until re-captured.