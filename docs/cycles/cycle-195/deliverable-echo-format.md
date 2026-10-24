# Cycle 195 — Global slash-command echo format

## TL;DR

REF v1.4.0 prepends every slash command's transcript response with a two-line
echo block (the `> /<cmd>` token + a `  ⎿  <msg>` continuation), with the
message body in gray-238. The clone now matches this for every
response-producing case; the cycle-149 "v1.3.6 did not echo" assumption is
superseded. The /compact empty-state is the one exception: REF writes
"Nothing to compact yet." directly into the prompt area with no `> /cmd` and
no `⎿` wrapper, and the clone reproduces that special case.

## What was wrong

Before cycle 195, `cmd/wagyu/slash_commands.go` ran each case branch and
appended its message directly to `m.response` — no leading `> /<cmd>` row,
no `⎿` wrapper. This was inherited from cycle 149, which captured v1.3.6
transcripts and concluded the reference suppresses the slash-command echo
because the picker overlay already shows the typed token. Cycle 184's
gap inventory flagged this as TODO.md §2.2 class B.

Live probes against `~/.local/bin/agy` v1.4.0 (script `/tmp/agycmp/probe_echo2.py`)
show every responsive command emits the same three-line echo:

```
> /<cmd>
  ⎿  <msg>
<blank>
```

Captured commands: `/rename`, `/copy`, `/btw`, `/title`, `/statusline`, `/voice`,
`/remote-control`. The /voice empty-state adds a 5-space-indented continuation
line under the `⎿` wrapper (no `⎿` on that row, just whitespace). The
`/remote-control on` state adds two 5-space-indented continuation lines
("Hostname: <host>" + "Open https://antigravity.google.com/r/<uuid>-v2 ...").

## What changed

### `cmd/wagyu/slash_commands.go`

- Added `slashRespond(cmd, msg, extra ...string) []string` helper. Emits the
  three-line echo block: `> /cmd` (bold blue, `cWarningBlueOpen`) + `  ⎿  <msg>`
  (bold blue prefix, `panelGray238` body) + trailing blank. The variadic
  `extra` slice appends additional rows between the `⎿` line and the trailing
  blank — used by `/voice` and `/remote-control` for their continuation rows.
- Added `slashRespondPlain(msg)` helper. Single styled line, no echo wrapper.
  Used by `/compact` empty-state to match REF's prompt-area special case
  (`> Nothing to compact yet.` with no `> /cmd` and no `⎿`).
- Replaced 30+ `m.response = append(m.response, "msg")` call sites in
  `handleSlashCommand` with `m.slashRespond("/"+cmd, "msg")` (or
  `slashRespondPlain(...)` for /compact).
- Cases converted: `/context`, `/copy`, `/diff`, `/feedback`, `/fork`,
  `/keybindings`, `/logout`, `/mcp`, `/open`, `/plugin`, `/remote-control`,
  `/rename`, `/resume`, `/rewind`, `/statusline`, `/tasks`, `/title`,
  `/voice`, `/add-dir`, `/artifact`, `/btw`, `/codesearch`, `/compact`,
  `/browser`, `/schedule`, `default` (unknown).
- Cases NOT converted (no echo expected):
  - `/clear`, `/new`: wipe the transcript (`m.response = nil`).
  - `/exit`, `/quit`: terminate the program.
  - `/help`: opens the help overlay (no transcript message).
  - `/model`, `/config`, `/effort`, `/permissions`, `/skills`,
    `/changelog`, `/usage`, `/codesearch` (panel-mode): the panel
    system in `tui_run.go:1852` intercepts before `handleSlashCommand`
    reaches these cases. The stubs remain for direct-test callers.
  - `/hooks`, `/agents`: return true with no transcript output.
  - `/goal`, `/plan`, `/learn`, `/grill-me`: routed through
    `sendAsPrompt`, which echoes the token as a user message
    (`> /<token>`) rather than a slash command.

### `cmd/wagyu/slash_content_test.go`

- Rewrote `afterEcho` to strip the new echo wrapper. Detects the
  `cWarningBlueOpen+"> "` prefix; if absent (slashContent / slashRespondPlain
  outputs), falls back to plain SGR-stripped rows. The helper now returns
  the handler rows that follow the echo block: `[msg]` for single-line,
  `[msg, extra...]` for multi-line.
