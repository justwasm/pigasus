# The cumulative prompt's config — deliverable

**Cycle:** `cycle-314-cumulative-prompt-config`
**Subject:** the reference's own cumulative-prompt config — its schema, its
default values, the gate's real key, and the two knobs cycle 313 read at the
wrong offsets — closed against the reference `agy` 1.4.7.
**Evidence:** 【一手】 the `exa.prompt_pb` descriptor recovered from the
binary's own bytes and decoded with `protoc --decode`; 【反编译】 the
generated getters' struct layout, the default config's builder, the default
gate's initializer; 【strings】 the three float constants the default config
reads, taken out of the binary's read-only data.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the config's schema** | 【推测】 a shape inferred from two field reads | 【一手】 the whole `exa.prompt_pb` file, recovered: six messages, and the config's 19 fields with their numbers and types |
| **the gate's key** | 【反编译】 a map — "from a Cortex step type" | 【一手】 `map<string, bool>`, keyed by the step type's **name** |
| **the config's defaults** | unknown | 【反编译】 the reference's own default config, value by value — including the trajectory's multipliers (0.65 / 0.9 / 0.5), the 512 reserved intent tokens and `disable_custom_step_id = true` |
| **the gate's default** | 【推测】 empty | 【反编译】 eight step types, every one of them true — the run command among them |
| **the two knobs** | 【反编译】 read at the handler's config +0x68/+0x70 and attributed to the proto's fields 24/25 | 【反编译】 the proto's own fields sit at **+0x88/+0x90**; the handler reads a *derived* config — the same two knobs, a different struct |

The clone's output does not change. The gate's key becomes the reference's
`map<string, bool>` shape, the gate's default becomes the reference's own
list, and two claims in cycle 313's report are corrected.

---

## 1. The reference's shape

### 1.1 The descriptor

【一手】 The binary carries the `FileDescriptorProto` of
`third_party/jetski/prompt_pb/prompt.proto` **uncompressed**, at file
offset `0x377c809`, 3,785 bytes long. Decoded with `protoc --decode` it is
the file the reference's prompt construction is configured by — six
messages in package `exa.prompt_pb`:

| message | what it is |
|---|---|
| `CumulativePromptConfig` | the config the cumulative prompt reads — 19 fields |
| `SystemPromptConstructionConfig` | one field: `element_separator` |
| `CommandPromptCacheConfig` | two fields: `max_tracked_files`, `max_cache_age_seconds` |
| `UnifiedPromptComponents` | what a turn is made of: the context module's result, the cortex trajectory, the command intent, the chat messages, the diagnostics, the knowledge-base groups |
| `CommandIntentInfo` | the command the user asked for — the selection, the instruction, the input document, the selection's line range, the terminal command data, the previous and pending commands, the request source, the intellisense and the diagnostics prompts |
| `PendingCommand` | an instruction and its completion |

The file's own option is `46: "OPEN_TO_OPAQUE_HYBRID"` — the API level that
puts the generated Go struct into the hybrid layout: the scalars keep their
value form, the proto3-optional fields become pointers, the map and the
slice keep their pointer forms.

The recovered artefact is `out/local-macos/protos/prompt_1.4.7.txt` — the
`protoc --decode` dump — with `prompt_1.4.7.proto` beside it, the
rendering, produced mechanically by `devutils/render_proto.py`.

### 1.2 The config's schema

【一手】 `CumulativePromptConfig`, in the file's declaration order, with the
reference's default value for each (the defaults are §1.4):

| # | field | type | default |
|---:|---|---|---|
| 2 | `trajectory_context_multiplier` | float | **0.65** |
| 6 | `intent_reservation_tokens` | int64 | 512 |
| 7 | `persistent_context_multiplier` | float | 0.25 |
| 9 | `ephemeral_context_multiplier` | float | **0.1** |
| 10 | `persistent_active_document_multiplier` | float | 0.5 |
| 11 | `persistent_open_docs_multiplier` | float | 0.5 |
| 12 | `persistent_max_ccis_considered` | int64 | 0 |
| 13 | `ephemeral_active_document_multiplier` | float | 0.5 |
| 14 | `ephemeral_max_ccis_considered` | int64 | 0 |
| 15 | `trajectory_refresh_threshold_multiplier` | float | **0.9** |
| 16 | `trajectory_truncation_multiplier` | float | 0.5 |
| 17 | `persistent_max_tokens_per_open_doc` | int64 | 1024 |
| 18 | `allowed_cascade_step_types` | repeated `exa.cortex_pb.CortexStepType` | empty |
| 20 | `persistent_document_suffix_frac` | float | 0.25 |
| 21 | `ephemeral_document_suffix_frac` | float | 0.25 |
| 22 | `allowed_implicit_step_types` | `map<string, bool>` | eight entries, all true |
| 23 | `disable_custom_step_id` | bool | **true** |
| 24 | `trajectory_start_index` | `optional int32`, in the oneof `_trajectory_start_index` | unset |
| 25 | `trajectory_index_increment` | int32 | 0 |

