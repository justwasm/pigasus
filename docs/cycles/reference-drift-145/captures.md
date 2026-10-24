# Reference drift 1.4.4 → 1.4.5 — captures

Ground truth for `docs/cycles/reference-drift-145/deliverable.md`. Every number
here comes from a live `agy` **1.4.5** run on this machine
(macOS/arm64, `~/.local/bin/agy`), not from inference.

## 1. The binaries

| | 1.4.4 (previous) | 1.4.5 (current) |
|---|---|---|
| path | `~/.local/bin/agy` | `~/.local/bin/agy` |
| size | — | 188,472,992 B |
| sha256 | `9abb478686dc08d48699ba1604c14d15819ef6a2542d5d32173acbd5f02d712d` | `b89677b741d8d9d38cf0a6911ab468fac629158194039904cbfd645fdb897554` |

The 1.4.5 binary self-updated; the clone's `internal/cli.ReferenceVersion` was
still `1.4.4` when this cycle started (that file belongs to another workstream
and was not touched here).

## 2. Function inventory

Regenerated offline from the 1.4.5 binary with the vendored seed tool
(`cd rea-go-seeds && mkdir -p <outdir> && go run . ~/.local/bin/agy <outdir>`,
~2 s, no network):

```
1.4.5 seeds.txt : 160,664 functions
committed seeds : 154,525 functions   (out/local-macos/seeds/seeds.txt, pre-1.4.4)
```

**+6,139 functions.** The committed seed set was the pre-1.4.4 inventory and
was already flagged as such before this cycle.

The set was regenerated and committed, and everything derived from it was
rebuilt (deliverable §5.2). The measurements behind that:

```
distinct names          : 154,413  ->  160,547
only in the committed set: 1,682
only in the 1.4.5 set    : 7,816
shared symbols          : 152,731   of which 152,486 moved, 245 stayed
priority functions      : 2,101     ->  2,257
```

The digests of the committed set (1.4.5):

```
seeds.txt : 160,664 lines  sha256 3d660aa4ef65745dbf072d0bb7dfac4d46db470f6a414a7d6f4bf78936002d8c
funcs.txt : 160,664 lines  sha256 eea2f961032fc86eb3a82be24d40c0d00953fb9a864c537bb952b7296af90e45
```

Two independent runs of the tool produced byte-identical output.

### 2.1 The Ghidra project, re-seeded

The project was re-imported from the 1.4.5 binary with `-noanalysis` and
seeded with the new set. A `DumpFuncs` script wrote every function's entry
point and name, and the dump was diffed against the seed file:

```
ghidra funcs: 161,175   1.4.5 seeds: 160,664   old seeds: 154,525
1.4.5  addresses present in the project: 160,664
1.4.5  addresses whose NAME also agrees:   160,531
old    addresses present in the project: 11,654
old    addresses whose NAME also agrees:      246
```

All 160,664 seed addresses resolve; the 133 that do not carry the seed's own
name are Go thunk aliases that share an address with their target.

### 2.2 The decompile pass

```
DecompilePriority: 2257 processed, 2257 ok, 0 err, 0 skip in 53 sec
```

2,257/2,257, **zero unresolved**, 330,089 LOC. (For comparison: the Linux/ELF
pass in `out/` resolved 860 of 1,498 — 57 % — because it imported *with*
analyzers, so Go thunks collided with instruction boundaries.)

The first run of the pass reported "2,022 skipped", because the priority list
had been reverted to the pre-1.4.5 one underneath it and every address in it
was stale. The project was verified independently (§2.1) before being blamed.

### 2.3 A function that disappeared

Comparing the two seed sets by name:

```
main.deleteAgentMessageFn   present in the 1.4.4 set, absent from the 1.4.5 set
```

Its decompile report (`decompile-main.deleteAgentMessageFn.md`) had outlived
the function and cited an address that no longer exists, so it was pruned —
and `build_reports.py` now prunes unproduced reports on every run.

### 2.4 The other rebuilt artefacts

