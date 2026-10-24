# Cycle `subagent-registry-texts` — the subagent registry surface, aligned

**Status**: implemented (no turn-engine change)
**Reference**: `~/.local/bin/agy` 1.4.4 — texts 【一手】 from
`internal/tools/testdata/result_texts_1.4.4.txt`, or 【反编译】 from
`tools.(*ManageSubagentsTool)` (`docs/cycles/async-subagent-framing/deliverable.md` §`manage_subagents`).
**Scope**: the *safe* half of the subagent parity gap — result text, declared schema and
description only. Nothing here touches the agent loop.

## TL;DR

`send_message` and `manage_subagents` were cycle-228 stubs: `send_message` advertised a
`subagent_id`/`message` schema that the reference never declares, and `manage_subagents` answered
`list` with `{"ok":false,"error":"…not yet implemented (cycle 228 stub)…"}` — a stub marker that
was reaching the model's context. Both now speak the reference's own vocabulary:

| tool | before | after |
|---|---|---|
| `send_message` | `subagent_id`/`message` schema, "not yet implemented (cycle 228 stub)" description | captured `Recipient`/`Message` schema (both `minLength 1`), captured description, unknown-recipient text unchanged (it was already the reference's) |
| `manage_subagents` `list` | `{"ok":false,…stub…}` with `IsError: true` | `You have 0 active subagent(s):\n[]` |
| `manage_subagents` `kill` (explicit ids) | `No active subagents to kill.` | `conversation %q is not an active subagent of this conversation, so nothing was killed; run the %q action to see the active subagents and their ids` |
| `manage_subagents` `kill_all` | `No active subagents to kill.` | unchanged — it was already the reference's text |
| `manage_subagents` schema | `action` enum `list,kill,status` + `subagent_id` | `Action` enum `list,kill,kill_all` + `ConversationIds`, matching the declaration |

## What was deliberately **not** adopted

The reference's *success* texts are recovered, and three of them are **not** emitted, because the
clone delegates synchronously and emitting them would assert something that did not happen:

| reference text | why not |
|---|---|
| `Message sent to %q.` / `Message sent to %d subagents: %s.` / `Message sent to %d of %d subagents: %s.` | by the time the parent can call `send_message`, the subagent's turn has already ended and its `coding.Session` is closed. Nothing is delivered, so "sent" would be a lie. `recipient %q not found` is the reference's own wording for an unknown conversation and is what the clone truthfully has. |
| `Created the following subagent:\n{…}\nThe subagent will send you a message when it has completed its task or requires guidance. There is no need to poll for its response.` (`invoke_subagent` ack, 【一手】) | the ack is inherently async: it promises a later message that the clone never sends. The clone returns the subagent's answer in the same result instead. Emitting the ack would either drop the answer or produce a hybrid the reference never prints. |
| `Successfully killed %d subagent(s) and their descendants.\nKilled roles: %s` | unreachable: the clone never has a live id to kill. Kept in this report as the target shape for a future registry. |

This is the same judgement recorded in `docs/cycles/async-subagent-framing/` (mailbox measured, not
implemented) and in `docs/cycles/cycle-293-model-tool-surface/` (synchronous delegation is a
documented, deliberate deviation).

## Why `list` always answers zero

Delegation is synchronous: `invokeTool.Execute` runs the subagent's session to completion before it
returns, so no subagent can be live while the parent runs another tool. `0 active subagent(s)` is
therefore not a placeholder — it is the true count for the clone.

The format is the reference's (`You have %d active subagent(s):\n%s`, 【反编译】 `.list` @`0x101d5fff0`,
`%s` being the marshalled state slice). The **zero case** is 【推测】: the string table carries only the
positive format, so an empty state slice was assumed to marshal as `[]` (Go's `json.Marshal` of a
non-nil empty slice). The entry schema — `type`, `role`, `conversationId`, `state` with
`state ∈ running,idle,waiting_for_input,waiting_for_dependents,waiting_for_message,canceling,errored,unspecified`
— is recorded in `docs/cycles/async-subagent-framing/deliverable.md` and is what a future registry
would emit.

## Schema alignment is cosmetic on the PiG path, and that is fine

`piagentbridge.SurfaceTools` wraps every registered executor in a `surfaceTool` whose `Schema()`
returns the **captured 1.4.4 declaration**, so the executor's own `Schema()` never reaches the model
(`internal/piagentbridge/tool.go`). The schemas are still aligned because they are what direct
callers, tests and future surfaces read — leaving a stub's invented `subagent_id` there invites a
second caller to depend on it.

`manage_subagents` keeps accepting the snake_case spellings (`action`, `subagent_id`,
`conversation_ids`) and `status` as an alias for `list`; the declared enum is the reference's
`list,kill,kill_all`, so those are clone-only compatibility paths, not advertised surface.

## Deferred: the TUI step labels

`cmd/wagyu/tui_tools.go` has no `stepTitle`/`toolStepSummary` case for `invoke_subagent` or
`manage_subagents`, so both render as raw snake_case names. The reference's spinner/terminal verbs
are recovered (`Starting…`→`Started`, `Finding…`→`Found`, `Killing…`→`Killed`, `Spawning`,
`Messaged` 【strings】) but the **rendered line is not**: the verb's arguments, the count, and where
the hint lands are unknown.

Cycle 285 spent a full cycle on exactly this class of mistake — display strings that looked obvious
and were wrong — and its rule is that only pty-captured forms get drawn. Adding `● Found subagents`
would be an invention, so the raw name stays until a capture of a real subagent turn exists.

## Verification

```
go build ./... ; go vet ./internal/subagenttools/
go test ./internal/subagenttools/ ./internal/tools/ ./internal/piagentbridge/
go test ./cmd/... ./internal/...
```

`go test` covers: the captured `Recipient`/`Message` schema, the `list`/`kill`/`kill_all` texts
(`TestManageSubagentsList`, `TestManageSubagentsKill`), the `Action`/`ConversationIds` compatibility
path, and the absence of the stub marker in either description.

## 硬约束

- No turn-engine change, no mailbox, no registry. The texts above are the whole delta.
- The three success texts in the "not adopted" table **must not be added** without an asynchronous
  delivery path; they would make the tool report work it did not do.
- Do not "improve" the captured descriptions — `send_message`'s ("Not a channel to the user.") and
  `manage_subagents`'s are byte-identical to the 1.4.4 capture.

## 存疑

1. The `list` zero case (`[]` rather than `null`, and the exact spacing) is 【推测】; the positive
   format is decompiled, the empty rendering is not.
2. Whether the reference lists a *finished* subagent as `idle` — i.e. whether `list` would be
   non-empty after a synchronous delegation completes — could not be measured. If it would, the
   clone's `0` is still truthful (it has no registry) but the reference's answer for the same call
   sequence may differ.
3. `status` as a `list` alias is clone-only; the reference's enum has no such value (its per-subagent
   state is the `state` field of the list JSON).

## limitations

- No live capture was made in this cycle; everything traces to the 1.4.4 result-text capture or the
  decompiled handlers recorded in `docs/cycles/async-subagent-framing/`.
- The TUI still shows raw tool names for both tools (see *Deferred*).
- `internal/tools/catalog.go`'s one-line `manage_subagents` description ("List, update or delete
  defined subagents.") was left alone: it feeds the availability list, not a declaration, and it is
  outside this cycle's surface.
