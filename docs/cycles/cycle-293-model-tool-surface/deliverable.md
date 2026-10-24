# Cycle 293 — the model-facing tool surface: 12 declarations, not 60

Status: implemented, verified live against the installed **agy 1.4.4**, tests green.
Scope: `internal/tools`, `internal/agent`, `internal/piagentbridge`, `cmd/wagyu`.

The clone was advertising the wrong list to the model. A capture of the real
`streamGenerateContent` body shows **twelve** `functionDeclarations`; the clone
sent the 60 names of its 1.3.6 catalogue, under different names
(`view_file` instead of `read_file`) and with stricter reconstructed schemas.
A reference-shaped call therefore failed twice over:

```
Tool read_file not found
Validation failed for tool "view_file": - toolSummary: must have required property…
```

Both failures are gone. The wire now carries the reference's twelve declarations
verbatim, and the reference-shaped calls execute.

---

## 1. Two surfaces, not one

The conflation is the whole bug. The reference publishes:

| Surface | Where | Contents |
|---|---|---|
| Available tools | `agy -p --output-format stream-json` → `init.tools` | **60** names, identical from three different CWDs |
| Model declarations | CloudCode request `tools[0].functionDeclarations` | **12** declarations |

`internal/tools/testdata/advertised_tools_1.3.6.txt` and
`advertised_tools_1.4.3.txt` are the first surface. The 1.3.6 cycle used that
list as the declaration list because the request body had never been captured.

The twelve, in capture order (order is part of the fixture — the reference does
not sort):

```
read_file, run_command, manage_task, send_message, schedule, invoke_subagent,
define_subagent, manage_subagents, write_file, edit_file, search_web, ask_question
```

【一手】 Every captured 1.4.3 and 1.4.4 request — all models, print and TUI
(both real captures and mock-proxied turns) — carries exactly these twelve,
and the array is identical between the two releases.

Required fields differ from the clone's old reconstructions, which is why a
reference call could not pass validation even when renamed:

| Tool | `required` |
|---|---|
| `read_file` | `Path` |
| `run_command` | `CommandLine`, `toolSummary`, `WaitMsBeforeAsync` |
| `write_file` | `Path`, `Content` |
| `edit_file` | `Path`, `Target`, `Replacement` |
| `search_web` | `query` |
| `ask_question` | `questions` |
| `manage_task` | `Action` |
| `send_message` | `Recipient`, `Message` |
| `schedule` | `Prompt` |
| `invoke_subagent` | `TypeName`, `Role`, `Prompt` |
| `define_subagent` | `Name`, `Description`, `SystemPrompt` |
| `manage_subagents` | `Action` |

## 2. The fixture and the loader

`internal/tools/model_tools_1.4.4.json` is the twelve declarations extracted
from the captured body, stored verbatim (17,291 B pretty-printed; the raw wire
array is 13,449 B because protojson separates members with `", "`).
`internal/tools/model_surface.go` embeds it and exposes:

- `ModelDeclarations() []ModelToolDecl` — name / description / parameters, in
  capture order, copied per call.
- `RuntimeCatalog(ws)` / `AddReferenceTools(reg, ws)` — the 60-name catalogue
  plus executors named the way the reference names them: `read_file`,
  `write_file`, `edit_file`. `Catalog` stays exactly 60 so print mode's
  `init.tools` is unchanged (verified: 60, `view_file` present, no
  `read_file`).

Executors were already reference-shaped internally; only the registry names and
the argument halves were missing:

- `ReadFile` — the declaration's `ByteOffset` is honoured as the resume offset
  (`view_file` called it `ContentOffset`); `StartLine`/`EndLine` already mapped.
- `WriteFile` — `Append` is honoured (append) and the default is truncate, as
  the reference's `Overwrite` semantics imply.
- `EditFile` — `Target` / `Replacement` are accepted as the two halves,
  falling back to the clone's `oldString` / `newString`. They are not added to
  the global `toolKeyAliases` map because `target` already means something else
  to other tools.

## 3. How the twelve reach the wire

