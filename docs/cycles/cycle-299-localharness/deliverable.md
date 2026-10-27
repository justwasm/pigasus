# The `localharness` layer — deliverable

**Cycle:** `cycle-299-localharness`
**Subject:** Antigravity ships a **second** reference binary — the agent
backend `localharness`, inside the `google-antigravity` PyPI wheel. This
cycle reverses it, recovers its protocol, reimplements three of its
subsystems in Go, and audits the clone against it.
**Evidence:** 【一手】 the wheel's own contents (the binary, the shipped
Python SDK, the embedded protobuf descriptors) · 【反编译】 decompiled
output at confirmed Mach-O addresses · 【strings】 string-table hits only ·
【推测】 hypothesis

---

## 0. Summary

`agy` and the `google-antigravity` SDKs do not talk to a model directly.
They spawn a closed-source Go binary and drive it over a wire. That binary
is `localharness`:

| | |
|---|---|
| File | `google/antigravity/bin/localharness` (in the wheel) |
| Size | 120,344,386 B |
| SHA-256 | `991bbea3edc82a9bc276f2965dea564898a494354ebdc4d9f098e30c715c361e` |
| Format | Mach-O arm64, Google-internal Go (`__lrodata` = the renamed `__gopclntab`) |
| Wheel | `google_antigravity-0.1.22-py3-none-macosx_11_0_arm64.whl` |
| pclntab functions | 110,396 |

**The binary owns the agent loop.** The client side owns the hooks, the
policy answers, and the model credentials. That split is the whole point:
it is why the hook protocol is *bidirectional*, why a hook verdict can
block a tool call, and why the budget has to be enforced by the harness
rather than by the caller.

The single most consequential finding of this cycle is not a feature. It
is that **`localharness` embeds `google3/third_party/jetski/cortex`** —
the *same* agent engine the `agy` CLI uses. Two binaries, one monorepo
tree. That is why this repository's `internal/tools` and
`internal/compaction` line up with the reference at all, and it is why
the `localharness` findings transfer to the CLI's surface.

**What this cycle ships**

| | |
|---|---|
| `out/localharness/` | the evidence: 3,109 decompiled functions, 279 package reports, the recovered protocol, the reproduction recipe |
| `internal/lifecyclehooks` | the hook *protocol* — nine types, request/response correlation, per-type verdicts, argument rewriting, fail-closed |
| `internal/budget` | the budget *manager* — five limits, the scope window, subagent accounting, the stop reason |
| `internal/localharness` | the *transport* — the stdio handshake, the websocket, the frame codec, and the generated bindings |
| `docs/guide/localharness.md` | the current-state guide for all three |

---

## 1. The RE run

The repo's existing pipeline applies unchanged: pclntab → seeds → Ghidra →
per-package reports. Two facts were specific to this target:

**The pclntab lives in `__lrodata`.** Google-internal Go renames the
section; `rea-go-seeds` already knows the Mach-O rename, so the seed pass
was uneventful — 110,396 functions in ~12 s.

**The address arithmetic is `Ghidra address = file offset + 0x100000000`**,
because `__TEXT` has vmaddr `0x100000000` and file offset `0`.

```bash
python3 out/localharness/build_priority.py out/localharness/seeds/seeds.txt \
  > out/localharness/priority-funcs.txt
# 3,109 functions across 279 Go packages
```

The Ghidra pass is the verified recipe from
`docs/guide/reverse-engineering.md` with the project's hardcoded paths
turned into script arguments, so one script serves both the `agy` run and
this one:

| | |
|---|---|
| Priority addresses resolved | **3,109/3,109** (0 unresolved) |
| Decompiled | **3,109/3,109** |
| Decompiled LOC | **478,907** |
| Wall time | **65 s** |

`dec/` is gitignored (3,109 raw `.c` dumps); the 279 committed
`decompile-*.md` reports and `decompile-summary.md` are the artefact. The
recipe is in `out/localharness/README.md`.

### What the packages are

【反编译】 — from the pclntab names:

| Package | Role |
|---|---|
| `jetski_prod/localharness/localagent` | the agent loop, hook dispatch, budget, policy |
| `jetski_prod/localharness/interactionstranslate` | proto ↔ internal model; the decision mapping |
| `jetski_prod/localharness/{customtools,skills,geminimodelapi,tools/truncation}` | tools, skills, model API, output truncation |
| `jetski/cortex/core/tools/tools` | the tool implementations (the biggest single block) |
| `jetski/cortex/policyguardian{,v2}` | the policy engine and its vetting |
| `jetski/cortex/permissions` | permissions and grants |
| `jetski/cortex/utils/mcp{,core}` | the MCP manager |
| `jetski/cortex/{trajectory,handlers,mixins,rehydration,shared,messages,command}` | the engine proper |

---

## 2. The protocol — recovered from the wheel, not from the binary

The interesting call in this cycle: **do not dig the schema out of the
binary.** The `google-antigravity` wheel carries
`google/antigravity/proto/*_pb2.py`, and every generated module embeds its
serialized `FileDescriptorProto`. That descriptor is the **authoritative**
schema, shipped by Google, with no inference involved.

The extraction is three small scripts, now committed at
`out/localharness/proto_tools/`:

1. `protodump.py` reads the `AddSerializedFile(b'…')` bytes literal **out
   of the source text** — no import — and prints the `.proto` back.
   Reading the bytes rather than importing the module matters: the
   generated modules import each other by absolute package path, which
   collides with the real `google.protobuf` namespace package.
2. `mkdescriptorset.py` stitches every file into one `FileDescriptorSet`.
3. `mergedescriptorset.py` merges in the well-known types, which the
   wheel's descriptor does not carry.

Result: **seven** `.proto` files, reproducible byte for byte (a re-run
diffs empty against the committed copies).

### The generator's two surprises

- **`protoc-gen-go` v1.36.11 generates the opaque API by default** for
  edition-2023 (proto3) files, and keeps the open API for edition-2022
  (proto2) files. `--go_opt=default_api_level=API_OPEN` **does not work**
  — it is accepted and silently ignored. The committed bindings keep the
  mixed shape, because the descriptor is reproduced as-is rather than
  reshaped for convenience.
- **`protoc-gen-go` is not on `PATH`** in this environment. It is built
  from the module cache:

  ```bash
  cd "$(go env GOMODCACHE)/google.golang.org/protobuf@v1.36.11/cmd/protoc-gen-go"
  GOFLAGS=-mod=mod GOPROXY=off go build -o /tmp/lh/bin/protoc-gen-go .
  ```

The bindings land in `internal/localharness/pb/{localharness,genai,
genaiskills,gaosparsing}` — 33,980 lines.

---

## 3. The transport — two dialects on one pipe

`internal/localharness` implements the client side of the protocol.

```
phase 1 — stdio handshake
    client  --[4-byte LE length][InputConfig protobuf]-->  harness stdin
    client  <--[4-byte LE length][OutputConfig protobuf]--  harness stdout

phase 2 — websocket
    client  --ws://<host>:<port>/ , header x-goog-api-key: <key]--> harness
    client  <--[protojson OutputEvent]--   harness
    client  --[protojson InputEvent]-->    harness
```

【一手】 — the handshake shape, the URL shape, the `x-goog-api-key` header
and the first-frame order come from the wheel's shipped Python SDK's local
connection transport. 【strings】 — the header name is in the harness
binary's own string table (`0x103355c50`).

**The two phases speak different encodings**: the stdio phase is *binary
protobuf*, the websocket phase is *protojson text*. Mixing them up is the
single easiest mistake to make here, so the two paths live in separate
functions — `WriteInputConfig` / `ReadOutputConfig` versus `Conn.Send` /
`Conn.Recv`.

The protojson dialect is the standard one: camelCase, `int64`/`uint64` as
JSON **strings**, `int32`/`uint32` as **numbers**, enums as their name,
`oneof` members flattened into the parent, `bytes` as base64. That is
exactly `protojson`'s default output, so the package uses the standard
library codec rather than a hand-rolled one.

Three things that are easy to get wrong and are pinned by tests:

- **The stdio length prefix is little-endian and exactly four bytes.** A
  big-endian prefix desynchronises the stream permanently — the harness
  reads a garbage length and the handshake never recovers.
