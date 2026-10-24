# Wrapper merge — the `agy` launcher becomes part of the clone

**Cycle:** `wrapper-merge`
**Touches:** `internal/wrapper` (new), `internal/agyproxy` (moved),
`internal/agybin` (moved), `internal/cli/hidden_command.go` (new),
`cmd/wagyu/main.go`, `cmd/wagyu/main_wrapper.go` (new), `cmd/agy/main.go`
**Report language:** English (like the other `docs/cycles/` reports)

> **Superseded in part by cycle `wrapper-flatten`.** This report describes the
> state after the merge: `cmd/agy` survived as a twelve-line shim and the
> `wrapper` namespace was still there. The next cycle dissolved the wrapper as
> a product — `cmd/agy` was deleted, the `wrapper` group was renamed `proxy`,
> and the legacy `agy wrapper providers|models` spelling went with it. Read
> `docs/cycles/wrapper-flatten/deliverable.md` for the current shape.

---

## 0. Summary

`cmd/agy` was a second product: a launcher that puts a local CloudCode proxy
in front of the *vendor* Antigravity CLI so the official binary can use models
it does not ship with, and so every request/response pair can be captured for
reverse engineering. `cmd/wagyu` is the clone. Two `cmd/` directories meant
two binaries to build, two CLIs to keep in sync, and — the actual smell —
**two implementations of the same provider/model registry**: the wrapper's
`agy wrapper providers|models` and the clone's `agy provider|model` both drove
`internal/models.Editor` and both wrote
`~/.config/openagy/providers.json`.

This cycle merges them:

| | before | after |
|---|---|---|
| the launcher | `cmd/agy` (650-line CLI + 107-line launcher) | `internal/wrapper` |
| the proxy | `devutils/agyproxy` | `internal/agyproxy` |
| the binary resolver | `devutils/agybin` | `internal/agybin` |
| the registry editor | `internal/models.Editor` | unchanged — **one** editor |
| the registry CLI | two copies (`cmd/agy/wrapper.go`, `internal/cli/provider_cmds.go`) | **one** copy (`internal/cli/provider_cmds.go`) |
| `cmd/agy` | the whole product | a twelve-line shim |

`cmd/agy` survives as a shim so the **drop-in** property survives: built from
`./cmd/agy` the binary is still named `agy`, and putting it earlier on
`$PATH` than the vendor binary still shadows it. Deleting the directory is a
one-line change if that property is ever dropped.

The clone gained the launcher and the admin commands:

```
wagyu agy [args…]                     the vendor agy behind the local proxy
wagyu wrapper doctor|log|capture|version
wagyu wrapper providers|models         the registry (the same as `wagyu provider|model`)
wagyu provider …                       hidden, canonical spelling
wagyu providers …                      hidden, alias
```

## 1. Why the launcher is dispatched before the parser

`wagyu agy` is **not** a parser subcommand. `cmd/wagyu/main.go` checks the
literal first argument before the parser exists:

```go
if len(os.Args) > 1 && os.Args[1] == "agy" {
        os.Exit(wrapper.Launch(os.Args[2:]))
}
```

Two reasons, both load-bearing:

1. **The vendor binary must see its command line untouched.** A parser
   subcommand would intercept `--help` and print the *clone's* help;
   `wagyu agy --help` must print the *vendor's* help, because the thing being
   invoked is the vendor binary.
2. **The clone's startup side effects are not the vendor's.** `main()` writes
   the compat defaults, runs the plugin cleanup and installs the daemon's
   signal handlers. The vendor binary does its own; running the clone's first
   would be a behaviour difference the wrapper never had.

The rule is the same one the wrapper binary already used: *only a literal
first argument selects the wrapper* (`agy -p wrapper` is a prompt, not a
subcommand).

## 2. The registry: one editor, one renderer

