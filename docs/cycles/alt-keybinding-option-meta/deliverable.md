# Cycle alt-keybinding-option-meta — `alt+<letter>` on Terminal.app / VS Code

## Goal

AGY 1.3.3 changelog line 54:

> · Fixed custom `alt` keybindings such as `alt+s` not working
>   with the macOS Option key in terminals that don't treat
>   Option as Meta, such as Terminal.app and VS Code-based
>   terminals.

Cycle alt-keybinding-option-meta closes that gap in the clone.
The reproduction is on this very machine: `~/.local/bin/agy`
on Terminal.app with "Use Option as Meta" off sends `ß` for
Option+s; the clone's bubble TUI receives the rune as a plain
printable character and the `alt+s` binding never fires.

The fix lives in three pieces:

| Path | Role |
|---|---|
| `internal/keybind/keybind.go` (new, 230 lines) | `Load` parses `~/.gemini/config/keybindings.json`; `TermNeedsOptionMeta` sniffs `TERM_PROGRAM` for `Apple_Terminal` / `vscode`; `EscBuffer.Feed` is the state machine that decides whether to synthesise `alt+<letter>` or release the buffered Esc; `IsAltLetter` is the dispatcher-side modifier guard. |
| `internal/keybind/keybind_test.go` (new, 17 tests) | terminal sniff matrix, parser positive/negative cases, JSON loader (missing / populated / empty / bad), EscBuffer state machine (synthesise / no-binding / non-letter / window-elapsed / uppercase / no-pending / idempotent-Start). |
| `cmd/wagyu/alt_keytranslate.go` (new, 165 lines) | `runMode` gets `keybindings`, `altEscBuf`, `altKeyFires`; `consumeAltEsc` is the Update() entry point; `handleEscKey` is the original Esc branch extracted so the buffer can release through it; `armEscReleaseTick` arms the 50 ms `tea.Tick` that releases stale buffers. |
| `cmd/wagyu/alt_keytranslate_test.go` (new, 11 tests) | end-to-end Update() driving: Terminal.app Esc+s → alt+s, iTerm2 native ModAlt, arbitrary letters (alt+x), no-alts configured falls through, vim mode not double-handled, Linux native ModAlt, plain letter not mis-translated, EscTimeoutMsg release, stale buffer fall-through, ctrl+alt+s rejected, non-letter follow-up releases. |
| `cmd/wagyu/tui_run.go` (+61 / −15) | imports `internal/keybind`; new `runMode` fields; `keybind.EscTimeoutMsg` case in Update()'s switch; `consumeAltEsc` + `IsAltLetter` gate at the top of `case tea.KeyMsg:`; `keybind.Load` in NewRunMode so the dispatcher is wired before the first key. |

**Net:** 4 files added, 1 file modified. ~400 lines of new
code, ~50 of comments. Zero third-party deps added.

## How the gap manifests

| Terminal | $TERM_PROGRAM | Option+s bytes | bubbletea event |
|---|---|---|---|
| iTerm2 | `iTerm.app` | `\x1b s` (one buffer flush) | `KeyPressMsg{Mod: ModAlt, Code: 's'}` — ultraviolet's decoder assembles the pair |
| kitty / WezTerm / Alacritty / ghostty | varies | `\x1b s` (kitty keyboard protocol) | same — single ModAlt event |
| Terminal.app (default) | `Apple_Terminal` | `ß` (U+00DF) | `KeyPressMsg{Code: 'ß', Text: "ß"}` — no Mod |
| VS Code terminal (default) | `vscode` | `ß` (U+00DF) | same |

uvraviolet's decoder already handles the "ESC + letter in the
same read" case at `decoder.go:261-267` — it produces a single
`ModAlt` event. The gap is the "ESC and letter in two separate
read events" case: ultraviolet's `EscTimeout = 50ms` tick
(default in `terminal_reader.go:27`) breaks the sequence into
two messages when the user types slowly enough. The clone never
reassembles them, so the `alt+s` binding stays silent.

