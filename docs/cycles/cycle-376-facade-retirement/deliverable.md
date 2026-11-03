# Cycle 376 — the facade shells are retired

## The problem

`docs/guide/architecture-tiers.md` described the tree's own debt:

> 58 top-level `internal/` packages are facades. Each is a `facade.go`
> that re-exports the tier package with `type`/`var`/`const` aliases,
> plus a `facade_test.go` that asserts the aliases still resolve.
>
> **58 facades, 219 files still import one.** The shells are the
> *migration mechanism*, not the destination: the retirements are still
> open.

A facade is an **indirection layer**: every consumer depended on a
shell that re-exported a package one directory away. The shell carried
no behaviour, so the dependency it created was pure coupling — a change
to the tier package's exported surface had to be mirrored into the
shell, and the shell's own test had to keep resolving.

The tier move (cycle 371) created the shells; this cycle closes them.

## What was done

**All 58 shells are gone.** 390 import edges in 274 files were
repointed at the tier packages, and the 58 directories were deleted.

The transformation is **import-only**:

```go
// before
import "github.com/justwasm/pigasus/internal/agystep"

// after
import agystep "github.com/justwasm/pigasus/internal/app/prompt/step"
```

Where the target package's own name already matched the shell's
(145 edges), the import is plain. Where it did not (245 edges), the old
name survives as an explicit **import alias**.

### Why an alias and not a rename

The obvious alternative is to rewrite the call sites too: `agystep.X`
becomes `step.X`, `accounts.X` becomes `core.X`, `plugins.X` becomes
`core.X`. That is what the first attempt did, and it has two problems:

1. **Collisions.** A qualifier rename lands on a name that may already
   be taken in that file. `internal/app/prompt/cumulative` has a local
   variable called `step`; renaming `agystep.` to `step.` shadowed it
   and the file stopped compiling. Every collision needs a hand-picked
   second alias, and the hand-picking does not survive the next move.
2. **Worse names.** `internal/features/plugins/core` imported as `core`
   says nothing at the call site — `core.Plugin` is ambiguous with
   `internal/features/accounts/core`, which is *also* `core`.

The alias avoids both. The qualifier in the source stays the one the
codebase already used (`agystep`, `accounts`, `plugins`), the diff stays
inside the import block, and the alias is the one place where the
generic directory name and the specific package name are reconciled.

The generic names stay generic — `internal/features/accounts/core` is
still package `core` — but every consumer now says `accounts` or
`plugins` at the call site, which is what the reader needs.

## The one semantic difference

One facade was **not** a pure alias. `internal/plugins` re-exported

```go
WatcherPtr = &core.WatcherPtr
```

— a **pointer** to the package's `atomic.Pointer`, where the package
itself exports the value. The consumer
(`internal/cliapp/slash_plugin_local_install.go`) stored through it:

```go
var pluginWatcherPtr = plugins.WatcherPtr   // a copy of a lock
```

Retiring the facade naively turned that into a copy of an
`atomic.Pointer`, and `go vet`'s copylocks check caught it. The
consumer now takes the address explicitly:

```go
var pluginWatcherPtr = &plugins.WatcherPtr
```

which is what the facade's alias meant all along. This is the only
place in the tree where a facade's re-export differed from its target,
and it is worth naming: a shell that is not a pure alias is a shell
that can change behaviour when it is removed.

## What else went with the shells

**`internal/permissions/store` was deleted** (762 lines, with its own
tests). It was the always-allow shared-config store, orphaned when
`cmd/wagyu` was removed. The live implementation is
`internal/remotecontrol/permissions_store_fs.go`
(`FSPermissionStore`), which the remote-control permission dispatcher
actually calls. The package had **no importer anywhere in the tree** —
it was a second implementation of a feature that already existed
somewhere else, which is the worst kind of coupling: two copies, one
live, one invisible.

