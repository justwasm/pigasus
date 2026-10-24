# Cycle `subagent-async-framing-capture` — the mailbox, on the wire

**Status**: measurement only — no clone code
**Reference**: `~/.local/bin/agy` 1.4.4, live turn 2026-10-22 11:24 (+08:00)
**Method**: one `-p` turn through `cmd/agy`'s capture proxy —
`Use the invoke_subagent tool to start a subagent that replies with exactly PONG, wait for it
to finish, then reply with what it said.` — then read every `streamGenerateContent`
`requestBody` in the capture directory.

This is the evidence `docs/cycles/async-subagent-framing/` could not get: that cycle recovered
the texts from decompiled handlers 【反编译】 and left the *wire role* of a delivered message
unknown. It is now 【一手】.

## What the parent model sees

Three messages after the call, in request order:

1. `functionResponse` for `invoke_subagent` — the ack (below).
2. a **model-role text part**: `Waiting for the subagent to respond...` (38 bytes) — the
   reference's own narration, not a tool result.
3. a **user-role text part** carrying the subagent's reply (below).

### The `invoke_subagent` ack (exact)

```
Created At: 2026-10-22T11:24:33+08:00
Completed At: 2026-10-22T11:24:33+08:00
Created the following subagent:
{
  "conversationId":  "73ab3617-5a99-4e26-b1f1-c88e712efe39",
  "logAbsoluteUri":  "file:///Users/gear/.gemini/antigravity-cli/brain/73ab3617-5a99-4e26-b1f1-c88e712efe39/.system_generated/logs/transcript.jsonl",
  "workspaceUris":  [
    "file:///private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode"
  ]
}
The subagent will send you a message when it has completed its task or requires guidance. There is no need to poll for its response.
```

Two details worth keeping: the JSON is **protojson** (two spaces after the colon, two-space
indent, one URI per line in the array) and `logAbsoluteUri` points at
`brain/<child-conversation-id>/.system_generated/logs/transcript.jsonl` — a file the clone
never writes.

### The delivery envelope (exact)

The subagent's reply does **not** arrive as a `functionResponse`, a tool result, or a
sentinel field. It is a plain **user-role** content part, appended to `contents`:

```
The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.

<SYSTEM_MESSAGE>
[Message] timestamp=2026-10-22T03:24:39Z sender=73ab3617-5a99-4e26-b1f1-c88e712efe39 priority=MESSAGE_PRIORITY_HIGH content=PONG
</SYSTEM_MESSAGE>
```

- The preamble (first paragraph + blank line) is fixed.
- The body is one `[Message]` line per delivered message, space-separated key=value:
  `timestamp` (RFC 3339, **UTC** — everything else in the request is +08:00), `sender`
  (the sender's conversation id), `priority` (`MESSAGE_PRIORITY_HIGH` here) and `content`
  (the message text, unquoted, so a multi-line message would continue to the end of the
  block).
- The child sees the mirror image: its *first* user message is the envelope carrying the
  parent's `Prompt`, with `sender` = the parent's conversation id.

### `send_message` success (exact)

`send_message {Recipient: <child id>, Message: "PONG"}` returned, as a `functionResponse`:

```
Created At: 2026-10-22T11:24:38+08:00
Completed At: 2026-10-22T11:24:39+08:00
Message sent to "df83f9c4-ac60-4bce-86f8-9ddad4103f23".
```

That is the `Message sent to %q.` text `subagenttools.sendMessageTool` documents but does not
emit. It is emitted only when a recipient is live — which requires the async transport.

## Why the clone still does not emit these

`invoke_subagent` here is synchronous: it runs the child turn inline and returns the child's
**answer**. That makes three of the reference's texts false in this clone:

| reference text | why the clone must not print it |
|---|---|
| `Created the following subagent: …` + "will send you a message…" | nothing is running after the call returns; there is no child conversation id to poll |
| `Message sent to %q.` | delegations have ended, so every recipient is gone — `recipient %q not found` is the truthful answer, and it is the reference's own wording for that case |
| the `<SYSTEM_MESSAGE>` delivery | a synchronous child has no later turn to deliver into |

The user-facing decision is unchanged: **do not build the mailbox**. What changes is that the
deviation is now documented against the real contract instead of a reconstruction, and the
three texts above are recorded with their exact bytes, so an async transport would not have
to re-measure them.

## Also observed (not this cycle's subject)

- The traffic is on **`gemini-3.8-flash-medium`** (parent) and **`gemini-3.5-flash-lite`**
  (a helper call); the clone's catalogue stops at the 1.4.3 list. Two new ids, a drift-gate
  question rather than a text-parity one.
- The parent request carries `<USER_REQUEST>`, `<ADDITIONAL_METADATA>` (local time) and
  `<USER_SETTINGS_CHANGE>` wrappers around the prompt — the print-mode prompt framing, not
  the system prompt.

## 硬约束

- Everything above is quoted from `requestBody` JSON in a live capture; nothing is inferred
  from decompiled code.
- Only one subagent shape was exercised (`TypeName: "self"`, one message each way). Fan-out
  to several children, `priority` values other than `MESSAGE_PRIORITY_HIGH`, and multi-line
  message bodies are untested.
- The capture directory contains bearer tokens; only the texts above are committed, never the
  raw request bodies.

## 存疑

- Whether the `<SYSTEM_MESSAGE>` preamble is a constant or depends on the child's role /
  `TypeName` is untested (one sample).
- Whether `[Message]` repeats for a burst, or whether several messages arrive as several
  content parts, is untested.
- The `timestamp` is UTC while `Created At` is local; whether that is deliberate or a
  serialisation accident is unknowable from one sample.

## limitations

- No TUI capture. The subagent tool's *transcript* labels (`Finding` / `Killing`) are still
  【strings】 only — this cycle reads the model-visible wire, not what is painted on screen.
- `manage_subagents` was not exercised against a live child, so its `conversationIds`
  round-trip remains decompiled-only.
