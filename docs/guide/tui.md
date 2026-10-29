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


## The `/model` effort slider (cycle 362)

The picker's **effort slider** is the second half of the panel: while the
cursor sits on a row that offers more than one reasoning tier, the panel
draws a horizontal bar under the rows, and `←`/`→` move the marker.

The reference's own picker has exactly **one** such row — Gemini 3.8 Flash,
whose three tiers are `low`, `medium`, `high`. Every other reference row
is a fixed model, and the whole slider block (and the `←/→ Effort` legend
entry) disappears on it.

### One row per model, not one row per tier

A model the user configured declares its own tiers in
`providers.json`:

```json
{"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash",
 "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
 "reasoning_effort": "medium"}
```

The clone lists it as **one row**, with the tiers on the slider — the
reference's own shape. So:

- `modelpicker.Row` carries `Tiers []string` and `DefaultTier string`.
  `Rows` (the reference's own rows) gives Gemini its three tiers and
  `DefaultTier: "high"`; the other reference rows carry none.
- `modelpicker.ConfiguredRows()` returns **one row per model**, not one
  per tier. It reads `cli.ConfiguredModelEntries()`, the tier-aware view
  of the catalogue.
- `cli.ModelCatalogue()` still lists **one entry per tier** — that is the
  shape the id↔label lookup needs (`CatalogueEntry("minimax-m3.1-flash-xhigh")`
  → `MiniMax M3.1 Flash (Xhigh)`). The two views answer different
  questions, and both are kept.
- `Row.CatalogueID(cursor)` spells the tier into the id:
  `minimax-m3.1-flash` + `xhigh` → `minimax-m3.1-flash-xhigh`. A row with
  no tiers keeps its fixed id.

### The geometry

The field between the two arrows is **45 cells wide, whatever the tier
count** — measured on the reference's own capture, and a longer bar packs
more tiers into the same field. The tiers divide it into equal groups:

```
w   = 45 / N, rounded down to the nearest odd number   (group width)
pad = (w - 1) / 2                                        (margin per side)
marker i sits at column pad + i*w                        (inside the field)
```

For the reference's three tiers: `w = 15`, `pad = 7` — the markers sit at
columns 7, 22 and 37 of the field, and the reference's own bar and label
row are reproduced byte for byte (`internal/modelpicker/slider_test.go`).
For five tiers: `w = 9`, `pad = 4`.

The label row centres each tier word inside its own group, so the labels
follow the markers. The three-tier result is the reference's own:

```
  ◂        ◉──────────────○──────────────○        ▸
          low          medium          high      
```

### The colours

The first tier is green (`PanelEffLowOpen`), the last is red-orange
(`PanelEffHighOpen`) and everything between is orange
(`PanelEffMedOpen`). With the reference's three tiers that is exactly the
reference's own palette; with more tiers the middle colour repeats.

### The cursor is row-relative

`m.panel.effortCursor` is an index **into the cursor row's own tier
list** — a five-tier row and a three-tier row never share a number. The
helpers that keep that true:

| helper | what it does |
|---|---|
| `m.currentModelRow()` | the row the cursor is on |
| `m.modelEffortCursor(row)` | the slider index a row shows: the running model's own tier when the running model *is* that row, the row's default otherwise |
| `m.setModelCursor(idx)` | moves the cursor and re-seeds the slider for the row it lands on |

Every cursor move in `tui_keymap_model.go` goes through `setModelCursor`.
A row **without** a slider leaves the position alone — the slider is not
on screen there, and the reference keeps the position so that coming back
to the row restores it.

### Clone-side text

The reference's slider has three places, so it says neither `xhigh` nor
`max`. The blurbs for those two tiers are the **clone's own** words
(`modelpicker.EffortDescription`), written in the reference's shape (a
speed clause and a strength clause joined by an em dash):

- `xhigh` — *Near-maximum reasoning for very hard problems — slower, barely stronger*
- `max` — *Maximum reasoning depth — slowest, strongest results*

The three tiers the reference does publish carry the reference's own text,
byte for byte.

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


## Prompt-history paging and the composer overlays (cycle 295)

`↑` / `↓` page the prompt history, and the reference's history **contains
slash commands** — `~/.gemini/antigravity-cli/history.jsonl` stores them with
`"type":"slash_command"` (the clone writes the same shape,
`internal/convstore/write.go`). Paging up therefore lands on a buffer that
starts with `/`.

The rule that keeps that from breaking the paging: **the composer overlays are
armed by the *typed* text, not by the buffer content.** A history recall is not
typing. 【一手】 measured against agy 1.4.7 on 2026-10-25 (tmux 220×40):

| keystroke | the composer | the popup |
|---|---|---|
| `↑` | `/agents` (a recalled slash command) | **closed** |
| `↑` | the next-older entry — the history pages across slash commands | closed |
| `↓` | `/agents` again | closed |
| `↑`, then `x` | `/agentsx` | **open** — the `No matches` row |

The `@` file-mention dropdown obeys the same rule, and `Enter` on a recalled
slash command runs the command — a recalled command behaves exactly like a
typed one.

| file | what it does |
|---|---|
| `internal/cliapp/history.go` | `historyUp` / `historyDown` → `closeComposerOverlays()`: a recall **closes** the command popup and the `@` dropdown, it never re-derives them |
| `internal/cliapp/tui_update_keys_edit.go` | `handleKeyUp` / `handleKeyDown` consult the overlays **first** — an open popup owns the arrow keys, even when it is empty (`No matches`) |
| `internal/cliapp/tui_update_keys_edit.go` | `updateTextareaAndDropdown` re-derives both overlays on every *keystroke* — this is what arms them again after a recall |

Two traps:

- **Do not re-derive the overlays from a recalled buffer.** That was the bug:
  the popup became visible on the recall, `handleKeyUp` handed it the arrow
  keys, and the history stopped paging at the first slash command in the list.
- **Do not gate the command popup on a trailing space.** That is the
  `/model <query>` *argument* popup's trigger, a different widget; the command
  popup opens on a bare `/` (the reference's own capture: `/a` →
  `/add-dir` highlighted).

The clone's **test suite** used to write into the user's real agy home —
every `go test` run appended fixture strings to the very `history.jsonl` the
reference pages through (15 entries per run; 1723 of the 2000 entries in the
file were fixtures). `internal/cliapp/main_test.go` now points
`$ANTIGRAVITY_HOME` at a scratch tree for the whole package.

Report: `docs/cycles/cycle-295-history-recall-popup/`.

---

## The paste key and the image attachments (cycle 362)

`ctrl+v` and the terminal's own bracketed paste land in **one** place —
`internal/cliapp/tui_paste_media.go`'s `handlePasteContent` — and that place
decides what the paste *means*:

| the paste delivered | the clone does |
|---|---|
| a path to an image file | **attaches the image**; the path is *not* inserted into the composer |
| any other text | inserts it as text (unchanged) |
| nothing | **probes the platform clipboard** — the "Cmd+V with only an image copied" case |

A terminal cannot hand a file to a program, so a dragged image arrives as
its **path**; that is the whole of "dragging image files into the prompt,
which attaches them instead of inserting their paths".

