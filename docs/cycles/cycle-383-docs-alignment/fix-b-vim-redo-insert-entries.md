# Fix B-13 / B-14 — the vim engine's redo key and its NORMAL-mode insert entries

Branch `fix/b1314`. It closes **B-13** (`U` is not bound in NORMAL mode)
and **B-14** (`/` and `!` in NORMAL mode do not enter INSERT) from
[`slice-b-tui-surfaces.md`](slice-b-tui-surfaces.md).

## The finding was already implemented in the base tree

Both fixes landed with `a2229f2f` ("fix(vim): Ctrl+R opens artifact
review, U redoes, r{char} replaces and / ! enter INSERT"), which is an
ancestor of this branch's base. The commit that removed the count
plumbing afterwards (`b8d95c02`, **B-11**) kept both: the parsed command
has no count field any more, and the two cases survive it.

So this branch changes **no behaviour**. What it does is verify the two
findings against the documentation, pin the parts that had no pin, and
record the two questions the documentation leaves open.

## What the documentation says

【一手】 `official-docs/cli/vim-editor-mode.md`:

- line 130 — "`u` `U` | Undo, redo. `Ctrl+R` does not redo; it opens
  artifact review"
- line 192 — "In NORMAL mode, `u` and `U` are undo and redo."
- line 196 — "Press `/` or `!` in NORMAL mode. The CLI inserts the
  character and switches to INSERT mode, so slash commands and shell
  commands work without pressing `i` first."

The note at line 192 sits under the **selection** table, so the VISUAL
meanings of `u` and `U` (lowercase / uppercase) are claimed first and
the NORMAL meanings are the fallback.

## What the engine does

| | where | what |
|---|---|---|
| `U` → redo | `vim/parse.go` (`case 'U'` in `parseActionCommand`) | `cmdRedo` → `executeEdit` → `s.redoOp()` |
| `u` → undo | `vim/parse.go` (`case 'u'`) | `cmdUndo` → `executeEdit` → `s.undoOp()` |
| `/`, `!` → insert entry | `vim/parse.go` (`case '/', '!'`) | `cmdInsertEntry` with the rune |
| the insert entry | `vim/execute.go` (`insertEntry`) | `case '/', '!': s.insertRune(r)`, then `s.Mode = Insert` |

The **insert entry is reused, not re-implemented**: `/` and `!` go
through the same `cmdInsertEntry` the `i`/`a`/`I`/`A`/`o`/`O` entries
use, and `insertEntry` hands the rune to `insertRune` — the insert
mode's own rune path. Three consequences fall out of the reuse and are
pinned below:

- the character lands **at the cursor**, before the character under it,
  and the cursor lands after it;
- the character joins the **insert run**, so `/settings` is one undo
  step, not eight keystrokes;
- the VISUAL guard still applies — a VISUAL selection is not dropped,
  the engine bells instead (`vim/parse.go`, the `rest[0] == '/' ||
  rest[0] == '!'` branch in `parse`).

The **redo is a separate stack** (`vim/operator.go`): `undoOp` pushes the
step it undid onto `s.redo`, `redoOp` pops it, and `recordUndo` (every
new edit) empties the stack. The steps come back most-recent-first.

## The host's half

The host adapter is **`internal/cliapp/vim_mode.go`** (`vimEngine`,
`handleVimKey`, `syncVimInput`) — the file was **not** changed, because
the behaviour it drives is already correct.

Two host call sites had to move with the engine, and both did (in
`a2229f2f`):

- `internal/cliapp/tui_update_keys.go` — the `!` no longer arms the
  bash mode when `m.vimEnabled()`; the engine owns the character.
- `internal/cliapp/tui_prompt_submit.go` — `submitVimBashMode` runs a
  submitted vim prompt that begins with `!` as a shell command, with the
  `!` stripped.

Outside vim mode nothing changes: the `!` still arms the bash mode
(`TestVimBangIsNotShellOutsideVimMode`).

## The pins this branch adds

`vim/official_docs_test.go` — the doc-contract file, which already held
the B-13/B-14 pins:

- `TestNormalInsertEntriesUnchanged` — `i`, `a`, `I`, `A`, `o`, `O`
  still enter INSERT where they did. The two new insert entries did not
  move the older ones.
- `TestInvalidKeysStillBellInNormalMode` — `?`, `%` and `#` are still
  invalid keys: consumed, with a bell, the buffer untouched, no
  half-typed sequence left behind. The parser is not more permissive
  than it was.
- `TestRedoIsASeparateStack` — the redo's arithmetic: the steps come
  back most-recent-first, a new edit empties the stack, and a `U` with
  an empty stack is consumed and rings no bell.
- `TestSlashInsertEntryJoinsTheInsertRun` — `/settings` is one undo
  step, and `U` brings the whole run back.
- `TestSlashInsertEntryInsertsAtTheCursor` — the character goes in at
  the cursor and the cursor lands after it.

`internal/cliapp/vim_redo_test.go` (new) — the host's half:

- `TestVimEngineStartsInNormalMode` — with vim on and no "Insert First",
  the engine opens in NORMAL mode. This is the precondition the `/` and
  `!` pins in `internal/cliapp/vim_bash_test.go` rely on and never
  asserted.
- `TestVimUndoRedoRoundTrip` — the whole path: `u` empties the
  composer, `U` brings the text back. No host binding steals either
  key.

## The pins were mutation-checked

A pin that cannot fail is not a pin. Each of the four behaviours above
was broken on purpose, and the pin was expected to notice:

| the mutation | the pin that noticed |
|---|---|
| `parseActionCommand` loses `case 'U'` and `case '/', '!'` | `TestUppercaseRedo`, `TestSlashAndBangEnterInsertMode`, `TestRedoIsASeparateStack`, `TestSlashInsertEntryJoinsTheInsertRun`, `TestSlashInsertEntryInsertsAtTheCursor` |
| `recordUndo` stops emptying the redo stack | `TestRedoIsASeparateStack` |
| `redoOp` pops the **oldest** step instead of the newest | `TestRedoIsASeparateStack` |
| `insertRune` stops coalescing the insert run | `TestSlashInsertEntryJoinsTheInsertRun` |

The first mutation was found by the check itself: the redo stack's
"a new edit empties it" assertion was written against a second edit
that produced the *same* text, so it could not tell the two behaviours
apart. The second edit now changes the text, and the pin fails when the
stack is not emptied.

## 硬约束

- **The `vim` package is a public compatibility surface** (invariant
  95). This branch adds **no exported name** and removes none; only
  test files and documentation changed.
- **VISUAL claims `U` first.** `parseVisual` takes `U` for uppercase
  before the action command is reached, so `U` redoes **only** in NORMAL
  mode. The same guard applies to `/` and `!`.
- **The engine's redo is a separate stack, and `U` with an empty stack
  is not an error** — it is consumed and rings no bell.
- **The insert entry joins the insert run.** `/settings` is one undo
  step, because `/` reuses the insert mode's rune path.

## 存疑

- **存疑**: the documentation does not say whether the reference's redo
  is a **separate stack** or an **undo-of-undo**, and it does not say
  what a redo does after a new edit. The clone implements a separate
  stack that a new edit empties — the ordinary editor behaviour — and
  that is 【推测】, not 【一手】. The pin records the clone's reading so a
  later change cannot move it silently.
- **存疑**: the documentation does not say **where** the inserted
  character goes. The clone puts it at the cursor, before the character
  under it — the same place `i` would put it — and the cursor lands
  after it. Also 【推测】.
- **存疑**: the slash command's **completion popup** does not open in
  vim's NORMAL mode. The `/` reaches the composer and the command
  dispatches when the prompt is submitted, so `/settings` opens the
  settings panel — but the popup the non-vim composer shows while the
  command is being typed is not opened, because the vim path consumes
  the key before the host's editor path (`internal/cliapp/tui_update_keys.go`)
  can update the picker. The documentation describes only the insertion
  and the mode switch, so the popup is outside the documented surface;
  it is recorded here rather than fixed.

## limitations

- No binary was run. Every statement above is either 【一手】 (read off
  `official-docs/cli/vim-editor-mode.md`) or read off the clone's own
  source and its pins.
- The other open findings of slice B are **not** touched: **B-15**
  (the three remappable vim actions), **B-16** (vim mode covers only
  the prompt box), **B-17** (the keybindings path contradiction),
  **B-21** (`?` overlay) and **B-22** (`esc esc`).
- The report was written against the tree at `aee9eadb`.
