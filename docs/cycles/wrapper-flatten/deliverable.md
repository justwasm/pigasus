# Wrapper flatten — the proxy becomes a part of the clone, not a product beside it

**Cycle:** `wrapper-flatten`
**Touches:** `internal/agyproxy/` (absorbs `internal/wrapper/`),
`internal/wrapper/` (deleted), `cmd/agy/` (deleted), `cmd/wagyu/main.go`,
`cmd/wagyu/main_proxy.go` (renamed from `main_wrapper.go`),
`cmd/wagyu/main_proxy_test.go` (renamed), `internal/cli/hidden_command.go`
(deleted), `internal/agyproxy/README.md` (moved from `cmd/agy/README.md`),
`AGENTS.md`
**Report language:** English (like the other `docs/cycles/` reports)

---

## 0. The question, and the answer

> "I don't understand what this wrapper still has a reason to exist; maybe it
> should be flattened."

The answer is one line: **the wrapper's *machinery* is irreducible; the
wrapper as a *product* is not.**

Three things were bundled under the name "wrapper", and only one of them
earns the name:

| what | why it survives | where it lives now |
|---|---|---|
| the **proxy** (HTTP server, `fetchAvailableModels` merge, Gemini ⇄ provider translation, JSON capture) | it is the only way to observe the *official* binary's wire traffic — every prompt and tool fixture in this repo was recorded through it | `internal/agyproxy` (the server, unchanged) |
| the **launcher** (proxy up → `CLOUD_CODE_URL` → exec the real `agy`) | it is the only way to run the *official* binary against the configured providers — the reference the clone is measured against | `internal/agyproxy/launch.go` |
| the **admin commands** (doctor / log / capture / version) | they inspect the proxy | `internal/agyproxy/admin.go` + `cmd/wagyu/main_proxy.go` |
| a **second binary** (`cmd/agy`, the drop-in `agy`) | nothing | **deleted** |
| a **second namespace** (`wrapper`) | nothing — the noun named the *binary*, not the thing | **renamed** to `proxy` |
| a **second registry CLI** (`agy wrapper providers\|models`) | nothing | **deleted** in cycle `wrapper-merge` |

The clone's own transport already speaks CloudCode *and* OpenAI *and* Anthropic
natively (`internal/backend`), so the proxy teaches the clone nothing. What it
gives the clone is a **reference**: the official binary's own traffic, to
diff against. That is a research instrument, not a feature — so it belongs
*inside* the tree, not beside it as a second deliverable.

## 1. What changed

| before | after |
|---|---|
| `cmd/agy/` — the drop-in binary | **deleted** (the `agy` verb in the clone covers it) |
| `internal/wrapper/` — a package of its own | merged into `internal/agyproxy/` |
| `wagyu wrapper doctor\|log\|capture\|version` | `wagyu proxy doctor\|log\|capture\|version` |
| `agy wrapper providers\|models` (the legacy spelling) | **deleted** — the registry is the clone's |
| `wagyu providers` (the plural alias) | unchanged |
| `internal/cli.RunHiddenCommand` / `ExitMessage` | **deleted** (no callers left) |
| `cmd/agy/README.md` | `internal/agyproxy/README.md`, rewritten |

### The command surface

```
wagyu agy [args…]                     the vendor agy behind the local proxy
wagyu proxy doctor|log|capture|version
wagyu provider …, wagyu model …       the registry (canonical; `wagyu providers` is an alias)
wagyu models                           the reference's catalogue listing (visible, byte-pinned)
```

