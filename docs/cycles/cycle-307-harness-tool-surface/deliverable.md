# The harness's tool surface — the reference's own vocabulary

**Cycle:** `cycle-307-harness-tool-surface`
**Subject:** cycle 306 drove the clone and the reference binary through the
same protocol and closed the frame-level gaps. It left the **tool surface**
itself uncompared: the reference's action vocabulary, its per-tool result
texts, its diff blocks, and the frame a *refused* call produces. This cycle
captures those against the reference and makes the clone match.
**Evidence:** 【一手】 the reference binary's own frames and request bodies,
captured live · 【反编译】 decompiled output at confirmed Mach-O addresses ·
【strings】 string-table hits only · 【推测】 hypothesis

---

## 0. Summary

Nine probes (`r_run`, `r_runq`, `r_write`, `r_grep`, `r_list`, `r_find`,
`r_view`, `r_edit`, `s_deny`) drove the reference binary and the clone
through the same fake model endpoint, in a workspace that is snapshotted
and restored between the two runs, and compared the frames and the request
bodies byte for byte.

The result:

| | before | after |
|---|---|---|
| `r_write`, `r_list`, `r_find`, `r_view`, `r_edit` frames | the action vocabulary was wrong | **0 differ** |
| `r_grep` frames | 4 differ (`numResults` × 3, the usage) | **0 differ** |
| `r_run` frames | 2 differ (the usage's position) | 0 differ — or 2 when the reference's own race fires (§6) |
| `r_runq` frames | 4 differ | 0 differ — or 2 when the reference's own race fires (§6) |
| `s_deny` frames | 8 differ (the clone announced a call the client refused) | **0 differ** |
| `s_deny` request body | the model saw the bare reason | **identical** (except the session id and the local time) |

Two claims in cycle 306's report and in `out/localharness/PARITY.md` are
corrected here:

* the `view_file` line-count difference is **retracted** — there is none,
  the two are byte-identical;
* the "racy `usage_update` position" claim **stands**, and is sharpened:
  the race is 24-to-3, and the clone now takes the majority order (§6).

---

## 1. The method

The harness is driven by the SDK's own protocol. The probe writes a
`HarnessConfig` into the `InitializeConversationEvent`, points the model
at a fake Gemini endpoint that replies with one scripted tool call, and
records every frame the harness writes and every request body it sends.

The workspace is snapshotted before the reference runs and restored before
the clone runs, so a probe that writes a file sees the same starting state
twice.

The comparison normalises the things that cannot match: the sequence
number, the timestamp, the prompt's local time, the `Created At` /
`Completed At` stamps, the tool call id (a UUID) and the session id.

---

## 2. The action vocabulary

The reference does not have one generic action. Each tool has its own, and
the clone now maps them the same way:

| tool | action | fields |
|---|---|---|
| `run_command` | `runCommand` | `commandLine`, `workingDir`; the done frame adds `exitCode` and `combinedOutput` |
| `write_to_file` | `editFile` | `filePath` and a `diffBlock` |
| `replace_file_content` | `editFile` | `filePath` and a `diffBlock` — on the **fourth** active frame *and* on the done frame |
| `grep_search` | `searchDirectory` | `directoryPath` (a `file://` URI), `query`, `numResults` |
| `list_dir` | `listDirectory` | `directoryPath`; the done frame adds `results[{name, fileSize}]` |
| `find_by_name` | `findFile` | `directoryPath` (a `file://` URI), `query`; the done frame adds `output` |
| `view_file` | `viewFile` | `filePath`, `startLine`, `endLine`, `contentOffset` |

`fileSize` is a **string**, not a number.

The number of active frames per call is a property of the tool, not a
constant:

| tool | active frames |
|---|---|
| `run_command`, the command printed something | 4 |
| `run_command`, the command printed nothing | 3 |
| `replace_file_content` | 4 |
| every other tool | 3 |

Two fields are **always present** where the reference always writes them,
even when their value is zero:

* `searchDirectory.numResults` is `0` on the announcement, the start and
  the result frames — the count is only known on the done frame.
* `runCommand.combinedOutput` is `""` on the done frame of a command that
  printed nothing.

That is proto3 *explicit presence*: setting the field is what makes it
appear. The clone's generated bindings take a plain `int32`/`*string`, and
the `Set…` method is what marks the field present — so the clone calls the
setter unconditionally rather than only when the value is interesting.

---

## 3. The result texts

The reference rewrites the result of **every** tool in its own words, and
the payload shape differs per tool:

* most tools answer `{"output": "<text>"}`;
* `grep_search` answers a structured `{"results": [{"path": …}]}`.

`run_command` uses the **step string** — `The command exited with code N.`
followed by `Output:` — not the CLI's own `Command exited with code N.`
The reference's terminal is a pty, so the output's line endings are
`\r\n`.

The clone's rendering lives in one new package, `internal/harnesstools`:
it owns the per-tool result text, the diff computation, and the parsing of
the clone's own `run_command` result. `cmd/harness/tools.go` wraps each
PiG `AgentTool` and re-renders the result through it; the structured
payload travels out of band in `AgentToolResult.Details` →
`ai.ToolResultMessage.Details` → `backend.Message.Result`, so the Gemini
transport can put the reference's own shape on the wire.

The wrapper has to forward `ArgumentSchema()` and `PrepareArguments`.
Without them PiG falls back to `Schema().Parameters` and validates
strictly, and every call the reference *runs* is rejected by the clone
with `Validation failed for tool "run_command": - toolSummary: must have
required properties toolSummary, toolAction`.

---

## 4. The diff blocks

`write_to_file` and `replace_file_content` carry a `diffBlock`:

```
{startLine, endLine, lines[{action, content}]}
```

with `action` in `NONE` / `DELETE` / `INSERT`. `endLine` is the **open**
upper bound of the *old* lines in the block.

The clone computes one change region — a common prefix and suffix with
three lines of context — where the reference computes a real line diff
with possibly several hunks. A single-hunk sample (`replace_file_content`
replacing one line) matches byte for byte; a multi-hunk sample has not
been probed.

---

## 5. The refused call

This was the largest gap. When the client's policy refuses a call, the
reference writes **one** frame — no announcement, no start frame, no
result frame:

```json
{
  "stepIndex": 1,
  "state": "STATE_ERROR",
  "source": "SOURCE_MODEL",
  "target": "TARGET_USER",
  "errorMessage": "the probe denies this tool",
  "textDelta": "the probe denies this tool (\"denied by pre-tool hook: the probe denies this tool\")",
  "text": "the probe denies this tool (\"denied by pre-tool hook: the probe denies this tool\")",
  "error": {
    "errorMessage": "the probe denies this tool (\"denied by pre-tool hook: the probe denies this tool\")",
    "httpCode": 0
  }
}
```

and the model sees the **failed step's** rendering, not the bare reason:

```
Created At: 2026-10-26T03:17:53+08:00
Encountered error in step execution: denied by pre-tool hook: the probe denies this tool
```

— the step template's shape, with a creation time and **no** completion
time.

The clone now evaluates the guards when the call is **announced**, not
when it runs. The reference asks the client before it announces the call,
so a call that is going to be refused is never announced. The verdict is
cached by the call's id and the PiG hook hands it back instead of asking
twice — which also means a dynamic rule costs one round trip, not two.

The two strings the reference produces come from one decision: the frame
carries the client's bare reason, and the model sees the failed step.
`internal/harnesstools.Denied` renders the step, `DeniedFrame` renders the
frame's text.

`httpCode: 0` is written explicitly — same explicit-presence rule as
`numResults`.

---

## 6. The usage frame's position

Cycle 306 reported the `usage_update` position as a race. It **is** a race,
but it is not evenly split. Over 27 captures:

| order | count | where |
|---|---|---|
| announcement → **start** → usage | **24** | `p_*`, `q_*`, `r_*` (batch 5) |
| announcement → **usage** → start | 3 | `r_run` / `r_runq` (batch 10), `view3` |

The clone now takes the majority order: the usage of a response that
carried tool calls is held (`holdUsage`) until the call's start frame has
been written (`flushUsage`). A call that never started — a refused one —
has no start frame, so the usage follows the refusal frame instead.

There is a **second** race in the reference: the number of frames a tool
call produces. The same `true` command gave three active frames in
`p_crlf3` (announcement, start, result) and two in `r_runq` (announcement,
start) — the result frame is sometimes coalesced away. The clone sends the
majority shape (the result frame is always there).

---

## 7. The architecture

```
cmd/harness/tools.go        the PiG AgentTool wrapper, the outcome store
cmd/harness/events.go       the action mapping, the extra frame, the
                             deferred usage, the refused-call frame
cmd/harness/conversation.go  the guards, the verdict cache
internal/harnesstools/      the result rendering, the diff, the pinned
                             reference texts
internal/tools/             the alias table (SearchDirectory → path) and
                             the grep pattern (query as well as pattern)
```

`internal/harnesstools` pins every captured reference text in a test, so
a change to the rendering fails a test rather than a differential.

---

## 8. Deliberate divergence

- The clone's `grep_search` reads the pattern from `query` as well as
  `pattern`: the reference spells the argument `Query`, and the CLI's
  global alias table maps that to `query`, not to `pattern`.
- The clone's diff block is a simplification (§4).
- The clone evaluates the client's policy at the announcement point. The
  reference does the same thing — the refusal is never announced — but the
  order of the *policy round trip* relative to the announcement is not
  observable on the wire.

---

## 9. 硬约束 / 存疑 / limitations

**硬约束 (hard constraints).**

1. A cascade id shorter than 32 characters is refused, and the connection
   is closed **without** an initialize response.
2. The action's path fields are `file://` URIs, not bare paths.
3. `view_file`'s window is 0-based; the announcement, the start and the
   result frames carry the *requested* window, the done frame carries the
   window the tool *showed*.
4. `usage_update` carries the cumulative total, and it is written **after**
   the tool's start frame.
5. A tool step's text is the literal `"Tool execution"`, and only the
   announcement frame's `textDelta` is non-empty.
6. The runtime argument check drops `required`; the advertised schema does
   not.
7. The request body's keys are alphabetical and it ends with a newline.
8. The declarations travel under `parametersJsonSchema`.
9. stdout is the protocol channel: the `OutputConfig` frame and nothing
   else.
10. A refused call writes **one** frame: `STATE_ERROR`, `TARGET_USER`, no
    announcement, no start frame.
11. Every tool has its own action and its own result text.

**存疑 (open questions).**

- The diff block is simplified: the clone computes one change region, the
  reference computes a real multi-hunk line diff. Only single-hunk samples
  were compared.
- `ask_question`'s failure puts the **usage** frame *before* the error
  frame, while a policy refusal puts it *after*. The reference's internal
  reason for the split was not recovered.
- A refusal from the client's lifecycle `PRE_TOOL` hook — as opposed to
  its policy rules — was not captured. The clone writes the same shape
  (hypothesis).
- The `run_command` action's `workingDir` and `write_to_file`'s content
  were sampled, not probed exhaustively.

**limitations.**

- **Only the arm64/macOS wheel was sampled.**
- **The fake endpoint answers one shape.** Streaming deltas, streamed tool
  arguments, thinking blocks and retries were not in the differential.
- **The hook and budget paths were not differentially tested.** The policy
  path was (`s_deny`).
- **Every address in the evidence is specific to one SHA-256.**