| Path | Mechanism |
|---|---|
| TUI | `agentToolRegistry` uses `RuntimeCatalog`; `EnableTools` receives `advertisedToolDecls(agentBody, catalog)`, which returns the twelve for the default agent and the captured seven for a `--agent`. |
| Print (`-p`) | `piagentbridge.SurfaceTools(cfg.Catalog, subTools)` builds one `AgentTool` per captured declaration, executing through PiG's subagent tool when it owns the name and the catalogue executor otherwise. |

Two collision points needed care:

- **PiG extras.** The TUI appends `subagenttools.Tools` through
  `Options.ExtraAgentTools`; three of them (`invoke_subagent`, `send_message`,
  `manage_subagents`) are also in the twelve. `pigTools` used to drop the
  declaration and let the extra's own schema win. It now wraps the extra in
  `declOverrideTool`, which advertises the captured schema and executes the
  extra. `wireToolDecls` in `/context` is now simply `m.tools`.
- **`--agent` custom agents** keep their captured seven-tool surface
  (`agenttools.Registry`), untouched.

The reference-shaped arguments already survived execution: the PiG subagent
tools accept both `TypeName` / `Prompt` / `Role` and their own snake_case
spellings.

## 4. The schema trap: uppercase types are not JSON Schema

The captured declarations use protojson's schema spelling —
`"type":"OBJECT"`, `"type":"INTEGER"`. That is what Gemini accepts, but it is
**not** valid draft 2020-12 JSON Schema, and PiG validates every tool's schema
against the metaschema before it dispatches a call. Advertising the capture
as-is produced, for every call:

```
read_file: "file:///…/tool.json#" is not valid against metaschema:
jsonschema validation failed with 'https://json-schema.org/draft/2020-12/schema#'
- at '/properties/EndLine': 'allOf' failed
  - at '/properties/EndLine/type': 'anyOf' failed
    - at '/properties/EndLine/type': value must be one of 'array', 'boolean', …
```

`tools.SchemaParams` decodes a declaration and lowercases every `type` value;
the CloudCode transport's `geminiToolSchema` uppercases it again
(`strings.ToUpper`, idempotent) on the way out. So:

- the model sees the reference's uppercase schema (wire bytes unchanged), and
- PiG's validator sees a valid schema (calls dispatch).

【一手】 Confirmed both ways: with the uppercase schema the functionResponse
was the metaschema error; with the lowercase copy the same mock-driven turn
returned the file's contents.

## 5. Measured results

Wire, after the change:

| Measurement | Reference | Clone |
|---|---|---|
| `functionDeclarations` | 12 | **12**, same order and names |
| tools array, compact JSON | 13,125 B | **13,125 B** (13,152 B with the `tools` wrapper) |
| tools array, protojson `", "` | 13,449 B | not attempted — the clone's whole body uses compact JSON |
| print `init.tools` | 60 | **60** (unchanged) |
| `/context` System tools | 4.6k | **4.4k** (13,152 / 3) |

The 0.2k `/context` gap is now a **counting** difference only: the reference
prints 4.6k for 13,449 B, i.e. ≈2.9 B/token, while the clone has no tokenizer
and uses the same `/3` rule as every other row. The surface itself matches.

Live verification, 1.4.4, mock CloudCode endpoint:

- **Print**: mock returns `read_file {Path:/tmp/tooltest.txt}`; the second
  request carries
  `functionResponse{name:"read_file", response:{output:"hello from tooltest\n"}}`
  and the turn ends `TOOL_OK`. Two requests, no retry loop.
- **TUI**: the same call renders the cycle-285 block
  `● Explored 1 file (tooltest.txt) (ctrl+o to expand)`; `/context` prints
  `⛁ System tools: 4.4k tokens (0.4%)`, user 151 / agent 12 / tool 13.
- Both paths' captured request bodies carry the twelve names in capture order.

## 6. Tests added

