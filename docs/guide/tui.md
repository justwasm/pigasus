# TUI surfaces

> The transcript display, the model picker, argument completion and bash mode.

## The `/model` picker (cycle `model-picker-search`)

`internal/cliapp/slash_panel_model.go` draws the picker; `internal/cliapp/slash_panel_keymap.go`
routes its keys. The reference's picker is a **search field first** — every
printable character lands in `Search:` and the rows filter live (captured
live against agy 1.4.5, `docs/cycles/model-picker-search/captures.md`):

- the match is a **case-insensitive subsequence of the display label** —
  not a substring (`clauson` matches `Claude Sonnet 4.6 (Thinking)`) and
  not a match on the catalogue id (`cl-opus` matches nothing);
- a printable key **filters**; it does not navigate (`j` goes into the
  box). `handleModelKey` therefore does **not** use
  `tuilib.ClassifyListNavKey`, which reads `j`/`k`/`h`/`l`/`g`/`q` as
  navigation;
- the rows come back in a **relevance order**: an unselectable (dim) row
  last, then the rows whose match lands on a word boundary, then the
  shorter label, then the earlier first match. The reference's own score
  function was not recovered — this rule reproduces every measured query;
- no match renders `  No models match "<query>"` (256-colour 60) and the
  legend collapses to `esc Clear search`;
- Esc clears a live query and keeps the panel open; the legend's last
  entry reads `esc Clear search` while a query is live and `esc Go Back`
  when the box is empty.

Two things that are easy to get wrong:

- **The row count is not a constant.** `panelRowCount(panelModel)` is
  `len(m.modelRows())` — the reference's five static rows, plus the
  conditional Claude 5.5 rows, plus the configured models, filtered by the
  query. The hard-coded `6` it replaced made every row past the sixth
  unreachable (the configured `MiniMax M3.1 Flash` could not be selected).
- **Every cursor-indexed helper goes through `m.modelRows()`** — the
  renderer, the effort slider (`modelRowHasEffortSlider`), the selection
  and the effort cycling. The cursor always points at a row that is on
  screen.

Report: `docs/cycles/model-picker-search/`.

---


## The `/model <name>` completion popup (cycle `model-argument-completion`)

The **argument** of `/model` completes in the composer. This is a *second
mode of the command popup*, not the Switch Model panel — do not confuse the
two (captured live against agy 1.4.6,
`docs/cycles/model-argument-completion/captures.md`):

| file | what it is |
|---|---|
| `internal/tuilib/picker.go` | `PickerKindModel`, `ModelRow`, `SetModelCatalogueSource`, `modelQuery`, `filterModels` |
| `internal/cliapp/picker_model.go` | the catalogue source, `resolveModelArgument`, `invalidModelMessage`, `applyModelArgument`, `submitModelCompletion` |
| `internal/cliapp/picker_view.go` | `renderModelCompletionRow`, the model branch in the row loop, `commitPickerSelectionToBuffer` |
| `internal/tuilib/composer_token.go` | `styleComposerCommandToken`, `composerArgumentHint`, `appendComposerHint` |

Six things that are easy to get wrong:

- **The trigger is `/model` + a space.** A bare `/model` is still a
  command prefix, and `/model` + Enter still opens the Switch Model panel.
  `modelQuery` is what decides, and it requires the space.
- **The filter is a substring of the label *or* of the id** — *not* the
  panel's subsequence matcher (`modelRowsFiltered`). `son` matches Sonnet
  and not Opus; `cl-opus` matches nothing.
- **The rows are `marker + label` padded to 30 columns, then two spaces,
  then the dim catalogue id** — the id starts at column 32 for every row,
  and the highlighted row colours the 30-column field *only*.
- **Enter has three outcomes**: the argument names a model (label or id,
  case-insensitively, in full) → the command runs (`Model set to …`); it
  does not but a row is highlighted → the row's label is completed into the
  composer; it does not and nothing matches → `Invalid model "x"
  (available: …)`. **Tab always completes the highlighted label** and
  never runs the command.
- **The composer colours the command token only when it is an exact
  command name** (`/model` blue, `/codesear` plain), and shows the `[model]`
  argument hint for an *empty* `/model ` argument. The hint is inserted at
  the *text's* column, not at the end of the padded line — the textarea
  pads the line, so `appendComposerHint` walks the visible columns
  (`visibleColumnOffset`).
- **The model popup drops the `esc to cancel` hint** the command popup
  carries (`statusLeftSegment`).

The catalogue is the CLI's own (`cli.ModelCatalogue`: the reference's rows
first, then the configured ones), re-read on every keystroke. So the
`↓ N more` tail and the `available:` list follow the clone's catalogue, not
the reference's.

Report: `docs/cycles/model-argument-completion/`.

---


## The transcript display (cycle 285)

`internal/cliapp/tui_tools.go` renders what the model *did*, and 1.4.3's display differs
from the tool result in three ways that cost a cycle to measure. Evidence is
byte-level pty captures (`docs/cycles/cycle-285-tool-display/`), not the
`tmux capture-pane` string table:

