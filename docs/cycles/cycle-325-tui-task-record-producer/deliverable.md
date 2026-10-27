# The record's producer, and the enum's reserved name — deliverable

**Cycle:** `cycle-325-tui-task-record-producer`
**Subject:** who fills `types.BackgroundTaskInfo` — the store's projection,
the model's own projection, and the third producer that only the agent
steps reach; the name the row actually renders; and
`CORTEX_STEP_STATUS_HALTED`, which is a **reserved name**, not a value.
**Evidence:** 【一手】 the Go type metadata of `types.CommandOutput`,
`types.BackgroundTaskInfo` and `model.activeItem`; 【一手】 the producers
read instruction by instruction out of the disassembly; 【一手】 the
protobuf descriptor that carries the `CortexStepStatus` enum, parsed byte
by byte; 【一手】 the static composite literal in `__rodata` that the
agent producer copies into its record.

This cycle closes the last two open questions the task record carried:

- cycle 324, §8: "**【存疑】 `CORTEX_STEP_STATUS_HALTED`'s number** was
  not recovered … and 10 is unused in the enum." — **10 is not unused: it
  is reserved, and so is the name `CORTEX_STEP_STATUS_HALTED`.**
- cycle 324, §8: "**【存疑】 Who produces the records.** The model's
  store holds a `[]types.BackgroundTaskInfo`, but the code that fills it
  was not traced — only the consumer was." — **three producers, traced
  end to end.**

---

## 0. Summary

| | before | after |
|---|---|---|
| **`CORTEX_STEP_STATUS_HALTED`** | an unrecovered enum value; 10 looked unused | **not a value at all** — a *reserved name*; the number **10** is a *reserved range* (10..10) |
| **who fills `[]types.BackgroundTaskInfo`** | "the store, but the code was not traced" | three producers: `store.(*TaskManager).GetBackgroundTasks`, `model.activeTasks`, `model.agentTaskInfo` |
| **the store's own record** | unknown | `types.CommandOutput` — 120 bytes, eleven fields |
| **`Command` (+0x08)** | "not rendered by this row" | **it is the row's name** — copied into `model.activeItem.name` |
| **`Result` (+0x20)** | "copied into the marker's argument and never read" | the command's output text; copied into the record by every producer, read by none of the row's rules |
| **`Index` (+0x00)** | "the row does not read it" | the record's index in the **source** list it was projected from |
| **`IsAgentTask` (+0x41)** | "possibly what the cancelled rule really means" | **true on every agent record** — set by a *bulk copy of a static literal*, not by a byte store |
| **`Status` (+0x44)** | "a step status, 6 is cancelled" | the step's own status — set by exactly one producer, `model.agentTaskInfo` |
| **the clone** | the row's rule documented in offsets | unchanged — **no rendered byte changed**; only the doc comments name the producer's fields |

---

## 1. `CORTEX_STEP_STATUS_HALTED` is a reserved name, not a value

【一手】 the enum is a protobuf `EnumDescriptorProto` inside the
reference's descriptor blob. It starts at **file offset 63900723**
(vaddr **0x103cf0c33**) and is 441 bytes long. Parsed field by field:

```
[1] name = "CortexStepStatus"                (file off 63900723)
[2] value CORTEX_STEP_STATUS_UNSPECIFIED  = 0
[2] value CORTEX_STEP_STATUS_GENERATING   = 8
[2] value CORTEX_STEP_STATUS_QUEUED       = 11
[2] value CORTEX_STEP_STATUS_PENDING      = 1
[2] value CORTEX_STEP_STATUS_RUNNING      = 2
[2] value CORTEX_STEP_STATUS_WAITING      = 9
[2] value CORTEX_STEP_STATUS_DONE         = 3
[2] value CORTEX_STEP_STATUS_INVALID      = 4
[2] value CORTEX_STEP_STATUS_CLEARED      = 5
[2] value CORTEX_STEP_STATUS_CANCELED     = 6
[2] value CORTEX_STEP_STATUS_ERROR        = 7
[2] value CORTEX_STEP_STATUS_INTERRUPTED  = 12
[4] reserved_range  start = 10  end = 10   (file off 63901131)
[5] reserved_name = "CORTEX_STEP_STATUS_HALTED"  (file off 63901137)
```