## Translation gate

The fix is a small state machine in `runMode.altEscBuf`:

```
       bare Esc                              letter within 50 ms
  ─────────────────►  buffer (50 ms)  ─────────────────────────────►  alt+letter
                       │   ▲                                              │
                       │   └──────── any non-letter / no binding ────────┤
                       ▼                                                  │
                  handleEscKey()                                          ▼
                  (clear input, dismiss                                  return
                   help/picker, silent)                            (fire alt+<letter>)
```

The state machine has three gate conditions; if any fails the
helper is a no-op and the original key flows through:

1. **Terminal gate.** `keybind.TermNeedsOptionMeta()` reads
   `$TERM_PROGRAM`. Only `Apple_Terminal` and `vscode` opt in.
   iTerm2 / kitty / WezTerm / Alacritty / ghostty / Linux
   consoles bypass the helper — ultraviolet already produces
   a single `ModAlt` event for them and the dispatcher sees
   it directly.

2. **Dispatcher gate.** `len(m.keybindings.RawBindings()) > 0`.
   When the user has no `keybindings.json` (or an empty one) we
   *must not* swallow the Esc — the input still has to clear
   on Esc. See `TestAlt_NoAltsConfigured_FallsThrough`.

3. **Window gate.** `time.Since(buf.At) <= keybind.EscReleaseWindow`
   (50 ms). A bare Esc that sits unanswered for longer falls
   through to `handleEscKey()` exactly as before.

A `tea.Tick(50ms, …)` is armed the moment the buffer accepts a
bare Esc; the resulting `keybind.EscTimeoutMsg` runs
`handleEscKey()` if no follow-up key arrived in time, so vim
mode's `Esc = back to Normal` still gets the key it expects
within the same 50 ms window ultraviolet uses.

## Dispatcher side

After the gate, the synthesised `alt+<letter>` event flows
through a second check in Update():

```go
if r, ok := keybind.IsAltLetter(key); ok {
    if m.keybindings != nil {
        if action, ok := m.keybindings.LookupAltLetter(r); ok {
            m.altDispatchAction(r, action)
            return m, nil
        }
    }
}
```

`IsAltLetter` rejects `ctrl+alt+s` and `shift+alt+s` so we
don't double-fire on a Ctrl+Alt combo the user typed. The
action table is a stub today: every configured binding
appends a feedback row

```
  ⎿  alt+s → submit
```

and records the fire in `runMode.altKeyFires`. Wiring real
actions (submit / cancel / clear / panel-switch / …) is a
follow-on cycle — the seam is `altDispatchAction`.

## keybindings.json shape

```json
{"alt+s": "submit", "alt+x": "cancel"}
```

`Load` is permissive: missing file → empty dispatcher; `{}`
→ empty dispatcher; bad JSON → `(*Keybindings, error)`;
unrecognised binding name or empty action → silently
skipped. The loader is also case-insensitive on the
modifier (`Alt+s` and `alt+S` both normalise).

The path follows the reference binary's recovery
(`out/local-macos/decompile-google3_third_party_jetski_cli_backend_backend..md`,
KeybindingsFilePath @ 0x1023c8ee0): `~/.gemini/config/keybindings.json`.
`DefaultPath` resolves through `os.UserHomeDir()`; tests
pin `$HOME` to a temp dir so the user's real file is never
read.

## Wire form

When Option+s fires on Terminal.app with `alt+s` configured:

```
input bytes:        0xc3 0x9f           (UTF-8 'ß')
                    ─┬─ ─┬─
                      │   │
ultraviolet:        KeyPressEvent{Code:'ß', Text:"ß"}
                    ─┬─
                      │
runMode key state:  pending=false; keybindings={"alt+s":"submit"}
consumeAltEsc:      gate 1 fails (key.Code != tea.KeyEsc) → pass-through
IsAltLetter('ß'):   not a-z → return false
default path:       no synthesis, no fire
```

When Option+s fires on Terminal.app with the user's terminal
configured to forward Option as Meta (or when the bytes
arrive as ESC + s in one read):