- **The first websocket frame is a standalone `InitializeConversationEvent`,
  not wrapped in an `InputEvent`.** `InputEvent`'s `oneof` has no member
  for it, so there is nowhere to put it.
- **Unknown fields must be discarded.** The descriptor came from an *SDK*,
  not from the binary's own schema, so the harness can send fields this
  build has never heard of. `protojson.UnmarshalOptions{DiscardUnknown:
  true}` is not optional.

The retry schedule (5 rounds, each trying `localhost` then `127.0.0.1`,
with a `0.1 * 2^attempt` second backoff) is 【推测】 — inherited from the
SDK rather than observed in the binary. The harness does not police the
client's reconnect policy, so the round count is a client-side choice;
`Options.ConnectRetries` makes it configurable.

---

## 4. The hook protocol — `internal/lifecyclehooks`

This is the part that does not exist in the clone at all.

The hooks are **bidirectional**. The callback lives on the *client* side;
the harness reaches it over the wire:

```
harness --OutputEvent{call_hook_request{request_id,type,name,...}}--> client
client  --InputEvent{call_hook_response{request_id,...}}------------> harness
```

Nine types, in wire-enum order: `ON_SESSION_START`, `ON_SESSION_END`,
`PRE_TURN`, `POST_TURN`, `PRE_TOOL`, `POST_TOOL`, `ON_TOOL_ERROR`,
`ON_COMPACTION`, `STOP`.

> **This is not `internal/hooks`.** That package is a same-process,
> fire-and-forget bus for the TUI's `/btw` side-question — two events, one
> `Decision` type, no request/response correlation. `internal/lifecyclehooks`
> is the *protocol* half, and it is a separate package for that reason.

### The authority question

The binary shows what the **harness** does. The wheel's Python SDK shows
what the **client** must do. The SDK is the half Google actually ships and
runs, so **the SDK is the authority** and the binary is corroboration.
Where they disagree, the package follows the SDK and says so inline.

The three semantics that are easy to get wrong:

**PRE_TOOL merges cumulatively.** Hook *N* sees the arguments merged by
hooks *1..N-1*, and the final response carries the **full** merged map —
not a delta. The SDK uses `dict.update`, so a key can be overwritten but
not removed.

**STOP: the first `CONTINUE` wins.** The SDK docstring says a non-empty
reason is required; the code only checks the decision.

**ON_TOOL_ERROR: the first non-empty custom message wins.** Inspect-style
hooks all run, and their errors do not block.

### The harness side

【反编译】 — at confirmed Mach-O addresses:

| Address | Function | Fact |
|---|---|---|
| `0x101989810` | `localagent.runPreTurnHooks` | walks the pre-turn hooks, returns on the first deny, wraps a failure as `"pre-turn hook error: %w"` |
| `0x1019881c0` | `localagent.preTurnResultFromProto` | `denied = (decision == DENY)`; the reason travels with it |
| `0x101988380` | `localagent.preToolResultFromProto` | same |
| `0x10157b780` | `interactionstranslate.translatePreTurnDecision` | a decision outside `{0,1,2}` is an error: `"unknown PreTurnResult decision: %v"` |
| `0x10157b890` | `interactionstranslate.translatePreToolDecision` | same, `"unknown PreToolResult decision: %v"` |
| `0x101971090` | `localagent.checkCortexHookResults` | the gate that turns a verdict into an outcome |

`checkCortexHookResults` decides on **strings** — `allow`, `deny`, `ask`,
`force_ask`, `auto_approve`, `deny_unapproved_grant` — and when it meets
one it does not recognise:

```
Unrecognized pre-tool hook decision %q; blocked execution for safety.
```

and calls `setStepError`. **The fail-closed rule is therefore pinned by
the reference's own string**, not inferred.

### One deliberate divergence

`preTurnResultFromProto` derives `denied` from `decision == DENY`, so a
`DECISION_UNSPECIFIED` arriving over the wire reads as **"not denied"** on
the harness side. The harness is therefore *more permissive* than this
package, which refuses to emit `DECISION_UNSPECIFIED` in the first place:
a decision the harness has to guess at is exactly what
`checkCortexHookResults` refuses to act on.

