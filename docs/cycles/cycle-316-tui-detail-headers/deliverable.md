# The `/tasks` detail headers — deliverable

**Cycle:** `cycle-316-tui-detail-headers`
**Subject:** the two `/tasks` detail views' *shapes* — the task detail's
rule and the subagent detail's five-line header, its own state vocabulary
and its body's indents. Cycle 315 built the two views but drew them from
the binary's *string table*; the header it drew (`Agent(%s)` /
`ID: [%s]`) is not what the reference renders.
**Evidence:** 【反编译】 `model.TaskDetailModel.buildTaskDetailLines`,
`model.TaskDetailModel.RenderHeader`,
`model.SubagentDetailModel.buildHeaderLines`,
`model.SubagentDetailModel.buildContentLines` and `model.TasksModel.View`
decompiled from the reference `agy` 1.4.7 binary; 【strings】 every label
and colour name the two views render, read out of the binary's
read-only data at the addresses the decompilation points at.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the task detail's separator** | a blank line | a forty-cell `─` rule |
| **the subagent detail's header** | two lines: `Agent(sub-2)`, `ID: [sub-2]` | five lines: `main ▸ …`, the elapsed time, the steps line, `ID: …`, a rule |
| **the id line** | `ID: [sub-2]` — the string table's literal | `ID: sub-2` — the reference's four-byte prefix |
| **the elapsed line** | none | `worked 12s` when the agent is done, the bare `12s` while it runs |
| **the subagent's state word** | the row's `completed` / `failed` | the subagent view's own `running` / `done` / `error` / `killed` |
| **the state word's colour** | none | ANSI-256 `196` / `34` / `220`, the reference's own picks |
| **the subagent body's indent** | flush left | two cells; the tool list's items four |
| **the header's depth** | not accounted for | the body window shrinks by the header's rows |

---

## 1. The task detail's rule

【反编译】 `model.TaskDetailModel.buildTaskDetailLines` (0x102bbbaa0)
emits, in order:

```
Command: <command>
Status:  <status>
Type:    <type>
────────────────────────────────────────   ← strings.Repeat("─", 0x28)
Output
<the captured output, one line per row>      (or "(no output)")
Started: <2006-01-02 15:04:05>
```

The rule is **forty cells and a constant** — the decompilation passes the
literal `0x28` to `strings.Repeat`, and the rule cell is the one-byte
string at 0x10338b3f6 (`─`, U+2500). It is *not* the panel width, and it
carries no indent: the rule is concatenated with the one-byte `"\n"` at
0x103d91ed8, not with the two-byte `"  "` at 0x103389194.

The task detail's heading is a separate function —
`model.TaskDetailModel.RenderHeader` (0x102bbb850) renders the styled
`Task Details` (0x10343ae6f) and a newline. The clone's panel title plays
that role.

**What the clone changed:** `internal/taskregistry/detail.go` replaces the
blank line with `strings.Repeat("─", 40)`.

---

## 2. The subagent detail's header

【反编译】 `model.SubagentDetailModel.buildHeaderLines` (0x102bb3ed0)
renders **five** lines:

```
main ▸ <title> [▸ <parent>]
<elapsed>
  <state> <name> · <kind> · <N> steps
ID: <id>
  ─────────────────────────────
```

Each line, and where it comes from:

| line | the reference's literal | address |
|---|---|---|
| 1 | `main ▸ ` + the agent's title, then ` ▸ ` + each non-empty parent | 0x1033ebf08 / 0x10339ce0c |
| 2 | `worked %s` once the agent is done; the bare duration while it runs | 0x1033ebf11 |
| 3 | `%s %s · %s · %d steps`, prefixed with two cells | 0x103516c0f |
| 4 | `ID: ` + the id — **no brackets** | the first four bytes of 0x103392681 |
| 5 | `─` repeated `min(60, width-4)` times, prefixed with two cells | 0x10338b3f6 |

Two details worth pinning down:

- **The brackets are gone.** The binary carries the literal `ID: [%s]`
  (0x103392681), but the header only uses its first four bytes — the
  reference's subagent header shows `ID: sub-12345`, not
  `ID: [sub-12345]`. Cycle 315's header copied the string table and
  therefore showed the brackets.