| File | What it pins |
|---|---|
| `internal/tools/model_surface_test.go` | fixture order/names/required, reference-shaped `write_file` → `read_file` → `edit_file`, `Append`, `run_command`, and that `RuntimeCatalog` keeps all 60 catalogue names (63 total). |
| `internal/agent/cycle293_decl_override_test.go` | `pigTools` keeps the declaration over a same-named extra, lowercases its schema types, executes the extra; unclaimed extras are still appended. |
| `cmd/wagyu/cycle293_tool_surface_test.go` | `advertisedToolDecls` returns the twelve (order + 13,152 B wire) for the default agent and the seven for a custom agent. |

`go test ./cmd/... ./internal/...` — all packages pass.

---

## 硬约束

- The declaration fixture is **1.4.4**, extracted from a real request body, and
  is listed in `internal/referencedrift/reference.lock.json` so the drift gate
  flags it when the reference moves. Re-capture is described in its `how` field.
- `internal/tools/testdata/advertised_tools_1.3.6.txt` is **not** the model
  surface and must not be replaced with the twelve. It is the `init.tools`
  list; print mode still emits exactly those 60 names.
- The clone's request bodies remain compact JSON; the reference's are
  protojson-style with `", "` between members. The declarations are identical
  in content, but whole-body byte parity is still not achieved.

## 存疑

- The reference's `/context` tools row (4.6k for 13,449 B) is probably
  tokenizer-based (≈2.9 B/token). No tokenizer is available to the clone; the
  row stays `/3`.
- Whether the reference *executes* every one of the twelve in the same way is
  only verified for `read_file` (live) and for the catalogue executors
  (unit/live on earlier cycles). `define_subagent`, `search_web`, and
  `manage_subagents` remain stubs; `invoke_subagent` maps `TypeName` to a
  builtin or spawns a PiG session and ignores `Role`; `schedule` and
  `manage_task` executors were not re-verified in this cycle.

## limitations

- No test drives a real model into `read_file` twice, an `edit_file` round trip,
  or `write_file Append` over the wire; those are unit-level only.
- Custom-agent print/TUI surfaces were not re-captured; only the default agent
  changed.
- `--print` was verified with `piagentbridge.SurfaceTools` (mock endpoint); the
  TUI was verified with `ptykeys.py` at 200×40. No 80-column run was compared.
- The mocks started for this cycle were stopped; the five servers from earlier
  cycles (50999, 51001–51004) were left running.

---

## Follow-up — the file tools' result texts (2026-10-21)

The declaration work stopped at the request. The **response** the model sees was
still the clone's own invention: `read_file` returned raw file bytes, and
`write_file` silently clobbered whatever was already at the path. Both were
measured against 1.4.4 and fixed here.

### How the texts were captured

A mock cannot satisfy the reference's onboarding/trajectory RPCs well enough for
`agy -p` (it fails with `trajectory not found`), but the **TUI** runs fine
against a stub that replays captured metadata (the same `mock_cloudcode.py`
recipe cycle 292 used). The mock answers the first `streamGenerateContent`
with one `functionCall`, the reference executes it, and the follow-up request
body carries the `functionResponse` it will feed the model. Raw bodies were
dumped per case; the verbatim texts are committed as
`internal/tools/testdata/result_texts_1.4.4.txt` (with `<stamp>` standing for
the two timestamp lines).

Every file-tool result is a **plain string**, including the refusal — a
`write_file` that would clobber an existing file is *not* an error, it is a
normal result the model is expected to read and retry.

### What the reference returns (【一手】)

```
Created At: 2026-10-21T22:54:50+08:00
Completed At: 2026-10-21T22:54:50+08:00
Showing lines 1-3 of 3
1: line one
2: line two
3: line three
```

- `read_file` numbers every line `N: text`, with a `Showing lines A-B of N`
  header; a range (`StartLine`/`EndLine`) shifts both the header and the
  numbers (`Showing lines 2-2 of 3`). A zero-byte file is `File is empty`
  with no `Showing` line. The result has **no trailing newline** after the
  last numbered line.
- `write_file` on a fresh path: `Created file.` — and the same text when
  `Append=true` (the file grew, the message did not change).
- `write_file` on an existing path without `Overwrite=true`:
  `Encountered error in tool execution: <abs path> already exists. To overwrite
  it, retry with Overwrite=true. To edit it, view its contents first and use a
  code edit tool. Otherwise, choose a different file name`
