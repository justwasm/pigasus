# Cycle 285 — the tool-call transcript, aligned row for row

Previous cycles rebuilt the step rows from string-table fragments. This cycle
re-derived the whole display from **byte-level pty captures of `agy` 1.4.3**
and replaced the reconstruction. Everything below marked 【一手】was read out of
raw SGR bytes, not out of `tmux capture-pane -e`.

The single sentence version: **the reference has two forms per call (collapsed
summary / expanded call), a provisional `Running` row, a blank line around
every block, and each assistant message is its own text run.**

## First-hand evidence — the row bytes

All captures: 200×40, `TERM=xterm-256color`, `COLORTERM=truecolor`, the real
`agy` 1.4.3 through `/tmp/agy-wrapper` (MiniMax M3).

### Running

```
\x1b[38;2;181;137;0m●\x1b[m \x1b[38;2;181;137;0;1mRunning\x1b[m \
\x1b[38;2;88;110;117m(sleep 8; echo hi)\x1b[m \
\x1b[38;2;88;110;117m(ctrl+o to expand)\x1b[m
⣿  Running command...            ← spinner row, #657B83 label
```

### Finished, collapsed

```
\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;181;137;0;1mRan\x1b[m \
\x1b[38;2;88;110;117m(echo one)\x1b[m \
\x1b[38;2;88;110;117m(ctrl+o to expand)\x1b[m
```

Non-zero exit appends ` failed` (lowercase) after the args and reddens the
bullet: `\x1b[38;2;220;50;47m●\x1b[m … Ran (echo boom; exit 2) failed`.

### Finished, expanded

```
\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;181;137;0;1mBash\x1b[38;2;88;110;117;22m(echo boom; exit 2)\x1b[m
  \x1b[38;2;38;139;210;1m⎿\x1b[m  Command exited with code 2.
<blank>
            Command output:
            boom \x1b[38;2;88;110;117m(ctrl+o to collapse)\x1b[m
```

Three things this pins:

1. **The expanded head has no space before the paren** — `Bash(cmd)`, not
   `Bash (cmd)`. `Read(/tmp/x)` likewise.
2. **The expanded bullet is green even when the call failed.** Red is a
   collapsed-only decoration. (Measured on a `false` capture:
   `\x1b[38;2;133;153;0m●` above `Command exited with code 1.`.)
3. **The hint moves to the last detail row** in expanded mode; the head carries
   none. Collapsed keeps it on the head.

### Detail bodies are not the tool result

The model-facing result and the transcript body differ:

| tool | tool result (model) | transcript body |
|---|---|---|
| `run_command` | `Created At: …\nCompleted At: …\n\nThe command exited with code N.\nOutput:\n<out>` | `Command exited with code N.` + blank + `Command output:` + `<out>` (or `No output from the command.`) |
| `view_file` | the file bytes | `Read N lines` |

The timestamps never reach the screen.

### Files, groups, spacing

```
● Explored 1 file (agy-cont.sh) (ctrl+o to expand)     ← collapsed file step
● Read(/tmp/agy-cont.sh)                              ← expanded
  ⎿  Read 3 lines (ctrl+o to collapse)
```

```
● Ran 2 commands (echo one · echo two) (ctrl+o to expand)
```

