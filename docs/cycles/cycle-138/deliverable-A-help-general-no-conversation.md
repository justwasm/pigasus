# Cycle 138 — /help GENERAL drops the Conversation row in v1.3.6

## Why

The v1.3.5 reference capture (`/tmp/ref_tab_bar_general_raw.bin`,
2026-10-16 01:43) shows:

```
Log File:       ~/.gemini/antigravity-cli/log/cli-20261016_010555.log
Conversation:   a5430daf-b360-49c8-b185-a46635969962

Quick Reference
```

The v1.3.6 reference capture
(`/tmp/agycmp/hotpath/help_tabs/ref.000-general.txt`,
2026-10-16 19:43) drops that row:

```
Log File:       ~/.gemini/antigravity-cli/log/cli-20261016_194308.log

Quick Reference
```

Between 1.3.5 and 1.3.6 the binary removed the
`Conversation:   <UUID>` line. The cycle-86 clone printed it on
every render of the general panel, so the harness diff counted an
extra 38 chars at step 000-general and propagated into 003-wrapped
and 004-closed where the same panel is rendered.

## What changes

### `cmd/wagyu/tui_v135.go`

`renderV135HelpGeneral` no longer emits the
`\x1b[38;2;95;99;104mConversation:   <uuid>\x1b[39m\n` row. The
two-line block now goes straight from the Log File row to the
blank-row separator before `Quick Reference`. `m.conversationID`
is still generated at session start (cycle-86 used it for
`/status` and `/rename`), it's just no longer printed in the
general panel.

### `cmd/wagyu/tui_v135_test.go`

`TestV135_HelpGeneralPanelStyling`:

- `wantRows` ends at `Log File:` (the previous final entry was
  `Conversation:   test-uuid-...`).
- A new negative assertion: the rendered output must NOT contain
  the literal string `Conversation:` — pinning the cycle-138
  removal against future regressions.

## What does NOT change in cycle 138

- `m.conversationID` is still populated at session start
  (`newV135ConversationID()` in `tui_v135.go:988`) so other code
  paths that read it (e.g. status row, /rename) keep working.
- The cycle-86 capture file (`/tmp/ref_tab_bar_general_raw.bin`)
  still has the row, by definition — that's a 1.3.5 artefact, not
  a target.
- The harness capture files under
  `/tmp/agycmp/hotpath/help_tabs/` are 1.3.6 captures and continue
  to be the source of truth.

## Hard constraints / 存疑 / limitations

- 【推测】Why did 1.3.6 drop the Conversation row? Probably to
  declutter the panel — the UUID is shown elsewhere (e.g.
  `--conversation=<uuid>` argument). Cycle 138 mirrors the visible
  behaviour.
- 【推测】The v1.3.6 reference's exact panel layout may still
  drift slightly (e.g. if the Quick Reference section gains a
  second item). Cycle 138 only fixes the Conversation row.