The attachment waits in `runMode.pendingMedia`, is shown above the composer
(`[Attached: <name>]`), and rides the **next** user turn as
`backend.Message.Images` — the shape the transports already carry
(CloudCode's `inlineData`, the gateway's and Anthropic's content parts,
PiG's `ImageContent`). The conversation store keeps the trajectory steps,
so the attachment lives in memory only.

### The three routes into one place

| the route | the user does | what arrives |
|---|---|---|
| the paste key | `ctrl+v` | a key event |
| the terminal's own paste | `Cmd+V` / `Ctrl+Shift+V` | the terminal's **bracketed paste** — an image-only clipboard delivers *nothing*, so the paste is empty |
| a dragged file | drags an image into the prompt | the file's **path**, as text |

The second route is why the empty paste has to probe the clipboard: the
terminal has no way to hand over image bytes, so "only an image copied"
reaches the program as an empty paste. The third route is why a paste that
*is* text still has to be checked against the file system.

The reference has the same two *code* routes and no more — reverse
engineering the 1.4.9 binary shows `model.PromptModel.handlePasteMedia`
(the explicit paste) and `model.PromptModel.handleImplicitPasteMedia` (the
empty paste's probe, whose closure carries the reference's own log line
`Implicit clipboard media probe: result=%v err=%v`). The clone's single
`handlePasteContent` covers both.

### The transport leg — where the picture must survive

The attachment is **not** delivered by the TUI. It travels as data, and the
chain has three links:

```
runMode.pendingMedia
   └─ appendUserTurn ──────────► backend.Message.Images
                                    └─ internal/agent/pig_messages.go
                                         └─ pi.UserContentBlocks
                                              └─ the transport
```

`internal/agent/pig_messages.go`'s `userContent` is the **only** place where
`backend.Message.Images` becomes PiG content, and it is where the chain used
to break: a user turn built as `pi.UserText` silently loses every picture,
so the model answered the text and never saw the image. The rule now:

- a turn with **no** picture stays the union's plain-text arm — every byte
  the clone sent before it learned to attach images goes out unchanged;
- a turn that **carries** pictures becomes a block list: the text first,
  then one `pi.ImageContent` per picture, the payload base64-encoded
  because PiG's image block holds it as a string;
- a picture with **no bytes** is dropped rather than sent as an empty one —
  the model would answer a broken image with a guess.

The transports read the image blocks and nothing else
(`piagentbridge.userImages` for CloudCode / Gemini / Anthropic,
`piagentbridge.userContentParts` for the gateway's OpenAI-shaped array), so
this one function is the whole leg.

The **`--print`** path carries no picture: the headless mode builds its turn
with `pigrt.Runtime.RunWith` (a plain prompt string), not `RunWithContent`.
An attachment is an interactive-path feature.

The read half is `internal/clipboard`'s platform readers, and every
sentence they can show is the reference's own:

| when | the composer says |
|---|---|
| the clipboard holds no image | `No image or video found in the clipboard` |
| the clipboard helper is not installed | names the tool and its package (`wl-paste` / `wl-clipboard`) |
| the session is over SSH with no display | `Can't read your clipboard over SSH: connect with ssh -X (and install xclip on this host) to paste images` |

The macOS reader runs the reference's **two AppleScript sources verbatim** —
the file-URL one and the image one — and tries the class codes in the
reference's own order (`PNGf`, `JPEG`, `GIFf`, `TIFF`), converting what the
imagery pipeline cannot decode with `sips`. The Linux reader picks
`wl-paste` on Wayland and `xclip` on X11.

Three traps:

- **A paste never submits.** It attaches or inserts; the empty-paste probe
  must not touch the composer buffer.
- **A panel owns its own paste.** The `/rename` field pastes into its own
  input; the composer's paste path must not reach it.
- **An attachment must reach the transport.** Attaching is not delivering:
  a picture that is dropped on the way into PiG's transcript is invisible to
  the model, and the turn looks like it succeeded.

### The known gaps

- **No `alt+v`.** The reference has it — but as the **1.0.15** bullet
  ("an alternative paste shortcut on Windows to resolve issues where ctrl+v
  is intercepted by the terminal emulator"), not a 1.4.9 feature — and it
  collides with the Vim engine's `alt` chords, so the clone leaves it out.
- **One path per paste.** The reference splits **several** paths out of one
  paste (`model.splitPastedPaths`) and reads each of them; the clone's
  `clipboard.MediaFromPath` recognises one.
- **No `cli_clipboard_paste` action name, and no iTerm2 upload.** The
  reference registers an action name for the paste key and can ask iTerm2 to
  upload the picture itself (`model.isITerm2` → `model.startITermUpload`).

Report: `docs/cycles/cycle-362-agy-1.4.9-alignment/`.

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


## Syntax highlighting (cycles 233, 271, 300)

Code is highlighted with **chroma**, on two surfaces, and the version is
pinned to the reference's.

| Surface | Where | What it does |
|---|---|---|
| fenced code blocks in the transcript | `internal/mdrender/chroma_inline.go` | `quick.Highlight(…, "terminal256", "solarized-light")` |
| syntax-coloured diff hunks | `diffsyntax/syntax.go` | chroma token types → the TUI's diff token classes |

The **style** and the **formatter** are not taste: cycle 271 recovered them
from a live 1.4.2 pty capture — `package` renders `ESC[1m ESC[38;5;166m`,
`func` renders `ESC[38;5;100m`, punctuation falls back to the 242 base
colour. That triple is chroma's `solarized-light` through the **terminal256**
formatter, and no other embedded style matches it. The clone's
`ChromaStyleForScheme` maps the reference's colour-scheme names onto chroma
styles on top of that default.

The **version** is pinned too (cycle 300): the reference's binary embeds
chroma's own assets — 279 XML lexers under the embed FS root `embedded/` and
74 XML styles at its root — and all 353 files are **byte-identical to
upstream chroma v2.27.0**. The clone must stay on v2.27.0; v2.24.1 (what it
pinned before) cannot highlight Arturo, Gemfile.lock, LilyPond or Templ.
`internal/mdrender/chroma_reference_test.go` is the fence: every one of the
reference's 279 lexers must resolve, and the list must still have 279
entries. The bump changes no rendered byte — 30 common languages hash
identically under both versions — so a rendering golden cannot catch a
downgrade; only the fence can.

The reference also links chroma's **HTML** and **SVG** formatters (28 and 8
recovered functions). The clone has no surface that renders code to HTML or
to an SVG image, so they have no consumer here yet.


## The diff row (cycle 361)

A diff row is **not** the git body with a `+` in front of it. The reference
draws three columns — a gutter, a marker column and the text:

```
   3 -  gamma
   3 +  GAMMA
   3    delta
```

`internal/diffrow` owns the model; `internal/tuitools` renders the
transcript's inline diff preview with it and `internal/diffaction` renders the
`/diff` file view with it.

- The **gutter** is `fmt.Sprintf("%*d", numWidth, num)` — the number,
  right-aligned in `max(4, digits)` columns. Not a hardcoded `%4d`.
- The **marker column** is four wide: `" +  "`, `" -  "`, `"    "`. The
  added and removed bodies carry one space more than the unchanged body, so
  the text starts in the same column in all three.
- The **content width** is `max(1, width-numWidth-2)`. An added or a
  removed body is padded to it with spaces; an unchanged body is not. The
  padding is measured in runes against the **plain** text — an escape
  sequence is not a visible column — and that is what lets the row's
  background span the row.
- The **summary** is per change and has three forms: `Added N lines`,
  `Removed N lines`, and the `+N / -M lines` pair. A change shows **20
  rows** at most; the rest collapse into `… and N more lines`.
- The inline preview's block is indented by **five** columns, so its text
  lines up under the `⎿` bullet's text.

【一手】 the shape and the colours come from agy 1.4.3's captured diff
(`docs/cycles/cycle-285-tool-display/deliverable.md`); 【反编译】 the widths,
the summary forms and the cap come from agy 1.4.8's
`render.codeActionDetail`. The **palette** is not part of the model: the row
model assembles the escapes from `palette.*`'s current values, so the rows
follow the selected colour scheme.


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

---

## The rendering mode — alternate screen vs inline (cycle 294)

The session renders either in the terminal's **alternate screen buffer**
("no flickering (altscreen)") or **inline** ("native terminal (inline)"). The
choice is the reference's `altScreenMode` setting, and the reference's
vocabulary is narrow — 【一手】 measured against agy 1.4.7 on 2026-10-25 by
capturing the terminal bytes under a pty and counting `ESC[?1049h`:

| `altScreenMode` | the reference renders |
|---|---|
| `"always"` | the alternate screen (`ESC[?1049h`) |
| `"never"` | inline (no `ESC[?1049h`) |
| absent | the alternate screen — the default |
| anything else (`"off"`, `"inline"`, `"default"`, `"auto"`, `"none"`, `"false"`) | the default; the reference's settings validation rejects the value |

So only the word `"never"` means inline. Three places have to agree:

| file | what it does |
|---|---|
| `internal/cliapp/tui_entry.go` | `resolveAltScreen` — the clone's own `--alt-screen` / `--no-alt-screen` flags win, then the setting, then the default |
| `internal/cliapp/slash_config_preview.go` | `applyConfigValue` → `applyAltScreenLive` — the `/config` row switches the **running** session in the same keystroke |
| `internal/configpanel/configpanel.go` | the row's catalogue: the alternate screen first, then inline (`"never"`) |

Four things that are easy to get wrong:

- **The reference's own panel writes an *empty* value for the inline stop**, which
  its `json:"altScreenMode,omitempty"` writer then drops from `settings.json` — so
  the reference's inline option does not survive a restart, because an absent key
  is the reference's *default* and its default is the alternate screen. The clone
  writes the reference's *own* inline value, `"never"`, which agy 1.4.7 accepts and
  honours. 【一手】
- **The row's order is load-bearing.** An absent key means the alternate screen,
  so the alternate screen has to be the row's *first* entry: otherwise the row
  labels the default "native terminal (inline)" while the session renders the
  alternate screen, and both ← and → walk away from that state — which is
  exactly the "the panel says inline but the UI is still altscreen" report.
- **Ctrl+L does not toggle the mode.** The reference's Ctrl+L clears the screen —
  its string table has "Clear CLI screen" and `cli.clear_screen`, and the 1.4.7
  changelog says "when clearing the screen with `Ctrl+L`". The clone's
  `/keybindings` panel and the `--alt-screen` usage used to advertise a Ctrl+L
  toggle; both were wrong and now say what the reference does. 【一手】
- **bubbletea reads the flag off the view, not off the program.** There is no
  `tea.WithAltScreen` in bubbletea v2: `applyTerminalSettings` sets `v.AltScreen`
  on every frame, and the renderer enters or leaves the alternate buffer when the
  flag flips (`cursed_renderer.go`). Flipping `m.altScreen` is therefore enough to
  switch modes mid-session — but only if the frame that carries the new flag is
  actually rendered.

Report: `docs/cycles/cycle-294-altscreen-rendering-mode/`.


---

## The custom status line and the custom window title (cycle 301)

Both are **user scripts**: the CLI pipes a JSON document to the script's
stdin and shows (or titles) what the script prints. The clone's own
example scripts live in the upstream repository —
`examples/statusline/statusline.sh` and `examples/title/title.sh` — and
they read members the clone's old four-field payload never wrote.

| file | what it is |
|---|---|
| `internal/statusline/payload.go` | the document — the reference's `types.StatusLineData`, member for member |
| `internal/statusline/config.go` | the `statusLine` / `title` blocks in `settings.json` |
| `internal/statusline/script.go` | `sh -c "<command>"`, the payload on stdin, the 5 s timeout, the title's sanitising |
| `internal/statusline/state.go` | the render cache, the tick counter, the 30-failure budget |
| `internal/statusline/vcs.go` | the repository probe behind the payload's `vcs` member |
| `internal/cliapp/statusline_payload.go` | the payload builder, the cached block |
| `internal/cliapp/statusline_render.go` | the status area's layout, the error card |
| `internal/cliapp/slash_statusline_external.go` | the tick, the script runs |
| `internal/cliapp/windowtitle.go` | the synchronised window title |

Five things that are easy to get wrong:

- **The payload is 27 members in the reference's declaration order**, and
  the omit rules are not uniform: `model` is written as JSON **null** while
  it is unresolved, `exceeds_200k_tokens` and `context_window.current_usage`
  are written as **null** too, `product` is *absent* until the CLI knows
  it, and `version` is written as `""`. `payload_test.go` pins the order
  and the bytes against a payload agy 1.4.7 produced.
- **`vcs` carries the type only.** The reference has `branch` and `dirty`
  in its struct but 1.4.7 never filled them: 14 payloads from a clean and
  a dirty repository all read `"vcs": {"type": "git"}`. The upstream
  script's `// ""` fallback absorbs it.
- **`enabled` defaults to true.** A block without the member is *on* — the
  reference's documented default, and its observed behaviour. A boolean
  block is an object without a command, so no script runs.
- **`/statusline delete` removes the `statusLine` key** from
  `settings.json`; it does not leave an empty block behind. And
  `/statusline <command>` writes the block *without* an `enabled` member —
  which is why the script starts running immediately.
- **The title script's output is not ANSI.** The status line keeps its
  escape sequences (the upstream script uses them); the window title drops
  the control characters, joins the lines with single spaces and keeps the
  rest as literal text: `TITLE-A\nTITLE-B\t\e[31mCOLOR\a` becomes
  `TITLE-A TITLE-B[31mCOLOR`.

And one trap that is not about the reference at all:

- **The title is written on the tick goroutine and read on the render
  goroutine.** A Go string is two words, and the writer's store of the
  length can be observed before its store of the pointer — on arm64 the
  renderer saw an *empty* title and fell back to the program name. The
  title lives behind a mutex (`internal/cliapp/windowtitle.go`), and the
  payload is built on the model's own goroutine, because the session's
  quota cache is an unguarded map and a concurrent read during a write is
  a fatal error.

Report: `docs/cycles/cycle-301-statusline-title-scripts/`.

---

## The `/tasks` panel and its two detail views (cycle 315)

`/tasks` is a **list panel**; Enter on a row opens a **detail view**. There
are two of them, because the reference has two detail models: the background
task's (`model.TaskDetailModel`) and the agent (subagent) task's
(`model.SubagentDetailModel`).

| file | what it is |
|---|---|
| `internal/cliapp/slash_tasks_panel.go` | the list panel and its keys |
| `internal/cliapp/slash_tasks_detail.go` | the two detail views, their scroll and their keys |
| `internal/taskregistry/detail.go` | the two renderers and the reference's labels |
| `internal/taskregistry/format.go` | the reference's state vocabulary |

Five things that are easy to get wrong:

- **Enter opens the detail; `k` kills; `x` removes.** The reference's
  `/tasks` keybinding scope binds three separate actions (`KeyEnter`,
  `KeyTaskKill`, `KeyTaskRemove`). The clone used to kill on Enter, which
  is why the detail view never appeared.
- **The detail view is a sub-view, not a panel of its own.** Esc leaves it
  and returns to the list *without* the `Exited /tasks command` line; a
  second Esc leaves `/tasks` and prints it. The detail view is also modal —
  it owns every key, so `ctrl+g` and `ctrl+o` do not reach the editor while
  it is open.
- **The state word is the reference's.** `model.taskStatus()` returns
  `running` / `completed` / `failed` / `cancelled` — four words, no fifth
  (cycle 318) — and appends ` (exit N)` to the completed and failed forms
  only. One function (`taskregistry.TaskStatus`) serves both the row and
  the detail view.
- **The task detail's labels are a nine-cell column** — `Command: `,
  `Status:  ` and `Type:    ` are the reference's own strings, and the
  values start at the same column. The reference's `Type:` carries the
  task's *origin* (`Agent` / `User`); the clone renders the task's *kind*
  there, because every task it registers is model-spawned and the origin
  would be constant.
- **The subagent detail is built from data the registry has to carry.**
  `Task.Prompt`, `Task.Output` and `Task.Tools` are filled by
  `internal/subagenttools` when the subagent runs — the prompt up front,
  the answer and the tool names afterwards.

Report: `docs/cycles/cycle-315-tui-subagent-task-panels/`.

## The detail views' shapes (cycle 316)

Cycle 315 built the two detail views but drew them from the binary's
*string table*. The decompilation of the two render functions
(`model.TaskDetailModel.buildTaskDetailLines`,
`model.SubagentDetailModel.buildHeaderLines` and
`model.SubagentDetailModel.buildContentLines`) shows the real shapes, and
they are not the same.

| | the task detail | the subagent detail |
|---|---|---|
| **the heading** | `Task Details` (`RenderHeader`) | none — the first header line *is* the title |
| **the header** | — | five lines: `main ▸ <name>`, the elapsed time, the steps line, `ID: <id>`, a rule |
| **the rule** | a fixed **forty** cells | `min(60, width-4)` cells |
| **the body's indent** | flush left | two cells; the tool list's items four |

Five things that are easy to get wrong:

- **`ID: ` has no brackets.** The binary carries the literal `ID: [%s]`,
  but the reference's header uses only its first four bytes.
- **The elapsed line changes with the state.** `worked 12s` once the
  agent is done; the bare `12s` while it runs.
- **The subagent detail has its own state vocabulary** —
  `running` / `done` / `error` / `killed`, each with the reference's own
  ANSI-256 colour (196 / 34 / 220). `model.taskStatus()`'s words
  (`completed` / `failed` / `cancelled`) belong to the `/tasks` row and
  the *task* detail.
- **The header's depth is chrome.** The body window is
  `height - 6 - (headerRows - 1)`, so the subagent detail's five-row
  header leaves four rows fewer than the task detail's one.
- **The step count is the trajectory's.** The reference counts the
  subagent's trajectory steps — the same number its trajectory section
  shows (cycle 317).

Report: `docs/cycles/cycle-316-tui-detail-headers/`.

## The subagent detail's trajectory section (cycle 317)

The reference's subagent detail has a **fourth** section, after the
answer: the agent's *trajectory* — the steps it took.

```
  Trajectory — <name> (<N> steps)
  No steps yet                              ← only when there are none
    1. read_file
    2. run_command
```

| | what it is |
|---|---|
| **the heading** | `Trajectory — %s (%d steps)` — the reference's own 28-byte literal |
| **the empty form** | `No steps yet` |
| **the items** | numbered rows, four-cell indent, one per step |

Four things that are easy to get wrong:

- **The section is last.** The reference renders prompt → tools → answer
  → trajectory. The order was recovered from the *offsets* of the
  literals inside `buildContentLines`, not from the decompiler's
  statement order.
- **The count is shared with the header.** The header's `· %d steps` and
  the trajectory's `(%d steps)` are the same `max(model step count,
  walked counter)` in the reference, so the clone reads both from
  `Task.Steps`.
- **The steps come from the subagent's own messages.**
  `internal/subagenttools` fills `Task.Steps` when the run ends — one
  step per assistant content block: a tool call by its name, a text
  block by its first line.
- **The clone fills the list when the run ends.** The reference's
  trajectory grows *live*; a running subagent there already shows a
  partial trajectory, while the clone shows `No steps yet`.

Report: `docs/cycles/cycle-317-tui-subagent-trajectory/`.

## The `/tasks` row's status cell (cycle 318)

The row's status cell is the reference's own, and it carries the exit
code.

```
  ● [15:04:05] npm test                                 completed (exit 0)
```

| | the reference |
|---|---|
| **the words** | `running` / `completed` / `failed` / `cancelled` — four, no fifth |
| **the exit code** | ` (exit N)` appended to `completed` and `failed` only |
| **the cell** | the word and the suffix are one styled span |

Three things that are easy to get wrong:

- **A task that has not started is a *running* one.** `model.taskStatus()`
  has four leaves and no `waiting` — cycle 315's fifth word was a guess,
  and cycle 318 removed it.
- **The exit code belongs to the cell**, not to a separate column: the
  reference formats ` (exit %d)` and concatenates it onto the word
  *before* it styles the cell.
- **A cancelled task never shows an exit code** — and neither does a
  running one.

The row's *rest* — the `●` marker, the `[15:04:05]` timestamp, the
name's truncation and the right-aligned padding — was mapped in the
cycle's report but **not implemented** then: the six `%s` arguments of
the reference's `%s%s %s %s%s%s` row format could not be paired.
Cycle 319 paired them and shipped the row.

Report: `docs/cycles/cycle-318-tui-tasks-row-status/`.

## The `/tasks` row's shape (cycle 319)

The row is the reference's own, cell for cell.

```
> ● [15:04:05] echo done                                     completed
  ● [15:04:51] ddgr --num=5 cats                             running
```

| | the reference |
|---|---|
| **the format** | `%s%s %s %s%s%s` — prefix, marker, time cell, name, padding, status |
| **the prefix** | `> ` on the cursor's row, `  ` on the others — two cells, always |
| **the marker** | `●` — the same glyph for every state, coloured by the state |
| **the time cell** | `[15:04:05]`, or `[          ]` when the task has no time |
| **the name** | cut to `max(10, width − len(status) − 17)` cells, with `...` |
| **the padding** | `max(2, width − len(status) − width(name) − 15)` spaces |
| **the status cell** | the word, plus ` (exit N)` on the completed and failed forms |

Five things that are easy to get wrong:

- **The status cell is right-aligned, not padded.** The gap in front of
  it is the row's own padding cell, so the row comes out exactly as
  wide as the panel and the status ends on its right edge. The clone
  used to pad the *word* to nine cells.
- **The row carries no id, no elapsed time and no error reason.** The
  clone invented all three; the reference has none of them.
- **The ellipsis is three dots**, not `…` — and a name cut to fewer
  than four cells becomes a single `.`.
- **A name is flattened before it is measured**, so a name with a
  newline in it becomes one line.
- **The prefix says *selected*, not *tool call*.** The clone's `⎿`
  bullet (borrowed from the transcript's tool steps) is gone.

Report: `docs/cycles/cycle-319-tui-tasks-row-layout/`.

## The ActiveItems row — the block under the input box (cycle 320)

The row the active-items block draws is the reference's own shared task
row, and it is *not* the `/tasks` row.

```
  ● [15:04:05] Search Shanghai Weather running
> ● [15:04:05] Running tests in background running
```

| | the reference |
|---|---|
| **the renderer** | `render.RenderTaskItem` — the same one the reference uses wherever a task is listed |
| **the prefix** | `> ` on the focused cursor's row, `  ` on the others — two cells, always |
| **the marker** | `●` — the same glyph for every state, coloured by the state |
| **the time cell** | `[15:04:05] ` — the format carries the trailing space |
| **the name** | cut to `max(0, width − width(cell) − width(statusCell))` cells, with `...` |
| **the status cell** | a space and the state word, in the state's colour |
| **the overflow lines** | `  ↑ %d more` in front, `  ↓ %d more` behind |

Four things that are easy to get wrong:

- **The row carries no id, no elapsed time and no error reason.** The
  clone invented all three; the reference's record has no other string
  field.
- **The row has no padding cell.** The `/tasks` row pads the name so
  that the status lands on the panel's right edge; this row puts the
  status right behind the name, separated by one space.
- **The empty time cell is `[] `, not `[          ]`.** The two rows pad
  the time column differently.
- **Only the prefix reacts to the focus.** The clone used to bold the
  whole row; the reference styles the `"> "` and nothing else.

The row's body is laid out in the block's width minus the prefix's two
cells — the same two cells the prefix occupies.

Report: `docs/cycles/cycle-320-tui-active-items-row/`.

## The ActiveItems row for a subagent (cycle 321)

A subagent in the active-items block is **not** drawn as a task — the
dispatcher picks the renderer by the record's kind, and a subagent gets
the reference's own subagent row.

```
  ● Agent(Search Shanghai Weather)  Processing... · 1m35s
```

| | the reference |
|---|---|
| **the renderer** | `render.RenderSubagentItem` — its own row, not the task row |
| **the format** | `%s %s  ` — the marker, the tool info, two spaces |
| **the tool info** | `Agent(<name>)` while the subagent calls no tool, `<tool> › <name>` while it is inside one |
| **the status word** | `Done`, `Error: <message>`, `Killed`, `Processing...` — the subagent's own vocabulary, not the task's `running` / `completed` / `failed` |
| **the elapsed cell** | ` · 1m30s` while the subagent runs, ` · worked 1m30s` once it is done |
| **the marker** | `●` in the ANSI-256 palette — `220` while it runs, `34` once it is done, `196` when it failed or was killed |
| **the flexible cell** | the status word, cut to `max(0, width − width(cell) − width(elapsedCell))` cells |

Six things that are easy to get wrong:

- **The status word is cut, never the name.** A long name overflows the
  row; the reference does not shorten it.
- **The elapsed cell has two forms.** A running subagent shows the bare
  duration, a finished one the duration behind a `worked` — the same
  rule the subagent detail's second header line follows.
- **The status word is the subagent's own, not the task's.** The
  reference never says `running` here; it says `Processing...`.
- **The marker uses the ANSI-256 palette**, not the true-colour one the
  rest of the clone's palette carries.
- **The status word's own style is the theme's** — cycle 323 recovered
  the scheme table the theme comes from; the clone still paints the
  status word in the subagent's state colour, the colour its marker
  carries.
- **A blocked subagent gets `Blocked · ` in front of the status word**,
  painted in the orange `214`. The clone has no approval flow yet, so
  the prefix never appears.

Report: `docs/cycles/cycle-321-tui-active-items-subagent-row/`.

## The interrupted hint — a cancelled turn answers in the composer (cycle 353)

Pressing `Esc` during a turn does **not** leave an error row in the
transcript. The reference answers the cancelled turn in the **composer**,
under the input box:

```
  ⎿  Interrupted · What should Antigravity CLI do instead?
```

【反编译】 agy 1.4.8 — the literal is 48 bytes at `0x103617a41`, and
`cli/model/model.(*PromptModel).View` (`0x102bb49f0`) renders it through
`(*PromptModel).interruptedLine` (`0x102bb48b0`) when
`PromptModel.IsInterrupted` (`0x102bb4800`) says the latest turn was
interrupted. The `%s` is the name of the CLI the user is talking to
(`Antigravity CLI`, or `Jetski CLI` in the internal build); the clone
fills it with `branding.Current().CLIName`.

| | the reference | the clone |
|---|---|---|
| the transcript | stays empty | stays empty — no error row, and the cancelled turn's own `"context canceled"` chunk is dropped |
| the composer | one hint row | the same hint row, under the input box |
| the next turn | the hint is gone | the same — the flag is cleared when the next turn starts |

The clone's own pieces:

| | |
|---|---|
| the literal | `internal/agent/interrupted.go` — `InterruptedHintFormat`, `InterruptedHintLine` |
| the flag | `runMode.turnInterrupted` — set by the `Esc`, cleared by the next turn |
| the row | `internal/cliapp/tui_interrupted.go` — `interruptedHintRow()` |

The colours are the clone's own: the `⎿` hook in the blue the other `⎿`
rows use, the sentence in dim gray. The reference paints the line with one
lipgloss style that the decompilation does not name.

Report: `docs/cycles/cycle-353-subagent-peers-interrupt/deliverable.md`.

## The colour schemes and the style tree (cycle 323)

The reference paints its panels from a **colour scheme**: `palette`
carries all eight of them, the twenty slots each one holds and the
whole style tree — the ninety-one styles, and where each one's colour
comes from.

```go
scheme, _ := palette.SchemeByName("solarized light")
color, _ := palette.StyleColor(scheme, palette.StyleToolDone)
```

| | the reference |
|---|---|
| **the schemes** | eight — `light`, `solarized light`, `colorblind-friendly light`, `dark`, `solarized dark`, `colorblind-friendly dark`, `tokyo night`, `terminal` |
| **a scheme** | twenty slots of sixteen bytes: `Bg`, `Fg`, `Accent`, `Border`, `UserFg`, `AgentFg`, `AddFg`, `AddBg`, `AddWordBg`, `RemoveFg`, `RemoveBg`, `RemoveWordBg`, `LineNumFg`, `ToolFg`, `TaskFg`, `ErrorFg`, `WarningFg`, `LinkFg`, `MutedFg`, `DimFg` |
| **the colours** | true colours (`#rrggbb`) in seven of the schemes, terminal indices in `terminal` |
| **the resolver** | `render.resolveStyles` — **two branches**: the scheme path (the name was in the map) and the fallback path (it was not) |
| **the style set** | `render.Styles` — 91 fields, 57,720 bytes, passed by value |
| **the four task styles** | `ToolDone`, `ToolRunning`, `ToolCanceled`, `ToolError` — the marker and the status word both use them, so they always carry the same colour |

Four things that are easy to get wrong:

- **The two branches disagree.** On the scheme path a *failed* task is
  painted with the scheme's **agent** colour (green in solarized
  light) and a *running* one with the **error** colour; on the
  fallback path a failed task is index 1 (red) and a running one index
  3 (yellow). The clone keeps the colours its own capture produced
  rather than either branch.