Byte-for-byte identical between the two binaries after this cycle
(`ref_two_parallel.bin` vs the clone's group row). The count token is the body
colour (`#073642`), not the verb orange.

Spacing, from `ref_view_exp2.bin`:

```
> prompt
<blank>
  I'll read that file.
<blank>
● Read(/tmp/agy-cont.sh)
  ⎿  Read 3 lines (ctrl+o to collapse)
<blank>
  It's a shell script …
```

One blank row before and after every block, and before the first assistant
message. The reference renders **sequential** calls in separate blocks when the
model emits text between them, and only calls batched into one assistant
message share a header (verified: `ref_two_collapsed2.bin` shows two
one-command blocks; `ref_two_parallel.bin` shows one `2 commands` header).

### The spinner label

```
\x1b[38;2;101;123;131mRunning command\x1b[38;2;38;139;210m..\x1b[m
```

`#657B83` base plus a five-character `#268BD2` highlight that walks the string,
then the settled phrase with dots cycling 0→3. The label is **not** the tool
name: `Generating` before the first call, `Working` once a call has run,
`Running command` / `Reading file` while one executes (`ref_view.bin`,
`ref_queue.bin`). Decoded frames for `Reading file`:

```
R|eadin  Re|ading  Rea|ding   Read|ing f  …  Reading| file  Reading |file.
Reading f|ile..  Reading fi|le...  Reading file  Reading file.  Reading file..
```

(`|` = base/highlight boundary.)

## Code

| File | Change |
|---|---|
| `cmd/wagyu/tui_tools.go` | `toolStep` grew `summary`, `running`, `batch`; `title` is now the expanded form. `toolStepTitle` emits `Bash(cmd)` / `Read(path)`; new `toolStepSummary` (`Ran (…)`, `Explored 1 file (base)`), `newRunningStep`, `runningSummary`, `runningSpinnerText`, `spinnerText`, `stepDetailText`/`commandDetailText`/`readDetailText`, `styledStepBullet`, `groupSummary`/`groupLines` |
| `cmd/wagyu/tui_tools.go` | `toolBlockLines` splits thoughts from calls, renders 1 step as `stepLines` and N as `groupLines`, and appends the hint to the last row |
| `cmd/wagyu/tui_run.go` | `toolRunningMsg` paints the provisional `● Running (…)` row and names the activity on the spinner; `toolStepMsg` rewrites that row in place |
| `cmd/wagyu/tui_run.go` | per-message text runs: `replyStart`/`liveTextLines`/`liveReplyBuf` restart after every step, so prose before a call stays above it |
| `cmd/wagyu/tui_run.go` | `finaliseLiveTurn` re-renders each batch at its own coordinates and records one `turnBlock` per batch; the old `block + reply` replace is kept only for the headless path (no live runs) |
| `cmd/wagyu/tui_run.go` | `blankBefore` inserts the reference's blank separator before a block or a new message |
| `cmd/wagyu/tui_run.go` | `spinFrame` advances on the spinner tick; `anyToolRun` selects `Working` over `Generating` |
| `cmd/wagyu/colors.go` | `cToolBulletRunningOpen` (#B58900), `cBodyOpen` (#073642), `cSpinnerBaseOpen` (#657B83), `cSpinnerHiOpen` (#268BD2) |
| `cmd/wagyu/conversation.go` | resumed steps go through `newToolStep`, so a loaded transcript renders the same summary/expanded pair |
| `cmd/wagyu/tui_tools.go` | `edit` count category ("Edited"), `Create(path)` / `Edit(path)` / `Read(path)` heads, `Edited 1 file (base)` / `Explored 1 file (base)` summaries |
| `cmd/wagyu/tui_tools.go` | `editDiff` + `buildEditDiff` / `editDiffFor`: the `+A / -B lines` hunk with 3 context lines, stripe and per-line colours; `Wrote N line(s)` detail; one-line reads suppressed |
| `cmd/wagyu/tui_tools.go` | `categoryGroups` — one summary row per category, summaries kept above the calls when expanded |
| `cmd/wagyu/tui_viewport.go` | spinner row renders `glyph + "  " + spinnerText(phrase)`; the `Thinking…` live frame is suppressed while a call is in flight |

## Verification

Live A/B, each side captured from a pty and replayed through `pyte`:

| Scenario | Reference | Clone |
|---|---|---|
| one command, expanded (`sleep 1; echo hi`) | `ref_seg2.bin` | `/tmp/cl285_b.bin` — identical rows, including the blank separators and `Reading`/`Command output:` layout |
| one command, collapsed | `ref_done1.bin` | `/tmp/cl285_c.bin` — `● Ran (echo one) (ctrl+o to expand)` |
| running | `ref_running.bin` | `/tmp/cl285_run4.bin` — `● Running (sleep 30; echo hi)` + `⣾  Running command…` in `#657B83` |
| failed, collapsed | `ref_fail_coll.bin` | `/tmp/cl285_fail2.bin` — red bullet + ` failed` |
| failed, expanded | `ref_fail.bin` | `/tmp/cl285_fail3.bin` — green bullet, `Command exited with code 2.`, `No output…` when empty |
| file read, expanded | `ref_view_exp2.bin` | `/tmp/cl285_view.bin` — `● Read(/tmp/agy-cont.sh)` + `Read 3 lines` |
| file read, collapsed | `ref_view.bin` | `/tmp/cl285_view2.bin` — `● Explored 1 file (agy-cont.sh)` |
| two sequential calls | `ref_two_collapsed2.bin` | `/tmp/cl285_two.bin` — two blocks with the reply text between them |
| two parallel calls | `ref_two_parallel.bin` | `/tmp/cl285_par2.bin` — group row byte-identical |
| write/create, expanded | `ref_write.bin` (`● Create(…)` + `Wrote 1 line`) | `/tmp/cl285_write.bin` — same pair |
| edit, expanded | `ref_edit3.bin` (5-line hunk) | `/tmp/cl285_edit.bin` — same hunk, same numbering, same `+1 / -1` |
| read + edit batch, collapsed | `ref_edit_c.bin` (`Explored` row + `Edited` row) | `/tmp/cl285_edit_c.bin` — same two rows, hint on the last |

Unit tests added or rewritten:

| Test | Pins |
|---|---|
| `TestStepTitle_NonZeroExit_HasFailedLabel` | collapse-only ` failed` + red bullet; expanded head stays clean |
| `TestStepTitle_ZeroExit_NoFailedLabel` | teal bullet, `Command output:` body, no timestamps |
| `TestStepTitle_GroupLabel_NoErroredConstabulary` | `Ran / 2 commands / (true · false)` |
| `TestGroupLabel` | category counts, `Explored N files (…)`, mixed-category order |
| `TestSpinnerText_MatchesReferenceFrames` | the eight measured `Reading file` frames, base/highlight split exact |
| `TestEditDiff_MatchesReferenceHunk` | the 7-row hunk and its exact SGR bytes (`+1 / -1 lines`, stripe, context numbering) |
| `TestEditDiff_SingleLineFile` | a no-trailing-newline edit: changed pair + hint spacer row |
| `TestNewToolStep_EditDiffFromFile` | hunk recovered from the post-edit file plus Target/Replacement |
| `TestReadFile_OneLineHasNoDetail` | one-line read → no `⎿` row; two lines → `Read 2 lines` |
| `TestGroupLines_PerCategoryRows` | mixed batch → `Explored` row + `Edited` row, calls below when expanded |
| `TestLiveRows_PostToolPhrase` | `Generating` → `Working` → `Running command` |
| `TestTUITools_SpinnerNamesRunningTool` | the provisional row is painted and rewritten in place |
| `TestTurnDoneKeepsStepsThatFollowAStreamedChunk` | interleaved text + two steps survive turn end |

`go build -o /tmp/wagyu ./cmd/wagyu` clean; `go test ./cmd/... ./internal/...`
green (27 packages, 0 FAIL).

## Second wave — file writes, edits, and mixed batches

The 1.4.3 surface sent to the model is **12 tools**, and three of them touch
files (`read_file`, `write_file`, `edit_file` — the wrapper capture at
`~/.openagy/captures/…-044-streamGenerateContent.json` carries their exact
declarations). Capturing those three closed the last display gaps.

### Writes and edits

```
● Edited 1 file (w285b.txt) (ctrl+o to expand)     ← collapsed, create
● Create(/tmp/w285.txt)                            ← expanded, create
  ⎿  Wrote 1 line (ctrl+o to collapse)

● Edited 1 file (w285b.txt) (ctrl+o to expand)     ← collapsed, edit
● Edit(/tmp/w285f.txt)                             ← expanded, edit
  ⎿  +1 / -1 lines
        1    alpha
        2    beta
        3 -  gamma
        3 +  GAMMA
        4    delta
        5    epsilon
        6     (ctrl+o to collapse)
```

Five facts here:

1. **A create is labelled "Edited", not "Wrote".** The collapsed row for a
   `write_file` that created a new file reads `● Edited 1 file (w285b.txt)`;
   the expanded head reads `Create(path)`.
2. **`Wrote N line(s)` is the detail row**, computed from the written content
   (`Wrote 1 line`, `Wrote 3 lines`), and it is **not** part of the title.
3. **An edit prints a unified hunk.** Context is three lines; context rows
   print the line number in `#839496` with no background, changed rows carry a
   base3 `#EEE8D5` stripe that runs to the right edge, removed text is
   `#DC322F`, added text is `#859900`. Raw:
   ```
   \x1b[6G\x1b[38;2;131;148;150m   1\x1b[m    alpha
   \x1b[6G\x1b[38;2;131;148;150;48;2;238;232;213m   3\x1b[38;2;220;50;47m -  gamma\x1b[K\x1b[m
   ```
   A file written without a trailing newline has no trailing empty context
   line; one written with `\n` does (it renders as line N+1).
4. **A one-line read has no detail row at all** — the hint moves onto the call
   head: `● Read(/tmp/w285d.txt) (ctrl+o to collapse)`. Two or more lines show
   `⎿  Read N lines`.
5. **The detail chevron is `#586E75`, not the warning blue.** This corrects an
   earlier assumption: every captured `⎿` (`Command exited…`, `Wrote…`,
   `Read…`, `+1 / -1 lines`) is `\x1b[38;2;88;110;117m  ⎿  \x1b[m`.

### Searches, and the in-flight verb

A `search_web` call was captured both while it ran and after it failed (429):

```
● Exploring 1 search (golang) (ctrl+o to expand)   ← in flight, amber bullet
⣿  Searching web...                                ← spinner phrase
● Explored 1 search (golang) (ctrl+o to expand)    ← finished
● WebSearch(golang)                                ← expanded head
```

Raw for the live row:

```
\x1b[38;2;181;137;0m●\x1b[m \x1b[38;2;181;137;0;1mExploring\x1b[m \x1b[38;2;7;54;66m1 search\x1b[m \x1b[38;2;88;110;117m(golang)\x1b[m
```

So the in-flight row is **not** a special "Running (…)" shape: it is the
finished summary with its verb in the present participle
(`Ran → Running`, `Explored → Exploring`, `Edited → Editing`) and the amber
bullet. The clone now derives it exactly that way. A failed search reddens the
bullet but prints **no** ` failed` — that word is command-only (a non-zero
exit), which matches the failed-command capture.

### Mixed batches are grouped per category

A message that emits a read and an edit is **not** one mixed header:

```
● Explored 1 file (w285b.txt)
● Edited 1 file (w285b.txt) (ctrl+o to expand)
```

One summary row per category, in first-appearance order, with the hint only on
the last row. In expanded form the reference keeps those summary rows and then
lists every call (verified on two parallel commands:

```
● Ran 2 commands (echo one · echo two)
● Bash(echo one)
  ⎿  Command exited with code 0. …
● Bash(echo two)
  ⎿  … two (ctrl+o to collapse)
```
so the block is *summaries first, calls second*, and the earlier single-header
model was wrong for N > 1).

## 硬约束

- The reference is `agy` 1.4.3 on MiniMax M3 through the wrapper; Google's own
  models are still location-blocked for this account. The TUI binary under test
  is the real one either way.
- Only `run_command` and `view_file` have captured collapsed/expanded forms.
  `write_to_file`, `grep_search`, `read_url_content` and the subagent tools
  still render through the cycle-98/157 reconstructions and were **not**
  re-verified in 1.4.3.
- The spinner's per-tool phrases are 【一手】 for command and file only;
  `Searching` / `Working` for other categories is 【推测】 (see 存疑).

## 存疑

- **Shimmer cadence.** The base colour, the five-character window and the
  phrase set are 【一手】, but the reference's exact frame count per cycle could
  not be pinned: its diff-based renderer transmits only the *changed* cells, so
  a settled repaint shows up as several partial writes. `spinnerText` uses
  phase A = `len(phrase)-2` frames and phase B = 4 (dots 0..3); the reference
  appears to spend one extra frame at the end of phase B. Visual difference:
  one ~100 ms frame.
- **`Working`.** It is the post-tool label in `ref_view.bin` and
  `ref_queue.bin`; whether it also covers the "thinking after a tool" state
  separately from generation could not be distinguished.
- **Running rows for non-command tools.** `● Reading (path)` is inferred from
  the same step model; only `Running (cmd)` was captured.
- **Thought rows.** MiniMax's reasoning arrives out of band and the reference
  renders nothing for it, so `▸ Thought for Ns, N tokens` and its expand hint
  remain clone-only. Their placement above the first block is unchanged.
- **`ctrl+o` on a `Running` row** keeps the running form (there is no detail
  yet) and flips the hint; the reference's behaviour here was not captured.
- **`Create` vs `Edit` for a write that overwrites.** Only a create was
  captured; an `Overwrite=true` or `Append=true` call may print `Edit(path)`
  instead. The clone always prints `Create` for the write tools.
- **The `Edited N files (a · b)` plural** is inferred from the two-step command
  group; only the singular create was captured.
- **Running rows for files and edits** (`● Exploring 1 file (x)`,
  `● Editing 1 file (x)`) follow the measured Ran→Running / Explored→Exploring
  rule; only the command and search live rows were captured.
- **Thought rows.** On MiniMax the reference renders the reasoning as plain
  body prose (no bullet, no duration), while the clone shows
  `▸ Thought for 3.9s` plus the body. The cycle-156 thought model is unchanged
  here and the reference's behaviour for Google's own models is unknown.
- The clone still shows `Thinking…` for a turn that reason-streams after a tool
  completes (`activeTool` is empty again), which the reference (no reasoning
  channel on MiniMax) cannot confirm.

## limitations

- `pyte` cannot parse the kitty keyboard protocol, so replayed screens show a
  stray `1u` in the input row on **both** binaries — a replay artefact.
- One live run per scenario: MiniMax's first-token latency swings 2–40 s, so
  fixed-delay key scripts are unreliable; the unit tests pin the invariants
  independently.
- The group separator between file args (` · `) is 【一手】 only for commands;
  the file group's `(a · b)` shape was inferred from the same renderer.
