# Cycle 350 deliverable: agy 1.4.8 — the delta, the failed-tool-call envelope, and the language server's two permission methods

**Cycle:** `cycle-350-agy-1.4.8-alignment`
**Subject:** the reference moved from 1.4.7 to **1.4.8** on 2026-10-27. This
cycle establishes the delta first-hand, implements the one change that touches
the wire, answers the two methods the language server gained, and re-pins the
evidence registry.
**Evidence:** 【一手】 first-hand against the binary — live request bodies,
decoded descriptors, the served changelog feed · 【反编译】 decompiled output at
confirmed Mach-O addresses · 【strings】 string-table hits only · 【推测】
hypothesis

---

## 0. The short answer

The reference binary on this machine is now
`~/.local/bin/agy` = **1.4.8**, sha256
`09fcdd8554e03bed9b2ec936e454c7f115d5610610de09ed7d79fa2feae76940`.

| | 1.4.7 | 1.4.8 |
|---|---|---|
| the model-facing tool surface | 12 declarations | **the same 12, byte for byte** |
| `init.tools` | 60 names | **the same 60, byte for byte** |
| `--help` | 30 commands | **the same, byte for byte** |
| the prompt's static sections | 8 | **the same 8, byte for byte** |
| the remote-control proto | 79 RPCs | 79 RPCs, **one field more** (`ProjectResource.default_branch`) |
| the language-server proto | 351 RPCs | **358 RPCs** (+8, −1) |
| `model_profiles.json` | 57 635 B | **57 984 B** (three aliases, one placeholder, one flag) |
| the pclntab seeds | 165 692 names | **166 181** (+782, −293) |
| chroma | v2.27.0 | **v2.27.0, byte for byte** |
| the served changelog feed | ends at 1.4.7 | **ends at 1.4.7 — the feed did not grow** |
| **a failed tool call** | `{"output": "<text>"}` | **`{"error": "<text>"}`** |

The last row is the release's behavioural change and the only one the *model*
sees. The clone now speaks it.

---

## 1. What did not move

Everything below was compared against the live 1.4.8 binary, byte for byte.

- **The `--help` surface** — the flag table, the column widths and every
  per-command usage block are unchanged.
- **The 60 advertised tool names** (`init.tools`) and the **63 names the
  binary knows** (the backing array moved to file offset `0xb623bb0`, the
  slice header to `0xb5ba450`; the length is still 63).
- **The twelve model-facing declarations** — same names, same order, same
  required fields, same prose.
- **The prompt's eight static sections** — identity, `terminal_sandbox`,
  artifacts, `conversation_transcript`, guidelines, `communication_style`,
  `slash_commands`, messaging. The environment-driven sections differ only
  because this machine's skill set and this repository's `AGENTS.md` moved.
- **The public remote-control descriptor** — 79 RPCs, 174 messages, 5 enums,
  16 imports; the internal one and the prompt descriptor are byte-identical.
- **chroma** — 279 lexers and 74 styles, still upstream v2.27.0, so the
  clone's pin does not move.
- **The overflow message** (438 B at `0x36c73e2`) and the **session-summary
  prompt** (3 937 B at `0x37bdb8d`).
- **Every tool description** — every literal (or its fragment) the 1.4.7
  capture records is still present in the 1.4.8 binary.

## 2. What did move

### 2.1 The language server's proto: 351 → 358 RPCs

【反编译】 `third_party/jetski/language_server_pb/language_server.proto`, read
out of the 1.4.8 binary at file offset `0x3c81c00` (164 508 B): **831
messages, 26 enums, 358 RPCs**.

Eight arrived — `WarmUpEnvironment`, `GetVoiceCallDebug`,
`ListBackgroundTasks`, `CallMcpAppTool`, `SetPluginAuthAccountLabel`,
`GetMarketplaceFeatured`, `GetPermissionSettings`,
`UpdatePermissionSettings` — and `GetResolvedCascadeConfig` left.

The list is pinned in `internal/remotecontrol/testdata/language_server_rpcs_1.4.8.txt`
and checked by a 358-method gate that also diffs the list against the
1.4.7 one (`out/langserver/coverage-351.json`).

### 2.2 The two permission methods

【一手】 the same descriptor:

```proto
message GetPermissionSettingsResponse {
  exa.codeium_common_pb.AgentPermissionPreset   global_preset            = 1;
  exa.codeium_common_pb.ArtifactReviewMode       global_artifact_review_mode = 2;
  exa.codeium_common_pb.PermissionGrantsConfig   global_grants            = 3;
}

message UpdatePermissionSettingsRequest {
  exa.codeium_common_pb.AgentPermissionPreset   preset              = 1;
  exa.codeium_common_pb.ArtifactReviewMode       artifact_review_mode = 2;
  exa.codeium_common_pb.PermissionGrantsConfig   grants              = 3;
}

message UpdatePermissionSettingsResponse {}   // no fields
```

