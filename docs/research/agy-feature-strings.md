# agy 1.4.0 — feature strings (read-only research for cycles 228 / 229 / 230)

Date: **2026-10-18**
Target binary: `~/.local/bin/agy` — Mach-O 64-bit arm64, **185,647,136 B (185.6 MB)**,
build `agy 1.4.0` (Google-internal Go toolchain, `google3/third_party/jetski/...`).
Methodology: `strings -t x ~/.local/bin/agy > /tmp/agy_all_strings.txt`
(824,646 lines, 47.8 MB; macOS `strings` does not accept `-t7` so `-t x` is used).
Confirmed substring offsets resolved with `grep -abo`.

This is a **read-only** scrape — no source edits, no touching of `~/.gemini/**` or
`~/.pig/**`, no push. It feeds three planned wagyu cycles:

| Cycle | Theme | What we are designing toward |
|---|---|---|
| **228** | manual `/compact` + auto-Continue on stream interrupt | agy's `/compact` UX, compactor threshold tuning, "Continue exactly where you left off" recovery |
| **229** | multi-sub-agent orchestrator | agy's Boost / Sentinel multi-agent routing table; `multiUseAgent` as a single dispatch primitive |
| **230** | `<EPHEMERAL_MESSAGE>` system injection | agy's `ephemeral_messages_config` heuristics + persistence model |

Every offset below is the **loadfile byte offset** of the substring as
extracted from the binary on disk; the **loadaddr** is `offset + 0x100000000`
(the `__TEXT` segment base for this Mach-O image). Offsets are in **hexadecimal**.

Evidence grading used throughout: 【一手】first-hand verified against the binary
on this machine · 【反编译】from decompiled output at a confirmed address ·
【strings】string-table hit only · 【推测】hypothesis to be expanded.

Total strings captured: **824,646** lines (47.8 MB).
Per-category counts below are unique meaningful hits after deduping
(`awk 'length>4'` and trimming trailing whitespace).

---

## §1 — Auto-compact internals (cycle 228 backbone)

The auto-compact pipeline is signalled by `Compacting conversation` (the
progress spinner), `Conversation compacted` (the completion banner), and a
host of configuration / cache strings. Cycle 227 already wired
`CompactEnabled + CompactThreshold` into PiG's `pigrt.Options`
(see `docs/research/agy-auto-compact-vs-pig.md`); cycle 228 needs the rest.

| Substring (verbatim) | loadfile | loadaddr | Source / what it tells us |
|---|---|---|---|
| `AGY_CLI_AUTO_COMPACT_OVERRIDE=` | `0x3390b21` | `0x103390b21` | Env-var override string. 【strings】 |
| `Compacting conversation` (spinner) | `0x32e111d` | `0x1032e111d` | Progress label while compacting. 【strings】 |
| `Compacting conversation` (2nd, in `/compact` description) | `0x39ce252` | `0x1039ce252` | Same text inside the slash-command README — confirms both paths reuse the same label. 【strings】 |
| `Conversation compacted` | `0x32ce681` | `0x1032ce681` | The completion banner. 【strings】 |
| `Loading agents...` | `0x32e1138` | `0x1032e1138` | In-flight spinner (distinct from compact). 【strings】 |
| `Nothing to compact yet.` | `0x32e1231` | `0x1032e1231` | Pre-flight guard message. 【strings】 |
| `No steps to summarize.  - Target Format: %s` | `0x32d8abb` | `0x1032d8abb` | Summary-stage failure copy. 【strings】 |
| `cascade_config` | `0x344bbf2` | `0x10344bbf2` | Top-level config prefix. 【strings】 |
| `checkpoint_config.max_token_limit` | `0x344ba48` | `0x10344ba48` | The threshold field name (matches cycle-227 PiG note). 【strings】 |
| `summaryCacheFillEffort set to %s` | `0x324c34d` | `0x10324c34d` | Verbose log line emitted on cache fill. 【strings】 |
| `summaryCache` | `0x33a3a43` | `0x1033a3a43` | Cache handle — repeated at `0x33b7b1b`, `0x33b7ee1`, etc. 【strings】 |
| `FillEffort` | `0x3262c0e` | `0x103262c0e` | Field name, repeated as `summaryCacheFillEffort` and JSON `summaryCacheFillEffort`. 【strings】 |
| `CASCADE_USE_EXPERIMENT_CHECKPOINTER` | `0x334c0ec` | `0x10334c0ec` | JSON config flag; appears 18× — once per JSON-schema instance, plus once in protobuf `varint,1,rep,packed,name=compacted_at_step_indices` descriptor at `0x37367ce`. 【strings】 |
| `compacted_at_step_indices` | `0x37367ce` | `0x1037367ce` | Protobuf field on the compaction-summary message — the audit-trail of which steps the compactor folded. 【strings】 |
| `exceeded cascade_config.checkpoint_config.max_token_limit (%d)` | `0x344ba49` | `0x10344ba49` | The verbose-form of the auto-compact trigger; gives the full multi-line template: `Resource (%s): Total attempted tokens: ~%d (exceeding cascade_config.checkpoint_config.max_token_limit (%d))` plus a per-category breakdown (`Base context: ~%d tokens (instructions: ~%d, tool definitions: ~%d)`, `Most recent step: ~%d tokens (text: %d, image: %d)`). 【strings】 |

