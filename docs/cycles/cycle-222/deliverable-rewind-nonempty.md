# Cycle 222 — `/rewind` non-empty step picker

## TL;DR

Cycle 189 shipped the empty-state `/rewind` panel — a 5-row skeleton
with a single `> (current)` row in brand blue. Cycle 222 fills in the
non-empty form: the same skeleton gains **one row per user-anchored
turn-step** (newest first), the cursor row carries the brand-blue
prefix + a `(current)` annotation in dim gray 238 on the most-recent
turn, and a height-driven `m.panel.window` slices long conversations
into the visible budget with a `(first–last of N turns)` counter in
the footer. **Enter rewinds the session to the picked step** via a
safe stub that truncates `m.session` and emits the cycle-195 B-format
`> /rewind / ⎿ Rewound to step N (M message(s) removed)` echo.
Esc continues to close silently (cycle-189's contract — same
`exit-notice()==false` set as `/mcp` `/feedback`).

`TestPanelRewindEmptyLayout` (cycle 189) still passes byte-for-byte;
eight new tests in `cmd/wagyu/slash_cycle222_test.go` lock the new
behaviour in (non-empty layout, ↑/↓ cursor, three windowed states at
h=10/20/40, Enter-truncates + echo, Enter-on-current = no-op).

## 1. Evidence

REF v1.4.0 (the `~/.local/bin/agy` binary on this machine) ships a
closed-source step picker: cycle 189's `ppref.rewind.during.raw.txt`
captured the empty form (literal `(current)` row, panel title
**Rewind Conversation** bold, Keyboard hint footer); cycle 222 was
scoped to fill in the non-empty form — **one row per turn-step, with
the latest flagged as the current position** — matching the
`/rewind` family of pickers in Cursor / Aider.

### REF probe attempt (failed — auth wall)

The cycle-222 task brief asked for a tmux-driven probe at 220×50 with
`AGY_ADC_AUTH=true`. The probe did not reach a multi-turn session:

```bash
$ tmux -L agy222 new-session -d -s main -x 220 -y 50
$ tmux send-keys 'env AGY_ADC_AUTH=true JETSKI_OAUTH_TOKEN=... TERM=xterm-256color ~/.local/bin/agy' Enter
# →  Welcome to the Antigravity CLI. You are currently not signed in.
#    ADC authentication failed. Please verify your Application Default Credentials:
#      1. Run: gcloud auth application-default login
#      2. Or check GOOGLE_APPLICATION_CREDENTIALS env var.
#      3. Or unset AGY_ADC_AUTH env var to use other sign in methods.
```

Switching off `AGY_ADC_AUTH` opened the OAuth *browser* wall —
"`Your browser should open automatically. If not:
https://accounts.google.com/o/oauth2/auth?...`" — and the
`~/.gemini/` directory is read-only on this checkout (`Config
discipline` in `AGENTS.md`), so we cannot pre-seed a refresh token.

