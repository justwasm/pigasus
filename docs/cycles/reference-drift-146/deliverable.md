# Reference drift 1.4.5 → 1.4.6 — deliverable

**Cycle:** `reference-drift-146`
**Reference:** `agy` **1.4.6** (`~/.local/bin/agy`, self-updated from the 1.4.5
the clone was emulating)
**Evidence:** `docs/cycles/reference-drift-146/captures.md`

---

## 0. Summary

`agy` moved from 1.4.5 to 1.4.6 while this clone was being built. The gate
(`internal/referencedrift`) noticed it and went red:

```
reference-drift: live /Users/gear/.local/bin/agy is 1.4.6; this clone is pinned to 1.4.5 …
```

This cycle closes that gap. It did four things:

1. **Aligned the version and the changelog.** `internal/cli.ReferenceVersion` is
   now `1.4.6`; `internal/cli/testdata/changelog_reference.txt` is the live
   `agy changelog` of the 1.4.6 binary (171,493 B, 994 lines, 76 sections,
   843 bullets, sha256 `4a2470be…`), and the five pinned constants in
   `internal/cli/changelog_data_test.go` follow it.
2. **Re-evidenced every artifact the gate watches**, and re-sealed the registry
   (§8). One artifact is *not* re-captured and stays SUSPECT — honestly.
3. **Implemented the behaviour 1.4.6 changed**: a new `<terminal_sandbox>`
   prompt section, one new `<guidelines>` bullet, the custom agent's
   heading-block wrapper, the `run_command` `BypassSandbox` parameter, and the
   plugin.json `skills` field (a 1.4.5 fix the clone had never implemented).
   After the change the clone's `agyprompt.Build()` reproduces the **live
   1.4.6 system prompt byte for byte** (70,853 B) — the first time the whole
   prompt has matched a live capture end to end.
4. **Regenerated the RE side** from the 1.4.6 binary: 161,286 seeds (was
   160,664), a fresh Ghidra project, 2,290/2,290 priority functions
   decompiled, 0 skipped, and 102 regenerated per-package reports.

Two findings in §2.3 and §2.4 **correct** conclusions this repository held
since 1.4.3. They are the reason the cycle is more than a version bump.

### 0.1 Against the gate's SUSPECT list

Before this cycle the gate reported ten artifacts as suspect. Their end state:

| artifact | before | after |
|---|---|---|
| `internal/cli/testdata/changelog_reference.txt` | SUSPECT | re-captured at 1.4.6 ✓ |
| `internal/agyprompt/testdata/capture_1.4.4.txt` | SUSPECT | **superseded** by `capture_1.4.6.txt` (the prompt changed) |
| `internal/agyprompt/testdata/capture_1.4.4.json` | SUSPECT | **superseded** by `capture_1.4.6.json` |
| `internal/agyprompt/testdata/user_rules_1.4.5.txt` | SUSPECT | re-verified at 1.4.6 ✓ (§2.5) |
| `internal/tools/testdata/advertised_tools_1.4.3.txt` | SUSPECT | re-verified byte-identical ✓ |
| `internal/tools/model_tools_1.4.5.json` | SUSPECT | **superseded** by `model_tools_1.4.6.json` |
| `internal/tools/testdata/result_texts_1.4.4.txt` | SUSPECT | **still SUSPECT** — not re-captured (§10) |
| `internal/agenttools/testdata/agent_decls_1.4.4.json` | SUSPECT | re-verified byte-identical ✓ |
| `internal/remotecontrol/testdata/jetski_remote_agent_proxy_1.4.5.txt` | SUSPECT | **superseded** by the 1.4.6 list (77 RPCs) |
| `out/local-macos/protos/remote_agent_proxy_1.4.5.txt` (+ `_pb_`) | SUSPECT | re-extracted at 1.4.6 ✓ |

One SUSPECT line remains, and it names an artifact this cycle genuinely did
not re-record.

---

## 1. What moved

### 1.1 The version

`internal/cli/version.go` pins one string:

```go
const ReferenceVersion = "1.4.6"
```

`internal/referencedrift/reference.lock.json` carries the same value, and
`internal/cli`'s `TestReferenceVersion_MatchesDriftLock` keeps the two
together. The live binary agrees: `agy --version` → `1.4.6`.

