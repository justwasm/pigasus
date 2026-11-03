# Cycle 377 — the shell retirement: eleven more

## The problem

Cycle 376 retired 58 shells and closed with a rule:

> a package under `internal/` that has a `facade.go` is a shell.

That rule is about a **filename**. The thing it was trying to name — a
package that holds no behaviour of its own — can be spelled any way at
all, and eleven of them were.

| shell | file | re-exports |
|---|---|---|
| `internal/branding` | `brand.go` | `internal/app/prompt/branding` |
| `internal/autocont` | `autocont.go` | `internal/app/session/autocont` |
| `internal/interaction` | `interaction.go` | `internal/core/seam` |
| `internal/workspacetrust` | `prompt.go` | `internal/core/policy/trust` |
| `internal/modular/engines/stream` | `stream.go` | `internal/app/engine/stream` |
| `internal/modular/subsystems/daemon` | `daemon.go` | `internal/infra/subsystems/daemon` |
| `internal/modular/subsystems/localharness` | `localharness.go` | `internal/infra/subsystems/localharness` |
| `internal/modular/subsystems/remotecontrol` | `remotecontrol.go` | `internal/infra/subsystems/remotecontrol` |
| `internal/modular/subsystems/terminal` | `terminal.go` | `internal/infra/subsystems/terminal` |
| `internal/modular/toolsets/native` | `native.go` | `internal/features/tools/native` |
| `internal/modular/toolsets/wasm` | `wasm.go` | `internal/features/tools/wasm` |

Four of them were *documented as facades* — `internal/branding`'s own doc
comment said "backward-compatible facade over internal/app/prompt/branding",
`internal/workspacetrust`'s said "Deprecated: … a backward-compatible facade
for internal/core/policy/trust". The rule was not "the filename says so";
the rule was always "it holds no behaviour". The filename was just the
shortcut, and eleven packages stepped around it.

## What was done

**All eleven are retired.** 35 import edges in 31 files now name the
implementation directly, and the eleven directories are gone.

The transformation is import-only, exactly as in cycle 376:

```go
// before
import "github.com/justwasm/pigasus/internal/interaction"

// after
import interaction "github.com/justwasm/pigasus/internal/core/seam"
```

Where the target package's own name already matched the shell's, the
import is plain. Where it did not, the old name survives as an explicit
alias — `interaction` for `internal/core/seam`, `workspacetrust` for
`internal/core/policy/trust`.

**The rule is now stated by shape.** A package is a shell iff its whole
surface is

- aliases (`type X = other.X`, `var X = other.X`, `const X = other.X`), and
- forwarding functions whose bodies are nothing but a call into one other
  package.

Nothing else. A shell with a literal in it, a `defer`, a `struct` or a
second import is a package with behaviour, and it is not a shell.

## The `internal/modular` shims

Seven of the eleven lived under `internal/modular/`, and the guide
explained them as:

> The `subcommands/*` and `subsystems/*` directories are blank-import
> shims over the tier packages (`internal/ui/cli/subcommands/*`,
> `internal/infra/subsystems/*`); they exist so the registration happens
> by import, and `cmd/pigga` (the WASM build) relies on that.

**That was stale.** `cmd/pigga` imports the tier packages directly:

```go
_ "github.com/justwasm/pigasus/internal/app/engine/stream"
_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/accounts"
```

and the registration is done by the tier package's own `init()`
(`internal/infra/subsystems/daemon` has `func init() { registry.RegisterSubsystem(&Subsystem{}) }`).
The shims added a hop and added no registration.

The only importers the shims still had were three **tests that went
through the shell instead of the package they were checking**:

| test | imported | should have imported |
|---|---|---|
| `internal/app/engine/stream/stream_test.go` | `internal/modular/engines/stream` | `internal/app/engine/stream` |
| `internal/features/tools/native/native_test.go` | `internal/modular/toolsets/native` | `internal/features/tools/native` |
| `internal/features/tools/wasm/wasm_test.go` | `internal/modular/toolsets/wasm` | `internal/features/tools/wasm` |

Each of them was a test of the *implementation* that reached it through
the shell — which means the implementation was never tested directly.
A test that imports a shell is a test of the shell.

The four `internal/modular/subsystems/*` shims had **no importer at all**
beyond one shared test file, which blank-imported them to assert that
the subsystems were registered. That test moved to
`internal/modular/subsystems_test.go` and now blank-imports the four tier
packages.

## What did **not** change

- **No call site changed.** The only edits outside the import blocks are
  the `package subsystems_test` → `package modular_test` rename in the
  moved test file.
- **No exported name changed.** The `internal/interaction` and
  `internal/workspacetrust` surfaces survive under their own names, via
  the aliases.
- **No behaviour changed.** The `internal/plugins` pointer case (cycle
  376, invariant 66) had no second instance: a scan of every shell for a
  re-export that was not an alias found exactly one, and it is gone.

## Verification

```
$ go build ./cmd/... ./internal/... ./agentlink/... ./endpoints/... ./configdir/...
$ go test ./cmd/... ./internal/... ./endpoints/... ./configdir/...
```

Green, except six failures in `internal/cliapp` that are **not this
cycle's**: five are the 1.4.12 changelog fixture's (`TestPanelChangelogLayout`,
`TestPanelChangelogViewerScrolls`, `TestChangelogSource_LoaderFindsReference`,
`TestChangelogPagination_RendersDoc`, `TestChangelogPagination_CtrlHome`) and
one is the model picker's (`TestModelPicker_KeepsConfiguredModels`). All six
fail identically at HEAD without this cycle's changes — verified in a
separate worktree.

## The rules this cycle added

1. **A shell is a shape, not a filename.** A package whose whole surface
   is aliases and forwarding calls into one other package is a shell,
   whatever its file is called.
2. **A test that imports a shell is a test of the shell.** The
   implementation is then untested.

## 硬约束 / 存疑 / limitations

- **硬约束**: no shell remains. The shape test over `internal/` finds
  none, and no file imports any of the eleven retired paths.
- **硬约束**: the retired paths do not resolve. A third party importing
  `internal/interaction` gets a build error, not a silent behaviour
  change.
- **硬约束**: the `internal/modular` registration surface is unchanged.
  `cmd/pigga` still registers the stream engine and the subcommands, and
  `internal/modular/subsystems_test.go` still asserts that the four
  subsystems register.
- **存疑**: the shape test is a heuristic, and a heuristic that finds 69
  packages is a heuristic that could find a *real* package with an
  unusual shape. The two guards are that the test requires **exactly one
  internal import** and **exactly one non-test file**, and that every
  candidate was read by hand before it was retired. A shell with two
  forwarding functions and one literal in it would not be caught — which
  is fine, because that is a package with behaviour.
- **存疑**: `internal/modular/subsystems/*` was described in the guide as
  a deliberate mechanism ("they exist so the registration happens by
  import"). The description was wrong — `cmd/pigga` never imported them —
  but the *intent* behind the sentence was real. If a future build needs
  the `internal/modular/*` shape back, it should be rebuilt as a real
  registration package with its own `init()`, not as a re-export.
- **limitations**: the shell detector is not in the tree. It is a
  migration tool, and a permanent one would be a test of the directory
  layout rather than of behaviour — but the *rule* is now in the guide
  and in `AGENTS.md`, which is where a rule belongs.
- **limitations**: eleven shells is a small number next to cycle 376's
  58. The finding that matters is not the count but the reason: the
  first cycle's rule was written against a filename, so it could only
  find what was named after itself.
