# Reference drift

> The gate that notices when the reference binary moves, and how to re-evidence an artefact.

## Reference drift (`internal/infra/referencedrift`, cycle `reference-drift`)

The parity claim in the README — "byte-level parity with `agy X.Y.Z`" — expires, and
Google's auto-updates expire it without asking. `internal/infra/referencedrift` is what notices.

`reference.lock.json` records three things:

| Field | Meaning |
|---|---|
| `version` | the reference release the clone emulates. Must equal `cli.ReferenceVersion`; `internal/cli`'s `TestReferenceVersion_MatchesDriftLock` pins the two together. |
| `lag_acknowledged_through` | the newest upstream release we have **consciously declined** to re-evidence, with `lag_note` saying why. |
| `artifacts[]` | every version-scoped fixture: `path`, `captured_at`, `digest` (sha256), `live`, and `how` to re-verify. |

The gate's semantics are the non-obvious part, and they are deliberate:

- A **digest mismatch or missing artifact** is a bug in *this* repo. Always fails.
- The live binary **past** `lag_acknowledged_through` fails — nobody has looked at that release.
- The live binary **within** the acknowledged ceiling passes, but the report lists which
  artifacts are now suspect.

So the gate is **not** "live version must equal the pin". Lagging upstream is normal for a
reverse-engineering workspace, and a gate phrased that way goes permanently red on every
patch release, gets skipped, and recreates the exact false confidence it exists to remove.
Accepting drift is a committed decision with a `git blame`, not an env var.

`CompareVersions` orders numerically; `"1.4.10" < "1.4.9"` lexicographically, which would let a
patch release read as *older* than the pin and walk drift straight through the gate.

