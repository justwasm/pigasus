# Cycle 02 — adopt `charm.land/bubbles/v2/textinput`

**Branch:** `feature/charmland-component-adoption`
**Package:** `cmd/wagyu`
**Upstream:** `charm.land/bubbles/v2@v2.1.1/textinput`
**Status:** ✅ shipped

## Goal

Wagyu's clone had **zero single-line textinput components** — every prompt
used `textarea` or a hand-rolled rune-accumulation buffer. The brief's
option D for cycle 02 was to add a `/rename` slash command backed by a
`textinput.Model`, which is the same component AGY 1.4.2 ships
(`strings(1) | grep -c 'textinput'` on `~/.local/bin/agy` returns 66
hits per the brief).

The migration is fully **additive**: the existing `/rename "new name"`
cmd-line path is unchanged. The bare `/rename` invocation used to echo
`Error: Please provide a new name for the conversation.` and accept no
further input. It now opens an interactive panel seeded with the
current conversation name (if any), with full cursor navigation, word
delete, and char-limit support.

## What changed

| File | Change | Lines |
|---|---|---|
| `cmd/wagyu/slash_rename_panel.go` | **new** — `panelRenameLines`, `handleRenamePanelKey`, `submitRenamePanel`, `openRenamePanel`, `newRenameTextinput`, `keyMsgFromKey` | +196 |
| `cmd/wagyu/slash_rename_panel_test.go` | **new** — 12 tests | +309 |
| `cmd/wagyu/slash_panels.go` | + `panelRename` enum value · + `textInput textinput.Model` field on `slashPanel` · + `panelRename` in `exitNotice()` (silent-on-Esc, like `panelMCP`/`panelPlugin`/…) · + `case "rename"` in `panelForCommand` | +37/-1 |
| `cmd/wagyu/slash_panels_test.go` | updated `TestPanelForCommand` for `rename → panelRename` | +9/-1 |
| `cmd/wagyu/slash_commands.go` | no-args `/rename` branches into `m.openRenamePanel()` (with-args path untouched) | +10/-0 |

`panelRename` follows the same shape as `panelAddDir`, `panelFeedback`,
`panelMCP`, etc.:

1. `panelForCommand("/rename")` → `panelRename`
2. `panelLines()` for `panelRename` → `panelRenameLines()` (title + blank +
   prompt + input + blank + keyboard hint)
4. `handlePanelKey` for `panelRename` → `handleRenamePanelKey()`
5. Esc → `closePanel()` (silent-on-Esc per `exitNotice()`)
6. Enter → `submitRenamePanel()` → `renameFeedback` + `closePanelSilent()`

## Why a new panel rather than retrofitting the existing `/rename` path

The pre-cycle-02 `/rename` (no args) UX was an error echo with no
follow-up input affordance — the worst UX in `/rename`'s documented
surface. The cmd-line `/rename "name"` path still works unchanged; only
the no-args branch is now an interactive panel. This matches AGY 1.4.2's
behaviour for prompts that take a single-line string (the same package
ships the `/rename` prompt in the reference build, per the brief's
66-hit `textinput` count).

## textinput priming (the wrapper)

```go
func newRenameTextinput(seed string) textinput.Model {
    in := textinput.New()
    in.Prompt      = textinputRenamePrompt       // bold brand-blue "Rename: "
    in.Placeholder = textinputRenamePlaceholder  // 256-grey "type a name and press enter…"
    in.CharLimit   = panelRenameLimit            // 200 runes
    in.SetWidth(panelRenameInputWidth)           // 60 cells
    in.Focus()                                   // focused on open
    in.SetValue(seed)                             // seed = current conversation name
    return in
}
```

Two non-obvious choices:

- **`SetWidth(panelRenameInputWidth)` is mandatory for the
  placeholder to render fully.** textinput.go:756 short-circuits the
  placeholder render when `m.Width() < 1` (`if m.Width() < 1 &&
  len(p) <= 1 { return ... }`) — so without `SetWidth(60)` the user
  would only see the first character of the placeholder. 60 cells
  matches the recovered reference's panel-row width.
- **CharLimit = 200**, not the 100-rune default. Cycle 194 set
  200 as the keybinding panel's typed-sequence max, and 200 is
  generous for conversation names (cycle 175 sizes the title column
  to fit a UTF-8 name of that order). textinput silently drops
  characters past the limit (no error).