- **`AgentFg` and `AddFg` are the same colour in every scheme**, so a
  capture cannot tell an agent's colour from an added line's.
- **`SharedStyles` resolves once per process** — the first caller's
  name decides the colours for the whole run, and a later caller with
  a different name gets the first one's set.
- **The clone's captured palette is the "solarized light" scheme** —
  nine of its constants are scheme slots, five of which differ between
  the solarized light and the solarized dark scheme. A test pins this.

Report: `docs/cycles/cycle-323-tui-render-style-scheme-table/`.

## The Color Scheme preview card, and the live palette (cycle 362)

`/config` → **Color Scheme** shows a 63-column, 22-line card beside the
eight schemes. The card is painted in the **hovered** scheme, and the
whole interface follows it.

| | the reference |
|---|---|
| **the card** | `internal/themepreview.Render` — the border, the user's line, the heading, the diff, the thought, the tool/task/error/warning rows, the link, the accent and the dim hint |
| **the code roles** | 24-bit escapes under the seven true-colour schemes; under `terminal` the basic sixteen-colour codes — `94` the keyword, `33` the string, `34` the function, `35` the type — and the keyword is bold |
| **the escape form** | the colour **and** the attribute in **one** escape: `38;2;25;103;210;1` for the user's line, `38;2;26;115;232;4` for the link |
| **the body after a bold run** | the body colour and `;22` in one escape, or a full reset when the body colour is unset |
| **the palette** | live — `palette.PushScheme` around the frame's `Update` and `View`; `palette.OnSchemeChange` for the packages that copy the palette's colours |

