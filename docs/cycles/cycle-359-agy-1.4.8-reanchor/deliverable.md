# Cycle 359 deliverable: re-anchoring the clone's citations on agy 1.4.8

**Cycle:** `cycle-359-agy-1.4.8-reanchor`
**Subject:** cycle 350 established that the reference moved to **agy 1.4.8** and
what changed on the wire. It did **not** re-anchor the addresses the clone's
sources cite. Every `0x10…` in this tree still pointed into the **1.4.7**
binary, and every address moves on every release. This cycle re-anchors them
— function entries, rodata literals, data globals and code ranges — against
the 1.4.8 binary, and re-verifies the two fixtures that hold the reference's
own text.
**Evidence:** 【一手】 first-hand against the 1.4.8 binary's bytes and seed
set · 【反编译】 decompiled output at confirmed Mach-O addresses · 【strings】
string-table hits only · 【推测】 hypothesis

---

## 0. The short answer

The clone's *behaviour* did not change. Its *citations* did.

| | 1.4.7 citation | 1.4.8 citation |
|---|---|---|
| distinct addresses cited in the clone's Go sources | 175 | 175, **all re-anchored** |
| function entries re-anchored by name lookup | — | 46 |
| rodata literals re-anchored by byte match | — | 69 |
| data globals re-read out of the 1.4.8 data section | — | 3 (tool-name slice, scheme-name headers, cascade-step set) |
| tool descriptions | 37 texts | **the same 37 texts, byte for byte** |
| tool names | 63 | **the same 63** |
| the overflow message | 438 B | **the same 438 B** |

The one *substantive* finding is a package move: in 1.4.8 the CLI's own tool
package moved from `cortex/core/tools/tools` to
`oneharness/internal/tools/tools`. The second is a schema: the runtime
`manage_subagents` action enum is **three** values on the CLI side and
**four** on the harness side, and the two are not the same declaration.

---

## 1. Three address spaces, and only one of them moved

The workspace holds three Go binaries, and they do **not** share an address
space. Mixing them is the single easiest way to make a citation wrong while
still looking right.

