# Reference drift 1.4.4 → 1.4.5 — deliverable

**Cycle:** `reference-drift-145`
**Reference:** `agy` **1.4.5** (`~/.local/bin/agy`, self-updated from the 1.4.4
the clone was emulating)
**Evidence:** `docs/cycles/reference-drift-145/captures.md`

---

## 0. Summary

`agy` moved from 1.4.4 to 1.4.5 while this clone was being built. The
`internal/referencedrift` gate noticed that ten committed artifacts were
captured at 1.4.4 and had not been re-checked against a live 1.4.5, and listed
them as SUSPECT. This cycle re-verified every one of them against the running
1.4.5 binary and fixed the four places where the reference had actually moved.

| artifact | 1.4.4 fixture | live 1.4.5 | verdict |
|---|---|---|---|
| `init.tools` (60 names) | `advertised_tools_1.4.3.txt` | 60 names | **identical** |
| system prompt (34 KB) | `agyprompt/testdata/capture_1.4.4.txt` | 38,743 B | **identical** apart from environment |
| model catalogue (24 models) | `fetchAvailableModels_1.4.4.json` | 24 models | **identical** apart from quota + a server-side monitor version |
| custom-agent declarations (7) | `agent_decls_1.4.4.json` | 7 | **identical** |
| model declarations (12) | `model_tools_1.4.4.json` | 12 | **1 changed** (`schedule`) |
| public proto | 71 RPC / 155 msg | 72 RPC / 158 msg | **changed** |
| internal proto | 10 msg | 10 msg | **changed** (one field) |
| `<user_rules>` | — (never captured) | 1,223 B | **newly characterised** |
| function inventory | 154,525 (pre-1.4.4) | 160,664 | **regenerated** (§5.2) |
| symbol citations in the comments | — | 51 citations | **all still valid** |

Four drifts, all fixed in this cycle:

1. **`<user_rules>`** — the preamble wording changed, the home-level files are
   labelled `user_global`, plugin rule files are part of the set, only
   `AGENTS.md`/`GEMINI.md` are taken from a rules directory, the rule body is
   kept verbatim, and a custom agent's prompt carries the section too.
2. **`schedule`** gained an `ExactTiming` boolean on the model surface.
3. **The public proto** gained a 72nd RPC (`FetchSandboxPort`), three messages
   and one field.
4. **The internal proto** gained `ConfigOverrides.autonomous`.

The static system prompt, the catalogue, the advertised surface and the
custom-agent declarations did **not** move.

### 0.1 Against the gate's SUSPECT list

`internal/referencedrift` lists ten artifacts as SUSPECT. Eight of them are
re-evidenced by this cycle; two are not:

| SUSPECT artifact | this cycle |
|---|---|
| `agyprompt/testdata/capture_1.4.4.txt` | re-verified — static text identical |
| `agyprompt/testdata/capture_1.4.4.json` | **not** re-recorded (its inputs are environment-specific) |
| `tools/testdata/advertised_tools_1.4.3.txt` | re-verified — 60/60 identical |
| `tools/model_tools_1.4.4.json` | re-verified — 11/12 identical, `schedule` changed → 1.4.5 fixture |
| `tools/testdata/result_texts_1.4.4.txt` | **not** re-captured |
| `agenttools/testdata/agent_decls_1.4.4.json` | re-verified — 7/7 identical |
| `remotecontrol/testdata/jetski_remote_agent_proxy_1.4.4.txt` | re-verified — 71 → 72 methods → 1.4.5 fixture |
| `out/local-macos/protos/remote_agent_proxy_1.4.4.txt` | re-extracted at 1.4.5 |
| `out/local-macos/protos/remote_agent_proxy_pb_1.4.4.txt` | re-extracted at 1.4.5 |
| `cli/testdata/changelog_reference.txt` | already captured at 1.4.5 |

The registry was rewritten afterwards (§5.1), so the gate now names only the
two artifacts this cycle genuinely did not re-record:
`agyprompt/testdata/capture_1.4.4.json` and
`tools/testdata/result_texts_1.4.4.txt`.

## 1. Why re-capture rather than re-read