### 1.2 The changelog

`agy changelog` at 1.4.6 is **171,493 B / 994 lines / 76 version sections /
843 top-level bullets**. The 1.4.5 capture was 14 lines shorter: the 1.4.6
release added the **`1.4.5:` section** — twelve bullets.

Note what the feed does *not* contain: there is **no `1.4.6:` section**. The
1.4.6 binary's changelog feed still ends at 1.4.5, so the reference's own
release notes for 1.4.6 are not observable from the binary. The newest section
in the fixture is therefore 1.4.5, and the `/changelog` panel opens on it
(`cmd/wagyu/slash_panels_test.go` was re-pinned accordingly).

The twelve 1.4.5 bullets are the *behavioural* half of this cycle: §5 is the
per-bullet gap register.

---

## 2. The system prompt

The 1.4.6 prompt is **70,853 B** in twelve sections. The 1.4.4 capture was
34,571 B in ten (it predates `<user_rules>`), so a byte comparison of the
two is not meaningful on its own; the section-by-section comparison is:

| section | 1.4.4 | 1.4.6 | verdict |
|---|---:|---:|---|
| `identity` | 477 | 477 | unchanged |
| **`terminal_sandbox`** | — | **3,371** | **new** (§2.1) |
| `artifacts` | 3,718 | 3,718 | unchanged |
| `conversation_transcript` | 1,969 | 1,969 | unchanged |
| `guidelines` | 237 | **715** | **one bullet added** (§2.2) |
| `communication_style` | 524 | 524 | unchanged |
| `slash_commands` | 1,839 | 1,839 | unchanged |
| `messaging` | 877 | 877 | unchanged |
| `subagents` | 2,057 | 2,107 | two workspace agents (§2.5) |
| `user_rules` | — | 25,224 | environment-derived, rendering unchanged |
| `skills` | 22,286 | 29,260 | nine shared-library skills (§2.3) |
| `user_information` | 581 | 623 | the conversation id / artifact dir |

### 2.1 `<terminal_sandbox>` — a new section

【一手】 agy 1.4.6, the live request body recorded through the cycle-274
capture wrapper. The section is **3,371 B** and sits **second**, right after
`<identity>`:

```
<terminal_sandbox>

The run_command tool executes commands inside a secure sandbox by default:
- **Standard Sandbox Mode (BypassSandbox: false)**: By default has full read/write access to your workspace, your artifact and scratch directories (`<appDataDir>/brain/`, including `scratch/`, task logs, and transcripts), standard system binaries, and `/tmp`, but no network access and no access to other files outside the workspace unless added explicitly by the user.
- **Bypass Sandbox Mode (BypassSandbox: true)**: Disables isolation, allowing network and full filesystem access. **Requires manual user approval.**

The main purpose of the sandbox is to auto-run commands without needing the user's approval. …
Execution Rules:
1. **Try Sandboxed First**: …
```

The full body is committed verbatim as
`internal/agyprompt/sections/terminal_sandbox.txt` (3,337 B — the section
block is 3,371 B including the tags and the trailing newline).

The section and the `BypassSandbox` parameter (§3) are the two halves of one
feature: the prompt tells the model that `run_command` runs sandboxed by
default and that bypassing needs the user's approval.

### 2.2 `<guidelines>` — one bullet

【一手】 the live 1.4.6 body adds one bullet to the two the 1.4.4 capture
had:

```
- Never defeat or bypass an access control, permission check, credential requirement, network/proxy restriction, or sandbox/environment boundary - including writing to the state or processes such a check reads, spoofing identity, intercepting the check, probing undocumented local/host services, or modifying host, harness, or test infrastructure outside your task scope. Use documented, sanctioned paths only; if none of them work, you are blocked - report the error and stop.
```

`internal/agyprompt/sections/guidelines.txt` is rebuilt from the live body
(689 B).

### 2.3 The skills list — a **correction**

This repository has asserted since cycle `custom-agents` that the shared
`~/.gemini/skills` library is **`/skills`-only** and "must not reach the
prompt", on the evidence of the 1.4.2/1.4.3 captures.

**That was wrong.** The live 1.4.6 request lists all nine skills of this
machine's shared library, with their real paths:

```
- agents-sdk (/Users/gear/.gemini/skills/agents-sdk/SKILL.md): …
- cloudflare (/Users/gear/.gemini/skills/cloudflare/SKILL.md): …
- cloudflare-email-service (…): durable-objects (…): sandbox-sdk (…):
- terminal-browser (…): web-perf (…): workers-best-practices (…): wrangler (…)
```

The reference's skill roots are the builtin bundle, the **shared
`~/.gemini/skills` library**, `<workspace>/.agents/skills`, and every loaded
plugin's `skills/`. The earlier conclusion almost certainly came from a
machine whose shared library was *empty* — an empty directory contributes
nothing, which is indistinguishable from "not a root".

The clone now includes the root (`internal/pigrt/skills.go`), and the
doc comments that carried the old claim were corrected in the same commit
(`internal/pigrt/skills.go`, `internal/agyprompt/skills.go`, `AGENTS.md`).

### 2.4 The custom agent's prompt — a **correction**

The clone wraps a custom agent's Markdown body in a single literal
`<System Prompt>` tag. The 1.4.6 reference does something else: it **splits
the body at level-1 Markdown headings** and wraps each part in a tag named
after the heading.

【一手】 agy 1.4.6, five live `--agent` request bodies (the probes are listed
in `captures.md`):

| agent body | the reference sends |
|---|---|
| `Answer with one word only.` | `<System Prompt>\nAnswer with one word only.\n</System Prompt>` |
| `# Probe agent\n\nAnswer in one short sentence.` | `<Probe agent>\nAnswer in one short sentence.\n</Probe agent>` |
| `# Totally Different Heading\n\nAnswer with one word.` | `<Totally Different Heading>\nAnswer with one word.\n</Totally Different Heading>` |
| `Intro line first.\n\n# Later Heading\n\nAnswer with one word.` | `<System Prompt>\nIntro line first.\n</System Prompt>` + `<Later Heading>\nAnswer with one word.\n</Later Heading>` |
| preamble + `# One` + `## Two` + `### Three` + `## Two Again` | `<System Prompt>\nPreamble text.\n</System Prompt>` + `<One>\nBody one.\n\n## Two\n…\n</One>` — the level-2 and level-3 headings stay inside the block |

Rules, all from those five captures:

* only **level-1** headings split — `## Two` and `### Three` stay inside the
  enclosing block (the fifth probe carried all three levels);
* the part **before** the first heading keeps the literal `System Prompt` tag;
  a body that starts with a heading has no preamble block at all;
* the heading text is used **verbatim** as the tag name (spaces and all);
* each block's text is trimmed at both ends; blank lines *inside* a block are
  kept.

Implemented as `agyprompt.agentBlocks` and pinned by
`internal/agyprompt/agent_blocks_test.go` (the five captured shapes).

The 1.4.6 agent prompt also keeps the 1.4.4 shape otherwise: seven sections
(`user_information`, `user_rules`, `skills`, `messaging`, `artifacts`,
`guidelines`, `communication_style`), no `terminal_sandbox`, and the same seven
tool declarations.

### 2.5 The end-to-end proof

The clone's `agyprompt.Build()` reproduces the **live 1.4.6 prompt byte for
byte** (70,853 B, `cmp` clean) when it is fed the capture's own inputs. That
single comparison pins, at once:

* the static sections, including the new `<terminal_sandbox>` and the
  new `<guidelines>` bullet;
* the `<user_rules>` rendering — preamble wording, the `user_global` labels,
  the plugin rules, the verbatim bodies and the **24,000-byte truncation
  marker** (this repository's own `AGENTS.md` is 50 KB, so the live block
  carries a truncated rule and the clone reproduces it exactly);
* the skill list, including the nine shared-library skills of §2.3;
* the `<subagents>` list, whose only delta against the 1.4.4 capture is the
  two workspace agents described below.

`alpha` and `zeta` are **this repository's own workspace agents** —
`.agents/agents/alpha.md` and `.agents/agents/zeta.md`, whose `description`
frontmatter is verbatim `Alpha demo agent` / `Zeta demo agent`. The reference
lists every discovered workspace custom agent in `<subagents>`, after the
builtin three (`self`, `research`, `image-generator`). The clone already
discovers them the same way (`customSubagents()` → `agent.Discover()`, wired
into both the TUI's `promptOptions()` and print mode's `print_pig.go`), so the
live capture is also a cross-check of the clone's discovery: the reference
listed exactly the two agents this workspace has, and nothing else.