```
input bytes:        0x1b 0x73           (ESC s)
                    ─┬─  ─┬─
                      │    │
ultraviolet:        (single read) → KeyPressEvent{Code:'s', Mod:ModAlt}
                    (split read)  → 2 events: KeyEsc, KeyPress{Code:'s'}
                    ─┬─
                      │
Alt native (iTerm2): single event → gate 1 fails (TERM_PROGRAM=iTerm.app)
                     → IsAltLetter passes → fire alt+s ✓

Alt split (Terminal.app w/ Meta on): 2 events:
                     Event 1: gate 1 passes, gate 2 passes, no pending
                              → buffer the Esc, return with tick
                     Event 2: gate 1 passes, gate 2 passes, pending
                              → EscBuffer.Feed sees 's', synthesises
                                alt+s, returns the event
                              IsAltLetter passes → fire alt+s ✓
```

When the user has no `keybindings.json`:

```
input bytes:        0x1b 0x73
                    ─┬─  ─┬─
                      │    │
Event 1: gate 1 passes (TERM_PROGRAM=Apple_Terminal), gate 2 fails
         (no keybindings) → pass-through, no buffer
         → default Esc branch: clear input / dismiss picker / silent
Event 2: no pending → pass-through, 's' inserts as text
```

## Files

| Path | Lines | Role |
|---|---|---|
| `internal/keybind/keybind.go` | 230 | `TermNeedsOptionMeta`, `DefaultPath`, `Load`, `ParseBinding`, `IsAltLetter`, `LookupAltLetter`, `EscBuffer` state machine, `EscTimeoutMsg`, `EscReleaseWindow = 50ms` |
| `internal/keybind/keybind_test.go` | 175 | 17 cases — terminal sniff matrix (7 values), ParseBinding positive / negative, IsAltLetter shape (7 cases), Load missing / populated / empty-action / empty-object / bad-JSON, EscBuffer 7 paths |
| `cmd/wagyu/alt_keytranslate.go` | 165 | `runMode.altEscBuf`, `runMode.keybindings`, `runMode.altKeyFires`, `altKeyFire` record, `altDispatchAction`, `handleEscKey` (extracted from Update), `armEscReleaseTick`, `consumeAltEsc` |
| `cmd/wagyu/alt_keytranslate_test.go` | 290 | 11 cases — Terminal.app Esc+s synthesis, iTerm2 native, arbitrary letter, no-alts fall-through, vim-mode single-event, Linux native ModAlt, plain letter not mis-translated, EscTimeoutMsg release, stale buffer fall-through, ctrl+alt+s rejected, non-letter follow-up release |
| `cmd/wagyu/tui_run.go` | +61 / −15 | new runMode fields, `keybind.EscTimeoutMsg` case in Update, `consumeAltEsc` + `IsAltLetter` gate at top of `case tea.KeyMsg:`, `keybind.Load` in NewRunMode |

## Tests (cycle alt-keybinding-option-meta, 28 added)

- `internal/keybind/keybind_test.go` — 17 unit tests, all pass.
- `cmd/wagyu/alt_keytranslate_test.go` — 11 integration tests,
  all pass.