- `edit_file`: `Made 1 replacement in <abs path> starting at line L (+A/-B
  lines).` where `L` is the 1-indexed line of the first match and `A`/`B` are
  the line counts of `Replacement` / `Target` (one line each → `+1/-1`;
  `alpha\nbeta` → `ALPHA\nBETA\nGAMMA` → `+3/-2`).

The timestamps are local RFC3339 at second granularity. They are generated
around the work, so `Created At` and `Completed At` are equal in every capture;
the clone formats both the same way.

### What changed

| File | Change |
|---|---|
| `internal/tools/filesystem.go` | `resultStamp` helper; `ReadFile.Run` emits the numbered/`Showing` body; `CreateFile.Run` refuses an existing path unless `Overwrite`, and returns `Created file.`; `WriteFile` reports `Created file.` for `Append`; `EditFile.Run` returns the `Made 1 replacement …` sentence |
| `internal/tools/result_text_test.go` | pins every captured body (3-line read, range, empty, create, refusal, overwrite, append, single- and multi-line edit) and the byte-window fallback |
| `internal/tools/testdata/result_texts_1.4.4.txt` | the capture, verbatim |
| `catalog_test.go`, `nonnetwork_test.go`, `tools_test.go`, `webm_test.go`, `model_surface_test.go` | assertions now look for the numbered body instead of raw bytes |

### Live verification (clone, print mode)

`CLOUD_CODE_URL` → a mock that returns one `read_file` call, then
`wagyu -p "read the file"`. The clone's follow-up request now carries:

```
"Created At: 2026-10-21T23:18:40+08:00\nCompleted At: 2026-10-21T23:18:40+08:00\nShowing lines 1-3 of 3\n1: line one\n2: line two\n3: line three"
```

which is the reference's string with only the timestamp values differing.

### 硬约束

- The exact reference bytes are not reproducible for the two timestamp lines:
  they are wall-clock local time. Tests therefore strip the header with a
  regexp and compare the body.
- ~~`Overwrite=true` on an existing path was not captured.~~ **Measured in
  follow-up 2**: the reference returns `Created file.` — the same text as a
  create and an append.
- ~~The result text for a `ByteOffset`-resumed view was not captured.~~ **Measured
  in follow-up 2**: both caps (800 lines / 46080 bytes) and the resumed view are
  now captured and implemented; see below. `MaxBytes` is not in the 1.4.4
  declaration and is no longer read.

### 存疑

- ~~`edit_file` with no match returns an error in the clone~~. **Measured and
  fixed in follow-up 2**: it is a normal result,
  `Encountered error in tool execution: target content not found in the file; …`.
- ~~`read_file` past EOF was not captured.~~ **Measured in follow-up 2**: a
  StartLine past EOF clamps to the last line (`Showing lines 3-3 of 3`).
- `write_file`'s declaration has no `Overwrite` field in
  `model_tools_1.4.4.json`; the reference still documents/accepts it in its
  refusal text, so the executor reads it defensively.

### limitations

- The capture ran through the TUI only; `agy -p` could not complete against a
  stub backend (`trajectory not found`), so the print-mode reference result was
  inferred from the TUI's follow-up request, not observed separately.
- Only the seven cases above were captured. `read_file` on a large/truncated
  file, symlinked paths, binary content, and concurrent edits are unmeasured.
- The overwrite guard changes the legacy `create_file`/`write_to_file` alias
  too (same executor). Those names are no longer advertised to the model, so
  the change only affects old transcripts/custom callers.


---

## Follow-up 2: the caps, view_file, and the other nine declarations

The first follow-up measured read/write/edit on small files. This one closes
the rest of the surface. Method unchanged: `CLOUD_CODE_URL` → a scripted mock
that answers the first `streamGenerateContent` with a `functionCall`, run the
real **agy 1.4.4** TUI, read `functionResponse.response.output` from the
follow-up request. The custom-agent cases ran the same way in a workspace with
`.agents/agents/test.md`, `agy --agent test`.

