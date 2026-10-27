# The reference's own tool descriptions — deliverable

**Cycle:** `cycle-305-tool-descriptions`
**Subject:** the reference carries **three** tool surfaces, and only the third
one has anything to say. The clone's catalogue held a hand-written paraphrase
for every advertised tool; of the sixty names it carried, exactly **one** —
`view_file` — matched the reference's text, and the other fifty-nine read like
captures without being ones. This cycle replaces them with the reference's own
bytes, fixes a stray newline in `run_command`'s prose, and records what the
reference's binary knows about tool names (63) against what a live session
advertises (60).
**Evidence:** 【一手】 the binary's own bytes — `~/.local/bin/agy` (agy
**1.4.7**, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`) — read
directly out of the Mach-O's rodata and data segments; 【反编译】 the converter
and runtime layers, from the binary's symbol names and its own literals; 【strings】
the prose itself, which is string-literal text in the binary.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the catalogue's descriptions** | 60 hand-written paraphrases (the clone's own prose) | the reference's own text for the **36** names it describes; the clone's prose for the other 24, marked as the clone's |
| **`run_command`'s prose** | the reference's converter text **plus one stray `\n`** | byte-exact with the reference's template rendering |
| **the reference's known tool names** | not recorded | **63**, read out of the binary's `[]string` global |
| **what the reference advertises** | 60 (the live capture) | 60 — unchanged, and now *explained*: the binary knows 63 and withholds the three searches |
| **`manage_task`'s kill** | returned before the task's log was written | waits for the task to be reaped, so the log file is written before the tool returns |

## 1. The three surfaces

【反编译】 agy 1.4.7 has three distinct places where a tool's *name* and its
*description* live, and they answer different questions:

| surface | where | what it carries | what reaches a model |
|---|---|---|---|
| the advertised list | `printmode.buildInitPayload` → `advertisedTools` → `sort.Strings` | **names only** | nothing — the headless client is told what is available |
| the converter layer | `cortex/tools/*`, `gemini_coder/framework/toolconverters` | the prose that assembles a tool *definition*, plus the schemas | the schema half, for the tools the wire declares |
| the runtime layer | `cortex/core/tools/tools`, `oneharness/internal/tools/tools` | the `Description()` methods | **the twelve** the reference sends as function declarations |

So the reference's *model-visible* prose is the runtime layer's, and the
clone already ships all twelve of those **byte-identical** in
`internal/tools/model_tools_1.4.6.json`. The catalogue's prose is a fourth
thing — the clone's own.

## 2. What the catalogue actually held

The catalogue's sixty descriptions were written by hand in earlier cycles.
Comparing them against the reference's own text (the fixture this cycle
commits):

| verdict | count | names |
|---|---|---|
| **exact** | 1 | `view_file` |
| **differs** | 35 | everything else the reference describes |
| **no reference text anywhere in the 1.4.7 binary** | 24 | the twenty-three browser tools and `delete_knowledge` |

The twenty-four "no text" names are not an extraction failure: the 1.4.7
binary describes them **nowhere** — not in the converter layer, not in the
runtime layer, not as a string literal. The browser surface has no prose at
all. For those, the clone keeps its own sentences and now says so.

`run_workflow` is a third case: its description is assembled from a list of
tool names, and only fragments (`"Ru,  Scripts can call these tools with
call_tool: %s."`) were recovered. It keeps the clone's prose, and the fixture
marks it `"complete": false`.

## 3. The byte-level win: `run_command`

The reference's converter layer builds `run_command`'s prose from a **template
literal** at file offset `0x369440e` (486 bytes) and two more literals:

```
0x10369440e / 486   the template: "PROPOSE a command to run on behalf of the
                    user. Operating System: %s. Shell: %s.\n…%s\nCommands will
                    be run with PAGER=cat. … (e.g. git log, use git log -n
                    <N>).%s"
0x10369990c / 560   the first part (the background-task paragraph, with its
                    own %s for the tool name)
0x103680610 / 261   the second part (the Cwd paragraph)
```

Rendered with the host's operating system, the host's shell and
`manage_task`, that is **1 314 bytes**. The clone's `runCommandDescription()`
was **1 315**: byte-identical except for one extra `\n`, between
`…(e.g. git log, use git log -n <N>).` and `IMPORTANT: The Cwd (working
directory) MUST be within the user's workspace.` The reference has no newline
there — the template's `%s` for the second part follows the sentence directly.

The clone now renders the reference's template byte for byte, and the
catalogue's `run_command` description is pinned against the fixture.

## 4. The names: 63 known, 60 advertised

【一手】 the reference's binary holds a `[]string` of **63** tool names: the
slice header is at vmaddr `0x10b517430`, its backing array at `0x10b580540`,
and the length is 63. The three names it has that the live 1.4.3 capture does
not are `code_search`, `moma_search` and `skill_search`.

A live 1.4.7 session's `init.tools` holds **60** names, byte-identical to the
1.4.3 capture (`docs/cycles/reference-drift-147/`). The difference is exactly
those three: the reference *knows* them and *withholds* them — they need a
service the CLI does not have (the IDE's own build carries a
`cortex/core/contrib/variants/no_code_search` variant, which is the same
switch seen from the other side).

**So the clone keeps advertising 60.** A live capture is the ground truth for
what the reference prints; the static list is the ground truth for what it
knows. Both are now committed — `advertised_tools_1.4.3.txt` (the live list)
and `tool_names_1.4.7.txt` (the binary's list) — and
`TestAdvertisedListIsTheLiveSubset` pins the difference.

## 5. The cross-check that validated the extraction

The reference's model-facing texts and the clone's shipped declaration
fixture are independent artefacts. Eight of the twelve declared tools have a
model-facing text in the new fixture; comparing them against
`model_tools_1.4.6.json`:

| declaration | 1.4.7 text vs the clone's 1.4.6 fixture |
|---|---|
| `ask_question`, `define_subagent`, `invoke_subagent`, `manage_task`, `schedule`, `search_web` | **byte-identical** |
| `manage_subagents` | differs (388 vs 261 bytes) |
| `send_message` | differs (164 vs 89 bytes) |
| `run_command` | the catalogue mirrors the *converter* text; the declaration carries the *runtime* text (499 vs 397 bytes) |

Six independent byte-identical matches is what makes the extraction
trustworthy; the three differences are real 1.4.6 → 1.4.7 changes in the
reference, and the clone's declaration fixture has not absorbed them yet (§7).

## 6. What changed in the clone

| file | change |
|---|---|
| `internal/tools/testdata/tool_descriptions_1.4.7.json` | **new** — the reference's own text for 36 names, with the layer each came from |
| `internal/tools/testdata/tool_names_1.4.7.txt` | **new** — the binary's 63 known names |
| `internal/tools/descriptions.go` | **new** — the embedded fixture, `ReferenceDescriptions()`, `refDesc()` |
| `internal/tools/catalog.go` | thirty-three descriptions become `refDesc("name")`; the header records the provenance and the 63/60 split |
| `internal/tools/command.go` | the stray `\n` is gone from `runCommandDescription()` |
| `internal/tools/support.go` | `bgTask.kill()` waits (bounded) for the task to be reaped, so the task log is written before the tool returns |
| `internal/tools/descriptions_test.go` | **new** — the catalogue is pinned against the fixture |
| `internal/tools/catalog_test.go` | the live list is pinned again; the 63/60 split gets its own test |
| `internal/tools/model_surface_test.go`, `internal/piagentbridge/bridge_test.go` | expectations follow the reference's text |
| `internal/referencedrift/reference.lock.json` | the two new artefacts are registered |

The tests derive the expectation **from the fixture**, so a paraphrase cannot
come back: `TestCatalogueDescriptionsComeFromTheReference` walks the fixture
and compares every entry with what the catalogue serves, and
`TestRunCommandDescriptionIsByteExact` compares the rendered converter text
with the captured one.

## 7. 硬约束 (hard constraints)

- **The catalogue derives its descriptions from the fixture.** Do not type a
  description back into `catalog.go`; `refDesc` is the only source. A
  hand-written sentence in that file is a divergence the tests cannot see.
- **The reference's own text is reproduced byte for byte**, including its
  quirks: the `%s` holes in `ask_permission`'s and `ask_custom_permission`'s
  prose (the reference fills them at runtime), the fragment in
  `run_workflow`, and the converter/runtime split on `run_command`.
- **`run_command`'s description is rendered per host** (the operating system
  and the shell). The fixture holds the *darwin* rendering; the byte-exact
  test is skipped off darwin, and the tool text itself is not frozen.
- **The advertised list stays at 60 names.** The binary's 63 are what the
  reference *knows*; a live session prints 60. Never grow `init.tools` to 63.
- **The declaration fixture is the model-facing source of truth.**
  `model_tools_1.4.6.json` stays untouched by this cycle — the two 1.4.7
  declaration changes (§5) need a re-capture of the whole request body, not
  a description.
- **`bgTask.kill()` waits for the reap.** The task log is written by the
  monitor goroutine when the command exits; a caller that tears the workspace
  down right after the kill races that write.

## 8. 存疑 (open questions)

- **Which variant of `edit_file`, `send_message` and `manage_subagents` is
  1.4.7's default** is still unresolved. The binary carries two variants of
  each; the fixture's `send_message` and `manage_subagents` texts are one of
  them (the 164- and 388-byte ones).
- **`replace_file_content`'s base prose** is a `text/template` value at
  `0x108235550` and was not recovered; the fixture carries the converter
  layer's text for it (210 bytes), which may not be the whole story.
- **The join separators** for `ask_permission`, `ask_custom_permission` and
  `replace_file_content` are not known: the reference assembles them from
  parts, and the fixture holds the assembled text with its `%s` holes.
- **`run_workflow`'s full text** is still a fragment.
- **The three withheld searches** are known to the reference and not
  advertised; *why* is inferred (a missing service; the IDE's
  `no_code_search` build variant is the same switch seen from the other side)
  — 【推测】, not measured.
- **The catalogue's descriptions are still dead text** on the default path:
  `tools.Describe` has one caller (`internal/piagentbridge/tool.go`), and
  `piagentbridge.Tools()` has no non-test caller. The descriptions will reach
  a model only if the registry is re-advertised somewhere.
- **The schema mismatches** the extraction found (the clone's executors use
  `path`/`content` where the reference uses `Path`/`Content`, and 1.4.7's
  `run_command` adds `NotificationTimeoutSeconds`) are untouched: the
  declaration fixture is a capture, and re-capturing it is a separate cycle.

## 9. Limitations

- The texts were read out of **this machine's** agy 1.4.7 Mach-O. A
  different build (or the IDE's own `language_server`) may carry different
  prose; the fixture's digest is what pins *this* one.
- The converter layer's schemas are generated at runtime from Go
  `jsonschema` tags, so only `ask_permission` and `ask_custom_permission`
  have hand-written ones; the twenty-four names with no description have no
  schema either, and `internal/tools/browser.go`'s schemas are the clone's
  own invention.
- The extraction covers the **CLI** binary only. The IDE's language server
  has its own tool definitions, and its addresses are not interchangeable with
  this binary's.
- The catalogue's *schemas* are still the clone's own for most tools; this
  cycle changed the descriptions only.