`internal/cli/provider_cmds.go` already implemented the whole registry —
`list`, `show`, `add`, `remove`, `test` — on `internal/models.Editor`. The
wrapper's copy implemented `list`, `add`, `remove` on the same editor. The
wrapper's copy is **deleted**; `internal/wrapper` delegates:

```
agy wrapper providers           → agy provider list
agy wrapper providers list      → agy provider list
agy wrapper providers add X     → agy provider add X
agy wrapper providers remove X  → agy provider remove X
agy wrapper models              → agy model list
agy wrapper models add X        → agy model add X
agy wrapper models remove X     → agy model remove X
```

The delegation goes through one new exported entry point,
`internal/cli.RunHiddenCommand(name, out, errOut, args)`, which finds the
hidden subcommand spec and dispatches it exactly the way the parser would
(case-insensitive sub-subcommand lookup, the parent's usage for an empty
argument list, the parser's rejection for an unknown one). Because the
handler is the clone's own, the exit code and the stderr line are the
clone's too — `internal/cli.ExitCodeOf` / `ExitMessage` carry them across the
package boundary.

Two flag names changed when the wrapper's `models add` moved onto the
clone's `model add`:

| wrapper | clone |
|---|---|
| `-external-model` / `-external_model_name` | `-model` |
| `-display-name` / `-display_name` | `-name` |

`internal/wrapper.translateModelFlags` rewrites them (one dash or two, and
the `--flag=value` form), so the old spelling keeps working. Every other flag
(`-provider`, `-context-window`, `-default-max-tokens`, `-reasoning-effort`,
and the whole `providers add` set) is already identical in both.

**One behaviour change:** the *output* of `agy wrapper providers|models` is
now the clone's renderer, not the wrapper's. The wrapper printed its own
one-line-per-model format (`models: <path>` + `name display provider=…`); the
clone prints the shared table. Same file, same data, one format.

## 3. The command surface

### The clone

| command | visible? | notes |
|---|---|---|
| `wagyu agy [args…]` | hidden | the vendor binary behind the proxy |
| `wagyu wrapper doctor` | hidden | agy path/version, registry, port, capture, upstream probe |
| `wagyu wrapper log [-n N]` | hidden | tail the proxy log |
| `wagyu wrapper capture on\|off\|dir [path]` | hidden | the capture toggle, persisted in `~/.openagy/wrapper.json` |
| `wagyu wrapper version` | hidden | wrapper and agy versions |
| `wagyu wrapper providers\|models` | hidden | the registry |
| `wagyu provider …` | hidden | the registry, canonical spelling |
| `wagyu providers …` | hidden | alias of `provider` |
| `wagyu model …` | hidden | the registry's models |
| `wagyu models` | **visible** | the *reference's* catalogue listing — byte-pinned, unchanged |

The `wrapper` group is hidden because `agy --help` lists the reference's
twelve visible subcommands and nothing else; the count is still twelve
(`cmd/wagyu/main_wrapper_test.go` pins it).

### The drop-in binary

`cmd/agy` is now:

```go
package main

import (
        "os"

        "github.com/justwasm/pigasus/internal/wrapper"
)

func main() { os.Exit(wrapper.Run(os.Args[1:])) }
```

`wrapper.Run` keeps the old contract — a literal first argument `wrapper`
selects the admin commands, everything else goes to the vendor binary
untouched — so `agy -p "hello"` still works and `agy models` still lists the
vendor's models.

## 4. What moved where

| from | to | change |
|---|---|---|
| `cmd/agy/launcher.go` | `internal/wrapper/launcher.go` | `runLauncher` → `Launch` |
| `cmd/agy/config.go` | `internal/wrapper/config.go` | unchanged |
| `cmd/agy/log.go` | `internal/wrapper/log.go` | `cmdLog` takes the streams |
| `cmd/agy/wrapper.go` | `internal/wrapper/admin.go` | the registry half deleted; the rest takes the streams |
| `cmd/agy/main.go` | `internal/wrapper/wrapper.go` + `cmd/agy/main.go` | the dispatch moved; the shim stayed |
| `cmd/agy/main_test.go` | `internal/wrapper/run_test.go` | the dispatch seams moved with it |
| `devutils/agyproxy/` | `internal/agyproxy/` | import path only |
| `devutils/agybin/` | `internal/agybin/` | import path only |
| `devutils/geminiwire/` | `internal/geminiwire/` | import path only |

`devutils/` was the wrong home for product code once the wrapper became part
of the clone: it now holds only the probes and the demo
(`devutils/pigloop-demo`, `devutils/agy228probe`, `devutils/hpgen`).

## 5. The streams

Every admin command now writes to an `io.Writer` pair instead of
`os.Stdout`/`os.Stderr`:

```go
type AdminOptions struct {
        Prog string   // the program name in usage and error text
        Out  io.Writer
        Err  io.Writer
}
```

The drop-in binary passes the process streams; the clone passes the parser's
(`j.Out`, `j.ErrOut`), so `wagyu wrapper doctor` writes where the rest of the
clone writes — and a test can capture it (`cmd/wagyu/main_wrapper_test.go`).

## 6. Tests

| test | what it pins |
|---|---|
| `internal/wrapper/admin_test.go` | the providers/models mapping and the flag renames |
| `internal/wrapper/run_test.go` | the dispatch (launcher vs admin), the capture round trip, the registry round trip |
| `internal/cli/hidden_command_test.go` | `RunHiddenCommand` routing, the rejection path, `ExitMessage` |
| `cmd/wagyu/main_wrapper_test.go` | the group stays hidden, the visible count stays 12, `wrapper capture` writes to the parser's stream, `wrapper providers` ≡ `provider list` |

`go test ./cmd/... ./internal/...` is green.

## 7. 硬约束 / hard constraints

- **The vendor binary is still never patched.** The only mechanism is
  `CLOUD_CODE_URL`; `~/.local/bin/agy` is SHA-256-identical before and after
  a run.
- **`agy --help` still lists exactly twelve visible subcommands.** The `agy`
  and `wrapper` groups are hidden; the clone's `--help` output is unchanged.
- **`agy models` is still the reference's catalogue listing**, byte-pinned.
  The *registry* models are `agy model` / `agy wrapper models`.
- **The drop-in property survives**: `go build ./cmd/agy` produces a binary
  named `agy`, and the self-exec guard (resolved path **and** inode) still
  stops it from exec'ing itself.

## 8. 存疑 / open questions

- **The drop-in binary is now bigger.** `internal/wrapper` imports
  `internal/cli` (for the registry delegation), which pulls the charm.land
  TUI stack into `cmd/agy`. The binary grows from ~10 MB to ~25 MB. The
  alternative — keeping a second copy of the registry commands in
  `internal/wrapper` — is exactly the duplication this cycle removed.
- **`wagyu providers` (the alias) prints the usage** when called bare, while
  `wagyu wrapper providers` prints the list: the alias follows the clone's
  `provider` semantics, the wrapper spelling keeps the wrapper's. Both are
  intentional, and both are old behaviour.
- **The `wrapper` group's own usage block** (`cmd/wagyu/main_wrapper.go`) is
  new text — the clone never had one. It is not byte-pinned to anything,
  because the reference has no such command.

## 9. Limitations

- The **capture directory, the proxy log and the wrapper config** are still
  the wrapper's own files (`~/.openagy/…`), not the clone's configuration
  tree. The merge was about the command surface and the registry; the
  on-disk layout was left alone deliberately.
- **The clone's TUI has no proxy panel.** `wagyu wrapper …` is a CLI command;
  the TUI's `/provider` panel edits the same registry but cannot toggle the
  capture or tail the log.
- The **upstream probe** in `doctor` is unchanged: one plain `GET /`, and a
  401/403/404 still proves DNS+TLS work.
- **The proxy's translation limits** are unchanged (no images, no thought
  parts, no compaction forwarding) — see the cycle-274 deliverable.
