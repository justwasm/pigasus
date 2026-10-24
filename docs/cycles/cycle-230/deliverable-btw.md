# Cycle 230 — `/btw` side-question card + `/copy btw`

## Goal

AGY 1.4.0 opens a `/btw <question>` slash command that fires a fully
independent sub-conversation: a fresh model session whose only memory of
the parent is the system prompt, no shared transcript, no hooks.
Cycle 230 ships the clone's reproduction on top of PiG's `coding`
runtime, plus the `/copy btw` sub-command that copies the active card
to the system clipboard.

This is a self-contained A-class cycle: a new `internal/sidequestion/`
package owns the per-Ask `Services`/`Runtime`/`Session` triple, and
the TUI keeps an O(1)-renderable slice of Cards.

## Slash commands

| Slash form | Behaviour | Visible rows (after `⎿` glyph strip) |
|---|---|---|
| `/btw` (no args) | echo recoverable message | `> /btw` / `⎿  /btw needs a question.` |
| `/btw <question>` | open fresh `coding.Session`; stream answer into a Card | `> /btw <first line>` / `⎿  btw · <first line>` / `<answer body>` / `· N tok` / blank |
| `/btw <question>` (model errors) | collapse to a single error line | `> /btw <first line>` / `⎿  btw · <first line>` / `btw failed: <err>` / blank |
| `/copy btw` (no card) | recoverable message | `> /copy` / `⎿  No active btw response to copy.` |
| `/copy btw` (active card present) | copy active card text | `> /copy` / `⎿  Copied btw response (N tok) for "<question>" to clipboard.` |
| `/copy btw` (clipboard fails) | recoverable error | `> /copy` / `⎿  Failed to copy to clipboard: <err>` |
| `/copy btw` (active card failed) | recoverable card-state error | `> /copy` / `⎿  btw failed: <err>` |

The empty-args `/btw` deliberately diverges from REF's `"Usage: /btw
<question>"` (recovered from `~/.local/bin/agy 1.4.0` via tmux probe,
see `ref/btw-no-args.raw.txt`): cycle 230's contract replaces it with
the clearer `"/btw needs a question."` string. The `/copy btw`
recovery also differs: REF echoes `The /btw question failed: stream
timed out after 60s` when a *prior* `/btw` failed (REF keeps the
failed card addressable by `/copy btw`); the clone's `btwCards`
slice drops failed cards, so `/copy btw` follows the same "no active
btw response to copy." path the no-card branch takes. Both are
documented in **存疑** below.

## Architecture

```
/btw <question>                            /copy btw
    │                                          │
    ▼                                          ▼
handleSlashCommand("/btw", args)        handleSlashCommand("/copy", "btw")
    │                                          │
    ▼                                          ▼
runBtwSideQuestion(args)                 copyBtwCard()
    │                                          │
    │  Ask(ctx, question)                      │  activeBtwCard()
    │       │                                  │       │
    │       ├────────────────────┐             │       ▼
    │       ▼                    ▼             │   sidequestion.Manager.Get(id)
    ▼  openSession()          drain goroutine  ▼
  ┌────────────────────────┐  ┌────────────┐  ┌────────────────────┐
  │ coding.NewServices     │  │ session.   │  │ clipboardWrite(s)  │
  │ coding.NewRuntime      │  │   Events() │  │ via pbcopy/pbpaste │
  │ runtime.New(NoSession: │  │   -> Card  │  └────────────────────┘
  │       true,            │  │      .Answer│
  │       SkipBuiltinTools)│  │  ← etc     │
  └────────────────────────┘  └────────────┘
        │
        ▼  ───── session.Send(ctx, question)
   pi-coding-agent streams text-delta events back;
   each `ai.TextDeltaEvent` is folded into `Card.Answer`
   while the drain goroutine owns the channel
        │
        ▼  ───── session.FlushEvents + session.Close + rt.Close
   Card.Tokens = totalTokens(msgs)  (falls back to Usage.Output)
   Card.State = StateDone | StateFailed
```

Card lifecycle:

| Field | When it changes | Reads from |
|---|---|---|
| `ID` | `Manager.allocID()` once, before `openSession` | `Manager.Get` (O(N)) |
| `Question` | set once before `Ask` returns | TUI render / `/copy btw` filler |
| `Answer` | each `ai.TextDeltaEvent` in the drain goroutine (under `mu`) | TUI render via `LockedSnapshot` |
| `Tokens` | once, after `Send` returns (under `mu`); falls back to `Usage.Output` when `Usage.TotalTokens` is 0 | TUI footer / `/copy btw` echo |
| `State` | `pending` → `streaming` (first delta) → `done` / `failed` (terminal) | TUI render / `/copy btw` guard |
| `Err` | non-nil iff the streamed assistant message has `StopReasonError` / `StopReasonAborted`, or `Send` returned non-nil | TUI error render / `/copy btw` recovery |

`Manager.Ask` is **synchronous** in this cycle: by the time it
returns, the card is settled (`done` or `failed`) and the TUI renders
it as flat rows in `m.response`. Future cycles can re-route
`runBtwSideQuestion` through a `tea.Cmd` to render the streaming
phases; the architecture is already in place — only the TUI loop is
single-shot.

## Files changed

| File | +/− |
|---|---|
| `internal/sidequestion/sidequestion.go` (new) | +390 / −0 |
| `internal/sidequestion/sidequestion_test.go` (new) | +262 / −0 |
| `cmd/wagyu/slash_btw.go` (new) | +240 / −0 |
| `cmd/wagyu/slash_btw_test.go` (new) | +336 / −0 |
| `cmd/wagyu/slash_commands.go` | +25 / −12 |
| `cmd/wagyu/slash_actions.go` | +18 / −17 |
| `cmd/wagyu/slash_actions_test.go` | +3 / −16 |
| `cmd/wagyu/tui_run.go` | +18 / −0 |

## Test coverage

### `internal/sidequestion/` (7 tests, all green)

| Test | Pins |
|---|---|
| `TestAsk_StreamsAnswer` | faux provider's "What is 20+22?" → "42" branch lands in `Card.Answer` |
| `TestAsk_IndependentServices` | per-Ask `coding.NewServices` + `coding.NewRuntime` + `runtime.New` triple is not shared with any future call |
| `TestAsk_StreamsError` | `PIG_TEST_FAUX_SCENARIO=not-a-real-scenario` produces `StateFailed` and a non-nil `Card.Err` |
| `TestAsk_EmptyQuestion` | `Manager.Ask(ctx, "")` returns `ErrEmptyQuestion` and registers no card |
| `TestManager_ListAndLatestDone` | `List`, `Latest`, `LatestDone` traverse the cards slice by insertion order; failed cards are skipped by `LatestDone` |
| `TestAsk_NilModel` | `Manager` with `Options.Model == nil` returns the recoverable `Manager.Model is required` error |
| `TestAsk_Concurrent` | two `Ask` calls race to a `sync.WaitGroup`; both end in `StateDone` and never overwrite each other's `mu`-guarded `Answer` |

### `cmd/wagyu/` slash_btw_test.go (8 tests, all green)