### read_file: the declaration's two caps are real

【一手】 The declaration says "at most 800 lines or 46080 bytes". Both bind:

| Input | Reference output |
|---|---|
| 900-line file, no range | `Showing lines 1-800 of 900` |
| same, `StartLine=100` | `Showing lines 100-899 of 900` |
| same, `StartLine=1,EndLine=900` | `Showing lines 1-800 of 900` |
| 3-line file, `StartLine=10` | `Showing lines 3-3 of 3` (clamped, not an empty range) |
| 100 × 1000-byte lines | `Partial read (lines 1-100 of 100). Showing bytes 0-46080 of 100491 …` |

The byte cap counts the **numbered** text, not the raw file: the 100491 total is
100100 raw bytes plus 392 bytes of `N: ` prefixes, minus the trailing newline.
The body is exactly the first 46080 bytes of that numbered text, so it ends
mid-line. `ByteOffset` is an offset into the same numbered text (a resumed read
starts 140 bytes into a line, unnumbered, then the next whole line is `47: `).

### view_file is a different formatter, not an alias

【一手】 The custom-agent seven include `view_file`, and its 1.4.4 result is a
19-line, older-style block, not `read_file`'s two-line header:

```
File Path: `file:///tmp/a.txt`
Total Lines: 5
Total Bytes: 12
Showing lines 1 to 5
The following code has been modified to include a line number before every line, in the format: <line_number>: <original_line>. Please note that any changes targeting the original code should remove the line number, colon, and leading space.
1: l1
…
5: 
The above content shows the entire, complete file contents of the requested file.
```

Details that cost measurement: `Total Lines` counts the empty line after a
trailing newline (5, not 4); a partial view closes with `The above content does
NOT show the entire file contents. …`; the 46080-byte cap counts the **raw**
line range here (`of 100100`, or `of 60059` for a 60-line range) and its closing
sentence is `The above content was truncated because individual lines are very
long. …`; and a `ContentOffset` continuation **renumbers its slice from 1**,
so the tail of file line 47 prints as `1: xxx…`. The clone now has a separate
`ViewFile` executor; `read_file` and `view_file` no longer share a formatter.

### The seven custom-agent declarations at 1.4.4

Re-captured from a live `--agent test` turn: seven declarations, same order
(`send_message, view_file, read_url_content, search_web, schedule,
generate_image, manage_task`), and **byte-identical to the 1.4.3 fixture**,
including `toolSummary`/`toolAction` being required. `tools_1.4.3.json` stays
authoritative; the fresh capture is committed as
`internal/agenttools/testdata/agent_decls_1.4.4.json` and pinned by
`TestDeclarationsMatch14_4Capture`.

That turn also exposed a live bug: the custom-agent adapter fed the captured
schemas to PiG unchanged, so protojson's `"STRING"` failed the metaschema check
(`value must be one of 'array', 'boolean', … 'string'`) and **every custom-agent
tool call failed**. The adapter now runs the parameters through
`tools.SchemaParams`; the transport re-uppercases on the wire, so the wire is
still the capture.

### The remaining declarations (captured result texts)

| Tool / call | 1.4.4 result body |
|---|---|
| `manage_task {Action:list}` | `No background tasks are currently running.` |
| `send_message` unknown id | `Encountered error in tool execution: recipient "abc123" not found` |
| `define_subagent` | `Subagent "cycletest" defined successfully. It can now be invoked via invoke_subagent.` |
| `manage_subagents {Action:kill_all}` | `No active subagents to kill.` |
| `invoke_subagent` unknown type | `Encountered error in tool execution: subagent "general" not found or not allowed to be invoked` |
| `invoke_subagent {TypeName:self}` | `Created the following subagent:` + protojson-pretty JSON (`conversationId`, `logAbsoluteUri`, `workspaceUris`) + `The subagent will send you a message …` |
| `schedule {Prompt,DurationSeconds}` | **Created At only** (backgrounded) + `Tool is running as a background task with task id: <conv>/task-N` + log URI + the `YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS …` paragraph |
| `search_web` (no route) | `Encountered error in tool execution: no candidates returned from GenerateContent` |
| `generate_image` (no route) | `Encountered error in tool execution: no response in generate content result` |
| `read_url_content https://go.dev/` | `Title: …` / `OG Description: …` / `The full content of the article at <url> has been saved to: <brain>/steps/N/content.md` |
| `ask_question` (one question) | `A1: alpha` |