The card's palette is read off the reference's own output bytes, in a
true-colour pty, one run per scheme: `palette.SyntaxColorsFor` holds the
eight schemes' code palettes. `palette.Syntax` — the 256 indices — stays
for `internal/onboarding`, the pre-TUI wizard.

Two things that are easy to get wrong:

- **The palette is live, not a table.** `palette`'s colours are recomputed
  from the scheme's slots when the scheme changes, so a package that
  copies a colour into its own variable has to refresh it
  (`palette.OnSchemeChange`) — otherwise the copy keeps the old scheme's
  escape.
- **The reference's *first* paint of the card is not its *hover* paint.**
  The first paint leaves the blank row's left border unpainted; every
  hover paint draws it. The clone always renders the whole card, so it
  matches the reference's **hovered** card on all eight schemes.

Report: `docs/cycles/cycle-362-theme-live-parity/`.

## The task record and its four-way rule (cycle 324)

The reference's task row renders a **record**, and the record has a name:
`types.BackgroundTaskInfo` — 120 bytes, eleven fields, in the package
`google3/third_party/jetski/cli/types` (the same package that holds
`types.ColorScheme` and `types.SchemeColors`).

| offset | field | what the row does with it |
|---|---|---|
| +0x00 | `Index` | — |
| +0x08 | `Command` | the row's name — via `model.activeItem.name` |
| +0x18 | `Running` | the running branch |
| +0x20 | `Result` | — |
| +0x30 | `IsError` | the failed branch |
| +0x38 | `ExitCode` | the `(exit N)` suffix |
| +0x40 | `NoExitCode` | suppresses that suffix |
| +0x41 | `IsAgentTask` | the cancelled branch |
| +0x44 | `Status` | the cancelled branch |
| +0x48 | `StartTime` | the row's time cell |
| +0x60 | `LastUpdatedTime` | — |