Field numbers 1, 3–5, 8 and 19 are reserved, and so are the names
`active_document_context_multiplier`, `context_item_context_multiplier`,
`cache_context_multiplier` and `max_ccis_considered` — fields the config
used to carry and that a newer server may not reintroduce.

The declaration order is not the field-number order: the config was
extended over time and the numbers stayed where they were.

### 1.3 The Go struct layout

【反编译】 the generated getters pin every field's offset in the Go struct
— `prompt_go_proto.(*CumulativePromptConfig).Get*`, `0x100978850`–`0x100978aa0`:

| offset | field | Go form |
|---|---|---|
| +0x08 | `persistent_context_multiplier` | float32 |
| +0x0c | `persistent_active_document_multiplier` | float32 |
| +0x10 | `persistent_open_docs_multiplier` | float32 |
| +0x18 | `persistent_max_tokens_per_open_doc` | int64 |
| +0x20 | `persistent_max_ccis_considered` | int64 |
| +0x28 | `persistent_document_suffix_frac` | float32 |
| +0x2c | `trajectory_context_multiplier` | float32 |
| +0x30 | `trajectory_refresh_threshold_multiplier` | float32 |
| +0x34 | `trajectory_truncation_multiplier` | float32 |
| +0x38 | `ephemeral_context_multiplier` | float32 |
| +0x40 | `intent_reservation_tokens` | int64 |
| +0x48 | `ephemeral_active_document_multiplier` | float32 |
| +0x50 | `ephemeral_max_ccis_considered` | int64 |
| +0x58 | `ephemeral_document_suffix_frac` | float32 |
| +0x60 | `allowed_cascade_step_types` | a slice — data at +0x60, **len at +0x68, cap at +0x70** |
| +0x78 | `allowed_implicit_step_types` | a map |
| +0x80 | `disable_custom_step_id` | bool |
| +0x88 | `trajectory_start_index` | `*int32` |
| +0x90 | `trajectory_index_increment` | int32 |

### 1.4 The default config

【反编译】 `prompt.ReadCumulativePromptConfigFromUnleash` (`0x101b16a60`)
builds the config the reference uses when the server-side flag is absent:
it allocates the message (`runtime.newobject`, type descriptor
`0x1097e56e8`, 184 bytes) and writes every field. Three of the multipliers
are not literal constants but are read out of the binary's read-only data:

| address | bytes | float |
|---|---|---|
| `0x103da3670` | `cdcccc3d` | 0.1 |
| `0x103da3674` | `6666263f` | 0.65 |
| `0x103da3678` | `6666663f` | 0.9 |

So the reference's default config is:

- the multipliers of §1.2's table;
- `allowed_cascade_step_types` — a copy of the compile-time slice at
  `0x10b50d0a0`, whose **len and cap are both zero**: by default **no**
  cascade step type may contribute to the cumulative prompt;
