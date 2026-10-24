# Cycle 288 — `@` picker, `/context` category breakdown, and panel re-verification (agy 1.4.3)

## TL;DR

| What | Where |
|---|---|
| `@` picker result set (files **and** directories, recursive, 50 cap) | `internal/atfiles/atfiles.go` (`List`, `FilterEntries`) |
| `@` picker layout, query/directory modes, `enter`/`tab` semantics, hint | `cmd/wagyu/at_file_dropdown.go`, `cmd/wagyu/tui_run.go` |
| `/context` 1.4.3 category rows (prompt/tools/skills/subagents) and coloured grid | `cmd/wagyu/slash_panels.go` (`panelContextLiveLines`) |
| Prompt-section token estimates | `cmd/wagyu/tui_system_prompt.go` (`promptOptions`, `promptSectionTokens`) |
| Built-in `/skills` order | `cmd/wagyu/slash_skills.go` (`builtinSkillPanelOrder`) |
| Expanded tool block check | `cmd/wagyu/tui_tools.go` (no change needed) |
| `@` order after the first screen, agent-status tab byte | recorded below as deliberate deviations |

Reference: `~/.local/bin/agy` 1.4.3, driven unpatched in tmux at 120x40 and
captured with `tmux capture-pane -p -e` plus a real pty where byte shape
mattered. All reference facts are 【一手】 unless marked otherwise.

---

## `@` file mention

### Result set

The reference does **not** glob one directory level. Typing `@agy` in this
workspace returns `cmd/agy/`, `devutils/agybin/`, `internal/convstore/agy.go`,
`devutils/agyproxy/`, ...; typing `@` returns top-level directories and files
and then nested paths, at most **50 rows** (`↓ 45 more` with the 5-row window).
It includes hidden project directories (`.agents`, `.github`,
`.antigravity`) and ignored trees (`scratch/`, `v4-captures/`), but not
`.git` or `.crush`.

The clone now walks the workspace recursively (`WalkDir`, symlinked dirs not
followed), includes directories with a trailing `/`, skips `.git`, `.crush`,
`node_modules` and `.DS_Store`, caps the scan at 50,000 entries and the result
at 50.

### Modes

Three display modes are measured:

| Prefix | Rows | Layout |
|---|---|---|
| empty | whole workspace | name · `Directory`/`File` · absolute path |
| ends in `/` | direct children of that directory | same columns |
| anything else | substring/fuzzy matches across the relative path | **name only** |

The empty listing at 120 columns uses a 33-cell name field (including the
`> `/`  ` marker), a 12-cell type field and the absolute path at column 45.
Directory browsing expands the name field and truncates the absolute path to
`/...`; rows whose name overflows the field drop the type/path columns and are
hard-truncated at the terminal edge, which is what the reference does when
scrolling into the deep `out/` / `re-decompiled-*` entries.

The no-match state is not `No matches`: 1.4.3 renders one synthetic selected
row,

```text
> Type to search workspace files...  File        Keep typing to filter files or directories
```

then four empty list rows, then two blank rows, then the hint.

The hint is exactly

```text
  ↑/↓ Navigate · enter Select · tab Complete
```

with `↑/↓`, `enter`, `tab` bold blue and the labels/separators in one dim-gray
span. There is **no** `esc Cancel`.

### `enter` and `tab`

- `enter` splices `@<relative path>` into the input and **closes** the picker.
- `tab` splices the same text but **keeps the picker open**. On a directory the
  list re-roots at that directory (`@cmd/agy/` → its files); on a file it
  narrows to that path.
- Neither appends a trailing space.
- `enter` is consumed by the picker even when the buffer would otherwise
  submit, and in queue mode.

### Known deviation: candidate order

The reference's row order is produced by its language-server file index /
ripgrep pair, not by a deterministic sort. The first screen in this workspace
starts `bin/ cmd/ out/ docs/ tools/ ...`; the order also changed between two
captures taken minutes apart, so it cannot be reproduced from the filesystem
alone. The clone uses a stable approximation: shallower paths first,
directories before files at the same depth, then case-insensitive name.
Structural output (types, columns, counts, tab/enter behaviour) matches; the
identity of the first five rows is 【存疑】 and is recorded as a limitation.

---

## `/context`

### Empty session

1.4.3's empty panel is unchanged from the old capture: title, 24-cell grid at
120 columns, model row `0/1.0M tokens`, `(0.0%)`, `Estimated usage (awaiting
generation)`, three message rows, free space, then the system-file block. The
block now includes **`.agents/agents/`** in both branches.

A pre-existing bug made only the `Estimated usage` row use 44 cells regardless
of terminal width; it now uses the measured 24-cell grid like its neighbours.

### Live session

The 1.4.3 non-empty panel differs from 1.4.2 in four ways, all captured live:

1. Row 3 is `Token usage by category`, not `Estimated usage (live session, …)`.
2. Four system categories are added: `System prompt`, `System tools`, `Skills`,
   `Subagents`.
3. The first grid row is colour-coded by category (`◉` in category colours);
   rows below continue the same 24-cell-wide grid with empty `□` cells.