The rule the row and the marker both run — `render.RenderTaskItem`
(0x1028a0d60) and `render.StatusMarker` (0x10289fe70) run the same one
on the same four fields:

1. `Running` → **running**, `ToolRunning`
2. `IsAgentTask` **and** `Status == CORTEX_STEP_STATUS_CANCELED` →
   **cancelled**, `ToolCanceled`
3. `IsError` → **failed**, `ToolError`
4. everything else → **completed**, `ToolDone`

Four things that are easy to get wrong:

- **Six is a step status, not an exit code.** The int32 at +0x44 is
  the record's `Status`, a `cortex_go_proto.CortexStepStatus`, and 6 is
  `CORTEX_STEP_STATUS_CANCELED`. Reading it as a process exit code
  sends the whole rule down the wrong path.
- **The cancelled branch needs both fields.** `IsAgentTask` alone is
  not enough — the status has to be the cancelled one.
- **The exit code belongs to the completed and the failed word only.**
  A running and a cancelled task never carry one, and `NoExitCode`
  suppresses it even then.
- **The record's `Command` is the row's name.** `getActiveItems` copies
  it into `model.activeItem.name`; the renderer takes it as a separate
  argument, which is why reading the row alone makes it look like it
  comes from somewhere else. `Result` is copied into the record and
  read by none of the row's rules.