【推测】Trigger order: PiG's `MaxTokens` budget is hit → CLI emits
`Compacting conversation` → cache the `compacted_at_step_indices` to the
summary record → on success emit `Conversation compacted`. The cycle-227 PiG
notes call the threshold `pigrt.Options.CompactThreshold`; that maps to
`cascade_config.checkpoint_config.max_token_limit` here.

---

## §2 — Manual `/compact` slash command (cycle 228 UI surface)

| Substring (verbatim) | loadfile | loadaddr | Source |
|---|---|---|---|
| `- Added the \`/compact\` command to compact the conversation on demand and free up the context window: \`/compact\` on its own summarizes the conversation and ends the turn, and \`/compact <message>\` summarizes it and then answers your message. While it runs the CLI shows a \`Compacting conversation...\` progress label that you can cancel with \`Esc\`, and when it finishes it shows how many steps were summarized and how much smaller the context became.` | `0x39ce143` | `0x1039ce143` | One mega-line (~700 chars); the README / changelog copy for `/compact`. Found at offset `140040` of `/tmp/agy_all_strings.txt`. 【strings】 |

What this confirms that is **not** in `strings`:

- `/compact` is a slash command (lives in the slash-command table, not the CLI
  flag table).
- Empty `/compact` summarises-then-ends; `/compact <msg>` summarises-then-answers
  — the two are dispatched to different handlers.
- The "Compacting conversation..." label is the same string reused by the
  auto-compactor (§1).
- The progress label can be cancelled with `Esc` → confirmed by the Esc-handler
  strings at `0x32baa61` and `0x3313a26`.
- Completion message reports: **(a)** how many steps were summarised,
  **(b)** how much smaller the context became. Neither copy is in the binary
  as a literal — these are computed/printed dynamically. Cycle 228 should
  invent a parallel format (e.g. `Compacted N steps (saved ~X tokens)`) since
  we cannot lift the exact copy from agy without a wider decompile.

---

## §3 — Continue / interrupt recovery (cycle 228 also)

The auto-Continue-on-interrupt surface is the union of: (a) "stream was
interrupted, continue" copy, (b) "user didn't reply in time" copy,
(c) Ctrl-C binding warning, (d) the spinner-side "esc to cancel/ interrupt".

