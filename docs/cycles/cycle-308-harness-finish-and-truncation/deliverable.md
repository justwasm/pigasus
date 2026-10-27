# The client's `finish` tool, and the tool-output truncation

**Cycle:** `cycle-308-harness-finish-and-truncation`
**Subject:** `out/localharness/PARITY.md` §4.4 listed two rows the clone had
not closed — the client's `finish_tool_schema_json`, and the tool-output
truncation. This cycle captures both against the reference binary, plus
three measurements that corrected the report's own premises: the
workspace containment, the reference's runtime call validation, and the
reference's wording for a tool's error.
**Evidence:** 【一手】 the reference binary's own frames and request bodies,
captured live through the SDK's protocol · 【反编译】 decompiled output at
confirmed Mach-O addresses · 【strings】 string-table hits only · 【推测】
hypothesis

---

## 0. Summary

| | before | after |
|---|---|---|
| `finish_tool_schema_json` | ignored — the clone kept its own schema | the client's schema, with the keys sorted; **19** declarations instead of 18 |
| a `finish` **call** | not a thing | four frames, `SOURCE_MODEL` → `TARGET_USER`, and **no** second model request |
| the declarations' dialect | guessed from the schema's shape | taken from the caller — the harness's schemas are `Struct` values, so **all** of them are `parametersJsonSchema` |
| the tool-output truncation | not implemented | the reference's rule: budget = `max_tokens × 3` bytes, the **tail** survives, the cut lands on a line boundary, and the notice is the reference's own text |
| the `error` arm of the truncation oneof | ignored | the same budget, the same notice |

Differentials (`f_finish2`, `c_trunc`, `t_5/20/100`, `l_8/10/40`, `e_10`,
`e_100`, `sfo`): **0 frame differences**, and the request bodies are
byte-identical after normalising the session id and the timestamps — the
one exception is the reference's own racy `usage_update` position, which
is documented in cycle 307 §6.

Three claims in the gap report turned out to be **wrong**, and are
corrected here (§7):

* the reference does **not** confine the tools' paths to the workspace —
  not even with `workspace_containment` enabled;
* the reference's runtime check is **not** "the advertised schema with
  `required` dropped" — it rejects some calls the advertised schema would
  accept and runs others it would not;
* the tool error's wording is the reference's own, not the Go error's.

---

## 1. The method

The probe drives the reference binary and the clone through the SDK's own
protocol: it writes a `HarnessConfig` into the `InitializeConversationEvent`,
points the model at a fake Gemini endpoint that replies with one scripted
tool call, and records every frame the harness writes and every request
body it sends. The workspace is snapshotted before the reference runs and
restored before the clone runs.

The comparison normalises what cannot match: the sequence number, the
timestamp, the prompt's local time, the `Created At` / `Completed At`
stamps, the tool call id (a UUID) and the session id.

---

## 2. The `finish` tool

The client may name a JSON schema for the `finish` tool
(`HarnessConfig.finish_tool_schema_json`).

### 2.1 The declaration

With a schema, the reference sends **nineteen** declarations; without one,
eighteen. The new one sorts into the list **by name** — between
`find_by_name` and `generate_image`:

```json
{
  "description": "Finish your task and end the interaction. Don't call this tool unless you are confident that your solution is correct, for example after you've written tests, executed them, and verified the task is complete.",
  "name": "finish",
  "parametersJsonSchema": {
    "properties": {"summary": {"type": "string"}},
    "required": ["summary"],
    "type": "object"
  }
}
```

The description is the **reference's own wording** — it is not the
client's, and it is not the clone's. It is pinned in
`internal/tools/testdata/tool_descriptions_1.4.7.json` (the "converter"
layer).

The schema itself is the client's own bytes, but the **keys come out
sorted**. The reference carries it as a `google.protobuf.Struct`, and a
Struct's keys are sorted — so the clone re-serialises the client's JSON
through a map and lets the map order it (`configFinishSchema`).

A schema that is not JSON cannot go into a Struct, so the reference drops
it, and the session has **no** `finish` declaration — the same session the
client gets when it names no schema at all.

### 2.2 The call

A `finish` call is **four frames** — announcement, start, result, done —
and every one of them is `SOURCE_MODEL` → `TARGET_USER`, with the text
`Finished` and an action carrying the model's own arguments:

```
1/ACTIVE _MODEL/_USER text='Finished'   ← announcement
usageUpdate
1/ACTIVE _MODEL/_USER text='Finished'   ← start
1/ACTIVE _MODEL/_USER text='Finished'   ← result
1/DONE   _MODEL/_USER text='Finished'   ← done
```

with

```json
"finish": {"outputString": "{\"summary\":\"all done\"}"}
```

Every other tool call targets the **environment**; `finish` is the model's
answer, so it targets the **user**.

After the done frame the trajectory goes to `STATE_FULLY_IDLE` and the
reference **does not ask the model again**. The clone asks the runtime to
stop after the batch — PiG's `AgentToolResult.Terminate`, which ends the
run only when *every* result in the batch sets it.