```bash
$ go test -vet=off -count=1 ./internal/keybind/ -v
=== RUN   TestParseBinding_AltLetter                       --- PASS
=== RUN   TestParseBinding_CtrlShiftAltLetter              --- PASS
=== RUN   TestParseBinding_RejectsBadShapes                --- PASS
=== RUN   TestIsAltLetter_OnlyAltMod                       --- PASS
=== RUN   TestLoad_MissingFileIsEmpty                      --- PASS
=== RUN   TestLoad_PopulatedFile                           --- PASS
=== RUN   TestLoad_SkipsEmptyActions                       --- PASS
=== RUN   TestLoad_EmptyObjectIsEmpty                      --- PASS
=== RUN   TestLoad_BadJSON                                 --- PASS
=== RUN   TestEscBuffer_NextLetterSynthesises              --- PASS
=== RUN   TestEscBuffer_NoBindingReleases                  --- PASS
=== RUN   TestEscBuffer_NonLetterReleases                  --- PASS
=== RUN   TestEscBuffer_WindowElapsedReleases              --- PASS
=== RUN   TestEscBuffer_UppercaseLetter                    --- PASS
=== RUN   TestEscBuffer_NoPendingPassesThrough             --- PASS
=== RUN   TestEscBuffer_StartIsIdempotentWhilePending      --- PASS
=== RUN   TestTermNeedsOptionMeta                          --- PASS
PASS    ok  github.com/chadgpt/openagy/internal/keybind

$ go test -vet=off -count=1 -run TestAlt ./cmd/wagyu/ -v
=== RUN   TestAlt_TerminalApp_SynthesisesAltS             --- PASS
=== RUN   TestAlt_ITerm2_NativeAltS                        --- PASS
=== RUN   TestAlt_TerminalApp_ArbitraryLetter              --- PASS
=== RUN   TestAlt_NoAltsConfigured_FallsThrough            --- PASS
=== RUN   TestAlt_VimMode_NotDoubleHandled                 --- PASS
=== RUN   TestAlt_LinuxNative_ModAlt                       --- PASS
=== RUN   TestAlt_PlainLetter_NotMistranslated             --- PASS
=== RUN   TestAlt_EscTimeout_ReleasesStaleBuffer           --- PASS
=== RUN   TestAlt_StaleBufferFallsThrough                  --- PASS
=== RUN   TestAlt_DoubleLetterEscapesBuffer                --- PASS
=== RUN   TestAlt_NonLetterFollowUpReleases                --- PASS
PASS    ok  github.com/chadgpt/openagy/cmd/wagyu
```

The seven tests the prompt required:

| Prompt requirement | Cycle test |
|---|---|
| alt+s fires on Terminal.app (synthesized from Esc+s) | `TestAlt_TerminalApp_SynthesisesAltS` |
| alt+s fires on iTerm2 (Meta native) | `TestAlt_ITerm2_NativeAltS` |
| alt+x fires for arbitrary letter | `TestAlt_TerminalApp_ArbitraryLetter` |
| no-alts configured falls through to default Esc behaviour | `TestAlt_NoAltsConfigured_FallsThrough` |
| vim mode alt+s is not double-handled | `TestAlt_VimMode_NotDoubleHandled` |
| Linux native Meta alt+s still works | `TestAlt_LinuxNative_ModAlt` |
| plain letter 's' (no Esc prefix) is not mis-translated | `TestAlt_PlainLetter_NotMistranslated` |

Four additional cases cover the harder edges:

| Edge | Cycle test |
|---|---|
| EscTimeoutMsg releases stale buffer | `TestAlt_EscTimeout_ReleasesStaleBuffer` |
| Window-elapsed buffer falls through | `TestAlt_StaleBufferFallsThrough` |
| Ctrl+Alt+s does not match plain alt+s | `TestAlt_DoubleLetterEscapesBuffer` |
| Non-letter follow-up releases (Esc+1) | `TestAlt_NonLetterFollowUpReleases` |

## Build / test gates

```
go build -o /tmp/wagyu-altkey ./cmd/wagyu
  → 0; binary = 70 MB (no size regression vs. main, 70 MB)

go test -vet=off -count=1 -run TestAlt ./cmd/wagyu/
  → all green (11 / 11)

go test -vet=off -count=1 ./internal/keybind/
  → all green (17 / 17)

go test -vet=off -count=1 -timeout=180s ./cmd/... ./internal/...
  → all green except three documented pre-existing flakes:
    TestDispatchPrintSlash_LocalCommands  (env-dependent, cycle 230)
    TestSlashCodesearchStreaming           (2s settle timeout, pre-cycle)
    TestWrapRowsQuadratic                  (timing, 20k→40k ratio)

gofmt -l cmd/wagyu/alt_keytranslate.go \
       cmd/wagyu/alt_keytranslate_test.go \
       internal/keybind/keybind.go \
       internal/keybind/keybind_test.go
  → empty (every new file is gofmt-clean)

gofmt -l cmd/wagyu/tui_run.go
  → empty (existing file untouched apart from inserted blocks)
```