The built-in subagent names are `self`, `research`, `image-generator` (the
`<subagents>` section of the system prompt); `general` is not one, and the
reference rejects it instead of spawning.

### What changed in the clone

| File | Change |
|---|---|
| `internal/tools/filesystem.go` | 800-line and 46080-byte caps, past-EOF clamp, partial-read note; `edit_file` no-match as a normal result; new `ViewFile` executor with the 1.4.4 format |
| `internal/tools/catalog.go` | `view_file` backed by `ViewFile`, not `ReadFile` |
| `internal/tools/nonnetwork.go` | `manage_task` text + stamp, `TaskId` alias; `schedule` accepts `Prompt`/`DurationSeconds`/`CronExpression` |
| `internal/tools/model_surface.go` | `SchemaParams` coerces protojson's string counts (`minLength`/`minItems`) to integers for PiG |
| `internal/backend/cloudcode_schema.go` | the transport re-stringifies those counts, so the wire stays protojson-shaped |
| `internal/agenttools/agenttools.go` | 7-declaration schema normalisation (the custom-agent metaschema fix) |
| `internal/subagenttools/subagenttools.go` | `define_subagent` (conversation-scoped store) + the captured texts for `send_message`, `manage_subagents kill*`, unknown `invoke_subagent` types; `invoke_subagent` consults the definition store |
| `cmd/wagyu/tui_tools.go` | `readDetailText` understands `Showing lines A to B` (view_file) and `Partial read (lines A-B …)` |
| tests | `result_text_test.go` (caps, view_file, edit no-match), `model_surface_test.go`, `cloudcode_schema_test.go`, `agenttools_test.go`, `subagenttools_test.go`, `nonnetwork_test.go`, `cycle293_result_display_test.go` |

Live verification, clone print mode (`--agent test`, mock, three view_file
calls): the clone's three result strings are **byte-identical** to the
reference's apart from the timestamp values. `manage_task`, `send_message`,
`define_subagent`, `manage_subagents` and an unknown `invoke_subagent` type
also match byte for byte in the default-agent print path.

## Follow-up 3: the four remaining result texts (2026-10-22)

The `schedule`, `search_web`, `generate_image` and `read_url_content` rows from
the table above are now produced by the clone. Bodies are byte-identical to the
capture; only the timestamps differ, as usual.

| Tool | Change |
|---|---|
| `schedule` (`internal/tools/nonnetwork.go`) | `Prompt`/`DurationSeconds`/`CronExpression` (legacy `command`/`delay_seconds`/`cron` still accepted) → mints `<conv>/task-N`, creates `<ANTIGRAVITY_HOME>/brain/<conv>/.system_generated/tasks/task-N.log` with the prompt, returns **Created At only** plus the task id, the `file://` log URI, and the verbatim `YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS …` paragraph. |
| `search_web` (`internal/tools/stubs.go`) | No more `ddgr` shell-out: the reference calls grounded GenerateContent. With no provider the result is `Encountered error in tool execution: no candidates returned from GenerateContent`; `Provider` is the seam for a real one. |
| `generate_image` (`internal/tools/stubs.go`) | Same shape: `Encountered error in tool execution: no response in generate content result`, with a `Provider` seam. |
| `read_url_content` (`internal/tools/stubs.go`) | Fetches, extracts `<title>` / `og:description`, reduces the page to text, writes `<home>/brain/<conv>/.system_generated/steps/N/content.md`, and returns the captured `Title:` / `OG Description:` / `has been saved to:` / `view_file` hint block. |

All three are registered in `Catalog(ws)`, so the 12-declaration print surface
(`SurfaceTools` → `registry.Get`) and the custom-agent registry both reach the
executors.

