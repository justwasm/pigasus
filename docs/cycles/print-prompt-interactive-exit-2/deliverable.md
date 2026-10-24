# print-prompt-interactive-exit-2 — AGY 1.3.4 changelog line 32

**Gap:** AGY 1.3.4 changelog line 32 — "Fixed combining `--print` with
`--prompt-interactive` exiting with status 0 after reporting the conflict;
it now exits with status 2 like other print-mode usage errors."

**Branch:** `feature/print-prompt-interactive-exit-2` (single commit)

**Base:** `main` @ `54db67b` (cycle 224)

## What the gap looked like before this cycle

In the AGY 1.3.4 binary, the recovered shell handled the conflict as
follows:

1. The parser (`internal/cli/jetski.go` lines 286–289) prints the canonical
   line `error: --print and --prompt-interactive are mutually exclusive`
   and exits with code 2 — that part was already correct.
2. `cmd/wagyu/main.go`'s `runRoot` had *no* equivalent check. The print
   mode block ran unconditionally because `printMode` was true, and the
   prompt-interactive block at line 341 was unreachable. A future caller
   that bypasses the parser (a new subcommand, a regression in `Parse`,
   the test rig) would silently accept both flags with `--print` winning.

The changelog fix narrows that surface: any path that sees both flags set
must reject the combination before reaching the model, with the same
"mutually exclusive" line and exit code 2 the parser already produces.

## Changes

| File | What changed |
|---|---|
| `cmd/wagyu/main.go` | Added a defense-in-depth check at the top of `runRoot` that detects `--print + --prompt-interactive` (`--print + -i` are both names for the first flag; `--prompt-interactive` and `-i` both cover the second). On conflict, prints the canonical error to stderr and returns `cli.ExitCodeError(2, "…")`. The check sits above the model dispatch so any direct caller of `runRoot` short-circuits before touching the backend. |
| `cmd/wagyu/print_prompt_interactive_test.go` | New test file with 11 test functions (20 test cases including subtests). Pins the exit-code contract on both the parser path and the new runRoot path; verifies no model call is made; verifies the recovered error message is preserved verbatim. |

The total production diff is +17 lines in `main.go`; everything else is
the new test file. `internal/convstore/sqlite.go` is untouched.

## Tests (all new, in `cmd/wagyu/print_prompt_interactive_test.go`)

| # | Test | What it pins |
|---|---|---|
| 1 | `TestPrintPromptInteractiveConflict_Exits2` (6 subtests) | Every alias combination — long/long, short/long, long/short, short/short, `--prompt` alias, reversed argument order — exits 2 with the "mutually exclusive" line and empty stdout (no model call) |
| 2 | `TestPrintPromptInteractiveConflict_StderrMentionsBothFlags` | The stderr line names both `--print` and `--prompt-interactive` so the operator can identify the conflicting pair |
| 3 | `TestPrintPromptInteractiveConflict_NotExit1` | The exit code is exactly 2, not 0 (the bug) and not 1 (the catch-all handler-failure path) |
| 4 | `TestPrintPromptInteractiveConflict_NoRegression` (3 subtests) | Other print-mode usage errors (`--input-format bogus`, positional argument, flag-shaped prompt) still exit 2 — the new runRoot guard does not broaden or narrow the contract |
| 5 | `TestPrintPromptInteractiveConflict_BeforeModelCall` | Spins up an `httptest.Server` that 500s on every request; confirms the parser-level guard short-circuits before the gateway is hit |
| 6 | `TestPrintPromptInteractiveConflict_RunRootGuard` | Defense-in-depth test: bypasses the parser by setting `--print` via `Parse` and `--prompt-interactive` via `fs.Set` on the resulting FlagSet, then calls `runRoot` directly. Confirms the new guard returns an `ExitCodeError` with code 2 carrying the mutually-exclusive message |
| 7 | `TestPrintPromptInteractiveConflict_RunRootGuardNoModelCall` | Same rig as (6) but stands up a gateway that 500s on every request; confirms the runRoot guard short-circuits before any backend dispatch |
| 8 | `TestPrintPromptInteractiveConflict_PrintAloneDoesNotTrigger` | `--print` alone on the shim backend produces the mock reply (rc 0, non-empty stdout) — the conflict must not be misfired when only `--print` is set |
| 9 | `TestPrintPromptInteractiveConflict_Exit2Exact` | Runs the conflict combo four times in a row; exit code stays at exactly 2 (off-by-one or copy-paste regression guard) |
| 10 | `TestPrintPromptInteractiveConflict_ErrorHasBothFlags` | Strict substring check on the recovered message — `error: --print and --prompt-interactive are mutually exclusive` is load-bearing |
| 11 | `TestPrintPromptInteractiveConflict_HelperExposesExitCode` | Sanity check on the test helper itself |