```
out/local-macos/gosrc/      : 379 packages, 39,314 declarations, 43,363 symbols in scope
out/local-macos/xref/       : 583,250 ADRP+ADD pairs, 794 string refs, 427 funcs
out/local-macos/plan-local.jsonl : 25 lines, 0 unresolved targets
```

### 2.5 The addresses the documentation quotes

Mapped by **name**, not by offset — the offsets moved, the functions did not:

| 1.4.4 address | 1.4.5 address | function |
|---|---|---|
| `0x1028dd2f0` | `0x102b39f90` | `main.hideFlagsForExternal` |
| `0x1018c28f0` | `0x1019d22b0` | `codeassistclient.shouldBypassKeyring` |
| `0x1018c2880` | `0x1019d2240` | `codeassistclient.keyringRecentlyUnavailable` |
| `0x1018b2ae0` | `0x1019c19d0` | `(*compositeTokenStorage).memoizeKeyringTimeout` |
| `0x1018b33c0` | `0x1019c22b0` | `(*compositeTokenStorage).LoadToken` |
| `0x10262f660` | `0x10284df30` | `store.(*Manager).waitForEligibility` |

## 3. The advertised tool surface (`init.tools`)

```
$ ~/.local/bin/agy -p 'x' --output-format stream-json
```

| | count | vs the committed fixture |
|---|---:|---|
| live 1.4.5 `init.tools` | 60 | **identical as a list** |
| `internal/tools/testdata/advertised_tools_1.4.3.txt` | 60 | — |

Same set, same order. **No drift.** (【一手】)

## 4. The system prompt

Captured from a live `streamGenerateContent` request body through the
cycle-274 capture wrapper:

```
$ OPENAGY_CAPTURE_DIR=$CAPDIR ./agy-wrapper -p 'x'
```

| | bytes |
|---|---:|
| live 1.4.5 prompt | 38,743 |
| committed `internal/agyprompt/testdata/capture_1.4.4.txt` | 34,571 |
| live prompt with `<user_rules>` removed | 37,519 |

After normalising the three environment-dependent inputs (workspace path,
conversation id, `<user_rules>` block) the two prompts differ by **8 added
lines, all of them skill entries** — skills that were installed on this machine
after the 1.4.4 capture was taken:

```
+- alphagenome-atlas-website-links (~/.gemini/config/plugins/science/skills/...)
+- alphagenome-variant-impact-score (…/science/skills/…)
+- biomolecular-structure-viewer (…/science/skills/…)
+- chrome-extensions (…/modern-web-guidance-plugin/skills/…)
+- cookie-debugging (…/chrome-devtools-plugin/skills/…)
+- credentials (…/science/skills/…)
+- memory-leak-debugging (…/chrome-devtools-plugin/skills/…)
+- predictingthepast (…/science/skills/…)
```

**The reference's static prompt text is unchanged between 1.4.4 and 1.4.5.**
The `<user_rules>` section and the skills list are the only moving parts, and
both are environment-derived. (【一手】)

## 5. The `<user_rules>` section

### 5.1 The block as sent (live 1.4.5)

1,223 B (1,198 B body), taken verbatim from the request body:

```
<user_rules>
The following are user-defined rules that you MUST ALWAYS FOLLOW WITHOUT ANY EXCEPTION. These rules take precedence over any other instructions.
Review them carefully and always take them into account when you generate responses and code:
<RULE[/Users/gear/.gemini/config/plugins/science/rules/AGENTS.md]>
# Science Visualization Priority Rules
…
</RULE[/Users/gear/.gemini/config/plugins/science/rules/AGENTS.md]>
</user_rules>
```

Committed as `internal/agyprompt/testdata/user_rules_1.4.5.txt`.

Two details the clone had wrong:

* the preamble says **"any other instructions"** — the clone emitted "any
  following instructions", which is the **1.3.x–1.4.3** wording. The 1.4.5
  binary contains exactly one variant:

  ```
  $ strings -a ~/.local/bin/agy | grep -o "These rules take precedence over any [a-z]* instructions\."
  These rules take precedence over any other instructions.
  $ strings -a ~/.local/bin/agy | grep -c "any following instructions"
  0
  ```

  and the old text is still visible in the 1.3.4 string dump
  (`docs/decompiled-v1.3.4/strings-v1.3.3.txt:278444`). (【一手】 + 【strings】)

* the rule body is **verbatim**, trailing newline included, so a rule that ends
  in a newline renders with a **blank line** before `</RULE[...]>`. The clone
  trimmed the trailing newline and produced no blank line.

### 5.2 The custom-agent prompt carries the section too

A live `--agent driftprobe -p 'x'` request body (31,570 B) has the section
order:

```
<System Prompt> … </System Prompt>
<user_information> … </user_information>
<user_rules> … </user_rules>
<skills> … </skills>
<messaging> …
<artifacts> …
<guidelines> …
<communication_style> …
```

The `<user_rules>` block is **byte-identical** to the main prompt's block
(1,223 B each). The clone's `BuildAgent` had no `user_rules` section at all.

### 5.3 The discovery set — nine probe rounds

Each round wrote a probe file, ran a live capture, and read the `<RULE[...]>`
labels back. All probe files were removed afterwards and
`~/.gemini/config/config.json` was restored byte-for-byte
(sha256 `6e3db3417b054962cbe399057013c99498c08494c8b263a5c535f5185378d40c`
before and after).

| # | probe | result |
|---|---|---|
| A | `<plugin>/rules/AGENTS.md`, **no** `plugin.json` | **not** injected |
| B | `plugin.json` + `rules/AGENTS.md`, not in `config.json` | not injected |
| C | `plugin.json` + `rules/AGENTS.md` + `plugins.<p>.enabled=true` | **injected** |
| D | `plugin.json` + `rules/AGENTS.md`, no `config.json` entry | **injected** (enabled by default) |
| E | `plugin.json` + `enabled:false` | not injected |
| F | `~/.gemini/config/rules/PROBE.md`, `<ws>/.agents/rules/PROBE.md`, `<plugin>/rules/ANY.md` | **not** injected — only `AGENTS.md` / `GEMINI.md` are taken |
| G | `~/.gemini/AGENTS.md`, `~/.gemini/GEMINI.md`, `~/.gemini/config/AGENTS.md`, `~/.gemini/config/GEMINI.md` | injected, all labelled **`user_global`** |
| H | `~/.gemini/config/rules/GEMINI.md` | injected |
| I | `<ws>/GEMINI.md`, `<ws>/.agents/rules/GEMINI.md` | injected |

The resulting order (live, probes combined):

```
user_global   ~/.gemini/AGENTS.md
user_global   ~/.gemini/GEMINI.md
user_global   ~/.gemini/config/AGENTS.md
user_global   ~/.gemini/config/GEMINI.md
<path>        ~/.gemini/config/plugins/<plugin>/rules/AGENTS.md
<path>        ~/.gemini/config/plugins/<plugin>/rules/GEMINI.md
<path>        ~/.gemini/config/rules/AGENTS.md
<path>        ~/.gemini/config/rules/GEMINI.md
<path>        <workspace>/AGENTS.md
<path>        <workspace>/GEMINI.md
<path>        <workspace>/.agents/rules/AGENTS.md
<path>        <workspace>/.agents/rules/GEMINI.md
```

`AGENTS.md` precedes `GEMINI.md` inside every directory; plugin directories
are visited in directory-name order.

### 5.4 The end-to-end check — the reference run inside this repository

The reference was run with the cwd set to this repository, so its
`<user_rules>` block had to contain **this repository's own `AGENTS.md`** (70,176
bytes at the time of the capture) next to the plugin rule:

```
RULE[/Users/gear/.gemini/config/plugins/science/rules/AGENTS.md]
RULE[/Users/gear/Documents/GitHub/openagy/AGENTS.md]
```