The raw bytes of the tail, at file offset 63901126:

```
54 45 44 10 0c 22 04 08 0a 10 0a 2a 19 43 4f 52 54 45 58 5f 53 54 45 50 5f 53 54 41 54 55 53 5f 48 41 4c 54 45 44
   "TED"   ^2 = 12   ^field 4 len 4 {1:10, 2:10}   ^field 5 len 25 "CORTEX_STEP_STATUS_HALTED"
```

So the answer to cycle 324's open question is **not a number**:

- **There is no `CORTEX_STEP_STATUS_HALTED` value.** The name sits in
  field 5, `reserved_name` — the list of names the enum has *given up*,
  kept so that a future value cannot reuse them.
- **The number 10 is reserved too** — field 4, `reserved_range`, with
  `start = 10` and `end = 10`, i.e. the inclusive range 10..10. Ten is
  the only gap in the numbering, and the descriptor says so explicitly.
- A `HALTED` status would therefore have to be **10** if the name were
  ever revived — but in 1.4.7 it is not a status at all, and the row's
  rule never compares against it.

The numbering the row actually uses is unaffected: 6 is
`CORTEX_STEP_STATUS_CANCELED`, 7 is `CORTEX_STEP_STATUS_ERROR`, and
both are read out of the same descriptor (§5).

**A note on the descriptor's order:** the twelve value entries are *not*
stored in numeric order (the blob goes UNSPECIFIED, GENERATING, QUEUED,
PENDING, RUNNING, WAITING, DONE, INVALID, CLEARED, CANCELED, ERROR,
INTERRUPTED). The numbers in the table above are the ones each entry
carries, not the position it sits at.

---

## 2. The store's own record: `types.CommandOutput`

【一手】 the thing the store holds is **not** the record the row renders.
The store's slice is `[]types.CommandOutput`, and the struct is at
descriptor **0x1096f9e68** — 120 bytes, eleven fields, in the package
`google3/third_party/jetski/cli/types`:

| offset | field | type |
|---|---|---|
| +0x00 | `Command` | string |
| +0x10 | `Result` | string |
| +0x20 | `Styled` | string |
| +0x30 | `BeforeStep` | int |
| +0x38 | `IsError` | bool |
| +0x39 | `Running` | bool |
| +0x3a | `Backgrounded` | bool |
| +0x3b | `Hidden` | bool |
| +0x40 | `ExitCode` | int |
| +0x48 | `StartTime` | time.Time |
| +0x60 | `LastUpdatedTime` | time.Time |

Two producers of *that* record are worth naming, because one of them is
the only thing in the binary that sets `Backgrounded`:

- `store.(*Manager).addCommandOutputWith` (0x10294fea0) builds the
  record in the store's slice. The record is assembled on the stack at
  `sp+0x178` (zeroed 0x102950130–0x102950150) and copied into the
  slice's last element at 0x1029501f4–0x102950218, with the write
  barrier at 0x1029501e4. The element's address is computed as
  `base + len*120 - 120` (0x1029501bc–0x1029501c8) — **the 120-byte
  stride of `types.CommandOutput`, confirmed by the arithmetic itself**.
- `store.(*TaskManager).SetShellCommandBackgrounded` (0x1029a1be0)
  marks a shell command as backgrounded:

  ```
  1029a1d6c: add  x4, x0, x4      ; x4 = &cmdOutputs[id]  (120-byte stride)
  1029a1d70: ldrb w5, [x4, #0x3b]  ; Hidden
  1029a1d74: orr  x6, xzr, #0x1
  1029a1d78: strb w6, [x4, #0x3a]  ; Backgrounded = true
  ```

  **The byte at +0x3a of a `types.CommandOutput` is `Backgrounded`** —
  confirmed by the descriptor and by this one store.

