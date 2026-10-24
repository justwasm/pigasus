# Cycle plugin-validate-components — `agy plugin validate` component checks (1.4.1 gap)

## TL;DR

| What | Where |
|---|---|
| Validator rewritten to the live 1.4.3 surface (human block, `--json` schema, exit codes) | `internal/cli/plugin_validate_json.go` |
| Dispatcher now delegates to the reference-accurate implementation; `--help` is rejected like the binary | `internal/cli/subcommand_parity.go` (`runPluginValidate`) |
| Skills / agents / rules / manifest / root-file / workflow / hooks / MCP checks | same file (`scanValidate*`, `parseFrontmatter`, `jsonPosMessage`) |
| Old fabricated-schema tests removed; 11 reference-pinned tests added | `internal/cli/plugin_validate_reference_test.go` |
| Existing agent/case-insensitive validate tests updated to reference behaviour | `internal/cli/plugin_agents_test.go`, `internal/cli/plugin_case_insensitive_test.go` |
| `cmd/wagyu` re-export shim (help text + exported report aliases) deleted | `cmd/wagyu/plugin_validate_json.go`, `cmd/wagyu/plugin_validate_json_test.go` |

The 1.4.1 changelog bullet

> Improved `agy plugin validate` to also check a plugin's skills, subagents, and
> rules … and adds `--strict` … and `--json` …

was previously implemented from the prose alone. Cycle 238 invented an output
shape; a live probe of `agy 1.4.3` shows that **none** of it matches. This cycle
replaces the validator with one that is byte-identical to the reference on 40+
crafted fixtures, in both the human and `--json` surfaces, including ANSI colour.

---

## Discovery — what the reference actually prints

All evidence below is 【一手】: captured from `~/.local/bin/agy` 1.4.3 (SHA-256
checked before/after; the binary was not modified) against throw-away plugin
trees under `/tmp/pvfix`. Fixtures exercise every branch the changelog names.

Human surface:

```
Validating <name> (<manifest>)

  ✗ skills/x/SKILL.md: no frontmatter: the file must start with a --- block holding name and description. The loader skips this skill
  ⚠ skills/y/SKILL.md: no name in the frontmatter: the skill is named "SKILL" after its file. Add name: y
  · plugin.json: declares the Agent Plugins 1.0.0 $schema: …

  skills 1 · agents 0 · rules 0 · hooks 0 · mcpServers 0 · sidecars 0

✔ Validation passed with 1 warning
```

* Findings sort by **severity first** (`error`, `warning`, `note`), then file.
* A failing footer (`Error: …`) goes to **stderr**; a passing footer stays on
  stdout. Exit code is 1 for errors and for `--strict` + warnings, 0 otherwise.
* Colours are TTY-only: `✗` red (`31`), `⚠` yellow (`33`), `·` cyan (`36`),
  `✔` green (`32`). Piped output is plain.
* `--help` is **not** supported: `Error: unknown validate argument "--help":
  usage is validate [path] [--strict] [--json]`, exit 1.

`--json` (indent 2, field order fixed):

```json
{"dir":"…","manifest":"plugin.json","name":"…","agentPluginsSpec":false,
 "components":{"agents":0,"hooks":0,"mcpServers":0,"rules":0,"sidecars":0,"skills":0},
 "findings":[{"severity":"warning","file":"…","message":"…"}]}
```

`severity` uses `"note"`, not `"info"`. `manifest` is emitted even when the
manifest fails to parse; `name` is omitted then. A missing directory surfaces as
`{"file":".","message":"<path> is not a directory"}` and preserves the path
exactly as typed (relative stays relative).

### Recovered checks

| Component | Errors | Warnings |
|---|---|---|
| Manifest | parse failure (`line L, column C: …`), UTF-8 BOM, unsupported Agent Plugins `$schema` | unknown field (with a `did you mean` suggestion within 2 edits), missing name, invalid name characters, invalid Agent Plugins name |
| Skills (`skills/<folder>/SKILL.md`, flat) | no frontmatter, invalid YAML | missing name (falls back to `SKILL`), missing description, folder mismatch, duplicate name, body > 24000 B |
| Agents (`agents/*.md`, flat) | invalid YAML, missing name/description, reserved name, unparseable `agent.json` | no frontmatter, duplicate name, any `agent.json` (never loaded) |
| Rules (`rules/*.md`, flat) | invalid YAML, missing/invalid `trigger` | `glob` without `globs`, `model_decision` without description, content > 24000 B |
| Root | — | `AGENTS.md` / `GEMINI.md` outside `rules/`, a `workflows/` directory |
| Hooks | `hooks.json` parse failure, groups without `command` | unknown event keys in the legacy shape; `note` for Claude-Code-style `{"hooks": …}` |
| MCP | `mcp_config.json` parse failure | — (valid `mcp_config.json` servers are counted; a BOM is tolerated) |
| `<component>.json` | BOM, malformed JSON | — (`exclude` filters the loaded set by skill folder / agent stem / rule stem) |

Non-obvious details, all measured:

* Discovery is **one level only**; nested `skills/a/b/SKILL.md`, loose
  `skills/x.md`, `rules/sub/x.md`, `agents/sub/x.md` are ignored.
* A rule `README.md` without frontmatter is skipped silently; any other rule
  without frontmatter is a missing-trigger error.
* `trigger` accepts only `always_on`, `model_decision`, `glob`, `manual`.
* The 24 KB limit is **24000**, not 24576. Skill bodies report their own length;
  rules report the whole body length as “cut by N bytes”.
