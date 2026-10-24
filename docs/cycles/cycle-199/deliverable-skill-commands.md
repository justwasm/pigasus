# Cycle 199 — `/<skill>` invokes the skill instead of "Unknown command"

## TL;DR

Every skill the reference lists under `/skills` is an invocable slash command.
`handleSlashCommand` used to answer `Unknown command: /<skill>`; it now resolves
the skill and sends its instruction to the model as a prompt (`sendAsPrompt`),
the same path `/goal` and `/plan` use.

The catalogue is the *existing* disk enumerator `printSlashSkillInfos()` in
`print_slash.go` (builtin bundle + `~/.gemini/skills` + each plugin's
`skills/`), so `/skills` and `/<skill>` can never disagree. `~/.local/bin/agy
-p /skills` (headless, no OAuth) lists 57 entries and the clone's enumerator
reproduces them 57/57.

## Accepted spellings (probed against REF 1.4.0)

| Form | Example | Accepted |
|---|---|---|
| display name | `science:alphafold-database-fetch-and-analyze` | yes |
| display name minus plugin prefix | `alphafold-database-fetch-and-analyze` | yes |
| SKILL.md directory name | `alphafold_database_fetch_and_analyze` | yes |
| underscore-normalised display name | `terminal_browser` | **no** |

`-`/`_` is *not* normalised; lookup is verbatim and case-sensitive.

## Files

| File | Δ |
|---|---|
| `cmd/wagyu/slash_skills.go` (new) | +160 |
| `cmd/wagyu/slash_skills_test.go` (new) | +313 |
| `cmd/wagyu/slash_commands.go` | +16 (the `default:` branch only) |

## Limitations 【存疑】

- The reference's outgoing payload for a skill is unobservable in print mode
  (`cli.log` never contains the body). The prompt sent is the SKILL.md body,
  falling back to the frontmatter description, then `Follow the <name> skill.`
- Whether the reference forwards `/skill <args>` (and how) was not verified; the
  clone echoes the args and appends them to the prompt.
- Builtin skills that `/skills` does not list (`automation`, `generative_ui`)
  stay non-invocable.
- The 1.4.0 *print* path forwards an unknown command to the model; the TUI keeps
  `Unknown command: /<cmd>`. Left as-is.