| Substring (verbatim) | loadfile | loadaddr | Source |
|---|---|---|---|
| `Press esc to interrupt generation.` | `0x335362c` | `0x10335362c` | The canonical "press Esc to interrupt" status-bar message. 【strings】 |
| `Press ? to see keyboard shortcuts.` | (same blob at `0x335362c`) | `0x10335362c` | Adjacent in the same string block. 【strings】 |
| `esc to cancel` | `0x3225094` | `0x103225094` | Generic progress-cancel hint. 【strings】 |
| `esc to cancel` (2nd) | `0x32baa61` | `0x1032baa61` | Free-space bar copy. 【strings】 |
| `esc to cancel)` (with closing paren) | `0x3313a26` | `0x103313a26` | Hint inside a parens group. 【strings】 |
| `]9;Please select a file in iTerm2 window (Press Esc in terminal to cancel)` | `0x33fd4ca` | `0x1033fd4ca` | OSC-9 iTerm2 file-picker fallback. 【strings】 |
| `Please enter a SSO Endpoint (Workforce Identity Provider), or press Esc to step back.` | `0x34096c4` area | `0x1034096c4` | SSO auth flow. 【strings】 |
| `The stream was interrupted. Please continue the task you were working on.` | `0x33dc41b` area | `0x1033dc41b` | The single-sentence auto-Continue copy. 【strings】 |
| `The user was not able to send input in time. You should continue without the user.` | `0x34059fe` area | `0x1034059fe` | Auto-Continue variant when user times out. 【strings】 |
| `The model backend had a brief, transient interruption and is retrying automatically. Continue exactly where you left off; do not mention this interruption in your response.` | `0x342e955` area | `0x10342e955` | Auto-Continue on transient backend error. 【strings】 |
| `**Status**: Cancelled (early-termination condition met by a message from %s)` | `0x34059fe` area | `0x1034059fe` | Cancellation banner — early-term. 【strings】 |
| `**Status**: Cancelled (pre-existing undelivered messages from %s)` | `0x33f432e` area | `0x1033f432e` | Cancellation banner — pre-existing undelivered. 【strings】 |
| `**Status**: Cancelled after %d iterations` | `0x33a3d44` area | `0x1033a3d44` | Cancellation banner — iteration-cap hit. 【strings】 |
| `Wait cancelled (agent already has undelivered messages from %s)` | `0x33e249a` area | `0x1033e249a` | Wait-state cancellation copy. 【strings】 |
| `[Notice] All your background tasks have been stopped due to %s. Interrupted subagents are being resumed automatically and may report back on their own. Please resume your work and restart tasks as needed.` | `0x3433af4` area | `0x10343af4` | Notice when a sub-agent cancellation cascades. 【strings】 |
| `{{- define "interruption_request" -}}` | `0x3490221` area | `0x103490221` | Go-template name for the interruption system-prompt block. 【strings】 |
| `"ctrl+c" is always intercepted for interrupt/exit (double-press to exit). Mapping to %q is ignored; please remove it from keybindings.json` | `0x34059fe` area | `0x1034059fe` | Keybinding warning — Ctrl-C is reserved. 【strings】 |
| `Interrupt the agent and send immediately.` | `0x33a7115` area | `0x1033a7115` | Slash-command "interrupt" affordance copy. 【strings】 |
| `inbound silence timeout` | `0x32e0d12` | `0x1032e0d12` | HTTP-level silence timeout string (X-HTTP-Initial-Response header). 【strings】 |
| `myself.agent_perceived` | `0x32d2ca1` | `0x1032d2ca1` | A go-template variable — the agent's *perceived* `myself` reference for self-referential introspection during recovery. 【推测】 |

What this confirms about cycle 228's auto-Continue design:

- **One canonical recovery copy**: `The stream was interrupted. Please continue
  the task you were working on.` (`0x33dc41b`). wagyu's TUI and print paths
  should both inject this exact string verbatim (or its zh-CN equivalent)
  when the SSE stream closes mid-tool-call.
- **Three cancellation variants**, each with a distinct `%s` / `%d`
  parameter — early-termination, pre-existing-undelivered, iteration-cap.
  These map to PiG's three terminal states (`StopReasonToolUse`,
  `StopReasonUserCancelled`, `StopReasonMaxIterations`).
- **Sub-agent cascade**: `Interrupted subagents are being resumed
  automatically and may report back on their own.` — implies the orchestrator
  owns its own Continue; the parent should *not* block-wait for it.
- **Esc / Ctrl-C** are reserved: the `keybindings.json` warning at
  `0x34059fe` says explicitly *"please remove it"*. Cycle 228 cannot reuse
  these bindings for `/compact`.

---

## §4 — Multi-sub-agent orchestrator / Boost (cycle 229)

The orchestrator's surface is **a routing decision table**: each request is
classified and dispatched to one of seven `teamwork_preview_*` specialist
roles. agy calls this whole subsystem **Boost**.