The clone already renders the reference's four words, the `(exit N)`
suffix and the same rule; cycle 324 only named the fields. The
clone's `taskregistry.ExitCodeUnset` is the reference's `NoExitCode`.

Report: `docs/cycles/cycle-324-tui-task-record-type/`.

## The record's producer (cycle 325)

The record the row renders is **not** what the store holds. The store's
own record is `types.CommandOutput` (120 bytes, eleven fields); the
row's record is `types.BackgroundTaskInfo` (120 bytes, eleven fields),
and three producers fill it.

| producer | the source | `Status` | `IsAgentTask` |
|---|---|---|---|
| `store.(*TaskManager).GetBackgroundTasks` (0x1029a39b0) | the store's `[]types.CommandOutput`, filtered on `Backgrounded` | 0 | false |
| `model.activeTasks` (0x102bc0160) | the same slice, filtered on `Backgrounded` **and** `Running` | 0 | false |
| `model.agentTaskInfo` (0x102bc0730) | a **cortex step** | the step's own status | **true** |

`model.activeTasks` and `model.backgroundTasks` each have both paths:
the projection for the shell commands, and a call to
`model.agentTaskInfo` for the agent steps (0x102bc04a8 and
0x102bbff3c).

The first two are **projections**, field by field: `Command` ← +0x00,
`Result` ← +0x10, `Running` ← +0x39, `IsError` ← +0x38,
`ExitCode` ← +0x40, `StartTime` ← +0x48, `LastUpdatedTime` ← +0x60, and
`Index` ← the loop counter — the record's index in the **source**
list, not in the result. `NoExitCode`, `IsAgentTask` and `Status` are
left zero.