**Two duplicated testdata directories went with the shells.**
`internal/implicit/testdata` and `internal/modelctx/testdata` were
byte-identical copies of the tier packages' own `testdata`. The
reference-drift lock named the copies, so the lock's artifact paths were
repointed at the live ones.

## What did **not** change

- **The bytes.** No call site changed. No exported name changed. The
  only string that moved is the seatbelt profile's own generator marker
  (`internal/permissions.SeatbeltProfile` →
  `internal/core/policy/permissions.SeatbeltProfile`), which names the
  clone's generator, not the reference's bytes.
- **The reference's surface.** The golden help, the hidden-surface
  inventory and the reference's own commands are untouched — 20 visible
  subcommands, same as before.
- **The tier structure.** The shells were *at* the top level; retiring
  them removed packages from the top level. It did not move anything
  into a tier.

## Verification

```
$ go build ./cmd/... ./internal/... ./agentlink/... ./endpoints/... ./configdir/...
$ go test ./cmd/... ./internal/... ./endpoints/... ./configdir/...
```

Green, except two failures that are **not** this cycle's:

- `internal/cliapp` `TestModelPicker_KeepsConfiguredModels` — the other
  agent's model-picker work, failing before this cycle started.
- `internal/referencedrift` `TestReferenceLock_NoUnacknowledgedDrift` —
  the live `agy` binary moved to 1.4.12 while the clone is pinned to
  1.4.11. Verified at HEAD before the cycle started.

Live, after the retirement:

```
$ /tmp/pigasus --print "reply with the single word: printed"
printed

$ /tmp/pigasus server --port 7799 --token smoketoken --workspace /tmp
$ go run ./devutils/agentsmoke -server ws://127.0.0.1:7799 -token smoketoken -prompt "reply with the single word: moved"
moved
```

## The rules this cycle added

1. **A move that renames a package's qualifier does it by import
   alias**, not by rewriting the call sites. The diff stays inside the
   import block, and a qualifier can never collide with a local
   identifier.
2. **A facade must be a pure alias.** The one that was not —
   `WatcherPtr = &core.WatcherPtr` — changed behaviour when it was
   removed. A shell that re-exports a pointer where its target exports
   a value is not a shell.
3. **A package with no importer is not a second implementation.** It is
   either dead code or an invisible copy of something that already
   exists. Both are worth deleting.

## 硬约束 / 存疑 / limitations

- **硬约束**: no facade remains. `find internal -name 'facade*.go'`
  returns nothing, and no file imports any of the 58 retired paths.
- **硬约束**: the retired paths do not resolve. A third party importing
  `internal/accounts` gets a build error, not a silent behaviour
  change.
- **硬约束**: no call site changed. The only edits outside the import
  blocks are the `pluginWatcherPtr` address, the two testdata fixture
  paths, and prose.
- **存疑**: the 245 import aliases are a deliberate choice, not a
  neutral one. The alternative — renaming the three `core` packages to
  `accounts`, `plugins` and `subagent`, and shortening the other
  aliases to the target's own name — would leave fewer aliases and
  better package names, at the cost of a much larger diff. That is a
  separate cycle, and it should be one: the package names `core`,
  `step`, `row` and `wrap` are the reason the shells existed.
- **存疑**: `internal/permissions/store` was deleted rather than moved
  into `internal/core/policy/permissions/store`. It had no importer,
  and the live always-allow path is
  `internal/remotecontrol/permissions_store_fs.go`. If the two ever
  turn out to have diverged, the deleted package's tests were the only
  record of the old shape — they are in this commit's history.
- **limitations**: the retirement was scripted, and the script is not
  in the tree. The transformation is uniform enough to be mechanical
  (import path + alias, then delete the shell), and the compiler plus
  the suite are the check; a test that asserted "there is no facade"
  would be a test of the directory layout, not of behaviour.
- **limitations**: the prose was repointed mechanically too, and the
  mechanical pass cannot tell a *historical* mention from a *current*
  one. The cycle reports under `docs/cycles/` were left alone — they
  are the evidence of what was true when they were written, and the
  guide is the current state.