- `allowed_implicit_step_types` — the map built at init time (§1.5);
- `disable_custom_step_id` — **true**;
- `trajectory_start_index` — unset (nil at the proto's +0x88);
- `trajectory_index_increment` — 0 (the proto's +0x90).

The builder's write order is the declaration order of §1.2, and every
offset it writes matches the getter of §1.3 — the message it builds is the
proto's own struct.

### 1.5 The default gate

【反编译】 `prompt.map.init.0` (`0x101b13d80`) is the initializer of the
map the default config carries: eight `mapassign_faststr` calls, each
followed by a store of `true`, and the map is written to the global
`0x10b5f09d8` — the global the default-config builder reads into the
config's field 22. The eight keys are the step types' enum names:

```
CORTEX_STEP_TYPE_CODE_ACTION
CORTEX_STEP_TYPE_GIT_COMMIT
CORTEX_STEP_TYPE_GREP_SEARCH
CORTEX_STEP_TYPE_VIEW_FILE
CORTEX_STEP_TYPE_RUN_COMMAND
CORTEX_STEP_TYPE_FIND
CORTEX_STEP_TYPE_COMMAND_STATUS
CORTEX_STEP_TYPE_CLIPBOARD
```

**The run command is one of them.** So the reference's own default lets a
`!` command reach the prompt. The clone's default gate was right to let it
through — but for the wrong reason: it was the clone's own guess (one
type) rather than the reference's list (eight).

### 1.6 The two knobs, and where cycle 313 read them

【反编译】 cycle 313 found the handler's renumbering and read two fields
out of the config it holds:

```
ldr  x8, [x7, #0x68]     ; a pointer — nil means "unset"
ldr  x2, [x8]            ; *start index
...
ldr  x1, [x7, #0x70]     ; the increment
```

Those offsets are **not** the proto's. The proto's own layout (§1.3) puts
`trajectory_start_index` at +0x88 and `trajectory_index_increment` at
+0x90 — and its +0x68/+0x70 are the len and the cap of the
`allowed_cascade_step_types` slice, which a length cannot be dereferenced
through. The handler's config — the one it holds at handler+0x248 — is a
**derived** struct that puts the two knobs at +0x68 and +0x70. The
semantics match exactly (an optional int32 that is nil-tested, and a plain
int32 that is added), so these are the same two knobs in a different
struct, not the proto itself.

Two globals were cited with the handler's reads: `0x10b50d0a8` and
`0x10b50d0b0`. They are **not** the two knobs either — they are the len and
the cap of the compile-time `allowed_cascade_step_types` slice at
`0x10b50d0a0`, and both are zero. The default config's two knobs are
zeroed explicitly, at the proto's +0x88 (nil) and +0x90 (zero), so the
conclusion cycle 313 drew — *with the default config, the id a step gets is
its own index* — stands; only the mechanism was wrong.

### 1.7 The converter's own config

【一手】 the two knobs cycle 313's clone models by name — the per-step token
limit and the context role — are **not** fields of the prompt config. They
belong to the converter's own config, `exa.cortex_pb.TrajectoryConversionConfig`
(recovered in `cortex_1.4.6.proto`): `enable_context_role` is field 12 and
`max_tokens_per_step` is field 13. Both are proto3-optional there, so both
are pointers in the Go struct — `enable_context_role` at +0x48 and
`max_tokens_per_step` at +0x50.

---

## 2. The clone's shape

### 2.1 What changed

- **The gate is keyed by name.** `Options.AllowedStepTypes` is now
  `map[string]bool` — the reference's own shape (`map<string, bool>`,
  field 22) — keyed by the step type's enum name.
- **The default gate is the reference's.** The clone's default is now the
  eight step types the reference's default config carries, all true.
- **The evidence comments are corrected.** The step id's disassembly block
  now says which struct the offsets belong to; the claim that the default
  config reads two globals for the two knobs is gone; the per-step limit and
  the context role are attributed to
  `exa.cortex_pb.TrajectoryConversionConfig`; and the claim that the
  reference's default gate is empty is gone.

### 2.2 What did not change

- The messages the clone builds — the header, the lead-ins, the input
  string, the step string (cycles 312 and 313).
- The step-id formula (cycle 313).
- The clone still converts one step type, and the trajectory is still never
  cut (§5).

---

## 3. 硬约束 / the load-bearing rules

1. **The gate is `map<string, bool>`, keyed by the step type's name.** A
   numeric key is not the reference's shape, and the config that would
   arrive from the server carries names.
2. **The default gate is the reference's eight step types.** Not empty, not
   one type. The run command is among them.
3. **The default config allows no cascade step type.**
   `allowed_cascade_step_types` is empty by default — so a conversation step
   does not join the cumulative prompt unless the server says so.
4. **The default config's `disable_custom_step_id` is true.**
5. **The trajectory's budget multipliers are 0.65 (context), 0.9 (refresh
   threshold) and 0.5 (truncation), with 512 tokens reserved for the
   intent.** These are the numbers the trajectory-level cut needs; the
   formula that uses them is still unknown.
6. **Cite the right offsets for the right struct.** The proto's two knobs
   are at +0x88 and +0x90; the handler's derived config holds them at
   +0x68 and +0x70.
7. **The per-step token limit and the context role are not prompt-config
   fields.** They live in `exa.cortex_pb.TrajectoryConversionConfig`.

---

## 4. 存疑 / open questions

- **【存疑】 The handler's config type.** The derived struct the handler
  holds at +0x248 is not the proto: `prompt.GetCascadeConfigForCumulativePrompt`
  (`0x101b16ba0`) builds a *linked list of converter entries*, not a
  config, and the store into handler+0x248 is not in the handler's own
  methods — so the type is set from outside the package. Two candidate type
  descriptors were found (`0x1097e56e8`, 184 B, the proto; `0x1097ead68`,
  200 B), and neither could be tied to the handler's struct. The
  *semantics* of the two fields the handler reads are not in doubt; only the
  type's identity is open.
- **【存疑】 What `disable_custom_step_id` switches off.** The field is
  true in the default config, yet the reference's handler renumbers the
  step id unconditionally on the path cycle 313 read. Whether the flag
  gates a *different* id (the "custom" one), or gates it at a different
  call site, was not traced.
