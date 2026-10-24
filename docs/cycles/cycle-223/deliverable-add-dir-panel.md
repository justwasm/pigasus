# Cycle 223 — `/add-dir` interactive panel (TODO §2.1 last A-class panel)

## TL;DR

Cycle 217 closed the with-args form of `/add-dir` — `> /add-dir /tmp/foo
⎿ Added /tmp/foo to workspace` — and the brief's TODO §2.1 marker for
the panel-form parity remained open. Cycle 223 closes it: a bare
`/add-dir` (no args) opens an A-class modal that lists the current
`trustedWorkspaces` (live from `auth.LoadTrustedWorkspaces`), exposes
a `  Path: <text>▮` input row backed by the existing `m.panel.text`
buffer, and commits via the cycle-217 `addDirFeedback` path on Enter
(so the panel-form and with-args form share one renderer); Esc closes
with the standard `Exited /add-dir command` echo. **REF 1.4.0 does
not render a panel for bare `/add-dir`** — it just echoes `> /add-dir
⎿ error: no directory specified` (verified against
`ref.adddir-bare.raw.txt` in this cycle's probe dir) — so the panel
is OUR-mode expansion; the brief accepts this and the closed-form
golden (`TestSlashAddDir_ViaRouter`, `TestAddDirFeedback_AddedToWorkspace`)
keeps the with-args path green.

11 new tests in `cmd/wagyu/slash_cycle223_test.go` lock the panel's
behaviour in (routing, empty/non-empty layouts, type/backspace,
three Enter paths: empty/invalid/success, Esc, exit-notice=true).
Cycle-217 golden tests still pass byte-for-byte.

## 1. Evidence

### REF probe (220×50)

```bash
$ tmux -L agy223 new-session -d -s main -x 220 -y 50
$ tmux send-keys 'env AGY_CLI_DISABLE_AUTO_UPDATE=1 ~/.local/bin/agy' Enter
# → Welcome banner; user lands at the prompt with the OAuth token cached
$ tmux send-keys '/add-dir' Enter
# → ref.adddir-bare.raw.txt
```

REF captures (`/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy223/ref/`):

| File | State | What it shows |
|---|---|---|
| `ref.adddir-bare.raw.txt` | bare `/add-dir` against REF 1.4.0 | only the welcome banner + `> /add-dir ⎿ error: no directory specified` echo. **No panel, no modal, no input prompt.** |
| `ref.adddir-welcome.raw.txt` | welcome screen | banner only |

### OUR probe (220×50, same session)

| File | State | What it shows |
|---|---|---|
| `our.adddir-bare.raw.txt` | bare `/add-dir` against OUR | welcome + `⚠ Warning` (MiniMax-M3 → MiniMax M3 mapping, unrelated) + `Add Directory to Workspace` (bold title) + `  Trusted workspaces:` (dim gray heading) + **27-entry trusted list** + `  Path: ▮` (input row with reverse-video cursor) + `Keyboard: enter Add Directory  esc Cancel` footer |
| `our.adddir-typed-after-typing.raw.txt` | `/add-dir` then type `/tmp/agy223/probe` | same panel, but the `Path:` row now reads `  Path: /tmp/agy223/probe▮` (typed text in front of the cursor block) |
| `our.adddir-typed-commit.raw.txt` | Enter after typing | the panel is gone, replaced by the single `> /add-dir ⎿ Added /tmp/agy223/probe to workspace` echo (cycle-217 wording rides through unchanged) |
| `our.adddir-esc.raw.txt` | Esc after typing | carries the cycle-217 commit echo from the prior probe + a fresh `> /add-dir ⎿ Exited /add-dir command` echo (Esc path uses `closePanel`, which emits the standard exit notice) |

### Probe-vs-clone summary table (220×50)

| Metric | REF 1.4.0 (bare `/add-dir`) | OUR cycle-223 (bare `/add-dir`) |
|---|---|---|
| **Modal / overlay** | none (just a one-liner echo) | full 6-section panel (title / blank / trustedWorkspaces / blank / Path input / blank / Keyboard hint) |
| **Title** | — | `Add Directory to Workspace` (bold via `panelTitleOpen` + `panelTitleClose`) |
| **trustedWorkspaces list** | not shown | rendered line-by-line from `auth.LoadTrustedWorkspaces()` (dim gray 241 via `cDimGrayOpen` + `cDimGrayClose`) |
| **Empty-state copy** | — | `  No trusted workspaces yet.` (same dim-gray SGR) |
| **Input row** | — | `  Path: <text>▮` — typed text + `searchCursorBlock` (reverse-video cell, same primitive `/model` and `/config` use) |
| **Keyboard hint footer** | — | `Keyboard: enter Add Directory  esc Cancel` (brand-blue keys + dim-gray labels via `panelKeyboardHint`) |
| **Enter on typed path** | n/a | `addDirFeedback` runs → `slashRespond` echoes `> /add-dir ⎿ Added <path> to workspace` (cycle-217 wording, byte-exact) → silent panel close (no double `Exited` notice) |
| **Esc** | n/a | panel closes with `Exited /add-dir command` echo via `closePanel` (standard exit-notice group) |
| **Bare `/add-dir` print line** | `> /add-dir ⎿ error: no directory specified` (in-band echo, no modal) | modal opens (no in-band echo — the panel is the response) |

The 27-entry trusted list visible in `our.adddir-bare.raw.txt` /
`our.adddir-typed-after-typing.raw.txt` is **the side-effect of a
prior probe run** (settings.json grew during `/add-dir
/tmp/agy223/probe` typing; the captures were taken before the cycle
end restored the SHA). The restored SHA at cycle end matches the
back-up byte-for-byte (see Verification §5). Cycle-223 test isolation
uses `t.Setenv("ANTIGRAVITY_HOME", t.TempDir())` so the test suite
never touches `~/.gemini/antigravity-cli/settings.json`.

## 2. Layout (220×50)

### Empty state (no trustedWorkspaces)

```
Add Directory to Workspace        ← bold title

  No trusted workspaces yet.      ← dim gray 241 (cDimGrayOpen..cDimGrayClose)

  Path: ▮                         ← input row, searchCursorBlock at end

Keyboard: enter Add Directory  esc Cancel   ← brand-blue keys + dim-gray labels
```

7 rows. The "No trusted workspaces yet." line replaces the heading +
list when `auth.LoadTrustedWorkspaces()` returns an empty slice.
This is the form exercised by every cycle-223 test (the test fixture
calls `writeSettings(t, home, "{}")` so the settings file is empty
→ `LoadTrustedWorkspaces` returns `[]`).

### Non-empty state (25+ trusted workspaces from prior probes)

```
Add Directory to Workspace

  Trusted workspaces:              ← dim gray heading
  /Users/gear/GitHub/crush         ← dim gray per-entry path
  /Users/gear/GitHub/vscode
  /Users/gear/GitHub/gearshell
  …                                ← one row per LoadTrustedWorkspaces entry
  /tmp/agy223/probe

  Path: /tmp/agy223/probe▮         ← typed text + reverse-video cursor block

Keyboard: enter Add Directory  esc Cancel
```

`N+6` rows where `N == len(trusted)`. The cursor sits at the **end**
of the typed text (the cycle-193 precedent is "cursor at end of
typed text, no blink"). `searchCursorBlock` is the same primitive
`/model`'s `Search:` and `/config`'s `Search:` use — a single
reverse-video cell appended to the typed buffer. See
`cmd/wagyu/slash_panels.go:1154`.

## 3. Behaviour

### Routing

`openSlashPanelArgs` in `cmd/wagyu/slash_codesearch.go:341` is the
TUI dispatcher. Cycle 223 prepends a guard:

```go
// Cycle 223: `/add-dir <path>` falls through to the cycle-217 echo path
// (case "add-dir" in slash_commands.go still calls addDirFeedback directly)
// and only a bare `/add-dir` opens the panel — the panel form is the
// only A-class gap TODO §2.1 had left.
if cmd == "add-dir" && args != "" {
    return false
}
```

This is the **single point that decides** bare-vs-with-args routing:

- `cmd == "add-dir"`, `args == ""` → return `true` after `openSlashPanel`
  has set `m.panel.kind = panelAddDir`.
- `cmd == "add-dir"`, `args != ""` → return `false` (fall through).
  The existing `case "add-dir"` in `slash_commands.go:472` runs and
  `addDirFeedback(args)` echoes the cycle-217 wording.

`TestPanelAddDirRouting` pins both branches via two `runMode`s sharing
the same temp-dir home; the fall-through guarantee is what keeps
`TestSlashAddDir_ViaRouter` and `TestAddDirFeedback_AddedToWorkspace`
green. Both still pass byte-for-byte (see Verification §5).

### Esc → `closePanel()`

`m.panel.kind == panelAddDir` is **not** in cycle-193's silent-close
carve-out (`panelMcp`, `panelFeedback`, … — see
`TestPanelExitNoticeNotEmitted`). Esc goes through the standard
`closePanel`, which writes `> /<cmd> ⎿ Exited /<cmd> command`. This is
what `our.adddir-esc.raw.txt` shows.

### Enter → `addDirFeedback` + `slashRespond` + `closePanelSilent()`

`handleAddDirKey` routes Enter through three steps:

```go
case key.Code == tea.KeyEnter:
    msg := addDirFeedback(m.panel.text)         // cycle-217 string
    m.slashRespond("/add-dir", msg)             // cycle-195 B-format echo
    return true, m.closePanelSilent()           // no second exit notice
```

`addDirFeedback` (in `cmd/wagyu/slash_actions.go:59`) returns one of
three strings:

| `m.panel.text` | Returned string | Echo |
|---|---|---|
| empty | `error: no directory specified` | `> /add-dir ⎿ error: no directory specified` |
| non-dir path (e.g. a file) | `error: not a directory: <path>` | `> /add-dir ⎿ error: not a directory: <path>` |
| valid directory | `Added <path> to workspace` | `> /add-dir ⎿ Added <path> to workspace` |

The panel-form and with-args form share this renderer byte-for-byte —
the panel is a front-end for the same path the command line was
already using. `TestPanelAddDirEnterEmpty` and `TestPanelAddDirEnterInvalid`
pin the first two; `TestPanelAddDirEnterEchoes` pins the third by
typing `/tmp/foo` into `m.panel.text`, hitting Enter, and asserting
the cycle-195 B-format echo block + panel close.

`closePanelSilent` (new this cycle, appended after `closePanel` in
`slash_panels.go:3072`) clears the panel and runs `reloadVimSettings`
but **skips the exit-notice echo**. This is what keeps the cycle-217
echo from doubling up with `Exited /add-dir command`. Esc still uses
`closePanel` (with notice).

### Printable keys / backspace

Standard text-buffer editing, mirroring `handleFeedbackTextKey`:

- `Backspace` → drop last rune (`[]rune(m.panel.text)[:len(r)-1]`).
- Printable key with `key.Mod == 0` → append `key.Text`.
- Modified key (`Ctrl+*`, `Alt+*`) → fall through (return `false, nil`).

`TestPanelAddDirTypingFillsBuffer` and `TestPanelAddDirBackspace` pin
both branches.

## 4. Change

`cmd/wagyu/slash_panels.go`

- `panelKind` enum gains `panelAddDir // /add-dir picker (cycle 223) —
  last A-class panel` (line 78).
- `panelForCommand("add-dir")` returns `(panelAddDir, true)` (line
  348–352). The mapping now matches the with-args command path's
  intent: every `/<cmd>` with an A-class panel routes through
  `panelForCommand`, and `/add-dir` is the last one that didn't.
- `panelLines()` switch gains `case panelAddDir: return
  m.panelAddDirLines()` (lines 534–539), placed after `panelDiff`
  so the diff-position ordering is preserved.
- `handlePanelKey` gains `if m.panel.kind == panelAddDir { return
  m.handleAddDirKey(key) }` (lines 2525–2527) just before the
  document-viewer scroll branch.
- New `closePanelSilent()` (lines 3083–3087): clears the panel,
  refreshes vim settings, returns nil. Skips the exit-notice echo.
- New `panelAddDirLines()` (lines 3246–3275): title + (empty-state
  copy OR `  Trusted workspaces:` heading + one dim-gray row per
  `auth.LoadTrustedWorkspaces()` entry) + blank + `  Path:
  <text>` + `searchCursorBlock` + blank + keyboard hint.
- New `handleAddDirKey()` (lines 3297–3319): Esc/Enter/Backspace
  switch + `key.Mod == 0` printable-key append + fall-through for
  modified keys.

`cmd/wagyu/slash_codesearch.go`

- `openSlashPanelArgs` (line 341) gains the cycle-223 guard at the
  top: `if cmd == "add-dir" && args != "" { return false }`. This
  is the only edit in this file; the rest of the dispatcher is
  untouched.

`cmd/wagyu/slash_cycle223_test.go` (new, 338 lines, 11 tests)

- `TestPanelAddDirForCommand` — `panelForCommand("add-dir")` returns
  `(panelAddDir, true)`; `openSlashPanel("add-dir")` sets
  `m.panel.kind == panelAddDir` and `m.panel.cmd == "/add-dir"`.
- `TestPanelAddDirRouting` — `openSlashPanelArgs("add-dir", "")`
  opens the panel; `openSlashPanelArgs("add-dir", "/tmp/foo")`
  returns `false` and leaves `m.panel.kind == panelNone`. Pins the
  bare-vs-with-args split that the cycle-217 tests rely on.
- `TestPanelAddDirEmptyLayout` — empty-state 7-row layout pinned
  row-for-row (title / blank / `  No trusted workspaces yet.` /
  blank / `  Path: ▮` / blank / Keyboard hint).
- `TestPanelAddDirTrustedListed` — settings.json with two entries
  produces the heading + two dim-gray rows; `panelAddDirLines()`
  returns the right length and the entry texts.
- `TestPanelAddDirTypingFillsBuffer` — three printable keys append
  to `m.panel.text`; `panelAddDirLines()` renders the typed text in
  the `Path:` row.
- `TestPanelAddDirBackspace` — Backspace drops the last rune; the
  typed-then-truncated text shows up correctly in the layout.
- `TestPanelAddDirEnterEchoes` — type `/tmp/foo`, Enter: panel
  closes, `m.response` carries the cycle-195 B-format echo block.
- `TestPanelAddDirEnterEmpty` — empty input + Enter: panel closes,
  `m.response` carries the cycle-217 `error: no directory specified`
  echo (the same `addDirFeedback("")` path the with-args form takes).
- `TestPanelAddDirEnterInvalid` — typed path that is a regular file
  + Enter: panel closes, `m.response` carries `error: not a
  directory: <path>` (same as with-args form on a non-dir path).
- `TestPanelAddDirEsc` — Esc closes the panel; `m.response` carries
  the cycle-195 B-format `Exited /add-dir command` echo via
  `closePanel`.
- `TestPanelAddDirExitNotice` — `panelAddDir.exitNotice()` returns
  `true` (panel is in the exit-notice-true set; Esc emits the
  notice).

`TODO.md`

- §2.1 top note: `✅ Cycle 223 已关闭：/add-dir panel (no-arg)` — the
  last A-class panel row.
- §2.1 row for `/add-dir` strikes the cycle-217 "未提供面板" note and
  adds the cycle-223 panel summary.
- §4 closed table gains the cycle-223 row.

## 5. Verification

```text
$ go build -o /tmp/wagyu223-verify ./cmd/wagyu   # exit 0, ~70 MB
-rwxr-xr-x 1 gear staff 72832818 Oct 18 02:59 /tmp/wagyu223-verify

$ go test -vet=off ./cmd/... ./internal/... -count=1
…
ok  	github.com/chadgpt/openagy/cmd/wagyu	9.789s
ok  	github.com/chadgpt/openagy/internal/agent	0.624s
ok  	github.com/chadgpt/openagy/internal/auth	26.655s
…
ok  	github.com/chadgpt/openagy/internal/vim	0.275s

$ go test -vet=off ./cmd/wagyu -run '^TestPanelAddDir|^TestAddDir|^TestSlashAddDir|^TestPanelForCommand' -count=1
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.169s

$ gofmt -l cmd/wagyu/slash_cycle223_test.go
# (empty — file is gofmt-clean)

$ gofmt -l cmd/wagyu/slash_panels.go
# (NOT empty — pre-existing whitespace inconsistencies from cycle
#  193/214, gofmt never clean per AGENTS.md, not introduced by cycle
#  223)

$ go test -vet=off ./cmd/wagyu -run '^TestPanelAddDir' -count=1 -v
=== RUN   TestPanelAddDirForCommand
--- PASS: TestPanelAddDirForCommand
=== RUN   TestPanelAddDirRouting
--- PASS: TestPanelAddDirRouting
=== RUN   TestPanelAddDirEmptyLayout
--- PASS: TestPanelAddDirEmptyLayout
=== RUN   TestPanelAddDirTrustedListed
--- PASS: TestPanelAddDirTrustedListed
=== RUN   TestPanelAddDirTypingFillsBuffer
--- PASS: TestPanelAddDirTypingFillsBuffer
=== RUN   TestPanelAddDirBackspace
--- PASS: TestPanelAddDirBackspace
=== RUN   TestPanelAddDirEnterEchoes
--- PASS: TestPanelAddDirEnterEchoes
=== RUN   TestPanelAddDirEnterEmpty
--- PASS: TestPanelAddDirEnterEmpty
=== RUN   TestPanelAddDirEnterInvalid
--- PASS: TestPanelAddDirEnterInvalid
=== RUN   TestPanelAddDirEsc
--- PASS: TestPanelAddDirEsc
=== RUN   TestPanelAddDirExitNotice
--- PASS: TestPanelAddDirExitNotice
PASS

# Golden tests pinned by earlier cycles still pass byte-for-byte:
$ go test -vet=off ./cmd/wagyu -run '^TestSlashAddDir|^TestAddDirFeedback' -count=1
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.041s

# Cycle-193 silent-close carve-out (panelMcp, panelFeedback, …) intact:
$ go test -vet=off ./cmd/wagyu -run '^TestPanelExitNotice' -count=1
ok  	github.com/chadgpt/openagy/cmd/wagyu	0.952s

# settings.json SHA matches the cycle-217 backup byte-for-byte:
$ shasum -a 256 ~/.gemini/antigravity-cli/settings.json
8731cabdc8b0eaf53159e81d468092c26d30a8254b5a48f68bdb579daf1493d7
```

27 of 27 packages green, 11 cycle-223 tests green, 3 cycle-217/193
golden groups still green, build exit 0, settings.json SHA preserved.

## 6. Not done / 存疑 / limitations

- **REF 1.4.0 has no panel for bare `/add-dir`.** Verified via
  `ref.adddir-bare.raw.txt` in this cycle's probe dir: REF prints
  `> /add-dir ⎿ error: no directory specified` and nothing else —
  no modal, no input prompt, no trustedWorkspaces list. The panel is
  **OUR-mode expansion**: cycle 193 declared that every `/<cmd>`
  with an A-class surface gets a modal in OUR, and the panel-form
  parity gap (panel-form vs with-args-form) was the last entry in
  TODO §2.1. A future cycle with a logged-in REF account could
  attempt to discover whether REF plans to ship a panel-form
  (Cursor / Aider have picker variants) or stay with the bare echo,
  but on the evidence available to this checkout the panel is
  OUR-only. **The brief explicitly accepts this** (cycle-223
  parenthetical: "REF 1.4.0 doesn't render a panel for bare
  /add-dir, OUR-mode expansion is acceptable").

- **The 27-entry trusted list visible in
  `our.adddir-bare.raw.txt` / `our.adddir-typed-after-typing.raw.txt`
  is a probe-order artefact.** The probe sequence was: type the
  panel open → typed `/tmp/agy223/probe` → committed via Enter →
  `settings.json` grew one entry → re-ran the panel-open capture
  (now showing 27 entries) before restoring the SHA. The **current
  state** of `settings.json` matches the cycle-217 backup byte-for-byte
  (SHA `8731cabd…`, verified at the end of the cycle). Cycle-223 tests
  use `t.Setenv("ANTIGRAVITY_HOME", t.TempDir())` so they never touch
  the user's settings file directly — the trusted-list rendering is
  pinned in `TestPanelAddDirEmptyLayout` (settings file = `{}`)
  and `TestPanelAddDirTrustedListed` (two-entry settings file).

- **Panel height-driven windowing is not implemented.** The
  `trustedWorkspaces` list could in principle grow past the screen
  height on a long history (40+ entries are possible). Cycle 222's
  `/rewind` panel already has windowing for that case (a
  `(first–last of N turns)` counter), but cycle 223 renders the
  full list and accepts that a very long list will scroll past the
  visible budget. The cap is unlikely to bite in practice (25
  entries × ~80 chars = ~2 KB ≈ 50 rows on 220×50), and a future
  cycle can lift the cycle-222 windowing pattern when a real REF
  probe demonstrates REF's own list-clamp behaviour.

- **`m.panel.text` is shared with `/feedback`'s stage-2 form.**
  Both panels reuse the same field — they're mutually exclusive
  (only one panel open at a time), so the field is never in
  contention. If a future cycle introduces a panel that switches
  between two text buffers (e.g. a tabbed `/add-dir` form), this
  field will need to graduate to a per-panel buffer.

- **The `Esc` capture (`our.adddir-esc.raw.txt`) shows the cycle-217
  commit echo from the prior probe.** This is again probe-order:
  the Esc capture was taken after the Enter commit, so the screen
  buffer at capture time still showed the cycle-217 echo. The
  **fresh** Esc notice is on lines 14–15 of the same file. Both are
  correct and consistent with the brief — Enter emits the
  cycle-217 echo via `slashRespond`, Esc emits the standard exit
  notice via `closePanel`.

- **Cycle 223 closes TODO §2.1 but does not yet close TODO §4 (the
  B-class surface).** §4 covers the in-band-mode behaviours (e.g.
  `/help`, `/compress`, `/statusline` non-prompt form). §2.1 was
  the A-class modal set; cycle 223 closes its last row. §4 is a
  separate workstream.