An artifact's `captured_at` is allowed to exceed `version` — the changelog is captured at 1.4.3
while the clone still emulates 1.4.2. The two fields answer different questions ("what do we
emulate" vs "when was this recorded"). What is forbidden is an artifact stamped from a release
*newer than the installed binary*.

When the reference moves:

```sh
go test ./internal/referencedrift/                          # see what is now suspect
agy changelog > internal/cli/testdata/changelog_reference.txt   # re-capture what you can
go test ./internal/referencedrift/ -run TestReferenceLock -update   # reseal the digests
```

`-update` is idempotent. It rewrites digests only — `captured_at` stays put, because moving an
artifact onto a newer reference is the decision the gate is meant to make loud.

Reports: `docs/cycles/reference-drift/deliverable.md`. Known still-unverified against 1.4.3:
`internal/app/prompt/agyprompt/testdata/capture_1.4.4.{txt,json}` (re-evidenced at 1.4.4) and
`internal/app/tools/surface/testdata/advertised_tools_1.4.3.txt` (re-verified at 1.4.4).

**Re-evidenced at 1.4.5** (`docs/cycles/reference-drift-145/`): eight of the
gate's ten SUSPECT artifacts were re-captured live and compared byte for byte.
Only `capture_1.4.4.json` (its inputs are environment-specific) and
`internal/app/tools/testdata/result_texts_1.4.4.txt` (the 32 result texts need a
tool-using turn under a pty harness) are still unverified. The re-verification
found four real drifts, all fixed in that cycle: the `<user_rules>` semantics,
a new `ExactTiming` parameter on `schedule`, a 72nd RPC
(`FetchSandboxPort`) in the public proto, and `ConfigOverrides.autonomous` in
the internal one.

**Re-evidenced at 1.4.6** (`docs/cycles/reference-drift-146/`): the prompt
changed, so the 1.4.4 pair was *superseded* by `capture_1.4.6.{txt,json}` and
the model-declaration fixture by `model_tools_1.4.6.json`; the shared
`~/.gemini/skills` library **is** in the prompt (the earlier "`/skills`-only"
claim came from captures taken on a machine whose library was empty), the
custom agent's body is split at level-1 Markdown headings, the public proto
grew 72 → 77 RPCs, and `run_command` gained `BypassSandbox`. The clone's
`agyprompt.Build()` now reproduces the live 1.4.6 prompt **byte for byte**
(70,853 B). `result_texts_1.4.4.txt` is the one artifact still SUSPECT — it
was not re-captured, and the gate keeps saying so.

**Re-evidenced at 1.4.7** (`docs/cycles/reference-drift-147/`): the clone is
pinned to 1.4.7 and the changelog feed grew the `1.4.6:` section. The
`--help` surface (30 commands), the 60-name advertised list and the twelve
model declarations are all **byte-identical** to 1.4.6 — the `<terminal_sandbox>`
section and `run_command.BypassSandbox` are conditional on `--sandbox`, not on
the version. The one real behaviour change was the **custom-agent tool
surface**: all seven declarations were rewritten (`toolSummary`/`toolAction`
dropped, descriptions reworded, `minLength`/`enum`/`maxItems` added). The
public proto grew 77 → 79 RPCs (`ListAgents`, `GetChangelistStatus`) and the
internal one gained `ConfigOverrides.is_cel_only` / `cel_type`.
`result_texts_1.4.4.txt` is still the one artifact SUSPECT — it was not
re-captured, and the gate keeps saying so.

**Re-evidenced at 1.4.8** (`docs/cycles/cycle-350-agy-1.4.8-alignment/`): the
clone is pinned to 1.4.8. The **served changelog feed did not grow** — its
newest section is still 1.4.7, so the 1.4.8 release notes (published in the
public repository) are not in the feed the reference reads. `--help`, the
60-name advertised list, the twelve model declarations and the prompt's static
sections are all **byte-identical**; the public proto kept its 79 RPCs and
gained one field (`ProjectResource.default_branch`), the internal proto and
the prompt descriptor are byte-identical, and chroma is still upstream
v2.27.0. What did move: the **language server's** proto grew 351 → 358 RPCs
(eight arrived, `GetResolvedCascadeConfig` left), the embedded
`model_profiles.json` changed (three new aliases, the browser-subagent
placeholder, the marketplace-search flag), the seed set moved 165,692 →
166,181 names, and the one **behavioural** change on the wire: a failed tool
call now comes back under `response.error` instead of `response.output`
(【一手】, captured live — `internal/app/tools/surface/testdata/tool_errors_1.4.8.txt`).

**Re-evidenced at 1.4.9** (`docs/cycles/cycle-362-agy-1.4.9-alignment/`): the
clone is pinned to 1.4.9 (sha256 `a66421b7…`). The **served changelog feed
grew by one section** — the 1.4.8 section arrived, so the feed is now
180,524 bytes / 1,038 lines / 79 sections / 881 bullets — but its newest
section is **still 1.4.8**: the 1.4.9 release notes live in the vendor's
public repository only, and they are recorded verbatim in
`internal/cli/testdata/changelog_upstream_1.4.9.md`, the way the 1.4.8 notes
were. `--help` is byte-identical to the 1.4.6 capture once the clone's own
four commands are removed. What did move is **behaviour**, and the clone
implements six of the fourteen 1.4.9 bullets: an unmatched tool call or
result is left out of the request (both the CloudCode dialect and the
Gemini API-key dialect), the invisible U+FFF9 / U+FFFA never reach the
terminal, the plugin validator checks the name and the logo, Vim's empty
last line and carried operator behave, a corrupt image is rejected with the
real header offset, and image pasting attaches an image from the clipboard
or from a dropped file. The other eight are documented in the cycle report
— four are not applicable to the clone's architecture, four are open.

**Not** re-verified at 1.4.9, and therefore still stamped 1.4.8: the sixty
advertised tool names, the twelve model declarations, the system prompt's
static sections, the remote-control and language-server descriptors, chroma,
the embedded `model_profiles.json`, the overflow message, the
session-summary prompt and the tool result texts.

**Re-evidenced at 1.4.10** (`docs/cycles/cycle-363-agy-1.4.10-alignment/`): the
clone is pinned to 1.4.10 (sha256 `35605054…`). The **served changelog feed
grew by one section** — the 1.4.9 section arrived, so the feed is now
184,882 bytes / 1,054 lines / 80 sections / 895 bullets — but its newest
section is **still 1.4.9**: the 1.4.10 release notes live in the vendor's
public repository only, and they are recorded verbatim in
`internal/cli/testdata/changelog_upstream_1.4.10.md`, the way the 1.4.9 notes
were. `--help` is byte-identical to the 1.4.6 capture once the clone's own
four commands are removed. What did move is **behaviour**, and the clone
implements four of the six 1.4.10 bullets: startup memory trim after 15 seconds
via `runtime/debug.FreeOSMemory` (`internal/infra/memtrim`), post-compaction size
counts carried content (`internal/core/compaction`), subagent compaction keeps
parent's task, ID, and 10 most recent parent messages (`internal/app/subagenttools`),
and image limits are capped at 2000px per side with large PNG converted to JPEG
and MIME type following bytes (`imagery`). The pclntab seed set is regenerated
from the 1.4.10 binary (167,678 names recovered, 3,751 added and 2,255 removed
vs 1.4.8), the Ghidra project was rebuilt (167,658 functions created, 0 failed),
and 2,366/2,366 priority functions were decompiled with 0 errors across 110 packages
(345,662 LOC).

**Re-evidenced at 1.4.11** (`docs/cycles/cycle-365-agy-1.4.11-version-alignment/`):
the clone is pinned to 1.4.11 (sha256 `09cefed6…`). Cycle 364 had already
mapped the 1.4.11 *behaviour* onto the clone; this cycle moves the **version
number** with it — `internal/cli.ReferenceVersion`, the drift lock, the npm
wrapper's `version` / `pigasusVersion`, the wasm runner's `1.4.11-wasm`, and
the `README.md` / `AGENTS.md` statements (which still said 1.4.9). The
**served changelog feed grew by one section** — the 1.4.10 section arrived,
so the feed is now 186,601 bytes / 1,062 lines / 81 sections / 901 bullets —
but its newest section is **still 1.4.10**: the 1.4.11 release notes live in
the vendor's public repository only, and they are recorded verbatim in
`internal/cli/testdata/changelog_upstream_1.4.11.md`, the way the 1.4.10
notes were. Because the pin now equals the live binary's version, the lock's
`lag_acknowledged_through` was **removed** (a lag acknowledgement must be
strictly newer than the pin) and the gate reports the twenty-odd artefacts
that were captured from an older binary as SUSPECT.

**The RE pipeline was regenerated at 1.4.11** (commit `cc4690f8`, "regenerate
priority function decompiles for agy 1.4.11"). The seed set
(`out/local-macos/seeds/`) holds **168,548** functions and the lock records it
as captured at 1.4.11; the Ghidra project, the decompiled output, the priority
list and the **112** per-package `out/local-macos/decompile-*.md` reports were
rebuilt from it — **2,397/2,397** priority addresses resolved, 0 unresolved,
350,577 LOC across 111 Go packages. `out/local-macos/decompile-summary.md` is
the headline table; `out/local-macos/FINDINGS.md` still carries the older
1.4.6 numbers in its "current tree" section, and the summary is the newer
record.

The seed set was last regenerated at 1.4.6 (`out/local-macos/seeds/`, 161,286
functions), and so was everything derived from it — the Ghidra project
(`out/local-macos/proj`), the decompiled output (`out/local-macos/dec/`), the
priority list, the MCP query plan and the 102 per-package
`out/local-macos/decompile-*.md` reports (2,290/2,290 addresses resolved, 0
skipped). The seed set is a registered lock artifact, so the gate now watches
it too. **It was *not* regenerated at 1.4.7 or 1.4.8** — every address moves on every
release, so the RE pipeline is a separate cycle. The 1.4.8 seed set is not
committed either: only the *name-level* difference is
(`out/local-macos/seeds/diff_1.4.7_1.4.8.txt`, 782 names added, 293 removed).

### Re-anchoring the citations without rebuilding the pipeline

The seed *set* staying at 1.4.7 does **not** mean the repository's addresses
stay at 1.4.7. Cycle `359` re-anchored every address this repository cites
against the 1.4.8 binary without touching the RE pipeline:

| what | how it was re-anchored |
|---|---|
| function entries | each 1.4.7 address was looked up in the 1.4.7 seed set, its *name* taken from there, and that name looked up again in the 1.4.8 seed set |
| rodata literals | each 1.4.7 address was resolved to its bytes in the 1.4.7 binary, the bytes matched against the 1.4.8 binary, and the 1.4.8 address taken from the match |
| data globals | slice headers and string headers were re-read out of the 1.4.8 binary's data section |

The second row is the one that cannot be skipped: a *name* is not a unique
key in a Go binary (a method is emitted once per receiver shape), so a
function address was never remapped by arithmetic — it was remapped by name
lookup plus a byte-level confirmation. Every remapped pair was then checked
back against the 1.4.8 seed set, and every literal against the 1.4.8
binary's bytes. Report:
[`cycle-359-agy-1.4.8-reanchor`](../cycles/cycle-359-agy-1.4.8-reanchor/deliverable.md).

Two things this cycle could **not** do, and therefore did not do:

- the **IDE language server** is a *different binary* with its own address
  space. Its 170,786-name seed set is byte-identical to the committed one, so
  every `LS 0x…` citation in the tree is still correct and was left alone.
  The `localharness` binary is a third address space and was left alone for
  the same reason.
- the **type descriptor** addresses (`abi.Type` pointers) cannot be resolved
  out of a stripped Go binary — the `namedata` layout they point at is not
  recoverable statically. Two citations that used one were replaced with
  evidence that *is* recoverable, rather than being carried forward.

---

## The 1.5.16 → 1.5.17 delta

> Recorded 2026-11-26. This section is the durable evidence for the bump: the
> `out/extracted-1.5.16/` tree was retired in the same change, so the
> differences are written down here rather than left implicit in a directory
> that no longer exists.

Extracted with `python3 scripts/re/extract_agy.py --bin ~/.local/bin/agy` into
`out/extracted-1.5.17/`. The release notes came from
`python3 scripts/re/fetch_changelog.py --version 1.5.17`, stored as
`internal/cli/testdata/changelog_upstream_1.5.17.md`.

### Unchanged

| Artifact | Evidence |
|---|---|
| Built-in changelog feed | byte-identical: 259,799 bytes / 1,352 lines / sha256 `b777354c…25e521`. `changelog_reference.txt` and the five constants in `changelog_data_test.go` did not change. |
| Embedded chroma lexers | all 353 XML files, same file set and same bytes |
| `prompt`, `trajectory`, `trajectory_steps`, `cascade_plugins` protos | descriptor bytes unchanged (only the file offset moved) |

### Changed

`cortex` proto (`exa.cortex_pb`, 516 messages / 90 enums in both releases):

```
RunCommandToolConfig:
  + 28 repeated RunCommandToolConfig.EnvEntry env;

PolicyGuardianConfig:
  + 26 TYPE_BOOL enable_proctor_read_budget;
  + 27 PolicyGuardianConfig.SkillScanMode proctor_skill_scan_mode;

PolicyGuardianCoverage.Area (new values 7..16):
  + url_data_exfil, destructive_action, remote_code_exec, dependency_install,
    skill_supply_chain, mcp_tool_use, recon, user_coercion,
    post_exec_tampering, network_exposure
```

`remote_agent_proxy` proto (`google.gca.aicode.v1main`): 368 → 391 messages,
20 → 26 enums. New messages include `MemoryConfig` (client_id, root_dir, mounts,
memory_rules_disabled, ambient_memory_disabled), `MemoryMount` (dumbo,
description), `DumboMemoryMount` (group, read_only) and `AgentPluginConfig`
(plugin_id, enabled, argument_values).

Six RemoteAgentProxy RPCs are genuinely new — they are absent from the 1.5.16
symbol table and present in 1.5.17:

| RPC | 1.5.16 | 1.5.17 | clone |
|---|---|---|---|
| `UploadSkill` | 0 | 17 | not implemented |
| `ListAgentSidecars` | 0 | 19 | not implemented |
| `SummarizeVoiceChat` | 0 | 18 | not implemented |
| `UpdateAgentRemoteSession` | 0 | 39 | not implemented |
| `UpdateAgentRemoteSessionViewerState` | 0 | — | not implemented |
| `ListAgentConfiguredPlugins` | 0 | 16 | not implemented |
| `DeleteSkill` | 33 | 48 | served (`remotecontrol/plugins_ls.go`) |

New upstream symbols also include
`openaiconverter.(*ResponsesStreamState).recordReasoningDelta` /
`extractReasoningSummaryText` (reasoning-summary streaming on the OpenAI
Responses path) and the `PolicyGuardianMonitor` post-tool verdict helpers.

### Deliberately not implemented

The six RPCs, the altscreen mouse-selection input model, the `/model`
Orchestrator rework, and the auth/quota/stream-resume fixes each need their own
cycle with wire evidence. That evidence is currently unavailable: the reference
account is suspended, so the binary could be extracted but not driven. **No live
parity was performed for 1.5.17.** The `how` notes in `reference.lock.json` say
so, and only the artifacts that were statically re-verified against the new
extraction carry a 1.5.17 re-verification claim.

<!-- BEGIN AUTOMATED DRIFT REPORT -->
## Automated Reference Drift Inspection & Parity Audit

> **Inspection Timestamp**: `2026-11-22T17:51:48Z`  
> **Baseline Version**: `agy 1.5.14` | **Candidate**: `1.5.14`  
> **Overall Parity Status**: `CLEAN`

### 1. Parity Audit Summary

| Surface / Subsystem | Baseline (1.5.14) | Candidate (1.5.14) | Status | Unaligned Diff |
|---|---|---|---|---|
| **Pclntab Symbols** | 183,446 | 183,446 | CLEAN | +0 / -0 |
| **CLI Flags** | 28 | 28 | CLEAN | +0 / unmapped: 0 |
| **Slash Commands** | 47 | 47 | CLEAN | +0 / missing: 0 |
| **Tool Schemas** | 13 tools | 13 tools | CLEAN | +0 / modified: 0 |

### 2. Surface Breakdown

#### Architectural Symbols by Subsystem
| Subsystem | Baseline Count | Candidate Count | Delta |
|---|---|---|---|
| `cascade_plugins` | 93 | 93 | 0 |
| `cortex_agent` | 67,390 | 67,390 | 0 |
| `language_server` | 412 | 412 | 0 |
| `other` | 99,709 | 99,709 | 0 |
| `prompt_engine` | 574 | 574 | 0 |
| `remote_agent_proxy` | 6,328 | 6,328 | 0 |
| `subagent_swarm` | 111 | 111 | 0 |
| `tools_pb` | 8,829 | 8,829 | 0 |

### 3. Actionable Drift Alerts & Remediation

All upstream reference contracts align with Pigasus implementation. Zero unaligned drift detected.

<!-- END AUTOMATED DRIFT REPORT -->