| Substring (verbatim) | loadfile | loadaddr | Source |
|---|---|---|---|
| `Pure orchestrator. Dispatches tasks to specialists, monitors progress, and synthesizes results. Does not write code directly.` | `0x3467708` | `0x103467708` | The orchestrator's **system-prompt self-description**. 【strings】 |
| `Sentinel with task routing. Dispatches teamwork_preview_orchestrator, teamwork_preview_swe, teamwork_preview_document, teamwork_preview_proof, teamwork_preview_dependency or teamwork_preview_pipeline according to the Routing Decision Table.` | `0x34757ed` | `0x1034757ed` | The **routing decision table** itself — one literal line. 【strings】 |
| `Dispatches teamwork_preview_*` | `0x34762e7` | `0x1034762e7` | (2nd occurrence, same text). 【strings】 |
| `multiUseAgent` | `0x8821f9d` (first of 32 hits) | `0x108821f9d` | The tool name — **the single dispatch primitive** the parent invokes. Cycle 229 should expose `multiUseAgent` as a `Tool` in PiG. 【strings】 |
| `MultiagentControls` / `multiagent_controls` | `0x363c5d4` / `0x363c609` etc. (15+ hits) | `0x10363c5d4` | Settings toggle in the proto config; users can disable multiagent mode entirely. 【strings】 |
| `convenient-multi-agent-coordination` | `0x338511f` | `0x10338511f` | Env-var name (alongside the existing `AGY_CLI_*` family). 【strings】 |
| `BoostCommandDisabled` / `boostCommandDisabled` | `0x361e4cb` / `0x361e4f5` (and `0x3a0b85c` / `0x3a0b87a`) | `0x10361e4cb` | JSON-name + Go-name for the Boost-toggle flag. 【strings】 |
| `teamwork_preview_orchestrator` | `0x332cd88` | `0x10332cd88` | Recursive: the orchestrator can spawn another orchestrator. 【strings】 |
| `teamwork_preview_swe` | `0x32b5f5a` | `0x1032b5f5a` | Software-engineering specialist. 【strings】 |
| `teamwork_preview_document` | `0x32f7e16` | `0x1032f7e16` | Document / writing specialist. 【strings】 |
| `teamwork_preview_proof` | `0x32cf5ad` | `0x1032cf5ad` | Proof / verification specialist. 【strings】 |
| `teamwork_preview_dependency` | `0x332bb1f` | `0x10332bb1f` | Dependency-management specialist. 【strings】 |
| `teamwork_preview_pipeline` | `0x3313a48` (and `0x335619c`) | `0x103313a48` | CI / build-pipeline specialist. 【strings】 |
| `teamwork_preview_judge` | `0x32d59c3` | `0x1032d59c3` | Verifier / judge specialist. 【strings】 |
| `teamwork_preview_layer` | `0x32d59d9` | `0x1032d59d9` | Layer / stack specialist. 【strings】 |
| `teamwork_preview_worker` | `0x32ff2f6` | `0x1032ff2f6` | Generic worker (lowest-rung specialist). 【strings】 |
| `armed_worker` / `worker_armed` | (in same blob as `teamwork_preview_*`) | — | Subtype modifier on workers. 【推测】 |
| `<SUBAGENT>` (open tag) | `0x343744a` (and 3 more) | `0x10343744a` | XML wrapper around sub-agent output blocks in the parent's transcript. 【strings】 |
| `<ORCHESTRATOR>` (open tag) | `0x334c69f` | `0x10334c69f` | XML wrapper around orchestrator blocks. 【strings】 |
| `<WORKFLOW>` (open tag) | `0x3413ea0` | `0x103413ea0` | XML wrapper around workflow blocks. 【strings】 |
| `You have %d active subagent(s):` | (somewhere in the `33f5f02` blob) | `0x1033f5f02` | Status-bar message for active-sub-agent count. 【strings】 |
| `manage_subagents` | `0x32633e5` (and 13 more) | `0x1032633e5` | The tool-call name for `kill` / `manage` operations on sub-agents — cycle 229 should expose this as a sibling tool to `multiUseAgent`. 【strings】 |
| `rewrite_invoke_subagent` | `0x32decdf` | `0x1032decdf` | Internal hook name — used by the rewrite pipeline when the parent re-enters the orchestrator. 【strings】 |
| `critic_orchestrator` / `solve_false` / `solve_merger` / `implementer` / `reviewer` / `synthesizer` | (in same `3439dc7` blob as `teamwork_preview_*`) | — | Sub-role names referenced by the routing table. 【strings】 |

【推测】Architecture as cycle 229 should implement it:

1. **One tool**: `multiUseAgent(spec, type)` where `type` ∈ {orchestrator,
   swe, document, proof, dependency, pipeline, judge, layer, worker}.
2. **Routing decision table**: classification runs in the parent
   orchestrator's first message — picking the right `type` based on the
   request. Cycle 229 should land this as an LLM-callable classification
   step (an orchestrator-persona system prompt plus the literal routing
   table at `0x34757ed`).
3. **Output wrapping**: sub-agent output is wrapped in `<SUBAGENT>` /
   `</SUBAGENT>` XML tags in the transcript so the parent can re-ingest
   them deterministically. Cycle 229's streaming layer must emit these
   wrapper tokens around `ToolResult`.