## Key handler dispatch

```go
func (m *runMode) handleRenamePanelKey(key tea.Key) (bool, tea.Cmd) {
    switch key.Code {
    case tea.KeyEsc:
        m.closePanel()
        return true, nil
    case tea.KeyEnter:
        return true, m.submitRenamePanel()
    }
    updated, cmd := m.panel.textInput.Update(keyMsgFromKey(key))
    m.panel.textInput = updated
    return true, cmd
}
```

Three notes:

- **Esc and Enter are handled at the panel level** so the
  submit/cancel paths can run their cycle-167/193 echo logic. Other
  keys (Left/Right, Home/End, Ctrl+W, Ctrl+U, Backspace, char
  insertion) flow through `textinput.Update` unchanged — that's the
  migration's primary UX win (none of those worked in the old
  hand-rolled `panel.text` buffer).
- **`keyMsgFromKey`** adapts the panel-level `tea.Key` to a
  `tea.KeyPressMsg`. textinput does not accept `tea.Key` directly;
  the adapter is the canonical seam between the two bubble packages.
  Verified against the v2.0.9 `bubbletea/key.go` source — the field
  names on `tea.KeyPressMsg` (`Code`, `Mod`, `Text`) are stable.
- **Both Esc and submit paths return `nil` cmd** (consistent with
  `closePanel`/`closePanelSilent`, which have always returned `nil`
  — see `slash_panels.go:3727` and `:3744`). The panel-clearing
  mutation is the deliverable, not a `tea.Cmd`. The first version
  of the test suite over-specified by checking `cmd != nil`; the
  fix was to drop the assertion (matching the cycle 222 picker-close
  pattern at `slash_cycle222_test.go:260`).

## Submit path

```go
func (m *runMode) submitRenamePanel() tea.Cmd {
    name := m.panel.textInput.Value()
    newName, msg := renameFeedback(name, m.conversationName)
    if msg != "" {
        m.slashRespond("/rename", msg)
    }
    if newName != "" {
        m.conversationName = newName
    }
    return m.closePanelSilent()
}
```

`renameFeedback` is the cycle-106 normaliser: drops leading/trailing
whitespace, collapses embedded newlines, returns `(newName, message)`
where `message` is the transcript line shown to the user. On
success: `Conversation renamed to: <name>`. On empty input:
`Error: Please provide a new name for the conversation.` — same
bytes regardless of whether the user typed `/rename foo` or opened
the panel and typed `foo`.