| binary | seed set | moved since 1.4.7? |
|---|---|---|
| the **agy CLI** (`~/.local/bin/agy`, 1.4.8) | 166 181 names | **yes — every address** |
| the **IDE language server** (the same monorepo commit) | 164 772 names | **no — byte-identical to the committed set** |
| the **localharness** backend (the PyPI SDKs' agent) | 110 396 names | its own space, never mixed |

Consequences, both of which this cycle got wrong once and then corrected:

- Every `LS 0x…` citation in the tree is **still correct** and was left
  untouched. The IDE's seed set was regenerated and compared byte for byte
  against the committed one: identical.
- `localharness/transport.go` cites the harness binary's own string table.
  A blanket remap rewrote that citation into the **CLI's** address space —
  the address resolved, the *meaning* did not. It was reverted to
  `0x103355c50` (the harness's) with the space named in the comment.

## 2. How an address was re-anchored

Arithmetic does not work: the 1.4.7 → 1.4.8 delta is neither a constant nor a
function of the address.

**Function entries — by name.** The 1.4.7 seed set says which function owns
a cited address; the 1.4.8 seed set says where that function now lives. A
name is not a unique key in a Go binary (a method is emitted once per
receiver shape), so the name lookup was **confirmed by the surrounding
bytes** and the pair was then read back against the 1.4.8 seed set.

**Rodata literals — by bytes.** A cited literal address was resolved to its
bytes in the 1.4.7 binary, and those bytes were located in the 1.4.8
binary. Where the same bytes appear more than once, the occurrence nearest
the old address was taken and the bytes were read back to confirm.

**Data globals — by reading the section.** The tool-name slice's header
(`0x10b5ba450` → backing `0x10b623bb0`, length 63), the eight colour-scheme
name headers (`0x1082ab820 + 0x10·k`) and the cascade-step set's length and
capacity words were read out of the 1.4.8 binary's data section.

**Code ranges — by instruction.** The cumulative-prompt handler's
renumbering block moved from `0x102236488–0x1022364ac` to
`0x10225b3b8–0x10225b3dc`; the two ranges were disassembled and compared
instruction for instruction — identical.

## 3. What did *not* move

Every text the clone quotes from the reference was re-checked against the
1.4.8 binary and is **unchanged byte for byte**:

- **the 37 tool descriptions** — each one reconstructs, chunk by chunk, from
  1.4.8 literals with no gap. The converter-layer texts live in the binary
  as `fmt.Sprintf` templates (`%[1]d`, `%d`); the fixture holds their
  rendered form, and the rendering is what the reference sends.
- **the 63 tool names** — the 1.4.8 slice holds exactly the same 63 strings
  as `testdata/tool_names_1.4.7.txt`, set-equal, in the same order.
- **the overflow message** — the same 438 bytes at file offset `0x36c73e2`
  (vmaddr `0x1036c73e2`).
- **the reference's result-text templates** — `"Showing lines %d-%d of %d"`
  at `0x10357c40a`, `"File is empty"` at `0x10349d3cc`, `"Created At: %s\n"`
  at `0x1034b540d`, `"Completed At: %s\n"` at `0x1034ddd83`, `"Step Id: %d\n"`
  at `0x10347022b`.

Only the *addresses* moved. Every fixture kept its file name (the name
records the capture, not the re-anchor) and its `captured_at` was moved to
`1.4.8` only where the fixture's own contents were re-verified against the
1.4.8 binary.

## 4. The finding: the CLI's tool package moved

【反编译】 In 1.4.8 the CLI's own tool types live in
`google3/third_party/oneharness/internal/tools/tools`:

| type | 1.4.8 entry |
|---|---|
| `(*ManageSubagentsTool).Description` | `0x101dedb70` |
| `(*RunCommandTool).ParameterSchema` | `0x101dfa2c0` |
| `(*RunCommandTool).withTimeoutNotificationField` | `0x101dfa4b0` |
| `(*ManageSubagentsTool).list` | `0x101deeef0` |
| `(*ManageSubagentsTool).kill` | `0x101def5f0` |
| `resolveBrowserSubagentBackend` | `0x101dec530` |

`google3/third_party/jetski/cortex/core/tools/tools` **still exists** in
1.4.8, but it no longer holds the CLI's tool types: it keeps the resource
tools and the `ask_question` / `finish` / `run_workflow` declarations
(`(*AskQuestionTool).Description` at `0x101e0d1b0`,
`(*FinishTool).Description` at `0x101e0e4b0`,
`(*RunWorkflowTool).Description` at `0x101e106b0`).

## 5. The finding: the runtime `manage_subagents` enum is three values

【反编译】 The CLI-side field's own tag reads, verbatim:

```
jsonschema:"required,enum=list,enum=kill,enum=kill_all"
```

at file offset `0x9404b5a` of the 1.4.8 binary. The action's description is
the two kill clauses only:

```
jsonschema_description:"'kill' terminates the subagents named in ConversationIds; 'kill_all' terminates all."
```

and the handler's own error text agrees:

```
manage_subagents: unknown action %q (must be 'list', 'kill', or 'kill_all')
```

at vmaddr `0x103670e3f`.

The **harness** side of the same binary is a different declaration: at file
offset `0x4f35ddc` it reads
`enum=list,enum=list_peers,enum=kill,enum=kill_all`, with a four-clause
description. The clone ships the **CLI's** three-value schema and keeps a
`list_peers` *executor* — the reference's own handler knows that fourth
action even though its schema never advertises it. PiG validates `required`
and the types, **not** the enum, so the extra executor stays reachable.

## 6. Two citations that could not be carried forward

A Go **type descriptor** (`abi.Type` pointer) cannot be resolved out of a
stripped binary: the `namedata` layout it points at is not recoverable
statically. Two citations used one, and neither was carried forward as a
guess:

| the citation said | replaced with |
|---|---|
| `types.SchemeColors` (type descriptor `0x1097d1f30`) | `types.init` at `0x102699fa0` — the function that fills the eight-entry scheme map — plus the eight name headers at `0x1082ab820 + 0x10·k` |
| `chat_go_proto.ChatMessagePrompt` (type descriptor `0x109804f68`) | the 408-byte size, the `source` field's offset, and `prompt.ConstructChatMessagesForChatCompletion` at `0x101b2b7b0` |

## 7. Verification

- **Every address in the tree resolves.** Across `docs/guide/`,
  `internal/`, `localharness/`, `palette/`, `lifecyclehooks/` and
  `out/local-macos/protos/`, every `0x1…` address is either a 1.4.8 seed
  entry or a valid offset inside the 1.4.8 binary. **Zero exceptions** (the
  single regex hit is the EBML element id `0x18538067` in
  `internal/tools/webm.go`, which is not an address).
- **Every name↔address pair agrees.** 84 pairs of the form "`name` at
  `0x…`" were checked against the 1.4.8 seed set: **0 mismatches**.
- **The 63 names match the binary's slice exactly** — set-equal, same order.
- **The 37 descriptions reconstruct** from 1.4.8 literals with no gap.
- `go build ./cmd/... ./internal/...` — clean.
- `go test ./cmd/... ./internal/...` — green.

## 8. The shared tree

This cycle's work landed in 54 files, some of which another agent also
touches. The re-anchor was therefore **file-scoped**: each file's diff was
classified as address-only (this cycle) or not (the other agent's), and
only the address-only files were staged. One file
(`internal/cliapp/tui_active_items.go`) carries *both* — the other agent's
staged import reorder and this cycle's address fix in different hunks — and
was left for the other agent's commit to pick up, so that no commit of this
cycle contains another agent's work.

---

## 硬约束 — hard constraints

1. **Three address spaces, never mixed.** The agy CLI, the IDE language
   server and the localharness backend each have their own. An address that
   resolves is not an address that *means* what the comment says.
2. **A function address is re-anchored by name, not by arithmetic.** A name
   is not unique in a Go binary; the lookup is confirmed by the surrounding
   bytes and read back against the 1.4.8 seed set.
3. **A literal address is re-anchored by bytes.** The 1.4.7 bytes must be
   found in the 1.4.8 binary, not the address nudged.
4. **A type descriptor address is not citable.** If the `abi.Type` pointer
   cannot be resolved statically, the citation is replaced with evidence
   that can be — never with the old address carried forward.
5. **The `manage_subagents` runtime schema is three values** (`list`,
   `kill`, `kill_all`) on the CLI side. The four-value variant is the
   harness's. The clone ships the CLI's and keeps `list_peers` reachable as
   an executor only.
6. **The 1.4.8 CLI's tool package is `oneharness/internal/tools/tools`.**
   `cortex/core/tools/tools` still exists and still holds the resource
   tools and three declarations.
7. **A fixture's `captured_at` moves only when its contents were re-verified
   against the new binary.** A live capture that could not be re-taken keeps
   its old stamp and says so in the lock's `how`.

## 存疑 — open questions

1. **The live captures could not be re-recorded.** `result_texts_1.4.7.txt`,
   `agent_decls_1.4.7.json`, `capture_1.4.6.json`, `user_rules_1.4.5.txt`
   and `reference_step_1.4.7.pb` all need a live session, and there is none
   on this machine (the reference is not logged in). They were re-checked
   *statically* — the reference-authored texts are unchanged in 1.4.8 — but
   the captures' own metadata (ids, paths, timestamps) stays at the 1.4.7
   recording. The lock records this per artefact.
2. **The seed *set* is still committed at 1.4.7.** The 166 181-name 1.4.8
   set exists (only the name-level diff is committed), but
   `out/local-macos/seeds/seeds.txt`, `out/local-macos/dec/` (2 366 files)
   and `priority-funcs.txt` are still the 1.4.7 artefacts. Rebuilding them
   is a separate cycle and must rebuild the Ghidra project with them.
3. **The `run_command` converter template's two prose companions were
   matched by bytes, not by name.** The template itself is at file offset
   `0x36cb4c0` (vmaddr `0x1036cb4c0`, 486 B, four `%s` placeholders); the
   first paragraph is three contiguous literals starting at `0x1036d0bd3`
   and the second is one literal at `0x1036b6def` (261 B). Which literal
   is which was confirmed by the decompile, not by a name. The four parts
   reconstruct the fixture's 1 314-byte text **exactly**.
4. **`cortex/core/tools/tools` may still hold more than the three
   declarations found.** The 1.4.8 seed set was searched for
   `Description`/`ParameterSchema` methods in that package and three were
   found; a tool that carries its text in a *field* rather than a method
   would not be found that way.

## Limitations

- **The audit is static.** Nothing here re-ran the reference. The 1.4.8
  binary's bytes and its seed set are the only inputs.
- **The 37 descriptions were verified by *reconstruction*, not by
  re-capture.** Each text was rebuilt from the 1.4.8 literals it is made
  of; that the reference's *renderer* still produces that text is a
  conclusion, not an observation.
- **The rodata byte match can be ambiguous** where the same byte sequence
  appears more than once in a 20 MB binary. Every such pair was read back
  and checked against the decompile, but the search is nearest-neighbour,
  not unique.
- **The Ghidra project was not rebuilt for 1.4.8.** The decompilations this
  cycle cites come from a 1.4.8 project built from the 1.4.8 seeds, but
  the committed `out/local-macos/dec/` tree is still 1.4.7 and its report
  filenames encode 1.4.7 addresses.
- **`docs/guide/tools.md` still carries 1.4.7 *capture* references** where
  they name a recording event (the 2026-10-22 request bodies, the result
  texts). Those are historical statements about a capture, not claims about
  the current binary, and were left alone deliberately.
- **This cycle changed citations, not behaviour.** The clone sends the same
  bytes it sent before; only the comments, the fixtures' provenance fields
  and the lock's `how` entries moved.

---

## Files

| | |
|---|---|
| `internal/tools/testdata/tool_descriptions_1.4.7.json` | `captured_at` → 1.4.8, `source` rewritten, the browser-agent variant addresses re-anchored |
| `internal/tools/testdata/tool_variants_1.4.7.json` | every address re-anchored, `captured_at` → 1.4.8 |
| `internal/referencedrift/reference.lock.json` | the registry: two `captured_at` moves, four `how` notes, the seed note, the lag note, two refreshed digests |
| `internal/tools/{catalog,descriptions,variants,notification,websearch,browser_subagent,command,mcp,model_surface,tools_tasks}.go` | the 1.4.7 → 1.4.8 citations and the package move |
| `internal/subagenttools/manage.go` | the three-value enum and the 1.4.8 package path |
| `internal/cumulativeprompt/cumulativeprompt.go` | the renumbering range and the type-descriptor replacement |
| `internal/modelctx/{overflow,validate}.go` | the overflow literal's 1.4.8 home and the string-table claim |
| `internal/keybind/keybind.go` | the 1.4.8 evidence for `~/.gemini/config/keybindings.json` |
| `palette/scheme.go` | the type-descriptor replacement |
| `localharness/transport.go` | the citation put back into the harness's address space |
| `docs/guide/tools.md` | the 1.4.8 offsets, the variant table, the blob address |
| `docs/guide/reference-drift.md` | *Re-anchoring the citations without rebuilding the pipeline* |
| `internal/{browsersubagent,builtinsubagent,rehydration,resumetree,taskregistry,cliapp,printmode}/*.go` | the address-only re-anchor |