---

## 3. The projection — `store.(*TaskManager).GetBackgroundTasks`

【一手】 **this is the producer cycle 324 could not find.**
`store.(*TaskManager).GetBackgroundTasks` (0x1029a39b0) walks the
store's `[]types.CommandOutput` and returns `[]types.BackgroundTaskInfo`
— the element type descriptor at the append is 0x1096f9d00
(`adrp x4, 0x1096f9000` + `add x4, x4, #0xd00`, 0x1029a3b58).

The walk, instruction by instruction:

| address | what it does |
|---|---|
| 0x1029a3a88 | `add x2, x2, #0x1` — the **counter**, incremented for *every* source record, skipped ones included |
| 0x1029a3a98 | `ldrb w7, [x5, #0x3a]` — read `Backgrounded` |
| 0x1029a3a9c–0x1029a3ac4 | copy the 120-byte source record to the stack (`sp+0xa0`) |
| 0x1029a3ac8 | `tbz w7, #0x0` — **skip the record unless `Backgrounded`** |
| 0x1029a3adc–0x1029a3af8 | zero the 120-byte destination record (`sp+0x128`) |
| 0x1029a3afc | `str x2, [sp, #0x128]` — **`Index` = the counter** |

and then the field-wise projection (the destination is `sp+0x128`, the
source copy is `sp+0xa0`):

| destination | source | field | instruction |
|---|---|---|---|
| +0x00 | — | `Index` | `str x2,[sp,#0x128]` — **the index in the source list**, not in the result |
| +0x08 | +0x00 | `Command` | `ldp x9,x10,[sp,#0xa0]` → `stp [sp,#0x130]` |
| +0x18 | +0x39 | `Running` | `ldrb w9,[sp,#0xd9]` → `strb [sp,#0x140]` |
| +0x20 | +0x10 | `Result` | `ldp x9,x10,[sp,#0xb0]` → `stp [sp,#0x148]` |
| +0x30 | +0x38 | `IsError` | `ldrb w9,[sp,#0xd8]` → `strb [sp,#0x158]` |
| +0x38 | +0x40 | `ExitCode` | `ldr x9,[sp,#0xe0]` → `str [sp,#0x160]` |
| +0x48 | +0x48 | `StartTime` | `ldp x9,x10,[sp,#0xe8]` → `stp [sp,#0x170]` |
| +0x60 | +0x60 | `LastUpdatedTime` | `ldp x9,x10,[sp,#0x100]` → `stp [sp,#0x188]` |

**Three fields are left zero**: `NoExitCode` (+0x40), `IsAgentTask`
(+0x41) and `Status` (+0x44). The zero-fill covers `sp+0x128` through
`sp+0x1f8`, and the projection never touches those three bytes.

So, for a *shell command*: `Command` is the command text, `Result` is
its output, `Index` is the command's position in the store's command
list, and the record carries **no** step status at all.

---

## 4. The model's own projection — `model.activeTasks`

【一手】 `model.activeTasks` (0x102bc0160) builds the *same* view
straight out of the store's `[]types.CommandOutput` (the slice's
interface slot at +0x48 of the model), without going through the
store's method. Its projection is the same field-for-field:

```
102bc01fc: ldrb w7, [x1, #0x39]   ; Running
102bc0200: ldrb w8, [x1, #0x3a]   ; Backgrounded
...
102bc0230: tbz  w8, #0x0, skip    ; Backgrounded?
102bc0234: tbz  w7, #0x0, skip    ; Running?
```

— so this one keeps **both** `Backgrounded` *and* `Running`: the
"active" background tasks. The view is built at `sp+0x108` (zeroed at
0x102bc0270–0x102bc0290) and the same three fields (`NoExitCode`,
`IsAgentTask`, `Status`) are left zero:

| destination | source | field | instruction |
|---|---|---|---|
| +0x00 | — | `Index` | `str x3,[sp,#0x108]` |
| +0x08 | +0x00 | `Command` | `ldp x8,x9,[sp,#0x190]` → `stp [sp,#0x110]` |
| +0x18 | +0x39 | `Running` | `ldrb w8,[sp,#0x1c9]` → `strb [sp,#0x120]` |
| +0x20 | +0x10 | `Result` | `ldp x8,x9,[sp,#0x1a0]` → `stp [sp,#0x128]` |
| +0x30 | +0x38 | `IsError` | `ldrb w8,[sp,#0x1c8]` → `strb [sp,#0x138]` |
| +0x38 | +0x40 | `ExitCode` | `ldr x8,[sp,#0x1d0]` → `str [sp,#0x140]` |
| +0x48 | +0x48 | `StartTime` | `ldp x8,x9,[sp,#0x1d8]` → `stp [sp,#0x150]` |
| +0x60 | +0x60 | `LastUpdatedTime` | `ldp x8,x9,[sp,#0x1f0]` → `stp [sp,#0x168]` |

---

## 5. The third producer — `model.agentTaskInfo`

【一手】 the third producer is the one that carries a **step status**,
and it is the only place in the binary that writes the record's
`Status`. `model.agentTaskInfo` (0x102bc0730) takes
`(index int, step *<step>, running bool)` and returns a
`types.BackgroundTaskInfo` built on the stack at `sp+0x1c8`.

| destination | source | field | instruction |
|---|---|---|---|
| +0x00 | arg 1 | `Index` | `ldr x5,[sp,#0x240]` → `stp x5, x0, [sp,#0x1c8]` |
| +0x08 | the step | `Command` | the same `stp` writes the text's pointer, `str x1,[sp,#0x1d8]` its length |
| +0x18 | arg 3 | `Running` | `ldrb w0,[sp,#0x250]` → `strb w0,[sp,#0x1e0]` |
| +0x30 | the status | `IsError` | `cmp w1,#0x7` / `cset` → `strb w1,[sp,#0x1f8]` — **status == `CORTEX_STEP_STATUS_ERROR`** |
| +0x38 | the step | `ExitCode` | `str x2, [sp,#0x200]` — an int32 read through the step's +0x80 pointer |
| +0x40 | arg 3 | `NoExitCode` | `strb w0,[sp,#0x208]` — **the same boolean as `Running`** |
| +0x41 | the literal | `IsAgentTask` | **the static composite literal** — see below |
| +0x44 | the step | `Status` | `ldrsw w4, [x4,#0xc]` → `str w4,[sp,#0x20c]` — **the step's own int32 status** |
| +0x48 | the step | `StartTime` | `ldp x0,x1,[sp,#0x48]` → `stp [sp,#0x210]` |
| +0x60 | the step | `LastUpdatedTime` | `ldp x0,x1,[sp,#0x68]` → `stp [sp,#0x230]` |

**`+0x41` (`IsAgentTask`) is written by a static literal.** Before the
fields are set, the record is overwritten with a 120-byte constant from
`__rodata`:

```
102bc0d0c: add  x5, sp, #0x1c8     ; the record
102bc0d10: adrp x6, 0x103da8000
102bc0d14: add  x6, x6, #0x138     ; the literal at 0x103da8138
102bc0d18..0x102bc0d3c              ; copy 120 bytes out of it
```

The literal's 120 bytes (file offset 0x3da8138, section
`__TEXT,__rodata`) are zero everywhere **except one byte**:

```
+0x00 Index      = 0
+0x08 Command    = ""
+0x18 Running   = 0
+0x20 Result     = ""
+0x30 IsError   = 0
+0x38 ExitCode  = 0
+0x40 NoExitCode= 0   +0x41 IsAgentTask = 1     ← the one non-zero byte
+0x44 Status    = 0
+0x48 StartTime = the zero time
+0x60 LastUpdatedTime = the zero time
```

**So `IsAgentTask` is true on every record the agent producer builds**,
and false on every record the two projections build (they zero the
record and never write +0x41).

The step's status is read at `ldrsw x2,[x1,#0xc]` (0x102bc0b28) — the
step's own int32 field — and it is the *same* value that decides
`IsError`:

```
102bc0b1c: bl  0x102940fe0        ; store.IsAgentCommandStep(step)
102bc0b28: ldrsw x2, [x1, #0xc]    ; the step's status
102bc0b34: str  w2, [sp, #0x44]    ; staged
...
102bc0d58: ldrsw x1, [sp, #0x44]
102bc0d5c: cmp   w1, #0x7          ; CORTEX_STEP_STATUS_ERROR
102bc0d60: cset  x1, eq
102bc0d64: strb  w1, [sp,#0x1f8]   ; IsError
```

`store.IsAgentCommandStep` (0x102940fe0) is the predicate that picks
*which* text becomes the record's `Command`: it unwraps an interface
at the step's +0x58 and compares the tool name against **"run_command"**
(length 0xb, byte-compared inline). The reference computes that
predicate and uses it to choose the command text; the record's
`IsAgentTask` is a constant of the literal, not that predicate's
result.

Two callers:

| caller | the `running` argument | the index |
|---|---|---|
| `model.activeTasks` (0x102bc04a8) | `orr x2, xzr, #0x1` — **always true** | the index in the live-agent-task list |
| `model.backgroundTasks` (0x102bbff3c) | a byte read out of the live-step map | ditto |

In `model.activeTasks` the boolean is 1, so `Running` **and**
`NoExitCode` are both true — a running task has no exit code yet, which
is why the `(exit N)` suffix never appears on a running row.

### What this means for the four-way rule

The rule cycle 324 read — `Running` → running; `IsAgentTask` **and**
`Status == CORTEX_STEP_STATUS_CANCELED` → cancelled; `IsError` →
failed; everything else → completed — **holds, and both of its
conditions can be true at once**:

| the record comes from | `IsAgentTask` | `Status` | the word |
|---|---|---|---|
| `GetBackgroundTasks` (a shell command) | false | 0 | completed / failed / running |
| `model.activeTasks`'s projection | false | 0 | completed / failed / running |
| `model.agentTaskInfo` (an agent step) | **true** | the step's status | **cancelled** when the step was cancelled |

The cancelled word therefore belongs to the *agent* path only, which is
what the field's name says.

### A methodological note worth writing down

`IsAgentTask` is **never written by a byte store** anywhere in the
binary: a search over the whole `__text` section for a store at +0x41
returns 30 hits inside the jetski tree, and every one of them belongs
to an unrelated struct (telemetry requests, cortex configs, permission
records, the language server's auth and completion structs, and five in
`cli/model` that belong to the markdown renderer and the projects
model). **The field is set anyway**, because Go emits a struct literal
as a constant in `__rodata` and copies it in one 120-byte move.

The lesson: *a field that is never stored is not necessarily never
written*. The bulk copy of a static composite literal is invisible to
any store-at-offset search, and it is the normal way the compiler
initialises a struct literal.

---

## 6. The item the row renders — `model.activeItem`

【一手】 the row does not receive the record directly: the model wraps
it. `model.activeItem` (descriptor **0x1095dd298**, 80 bytes) is:

| offset | field | type |
|---|---|---|
| +0x00 | `kind` | `model.activeItemKind` (1 byte) |
| +0x08 | `source` | `model.ActiveItemSource` |
| +0x18 | `subagent` | `*types.SubagentState` |
| +0x20 | `task` | `*types.BackgroundTaskInfo` |
| +0x28 | `name` | string |
| +0x38 | `time` | time.Time |

`model.ActiveItemsModel.getActiveItems` (0x102b8b370) fills it. It
allocates a **fresh copy** of the record (`runtime.newobject` with the
descriptor 0x1096f9d00 at 0x102b8b6e4, the 120-byte copy at
0x102b8b718–0x102b8b740), zeroes an 80-byte item at `sp+0x108`
(0x102b8b748–0x102b8b758), and then:

```
102b8b75c: str  x0, [sp, #0x128]   ; item+0x20 = the record pointer   (task)
102b8b768: ldp  x8, x9, [x0, #0x8] ; the record's Command  (+0x08)
102b8b76c: stp  x8, x9, [sp, #0x130]; item+0x28 = name
102b8b770: ldp  x8, x9, [x0, #0x48]; the record's StartTime (+0x48)
102b8b778: stp  x8, x9, [sp, #0x140]; item+0x38 = time
102b8b77c: str  x10, [sp, #0x150]  ; item+0x48 = the time's location pointer
```

and appends the item to a `[]model.activeItem` (the growslice at
0x102b8b7ac carries the element type 0x1095dd298).

**This corrects cycle 324, §8.** The record's `Command` **is** the
row's name: `getActiveItems` copies it into `activeItem.name`, and the
row renders that. Cycle 324's "the row's name comes from another
argument" is true only of the *renderer* — `render.RenderTaskItem`
(0x1028a0d60) takes the name as a separate argument — but the value it
is given is the record's own `Command`. Cycle 324's other two claims
stand as corrected: the row reads neither `Command` nor `Result`
*itself* (it is handed the name), and `Result` is never read by the
row's rules at all.

The row's **time cell** is the record's `StartTime` (+0x48) — not
`LastUpdatedTime`.

---

## 7. What the clone changed

| file | change |
|---|---|
| `internal/taskregistry/format.go` | `StateStyle`'s doc comment names the three producers and says which one sets `Status` and `IsAgentTask` — and that the clone's `StateKilled` is that agent step |

**No rendered byte changed.** The clone's row already renders the
reference's four words, the `(exit N)` suffix and the same rule; the
clone's `taskregistry.ExitCodeUnset` is the reference's `NoExitCode`,
and the clone's own task states are mapped by hand in
`taskregistry.StateWord` — `StateKilled` is the reference's cancelled
agent step, and it already renders "cancelled".

---

## 8. 硬约束 / the load-bearing rules

- **The store's record is `types.CommandOutput`; the row's record is
  `types.BackgroundTaskInfo`.** They are two different 120-byte structs
  with eleven fields each, and the mapping between them is the
  projection of §3.
- **The projection is field-wise, never a bulk copy.** `Index` comes
  from the loop counter, not from the source record; `Command`,
  `Result`, `Running`, `IsError`, `ExitCode`, `StartTime` and
  `LastUpdatedTime` come across; `NoExitCode`, `IsAgentTask` and
  `Status` are left zero.
- **`Index` is the index in the *source* list** — the counter is
  incremented for every record, including the ones the filter skips.
- **Only `model.agentTaskInfo` writes `Status`**, and it writes the
  step's own int32 status. `IsError` in that producer is
  `Status == CORTEX_STEP_STATUS_ERROR` (7).
- **`NoExitCode` is "still running".** The agent producer writes the
  same boolean into `Running` and `NoExitCode`; the two projections
  leave `NoExitCode` false.
- **`IsAgentTask` is true on the agent path and false on the two
  projections** — and it is set by the *static composite literal* at
  0x103da8138, not by a store.
- **The row's name is the record's `Command`** — copied into
  `model.activeItem.name` by `getActiveItems`. The row's time cell is
  the record's `StartTime`.
- **10 is a reserved range and `CORTEX_STEP_STATUS_HALTED` is a
  reserved name.** Neither is a value; the row's rule never compares
  against either.
- **A field that is never stored is not necessarily never written** —
  look for the static literal in `__rodata` before concluding a branch
  is dead.

---

## 9. 存疑 / open questions

- **【存疑】 The step's own type.** `model.agentTaskInfo` reads the
  status from an int32 at +0x0c of its second argument, and the tool
  name through an interface at +0x58. The type of that argument was not
  identified — it is not the protobuf message the enum lives in, and
  its identity does not affect any of the findings above.
- **【存疑】 Which producer feeds which panel.** The store's
  `GetBackgroundTasks` and the model's `activeTasks` produce the same
  view; the reference has both, and this cycle did not establish which
  of the two the `/tasks` panel reads on any given path.
