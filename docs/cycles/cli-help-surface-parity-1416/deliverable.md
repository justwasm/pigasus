# The CLI's `--help` surface, diffed against the reference 1.4.6

## Scope

The clone's job is byte-level parity with the reference binary. The twelve
visible subcommands are pinned by an invariant in `AGENTS.md`; the *flag*
column and the per-subcommand usage blocks were never pinned, so they drifted.

This cycle diffed the two `--help` surfaces command by command and fixed
every divergence it found, then pinned the whole surface with a golden-file
test so it cannot drift again.

**Nothing outside `internal/cli` was touched.** No prompt, no transport, no
tool surface.

## Method

The reference binary is `~/.local/bin/agy` (189 MB, `--version` → `1.4.6`).
Every comparison below is 【一手】: both binaries were run on this machine and
their output compared byte for byte.

```
$ ~/.local/bin/agy --help            # reference
$ pigasus --help                     # clone
$ ~/.local/bin/agy <cmd> --help      # reference, per command
$ pigasus <cmd> --help               # clone, per command
```

The reference prints **all** help on **stderr** (the top level included) and
writes nothing to stdout. The clone does the same, so the two are comparable
without a stream swap.

The capture used a throwaway `HOME` (`/tmp/agy-help-home`), because three of
the plugin subcommands treat `--help` as an operand —
`agy plugin uninstall --help` printed `Uninstalled plugin "--help"` — and
`plugin install --help` would otherwise have written into the real plugin
directory. Nothing was left behind in `~/.gemini/config/plugins`.

The first diff (top level only) showed one divergence. The per-subcommand
diff showed four more.

## Findings

### 1. `--print-partial-response` — a flag the reference does not have

The clone's top-level `--help` carried one flag the reference's does not:
`--print-partial-response`.

