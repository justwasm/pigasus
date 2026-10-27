# The reference's enum numbering — deliverable

**Cycle:** `cycle-307-cortex-enums`
**Subject:** `internal/cortex_pb`'s enums were the clone's own reconstruction:
consecutive `iota` numbers and, in several places, member names the reference
does not have. The reference's numbering is sparse and different — the
`CORTEX_STEP_TYPE_RUN_COMMAND` the reference puts on the wire is **21**, the
clone's constant said **3**. This cycle rewrites the package's enums from the
reference's own descriptor and makes the descriptor the package's source of
truth.
**Evidence:** 【一手】 the reference's own `FileDescriptorProto`, decoded
verbatim from the binary (`out/local-macos/protos/cortex_1.4.6.txt`, rendered
as `cortex_1.4.6.proto` — 487 messages, 86 enums), plus the numbers read out
of a real implicit-trajectory record the reference wrote on 2026-10-22.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the numbers** | consecutive `iota` values from 0 | the reference's own, sparse (`CORTEX_STEP_TYPE_RUN_COMMAND` = 21, `CORTEX_STEP_STATUS_DONE` = 3, `CORTEX_STEP_SOURCE_USER_EXPLICIT` = 4) |
| **the member names** | partly invented (`PlanStatusApproved`, `AgentModeChat`, `ModelTierSonnet`…) | the reference's own (`PlanStatusPlanned`, `AgentModePlanning`, `ModelTierPro`…) |
| **`String()`** | a short guess (`"CHAT"`) | the reference's own spelling (`"AGENT_MODE_PLANNING"`) |
| **the enums the package lacked** | `CortexStepSource`, `CortexTrajectoryType` | added — the two the implicit trajectory writes |
| **`GoalState`** | looked like the rest | marked as the clone's own: the .proto has no such enum |
| **the source of truth** | a hand-written table | the recovered .proto, checked by a test |

## 1. What was wrong

The package's own header said the enums came from the 1.3.5 pclntab, and
that was true — of the *names*. The **numbers** were never recovered: they
were `iota`, which is a plausible-looking reconstruction and completely
wrong for a proto enum, whose members are sparse.

The reference's numbers are observable. The implicit trajectory the clone
writes is a proto message, and a real record — the `printf 'a\nb\n'` command
of 2026-10-22, cut out of the reference's own file on this machine — carries:

```
#1 varint 21     <- CORTEX_STEP_TYPE_RUN_COMMAND
#4 varint 3      <- CORTEX_STEP_STATUS_DONE
#3 varint 4      <- CORTEX_STEP_SOURCE_USER_EXPLICIT
#4 varint 1      <- CORTEX_TRAJECTORY_TYPE_USER_MAINLINE
```

The clone's `internal/implicit` had those numbers hard-coded — with a comment
explaining why: `internal/cortex_pb` carried a *reconstructed* enum with
different numbers, so the numbers were repeated rather than borrowed. That
comment was accurate and it was a smell: the same table, twice, one of them
wrong.

## 2. The source of truth

`out/local-macos/protos/cortex_1.4.6.txt` is the `FileDescriptorProto` the
Go protobuf runtime links into the binary, decoded verbatim (no field
hand-written). Its rendering, `cortex_1.4.6.proto`, carries **86** enums —
including the four the clone's own code touches.

`devutils/gen_cortex_enums.py` rewrites `internal/cortex_pb/cortex.pb.go`
from it. The generator is mechanical: parse the .proto, keep the member
names and their numbers, emit the Go constants, the `String()` switch and the
`EnumMembers` table.

| | before | after |
|---|---|---|
| enums rewritten | — | 21 |
| enums added | — | 2 (`CortexStepSource`, `CortexTrajectoryType`) |
| enums left alone | — | 1 (`GoalState` — no .proto counterpart) |
| `EnumMembers` | — | 23 entries, the machine-readable form |

## 3. The numbers that matter

