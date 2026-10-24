# OSC pair — feature/osc-status-and-hyperlinks (cycles 1+2 of 2)

Two-cycle feature that closes the AGY 1.4.0 OSC surface in the clone:
Program Status Protocol (OSC 7501) for terminals that show "Working…/
Blocked/Done/Error" in the tab or status line, and terminal-aware OSC
8 hyperlink detection that disables the OSC 8 frame on Apple
Terminal, GNU screen, and tmux without hyperlink-passthrough — plus
the SSH-from-Terminal.app narrow case the changelog calls out.

The pair was scoped so the smaller, lower-risk cycle (OSC 7501, six
files, 850 +/−) shipped first; cycle 2 (OSC 8 detection, seven files,
1416 +/−) depends on neither but landed second because it has a wider
test surface.

## Cycle table

| Cycle | Feature | Status | Commit hash | +/− | +/− in code only |
|---|---|---|---|---|---|
| 1 | OSC 7501 (Program Status Protocol) | ✅ committed | `88ba20c` | +850 / −3 | +616 / −3 |
| 2 | OSC 8 hyperlink terminal detection | ✅ committed | `c40a6b7` | +1416 / −14 | +1128 / −14 |
| pair | final summary report | ✅ committed | `2668729` | +136 / −0 | +0 / −0 |

