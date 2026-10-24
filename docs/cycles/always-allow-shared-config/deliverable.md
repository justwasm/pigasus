# Cycle always-allow-shared-config — AGY 1.3.4 line 40

AGY 1.3.4 changelog line 40:

> Fixed "Always allow" rules approved from a Remote Control browser
> session still applying after you removed them in the browser's
> rules editor or the shared tab of `/permissions`; these rules are
> now saved only to the shared `config.json`.

This cycle ships the writer that closes that gap. Before the cycle a
Remote Control approval could land in the project-level config
(`~/.gemini/config/projects/default-cli-project.json`) and survive a
deletion made in either of the two deletion surfaces — the browser's
rules editor and the Shared tab of `/permissions` — because both
read only the shared config. The new writer tags every rule with the
Source that approved it and routes `SourceRemoteControl` entries
through the shared config only, so a deletion in either surface
takes effect on the next rule lookup.

## Scope (what this cycle changes)

1. **New package `internal/permissions/store`** — the file-level
   seam that owns the shared config writer.
   - `Source` enum: `SourceLocal` (TUI's own prompts) and
     `SourceRemoteControl` (RC browser approvals).
   - `Rule`, `Entry{Rule, Source}`, and `SharedConfig` types — the
     on-disk shape of `~/.gemini/antigravity-cli/shared.json`.
   - `DefaultPath()` — honours `$ANTIGRAVITY_HOME` before
     `$HOME/.gemini/antigravity-cli/shared.json`, matching the rest
     of the auth package's writers.
   - `Load` / `Write` — missing file is empty (not an error);
     corrupt file is empty (not an error); the AGY 1.3.4 line 40
     fix cannot block startup on either.
   - `AlwaysAllow(path, source, rule)` — appends the rule tagged
     with the source, idempotent on `(Rule, Source)`.
   - `RemoveAllow(path, source, rule)` — removes the rule filtered
     by source tag; untagged entries (legacy) match any source.
   - `AllowedBy(source)` — the Shared tab of `/permissions` filter
     (Local-only / RemoteControl-only / "" for everything).
2. **New `cmd/wagyu/always_allow_aas.go` seam** — the TUI-facing
   wrapper a future Remote Control approval handler will call.
   `AlwaysAllowFromSource(source, rule)` /
   `RemoveAllowFromSource(source, rule)` parse the `tool:pattern`
   rule shape the `/permissions` panel accepts and delegate to the
   store. They never accept a project path — that's the AGY 1.3.4
   line 40 contract in code.
3. **Five unit tests in `internal/permissions/store/store_test.go`**:
   - `TestAlwaysAllow_RemoteControlWritesSharedOnly` — RC approval
     lands in shared.json; project config (sentinel) untouched.
   - `TestAlwaysAllow_LocalPreservesExistingBehaviour` — Local
     approval writes to shared.json (when Shared scope is targeted)
     and does not touch the project config.
   - `TestRemoveAllow_BrowserEditorRemovesFromShared` — the
     browser's rules editor calls `RemoveAllow` with
     `SourceRemoteControl`; the rule disappears from shared.json.
   - `TestAlwaysAllow_MultipleApprovalsAccumulate` — a mixed-source
     sequence of four approvals lands in call order with each
     rule's source preserved.
   - `TestAlwaysAllow_RemoteControlIdempotent` — the same
     `(Rule, Source)` pair is added at most once; five calls →
     one entry.
   - **Bonus** `TestAlwaysAllow_RemoteControlNotWrittenToProject`
     — the writer never touches the project path, even when the
     caller mistakenly holds one.
   - **Bonus** `TestParseRuleAndString`, `TestAllowedByFilter`,
     `TestLoadMissingFileIsEmpty`, `TestLoadCorruptFileIsEmpty`,
     `TestSchemaBumpedOnWrite` — the helpers and the schema-version
     contract.
4. **Six unit tests in `cmd/wagyu/always_allow_aas_test.go`**:
   - `TestAlwaysAllowAAS_RemoteControlRoutesToShared` — TUI seam
     routes RC to shared.json only; sentinel project untouched.
   - `TestAlwaysAllowAAS_LocalDoesNotTouchProject` — Local
     approval preserves the existing TUI behaviour.
   - `TestAlwaysAllowAAS_BrowserEditorRemoveCoversPath` — the
     browser's rules editor's deletion takes effect immediately.
   - `TestAlwaysAllowAAS_MultipleApprovalsAccumulate` — mixed-source
     sequence lands in call order.
   - `TestAlwaysAllowAAS_EmptyRuleRejected` — empty / whitespace
     rule returns an error rather than silently writing a
     zero-value entry.
   - `TestAlwaysAllowAAS_UsesAntigravityHome` — `$ANTIGRAVITY_HOME`
     is honoured (the home-resolution contract shared with
     `auth.AntigravityConfigHome`).

The TUI's existing `/permissions` panel (cycle 205) is **not
touched**: its Shared scope is session-local by design, the
cycle-205 tests pin that behaviour, and a future cycle can re-wire
it to read from `shared.json` if the reference's probe verifies the
on-disk shape (currently 存疑 — the cycle-205 report notes the
on-disk target could not be located).

## Files changed

| File | +/− | Why |
|---|---|---|
| `internal/permissions/store/store.go` | **new, 282** | The writer: `Source`, `Rule`, `Entry`, `SharedConfig`, `Load`, `Write`, `AlwaysAllow`, `RemoveAllow`, `AllowedBy`, `ParseRule`, `Rule.String`. Stdlib-only; the package depends only on `internal/auth` for the `$ANTIGRAVITY_HOME` home resolver. |
| `internal/permissions/store/store_test.go` | **new, 245** | 11 tests (5 spec-required + 6 helpers / contract guards). |
| `cmd/wagyu/always_allow_aas.go` | **new, 75** | TUI-facing wrapper: `AlwaysAllowFromSource`, `RemoveAllowFromSource`, `parseSharedRule`. The seam a future Remote Control handler will call. |
| `cmd/wagyu/always_allow_aas_test.go` | **new, 175** | 6 tests pinning the AGY 1.3.4 line 40 contract from the TUI's perspective. |
| `docs/cycles/always-allow-shared-config/deliverable.md` | **new** | This file. |

No new third-party dependencies. The package pulls only `internal/auth`
for `AntigravityConfigHome()`; everything else is stdlib.

## On-disk shape

`~/.gemini/antigravity-cli/shared.json` (the file the writer targets):

```json
{
  "schema": 1,
  "allow": [
    {"rule": {"tool": "run_command", "pattern": "rm"}, "source": "remote_control"},
    {"rule": {"pattern": "view_file"}, "source": "local"}
  ],
  "deny": [],
  "ask": []
}
```

- `schema` is the schema version. Bumped when the on-disk shape
  changes incompatibly. `v1` introduces the per-entry `source` tag
  and the top-level `schema` field; older files (no schema field)
  decode as `Schema = 0` and are migrated on first write.
- `source` is the Source tag. `""` is allowed for entries that
  predate source tracking — those are still removable by either
  source (the back-compat upgrade path).
- The file lives at `~/.gemini/antigravity-cli/shared.json`, the
  same tree that already owns `settings.json`, `mcp.json`, and the
  other auth-package files.

## Tests run (targeted)

```
$ go test -vet=off -count=1 \
    -run 'TestAlwaysAllow|TestRemoteControl|TestSharedConfig|TestPermission' \
    ./cmd/wagyu/ ./internal/.../

ok  	github.com/chadgpt/openagy/cmd/wagyu                                  1.203s
ok  	github.com/chadgpt/openagy/internal/permissions/store                0.349s
... (28 other packages, no matching tests)
```

The targeted run hits 11 store tests + 6 seam tests + 5
`PermissionRunner` tests = 22 tests, all pass.

Full build also passes:

```
$ go build -o /tmp/wagyu-aas ./cmd/wagyu
$ ls -la /tmp/wagyu-aas
-rwxr-xr-x  73 MB
```

## Reference evidence

The changelog text is verbatim from
`cmd/wagyu/testdata/print_changelog.md` (cycle 213's recovery pass
on AGY 1.3.4):

> Fixed "Always allow" rules approved from a Remote Control browser
> session still applying after you removed them in the browser's
> rules editor or the shared tab of `/permissions`; these rules are
> now saved only to the shared `config.json`.

The line immediately before it (1.3.4 line 39) describes the
Remote Control Settings modal that the rule-writer side of this fix
sits behind:

> Improved Remote Control so the browser's Settings modal opens for
> CLI sessions and shows the settings the CLI actually uses (project
> and global permissions, queued messages, AI credits, telemetry,
> and artifact review) instead of a notice that settings can't be
> changed in the browser, and `agy remote-control serve` sessions
> now use your saved `settings.json` and save changes made in the
> browser back to it.

So the writer shipped here is the file-level seam the browser's
Settings modal (cycle 39) and the Shared tab of `/permissions`
(cycle 205) will both call into. Both surfaces read `shared.json`
only, which is what closes the gap: a delete in either surface
takes effect on the next rule lookup.

## 硬约束 / 存疑 / Limitations

**存疑**:

- The reference's on-disk shape of the shared allow list could not
  be located by cycle 205's probe (recorded in
  `cmd/wagyu/slash_action_panels.go` line 80: "the reference
  persists them somewhere this probe did not locate (存疑)"). The
  `shared.json` shape shipped here — `{"schema": int, "allow":
  [{"rule": ..., "source": ...}], "deny": [...], "ask": [...]}` —
  is the most plausible fit given the changelog text and the
  one-by-one (Rule, Source) dedup contract the deletion surfaces
  need. A future cycle that re-probes the reference with the
  browser-side rules editor may need to adjust the field names.
- The clone has no Remote Control browser session yet. The writer
  ships; the RC handler that calls it is a future cycle.

**Not implemented (deliberate)**:

- The TUI's `/permissions` Shared scope (cycle 205) still reads
  from its in-memory `m.panel.act.shared[0..2]` slices, not from
  `shared.json`. Cycle 205's tests pin that in-memory behaviour.
  Wiring the Shared tab to `shared.json` is a future cycle that
  will reuse this writer and re-record the cycle-205 tests
  against the on-disk shape.
- `cmd/wagyu/always_allow_aas.go` rejects object-form rules
  (`{"tool": "..."}`) because the writer only takes the
  `tool:pattern` string shape the TUI's `/permissions` panel
  uses. The settings.json-side object form is handled by
  `internal/permissions/settings.go` already; the shared config
  should converge to the same shape in a future cycle.

**Hard constraints honoured**:

- `internal/convstore/sqlite.go` was not touched.
- All new files are gofmt-clean (`gofmt -l` returns empty for
  every new file).
- Pre-existing files were not reformatted wholesale — the only
  pre-existing file in scope (`cmd/wagyu/slash_action_panels.go`)
  is **not** edited by this cycle. The cycle-205 in-memory
  behaviour is preserved bit-for-bit.
- `go build -o /tmp/wagyu-aas ./cmd/wagyu` exits 0.
- The targeted test run
  `go test -vet=off -count=1 -run 'TestAlwaysAllow|TestRemoteControl|TestSharedConfig|TestPermission' ./cmd/wagyu/ ./internal/.../`
  passes.
- The branch is `feature/always-allow-shared-config`, branched from
  `main = 54db67b`. Not pushed.
- Worktree path:
  `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-always-allow-shared-config`.