- **【存疑】 Whether `Result` is ever rendered anywhere.** It is copied
  into the record by all three producers and read by none of the row's
  rules; some other view may read it, and that was not traced.
- **【存疑】 The `Hidden` field (+0x3b of the store's record).** It is
  read by `SetShellCommandBackgrounded` and by no projection traced
  here; what filters hidden commands out of the views is not known.

---

## 10. Limitations

- **The disassembly was read, not the decompiler.** Go's register
  allocation is aggressive and the same boolean is often reused for two
  fields; every claim about *which* field a store writes is backed by
  the field's offset in the type descriptor.
- **The byte-offset searches for `+0x41` were run over the whole
  `__text` section** (13.3 M lines) and mapped to functions through the
  seed file. A store through a *computed* address would not be visible
  to that search — and, as §5 shows, neither is a bulk copy of a static
  literal. The claim "no code writes `IsAgentTask`" is therefore a
  claim about *stores*, and it was corrected by the literal.
- **The `IsAgentTask` claim covers the jetski tree only.** The IDE's
  own `language_server` binary is a different program and may set the
  field; the CLI's row is what this cycle is about.
- **The projections were traced for the fields they write**, not for
  every field they read.
- **Only the `CortexStepStatus` enum was parsed.** The rest of the
  reference's protobuf descriptors were not walked.

---

## 11. The files

### The binary

`~/.local/bin/agy` — Antigravity CLI 1.4.7, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`.
The file offset of an address is the address minus 0x100000000.

### The functions this cycle is based on

| address | function |
|---|---|
| 0x1029a39b0 | `store.(*TaskManager).GetBackgroundTasks` — the producer |
| 0x1029a1be0 | `store.(*TaskManager).SetShellCommandBackgrounded` — writes `Backgrounded` |
| 0x10294fea0 | `store.(*Manager).addCommandOutputWith` — writes the store's record |
| 0x102940fe0 | `store.IsAgentCommandStep` — the "run_command" test |
| 0x1029410b0 | `store.IsBackgroundTaskStep` |
| 0x102bc0160 | `model.activeTasks` — the model's projection |
| 0x102bc0730 | `model.agentTaskInfo` — the agent-steps producer |
| 0x102bbfc30 | `model.backgroundTasks` |
| 0x102b8b370 | `model.ActiveItemsModel.getActiveItems` — the item |
| 0x1028a0d60 | `render.RenderTaskItem` — the row (cycle 324) |
| 0x10289fe70 | `render.StatusMarker` — the marker (cycle 324) |
| 0x1000215f0 | `runtime.newobject` |
| 0x100025320 | `runtime.wbMove` |
| 0x10008cab0 | `runtime.growslice` |

### The type descriptors this cycle is based on

| address | type |
|---|---|
| 0x1096f9d00 | `types.BackgroundTaskInfo` — the row's record, 120 bytes |
| 0x1096f9e68 | `types.CommandOutput` — the store's record, 120 bytes |
| 0x1095dd298 | `model.activeItem` — the item, 80 bytes |
| 0x108a61260 | `*types.BackgroundTaskInfo` |
| 0x108af6de8 | `[]types.BackgroundTaskInfo` |
| 0x1086f91b0 | the types section — the base of the type name offsets |

### The static literal this cycle is based on

| address | file offset | what |
|---|---|---|
| 0x103da8138 | 0x3da8138 | the 120-byte `types.BackgroundTaskInfo` literal — `IsAgentTask` = 1, everything else 0 |

### The protobuf descriptor this cycle is based on

| file offset | vaddr | what |
|---|---|---|
| 63900723 | 0x103cf0c33 | the `CortexStepStatus` `EnumDescriptorProto`, 441 bytes |
| 63901131 | 0x103cf0dcb | `reserved_range` — `{start: 10, end: 10}` |
| 63901137 | 0x103cf0dd1 | `reserved_name` — `"CORTEX_STEP_STATUS_HALTED"` |
| 63901139 | 0x103cf0dd3 | the name's bytes |
