# Cycle 96 A — v1.3.5 subcommand parity deliverable

**Task:** make the recovered `agy` clone (`./cmd/antigravity`) match the
official v1.3.5 binary's **subcommand surface** — usage/help text,
stdout-vs-stderr routing, exit codes, and simple real handlers — verified
byte-for-byte against the reference binary.

Reference: `~/.local/bin/agy` (v1.3.5, Mach-O arm64, 180,917,280 bytes,
SHA-256 `ebd71c4db14f79fca3fd0160332a38d9037146737422dcfc1f1d21ba01a13fff`).

## Result

`/tmp/agycmp/harness.sh` — 53 invocations, each run against the reference
and our build with a fresh scratch `$HOME`, diffing `rc` + stdout +
stderr:

```
MATCH: 52   DIFF: 1        (the single diff is `update`, see below)
```

## Supersedes

This deliverable supersedes **all five** `docs/cycles/cycle-49/`
deliverables, which were written against an early, un-verified baseline
and are contradicted point-for-point by the measurements here. A
`SUPERSEDED` banner was added at the top of each (A short-subcommand,
B mcp-list, C changelog, D update-models, E install); their original text
is kept for history. The largest correction is E: cycle-49 `install`
copied the running binary over `$HOME/.local/bin/agy`, which clobbered the
installed reference — the real v1.3.5 `install` is print-only.

## Method

Both binaries are copied to a neutral directory under the same basename so
the argv0-derived program name matches:

```
/tmp/agycmp/ref/agy   # the reference
/tmp/agycmp/our/agy   # our build
```

`harness.sh` runs each case as

```
( cd /tmp && HOME=<fresh-scratch> timeout 6 <bin> <args> )
```

and records stdout/stderr/rc separately, then diffs all three. A scratch
`HOME` is essential: several subcommands read/write
`$HOME/.gemini/config/...`, and the reference's sign-in check consults
only on-disk token files (not the macOS keychain — verified: a scratch
`HOME` reports signed-out even though a `gemini`/`antigravity` keychain
item exists).

The 53 cases cover: help forms, `--version`, unknown top-level flag,
plugin (11 cases), mcp (12), remote-control (7), mic-serve, agents (5),
update (2), models (2), changelog, install, `help <sub>`, and an unknown
top-level subcommand.

## What changed

| File | Change |
|---|---|
| `internal/cli/v135_parity.go` (new) | Exact v1.3.5 usage blocks + `RunJ` handlers for plugin / mcp / remote-control / agents / mic-serve / models / changelog / help. Handlers write results to `j.Out` and usage/`Error:` lines to `j.ErrOut`. |
| `internal/cli/jetski.go` | Added the `RunJ`, `Help`, `HelpExit`, `UnknownSubError` seam; corrected the parent-command no-arg path; reworded the stdlib flag error to `flags provided but not defined: -X` and route it + usage to `ErrOut` (rc 2); made stdlib's internal `usage()` a no-op so the block prints once, not twice. |
| `internal/cli/parser.go` | `RunJ` / `HelpFn` / `HelpExit` / `UnknownSubError` on the spec types, wired in `NewParser`; `Parser.lastCode` + `ExitCode()`. |
| `internal/cli/parser_specs.go` | `StandardSubcommands()` rewired: 12 visible subcommands to the parity handlers, hidden openagy-only subcommands (login/logout/auth/chat/acp/http/hooks/migrate/stats) unchanged. `install` switched to `RunJ` so it honours the parser's `ErrOut`. |
| `internal/cli/changelog_data.go` + `testdata/changelog_v135.txt` (new) | Replaced a hand-transcribed 4,640-byte subset with `//go:embed` of the full 148,631-byte `agy changelog` output (892 lines, 68 sections, no 1.3.5 section). |
| `internal/cli/install.go` | Rewrote `install` to match the reference: **print-only, no filesystem mutation**, with the reference's stdout/stderr split. |
| `cmd/antigravity/main.go` | Program name is now `filepath.Base(os.Args[0])`; exit code propagated via `p.ExitCode()` instead of always 1. |
| tests (see below) | Updated to assert the corrected reference behaviour and to be hermetic. |

### The three harness bugs fixed this pass

1. **Unknown top-level flag printed the usage block twice.** stdlib's
   `flag` calls `fs.Usage()` from its error path *and* the error branch
   printed it manually. Fixed by making `fs.Usage` a no-op; all printing
   now happens in the error branch.