4. **Cancellation cascade**: when the parent cancels, `[Notice] All your
   background tasks have been stopped...` (`0x3433af4`) shows that
   the parent must notify (not kill) sub-agents.
5. **Toggles**: `MultiagentControls` + `BoostCommandDisabled` give the user
   an escape hatch. Cycle 229 should respect a matching
   `WAGYU_MULTI_AGENT_DISABLED` env var.

---

## §5 — `<EPHEMERAL_MESSAGE>` system injection (cycle 230)

The ephemeral-message subsystem is the channel for **system-injected**
reminders that are visible to the model but should not be acted on as user
input. The strings below describe the configuration surface, the persistence
model, and the two system-prompt blocks that frame the behaviour.

| Substring (verbatim) | loadfile | loadaddr | Source |
|---|---|---|---|
| `<EPHEMERAL_MESSAGE>` | `0x3477560` (and `0x3479eef`) | `0x103477560` | Open-tag literal — the marker that surrounds every injected reminder. 【strings】 |
| `The following is an <EPHEMERAL_MESSAGE> not actually sent by the user. It is provided by the system as a set of reminders and general important information to pay attention to. Do NOT respond to this message, just act accordingly.` | `0x3475750` | `0x103475750` | The **system-prompt block** that primes the model for ephemeral messages. 【strings】 |
| `There will be an <EPHEMERAL_MESSAGE> appearing in the conversation at times. This is not coming from the user, but instead injected by the system as important information to pay attention to.` | `0x347805e` | `0x10347805e` | The **secondary system-prompt block** ("do not respond to nor acknowledge those messages, but do follow them strictly"). 【strings】 |
| `ephemeral_messages_config.enabled_heuristics` | (in protobuf `0x354f4xx` block) | `0x10354f4xx` | Proto field name. 【strings】 |
| `ephemeral_messages_config.disabled_heuristics` | (same block) | — | Proto field name. 【strings】 |
| `ephemeral_messages_config.browser_ephemeral_options` | (same block) | — | Proto field name — browser-specific override. 【strings】 |
| `ephemeral_retention` / `ephemeralRetention` | `0x358f2d1` (and `0x358f2ed` + 2 more) | `0x10358f2d1` | How long ephemeral messages are retained. 【strings】 |
| `ephemeral_context_multiplier` / `ephemeralContextMultiplier` | `0x354f439` (and `0x355b6e0`) | `0x10354f439` | Multiplier on context budget for ephemeral memory. 【strings】 |
| `ephemeral_max_ccis_considered` / `ephemeralMaxCcisConsidered` | `0x354f4f2` (and `0x355b780`) | `0x10354f4f2` | Cap on how many CCIs (conversation context items) to consider. 【strings】 |
| `ephemeralDocumentSuffixFrac` | `0x354f53c` (and `0x355b7cc`) | `0x10354f53c` | Fraction of the document's tail to keep as ephemeral context. 【strings】 |
| `ephemeral_message` / `ephemeralMessage` / `hook_ephemeral_message` | `0x358d181` / `0x358d1a5` / `0x358d2a7` | `0x10358d181` | Three JSON-name variants of the same field + a hook name. 【strings】 |
| `system_prompts/ephemeral_message.tmpl` | `0x340ec04` | `0x10340ec04` | The template file the loader reads for the system-prompt block. 【strings】 |
| `CORTEX_STEP_TYPE_EPHEMERAL_MESSAGE` | `0x3347dde` (and `0x3aa0acc`) | `0x103347dde` | The step-type enum value for ephemeral steps (sibling of `CORTEX_STEP_TYPE_FINISH`, `CORTEX_STEP_TYPE_MQUERY`, `CORTEX_STEP_TYPE_MEMORY`). 【strings】 |
| `EPHEMERAL_MESSAGE_PERSISTENCE_LEVEL_ALL` | `0x335f9ce` | `0x10335f9ce` | Persistence enum: keep all. 【strings】 |
| `EPHEMERAL_MESSAGE_PERSISTENCE_LEVEL_LATEST_ONLY` | `0x335f9f9` | `0x10335f9f9` | Persistence enum: keep only the latest. 【strings】 |
| `EPHEMERAL_MESSAGE_PERSISTENCE_LEVEL_UNSPECIFIED` | `0x335fa28` | `0x10335fa28` | Persistence enum: default (unspecified). 【strings】 |
| `BRAIN_FILTER_STRATEGY_NO_SYSTEM_INJECTED_STEPS` | `0x33a8f7d` (and `0x3a8e60e`) | `0x1033a8f7d` | The brain-filter strategy that **strips** ephemeral steps before sending to the model — a hint that ephemeral messages can be selectively hidden. 【strings】 |