- **The rule follows the width.** The decompilation computes `0x3c` (60)
  and clamps it to the model's width minus four — so a 24-cell panel gets
  a twenty-cell rule. The task detail's rule does not: it is the fixed
  forty cells above.

The parent chain: the reference walks a two-entry slice of parent names
and appends ` ▸ <parent>` for each non-empty one. Every subagent the
clone starts is a child of the main agent, so the clone's header reads
`main ▸ <name>` and `SubagentHeaderContext.Parent` carries the extra
link when one is known.

---

## 3. The subagent view's own state vocabulary

The subagent detail does **not** use `model.taskStatus()`'s words.
【反编译】 `buildHeaderLines` compares the agent's status against four
literals of its own:

| the agent's status | the colour the reference paints it with |
|---|---|
| `error` | `196` (0x10338b444) |
| `killed` | `196` (0x10338b444) |
| `done` | `34` (0x103389540) |
| anything else (`running`) | `220` (0x10338b441) |

The colour names are handed to `lipgloss.Color`, so they render as the
**ANSI-256** palette — `\x1b[38;5;196m`, `\x1b[38;5;34m` and
`\x1b[38;5;220m` — not as the 24-bit RGB escapes the rest of the clone's
palette carries. The three escapes live in `internal/palette` as
`AgentErrorOpen`, `AgentDoneOpen` and `AgentActiveOpen`.

The clone maps its registry states onto that vocabulary:
`pending`/`running` → `running`, `done` → `done`, `failed` → `error`,
`killed`/`removed` → `killed`.

The binary also carries a three-cell spinner set (`●◉◐`, 0x10338b3f9) in
the same function — the running agent's own glyph, most likely. The clone
keeps the panel's existing spinner instead; see 存疑.

---

## 4. The body's indents

【反编译】 `model.SubagentDetailModel.buildContentLines` (0x102bb4710)
renders the prompt, the tools and the answer, and every line of it is
prefixed with the two-byte string at 0x103389194 (`"  "`) — the section
headings (`Prompt` 0x1033aa963, `Tools` 0x10339d492, `Answer`
0x1033ab4d9) as well as their content and the empty forms (`(no prompt
available)` 0x1034ef424, `No tools used yet` 0x1034a866f).

The tool list's *items* are prefixed one level deeper, with the four-byte
string at 0x1033921e5 (`"    "`).

So the reference's subagent body is:

```
  Prompt
  <the prompt>
  Tools
    <tool name>
  Answer
  <the answer>
```

The task detail's body, by contrast, is flush left — its labels carry
no indent at all. The two views do not share a layout, and the clone now
follows both.

---

## 5. What the clone changed

| file | change |
|---|---|
| `internal/taskregistry/detail.go` | the task detail's forty-cell rule; the subagent detail's five-line header (`SubagentDetailHeader`, `SubagentHeaderContext`); the state vocabulary (`SubagentStateWord`, `SubagentStateColor`); the body's two- and four-cell indents; the elapsed line (`worked %s` vs the bare duration) |
| `internal/palette/palette.go` | the three ANSI-256 agent-status colours |
| `internal/cliapp/slash_tasks_detail.go` | the panel passes the width into the header; the body's visible-row count now subtracts the header's depth, so the subagent detail's five-row header leaves fewer rows for the body; the scroll pages by the same amount |
| `internal/taskregistry/detail_test.go` | new — the rule, the header's five lines, the rule's width, the parent link, the running form, the vocabulary and the colours, the body's indents and its empty forms |
| `internal/cliapp/slash_tasks_detail_test.go` | the subagent detail's expectations replaced with the reference's shape |

The rendered result:

```
main ▸ review the diff
worked 12s
  done review the diff · subagent · 2 steps
ID: sub-2
  ────────────────────────────────────────────────────────────
  Prompt
  review the diff please

  Tools
    read_file
    run_command

  Answer
  the answer
```

---

## 6. 硬约束 / the load-bearing rules

