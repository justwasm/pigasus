# Cycle 278 — Ctrl+V paste, Ctrl+O expanded rows, selection highlight, `/resume` parity, PiG 0.5.1

## Scope

Five user-reported TUI gaps against **agy 1.4.2** (SHA-256 `df2d4fb5…`,
`~/.local/bin/agy`), plus the dependency upgrade that landed in the same
cycle:

| # | Report | Outcome |
|---|---|---|
| 1 | Ctrl+V pastes nothing | fixed — `tea.PasteMsg` + a Ctrl+V intercept, clipboard read through the OS tool |
| 2 | Ctrl+O expanded tool rows render blank / smear | fixed — display-only CRLF normalisation, cache invalidation, scroll anchors |
| 3 | mouse selection has no visible highlight | fixed — reverse video over a stripped slice, CJK-safe |
| 4 | `/resume` output differs from the reference | 10 measured differences fixed; 2 remain, recorded below |
| 5 | PiG is on v0.4.1 | upgraded to v0.5.1, 11 breaking changes resolved |

Everything below was driven by live A/B captures (both binaries in tmux at
200×40 and 220×30), not by inference from the clone's own output. Evidence
grades follow the repo convention: 【一手】 observed live · 【反编译】 from
decompiled code · 【strings】 string table only · 【推测】 hypothesis.

---

## 1. Ctrl+V paste

The textarea never saw the clipboard for two reasons.