The free-text comment sweep that started this cycle turned out to be
low-signal: the loose marker filter produced **713** candidate hits, of which
the strict same-line rule kept **4**, and none of the four was a wrong
citation. The comment *citations* are almost all still valid (§6). What is
actually version-scoped is the **fixtures** — the captured bytes — and those
are what the drift gate lists. So the cycle pivoted there: every fixture the
gate flagged was re-captured from the live binary and compared byte for byte.

## 2. `<user_rules>` — the largest drift

【一手】 every statement in this section comes from a live 1.4.5 request body
read back through the cycle-274 capture wrapper. The probe files were removed
afterwards and `~/.gemini/config/config.json` was restored byte-for-byte
(sha256 verified before and after).

### 2.1 The preamble changed

The reference's fixed preamble now reads:

> The following are user-defined rules that you MUST ALWAYS FOLLOW WITHOUT ANY
> EXCEPTION. These rules take precedence over **any other instructions**.

The clone emitted "**any following** instructions" — the **1.3.x–1.4.3**
wording, which is what `docs/decompiled-v1.3.4/strings-v1.3.3.txt:278444`
still carries, and what the cycle-292 commit transcribed. The 1.4.5 binary
contains exactly one variant of the sentence, so this is a reference-side
change between 1.4.3 and 1.4.5, not a clone typo.

### 2.2 The home-level files are labelled `user_global`

The four home-level rule files are printed with the fixed label `user_global`
instead of their path:

```
<RULE[user_global]>      ← ~/.gemini/AGENTS.md
<RULE[user_global]>      ← ~/.gemini/GEMINI.md
<RULE[user_global]>      ← ~/.gemini/config/AGENTS.md
<RULE[user_global]>      ← ~/.gemini/config/GEMINI.md
<RULE[/…/rules/AGENTS.md]>   ← everything else prints its own path
```

### 2.3 Plugin rule files are part of the set

The clone never looked at `<plugin>/rules/`. The reference does, with the
same plugin-loading rules the custom-agent loader already used:

* a directory under `~/.gemini/config/plugins/` is a plugin only when it has a
  **parseable `plugin.json`** (probes A and B: a plugin without one
  contributes nothing);
* `plugins.<name>.enabled=false` in `~/.gemini/config/config.json` disables one
  (probe E);
* a plugin that is **absent** from the config.json is enabled (probe D).

Those three facts were already implemented in `internal/agent/list.go`; they
are now shared through the new `internal/plugins` package so the rule
discovery and the agent loader cannot drift apart.

### 2.4 Only two file names are taken

`~/.gemini/config/rules/`, `<plugin>/rules/` and `<dir>/.agents/rules/` each
contribute **`AGENTS.md` and `GEMINI.md`** only. A `PROBE.md` beside them is
ignored (probe F). The clone used to take *any* `*.md` in those directories.

`AGENTS.md` precedes `GEMINI.md` in every directory — the clone had the two
the other way round.

### 2.5 The rule body is verbatim

A rule file that ends in a newline renders with a **blank line** before
`</RULE[...]>`: the reference writes the body as it is and then puts the
closing delimiter on its own line. The clone trimmed the trailing newline.

### 2.6 A custom agent's prompt carries the section

The live `--agent` prompt has the section between `<user_information>` and
`<skills>`, and the block is byte-identical to the main prompt's. The clone's
`BuildAgent` had no `user_rules` section at all — so a user with rule files
got a materially different agent prompt from the reference.

### 2.7 The resulting discovery order

```
user_global   ~/.gemini/AGENTS.md
user_global   ~/.gemini/GEMINI.md
user_global   ~/.gemini/config/AGENTS.md
user_global   ~/.gemini/config/GEMINI.md
<path>        ~/.gemini/config/plugins/<plugin>/rules/{AGENTS,GEMINI}.md   (plugin dirs in name order)
<path>        ~/.gemini/config/rules/{AGENTS,GEMINI}.md
<path>        <dir>/{AGENTS,GEMINI}.md                                    (root → workspace)
<path>        <dir>/.agents/rules/{AGENTS,GEMINI}.md                       (root → workspace)
```

### 2.8 The 24 KB truncation cap — re-verified, not inherited

The cap and its marker were a cycle-292 measurement against 1.4.3. This cycle
re-measured them at 1.4.5 by running the reference **inside this repository**,
whose own `AGENTS.md` (70,176 B) had to be injected:

```
file              70,176 B
kept              23,981 B   (content[:24000], cut back to the last '\n')
dropped           46,195 B   (70,176 − 23,981)   →  <truncated 46195 bytes>
```

The reference's numbers reproduce the clone's implementation exactly — same
cap, same cut-back-to-the-last-newline, same arithmetic, same marker text. So
this is now a 1.4.5 measurement rather than an inherited assumption. (【一手】)

### 2.9 The end-to-end proof

The clone's own renderer was run against the same workspace and compared with
the live capture:

```
agyprompt.renderUserRules(agyprompt.DiscoverUserRules(workspace, ~/.gemini/antigravity-cli))
  vs  the <user_rules> block of the live 1.4.5 request body

IDENTICAL: 25,315 bytes
```

Not "equivalent" — byte-identical, including the preamble wording, the
`user_global` handling, the plugin rule, the truncation marker and the blank
line a trailing newline produces. This is the end-to-end statement that the
rewrite in §2.1–§2.6 actually reproduces the reference. (【一手】)

## 3. The model-facing declarations

`schedule` gained an `ExactTiming` boolean:

```json
"ExactTiming": {
  "type": "BOOLEAN",
  "description": "Cron only. Set true only when the user explicitly requires the schedule to fire at the exact wall-clock time without jitter. Leave false (the default) for polling, monitoring, or periodic tasks so triggers are jittered to avoid load spikes."
}
```

plus one sentence in the description: *"Set ExactTiming only when a cron must
fire at the exact wall-clock time without jitter."*

The other eleven declarations are byte-identical. The **custom-agent** copy of
`schedule` did **not** change — the addition is on the main surface only.

The clone's wire output comes from the embedded fixture, so the swap is the
whole fix; the executor's parameter struct gained the field so the value is
carried rather than silently dropped (the clone has no scheduler, so nothing
acts on it yet).

## 4. The remote-control surface

The public descriptor gained a 72nd RPC, declared last:

```proto
rpc FetchSandboxPort(FetchSandboxPortRequest) returns (FetchSandboxPortResponse);
```

with two new request/response messages and a `SandboxHttpHeader` type, plus a
`conversation_id` field on `ListSlashCommandsRequest`. The internal
`exa.remote_agent_proxy_pb` descriptor gained `ConfigOverrides.autonomous`.

The clone pins the RPC list so drift is noticed, so the pinned list, its
fixture and the evidence dumps all move to 1.4.5. `FetchSandboxPort` has no
clone-side equivalent, so it is **not** added to the alias map — the alias map
only carries exact-behaviour pairs, and the reference's sandbox-port RPC has
no clone handler.

The evidence directory `out/local-macos/protos/` gains the 1.4.5 dumps, with
the new offsets and the extraction pitfall written down: the naive top-level
field walk overshoots the 1.4.5 public descriptor by 62 bytes and swallows the
next blob. The `syntax:"proto3"` marker is the reliable end.

## 5. The changes

| path | change |
|---|---|
| `internal/plugins/plugins.go` | **new** — which plugin directories are loaded (parseable `plugin.json`, `plugins.<name>.enabled`) |
| `internal/agyprompt/rules.go` | the whole discovery set, the `user_global` label, the verbatim body, the 1.4.5 preamble |
| `internal/agyprompt/agyprompt.go` | `BuildAgent` carries `<user_rules>` |
| `internal/agyprompt/rules_test.go` | **new** — order, labels, plugin rules, the name filter, the preamble, the live-capture pin |
| `internal/agyprompt/testdata/user_rules_1.4.5.txt` | **new** — the live 1,223 B block |
| `internal/agent/list.go` | uses `internal/plugins` instead of its own copy |
| `internal/tools/model_tools_1.4.5.json` | **new** — the live 12 declarations |
| `internal/tools/model_surface.go` | embeds the 1.4.5 fixture |
| `internal/tools/tools_tasks.go` | `schedule` carries `exact_timing` |
| `internal/tools/model_surface_test.go` | pins the fixture on disk and the new `ExactTiming` parameter |
| `internal/remotecontrol/reference_surface.go` | the 72nd RPC; the header comment counts |
| `internal/remotecontrol/testdata/jetski_remote_agent_proxy_1.4.5.txt` | **new** — the 72-method list |
| `out/local-macos/protos/*_1.4.5.{txt,proto}` | **new** — the re-extracted descriptors |
| `out/local-macos/protos/README.md` | the 1.4.5 offsets, the drift summary, the extraction pitfall |
| `internal/referencedrift/reference.lock.json` | the registry now describes the 1.4.5 evidence (§5.1) |
| `out/local-macos/seeds/{seeds,funcs}.txt` | regenerated from the 1.4.5 binary (§5.2) |
| `out/local-macos/priority-funcs.txt`, `plan-local.jsonl` | regenerated from the new seeds |
| `out/local-macos/decompile-*.md` (101 files) | regenerated from the new project |
| `out/local-macos/build_reports.py` | the headline numbers are computed, not hardcoded |