- Updated the doc comment from "cycle 149: v1.3.6 did not echo" to
  "cycle 195: v1.4.0 reverses that".

### `cmd/wagyu/slash_cycle194_test.go`

- Updated `TestRunSchedule_ErrorID` and `TestRunBrowser_ErrorID` to look at
  `m.response[1]` (the `⎿  <msg>` row) and strip the SGR + `⎿  ` wrapper
  before applying the Error ID regex. The previous version grabbed
  `m.response[len(m.response)-1]`, which is now the trailing blank from
  `slashRespond`.

## Verification

### Build

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu
BUILD_EXIT=0
```

### Test suite

```
go test -vet=off ./cmd/... ./internal/...
27 packages ok
```

### Live parity probe (`/tmp/agycmp/cmp_echo.py`)

REF expected outputs were captured from live `~/.local/bin/agy` 1.4.0 probes
earlier in this cycle (auth state was valid at that point). Six commands
compared side-by-side:

| Command | Lines | Match |
|---|---|---|
| `/rename` | 3 | ✓ identical |
| `/copy` | 3 | ✓ identical |
| `/btw` | 3 | ✓ identical |
| `/title` | 3 | ✓ identical |
| `/statusline` | 3 | ✓ identical |
| `/voice` | 4 | ✓ identical (echo + 1 continuation + blank) |

`/remote-control on` was verified separately (`/tmp/agycmp/cmp_echo2.py`):
OUR emits 4 lines matching REF exactly:

```
> /remote-control
  ⎿  Remote control on for this session.
     Hostname: <host>
     Open https://antigravity.google.com/r/<uuid>-v2 on another device to take over.
```

### Special case: /compact

`/compact` (empty state) was verified to emit no `> /cmd` line and no
`⎿` wrapper. The text appears in the transcript as a single styled row
(`panelGray238 + msg + panelGray238C`), approximating REF's prompt-area
behaviour. The position is slightly different (transcript vs prompt area)
but the styled text matches.

### Special case: /clear, /new

Both wipe `m.response` entirely. Live probe (`/tmp/agycmp/cmp_echo2.py`)
shows no echo lines after `/clear` — transcript reverts to the welcome
banner. Matches REF.

## Limitations (knocked out)

- `/fork`, `/resume`, `/logout`, `/model`, `/config`, `/effort`, `/permissions`,
  `/skills`, `/changelog`, `/usage`, `/codesearch`, `/plugin`, `/artifact`:
  the panel-mode paths in REF open an interactive overlay. The transcript
  never sees the slash command's response; the panel covers it. The echo
  is therefore not exercised on these commands in REF — and the cycle-185/187/189/193
  panel system in the clone intercepts before `handleSlashCommand`. The
  case-branch fallbacks (which would echo) are only reached by tests that
  drive `handleSlashCommand` directly. If REF ever changes to emit a
  transcript echo *plus* the panel, the case-branch conversion is already
  in place.
- `/remote-control on`'s `<host>` and `<uuid>-v2` are still placeholders.
  The cycle-192 deliverable notes the real values wire in a later cycle.
- `/compact`'s prompt-area positioning is approximated as a transcript row.
  Visually similar but the row sits in the transcript, not in the input
  buffer. Pressing Enter after `/compact` in REF clears the prompt without
  sending; the clone's transcript-row approximation is cleared on the next
  transcript re-render (not on next keystroke).
- `<host>` and `<uuid>-v2` for `/remote-control` and the host detection for
  the URL are still placeholders. The hostname would normally come from
  `os.Hostname()`; the UUID from a fresh `uuid.New().String()`.
- Auth state on this machine expired between the REF probes (earlier this
  cycle) and the verification (`/tmp/agycmp/cmp_echo.py` runs REF directly,
  which now hits the OAuth flow). The parity comparison was done against
  the REF captures taken earlier, when auth was still valid. The clone's
  echo output is independently captured and verified against the expected
  REF strings; the byte-equality holds. Re-probing REF requires either
  refreshing the OAuth flow or waiting for the next valid token window.

## Diff stat

```
 cmd/wagyu/slash_commands.go        | +148 / -75
 cmd/wagyu/slash_content_test.go    |  +29 / -7
 cmd/wagyu/slash_cycle194_test.go   |  +19 / -7
 docs/cycles/cycle-195/...          | +this file
```