- **bubbletea v2 delivers a paste as `tea.PasteMsg`, not as `KeyMsg`.** The
  `Update` switch had no `case tea.PasteMsg:`, so a terminal paste (and
  ctrl+shift+v through the terminal's own paste) reached nothing.
- **Ctrl+V is a plain key.** It has to be intercepted *before*
  `m.input.Update(msg)`; otherwise the textarea treats it as a control chord
  and swallows it.

`cmd/wagyu/paste.go` (new) adds `clipboardReadText` (a seam for tests),
`readClipboardText` (OS shell-out: `pbpaste` · `wl-paste` · `xclip -o` ·
`xsel -b`, in that order, mirroring the reference's vendored
`atotto/clipboard`) and `insertPastedText`, which routes the text to whichever
field currently owns input: the rename panel, an open overlay, the `/resume`
search, or the textarea. `normalizePaste` strips trailing CR/LF — a clipboard
carrying `foo\n` must not submit the prompt.

**Out of scope:** images. The reference can attach an image from the
clipboard (`media_pasted` is in its string table), but the clone's backend
request has no media field, so a pasted image has nowhere to go. Recorded
under limitations.

Tests: `cmd/wagyu/paste_test.go` (11 cases) covering the textarea, rename
field, overlays, resume search, empty clipboard, and newline trimming.

## 2. Ctrl+O expanded rows

Root cause was cycle 277, not the tool renderer. `run_command`'s result text
deliberately reproduces the reference's naive `\n → \r\n`, so
`toolStep.detail` carries **CRLF**. The expanded view split on `\n`, leaving a
trailing `\r` on every row; the viewport padded the row *after* the CR and
ultraviolet repainted from column 0, which blanks the line and leaves the
`(Failed)`-looking smear.

The fix is display-only — the stored/wire text stays byte-identical to the
capture, which is what cycle 277 pins:

- `normalizeDetailNewlines` in `tui_tools.go` (CRLF → LF, then drop stray CR),
  applied in `stepDetailLines`.
- `fitWidth`/`padTo` in `tui_viewport.go` strip CR defensively.
- `rerenderBlocks` now sets `m.rowsCacheDirty` and shifts `m.liveStart` /
  `m.replyStart` by `delta` when `delta >= oldEnd`, so collapsing or expanding
  does not leave the scroll anchors pointing into the old row list.

Tests: `cmd/wagyu/ctrl_o_detail_test.go` — the previous failure mode
(`TestCtrlOExpandedRunCommandOutputIsNotBlank`) plus a table for the
normaliser.

## 3. Mouse selection highlight

The clone painted the selected slice with `48;5;240` (a 256-colour
background). The reference strips the row and then applies
`lipgloss.StyleRanges` — i.e. **strip first, style second** — which is why its
highlight survives rows that already carry SGR and does not depend on the
terminal's palette.

The clone now emits `\x1b[0m\x1b[7m` … `\x1b[0m` around `ansi.Strip(ansi.Cut(row, from, to))`.
Reverse video is reset-proof and theme-independent.

The same pass fixed a real bug: `selectionText` sliced by **byte** offset
while the selection was recorded in **cells**, so a CJK row copied the wrong
range. It now slices the stripped text with `ansi.Cut(plain, from, to)` and
clamps with `clampInt`.

Tests: `TestApplySelectionHighlight_Table` (8 row shapes) and
`TestApplySelectionHighlight_OutOfRangeRowsAreUntouched` in
`cmd/wagyu/tui_selection_test.go`.

## 4. `/resume` parity

Ten differences were measured live at 200×40 and fixed. Two more remain; they
are listed under 存疑.

### 4.1 Fixed

| # | Reference | Clone (before) | Fix |
|---|---|---|---|
| 1 | search keeps `Fixing`, `A_B`, `Hi` | `ixing`, `_`, `i` | `isTypedText` (`keytext.go`): allow `ModShift`, reject Ctrl/Alt. Applied at `tui_run.go` and the three panel inputs in `slash_panels.go` |
| 2 | Esc closes the picker silently | `  ⎿  Cancelled /resume` | line removed 【一手】 |
| 3 | Enter replaces the transcript silently | `  ⎿  Resumed conversation <id>` | line removed 【一手】 |
| 4 | untitled row shows the conversation id | `(no title)` | `conversationLabel(title, preview, id)` — title → preview → id 【一手】 |
| 5 | blank ages on zero-time rows | `just now` | age reads `summary.UpdatedAt` only (no `last_modified_time` fallback) 【一手】 |
| 6 | zero-time rows ordered by id | arbitrary | `sort.SliceStable` with an id tie-break 【一手】 |
| 7 | row is 200 cells wide, steps flush right | 186 cells (blank age field) | drop the 12-cell age field when the label is empty 【一手: steps at col 195 in both】 |
| 8 | empty store opens the overlay: `No conversations available.` / `Press Enter to return.` / reduced hint | one transcript line, no overlay | `slash_commands.go` no longer early-returns; `picker_view.go` renders the store-empty screen (no search row) |
| 9 | `ctrl+f Grouped View` appears only with project groups | always | `resumeProjectGroups(rows) >= 2` 【推测 on the exact threshold】 |
| 10 | cache row whose `.db` is gone is not listed | listed, then `Failed to load <id>` | stat check in `listFromCache` |

Two further changes came out of the same captures:

- **The picker is cache-first, not a merge.** With a 38-entry cache and a
  168-row SQLite index, REF listed exactly the cache's rows (34 after filtering
  `is_internal` / `NumSteps == 0` / missing `.db`); the clone listed the union,
  which is where the phantom "just now" rows came from. `ListConversations` now
  reads the index **only when the cache yields nothing**.
- **`/resume` no longer eats global ctrl keys.** `ctrl+l/o/g/k` fired under the
  open picker; they are now gated on `!m.resumeMode`. `resumeConversation` also
  clears `m.autoTitle`, mirroring `/continue` — without it, resuming B after
  working in A and sending one prompt overwrote B's stored title.

### 4.2 Model banner

The clone printed, on every launch with MiniMax M3 configured:

```
⚠ Warning
  ⎿  "MiniMax M3" is no longer available. Using "MiniMax M3".
```

which is self-contradictory and also **cost three rows**: the separator, the
prompt and every overlay sat three rows lower than the reference's. The
catalogue check is real (cycle 99 recovered it for a settings file naming a
model the catalogue does not ship), but when the agw backend actually serves
MiniMax M3 the warning must not fire. `tui_run.go`'s cycle-219 override now
clears `unavailableFrom` in that case. Frames align afterwards.

## 5. PiG v0.4.1 → v0.5.1