2. **`agy mcp` / `agy remote-control` with no subcommand printed
   nothing.** The parent-no-arg path fell through to an always-non-nil
   no-op `Run` closure before reaching the `Help` block. Fixed by giving
   both parents a `RunJ` that prints the usage block to stderr (rc 0),
   mirroring `runPluginParent`.
3. **`plugin validate` said `open ...`, reference says `stat ...`.** Fixed
   by using `os.Stat` for the existence check.

## Reference facts pinned by the harness

- Program name is the argv0 basename, in `Usage of <name>:` and every
  subcommand preamble / error string.
- Unknown **top-level flag** → stderr `flags provided but not defined: -nope`
  then the usage block, rc 2.
- Unknown **sub-flag** (e.g. `mcp list --json`, `changelog --json`) →
  usage block **then** `Error: flags provided but not defined: -json`, rc 1.
- `mcp`/`remote-control` no-args and `--help` → usage to stderr, rc 0.
  `plugin` no-args → usage + `Error: missing plugin command`, rc 1.
- `mcp list` config = `$HOME/.gemini/config/mcp_config.json`, schema
  `{"mcpServers":{name:{args,command,disabled,serverUrl,headers}}}`. Empty
  or missing → stdout `No MCP servers configured.` rc 0. Table widths
  `NAME=max(4,maxlen)+2`, `TYPE=max(4,maxlen)+2`, `STATUS=max(6,maxlen)+2`.
- `plugin` config = `$HOME/.gemini/config/plugins/<name>/plugin.json`;
  skills counted only from `skills/<x>/SKILL.md`.
- `remote-control status` → stdout `Daemon status: not running` +
  `Instance name: not assigned yet; ...`, rc 0.
- `agents`/`agent` no-args → no output rc 0; `agents list` →
  `Error: unexpected arguments: [list]` stderr, rc 1; `agent --help` →
  `Usage: agy agent [flags]` rc 0.
- `models` signed out → stderr `Fetching available models...` +
  `Error: Please sign in ...`, rc 1.
- `changelog` → the full 148,631-byte block on stdout; extras ignored.
- `update --help` → stderr `Usage of update:` rc 2. `install --help` →
  usage rc 0 (must not run the installer).
- `help <sub>` → that sub's usage on stderr rc 0; `help frob` →
  `Error: unknown subcommand: frob` rc 1.

## The `update` divergence (the one remaining diff)

The reference `update` is a real self-updater:

```
⟳ Checking for updates... (current version 1.3.5)
✓ Found new version 1.3.6.
⟳ Downloading update...
<downloads and stages a replacement binary>
```

It queries `https://antigravity-cli-auto-updater-974169037036.us-central1.run.app`
(recovered from the binary), whose body is
`Antigravity CLI auto updater is running! Stable Version: 1.3.6. Rolled out to 100%`.
When a newer version exists it resolves a manifest, downloads the package,
stages it and prints `Update successful! Please restart agy.` — i.e. it
replaces the installed binary.

Our clone deliberately **does not** perform network I/O or self-replace.
It prints the deterministic up-to-date result:

```
⟳ Checking for updates... (current version 1.3.5)
✓ You are already on the latest version.
```

This is the only case that cannot match: the reference's own output is
network-dependent, and the harness's 6-second timeout truncates it mid
download (rc 124). Reproducing it would mean either hanging or downloading
and overwriting a 180 MB binary — neither is acceptable in a test, and the
latter would clobber an installed reference. Documented, not hidden.

## The `install` correction (safety)

The v1.3.5 `install` is **informational only**: running it against a
scratch `$HOME` and a non-default `--dir` creates neither `--dir` nor
`$HOME/.local/bin`, and writes no shell profile. The output splits by
stream and branches on whether `--dir` is on `$PATH`:

not on `$PATH` (stderr → stdout):

```
Running Antigravity CLI setup...
(blank)
Warning: <dir> is not present in your active PATH.
To use the 'agy' CLI globally, please manually add it to your shell profile:
(blank)
  echo 'export PATH="<dir>:$PATH"' >> ~/.bashrc && source ~/.bashrc
(blank)
(blank)
✅ Antigravity CLI binary placed successfully at $HOME/.local/bin/agy
Note: The binary is installed, but it is not in your active PATH. ...
```

on `$PATH`:

```
Running Antigravity CLI setup...
PATH verification: ~/.local/bin is correctly configured in active PATH environment.
(blank)
✅ Antigravity CLI installed successfully at $HOME/.local/bin/agy
Run 'agy' to start the CLI
```