## Hard-constraint checklist

| Constraint | Status |
|---|---|
| `go build -o /tmp/wagyu-ppi ./cmd/wagyu` | exits 0 (verified locally; binary is 73 MB) |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` | passes for every package except two documented pre-existing flakes: `TestDispatchPrintSlash_LocalCommands` (a `/model` selector pinning issue from cycle 226 that lists `gemini-3.8-flash-medium` instead of `minimax-m3`) and `TestRunStatusLineScript_SpawnEcho` (a 1.01s spawn-flake, passes in isolation). Both fail identically on `main` HEAD without my changes |
| `gofmt -l` on every new file | empty |
| Pre-existing not-clean files left alone | confirmed; only `cmd/wagyu/main.go` was touched in `cmd/wagyu/` and the diff is the comment block + 6 new lines (not a reformat) |
| `internal/convstore/sqlite.go` untouched | confirmed (`git diff --stat` shows only `cmd/wagyu/main.go` and the new test file) |
| Branch not pushed | confirmed |
| Single commit | confirmed (see `git log --oneline -1` below) |
| Worktree at `/private/var/folders/.../openagy-print-prompt-interactive-exit-2` | confirmed |

## Verification

```bash
$ go build -o /tmp/wagyu-ppi ./cmd/wagyu && echo BUILD_OK
BUILD_OK

$ /tmp/wagyu-ppi --print=hi --prompt-interactive=bye
error: --print and --prompt-interactive are mutually exclusive
$ echo $?
2

$ /tmp/wagyu-ppi --print=hi
mock reply from model=Gemini 3.8 Flash (Medium): hi
$ echo $?
0

$ go test -vet=off -count=1 -run 'TestPrintPromptInteractive' ./cmd/wagyu/ -v
... (all 11 top-level tests pass, including 9 subtests)
PASS
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.0s
```

## Limitations / 存疑

- The recovered 1.3.4 changelog text references the *fix* — "Fixed combining
  --print with --prompt-interactive exiting with status 0 after reporting
  the conflict". The reference binary has not been re-decompiled to confirm
  *which* code path produces the fix; the most plausible reading is that
  the conflict line was already printed but the exit code was wrong (0
  instead of 2) — exactly what the runRoot guard prevents today. The parser
  side already exits 2 in this clone, so the fix is partly already
  applied; the runRoot guard is the missing defense-in-depth.
- The error message wording ("mutually exclusive") is taken from the
  existing parser-level message; if AGY 1.3.4 actually used different
  wording, the strict-substring test
  (`TestPrintPromptInteractiveConflict_ErrorHasBothFlags`) would need to
  be updated. The wording is also referenced in
  `docs/cycles/cycle-101/deliverable-A-print-args.md` (cycle 101) and
  `internal/cli/print_args_test.go`, so three places agree.
- `TestRunStatusLineScript_SpawnEcho` is a pre-existing flake that fails
  ~1 in N full runs but passes in isolation. It is not related to this
  change; stashing the patch reproduces it.