The third is the agent path, and it is the only one that can reach the
cancelled word:

- `Status` ← the step's own int32 status, and `IsError` ←
  (`Status == CORTEX_STEP_STATUS_ERROR`, 7).
- `NoExitCode` ← the same boolean as `Running` — a running task has no
  exit code yet.
- `IsAgentTask` is **true**, and it is set by the *static composite
  literal* the compiler copies into the record (120 bytes at
  0x103da8138 in `__rodata`), not by a store. That is why a search for
  a store at +0x41 finds nothing and the field is nevertheless true.

The row's **name cell** is the record's `Command` — `getActiveItems`
copies it into `model.activeItem.name` — and the row's **time cell** is
the record's `StartTime`, not `LastUpdatedTime`.

The enum's tenth number has no value: `CORTEX_STEP_STATUS_HALTED` is a
**reserved name** and 10 a **reserved range** (`start = 10`,
`end = 10`) in the enum's protobuf descriptor. It is never compared
against.

Report: `docs/cycles/cycle-325-tui-task-record-producer/`.

## The `/resume` picker — the workspace tree (cycle 336)

`internal/cliapp/picker_resume.go` draws the flat list and the picker's
palette; `internal/cliapp/picker_resume_tree.go` draws the **tree**, which
is agy 1.4.7's default view of the picker. The model lives in its own
package, `internal/resumetree`: the label rule, the grouping, the
comparator and the pin (captured live against agy 1.4.7,
`docs/cycles/cycle-336-resume-workspace-tree/deliverable.md`).

| file | what it is |
|---|---|
| `internal/resumetree/resumetree.go` | `Build`, `WorkspaceLabels`, `BaseWorkspaceLabel`, `ItemWorkspaceURI`, the comparator |
| `internal/resumetree/store.go` | the readers of the reference's `conversation_summaries.db` and its metadata cache |
| `internal/cliapp/picker_resume_tree.go` | the rows, the marker, the five-children cap, the hint, the keys, the cursor's target |

Five things that are easy to get wrong:

- **The tree is the default view.** `ctrl+f` switches between the tree and
  the flat "Conversation View"; the flat list's hint reads `ctrl+f Grouped
  View`, the tree's `ctrl+f Conversation View`. A *search* leaves the tree
  and draws the flat filtered list, and then there is no `ctrl+e` at all.
- **The group order is the reference's comparator** (0x102bde610): the group
  of the session's current conversation first, then the other workspaces by
  their newest conversation (descending), then by label, and the
  workspace-less group (`[No Workspace]`) last. The pin is what puts the
  reference's own `gear` group on top although its timestamp is older than
  the next group's — it is not a different comparator.
- **A group shows at most five conversations.** The cap is on the *model*,
  not on the window: expanding the reference's 486-conversation group grew
  its tree from 12 rows to 15.
- **`enter` and `ctrl+e` both toggle the group under the cursor**, and the
  cursor stays where it was (a group row stays a group row; a conversation
  stays its conversation). `f2` and `f4` act on a *conversation* and do
  nothing at all on a group row.
- **The picker's palette is its own**, not the command picker's: the cursor
  and the hint's keys are `38;2;66;133;244`, the right-hand cells of a row
  are `38;2;60;64;67`, the hint's labels and the page indicator are
  `38;2;95;99;104`, the inactive tab is `38;2;218;220;224`.

The geometry, measured at a 200-column window — **both** rows are 200 cells
wide and both right-hand blocks end at the last column:

```
group row         2 cursor + 2 marker + 170 label + 12 count + 14 age
conversation row  2 cursor + 2 indent + 170 title + 12 steps + 2 + 12 age
```

Measure the rows in **cells**, not in runes: a CJK title is two cells wide,
so the reference's own conversation row reads 194 runes but 200 cells.

Report: `docs/cycles/cycle-336-resume-workspace-tree/`.

---

## The slash-command surface (cycle 351)

The reference's slash surface is three surfaces that share one command
vocabulary: the **picker** (`/` at an empty prefix), the **TUI panels**
(the commands that open one), and the **print mode** (`agy -p /<cmd>`).
This section records what agy 1.4.8 does on each, and the three places the
clone was wrong.

### The picker's catalogue

The reference's picker lists **105** entries — the `/help` COMMANDS tab
reports the same count ("[1-33 of 105 items]"). The clone's catalogue holds
**109**: the reference's 105 plus four documented clone-only commands
(`/feature-diff`, `/accounts`, `/provider`, `/pool`).

| | |
|---|---|
| the reference's rows | 105 — 59 built-in commands and workspace skills, 41 `/science:*` skills, 6 `/chrome-devtools-plugin:*` skills, 2 `/modern-web-guidance-plugin:*` skills |
| the clone's own four | `/feature-diff`, `/accounts`, `/provider`, `/pool` |
| the filter | **none** — 1.4.8 shows every row, including the `chrome-devtools-plugin:*` ones the 1.3.6 filter used to hide |

The plugin rows are environment state: the reference enumerates the installed
plugins at runtime, and the clone keeps a snapshot of them
(`internal/tuilib/commands.go`).

### Which panels echo "Exited /<cmd> command"

Escaping a panel leaves a `⎿  Exited /<cmd> command` line in the
transcript — for **ten** commands only. The reference's own binary carries
ten literal messages ("Exited /diff command", "/help", "/hooks", "/model",
"/tasks", "/agents", "/effort", "/skills", "/credits", "/artifact") plus one
generic format string ("Exited %-*s %s"), which is how `/config`'s own
message is built. Every other panel closes silently.

| echoes | closes silently |
|---|---|
| `/config`, `/diff`, `/skills`, `/agents`, `/effort`, `/credits`, `/artifact`, `/model`, `/hooks`, `/tasks`, `/help` | `/usage`, `/permissions`, `/context`, `/codesearch`, `/changelog`, `/plugin`, `/mcp`, `/feedback`, `/rewind`, `/add-dir` |

`/add-dir` is not a panel at all in the reference — it answers
`error: no directory specified` inline — so it does not echo either.

### `/statusline` is a toggle

A **bare** `/statusline` toggles the status line:

| input | answer |
|---|---|
| `/statusline` (off → on) | `Statusline on.` |
| `/statusline` (on → off) | `Statusline off. Run /statusline to re-enable.` |
| `/statusline on` / `/statusline enable` | `Statusline on.` |
| `/statusline off` / `/statusline disable` | `Statusline off.` |

The explicit `off` answers with the **short** form; only the bare toggle
answers with the long one, which also tells the user how to switch it back
on (`internal/tuilib/feedback.go`).

### The `/skills` panel renders the icon next to the name

The catalogue keeps the **bare** frontmatter name — `agy -p /skills` prints
`automation`, and the JSON payload carries the six keys `name`,
`description`, `path`, `plugin`, `builtin`, `model_invocable` and **no**
icon. The *panel* is the surface that renders the icon: the frontmatter's
`metadata.icon`, one space, then the name.

```
metadata:            name: automation
  icon: schedule   →  "schedule automation"
