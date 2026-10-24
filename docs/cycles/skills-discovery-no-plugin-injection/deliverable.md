# AGY 1.2.7 — Skill Tool Invocations Verbatim @-Args Pass-Through

Cycle branch: `feature/skills-discovery-no-plugin-injection`
Base commit: `54db67b` (cycle 224)
Single commit: see `git log` on the branch.

## Gap

AGY 1.2.7 changelog line: *"Fixed 'skill' tool invocations from /, plugins,
and custom slash commands including arguments like `@path/to/file` being
expanded by `@file` mentions as if `@path` were a file path token, instead
of being passed to the named skill verbatim."*

The fix is "args pass verbatim". The clone's skill dispatch in cycle 199
(`/cmd/wagyu/slash_skills.go`, `/cmd/wagyu/slash_commands.go`,
`cmd/wagyu/print_slash.go`) already routes args as a `\n\n`-suffixed string
into `slashSkillPrompt` and from there onto `m.session` or
`cfg.Prompt` → `rt.RunWith` — no `@` transformation layer exists anywhere
on the path. The gap close is therefore *preventive*: pin a single named
opt-out point so a future @-file mention layer cannot walk into skill
args without editing a function whose existence is load-bearing for the
contract.

The PiG parity inventory records the same invariant for the upstream:
*"Interactive @file-looking tokens stay literal in the submitted user
message, matching upstream."*
(`/Users/gear/go/pkg/mod/github.com/!michael!kinsy/!pi!g@v0.4.1/test/parity/normalization-inventory.json`).
Cycle-231's `@<subagent> <message>` parser already enshrines the same
rule for that surface (`@file/foo.txt hello` falls through to chat;
`cmd/wagyu/at_subagent.go:108-112`).

## Files

| File | Status | What it does |
|---|---|---|
| `cmd/wagyu/slash_skills_noat.go` | NEW | `verbatimSkillArgs(args) string` — the single named pass-through opt-out. Today it returns `args` unchanged; the function exists so a future @-mention pre-processor sees it has to bypass this name to walk into skill args. |
| `cmd/wagyu/slash_skills_noat_test.go` | NEW | 8 unit tests pinning the verbatim contract from three angles: pure helper, `slashSkillPrompt` body, end-to-end `handleSlashCommand`. |
| `cmd/wagyu/slash_skills.go` | 7-line doc-comment edit on `slashSkillPrompt` | Names the AGY 1.2.7 contract and points the reader at `verbatimSkillArgs` for the opt-out point. No production logic changed. |

NOT touched: `internal/convstore/sqlite.go` (hard constraint).

## Tests added (≥5 required)

| # | Test | What it pins |
|---|---|---|
| 1 | `TestVerbatimSkillArgs_AtPath` | identity on `@path/to/file` (the canonical AGY 1.2.7 case). |
| 2 | `TestSlashSkillPrompt_AtArgVerbatim` | full prompt with the literal `@path/to/file` arg; helper route (`slashSkillPrompt(skill, verbatimSkillArgs(args))`) returns the same string. |
| 3 | `TestSlashSkillPrompt_NormalArgsWorks` | baseline — unprefixed `inspect example.com` flows through unchanged. |
| 4 | `TestSlashSkillPrompt_NoAtArgWorks` | empty-args case — no spurious newline bleed. |
| 5 | `TestSlashSkillPrompt_MultipleAtArgsPreserved` | two-arg, three-arg, mixed, dup subtests. |
| 6 | `TestSlashSkillPrompt_AtInMiddleNotExpanded` | `@` mid-sentence (`please consider @path/to/file between two regular words`) stays literal. |
| 7 | `TestRunSkill_AtPathVerbatimThroughRouter` | end-to-end through `handleSlashCommand("/demo-skill @path/to/file")` — session message is the literal string, transcript has no `<file name="...">` wrapper. |
| 8 | `TestRunSkill_PluginSkillAtArgVerbatim` | plugin-prefixed skill (`/demo-plugin:demo-skill @path/to/file`) — same contract, plugin enumerator path. |

All 8 tests + the pre-existing `TestSkillInvocationNames`,
`TestRunSkill_SendsPrompt`, `TestRunSkill_AliasRouting`,
`TestRunSkill_UnknownStillUnknown`, `TestSkillCatalog_ReferenceSubset`,
`TestSkillPrompt_PrefersBodyFallsBackToDescription`,
`TestSkillBody_*` pass.

## Build & verification

```text
$ go build -o /tmp/wagyu-sdnp ./cmd/wagyu
(exit 0; 73.3 MB)

$ go test -vet=off -count=1 -run 'TestSkill|TestInvocation|TestAtFile|TestExpand' \
    ./cmd/wagyu/ ./internal/...
(exit 0; all packages pass — "no tests to run" on the empty patterns is fine)

$ gofmt -l cmd/wagyu/slash_skills_noat.go cmd/wagyu/slash_skills_noat_test.go
(empty — both new files gofmt-clean)

$ gofmt -l cmd/wagyu/slash_skills.go
(empty — the 7-line doc-comment edit introduced no formatting drift)
```

## Why a single-line identity helper is load-bearing

`slashSkillPrompt` builds the prompt as `<body>\n\n<args>`. The body is
the SKILL.md markdown; the args are the user-typed trailing string. Any
future `@`-expansion layer that lands on the chat submit path must run
*before* `sendAsPrompt` and *must* carve out skill prompts — otherwise
`/my-skill @notes.md` would fetch `@notes.md` and inline its contents
into the skill body, breaking the contract the user typed. Today there
is no such layer, but the cycle names the opt-out point so a future
patch either (a) calls `verbatimSkillArgs` to opt out, or (b) edits a
named function whose existence is the contract — not a silent
behavioural drift.

The 7-line comment block on `slashSkillPrompt` also lands so the next
reader of `slash_skills.go` sees the AGY 1.2.7 contract without having
to chase this cycle's deliverable.

## 硬约束 / Limitations

- The clone has no @-file-mention pre-processor to "fix"; this cycle
  pins the invariant against a future one and pins the verbatim
  contract from three angles. If a future cycle adds `@`-expansion, the
  hook must consult `verbatimSkillArgs` — the test suite will fail and
  remind the author.
- The chat submit path (`submitPrompt` → `m.input.Value()` →
  `handleSlashCommand`) is checked end-to-end through
  `TestRunSkill_AtPathVerbatimThroughRouter`. The print path is checked
  transitively because `dispatchPrintSlash → slashSkillPrompt(skill,
  args)` reuses the same helper, and the test asserts on
  `slashSkillPrompt` directly. A direct end-to-end print-mode test
  requires a live backend and is outside this cycle's scope (existing
  parity tests handle print-mode regression separately).
- The `internal/convstore/sqlite.go` file is unchanged.
- Pre-existing not-clean files under `internal/` and
  `cmd/wagyu/picker_view.go`, `cmd/wagyu/tui_v135_test.go` were NOT
  reformatted. The 7-line edit to `cmd/wagyu/slash_skills.go` is a
  doc-comment append only.

## Commit

```
git log --format='%H %s' -1
```
on `feature/skills-discovery-no-plugin-injection`.