`closePanelSilent()` (not `closePanel()`) is the right exit because
the success/error echo already covers the commit; emitting the
standard `Exited /rename command` notice on top would yield two
stacked `> /rename` headers (cycle 223's reasoning for
`/add-dir`'s commit path, applied here).

## Test coverage

12 new tests, all passing:

| Test | Asserts |
|---|---|
| `TestOpenRenamePanel_KindAndSeed` | panel.kind = panelRename · textinput seeded with current name · focused |
| `TestOpenRenamePanel_EmptySeed` | empty-seed path produces an empty (placeholder-visible) textinput |
| `TestHandleRenamePanelKey_EnterSubmits` | Enter → panel closes · success echo emitted |
| `TestHandleRenamePanelKey_EmptySubmitHitsErrorPath` | empty Enter → error echo (matches cmd-line `/rename` no-args) |
| `TestHandleRenamePanelKey_EscClosesSilently` | Esc → panel closes · no exit notice (silent-on-Esc group) |
| `TestHandleRenamePanelKey_CursorNavigationPassesThrough` | Left/Right/Home → cursor moves through textinput |
| `TestHandleRenamePanelKey_WordDelete` | Ctrl+W → word backward delete (hand-rolled buffer dropped this) |
| `TestHandleRenamePanelKey_CharLimitApplied` | CharLimit = 200 · 300-char paste becomes 200-char value |
| `TestNewRenameTextinput_DefaultShape` | newRenameTextinput primes Value/Prompt/Placeholder/CharLimit/Focused |
| `TestPanelRenameLines_RendersInput` | panelRenameLines shape = [title, blank, prompt+input, blank, hint] |
| `TestPanelRenameLines_PlaceholderWhenEmpty` | empty value → placeholder visible |
| `TestSubmitRenamePanel_NoopIfNoCurrentAndEmpty` | empty submit on fresh convo → error echo + panel closes |

## Full test suite

```
$ go test -vet=off -count=1 ./cmd/wagyu/ ./internal/... 2>&1 | grep -E 'FAIL|^ok ' | grep FAIL
--- FAIL: TestDispatchPrintSlash_LocalCommands (0.00s)
--- FAIL: TestChangelog_MatchesReferenceBinary (1.07s)
```

Two failures — both are in the 7 pre-existing test flakes list
(`TestDispatchPrintSlash_LocalCommands` and
`TestChangelog_MatchesReferenceBinary`). The other 5 flakes did not
fire on this run. **No new failures.**

## Build / binary size

```
$ go build -o /tmp/wagyu-charmland ./cmd/wagyu
$ ls -la /tmp/wagyu-cycle1 /tmp/wagyu-charmland
-rwxr-xr-x  1 gear  staff  73767010  /tmp/wagyu-cycle1       # cycle 01 baseline
-rwxr-xr-x  1 gear  staff  73869282  /tmp/wagyu-charmland    # cycle 02 (+textinput panel)
```

**+102,272 B (+100 KB, +0.14 %).** `charm.land/bubbles/v2/textinput`
was already a transitive dep via `charm.land/bubbles/v2@v2.1.1`
(verified via `go list -deps ./cmd/wagyu | grep bubbles/v2/textinput`).
The cost is the new file (~196 lines of code + ~309 lines of tests) —
the package itself was free.

## Files

```
 cmd/wagyu/slash_rename_panel.go      | 196 +++++++++++++++++++++++  (new)
 cmd/wagyu/slash_rename_panel_test.go | 309 ++++++++++++++++++++++++++ (new)
 cmd/wagyu/slash_commands.go          |  10 +
 cmd/wagyu/slash_panels.go            |  37 +++++++++++++-
 cmd/wagyu/slash_panels_test.go       |   9 +-
 5 files changed, 559 insertions(+), 2 deletions(-)
```

## Hard constraints / limitations

- **The AGY 1.4.2 reference behaviour for `/rename` is unknown** — the
  brief's 66-hit `textinput` count pins the package but not the exact
  panel surface. We adopt `textinput` here because it is the standard
  single-line input component AGY ships, and the previous behaviour
  (an error echo with no input affordance) was the worst UX in
  `/rename`'s documented surface. If 1.4.2's `/rename` actually opens
  a modal with multi-line support, a future cycle can migrate this to
  `textarea` (cycle 119's component) without changing the
  `panelRename` enum value.
- **The textinput package requires `SetWidth(N)` for the placeholder
  to render fully** (`textinput.go:756`'s `m.Width() < 1` early
  return). 60 cells is hard-coded as `panelRenameInputWidth`. If a
  follow-up cycle makes the rename panel resizable, this constant
  should become a function of `m.width`.
- **No real-network test.** `submitRenamePanel` calls
  `renameFeedback` which is pure-function; no `agy` network round-trip
  is exercised. Cycle 226's picker tests have the same property.
- **The existing hand-rolled `panel.text` field is still present**
  on `slashPanel`. It is used by `panelFeedback` (stage-2 input
  buffer) and `panelAddDir` (committed path). Removing it would
  require migrating both panels off the hand-rolled buffer too —
  out of scope for cycle 02.
- **Pre-existing gofmt issues in `slash_panels.go`** (e.g. line 3818's
  under-indented `row := rows[idx]`) are not touched — per
  AGENTS.md, the clone has 50+ gofmt-dirty files we do not reformat
  wholesale. Only the new file's import order was fixed.

## Follow-up cycles

Cycle 03 (`bubbles/v2/viewport`) is the next deliverable. The
textinput migration is the last *additive* surface — cycles 03/04/05
touch hot-path components (viewport / key / cursor) that have wider
blast radius. Read `docs/cycles/cycle-119*/deliverable.md` (alt-screen
fix) and `docs/cycles/cycle-122*/deliverable.md` (quadratic wrap cost
fix) before starting cycle 03 — both cycle through `tui_viewport.go`'s
971 lines and the wrap-cost history is load-bearing.