v0.5.1 is a retraction-only release (v0.5.0 exceeded Go's 500 MB module
limit), so the whole 0.4.x → 0.5.0 break applies. Two environment notes first:

- `proxy.golang.org` lists `v0.5.1` but `sum.golang.org` has **no checksum
  entry** for the tag, so both the proxy and `GOPROXY=direct` fail with
  `unknown revision v0.5.1`. The upgrade needs
  `GONOSUMDB='github.com/MichaelKinsy/PiG,github.com/MichaelKinsy/PiG/*'`.
  Nothing is vendored; `go.sum` carries real hashes.
- The binary grows **+11 MB (+12 %)**, to 102,060,338 B, because PiG 0.5.1
  newly links `go/types`, `go/doc`, `go/format`, `runtime/pprof`,
  `runtime/metrics` and `internal/zstd`.

Breaking changes, all resolved:

| # | Break | Fix |
|---|---|---|
| 1 | `agent.NewAgent` returns `(*Agent, error)` | `piagentbridge/bridge.go`, `internal/agent/pig.go`, `cmd/pigloop-demo` |
| 2 | a model's `Provider` no longer doubles as the default stream fn | new `piagentbridge.ProviderStreamFn()` |
| 3 | built-ins are factories registering into `extension.API` | `pigrt/runtime.go` via `factoryload.LoadExtensionFromFactory`; the *same* `*ExtensionRuntime` must reach `RuntimeOptions` or MCP servers never see `session_start` |
| 4 | `coding.NewServices` → `CreateAgentSessionServices`, `coding.Services` → `coding.AgentSessionServices` | six call sites |
| 5 | `Session.BindExtensions(ctx, coding.ExtensionBindings{})` | `pigrt/runtime.go` |
| 6 | `Agent.Continue` → `Agent.ContinueMessages` | `pigrt/auto_continue.go` |
| 7 | `Session.Compact` returns `(*CompactionResult, error)` | `cmd/wagyu/slash_compact.go` |
| 8 | `ai.ThinkingLevel` → `ai.ModelThinkingLevel` | `pigrt/runtime.go`, `internal/agent/pig.go` |
| 9 | `RetrySettings.MaxDelayMs` → `MaxAgentDelayMs` | `pigrt/retry_test.go` |
| 10 | mcpext `Send` takes a context | `pigrt/mcp_integration_test.go` |
| 11 | `ToolExecutionEndEvent.Duration` → `DurationMs *int64` | `cmd/pigloop-demo` |

The five documented stream-contract invariants (event order, mandatory
`End(result)`, `StopReasonToolUse`, `ToolsAdded`, provider pinning) and the
four traps (`ErrorSlot`, nil-error-on-failed-turn, blocking `emit`,
`FlushEvents`) all still hold: `ProviderStreamFn` pins on the `*ai.Model` the
agent was built with, exactly as `pinnedProvider` did.

Verification: `go build`, `go vet`, `go test ./cmd/... ./internal/...` clean
(65 packages); `go run ./cmd/pigloop-demo -mode loop` byte-identical to a
`main` baseline apart from timing.

## 6. Store cleanup

The probes created thousands of conversations. With the user's real sessions
kept, `devutils/cleanup-test-sessions-278.py` (dry-run by default) moved the
rest out. It ran twice — the store regrew between passes (see below):

| pass | rows removed | `.db` files moved | rows left | cache entries left |
|---|---|---|---|---|
| 1 | 3709 (3623 empty preview, 86 probe prompt) | 500 | 168 (146 titled) | 4 → 207 after regrowth |
| 2 | 3267 | 467 | 168 (146 titled) | 674 → 207 |

Nothing is destroyed: `trash-278/` holds every moved `.db` plus timestamped
copies of both `conversation_summaries.db` and `conversation_metadata.json`.
530 stale cache entries and 15 `last_conversations.json` entries were dropped.

**Why it regrew:** `go test ./cmd/wagyu/` is not store-isolated. Tests that
call `NewRunMode` / `saveSession` without setting `ANTIGRAVITY_HOME` write real
conversations into `~/.gemini/antigravity-cli`, so every full test run adds a
few hundred summaries and `.db` files. The cleanup is therefore a treadmill
until the suite is isolated; this cycle's second pass was run after the last
test run, and the final state is 168 rows / 203 files.

