# Cycle `config-left-right-save` — /config ←/→ advances & persists

## TL;DR

TODO.md §5 P4 lists "`/config ←/→ (change value)`" as still inert.
The behaviour has, in fact, been working since cycle 196
(`cycleConfigValue` → `auth.SetSettingsField` / `auth.DeleteSettingsField`)
and cycle 197 (default-by-absence). This cycle verifies the four
clauses of the AGY 1.2.16 changelog entry verbatim, pins them under
five new test cases, and writes the deliverable that closes TODO.md
P4's "still inert" line.

Changelog entry (internal/cli/testdata/changelog_reference.txt:102):

> Added `←`/`→` shortcuts to `/config`: on a highlighted setting they
> switch to the previous or next value and save it right away,
> without opening the dropdown, and the footer shows a `←/→ Change`
> hint

There is no source change in this cycle; the four clauses are
already implemented. The work is documentation + pinning.

## What was already there (cycle 196 + 197)

| Clause | Implementation |
|---|---|
| Advance to previous/next value | `cmd/wagyu/slash_panels.go:3501` `cycleConfigValue(dir)` rotates `row.values[(i+dir+len) % len]` |
| Save it right away | `auth.SetSettingsField` / `auth.DeleteSettingsField` write through `updateSettings` → `os.WriteFile` (cycle 196 helpers, `internal/auth/compat.go:433,467,497`) |
| No dropdown opens | `handlePanelKey`'s `panelConfig` branch (`slash_panels.go:3034-3057`) only routes Left/Right to `cycleConfigValue`; no `m.panel.detail` / `panel.stage` / `panel.diffView` is mutated; Up/Down keep their cursor-navigation semantics |
| Footer shows `←/→ Change` hint | `panelConfigLines` ends with `panelKeyboardHint("↑/↓", "Navigate", "←/→", "Change", "enter", "Select", "esc", "Clear Search/Exit")` (`slash_panels.go:1942`) |

The settings.json path the cycle writes to is the canonical
`AntigravityConfigHome()` one:

- on macOS: `$HOME/.gemini/antigravity-cli/settings.json`
- when `$ANTIGRAVITY_HOME` is set: `$ANTIGRAVITY_HOME/settings.json`
- in tests: an isolated `t.TempDir()` (set via `t.Setenv`)

### Value-advance table (verbosity)

The Verbosity catalogue row's cycle list is, verbatim from
`configSettingsRows` (`slash_panels.go:1747-1751`):