| proto member | number | the clone's old constant |
|---|---|---|
| `CORTEX_STEP_TYPE_RUN_COMMAND` | 21 | `CortexStepTypeRunCommand` = 3 ✗ |
| `CORTEX_STEP_TYPE_USER_INPUT` | 14 | `CortexStepTypeUserInput` = 1 ✗ |
| `CORTEX_STEP_STATUS_DONE` | 3 | `CortexStepStatusSucceeded` = 3 — the number by luck, the name is not the reference's ✗ |
| `CORTEX_STEP_STATUS_CANCELED` | 6 | — |
| `CORTEX_STEP_SOURCE_USER_EXPLICIT` | 4 | — |
| `CORTEX_TRAJECTORY_TYPE_USER_MAINLINE` | 1 | — |
| `PLAN_STATUS_INITIALIZED/PLANNING/PLANNED/ERROR` | 1/2/3/4 | `PlanStatusDraft` = 1, `PlanStatusApproved` = 2 — neither name exists in the reference ✗ |
| `TASK_STATUS_TODO/IN_PROGRESS/DONE` | 1/2/3 | `TaskStatusPending` = 1, `TaskStatusCompleted` = 4 — the reference has neither ✗ |
| `AGENT_MODE_PLANNING/EXECUTION/VERIFICATION` | 1/2/3 | `AgentModeChat` = 1 — the reference has no `CHAT` ✗ |
| `MODEL_TIER_INHERIT/FLASH_LITE/FLASH/PRO` | 1/2/3/4 | `ModelTierSonnet` = 2 — the reference has no model names in this enum ✗ |

`CortexStepType` alone has **118** members with gaps (0, 1, 2, 4, 5, 6, 7,
13, 17, 21, …) — a shape no `iota` reconstruction would ever produce.

## 4. What this cycle changed

- `internal/cortex_pb/cortex.pb.go` — 21 enums rewritten (member names,
  numbers, `String()`), 2 enums added, `GoalState` marked as the clone's
  own, the `EnumMembers` table emitted, the package header corrected (it
  claimed "30+ enums" and "the v1.3.5 pclntab").
- `internal/cortex_pb/enums_proto_test.go` — **new**. Parses the recovered
  .proto and compares every member name and number against the package;
  checks that `String()` maps a number back to the reference's spelling;
  pins the six numbers the implicit trajectory carries. Skips when `out/`
  is absent — the evidence tree is not part of the build.
- `internal/cortex_pb/cortex_test.go` — the spot checks now use the
  reference's names.
- `internal/implicit/trajectory.go` — the hard-coded numbers are now
  `cortex_pb`'s constants, and the comment that explained the duplication
  is gone.
- `devutils/gen_cortex_enums.py` — **new**. The generator.

## 5. What this cycle proves

1. The clone's enum tables are the reference's, checked mechanically against
   the reference's own descriptor rather than against a hand-written copy.
2. The implicit trajectory's numbers are now one source of truth: the
   package that writes them borrows them from the package that defines them.
3. `GoalState` is the one thing in this package that is the clone's own,
   and it now says so.

## 6. 存疑 / open questions

- **【推测】** The .proto is the **1.4.6** descriptor; the live binary is
  **1.4.7**. The six numbers the trajectory uses are confirmed against a
  1.4.7 record ✓, so the ones that matter did not move — but the other 80
  enums are only evidenced at 1.4.6. Re-extracting the 1.4.7 descriptor
  would close that.
- **【推测】** `CortexStepType` has 118 members; the clone now carries all
  of them, but nothing in the repository uses more than a handful. They are
  carried because they are the reference's, not because the clone needs them.
- **【推测】** The clone's *message* structs still carry the 1.3.5-era
  reconstruction (field numbers, getters). This cycle fixed the enums only;
  the messages are a larger job and were left alone on purpose.

## 7. Limitations

- The comparison test needs `out/local-macos/protos/cortex_1.4.6.proto`. When
  that tree is absent (a snapshot release, a clean clone) the test **skips**
  rather than fails — the guarantee is then only as good as the committed
  table.
- The .proto's rendering is mechanical for the public file but hand-
  abridged for nested types (per `out/local-macos/protos/README.md`). The
  enums the clone uses are all top-level ✓.
- The extraction covers the **CLI** binary only; the IDE's language server
  carries its own descriptors.
