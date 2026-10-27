# `cmd/harness` — byte parity with the reference binary

**Cycle:** `cycle-306-harness-byte-parity`
**Subject:** cycle 300 wrote the *server* half of the `localharness`
transport and `cmd/harness`, the clone of the closed-source binary the
`google-antigravity` SDKs spawn. This cycle drives the clone and the
**reference binary** through the same protocol against the same fake model
endpoint and compares what comes out — byte for byte — then closes the
gaps the comparison exposes.
**Evidence:** 【一手】 the reference binary's own frames and request bodies,
captured live · 【反编译】 decompiled output at confirmed Mach-O addresses ·
【strings】 string-table hits only · 【推测】 hypothesis

> **Erratum (cycle 307).** One claim below is **wrong** and is
> retracted by
> [`cycle-307-harness-tool-surface`](../cycle-307-harness-tool-surface/deliverable.md):
>
> 1. §2.4 and §9's `view_file` line-count difference — there is **none**.
>    The reference's `view_file` output and the clone's are byte-identical
>    (4 lines for a 3-line file with a trailing newline); the earlier
>    reading was a mistake.
>
> §9's "the `usage_update` position is not deterministic" **stands**, and
> cycle 307 sharpens it: over 27 captures the order is *announcement →
> start → usage* 24 times and *announcement → usage → start* 3 times, and
> the clone now takes the majority order.
>
> The rest of the report stands.

---

## 0. Summary

Until this cycle, `cmd/harness` had never been run against the reference
binary. It was driven by the clone's own client code and by protocol tests,
and its wire behaviour was *inferred* from the recovered descriptor.

This cycle runs both binaries through the SDK's own protocol against one
fake Gemini endpoint, and compares:

| | result |
|---|---|
| Gemini request body | **identical** after normalising the local time and the random `sessionId` |
| Gemini request headers | identical except `X-Goog-Api-Client`'s binary-owned fields |
| text-only turn | **8 / 8 frames identical** |
| tool-call cycle | **frame order identical**, 12 / 12 sampled step frames identical, usage payloads identical |

Four real defects surfaced, and all four are fixed here:

| | defect | fix |
|---|---|---|
| 1 | a tool call whose arguments miss a required field was **refused** by PiG's runtime validation — the reference runs it | `tools.RelaxedSchema` + `piagentbridge.LenientSurfaceToolsFor` |
| 2 | a tool cycle emitted **2** step frames; the reference emits **4** | the announcement frame, at `MessageEndEvent` |
| 3 | the `usage_update` carried the **last** response's tokens; the reference carries the **cumulative** total | a running counter on the conversation |
| 4 | `view_file`'s action reported the raw arguments — no 800-line window, and a `contentOffset` of zero that the reference omits | `viewFileWindow` + `shownLines` |

The artefacts are:

| | |
|---|---|
| `out/localharness/PARITY.md` | the gap report (zh-CN): the method, the results, the remaining gaps |
| `internal/backend/gemini.go` | the Gemini transport: `parametersJsonSchema`, alphabetical body, trailing newline |
| `internal/tools/harness_tools_1.4.7.json` | the reference's own 18 declarations, captured |
| `internal/tools/harness_surface.go` | the harness's surface — *not* the CLI's twelve |
| `internal/harnessprompt/` | the reference's system instruction, byte for byte |
| `cmd/harness/{models,config,prompt,policy,sidetools}.go` | the client's configuration, resolved |

---

## 1. The method

### 1.1 The harness

```bash
python3 fakeserver.py 19247 gemini.log     # a fake Gemini endpoint that logs
ALL_SIDE_TOOLS=1 FAKE_BASE_URL="http://127.0.0.1:19247/v1beta" \
  python3 drive.py wheel/google/antigravity/bin/localharness turn
```

`drive.py` is a minimal client transcribed from the shipped SDK: the stdio
handshake, the websocket, the `InitializeConversationEvent`, a `UserInput`,
and a frame reader. The fake endpoint answers with a function call when
`FAKE_TOOL` / `FAKE_TOOL_ARGS` are set, so a **tool cycle** is driven
exactly like a text turn.

Both binaries run the same script against the same endpoint. The only
normalisation is `seqNum` / `timestampMicros`, the local time embedded in
the prompt, and the random `sessionId`. Nothing else is touched: not the
prompt text, not the declarations, not the frame order, not a single key
order.

### 1.2 What the comparison found

The request body is **the same 54,990 bytes** on both sides once the time
and the session id are normalised. That pins, at once:

- the system instruction — byte for byte, including every section;
- the 18 function declarations — same names, same order, same schemas;
- the body layout — `contents`, `generationConfig`, `sessionId`,
  `systemInstruction`, `toolConfig`, `tools`, **alphabetically**, with a
  trailing newline;
