# Cycle 149 — Drop slash command echo in transcript

## Why

The v1.3.6 reference binary does **not** echo slash commands into
the transcript. The picker overlay's prompt area already shows
the command while the user is typing it, and after Enter the
handler's output (changelog dump, /help panel, error message) is
the next thing rendered. Captured `/changelog`, `/help`, and
other slash commands against the reference binary all confirm
this — none shows a `> /<cmd>` line in the transcript.

Cycle 92 added the echo (`m.response = append(m.response,
userPromptRow(buf))` at slash_commands.go:50) based on a
misreading of the picker overlay's prompt area as a transcript
entry. Cycle 126 normalised the colour escapes but kept the
echo. Cycle 149 removes the echo entirely so the transcript
matches the reference byte-for-byte.

Captured at `/tmp/refcl2.txt` (ref binary, `/changelog` + Enter):

```
   Antigravity CLI Changelog

  The terminal-first surface to interact with Antigravity agents. Stay in your flow without
  context switching.
  ...
```

No `> /changelog` line — the command goes straight to the
changelog dump.

Captured at `/tmp/agycmp/hotpath/help_tabs/ref.000-general.txt`
(ref binary, `/help` + Enter):

```
──────────────────────...
>                                    ← prompt area (empty after Enter)
──────────────────────...
Antigravity CLI   general    commands   ...
```

No `> /help` line — the prompt area is empty and the /help
panel starts immediately.

## What changes

### `cmd/wagyu/slash_commands.go::handleSlashCommand`

- Removed `m.response = append(m.response, userPromptRow(buf))`
  on line 50. The cycle-92 echo is gone; the transcript now goes
  straight from the prior state to the handler's output.

### `cmd/wagyu/tui_v135_test.go::TestV135_HelpOpensOverlay`

- Updated assertion: `len(m.response) == 0` (was `len == 1` with
  the cycle-92 echo string). The /help case sets `helpMode`
  without producing transcript output.

### `cmd/wagyu/slash_content_test.go::afterEcho`

- Updated helper to return `m.response` directly (was
  `m.response[1:]`, skipping the cycle-92 echo). Every test that
  uses `afterEcho` continues to assert the handler's output
  bytes-for-bytes.

## What does NOT change in cycle 149

- The picker overlay's prompt area still shows `> /<chars>` while
  the user is typing. That surface is rendered by `viewBody`,
  not by the transcript.
- Regular chat messages still echo `> <prompt>` into the
  transcript (at tui_v135.go:1416). The cycle-92 echo was only
  for slash commands.
- `userPromptRow` itself is unchanged — it's still used by
  `conversation.go::seedConversation` for the resumed-conversation
  transcript path, and by the chat-message echo at
  tui_v135.go:1416.
- The cycle-148 `(Failed)` suffix on resumed tool calls is
  unaffected.

## Hard constraints / 存疑 / limitations

- 【推测】Why the cycle-92 capture was wrong. The cycle-92
  capture observed a transcript line that *looked* like a
  slash-command echo. It was probably either (a) the prompt
  area bleeding into a capture taken at the wrong vertical
  offset, or (b) a side effect of the way the cycle-92
  test faked the key dispatch. Capturing the reference binary
  on a real terminal makes the missing echo obvious.
- 【推测】Why the picker scenario doesn't have a transcript
  entry either. The picker scenario only types `/` (no
  Enter) before pressing arrows, so `handleSlashCommand` is
  never called. The transcript stays clean. With cycle 149,
  this matches what the user sees for `/help`, `/changelog`,
  and every other slash command.