The block is 25,315 B, and the second rule carries the truncation marker:

```
<truncated 46195 bytes>
```

The reference's arithmetic, recomputed from the file:

```
file              70,176 B
kept              23,981 B   (content[:24000], cut back to the last '\n')
dropped           46,195 B   (70,176 − 23,981)
```

**This matches the clone's implementation exactly** — same 24,000-byte cap,
same cut-back-to-the-last-newline, same `len(content) − len(kept)` arithmetic,
same marker text. The 24 KB cap and the marker are therefore **re-verified at
1.4.5**, not merely inherited from the 1.4.3 measurement. (【一手】)

Two incidental facts fall out of the same capture: the four home-level files
do not exist on this machine, so the block has no `user_global` entry here;
and no ancestor of the workspace carries an `AGENTS.md`/`GEMINI.md`, so the
ancestor walk contributes nothing in this checkout.

### 5.5 The clone's own output, byte for byte

The clone's `agyprompt.renderUserRules(agyprompt.DiscoverUserRules(workspace,
~/.gemini/antigravity-cli))` was run against the same workspace and compared
with the capture above:

```
IDENTICAL: 25,315 bytes
```

Not "equivalent" — **byte-identical**, including the preamble wording, the
`user_global` handling, the plugin rule, the 24 KB truncation marker and the
blank line the trailing newline produces. (【一手】)

## 6. The model-facing tool declarations

Captured from the same live request body (`tools[]`, 12 `functionDeclarations`,
one per entry).

| | 1.4.4 fixture | 1.4.5 live |
|---|---:|---:|
| declarations | 12 | 12 |
| file size (indent 2) | 17,291 B | 17,712 B |
| identical | — | **11 of 12** |

The changed one is `schedule`:

```
1.4.4 parameters: CronExpression, DurationSeconds, IsDaemon, MaxIterations, Prompt, TimerCondition
1.4.5 parameters: CronExpression, DurationSeconds, ExactTiming,  IsDaemon, MaxIterations, Prompt, TimerCondition
required:         Prompt  ->  Prompt
```

```json
"ExactTiming": {
  "type": "BOOLEAN",
  "description": "Cron only. Set true only when the user explicitly requires the schedule to fire at the exact wall-clock time without jitter. Leave false (the default) for polling, monitoring, or periodic tasks so triggers are jittered to avoid load spikes."
}
```

and one sentence added to the description: *"Set ExactTiming only when a cron
must fire at the exact wall-clock time without jitter."* (【一手】)

## 7. The custom-agent declarations

Captured from the live `--agent driftprobe -p 'x'` request body.

```
send_message, view_file, read_url_content, search_web, schedule, generate_image, manage_task
```

**Byte-identical** to `internal/agenttools/testdata/agent_decls_1.4.4.json`,
`schedule` included — the `ExactTiming` change is on the **main** surface only.
(【一手】)

## 8. The model catalogue

Captured from the live `fetchAvailableModels` response (upstream, un-merged).

| | 1.4.4 fixture | 1.4.5 live |
|---|---:|---:|
| models | 24 | 24 |
| ids | — | identical |
| payloads | — | identical apart from the two items below |

* `quotaInfo.remainingFraction` — live quota consumption, time-dependent.
* `gemini-4-argon-disabled.modelExperiments` → the
  `sonar-monitors-config` textproto: `version: "d0d47979"` → `"5a260837"`
  and its checksum. A server-side monitor config, not a client contract.

Everything the clone consumes — the six selectable ids, the per-model
`maxTokens`, the `CASCADE_USE_EXPERIMENT_CHECKPOINTER` payloads, the
`session_summary_prompt_override` bodies — is **unchanged**. (【一手】)

## 9. The protobuf descriptors

Re-extracted from the 1.4.5 binary with
`out/local-macos/extract_descriptors.py`. See
`out/local-macos/protos/README.md` for the procedure.

### 9.1 Public — `google/gca/aicode/v1main/remote_agent_proxy.proto`

| | 1.4.4 | 1.4.5 |
|---|---:|---:|
| file offset | `0x3999829` | `0x39ba372` |
| length | 40,796 B | 41,636 B |
| RPCs | 71 | **72** |
| messages | 155 | **158** |
| enums | 4 | 4 |

* **new RPC**, declared last:
  `FetchSandboxPort(FetchSandboxPortRequest) → FetchSandboxPortResponse`
* **new messages**:

  ```proto
  message SandboxHttpHeader { string name = 1; string value = 2; }
  message FetchSandboxPortRequest {
    string conversation_id = 1; int32 port = 2; string method = 3;
    string path = 4; repeated SandboxHttpHeader headers = 5; bytes body = 6;
  }
  message FetchSandboxPortResponse {
    int32 status_code = 1; repeated SandboxHttpHeader headers = 2;
    bytes body = 3; bool truncated = 4;
  }
  ```

* **new field**: `ListSlashCommandsRequest.conversation_id = 3` (string)

### 9.2 Internal — `third_party/jetski/remote_agent_proxy_pb/remote_agent_proxy.proto`

| | 1.4.4 | 1.4.5 |
|---|---:|---:|
| file offset | `0x367539d` | `0x368ef0d` |
| length | 4,243 B | 4,284 B |
| messages | 10 | 10 |

* **new field**: `ConfigOverrides.autonomous = 3` (bool)

### 9.3 The extraction pitfall

The naive top-level field walk (`out/local-macos/extract_descriptors.py`'s
`walk_end`) **overshoots the 1.4.5 public descriptor by 62 bytes** and
swallows the next linked blob; the parsed `name` then reads as a LilyPond XML
fragment. The reliable end marker is the trailing `syntax: "proto3"` field
(tag `0x62`, length `0x06`): the descriptor ends 8 bytes after that literal.
The committed `out/local-macos/protos/remote_agent_proxy_1.4.5.txt` is cut
there. (【反编译】)

## 10. The symbol citations in the clone's comments

Every package-qualified symbol the clone's comments cite, resolved against the
1.4.5 inventory:

| | count |
|---|---:|
| distinct citations (the clone's own packages excluded) | 51 |
| exact match in the 1.4.5 inventory | 10 |
| present under another path (leaf name only) | 24 |
| absent | 17 |

Every one of the 17 is explained and **none is a reference function**:

* stdlib / third-party symbols — `json.RawMessage`, `flag.FlagSet`,
  `ai.StopReason`, `ai.ToolCall`, `coding.Runtime`, `coding.Settings`,
  `pi.Model.Provider`, `opts.Effort`, `prompt.Effort`, `prompt.BackendLabel`
  (these live in the Go standard library or in PiG, not in agy);
* clone-internal types — `internal/tools.SharedSubagentManager`,
  `frame.Phase`, `runtime.Worker`, `piagent.BeforeToolCallHook`,
  `statusLinePayload.ConversationTitle`;
* two reference **proto types**, which are not functions and therefore cannot
  be in a function inventory — `exa.extension_server_pb.ExtensionServerService`,
  `jetski_cortex.ModelPricingInfo`.

**No reference function symbol cited in the clone's comments is missing from
agy 1.4.5.** (【一手】)

## 11. What was *not* re-captured

* `internal/tools/testdata/result_texts_1.4.4.txt` — the 32 captured
  call/result pairs. Re-capturing them needs a full tool-using turn against
  the reference, with a pty harness, and is not a version-stamped *wire*
  fixture in the same way (the texts are the reference's result bodies, which
  the `schedule` declaration change does not touch directly).
* `internal/agenttools/testdata/agent_decls_1.4.4.json` — re-verified (§7) and
  found unchanged, so the 1.4.4 file stays authoritative.
* `internal/agyprompt/testdata/capture_1.4.4.{txt,json}` — the *static* part
  is unchanged (§4); the environment-dependent part cannot be re-frozen, since
  it is a snapshot of one machine's skill inventory.