`wagyu agy` is still dispatched in `cmd/wagyu/main.go` **before the parser
exists** — the vendor binary must see its command line untouched
(`wagyu agy --help` prints the *vendor's* help) and must not inherit the
clone's startup side effects. The `proxy` group is hidden: `agy --help` lists
the reference's twelve visible subcommands and nothing else.

### The files that moved

| from | to |
|---|---|
| `internal/wrapper/launcher.go` | `internal/agyproxy/launch.go` |
| `internal/wrapper/config.go` | `internal/agyproxy/fileconfig.go` (`config` → `fileConfig`, so it does not read as the server's `Config`) |
| `internal/wrapper/log.go` | `internal/agyproxy/log.go` |
| `internal/wrapper/admin.go` | `internal/agyproxy/admin.go` (the registry half deleted) |
| `internal/wrapper/wrapper.go` | `internal/agyproxy/ctl.go` (`Admin` + the usage text; `Run` — the drop-in personality — deleted) |
| `internal/wrapper/run_test.go` | `internal/agyproxy/ctl_test.go` |
| `internal/wrapper/log_test.go` | `internal/agyproxy/log_test.go` |
| `cmd/wagyu/main_wrapper.go` | `cmd/wagyu/main_proxy.go` |
| `cmd/wagyu/main_wrapper_test.go` | `cmd/wagyu/main_proxy_test.go` |

`internal/agyproxy` is now one package for one concept: **the local CloudCode
proxy and how to run it.** The package-level doc says so; the file names
(`ctl.go`, `launch.go`, `admin.go`, `fileconfig.go`, `log.go`) say which part
is which.

### The dependency that went away

`internal/wrapper` imported `internal/cli` to route the legacy
`agy wrapper providers|models` spelling into the clone's registry handlers.
That import is what made the wrapper a *peer* of the clone rather than a part
of it. With the legacy spelling gone, `internal/agyproxy` depends on
`internal/models` (read-only) and `internal/agybin` — **not** on the CLI
package. The registry editor stays in `internal/cli`, and the proxy never
edits the registry.

## 2. Why the env vars and the file names did *not* change

`OPENAGY_WRAPPER_ADDR`, `OPENAGY_WRAPPER_CONFIG`, `OPENAGY_WRAPPER_LOG`,
`~/.openagy/wrapper.json`, `~/.openagy/wrapper.log`, `~/.openagy/captures` —
all kept their names.

They are the **on-disk contract of an installed proxy**. Renaming them would
have silently moved an existing capture directory (this machine's is 10 GiB)
and reset the persisted settings, and an env var rename breaks every shell
profile and CI step that sets it. The *command surface* was flattened; the
*files* were deliberately not. The names are documented as legacy in
`internal/agyproxy/README.md`.

## 3. The registry keeps one spelling family

| spelling | status |
|---|---|
| `wagyu provider list\|show\|add\|remove\|test` | canonical |
| `wagyu model list\|show\|add\|remove` | canonical |
| `wagyu providers …` | alias of `provider` (the parser's own alias mechanism) |
| `agy wrapper providers\|models` | **deleted** with the binary that owned it |

The alias mechanism is the clone's, not a second implementation: the `provider`
spec carries `Aliases: []string{"providers"}` and the parser resolves it
through its own alias table. `cmd/wagyu/main_proxy_test.go` pins that
`wagyu providers list` and `wagyu provider list` print the same bytes.

Note that `wagyu providers` **bare** prints the usage (exit 1), exactly like
`wagyu provider` bare does — it is the same command, so it has the same
semantics. The old `agy wrapper providers` printed the *list*; that spelling is
gone.

## 4. What the doctor output changed

One line, and only because the noun changed:

```
- proxy     0.1.0 (cycle 274)
+ proxy       0.1.0 (cycle 274)
```

The old label was two columns short of the others (`agy`, `registry`, `config`,
`log`, `upstream`, `port`, `capture` are all 12 columns wide). The new one
lines up. Nothing else in the doctor output moved.

## 5. Tests

| test | what it pins |
|---|---|
| `internal/agyproxy/ctl_test.go` | the admin dispatch (`help` → 0, unknown → 1), the capture round trip, `withEnv` replaces rather than duplicates `CLOUD_CODE_URL`, the settings precedence (env > file > default) |
| `internal/agyproxy/log_test.go` | the log sink never falls back to stderr, the unwritable-sink fallback, `tailLines` |
| `cmd/wagyu/main_proxy_test.go` | the `proxy` group stays hidden, the visible count stays **12**, `wagyu proxy capture` writes to the *parser's* stream, the bare `wagyu proxy` prints the usage and exits 1, an unknown subcommand is rejected, `wagyu providers list` ≡ `wagyu provider list`, and the parser does **not** know the `agy` verb |

`go test ./cmd/... ./internal/...` is green.

The tests that went away with the legacy spelling are
`internal/wrapper/admin_test.go` (the `registryArgs` / `translateModelFlags`
mapping) and `internal/cli/hidden_command_test.go` (`RunHiddenCommand` /
`ExitMessage`). Both tested code that no longer exists — there is nothing to
port them to, because the thing they tested was the *duplication*.

## 6. 硬约束 / hard constraints

- **The vendor binary is still never patched.** The only mechanism is
  `CLOUD_CODE_URL`; `~/.local/bin/agy` is SHA-256-identical before and after
  a run.
- **`agy --help` still lists exactly twelve visible subcommands.** The `agy`
  and `proxy` groups are hidden; the clone's `--help` output is unchanged
  (verified live against the built binary).
- **`wagyu agy --help` still prints the *vendor's* help** ("Usage of agy:"),
  because the launcher is dispatched before the parser exists.
- **`agy models` is still the reference's catalogue listing**, byte-pinned.
  The *registry* models are `wagyu model` / `wagyu providers`.
- **The capture directory, the proxy log and the proxy config keep their
  paths** (`~/.openagy/…`): renaming them would move an installed capture
  directory and reset the persisted settings.
- **The self-exec guard is unchanged** (`internal/agybin`: resolved path *and*
  inode), so a symlink loop cannot fork-bomb the machine.

## 7. 存疑 / open questions

- **The drop-in `agy` binary is gone for good.** The shell alias
  (`alias agy='wagyu agy'`) reproduces the old behaviour for interactive use,
  but a *script* that runs `agy` now runs the vendor binary directly — no
  proxy, no custom models. Nothing in this repo depends on that; a user who
  wants it back needs `wagyu agy` in the script.
- **`internal/agyproxy` is now a large package** (~2,200 lines of
  non-test code: the server, the launcher, the admin commands, the config and
  the log sink). It is one concept, but it is one *big* concept. If it grows
  further, the split that would help is `internal/agyproxy` (the server) and
  `internal/agyproxy/ctl` (the launcher + admin) — not a second top-level
  package.
- **The proxy's own version string still says "cycle 274"** in the doctor
  output. The *code* has moved twice since (merge, flatten), but the cycle
  that introduced the proxy is the one the string names. Cosmetic only.
- **The `proxy` group's usage block is new text** — the reference has no such
  command, so nothing pins it.

## 8. Limitations

- **The clone's TUI has no proxy panel.** `wagyu proxy …` is a CLI command;
  the TUI's `/provider` panel edits the same registry but cannot toggle the
  capture or tail the log.
- The **upstream probe** in `doctor` is unchanged: one plain `GET /`, and a
  401/403/404 still proves DNS+TLS work.
- **The proxy's translation limits** are unchanged (no images, no thought
  parts, no compaction forwarding) — see the cycle-274 deliverable.
- The **captures are still unredacted by design** and the capture directory
  holds every request the official binary made — on this machine, 10 GiB.
  `wagyu proxy capture off` is the switch.
