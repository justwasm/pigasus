# Cycle `model-argument-completion` — the `/model <name>` completion popup

**Status**: implemented, `go test ./cmd/... ./internal/... ./devutils/...` green for
every package this cycle touches
**Reference**: `~/.local/bin/agy` — Antigravity CLI **1.4.6**, driven live in tmux
**Evidence**: `captures.md` in this directory — the reference's and the clone's
byte-level captures; `repro/wg01_clone_model_completion.sh` and
`repro/wg02_reference_model_completion.sh` regenerate them

The reference completes the `/model` **argument** with a popup of models.
The clone had no such popup: `/model ` showed nothing, `/model <name>`
opened the Switch Model panel and threw the argument away, and the
composer never showed the argument hint. This cycle adds the popup and the
setter.

> The clone is pinned to agy **1.4.5**; the installed binary auto-updated
> to **1.4.6** before this cycle, so the captures are 1.4.6.
> `internal/referencedrift` already reports the drift (that test fails on
> this machine, before and after this cycle).
>
> Two tests on this machine are red **for that reason alone**, and neither
> is touched by this cycle:
> `internal/cli.TestChangelog_MatchesReferenceBinary` (the live changelog
> grew by the 1.4.5 section the fixture was captured before) and
> `internal/referencedrift.TestReferenceLock_NoUnacknowledgedDrift` (the
> live binary is 1.4.6, the pin is 1.4.5). Re-evidencing them is the
> drift cycle's job (`docs/cycles/reference-drift-145/`), not this one's.

---

## 1. What the reference does 【一手】

| # | behaviour | the reference |
|---|---|---|
| 1 | the trigger | the composer holds `/model` **and a space**; a bare `/model` is still a command prefix |
| 2 | the rows | the display label in a **30-column** field (`marker + label`), the catalogue id **two columns later**, in the dim gray the command descriptions use |
| 3 | the window | five rows, then `   ↓ N more` (dim gray) |
| 4 | the filter | a case-insensitive **substring** of the label **or** of the id — *not* the panel's subsequence matcher |
| 5 | no match | the popup stays open with a single `   No matches` row |
| 6 | `Tab` | completes the **highlighted row's label** into the composer (`/model Claude Sonnet 4.6 (Thinking)`) and closes the popup |
| 7 | `Enter`, resolvable | runs the command: the model changes, the transcript shows `> /model` + `  ⎿  Model set to <label>` (or `Model already set to <label>`) |
| 8 | `Enter`, not resolvable but rows match | completes the highlighted row into the composer — nothing runs |
| 9 | `Enter`, nothing matches | runs the command and reports `Invalid model "<arg>" (available: <ids>)` |
| 10 | the composer | the command token is **blue** when it is an exact command name (`/model`, `/quit` — not `/codesear`, `/clea`, `/mod`) |
| 11 | the argument hint | `[model]` in the dim gray, right after the typed text, and it **survives Esc** |
| 12 | the status row | the model popup leaves the left segment empty — no `esc to cancel` (the command popup keeps it) |

## 2. How the clone does it

The popup is a **second mode of the existing picker widget**, not a new
widget: `tuilib.PickerState` gains a `kind`, and the model mode reuses the
rows, the highlight, the scroll window and the `↓ N more` tail that the
command popup already had. All ~15 `m.picker.Visible()` call sites keep
working unchanged.

| file | what it does |
|---|---|
| `internal/tuilib/picker.go` | `PickerKindModel`, `ModelRow`, `SetModelCatalogueSource`, `modelQuery`, `filterModels` (the substring matcher), the model branch in `Update` |
| `cmd/wagyu/picker_model.go` | the catalogue source, the argument resolver, the invalid-model message, the setter and the Enter semantics |
| `cmd/wagyu/picker_view.go` | `renderModelCompletionRow` (the 30-column field, the two-column gap, the dim id), the model branch in the row loop, `commitPickerSelectionToBuffer` |
| `cmd/wagyu/composer_token.go` | the command-token colouring and the `[model]` argument hint |
| `cmd/wagyu/tui_view.go` | applies both to the composer's view |
| `cmd/wagyu/tui_status_row.go` | the model popup drops the `esc to cancel` hint |
| `cmd/wagyu/tui_prompt_submit.go` | `submitPrompt` gives Enter to the popup first |
| `cmd/wagyu/tui_update_keys.go` | `Tab` goes through `commitPickerSelectionToBuffer` |
| `cmd/wagyu/slash_commands_handlers.go` | `/model <name>` is a direct setter |
| `cmd/wagyu/slash_codesearch.go` | `/model <name>` does **not** open the Switch Model panel |
| `cmd/wagyu/tui_runmode_new.go` | installs the catalogue source |

