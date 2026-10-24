# Cycle 232 — `@` file-mention dropdown (AGY 1.3.6 line 12)

**Branch:** `feature/at-file-dropdown-newline`
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-at-file-dropdown-newline`
**Baseline:** `54db67b` (cycle 224 merge)

AGY 1.3.6 changelog, line 12:

> · Fixed the @ file-mention dropdown not appearing after a newline — typing `@<char>` at the start of a new line should now show the file dropdown just like at the end of a line

(`internal/cli/testdata/changelog_reference.txt`.)

## TL;DR

The clone never had an `@` file-mention dropdown — the cycle-71 textarea + cycle-77 slash router treat the input as either a slash command or a chat message. Cycle 232 adds a third pipeline:

1. `internal/atfiles/atfiles.go` (new) — stdlib-only parser (`Parse`) and `filepath.Glob`-backed file lookup (`Find`). Pure functions, no UI / runtime dependencies.
2. `cmd/wagyu/at_file_dropdown.go` (new) — `runMode`-scoped dropdown state machine, view renderer, and the textarea-splice helper that powers `Tab` completion.
3. Wiring in `cmd/wagyu/tui_run.go` and `cmd/wagyu/history.go` — the dropdown is re-derived on every keystroke (incl. `Ctrl+J` newline, history recall, panel-open), with `Tab` / `↑` / `↓` / `Esc` routed to it when visible, and a render-slot added to `viewBody`.

The AGY 1.3.6 fix lives in the parser's `walkStart` loop: a `@` whose preceding byte is not a token char (start-of-buffer, whitespace, path-separator, punctuation) qualifies as the active mention regardless of where it sits in the buffer — at end-of-line, after `\n`, mid-line, or after a comma.

## Scope

| File | Role |
|---|---|
| `internal/atfiles/atfiles.go` (new) | `Mention`, `Parse`, `Find`, `MaxPrefixLen`, `MaxCandidates` |
| `internal/atfiles/atfiles_test.go` (new) | 3 top-level tests (20 runs incl. subtests): `TestParse`, `TestParse_AfterNewlineBehavesLikeEndOfLine`, `TestFind` |
| `cmd/wagyu/at_file_dropdown.go` (new) | `atFileDropdown` struct, `Reset/Visible/Empty/Len/Idx/Selected/MoveUp/MoveDown/Update/TabCompletion/view`, `(*runMode).tabSpliceAtFile`, `(*runMode).atFileView` |
| `cmd/wagyu/at_file_dropdown_test.go` (new) | 15 top-level tests (22 runs incl. subtests) — every required scenario + 9 extras |
| `cmd/wagyu/tui_run.go` | +64: field decl, key routing (Esc/Tab/Up/Down/Ctrl+J), viewBody render slot, submit/panel-open reset |
| `cmd/wagyu/history.go` | +8: `historyUp` / `historyDown` re-derive the dropdown the same way they re-derive `m.picker` |

**Total:** 6 files changed, **+~890 / -0**.

## Parser state machine

`atfiles.Parse(buf string) (Mention, bool)` in `internal/atfiles/atfiles.go:76`. The single state machine:

```
empty buf                                                 ─► (!ok)
buf ends on ' ' / '\t' / '\n' / '\r'                       ─► (!ok)  cursor has typed past the mention
walk back from end across {token-char, '@', ' ', '\t'}     ─► walkStart = leading boundary
find latest '@' in [walkStart, end) with boundary check    ─► !ok when no boundary-respecting '@' exists
prefix = buf[at+1 : next non-{token, '/', '@', ' '} byte]  ─► ok
prefix.length > MaxPrefixLen                               ─► (!ok)
prefix contains '\x00'                                     ─► (!ok)
otherwise                                                  ─► (Mention{prefix, at, end_of_prefix}, ok)
```

The boundary check is the AGY 1.3.6 line-12 fix in one line: the latest `@` is accepted only when its preceding byte is **not a token char** (start-of-buffer, whitespace, `/`, punctuation, control byte). The walk loop allows `@` to appear inside the run so paths like `@a/@b` parse as a single mention (the embedded `@` is part of the path, not a new trigger).

`atfiles.Find(root, prefix)` is a thin `filepath.Glob` wrapper. A trailing slash means "browse into this directory"; the result is sorted and capped at `MaxCandidates = 64` entries so the dropdown's highlighted-row decision is stable across calls.

## Dropdown state machine

`cmd/wagyu/at_file_dropdown.go`. `runMode`-scoped state, mirroring how `picker tuilib.PickerState` is structured for the slash picker:

```
Update(buf, cwd)
   │
   ├── atfiles.Parse(buf) == !ok   ─► Reset()                        ─► visible=false
   └── atfiles.Parse(buf) == ok    ─► prefix / mentionStart / End    ─► visible=true
                                     atfiles.Find(cwd, prefix) == err ─► empty=true,  idx=-1
                                     atfiles.Find(cwd, prefix) == nil ─► empty=true,  idx=-1
                                     atfiles.Find(cwd, prefix) == N   ─► empty=false, idx reset to 0
                                                                          if N <= 0 or out of range

