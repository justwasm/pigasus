# Cycle agy-system-prompt — the reference system prompt, reproduced and applied

## Goal

wagyu is supposed to reach byte-level parity with `agy`. Until this cycle it
sent **no system prompt at all**: the conversation reached the model as tools
plus bare user turns, while the reference sends a 34,350-byte instruction block
on every request. This cycle freezes that block, reproduces it in Go, and puts
it on the wire in both the print and the TUI paths.

## Where the prompt comes from

Two external references were read and one ground truth used:

| Source | Verdict |
|---|---|
| `internal/agyprompt/testdata/capture_1.4.2.txt` — a real agy 1.4.2 request, recorded by the cycle-274 wrapper | **Authoritative.** Byte-exact, this build. |
| [asgeirtj/system_prompts_leaks PR #233](https://github.com/asgeirtj/system_prompts_leaks/pull/233) (`Google/antigravity-cli.md`, ~50 KB) | The **IDE** prompt, not the CLI. Corroborates the `<identity>` opening and the `<user_information>` field layout; documents IDE-only sections (MCP servers, Knowledge Items, JSON tool schemas) and prose sections the CLI does not have. |
| [Jackwio gist](https://gist.github.com/Jackwio/fd918f4d5d3e530cf95c6b8b74e10fde) | A zh-Hant feature/command guide. No prompt text; useful only to cross-check the slash commands and tool vocabulary the prompt names. |

Both links are recorded in `docs/research/REFERENCES.md`.

## What the prompt is

Ten XML-tagged sections, in a fixed order, joined with a single newline and
with **no trailing newline**:

| Section | Size | Kind |
|---|---|---|
| `<identity>` | 455 B | static |
| `<artifacts>` | 3,694 B | static (names `<appDataDir>/brain/<conversation-id>`) |
| `<conversation_transcript>` | 1,917 B | static |
| `<guidelines>` | 211 B | static |
| `<communication_style>` | 480 B | static |
| `<slash_commands>` | 1,494 B | static list of 7 commands |
| `<messaging>` | 853 B | static |
| `<subagents>` | 2,033 B | fixed head + generated list of 3 builtins + fixed tail |
| `<skills>` | 22,268 B | fixed preamble + generated list + fixed tail |
| `<user_information>` | 640 B | fully generated (OS, workspaces, cwd, app data dir, conversation id, artifact dir) |

The 13 tool declarations agy sends are **not** part of this text; they ride the
request as a separate `tools` array (see cycle 274's capture notes).

## Finding: the system prompt never reached the wire

Reproducing the text was only half the job. The first end-to-end attempt showed
the recorded request carrying **only the user message**:

```
messages = [{role:user, content:"say hi"}]
```

`internal/piagentbridge`'s `systemText` read `SystemMessage.Content` and
nothing else:

```go
case pi.SystemText: return string(content)
```

But a `coding.Session` keeps the instructions in the system message's
**`Sections`** and leaves `Content` empty (`promptSectionUpdate` builds
`ai.SystemMessage{Content: SystemText(""), Sections: patch}`). So every system
prompt — PiG's default one included — was rendered as an empty string and
dropped. This was a pre-existing bug affecting both transports, found only
because this cycle checked the wire instead of the API.

`systemText` now renders content **and** sections, joined with `"\n\n"`,
matching PiG's own `ai.GetCurrentSystemPrompt`. Both the native agw provider
(`agwprovider_request.go`) and the `StreamFn` path (`stream.go` →
`backend.StreamRequest.SystemPrompt` → `agw_k0s.go`) call it, so the two
transports stay identical.

## Implementation

```
internal/agyprompt/            new package
  agyprompt.go                 Options, Build, section assembly, OSName,
                               BuiltinSubagents, DefaultOptions
  skills.go                    DiscoverSkills(roots) + SKILL.md frontmatter
  id.go                        NewConversationID (UUID v4 shape)
  sections/*.txt               the 11 embedded static bodies
  testdata/capture_1.4.2.txt   the ground-truth prompt
  testdata/capture_1.4.2.json  its dynamic inputs (skills, subagents, host)
  agyprompt_test.go            byte-for-byte + section-order + discovery tests
```

Wiring:

| Path | Change |
|---|---|
| `internal/pigrt/runtime.go` | A new `Options.ConversationID`; when `SystemPrompt` is empty the runtime builds `agyprompt.Build` from the cwd, agent dir, a conversation id and the **skills the loader actually discovered**. `Options.PigPrompt` restores PiG's own prompt. |
| `internal/pigrt/runtime.go` | New `Runtime.SystemPrompt()` / `Runtime.ConversationID()` accessors. |
| `cmd/wagyu/print_pig.go`, `cmd/wagyu/main.go` | `pigPrintConfig.ConversationID`, threaded from `runPrintMode`'s envelope id. |
| `cmd/wagyu/tui_system_prompt.go` | `(*runMode).systemPrompt()`; `cmd/wagyu/tui_run.go` passes it as `agent.Options.SystemPrompt` (previously unset). |
| `internal/piagentbridge/stream.go` | `systemText` renders `Sections` too. |

The skill list is generated from the same agy roots `pigrt.AgySkillPaths`
computes (builtin bundle, `~/.gemini/skills`, each plugin's `skills`), parsed
from each `SKILL.md` frontmatter, so the prompt names the skills the model can
actually open.

## Verification

**Byte-for-byte against the capture** (`internal/agyprompt`):

```
$ go test -count=1 ./internal/agyprompt/
ok  github.com/chadgpt/openagy/internal/agyprompt
```

`TestBuildMatchesCapture` rebuilds the prompt from `capture_1.4.2.json` and
requires string equality with the recorded 34,350-byte prompt. This pins every
static section, the list rendering, and the `<user_information>` layout to the
reference rather than to our reading of it.

**The wire**, end to end (`cmd/wagyu/print_system_prompt_test.go`): an
`httptest` server speaks the agw SSE shape and records the request body; the
turn runs through `runPigPrintTurn` → `pigrt` → PiG's session → the clone's agw
provider. The recorded body's first message is

```json
{"role":"system","content":"<identity>\nYou are Antigravity, a powerful agentic AI coding assistant …<user_information>\nThe USER's OS version is …\nCommand Working Directory: <dir>\nConversation ID: 99999999-…"}
```

with no `operating inside pig` anywhere.

**The TUI** (`cmd/wagyu/tui_system_prompt_test.go`): `(*runMode).systemPrompt()`
against an isolated `ANTIGRAVITY_HOME` returns the reference prompt including
the discovered skill.

**The renderer** (`internal/piagentbridge/system_text_test.go`): sections-only,
content+sections, empty-section and content-only cases.

**`internal/pigrt/system_prompt_test.go`**: default is the reference prompt
(identity, skills, user information, no trailing newline); `PigPrompt` restores
PiG's; an explicit `SystemPrompt` still wins.

Full suite, `go vet` and `gofmt` are clean:

```
$ go test -count=1 ./cmd/... ./internal/...
(no failures)
$ go vet ./internal/agyprompt/ ./internal/piagentbridge/ ./internal/pigrt/ ./cmd/wagyu/
$ gofmt -l internal/agyprompt
```

## 硬约束 / 存疑 / limitations

- **The captured prompt is 1.4.2 on macOS.** The static sections are only known
  to be correct for that build. A new `agy` release can change any of them; when
  it does, the fixture must be re-captured and the `sections/*.txt` regenerated.
  There is no automated detector for that drift yet.
- **`<slash_commands>` is treated as static.** It lists seven commands for this
  build. Whether the reference varies it by account, experiment flag or feature
  gate is **unverified** (推测: it is compiled in).
- **OS-version mapping is verified for macOS only.** `OSName()` maps
  `darwin → mac`; `linux → linux` and `windows → windows` are guesses about what
  the reference prints. Only "mac" is first-hand.
- **Multiple workspaces are untested.** The capture has exactly one. `Build`
  renders one bullet per workspace; what the reference does with zero or several
  is unknown.
- **Custom subagents are not appended.** The prompt lists the three builtins.
  If a user defines subagents through agy's own mechanism the reference surely
  lists them; wagyu does not, because it does not track that state. The builtin
  three are verbatim from the capture.
- **The artifact directory is derived, not observed.** `ArtifactDir` defaults to
  `AppDataDir/brain/<conversation-id>` because that is what the capture shows for
  its own conversation id; agy never printed the rule.
- **The leaked prompt is the IDE variant** and is corroboration only. Any future
  diffing against it must first separate IDE-only sections.
- **Skill ordering is `os.ReadDir` lexical order**, matching
  `printSlashSkillInfos`; the capture happens to agree. Whether agy sorts by
  name, by root, or by discovery order is not proven beyond that agreement.
- **Only `SKILL.md` frontmatter is read.** Skills whose description cannot be
  unmarshalled are skipped, as in `printSlashSkillInfos`; agy might list them
  with an empty description instead. Unverified.
- **The prompt is rebuilt per TUI prompt.** Skill discovery is a handful of
  small file reads, but it is not free and is not cached on the run mode.
- **`PigPrompt` is a divergence knob.** Setting it reproduces pre-cycle
  behaviour; it is not part of the reference surface and nothing sets it.
- **Recapture requires the wrapper.** The fixture came from
  `OPENAGY_CAPTURE_DIR` on an agy `-p` turn; the Google inference path is
  location-blocked on this account, but the *request* is captured before the
  403, which is why the system prompt is recoverable at all.