The wire is canonical proto-JSON: `globalPreset`,
`globalArtifactReviewMode`, `globalGrants{allow,deny,ask}`, the enums spelled
by name, and the update's answer is `{}`.

The vocabulary itself came from a second descriptor —
`third_party/jetski/codeium_common_pb/codeium_common.proto` at `0x3cd5130`
(185 828 B):

```proto
enum AgentPermissionPreset {
  AGENT_PERMISSION_PRESET_UNSPECIFIED    = 0;
  AGENT_PERMISSION_PRESET_DEFAULT        = 1;
  AGENT_PERMISSION_PRESET_REQUEST_REVIEW = 2;
  AGENT_PERMISSION_PRESET_TURBO          = 3;
  AGENT_PERMISSION_PRESET_VETTED         = 4;
  AGENT_PERMISSION_PRESET_NONE           = 5;
  AGENT_PERMISSION_PRESET_AUTO           = 6;
}

enum ArtifactReviewMode {
  ARTIFACT_REVIEW_MODE_UNSPECIFIED = 0;
  ARTIFACT_REVIEW_MODE_ALWAYS      = 1;
  ARTIFACT_REVIEW_MODE_TURBO       = 2;
  ARTIFACT_REVIEW_MODE_AUTO        = 3;
}
```

【反编译】 the 1.3.5 recovery of these two enums had **guessed** four preset
values (`Unspecified`, `Strict`, `Balanced`, `Permissive`) and three review
modes (`Unspecified`, `Inline`, `Sidebar`) from the binary's *strings*. None of
the guessed names exists in the descriptor. They are corrected here, and
because proto-JSON spells an enum by its name, the names matter more than the
numbers.

### 2.3 The catalogue

【一手】 `model_profiles.json` at `0x9ab160a` (57 984 B). Three aliases were
added (`M429`, `M430`, `M431`, all folding onto `M408`),
`base.browser-subagent-model`'s placeholder became `gemini-3.8-flash-low`, and
`base.enable-marketplace-search-tool` became `true`.

### 2.4 The seeds

【一手】 the pclntab seed set moved 165 692 → 166 181 names (782 added, 293
removed). The 20 MB seed file and the Ghidra project are **not** committed at
1.4.8 — only the name-level diff is
(`out/local-macos/seeds/diff_1.4.7_1.4.8.txt`).

### 2.5 The changelog feed did not grow

【一手】 `agy changelog` at 1.4.8: **177 232 bytes, 1 023 lines, 78 sections,
868 bullets** — byte-identical to the 1.4.7 re-capture, and its newest
section is still **1.4.7**. The 1.4.8 release notes are published in the
vendor's repository
(`internal/cli/testdata/changelog_upstream_1.4.8.md`, fetched 2026-10-27,
sha256 `2d2a18bc…` for the section) but the *served* feed does not carry
them yet.

The clone's `/changelog` therefore still prints exactly what the reference
prints. The upstream notes are committed as evidence about the release, not
as a feed capture — the fixture stays a verbatim capture.

---

## 3. The wire change: a failed tool call is an error

### 3.1 The claim

【一手 — the vendor's own release notes, `internal/cli/testdata/changelog_upstream_1.4.8.md`]

> Fixed failed tool calls, including calls to a tool that doesn't exist and
> canceled or interrupted calls, being passed back to the agent as ordinary
> results; the agent now sees them as errors and can recover on its next step.

The 1.4.7 capture says the same thing from the other side — its header reads
*"Every result is a plain string under `functionResponse.response.output`,
never an error, even for the refusal."*

### 3.2 The capture

【一手 — live, 2026-10-27, `internal/tools/testdata/tool_errors_1.4.8.txt`]

Three runs of the real 1.4.8 binary against `devutils/refcapture`, reading the
`functionResponse` the reference sends back in the follow-up request body:

| the call | the envelope | the body |
|---|---|---|
| `read_file` on a file that exists | `{"output": …}` | the two stamp lines, then the result |
| `read_file` on a file that does not exist | `{"error": …}` | the two stamp lines, then `Encountered error in tool execution: file does not exist` |
| `view_file` — a known name the model-facing surface does not carry | `{"error": …}` | the two stamp lines, then `Encountered error in tool validation: unknown tool: "view_file" — check spelling` |

The **body did not change**: it is the same string the clone already writes,
stamp lines and all. What changed is the **member** it travels in.

### 3.3 What the clone does now