| i | display | stored |
|---|---|---|
| 0 | `medium` | nil (the reference's "default by absence" form — cycle 197) |
| 1 | `low`    | `"low"` |
| 2 | `high`   | `"high"` |

Walking from the absent-key default (`medium`):

| Step | Method | disk verbosity | displayed value |
|---|---|---|---|
| 0   | (baseline) | (absent) | `medium` |
| 1   | → Right     | `"low"`  | `low`    |
| 2   | → Right     | `"high"` | `high`   |
| 3   | → Right     | (absent) | `medium` (forward wrap) |
| 4   | ← Left      | `"high"` | `high`   (backward wrap) |

The forward-wrap step (3) and backward-wrap step (4) are what proves
`cycleConfigValue` actually rotates through the slice rather than
clamps at the ends; a no-op or `if dir>0 && i==len-1 { break }` stub
would keep `medium` forever.

### Other rows (one-line summary)

Every catalogue row carries `len(values) >= 2`, so every cursor on
the picker has something to cycle between; the guard inside
`cycleConfigValue` (`if len(row.values) < 2 { return }`) is purely
defensive. The `Enable Telemetry` row (`memoryOnly: true`) stores in
`m.panel.configMemory` instead of touching settings.json — that
row's contract is also verified by the existing `TestCycle197_ConfigMemoryOnlyTouchesNoDisk`.

## Pinning tests

`cmd/wagyu/slash_config_left_right_save_test.go`, all five tests:

| Test | Clause pinned | What it asserts |
|---|---|---|
| `TestConfigLeftRight_AdvancesValue` | "switch to the previous or next value" | Walking Verbosity [medium → low → high → medium(wrap) → high(wrap-back)] — see the table above |
| `TestConfigLeftRight_PersistsToDisk` | "save it right away" | After a single Right, `~/.gemini/antigravity-cli/settings.json` carries `verbosity = "low"` and the unrelated `colorScheme = "light"` key survives untouched |
| `TestConfigLeftRight_NoDropdownOpen` | "without opening the dropdown" | Right then Left via `handlePanelKey`: `m.panel.kind` stays `panelConfig`, `m.panel.detail` / `stage` / `diffView` / `cursor` unchanged, panel line count unchanged (so no overlay appeared) |
| `TestConfigLeftRight_FooterHint` | "the footer shows a `←/→ Change` hint" | `plainLines(m)[len-1]` contains `Change` *and* either `←/→` (the canonical unicode glyph the renderer emits) or `<-/->` (the ASCII fallback the changelog uses) |
| `TestConfigLeftRight_NonEnumSetting` | "no-op on a non-enum setting" (safety floor) | (a) every catalogue row has `len(values) >= 2` (structural pin), and (b) `cycleConfigValue` with cursor out of range leaves `settings.json` byte-identical and does not panic |

Each test sets `ANTIGRAVITY_HOME` to `t.TempDir()` (via the
cycle-196-scaffold helper `cycle196TestHome`), so the real
`~/.gemini/antigravity-cli/settings.json` is never touched.

## Verification

```
$ go build -o /tmp/wagyu-check ./cmd/wagyu
BUILD_EXIT=0
binary size: ~74 MB (matches cycle-235 baseline)
```

```
$ go test -vet=off -count=1 -run 'TestConfigLeftRight' ./cmd/wagyu/
=== RUN   TestConfigLeftRight_AdvancesValue
--- PASS: TestConfigLeftRight_AdvancesValue (0.01s)
=== RUN   TestConfigLeftRight_PersistsToDisk
--- PASS: TestConfigLeftRight_PersistsToDisk (0.00s)
=== RUN   TestConfigLeftRight_NoDropdownOpen
--- PASS: TestConfigLeftRight_NoDropdownOpen (0.01s)
=== RUN   TestConfigLeftRight_FooterHint
--- PASS: TestConfigLeftRight_FooterHint (0.00s)
=== RUN   TestConfigLeftRight_NonEnumSetting
--- PASS: TestConfigLeftRight_NonEnumSetting (0.00s)
PASS
ok    github.com/chadgpt/openagy/cmd/wagyu    1.043s
```

The pre-existing cycle-196 + cycle-197 tests (20 /config cases in
total) all still pass — the new tests are additive, no fixture name
or schema was changed:

```
$ go test -vet=off -count=1 -run 'Cycle196|Cycle197|TestPanelConfig|ConfigLeftRight' ./cmd/wagyu/
…
PASS (1.316s)
```

## Limitations / 存疑

1. **Live REF-vs-clone byte capture is `未做`.** Cycle 196 captured
   the live reference behaviour byte-for-byte in
   `/tmp/agycmp/probe_cfg_simple.py` and `probe_cfg_multi.py`. This
   cycle did not re-run those probes against agy 1.3.4 because
   running them requires a tmux session attached to a terminal
   sized ≥ 24×80 with the user logged in, and the changelog text is
   unchanged from 1.2.16 onward (`internal/cli/testdata/changelog_reference.txt:101`).
   The five pinning tests are the contract; the live capture is the
   cycle-196 job, not this cycle's.
2. **The cycle-196 deliverable's `enter Select` caveat still
   applies.** REF's picker legend advertises "enter Select" but the
   build did not visibly open a sub-modal; this cycle does not
   touch Enter handling and neither does cycle 196, 197, or any
   later cycle. Closed in a future cycle if a sub-modal ever
   materialises in REF.
3. **Footer-rendering caveat.** The footer test accepts both `←/→`
   (the renderer-emitted form) and `<-/->` (the changelog's
   shorthand), so a Ghidra-recovered render that loses the unicode
   arrow will not break the test, but neither will one that emits
   `← / →` with surrounding spaces (which `panelKeyboardHint` does
   not produce, but the test is also coupled to the substring
   `Change` regardless of glyph format). If a future cycle changes
   the legend's wording, the test will need to be amended.
4. **No source change.** `git diff main..feature/config-left-right-save`
   shows the test file as the only addition; `cmd/wagyu/slash_panels.go`
   and `internal/auth/compat.go` are unchanged from `10a4d31`. The
   deliverable is a verification, not a behaviour addition.

## Closing TODO.md

TODO.md §5 P4 contains the line:

> - **panel 内的按键动作**：`/model ←/→`（切换 effort）、`/config ←/→`（change value）、`/plugin tab`（cycle tabs）、`space Toggle` 等仍 inert。

Of the four sub-items listed, three were closed by their own cycles
(`/model ←/→` by cycle 197, `/plugin tab` by cycle 198, `space
Toggle` by cycle psif); this cycle closes the fourth (`/config
←/→`). The TODO.md line is left untouched in this branch — a
single-cycle edit of that line is out of scope and a bulk rewording
of P4 belongs in a separate "audit TODO.md P-class backlog" cycle.
