# Cycle 99 A — interactive tool calling, the tool-call UI with `ctrl+o`, and slash commands

Cycle 99 closes four gaps the user reported, in order:

1. *"继续 tool call 集成，现在还是无法调用工具"* — the interactive TUI never
   advertised any tools, so the model could only talk. Print mode had driven the
   cycle-97 agent loop since cycle 97; the TUI had not.
2. *"你应该实现并且验证这些工具能够正常在这个 antigravity clone 里面被调用"* — the
   tools must be **verifiable** as invocable, not merely present.
3. *"实现相关的 UI 提示，还有 ctrl+o expand details 视图"* — render the tool steps
   in the transcript and let `ctrl+o` expand/collapse their detail.
4. *"你的这个 slash commands 都不应该被 interpreted as normal chat messages"* — a
   slash command must never be forwarded to the model as chat text.

A fifth, adjacent gap was found and fixed on the way: cycle 95's model
resolution diverged from the reference and silenced the `"… is no longer
available"` warning (see Part 4).

Reference throughout: **agy 1.3.6**, 183,407,440 B, SHA-256
`827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab` (re-checked
after the harness run — unchanged). All reference runs used
`AGY_CLI_DISABLE_AUTO_UPDATE=1`.

## Result

| Surface | Before | After |
|---|---|---|
| Tools in the interactive TUI | none advertised, `chunk.ToolCalls` discarded | full recovered catalogue advertised; calls executed; verified live |
| Tool-call UI | nothing | step lines + `(ctrl+o to expand)` / `(ctrl+o to collapse)`, `%d steps` groups, `<output +%d lines>` |
| `ctrl+o` | unbound | toggles every tool group in the transcript |
| Spinner line | always `Generating...` | names the running tool |
| TUI `/unknown` | `Unknown slash command: /x (cycle 77: …)` | `Unknown command: /x` (recovered wording), never forwarded |
| TUI `/` alone | **forwarded to the model** | `Unknown command: /` |
| `--print /<cmd>` | every slash command forwarded to the model | 11 served locally, 26 rejected with recovered messages, the rest forwarded |
| Model resolution | cycle 95 divergence: settings model used unconditionally | reference's flag → settings → catalogue → fallback + warning |
| Go tests | — | +24 test functions (all green) |
| Parity harness | 61/61 | **86/87** (1 explained difference) |
| Print-mode slash comparison | — | **36/38** (2 explained differences) |

## Part 1 — interactive tool calling

### The gap

`v135RunMode.streamPrompt` issued a single `backend.Stream` with no `Tools`
field and dropped every `StreamChunk.ToolCalls`. A live pty capture of our
clone answering *"Read secret.txt with view_file"* produced:

```
I don't have a file reading tool available. There's no view_file tool in my function list.
```

### The fix

`streamPrompt` now drives the shared `agent.Loop` with the same inputs print
mode uses:

```go
loop, err := agent.New(agent.Options{
    Backend:      be,
    Handler:      …sanitized chunk sink…,
    DefaultModel: model,
    MaxTurns:     1,
    Tools:        decls,
    ToolRunner:   wrapped,
})
```

* `decls` is `toolDeclsFromRegistry(tools.Catalog(workspace))` — the 32-name
  recovered catalogue, sorted, exactly as print mode builds it.
* `wrapped` is `tuiToolRunner`, which decorates the real runner (the cycle-98
  `permissionRunner` gate over `registryRunner`) so the UI hears about a call
  starting and finishing.
* The reply is passed through `agent.NewSanitizer()`, so ` thinking` blocks are
  hidden — matching the reference, which never shows raw reasoning. Print mode
  already did this.

`EnableTools(decls, runner)` installs both, and `runLiveTUI` calls it. The
10-call-site `NewV135RunMode` signature is unchanged.

### Verification (live, real model, real TUI)

pty driver, prompt *"Read secret.txt with view_file and print its contents."*
in a scratch workspace containing `secret.txt` = `SECRETTOKEN42`:

```
> Read secret.txt with view_file and print its contents.
⏺ Read secret.txt  (ctrl+o to expand)
The contents of `secret.txt` are:

```
SECRETTOKEN42
```
```

`SECRET ON SCREEN: True` — the secret only exists on disk, so the model could
not have produced it without the tool actually running.

## Part 2 — the tool-call UI and `ctrl+o`

### Recovered vocabulary

【strings】 1.3.6 string table:

| String | Role |
|---|---|
| `ctrl+o to expand` / `ctrl+o to collapse` | the two hints |
| `(ctrl+o to expand)` | collapsed hint as rendered |
| `%d steps` | group summary |
| `<output +%d lines>` | truncated expanded detail |
| `KeyToggleExpandGroup` | the keybinding's name |
| `Ran` `Read` `Wrote` `Explored` `Searched` `Listed` `Messaged` `Spawning` `Browsing` `Needs approval` `Failed` | step titles |
| `Ran in the sandbox`, `Command output:` | command-step body |
| `Unknown command: /` | TUI rejection |

【strings】 the changelog shipped in the binary (dumped with
`agy -p /changelog`, 150,390 B) names the same surfaces:

* *"groups related tool calls and thoughts into concise summaries"* and
  *"Changed the default `Verbosity` setting from `high` to `medium`"* — tool
  **groups** exist and are summarised;
* *"a more specific hint on the spinner line, which now names the tool call the
  agent is currently running"* — the spinner carries the tool name;
* *"Changed the step title of commands that exit with a non-zero code from
  `Errored` to `Failed`"* — the `Failed` suffix;
* *"Fixed `invoke_subagent`, … rendering with no tool call line at all in
  collapsed and expanded tool groups, leaving a bare `(ctrl+o to expand)` hint
  with no tool name"* — a collapsed group **must** still show its tool-call
  lines. This is why the collapsed multi-step rendering below keeps the
  per-step titles.

### Layout

```
single step, collapsed     ⏺ Read secret.txt  (ctrl+o to expand)
single step, expanded      ⏺ Read secret.txt  (ctrl+o to collapse)
                             ⎿ SECRETTOKEN42
group, collapsed           ⏺ 2 steps  (ctrl+o to expand)
                           ⏺ Ran date
                           ⏺ Read secret.txt
group, expanded            ⏺ 2 steps  (ctrl+o to collapse)
                           ⏺ Ran date
                             ⎿ <output>
                           ⏺ Read secret.txt
                             ⎿ <output>
```

`⏺` leads a step and `⎿` leads a detail line — the same detail glyph the
existing `renderV135Warning` already emits for the reference's warning block
(`  ⎿  "MiniMax-M3" is no longer available. …`).

**Grade**: the *vocabulary*, the *hints*, the *expand/collapse behaviour*, the
`Failed` suffix and the "never a bare hint" rule are 【一手】/【strings】. The
*arrangement* (which glyph leads a line, where the hint sits) is 【推测】: the
reference renders through its own conversation-view component and no offline
capture of a tool-bearing transcript is available — the account's quota is
exhausted (`⚠ Individual quota reached … Resets in 93h…`), so every reference
turn terminates with `RESOURCE_EXHAUSTED` before a step can be drawn.

### How `ctrl+o` re-renders

`m.response []string` is the rendered transcript and is written from ~45 call
sites (mostly slash-command feedback), so it was left as the single source of
rendered text. Each turn that produced steps records a `turnBlock{start, lines,
steps}` — the contiguous range of `m.response` its step block occupies.
`ctrl+o` flips `m.expanded` and `rerenderBlocks` replaces each block's range in
place, shifting the offsets of later blocks by the size delta. Slash-command
feedback interleaved between turns is untouched, and `/clear` drops the blocks.

During a turn, each finished step is appended immediately (so progress is
visible), and `turnDoneMsg` re-renders the whole block from the full step list —
which is what introduces the `%d steps` group header once there are two or more.

### Verification