- the `User-Agent` — the **client's** own, forwarded verbatim by the
  reference binary. A surprise, and a rule the clone now follows.

---

## 2. The four defects

### 2.1 The reference runs a call that misses a required field

The captured `view_file` call carried only `AbsolutePath` — no
`toolSummary`, no `toolAction` — and the reference ran the tool. PiG's
runtime validates a call's arguments against the tool's schema before it
dispatches, so the clone refused the call with
`Validation failed for tool "view_file"`.

The fix keeps the two things apart:

| | schema | effect |
|---|---|---|
| the **advertised** declaration | the captured one, unchanged | the model is still told which fields the tool wants; the request body is unchanged |
| the **runtime** check | the same schema with `required` dropped | a call that misses a field runs |

PiG prefers a tool's `ArgumentSchema()` over `Schema().Parameters`, so
`internal/piagentbridge.LenientSurfaceToolsFor` gives the harness's
surface a relaxed runtime schema while `Schema()` keeps advertising the
captured one. `internal/tools.RelaxedSchema` does the relaxation.

> The *strict* surface validates exactly as before: `ArgumentSchema()`
> returns the lowered schema `Schema()` advertises, so the CLI's path is
> untouched. The CLI's schemas use uppercase type names
> (`OBJECT` / `STRING`), which PiG's JSON-Schema library rejects — the
> lowered form is the one that compiles.

### 2.2 A tool cycle is four frames, not two

The reference's cycle:

```
step N  ACTIVE  textDelta="Tool execution"   the announcement
usage_update
step N  ACTIVE  textDelta=""                 the tool started
step N  ACTIVE  textDelta=""                 the tool produced output
step N  DONE    textDelta=""                 the tool finished
```

All four share **one** `step_index`: `step_index` names a *logical step*,
not a frame. The announcement is the model's message — a message that
carries a tool call instead of text — and the reference renders it as the
call's announcement, before the turn's usage.

The clone emitted only the start and the end. It now emits the
announcement at `MessageEndEvent` (`emitModelMessage` → `announceToolCall`),
reuses the announced index for the three frames that follow, and splits the
end event into the result frame and the done frame.

### 2.3 The usage frame carries the cumulative total

The reference's frames go 18, 36, 54 — each one the sum of the responses
so far, not the last response's count. The clone reported the last
response's count every time. It now accumulates, per counter, on the
conversation.

### 2.4 `view_file`'s action reports a window

The action's line window is **0-based**, even though the tool's own
description says the lines are 1-indexed:

| call | action |
|---|---|
| `AbsolutePath` only | `startLine=0, endLine=799` — the 800-line window |
| `StartLine=10, EndLine=20` | `startLine=9, endLine=19` |

`contentOffset` is omitted when it is zero (proto3 explicit presence).

And the frame that **closes** the step carries the window the tool
actually *showed*, not the one the call asked for. Five probes against the
reference binary:

| probe | reference's `endLine` |
|---|---|
| 1 line, trailing newline | 1 |
| 2 lines, no trailing newline | 1 |
| 3 lines, trailing newline | 3 |
| 800 lines | 799 |
| 900 lines | 799 |

The rule is **"the last line shown − 1"** (0-based). The clone derives it
from the tool's own result — `Showing lines A to B`, 1-indexed — and hits
all five. The derivation is pinned by
`TestViewFileRangeMatchesTheReferenceProbes`, which runs the real
catalogue tool against the same five files.

---

## 3. The Gemini transport

`internal/backend/gemini.go` is the transport the harness uses. It is
**not** the OpenAI-shaped one the CLI's native provider uses.

| | rule | why |
|---|---|---|
| declarations | `parametersJsonSchema` | the reference advertises plain JSON Schema (draft 2020-12); the API's `parameters` field is the OpenAPI subset |
| body keys | alphabetical | the reference's body is `contents`, `generationConfig`, `sessionId`, `systemInstruction`, `toolConfig`, `tools` |
| body tail | one newline | the genai SDK writes the body with a `json.Encoder` |
| `X-Goog-Api-Client` | `google-genai-sdk/1.74.0 gl-go/<go> cl/985595000 +<revision> X:<experiments>` | the reference's value, verbatim in shape |

