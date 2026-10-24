# Reference drift 1.4.6 → 1.4.7 — deliverable

**Cycle:** `reference-drift-147`
**Reference:** `agy` **1.4.7** (`~/.local/bin/agy`, self-updated from the 1.4.6
the clone was emulating; sha256 `b7a13e53…`)
**Evidence:** `docs/cycles/reference-drift-147/captures.md`

---

## 0. Summary

`agy` moved from 1.4.6 to 1.4.7. The clone's version pin moved with it, and
the cycle re-evidenced every surface the drift gate watches. Most of it is
quiet: **the `--help` surface, the 60-name advertised list and the twelve
model-facing declarations are byte-identical to 1.4.6.** The reference changed
the *custom-agent* tool surface and nothing else on the model's side.

What this cycle did:

1. **Aligned the version and the changelog.** `internal/cli.ReferenceVersion`
   is now `1.4.7`; `internal/cli/testdata/changelog_reference.txt` is the live
   `agy changelog` of the 1.4.7 binary (174,284 B, 1,007 lines, 77 sections,
   854 bullets, sha256 `b1b3cd19…`), and the five pinned constants in
   `internal/cli/changelog_data_test.go` follow it.
2. **Re-evidenced every artifact the gate watches**, and re-sealed the
   registry (§8). Two artifacts stay SUSPECT — honestly, and for stated
   reasons.
3. **Implemented the one real behavioural change**: the **custom-agent tool
   surface** was rewritten at 1.4.7. `toolSummary`/`toolAction` are gone from
   all seven declarations, the descriptions were reworded to the current
   wording, and the schemas gained `minLength`, an `enum`, `maxItems` and a
   new `schedule.ExactTiming` property. The clone's `--agent` request body is
   now byte-for-byte identical to the reference's (§3.3).
4. **Re-extracted the remote-control descriptors** from the 1.4.7 binary: the
   public service grew 77 → **79** RPCs (`ListAgents`, `GetChangelistStatus`),
   the internal one gained `ConfigOverrides.is_cel_only` / `cel_type`.

Two things this cycle did **not** do, both stated plainly in §10: the **seed
set and the whole Ghidra pipeline** were not regenerated (the reverse
engineering cycle is separate work, and every address moves on every
release), and the **tool result texts** were not re-captured.

### 0.1 Against the gate's SUSPECT list

Before this cycle the gate reported eleven artifacts as suspect. Their end
state:

| artifact | before | after |
|---|---|---|
| `internal/cli/testdata/changelog_reference.txt` | SUSPECT | re-captured at 1.4.7 ✓ |
| `internal/agyprompt/testdata/capture_1.4.6.txt` | SUSPECT | re-verified byte-identical ✓ (static sections) |
| `internal/agyprompt/testdata/capture_1.4.6.json` | SUSPECT | **still SUSPECT** — the prompt *inputs* (§2.4) |
| `internal/agyprompt/testdata/user_rules_1.4.5.txt` | SUSPECT | re-verified ✓ (the 24 KB truncation path was not) |
| `internal/tools/testdata/advertised_tools_1.4.3.txt` | SUSPECT | re-verified byte-identical ✓ |
| `internal/tools/model_tools_1.4.6.json` | SUSPECT | re-verified byte-identical ✓ |
| `internal/tools/testdata/result_texts_1.4.4.txt` | SUSPECT | **still SUSPECT** — not re-captured (§10) |
| `internal/agenttools/testdata/agent_decls_1.4.4.json` | SUSPECT | **superseded** — the surface changed (§3) |
| `internal/remotecontrol/testdata/jetski_remote_agent_proxy_1.4.6.txt` | SUSPECT | **superseded** by the 1.4.7 list (79 RPCs) |
| `out/local-macos/protos/remote_agent_proxy_1.4.6.txt` (+ `_pb_`) | SUSPECT | re-extracted at 1.4.7 ✓ |
| `internal/cli/testdata/help_1.4.6.txt` | (not tracked) | added to the registry, re-verified ✓ |

---

## 1. What moved

### 1.1 The version

`internal/cli/version.go` pins one string:

```go
const ReferenceVersion = "1.4.7"
```

### 1.2 The changelog

The 1.4.7 binary's feed published the `1.4.6:` section — eleven bullets. The
1.4.7 release notes are **not** in the feed (the binary ships one release
behind, as always).

| | 1.4.6 capture | 1.4.7 capture |
|---|---:|---:|
| bytes | 171,493 | **174,284** |
| lines | 994 | **1,007** |
| version sections | 76 | **77** |
| top-level bullets | 843 | **854** |
| newest section | `1.4.5:` | **`1.4.6:`** |

