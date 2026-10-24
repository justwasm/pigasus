# Cycle 167 — `/resume` picker + cycle-77 stub clean-up

## Scope

Closes the cycle-77 stub bodies that have been pending since
`docs/cycles/cycle-77` and wires `/resume` into the real
`internal/convstore` reader the user added in cycle 166. Highlights:

* `/resume` opens a transcript-overlay picker (Up/Down/Enter/Esc)
  populated from `convstore.ListConversations()` against the real
  `~/.gemini/antigravity-cli/conversations/*.db` (234 files on this
  machine). The picked db is loaded through the same `LoadConversation`
  path `--conversation=<id>` uses, then rendered through the existing
  `seedConversation`.
* `/fork` writes a fresh SQLite db under `StoreDir()/conversations/`
  with the recovered `trajectory_meta + steps` schema, so a
  `/resume`-to-fork roundtrip works without manual schema work.
* The remaining cycle-77 stubs are replaced with either a meaningful
  in-memory implementation (`/rewind`, `/compact`, `/plan`, `/goal`,
  `/learn`, `/btw`, `/keybindings`, `/fork`, `/feedback`, `/plugin`)
  or a clean "not available in openagy" message (`/voice`, `/browser`,
  `/artifact`, `/schedule`, `/grill-me`). None still print
  "(cycle 77: stub)".

## Files

| Path | Why |
|---|---|
| `internal/convstore/list.go` (new) | `ConversationMeta` + `ListConversations()` — the cycle-167 reader left over from cycle 166. |
| `internal/convstore/write.go` (new) | `WriteConversation()` — the cycle-167 `/fork` writer; emits the recovered schema. |
| `cmd/wagyu/slash_helpers.go` (new) | `forkConversation`, `rewindSession`, `compactSession`, `sessionToSteps`, `printSlashKeybindings`, UUID v4. |
| `cmd/wagyu/tui_resume_test.go` (new) | Resume picker + fork roundtrip tests. |
| `cmd/wagyu/slash_commands.go` | Replaces 16 stub bodies (resume, fork, rewind, voice, artifact, btw, compact, browser, plan, goal, grill-me, learn, schedule, feedback, keybindings, plugin). |
| `cmd/wagyu/tui_v135.go` | Adds `resumeMode/resumeList/resumeCursor`, `goal`, `learnings` fields; `/resume`/Up/Down/Enter/Esc handlers; `resumeView()` and View() wiring. |
| `cmd/wagyu/conversation.go` | Adds `resumeConversation()` — LoadConversation + seedConversation integration. |
| `cmd/wagyu/picker_view.go` | Adds `resumeView()` — picks a 220-column row layout (id, date, step count, title). |

## Resume flow

```
/resume ──► convstore.ListConversations()
          \─► m.resumeList = list (sorted ModTime desc, oldest at tail)
           \─► m.resumeMode = true

Up/Down ──► m.resumeCursor clamped to [0, len(resumeList))
Enter   ──► m.resumeConversation(picked)
            ├─ LoadConversation(picked.Path)         ← same as --conversation
            ├─ reset m.response/session/blocks/...
            ├─ m.seedConversation(c)                 ← renders transcript
            └─ m.conversationID = c.ID
Esc      ──► m.resumeMode = false, "  ⎿  Cancelled /resume"
```

## Data roundtrip

`/fork` produces a new `StoreDir()/conversations/<uuid>.db`. The
sqlite-writer emits the schema `gen_fixture.py` produces, including
a `trajectory_meta` row (`trajectory_id=uuid, trajectory_type=4,
source=17`) and one `steps` row per message. The protobuf payload
is the minimal shape `parseStepPayload` reads for `user-input`
(field 19 → 3 → text) and `model-response` (field 20 → 1 → text).

Subsequent `/resume` lists the new db on top — verified by
`TestFork_WritesConvStoreAndAppearsInPicker`.

## Tests

```
go test ./cmd/wagyu -run "TestResume|TestFork" -v
=== RUN   TestResume_PopulatesFromStoreDir
--- PASS
=== RUN   TestResume_UpDownMovesCursor
--- PASS
=== RUN   TestFork_WritesConvStoreAndAppearsInPicker
--- PASS
PASS
```

* `TestResume_PopulatesFromStoreDir`: writes two dbs to a temp
  `$ANTIGRAVITY_HOME`, runs `/resume`, asserts `m.resumeMode=true`
  and `resumeView()` shows both ids.
* `TestResume_UpDownMovesCursor`: writes 3 dbs, runs `/resume`,
  Down×3 → cursor clamps at 2, Up → 1, Esc → picker closes.
* `TestFork_WritesConvStoreAndAppearsInPicker`: 4-message live
  session, `/fork` writes exactly 1 db to `conversations/`, then
  `/resume` lists it.

## Stubs replaced — what each one does now

| Command | Behaviour |
|---|---|
| `/resume` | Opens picker overlay populated from convstore. |
| `/fork` | Writes a new db; subsequent `/resume` lists it. |
| `/feedback <text>` | "Feedback recorded (openagy has no upload endpoint)." |
| `/keybindings` | Lists the recovered keybinding table. |
| `/plugin` | "Plugins: 0 installed." |
| `/rewind` | Pops trailing user/assistant pair; "Rewound N message(s)." |
| `/voice` | "Voice mode: not available in openagy." |
| `/artifact` | "No artifacts available." |
| `/btw <q>` | Appends to `m.session` as a follow-up user turn. |
| `/compact` | Drops older half of `m.session`. |
| `/browser` | "Browser agent: not available in openagy." |
| `/plan` | Toggles `m.mode` between "" and "plan". |
| `/goal [text]` | Sets/clears `m.goal`; surfaced on the next call. |
| `/learn <note>` | Appends to `m.learnings`. |
| `/grill-me` | "Interview mode enabled (openagy logs follow-up questions)." |
| `/schedule` | "Schedule: not available in openagy." |

## Open / Limitations

* The picker rows show titles truncated to 60 chars; long titles
  fall back to `(empty)` when none can be inferred. The reference
  has grouping + tabs we don't reproduce.
* `/fork` carries only user/assistant text turns — tool calls from
  a forked session are dropped (we don't have the protobuf for them
  yet). Marked 【推测】 in `slash_helpers.go`.
* The pre-existing `TestChangelog_MatchesReferenceBinary` failure
  (3232-byte drift) is unchanged by this commit; it pre-dates
  cycle 167.
* Mouse-mode trade-off is unchanged: wheel scroll requires
  `/mouse on`, terminal-native select-and-copy requires `/mouse off`.
  In-app selection (the reference's compromise) is still a
  multi-cycle feature, not pursued here.