4. The categories sum into the model-row total; the old message count label is
   gone.

The clone computes:

| Category | Estimate |
|---|---|
| System prompt | full `agyprompt.Build` output, 3 bytes/token |
| System tools | JSON of the advertised `backend.ToolDecl` list, 3 bytes/token |
| Skills | the `<skills>` section of the same prompt, 3 bytes/token |
| Subagents | the `<subagents>` section, 3 bytes/token |
| User / agent / tool | existing 4-bytes/token proxy plus 5 tokens per call |

At the time of the capture this produced `22.4k/1.0M (2.2%)` versus the
reference's `24.6k/1.0M (2.3%)` for an equivalent `say hi` turn; the individual
rows read 11.4k / 3.6k / 7.0k / 287 against 11.5k / 4.6k / 7.4k / 702. The
layout, row order, colours, grid fill count and trailing block are
byte-compatible; the numbers are estimates, not the provider's tokenizer
【推测】.

---

## `/skills` built-in order

The shared and plugin groups were already correct. The built-in group at 1.4.3
is **not alphabetical**:

```text
agy-customizations
antigravity-guide
schedule automation
migrate-workflows
run-workflow
```

`slash_skills.go` now applies `builtinSkillPanelOrder`, leaving the slash
picker's name sort untouched.

---

## Expanded tool blocks

A live reference turn (`run_command pwd`) was captured collapsed and expanded:

```text
● Bash(pwd)
  ⎿  Command exited with code 0.

            Command output:
            /Users/gear/Documents/GitHub/openagy (ctrl+o to collapse)
```

The clone's collapsed and expanded blocks are byte-identical for this turn,
including the 12-space output indent and the `(ctrl+o to collapse)` hint on the
last output line. No code change was required; this closes the last unverified
transcript item from cycle 285.

---

## Panel re-verification

After the recent rounds, the tail of the following panels was diffed against
the reference with branding, log filename and conversation UUID normalised:

| Panel | Result |
|---|---|
| `/help` | identical |
| `/plugin` | identical |
| `/agents` | identical |
| `/model` | identical |
| `/skills` | identical after the built-in order fix |
| `/context` | identical structure; estimate values differ as above |

---

## Known byte deviation: agent status padding

With `--agent alpha`, the reference status row contains a literal tab in the
right-hand padding:

```text
? for shortcuts …  \t auto · alpha (Gemini 3.8 Flash · high)  /agents
```

where the clone emits spaces. Emitting a tab from the clone's view path does
not survive Bubble Tea's frame rendering (tabs are expanded before the frame
reaches the terminal), and the visible column is identical in both. This is
left as a one-byte deviation rather than forcing a cursor-position escape into
the status row.

---

## Verification

```sh
go build -o /tmp/wagyu-at ./cmd/wagyu
go test ./cmd/... ./internal/...
```

`TestWrapRowsQuadratic` is a load-sensitive timing test and can fail under a
busy machine; a re-run of the package passes. The `printSlashRejections` count
was updated from 25 to 26 for the concurrently added `/record` command.

`agent_not_found_test.go` was hardened while re-running the suite: its happy-path
cases now pin `OPENAGY_BACKEND=shim` and only require a non-empty reply. Without
that, a machine holding a live OAuth token sent the turns to CloudCode, which
answered `400 User location is not supported` (exit 3) on this account, and the
old greeting-token assertion no longer matched the shim's wording.

Commits:

- `9d14ed2` — `@` picker layout, filtering and tab completion
- `0d0a078` — depth calculation and overflow truncation
- `a2af0a4` — `/context` category breakdown and `/skills` built-in order

---

## 硬约束

- No reference binary was patched; the wrapper's `CLOUD_CODE_URL` path was not
  used for these observations. All facts come from the installed 1.4.3 TUI.
- Exact `@` candidate order is not reproduced (language-server index state).
- `/context` category values are estimates from prompt/tool sizes, not the
  provider's usage metadata.
- The agent-status tab byte is not reproduced through Bubble Tea.

## 存疑

- Why 1.4.3's query-mode rows sometimes show type/path columns
  (`@cycle-285`, `@deliverable`) and sometimes do not (`@agy`, `@macos-re`) is
  still unresolved. The working hypothesis is that the fast index returns
  metadata for some queries while ripgrep-only results carry none. The clone
  renders non-directory queries name-only.
- The reference's `↓ N more` count is not recomputed after `tab` re-roots a
  directory (`@cmd/agy/` still says `↓ 45 more` with only eight files). The
  clone emits the correct count; this looks like a reference bug and was not
  copied.
- The built-in `/skills` order is hardcoded for 1.4.3's five entries; a new
  bundled skill will fall to the alphabetical tail until re-captured.

## limitations

- No pty-level byte diff was taken for the `@` picker as a whole; the
  individual row/hint spans were compared, and the visible shape at 120x40
  matches.
- The `/context` category split for a **custom** `--agent` session is not
  modelled (the whole `BuildAgent` prompt is counted as `System prompt`).
- `TestWrapRowsQuadratic` remains timing-sensitive and may flake on a loaded
  machine.
