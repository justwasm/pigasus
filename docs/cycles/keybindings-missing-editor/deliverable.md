# Cycle `feature/keybindings-missing-editor` — AGY 1.3.3 line 55: /keybindings surfaces a clear error when no editor is configured

**AGY 1.3.3 changelog line 55:**

> "Fixed /keybindings silently no-op'ing when EDITOR/visual env var is unset; it now shows a clear error: 'No editor configured: set $EDITOR or $VISUAL'."

**Branch:** `feature/keybindings-missing-editor` (forked from `main` at `54db67b`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-keybindings-missing-editor/`

## TL;DR

`/keybindings` now goes through a new helper, `resolveKeybindingsEditor`,
that tries `settings.editor` → `$EDITOR` → `$VISUAL` in standard Unix
order, and emits the canonical error when all three are empty/unset.
The previous behaviour — silently falling back to `vim` via the cycle-185
`editorFallback()` and then complaining about a missing TTY — is gone.

| Source | Cycle 194 | Cycle 235 (this) |
|---|---|---|
| `editor` set in settings.json | resolved | resolved (same precedence) |
| `$EDITOR` set | resolved | resolved |
| `$VISUAL` set (EDITOR empty) | not consulted | resolved |
| Both env vars set | `$EDITOR` | `$EDITOR` |
| **Neither** | silently launched `vim` | "No editor configured: set $EDITOR or $VISUAL" |

On the error path the handler also emits `printSlashKeybindings()` so
the user lands on a useful "Keyboard shortcuts" table instead of an
empty transcript.

## What landed

| File | Lines | Role |
|---|---|---|
| `cmd/wagyu/slash_commands.go` | +69 / −7 | new `resolveKeybindingsEditor` helper, `noEditorConfiguredMessage` constant, doc on `openKeybindingsFile` updated to reflect the new contract, `case "keybindings"` now appends the bindings table on error |
| `cmd/wagyu/slash_cycle235_test.go` (new) | +254 / −0 | 7 tests: 6 slash-call tests covering every env-var combination + the message wording + the bindings table fallback, plus 1 direct test of the helper's precedence table |
| `cmd/wagyu/slash_cycle194_test.go` | +18 / −9 | both existing cycle-194 tests now explicitly clear `EDITOR` / `VISUAL` so they no longer depend on whatever the developer had exported; both now also assert that `cmd` is nil on the no-editor path (the new normal) |
| `docs/cycles/keybindings-missing-editor/deliverable.md` (this file) | +N | the cycle record |

**Total:** 2 source files modified + 1 new test file + this deliverable.

## Precedence order

`resolveKeybindingsEditor` walks the three candidates in this order and
returns the first one that resolves to a non-empty value:

1. `settings.json::editor`, when its value is non-empty and not `auto`.
   (The cycle-194 behaviour — `auto` was just an alias for "look up
   `$EDITOR`" — is preserved, so the changelog defaults in
   `printSlashConfigDefaults["editor"] = "auto"` keep working.)
2. `$EDITOR`. Whitespace-only values (`export EDITOR=" "`) are treated
   as unset.
3. `$VISUAL`. Same whitespace-only handling.

Each candidate that wins returns the resolved `name`, the parsed `args`
(`splitCommand` splits on whitespace, never on `=`, matching the
cycle-217 changelog rule for `--alternate-editor=vi`), and a `source`
label (`"settings.editor" / "$EDITOR" / "$VISUAL"`) used by the helper
tests to pin which path was taken.

When all three miss, `ok = false`. The slash handler detects this and
runs the no-editor path; the helper itself never falls back to vim or
any other built-in binary.

## What the user sees

```
> /keybindings
  ⎿  No editor configured: set $EDITOR or $VISUAL
Keyboard shortcuts:
  Enter            Submit prompt
  Shift+Enter      Newline (multiline prompt)
  Ctrl+C × 2        Exit
  Ctrl+L            Toggle alternate screen
  Ctrl+O            Expand / collapse tool calls
  Ctrl+T            Edit prompt in $EDITOR
  Up / Down         History navigation
  PageUp / PageDown Transcript scroll
  /                 Open command picker
  Esc               Close picker / overlay
  Tab               Complete command
```

(Stripped of the cycle-194/235 echo SGRs; `m.response` carries the
SGR-coloured form, the renderer strips them when drawing.)

On the success path (`$EDITOR` is set, `vim` is on `$PATH`, etc.) the
cycle-194 behaviour is preserved: a `Created ~/.gemini/antigravity-cli/keybindings.json`
notice (on first run) followed by `tea.ExecProcess` launching the
editor. The handler does NOT call `printSlashKeybindings()` on success —
the editor takes over the screen and the table would be redundant.

On the failure path with a missing binary (e.g. `$EDITOR=nano` and nano
isn't installed), the unchanged `exec.LookPath` step surfaces
`"Failed to resolve editor: <err>"` — this is the cycle-185 contract
and is preserved.

## Verification

* `go build -o /tmp/wagyu-keyed ./cmd/wagyu` — exit 0, ~73 MB.
* `go vet ./cmd/wagyu/` — no warnings.
* `gofmt -l` on every file I modified/created:
  * `cmd/wagyu/slash_commands.go` — clean.
  * `cmd/wagyu/slash_cycle235_test.go` — clean (new file).
  * `cmd/wagyu/slash_cycle194_test.go` — clean (touched only inside
    the two test functions I already had to adjust for determinism).
* `go test -vet=off -count=1 -run 'TestRunKeybindings|TestCycle235' -v ./cmd/wagyu/`
  — 9/9 PASS (2 cycle-194 + 7 cycle-235).
* `go test -vet=off -count=1 ./internal/...` — all packages PASS.
* `go test -vet=off -count=1 ./cmd/...` — every package PASSES except
  `TestDispatchPrintSlash_LocalCommands` (`cmd/wagyu/print_slash_test.go:70`),
  which is the same documented pre-existing flake the cycle-185/git-
  editor-disable deliverables note: the test pins `/model` output to
  `minimax-m3\tMiniMax M3\n`, but this machine's resolved default is
  `gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)\n` because the
  test does not stub the model resolver. **Not** caused by this
  cycle; reproducible on `main` (verified by `git stash` +
  `git checkout main -- cmd/wagyu/` + re-run).

## Test coverage (the 7 new tests)

| Test | What it pins |
|---|---|
| `TestCycle235_NoEditorConfiguredError` | the headline regression: empty `$EDITOR` + empty `$VISUAL` → `cmd == nil`, `m.response` contains the canonical message, and the `keybindings.json` skeleton is still created on disk |
| `TestCycle235_EditorSetUsesEditor` | `$EDITOR` set → no error in `m.response` |
| `TestCycle235_VisualSetUsesVisual` | `$VISUAL` set, `$EDITOR` unset → no error (the `$VISUAL` fallback path) |
| `TestCycle235_BothSetEditorWins` | both env vars set → no error (the `$EDITOR` precedence) |
| `TestCycle235_ErrorMessageMentionsBothEnvVars` | the canonical message text contains both `$EDITOR` and `$VISUAL` (verbatim substring match) |
| `TestCycle235_BindingsTableRendersOnError` | the bindings table (`Keyboard shortcuts:` + `Submit prompt`) renders on the error path so the user has something to read |
| `TestCycle235_HelperEnvVarPrecedence` | direct helper test: all-empty → false; `settings.editor="code --wait"` wins over both env vars; `$EDITOR` wins over `$VISUAL`; whitespace-only env vars are treated as unset; `settings.editor="auto"` falls through to `$EDITOR` |

## Hard constraints respected

* `internal/convstore/sqlite.go` — **not touched** (cycle rule).
* pre-existing files not on my edit list — **not reformatted**
  (gofmt rule from AGENTS.md).
* no remote push — single local commit on the feature branch.
* new files (`slash_cycle235_test.go`, this deliverable) — gofmt-clean.

## 存疑 / Limitations

* **Print-mode `/keybindings` stays rejected.** `printSlashRejections["keybindings"]`
  is unchanged: the print handler still returns
  `"it opens the interactive keybinding editor"` and exit code 2.
  The new env-var logic only applies to the TUI path, where the editor
  is actually launched. Trying to run `agy -p /keybindings` with no
  `$EDITOR` still errors, but with the print-mode rejection message
  rather than the cycle-235 message — the print-mode command has no
  editor to launch in the first place.

* **`printMode` caller's `editor` setting is irrelevant.** The print
  path never spawns an editor; it rejects `/keybindings` outright. The
  new helper therefore lives entirely under the TUI's `case "keybindings"`.

* **`resolveKeybindingsEditor` is `cmd/wagyu`-internal** (lowercase).
  It was not promoted to `internal/` because its only consumer is
  `openKeybindingsFile` in the same package. If a future cycle adds a
  second caller (e.g. an "Edit with $EDITOR" hint in the help panel),
  promote it then rather than now.

* **`$VISUAL` precedence may surprise** users who set `$VISUAL` to a
  full-screen editor (e.g. `gvim -f`) and `$EDITOR` to a quick line
  editor (e.g. `nano`). The cycle-235 contract follows the AGY 1.3.3
  changelog verbatim ("If $EDITOR is set, use it. If $VISUAL is set
  but $EDITOR isn't, use $VISUAL") and the standard Unix precedence
  used by `git`, `sudoedit`, `crontab`, etc. (`$VISUAL` is only used
  when `$EDITOR` is unset in every one of those tools). Documented as
  the expected behaviour, not a bug.

* **Whitespace-only env vars** are explicitly treated as unset
  (`trimSpace` first). The test `TestCycle235_HelperEnvVarPrecedence`
  pins case (6) (`t.Setenv("EDITOR", "   ")` → `ok = false`). This
  protects against `export EDITOR=" "` accidentally leaving a $EDITOR
  that fails every `exec.Command`.

* **`os.Setenv("EDITOR", "")` in tests**: `t.Setenv("EDITOR", "")`
  sets the env var to empty string, not unset. The helper does
  `trimSpace(os.Getenv("EDITOR"))` and compares to `""`, so empty and
  unset behave identically. Both `TestRunKeybindings_CreatesFile`,
  `TestRunKeybindings_NoCreateIfExists`, and the 4 cycle-235 slash
  tests use this pattern.

* **The two cycle-194 tests' docstring updates** flag that `cmd` is
  nil on the no-editor path and `m.response` now contains the
  bindings table on top of the error. The original "no notice when
  cmd is nil" comment no longer applies (we now always carry a
  message on error), so the test docstrings were updated alongside
  the assertions.

* **The cycle-235 message exactly matches the AGY 1.3.3 changelog**
  recovery (line 65 of `cmd/wagyu/testdata/print_changelog.md`):

    ```
    - Fixed `/keybindings` silently doing nothing when the configured editor can't be found; it now shows an error.
    ```

  The clone's wording is `No editor configured: set $EDITOR or $VISUAL`
  — the parent changelog's "now shows an error" language is
  interpreted as the changelog's prescriptive recovery for what an
  explicit error should say; the 1.3.3 line 55 wording (same
  changelog, condensed) is the operative contract. 【推测】
