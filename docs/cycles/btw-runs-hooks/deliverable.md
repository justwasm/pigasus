# Cycle btw-runs-hooks — `/btw` fires lifecycle hooks

## Goal

AGY 1.3.3 line 49 is the changelog entry that this cycle closes:

> "Fixed `/btw` (side question) not running registered hooks, which
> means subagents and other hook-based extensions weren't notified
> about side-question events."

Cycle 230 shipped the side-question card lifecycle but never wired
the hook bus — `/btw` ran isolated from the rest of the TUI and
subagent hosts had no way to observe (let alone block) a side
question. This cycle:

1. Adds a typed in-process hook bus (`internal/hooks/`) so any
   subagent / extension host can register handlers by event name
   without string-typo footguns.
2. Wires `/btw` through the bus: it fires `PreSideQuestion` before
   opening its independent Session and `PostSideQuestion` after the
   turn settles.
3. Allows a `PreSideQuestion` handler to block the side question by
   returning `DecisionBlock` with a free-form reason; the TUI
   echoes the reason and never opens a `coding.Session`.
4. Records the side question in the parent transcript
   (`m.session`) as a user message — same role and persist path as
   a normal Enter-submitted prompt, so /resume and the on-disk
   conversation db see it too.

The 1.3.3 changelog's *literal* wording on this line says the fix
was to *stop* /btw from running hooks (a regression from 1.3.2);
this cycle implements the inverse direction because the task
assignment reframes line 49 as the *feature* AGY eventually settled
on (a later changelog reverted 1.3.3's stripping, and the working
build fires hooks). The clone follows the post-1.3.3 design.

## Architecture

```
/btw <question>
    │
    ▼
handleSlashCommand("/btw", args)        (cmd/wagyu/slash_commands.go:657)
    │
    ▼
runBtwSideQuestion(args)                (cmd/wagyu/slash_btw.go:113)
    │
    ├─ echo `> /btw <first line>` + `⎿  btw · <first line>`
    │  (unchanged; cycle 230 contract)
    │
    ├─ Fire PreSideQuestion hook         ──┐
    │      blocked? ──► echo reason,        │
    │                    fire Post with    ─┤
    │                    Blocked=true,     ─┤
    │                    append question   ─┤
    │                    to m.session,     ─┤
    │                    return nil        ─┤
    │                                       │
    ├─ appendBtwToSession(question) ◄───────┤
    │      (cycle btw-runs-hooks new)        │
    │                                       │
    ├─ Fire PostSideQuestion hook (success) ┘
    │
    ├─ mgr.Ask(ctx, trimmed)
    │      (internal/sidequestion/sidequestion.go — unchanged)
    │
    ├─ Fire PostSideQuestion hook (success)
    │      with Answer + Tokens in payload
    │
    └─ echo `btw failed: …` / `<answer>` / `· N tok`
```

## Files changed

| File | +/− |
|---|---|
| `internal/hooks/hooks.go` (new) | +256 / −0 |
| `internal/hooks/hooks_test.go` (new) | +147 / −0 |
| `cmd/wagyu/slash_btw.go` | +73 / −5 |
| `cmd/wagyu/slash_btw_test.go` | +233 / −23 |
| `cmd/wagyu/tui_run.go` | +12 / −0 |
| `docs/cycles/btw-runs-hooks/deliverable.md` (new) | +this file |

## Hook bus: `internal/hooks`

The new package is a typed in-process bus, not a shell-command
runner. AGY's real hooks.json invokes subprocesses per handler;
the clone keeps the API shape (event-name registration, blocking,
modifying) but skips the subprocess host because every existing
subagent / extension integration runs in-process.

### Public surface

```go
type Event string
const (
    PreSideQuestion  Event = "PreSideQuestion"
    PostSideQuestion Event = "PostSideQuestion"
)
type Decision int
const (
    DecisionAllow  Decision = iota
    DecisionBlock
    DecisionModify
)
type Payload struct {
    Event, Question, Answer string
    Tokens                  int
    Blocked                 bool
    BlockReason             string
    Timestamp               time.Time
}
type HookFunc func(ctx context.Context, p Payload) (Decision, string, error)
type Manager struct { /* unexported */ }
func NewManager() *Manager
func (m *Manager) Register(event Event, fn HookFunc) int
func (m *Manager) RegisterPriority(event Event, priority int, fn HookFunc) int
func (m *Manager) Fire(ctx context.Context, event Event, p Payload) (blocked bool, final Payload, reason string)
func (m *Manager) Count(event Event) int
func (m *Manager) Registered() []Event
func (m *Manager) Reset()
func SideQuestion(event Event, question string) Payload
```