The five constants in `internal/cli/changelog_data_test.go` and the shape
assertion in the same file follow. The TUI's changelog panel
(`internal/cliapp/slash_panels_test.go`) opens on `1.4.6` now, and the
`/changelog` viewer's header row assertion moved with it.

---

## 2. What did **not** move

This is the part worth stating first, because it is most of the surface.

### 2.1 `--help` — byte-identical

All thirty commands, stdout and stderr captured separately with a throwaway
`HOME`:

```sh
$ diff internal/cli/testdata/help_1.4.6.txt /tmp/help_1.4.7.txt && echo IDENTICAL
IDENTICAL
```

The flag table, the column widths and every per-command usage block are
unchanged. The golden was added to the drift registry as a live artifact
(`captured_at: 1.4.7`) — it was not tracked before.

### 2.2 The advertised list — byte-identical

Sixty names, byte-identical to the 1.4.3 / 1.4.5 / 1.4.6 capture
(`46dcfca2…`).

### 2.3 The model's twelve declarations — byte-identical

With `--sandbox`, the live 1.4.7 request body compares `True` against
`internal/tools/model_tools_1.4.6.json` — the whole list, field by field.

Without `--sandbox` the only difference is that the reference omits
`run_command.BypassSandbox`. **That parameter is conditional on `--sandbox`,
not on the version** — the same finding the 1.4.6 cycle recorded, re-measured
on a new binary. The fixture therefore keeps its name and its bytes; only the
lock's `captured_at` moved.

### 2.4 The system prompt — the static sections are byte-identical

The **eight** static sections (`identity`, `terminal_sandbox`, `artifacts`,
`conversation_transcript`, `guidelines`, `communication_style`,
`slash_commands`, `messaging`) are byte-identical to the committed 1.4.6
capture. The `<terminal_sandbox>` section and the `BypassSandbox` parameter
are again conditional on `--sandbox`, not on the version.

The environment-driven sections (`subagents`, `user_rules`, `skills`,
`user_information`) differ — because this machine's skill set and this
repository's `AGENTS.md` changed, not because the reference changed.

**The prompt's *inputs* were not re-recorded.** `capture_1.4.6.json` carries
the `os_version` / `workspaces` / `subagents` / `skills` / `user_rules` inputs
`Build()` is fed. Re-recording them means re-discovering the skill list from
the filesystem — it cannot be parsed back out of the rendered prompt — so the
pair stays at the 1.4.6 inputs and the lock keeps that artifact SUSPECT.
That is the honest state: the *rendering* is verified against 1.4.7, the
*inputs* are not.

The `<user_rules>` artifact is re-verified at 1.4.7 as far as it goes: the
`<RULE[path]>` wrapper renders the same way. What was **not** re-exercised is
the 24 KB truncation marker — this machine's rules no longer reach that
threshold, so that path is untested at 1.4.7.

---

## 3. The custom-agent tool surface — the one real change

### 3.1 What changed

The seven declarations a `--agent` markdown agent gets
(`send_message`, `view_file`, `read_url_content`, `search_web`, `schedule`,
`generate_image`, `manage_task`) kept their names and their order. Everything
else moved:

| | 1.4.4 capture | 1.4.7 capture |
|---|---|---|
| `toolSummary` / `toolAction` | on all seven, required | **gone from all seven** |
| `required` lists | `+ toolSummary + toolAction` | the real parameters only |
| `send_message` | "Send a message to another agent. This tool can be used to communicate with subagents, peer agents, etc. Do not use this tool to communicate with the user." | "Send a message to another agent, such as a subagent or a peer. Not a channel to the user." |
| `schedule` | old wording | new wording, `ExactTiming` property added |
| `generate_image` | old wording | new wording, `AspectRatio` gained an `enum`, `ImagePaths` gained `maxItems: "3"` |
| `manage_task` | old wording | new wording |
| `minLength` | none | `"1"` on `send_message.Message` / `.Recipient`, `generate_image.Prompt` / `.ImageName` |

`view_file`, `read_url_content` and `search_web` kept their descriptions; only
their `toolSummary`/`toolAction` parameters went away.

### 3.2 How the finding was made, and what rules it out

Three things had to be ruled out before the change could be attributed to
1.4.7 rather than to the probe:

* **The agent body.** The probe agent was written to *demand* the old
  parameters ("When you call a tool, you must include a `toolSummary` … and a
  `toolAction` …"). The reference ignored the demand: the declarations it sent
  carry neither.
* **The model.** The same agent and workspace were run against a Gemini model
  and a Claude model. The seven names, their order, their `required` lists and
  the absence of `toolSummary`/`toolAction` were identical. Only `view_file`'s
  *description* differed — by the binary types the model can read
  (`image, pdf, video, audio` vs `image, video`), which is a per-model
  capability list.
* **The `strings` table.** Both the old and the new `send_message` wording
  are still in the 1.4.7 binary. The old one belongs to the reference's
  **subagent** surface, not to the custom-agent surface. A `strings` hit
  proves nothing about which surface a declaration is on — only a captured
  request body does.

What is **not** ruled out: the change landed somewhere in 1.4.5–1.4.7. The
1.4.6 binary is gone (it self-updated), so it cannot be re-checked. The
1.4.4 capture is the last one that shows the old shape.

### 3.3 What the clone does now

`internal/agenttools/tools_1.4.7.json` (embedded) and
`internal/agenttools/testdata/agent_decls_1.4.7.json` (the test's copy) hold
the new capture; the 1.4.3 / 1.4.4 files are gone.

The clone's own `--agent test` request body, captured through the same proxy
and compared to the reference's, is **identical**:

```
clone == reference (byte-for-byte JSON): True
```

`internal/agenttools/agenttools_test.go` gained
`TestDeclarationsCarryNoToolSummaryOrAction`, which fails if either parameter
ever comes back — a model that still sent them would be asking for a
parameter the reference does not accept.

No executor needed changing: none of the seven required `toolSummary` or
`toolAction`, and the schemas are advisory to the model, not to the clone.

---

## 4. The remote-control surface

### 4.1 The public descriptor — 77 → 79 RPCs

Both descriptors were re-extracted from the 1.4.7 binary.

| file | offset | bytes | methods | messages | enums |
|---|---|---:|---:|---:|---:|
| `google/gca/aicode/v1main/remote_agent_proxy.proto` | `0x3b2124f` | 46,301 | **79** | **174** | 5 |
| `third_party/jetski/remote_agent_proxy_pb/remote_agent_proxy.proto` | `0x37ab385` | 4,361 | 0 | 10 | 3 |

The two appended RPCs:

```
78  ListAgents               ListAgentsRequest               ListAgentsResponse
79  GetChangelistStatus      GetChangelistStatusRequest      GetChangelistStatusResponse
```

with

```proto
message ListAgentsRequest {
}

message ListAgentsResponse {
  repeated PersistentAgent agents = 1;
}

message GetChangelistStatusRequest {
  repeated int64 cl_numbers = 1;
  bool include_content = 2;
}

message GetChangelistStatusResponse {
  repeated exa.language_server_pb.ChangelistStatus statuses = 1;
}
```

`ListAgents` returns the `PersistentAgent` records that `CreateAgent` /
`GetAgent` (added at 1.4.6) already deal in — the agent registry is
becoming listable. `GetChangelistStatus` is about the language server's
changelists (`cl_numbers`, with an `include_content` flag) — the IDE-side
"which change list is this" query.

### 4.2 The internal descriptor — a CEL hook

`ConfigOverrides` gained two fields:

```
bool   is_cel_only = 8
string cel_type    = 9
```

CEL is Common Expression Language. A `cel_type` on the environment's config
overrides, with an `is_cel_only` flag, is the shape of a *computed* config:
the override value is an expression, evaluated against something, rather than
a literal.

### 4.3 The clone's side

`internal/remotecontrol/reference_surface.go` pins the 79-name list;
`ReferenceRPCMethods` gained the two names; the fixture moved to
`testdata/jetski_remote_agent_proxy_1.4.7.txt` and the test's expected count
moved with it.

**Neither new RPC has a clone implementation.** Like the 1.4.6 additions
(`CreateAgent`, `GetAgent`) they are pinned as names only, so the clone's
dispatcher answers them as unknown methods rather than pretending to
implement them.

---

## 5. The evidence files

`out/local-macos/protos/` gained the 1.4.7 dumps:

| file | what it is |
|---|---|
| `remote_agent_proxy_1.4.7.txt` | the public descriptor, verbatim |
| `remote_agent_proxy_1.4.7.proto` | its proto3 rendering |
| `remote_agent_proxy_pb_1.4.7.txt` | the internal descriptor, verbatim |

The rendering was produced with the repository's own
`out/local-macos/render_proto.py`, which was checked first by re-rendering
the committed 1.4.6 file and diffing: **byte-identical**. (The renderer drops
proto options and flattens oneofs — that is the committed convention, and the
`.txt` dumps keep everything.)

The 1.4.6 and 1.4.5 dumps stay on disk as the previous releases' evidence.

---

## 6. The changes

| file | change |
|---|---|
| `internal/cli/version.go` | `ReferenceVersion` → `1.4.7` |
| `internal/cli/testdata/changelog_reference.txt` | the 1.4.7 capture |
| `internal/cli/changelog_data.go` | the doc comment's numbers |
| `internal/cli/changelog_data_test.go` | the five constants, the shape assertion |
| `internal/cli/changelog_v133_test.go` | the newest-bullet prefix → `1.4.6:` |
| `internal/cliapp/slash_panels_test.go` | the changelog panel's headings → `1.4.6` |
| `internal/agenttools/tools_1.4.7.json` | **new** — the 1.4.7 custom-agent capture |
| `internal/agenttools/testdata/agent_decls_1.4.7.json` | **new** — the same capture |
| `internal/agenttools/agenttools.go` | the embed path, the package doc |
| `internal/agenttools/agenttools_test.go` | the test renamed; the no-`toolSummary` gate added |
| `internal/remotecontrol/testdata/jetski_remote_agent_proxy_1.4.7.txt` | **new** — 79 methods |
| `internal/remotecontrol/reference_surface.go` | `+ ListAgents`, `+ GetChangelistStatus`; the doc comment |
| `internal/remotecontrol/reference_surface_test.go` | the fixture path, the count 79 |
| `internal/referencedrift/reference.lock.json` | version, `lag_note`, per-artifact `captured_at` / `how`, digests |
| `out/local-macos/protos/remote_agent_proxy_1.4.7.{txt,proto}` | **new** — the public descriptor and its rendering |
| `out/local-macos/protos/remote_agent_proxy_pb_1.4.7.txt` | **new** — the internal descriptor |
| `out/local-macos/protos/README.md` | the file table, the counts, the offsets, the drift section |
| `README.md`, `AGENTS.md`, `npm/pigasus/package.json` | the version statements |
| `docs/guide/prompt.md`, `docs/guide/tools.md`, `docs/guide/reference-drift.md` | the 1.4.7 entries |

Deleted: `internal/agenttools/tools_1.4.3.json`,
`internal/agenttools/testdata/agent_decls_1.4.4.json`,
`internal/remotecontrol/testdata/jetski_remote_agent_proxy_1.4.6.txt` — all
three were superseded by a capture with different content.

---

## 7. The parity table

| surface | 1.4.6 | 1.4.7 | verdict |
|---|---|---|---|
| `--help` (30 commands) | golden | **identical** | ✓ |
| `init.tools` (60 names) | golden | **identical** | ✓ |
| model declarations (12) | `model_tools_1.4.6.json` | **identical** (with `--sandbox`) | ✓ |
| system prompt (8 static sections) | `capture_1.4.6.txt` | **identical** | ✓ |
| `<terminal_sandbox>` / `BypassSandbox` | present | present, `--sandbox`-conditional | ✓ |
| custom-agent declarations (7) | `tools_1.4.3.json` | **rewritten** | **changed** |
| changelog feed | 76 sections | 77 sections | changed (expected) |
| remote-control RPCs | 77 | **79** | changed |
| internal proto | 10 messages | 10 messages, +2 fields | changed |
| tool result texts | `result_texts_1.4.4.txt` | not re-captured | **unverified** |
| seeds / Ghidra project | 161,286 functions at 1.4.6 | not regenerated | **stale** |

---

## 8. The registry

`internal/referencedrift/reference.lock.json`:

* `version` → `1.4.7`.
* **No `lag_acknowledged_through`.** The live binary *is* 1.4.7 and the clone
  is pinned to 1.4.7, so there is no lag to acknowledge. The field must be
  strictly newer than the pin, so it is absent rather than equal.
* A `lag_note` explaining, in one paragraph, what was re-captured and what
  was not.
* Per-artifact `captured_at` / `how` updated for the eleven artifacts the
  gate watches, plus the `--help` golden added as a new entry.
* Digests regenerated with `go test ./internal/referencedrift/ -run
  TestReferenceLock -update`.

The gate is green and its report is honest:

```
reference-drift: live /Users/gear/.local/bin/agy is 1.4.7; this clone is pinned to 1.4.7 (lag acknowledged through 1.4.7).
  1.4.7 is the live binary and the clone's pin, so no lag is acknowledged. …
  SUSPECT  internal/agyprompt/testdata/capture_1.4.6.json — …
  SUSPECT  internal/tools/testdata/result_texts_1.4.4.txt — …
```

---

## 9. The version statements

`README.md`, `AGENTS.md` and `npm/pigasus/package.json` all said 1.4.6 and now
say 1.4.7.

One wrinkle worth naming: the npm package's `version` was already `1.4.7`
(the other agent bumped it), while its `pigasusVersion` — the field the
downloader uses to pick the release binary — was still `1.4.6`. Leaving that
alone would have made the npm wrapper download the 1.4.6 binary, so the two
fields are now consistent.

---

## 10. 硬约束 — hard constraints

* **The `--agent` request body is now byte-identical to the reference's.**
  Seven declarations, same order, same descriptions, same schemas. Do not
  "improve" them.
* **`toolSummary` and `toolAction` must not come back** to the custom-agent
  surface. They belong to the reference's subagent surface; the 1.4.7
  custom-agent surface has neither, and a model that sent them would be
  asking for a parameter the reference does not accept.
  `TestDeclarationsCarryNoToolSummaryOrAction` guards this.
* **`run_command.BypassSandbox` and the `<terminal_sandbox>` section are
  `--sandbox`-conditional, not version-conditional.** A capture without
  `--sandbox` will look like a drift and is not one.
* **A `strings` hit does not identify a tool surface.** The old wording is
  still in the 1.4.7 binary. Only a captured request body does.
* **The two new remote-control RPCs are names, not implementations.** The
  clone must answer them as unknown methods.
* **`capture_1.4.6.json` stays at the 1.4.6 inputs.** The skill list must be
  re-discovered from the filesystem; it cannot be parsed out of the rendered
  prompt.

---

## 11. 存疑 — open questions

* **When exactly did the custom-agent surface change?** Somewhere in
  1.4.5–1.4.7. The 1.4.6 binary is gone, so the boundary cannot be pinned
  from this machine.
* **Why did the custom-agent surface change at all?** The new wording is
  *identical* to the main agent's wording (`send_message`, `search_web`,
  `schedule`, `manage_task` all match the 1.4.7 main surface). The most
  likely explanation is that 1.4.7 unified the custom-agent surface with the
  main one — but that is a hypothesis fitted to one data point, not a
  measurement. The alternative — that the *main* surface changed at some
  point and the custom-agent one lagged — fits the same evidence.
* **What is the reference's subagent surface?** It still exists (the old
  wording is in the binary), but it was not captured: it needs a turn in
  which the model actually calls `invoke_subagent`.
