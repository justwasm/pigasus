# The task record's shape — deliverable

**Cycle:** `cycle-324-tui-task-record-type`
**Subject:** the record the reference's task row renders — its type, its
eleven fields, and the four-way rule that picks the row's word and the
marker's colour.
**Evidence:** 【一手】 the Go type metadata read out of the binary — the
struct descriptor, its field names and its offsets; 【反编译】
`render.StatusMarker` (0x10289fe70) and `render.RenderTaskItem`
(0x1028a0d60) read instruction by instruction; 【一手】 the protobuf
descriptor that carries the `CortexStepStatus` enum.

This cycle closes the last two open questions the task row carried:

- cycle 320, §8: "**【存疑】 The record's shape.** The row reads a byte
  at +0x18, a byte at +0x30, a byte at +0x41 and an int32 at +0x44 of
  the record it is given; the record's type was not found."
- cycle 320, §8: "**【存疑】 The flag at +0x40.**" — that flag is
  `NoExitCode`.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the record** | an unnamed blob with five offsets read out of the disassembly | `types.BackgroundTaskInfo` — eleven fields, every offset accounted for |
| **the "exit code 6" rule** | "the int32 at +0x44 is 6" — read as a process exit code | the record's `Status` field, and **6 is `CORTEX_STEP_STATUS_CANCELED`** |
| **the cancelled rule** | "the byte at +0x41 has its low bit set" | `IsAgentTask` **and** `Status == CORTEX_STEP_STATUS_CANCELED` |
| **the flag at +0x40** | unnamed | `NoExitCode` — it suppresses the "(exit N)" suffix |
| **the clone** | the row's rule was documented in raw offsets | the same rule, named — **no rendered byte changed** |

---

## 1. How the record was found

【一手】 the reference is a Go binary, so the shape of every type it
uses is in its type metadata — `__go_type`, 0x1086f91b0–0x1098f8420 in
this binary. Three facts about that metadata are worth writing down,
because the first two are counter-intuitive and cost the most time:

- **A type descriptor** (`abi.Type`) is 48 bytes: the size at +0, the
  pointer size at +8, the hash at +16, the flags at +20, the **kind at
  +23**, the equality function at +24, the GC data at +32, the name at
  +40 and the pointer-to-this type at +44. A **struct** type continues
  with the package path at +48 and the **field slice at +56 (pointer),
  +64 (length)**; each field is 24 bytes — name at +0, type at +8,
  offset at +16.
- **The two name offsets have different bases.** A *type*'s name offset
  is relative to the **types section** (0x1086f91b0); a *field*'s name
  offset is relative to the **image base** (0x100000000). Reading a
  field name with the types-section base gives an address past the end
  of the file, which is why the first scan found nothing.
- **The name blob** is `[flags][uvarint length][bytes]`, and the flags
  byte's low bit marks an exported name. A descriptor's name in this
  binary often reads with a leading `*` even when the descriptor is the
  value type — the struct `types.BackgroundTaskInfo` carries the blob
  `*types.BackgroundTaskInfo`, and the pointer type (kind 22) shares
  it. The *fields* are the reliable part.

With that, every struct type in the binary can be listed and matched
by its shape. 25,049 struct types carry a readable name; one of them
matches the row's five offsets exactly.

```bash
# the scan, in outline
for t in range(TYPES_BASE, TYPES_END, 8):          # every 8-aligned
    if kind(t) != 25: continue                    # struct type?
    for f in fields(t):                           # 24-byte entries
        offset, name, typ = f[16], field_name(f[0]), f[8]
```

---

## 2. `types.BackgroundTaskInfo` — the eleven fields

【一手】 the descriptor is at **0x1096f9d00**: kind 25 (struct), 120
bytes. The package is `google3/third_party/jetski/cli/types` — the same
package that holds `types.ColorScheme` and `types.SchemeColors`
(cycle 323).

