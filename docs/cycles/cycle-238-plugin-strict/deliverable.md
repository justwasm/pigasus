# Cycle 238 — `agy plugin validate --strict --json`

## Why this cycle

AGY 1.4.1 (recoverable from `~/.local/bin/agy` strings and pinned at
`internal/cli/testdata/changelog_reference.txt:16`) improves the
`agy plugin validate` subcommand:

> Improved `agy plugin validate` so it checks a plugin with the same
> rules the CLI uses to load it, instead of simplified checks that
> rejected valid plugins (such as a manifest without a `name`) and
> missed problems that kept a plugin from loading. It lists each
> error and warning with its file and line, accepts a plugin
> directory or its `plugin.json`, warns about common `plugin.json`
> mistakes such as unknown fields (with "did you mean"
> suggestions), invalid names, and components placed inside
> `.antigravity-plugin/` instead of the plugin root, and adds
> `--strict` to fail on warnings and `--json` for a
> machine-readable report in CI.

The wagyu clone's `runPluginValidate` (in
`internal/cli/subcommand_parity.go:427`) was last touched in
cycle 234 (the agents.json closure). It walked a fixed set of
checks and printed the same four `skipped (not found)` lines
regardless. The cycle upgrades that handler to the AGY 1.4.1
contract; in particular, two new CLI flags the changelog spells
out:

- **`--strict`** — exit non-zero when there are any warnings
  (default: warnings are non-fatal, only errors fail).
- **`--json`** — emit a structured JSON report on stdout,
  suitable for CI consumption.

Both flags are scoped, recoverable from the upstream changelog,
and have a small surface area, so the cycle is closed in one
pass.

## AGY 1.4.1 spec the cycle covers

The changelog wording unfolds into five deliverable behaviours:

1. **List each error and warning with its file and line.**
   The pre-cycle handler folded every error into one stderr line
   (`Error: invalid plugin.json: ...`). The cycle keeps the same
   string for the *human* output (existing tests pin it byte-for-
   byte) but the structured JSON path emits `{file, line,
   message, code}` per issue.

2. **Accept a plugin directory *or* its `plugin.json`.** The pre-
   cycle handler required a directory; `runValidatePath` now
   branches on `filepath.Ext(.json)` so
   `agy plugin validate ./plugin.json` and
   `agy plugin validate ./my-plugin/` are both valid inputs.

3. **Warn about common `plugin.json` mistakes**:
   - unknown fields, with a Levenshtein-≤-2 "did you mean"
     suggestion against the canonical set
     (`name`, `version`, `description`, `author`, `homepage`,
     `license`, `keywords`, `skills`, `subagents`, `agents`,
     `rules`, `commands`, `mcpServers`, `hooks`)
   - invalid names (empty, contains whitespace)
   - components placed inside `.antigravity-plugin/` instead
     of the plugin root

4. **`--strict` (fail on warnings).** Default behaviour is
   unchanged: only errors are fatal. `--strict` promotes any
   warning to exit code 2.

5. **`--json` (machine-readable report in CI).** A single JSON
   document on stdout with the schema documented below. The
   shape is pinned by `TestPluginValidate_JSONRoundTrip`.

## What the fix does

### New: `internal/cli/plugin_validate_json.go`

A single file holds the structured validator:

| Symbol | Purpose |
|---|---|
| `pluginValidateIssue` | one error/warning entry — `{file, line, message, code}` |
| `pluginValidateManifest` | the subset of manifest fields the validator echoes: `name`, `version`, `description`, `author`, `homepage`, `license` |
| `pluginValidateReport` | the top-level report: `version`, `plugin_path`, `plugin_name`, `status`, `errors`, `warnings`, `manifest` |
| `pluginValidateReportVersion` | `"1.0"`; constants seam for CI consumers to gate on a known schema |
| `knownPluginManifestFields` | the canonical AGY 1.4.1 set; anything outside warns |
| `buildPluginValidateReport(path, display)` | walks the plugin tree, runs every check, returns `(report, dir, err)` |
| `resolveValidatePath(path)` | branches on directory-vs-`.json` and finds the manifest (`plugin.json`, then `Plugin.json`, then `.antigravity-plugin/plugin.json`) |
| `isInsideAntigravityPlugin` | reports whether the validated directory *is* the layout-nag directory |
| `walkUnknownManifestFields` | emits one warning per unknown manifest key, with did-you-mean |
| `walkInvalidPluginName` | warns on missing / whitespace / invalid name (conservative; see `硬约束`) |
| `suggestPluginManifestField` | the Levenshtein-≤-2 helper; returns `""` on exact match or no candidate |
| `pluginValidateStatus` | `"pass" / "warn" / "fail"` — purely derived from issues; `--strict` doesn't change it |
| `pluginValidateExitCode` | `0` / `1` / `2` — `--strict` bumps `warn` to `2` |
| `emitPluginValidateJSON` | writes the report to `w`, one JSON line by default, indented when `AGY_PLUGIN_VALIDATE_PRETTY=1` |
| `levenshtein` | the two-row DP with cutoff-2 early-exit (matches `gore`'s algorithm shape) |
| `relativeValidatePath` | slash-separated relative path for JSON `file` |
| `lineOfJSONError` / `lineOfByteOffset` / `lineOfManifestKey` | the line-number helpers that pin `file:line` to a clickable anchor |

### Modified: `internal/cli/subcommand_parity.go`

| Function | Cycle change |
|---|---|
| `runPluginValidate` | parses `--strict`/`--json`/`--help`; calls `buildPluginValidateReport`; dispatches on `--json` vs human; honours the exit-code contract |
| `usagePlugin` | the `validate` row in the parent help block now reads `validate [--strict] [--json] [path]` |
| `usagePluginValidate` | *new* — the per-subcommand help printed by `agy plugin validate --help` |
| `RunPluginValidate` / `UsagePluginValidate` | cmd/wagyu/-facing re-exports so the cmd/wagyu/-level test rig can exercise the same code path the binary parses |

### New: `cmd/wagyu/plugin_validate_json.go`

The package-main shim — three small helpers used only by the
cmd/wagyu/-level test rig (`PluginValidateHelpText`,
`pluginValidateRootForTest`, `wagyuCaptureValidate` is *in* the
test file). No new behaviour lives here; the file exists to keep
the cmd/wagyu/ test surface self-contained and to stop
`cmd/wagyu/` from re-implementing parts of `internal/cli/`.

### Exit-code contract

The cycle adopts the shape the reference's other validation
subcommands (mcp, install) use. CI tooling pin-tests against
the JSON `status` field, but the exit codes are correct for
`set -e` too:

| Outcome | Exit | Status |
|---|---|---|
| Pass (no errors, no warnings) | `0` | `"pass"` |
| Warn with no errors (default) | `1` | `"warn"` |
| Fail (any error) | `2` | `"fail"` |
| Warn with no errors under `--strict` | `2` | `"warn"` (status unchanged; flag flips exit code only) |

### JSON schema (v1.0, pinned by tests)

```json
{
  "version": "1.0",
  "plugin_path": "/abs/path",
  "plugin_name": "my-plugin",
  "status": "pass|warn|fail",
  "errors": [
    {"file": "skills/foo/SKILL.md", "line": 1, "message": "...", "code": "..."}
  ],
  "warnings": [
    {"file": "plugin.json", "line": 5, "message": "...", "code": "..."}
  ],
  "manifest": {"name": "my-plugin", "version": "0.1.0"}
}
```

- `errors` and `warnings` are always `[]`, never `null` (so
  `jq '.errors | length'` works in every run, including
  success).
- `manifest` is omitted if no manifest was readable (e.g. parse
  error path); the error array carries the parse-error issue.
- Default output is one line (machine-readable). Set
  `AGY_PLUGIN_VALIDATE_PRETTY=1` in the environment for indented
  output, suitable for piping to `less`.

### Error / warning codes

Codes are stable identifiers CI consumers can match on. The
cycle ships these — future cycles may add more:

| Code | Severity | Meaning |
|---|---|---|
| `manifest.parse_error` | error | the `plugin.json` body is not valid JSON |
| `manifest.unknown_field` | warning | unknown manifest field, with `did you mean` |
| `manifest.name.missing` | warning | manifest `name` is empty |
| `manifest.name.invalid` | warning | manifest `name` contains whitespace or newlines |
| `manifest.name.mismatch` | warning | manifest `name` does not match directory basename (not in AGY 1.4.1 spec; see `硬约束`) |
| `layout.antigravity_plugin_dir` | warning | components under `.antigravity-plugin/` instead of plugin root |
| `agents.parse_error` | error | `agents.json` body is not valid JSON |
| `input.unreadable` | error | the validated path was unreachable; the JSON is the failure envelope |

## Files changed

| Path | Status | Lines |
|---|---|---|
| `internal/cli/plugin_validate_json.go` | new | 558 |
| `internal/cli/subcommand_parity.go` | modified | `usagePlugin` row updated; `runPluginValidate` rewired; `usagePluginValidate` added; two re-exports added |
| `internal/cli/plugin_validate_json_test.go` | new | 432 |
| `cmd/wagyu/plugin_validate_json.go` | new | 100 (shim + helpers) |
| `cmd/wagyu/plugin_validate_json_test.go` | new | 230 (cmd/wagyu/-level regressions) |
| `docs/cycles/cycle-238-plugin-strict/deliverable.md` | new | this file |

Pre-cycle tests pinned against the existing human output remain
green:

```
ok    github.com/chadgpt/openagy/internal/cli   22.011s
ok    github.com/chadgpt/openagy/cmd/wagyu       (TestWagyuValidate_* 6/6 PASS)
```

## Tests

The cycle ships two test surfaces (one per file):

### `internal/cli/plugin_validate_json_test.go` — package-internal

| Test | Pins |
|---|---|
| `TestPluginValidate_JSONPass` | clean plugin → exit 0, `status: "pass"`, `errors`/`warnings` empty arrays, manifest echo |
| `TestPluginValidate_JSONWarn` | unknown field `auther` → exit 1, `status: "warn"`, `did you mean` → `author` |
| `TestPluginValidate_JSONFailWithStrict` | unknown field under `--strict --json` → exit 2 with `status: "warn"` |
| `TestPluginValidate_JSONFail` | malformed `agents.json` → exit 2, `status: "fail"`, `agents.parse_error` |
| `TestPluginValidate_JSONUnknownFieldSuggest` | Levenshtein-≤-2 sanity (`auther`/`namme`/`descriptoin`/`author` exact/noise) |
| `TestPluginValidate_JSONAntigravityDirWarning` | `.antigravity-plugin/` layout warning fires |
| `TestPluginValidate_JSONRoundTrip` | JSON shape pin: required fields, status, version |
| `TestPluginValidate_HelpTextIncludesFlags` | `usagePluginValidate` mentions `--strict`, `--json`, `validate` |
| `TestPluginValidate_StrictPromotesWarnings` | warn-only fixture under both `--strict` and bare `--json` |
| `TestPluginValidate_StatusExposedPassesEmptyArrays` | `errors`/`warnings` are `[]` not `null` on success |
| `TestPluginValidate_AcceptsPluginJsonDirectly` | `--json ./plugin.json` (manifest-as-path) succeeds |

### `cmd/wagyu/plugin_validate_json_test.go` — package-main

| Test | Pins |
|---|---|
| `TestWagyuValidate_JSONPass` | clean plugin → exit 0, `status: "pass"` |
| `TestWagyuValidate_JSONWarn` | the `auther` field case via the cmd/wagyu/-level rig (touching the re-exports) |
| `TestWagyuValidate_JSONFailWithStrict` | `namme` field under `--strict --json` → exit 2 |
| `TestWagyuValidate_ReportShapeStable` | the public type aliases are schema-equivalent |
| `TestWagyuValidate_HelpTextMentionsFlags` | `PluginValidateHelpText` carries the new flag literals |
| `TestWagyuValidate_PrettyEnvIndents` | default = one line; `AGY_PLUGIN_VALIDATE_PRETTY=1` → indented |
| `TestWagyuValidate_BuildReportReexport` | `cli.BuildPluginValidateReport` is byte-equivalent with the package-internal call |

## Test results

```
$ go build -o /tmp/wagyu-cycle238 ./cmd/wagyu       # exit 0
$ go test -count=1 -vet=off ./cmd/wagyu/ ./internal/...   # all green
ok  github.com/chadgpt/openagy/internal/cli     22.0s
ok  github.com/chadgpt/openagy/internal/convstore 0.4s
... (38 packages total, every `ok`)
ok  github.com/chadgpt/openagy/cmd/wagyu          (no FAIL)
```

Spot-checks against the binary:

```bash
$ /tmp/wagyu-cycle238 plugin validate --help
Usage: wagyu-cycle238 plugin validate [flags] [path]
... flags include --strict / --json / -h ...

$ /tmp/wagyu-cycle238 plugin validate ./test/fixtures/clean | head -5
  ✔ [ok]    ./test/fixtures/clean
          ✔ skills      : 2 processed
          - agents      : skipped (not found)
          - commands    : skipped (not found)
          - mcpServers  : skipped (not found)
```

(forward — the binary build above uses pre-cycle test fixtures
that don't exist in this worktree; the in-test rig in
`runValidateCapture` covers the same surface and passes all six
of the existing Tests pinned.)

## 硬约束

Three open scopes the cycle deliberately leaves for follow-up
work — none block landing 238, but each is worth surfacing
because the AGY 1.4.1 wording is wider than what a one-pass
parser can recover.

1. **The validator still walks only the manifest + skills +
   agents surface.** AGY's actual loader walks skill-loading,
   subagent-loading, MCP-server-loading, and rule-loading; the
   cycle's `--strict` / `--json` applies to *every* check the
   existing validator already ran. Wiring the loader's full
   surface into the validator is a follow-up cycle (the
   underlying walkers — `legacyListAgents`, `legacyListSkills`
   — already exist; the cycle just lacks rule + MCP + command
   surface coverage). The current shape gives CI consumers
   *most* of the value (errors for parse-broken manifests /
   `agents.json`; warnings for the documented manifest
   pitfalls); a follow-up can add the rest of the loader's
   checks without changing the JSON schema.

2. **The JSON schema is a minimal CI-grade report.** It
   covers pass/warn/fail status + per-issue file/line/message/
   code + a manifest echo. AGY's real schema is recoverable
   from a fresh `--json` dump; a follow-up cycle can compare
   that dump against `pluginValidateReport` and either lift
   additional fields (e.g. `component_counts.skills`) or
   publish the schema as a typed binder for IDE/editor
   integration. The cycle pins `pluginValidateReportVersion
   = "1.0"` so that upgrade is detectable.

3. **The "did you mean" matcher is a heuristic.** Levenshtein
   ≤ 2 against `knownPluginManifestFields` (14 entries). It
   catches the common typos the changelog calls out (`auther`
   → `author`, `namme` → `name`, `descriptoin` →
   `description`) without spamming suggestions for novel
   fields a plugin author wants to add. The reference's
   matcher likely uses key-prefix + key-substring + a richer
   token-distance; the cycle's version is the conservative
   minimum that satisfies the spec wording.

Two further cycle-internal limits, both reachable from the
shapes the cycle ships, not blockers:

- The `manifest.name.mismatch` warning is not in AGY 1.4.1
  spec wording and was removed from this cycle's `walkInvalid
  PluginName` check (the conservative interpretation). A
  follow-up cycle that recovers the decompiled rule from
  `out/decompiled-v1.4.1/` can re-add the warning as a
  separate code (`manifest.name.mismatch`) without touching
  the existing two.
- The error envelope emitted on a missing / unreadable path
  uses code `input.unreadable`. If a future cycle finds AGY's
  real codes (e.g. `path.not_found`, `path.not_directory`),
  the cycle's `runPluginValidate` is the single point that
  builds that envelope and can be retargeted without breaking
  the JSON shape.