```

The icon is rendered **verbatim** — the reference filters it against no table
of known glyphs, so a skill with `metadata.icon: zzznotanicon` lists as
`zzznotanicon zziconprobe`. It travels next to the name rather than inside
it (`printmode.SkillInfo.Icon` is `json:"-"`), and it heads the skill's
detail view as well.

The built-in group is sorted by the **bare** name — `agy-customizations`,
`antigravity-guide`, `automation`, `migrate-workflows`, `run-workflow` —
which is why "automation" sits between "antigravity-guide" and
"migrate-workflows" even though its row reads "schedule automation".

### The print-mode surfaces: `/config`, `/usage`, `/changelog`

- **`/config` is a union.** The dump carries the reference's 36 known keys
  **plus every key in `settings.json`** — a file-only key
  (`dismissedAnnouncements`) sits at its alphabetical place. The keys are
  sorted globally, and a value renders by its JSON type: a string bare, an
  object or a list as compact JSON on one line, a number or a boolean as its
  literal, and a null as an **empty cell**.
- **The defaults are the reference's 1.4.8 defaults**, not the 1.3.6 ones
  the clone carried: `artifactReviewPolicy` is `asks-for-review`,
  `toolPermission` is `request-review` and `colorScheme` is `terminal`. The
  seven nullable keys (`customModelsConfig`, `gcp`, `permissions`,
  `sandboxProxy`, `statusLine`, `title`, `trustedWorkspaces`) are **null**
  when they are absent — not an empty object and not an empty list. The
  official settings reference agrees on every default it lists.
- **An explicit `null` decodes to the zero value of the key's type**: a
  string key becomes `""` (not its default), a number key `0`, a boolean key
  `false`; a key the reference does not know keeps its `null`.
- **`init.permission_mode` follows the same setting.** With an empty
  `settings.json` the reference's init event reports `request-review` — the
  value the 1.1.0 changelog calls the default. The `always-proceed` the
  cycle-113 capture carries came from a capture machine whose `settings.json`
  set `toolPermission` explicitly; the file value wins.
- **`/usage` is tab-separated**, four columns per row —
  `<group>\t<bucket>\t<pct>%\t<RFC3339 UTC>` — and the text answer is the
  payload rendered, not a separate "Quota:" line.
- **`/changelog` prints the reference's own release blob** — 176 788 bytes
  at 1.4.8, topping out at `## 1.4.7` — with a single trailing newline.

Report: `docs/cycles/cycle-351-slash-command-alignment/deliverable.md`.

### A tabbed panel: `/accounts` (cycle 361)

The reference's panels are single-view. The clone's `/accounts` panel has
**two**: the accounts the user signed in to, and the **Pool** tab — the
account-routing policy that decides which of them serves a turn. The tab bar
is the `/resume` picker's own: the active tab is bold, underlined and blue, the
inactive one is the separator grey, and the row ends with *"(tab to cycle)"*.

`/pool` is the same panel opened on the Pool tab, so the policy has a command
of its own as well as a place in the account list.

The rules that make it work:

- **The cursor belongs to the view.** `m.panel.cursor` is shared by every
  panel kind, so a tab change resets it — the Pool tab opens on its first row,
  the account list on its first account.
- **The panel owns every key.** The Pool tab turns `←/→` into "change this
  value" and `enter` into "edit this field", so its handler runs *before* the
  generic list navigation (`internal/cliapp/slash_panel_keymap.go`) and never
  falls through to it.
- **A change is written out at once.** The policy is read per turn, so a
  policy changed in the panel is the policy the next turn runs on. See
  [`accounts.md`](accounts.md) for the writer and the router.

Report: `docs/cycles/cycle-361-account-pool-panel/deliverable.md`.

## The start-up memory trim (`internal/memtrim`, cycle 363, agy 1.4.10)

【一手】 agy 1.4.10: "Improved memory use shortly after startup: the CLI
now returns about 100 MB of memory left over from startup to the operating
system 15 seconds after the interactive UI opens; print and headless modes
are unaffected."

The interactive UI is where the memory is: the start-up path builds the tool
catalogue, the plugin registry, the prompt and the banner before the first
frame, and most of it is dead once the first turn is on screen. The Go
runtime keeps those pages for the next allocation instead of handing them
back, so the process looks like it is holding a hundred megabytes it is not
using.

`runtime/debug.FreeOSMemory` is the call that gives the pages back — it runs
a collection and then returns as much of the freed heap to the OS as it can.

| what | where |
|---|---|
| the delay (15 s) | `memtrim.Delay` |
| the trim itself | `memtrim.Release` / `memtrim.After` (the cancellable schedule) |
| the only arming site | `internal/cliapp/tui_entry.go` — `runLiveTUI` |

The trim is armed **only in the interactive TUI**, which is the reference's
own scope: print and headless modes are explicitly unaffected, so arming it
there would be a behaviour the reference does not have. `memtrim.After`
returns the cancel function, and the entry point defers it, so a session that
ends before the delay does not leave a timer behind.

Report: `docs/cycles/cycle-363-agy-1.4.10-alignment/deliverable.md` §2.

## The scrollback (cycle 363, agy 1.4.10) — **open**

【一手】 agy 1.4.10: "Improved streaming performance: the CLI now streams
model output into the terminal scrollback instead of redrawing the whole
conversation, and uses about 70% fewer bytes on the wire."

The reference owns the mechanism in its own view layer, and the clone can
see it in the 1.4.10 symbol table:

| reference symbol | address |
|---|---|
| `jetski/cli/viewport.InlineStrategy.FlushToScrollback` | `0x102aeb830` |
| `jetski/cli/viewport.InlineStrategy.CommitScrollback` | `0x102aec2f0` |
| `jetski/cli/viewport.buildScrollbackContent` | `0x102aec400` |
| `jetski/cli/viewport.prepareScrollbackContent` | `0x102aecaa0` |
| `jetski/cli/viewport.computeSplitPoint` | `0x102aec380` |
| `model.(*ConversationModel).pushToScrollback` | `0x102b68a00` |

The shape is: the live region is an **inline** strategy over the terminal's
scrollback, and a completed turn is *committed* into it — the content is
split at a computed point, the finished part is written to the scrollback
once, and only the unfinished tail stays in the live region.

The clone **does not implement this yet**, and the reason is structural
rather than a missing feature:

- The clone's TUI renders through **bubbletea v2** (`charm.land/bubbletea/v2`
  v2.1.0). Its renderer (`cursed_renderer.go`) has **no** notion of a
  scrollback — the string "scrollback" does not appear in it — so the
  terminal's own scrollback is never written to; the alternate screen is
  used instead (see *The rendering mode* above).
- The clone's turn output therefore **redraws the whole conversation** on
  every frame, which is the half of the 1.4.10 note the clone does not
  match.
- The "70% fewer bytes on the wire" half is the reference's *transport*
  change (streaming deltas instead of whole-message payloads). The clone's
  CloudCode dialect (`geminicode/`) carries its own framing and was not
  changed by this cycle.

To close the gap the clone would need a renderer it owns — one that writes
to the terminal's scrollback directly and keeps a live region on top of it —
which means replacing or wrapping bubbletea's renderer rather than
configuring it. That is a separate cycle.

Report: `docs/cycles/cycle-363-agy-1.4.10-alignment/deliverable.md` §1.
