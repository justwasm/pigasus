# The hidden command and flag surface

> The clone's `--help` is the **reference's**, byte for byte: 25 flag
> entries (22 long flags and the three short aliases `-c`, `-i`, `-p`)
> and 20 subcommands — the reference's twelve plus the clone's eight.
> Everything else that still *parses* is **hidden** — reachable,
> accepted, and mentioned nowhere in `--help`.
>
> This page is the inventory of that surface. It exists because a hidden
> command is invisible by construction: the only way to find it is to
> read the spec tables, and the only way to keep the inventory honest is
> to keep it written down.

| | |
|---|---|
| The spec tables | `internal/cli/parser_specs.go` (subcommands), `internal/cli/parser_flags.go` (flags) |
| The help renderer | `internal/cli/jetski.go` — `printHelpFlags`, `printHelpSubcommands` |
| The pre-parser dispatch | `internal/cliapp/run.go` — `Run` |
| The inventory test | `internal/cli/hidden_surface_test.go` |

## How a surface goes hidden

There are exactly three mechanisms, and they behave differently.

**1. `Hidden: true` in the spec.** The parser registers the entry and
the help renderer skips it. `printHelpFlags` skips a flag when
`f.Hidden` is set *or* when its usage string is empty; `printHelpSubcommands`
skips a subcommand when `s.Hidden` is set
(`internal/cli/jetski.go:456`, `internal/cli/jetski.go:479`). The entry is
fully parseable — `pigasus stats` works, `pigasus stats --help` does not
exist, and `pigasus --help` never mentions it.

**2. A usage block that does not list it.** The reference's usage blocks
are byte-for-byte copies, so a flag the clone added cannot be advertised
in them. The flag is parsed by the subcommand's own argument loop and
simply does not appear in the help text. This is how `remote-control
start --desktop` works: the flag is real, the usage block is the
reference's, and the two are not allowed to meet.

**3. Dispatch before the parser exists.** `pigasus agy` never reaches
the parser at all — `internal/cliapp/run.go` inspects `args[1]` before
`setupCLIParser()` is called and hands the rest of the command line to
the vendor binary untouched.

Mechanism 3 exists for a reason worth stating plainly: the vendor
binary must see its own command line. The clone's compat defaults, its
plugin cleanup and its signal handlers all run *after* the vendor's
process has taken over, because the vendor owns those.

## The hidden root subcommands (11)

| Command | What it does | Defined in |
|---|---|---|
| `login` | Start the OAuth device-code login flow | `internal/cli/parser_specs.go:242` → `internal/cli/dispatch.go` `runLogin` |
| `logout` | Clear the stored OAuth token | `internal/cli/parser_specs.go:243` → `runLogout` |
| `auth` | Set up the mTLS bypass proxy | `internal/cli/parser_specs.go:244` → `runAuth` |
| `chat` | Launch the recovered TUI chat | `internal/cli/parser_specs.go:245` → `runChat` |
| `acp` | Start the Agent Client Protocol listener | `internal/cli/parser_specs.go:246` → `runAcp` |
| `http` | Start the recovered HTTP server | `internal/cli/parser_specs.go:247` → `runHttp` |
| `hooks` | List configured lifecycle hooks | `internal/cli/parser_specs.go:248` → `runHooks` |
| `migrate` | Migrate the legacy config tree to the v4 layout | `internal/cli/parser_specs.go:249` → `runMigrate` |
| `stats` | Show on-disk session statistics | `internal/cli/parser_specs.go:250` → `runStats` |
| `logo` | Print the startup banner logo | `internal/cli/parser_specs.go:251` → `runLogoHandler` |
| `remote-control-foreground` | The foreground subcommand the persistent daemon invokes | `internal/cli/parser_specs.go:186` → `runRCForegroundPlaceholder` |

The first ten are the cycle-41 dispatch table (`internal/cli/dispatch.go`,
`runXxxTable`) — twelve entries, of which `version` and `models` are
reachable under other names (`--version` and the visible `models`
subcommand). `remote-control-foreground` is the daemon's own child
process entry point; it is not meant to be typed by a human, and it
rejects any argument it does not own.

**Why they are hidden.** These are the commands the reference's own
binary carries but does not advertise — `agy --help` lists twelve
subcommands and stops there. The clone keeps the reference's list, so
everything the reference has *beyond* that list has to be hidden too, or
the help stops matching.