The 1.4.4 fixtures stay on disk: they are the historical evidence. They leave
the registry, because the registry tracks the *current* evidence and a
superseded capture is not a stale one — it is the previous release's copy.

### 5.1 The registry

`internal/referencedrift/reference.lock.json` is rewritten to describe what the
evidence actually is. The gate's question is *"is this still true of the live
binary"*, so the three kinds of artifact are handled three ways:

| kind | example | what the lock does |
|---|---|---|
| re-verified against 1.4.5 | `capture_1.4.4.txt`, `advertised_tools_1.4.3.txt`, `agent_decls_1.4.4.json` | `captured_at` moves to 1.4.5 — the artifact is still valid, the stamp is what says so |
| superseded by a newer capture | `model_tools_1.4.5.json`, `jetski_remote_agent_proxy_1.4.5.txt`, the two 1.4.5 proto dumps, `user_rules_1.4.5.txt` | the newer file takes the entry; the 1.4.4 file stays on disk as history |
| not re-verified | `capture_1.4.4.json`, `result_texts_1.4.4.txt` | the stamp stays at 1.4.4, so the gate **keeps saying SUSPECT** |

The seed set joins the registry as a first-class artifact: it is the largest
version-scoped file in the tree and every address in the repository that came
out of it moves on every release.

The gate's output after the rewrite:

```
reference-drift: live /Users/gear/.local/bin/agy is 1.4.5; this clone is pinned to 1.4.5 (lag acknowledged through 1.4.5).
  SUSPECT  internal/agyprompt/testdata/capture_1.4.4.json — captured at agy 1.4.4, unverified against 1.4.5
  SUSPECT  internal/tools/testdata/result_texts_1.4.4.txt — captured at agy 1.4.4, unverified against 1.4.5
```

Two SUSPECT lines are the honest end state, not a failure: both name artifacts
this cycle genuinely did not re-record.

### 5.2 The seed set and everything derived from it

The seed set was regenerated with the vendored tool (offline, ~2 s):

```
cd rea-go-seeds && mkdir -p /tmp/seeds && go run . ~/.local/bin/agy /tmp/seeds
```

| | before | after |
|---|---|---|
| functions recovered | 154,525 | **160,664** |
| distinct names | 154,413 | **160,547** |
| priority functions | 2,101 | **2,257** |

152,486 of the 152,731 symbols shared between the two sets **moved**; only 245
stayed put. Regenerating it invalidates everything derived from it, so the
whole chain was rebuilt:

1. **`out/local-macos/seeds/{seeds,funcs}.txt`** — regenerated.
2. **`out/local-macos/priority-funcs.txt`** — 2,257 functions, and
   **`plan-local.jsonl`** — 25 lines, no unresolved target.
3. **The Ghidra project** — re-imported from the 1.4.5 binary and seeded with
   the new set: **all 160,664 seed addresses resolve**, and 160,531 of them
   carry the seed's own name (the remaining 133 are Go thunk aliases that
   share an address with their target).
4. **`out/local-macos/dec/`** — the priority set decompiled:
   **2,257 processed, 2,257 ok, 0 errors, 0 skipped, 53 s**, 330,089 LOC.
   A 100 % hit rate, against the 57 % of the Linux/ELF pass in `out/`.