* Go: `TestTUITools_CtrlOTogglesStepDetail`, `…GroupHeaderForMultipleSteps`,
  `…TruncatesLongDetail`, `…SpinnerNamesRunningTool`, `…FailedStepTitle`,
  `…StepTitleVocabulary`, `…TurnAdvertisesAndRunsTool`, `…NoRunnerIsTextOnly`.
* Live pty, `ctrl+o` (0x0f) sent after the turn:

```
⏺ Read secret.txt  (ctrl+o to expand)
…
  ⎿ SECRETTOKEN42
```

## Part 3 — slash commands are never chat

### 3a. The TUI

Two bugs:

1. `splitSlash` returns `""` for `/` and `/ `, and `handleSlashCommand` bailed
   out on `cmd == ""`, so a bare `/` fell through and **was sent to the model**.
2. The unknown-command feedback used our own cycle-77 wording
   (`Unknown slash command: /x (cycle 77: unrecognized …)`).

Fixed: a bare `/` is handled like any other unknown command, and the wording is
the recovered `Unknown command: /<name>`. A live 1.3.6 TUI capture of
`/zzznotacommand` renders exactly `Unknown command: /zzznotacommand` and sends
nothing upstream; our clone now does the same (verified live for both `/zzznotacommand`
and `/`).

### 3b. Print mode — the recovered classification

`agy -p /<cmd>` on 1.3.6 with a signed-in HOME yields exactly three outcomes,
all captured:

| Outcome | Count | Behaviour |
|---|---|---|
| served locally | 11 | `help` `changelog` `permissions` `agents` `model` `skills` `usage` `config` `effort` `hooks` `credits` |
| rejected | 26 | the interactive-only commands, exit 2 (`credits` exits 1) |
| forwarded | rest | sent to the model as literal text (`/plan`, `/goal`, `/init`, `/xyz`, …) |

The rejection message is recovered verbatim:

```
error: /clear is not available in print mode (every print-mode run already
starts a new conversation unless --continue or --conversation is passed);
pass --disable-slash-commands to send /clear to the model as literal text
```

Three commands carry a bespoke tail that offers a CLI alternative —
`/resume` (`use --conversation <id> or --continue instead`), `/add-dir`
(`use --add-dir <path> instead`) and `/remote-control`
(`use --remote-control instead`). `/plugin` has an entirely bespoke message and
its own shape (`/plugin failed: … use /plugin [list|install|uninstall|enable|disable]
or the \`plugin\` CLI subcommand`).

`--disable-slash-commands` (already a v1.3.5-parity flag in
`internal/cli/parser_specs.go`, previously unused) suppresses both local
handling and rejection so every slash command is forwarded.

### Local command fidelity

Byte-identical to the reference on this machine (verified by diffing
`agy -p /<cmd>` against our clone):

| Command | Source |
|---|---|
| `/help` | the 12-row table, recovered verbatim |
| `/changelog` | 150,390 B markdown, embedded as `cmd/antigravity/testdata/print_changelog.md` |
| `/config` | 34 keys, sorted, defaults recovered, non-string values re-encoded with Go's HTML-escaping encoder |
| `/permissions` | `global\tallow\|deny\t<rule>` from settings.json |
| `/model` | `<slug>\t<label>` of the **effective** model |
| `/skills` | 57 rows from the builtin bundle, `~/.gemini/skills`, and `~/.gemini/config/plugins/*/skills` (prefixed `<plugin>:<name>`) |
| `/agents`, `/hooks`, `/effort` | the reference's (empty / empty / `high`) |

Two non-obvious discoveries while matching `/skills`:

* `agy changelog` and `agy -p /changelog` print **different renderings** of the
  same notes (a compact `1.3.5:\n· Added …` bullet list vs the markdown
  document). The subcommand's blob was already embedded; the print-mode blob is
  new.
* `~/.claude/skills` is **not** a root. The reference lists `agents-sdk` (which
  lives in `~/.gemini/skills`) but none of the Claude-only skills
  (`algo`/`alpha`/`assets`/…).
