# The subagent detail's trajectory section — deliverable

**Cycle:** `cycle-317-tui-subagent-trajectory`
**Subject:** the fourth section of the reference's subagent detail — the
agent's **trajectory**, the steps it took while it ran. Cycle 316 listed
it as a missing piece ("the reference's subagent detail lists the
subagent's steps (`Trajectory — %s (%d steps)`)"); this cycle renders it,
and replaces the header's step-count *proxy* with the real count.
**Evidence:** 【反编译】 `model.SubagentDetailModel.buildContentLines`
(0x102bb4710) and `model.SubagentDetailModel.buildHeaderLines`
(0x102bb3ed0) decompiled from the reference `agy` 1.4.7 binary;
【strings】 every literal the section renders, read out of the binary's
read-only data at the addresses the decompilation points at; 【反编译】
the *order* of the sections inside `buildContentLines`, recovered with
the ADRP+ADD reference scanner this cycle added
(`devutils/macho_strrefs.py`).

---

## 0. Summary

| | before | after |
|---|---|---|
| **the subagent detail's sections** | three — prompt, tools, answer | four — prompt, tools, answer, **trajectory** |
| **the trajectory's heading** | none | `Trajectory — <name> (<N> steps)` |
| **the trajectory's empty form** | none | `No steps yet` |
| **the trajectory's items** | none | numbered rows, one per step, indented four cells |
| **the header's step count** | the number of tool calls (a proxy) | the number of trajectory steps — the same number the trajectory section shows |

---

## 1. The section's shape

【反编译】 `model.SubagentDetailModel.buildContentLines` (0x102bb4710)
renders, after the answer:

```
  Trajectory — <name> (<N> steps)
  No steps yet                              ← only when there are none
```

and the heading is built by

```c
iVar26 = *(int *)(puVar25 + 0xe2f0);
if (*(int *)(puVar25 + 0xe2f0) <= *(int *)(puVar25 + 0xe2f8)) {
  iVar26 = *(int *)(puVar25 + 0xe2f8);       /* max of two counters */
}
auVar46 = fmt_Sprintf(&UNK_103555f98, 0x1c, …);   /* "Trajectory — %s (%d steps)" */
```

Three details fall out of that:

- **The literal is 28 bytes** (`0x1c`) — `Trajectory — %s (%d steps)` with
  an em dash (U+2014), and it is referenced **exactly once** in the whole
  binary (0x102bb61d0, inside `buildContentLines`). It is the reference's
  own string, not a reconstruction.
- **The count is `max(a, b)`** — the model's own step count and the
  counter the function keeps while it walks the steps. The same `max` of
  two counters appears in the *header* (`buildHeaderLines`, at
  `max(*(0x350), *(0x358))`), so the header's `· %d steps` and the
  trajectory's `(%d steps)` are the same number.
- **The empty form is a separate literal.** `No steps yet`
  (0x10343ac6b, 12 bytes) is referenced at +0x1cd8 of the same function,
  right after the heading — the section renders the heading and then, when
  the list is empty, that line instead of the items.

The heading carries the two-cell body indent (the `"  "` at 0x103389194),
the same indent the prompt and the tools sections carry; the items, like
the tool list, carry the four-cell one (0x1033921e5).

---

## 2. The order of the sections

The decompiler does not preserve the source order of a function's
blocks, so the section order was recovered from the **code offsets** of
the literals instead: the ADRP+ADD scanner
(`devutils/macho_strrefs.py`, added this cycle) reports every code
address that materialises a literal's address, and the difference
between that address and the function's entry point is the offset inside
the function.

| offset in `buildContentLines` | literal | section |
|---|---|---|
| +0x2a8 | `Prompt` (0x1033aa963) | the prompt |
| +0x450 | `(no prompt available)` (0x1034ef424) | its empty form |
| +0x700 | `Tools` (0x10339d492) | the tools |
| +0xa14 | `No tools used yet)` (0x1034a866f) | its empty form |
| +0x17ec | `Yes, approve` (0x10343ae63) | the approval flow (not implemented) |
| +0x1804 | `No, deny` (0x1033d431f) | the approval flow (not implemented) |
| +0x1864 | `Answer` (0x1033ab4d9) | the answer |
| +0x187c | `Skip` (0x1033925e1) | the approval flow (not implemented) |
| **+0x1ac0** | **`Trajectory — %s (%d steps)`** | **the trajectory** |
| **+0x1cd8** | **`No steps yet`** | **its empty form** |
| +0x293c | `%d. %s` (0x1033aadf5) | the numbered rows |

So the trajectory is the **last** section, after the answer — the clone
renders it there.

---

## 3. The numbered rows

【反编译】 `%d. %s` (0x1033aadf5) is the binary's only numbered-list
format. Inside `buildContentLines` it is referenced at +0x293c — *after*
the trajectory's heading — and the decompilation shows it formatting an
index (`iVar24 + 1`) and a string:

```c
*(int *)(puVar25 + 0xe2f0) = iVar24 + 1;
uVar20 = runtime_convTstring(…);
auVar46 = fmt_Sprintf(&UNK_1033aadf5, 6, …);
```

The same literal is used by `model.ToolConfirmationModel.View`
(0x102bc789c), where it numbers the approval menu's choices — so the
*format* is shared between the approval menu and (very likely) the
trajectory's rows. The clone numbers its trajectory rows with it.

The reference's *step text* is the content of its own trajectory's
message; the clone's is the assistant's content block. See 存疑.

---

## 4. What the clone changed

| file | change |
|---|---|
| `internal/taskregistry/taskregistry.go` | `Task.Steps []string` — the subagent's steps, in order |
| `internal/subagenttools/session.go` | `usedSteps` — one step per assistant content block: a tool call by its name, a text block by its first line; filled into the registry when the run ends |
| `internal/taskregistry/detail.go` | the trajectory section (heading, empty form, numbered rows) and the header's step count, which now reads `len(t.Steps)` |
| `internal/taskregistry/detail_test.go` | new — the heading, the rows, the empty form, the section's order |
| `internal/subagenttools/session_steps_test.go` | new — `usedSteps` and `firstLine` |
| `internal/cliapp/slash_tasks_detail_test.go` | the subagent detail's expectations replaced with the trajectory section |
| `devutils/macho_strrefs.py` | new — the ADRP+ADD reference scanner |

The rendered result:

```
main ▸ review the diff
worked 12s
  done review the diff · subagent · 3 steps
ID: sub-2
  ────────────────────────────────────────────────────────────
  Prompt
  review the diff please

  Tools
    read_file
    run_command

  Answer
  the answer

  Trajectory — review the diff (3 steps)
    1. read_file
    2. run_command
    3. the answer
```

---

## 5. 硬约束 / the load-bearing rules

- **The trajectory is the last section.** The reference's order is
  prompt → tools → answer → trajectory, pinned by the literal offsets
  in §2.
- **The heading is `Trajectory — %s (%d steps)`** — an em dash, and the
  agent's name. The count is the number of steps.
- **The empty form is `No steps yet`**, not an empty section and not a
  `0 steps` heading alone.
- **The header's count and the trajectory's count are the same number.**
  Both are the reference's `max(model step count, walked counter)`; the
  clone reads both from `Task.Steps`.
- **The section's items are indented four cells, the heading two.** The
  same rule as the tool list.

---

## 6. 存疑 / open questions

- **【存疑】 The step text.** The reference's step is a message of its
  own trajectory (`Role`, `Content`, `ToolName`, `ToolArgs` — the shape
  the clone's `internal/cortex` already carries). The clone's step is an
  assistant content block: a tool call by its name, a text block by its
  first line. The reference's exact rendering of a step was not
  recovered.
- **【存疑】 The numbered rows.** `%d. %s` is the only numbered-list
  format in the function and sits after the heading, but the reference
  may format the trajectory's rows differently — the same literal
  numbers the approval menu's choices.
- **【存疑】 The three words of the header's steps line** (unchanged from
  cycle 316): the reference's format is `%s %s · %s · %d steps`; which
  agent field fills each of the three words was not recovered. The
  clone renders the state word, the name, the kind and the step count.
- **【存疑】 The approval flow.** `Yes, approve` / `No, deny` / `Skip`
  live in the same function (offsets +0x17ec … +0x187c) and are not
  implemented.

---

## 7. Limitations

- **The clone fills the step list when the run ends.** The reference's
  model keeps the count live — its trajectory grows while the agent
  works — and a running subagent in the reference already shows a
  partial trajectory. The clone's PiG session hands the messages back
  when the run settles, so a running subagent shows `No steps yet` and
  `0 steps` in the header.
- **The step text is the clone's own.** See 存疑.
- **The `/tasks` row layout is still not reproduced.** This cycle
  reached the reference's row machinery and mapped it, but could not
  assemble it: `model.TasksModel.View` (0x102bbe370) formats a row with
  `%s%s %s %s%s%s` (0x10346a9b8) — six arguments whose pairing the
  decompiler's argument slots do not resolve. The pieces are known
  (`render.RenderTaskItem` 0x1028a0d60 draws the *ActiveItems* row: a
  status marker, the status word, a bracketed `15:04:05` timestamp or
  `n/a`, and the truncated command; `render.RenderSubagentItem`
  0x1028a0a60 draws an agent row from `render.GetSubagentRenderInfo`
  0x1028a0220, whose vocabulary is `Agent(<id>)`, `Processing...`,
  `Done`, `Error: <msg>`, `Killed · <reason>`, `Blocked · <reason>`,
  ` · <n>` while running and ` · worked <n>` when done). Cycle 318
  mapped the row's own pieces and gave the row the reference's status
  cell; the layout is still the next cycle's subject.
  Report: `docs/cycles/cycle-318-tui-tasks-row-status/`.
- **The approval flow and the "User Backgrounded" badge are still
  missing** from the subagent detail.

---

## 8. The files

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
  0x102bb4710 0x102bb3ed0 0x102bbe370 0x1028a0220 0x1028a0a60 0x1028a0d60
```

### The functions this cycle is based on

| address | function |
|---|---|
| 0x102bb4710 | `model.SubagentDetailModel.buildContentLines` |
| 0x102bb3ed0 | `model.SubagentDetailModel.buildHeaderLines` |
| 0x102bbe370 | `model.TasksModel.View` (the row — next cycle) |
| 0x1028a0220 | `render.GetSubagentRenderInfo` |
| 0x1028a0a60 | `render.RenderSubagentItem` |
| 0x1028a0d60 | `render.RenderTaskItem` |

### The literals this cycle is based on

| address | literal | references |
|---|---|---|
| 0x103555f98 | `Trajectory — %s (%d steps)` | 0x102bb61d0 (`buildContentLines` +0x1ac0) |
| 0x10343ac6b | `No steps yet` | 0x102bb63e8 (+0x1cd8) |
| 0x1033aadf5 | `%d. %s` | 0x102bb704c (+0x293c), 0x102bc789c (`ToolConfirmationModel.View`) |
| 0x1033aa963 | `Prompt` | 0x102bb49b8 (+0x2a8) |
| 0x10339d492 | `Tools` | 0x102bb4e10 (+0x700) |
| 0x1033ab4d9 | `Answer` | 0x102bb5f74 (+0x1864) |
| 0x10343ae63 | `Yes, approve` | 0x102bb5efc (+0x17ec) |
| 0x1033d431f | `No, deny` | 0x102bb5f14 (+0x1804) |
| 0x1033925e1 | `Skip` | 0x102bb5f8c (+0x187c) |

### The tool

`devutils/macho_strrefs.py` — the ADRP+ADD reference scanner. It reads
the executable segment of the Mach-O image and reports every code
address that materialises a literal's address:

```bash
python3 devutils/macho_strrefs.py 0x103555f98 0x10343ac6b 0x1033aadf5
```

```
0x1033aadf5 6 ['0x1012ff7dc', '0x101315ef0', '0x102842f78', '0x10284350c', '0x102bb704c', '0x102bc789c']
0x10343ac6b 2 ['0x102aabc24', '0x102bb63e8']
0x103555f98 1 ['0x102bb61d0']
```