## The hidden root flags

### The reference's own hidden flags (7)

These are part of the reference's surface. Their names, types, defaults
and usage strings are the reference's own — recovered from the binary's
string table and from its `--help` — and they are hidden because the
reference hides them.

| Flag | Type | Default | What it is |
|---|---|---|---|
| `--cdp_port` | int | `9222` | Port for Chrome DevTools Protocol |
| `--local_chrome_headless` | bool | `true` | Whether Chrome runs in headless mode |
| `--local_chrome_user_data_dir` | string | `""` | Chrome user data directory |
| `--use_local_chrome` | bool | `false` | Whether to use local chrome |
| `--print-partial-response` | bool | `false` | Stream each chunk of the model's output to stdout in headless mode as it arrives instead of buffering until the turn completes |
| `-v` | int | — | The recovered hidden `-v N` int flag (cycle 101) |
| `--verbose` | bool | `false` | Print extra stream-json events (`assistant_message`) in `--output-format stream-json` |

The first four are the reference's **local-Chrome CDP control**
(【一手】 + 【反编译】, `out/local-macos/FINDINGS.md` §2, items 4–7): the
binary carries their usage strings but never lists them, and the clone's
browser client reads them (`browsercdp.Config`).

`--print-partial-response` and `--verbose` are the other way round: the
reference has **no** such flag. The 1.4.7 binary carries no
"print-partial-response" string, its `--help` does not list it, and its
changelog never mentions it — the cycle report that added it quoted a
1.2.10 changelog line that is in neither the live changelog nor the
vendored `internal/cli/testdata/changelog_reference.txt`. They stay
parseable as clone extras so the side channels remain reachable, and the
visible `--help` stays byte-identical with the reference's.

### The clone's hidden extras (11)

These are openagy-only. None of them exists in the reference at all.

| Flag | Type | Default | What it is |
|---|---|---|---|
| `--backend` | string | — | Backend type: `gateway` (real) or `shim` (mock) |
| `--alt-screen` | bool | `true` | Use the terminal's alternate screen buffer |
| `--no-alt-screen` | bool | `false` | Disable the terminal's alternate screen buffer |
| `--trust-cwd` | bool | `false` | Skip the on-startup workspace-trust dialog (or set `$OPENAGY_TRUST=1`) |
| `--max-thinking-budget` | int | `0` | Cap thinking tokens per turn; values ≤ 0 disable thinking |
| `--diff-with-agy` | bool | `false` | Print the AGY 1.4.2 feature parity (aligned / gap / diverged) |
| `--debug` | bool | `false` | Enable verbose debug logging |
| `--desktop` | bool | `false` | Serve the reference desktop UI (voice input, jetbox terminal, aux pane redesign) |
| `--remote-control-tcp` | string | `""` | Bind addr (host:port) for the WebSocket JSON-RPC transport; empty disables |
| `--remote-control-socket` | string | `""` | Override the unix socket path for `--remote-control` |
| `--experimental-rc-v2` | bool | — | Opt in to the deferred v2 (WebRTC mesh / Hub) features; no-op today |

`--alt-screen` and `--no-alt-screen` are a pair: the explicit flag
overrides the `/config` Rendering Mode setting, in either direction.
`--remote-control-tcp` replaces `--remote-control` when it is set;
`--remote-control-socket` is still overridden by `$OPENAGY_RC_SOCKET`.

### One more, hidden by an empty usage string

`--version` is registered by the parser itself
(`internal/cli/parser.go:116`) with an **empty** usage string, and the
help renderer skips empty-usage flags. So it does not appear in
`--help`, but `pigasus --version` prints `1.4.11`. It is hidden by
accident of the renderer, not by a `Hidden: true` flag — worth knowing
when auditing the help surface.

## The hidden sub-subcommands (3)

All three live under `remote-control`, and all three are hidden by
mechanism 2: the reference's `remote-control --help` lists **start,
status and stop** only, and that block is the reference's, byte for byte.

| Command | What it does | Defined in |
|---|---|---|
| `remote-control serve` | The foreground daemon, with the inline OAuth login | `internal/cli/subcommand_rc_serve.go` |
| `remote-control run` | The foreground daemon on **every** platform — the platform-independent spelling of `serve` | `internal/cli/subcommand_rc_run.go` |
| `remote-control rename` | Set this machine's instance name, as shown in Remote Control | `internal/cli/subcommand_rc_rename.go` |