### 2.3 The dialect of the schemas

The reference declares its tools with a `Struct`, so **every** schema it
sends travels under `parametersJsonSchema` — including the client's, whose
shape says nothing about the dialect it belongs to.

The transport used to guess the dialect from the schema's shape. That
guess cannot survive the client's schema, so the answer now comes from the
caller: `backend.GeminiConfig.JSONSchemaTools`, set on all four harness
clients (Gemini, Vertex, Gemma and the custom endpoint's Gemini-shaped
one).

---

## 3. The tool-output truncation

`HarnessConfig.tool_output_truncation` is a **oneof**:

```
truncate { max_tokens, save_full_output } | error { max_tokens, error_message }
```

The SDK only ever sends the `truncate` arm
(`connections/local/local_connection.py`); the `error` arm is reachable
only from a hand-written client.

### 3.1 The budget

`max_tokens` is a budget of **three bytes per token**. Four captures fix
the ratio: 5 tokens kept 15 bytes, 10 kept 30, 20 kept 60, 100 kept 300.

### 3.2 The tail survives

The SDK's own docstring says the opposite:

> When a tool's output exceeds `max_tokens`, the harness preserves the
> beginning (prefix) of the output up to the limit and truncates the
> remainder (tail), appending a notice informing the model that the output
> was truncated.

The reference keeps the **tail**. A capture of a hundred `A`s followed by
a hundred `B`s keeps the `B`s. **The docstring is stale; the binary is
not.**

### 3.3 The cut and the notice

When the surviving tail contains a newline, the reference drops the tail's
**first line** — whether it is a whole line or the partial one the cut
landed in — and the notice is:

```
<truncated 1 lines>
```

When the tail has no newline, the notice is:

```
<truncated N bytes>
```

and `N` is **not** the number of dropped bytes. It is the number of
**decimal digits** of the dropped byte count, plus nineteen. Ten captures
pin the rule:

| dropped bytes | notice |
|---|---|
| 10 / 30 / 70 / 99 | `<truncated 21 bytes>` |
| 100 / 170 / 200 / 470 | `<truncated 22 bytes>` |
| 1000 / 4970 | `<truncated 23 bytes>` |

What the reference *means* by that number is not known (§8).

### 3.4 The `error` arm

The reference's `error` arm bounds the output the same way: the same
three-bytes-per-token budget, the same notices. Its `error_message` never
reached the model in any capture — the notices were always one of the two
above. The clone therefore treats the two arms' `max_tokens` alike and
does not invent a use for `error_message`.

### 3.5 `save_full_output`

No observable effect. `SAVE_FULL=0` and `SAVE_FULL=1` produced byte-identical
output for the model.

---

## 4. What the clone does

```
internal/harnesstools/truncate.go   the rule: the budget, the tail, the
                                    line-boundary cut, the two notices
internal/harnesstools/harnesstools.go  the truncation is wired into the
                                    run_command rendering and into the
                                    default case of Render
cmd/harness/config.go               configToolOutputTokens reads the
                                    oneof's *active* arm;
                                    configFinishSchema sorts the client's
                                    schema
cmd/harness/sidetools.go            the finish declaration, inserted by
                                    name into the declaration list
cmd/harness/events.go               the finish frames: the target, the
                                    text, the action
cmd/harness/tools.go                the finish result asks the runtime to
                                    stop after the batch
cmd/harness/models.go               JSONSchemaTools on every harness
                                    client
internal/backend/gemini.go           the dialect of the schemas
```

`internal/harnesstools/truncate_test.go` pins the rule with the captures'
own numbers; `cmd/harness/sidetools_test.go` pins the declaration's place,
its shape and the drop of a non-JSON schema; `cmd/harness/events_test.go`
pins the action, the target and the text.

---

## 5. The measurements that corrected the report

### 5.1 The reference does not confine the tools' paths

The gap report said the clone "reads the config but does not use it to
restrict the tools' paths", implying that the reference *does* restrict
them. It does not.

【一手】 A write of an **absolute** path outside the workspace, in four
configurations:

| configuration | reference | clone |
|---|---|---|
| nothing set | the write succeeds | refused (`expected path … to not start with /`) |
| `workspace_containment = ENABLED` | the write succeeds | refused |
| `enforce_workspace_validation = true` | the write succeeds | refused |
| both | the write succeeds | refused |

So `workspace_containment` has **no observable effect** on this path, and
the clone is the **stricter** of the two. The clone's rule comes from the
CLI's own `internal/tools` (`workspacePath`), which the harness shares
with the CLI. The SDK never sends `ENABLED` anyway — it sends `DISABLED`
or `UNSPECIFIED` (`hooks/policy.py`).

The reference's attitude to a **relative** path is a different thing, and
it belongs to §5.2.

### 5.2 The reference's runtime call validation

【一手】 The reference checks a call's arguments **before** it runs the
tool, and refuses an invalid one as an *invalid tool call*:

| call | reference | clone |
|---|---|---|
| `view_file` with no `AbsolutePath` | refused: `invalid_signature` / `AbsolutePath is a required parameter` | **runs** (the `required` is relaxed), fails with `read …: is a directory` |
| `write_to_file` with a relative path | refused: `invalid_args` / `../outside2.txt must be an absolute path: path is not absolute` | **runs**, fails with `path ../outside2.txt escapes the workspace` |
| `run_command` with no `CommandLine` | **runs** | runs |
| `run_command` with no `WaitMsBeforeAsync` | **runs** | runs |
| `list_dir` with no `DirectoryPath` | **runs** (fails at run time) | runs |

So the decision is **not** the advertised schema with `required` dropped:
the reference's *runtime* schema is a different one from the advertised
schema — the advertised one requires `CommandLine`, the runtime one does
not.

The refused step's result is a paragraph of the reference's own:

```
Created At: …
Error invalid tool call: There was a problem parsing the tool call.
Error Message: model output error: invalid tool call error (invalid_args) …
Guidance: You are trying to correct your previous tool call error, you must focus on fixing the failed tool call with sequential tool calls and try again. Do not do parallel tool calls and if you are fixing multiple tool calls, do them one at a time. Do not apologize.
Retries remaining: 4.
```

and the frame carries `errorMessage: "The model produced an invalid tool
call."`.

This path is **not** reproduced by the clone. It is a whole error-reporting
surface (two error kinds, a retry counter, a guidance paragraph), and it
was left for a cycle of its own rather than half-built here.

### 5.3 The tool error's wording

【一手】 `view_file` on a file that does not exist:

| | reference | clone |
|---|---|---|
| the frame's `errorMessage` | `Cannot view file file://…/nope.txt which does not exist.` | `open …/nope.txt: no such file or directory` |
| what the model sees | `Created At: …` + `Encountered error in step execution: path file://…/nope.txt does not exist` | `open …/nope.txt: no such file or directory` |

`list_dir` has the same shape: `Cannot list directory file://… which does
not exist.` and `Encountered error in step execution: directory … does not
exist`.

The reference has **its own sentence per tool** for the frame, and wraps
the execution error in the step template's own words. The clone passes the
Go error through. The wording is per-tool and was not probed exhaustively;
it is left as a gap.

---

## 6. The reference's own race

The `usage_update` frame's position is a race in the reference: 24 of 27
captures put it after the tool's start frame, 3 put it before. The clone
takes the majority order. Two of the differentials in this cycle hit the
minority order, and the two differing frames are exactly that — the
usage frame and the frame it displaced.

---

## 7. Deliberate divergence

None beyond what §5 records. The clone's `finish` declaration, its
frames, the truncation and the schema dialect are byte-identical to the
reference's.

---

## 8. 硬约束 / 存疑 / limitations

**硬约束 (hard constraints).**

1. The client names a schema for `finish` → the reference sends **nineteen**
   declarations, and the new one sorts by name between `find_by_name` and
   `generate_image`.
2. The client's schema travels with its **keys sorted** — the reference
   carries it in a `Struct`.
3. A schema that is not JSON is dropped: the session then has no `finish`
   declaration at all.
4. A `finish` call writes **four frames**, all `SOURCE_MODEL` →
   `TARGET_USER`, the text `Finished`, and the action carries the model's
   own arguments. There is **no second model request**.
5. The harness's schemas all travel under `parametersJsonSchema` — the
   dialect cannot be guessed from the schema's shape.
6. The truncation budget is `max_tokens × 3` bytes, and the **tail**
   survives.
7. The cut lands on a line boundary; the tail's first line goes, and the
   notice is `<truncated 1 lines>`.
8. The byte notice is the dropped byte count's **decimal digit count plus
   nineteen** — not the byte count.
9. `tool_output_truncation` is a **oneof**: the `error` arm's `max_tokens`
   bounds the output the same way.

**存疑 (open questions).**

- What the byte notice's number *means*. The reference computes the
  dropped byte count's digit count plus nineteen — the formula has no
  readable provenance. It is copied, not understood.
- What the `error` arm's `error_message` is for. It never reached the
  model in any capture. The clone copies the arm's `max_tokens` behaviour
  and invents nothing.
- Whether `save_full_output` has an effect somewhere the probes did not
  look. The output the model sees is byte-identical with the flag on and
  off.
- Whether the reference's runtime validation is a second schema or a set
  of ad-hoc rules. The measurements only show that it is not the
  advertised schema.

**limitations.**

- **Only the arm64/macOS wheel was sampled.**
- **The truncation was probed through `run_command` only.** The clone
  applies the rule to every tool; the other tools' long outputs were not
  captured against the reference.
- **The `finish` tool was probed with one schema** (`{"summary": …}`). A
  multi-field schema and a non-JSON schema were only tested on the
  clone's side.
- **The runtime call validation was not reproduced** (§5.2), so the
  clone's behaviour on an invalid call still differs from the
  reference's.
- **The tool error's wording was sampled, not enumerated** (§5.3): two
  tools were captured, and the per-tool sentence list is unknown.