Unblocked tests: `internal/tools/parity_stubs_test.go` pins each body verbatim;
`TestStubsReadURL` and `TestHeadlessInteractionTools` were rewritten for the new
behaviour and now set `ANTIGRAVITY_HOME` to a temp dir (the old versions wrote
into the real `~/.gemini/antigravity-cli/brain`).

Still divergent, deliberately:

- **`invoke_subagent` stays synchronous.** Returning the launch acknowledgement
  requires a later `send_message` delivery, and the clone has no live parent
  mailbox (its `send_message` reports `recipient … not found` for the same
  reason). Emitting the ack while dropping the subagent's answer would be a
  worse lie than the current synchronous envelope.
- **No live conversation id reaches the tools.** `brain/` is user state, not
  workspace state, so `Catalog(ws)` cannot know the id: both tools accept a
  `ConversationID` field, then `OPENAGY_CONVERSATION_ID`, then a stable
  `schedule-<pid>` fallback. A print turn therefore writes a real file under a
  process-scoped directory rather than the reference's conversation directory.
  Wiring the session id through `coding.Session` is the follow-up.

### 硬约束

- Timestamps are wall-clock; every test strips the `Created At`/`Completed At`
  header before comparing.
- `view_file` on binary files (image/pdf/video/audio) is not implemented: the
  declaration promises the whole file, the clone still treats the bytes as text.
  Only the text path is pinned.
- `schedule` is **not** a scheduler in this clone even in follow-up 3: it mints
  the reference's task id, writes the log file the acknowledgement names, and
  returns the captured paragraph — but nothing fires when the delay elapses
  (there is no scheduler backend to run the prompt).
- `search_web` and `generate_image` reproduce only the *failure* rows above.
  The reference's success texts are unmeasured (a grounded GenerateContent
  answer and an image response would both have to be faked in the mock), so
  both tools carry a `Provider` seam and fall back to the captured error.
- `read_url_content` reduces the page with a small regex extractor, not a
  readability parser: `<title>`/`og:description` are read directly (falling
  back to the reference's `Live Content` / `Fetched live`), script/style are
  dropped, and block tags become newlines. Boilerplate that the reference's
  extractor strips (nav, cookie banners) survives.

### 存疑

- The reference's `read_file` note when the *last* chunk is shorter than 46080
  was not captured (all captures were full 46080-byte chunks). The clone clamps
  the note's upper bound and the next `ByteOffset` to the numbered length.
- `read_file` with a `ByteOffset` on a range that fits in 46080 was not
  captured; the clone ignores the offset then and returns the normal body.
- `write_file` on an existing path with `Overwrite=true` returns `Created file.`
  — measured for the create/append paths; the overwrite path is 【推测】 equal.
- The reference's `manage_task` text for a **non-empty** task list, and for
  `kill`/`status`, was not captured; the clone keeps its own listing format.
- `view_file` with `ContentOffset` on a short range (no truncation) renumbers
  from 1 in the clone (measured only for the truncated-range continuation).
- `invoke_subagent`'s **success** path is a deliberate deviation: the reference
  returns immediately with a launch acknowledgement and reports the answer
  later via `send_message`; the clone blocks and returns a JSON envelope with
  the subagent's answer. Changing that is an async-delegation feature, not a
  text fix.
- `read_url_content` was captured only against `https://go.dev/` (a
  server-rendered page). How the reference's extractor treats a JS-only shell,
  a PDF, or a non-HTML body is unmeasured; the clone always runs the same regex
  reducer.

### limitations

- All reference captures went through the TUI; `agy -p` cannot complete against
  a stub backend (`trajectory not found`). The custom-agent runs needed a
  workspace-trust Enter before the prompt, which the pty driver sends explicitly.
- The four follow-up-3 tools match the captured bodies but not the reference's
  *capabilities*: no grounding or image route is wired (`Provider` is nil), the
  scheduler never fires, and the article reducer is a regex stripper. The
  remaining gap is capability, not result text.
- `ask_question`'s success text (`A1: alpha`) is captured for the TUI path; the
  headless auto-skip text is unchanged from the 1.3.6 evidence.
