# Cycle 228 — chat-transcript markdown rendering fix

## Why this cycle

After merging the AGY 1.4.2 package-adoption wave the user reported
that the live chat transcript shows raw markdown output (`**bold**`
and `- list items` literal, no glamour styling), while the resumed
conversation path renders correctly. Root cause:

```
$ git log -L 3034,3034:cmd/wagyu/tui_run.go
8e32c2d Cycle 99: interactive tool calling, ...
diff --git a/cmd/antigravity/tui_v135.go b/cmd/antigravity/tui_v135.go
+                       m.response = append(m.response, msg.reply)
```

The cycle-99 turn-end handler has appended `msg.reply` verbatim since
the very first interactive tool-calling cycle. Cycle 155 added
`renderMarkdown` to the **resumed** conversation path
(`seedConversation`), and cycle glamour-migration (9b0920a) replaced
the hand-rolled mdrender walker with `charm.land/glamour/v2` v2.0.1,
but only migrated the four production callers
(`cmd/wagyu/conversation.go:466 renderMarkdown`,
`cmd/wagyu/tui_tools.go:918 stepDetailLines`,
`cmd/wagyu/render_user_text.go:47 renderUserText`,
`internal/cli/login_prompt.go:48 RenderLoginPrompt`). The cycle-99
turn-end handler in `cmd/wagyu/tui_run.go:3034` was never wired
through `renderMarkdown`, so every live chat reply reached `m.response`
as raw markdown — a bug visible from cycle 99 onward, surfaced by
glamour migration only because the resumed-path output diverged from
the live-path output and the divergence became harder to ignore.

## The fix

`cmd/wagyu/tui_run.go:3034` now routes the reply through the same
two helpers the resumed-conversation path uses:

```go
styled := renderMarkdown(msg.reply)
m.response = append(m.response, indentModelReply(styled, "  "))
m.session = append(m.session, backend.Message{Role: "assistant", Content: msg.reply})
```

- `renderMarkdown` is the glamour-backed wrapper added in cycle 155
  and migrated to `mdrender.ANSI` in cycle glamour-migration.
- `indentModelReply` is the cycle-154 two-space indent that makes
  the model-reply block read as a quote on a 200×60 TUI.
- `m.session` is **deliberately** left with the raw text so the
  conversation DB stores the model's un-styled output. This matches
  the resumed-path policy (cycle 128 explicit on this) — the colour
  hint lives in the rendered transcript, not the persisted session.

## Regression test: `TestTUITools_TurnDoneRendersMarkdown`

Pins the cycle-228 contract end-to-end:

1. Build a `runMode`, set a synthetic prompt row, send a `turnDoneMsg`
   whose `reply` contains `**bold**` and a list (`- one\n- two`).
2. Read back `m.response` and assert:
   - the literal substring `**bold**` is **not** present (raw
     markdown would have leaked through),
   - `bold`, `one`, `two` are all present (the reply text survives),
   - the ANSI escape `\x1b[1m` is present (glamour v2 emitted the
     bold style).

The escape-presence check is intentionally a coarse sentinel rather
than a byte-exact glamour snapshot — glamour v2 emits 24-bit
colours (`\x1b[38;2;R;G;Bm`) and the cycle-126 true-colour
recolourings ride on top of that. Pinning byte-exact would couple
the test to glamour's internal theme table; the sentinel catches a
regression where `renderMarkdown` is bypassed again.

## Hard constraints / limitations

- **No live streaming fix.** The user also observed "session output is
  not streaming". The TUI's `agent.Loop` handler at
  `cmd/wagyu/tui_run.go:3366` accumulates chunks into a `full`
  `strings.Builder` and emits the entire reply as one `turnDoneMsg`
  at end-of-turn. Forwarding each chunk to `prog.Send(backendChunkMsg(...))`
  for live streaming is a separate cycle — it interacts with the
  final-reply append (must avoid double-rendering), and requires
  line-buffered glamour to render partial `**bo` tokens. Documented
  as a follow-up cycle.

- **No print-mode markdown fix.** `cmd/wagyu/main.go:944` writes
  sanitised chunks straight to `out` with no glamour call. Adding
  per-line glamour there needs TTY detection (glamour ANSI is noise
  on a piped stdout) and is therefore a follow-up.

- **`renderMarkdown(text)` is the global helper in
  `cmd/wagyu/conversation.go:491`.** It works on a full string and
  has no streaming API; calling it once per `turnDoneMsg` is the
  cheapest correct fix.

## Test results

| Sweep | Result |
|---|---|
| `go build -o /tmp/wagyu-cycle228 ./cmd/wagyu` | exit 0, 85 MB |
| `go test -vet=off -count=1 -run TestTUITools_TurnDoneRendersMarkdown ./cmd/wagyu/` | PASS |
| `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | 56 packages OK, 0 FAIL |
| `go test -vet=off -count=1 -run TestTUITools_CtrlOTogglesStepDetail ./cmd/wagyu/` | PASS (existing byte-pin on "It is now." unaffected — `renderMarkdown("It is now.")` is a no-op) |

## Files changed

| File | Change |
|---|---|
| `cmd/wagyu/tui_run.go` | wrap `msg.reply` in `renderMarkdown` + `indentModelReply` at the turn-end handler (cycle-99 site, line 3034) |
| `cmd/wagyu/tui_tools_test.go` | add `TestTUITools_TurnDoneRendersMarkdown` regression test |
| `docs/cycles/cycle-228-chat-rendering/deliverable.md` | this report |

## AGY alignment

AGY 1.4.2's chat transcript renders bold / lists / fenced code
through its embedded glamour v2 — same library this cycle wires the
clone to. The cycle brings the live-turn path to byte-level parity
with the resumed-turn path, and both align with the AGY changelog
string "Improved markdown rendering by upgrading `glamour` to v2.0.1
for cleaner headings and block padding."