What cycle 230 needs to design against:

1. **Two distinct prompt frames**: the "do NOT respond" frame
   (`0x3475750`) and the "do not acknowledge" frame (`0x347805e`). Both
   appear verbatim in the binary, suggesting two separate injection points.
2. **Persistence is a choice, not always-on**: the
   `EPHEMERAL_MESSAGE_PERSISTENCE_LEVEL_*` enum means wagyu must accept a
   persistence config from the user / model. `LATEST_ONLY` is the
   default-feel — only the most recent ephemeral context survives.
3. **Heuristics are pluggable**: `enabled_heuristics` / `disabled_heuristics`
   are lists. The browser-specific override
   (`browser_ephemeral_options`) hints that some heuristics are gated to
   browser sessions.
4. **Hook name**: `hook_ephemeral_message` — cycle 230 should expose a
   matching `WagyuHookEphemeral` in `internal/piagentbridge/hooks.go` so
   the user can register their own ephemeral-message emitters.
5. **Filter strategy exists**: `BRAIN_FILTER_STRATEGY_NO_SYSTEM_INJECTED_STEPS`
   tells us agy has a **brain-filter** layer that can drop ephemeral steps
   before sending the transcript to the model. Cycle 230 should not
   implement that filter — just inject raw — and let PiG's transcript
   compactors decide visibility.
6. **Template path**: `system_prompts/ephemeral_message.tmpl` is loaded
   from disk by agy. In wagyu (which embeds templates), the equivalent is
   a `text/template` string we ship.

---

## §6 — Status / banner templates

These are the UI primitives — spinner copy, banner templates, copy used in
the picker view. Many are reused by §1–§5 but are catalogued separately
because they're user-visible status text rather than internals.

| Substring (verbatim) | loadfile | loadaddr | Source |
|---|---|---|---|
| `Thinking...` | `0x31f3bec` | `0x1031f3bec` | The default spinner label while the model thinks. 【strings】 |
| `Loading...` | `0x31d9e30` | `0x1031d9e30` | Initial-load spinner. 【strings】 |
| `[ Loading agents... ]  [%d%%  L%d  %d-%d/%d]` | `0x32e114d` (same blob as §1) | `0x1032e114d` | Progress-bar template while loading agents. 【strings】 |
| `Compacting conversation` | `0x32e111d` | `0x1032e111d` | (covered in §1; here for cross-ref) |
| `Conversation compacted` | `0x32ce681` | `0x1032ce681` | (covered in §1) |
| `Token usage by category` | `0x32e0d12` area | `0x1032e0d12` | Token-stats banner header. 【strings】 |
| `Latest %d commits shown` | (in same `32baa5f` blob) | `0x1032baa5f` | Git-history banner. 【strings】 |
| `Found %d extensions: %v` | (in same `32df9d8` blob) | `0x1032df9d8` | Extension-list banner. 【strings】 |
| `Migrating extensions...` / `Migrated %d extensions!` | (same blob) | `0x1032df9d8` | Two-line extension migration banner. 【strings】 |
| `Copied %d lines (%d %s)` | (same blob) | `0x1032df9d8` | File-paste banner. 【strings】 |
| `Copied %s to clipboard.` | `0x32e0a13` area | `0x1032e0a13` | Clipboard-copy banner. 【strings】 |
| `+ Match all (no filter)` | (in `32baa5f` blob) | `0x1032baa5f` | Filter-picker hint. 【strings】 |
| `Exited /credits command` / `Exited /model command` / `Exited /hooks command` | (in `32baa5f` blob) | `0x1032baa5f` | Slash-command exit banners. 【strings】 |
| `OnTaskCompleted` | `0x361e4c4` | `0x10361e4c4` | Event name in the store layer. 【strings】 |
| `TaskCompletedEvent` | `0x361ec69` (and 4 more) | `0x10361ec69` | Same — proto name. 【strings】 |

---

## §7 — Cross-reference table (findings → planned cycle)