---

## 3. The model-facing declarations

【一手】 the live 1.4.6 request body carries **twelve** function declarations
in the same order as 1.4.5. Eleven are byte-identical; `run_command` gained
one parameter:

```json
"BypassSandbox": {
  "type": "BOOLEAN",
  "description": "Set to true to run outside the sandbox (requires elevated user permission for auth exchange, network, or port binding)."
}
```

The wire grew from 13,530 to **13,701** bytes. The fixture is committed as
`internal/tools/model_tools_1.4.6.json` (the 1.4.5 and 1.4.4 files stay on
disk as the previous releases' evidence).

The clone now:

* ships the 1.4.6 declarations (`internal/tools/model_surface.go` embeds the
  new fixture);
* accepts the parameter in the executor's own schema
  (`internal/tools/command.go`, `runCommandSchema`);
* **honours it at the gate**: `tools.BypassSandboxRequested` reads the flag
  and both command gates (`internal/toolrunner/runner.go`,
  `internal/printmode/main_print_tools.go`) pass it to
  `sandbox.CheckCommandWithBypass`, so a call that asks to run outside the
  sandbox is not blocked by the clone's path sandbox.

The `init.tools` list (the *available* surface, 60 names) is **byte-identical**
to the 1.4.3 capture — verified again at 1.4.6.

---

## 4. The remote-control surface

【反编译】 the public `FileDescriptorProto` re-extracted from the 1.4.6
binary (`google/gca/aicode/v1main/remote_agent_proxy.proto`, 45,388 B of
descriptor at offset `0x3a3efd7`) grew:

| | 1.4.5 | 1.4.6 |
|---|---:|---:|
| RPCs | 72 | **77** |
| messages | 158 | **170** |
| enums | 4 | **5** (`AgentStatus` is new) |

The five appended RPCs are `SearchFiles`, `StartLocalPluginSignIn`,
`CompleteLocalPluginSignIn`, `CreateAgent` and `GetAgent`. The internal
descriptor (`exa.remote_agent_proxy_pb`) did **not** change — the 1.4.6
extraction is byte-identical to the 1.4.5 file.

The clone's `remotecontrol.ReferenceRPCMethods` carries the 77 names, and the
fixture `internal/remotecontrol/testdata/jetski_remote_agent_proxy_1.4.6.txt`
pins the order. The five new RPCs have **no clone-side implementation** — the
alias map only carries exact-behaviour pairs, and the clone answers "method
not found" for them, which is honest.

The `.proto` rendering is now produced by a script
(`out/local-macos/render_proto.py`) instead of by hand, and the script is
validated by re-rendering the 1.4.6 descriptor and diffing it against the
committed 1.4.5 rendering: the diff is the version header plus the additions
and nothing else.

---

## 5. The changelog gap register — the twelve 1.4.5 bullets

Each bullet of the newly published `1.4.5:` section, and what it means for
this clone.

| # | bullet (abridged) | verdict |
|---:|---|---|
| 1 | blank line between the collapsed thought block and the response; the collapsed thought shows only its `Thought for …` header | **already matches** — `blankBefore()` (`cmd/wagyu/tui_turn_blocks.go:62`) and `thoughtStepLines` (`cmd/wagyu/tui_tool_render.go:122`) already do this; the reference's *old* snippet-repeating behaviour was never implemented here |
| 2 | `/` search in the `/diff` file view counts matches inside folded unchanged lines; the `f` hint reads `show N hidden matches` | **pre-existing gap** — the clone has no `/` search in the diff view at all (the reference added it in 1.4.3). Not a 1.4.5 regression, and not implemented here (§11) |
| 3 | `/diff`'s `Commit Tree` marks the selected commit with the same `>` cursor and selection colour; `Per Turn` starts on the first file | **pre-existing gap** — the clone's Commit Tree rows carry no cursor marker and the `Per Turn` tab is a placeholder (§11) |
| 4 | recurring cron schedules fire at a consistent per-schedule offset; the agent can request exact timing | **partially matches** — `ExactTiming` exists in the shipped declaration and in the executor's parameters since 1.4.5; the clone has no cron timer, so the *jitter* has no counterpart |
| 5 | idle CPU usage halved by pausing screen refreshes | **not parity-relevant** — the clone has no refresh scheduler to pause |
| 6 | CPU usage while a response streams | **not parity-relevant** — same reason |
| 7 | rendering speed for long conversations | **not parity-relevant** |
| 8 | startup CPU when refreshing conversation summaries | **not parity-relevant** |
| 9 | memory: text inputs allocated as you type | **not parity-relevant** |
| 10 | fixed the agent silently retrying when a response was blocked by content safety filters | **no counterpart** — the clone has no content-safety retry path at all (`internal/backend`, `internal/agent`, `internal/piagentbridge` contain no safety/blocked handling), so the bug cannot exist |
| 11 | fixed plugins failing to load when `plugin.json` gives `skills` as a single path string | **fixed in this cycle** — `internal/plugins.SkillDirs` reads the manifest field (array *or* single string); the prompt roots and the plugin panel use it |
| 12 | marketplace plugin installs retry the download once | **N/A** — the clone has no marketplace *downloader* (only the link helper `internal/cli/marketplace_link.go`), so there is no download to retry |

Bullets 5–9 are performance work inside the reference's TUI. The clone's TUI
is a reimplementation, not a port, so there is no behavioural surface to match
and no measurement this repository can take that would mean anything.

---

## 6. The changes

| path | change |
|---|---|
| `internal/cli/version.go` | `ReferenceVersion` → `1.4.6` |
| `internal/cli/testdata/changelog_reference.txt` | the live 1.4.6 changelog (171,493 B) |
| `internal/cli/changelog_data_test.go` | the five pinned constants; the shape test starts at 1.4.5 |
| `internal/cli/changelog_v133_test.go` | the newest-section pin → 1.4.5 |
| `internal/agyprompt/sections/terminal_sandbox.txt` | **new** — the live 3,337 B body |
| `internal/agyprompt/sections/guidelines.txt` | rebuilt from the live body (689 B) |
| `internal/agyprompt/agyprompt.go` | `<terminal_sandbox>` in the section order and the body map; `BuildAgent` splits the body at level-1 headings (`agentBlocks`) |
| `internal/agyprompt/skills.go` | the shared library is a prompt root |
| `internal/agyprompt/agent_blocks_test.go` | **new** — the five captured agent-body shapes |
| `internal/agyprompt/agyprompt_test.go` | the capture fixture → 1.4.6; the section-order list |
| `internal/agyprompt/testdata/capture_1.4.6.{txt,json}` | **new** — the live prompt and its inputs |
| `internal/pigrt/skills.go` | the shared `~/.gemini/skills` root; the plugin skill dirs come from the manifest |
| `internal/pigrt/{runtime,skills,system_prompt}_test.go` | re-pinned to the 1.4.6 prompt and skill roots |
| `internal/plugins/plugins.go` | `SkillDirs` — the plugin.json `skills` field |
| `internal/plugins/data.go` | the plugin panel's Skills column reads the same directories |
| `internal/plugins/skills_test.go` | **new** — the manifest field's three shapes |
| `internal/tools/model_tools_1.4.6.json` | **new** — the live 12 declarations |
| `internal/tools/model_surface.go` | embeds the 1.4.6 fixture |
| `internal/tools/command.go` | `BypassSandbox` in the executor schema; `BypassSandboxRequested` |
| `internal/tools/model_surface_test.go` | the fixture pin; the `BypassSandbox` pin |
| `internal/toolrunner/runner.go`, `internal/printmode/main_print_tools.go` | the command gates honour the per-call bypass |
| `internal/remotecontrol/reference_surface.go` | the five new RPCs; the header counts |
| `internal/remotecontrol/testdata/jetski_remote_agent_proxy_1.4.6.txt` | **new** — the 77-method list |
| `internal/remotecontrol/reference_surface_test.go` | 77 methods |
| `internal/referencedrift/reference.lock.json` | the registry now describes the 1.4.6 evidence (§8) |
| `out/local-macos/protos/*_1.4.6.{txt,proto}` | **new** — the re-extracted descriptors |
| `out/local-macos/render_proto.py` | **new** — the mechanical `.proto` renderer |
| `out/local-macos/seeds/{seeds,funcs}.txt` | regenerated from the 1.4.6 binary (§7) |
| `out/local-macos/priority-funcs.txt` | regenerated from the new seeds |
| `out/local-macos/decompile-*.md` (102 files) | regenerated from the new project |
| `cmd/wagyu/slash_panels_test.go`, `cmd/wagyu/tui_tools_test.go` | re-pinned to the 1.4.6 changelog and the 13,701-byte wire |

---

## 7. The seed set and everything derived from it

The seed set was regenerated with the vendored tool (offline, ~2 s):

```
cd rea-go-seeds && mkdir -p ../out/local-macos/seeds && go run . ~/.local/bin/agy ../out/local-macos/seeds
recovered 161286 function names
```

| | 1.4.5 | 1.4.6 |
|---|---:|---:|
| functions recovered | 160,664 | **161,286** (+622) |
| seeds applied by Ghidra | — | 161,286 (161,267 created, 19 existing, **0 failed**) |
| priority functions | 2,257 | **2,290** |
| decompiled | 2,257 ok, 0 skipped | **2,290 ok, 0 err, 0 skipped** |
| decompiled LOC | 330,089 | **335,020** |

The Ghidra project was rebuilt from scratch (`-noanalysis`, `MAXMEM=8G`, 4
CPUs): the import took ~11 minutes, the decompile pass 31 seconds. The old
project was moved aside to `out/local-macos/proj-1.4.5/` rather than deleted.

---

## 8. The registry

`internal/referencedrift/reference.lock.json` is rewritten to describe what
the evidence actually is. The gate's question is *"is this still true of the
live binary"*, so the artifacts are handled three ways:

| kind | example | what the lock does |
|---|---|---|
| re-verified at 1.4.6 | `changelog_reference.txt`, `advertised_tools_1.4.3.txt`, `agent_decls_1.4.4.json`, `user_rules_1.4.5.txt`, the seed set | `captured_at` moves to 1.4.6 — the artifact is still valid, the stamp is what says so |
| superseded by a newer capture | `capture_1.4.6.{txt,json}`, `model_tools_1.4.6.json`, `jetski_remote_agent_proxy_1.4.6.txt`, the two 1.4.6 proto dumps | the newer file takes the entry; the older file stays on disk as history |
| not re-verified | `result_texts_1.4.4.txt` | the stamp stays at 1.4.4, so the gate **keeps saying SUSPECT** |

The gate's output after the rewrite:

```
reference-drift: live /Users/gear/.local/bin/agy is 1.4.6; this clone is pinned to 1.4.6 (lag acknowledged through 1.4.6).
  SUSPECT  internal/tools/testdata/result_texts_1.4.4.txt — captured at agy 1.4.4, unverified against 1.4.6
```

One SUSPECT line is the honest end state, not a failure: it names an artifact
this cycle genuinely did not re-record (§10).

`lag_acknowledged_through` stays unset. The live binary is the pinned
version, so no lag is being tolerated — the gate would go red the moment
Google ships 1.4.7, which is the point.

---

## 9. Parity table

| surface | 1.4.5 | 1.4.6 | verdict |
|---|---|---|---|
| version pin | 1.4.5 | 1.4.6 | ✓ aligned |
| changelog fixture | 1.4.4 section | 1.4.5 section | ✓ re-captured live |
| system prompt | 34,571 B (1.4.4 capture) | 70,853 B | ✓ **byte-identical** to the live capture |
| prompt sections | 11 | 12 (`terminal_sandbox` added) | ✓ |
| `<guidelines>` | 2 bullets | 3 bullets | ✓ |
| custom agent wrapper | one `<System Prompt>` | one tag per level-1 heading | ✓ |
| skill roots | builtin + workspace + plugins | **+ the shared `~/.gemini/skills`** | ✓ corrected |
| model declarations | 12 | 12 (`run_command` + `BypassSandbox`) | ✓ byte-identical except the new parameter |
| `init.tools` | 60 names | 60 names | ✓ byte-identical |
| agent declarations | 7 | 7 | ✓ byte-identical |
| remote-control RPCs | 72 | 77 | ✓ re-extracted |
| internal proto | — | — | ✓ unchanged |
| seeds | 160,664 | 161,286 | ✓ regenerated |
| priority decompile | 2,257/2,257 | 2,290/2,290 | ✓ 0 skipped |
| result texts | 32 captured | — | **not re-captured** (SUSPECT) |

---

## 10. 硬约束 — hard constraints

These are the things this cycle *did not* do, on purpose:

* **`internal/tools/testdata/result_texts_1.4.4.txt` was not re-captured.**
  The 32 result texts need a full tool-using turn against the reference under
  a pty harness; that is the cycle-275/277 apparatus, not a fixture refresh.
  The artifact therefore stays stamped at 1.4.4 and the gate keeps reporting
  it as SUSPECT.
* **The `/diff` search and Commit Tree cursor were not implemented.** Both are
  pre-existing gaps from the reference's 1.4.3 (`/diff` search) and 1.4.5
  (Commit Tree cursor) — the 1.4.5 bullets *improve* features the clone does
  not have. This repository's discipline is to implement a reference UI only
  from a capture; there is no capture of the reference's Commit Tree with a
  selection colour, and `cmd/wagyu/` is another workstream's area. Both are
  recorded in §5 and §11 instead of being guessed at.
* **The plugin marketplace downloader was not written.** The clone's
  marketplace support is a link helper (`internal/cli/marketplace_link.go`);
  the reference's retry-once download is a different subsystem and is not
  needed for the clone's current feature set.
* **The 1.4.6 release notes were not captured**, because they are not in the
  binary's changelog feed. §5 therefore works from the *1.4.5* bullets, which
  is what the feed actually published.
* **The Ghidra project was rebuilt, but the old one was not deleted** — it
  was moved to `out/local-macos/proj-1.4.5/` (747 MB, not committed).

---

## 11. 存疑 — open questions

* **When did the shared `~/.gemini/skills` library enter the prompt?** The
  clone asserted since 1.4.3 that it was excluded. The 1.4.6 capture proves it
  is included *now*; the 1.4.4/1.4.5 binaries are gone (they self-updated), so
  the change cannot be dated. The most likely explanation is that the library
  was empty on the machine that produced the old captures, but that is a
  hypothesis, not a measurement.
* **What does the reference do with a plugin's `skills` field when it is an
  array?** The 1.4.5 changelog only says the single-string form used to break
  the plugin and now works. The clone accepts both and resolves relative
  paths against the plugin directory — the reference's resolution rule for a
  path outside the plugin is unobserved.
* **Does the reference's Commit Tree really use All Changes' exact colours?**
  The 1.4.5 bullet says "the same `>` cursor and selection color", and the
  clone's All Changes row is `> ` in brand blue — but no capture of the
  reference's Commit Tree with a selection exists.
* **What is the `<terminal_sandbox>` section's relationship to the reference's
  auto-approval policy?** The section says a bypass "requires manual user
  approval"; the clone's approval machinery (`internal/permissions`) has no
  notion of a per-call bypass approval. Whether the reference blocks the
  bypass or merely warns the model is not observable from the wire.

---

## 12. Limitations

* Every capture in this cycle came from **one machine** and **one account**.
  The Google CloudCode path worked for this account; a different account could
  see a different catalogue, a different plugin set or a different
  `<subagents>` list.
* The captures are unredacted request bodies held in a `0700` directory
  outside the repository. **No capture file was committed**; the fixtures
  derived from them carry no bearer token, but they do carry this machine's
  absolute paths.
* The `<skills>` and `<user_rules>` sections of the committed 1.4.6 capture
  are *this machine's* — nine shared skills and this repository's own 50 KB
  `AGENTS.md`. They are evidence of the reference's rendering, not of a
  portable environment.
* The `.proto` rendering in `out/local-macos/protos/` is a reading aid: the
  public one is mechanical (validated by re-rendering and diffing against the
  committed 1.4.5 file), the internal one is hand-abridged and elides nested
  types.
* The five new remote-control RPCs are pinned as reference RPCs but have **no
  clone implementation** and no alias.
* `SearchFiles`, `CreateAgent` and `GetAgent` suggest a remote file-search
  and agent-registry surface the reference has and the clone does not. The
  descriptor names them; nothing else about them was recovered.