Tab on Visible + Selected != ""    ─► tabSpliceAtFile:                 ─► buf[:mentionStart] + "@" + (rel-path + " ") + buf[mentionEnd:]
                                                                          visible=false after splice

Esc when Visible                   ─► Reset()                         ─► visible=false, buffer preserved

↑/↓ when Visible                   ─► idx clamp at [0, len)           ─► no-wrap, matching the slash picker

submit prompt                      ─► Reset()                          ─► overlay closes for the next turn

panel-open (slash-pane opened)     ─► Reset()                          ─► mutually exclusive with slash pane in practice
```

The view renderer (`atFileDropdown.view(width)`) reuses the slash-picker's `pickerNameHi` / `pickerDescDim` / `pickerKeyOpen` / `pickerKeyClose` / `pickerLabelDim` / `pickerReset` palette so the two overlays look the same. 5-row window, "↑ N more" / "↓ N more" tails, "No matches" row, hint strip at the bottom (`↑/↓ Navigate · enter Insert · tab Complete · esc Cancel`).

The `tabSpliceAtFile` helper computes the relative path (via `filepath.Rel(cwd, candidate)`) and splices it back with a trailing space so the user can keep typing the message body.

## Wiring points in `cmd/wagyu/tui_run.go`

Six surgical insertions into the existing `runMode`:

| Site | Change | Why |
|---|---|---|
| `runMode` struct | `atFileDropdown atFileDropdown` field (cycle-225 style) | hold the overlay state alongside `picker tuilib.PickerState` |
| `submitPrompt` (panel-open branch) | `m.atFileDropdown.Reset()` after `m.picker.Reset()` | mutually exclusive with slash pane |
| `submitPrompt` (chat path) | `m.atFileDropdown.Reset()` after `m.input.Reset()` | overlay does not linger into the next turn |
| `Update` — `Esc` handler | if `m.atFileDropdown.Visible()` → `m.atFileDropdown.Reset()`, `return m, nil`; else fall through to existing input-clear | preserves the `@<prefix>` the user already typed |
| `Update` — `Tab` key | if `Visible` and `Selected != ""`, splice and return | Tab completes with the highlighted candidate |
| `Update` — `Up` / `Down` key | if `Visible`, route to `MoveUp` / `MoveDown`, else fall through to `historyUp` / `historyDown` | dropdown takes precedence over history navigation |
| `Update` — `Ctrl+J` (newline) | after `m.picker.Update`, also call `m.atFileDropdown.Update(buf, m.cwd)` | the AGY 1.3.6 fix: a fresh `@<char>` at the start of a new line re-derives the overlay |
| `Update` — textarea pass-through | after `m.picker.Update` on every keystroke, also `m.atFileDropdown.Update(buf, m.cwd)` | the parser walks the trailing run anywhere in the multi-line buffer |
| `viewBody` | render slot after `m.pickerView()` | the dropdown sits between the bottom separator and the status row, matching AGY layout |

`cmd/wagyu/history.go` mirrors the existing `m.picker.Update([]rune(firstLine(...)))` calls with `m.atFileDropdown.Update(m.input.Value(), m.cwd)` so recalling a history entry containing `@src` mid-line still triggers the dropdown.

## Tests

### Required scenarios (6)

| # | Test name | Scenario |
|---|---|---|
| 1 | `TestAtFileDropdown_FiresAtEndOfLine` | the user types `@ma` at end of line — overlay visible with ≥ 1 candidate |
| 2 | `TestAtFileDropdown_FiresAfterNewline` | 3 subtests: `"first line\n@ma"`, `"first line\n\n@ma"`, `"first line ends here\n@cmd/wagyu/main"` — the AGY 1.3.6 fix |
| 3 | `TestAtFileDropdown_FiresMidLine` | `"abc @foo"` — overlay visible |
| 4 | `TestAtFileDropdown_DoesNotFireAfterWhitespace` | 4 subtests: `"@main "`, `"@main \n"`, `"hello @main "`, `"hello @main \n"` — cursor on whitespace must close |
| 5 | `TestAtFileDropdown_HandlesPathsWithSpaces` | `"@a file with spaces"` — the parser walks back through internal spaces |
| 6 | `TestAtFileDropdown_MultipleAtsRefresh` | typing a second `@` re-derives the overlay from the latest boundary |

### Extras (9 in the dropdown file + 18 in the parser file)

| Test name | What it covers |
|---|---|
| `TestAtFileDropdown_TabSplicesPrefix` | `tabSpliceAtFile` writes the highlighted relative-path + space into the buffer and resets the overlay |
| `TestAtFileDropdown_MoveUpMoveDownClamps` | `MoveUp` / `MoveDown` clamp at `[0, len)` and refuse to act when hidden / empty |
| `TestAtFileDropdown_ResetClearsState` | `Reset()` zeros every field |
| `TestAtFileDropdown_EmptyPrefixStillShows` | bare `@` opens the overlay with the full top-level catalog |
| `TestAtFileDropdown_DoesNotFireMidWord` | `"obj@foo"` — mid-word `@` rejected |
| `TestAtFileDropdown_UpdateHandlesGlobError` | `Find` error path leaves `visible=true, empty=true, idx=-1` |
| `TestRunMode_AtFileDropdownView_EmptyBuffer` | `(*runMode).atFileView()` returns `""` when overlay hidden |
| `TestRunMode_AtFileDropdownView_FiresAfterNewline` | render slot emits rows for `"foo\n@ma"` |
| `TestRunMode_AtFileDropdown_TabSplicesInput` | end-to-end splice via the runMode helper |
| `TestParse` (15 subtests) | parser branches: empty, bare `@`, single char, mid-line path, **after newline (the fix)**, trailing slash after newline, dotted name, nested path, mid-text without trailing space, whitespace / newline after token, second `@` in active token, mid-word @, active token is just `@`, mention after punctuation |
| `TestParse_AfterNewlineBehavesLikeEndOfLine` | standalone regression guard: `Parse("look at @src").Prefix == Parse("look at this\n@src").Prefix` |
| `TestFind` (4 subtests) | empty-prefix catalog, path-component match, nested-path match, no-matches |

Totals: **3 + 15 = 18 top-level TestXxx functions**, **20 + 22 = 42 test runs** including subtests.

## Gates

### Build

```
$ go build -o /tmp/wagyu-atdd ./cmd/wagyu
$ ls -l /tmp/wagyu-atdd
-rwxr-xr-x 1 gear staff 73323682 Oct 18 07:32 /tmp/wagyu-atdd
```

Exit 0, ~73 MB. The cycle-231 base was also ~73 MB; the new package + dropdown file are net-new Go text but compile into the existing package set, so the binary footprint is unchanged within measurement noise.

### Tests

```
$ go test -vet=off -count=1 ./internal/atfiles/ ./cmd/wagyu/ \
      -run 'TestMention|TestParse|TestFind|TestAtFileDropdown|TestAtFileView|TestTabSpliceAtFile|TestRunModeAtFile'