| offset | field | type | what the row does with it |
|---|---|---|---|
| +0x00 | `Index` | `int` | — |
| +0x08 | `Command` | `string` | — |
| +0x18 | `Running` | `bool` | the running branch |
| +0x20 | `Result` | `string` | — |
| +0x30 | `IsError` | `bool` | the failed branch |
| +0x38 | `ExitCode` | `int` | the "(exit N)" suffix |
| +0x40 | `NoExitCode` | `bool` | suppresses that suffix |
| +0x41 | `IsAgentTask` | `bool` | the cancelled branch |
| +0x44 | `Status` | `cortex_go_proto.CortexStepStatus` (int32) | the cancelled branch |
| +0x48 | `StartTime` | `time.Time` | the row's time cell |
| +0x60 | `LastUpdatedTime` | `time.Time` | — |

The pointer type is at 0x108a61260 and the slice type
(`[]types.BackgroundTaskInfo`, the shape the model's store holds) at
0x108af6de8.

Every offset the row reads is now named: +0x18 is `Running`, +0x30 is
`IsError`, +0x40 is `NoExitCode`, +0x41 is `IsAgentTask`, +0x44 is
`Status`, +0x48 is `StartTime`'s wall word and +0x50 its ext word.

---

## 3. The four-way rule

【反编译】 both renderers run the same rule, on the same four fields,
and they reach the same four styles. `render.RenderTaskItem`
(0x1028a0d60) reads the record through its pointer (`x2`):

```asm
0x1028a0f1c: ldrb   w5, [x2, #0x18]      ; Running
0x1028a0f20: tbz    w5, #0x0, 0x1028a0f34
                                       ; → "running",  ToolRunning
0x1028a0f34: ldrb   w5, [x2, #0x41]      ; IsAgentTask
0x1028a0f38: tbz    w5, #0x0, 0x1028a0f58
0x1028a0f3c: ldrsw  x5, [x2, #0x44]      ; Status
0x1028a0f40: cmp    w5, #0x6            ; CORTEX_STEP_STATUS_CANCELED
0x1028a0f44: b.ne   0x1028a0f58
                                       ; → "cancelled", ToolCanceled
0x1028a0f58: ldrb   w5, [x2, #0x30]      ; IsError
0x1028a0f5c: tbz    w5, #0x0, 0x1028a1018
                                       ; → "failed",    ToolError
0x1028a1018: …                         ; → "completed", ToolDone
```

`render.StatusMarker` (0x10289fe70) runs the identical rule — it takes
the record **by value** (all three call sites copy the 120 bytes into
its argument area) and picks the same four styles, in the same order,
at the same offsets inside `render.Styles`:

| branch | word | style | the style's offset in `render.Styles` |
|---|---|---|---|
| running | `running` (0x1033bc1d5) | `ToolRunning` | +0x83d0 |
| cancelled | `cancelled` (0x1033eac42) | `ToolCanceled` | +0x8658 |
| failed | `failed` (0x1033aa66f) | `ToolError` | +0x88e0 |
| done | `completed` (0x1033eb2ba) | `ToolDone` | +0x8148 |

The marker itself is the three bytes at 0x10338b3f9 — `●` — and it is
rendered with `lipgloss.Style.Render` (0x1026614e0).

**So the marker and the status word always carry the same colour** —
which is what the clone already does.

---

## 4. The exit code

【反编译】 the failed and the completed branches build a suffix:

```asm
0x1028a0f60: ldrb   w5, [x2, #0x40]      ; NoExitCode
0x1028a0f94: tbz    w5, #0x0, 0x1028a0fa4
                                       ; NoExitCode → no suffix at all
0x1028a0fa4: …                        ; else: fmt.Sprintf(" (exit %d)", ExitCode)
0x1028a0ff8: … "failed" + the suffix
```

The value formatted is the record's `ExitCode` at +0x38, and the
format is the ten bytes at 0x1034051bd — `" (exit %d)"`. The word and
the suffix are joined by `runtime.concatstring2` (0x10006c1e0).

So the reference's row reads:

```
completed (exit 0)
failed (exit 1)
```

and the two other branches carry no suffix at all — a running and a
cancelled task never show an exit code.

The clone already does exactly this: `taskregistry.TaskStatus` appends
`" (exit %d)"` to the completed and the failed word, and
`taskregistry.ExitCodeUnset` is the clone's name for the reference's
`NoExitCode`.

---

## 5. `CortexStepStatus` — and what "6" is

【一手】 the enum lives in the reference's protobuf descriptor, and the
`EnumValueDescriptorProto` entries carry the name and the number:

| value | number |
|---|---|
| `CORTEX_STEP_STATUS_UNSPECIFIED` | 0 |
| `CORTEX_STEP_STATUS_PENDING` | 1 |
| `CORTEX_STEP_STATUS_RUNNING` | 2 |
| `CORTEX_STEP_STATUS_DONE` | 3 |
| `CORTEX_STEP_STATUS_INVALID` | 4 |
| `CORTEX_STEP_STATUS_CLEARED` | 5 |
| **`CORTEX_STEP_STATUS_CANCELED`** | **6** |
| `CORTEX_STEP_STATUS_ERROR` | 7 |
| `CORTEX_STEP_STATUS_GENERATING` | 8 |
| `CORTEX_STEP_STATUS_WAITING` | 9 |
| `CORTEX_STEP_STATUS_QUEUED` | 11 |
| `CORTEX_STEP_STATUS_INTERRUPTED` | 12 |

**Ten is missing from the table on purpose** — it is a reserved range,
and `CORTEX_STEP_STATUS_HALTED` is a reserved *name*, not a value.
See cycle 325, §1.

**The "exit code 6" of the earlier cycles is not an exit code at all:**
it is the record's step status, and 6 is `CORTEX_STEP_STATUS_CANCELED`.

The cancelled branch therefore reads: *this step is an agent task, and
the agent task's step was cancelled.* The reference's own prompt
templates use the same vocabulary — the binary carries
`{{- if eq $statusStr "CORTEX_STEP_STATUS_CANCELED" }}` and a dozen
sibling conditions.

---

## 6. What the clone changed

| file | change |
|---|---|
| `internal/taskregistry/format.go` | `StateStyle`'s doc comment names the four fields instead of quoting raw offsets; `statusParts`'s doc comment names `ExitCode` and `NoExitCode` |

**No rendered byte changed.** The clone's row already rendered the
reference's four words, the "(exit N)" suffix and the same rule for
which word a task gets; only the *documentation* of that rule was
written in offsets instead of names.

---

## 7. 硬约束 / the load-bearing rules

- **The record is `types.BackgroundTaskInfo`** — 120 bytes, eleven
  fields, in the order of §2.
- **The row's rule is four-way**, in this order: `Running` →
  running; `IsAgentTask` **and** `Status == CORTEX_STEP_STATUS_CANCELED`
  → cancelled; `IsError` → failed; everything else → completed.
- **The marker and the status word pick the same style** — the marker
  is `●` in the style, the word is the word in the same style.
- **The exit code is shown only for the completed and the failed word**,
  and only when `NoExitCode` is clear.
- **Six is `CORTEX_STEP_STATUS_CANCELED`** — never read it as a process
  exit code.
- **A pending task is a running one** — the reference has no fifth
  word.

---

## 8. 存疑 / open questions

> **Resolved by cycle 325**
> (`docs/cycles/cycle-325-tui-task-record-producer/`). The questions
> below were open when this cycle was written; the answers are in the
> newer report, which wins.

- **【存疑】 `CORTEX_STEP_STATUS_HALTED`'s number** was not recovered
  (the descriptor's value entry has its number field in a different
  place than the other twelve), and 10 is unused in the enum. The
  value does not affect the row.
  → **cycle 325: it is not a value.** `CORTEX_STEP_STATUS_HALTED` is a
  **reserved name** (the enum descriptor's field 5) and 10 is a
  **reserved range** (field 4, `start = 10`, `end = 10`).
- **【存疑】 What `Index` (+0x00) is for.** The row does not read it.
  → **cycle 325: the record's index in the *source* list it was
  projected from** — the loop counter of the projection, incremented for
  every source record including the ones the filter skips.
- **【存疑】 `Command` (+0x08) and `Result` (+0x20)** are not rendered by
  this row: the row never reads them. `Result` is copied into the
  marker's argument and never read there either. The row's name comes
  from another argument.
  → **cycle 325: the row's name *is* the record's `Command`** —
  `model.ActiveItemsModel.getActiveItems` copies it into
  `model.activeItem.name`, and the renderer takes that as its separate
  argument. `Result` is the command's output text; every producer
  copies it in and no rule of the row reads it.
- **【存疑】 Who produces the records.** The model's store holds a
  `[]types.BackgroundTaskInfo`, but the code that fills it was not
  traced — only the consumer was.
  → **cycle 325: three producers**, traced end to end —
  `store.(*TaskManager).GetBackgroundTasks`, `model.activeTasks` and
  `model.agentTaskInfo`.
- **【存疑】 Whether `IsAgentTask` is what the cancelled rule really
  means.** The name and the rule fit, but the reference's own producer
  was not read.
  → **cycle 325: it is.** `model.agentTaskInfo` builds the record with
  `IsAgentTask` true — from the static composite literal it copies —
  and `Status` set to the step's own status, so the cancelled branch is
  live on the agent path, and only there.

---

## 9. Limitations

- **Only the consumer was read.** The record's producer — the store
  that fills `[]types.BackgroundTaskInfo` — was not traced, so the
  meaning of `Command`, `Result` and `Index` stays open.
  → **cycle 325 traced all three producers**; see
  `docs/cycles/cycle-325-tui-task-record-producer/`.
- **The row's name cell was not re-derived** in this cycle; cycle 320's
  reading stands.
  → **cycle 325: the name cell is the record's `Command`**, copied into
  `model.activeItem.name` by `getActiveItems`.
- **The `LastUpdatedTime` field (+0x60) is unused by this row.**
- **The enum is the reference's own vocabulary**, not the clone's: the
  clone's task registry has its own states, and the two are mapped by
  hand in `taskregistry.StateWord`.

---

## 10. The files

### The binary

`~/.local/bin/agy` — Antigravity CLI 1.4.7, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`.
The file offset of an address is the address minus 0x100000000.

### The functions this cycle is based on

| address | function |
|---|---|
| 0x10289fe70 | `render.StatusMarker` — the marker |
| 0x1028a0d60 | `render.RenderTaskItem` — the row |
| 0x1028daef0 | `render.resolveStyles` — the style set (cycle 323) |
| 0x102657610 | `lipgloss.Color` |
| 0x1026614e0 | `lipgloss.Style.Render` |
| 0x10006c1e0 | `runtime.concatstring2` |
| 0x10011d5f0 | `fmt.Sprintf` |

### The type descriptors this cycle is based on

| address | type |
|---|---|
| 0x1096f9d00 | `types.BackgroundTaskInfo` — the struct, 120 bytes |
| 0x108a61260 | `*types.BackgroundTaskInfo` |
| 0x108af6de8 | `[]types.BackgroundTaskInfo` |
| 0x108f3ece0 | `types.ColorScheme` — a string |
| 0x108c51e40 | `map[types.ColorScheme]types.SchemeColors` (cycle 323) |
| 0x1086f91b0 | the types section — the base of the type name offsets |

### The literals this cycle is based on

| address | literal | length |
|---|---|---|
| 0x1033bc1d5 | `running` | 7 |
| 0x1033eac42 | `cancelled` | 9 |
| 0x1033aa66f | `failed` | 6 |
| 0x1033eb2ba | `completed` | 9 |
| 0x1034051bd | `" (exit %d)"` | 10 |
| 0x10338b3f9 | `●` — the marker | 3 |