5. **The committed reports** — 93 `decompile-*.md` files rewritten and 9 added
   (101 packages now, against 93 before). One more was **pruned**:
   `main.deleteAgentMessageFn` is in the 1.4.4 seed set and **gone from
   1.4.5**, so its report outlived the function and cited an address that no
   longer exists. `build_reports.py` now deletes the reports it did not write,
   so the next release cannot leave one behind either.

`out/local-macos/build_reports.py` used to hardcode three headline numbers
("154,525 functions", "154,506 created", "56 s") — including a wall time for a
run that had not happened yet. They are now computed: the function count from
`seeds/seeds.txt`, the resolved count and the elapsed time from the decompile
pass's own `_summary.txt`.

The reconstructed Go module (`out/local-macos/gosrc/`) was rebuilt from the
new set as well: 379 packages, 39,314 declarations, 43,363 symbols in scope
(was 41,739). The string cross-reference pass (`out/local-macos/xref/`) was
re-run against the new binary: 583,250 ADRP+ADD pairs, 794 matching a
`strings(1)` entry, 427 funcs with at least one cross-ref.

**One trap worth recording:** the decompile pass is *resume-safe* — it skips a
function whose output file already exists and is longer than 100 bytes. Run
against a priority list whose addresses no longer match the project, it
reports `SKIP no function at 0x…` for every entry and silently produces
almost nothing. The first run of this cycle hit exactly that, because the
priority list had been reverted to the old one underneath it; the symptom was
"2,022 skipped", and the fix was to regenerate the list from the *new* seeds
rather than to touch the project. The project was verified independently
first: a `CheckProg` script dumped all 161,175 functions and confirmed that
**160,664 of them sit at seed addresses**, with 160,531 names agreeing.

**A second trap, in the Go-module rebuild.** `out/local-macos/rebuild-gosrc.sh`
starts with `rm -rf "$OUT"` and then regenerates. But two files under
`gosrc/cmd/symsearch/` are hand-maintained and tracked in git — `main.go`
carries cycle 173/177's `-xrefs` support, which `write_symsearch.py` does not
generate, and `main_test.go` is not generated at all. Running the rebuild
therefore **deleted the tracked test and downgraded `main.go`** to the
pre-cycle-173 CLI. The script now preserves both and restores them after
generation. Worth knowing: the first version of this cycle's report would
have said the module rebuilt cleanly, when in fact it had quietly lost a
feature and its test.

## 6. The comment citations

Every package-qualified symbol the clone's comments cite was resolved against
the 1.4.5 function inventory: **51** citations, of which 10 match exactly and
24 are present under another path. The 17 that do not resolve are all
explained — stdlib or PiG symbols, clone-internal types, and two reference
*proto types* (which are not functions and cannot be in a function
inventory). **No reference function symbol cited in the clone's comments is
missing from agy 1.4.5.**

The *committed* seed set was the pre-1.4.4 inventory (154,525 functions)
while the binary had 160,664. The 6,139-function gap was the concrete cost of
not re-seeding; the set is now regenerated offline in ~2 s by the vendored
tool, and everything derived from it was rebuilt with it (§5.2).

One citation was corrected along the way. `internal/pigrt/alwaysload.go`
claimed the changelog sentence was *"recovered verbatim from the v1.3.4 binary
at offset 0x8745 in out/local-macos/seeds/seeds.txt"* — `0x8745` is a **line
number** in `docs/decompiled-v1.3.4/strings-new-in-v1.3.4.txt` (the file the
same sentence cites), not an offset, and `seeds.txt` holds symbols rather than
strings. The comment now says what is true: line 8745 of that dump, and file
offset `0x3b39e39` of the agy 1.4.5 binary.

## 7. Parity table