* **What does `cel_type` evaluate against?** `is_cel_only` / `cel_type` on
  `ConfigOverrides` is a CEL hook, but nothing in the descriptor says what
  the expression is evaluated over.
* **Is `GetChangelistStatus` reachable from the CLI at all?** It is in the
  *remote-control* service, and the reference answers no local JSON-RPC
  (`docs/cycles/rc-wire-live-verify/`). It may be an IDE-only path.

---

## 12. Limitations

* Every capture came from **one machine** and **one account**. A different
  account could see a different catalogue, a different plugin set or a
  different `<subagents>` list.
* The captures are unredacted request bodies held in a `0700` directory
  outside the repository. **No capture file was committed**; the fixtures
  derived from them carry no bearer token, but they do carry this machine's
  absolute paths.
* The `<skills>` and `<user_rules>` sections of the 1.4.6 capture are *this
  machine's* — the shared skill library and this repository's own `AGENTS.md`.
  They are evidence of the reference's rendering, not of a portable
  environment.
* **The seed set, the Ghidra project and everything derived from them are
  still at 1.4.6.** Every address in `out/local-macos/seeds/`, in
  `out/local-macos/proj`, in `out/local-macos/dec/` and in the 102 committed
  `out/local-macos/decompile-*.md` reports is stale with respect to the
  installed binary. Citing one for a 1.4.7 finding would be citing whatever
  function happens to sit there now.
* **The tool result texts were not re-captured.** The 32 result strings in
  `internal/tools/testdata/result_texts_1.4.4.txt` are from 1.4.4 and the
  gate keeps reporting them as SUSPECT.
* The `.proto` rendering in `out/local-macos/protos/` is a reading aid: proto
  options, visibility rules and custom field behaviour are dropped, and
  oneofs are flattened. The `.txt` dumps keep everything.
* The clone's custom-agent surface is the *reference's* — the clone does not
  implement a separate "subagent surface", so the two surfaces that the
  reference keeps apart are one surface here.