| | before | now |
|---|---|---|
| the signal | none — every result was an `output` | `backend.Message.IsError` |
| where it is read | — | `tools.IsFailureText` — the reference's own marker, read out of the result's body |
| the CloudCode envelope | `{"output": <text>}` | `{"error": <text>}` for a failure |
| the Gemini API-key envelope | `{"output": <text>}` | `{"error": <text>}` for a failure |
| the gateway dialects | unchanged | **unchanged on purpose** |

The check is deliberately narrow: the marker has to be the **first thing in
the body**, after the two stamp lines. A result that merely *mentions*
`Encountered error in tool execution:` — a file that contains the sentence, a
grep hit — is an ordinary result.

The clone's own tools fail by *writing* the reference's wording into an
ordinary result, so the wording is the only signal there is; a tool that
*throws* is flagged by the runtime instead, and both paths set the same flag.

---

## 4. The language server's permission methods

【一手】 the two methods are new to the clone as well as to the reference, and
they speak the reference's vocabulary, not the clone's. The clone's dashboard
methods (`SetRemoteAgentPermissionPreset` and its siblings) speak a
three-value vocabulary the clone invented; the language server speaks
`exa.codeium_common_pb`'s.

The translation lives in **one** file — `internal/remotecontrol/permission_vocabulary.go` —
and the pairing is marked as a judgement, not a fact:

| the clone | the reference | |
|---|---|---|
| `default` | `AGENT_PERMISSION_PRESET_DEFAULT` | 【推测】 |
| `strict` | `AGENT_PERMISSION_PRESET_REQUEST_REVIEW` | 【推测】 |
| `permissive` | `AGENT_PERMISSION_PRESET_NONE` | 【推测】 |
| `always` | `ARTIFACT_REVIEW_MODE_ALWAYS` | 【推测】 |
| `never` | `ARTIFACT_REVIEW_MODE_TURBO` | 【推测】 |
| `auto` | `ARTIFACT_REVIEW_MODE_AUTO` | 【推测】 |

The **names** are first-hand (the descriptor). The **pairing** is not: the
review modes are anchored by the reference's own three display names
(*always-proceed*, *agent-decides*, *asks-for-review*), which fixes the
direction — ALWAYS asks for the review, AUTO lets the agent decide, TURBO
proceeds without one. The presets have no such one-to-one anchor.

The grant model stays **allow-only**: a grant rule is the reference's own
`"<tool>:<pattern>"` spelling, and an update that carries `deny` or `ask`
rules is **refused loudly** (an `invalid_argument`, −32602) rather than
silently dropped.

A name collision worth knowing: the *remote-agent proxy* service has a
`GetPermissionSettings` of its own, whose response carries two
`PermissionSettings` messages (global and project). The clone serves the
language server, and the language-server table is consulted **first**
(`dispatch_languageserver.go`), so the flat shape is the one the clone
answers.

---

## 5. The registry