This is recorded on both sides — in the package doc and in
`out/localharness/FINDINGS.md` §2.5 — because "the harness is more
permissive here" is a fact a future reader needs, not a footnote.

### The package shape

`internal/lifecyclehooks/lifecyclehooks.go` is a **plain typed model plus
a dispatcher** — it imports no protobuf at all. The pb coupling is
quarantined in `internal/lifecyclehooks/proto.go`, so the dispatcher is
unit-testable without a descriptor, and the pb layer can be swapped without
touching the semantics.

---

## 5. The budget manager — `internal/budget`

```proto
message BudgetConfig {
  uint32 max_model_calls  = 1;
  uint32 max_tool_calls   = 2;
  uint64 max_input_tokens = 3;
  uint64 max_output_tokens = 4;
  uint64 max_total_tokens = 5;
  BudgetScope scope       = 6;   // UNSPECIFIED | LIFETIME | FORWARD_LOOKING
}
```

【反编译】 — `exhaustedLimitStopReason` (`0x101946130`) fixes the
semantics: every limit check is guarded by `0 < limit` (a limit of zero
means "not set"), a limit is exhausted when `limit <= used`, and the usage
snapshot it compares against is laid out as `{model calls, tool calls,
input tokens, output tokens, total tokens}`.

【反编译】 — `stopReasonForUsage` (`0x101962d60`) adds stop reason 6
(`STOP_REASON_QUOTA_EXHAUSTED`) for provider-side quota errors.

【反编译】 — `translateBudgetScope` (`0x101952820`) maps the three scopes
and rejects anything else with `"unsupported budget scope: %v"`.

【反编译】 — `MergeBudgetConfigs` (`0x1019465d0`) merges two configs field
by field using the proto presence bitmap.

【反编译】 — `(*BudgetManager).usage` (`0x101945b80`) resolves the
evaluation window from the scope and subtracts the baseline with a clamp
at zero.

The upstream API surface the package mirrors: `NewBudgetManagerWithProvider`,
`(*BudgetManager).{Config, Baseline, SetBaseline, UpdateConfig,
ResolveEvaluationWindow, SetSubagentLookup, RetireSubagents, allSubagents,
subagentUsage, usage, StopReason, LimitExhausted, HasBudget,
RootTrajectoryID}`, plus the package-level `MergeBudgetConfigs` /
`resolveEvaluationWindow`.

【strings】 — the user-facing text, verbatim:

```
"Input token budget exceeded (limit: %d). Do not emit more tool calls."
"Output token budget exceeded (limit: %d). Do not emit more tool calls."
"Total token budget exceeded (limit: %d). Do not emit more tool calls."
"Tool call budget exceeded (limit: %d). Do not emit more tool calls."
"Model call budget exceeded (limit: %d). Do not emit more tool calls."
"[TokenBudgetHook] Token spend reached budget (input=%d/%d, output=%d/%d, total=%d/%d). Halting turn."
"max_total_tokens must be non-negative, got %d"
"max_input_tokens must be non-negative, got %d"
"max_output_tokens must be non-negative, got %d"
"max_tool_calls must be between 0 and %d, got %d"
"max_model_calls must be between 0 and %d, got %d"
"cannot set both budget_config and top-level max_total_tokens or max_tool_calls"
```

**Subagent spend never counts against the root trajectory's budget.**
`RecordSubagentUsage` touches the subagent accounting only.

---

## 6. The gap audit

Against the clone's current state, the harness has:

| Capability | harness | pigasus | Status |
|---|---|---|---|
| hook protocol | nine types + `request_id` correlation | same-process `/btw` bus only | **absent** → `internal/lifecyclehooks` |
| budget manager | five limits, scope window, subagent accounting | none | **absent** → `internal/budget` |
| harness transport | stdio handshake + websocket | none | **absent** → `internal/localharness` |
| policy | dynamic rules, `server_name`, workspace containment | static only | absent |
| Ask verdict | six-valued | collapsed into `deny` | absent |
| tool output truncation | error strategy + `save_full_output` | truncation only | absent |
| tool search / deferred loading | present | absent | absent |
| skill lookup action | present | absent | absent |
| custom agents | `agent_behavior`, `max_nesting_depth`, `allowed_subagents`, per-agent tools/skills/model | absent | absent |
| compaction threshold | `uint32` token **count** | `float` **ratio** | **not the same quantity** |

