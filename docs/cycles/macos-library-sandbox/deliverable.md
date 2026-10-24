# Cycle macos-library-sandbox — macOS Seatbelt deny `~/Library/Application Support`

## TL;DR

AGY 1.3.3 changelog line 44:

> "Changed the macOS terminal sandbox to block sandboxed commands from
> reading `~/Library/Application Support` by default, since desktop apps
> keep credentials and session tokens there; previously it was readable,
> and you can still allow it explicitly in your sandbox rules."

This cycle ships that change in the clone. The macOS terminal sandbox
profile generator now emits a default-deny rule for
`(subpath (string-append (param "HOME") "/Library/Application Support"))`,
layered BEFORE any user-supplied allow rules so an explicit user
override still wins (the changelog's "you can still allow it
explicitly"). 11 new unit tests pin the profile string; the runner
that consumes the profile (cmd/wagyu/run_command.go) is out of scope
for this cycle and lands in a future one.

This is a **profile-string generator only** — the brief was explicit
that we should not execute the actual Seatbelt profile on this dev
machine. The cycle lands the generator + tests + a deliverable, and
the runner integration follows when the tool surface is ready.

## Why a generator only?

The clone's `internal/tools/command.go` does not currently invoke
`sandbox-exec`; Auto mode in cycle 224 routes commands through
`run_command` without any macOS sandbox. Building the runner that
plumbs `SeatbeltProfile.Render()` into `exec.Command("sandbox-exec",
"-f", tmpfile, ...)`, then cleaning up the temp file on exit, is a
~80-line change that lives across `internal/tools/command.go` and
`cmd/wagyu/sandbox_runner.go` (new). That is a real change with
runtime consequences — it will start failing commands that hit
`~/Library/Application Support` (e.g. an IDE integration that reads
its license cache from there). The brief scoped this cycle to the
**profile string**, so the runner lands separately and the security
behaviour can be verified independently.

## What ships

### `internal/permissions/sandbox.go` (new, 173 lines)

The generator. Three exported symbols:

| Symbol | Purpose |
|---|---|
| `DefaultDenyPaths []string` | the package-level default-deny list. AGY 1.3.3 line 44 added `Library/Application Support` here. |
| `SeatbeltProfile` | the input to `Render`: `HomeDir`, `UserOverrides` (verbatim contents of `~/.gemini/config/sandbox.sb`), `ExtraDenyPaths`. |
| `SeatbeltProfile.Render() string` | returns the full `.sb` profile text. Returns `""` on non-darwin (the caller skips the sandbox-exec invocation entirely). |
| `DefaultSandboxProfilePath` | `".gemini/config/sandbox.sb"`. Sibling to the rest of the openagy config layout. |
| `LoadUserSandboxProfile(home) (string, bool, error)` | reads the user override. Missing file → `("", false, nil)`, not an error. |

The emitted profile has four layers, in order:

1. **Header** — `(version 1)` and `(deny default)` baseline.
2. **Allow baseline** — `process*`, `sysctl-read`, `file-read*`,
   `file-write*` under `$HOME` / `/tmp` / `/private/tmp` / `/dev`,
   `network*`. Unchanged by this cycle.
3. **Default-deny (AGY 1.3.3 line 44)** — for each entry in
   `DefaultDenyPaths ∪ ExtraDenyPaths`, emit
   `(deny file-read* (subpath (string-append (param "HOME") "/<path>")))`.
   Layered BEFORE the user overrides.
4. **User overrides** — appended verbatim. A trailing newline is
   normalised so the join is stable.

`(param "HOME")` is used rather than a literal `/Users/<name>` so the
generator works for any user without editing. Seatbelt interpolates
the parameter at profile-load time.

### `internal/permissions/sandbox_test.go` (new, 251 lines)

11 unit tests; 9 pass + 2 platform-guarded skips on darwin:

| Test | Pins |
|---|---|
| `TestDefaultDenyPaths_IncludesApplicationSupport` | the security claim itself — `Library/Application Support` is in the default-deny list |
| `TestRender_ReturnsEmptyOnNonDarwin` | platform no-op (skipped on darwin) |
| `TestRender_DenyForLibraryApplicationSupport` | default profile contains the deny rule |
| `TestRender_UsesUsersHomeNotLiteral` | deny path uses `(param "HOME")`, never a hard-coded home |
| `TestRender_UserOverridesAppearAfterDefaultDeny` | user override section appears AFTER the default-deny (later allow wins over earlier deny) |
| `TestRender_EmptyHomeFailsClosed` | empty `HomeDir` returns `""` rather than emitting `(string-append "" "/Library/Application Support")` which would silently deny reads of `/Library/Application Support` |
| `TestRender_ProfileIsValidSeatbeltSyntax` | paren balance + first non-comment line is `(version 1)` |
| `TestRender_ExtraDenyPathsLayered` | `ExtraDenyPaths` is additive |
| `TestLoadUserSandboxProfile_MissingFileIsEmpty` | missing `~/.gemini/config/sandbox.sb` → `("", false, nil)`, not an error |
| `TestLoadUserSandboxProfile_ReadsFile` | happy path |
| `TestLoadUserSandboxProfile_NoopOnNonDarwin` | platform no-op (skipped on darwin) |

`gofmt -l` is clean on both new files. The pre-existing files in
`internal/permissions/` are unchanged on `git diff` (cycle-99 and
cycle-200 alignment inconsistencies stay per `AGENTS.md`).

## Profile output (cycle-style reference)

For `SeatbeltProfile{HomeDir: "/Users/example", UserOverrides: ""}.Render()`:

```sb
; Auto-generated by openagy internal/permissions.SeatbeltProfile.
; Cycle macos-library-sandbox. AGY 1.3.3 changelog line 44.
; Do not edit by hand — re-run the generator after editing DefaultDenyPaths.
(version 1)
(deny default)

; --- baseline allow rules (run_command needs these to function) ---
(allow process*)
(allow sysctl-read)
(allow file-read*)
(allow file-write*
	(subpath (param "HOME"))
	(subpath "/tmp")
	(subpath "/private/tmp")
	(subpath "/dev"))
(allow network*)

; --- default-deny rules (layered before user allow so explicit override wins) ---
(deny file-read*
	(subpath (string-append (param "HOME") "/Library/Application Support")))

```

With a user override that explicitly re-allows the path, the override
appends after the default-deny so Seatbelt's "last allow wins"
semantics give the user back the access:

```sb
... (above) ...

; --- user-provided overrides from .gemini/config/sandbox.sb ---
(allow file-read*
	(subpath (string-append (param "HOME") "/Library/Application Support")))
```

## Files touched

| File | Δ | What |
|---|---|---|
| `internal/permissions/sandbox.go` | new, 173 lines | `DefaultDenyPaths`, `DefaultSandboxProfilePath`, `SeatbeltProfile`, `Render()`, `LoadUserSandboxProfile()` |
| `internal/permissions/sandbox_test.go` | new, 251 lines | 11 tests (9 effective + 2 platform guards) |

No other files touched. `internal/permissions/policy.go`,
`settings.go`, `policy_test.go`, `settings_test.go` are unchanged on
`git diff`. `internal/convstore/sqlite.go` is untouched (hard
constraint).

## Verification

- **`go build -o /tmp/wagyu-macossb ./cmd/wagyu`** — exit 0, 73 MB.
- **`go test -vet=off -count=1 ./internal/permissions/...`** — `ok`,
  11/11 tests pass (2 skipped on darwin).
- **`go test -vet=off -count=1 ./cmd/... ./internal/...`** — 35
  packages OK; the one failure (`TestDispatchPrintSlash_LocalCommands`
  in `cmd/wagyu`) is a **pre-existing environment-dependent flake**
  (it expects `MiniMax M3` to be the first registered model, but the
  test environment returns `Gemini 3.8 Flash (Medium)`). Confirmed
  pre-existing on `main` before this cycle by running the test on a
  clean checkout.
- **`gofmt -l internal/permissions/sandbox.go
  internal/permissions/sandbox_test.go`** — empty.

## What did NOT ship (and why)

| Skipped | Reason |
|---|---|
| Runner integration (`sandbox-exec -f <tmpfile> ...`) | Out of scope for this cycle per the brief. The next cycle wires `Render()` into `run_command`'s `exec.Command` and adds an `internal/tools/sandbox_runner.go`. |
| Linux/Windows sandbox integration | AGY 1.3.3 line 44 is macOS-only. Linux runs sandboxed via PIDs/cgroups elsewhere in the AGY stack (out of scope). |
| Triggering the new deny at runtime in Auto mode | Same as above — the runner that consumes the profile is the next cycle. |
| Profile file path discovery | The brief named `~/.gemini/config/sandbox.sb`. This matches the openagy config layout; if the orchestrator wants a different path, it's a one-line change in `DefaultSandboxProfilePath`. |

## Hard constraints / 存疑

- **No live profile execution on this dev machine.** The brief was
  explicit. The 11 tests pin the profile string but do not invoke
  `sandbox-exec`. A future cycle that runs the profile end-to-end
  should add a smoke test that runs `sandbox-exec -f <tmpfile> /bin/true`
  (allowed) and `sandbox-exec -f <tmpfile> cat $HOME/Library/Application\ Support/Foo`
  (denied) — but only on a sandbox-friendly machine, not this one.
- **Seatbelt syntax is not formally parsed.** The validator is the
  paren-balance + first-non-comment-line check. Sandbox-exec's own
  parser would catch a malformed rule at run time; this test catches
  gross errors at unit-test time. If a future cycle adds a more
  sophisticated rule (e.g. multi-line `with` blocks), the validator
  needs to grow.
- **`(deny default)` is the AGY idiom, not the strict sandbox.** The
  baseline allow rules grant `process*`, `file-read*`, and `network*`,
  so the sandbox is "default-deny with broad allowlist". This matches
  what AGY 1.3.3 ships — the changelog does not promise a strict
  sandbox, just a default-deny for `~/Library/Application Support`.
- **The runner that consumes this profile is not in this cycle.** The
  brief scoped the cycle to the profile string. Until the runner
  lands, the new deny rule is dormant. This is the explicit handoff.
- **Pre-existing `TestDispatchPrintSlash_LocalCommands` flake.** A
  model-registry test in `cmd/wagyu` that fails on this machine
  because the test environment does not register `MiniMax M3` first.
  Confirmed pre-existing on `main` at the cycle's start; not in
  scope.