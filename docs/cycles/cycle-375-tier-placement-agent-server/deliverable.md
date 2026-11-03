# Cycle 375 — the client/server runtime moves into its tier

## The problem

Cycle 374 added the client/server split: the `agentlink` wire, the turn
seam (`internal/agentturn`) and the agent server (`internal/agentserver`).
Both internal packages landed at the **top level** of `internal/`, and
`docs/guide/architecture-tiers.md` rule 1 says:

> **Put the new package in its tier**, by dependency: a package that depends
> on nothing above it is tier 1, one that orchestrates is tier 2, and so on.

Neither package has a facade — they are new, so rule 2 (no facade for a new
package) is satisfied — but they were also **not entered in the ledger** of
the packages that stayed at the top level. That table is the record of the
*justified* exceptions; a package that is neither in a tier nor in the table
has not been placed at all.

So the split had added a real architectural seam and had not paid for it in
the tree's own vocabulary.

## The placement

| package | was | is | why |
|---|---|---|---|
| the turn seam | `internal/agentturn/` | `internal/app/agentturn/` | tier 2 — it orchestrates a turn over `internal/agent` and the transport, and it depends on nothing above tier 2 |
| the host | `internal/agentserver/` | `internal/app/agentserver/` | tier 2 — it orchestrates a session; tier 4 (`internal/infra/`) would have been an **upward** dependency, because the server needs the turn seam |
| the session's shared cores | `internal/printmode/` | `internal/app/session/setup/` | see below |

The tier choice is by dependency, and it is worth spelling out because tier
4 looked plausible at first: `pigasus server` is a *process*, and
`internal/infra/` is where the daemon lives. But the server depends on the
turn seam (`internal/app/agentturn`), which is tier 2 — so a tier-4 package
would reach **up**. Tier 2 is the honest answer: the server orchestrates a
session the way the other orchestrators do.

## The upward dependency that had to go first

`internal/agentserver` imported `internal/printmode` for four things:

```go
printmode.InstallMCPProvider(auth.AntigravityHome())
printmode.PolicyFrom(s.cfg.PermissionMode, s.cfg.SkipPermissions)
printmode.SandboxFrom(s.cfg.SkipPermissions, workspace, conversationID)
opts.Subagents = append(opts.Subagents, printmode.CustomSubagents()...)
```

`internal/printmode` is an **entry surface** — the headless print path. An
entry surface is *above* a tier package, so a tier package that imports one
inverts the layering. (The server cannot import `internal/cliapp` either:
that one is a plain import cycle.)

The four functions are not print mode's at all — they are the questions
*every* host answers before a turn can run: which permission policy, which
terminal sandbox, which subagents, which MCP servers. They were lifted out
of `internal/cliapp` in cycle 374 and parked in `internal/printmode` because
that was the nearest shared home; the correct home is **below every host**.

So they now live in `internal/app/session/setup`:

- `PolicyFrom(mode, skipPermissions)`
- `SandboxFrom(skipPermissions, workspace, conversationID)`
- `CustomSubagents()`
- `InstallMCPProvider(agentDir)`

and `internal/printmode` keeps only what is print mode's own — the flag
reading (`PrintModePolicy`, `PrintModeSandbox`) and the headless surface.

**No facade.** The names moved; the callers moved with them, in the same
commit. `internal/printmode` does not re-export them, because a second name
for one thing is what the guide's rule 2 forbids.

## What did **not** change

- **The bytes.** The four cores moved verbatim. The policy's notice still
  goes to `log.New(io.Discard, …)` — the print mode's silent logger, which
  was never reassigned — so the print path's stdout and stderr are byte for
  byte what they were. A `log.New(os.Stderr, …)` would have looked like a
  tidier carry-over and would have changed the print path's output.
- **The wire.** `agentlink/` is a public package and stays where it is; it
  has no `internal/` dependency and never had.
- **The reference's surface.** The golden help, the hidden-surface
  inventory and the reference's own commands are untouched — this cycle
  moved packages, it did not add or remove a command.
- **The behaviour.** The turn seam still runs the local turn over
  `internal/agent.Loop` with the same `agent.Options`, and the server still
  hosts the same session.

## Verification

```
$ go build ./cmd/... ./internal/... ./agentlink/... ./endpoints/... ./configdir/...
$ go test ./cmd/... ./internal/... ./endpoints/... ./configdir/...
```

Green, except the pre-existing and unrelated
`internal/cliapp` `TestModelPicker_KeepsConfiguredModels` (the model
picker's own work).

Live, after the move:

```
$ /tmp/pigasus server --port 7799 --token smoketoken --workspace /tmp
pigasus server listening on ws://127.0.0.1:7799

$ go run ./devutils/agentsmoke -server ws://127.0.0.1:7799 -token smoketoken -prompt "reply with the single word: moved"
server 1.4.11
  session:   a8d480ac-0e2f-286d-fa5a-dcf2c654a29d
moved
[usage] 14028 in / 1 out
```

and the headless path, whose policy and sandbox now come from `setup`:

```
$ /tmp/pigasus --print "reply with the single word: printed"
printed
```

## The rules this cycle added

1. **A host never imports another host.** The three hosts — the TUI, the
   print path and the agent server — share through
   `internal/app/session/setup`, which is below all three.
2. **A new package is placed in a tier or it is entered in the ledger.**
   There is no third state.

## 硬约束 / 存疑 / limitations

- **硬约束**: no facade was written for either moved package, and the
  callers moved in the same commit. The old paths
  (`internal/agentturn`, `internal/agentserver`) do not exist any more.
- **硬约束**: `internal/printmode` no longer exports `PolicyFrom`,
  `SandboxFrom`, `CustomSubagents` or `InstallMCPProvider`. Anything that
  imported them from there now imports `internal/app/session/setup`.
- **硬约束**: the policy's notice still goes to `io.Discard`. Making it
  visible is a behaviour change and needs its own cycle.
- **存疑**: `internal/app/session/setup` is a small package with four
  functions. If a fifth host appears, or if the cores grow, the package
  should be split by concern (policy, sandbox, subagents, MCP) — the
  directory name is the one thing that would have to change.
- **存疑**: `internal/agentserver` still depends on `internal/cli` for one
  constant (`cli.ReferenceVersion`). That is accepted: tier packages already
  import `internal/cli` (`internal/core/registry`,
  `internal/features/models/catalog`, `internal/infra/backend/select`), and
  `internal/cli` is a top-level package outside the tier ordering.
- **limitations**: the move was mechanical apart from the `setup` extraction.
  No test was added for the placement itself — a test cannot assert a
  directory's address. The rule is therefore carried by the guide
  (`docs/guide/architecture-tiers.md`, rules 1 and 5) and by this report.