* Skill entries may be **symlinks** (`~/.gemini/skills/terminal-browser` is);
  `os.DirEntry.IsDir()` is false for those, so the directory test follows the
  link.

### Verification

`/tmp/agycmp/cmp_slash.sh` runs 38 commands through both binaries and diffs
rc/stdout/stderr: **36/38 MATCH**. The harness
(`/tmp/agycmp/harness136.sh`, extended with the 26 rejection cases) is
**86/87**. Go tests pin the same strings and exit codes without a network:
`TestDispatchPrintSlash_{ForwardsNonSlash,DisableFlag,LocalCommands,Rejections,PluginAndCredits,RejectionTailSubstitution,ResumeTailIsBespoke}`,
`TestPrintSlashHelpTextShape`, `TestPrintSlashConfigShape`, `TestModelSlug`,
`TestSkillFrontmatter`.

## Part 4 — model resolution (cycle 95 correction)

Cycle 95 made the settings.json model win unconditionally, which silenced the
`"… is no longer available"` warning. A live 1.3.6 capture with
`"model": "MiniMax-M3"` in settings.json shows the reference does the opposite:

```
▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
…
⚠ Warning
  ⎿  "MiniMax-M3" is no longer available. Using "Gemini 3.8 Flash (High)".
…
Gemini 3.8 Flash · high
```

`model_resolve.go` implements the reference's three steps — `--model` flag,
then settings.json, then default — followed by a catalogue check against the
same six entries `agy models` prints. An unavailable name falls back to
`Gemini 3.8 Flash (High)` and reports the source for the warning block. Our
banner now matches the capture above byte-for-byte.

This also makes `/model` match: the reference prints the *effective* model
(`gemini-3.8-flash-high`), not the configured one.

The backend is unaffected: `internal/backend/agw_k0s.go` ignores `req.Model`
and sends the registry entry's `ExternalModelName` (the same behaviour the
reference has), so the wire is unchanged and live tool calling still works.

## Verification summary

| Check | Result |
|---|---|
| `go build ./cmd/antigravity/` | clean |
| `go vet ./internal/... ./cmd/...` | clean |
| `go test ./internal/... ./cmd/...` | 24 packages ok |
| New test functions | 24 |
| Parity harness vs 1.3.6 | **86/87** |
| Print-mode slash comparison | **36/38** |
| TUI tool call, live model | secret read off disk, rendered as a step |
| TUI `ctrl+o`, live | detail revealed |
| TUI unknown command, live | `Unknown command: /zzznotacommand`, nothing upstream |
| TUI bare `/`, live | `Unknown command: /` |
| Banner/warning, live | byte-identical to the reference capture |
| Reference SHA-256 after the run | unchanged |

## 硬约束 / 存疑 / limitations

* **Tool-step layout is a reconstruction.** Names, hints, the `Failed` suffix,
  the "no bare hint" rule and the expand/collapse behaviour are recovered; the
  glyph arrangement is ours. No reference capture of a tool-bearing transcript
  exists — the account's quota is exhausted, so the reference terminates every
  turn with `RESOURCE_EXHAUSTED` before drawing a step. Consequence: the
  transcript will not be byte-identical to the reference for tool-bearing turns.
* **Verbosity is not modelled.** The reference's `Verbosity` setting
  (`high`/`medium`/`low`, default `medium`) selects how aggressively steps are
  grouped and summarised (`Explored N files` style). Our clone always renders
  the per-step form with a `%d steps` group header for two or more. `verbosity`
  appears in `/config` with its recovered default, but nothing reads it.
* **`/plugin` rejects earlier than the reference.** Our clone rejects it
  pre-auth (exit 2). The reference authenticates first and only then rejects,
  so with a scratch HOME it hangs instead. With a signed-in HOME both give
  exit 2 and the same message. This is the single harness difference
  (`slash_plugin 124/2`).