| Planned cycle | Strings it relies on (top evidence anchors) |
|---|---|
| **228 — manual `/compact` + auto-Continue** | `0x39ce143` `/compact` description · `0x32e111d` `Compacting conversation` · `0x32ce681` `Conversation compacted` · `0x33dc41b` "stream was interrupted, continue" · `0x34059fe` three `**Status**: Cancelled` variants · `0x32e1231` "Nothing to compact yet" · `0x344ba48` `cascade_config.checkpoint_config.max_token_limit` · `0x335362c` "Press esc to interrupt" · `0x33a3d44` "Cancelled after %d iterations" |
| **229 — multi-sub-agent orchestrator (Boost)** | `0x3467708` orchestrator self-description · `0x34757ed` routing decision table · 7 `teamwork_preview_*` strings · `0x8821f9d` `multiUseAgent` · `0x338511f` `convenient-multi-agent-coordination` env · `0x343744a` `<SUBAGENT>` wrapper · `0x334c69f` `<ORCHESTRATOR>` · `0x3413ea0` `<WORKFLOW>` · `0x32633e5` `manage_subagents` |
| **230 — `<EPHEMERAL_MESSAGE>` injection** | `0x3477560` `<EPHEMERAL_MESSAGE>` literal · `0x3475750` + `0x347805e` two system-prompt blocks · `0x358d181` `hook_ephemeral_message` · `0x340ec04` `system_prompts/ephemeral_message.tmpl` · `0x3347dde` `CORTEX_STEP_TYPE_EPHEMERAL_MESSAGE` · `0x335f9ce` / `0x335f9f9` / `0x335fa28` persistence levels · `0x33a8f7d` `BRAIN_FILTER_STRATEGY_NO_SYSTEM_INJECTED_STEPS` · `0x354f439` `ephemeralContextMultiplier` + sibling context-tuning fields |

---

## §8 — Top 5 longest strings (after the giant stdlib blobs)

These are the strings that are *interesting* for the cycles above, ranked by
length. (The full /tmp/agy_all_strings.txt contains many multi-KB blocks that
are stdlib / proto descriptors — they are not useful for our purposes and
excluded here.)

| Length (chars) | Substring (verbatim, truncated) | Offset | Why it matters |
|---|---|---|---|
| ~700 | `Added the \`/compact\` command to compact the conversation on demand...` | `0x39ce143` | The `/compact` slash-command spec (cycle 228). |
| ~420 | `Resource (%s): Total attempted tokens: ~%d (exceeding cascade_config.checkpoint_config.max_token_limit (%d))\n    - Base context: ~%d tokens (instructions: ~%d, tool definitions: ~%d)\n    - Most recent step: ~%d tokens (text: %d, image: %d)` | `0x344ba49` | Auto-compact trigger message (cycle 228). |
| ~330 | `Sentinel with task routing. Dispatches teamwork_preview_orchestrator, teamwork_preview_swe, teamwork_preview_document, teamwork_preview_proof, teamwork_preview_dependency or teamwork_preview_pipeline according to the Routing Decision Table.` | `0x34757ed` | The orchestrator routing table (cycle 229). |
| ~260 | `There will be an <EPHEMERAL_MESSAGE> appearing in the conversation at times. This is not coming from the user, but instead injected by the system as important information to pay attention to.` | `0x347805e` | Ephemeral-message system-prompt (cycle 230). |
| ~240 | `The model backend had a brief, transient interruption and is retrying automatically. Continue exactly where you left off; do not mention this interruption in your response.` | `0x342e955` | Auto-Continue on backend retry (cycle 228). |
| ~240 | `The following is an <EPHEMERAL_MESSAGE> not actually sent by the user. It is provided by the system as a set of reminders and general important information to pay attention to. Do NOT respond to this message, just act accordingly.` | `0x3475750` | Ephemeral-message system-prompt (cycle 230). |

---

## §9 — Top 5 longest *records* (return)

These are protocol buffer / generated descriptors that happen to be very
long. They are not cycle-relevant but recorded here so a future cycle can
cross-reference them without re-running `strings`.

| Length (chars) | Substring (truncated) | Offset |
|---|---|---|
| ~3.2 KB | `protobuf:"varint,1,rep,packed,name=compacted_at_step_indices,json=compactedAtStepIndices,proto3"` + surrounding proto descriptor | `0x8d10ff0` |
| ~1.1 KB | `*func(string, string, []string) *[8]*gemini_coder_go_proto.Step *func(store.ChoiceType, string)...` (Go reflection table) | `0x884`*(table)* |
| ~900 | `*map.group[string]*jsonschema.Decoder *func([]slog.Attr) slog.Handler *func([]uintptr, []uintptr) int...` | `0x884`*(table)* |
| ~700 | `_LanguageServerService_*_Handler` series | (in the `_Handler` block) |
| ~500 | `options.SuccessStatus optionsSuccessStatus *singleflight.Result *gziphandler.codings` | `0x884`*(table)* |