It was added by commit `61d41e56` ("feature: --print-partial-response flag
for headless mode (AGY 1.2.10)") and justified in
`docs/cycles/headless-partial-response-flag/deliverable.md` with a quoted
changelog line:

> Added a `--print-partial-response` flag for headless mode that emits each
> chunk of the model's output as soon as it arrives instead of buffering
> until the turn completes.

The report says the line was "recovered verbatim from
`internal/cli/testdata/changelog_reference.txt`". **It is not in that file.**
Nor in the live changelog of the 1.4.6 binary. Nor is the flag's name in the
binary's string table:

```
$ grep -ac "print-partial-response" ~/.local/bin/agy
0
```

【一手】 The reference has no such flag. It is also absent from the older
captured help (`v4-captures/v4-help.json`), so it was never there.

The flag is now `Hidden`, which is the convention this clone already uses for
its extras (`--verbose`, the recovered `-v N`): it stays parseable, so the
partial-response side channel is still reachable, but it no longer shows up
in a surface the reference does not have.

### 2. `plugin`'s usage block

The reference's block:

```
Usage: agy plugin <command> [arguments]

Commands:
  list [--json]          List installed plugins
  import [source]        Import plugins from gemini or claude
  install <target>       Install a plugin (supports plugin[@marketplace])
  uninstall <name>       Uninstall a plugin
  enable <name>          Enable a plugin
  disable <name>         Disable a plugin
  validate [path] [--strict] [--json]
                         Check a plugin's manifest and files
  link <mp> <target>     Generate link to a marketplace
  help                   Show this help
```

The clone's differed in three ways: the `validate` row read
`validate [--strict] [--json] [path]` with the description
`Validate a plugin (AGY 1.4.1)`, the description column was four characters
wider, and the `help` row was missing. The reference's `validate` usage
string is 36 characters — longer than the 25-column description gutter — so
its description wraps onto the next line. The clone's block is the
reference's, byte for byte, now.

### 3. `plugin validate --help`

The clone rejected `--help` like any other unknown argument:

```
$ pigasus plugin validate --help
Error: unknown validate argument "--help": usage is validate [path] [--strict] [--json]
```

【一手】 The reference prints its usage block on stderr and exits 0:

```
$ agy plugin validate --help
Usage: agy plugin validate [path] [--strict] [--json]

Checks a plugin the way the plugin loader reads it, and reports the files and
fields that the loader skips or ignores.

Arguments:
  path        A plugin directory, or its plugin.json (default: the current
              directory)

Flags:
  --strict    Also fail on warnings
  --json      Print the report as JSON
  -h, --help  Show this help

Each finding is an error (the loader skips a file or the plugin, or an item
can never work), a warning (a field or file is ignored or falls back to a
default) or a note (how the plugin loads). Exits non-zero if there are errors,
or warnings with --strict.
```

The unknown-argument message also gained the reference's `. See validate
--help` suffix. The clone's own probe (`plugin_validate_json.go`) had
concluded that `--help` was an error — that conclusion was wrong against
1.4.6 and the probe comment now says so.

### 4. `remote-control`'s listing advertised `serve`

The reference's listing shows three subcommands:

```
Available subcommands:
  start    Register and start the remote-control daemon
  status   Show the daemon's status
  stop     Stop the daemon and unregister it
```

The clone listed a fourth, `serve`. 【一手】 The reference *has* the
subcommand — `agy remote-control serve --help` does not say "unknown
command", it authenticates and starts the foreground daemon — but it does
not advertise it. The clone's `serve` row is gone from the listing; the
subcommand itself still works.

### 5. `remote-control start` / `status` / `stop` advertised flags the reference does not

| Block | Clone advertised | Reference advertises |
|---|---|---|
| `start` | `--addr`, `--log-file`, `--name`, `--session` | `--name`, `--session` |
| `status` | `--json`, `--name` | *(neither)* |
| `stop` | `--name` | *(neither)* |

`status` was the worse one: its description claimed
`Show the daemon's status (emits a JSON document)` while the command prints
the reference's human-readable block (`Daemon state = running`, `Daemon pid =
…`). 【一手】 The reference rejects the extra flags by printing its usage
block — `agy remote-control start --addr 1.2.3.4:9` prints the usage, it
does not start a daemon.

The clone still *accepts* `--addr`, `--log-file`, `--json` and `--name`
(the foreground daemon and the persistent daemon use them), but it no longer
advertises them — the visible surface is the reference's.

## The fix

| File | Change |
|---|---|
| `internal/cli/parser_flags.go` | `--print-partial-response` → `Hidden` |
| `internal/cli/subcommand_plugin.go` | `usagePlugin` → the reference's block |
| `internal/cli/plugin_validate_json.go` | `--help`/`-h` → the reference's usage block on stderr, exit 0; the unknown-argument message gained its suffix |
| `internal/cli/subcommand_rc.go` | `usageRemoteControl` lost the `serve` row; `usageRCStart` / `usageRCStatus` / `usageRCStop` are the reference's blocks |

## The pins

Two tests, so none of this can drift again:

- **`TestHelpSurfaceMatchesReference1416`** (`internal/cli/help_golden_test.go`)
  compares the clone's help output with `testdata/help_1.4.6.txt` — a
  capture of the reference's own output for the top level, the twelve visible
  subcommands and their sub-subcommands, stdout and stderr kept separately
  and byte for byte. Thirty commands, one block each.
- **`TestVisibleFlagsMatchReferenceHelp`** (`internal/cli/parser_test.go`)
  pins the visible flag column to the reference's 24 entries
  (21 long flags plus the short aliases `-c`, `-i`, `-p`) in the reference's
  order. Hidden flags are the clone's extras and must never leak into the
  column.

The golden file's program name is `agy`; the test rig's is `antigravity`.
That one word is the only normalisation, and the test says so.

## Result

```
$ diff <(agy --help) <(pigasus --help | sed 's/pigasus/agy/g')   # (modulo the name)
$ # identical
```

Top level, all twelve subcommands, all thirty captured commands: identical
to the reference, modulo the program name.

## Hard constraints

- **The clone keeps its own name.** `pigasus` is not `agy`; every usage
  block takes the program name as a parameter and the golden test
  normalises that one word.
- **`--print-partial-response` stays parseable.** Hiding it is not the same
  as deleting it: the partial-response side channel in `internal/printmode`
  is unchanged, and the flag still switches it on. Only the *advertisement*
  changed.
- **The clone's extra `remote-control` flags stay accepted.** Removing
  `--addr` / `--log-file` / `--json` / `--name` would break the clone's own
  daemon paths and its tests. Only the advertisement changed.
- **The golden file is a capture, not a rendering.** It is the reference's
  bytes. If the reference's help changes, the file is re-captured — the
  clone is not edited to match by hand.

## Open questions

- **`plugin validate` was probed against an older build.** The probe that
  produced the first implementation recorded `--help` as an error; the
  live 1.4.6 binary prints a usage block. Which version the probe saw is
  not recorded — if other plugin surfaces were probed against the same
  build, they may carry the same staleness. The golden test covers the
  help blocks of all nine plugin subcommands now, so the *surface* is
  pinned; the *behaviour* behind them is not re-probed here.
- **`--print-partial-response` may have existed in some 1.2.x build.** The
  changelog quote in `docs/cycles/headless-partial-response-flag/` claims
  it did, but no changelog this repo holds contains that line and the
  binary carries no such string. If a 1.2.10 binary turns up, the flag may
  deserve to be visible again — for 1.4.6, it is not.
- **`remote-control serve` has no reference help block to copy.** The
  reference's `serve` ignores `--help` and starts the daemon, so there is
  nothing to diff against. The clone's `serve --help` prints its own
  usage block; that is a deliberate divergence, not an oversight.

## limitations

- The diff covers the **help surface** only. The reference's *behaviour*
  behind each command (the exit codes, the output formats) was not
  re-verified in this cycle.
- The golden capture used a throwaway `HOME`, so any help text that varies
  with the environment (a configured plugin list, a configured MCP server)
  is captured in the empty-environment form.
- `remote-control serve` was captured on a machine where the daemon was
  already running, so the reference's `serve` output is not in the golden
  file at all.