- **The task detail's rule is forty cells, always.** `strings.Repeat("─",
  0x28)` — a constant. Do not make it follow the width; the subagent
  detail's rule is the one that follows the width.
- **The subagent detail's rule is `min(60, width-4)`.** Sixty is the cap;
  a narrow panel gets `width-4`.
- **`ID: ` has no brackets.** The `ID: [%s]` literal in the binary is
  not what the header renders.
- **The subagent detail's state word is not `model.taskStatus()`'s.**
  `running` / `done` / `error` / `killed` — four words, with their own
  colours. `completed` / `failed` / `cancelled` belong to the `/tasks`
  row and the *task* detail, not to this view.
- **The body indent is two cells, the tool items four.** The task
  detail's body is flush left.
- **The header's depth is chrome.** The body window is
  `height - 6 - (headerRows - 1)`; the subagent detail's five-row header
  leaves four rows fewer than the task detail's one.

---

## 7. 存疑 / open questions

- **【存疑】 The three words of the steps line.** The reference's format
  is `%s %s · %s · %d steps` — three strings and a count. The
  decompilation shows the first argument is a *styled* string (the state
  word in the state word's colour) and that the third comes out of the
  model at offset `0x68`, but which field of the agent record that is was
  not recovered. The clone renders the state word, the agent's name, the
  task's kind and the number of tool calls the subagent made — the
  count is the clone's proxy for the reference's step count.
- **【存疑】 `Agent(%s)`.** The literal (0x1033ebfd7) is in the binary
  but no decompiled function references it. It is probably the `/tasks`
  *row*'s label for an agent task, not part of the detail view — cycle
  315's header used it and was wrong.
- **【存疑】 The doubled rule.** `buildHeaderLines` ends with a rule and
  `buildContentLines` starts with one — the same `min(60, width-4)`
  length in both. Drawing both would put two rules next to each other,
  so the clone draws the header's one and does not repeat it in the
  body. Which of the two the reference actually shows was not settled.
- **【存疑】 The spinner.** `buildHeaderLines` stores the three-cell set
  `●◉◐` (0x10338b3f9) next to the state word, so the running agent's
  line probably shows a spinner frame rather than a word. The clone
  shows the word.
- **【存疑】 The `Type:` axis.** The reference's `Type:` value is `Agent`
  or `User` — the task's *origin*. The clone renders the task's *kind*
  (`shell`, `subagent`) there; every task the clone registers is
  model-spawned, so the origin would be constant.

---

## 8. Limitations

- **The trajectory section is still missing.** The reference's subagent
  detail lists the subagent's steps (`Trajectory — %s (%d steps)`,
  0x103555f98, and `No steps yet`, 0x10343ac6b). The clone's registry
  does not keep the step list, so the section is not rendered and the
  step count is a proxy.
- **The approval flow is still missing.** The reference's subagent detail
  can ask the user to approve or reject a tool call (`⚠ Approval
  Required`, `Yes, approve`, `No, deny`).
- **The row layout is not part of this cycle.** `model.TasksModel.View`
  (0x102bbe370) renders a row with the format `%s%s %s %s%s%s`
  (0x10346a9b8) and a bracketed timestamp (`[%s]` 0x103392685 over
  `15:04:05` 0x1033d4327). The clone's `/tasks` row does not reproduce
  that yet; it is the next cycle's subject.
- **The `Type:` column** keeps the clone's own axis (see 存疑).

---

## 9. The files

### The binary

`~/.local/bin/agy` — Antigravity CLI 1.4.7, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`.

### The extraction

`analyzeHeadless` with `DecompileAt.java`, run from `/tmp` (Ghidra
searches the CWD for scripts before `-scriptPath`):

```bash
cd /tmp && GHIDRA_INSTALL_DIR=~/tools/ghidra_12.1.4_PUBLIC \
  JAVA_HOME=/opt/homebrew/opt/openjdk@21 MAXMEM=8G \
  ~/tools/ghidra_12.1.4_PUBLIC/support/analyzeHeadless \
  out/local-macos/proj agy -process -noanalysis -max-cpu 4 \
  -scriptPath /tmp/ghscripts -postScript DecompileAt.java \
  0x102bbb850 0x102bbe370 0x102bb1bf0 0x102bb1c00 0x102bb0880 0x102bbcd60
```

The functions this cycle is based on:

| address | function |
|---|---|
| 0x102bbb850 | `model.TaskDetailModel.RenderHeader` |
| 0x102bbbaa0 | `model.TaskDetailModel.buildTaskDetailLines` |
| 0x102bb3ed0 | `model.SubagentDetailModel.buildHeaderLines` |
| 0x102bb4710 | `model.SubagentDetailModel.buildContentLines` |
| 0x102bbe370 | `model.TasksModel.View` |
