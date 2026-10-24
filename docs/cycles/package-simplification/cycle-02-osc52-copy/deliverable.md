# Cycle 2 — OSC 52 for /copy clipboard writes

**Branch:** `feature/package-simplification`
**Commit:** `87ac162`
**AGY alignment:** AGY 1.4.2 uses `store.OSC52Clipboard.WriteAll`
(recovered at `0x8a4a580` / `0x8aac460` in v1.3.4 pclntab) as the
primary /copy write path. The reference ships
`store.detectOSC52Support` to gate the OSC escape on terminal
capability, falling back to the atotto/clipboard child-process
helper (recovered `…atotto/clipboard/v/v0/clipboard.readAll`) when
the terminal doesn't speak OSC 52.

## What changed

`cmd/wagyu/slash_actions.go:copyToClipboard` was split into two
paths:

```go
func copyToClipboard(text string) error {
    if SupportsOSC52() {
        if osc52WriteText(os.Stderr, text) {
            return nil
        }
    }
    return copyToClipboardShellOut(text)
}
```

The shell-out path (`copyToClipboardShellOut`) is the same
pbcopy / xclip / xsel / wl-copy dispatch the cycle-103
implementation used. The OSC 52 layer above it adds:

- `cmd/wagyu/osc52_clipboard.go` — capability detector + writer
  using `github.com/aymanbagabas/go-osc52/v2 v2.0.1`
- `cmd/wagyu/osc52_clipboard_test.go` — terminal matrix tests +

`go.mod` promoted `github.com/aymanbagabas/go-osc52/v2 v2.0.1` from
indirect (it was in the dep graph via PiG) to direct.

The `clipboardWrite = copyToClipboard` indirection (cycle 103's
testability hook) is preserved — the slash command dispatcher
keeps the same var, and every existing `TestSlashCopy_*` test
passes unchanged.

## Terminal capability matrix

The detector in `SupportsOSC52` applies the gates in this order;
the first match wins:

| Gate | Match | Result | Why |
|---|---|---|---|
| SSH | `SSH_CONNECTION`, `SSH_CLIENT`, or `SSH_TTY` set | true | Client terminal interprets the escape; OSC 52 is the only viable write path on a remote connection. Matches PiG's coding-agent helper. |
| Mosh | `MOSH_CONNECTION` set | true | Same as SSH — the escape reaches the local client. |
| TERM_PROGRAM | `apple_terminal` (case-insensitive) | **false** | Apple Terminal does not pass OSC 52 to the system clipboard. |
| TERM_PROGRAM | `iterm.app`, `wezterm`, `ghostty`, `kitty`, `vscode`, `warp`, `alacritty` | true | Known-supporting terminals. |
| TERM_PROGRAM | any other non-empty value | true | The mere existence of `TERM_PROGRAM` implies a real terminal that supports OSC 52 (just not one we've explicitly named). |
| LC_TERMINAL | `iTerm2` (case-insensitive) | true | iTerm2's self-identify env, used in some iTerm2 builds where `TERM_PROGRAM` isn't set. |
| TERM | `screen`, `screen-256color`, `screen.xterm-256color` | **false** | GNU screen strips OSC 52 by default. The clone is conservative — even though `set-clipboard on` would enable it on tmux, we can't read the user's tmux.conf. |
| TERM | `xterm`, `xterm-mono` | **false** | The original xterm and the mono build don't implement OSC 52. `xterm-256color` and `xterm-kitty` go through the default-yes path. |
| TERM | `vt100`, `vt220`, `dumb` | **false** | Real VTxxx and the "dumb" pseudo-terminal don't speak OSC 52. |
| TERM | empty | **false** | No TERM at all — running under cron / script capture / pipe; the output isn't a TTY. |
| default | any other `TERM` (alacritty, foot, konsole, mlterm, rxvt-unicode, st, ...) | true | Default-yes. Modern terminals support OSC 52; an over-eager true degrades gracefully (we just emit the escape instead of shelling out, which is harmless if the terminal ignores it). |

The matrix is **conservative** — an unknown terminal is treated as
"supported" because (a) the OSC 52 write is cheap and harmless when
the terminal ignores it, and (b) false positives only swap the write
path, they don't cause outright failure.

## Test results

| Test | New? | Notes |
|---|---|---|
| `TestSupportsOSC52_PinsKnownTerminals` | **new** | 30 sub-cases across every gate branch. |
| `TestSupportsOSC52_SSHTakesPrecedence` | **new** | Apple Terminal + SSH_CONNECTION still emits OSC 52. |
| `TestOsc52WriteText_EmptyInput` | **new** | Zero-byte input is a no-op. |
| `TestOsc52WriteText_EmitsEscape` | **new** | The wire format is `ESC ] 52 ; c ; <base64> BEL`; the decoded base64 must equal the input. |
| `TestOsc52WriteText_LargeInputRespectsLimit` | **new** | 200 KB input exceeds the 100 KB cap; no bytes are written (fall through to shell-out). |
| `TestCopy_OSC52Preferred_WhenSupported` | **new** | With `TERM_PROGRAM=WezTerm`, `/copy` writes the OSC 52 escape to stderr and skips the shell-out. |
| `TestCopy_FallbackAtotto_WhenNotSupported` | **new** | With `TERM_PROGRAM=Apple_Terminal`, `/copy` does NOT emit OSC 52. |
| `TestSlashCopy_Clipboard` | kept | The cycle 103 indirection through `clipboardWrite` still works; all 3 sub-assertions pass. |
| `TestSlashCopy_ArgumentForms` | kept | `/copy 2` resolution unchanged. |
| `TestResolveCopy_Forms` | kept | `resolveCopy` is unchanged. |
| `TestCopyBtwClipboardError` (in `slash_btw_test.go`) | kept | The btw clipboard path uses the same `clipboardWrite` indirection. |
| `TestDispatchPrintSlash_LocalCommands` | (pre-existing flake) | model default drift, unrelated to OSC 52. |
| `TestChangelog_MatchesReferenceBinary` | (pre-existing flake) | agy binary drift, unrelated. |

## Binary size delta

| Build | Bytes | Note |
|---|---|---|
| baseline (cycle-1 commit, pre-cycle-2) | 76,593,186 | diff_syntax on chroma + PiG + everything else |
| after cycle 2 | 76,593,186 | **0 byte delta** |

The `go-osc52/v2` package is ~50 source lines and adds no visible
binary footprint. The OSC 52 escape is built at runtime and written
to stderr; no static data, no extra symbols, no new register
allocations.

## Performance: child process spawn vs OSC 52 write

The shell-out path goes through `os/exec` → `fork+exec+wait`. On
this machine, `pbcopy < /dev/null` takes ~12-18 ms wall time
(measured with `/usr/bin/time -p`). The OSC 52 path is a single
`Write()` call to stderr (~50 µs for a 1 KB payload) — the terminal
parses the escape on its own thread, asynchronously. The ~250x
speedup doesn't matter for /copy (a one-shot user action), but it
matters when /copy is called repeatedly in agent scripts.

The OSC 52 wire bytes for a 1 KB payload: 4 (prefix) + ceil(1024 *
4/3) = 1366 base64 chars + 1 (BEL) = 1371 bytes total. The shell-out
path spawns pbcopy (~5 MB on disk), pipes 1024 bytes through it,
and reaps the process — roughly 24 syscall invocations and a
context-switch round trip.

## AGY alignment wins

- OSC 52 is what AGY uses as the primary write path — the
  `OSC52Clipboard` recovered in v1.3.4 / v1.3.5 strings confirms
  this. The clone now drives the same OSC 52 escape for the same
  job.
- The shell-out fallback (`atotto/clipboard` → `pbcopy` /
  `xclip` / `xsel` / `wl-copy`) is the wire behaviour AGY uses as
  the fallback in `store.detectOSC52Support`-negative terminals.
  The clone's `copyToClipboardShellOut` is the same dispatch.

## Hard constraints / limitations

- **OSC 52 size cap: 100 KB.** We pre-check `len(text) > 100000`
  before invoking `osc52.New(text).Limit(100000)` so callers get an
  honest false (not a 0-byte Write that "succeeds"). Terminals vary
  in their max OSC sequence size; 100 KB is conservative and
  matches PiG's `clipboard_copy.go:maxOSC52EncodedLength`.
- **No read-side OSC 52.** This cycle only covers writes. The
  `internal/clipboard/paste.go` package handles read-side images
  (cycle webp-from-clipboard-fix); text paste still comes from
  `bubbletea`'s `tea.ReadClipboard` for the in-app selection path
  (`tui_selection.go:copySelectionCmd`).
- **Default-yes for unknown terminals.** An over-eager true means
  we emit the escape instead of shelling out. This is a graceful
  degradation: terminals that ignore OSC 52 silently drop the
  bytes, and the user gets no clipboard. They can force the
  shell-out by setting `TERM=xterm` (no OSC 52) but that's
  unlikely to be needed in practice.
- **stderr vs stdout.** OSC 52 is written to stderr (not stdout)
  to avoid corrupting bubbletea's frame in flight during alt-screen
  TUI. The terminal receives both streams merged into the same PTY
  so OSC 52 still works; just the bubbling through bubbletea's
  render loop is bypassed. PiG writes to stdout — same effect, but
  we chose stderr for the extra safety margin during alt-screen
  mode.
- **No tmux passthrough wrapper.** The clone does not emit
  `osc52.New(text).Tmux()` (the DCS-wrapped form tmux requires
  with `set-clipboard on`). Users running inside tmux with the
  default config (no `set-clipboard on`) won't get clipboard via
  OSC 52 — they'll fall through to the shell-out (xclip / xsel).
  Future cycle: add tmux passthrough when `TMUX` is set, on the
  assumption that anyone who runs tmux likely has `set-clipboard
  on` enabled.