Observation worth recording: the 146 titled rows had **no `.db` file** even
before the cleanup, so they were already unresumable — the index and the
conversation directory had drifted apart. That is what exposed gap #10 above.

## 7. Tests

New: `cmd/wagyu/paste_test.go`, `cmd/wagyu/ctrl_o_detail_test.go`,
`cmd/wagyu/resume_cycle278_test.go`, `internal/convstore/list_cycle278_test.go`,
and the selection table cases.

| Command | Result |
|---|---|
| `go build -o /tmp/wagyu ./cmd/wagyu` | ok |
| `go vet ./cmd/... ./internal/...` | clean |
| `go test ./cmd/... ./internal/...` | all packages ok |
| A/B: `/resume` search (`Fixing`, `A_B`, `Hi`) | identical |
| A/B: `/resume` fresh / tab / down3 row text | identical (SGR palette aside) |
| A/B: row geometry | 200 cells, steps at col 195, both |

## 硬约束

- **No vendor binary patching.** `~/.local/bin/agy` is byte-identical before
  and after (SHA-256 `df2d4fb5…`). All measurements are of the shipped binary.
- **The wire text never changed.** Cycle 277's CRLF + ANSI contract is
  untouched: the CR fix is applied when *rendering* `toolStep.detail`.
- **No `go test ./...`** — `scratch/` does not compile. `./cmd/... ./internal/...`
  only.
- **No push, no reformat of pre-existing gofmt-dirty files.** `picker_view.go`'s
  header comment was restored byte-for-byte after `gofmt -w` rewrote it.

## 存疑

1. **The picker's initial scroll position.** REF opened at `[1-10 of 445]`
   once, `[57-66 of 445]` once, and `[385-394 of 500]` once, with the cursor
   always on the first visible row. Every hypothesis that fits (position of the
   *current* conversation, a persisted offset) is contradicted by one of the
   three observations, and the current conversation's id could not be
   correlated with the cursor row (`last_conversations.json` had no entry for
   `/tmp/agycmp`). The clone stays on `[1-10]`.
2. **The `ctrl+f Grouped View` threshold.** Measured at the two extremes
   (0 project groups → hidden; many → shown). "Two or more distinct projects"
   is 【推测】; the one-project case was not reachable in this store.
3. **`--conversation <id>` renders nothing in either binary.** Both start on an
   empty prompt, so the flag comparison the user asked for produces no visible
   transcript on either side — there is nothing to diff. The resumed
   *transcript* is only observable through `/resume` → Enter, which is what
   section 4 covers. A model turn after resume is unproven because the Google
   path is 403-blocked for this account.

## Limitations

- **Paste of images** is not implemented: the backend request has no media
  field, so the reference's `media_pasted` path has no equivalent here.
- **Clipboard read is a shell-out** (`pbpaste`/`wl-paste`/`xclip`/`xsel`). On a
  host with none of those installed, Ctrl+V silently does nothing; the
  reference's vendored `atotto/clipboard` fails the same way, but it also
  supports OSC 52, which this does not.
- **`ctrl+f` Grouped View, `f2` Rename and `f4` Delete are still advertised but
  unimplemented.** They are shown/hidden correctly now, but pressing them does
  nothing.
- **The test suite writes into the real store** (see §6). Not fixed here:
  isolating `cmd/wagyu`'s tests behind a temp `ANTIGRAVITY_HOME` is a
  suite-wide change, and doing it half-way would hide the leak.
- **Banner layout still differs** when a non-catalogue model is configured: the
  clone puts the model on row 3 and the cwd on row 4, and appends
  ` (Antigravity Starter Quota)` to the account line; REF prints only the cwd on
  row 3 and no quota suffix.
- **PiG's real network path was not driven.** The upgrade is verified by the
  fake-gateway tests and the scripted loop demo, not by a live `agw.k0s.io`
  turn.
- **The `-tags pig_strip_codemode` build** was not exercised (the old
  `nocodemode` tag name is gone).