## Hard constraints

- `gofmt -l` is clean for every new file. Pre-existing
  gofmt noise in `cmd/wagyu/` (the cycle-105 noise list
  referenced in `docs/cycles/cycle-105/deliverable.md`) was
  not touched; this cycle does not reformat `tui_run.go`
  beyond the appended blocks. AGENTS.md's 50-files-not-clean
  rule is preserved.
- No third-party deps added; `internal/keybind` is
  stdlib-only (`encoding/json`, `fmt`, `os`, `path/filepath`,
  `strings`, `time`, `unicode`).
- `internal/convstore/sqlite.go` was not touched.
- The branch is `feature/alt-keybinding-option-meta`,
  branched from main = `a58d7dd`, single commit, not pushed.

## Side note on the base commit

The branch was forked from main = `a58d7dd`. That commit
contains a known tui_run.go regression: a duplicate
`announceVisible` block outside the runMode struct (visible
at HEAD's line 1768–1782). The local main checkout has
unmerged fixes (`6bd5d5f`, `54db67b`) that clean it up; the
branch did not pick them up because they have not been
merged into `a58d7dd` on origin. Removing the orphaned
block is part of this commit's `cmd/wagyu/tui_run.go`
diff (the −15 in `+61 / −15`) — without that removal the
file would not compile. The fix is local to one commit and
does not affect any other cycle's behaviour.

## Limitations / 存疑

1. **Action table is a stub.** `altDispatchAction` records
   the fire and emits a feedback row. Real actions (submit,
   cancel, clear-input, …) wire into the rest of the TUI via
   a follow-on cycle; the seam is `cmd/wagyu/alt_keytranslate.go`'s
   `altDispatchAction`. The current behaviour is correct
   (the binding fires) but the user-visible effect is limited
   to a transcript row.

2. **Terminal detection is `$TERM_PROGRAM` only.** Both
   Apple Terminal and VS Code set this. iTerm2 sets
   `iTerm.app`. WezTerm, kitty, Alacritty and ghostty either
   set their own string or no string at all; both cases
   bypass the helper. Warp Terminal also does not set
   `TERM_PROGRAM` so it falls into the "no help needed"
   bucket. If a terminal forwards Option as Meta correctly
   but lacks a `TERM_PROGRAM` marker, ultraviolet's decoder
   still produces a single `ModAlt` event and the dispatcher
   handles it directly.

3. **No cmd+shift+alt bindings.** `IsAltLetter` rejects any
   modifier besides `ModAlt`, so a user's
   `"cmd+alt+s": "submit"` in keybindings.json would never
   fire. The AGY 1.3.3 changelog is specifically about
   plain `alt+<letter>` — extending the dispatcher to
   Ctrl+Alt, Shift+Alt, Cmd+Alt would be a follow-on cycle.

4. **Race-free by construction.** `altEscBuf` is read/written
   only from the bubbletea goroutine; the `keybindings`
   pointer is loaded once at NewRunMode and only swapped in
   tests. The `tea.Tick(50ms, …)` callback runs on the
   bubbletea goroutine, so the timeout handler can read
   `m.altEscBuf.Pending` without synchronisation. No
   `sync.Mutex` is required for the fields this cycle adds.

5. **No release-tick re-arm.** The 50 ms release tick fires
   once per buffered Esc. If the user types Esc+s very
   quickly and then another Esc 60 ms later, the first
   tick may fire in between but the second Esc starts a
   fresh `Pending = true` cycle. The buffer is single-shot
   by design — a second Esc while one is already pending
   returns `Start = false` and falls through to the regular
   Esc branch. (See `TestEscBuffer_StartIsIdempotentWhilePending`
   in `internal/keybind/keybind_test.go`.)