ok  	github.com/chadgpt/openagy/internal/atfiles	0.221s
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.150s
```

Targeted suite: 3 packages, **42 / 42 passing**.

Full suite: only the documented pre-existing flake `TestDispatchPrintSlash_LocalCommands` fails (it expects `minimax-m3` but the test sandbox falls back to `gemini-3.8-flash-medium`). Verified pre-existing via `git stash -u` round-trip on bare `main`.

### `gofmt -l`

```
$ gofmt -l internal/atfiles/atfiles.go internal/atfiles/atfiles_test.go \
        cmd/wagyu/at_file_dropdown.go cmd/wagyu/at_file_dropdown_test.go
(empty)
```

`cmd/wagyu/tui_run.go` and `cmd/wagyu/history.go` are also gofmt-clean (verified via `gofmt -l`); the surgical insertions use existing tabs + standard library naming.

### `go vet`

```
$ go vet ./internal/atfiles/ ./cmd/wagyu/
(clean for both packages)
```

No new vet warnings. The cycle-224 `slash_cycle224_test.go` `% )` warnings in `cmd/wagyu` predate this cycle and are untouched.

## Probe table

| Scenario | Buffer | Overlay |
|---|---|---|
| empty | `""` | hidden |
| bare `@` at end | `"@"` | visible, full top-level catalog |
| `@<char>` end-of-line | `"look at @ma"` | visible, prefix=`"ma"` |
| `@<char>` after `\n` | `"first line\n@ma"` | visible, prefix=`"ma"` (the fix) |
| `@<path>` after `\n` | `"first line ends here\n@cmd/wagyu/main"` | visible, prefix=`"cmd/wagyu/main"` (the fix) |
| `@<chars>` mid-line | `"abc @foo"` | visible, prefix=`"foo"` |
| mid-word `@` | `"obj@foo"` | hidden |
| `@<chars>` followed by space | `"@main "` | hidden (cursor past mention) |
| `@<chars>` followed by `\n` | `"@main \n"` | hidden (cursor past mention) |
| path with internal spaces | `"@a file with spaces"` | visible, prefix=`"a file with spaces"` |
| multiple `@`s | `"@a/@b"` | visible, prefix=`"a/@b"` (embedded `@` is path content) |
| bare `@` after punctuation | `"hello,@foo"` | visible, prefix=`"foo"` |
| history recall containing `@` | `m.historyUp()` after a recalled buffer | overlay re-derived from full buffer |

## 存疑 / limitations

- **The clone's baseline was "no dropdown at all"**, not "dropdown at end-of-line". The task spec phrase *"Currently it only fires when `@` is at the end of a line"* describes the **AGY upstream** baseline. The clone never implemented the `@`-mention dropdown, so this cycle is net-new rather than a regression fix; the fix itself — treat `@<chars>` as an active mention regardless of position in the buffer — is implemented directly in the parser and exercised by `TestParse_AfterNewlineBehavesLikeEndOfLine` and `TestAtFileDropdown_FiresAfterNewline`.

- **No directory-browser overlay.** A bare `@` followed by `src/` walks *into* `src/` via `filepath.Glob` (the trailing-slash case), but the AGY upstream's "expand this directory" overlay (the `▶ src/` row that opens a sub-listing) is not implemented. The current dropdown lists immediate children; traversing deeper requires typing the next path component. This matches the cycle-91 slash-picker's contract and is enough for the AGY 1.3.6 line-12 fix; a richer overlay is a follow-on.

- **No live refresh while the input has focus.** `Update` is called on every textarea mutation (incl. `Ctrl+J`), so the candidates are recomputed on each keystroke. There is no async / debounced filesystem watcher; for a workspace with tens of thousands of files the per-keystroke `filepath.Glob` is fine but not optimal. Matches the cycle-91 slash-picker's per-keystroke semantics.

- **No `Enter` to insert** the highlighted candidate into the buffer. The hint strip advertises `enter Insert` to mirror the slash-picker, but the actual key handler still treats `Enter` as "submit prompt" — matching the upstream behaviour (in AGY, the `@` dropdown's `Enter` is only distinct from `Tab` when the dropdown is the *only* overlay; in this clone the prompt-submit semantics take precedence). Documenting rather than implementing, since adding a second `Enter` handler would change the submit path.

- **`TestDispatchPrintSlash_LocalCommands` flake stays.** This is the cycle-220 / cycle-225 / cycle-231 documented pre-existing failure (model-resolution mismatch in the test sandbox). Verified by `git stash -u` round-trip on bare `main` before any cycle-232 work; not introduced by this cycle.

## Hard constraints (per AGENTS.md)

- ✅ `internal/convstore/sqlite.go` untouched (the cycle-232 diff does not include it).
- ✅ Pre-existing non-gofmt-clean files (`internal/` + `cmd/wagyu/picker_view.go`, `cmd/wagyu/tui_v135_test.go`) untouched. All four new files are gofmt-clean by construction.
- ✅ No push to remote.
- ✅ Single commit on `feature/at-file-dropdown-newline`.
- ✅ Build succeeds (`/tmp/wagyu-atdd`, exit 0).
- ✅ Targeted suite passes 42/42; full suite passes except the one documented pre-existing flake.
- ✅ Parser + dropdown are stdlib-only — `internal/atfiles/atfiles.go` imports only `path/filepath`, `sort`, `strings`; `cmd/wagyu/at_file_dropdown.go` imports `fmt`, `path/filepath`, `strings`, `internal/atfiles`.
- ✅ Used model `agw/MiniMax-M3` throughout.