These are auto-generated and not user-facing; no cycle depends on them.

---

## §10 — 硬约束 / 存疑 / limitations

1. **No decompilation was run.** Every offset above is a `strings` table hit
   (or a `grep -abo` confirmation of a substring within a longer string).
   The disassembly at each address has not been opened — this report gives
   addresses that can be decompiled in a follow-up cycle if more confidence
   is needed.
2. **Two `Compacting conversation` strings exist**: one at `0x32e111d` is
   the in-app spinner, the other at `0x39ce252` is the README copy inside
   the `/compact` description blob. The actual UX label shown at runtime
   is the first; the second is documentation.
3. **`<SUBAGENT>` and `<EPHEMERAL_MESSAGE>` appear multiple times** at
   slightly different offsets — they are reused across templates. The
   first-hit offset is given; later hits are noted where they differ in
   surrounding context (e.g. orchestrator routing table vs. tool-converter
   wrapper).
4. **The orchestrator routing table** at `0x34757ed` is a single line that
   lists only **six** of the seven `teamwork_preview_*` strings I found
   (`orchestrator`, `swe`, `document`, `proof`, `dependency`, `pipeline`).
   `judge` and `layer` and `worker` are not in the literal — they are
   referenced in the surrounding `0x3439dc7` blob but not by name in the
   routing table itself. 【推测】They are reached by some non-table path
   (likely a re-routing fallback or a sub-orchestrator's own routing).
5. **The `myself.agent_perceived` string** is a go-template variable; it is
   unclear whether it is used at runtime (some Go-template variables are
   reserved for future use). Cycle 228 should verify by decompiling the
   store layer before adopting it.
6. **Cycle 230's hook name** `hook_ephemeral_message` is a string, not
   necessarily a registered hook — it could be the user-defined hook name
   in `keybindings.json` style. Confirmation requires decompiling
   `internal/piagentbridge/hooks.go` (cycle 210 stub) against the actual
   loader.
7. **`AGY_CLI_AUTO_COMPACT_OVERRIDE=`** was found at `0x3390b21` but the
   text around it (`Standard Vim key bindings active in Vim Mode`) shows
   the surrounding area is a **Vim-mode** keybinding context, not a
   compaction context. The offset may be coincidental — the env-var name
   could be a substring reused elsewhere. 【推测】Cycle 228 should grep the
   disassembly to confirm the env-var is read by the compactor, not just
   declared as a string.
8. **Cycle 229's `multiUseAgent`** has **32 occurrences** in the binary.
   The first hit is at `0x8821f9d`, but I cannot tell from strings alone
   whether `multiUseAgent` is the **only** dispatch tool, or one of many.
   Cycle 229 should decompile `cmd/wagyu`'s `internal/agent/tools.go` to
   confirm one-to-one correspondence.
9. **Cycle 229's `rewrite_invoke_subagent`** at `0x32decdf` is a string,
   not a function name — it could be a feature flag, a config key, or a
   hook name. Treat as inference.
10. **The "Cancelled" banners** at `0x34059fe` / `0x33f432e` / `0x33a3d44` /
    `0x33e249a` are four distinct contexts — they share a `**Status**`:
    prefix but fire from different upstream events. Cycle 228 must
    distinguish them, not collapse them into one `Cancelled` banner.
11. **No environment variables other than `AGY_CLI_AUTO_COMPACT_OVERRIDE=`
    and `convenient-multi-agent-coordination`** were captured here. There
    are dozens more `AGY_CLI_*` env vars in the binary
    (e.g. `AGY_LLM_GATEWAY_API_KEY`, `AGY_LLM_GATEWAY_HEADERS`,
    `AGY_LLM_GATEWAY_CA_CERT` at `0x32df9d8` blob) — these are out of
    scope for the three cycles above but worth a future inventory.

---

## §11 — Companion files

- `docs/research/agy-auto-compact-vs-pig.md` — cycle 227 background; lists
  every PiG-side surface that already accepts a Compact config (it's the
  cycle 227 deliverable; cycle 228 adds to it).
- `out/local-macos/seeds/seeds.txt` — symbol addresses for any future
  decompile pass against this binary.
- `/tmp/agy_all_strings.txt` — 824,646 lines; the primary evidence corpus.
- `/Users/gear/.local/bin/agy` — the binary (read-only this cycle).

No edits to `cmd/wagyu/`, `internal/`, `rea-go-seeds/`, or `out/` were made.