The last row is the only header that differs, and it differs in the three
fields that belong to the *binary*: the Go version (`go1.28-20260921-RC02`
against the clone's `go1.27.2`), the VCS revision, and the `GOEXPERIMENT`
list. Matching them would need the reference's own toolchain.

---

## 4. The tool surface

The harness's surface is **not** the CLI's.

| | CLI | harness |
|---|---|---|
| declarations | 12 (`model_tools_1.4.6.json`) | **18** (`harness_tools_1.4.7.json`) |
| reads a file | `read_file` | `view_file` |
| writes a file | `write_file` | `write_to_file` |
| edits a file | `edit_file` | `replace_file_content` |

A harness session that advertised the CLI's declarations would offer the
model a tool the harness never implements and hide the ones it does. The
capture is in `out/localharness/captures/harness_tool_declarations.json`.

The surface is also **client-owned**: the SDK tells the harness which tools
exist through `HarnessConfig.harness_side_tools`, and the harness
advertises exactly those. `cmd/harness/sidetools.go` maps the protocol's
fields onto the model-facing names (`find` → `find_by_name`, `subagents` →
`invoke_subagent` + `define_subagent` + `manage_subagents`, …), and
`send_message` and `finish` are always on.

---

## 5. The prompt

`internal/harnessprompt` rebuilds the reference's system instruction byte
for byte. Two flags drive its shape, and both come from the cascade
config, not from the client:

| flag | effect |
|---|---|
| the subagent tool | whether the `<subagents>` section exists, and whether the messaging section counts agents among its sources |
| the message feature | whether the messaging section's wakeup conditions appear |

The built-in subagent catalogue is `research` and `self`, recovered from
the binary's strings.

---

## 6. The tests

| test | what it pins |
|---|---|
| `cmd/harness/events_test.go` | the action mapping, the 0-based window, the shown-window derivation against the reference's five probes, the announcement's step index, the cumulative usage |
| `cmd/harness/policy_test.go` | the client's policy precedence: scope, then decision; an undecided rule is a denial |
| `cmd/harness/prompt_test.go` | the prompt's flags, the built-in and the client's subagents, the custom and the appended instruction |
| `cmd/harness/config_test.go` | the `HarnessConfig` resolution: the workspace, the behaviour, the skills, the cascade id |
| `cmd/harness/sidetools_test.go` | the protocol → model-facing name mapping; the eighteen declarations |
| `cmd/harness/harness_test.go` | the flags, the fail-closed verdicts, the whole protocol end to end, the cascade-id rejection |
| `internal/tools/harness_surface_test.go` | the eighteen captured declarations, the fixture on disk, `RelaxedSchema` |
| `internal/piagentbridge/surface_test.go` | the strict and the lenient runtime schemas; the advertised declaration is the captured one |
| `internal/backend/gemini_test.go` | the body's alphabetical keys, the trailing newline, `parametersJsonSchema` |

---

## 7. Deliberate divergence

The clone follows the **strict** reading of a verdict it cannot interpret:
anything that is not an explicit `ALLOW` denies the guarded action. The
reference's own wire layer is permissive (`preToolResultFromProto` derives
`denied` from `decision == DENY`), but its inner layer —
`checkCortexHookResults` — refuses to guess. The STOP hook is the
exception: an unrecognised verdict means `ALLOW_STOP`, because
`CONTINUE` is a loop that never ends. The continuation count is bounded at
16.

---

## 8. Limitations

- **The `X-Goog-Api-Client` header cannot match.** The Go version, the VCS
  revision and the experiment list belong to the binary that was built.
- **The usage frame's position is racy in the reference.** Three runs put
  it in three different places relative to the tool's start frame; the
  clone picks the placement that is consistent with the text-only turn.
- **The tool implementations are not the same code.** The clone runs the
  CLI's catalogue; the reference runs cortex's own. The observable
  difference is the tool's *output text* — the CLI's `view_file` counts the
  empty line after a trailing newline, the reference's does not — even
  though the action's window matches on every probe.
- **Only the arm64/macOS wheel was sampled.**
- **The fake endpoint answers one shape.** Streaming deltas, streamed tool
  arguments, thinking blocks and retries were not in the differential.
- **The hook, policy and budget paths were not differentially tested.** They
  have unit tests only.
- **Every address in the evidence is specific to one SHA-256.**

---

## 9. 硬约束 / 存疑 / limitations

**硬约束 (hard constraints).**

1. A cascade id shorter than 32 characters is refused, and the connection
   is closed **without** an initialize response.
2. The action's path fields are `file://` URIs, not bare paths.
3. `view_file`'s window is 0-based; the announcement, the start and the
   result frames carry the *requested* window, the done frame carries the
   window the tool *showed*.
4. `usage_update` carries the cumulative total.
5. A tool step's text is the literal `"Tool execution"`, and only the
   announcement frame's `textDelta` is non-empty.
6. The runtime argument check drops `required`; the advertised schema does
   not.
7. The request body's keys are alphabetical and it ends with a newline.
8. The declarations travel under `parametersJsonSchema`.
9. stdout is the protocol channel: the `OutputConfig` frame and nothing
   else.

**存疑 (open questions).**

- The reference's `usage_update` position is not deterministic, so "the
  usage comes right after the announcement" is an interpretation, not an
  observation.
- Whether the reference's `view_file` is the CLI's tool under a different
  branch or a separate implementation is not established; the line count
  differs, the derived window does not.
- The other tools' action fields were sampled, not probed exhaustively.

**limitations.** See §8.
