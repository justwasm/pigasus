# The 1.4.7 declaration variants — deliverable

**Cycle:** `cycle-306-declaration-variants`
**Subject:** the 1.4.7 binary holds **two** variants of four model-facing
tool descriptions. The clone shipped one of them and called it "the 1.4.7
declaration"; this cycle proves *which* one a live 1.4.7 session sends,
records the other four as evidence, and corrects the descriptions fixture so
that the catalogue stops advertising text the reference never puts on the
wire.
**Evidence:** 【一手】 the live reference — the request bodies captured on
2026-10-22 (`/tmp/agy-cap-147`, `/tmp/agy-cap-sb`) — and the binary's own
bytes, read straight out of the Mach-O (`~/.local/bin/agy`, agy **1.4.7**,
SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`);
【反编译】 the runtime layer's `Description()` methods, from the binary's
literals and addresses.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the shipped declaration** | `internal/tools/model_tools_1.4.6.json` — believed to be "the 1.4.7 declaration" | unchanged, and now **proven** to be what a live 1.4.7 session sends |
| **the second variant** | unknown; three texts in the descriptions fixture were the *newer* variant | recorded as evidence — `internal/tools/testdata/tool_variants_1.4.7.json`, four tools, every literal's vmaddr and length |
| **the descriptions fixture** | carried the newer variant for `send_message`, `manage_subagents` and `run_command` | carries the **default** variant — the text the reference sends |
| **the catalogue's prose** | advertised the newer variant | advertises the default, like the reference |

## 1. The question

Cycle 305 read thirty-six tool descriptions out of the 1.4.7 binary. For
three of them it found a text that differs from the clone's shipped
declaration:

| tool | shipped (1.4.6 capture) | the other text in the binary |
|---|---|---|
| `run_command` | 397 B | 499 B — adds *"Instead, set NotificationTimeoutSeconds to be notified if a command is still running after that long."* |
| `send_message` | 89 B | 164 B — adds *"or pass Recipient='@children' to broadcast to all active direct subagents"* |
| `manage_subagents` | 261 B | 388 B — adds the `'list_peers'` sentence |

The 1.4.7 binary holds **both** texts for each. Which one goes on the wire
decided whether the clone's declaration was stale or not — so this cycle
settled it against the live reference rather than against the strings.

## 2. The live answer 【一手】

The reference's request bodies captured on 2026-10-22 are still on this
machine:

| capture | surface | `run_command` | `send_message` | `manage_subagents` | `edit_file` |
|---|---|---|---|---|---|
| `/tmp/agy-cap-147/…-026-streamGenerateContent.json` | default agent, no sandbox | 397 B, 5 properties | 89 B | 261 B | 393 B |
| `/tmp/agy-cap-sb/…-024-streamGenerateContent.json` | default agent, `--sandbox` | 397 B, 6 properties | 89 B | 261 B | 393 B |
| `internal/tools/model_tools_1.4.6.json` | the clone's fixture | 397 B, 6 properties | 89 B | 261 B | 393 B |

Every description is **byte-identical** to the shipped fixture. The only
difference between the two captures is `run_command.BypassSandbox`, which the
reference omits when the session is not sandboxed — the known `--sandbox`
conditional, not a version difference.

The 499 B / 164 B / 388 B texts are therefore **not** what a default 1.4.7
session sends. The clone's declaration was already right; the *descriptions
fixture* was wrong, because cycle 305 recorded the newer variant as if it
were the reference's text.

## 3. What the four variants are

Read out of the binary (【一手】, the literals' vmaddr and length are in the
fixture):

| tool | default | alternate | what the alternate adds |
|---|---|---|---|
| `run_command` | 397 B — `0x10362bc2c/68` + `0x10366028f/117` + `0x1036834ea/286` | 499 B — the same three literals, the third rendering one more sentence | the `NotificationTimeoutSeconds` sentence, and the parameter the runtime's `(*RunCommandTool).ParameterSchema` declares (`0x101de04d0`; the name at `0x10353caa0/26`, its prose at `0x10368c1d2/386`) |
| `send_message` | 89 B — `0x10364c32c/89` | 164 B — `0x10366f6a0/164` | the `Recipient='@children'` broadcast clause |
| `manage_subagents` | 261 B — `0x10368050a/261` | 388 B — `0x10368c95e/388` | the `'list_peers'` sentence |
| `edit_file` | 393 B — `0x10368d593/393` | 656 B — `0x10369fbdd/656` | the multi-edit form: an `Edits` list, an `AllowMultiple` flag, and *"Target must appear exactly once in the file"* |

The alternates are not noise: they name a parameter, a recipient form, a
subcommand and a schema the reference's own code paths know about. They are
the reference's *newer* surface — a server or a flag selects them — and the
selection was not traced.

## 4. What this cycle changed

- `internal/tools/testdata/tool_variants_1.4.7.json` — **new**. The four
  alternates, with the default's length and address, the alternate's text
  and address, and the provenance note.
- `internal/tools/variants.go` — **new**. `ReferenceVariants()` reads it.
- `internal/tools/testdata/tool_descriptions_1.4.7.json` — the three texts
  that were the newer variant now carry the **default**, and the file's
  `source` says so.
- `internal/tools/variants_test.go` — **new**. Pins the shipped declaration
  against the default lengths, the alternates' bytes, and the fact that the
  catalogue does not advertise an alternate.
- `internal/tools/model_surface.go` — the provenance paragraph now records
  the two variants and which one the clone ships.
- `internal/referencedrift/reference.lock.json` — the new artefact is sealed;
  the descriptions fixture's digest and the two `how` texts moved.

**Not changed:** the wire. `internal/tools/model_tools_1.4.6.json` stays the
declaration the clone sends, because it is the declaration the reference
sends.

## 5. What this cycle proves

1. The clone's twelve model-facing declarations are the reference's 1.4.7
   declarations — byte for byte, against a live capture rather than against
   the binary's strings.
2. The 1.4.7 binary's *other* texts are a second surface, not a newer
   default. A `strings` hit proves a text exists, never that it is sent —
   the same trap `docs/cycles/reference-drift-147/captures.md` §7.2 records
   for the custom-agent surface.
3. The catalogue's prose is now the text the reference actually sends.

## 6. 存疑 / open questions

- **【推测】** The alternates are selected by a runtime flag or by the
  server's request. The selector was not traced (no flag name, no request
  field); the reference's own changelog does not mention it.
- **【推测】** `run_command`'s alternate schema carries
  `NotificationTimeoutSeconds` (the runtime's `ParameterSchema` names it at
  `0x10353caa0/26`). The alternate's *type* is inferred — the prose says
  "in seconds" and every other duration in the capture is an `INTEGER`.
- **【推测】** The alternates may be the surface a *newer Cascade server*
  asks for. Nothing in this repository shows the request that selects them.

## 7. Limitations

- The proof rests on the request bodies captured on **2026-10-22** from
  this machine. A different account, workspace or server could select the
  other variant; the captures in `/tmp` are ephemeral and are not committed.
- The alternates' *schemas* were not recovered: only the descriptions and,
  for `run_command`, the parameter name and prose. The alternate
  `edit_file`'s `Edits` / `AllowMultiple` schema is described by its prose
  alone.
- The extraction covers the **CLI** binary only. The IDE's language server
  carries its own tool definitions and its addresses do not mix with this
  binary's.