### 2.1 The catalogue

The popup lists what the CLI can select: the reference's own models first,
then the ones the user configured (`internal/cli.ModelCatalogue`), the same
list the Switch Model panel shows. The catalogue is re-read on **every**
keystroke, so a configuration change shows up immediately.

The rows are carried as `tuilib.SlashCommand` (`Name` = the display label,
`Description` = the catalogue id), so the renderer and the picker share one
row type.

### 2.2 The filter

```go
// internal/tuilib/picker.go
func (p *PickerState) filterModels(query string) []tuilib.SlashCommand
```

a case-insensitive `strings.Contains` over the label **and** the id. This is
deliberately **not** `modelRowsFiltered` (the panel's subsequence matcher):
the reference's completion popup matches a substring, and `cl-opus` matches
nothing there while it matches in the panel.

### 2.3 The three Enter outcomes

`submitModelCompletion` (called from `submitPrompt`, before the prompt is
resolved) asks the resolver first:

* the argument names a model (label or id, case-insensitively, in full) →
  the model changes, the settings are written, the backend is re-pointed and
  the transcript shows `Model set to <label>`;
* it does not, but a row is highlighted → the row's label is completed into
  the composer (`/model <label>`) and the popup closes;
* it does not and nothing matches → `Invalid model "<arg>" (available: …)`.

The same setter is reachable without the popup: `/model <name>` typed and
submitted with the popup closed (`slashModel`'s argument branch) — the
reference behaves the same way, and `openSlashPanelArgs` lets the argument
past the Switch Model panel.

## 3. Parity, state by state

Captured live against the reference (agy 1.4.6) and the clone in the same
tmux geometry (120×40). The full dumps are in `captures.md`.

| state | reference | clone | |
|---|---|---|---|
| `/model ` — composer | `> /model [model]`, `/model` blue, `[model]` dim | **byte-identical** | ✅ |
| `/model ` — rows | 5 rows + `↓ 1 more` | 5 rows + `↓ 3 more` | ⚠️ the catalogue differs (two configured MiniMax models) |
| `/model claude` — rows | the two Claude rows, ids at column 32 | **byte-identical** | ✅ |
| `/model zzz` — rows | `   No matches` | **byte-identical** | ✅ |
| `/model claude` + Tab | the composer holds the label, the popup is gone | **byte-identical** | ✅ |
| `/model <label>` + Enter | `> /model` + `  ⎿  Model set to …` | same, `Model already set to …` when unchanged | ✅ |
| `/model zzz` + Enter | `Invalid model "zzz" (available: …)`, word-wrapped, continuation indented by 5 | same text, the clone hard-wraps without the indent | ⚠️ see *limitations* |
| `/model` (bare) | the command popup | the command popup | ✅ |
| `Esc` | the popup closes, `> /model [model]` stays | **byte-identical** | ✅ |
| the status row | the model popup has no `esc to cancel` | same | ✅ |
| the command token | blue only for an exact command name | same | ✅ |

## 4. The tests

* `internal/tuilib/picker_test.go` — the model mode: the kind, the full
  catalogue, the substring filter (including the `son` / `cl-opus` cases
  that separate it from the panel's matcher), the empty result, the bare
  `/model` staying a command, and the catalogue being re-read.
* `cmd/wagyu/picker_model_test.go` — the rendered rows and the id column,
  the empty query, `Tab`, `Enter` in its three outcomes, the catalogue-id
  resolution, the composer's hint and token colouring, and the status row.

`go test ./cmd/... ./internal/... ./devutils/...` is green.

## 硬约束

- **The popup is the command popup's sibling, not a replacement.** A bare
  `/model` still shows the command rows and `/model` + Enter still opens
  the Switch Model panel; only `/model ` (with the space) switches the
  rows.
- **The filter is a substring of the label or the id.** It is *not* the
  panel's subsequence matcher, and the two must not be unified.
- **`/model <name>` never opens the Switch Model panel.** The argument
  branch comes first in `slashModel`, and `openSlashPanelArgs` lets the
  argument through.
- **The catalogue is the CLI's own.** The popup offers the reference's
  models plus the configured ones — it does not extend the reference's
  list, and the `↓ N more` tail follows the clone's own catalogue.
- **The command token is coloured only for an exact command name.** A
  prefix (`/codesear`) stays plain, like the reference.
- **The `[model]` hint belongs to the value, not to the popup** — it stays
  after Esc.
- **The model popup has no `esc to cancel`.** The command popup keeps it.

## 存疑

- **The captures are 1.4.6, the clone is pinned to 1.4.5.** The 1.4.6
  changelog section is not published in the binary yet, so whether the
  popup is new in 1.4.6 could not be checked. If the popup turns out to be
  a 1.4.6 feature, this cycle moves the clone ahead of its pin — which is
  what the user asked for, but the pin was not moved.
- **The reference's `available:` list is the reference's own catalogue**
  (six ids). The clone lists its own eight. The *rule* is the same; the
  content follows the catalogue.
- **The reference's invalid-model line word-wraps.** The clone hard-wraps
  at the pane's width with no continuation indent, and the break lands
  mid-token (`claude-sonnet` / `-4-6`). The reference breaks at a space and
  indents the continuation by the width of the `  ⎿  ` block prefix. The
  clone's wrapper (`tuilib.WrapRows`) is generic and prefix-unaware, so
  this is a **general** transcript-wrapping difference, not a `/model`
  one — it was left alone.
- **The reference's `/model` *panel* is a different widget.** It has a
  `Search:` box, a relevance order and a dim last row
  (`docs/cycles/model-picker-search/`). The completion popup has none of
  those; the two are not interchangeable and the clone keeps them apart.
- **The reference's status row shows a Claude model verbatim**
  (`Claude Sonnet 4.6 (Thinking)`) while the clone shows
  `Claude Sonnet 4.6 · high`. That is the clone's pre-existing effort-tier
  rendering (`statusEffortSuffix` / `hasEffortTiers`), it affects the
  Switch Model panel identically, and it was not touched here.
- **The reference's print mode rejects a `/model` argument**
  (`error: /model takes no arguments, got "…"`), like every other
  locally-handled print-mode command. The clone's print mode
  (`internal/printmode`) accepts it. That package is separate from the TUI
  and was not touched.

## limitations

- The popup was exercised on one machine, on 2026-10-23, against agy
  1.4.6, in a 120×40 tmux pane. The reference's catalogue is fetched over
  the network, so its captures depend on the account's catalogue.
- The mouse was not exercised: the reference's click handling inside the
  completion popup was not measured. The clone's mouse path completes the
  clicked row the same way `Tab` does (`commitPickerSelectionToBuffer`).
- The `↓ N more` tail, the `available:` list and the popup's row count
  cannot be compared byte-for-byte against the reference on this machine:
  the clone's catalogue holds the two configured MiniMax models the
  reference does not know about. With an empty provider configuration the
  two catalogues are identical and the tails match (`↓ 1 more`).
- The reference's `/model` popup was not driven with a terminal narrower
  than 120 columns; the 30-column label field may collide with the id on a
  narrow pane, and that case is unmeasured on both sides.
- No `internal/referencedrift` artifact was added: a live UI behaviour is
  not a version-scoped fixture, and the reference's own popup is
  re-captured by `repro/wg02_reference_model_completion.sh`.