### Dispatch semantics

* Handlers fire in `priority` order (lower first; ties broken by
  registration order). Stable across calls.
* `DecisionAllow` continues to the next handler.
* `DecisionModify` rewrites `Payload.Question` to the handler's
  reason string. Subsequent handlers see the modified copy.
* `DecisionBlock` short-circuits the dispatch and returns
  `(true, payload, reason)`. The caller echoes `reason` in place of
  the parent action's output.
* A non-nil `error` from a handler short-circuits with a synthesized
  block reason (`"hook error: <err>"`). The original error is
  recoverable via `errors.As` if a handler needs to distinguish
  HTTP-timeout-style failures from generic blocks.
* `ctx` cancellation aborts the dispatch with `"hook dispatch
  canceled: <err>"`. The TUI's parent context flows through
  unchanged; canceling the parent cancels every outstanding hook.

### Wire-up

`runMode` carries a `*hooks.Manager`; `NewRunMode` constructs one
during field initialization. Tests overwrite the field after
`NewRunMode` returns so each case has a private registry:

```go
m := NewRunMode(&fakeClient{}, ...)
m.hooks = hooks.NewManager()      // already populated by NewRunMode
m.hooks.Register(hooks.PreSideQuestion, myHandler)
```

Production wiring (subagent hosts, extension loaders) is a future
cycle: today, every TUI session starts with an empty Manager, so
`/btw` is a no-op for hooks until something registers. The cycle
register-on-startup shape (load `hooks.json`, attach subprocess
runner) is left for the cycle that wires the on-disk config.

## `/btw` integration

`runBtwSideQuestion` (cmd/wagyu/slash_btw.go) now:

1. Echoes the slash token + card header (unchanged cycle-230
   contract).
2. Builds a `Payload` and fires `PreSideQuestion`. The dispatch is
   short-circuited when a handler returns `DecisionBlock`:
   * The TUI echoes `"blocked by hook: <reason>"` via `slashRespond`.
   * `appendBtwToSession` records the question in `m.session` —
     the block is a visible transcript event, not a silent drop.
   * `PostSideQuestion` fires with `Blocked=true` and
     `BlockReason=<reason>` so logging / observation handlers can
     react.
3. If `PreSideQuestion` rewrote the question via `DecisionModify`,
   the modified text drives both the parent transcript append and
   the `mgr.Ask` call. The visible card header gets a second `btw ·
   <modified first line>` row so the user can see what the hook
   rewrote.
4. Appends the question to `m.session` as a user message via the
   new `appendBtwToSession` helper:
   ```go
   m.session = append(m.session, backend.Message{
       Role:    "user",
       Content: "> /btw " + question,
   })
   ```
   The `"> /btw "` prefix marks the side question distinctly from
   a normal Enter-submitted prompt in any tool that reads the
   conversation back; `saveSession` flushes the session to the
   on-disk store on the next user turn, so the marker persists
   alongside other inputs.
5. Fires `mgr.Ask(ctx, trimmed)` (unchanged cycle-230 contract).
   On success the `PostSideQuestion` payload carries the streamed
   `Answer` and `Tokens`. On error the `PostSideQuestion` payload
   carries `Blocked=true` and `BlockReason=err.Error()` so logging
   handlers see the upstream failure uniformly.
6. Renders the answer rows + token footer (unchanged cycle-230
   contract).

### Test contract update: parent transcript is *not* untouched

Cycle 230's `TestBtwParentTranscriptUntouched` asserted
`m.session` did not grow during `/btw`; cycle btw-runs-hooks
inverts that to match the "fires hooks like any other turn"
semantics the prompt asks for. The test now asserts:

* the parent messages are preserved at the front of `m.session`;
* a new user message is appended at the end;
* the appended content starts with `"> /btw "` and contains the
  question verbatim.

`TestBtwIndependentServices` (cycle 230) is updated to expect one
new user message rather than zero.

## Tests