`serve` exists in the reference binary too — the reference has the
subcommand but does not list it. `run` and `rename` are the clone's own.

`run` and `serve` are the same implementation behind two names
(`cli.runRCForegroundDaemon`); only the label differs. `rename` writes
the name into the configuration file, and a running daemon watches that
file and renames the instance over the API without dropping the tunnel —
the reference renames from the Web UI's settings instead.

## The hidden subcommand flags (3)

`remote-control start` parses three flags its own usage block does not
list: `--addr`, `--log-file` and `--desktop`. The reference's `start`
advertises `--name` and `--session` only, and the clone's usage block is
the reference's, so the three extras are accepted and not advertised
(`internal/cli/subcommand_rc.go:36`, `internal/cli/subcommand_rc.go:121`).

`remote-control serve` and `remote-control run` *do* list their flags
(`--addr`, `--name`, `--no-login`, `--desktop`), and `web` lists its own
(`--desktop` included).

## The hidden TUI slash commands (5)

The TUI's picker shows 99 built-in commands. Five more are handled but
never listed — they are in the handler map
(`internal/cliapp/slash_commands.go:101`) and not in the catalog
(`internal/ui/tuilib/commands.go:57`).

| Command | Handler | What it does |
|---|---|---|
| `/continue` | `slashContinue` | Continue the most recent conversation — resolves it from the store and loads it into the session |
| `/login` | `slashLogin` | Sign in to a Google account (the device-code login UI) |
| `/narration` | `slashNarration` | Print the narration feedback — what the narration of a turn does |
| `/quote <n>` | `slashQuote` | Insert a quoted block of the n-th turn into the prompt |
| `/share <name>` | `slashShare` | Open a read-only, isolated share of the workspace |

`/logout` **is** visible (`/logout — Log out`); only `/login` is hidden.

## The pre-parser launcher

`pigasus agy <args…>` runs the vendor Antigravity CLI behind the local
CloudCode proxy (`internal/infra/agyproxy`). It is dispatched in
`internal/cliapp/run.go:53`, before the parser is built, before the
compat defaults are written, before the plugin cleanup and before the
signal handlers are installed.

`pigasus agy --help` prints the **vendor's** help, not the clone's. That
is the point: the command line has to reach the vendor binary untouched.

## Verifying the inventory

```bash
go build -o /tmp/pigasus ./cmd/pigasus

# the visible surface — 25 flags, 20 subcommands
/tmp/pigasus --help

# a hidden subcommand still parses
/tmp/pigasus stats

# a hidden flag still parses
/tmp/pigasus --experimental-rc-v2 --help

# the pre-parser launcher prints the vendor's help
/tmp/pigasus agy --help

# the inventory itself
go test ./internal/cli/ -run TestHiddenSurface
```

## What is *not* hidden

- **`proxy`** is a *visible* subcommand (`pigasus proxy doctor`,
  `… log`, `… capture`, `… version`), even though it is an openagy-only
  feature. It is one of the eight subcommands the clone adds on top of the
  reference's twelve.
- **`accounts`, `model`, `provider`, `halloween`, `web`** are the other
  openagy-only *visible* subcommands, and so are **`server`** and **`tui`**
  — the client/server pair (see
  [`agent-server.md`](agent-server.md)). Their flags are their own usage
  blocks', so they are not hidden: `pigasus server --help` and
  `pigasus tui --help` list them.
- The `agent`/`agents` and `plugin`/`plugins` pairs are **aliases**,
  not hidden commands: both spellings are listed.
- The reference's own `--help` block is reproduced in
  `internal/cli/testdata/help_1.4.6.txt` and compared byte for byte by
  the parser tests.

## Notes and limitations

- The inventory is a snapshot of **agy 1.4.11**. A hidden entry is not a
  promise about the next release; the reference's surface moves.
- "Hidden" here means *not printed in `--help`*. It does not mean
  unsupported, unstable or deprecated — `--print-partial-response` is
  hidden and fully functional, and `--experimental-rc-v2` is hidden and
  deliberately inert.
- The TUI's slash-command list is also affected by plugins and skills:
  a plugin can contribute commands the picker shows. The five above are
  the ones the *clone* handles but does not list.