The earlier revision **copied the running binary into
`$HOME/.local/bin/agy`**, which clobbered the installed reference during
`go test` (caught and restored from a backup this pass). It also got
`--dir` wrong (the banner always names `$HOME/.local/bin/agy`; `--dir`
only changes the PATH warning) and `--skip-path` wrong (the reference
still prints the warning with `--skip-path`). All corrected; verified
byte-for-byte against the reference for both branches (modulo the glog
prefix, below).

## Tests

`go test ./internal/cli/` and `go test ./internal/... ./cmd/...` → all
`ok`. Updated/rewritten:

- `close_gap_test.go` — now captures stdout **and** stderr and asserts the
  exit code: `version` → stderr rc 2 (was asserted as stdout rc 1, which
  was wrong), `remote-control status` → `not running` (was `inactive`),
  `agents list` → stderr rc 1, `plugin list` hermetic via a temp `$HOME`.
  Added `TestParentNoArgsUsage` for the `mcp`/`remote-control` no-arg fix.
- `mcp_list_test.go` — retargeted at the live implementation
  (`$HOME/.gemini/config/mcp_config.json`, `mcpServers` schema) instead of
  the superseded `dispatch.go` path (`$ANTIGRAVITY_HOME/mcp.json`).
- `changelog_data_test.go` — asserts the full 148,631-byte block: byte
  length, SHA-256 (`cf1e6f805a5e…`), 68 sections, 757 bullets, no 1.3.5
  section, and byte-for-byte equality with a reference `agy changelog` run
  when one is on disk (`$AGY_REF` / `~/.local/bin/agy`).
- `update_models_test.go` — hermetic `models` signed-out test (stderr,
  rc 1) + signed-in catalogue; `update` pins the documented offline output.
- `parser_test.go` — `agent`/`agents` now correctly have 0 subcommands.
- `install_test.go` — rewritten for the print-only, split-stream behaviour
  and made hermetic; added `TestInstall_NoFilesystemMutation`.

## Verification

```console
$ bash /tmp/agycmp/harness.sh
MATCH: 52   DIFF: 1           # only `update`

$ cd /Users/gear/Documents/GitHub/openagy
$ go build ./cmd/antigravity      # rc 0
$ go vet ./internal/cli/ ./cmd/antigravity/   # clean
$ go test ./internal/cli/         # ok
$ go test ./internal/... ./cmd/...   # all ok
$ shasum -a 256 ~/.local/bin/agy
ebd71c4db14f79fca3fd0160332a38d9037146737422dcfc1f1d21ba01a13fff  # unchanged
```

`install` byte comparison (both branches, glog prefix stripped):

```
### agy install --dir=/tmp/agycmp/notinpath
  stderr: MATCH   stdout: MATCH
### agy install --dir=/usr/bin
  stderr: MATCH   stdout: MATCH
```

## 硬约束 / 存疑 / limitations

- **`update` is not reproduced.** It is a network self-updater that
  downloads and replaces the running binary; our clone emits the
  deterministic up-to-date result instead. This is the sole harness diff.
- **glog prefix is dropped.** The reference logs via glog, so its stderr
  lines carry
  `ERROR: logging before google.Init: I<MMDD> <HH:MM:SS.µs> <pid> <file>:<line>]`.
  Our build prints the bare message text. Non-deterministic (timestamp +
  pid), so byte-parity here is impossible by construction.
- **`models` catalogue is server-driven.** Signed out, we match exactly.
  Signed in, the reference's list comes from the server; our list is a
  representative subset and is not byte-verified against a signed-in
  reference.
- **`dispatch.go` retains a superseded MCP/models generation**
  (`runMcp*`, `runModels`, `v135LoadMCPConfig`, `v135RenderMCPList`,
  `runAgentsList`) that the parser no longer routes to. It is left in
  place (out of scope to delete), but its tests were retargeted at the live
  handlers.
- **Harness scope.** Only the visible subcommand surface + simple handlers
  are covered. Interactive TUI, the real agent loop, mTLS, and OAuth flows
  are not part of this deliverable.
- Reference binary restored from
  `~/.local/bin/agy.bak-1.3.5-2026-10-15T08-23-29-532Z` after the earlier
  `install` tests clobbered it; SHA confirmed `ebd71c4d…`.

## Files

- `internal/cli/v135_parity.go` (new), `internal/cli/{jetski,parser,parser_specs,install,changelog_data}.go`
- `internal/cli/testdata/changelog_v135.txt` (new, 148,631 bytes)
- `cmd/antigravity/main.go`
- tests: `internal/cli/{close_gap,mcp_list,changelog_data,update_models,parser,install}_test.go`
- harness: `/tmp/agycmp/harness.sh` (scratch, not committed)