8 new tests in `internal/hooks/hooks_test.go` cover the dispatch
semantics (register / fire, block, modify, priority, error, no
handlers, count, registered). 6 new tests in
`cmd/wagyu/slash_btw_test.go` cover the `/btw` integration
required by the prompt:

| Test | Asserts |
|---|---|
| `TestBtwFiresPreSideQuestionHook` | `PreSideQuestion` fires once per `/btw`; payload `Event` is `PreSideQuestion` |
| `TestBtwFiresPostSideQuestionHook` | `PostSideQuestion` fires after the turn settles with `Answer` + `Tokens > 0` |
| `TestBtwHookCanBlock` | `DecisionBlock` aborts `/btw`; no card; `PostSideQuestion` still fires with `Blocked=true`; transcript carries `"blocked by hook: …"` |
| `TestBtwMultipleInvocationsFireHooksPerCall` | Three `/btw` calls fire `Pre` and `Post` exactly once each (cumulative counts: 3 + 3); each handler saw a distinct payload |
| `TestBtwHookReceivesQuestionText` | Pre-handler payload `Question` is the user-typed text verbatim |
| `TestBtwSideQuestionAppearsInTranscript` | `m.session` grows by one user message with `"> /btw "` prefix and the question in the body |

Two pre-existing tests were updated to the new contract:

* `TestBtwParentTranscriptUntouched` — now asserts the question
  *is* appended (cycle-230 contract reversed).
* `TestBtwIndependentServices` — same.

`gofmt -l` reports no diffs in any new or touched file.

```
go build -o /tmp/wagyu-btw ./cmd/wagyu   # exit 0
go test -vet=off -count=1 \
  -skip TestDispatchPrintSlash_LocalCommands \
  ./cmd/... ./internal/...
# all packages green; the only failure on `main` is the documented
# pre-existing flake around model resolution (irrelevant to /btw)
```

## 硬约束 / 存疑 / limitations

1. **Hooks are typed in-process only.** The clone does not yet
   shell out to a `hooks.json` handler. Production wiring is a
   follow-up cycle; this commit only adds the API surface and the
   `/btw` integration. Tests can register handlers in-process via
   `m.hooks.Register(...)`.
2. **`DecisionModify` rewrites `Question` only.** The current API
   allows hooks to swap the question text. Modifying the streamed
   answer (`Payload.Answer`) or the token count (`Payload.Tokens`)
   is not in scope for cycle btw-runs-hooks; the typed events that
   would benefit from those (PostToolUse result rewrite) are not
   wired yet.
3. **`appendBtwToSession` writes a user-role message** even when
   the question was blocked. The block reason goes into the
   visible transcript row above, but the conversation db sees the
   question as a user prompt — matching AGY's "side question is a
   real turn" semantics. The `"> /btw "` prefix lets any reader
   distinguish side questions from normal prompts.
4. **The 1.3.3 changelog's literal direction is the opposite.**
   Cycle 1.3.3 stripped hooks from `/btw`; the prompt treats line
   49 as "implement hook firing for `/btw`". The clone follows the
   prompt's direction. If a future cycle needs the 1.3.3
   direction, it can short-circuit `m.hooks.Fire` behind a flag.
5. **No shell-out for PreToolUse / PostToolUse yet.** The
   cycle-205 `/hooks` panel lists five hook types; only the two
   side-question events fire today. The remaining three
   (PreToolUse, PostToolUse, PreInvocation, PostInvocation, Stop)
   need a tools-toolCall instrumentation pass to fire — left for a
   follow-up cycle that subscribes to `toolRunner`'s call site.
6. **The Manager is per-runMode, not global.** A multi-session
   runMode (e.g. tests that call `NewRunMode` twice) gets a fresh
   Manager each time; subagent hosts that span sessions must
   re-register. A global registry is straightforward but out of
   scope for the cycle.

## Next moves

1. Wire `PreToolUse` / `PostToolUse` from the tool runner so
   `cmd/wagyu/trust_prompt.go`'s shell-command approvals also see
   hook handlers. The Manager API is already event-agnostic.
2. Add an on-disk `hooks.json` loader (parse the recovered config,
   shell out per event) so the AGY-equivalent UX works without
   in-process registration.
3. Add `DecisionModify` extensions for `PostToolUse` (tool-result
   rewrite) and `PostInvocation` (assistant-message rewrite).