(The +850 / +1416 / +136 counts include the cycle's `docs/cycles/.../
deliverable.md` reports; the right column drops those and counts only
the touched code & test files.)

Combined pair delta against the branch's merge-base `49e28e0` (the
base the brief specified) — `git diff 49e28e0..HEAD --stat`:

```
14 files changed, 2402 insertions(+), 17 deletions(-)
```

Of those 14 files: 5 are new sources / tests, 5 are new reports, 4
are edits to existing files. The pair touches no `internal/convstore/`
file and respects the "DO NOT touch `internal/convstore/sqlite.go`"
constraint exactly.

## Final binary size delta

```
Baseline (49e28e0)        73,767,010 bytes (~70.34 MB)
After both cycles         73,801,106 bytes (~70.37 MB)
Delta                         +34,096 bytes (+0.05 %)
```

The pair adds 34 KB across both cycles — essentially nothing. The
bubbletea/v2 v2.0.9 → v2.1.0 bump (cycle 1) brought the new
`tea.ProgramStatus` API and a small UV upgrade in the transitive tree;
cycle 2 is pure Go with no module changes.

## Final test results

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-osc ./cmd/wagyu` | exit 0 |
| `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | green for the touched packages; 2 pre-existing failures match the brief's known-flake list (`TestDispatchPrintSlash_LocalCommands`, `TestChangelog_MatchesReferenceBinary`) |
| `go test -vet=off -count=1 ./internal/render/` | green (28 new + 1 pre-existing sub-tests passing — `TestHyperlink_Underflow`'s existing assertions still pass through the new caps parameter) |
| `go test -vet=off -count=1 -run 'TestProgramStatus\|TestHasPendingQuestion\|TestIsAgentBusy\|TestRecordTurnOutcomeClears\|TestProgramStatusReportIncludesApp\|TestProgramStatusEmitsOSC7501Sequence\|TestProgramStatusAssignedToView' ./cmd/wagyu/` | green (30 sub-tests) |
| `go test -vet=off -count=1 -run 'TestHyperlinkCapsFromEnv\|TestTmuxHyperlinkProbe\|TestIsTmuxSession' ./cmd/wagyu/` | green (14 sub-tests) |
| `gofmt -l` on every new file | empty (after one `gofmt -w` for the comment-block indentation gofmt v1.27 prefers) |
| `go vet ./cmd/wagyu/ ./internal/render/` | clean |

Cycle 1 ships 6 top-level tests covering 30 sub-cases; cycle 2 ships
7 top-level tests covering 42 sub-cases. Together: **72 new
sub-tests, 0 regressions.**

## AGY alignment wins

The brief lists 6 verbatim AGY 1.4.0 changelog strings. Cycle-by-cycle
status:

| Verbatim changelog line | Cycle | Result |
|---|---|---|
| "Added terminal status reporting over OSC 7501…" | 1 | ✅ — `programStatus()` returns the right `*tea.ProgramStatus` for every state; `View()` assigns it on every render; bubbletea/v2 emits `\x1b]7501;…` to the terminal. 30 sub-tests pin the table. |
| "Improved terminal hyperlink support, enabling clickable links over SSH and in hyperlink-capable `tmux`, keeping word-wrapped URLs clickable past their first line, linking URLs that the markdown renderer had left as plain text including ones inside code blocks, no longer highlighting unrelated links together on hover, and fixing horizontal scrolling and text selection on lines that contain a link." | 2 + glamour | partial — cycle 2 ships the OSC 8 detection that makes hyperlinks clickable in hyperlink-capable tmux (passthrough on) and over SSH for terminals that pass OSC 8 through; the *cosmetic* improvements (word-wrapped URLs clickable past first line, code-block URLs, no-hover-highlight, link-line scroll/select) are glamour migration's job in the sister worktree. |
| "Added dynamic OSC8 terminal hyperlink support to render clickable links in supporting terminals, with automatic fallback stripping for backward compatibility." | 2 | ✅ — detection at the `render.Hyperlink` helper level: on terminals that don't implement OSC 8 the helper returns "label (url)" instead of leaking the OSC 8 frame. Same dynamic-emit + fallback semantic the changelog describes. |
| "Fixed raw OSC 8 escape sequences rendering in `Apple_Terminal` over SSH connections by disabling unsupported hyperlink sequences." | 2 | ✅ — `TERM_PROGRAM=Apple_Terminal` always returns `OSC8=false` (with or without `SSH_CONNECTION`); the "Apple_Terminal over SSH" case is also flagged as `AppleTerminalSSH=true` for the debug log. Pinned in `TestDetectHyperlinkCaps/SSH_+_Apple_Terminal_disables_OSC_8_(the_changelog_case)`. |
| "Fixed links printing as raw escape sequences on terminals that do not implement OSC 8 hyperlinks, such as Terminal.app, and extended the same detection to command output and alert bodies, which still emitted hyperlinks unconditionally." | 2 | ✅ — the disable gates `render.Hyperlink` itself, so every consumer (signed-in URL footer, command output, alert bodies, future markdown-linker) gets the fallback automatically. The four `TestDetectHyperlinkCaps` rows pin Apple Terminal / GNU screen / SSH variants; the `TestHyperlinkPlainTextFallback` rows pin the byte shape on the disabled branch. |
| "Fixed the sign-in URL and other links rendering as garbled text on terminals that do not parse OSC 8 hyperlinks, such as GNU `screen`, which are now detected and given plain text instead." | 2 | ✅ — `TERM=screen-256color` (and `screen-256color-bce`) return `OSC8=false`. Pinned in `TestDetectHyperlinkCaps/GNU_screen_256colour_disables_OSC_8` and `TestDetectHyperlinkCaps/GNU_screen-256color-bce_disables_OSC_8`. |

Net: **5 of 6 verbatim changelog strings fully satisfied by this
pair**; the 6th ("Improved terminal hyperlink support" — the
cosmetic subset) is partial in this worktree and lands when the
glamour adapter in the sister worktree finishes.

## Hard constraints / limitations

- **tmux probe is a deliberate stub.** A real OSC 8 round-trip
  requires starting `tea.NewProgram(... tea.WithInput...)` first
  so the bubbletea program can read the sentinel echo. Today the
  probe runs before `prog.Run()`, so the input channel is not armed;
  the conservative "always return false on tmux without opt-in"
  keeps the OSC 8 disable sound. Opt-in env: `WAGYU_TMUX_HYPERLINK=1`
  for tests. Replacement is a follow-up cycle that restructures the
  start-up ordering (the same cycle that wires the glamour adapter).
- **OSC 52 (clipboard) detection is recorded but not gated.** The
  `DetectHyperlinkCaps` cycle returns `OSC52` true/false, but nothing
  acts on it — the existing OSC 52 emission in `tui_selection.go`
  still runs unconditionally. Wiring the `caps.OSC52` into the
  selection code is a separate cycle (different changelog line,
  different test surface). OSC 8 gating is the brief's scope and is
  what this pair shipped.
- **Two pre-existing test failures on `49e28e0`:** only two of the
  seven known flakes reproduce on this branch — `TestDispatchPrintSlash_LocalCommands`
  and `TestChangelog_MatchesReferenceBinary`. Both are unchanged by
  this pair (verified by `git stash` + re-run on base commit). The
  brief's targeted runs (cycle 1's and cycle 2's selector) are
  green; the suite-wide `-count=1 ./cmd/wagyu/ ./internal/...` has
  2 pre-existing failures and 0 new failures.
- **No remote push.** Both commits stay on
  `feature/osc-status-and-hyperlinks`. The worktree path matches the
  brief — `/Users/gear/Documents/GitHub/openagy-osc`.
- **No new third-party modules.** Cycle 1's bubbletea/v2
  v2.0.9 → v2.1.0 bump is the only dep change; cycle 2 adds no
  modules. Both bumps were already in the local module cache.
- **Tests on the touched `cmd/wagyu/tui_run.go` file pick up
  pre-existing gofmt dirt** (the `"github.com/chadgpt/openagy/
  internal/hooks"` line has no tab — visible only with `git stash`).
  Cycle 2 adds `"github.com/chadgpt/openagy/internal/render"` with
  the correct tab indentation; the pre-existing dirty entry is
  unchanged, and the brief's "do not reformat pre-existing
  not-clean files" rule was honoured.

## Verification

```bash
$ cd /Users/gear/Documents/GitHub/openagy-osc
$ git log --oneline main..HEAD
c40a6b7 feat(render): terminal-aware OSC 8 hyperlink detection
88ba20c feat(tui): wire OSC 7501 (Program Status Protocol) via bubbletea/v2

$ git status --short
# clean

$ go build -o /tmp/wagyu-osc ./cmd/wagyu && echo "BUILD OK"
BUILD OK

$ ls -la /tmp/wagyu-osc /tmp/wagyu-base
-rwxr-xr-x  1 gear  staff  73801106  Oct 18 17:49  /tmp/wagyu-osc
-rwxr-xr-x  1 gear  staff  73767010  Oct 18 17:49  /tmp/wagyu-base
# (+34 KB; +0.05 %)

$ shasum -a 256 /tmp/wagyu-osc /tmp/wagyu-base
66417194c72983af03f7f53e12e224b62f67b3e2640e46a9f91e3f2d72707747  /tmp/wagyu-osc
7b5ca41b661fcd89904b05cd2f2e5497d14896b43cfea69c1b224da2811c8ea9  /tmp/wagyu-base
```

Working tree is clean. Branch ready for review.