- **Two forms per call.** Collapsed is a summary (`● Ran (cmd) (ctrl+o to expand)`,
  `● Explored 1 file (base)`, ` failed` appended on a non-zero exit, red bullet);
  expanded is the call (`● Bash(cmd)` — **no space** before the paren, `● Read(path)`)
  with the hint on the *last detail row*. Expanded bullets stay green even on
  failure; red is collapsed-only.
- **The detail body is not the result body.** `run_command` shows
  `Command exited with code N.` + blank + `Command output:` (or
  `No output from the command.`), never `Created At`/`Completed At`; `view_file`
  shows `Read N lines`, not the file.
- **Blocks are interleaved, one blank row around each.** Prose that precedes a
  call stays above its block; sequential calls separated by text are separate
  blocks; only calls batched in one assistant message share a block, and that
  block prints **one summary row per category** (`● Explored 1 file (a)` then
  `● Edited 1 file (a)`), keeping those rows above the per-call heads when
  expanded. `finaliseLiveTurn` re-renders each batch in place
  (`toolStep.batch`), and the spinner label is `Generating` → `Working` →
  `Running command`/`Reading file`, painted `#657B83` with a five-character
  `#268BD2` shimmer (`spinnerText`).
- **File writes and edits.** Collapsed is `● Edited 1 file (base)` for both a
  create and an edit (a create is *not* "Wrote"); expanded is
  `● Create(path)` / `● Edit(path)`. A create's detail is `Wrote N line(s)`; an
  edit's is a unified hunk (`+A / -B lines`, 3 context lines, `#EEE8D5` stripe
  on changed rows, `#DC322F` / `#859900` text). A **one-line read prints no
  detail row** (the hint lands on the head); two or more print `Read N lines`.
  The detail chevron `⎿` is `#586E75`, not the warning blue.
- **The in-flight row is the summary with a present-participle verb** and the
  amber bullet: `● Running (cmd)`, `● Exploring 1 search (q)`,
  `● Exploring 1 file (x)`. Searches collapse to `● Explored 1 search (q)` and
  expand to `● WebSearch(q)`; the spinner says `Searching web`. A failed
  non-command call reddens the bullet but prints no ` failed` — that word is
  command-only.


## Bash mode (cycle `bash-mode`)

`!` at the start of an **empty** composer runs the buffer through the shell instead
of sending it to the model. `internal/cliapp/tui_bash_mode.go`; the composer prompt becomes
a bold-blue `!`, the hint row becomes ` activated bash mode · esc to cancel`
(leading space, `#657B83`), and Enter prints a local block:

```
● ! pwd
  ⎿  /Users/gear/GitHub/openagy
```

Facts that are easy to get wrong, all 【一手】 from pty captures
(`docs/cycles/bash-mode/`):

- The mode is a **composer state**, not a prefix in the buffer: the `!` key is
  consumed when the buffer is empty; ` !echo hi` (leading space) is an ordinary
  prompt and *does* go to the model.
- The shell is the same `tools.DefaultShellArgv` the `run_command` tool uses
  (`zsh -c` on darwin), cwd = workspace; stdout+stderr merged, trailing newlines
  dropped.
- The `⎿` block only exists when there is output (`!true` prints the head row
  alone). A failed command **with** output prints just the output; the Go error text
  (`exit status 3`) appears only when a failed command printed nothing.
- Success is a green `●` with a `#586E75` body, failure a red `●` with `#DC322F`;
  the `  ⎿  ` prefix is bold blue in both, and only the first body row is indented.
- It behaves like a local turn: spinner `Generating...`, status `esc to cancel`, and
  Esc cancels it — leaving the head row, no body, and a **green** bullet (`agy` 1.4.5
  paints that interrupt bullet amber `#B58900`; the clone pins 1.4.4's green).
- A command submitted **while another shell command runs preempts it** (cycle
  `bash-mode-preempt`): the old process tree is killed, the output it had already
  produced becomes its body, its head keeps the hollow amber `○`, and the new command
  starts immediately. Only a command submitted during a **model** turn queues as
  `▸ <cmd>` (no `(alt+m …)` suffix). Up-arrow recalls a command into bash mode.
- Cancelling kills the **process group**, not just the shell: `sh -c 'sleep 30'` leaves
  a grandchild holding the stdout pipe, and a shell-only kill makes `exec.Cmd.Wait`
  (and the spinner) outlive the cancel by the orphan's lifetime. Ctrl+C also cancels
  and still arms the two-step exit confirmation on the same press.
- **Long output truncates to half the terminal height.** The collapsed block is
  `  ⎿  <output +N lines>` + the last `height/2` rows (`20` at a 40-row pty, `25` at 50,
  `30` at 60) with ` (ctrl+o to expand)` on the final row; a body of `height/2` rows or
  fewer never truncates. ctrl+o re-renders the block from its full body and flips the
  hint to ` (ctrl+o to collapse)` — bash blocks are recorded in `m.blocks` with a
  `bash *bashBlock`, so they ride the same re-render path as tool blocks.
- The command never enters `m.session`, so it is invisible to the model; it is also
  not persisted to the conversation store (the reference's "implicit trajectory"),
  which is the main documented gap.

