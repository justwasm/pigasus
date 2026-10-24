# Cycle 279 — long-horizon turns stop silently

**Defect (as reported):** on a long task the clone "suddenly can't continue" and ends
up printing nothing; the reference `agy` 1.4.2 does the same task.

**Verdict:** reproduced against a real model, and the cause is not the model. The
live TUI put a **60-second wall-clock deadline on the whole agentic turn**, and
when it fired the turn's error was **discarded** while the transcript renderer
**replaced** everything the turn had streamed with an empty tool block. Two more
long-run hazards sit behind it: a second 60-second cap on the interactive HTTP
transport, and an 8-iteration tool budget that the reference does not have.

The reference, on the same prompt, has no per-turn deadline: it waited 70 s for a
tool and answered normally.

---

## 1. Method — real MiniMax M3, both CLIs, same task

Per the brief, no scripted-backend evidence is used for the headline claim.
Everything below is a live turn against `https://agw.k0s.io`
(`MiniMax-M3`, 1 M-context registry entry).

| | reference | clone |
|---|---|---|
| binary | `/tmp/agy-wrapper` (official `agy` 1.4.2 + local CloudCode proxy, `CLOUD_CODE_URL` redirect, vendor binary unpatched) | `/tmp/wagyu-long` (this worktree) |
| model | `--model custom-minimax-m3` | `--model "MiniMax M3"` |
| approvals | `--dangerously-skip-permissions` | same |
| terminal | tmux 200×40, frame capture every 2 s | same |
| wire | `OPENAGY_CAPTURE_DIR` (wrapper capture) | a stdlib SSE relay in front of agw (`OPENAGY_AGW_URL`), recording every request/response body |
| state | — | `ANTIGRAVITY_HOME=/tmp/longrun-home` (copy of the store) |

The two runs used two byte-identical copies of the same working directory
(`/tmp/longrun/ab/{ref,clone}`) so neither could clobber the other's `NOTES.md`.