* **`/usage` cannot be reproduced.** The reference reads a live quota service
  (`Gemini Models\tWeekly Limit Remaining\t0%\t2026-10-19T20:56:40Z`). We have
  no such source; the clone returns `error: /usage failed: retrieving usage:
  failed to retrieve user quota summary: no quota info found`. The
  `failed to retrieve user quota summary: %w` inner string is recovered; the
  `retrieving usage:` wrapper is 【推测】. This is the second `/model`-family
  difference in the 36/38 comparison.
* **`/skills` builtin allowlist is empirical.** The bundle ships nine skills on
  disk but `/skills` lists only `agy-customizations` and `antigravity-guide`.
  The other seven (`automation`, `generative_ui`, `migrate-workflows`,
  `permissioned-github`, `plugin`, `run-workflow`, `ui-plugin-navigation`) have
  no distinguishing frontmatter — not `metadata:`, not `disable-slash-command:`,
  not the block-scalar style. The allowlist reproduces this machine's output
  exactly; the reference's selection rule is **unknown** and a different bundle
  could break it.
* **`/skills` descriptions use a hand-rolled YAML reader.** Only `name:` and
  `description:` are read, with folding for `>`/`|`. A description using any
  other YAML construct would be mis-rendered.
* **Tool→title mapping is 【推测】.** `toolStepTitle` maps each recovered tool
  name to a verb (`run_command` → `Ran`, `view_file` → `Read`, …). The verbs are
  recovered; the *mapping* is inferred from the tool semantics.
* **The `(ctrl+o to expand)` hint is per-group, not per-key.** The reference's
  `/help` shortcuts tab lists `ctrl+o`; we bind it globally to the tool groups.
  `/diff` folding also uses `Ctrl+O` in the reference — our `/diff` is a stub,
  so there is no conflict yet, but there will be one when `/diff` is built.
* **Print mode's backend still uses the configured model string.** Only the
  *displayed* model is corrected. `pickBackendWithLog` keeps receiving the
  flag/settings value, because the agw registry lookup depends on it. Since
  `agw_k0s.go` ignores `req.Model`, the upstream request is identical either way
  — but a future non-agw backend that honours `req.Model` would send the
  unavailable name.
* **`internal/cli/dispatch.go` remains gofmt-unclean** (pre-existing; the
  complaint is in the package doc comment, not this cycle's additions).
* **Not verified**: the reference's *interactive* `ctrl+o` on a real tool group
  (quota), the reference's TUI rendering of a failed step, and the reference's
  `/skills` on a machine with a different bundle.

## Files

| File | Change |
|---|---|
| `cmd/antigravity/tui_tools.go` | new — step model, titles, rendering, `tuiToolRunner`, tea messages |
| `cmd/antigravity/tui_tools_test.go` | new — 8 tests |
| `cmd/antigravity/print_slash.go` | new — print-mode slash dispatch, local handlers, rejections |
| `cmd/antigravity/print_slash_test.go` | new — 11 tests |
| `cmd/antigravity/model_resolve.go` | new — effective-model resolution |
| `cmd/antigravity/model_resolve_test.go` | new — 5 tests |
| `cmd/antigravity/testdata/print_changelog.md` | new — the 150,390 B `/changelog` blob |
| `cmd/antigravity/tui_v135.go` | tool wiring, `EnableTools`, loop-driven `streamPrompt`, `ctrl+o`, block re-render, spinner hint, model resolution |
| `cmd/antigravity/slash_commands.go` | bare `/` no longer forwarded; `Unknown command: /x` wording; `/clear` drops blocks |
| `cmd/antigravity/main.go` | print mode dispatches slash commands before the agent loop |
| `internal/cli/dispatch.go` | exported `ExitCodeError`, `ExitCodeOf` |
| `internal/cli/v135_parity.go` | exported `ReferenceModels` |
| `internal/cli/changelog_data.go` | exported `ReferenceChangelog` |

## Supersedes

* Cycle 95's "settings.json model wins unconditionally" behaviour. Its premise
  — that the `"… is no longer available"` warning was spurious — does not hold
  against 1.3.6: the reference emits exactly that warning in exactly that
  configuration. See Part 4.