- **【存疑】 The cascade list.** The default is empty; the *server's* list —
  and therefore which step types join the cumulative prompt as conversation
  steps — is still unknown. Cycle 313's open question, now with the
  default's emptiness pinned.
- **【推测】 The trajectory-level cut's budget.** The multipliers are known
  now, but the formula, the token counter and the context window are not.

---

## 5. Limitations

- **The trajectory is still never cut.** The reference's
  `TrajectoryToChatMessagesToTokenLimitOrStepIndex` drops the oldest steps
  when the running token total passes the limit and inserts a checkpoint
  step; the clone converts the whole trajectory, every time. The budget's
  multipliers are now known (§1.2), but the formula that combines them
  with the model's context window and the model's reported prefix tokens
  is not.
- **The clone does not read a server config.** The reference's
  `allowed_implicit_step_types` arrives with the config; the clone's gate
  is its own default — the reference's default, but still its own.
- **The gate's other seven entries never fire here.** The clone's store
  holds run commands only.
- **The clone converts one step type.** Its store holds run commands only.
- **The token count is an estimate.** The reference counts tokens with its
  own counter; the clone uses `compaction.EstimateTokens` — the reference's
  own three-bytes-per-token divisor, but a divisor, not a tokenizer.

---

## 6. The evidence

| artefact | what it is |
|---|---|
| `out/local-macos/protos/prompt_1.4.7.txt` | the `protoc --decode` dump of the `exa.prompt_pb` descriptor — the evidence |
| `out/local-macos/protos/prompt_1.4.7.proto` | the proto3 rendering of that dump |
| `out/local-macos/protos/README.md` | the index, the target binary's sha256, the config's schema |
| `devutils/render_proto.py` | the renderer: a descriptor text dump → proto3 source |
| `internal/cumulativeprompt/cumulativeprompt.go` | the converter: the name-keyed gate, the reference's default gate, the corrected evidence comments |
| `internal/cumulativeprompt/cumulativeprompt_test.go` | the gate's rule, the default gate's eight names |

### The binary

Reference `agy` 1.4.7, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`.

| what | where |
|---|---|
| the `prompt.proto` descriptor | file offset `0x377c809`, 3,785 B |
| the config's getters | `prompt_go_proto.(*CumulativePromptConfig).Get*` `0x100978850`–`0x100978aa0` |
| the default config | `prompt.ReadCumulativePromptConfigFromUnleash` `0x101b16a60` |
| the default gate's initializer | `prompt.map.init.0` `0x101b13d80` |
| the prompt package's init | `prompt.init` `0x101b13c60` |
| the cascade list's builder | `prompt.GetCascadeConfigForCumulativePrompt` `0x101b16ba0` |
| the handler's renumbering | `cumulative_prompt_handler` `0x102236488`–`0x1022364ac` |
| the gate's rule | `prompt.shouldFilterTrajectoryStep` `0x101b16250` |
| the float constants | `0x103da3670` / `0x103da3674` / `0x103da3678` |
| the cascade slice's globals | `0x10b50d0a0` (data), `0x10b50d0a8` (len), `0x10b50d0b0` (cap) |
| the default gate's global | `0x10b5f09d8` |

### The extraction

The descriptor is a plain `FileDescriptorProto` in `__TEXT,__const`: it
starts with protobuf field 1 (`0x0a`, length, the file name) and ends with
field 12 (`0x62`, `syntax: "proto3"`). Walk the top-level fields from the
start offset to the end marker, then decode:

```
protoc --decode=google.protobuf.FileDescriptorProto google/protobuf/descriptor.proto \
  < prompt_1.4.7.bin > prompt_1.4.7.txt
python3 devutils/render_proto.py out/local-macos/protos/prompt_1.4.7.txt
```

The renderer parses the dump as the text format it is — no binary parsing
and no guessing — so the rendering can be checked against the dump field by
field. Run against `trajectory_1.4.6.txt` it reproduces that file's content
(the committed `.proto` is a hand-made reading aid: it flattens the `oneof`
and drops the reserved ranges).

### The Mach-O layout

`otool -l` prints `vmaddr` and `vmsize` in hex and `fileoff` and `filesize`
in **decimal** — a file offset parsed as hex lands far past the end of the
file. The segments of the 1.4.7 binary:

| segment | vmaddr | fileoff |
|---|---|---|
| `__TEXT` | `0x100000000` | 0 |
| `__DATA_CONST` | `0x108204000` | `0x8204000` |
| `__DATA` | `0x109904000` | `0x9904000` |
| `__LINKEDIT` | `0x10d8a4000` | `0xb5f0000` |

A virtual address in `__DATA` maps to `fileoff + (va − vmaddr)`; the
globals of §1.4 and §1.5 are all in `__DATA`.
