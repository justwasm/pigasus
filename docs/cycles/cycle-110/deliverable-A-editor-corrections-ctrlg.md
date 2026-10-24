# Cycle 110 — external-editor corrections and `ctrl+g` prompt editing

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** the changelog is the roadmap. Three entries about the external editor
contradict or extend cycle 108, so this cycle corrects cycle 108 and adds the
editor entry point the changelog names.

---

## 1. Correction: the editor fallback is `vim`, not `vi` 【一手】

Cycle 108 guessed `vi` for the case where the `editor` setting is `auto` and
`$EDITOR` is unset, and marked it 【推测】. The changelog settles it:

> Fixed **Windows** editor fallback to use "edit" or "notepad" when the editor
> setting is "auto" and no editor is configured, **instead of attempting to use
> "vim"**.

The fix is explicitly Windows-only, so `vim` is the Unix fallback. `editorFallback()`
now returns `vim` on Unix and, on Windows, the first of `edit` / `notepad` that is
on `PATH` (falling back to `notepad`).

The notice `No editor configured; using %s` is unchanged — only the `%s` moves
from `vi` to `vim`.

## 2. Correction confirmed: `$EDITOR` is not split on `=` 【一手】

> Fixed `$EDITOR` environment variable parsing: resolved issues where arguments
> containing `=` (e.g., `--alternate-editor=vi`) were incorrectly split, causing
> editor launch failures.

Cycle 108's `splitCommand` already used `strings.Fields`, which splits on
whitespace only, so `--alternate-editor=vi` already survived intact. This cycle
pins the rule with `TestSplitCommand` rather than changing code.

## 3. New: `ctrl+g` edits the prompt in `$EDITOR` 【一手】

> Fixed pressing `Ctrl+G` to edit the prompt in a full-screen terminal editor
> such as `vim` hanging with `Vim: Warning: Output is not to a terminal`;
> external editors now run against a real terminal stdout.

> Added `ctrl+g` on the artifact view to open $EDITOR.

`ctrl+g` is handled next to the existing `ctrl+o` toggle:

* `promptScratchFile(prompt)` writes the current input to a `agy-prompt-*.md`
  scratch file in the system temp directory;
* `editPromptCmd()` resolves the editor exactly like `/open` (same
  `resolveEditor`, same `Failed to resolve editor: %v` message), schedules it via
  `tea.ExecProcess` — which is what gives it "a real terminal stdout" — and
  returns an `editorFinishedMsg` carrying the scratch path;
* the `editorFinishedMsg` branch reads the file back into the prompt, removes the
  scratch file, and reports `Editor exited with error: %v` on failure.

`editorFinishedMsg` gained a `tempPath` field. It is empty for `/open` (which
edits a real file in place) and set for `ctrl+g`, so the two share one message
type without either reading a file it should not.

## 4. Files changed

| File | Change |
|---|---|
| `cmd/antigravity/slash_actions.go` | `editorFallback`, `promptScratchFile`, `editPromptCmd`; `resolveEditor` uses the fallback; `editorFinishedMsg.tempPath` |
| `cmd/antigravity/tui_v135.go` | `ctrl+g` dispatch; `editorFinishedMsg` reads the scratch file back |
| `cmd/antigravity/slash_actions_test.go` | `TestResolveEditor` updated to `vim`; `TestSplitCommand`, `TestEditPromptCmd`, `TestEditPromptCmd_Scheduled`, `TestEditPromptCmd_BadEditor` added |

## 5. Verification

* `go build ./cmd/antigravity`, `go vet` — clean; `gofmt` clean.
* `go test ./cmd/... ./internal/...` — all packages pass.
* `TestEditPromptCmd` exercises both halves of the round trip: the scratch file
  holds the prompt, and after `editorFinishedMsg` the prompt holds the edited
  text and the scratch file is gone. (`tea.ExecProcess` yields bubbletea's own
  internal message rather than the callback's, so the command itself is only
  asserted non-nil; the process plumbing is covered live below.)
* Live pty probe, `EDITOR` pointing at a script that rewrites the scratch file
  and touches a marker:

  ```
  > hello            (typed, then ctrl+g)
  > EDITED BY EDITOR
  ```
  the marker file is created and the prompt is replaced. **3/3 runs.**
* `/open` still drives the same editor plumbing (the marker file is created for
  `/open secret.txt` too), so the two entry points are consistent.
* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** (both documented cycle-101 diffs).

> Note for anyone re-running the probe: the first two `ctrl+g` attempts used a
> **stale `/tmp/agycmp/our/agy`** (cycle 109's build, which has no `ctrl+g`
> handler) and appeared to fail. Rebuild with
> `go build -o /tmp/agycmp/our/agy ./cmd/antigravity` before probing.

## 6. Divergences and limitations

* **The scratch file is `.md`** and holds the prompt verbatim. The reference's
  filename, extension and any surrounding template are unrecovered.
* **No `ctrl+g` on the artifact view.** The changelog's other `ctrl+g` entry
  belongs to the artifact viewer, which the clone does not have.
* **The prompt is trimmed of trailing newlines** on read-back
  (`strings.TrimRight(…, "\n")`); a prompt that deliberately ends in a newline
  loses it.
* **Windows `edit`/`notepad` selection is untested** — this is a macOS/arm64
  machine, so only the `vim` branch runs here.
* **`/keybindings` is still a stub.** It is the next cheapest editor user
  (`Set custom keybindings`, `No keybindings found.`, `keybindings.json`), but
  its file *contents* are unrecovered — the reference injects and persists its
  full default binding set (dozens of scopes; only `cli.escape`,
  `navigation.half_page_up` = `shift+up`, `navigation.half_page_down` =
  `shift+down`, `item.rename` = `f2`, `item.delete` = `f4` and
  `vim.insert.submit` are named in the changelog), so a clone-generated file
  would be a fabrication. Deferred until that can be resolved.
* **Still cycle-77 stubs (~8):** `/keybindings`, `/voice`, `/resume`, `/rewind`,
  `/fork`, `/feedback`, `/artifact`, `/btw`, `/context`, `/plugin`.