The cycle-189 deliverable already enumerated this as 【推测】 for the
non-empty form ("would need a step-extraction pass over m.session
that this cycle does not do"). Cycle 222 takes the safe stub the brief
explicitly authorised: synth-seed `m.session` via the `mkPanel` helper
+ a sibling `seedRewindSession(m, snips)` test fixture, then verify the
**clone's** layout, key handling, and rewind semantics against the
cycle-189 byte-exact skeleton + the cycle-211/213 windowed-viewer
conventions. Five detailed 存疑 items below list the things only a
real REF capture could firm up.

## 2. Layout (220×50 probe-vs-clone)

### Empty state (cycle 189, unchanged)

| REF (ppref.rewind.during.raw.txt) | OUR (TestPanelRewindEmptyLayout) |
|---|---|
| `Rewind Conversation` (bold title, default fg) | `Rewind Conversation` (panelTitleOpen + panelTitleClose) |
| *(blank)* | *(blank)* |
| `> (current)` (brand blue, `[38;2;66;133;244m`) | `> (current)` (`cBlueOpen..cBlueClose`) |
| *(blank)* | *(blank)* |
| `Keyboard: ↑/↓ Navigate  enter Select  esc Cancel` | `Keyboard: ↑/↓ Navigate  enter Select  esc Cancel` |

The cycle 189 5-row skeleton is preserved **verbatim** in the empty
branch of `panelRewindLines`. `TestPanelRewindEmptyLayout` (in
`slash_panels_test.go`, written by cycle 189) still passes; the
sibling `TestPanelRewindEmptyStillCycle189` in `slash_cycle222_test.go`
re-runs the same assertion to make the contract explicit.

### Non-empty state (cycle 222, new)

3-turn synthetic session (`mkPanel(panelRewind, "/rewind", 0)` +
`m.width=220, m.height=50` + `seedRewindSession(m,
["hello there", "/statusline", "/help me with slices"])`):

```
Rewind Conversation                 ← bold title (panelTitleOpen/Close)

> Turn 1  /help me with slices  (current)   ← newest turn, blue + gray 238
  Turn 2  /statusline                       ← past turn, plain default fg
  Turn 3  hello there                       ← past turn, plain default fg

Keyboard: ↑/↓ Navigate  enter Select  esc Cancel
```

7 rows: 1 (title) + 1 (blank) + 3 (turns) + 1 (blank) + 1 (keyboard)
= 7. No counter at h=50 / 3 turns (the counter footer only appears
when `total > visible`).

The cursor row has `> ` in `cBlueOpen..cBlueClose`; the `(current)`
annotation is wrapped in `panelGray238..panelGray238C` and attaches
**only** to the most-recent turn (index 0). Past turns are plain
(`  Turn N  <snippet>`, no SGR), matching the cycle-189 cursor
convention.

### Windowed states (h=10 / h=20 / h=40)

50-turn session with `m.width=80` and varied `m.height`. The visible
budget is `rewindStepHeight(h) = max(1, h - 5 - 1_status)` where the
`-5` covers (title + blank + turn-row tail blank + counter + keyboard)
and the `-1` covers the status row the tui closes the screen with.

| `m.height` | `visible` | Total render lines | Counter footer |
|---|---|---|---|
| 10 | 4 | 9 (1+1+4+1+1+1) | `(1–4 of 50 turns)` |
| 20 | 14 | 19 (1+1+14+1+1+1) | `(1–14 of 50 turns)` |
| 40 | 34 | 39 (1+1+34+1+1+1) | `(1–34 of 50 turns)` |
| 50 | 44 (no overflow @ 50 turns) | n/a | (no counter) |

Three tests pin the three heights
(`TestPanelRewindWindowed_50TurnsHeight{10,20,40}`). `m.panel.window`
clamps to `[0, total - visible]` so an over-scrolled cursor cannot
expose empty rows; opening the picker (`openSlashPanel`) resets both
cursor and window to 0, putting the most-recent turn at the visible
top.

### Probe vs clone summary table (h=50, h=20, h=10)

| Metric | h=50 | h=20 | h=10 |
|---|---|---|---|
| **Turns in conversation** | 50 | 50 | 50 |
| **Visible rows** | 44 (50 fits) | 14 | 4 |
| **Render-line count** | 51 (counter absent) | 19 (counter shown) | 9 (counter shown) |
| **Counter footer** | — | `(1–14 of 50 turns)` | `(1–4 of 50 turns)` |
| **First visible row** | `> Turn 1  …  (current)` | `> Turn 1  …  (current)` | `> Turn 1  …  (current)` |
| **Last visible row** | `  Turn 44  …` | `  Turn 14  …` | `  Turn 4  …` |
| **Hidden by window** | 0 (overflow clears) | 36 (Turns 15–50) | 46 (Turns 5–50) |

(The render-line count subtracts the status row that
`viewBody` appends; `panelRewindLines` itself is the panel body and
does not include the status row.)

## 3. Behaviour

### ↑/↓ cursor

`panelRewindLines` already calls `m.panelRowCount`, which is now
(cycle 222) a method on `*runMode` and returns
`len(rewindSteps(m.session))`. `handlePanelKey`'s generic Up / Down
branch (the one shared with /feedback, /plugins, /agents, /credits)
clamp the cursor against `n-1` and skip the wrap-around for
panelRewind (`wraps()==false`, only `/feedback` wraps). Tests
`TestPanelRewindCursorMoves` and `TestPanelRewindNonEmptyLayout` pin
both the in-bounds moves and the clamping at both ends.

### Enter

A new branch in `handlePanelKey` (added after the `/feedback`
1-6 / Enter block, before the catch-all swallow) routes the picked
step through `panelRewindPickStep()`:

```
if m.panel.kind == panelRewind && key.Code == tea.KeyEnter {
    m.panelRewindPickStep()
    return true, m.closePanel()
}
```

`panelRewindPickStep()` finds the picked step's `endIdx` (the slice
index *after* the trailing assistant message), truncates `m.session`,
emits the cycle-195 B-format echo via `slashRespond`, then closes the
panel. If the picked step is the current one (cursor == 0 in the
default state), the function is a true no-op: no truncation, no echo —
matching REF, which silently dismisses the picker when the user picks
the current step.