* `hooks.json` counts **top-level keys**, so the Claude-Code wrapper form
  (`{"hooks": {...}}`) counts as 1 and emits a `note`.
* `skills.json`, `agents.json` and `rules.json` all share one `exclude` list.
  `skills.json` matches the **folder**; `agents.json` and `rules.json` match the
  Markdown **file stem** (not the frontmatter name). Excluded entries still get
  some diagnostics — skills keep parse errors and (when duplicate) the mismatch,
  agents keep everything, rules keep only YAML parse errors — but are never
  counted and never body-checked.
* Excluded entries are processed **after** included ones, which decides the
  direction of a duplicate warning.
* Root `agents.json` entries are not counted (despite the 1.3.6 changelog note),
  but the file is parsed: a bare array or a BOM produces an error.
* BOM policy is per file: the manifest, `hooks.json`, `agent.json`, `skills.json`,
  `agents.json` and `rules.json` reject a UTF-8 BOM with the explicit message;
  `mcp_config.json` and `SKILL.md` tolerate it; agent/rule Markdown does not
  strip it (so it reads as “no frontmatter”).
* When the manifest lives inside `.antigravity-plugin/` or `.gemini-plugin/`,
  components next to it (skills/agents/rules/workflows/hooks.json/mcp_config.json)
  produce a warning and are not loaded. The reference repeats the directory name:
  “Move it next to .antigravity-plugin”. Components at the plugin root still
  load, even when both copies exist.
* Manifest lookup is case-insensitive (`Plugin.json`, `PLUGIN.JSON`); the hidden
  directory names are not.
* Reserved agent names observed: `deepagent`, `review-agent` (~900 candidates
  probed; see 存疑).
* Direct `plugin.json` arguments keep their parent path as typed, except a bare
  filename, whose parent `.` is canonicalised to the absolute CWD.

---

## Verification

Every fixture under `/tmp/pvfix` was run through both binaries and diffed on
stdout, stderr and exit code:

```
for d in …; do
  agy plugin validate /tmp/pvfix/$d        >ref.out 2>ref.err; r1=$?
  /tmp/wagyu plugin validate /tmp/pvfix/$d >clo.out 2>clo.err; r2=$?
  diff ref.out clo.out && diff ref.err clo.err && [ $r1 = $r2 ] || echo "MISMATCH $d"
done
```

Covered: pass / warn / error / strict; skills (no frontmatter, bad YAML, missing
name/description, mismatch, duplicate, 24000/24001 B boundary); agents (all four
errors, duplicate, no frontmatter, both `agent.json` cases); rules (all four
triggers, missing trigger, bad YAML, `globs` as a YAML sequence, `README.md`,
truncation); manifest (unknown fields, no name, invalid native name, invalid
Agent Plugins name, unsupported `$schema`); root files; `workflows/`; hooks
(legacy and modern, valid and invalid); `mcp_config.json`; relative/absolute
paths; direct manifest; `--help`; extra positional args; ANSI output under a
pty. The only remaining mismatch is the macOS case-insensitive
`.Antigravity-Plugin` edge documented below.

Tests: `go test ./internal/cli/ -run TestPluginValidate_` and the full
`go test ./cmd/... ./internal/...` are green.

---

## 硬约束

* The clone reproduces the reference **only for the fixtures above**. Messages
  were copied from stdout; a branch not probed may still differ.
* Manifest/config JSON parse errors reproduce `line/column` for the bare-token
  (`invalid literal: bad`) and truncated (`parsing value` / `parsing object
  after value` / `parsing array after value` / `parsing string` +
  `unexpected EOF`) cases probed. Other `encoding/json` errors keep the
  decoder's wording, so exotic malformed manifests can differ.
* The reserved-agent set is `{deepagent, review-agent}` — both observed. The
  reference's complete built-in set is not recoverable from strings alone.
* `sidecars` is always counted as 0; no fixture produced a non-zero value, and
  `plugin.json` has no recognised `sidecars` field.
* The `hooks.json` “modern” group validator only checks that each group has a
  `command`; deeper hook-type semantics are not reproduced.

## 存疑

* **Reserved names.** ~900 candidates (tool names, agent-ish names, `-agent`
  variants, DeepCoder/DeepInvestigator, image-generator) produced exactly two
  hits. The check may key off a list populated from internal registrations, so
  more names likely exist.
* **`agentPluginsSpec`.** True only when `$schema` is
  `https://agent-plugins.org/schemas/1.0.0/plugin.schema.json`; verified against
  an unsupported `9.9.9` (hard error) and a native manifest (false). The
  `enable-agent-plugins-spec` flag itself is not modelled.
* **`.Antigravity-Plugin` on macOS.** Because APFS resolves the directory
  case-insensitively, the clone accepts it; the reference reports
  `.antigravity-plugin/plugin.json: the loader doesn't load this plugin`. This
  is a wrong-case edge on a case-insensitive filesystem.

## limitations

* No attempt was made to re-derive the validator from a 1.4.3 decompilation.
  The committed Ghidra project/seeds under `out/local-macos/` are from 1.3.4, so
  their addresses do not map onto the installed 1.4.3 binary (the strings dump
  used here was regenerated from 1.4.3). Evidence is therefore behavioural, not
  decompiled.
* The previous cycle-238 exported API (`BuildPluginValidateReport`,
  `EmitPluginValidateJSON`, `PluginValidateReportVersion`,
  `PluginValidateExitCode`, and the `cmd/wagyu` help shim) was removed. It had
  no callers outside its own tests; CI consumers of the old `--json` schema must
  switch to the reference schema documented above.
* Report language: English, matching the file under test (`internal/cli/`).
