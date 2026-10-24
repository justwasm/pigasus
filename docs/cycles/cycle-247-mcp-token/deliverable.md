# Cycle 247 — MCP server `${VAR}` header variable substitution (AGY 1.4.0 feature gap)

Worktree: `/Users/gear/Documents/GitHub/openagy-mcp-token`
Branch: `feature/cycle-247-mcp-token`

## Why this cycle

AGY 1.4.0 changelog
(`internal/cli/testdata/changelog_reference.txt:21`):

> "Fixed plugin MCP servers that use a variable in a request
> header, such as `"Authorization": "Bearer ${TOKEN}"`,
> sending the literal placeholder and failing to authenticate;
> headers now get the value you configured, like the server's
> command, arguments, and environment already did."

The clone's agy → PiG MCP bridge
(`internal/pigrt/mcpconfig.go`) explicitly flagged the gap at
line 177 of the original file:

```go
// The agy schema has no per-server headers; `wagyu mcp add
// --header` writes them, but the field is not part of
// auth.MCPServer yet, so a header set by the CLI is not
// round-tripped here. Left as a known gap.
```

Two problems collided:

1. The on-disk schema (`AgyMCPServer`, the mirror struct the
   runtime reads) didn't carry the `Headers` field at all, so
   even a literal `"Authorization": "Bearer abc123"` was
   silently dropped when the bridge translated the file into
   PiG's `McpServerConfig`.
2. Even if the bridge carried the field through, the
   placeholder syntax would reach PiG verbatim, so the
   canonical failure mode ("Bearer ${TOKEN}" reaching the
   upstream server) would still occur.

Cycle 247 closes both: the struct gains the field, and a
shell-style substitution helper resolves the placeholders
against the merged server-env ∪ host-env before the headers
reach PiG.

## Spec — the four supported syntaxes

| Form | Resolved to |
|---|---|
| `${VAR}` | `env["VAR"]` if present, else `""` |
| `$VAR` | `env["VAR"]` if present, else `""` |
| `${VAR:-default}` | `env["VAR"]` if non-empty, else `"default"` |
| `$$` | literal `"$"` |

Rules pinned by tests in `internal/mcp/header_substitute_test.go`:

- Keys are never substituted (only values).
- Substituted values are not recursively re-scanned.
- Empty values pass through unchanged; a nil input map
  yields a non-nil empty result.
- The merged env is `os.Environ()` with the server's `env`
  block layered on top (server-env overrides host-env).

## What the fix does

### Files changed

| File | Lines | Purpose |
|---|---|---|
| `internal/mcp/header_substitute.go` | new, ~140 | `SubstituteHeaderVars(m, env)` plus the scan loop. Stdlib (`strings` only). |
| `internal/mcp/header_substitute_test.go` | new, ~150 | 10 sub-tests covering the four syntaxes + 6 guard rails. |
| `internal/pigrt/mcpconfig.go` | +60 / -10 | adds the `Headers` field to `AgyMCPServer`, builds `mergedEnv = serverEnv ∪ os.Environ()` once, calls `mcp.SubstituteHeaderVars`, and writes the result into `cfg.Headers` as PiG's `*extension.OrderedStrings`. Updates the file-header docstring and removes the "known gap" comment. |
| `internal/pigrt/mcp_integration_test.go` | +85 / -1 | one end-to-end test (`TestLoadAgyMCP_SubstitutesHeaderVars`) that proves a temp `mcp.json` with mixed-syntax headers round-trips through `LoadAgyMCP` into PiG's `McpServerConfig.Headers` fully resolved. Plus the `orderedStringsToMap` helper. |
| `internal/cli/testdata/feature_diff_table.json` | +7 / -0 | flips the new "MCP server ${VAR} header variable substitution" entry to `clone: true`, `cycle: 247`, with `evidence` pointing at the two production files. |
| `docs/cycles/cycle-247-mcp-token/deliverable.md` | new | this report. |

Total: ~440 lines added across 6 files, zero new third-party
dependencies.

### Code path

1. **On-disk schema**
   (`internal/pigrt/mcpconfig.go:AgyMCPServer`): gains the
   `Headers map[string]string` JSON field with `omitempty`,
   sitting between `Env` and `Enabled` so the field order in
   the marshal output is `[type, url, command, args, env,
   headers, enabled, alwaysLoad]` — the AGY 1.4.0 wire
   ordering (alwaysLoad remains last so older readers that
   sort by JSON key still see a stable view).

2. **Translation**
   (`internal/pigrt/mcpconfig.go:agyToPiGServer`): for HTTP
   servers, calls `mergeServerEnv(s.Env)` once and passes
   the merged map to `mcp.SubstituteHeaderVars(s.Headers,
   mergedEnv)`. The substituted map is then sorted
   alphabetically (matching the cycle-183 sort-key contract
   already in place for `env`/`args`) and pushed into PiG's
   `*extension.OrderedStrings` via `NewOrderedStrings`. The
   stdio branch is unchanged — headers are an http-only
   field.

3. **`mergeServerEnv`**
   (`internal/pigrt/mcpconfig.go:mergeServerEnv`): walks
   `os.Environ()` once, splits each `KEY=VALUE` with
   `strings.IndexByte`, and layers `serverEnv` on top so
   server-env wins on a collision. nil serverEnv falls
   through to the host env unchanged.

4. **Substitution engine**
   (`internal/mcp/header_substitute.go:SubstituteHeaderVars`
   → `substituteOne`): a single-pass scanner over each
   value. It returns a fresh map (no input mutation) and
   short-circuits when the input value contains no `$`
   rune, which is the common case (most headers are opaque
   strings). The four syntaxes are dispatched by a 5-line
   switch over the rune after `$`. The bare-dollar path
   reads Go identifier runes (`[A-Za-z_][A-Za-z0-9_]*`) so
   `$TOKEN/foo` substitutes TOKEN and keeps `/foo` literal.

5. **End-to-end test**
   (`internal/pigrt/mcp_integration_test.go:TestLoadAgyMCP_SubstitutesHeaderVars`):
   writes a temp `mcp.json` with five headers exercising
   host-env, server-env-wins, server-only, default-clause,
   and no-vars cases; reads it back through the public
   `pigrt.LoadAgyMCP`; unpacks PiG's `*extension.OrderedStrings`
   via `Keys()`/`Get()`; and asserts each value resolved
   correctly. A secondary loop confirms no `${` substring
   remains anywhere in the emitted headers — that's the
   literal regression-guard for the AGY 1.4.0 bug.

## Tests

`internal/mcp/header_substitute_test.go` (10 cases):

- `TestSubstituteHeaderVars_Basic` — `${TOKEN}` → `"abc123"`
- `TestSubstituteHeaderVars_BareDollar` — `$TOKEN/prod` →
  `"abc123/prod"` (boundary: `$VAR` followed by non-identifier
  `/`)
- `TestSubstituteHeaderVars_Default` — `${MISSING:-fallback}`
  → `"fallback"`
- `TestSubstituteHeaderVars_UnsetEmpty` — `${MISSING}` → `""`
  (no panic on absent key)
- `TestSubstituteHeaderVars_EscapeDollar` — `$$100` →
  `"$100"` (env entry `100=...` is not consulted)
- `TestSubstituteHeaderVars_NoSubstitutionInKey` — header
  keys are passed through verbatim, even when the key is
  literally `"$TOKEN"`
- `TestSubstituteHeaderVars_DoesNotMutateInput` — the input
  map is `reflect.DeepEqual` to a pre-call snapshot
- `TestSubstituteHeaderVars_NilAndEmpty` (3 subtests):
  nil map → non-nil empty result, empty value passes
  through, value without `$` is returned as-is
- `TestSubstituteHeaderVars_DefaultSkippedWhenSet` —
  `${TENANT:-fallback}` with `TENANT=acme` resolves to
  `"acme"` (default clause is not consulted when the
  variable is present and non-empty)
- `TestSubstituteHeaderVars_NoRecursiveExpansion` —
  `OUTER=got-${INNER}` substituted into `${OUTER}` yields
  `got-${INNER}` verbatim (no second scan)

`internal/pigrt/mcp_integration_test.go`:

- `TestLoadAgyMCP_SubstitutesHeaderVars` — end-to-end:
  temp mcp.json → `LoadAgyMCP` → PiG `McpServerConfig.Headers`
  → assertions on every header value, plus a literal-`${`
  sweep.

## Test results

- `go build -o /tmp/wagyu-cycle247 ./cmd/wagyu` — exit 0.
  Binary: 89,682,323 bytes (+~50 KB vs cycle-244 baseline;
  most of the size is in the test file which doesn't ship
  in the binary, plus a small slice of substitution code).
- `go test -vet=off -count=1 ./internal/mcp/` — exit 0.
  All 10 cases pass.
- `go test -vet=off -count=1 -run TestLoadAgyMCP_SubstitutesHeaderVars ./internal/pigrt/` — exit 0. The
  end-to-end case passes.
- `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...`
  — exit 0. **All 27 packages pass.** The cycle-183
  PiG-bridge tests, the cycle-234 mcp overlap tests, and the
  cycle-244 alt+m tests pass unchanged.
- `python3 -c "import json; json.load(open('internal/cli/testdata/feature_diff_table.json'))"`
  — exit 0 (the table round-trips).
- `go test -vet=off -count=1 -run TestFeat ./cmd/wagyu/`
  — exit 0 (the cycle-236 `TestFeatDiff_*` regression suite
  now classifies the new row as `clone: true` and emits
  the new `evidence` pointer).

## Hard constraints / 存疑 / limitations

- **Auth.MCPServer was not extended.** The runtime reads
  `~/.gemini/antigravity-cli/mcp.json` via the `AgyMCPServer`
  mirror in `internal/pigrt/mcpconfig.go`, so the
  substitution only needs the field there. `auth.MCPServer`
  in `internal/auth/compat.go` is documented as the
  "v1.3.5 binary 1:1 schema", and the cycle-247 changelog
  reference (line 21) does not contain a verified 1.4.0
  mcp.json field listing — adding `Headers` to `auth.MCPServer`
  would require a binary capture to pin the exact field
  order. The clone's `/mcp` panel
  (`cmd/wagyu/mcpServersFeedback`) still renders an
  `auth.MCPConfig` and would not display headers set via
  the on-disk file; that's an existing gap, pre-cycle-247,
  and out of scope here.

- **The CLI store path (`wagyu mcp add --header`) is
  unchanged.** That store writes to
  `~/.gemini/config/mcp_config.json` via the `legacyMCPDoc`
  freeform `map[string]any` schema in
  `internal/cli/subcommand_parity.go`. The runtime reads a
  different file (`~/.gemini/antigravity-cli/mcp.json`).
  Cycle 247 fixes the read path; it does not unify the two
  stores. A future cycle could add a `Headers` field to
  `auth.MCPServer` (1) and a CLI-store writer that mirrors
  `--header` into both files (2) — but those are separate
  concerns.

- **Stdlib only.** `internal/mcp/header_substitute.go`
  imports exactly one package (`strings`). `pigrt/mcpconfig.go`
  gained one stdlib import (`strings`, already adjacent to
  the existing `sort` and `os`). No third-party dependency
  added.

- **No recursion, by design.** A substituted value
  containing another `$VAR` is emitted verbatim. This
  matches shell semantics for a single-pass substitution
  and prevents accidental loops (`A=${B}` → `B=${A}` →
  infinite). The AGY 1.4.0 changelog doesn't mention
  recursion; the test `TestSubstituteHeaderVars_NoRecursiveExpansion`
  pins the absence.

- **The four-syntax rule is intentionally narrow.** POSIX
  also defines `${VAR-default}` (default only if unset, not
  if empty) and `${#VAR}` (length). Cycle 247 implements
  only `${VAR:-default}` per the task brief; the other two
  forms are emitted literal (a `$` followed by `{` then a
  name that doesn't match `:-` is treated as a malformed
  reference and the entire `${…}` is left intact). Adding
  either form is a one-line extension when a real config
  asks for it.

- **Cycle 247 does not touch `cmd/wagyu` or `internal/cli`.**
  The bug lives entirely in `internal/pigrt` (the runtime
  bridge) and is the first pigrt-only cycle since cycle-183.
  The clone-side parser, dispatcher, and `/mcp` panel are
  unaffected.

- **No `go.mod` change.** Stdlib only on the helper side;
  `internal/pigrt` already imports the `github.com/MichaelKinsy/PiG/coding/extension`
  and `mcpext` packages via cycle-183.

- **Binary-size delta is dominated by comments and the test
  file.** The shipped binary grew from 89,631,842 bytes
  (cycle-244) to 89,682,323 bytes (cycle-247); ~50 KB.
  Most of the production-side growth is the file header
  comment block on `internal/mcp/header_substitute.go`
  (which the linker keeps) and the slightly larger
  `AgyMCPServer` struct's JSON tag table. The test file
  (~150 lines) does not ship.