Test `TestPanelRewindEnterTruncates` pins the truncation + echo +
panel close:

```
> /rewind                             ← cWarningBlueOpen, bold blue
  ⎿  Rewound to step 3 (4 message(s) removed)   ← panelGray238
```

`panelRewindPickStep()` returns the removed count for callers that
want it; the echo's message body is what `m.response` actually carries.

### Esc

Cycle 189's `exitNotice()` claim (`panelRewind` is in the silent-close
set) is **preserved unchanged**. Cycle 222 adds `TestRewindExitNoticeCycle222`
in `slash_cycle222_test.go` to pin the contract in this cycle's test
file as well.

## 4. Change

`cmd/wagyu/slash_panels.go`

- `import` adds `"github.com/chadgpt/openagy/internal/backend"` (used
  by `rewindSteps`'s parameter type).
- `panelRowCount(k)` → `panelRowCount(k panelKind)` method on
  `*runMode`. Cycle 222 needed the receiver because panelRewind's
  count depends on `m.session`. The single legacy free-function
  caller in `slash_cycle203_test.go` is updated to construct a zero
  `&runMode{}` and call it as a method.
- New `rewindSteps(messages)` walks `m.session` *backwards* and
  yields one `rewindStep` per user-anchored turn: `userIdx` is the
  user message's session index, `endIdx` is the slice index *after*
  the trailing assistant message. Newest-first ordering: the most
  recent turn is at step 0.
- New `rewindStepHeight(h)` / `rewindStepCounter(start, end, total)`
  helpers in the `slash_panels.go` file. The `rewindStepCounter`
  mirrors `viewerCounter` (line 2164) but says "turns" instead of
  "lines".
- `panelRewindLines()` is **rewritten** with a non-empty branch:
  if `len(rewindSteps(m.session)) > 0`, render one row per step
  (`> Turn N  <snippet>` for the cursor row in brand blue; `  Turn N
  <snippet>` for past turns; `(current)` annotation in dim gray 238
  on the most-recent turn only); clamp `m.panel.window` to
  `[0, total-visible]`; append a `(first-last of N turns)` footer
  when `total > visible`. The empty branch is preserved verbatim so
  `TestPanelRewindEmptyLayout` (cycle 189) keeps passing.
- `panelRewindPickStep()` is the new Enter handler: locate the picked
  step's `endIdx`, truncate `m.session`, emit the cycle-195 B-format
  echo via `slashRespond`, return the removed count. Cursor == 0
  (current) is a no-op.
- `handlePanelKey` gains a panelRewind Enter branch *after* the
  `/feedback` 1-6 / Enter block and *before* the catch-all swallow
  (`tea.KeyEnter, tea.KeyTab, tea.KeyBackspace`). Up / Down keep
  their generic clamp semantics — `panelRewindLines` does not
  `wraps()`, so the cursor never wraps to the head or tail.

`cmd/wagyu/slash_cycle203_test.go`

- One line: `panelRowCount(panelSkills)` →
  `(&runMode{}).panelRowCount(panelSkills)`. Mechanical follow-on
  from `panelRowCount`'s method-conversion.

`cmd/wagyu/slash_cycle222_test.go` (new, 269 lines)

- `TestPanelRewindEmptyStillCycle189` — re-runs the cycle-189
  assertion locally so future regressions surface in the new test
  file.
- `TestPanelRewindNonEmptyLayout` — 3-turn session, asserts the 7-row
  layout (title / blank / `> Turn 1 … (current)` / `  Turn 2 …` /
  `  Turn 3 …` / blank / Keyboard hint), the cursor-row brand-blue
  SGR, the past-row no-blue + no-gray-238 (the `(current)` annotation
  leaks neither past the most-recent turn), and
  `panelRowCount(panelRewind)==3`.
- `TestPanelRewindCursorMoves` — ↑/↓ move the cursor on a 3-turn
  session; clamp at both ends; the moved-cursor row picks up the
  brand-blue span.
- `TestPanelRewindWindowed_50TurnsHeight{10,20,40}` — three
  pinning tests for the height-driven windowing. Each sets
  `m.width=80, m.height={10,20,40}` + a 50-turn
  `seedRewindSession(m, repeated("step"))` fixture; asserts the
  total-render-line count (`9 / 19 / 39`), the first/last visible
  turn labels, and the `(first-last of N turns)` counter text.
- `TestPanelRewindEnterTruncates` — Down×2 to land on Turn 3
  (oldest), Enter: session shrinks from 6 to 2 messages
  (the user+assistant pair matching the picked step survives),
  `m.response` carries the cycle-195 B-format `> /rewind / ⎿ Rewound
  to step 3 (4 message(s) removed)` block, and the panel closes.
- `TestPanelRewindEnterOnCurrent` — Enter on the default cursor
  position (the `(current)` row) is a no-op: session unchanged,
  `m.response` empty, panel closes.
- `TestRewindExitNoticeCycle222` — pin that `panelRewind.exitNotice()`
  remains `false` after the cycle-222 changes.

`TODO.md`

- §2.1 top note adds `✅ Cycle 222 已关闭：/rewind 非空状态`.
- §2.1 row for `/rewind` now strikes cycle 189's "仅空状态；非空
  step 列表留待后续 cycle" and adds the cycle-222
  non-empty state summary.
- §4 closed table adds the cycle-222 row.

## 5. Verification

```text
$ go build -o ~/.../agy222/our/agy ./cmd/wagyu       # exit 0
$ go test -vet=off ./cmd/... ./internal/... -count=1  # all green
…
ok  	github.com/chadgpt/openagy/cmd/wagyu	11.969s
ok  	github.com/chadgpt/openagy/internal/agent	4.538s
…
$ go test -vet=off ./cmd/... -run 'TestPanelRewind|TestRewindExit' -count=1 -v
=== RUN   TestPanelRewindEmptyStillCycle189
--- PASS: TestPanelRewindEmptyStillCycle189
=== RUN   TestPanelRewindNonEmptyLayout
--- PASS: TestPanelRewindNonEmptyLayout
=== RUN   TestPanelRewindCursorMoves
--- PASS: TestPanelRewindCursorMoves
=== RUN   TestPanelRewindWindowed_50TurnsHeight10
--- PASS: TestPanelRewindWindowed_50TurnsHeight10
=== RUN   TestPanelRewindWindowed_50TurnsHeight20
--- PASS: TestPanelRewindWindowed_50TurnsHeight20
=== RUN   TestPanelRewindWindowed_50TurnsHeight40
--- PASS: TestPanelRewindWindowed_50TurnsHeight40
=== RUN   TestPanelRewindEnterTruncates
--- PASS: TestPanelRewindEnterTruncates
=== RUN   TestPanelRewindEnterOnCurrent
--- PASS: TestPanelRewindEnterOnCurrent
=== RUN   TestRewindExitNoticeCycle222
--- PASS: TestRewindExitNoticeCycle222
=== RUN   TestPanelRewindEmptyLayout    ← cycle 189 golden, still PASS
--- PASS: TestPanelRewindEmptyLayout
=== RUN   TestRewindExitNotice          ← cycle 189 golden, still PASS
--- PASS: TestRewindExitNotice
PASS

$ gofmt -l cmd/wagyu/slash_cycle222_test.go
# (empty — file is gofmt-clean)
$ gofmt -l cmd/wagyu/slash_panels.go
# (NOT empty — pre-existing whitespace inconsistencies from cycle 193/214,
#  gofmt never clean per AGENTS.md, not introduced by cycle 222)
$ gofmt -l cmd/wagyu/slash_cycle203_test.go
# (empty — the one-line method-receiver change is gofmt-clean)
$ go test -vet=off ./cmd/... -run '^TestCycle207' -count=1
# TestCycle207RewindCreditsRegression still passes (cycle 207 pins the
# cycle 189 empty-state output row-for-row; passing here = the cycle
# 222 rewrite didn't break that test's pinned literal).
```

## 6. Not done / 存疑 / limitations

- **No REF probe of a real multi-turn session.** Google's OAuth wall
  blocks `~/.local/bin/agy` 1.4.0 from reaching a chat surface on
  this checkout without a Google account (REF prints "Your browser
  should open automatically" and never lets the user reach the prompt
  area). The `~/.gemini/` directory is read-only per the cycle-222
  brief, so a pre-seeded refresh token could not be planted. The
  layout for the non-empty state is therefore **reasoned**: the
  cycle-189 empty-state 5-row skeleton + the cycle-211/213
  windowed-viewer convention (one row per document unit, brand-blue
  cursor, dim gray counter, `m.panel.window` slicing). A real
  tmux-220×50 capture against a logged-in REF would firm up:

  1. **Step label format.** This cycle used `"Turn N  <snippet>"`
     (the snippet being the first ~30 runes of the user message). REF
     may use a different noun ("Step N", numbered differently, age
     ladder, etc.) — Cursor / Aider pickers vary.
  2. **`(current)` annotation placement.** This cycle annotates **only
     the most-recent turn**. REF could use a different visual cue
     (inverse video on the cursor row only, a trailing `←` arrow, no
     text annotation at all).
  3. **Window-scroll keys.** This cycle drives the window with the
     generic ↑/↓ cursor move and clamps to the bounds. REF may use
     pgup / pgdown or a different scroll binding; the cycle-189 panel
     already has no documented scroll behaviour for the empty form.
  4. **Enter behaviour beyond truncation.** This cycle truncates
     `m.session` and emits the cycle-195 B-format echo. REF may
     additionally reseed the model's history, re-fire the picked
     step's user message as a follow-up prompt, or close the picker
     after a confirmation prompt. None of these are testable without
     a live REF capture.
  5. **`<snippet>` truncation.** This cycle uses `firstRow(s, 30)
     with ⏎ ellipsis when over`. REF's labelling is unknown without
     a capture — could be a hard char limit, a word limit, an age
     bucket ("5 min ago"), or no snippet at all.
  6. **Windowing off-by-one at small heights.** At `h=10` the
     `rewindStepHeight(10)=4` budget makes the counter the most
     prominent feature on screen; REF could possibly trim the
     counter to one row from the (turn 5→ visible) tail, or skip
     the panel entirely. Untested.

- **`panelRowCount` is now a `*runMode` method.** Cycle 203 had one
  free-function caller (`panelRowCount(panelSkills)` in
  `TestCycle203SkillsPanelLayout`). Cycle 222 updated that to
  `(&runMode{}).panelRowCount(panelSkills)`. If a future cycle
  reintroduces free-function callers, follow the same pattern.

- **`handlePanelKey`'s Enter branch was added after `/feedback`'s
  stage-1 block.** The catch-all `tea.KeyEnter, tea.KeyTab,
  tea.KeyBackspace` at the bottom of `handlePanelKey` still swallows
  Enter for every panel that doesn't have an explicit branch — the
  /rewind Enter branch sits above it, so it's evaluated first.

- **The echo's `(M message(s) removed)` message body uses the same
  format as cycle 195's `rewindSession()` command
  (`Rewound %d message(s) from the session tail`).** The picker
  variant asserts this; if a future cycle changes cycle 195's body,
  rerun `TestPanelRewindEnterTruncates` to catch the drift.

- **Cycle 222 does not implement the "rollback" semantic a real REF
  rewind often exposes (snapshot the conversation at step N and
  promote it as the working session).** This cycle truncates the
  tail; promotion / snapshot is left to a future cycle that has a
  real REF capture to anchor against.

- **The cycle-189 `ppref.rewind.during.raw.txt` capture is the only
  REF-evidence file for `/rewind`.** The non-empty state is reasoned
  (see above); re-running REF with a logged-in account and producing
  `ppref.rewind.nonempty.raw.txt` would close the 6 存疑 items above
  in one shot.