| surface | 1.4.4 | 1.4.5 | clone state |
|---|---|---|---|
| `init.tools` (60 names) | — | unchanged | pinned to `advertised_tools_1.4.3.txt` (re-verified) |
| model declarations (12) | `schedule` w/o `ExactTiming` | `schedule` **with** `ExactTiming` | **fixed** — `model_tools_1.4.5.json` |
| custom-agent declarations (7) | — | unchanged | pinned to `agent_decls_1.4.4.json` (re-verified) |
| system prompt, static text | — | unchanged | `capture_1.4.4.txt` (re-verified) |
| system prompt, `<user_rules>` | preamble "following" | preamble "other" | **fixed** |
| rule discovery | no plugin rules, any `*.md`, path label | plugin rules, two names, `user_global` label | **fixed** |
| agent prompt `<user_rules>` | absent | present | **fixed** |
| catalogue (24 models) | — | unchanged | `fetchAvailableModels_1.4.4.json` (re-verified) |
| public proto | 71 RPC / 155 msg | 72 RPC / 158 msg | **fixed** — the pinned list moved |
| internal proto | 10 msg | 10 msg, one new field | evidence refreshed |
| function inventory | 154,525 | 160,664 | **regenerated** — seeds, project, `dec/`, reports (§5.2) |
| comment citations | — | all valid | no change needed |

## 8. 硬约束 — hard constraints

These are the things this cycle *did not* do, on purpose:

* **`internal/cli.ReferenceVersion` was not touched.** It was still `1.4.4` when
  this cycle started; another workstream has since moved it (and the lock's
  `version`) to `1.4.5`, which is why the gate now reads *"live is 1.4.5;
  this clone is pinned to 1.4.5"*.
* **`internal/tools/testdata/result_texts_1.4.4.txt` was not re-captured.**
  The 32 result texts need a full tool-using turn against the reference under
  a pty harness; that is the cycle-275/277 apparatus, not a fixture refresh.
* **`internal/agyprompt/testdata/capture_1.4.4.txt` was not replaced.** Its
  static half is provably unchanged, and its environment half is a snapshot of
  one machine's skill inventory — replacing it with this machine's would
  freeze *today's* plugin set into a fixture and make the next re-capture
  harder, not easier. The environment-dependent part that *is* a contract —
  the `<user_rules>` block — is pinned separately as
  `user_rules_1.4.5.txt`.
* **`internal/agyprompt/testdata/capture_1.4.4.json` was not re-recorded.**
  Its inputs (os_version, workspaces, subagents, skills) are one machine's,
  so a new capture would only move the staleness to a new file name.

The lock and the seed set, which this cycle originally left alone, are now
done — §5.1 and §5.2.

## 9. 存疑 — open questions

* **Which release changed the `<user_rules>` preamble?** The 1.4.5 binary says
  "any other instructions" and the 1.3.4 string dump says "any following
  instructions". The 1.4.4 binary is gone (it self-updated), so the change
  cannot be dated more precisely than "between 1.4.3 and 1.4.5".
* **Does the reference discover rules in *every* ancestor directory?** The
  probes covered the home scope, the plugin scope, the global rules directory
  and the workspace directory itself. The ancestor walk above the workspace
  was exercised only where files exist on this machine — no ancestor of this
  checkout carries a rule file, so the walk was never observed to contribute
  one.
* **Does the reference truncate a rule body at 24,000 bytes the same way at
  1.4.5?** **Answered in this cycle** (§2.8): yes — the cap, the
  cut-back-to-the-last-newline and the `<truncated N bytes>` marker all match
  the clone's implementation, measured against this repository's own
  70,176-byte `AGENTS.md`.
* **What does `ExactTiming` actually do?** The declaration says the schedule
  fires "at the exact wall-clock time without jitter"; the reference's jitter
  source and its behaviour with `ExactTiming` set are not observable from the
  wire.

## 10. Limitations

* Every capture in this cycle came from **one machine** and **one account**.
  The Google CloudCode path worked for this account; a different account could
  see a different catalogue or a different monitor configuration.
* The captures are unredacted request bodies held in a `0700` directory
  outside the repository. **No capture file was committed**; the two fixtures
  derived from them (`user_rules_1.4.5.txt`, `model_tools_1.4.5.json`) carry
  no bearer token, but they do carry this machine's absolute paths.
* The `.proto` reconstructions in `out/local-macos/protos/` are renderings,
  not source: the public one is mechanical (validated by re-rendering the
  committed 1.4.4 descriptor and diffing byte-for-byte), the internal one is
  hand-abridged and elides nested types.
* `FetchSandboxPort` is pinned as a reference RPC but has **no clone
  implementation** and no alias. The clone answers **34** of the reference's
  **72** RPC names — the alias map only carries exact-behaviour pairs, and
  `FetchSandboxPort` has no clone-side counterpart.