The compaction row deserves its own note: the harness's threshold is an
absolute token count and the clone's is a fraction of the window. They are
**not interconvertible** — two independent designs, and the harness gives
no evidence about which is right.

---

## 7. Hard constraints (硬约束)

- **The stdio length prefix is little-endian and exactly four bytes.** A
  big-endian prefix desynchronises the stream permanently.
- **The `x-goog-api-key` header carries the key the harness minted in its
  `OutputConfig`;** the websocket upgrade is refused without it.
- **The first websocket frame is a standalone `InitializeConversationEvent`.**
- **Unknown fields must be discarded** (`DiscardUnknown: true`). The
  descriptor came from an SDK, not from the binary's schema.
- **Frames are not size-limited.** `coder/websocket`'s 32 KiB default read
  limit is disabled explicitly — a single `StepUpdate` can exceed it.
- **A hook verdict the harness cannot interpret must never be treated as
  "allow".** The harness's own string pins this.
- **`request_id` must be echoed verbatim.** The harness correlates
  strictly.
- **The client must never answer a hook it did not enable.** The SDK only
  advertises the hook types it has handlers for
  (`HarnessConfig.enabled_hooks`).
- **PRE_TOOL's response carries the full merged map, not a delta.**
- **A limit of 0 is "not set"** and never triggers a violation, in any
  scope.
- **Subagent spend never counts against the root trajectory's budget.**
- **`internal/localharness` does not depend on `internal/lifecyclehooks`.**
  The transport is a transport; the protocol is a protocol.
- **The `go.mod` change is minimal.** `google.golang.org/protobuf v1.36.11`
  moves from indirect to direct; `go.sum` is unchanged.

## 8. Open questions (存疑)

- **PRE_TOOL merging: overwrite-only, or overwrite-and-delete?** The SDK
  uses `dict.update`, which cannot delete. That is "there is no delete
  interface", not "deleting is forbidden". Nothing settles it.
- **STOP's reason requirement.** The SDK docstring says a non-empty reason
  is needed; the code only checks the decision. The code wins.
- **The harness's `Stop` hook has a second, tool-mediated path** — the
  string *"Call this tool to submit your final structured decision for the
  hook."* — which is not implemented here.
- **The harness's own JSON-hook host** (a `hooks.json` with `command` /
  `prompt` handlers) is not modelled; only the client-side callback
  surface is.
- **The budget check order.** The binary checks output tokens *before*
  input tokens (`0x101946130`, offsets `+0x28` before `+0x20`); the clone
  checks input first. The two differ only when both token limits are
  exhausted at the same instant. The binary's order is the 【反编译】 fact;
  the clone's is the local contract.
- **`MergeBudgetConfigs`: override or minimum?** The binary walks the
  presence bitmap and lets the second config win field by field; the clone
  keeps the *tighter* non-zero limit. No string says which upstream
  intends.
- **The retry schedule** is 【推测】 — inherited from the SDK, not observed
  in the binary.

## 9. Limitations

- **The transport was never run against the real binary.** The handshake is
  transcribed from the SDK; the end-to-end test drives a **stub harness**
  (the test binary re-executing itself), not `localharness`.
- **The descriptor came from a shipped SDK, not from the binary's own
  schema.** Field numbers and types are believed correct, but the recovered
  `.proto` had several scalar types stripped — `BudgetConfig.max_total_tokens`
  is typed `int64` here. A descriptor drift surfaces as a wire mismatch,
  not a compile error, and `DiscardUnknown` hides *extra* fields but not
  *wrong* types.
- **`Conn.Close` kills the child with `SIGKILL`**, mirroring the SDK's
  `process.kill()`. There is no graceful shutdown of the harness.
- **Only the arm64/macOS wheel was sampled.** The Linux wheel was not
  fetched; its addresses and package names may differ.
- **This package speaks no part of the agent protocol** — no hooks, no tool
  results, no policy decisions. It is a transport, nothing more.
- **Every address in the evidence is specific to one SHA-256.** A new wheel
  version needs the whole seeds → priority → Ghidra → reports chain re-run.