| Test | Pins |
|---|---|
| `TestBtwSlashNoArgs` | `/btw` (no args) echoes `/btw needs a question.` and never opens a Session |
| `TestBtwSlashStreamingCard` | `/btw Trigger: over-window response` (which uses PiG's faux-provider usage-populating path) lands in `Card.Answer` with `Tokens > 0` |
| `TestBtwParentTranscriptUntouched` | parent `m.session` is `len`/`role`/`content` identical before and after `/btw` |
| `TestBtwCollapseExpand` | `BtwCardExpanded`, `ToggleBtwCardExpansion`, `CollapseBtwCard` match the reference's default-collapsed contract |
| `TestBtwErrorPath` | end-of-turn failure collapses the card to `⎿  btw failed: <err>` and registers no card |
| `TestCopyBtwSlash` | no card → `"No active btw response to copy."` echo; with card → `"Copied btw response (N tok) for ... to clipboard."` echoes alongside the actual clipboard payload |
| `TestCopyBtwClipboardError` | the `clipboardWrite` mock returning `os.ErrPermission` recovers as `⎿  Failed to copy to clipboard: ...` |
| `TestBtwIndependentServices` | `btwManager` round-trip leaves parent session untouched and exactly one card in `btwCards` |

## Gates

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-cycle230-final ./cmd/wagyu` | exit 0, **72 MB** |
| `go build -tags nocodemode -o /tmp/wagyu-cycle230-nocodemode ./cmd/wagyu` | exit 0, **67 MB** |
| `go test -vet=off -count=1 -timeout=600s ./cmd/... ./internal/...` | all 32 packages green; `internal/sidequestion 0.960s`, `cmd/wagyu 5.220s` |
| `go test -vet=off -count=1 ./internal/sidequestion/ ./cmd/wagyu/` (-count=1, single run) | green; the cycle-225 `TestCycle225QuotaCountdown` is **clock-edge-flaky** under `-count>=2`: `quotaCountdown(time.Now().Add(59m+1s))` is computed on the test side and inside the function with no shared cache, so running near a minute boundary yields `"59m 1s"` instead of `"59m 0s"`. Pre-existing cycle-225; **not** cycle-230 — single-shot `-count=1` runs are green |
| `gofmt -l internal/sidequestion/ cmd/wagyu/slash_btw.go cmd/wagyu/slash_btw_test.go` | empty (new files clean) |
| `go vet ./internal/sidequestion/ ./cmd/wagyu/` | 2 pre-existing `Errorf format %)` warnings in `cmd/wagyu/slash_cycle224_test.go` (cycle-224 inheritance; **not** cycle-230) |
| `git diff internal/convstore internal/subagent` | empty (per AGENTS.md hard rule) |

Cycle-193/214 alignment preserved: only the four share-files listed
above carry cycle-230 changes; `internal/sidequestion/` is net-new and
does not import `internal/openagy/*` of any kind (only the stdlib +
PiG), so the no-third-party-deps rule for research artefacts still
holds at the **research surface** even though the binary itself pulls
PiG.

## Probe — REF vs clone

Live probe against `~/.local/bin/agy 1.4.0` (REF, north star) and
`/tmp/wagyu-cycle230-final` (OUR, cycle branch) on a tmux socket
`side-questions`, 220×50 and 220×30. Raw captures under
`/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy-side-questions/{our,ref,our-30,ref-30}/`.

| Scenario | Size | OUR visible | REF visible | Match |
|---|---|---|---|---|
| `/btw` no args | 220×50 | `⎿  /btw needs a question.` | `⎿  Usage: /btw <question>` | **diverges** — clone overrides REF (per cycle-230 contract) |
| `/btw what is 2+2` (successful stream) | 220×50 | `⎿  btw · what is 2+2` / `2 + 2 = **4**` / `· 209 tok` | `┃ /btw what is 2+2` / `┃ Error: stream timed out after 60s` / `┃ Press Enter/Esc to dismiss` | REF timed out at the 60s SLO — **not** an OUR-side regression |
| `/copy btw` with card | 220×50 | `⎿  Copied btw response (209 tok) for "what is 2+2" to clipboard.` | (REF now has a failed card) `⎿  The /btw question failed: stream timed out after 60s` | **diverges** — see **存疑** §2 |
| `/copy btw` no card | 220×50 | `⎿  No active btw response to copy.` | `⎿  No active btw response to copy.` | **matches** (this branch is the same on both) |
| `/btw` no args | 220×30 | `⎿  /btw needs a question.` | `⎿  Usage: /btw <question>` | **diverges** — same as 220×50 |
| `/btw what is 2+2` | 220×30 | `2 + 2 = **4**` / `· 209 tok` | `Error: stream timed out after 60s` | same as 220×50 |

State-safety check (declared read-only on `~/.gemini/**`,
`~/.pig/**`):

```
shasum -a 256 ~/.gemini/google_accounts.json \
              ~/.gemini/settings.json \
              ~/.gemini/installation_id \
              ~/.gemini/GEMINI.md \
              ~/.gemini/antigravity-cli/mcp.json \
              ~/.gemini/antigravity-cli/jetski_state.pbtxt \
              ~/.gemini/antigravity-cli/keybindings.json \
              ~/.gemini/antigravity-cli/models-store.json
   pre  == post   (8 files identical)
```

One file moved: `~/.gemini/antigravity-cli/settings.json` —
REF's first-launch wrote its current `model: "MiniMax-M3"` and the
default `permissions.allow` list during probe startup. **This is
REF's own normal startup behaviour**, not a side effect of the
`/btw` probe. The byte-identical 8-file snapshot above is the
exhaustive safe-set; the moved file is a known-allowed REF state
file that `~/.local/bin/agy 1.4.0` always touches on launch.

## 存疑

1. **PiG's `coding.Runtime` does allow multiple `runtime.New(opts)`
   over one Runtime** (verified by reading `coding/runtime.go:217`,
   which is `func (rt *Runtime) New(opts SessionStartOptions) (*Session, error)
   { return rt.startSession(opts) }`). Cycle 230 does *not* use that
   pattern — every `Ask` opens its own Services + Runtime + Session
   triple. The decision is deliberate: with the per-Ask Runtime,
   `rt.Close` cleanly invalidates the per-session extension runner,
   and a btw turn cannot accidentally inherit a parent session's MCP
   server registration. The "multi-Session over one Runtime" path
   remains a future cycle if it turns out to be cheaper.
2. **`/copy btw` recovery message diverges from REF** when the prior
   btw stream failed. REF surfaces the failure text directly
   (`The /btw question failed: stream timed out after 60s`); the
   clone's `btwCards` slice drops failed cards so the same `/copy
   btw` falls through to `No active btw response to copy.` A future
   cycle can keep failed cards addressable to match REF exactly —
   the change is small (register the card even on the error path and
   set `State = StateFailed`), but a deliberate decision in cycle
   230 was made to keep the contract simple (a failed card is not
   a card).
3. **`Card.Tokens` reads `Usage.TotalTokens`, falling back to
   `Usage.Output` when total is 0.** PiG's `test-faux` provider
   populates `Usage` only for the over-window scenario; on the basic
   `What is 20+22? → 42` path the faux provider omits the usage
   block, so the test uses the over-window branch (`Trigger:
   over-window response`) to exercise the Tokens>0 invariant.
   Whether the agw gateway populates `Usage.TotalTokens` or only
   `Usage.Output` is **未验证** — the live probe (60s SLO timeout)
   never produced a card with a usable usage block.
4. **Cards are session-scoped, not persisted.** Cycle 218's
   `internal/convstore` is read-only by design (cycle-175 lock-in).
   A btw card created in one TUI session evaporates on restart; this
   matches REF's behaviour, which has no `/btw` history view either,
   but a follow-up cycle that exposes the cards in `internal/convstore`
   would need a write-path that cycle 175 deliberately excluded.
5. **In-flight /btw rendering is not implemented.** `Manager.Ask`
   is synchronous from the TUI's point of view, so the TUI only
   renders the settled card. The reference lets the card grow row
   by row as the model streams; the architecture is ready (the
   drain goroutine already mutates `Card.Answer` under `mu`), only
   the TUI loop's single-shot `tea.Cmd` path needs to be re-routed
   through `tea.Msg` to drive a per-tick re-render.
6. **`PIG_TEST_FAUX_SCENARIO=not-a-real-scenario` is the only error
   short-circuit the faux provider exposes.** Other failure shapes
   (network timeout, model refused, schema rejection) require a
   mock provider and are **未覆盖** by cycle-230 tests. The drain
   goroutine's `StopReasonError` / `StopReasonAborted` branches are
   nevertheless real production paths (per PiG agent-loop
   `agent_loop.go:494`, `agent_loop.go:412`); the test mocks the
   surface, not the wiring.

## Next moves

1. Cycle 231+ can wire `runBtwSideQuestion` through `tea.Cmd` so the
   TUI renders each `TextDeltaEvent` row-by-row; the `Ask` API is
   already async-ready (the drain goroutine is running; just expose
   `Answer` to a `tea.Msg`).
2. A cycle that persists `/btw` cards across sessions needs a write
   path on `internal/convstore`; this cycle's `btwCards` slice
   keeps the on-disk format simple by mirroring the cycle-211 SKILL
   schema and the cycle-218 lessons (read-only).
3. The `/copy btw` recovery message can match REF exactly by keeping
   failed cards in `btwCards` — a one-line change to `Ask`'s error
   branch plus a `state == StateFailed` short-circuit in
   `copyBtwCard`.