`internal/referencedrift/reference.lock.json` is rewritten for 1.4.8: the pin
moves, the twelve artifacts that were re-captured get `captured_at: 1.4.8`,
the four artifacts whose *content* changed are replaced by their 1.4.8
counterparts (the two remote-control dumps, the prompt dump, the catalogue),
and four new artifacts are registered (the tool-error capture, the language
server's RPC list, the permission enums, the seed diff).

The eight artifacts that were **not** re-captured keep their old stamps and
are reported as SUSPECT by the gate — which is the honest state.

---

## 硬约束 — hard constraints

1. **A failed tool call travels under `response.error`; a successful one
   under `response.output`.** The body is the same string either way. The
   signal is read with `tools.IsFailureText`, never by matching a Go error.
2. **The reference's failure text is the only failure signal the clone has.**
   Its own tools write the wording into an ordinary result; changing the
   wording would silently unmark every failure.
3. **The two reference dialects are the only ones that carry the envelope.**
   The gateway dialects (OpenAI, Anthropic) are not the reference's dialect
   and keep their own conventions — the Anthropic `tool_result` block has an
   `is_error` member the clone still does not set.
4. **The clone's changelog prints the feed, not the upstream notes.** The
   upstream 1.4.8 section is evidence about the release; the feed is what the
   reference prints.
5. **The language-server table is consulted before the alias table.** A
   method that exists in both is the language server's.
6. **The permission vocabulary is one file.** The translation is in
   `internal/remotecontrol/permission_vocabulary.go` and nowhere else.
7. **An update that carries `deny` or `ask` rules is refused**, not
   silently truncated.

## 存疑 — open questions

1. **The unknown-tool wording is not the reference's.** The reference answers
   a call to a tool outside the model-facing surface with
   `Encountered error in tool validation: unknown tool: "view_file" — check
   spelling`. The clone's runtime (PiG) answers `Tool view_file not found`.
   Both are errors and both travel under `response.error` now, but the text
   differs. Matching it exactly would mean intercepting PiG's dispatch.
2. **The preset pairing is a judgement.** The enum names are first-hand; the
   mapping from the clone's three values onto them is 【推测】.
3. **The reference's own grant lists are wider than the clone's.** The
   reference's `settings.json` carries allow *and* deny rules; the clone's
   store keeps allow only, and refuses a write that carries the rest.
4. **The cancelled/interrupted case was not captured.** The release notes
   name it beside the failed call; only the failed call was reproduced here.
5. **The seed set and the Ghidra project are still at 1.4.7.** Every address
   in `out/local-macos/dec/` moved with the release.
6. **The `codeium_common_pb` descriptor carries ~564 other mismatches**
   between the clone's recovered types and the reference's; only the three
   types the new methods speak were corrected.

## Limitations

- **The captures are print-mode, single-turn.** `devutils/refcapture` scripts
  one functionCall and reads the answer. Nothing here exercises a multi-step
  turn, the TUI's rendering of a failed call, or the localharness dialect.
- **The two new language-server methods answer JSON only.**
  `internal/lsproto/descriptor/language_server.protoset` is still the 1.4.7
  schema, so the proto-JSON answer is hand-shaped rather than generated from
  the descriptor.
- **The `IsError` flag is not persisted.** A resumed conversation replays
  the stored text, and the flag is re-derived from that text — which works
  only because the wording is the reference's.
- **The upstream changelog was fetched from the network.** It is a moving
  target; the sha256 in the file's header pins what was read on 2026-10-27.
- **The 1.4.8 model catalogue is committed as a document, not as the
  binary's bytes.** The 20 MB seed file and the Ghidra project are not
  rebuilt.

---

## Files

| | |
|---|---|
| `internal/tools/failure.go` | **new** — the reference's failure marker and `IsFailureText` |
| `internal/tools/failure_test.go` | **new** — the marker, and what is *not* a failure |
| `internal/backend/client.go` | `Message.IsError` |
| `internal/backend/cloudcode_request.go` | the envelope: `error` for a failure, `output` otherwise |
| `internal/backend/gemini.go` | the same envelope for the Gemini API-key dialect |
| `internal/backend/tool_error_envelope_test.go` | **new** — both dialects |
| `internal/piagentbridge/stream.go` | the flag is set from the runtime *or* from the text |
| `internal/piagentbridge/failure_envelope_test.go` | **new** — the three ways a call can fail |
| `internal/tools/testdata/tool_errors_1.4.8.txt` | **new** — the three captured envelopes |
| `internal/remotecontrol/permission_vocabulary.go` | **new** — the reference's permission vocabulary and the translation |
| `internal/remotecontrol/permissions_ls.go` | **new** — the two new language-server methods |
| `internal/remotecontrol/permissions_ls_test.go` | **new** — the answers and the refusal |
| `internal/remotecontrol/dispatch_languageserver.go` | the two new arms, before the alias table |
| `internal/remotecontrol/language_server_rpcs_test.go` | **new** — the 358-method gate and the delta |
| `internal/remotecontrol/testdata/language_server_rpcs_1.4.8.txt` | **new** — the 358 methods |
| `internal/codeium_common_pb/types.go` | the three corrected types and their wire names |
| `internal/codeium_common_pb/permission_enums_test.go` | **new** — the enum names and numbers |
| `internal/codeium_common_pb/testdata/permission_enums_1.4.8.txt` | **new** — the descriptor's own text |
| `internal/modelctx/profiles_1.4.8.go` / `_test.go` | the 1.4.8 catalogue (renamed), three aliases, the delta test |
| `internal/modelctx/testdata/model_profiles_1.4.8.json` | **new** — the catalogue, verbatim |
| `internal/cli/testdata/changelog_upstream_1.4.8.md` | **new** — the vendor's own 1.4.8 notes |
| `internal/cli/version.go` | the pin moves to 1.4.8 |
| `internal/cliapp/slash_panels_test.go` | the changelog panel opens on 1.4.7 now |
| `internal/rctunnel/client.go` | the advertised version is derived, not hard-coded |
| `internal/referencedrift/reference.lock.json` | the registry, rewritten for 1.4.8 |
| `out/local-macos/protos/README.md` | the 1.4.8 offsets and the drift section |
| `out/local-macos/protos/*_1.4.8.txt` | **new** — the two remote-control dumps and the prompt dump |
| `out/local-macos/seeds/diff_1.4.7_1.4.8.txt` | **new** — the name-level seed diff |
| `docs/guide/tools.md` | *A failed tool call is an error, not a result* |
| `docs/guide/models.md`, `compaction.md`, `reference-drift.md`, `remote-control.md` | the 1.4.8 claims |
| `AGENTS.md` | the version, and the new invariant |