The raw artefacts for the runs below (turn log, tmux frames around the stop,
both wire captures, the reference's `NOTES.md` head) were captured in `TMPDIR`
and were destroyed by a TMPDIR reset on 2026-10-20 before they could be copied
into the repository. What is committed is a **post-fix** live re-capture of the
same H1 prompt against the landed tree
(`evidence/postfix-frames-sleep70.txt`); the pre-fix numbers below are quoted
from the lost captures and are flagged in §10.

Three prompts were used:

1. **H1, deterministic > 60 s** — *"Run the shell command `sleep 70` … then tell me
   in one sentence how long it slept. Do not background it."*
2. **H2, long-horizon** — *"Follow the token chain … run `sh chain.sh <token>` for
   each token … then write every token you visited … to RESULT.md"* (31 links).
3. **H3, 51-file sweep** — *"For each .go file under `src/`, read it and append one
   line summarising it to NOTES.md. Do not stop until every file has been
   summarised."*

---

## 2. The failure, reproduced 【一手】

### 2.1 Pre-fix clone, H1 (`sleep 70`)

Instrumented build of the pre-fix tree (`OPENAGY_TURN_LOG`, env-gated, not
committed). The turn log:

```
05:39:52.588 turn start msgs=1
05:40:52.582 tool end name=run_command err=<nil> bytes=122 ctxErr=context deadline exceeded
05:40:52.591 sendPig: pigErr=context deadline exceeded
             failure=Post "https://agw.k0s.io/v1/chat/completions": context deadline exceeded
             ctxErr=context deadline exceeded maxTurns=8
05:40:52.606 turn end elapsed=1m0.017s err=context deadline exceeded
             ctxErr=context deadline exceeded text=0 steps=1
```

`elapsed=1m0.017s`, `text=0`. The pane, captured two seconds apart:

```
05:40:51   ▸ Thinking... (55.7s)
           ⣻  Bash (sleep 70)

05:40:53   ● 1 command, 1 tool (ctrl+o to expand)
           >                    ← prompt back, no reply, no error
```

That is the reported symptom exactly: the spinner stops, one collapsed tool row
remains, and **nothing else is ever printed**. The turn was not cancelled by the
user (no Esc), the process stayed alive, and the session remained usable.

### 2.2 Reference, same prompt 【一手】

```
● [05:39:57] time sleep 70 running
● [05:40:11] Schedule check after 75s running

  The sleep 70 command slept for 70.01 seconds of wall-clock time (real total: 1:10.01).
```

It waits, keeps the task visible, and answers. No turn deadline fired at 60 s.

The fixed clone, same prompt, same session shape: turn issued 06:02:35, tool
started 06:02:41, answer printed at 06:03:51 —

```
The sleep 70 command slept for exactly 70 seconds, finishing at 06:03:51
after starting at 06:02:41.
```

~76 s of wall clock, no deadline, no loss of the reply.

### 2.3 Where the 60 s comes from

`cmd/wagyu/tui_run.go` (`streamPrompt`, the function behind every prompt
submission, `/` command, and queued prompt):

```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
```

Introduced by cycle 155, it covers **the entire tool loop**, not one request: every
model call plus every tool call, across all iterations. A 51-file sweep, a test
suite run, or a single `sleep 70` all exceed it.

### 2.4 Why nothing was printed

Two independent decisions compounded:

1. **The error was dropped.**
   `tui_run.go` called `_ = loop.Send(ctx, agent.TurnInput{Messages: msgs})` and
   `turnDoneMsg` had no error field. `ctx.Err()` is (correctly) checked *inside*
   `sendPig` before classification, so a deadline is indistinguishable from a user
   Esc there — and the only consumer that could have reported it threw it away.
2. **The transcript was overwritten.**
   The chunk handler had already streamed the failure text into `m.response`, but
   the `turnDoneMsg` handler ends with
   `m.replaceRange(m.liveStart, len(m.response), block)` — the streamed range is
   replaced by the tool block. With zero steps the block is empty, so the error
   text was *erased* by the very handler that finalised the turn.

The second point is why the user sees "nothing" rather than an error: the message
existed for a frame or two at most, then got wiped.

---

## 3. The second 60 s: the interactive transport 【推测】

`cmd/wagyu/main.go` picked the interactive backend with the agw client's default:

```go
return pickBackendWithLog(fs, modelName, io.Discard, 0)   // 0 → 60 s
```

and `backend.AgwK0sConfig.withDefaults()` turns that into
`http.Client{Timeout: 60s}`, which caps **the whole HTTP exchange, including the
SSE body**. A single model response that streams for more than 60 s — deep
thinking, a long answer — is cut there even though the turn is no longer
time-boxed. The headless path already passes `backend.AgwTimeoutUnlimited`
(`main.go:920`), so `-p` never had this problem; only the live TUI did.

This one is **not** separately reproduced: both caps were exactly 60 s, so the
observed failure is attributed to the turn deadline (the log shows the tool
returning with a dead context, and the `Post … context deadline exceeded` is the
*next* request failing instantly, not a body cut). It is graded 【推测】 and fixed
preventively; see §8 for the untested part.

---

## 4. The 8-iteration budget 【一手】+【推测】

`internal/agent/loop.go` capped a turn at eight model turns:

```go
const defaultMaxToolIterations = 8
```

On the clone's own H3-shaped run (16 files) the model used **exactly 8** requests
and finished on the last one; the next file would have failed. The reference on
H3 used **17** `custom-minimax-m3` requests for the 51 files
(`cap-ab-ref/*-streamGenerateContent.json`, plus 3 title-generation calls on a
different model) — more than twice the clone's cap.

Two consequences beyond "the turn stops":

- **The wrong recovery copy.** `sendPig` returned
  `fmt.Errorf("agent: exceeded tool-iteration budget (%d)", maxTurns)`, flattening
  `piagent.ErrMaxTurnsReached`. `recoveryForError` therefore classified the cap as
  a *stream interrupt*, so the model was told the stream dropped instead of the
  reference's `**Status**: Cancelled after N iterations`.
- **The retry has no history.** `sendPig` builds a fresh PiG agent per attempt and
  discards the messages it returns (`_, err = runner.SendMessages(...)`), while
  `sendPigAuto` retries with `input.Messages` — the *user/assistant text* only.
  After a cap or a dropped stream the model restarts the task knowing nothing
  about the tool calls it already made. See §7.

PiG's own contract is explicit: `MaxTurns` **0 = unlimited, "as upstream, which
has no turn cap"** (`PiG/agent/agent.go:747`). `8` was inherited from the clone's
pre-PiG hand-rolled loop and never revisited when cycle 183 moved the loop to PiG.

---

## 5. What the reference does with a long task 【一手】

| | reference | clone (pre-fix) |
|---|---|---|
| H1 `sleep 70` | waits, answers at ~70 s | dies at 60.017 s, prints nothing |
| H2 chain, 31 links | 8 task requests (+3 title), completes | (not run to the cap pre-fix) |
| H3 51 files | **17** task requests (+3 title), `NOTES.md` **53 lines**, completes | — |

The reference never aborted a turn on elapsed time in any run, kept partial output
visible, and its own long-command handling is a *task* row (`time sleep 70
running`), not a turn corpse.

Its `**Status**: Cancelled after %d iterations` string (1.4.2 string table) shows
it does have a generator-invocation limit, but the behaviour around it is
**auto-continue** (`AUTO_CONTINUE_ON_MAX_GENERATOR_INVOCATIONS`), not abort with
no output.

---

## 6. Fixes

### 6.1 The turn is cancellable, not time-boxed — `cmd/wagyu/tui_run.go:3999`

```go
// Cycle 279: the turn is cancellable, NOT time-boxed.
ctx, cancel := context.WithCancel(context.Background())
```

Esc still cancels (`m.turnCancel`, cycle 235, unchanged). What bounds a turn now,
all of which existed already:

- `piagentbridge.StallTimeout` = 5 min with **no frame** — the reference's own
  rule (AGY 1.3.6 changelog: *"Added a 5-minute stall detection for stuck API
  requests"*), wired in `pumpSSE`;
- the user's Esc;
- per-tool timeouts carried by the tool schema.

### 6.2 No total deadline on the interactive transport — `cmd/wagyu/main.go:1468`

`pickBackend` now passes `backend.AgwTimeoutUnlimited`, matching print mode. The
5-minute idle watcher is the bound, not elapsed time.

### 6.3 The cap is unlimited by default — `internal/agent/loop.go:208`

`defaultMaxToolIterations = 0` (PiG's sentinel for "no cap"), with a comment
recording why 8 was wrong and what bounds a runaway turn instead. A caller that
wants a budget sets `Options.MaxToolIterations`; the cap path now also **wraps**
the sentinel (`pig.go:190`):

```go
return fmt.Errorf("agent: exceeded tool-iteration budget (%d): %w", maxTurns, err)
```

so `errors.Is(err, piagent.ErrMaxTurnsReached)` holds and auto-continue injects the
reference's iteration-cap copy.

### 6.4 A failed turn is always visible — `cmd/wagyu/turn_error.go` + `tui_run.go:3546`

`turnDoneMsg` carries `err`; `streamPrompt` returns the real `loop.Send` error
(after the `ctx.Err()` check, so Esc stays a silent cancel); the handler appends
an `Error: …` row **after** the reply range is finalised, so nothing replaces it.
API errors go through `classifyAPIError`, so the TUI shows the same message as
headless `error:` / `AGY_ERROR`. Verified live against a dead endpoint:

```
Error: Network error after 8 retries: gemini-api-key: connection to
http://127.0.0.1:1/v1/chat/completions dropped: Post "http://127.0.0.1:1/…":
dial tcp 127.0.0.1:1: connect: connection refused
```

Before the fix that turn ended with no row at all.

### 6.5 Auto-continue banner reported the wrong budget — `internal/agent/loop.go:175`

The network path allows 8 attempts (`AutoContMaxNetworkDepth`) but the banner was
hard-coded to 3, so a real outage rendered `retry 4 of 3`, `retry 6 of 3`. The
callback now receives `(attempt, maxDepth)`, and the TUI prints what the loop is
actually doing.

### Tests

| test | pins |
|---|---|
| `TestLoop_NoToolIterationCapByDefault` | a 12-turn chain runs to completion with the default options |
| `TestLoop_ToolIterationBudgetStopsRunaway` (extended) | an explicit budget still stops, *and* wraps `ErrMaxTurnsReached` / classifies as iteration-cap |
| `TestTurnDoneMsg_ErrorRowSurvivesTurnBlock` | the error row survives `replaceRange` next to the tool block |
| `TestTurnDoneMsg_ErrorRowUsesReferenceCopy` | API errors use `classifyAPIError`'s copy; `nil` renders nothing |
| `TestPickBackend_InteractiveHasNoTotalRequestDeadline` | interactive backend is `AgwTimeoutUnlimited` |
| `TestPickBackend_HonoursExplicitBackendFlag` | the assertion above is not vacuous (agw, not shim) |

`go test ./cmd/... ./internal/...` — all packages pass.

---

## 7. Known-but-unfixed: auto-continue replays without tool history

`sendPigAuto` retries with `input.Messages` + a recovery line. `input.Messages` is
the TUI's user/assistant transcript; the tool calls and their results from the
failed attempt were never in it (`sendPig` drops the messages PiG returns). So
after a *genuine* stream drop the model is told "continue exactly where you left
off" while its working memory of the task is gone — it re-reads files and redoes
work, and on a long task can burn the whole retry budget doing it.

The design that fixes it (not implemented here, because it changes the `Loop.Send`
contract and the TUI's session model):

1. `sendPig` returns `runner.MessagesSnapshot()` alongside its error on the
   failure paths, or stashes it on the `Loop` under the same single-send guard.
2. `sendPigAuto` converts that snapshot to `[]backend.Message` (text, assistant
   tool calls, `tool` results) and retries with
   `snapshot + recovery` instead of `input.Messages + recovery`.
3. `Loop.Send` gains a way to report the transcript it produced, so the TUI can
   persist the tool steps it currently only renders.

`piagentbridge.pigMessages` is the inverse of the needed converter; the missing
half is `backend.MessagesFromPig`.

---

## 8. 硬约束

- **No per-turn cap in the reference** is an observation across four live turns
  (H1 ×2, H2, H3), not a proof that no limit exists. `agy` carries a
  generator-invocation limit server-side (its `**Status**: Cancelled after %d
  iterations` string + `AUTO_CONTINUE_ON_MAX_GENERATOR_INVOCATIONS`); we did not
  reach it.
- **The 60 s transport cap (§3) is not reproduced.** Both caps shared the same 60 s
  value, so the measured failure is attributed to the turn deadline. Removing the
  transport cap is preventive and derived from the code (`http.Client.Timeout`
  includes body read) plus the fact that print mode already ran unlimited.
- **H2 was not re-run to the old cap** after the fix; the cap evidence is the
  reference's 17-request H3 run versus the clone's structural cap of 8, plus the
  unit test.
- **One live comparison is incomplete:** the post-fix clone H3 run (51 files)
  was invalidated by this environment losing DNS for `agw.k0s.io` mid-run
  (`socket.gaierror`), which the relay surfaced as 502. The reference leg of H3
  completed (17 task requests, 53-line `NOTES.md`). The clone leg proved the new error
  row, not the completion.
- The fixes do not add a *new* timeout; they remove two and rely on the existing
  5-minute idle watcher. A gateway that trickles one byte every 4 minutes will now
  hold a turn open indefinitely — same as the reference.
- `internal/agyprompt` and `internal/tools/catalog.go` were not touched.

## 9. 存疑

- Whether `ctx.Err()` should suppress the error row for a *deadline* as well as a
  user cancel. The reference shows nothing for a user cancel; we show nothing for
  any cancel. With no deadline left, the distinction only matters if a caller
  adds one back.
- `turnErrorRow`'s wording for the two give-up wrappers
  ("Network error after N retries: …", "The turn stopped before it finished: …")
  is clone prose, not recovered reference copy. The API-error branch is verbatim.
- The 5-minute idle watcher runs only on the native provider path
  (`pumpSSE`), which is what the TUI and `-p` use for agw. A shim/MCP-backed
  provider still relies on the transport's own bounds.

## 10. limitations

- **The pre-fix raw captures were lost to a TMPDIR reset.** The turn log
  (`1m0.017s text=0`), the stop-adjacent frames and the H2/H3 wire JSONL lived
  in the session's scratch tree and were deleted at 14:19 on 2026-10-20, before
  they were copied in. The numbers quoted from them above are therefore
  second-hand in this repository; only the post-fix H1 confirmation
  (`evidence/postfix-frames-sleep70.txt`, 14:52–14:54, `main` @ `2391ae4`) is
  committed as raw evidence. The pre-fix claim is still independently
  reproducible by removing the `streamPrompt` patch and re-running H1.
- **The tree was reconstructed from the session record.** After the reset, the
  original commits were gone; the changes were replayed from the recorded
  edit/write/instrumentation steps onto `a5af64b`, rebuilt, and re-tested. The
  replayed tree is what `997b0b2` contains.
- **The evidence run and the committed tree differ by instrumentation only.** The
  pre-fix repro used an env-gated `dbg()` writer; it was removed before the
  commit, and the committed binary was rebuilt and re-tested. The line numbers in
  §2.3/§6 refer to the committed files.
- **Git history had to be re-rooted.** Mid-session the checkout at
  `/Users/gear/Documents/GitHub/openagy` became unreadable (`EPERM`, including
  `.git`), so this worktree's `.git` gitfile no longer resolved. The branch
  `cycle-279-long-run` therefore sat on a locally reconstructed `main` snapshot
  (the pre-fix tree, reconstructed by reversing exactly these edits; `go build` +
  `go vet` clean) rather than on upstream `a5af64b`. The change was later
  transplanted onto the real repository as `997b0b2`; it applies cleanly to
  `a5af64b` and the rebuilt binary is what the 14:52 re-capture ran.
- Frame captures are sampled every 2 s; the "nothing printed" claim also rests on
  the turn log (`text=0`) and on `m.replaceRange`'s code path, so a sub-2-second
  flash cannot be fully excluded from the pane evidence alone.
- Only MiniMax M3 was exercised. Whether the reference behaves identically on
  Gemini/Claude is untested here; the turn deadline is model-independent.
- Interactive transport verification was done through a local relay for the
  clone. That relay is an observer, not a proxy transform: it forwards the request
  bytes and streams the response bytes unchanged (no re-encoding), but it does add
  a TCP hop.
