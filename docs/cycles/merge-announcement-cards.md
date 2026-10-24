# merge announcement-cards — feature cards above prompt

## TL;DR

AGY 1.2.17 changelog line 96: "cards above the prompt for model launches,
deprecations, and service notices". The clone now renders them as one row
between the top separator and the prompt, newest-first, persistent across
restarts (Esc to dismiss permanently), and per-session hidden once any
message is sent in that session.

| | |
|---|---|
| Branch | `feature/announcement-cards` |
| Commit | `7bc5f3a` |
| Merge | `f29b662` |
| Binary delta | 73.1 MB → 73.1 MB (+0; fully additive) |
| Lines | +1349 / −0 (6 files) |
| New package | `internal/announce` |
| Cycle report | `docs/cycles/announcement-cards/deliverable.md` |

## Scope (vs AGY 1.2.17/96)

| Reference behaviour | Clone implementation | Status |
|---|---|---|
| Card row above prompt | `cmd/wagyu/announcement_cards.go::announcementRow()` in `viewBody` | ✅ |
| Esc-on-empty-prompt dismisses permanently | `cmd/wagyu/tui_run.go::Update` Esc branch | ✅ |
| Sending a message hides for the session | `runMode.announceSessionHiddenID` set in `submitPrompt` | ✅ |
| Persist dismissal | `internal/announce.SaveDismissed` writes `settings.json.dismissedAnnouncements` | ✅ |
| Idempotent / no-clobber | alphabetised insert path, `mutateSettingsJSON` round-trip | ✅ |

## Architecture / data flow

```
settings.json (~/.gemini/antigravity-cli/settings.json)
  └─ dismissedAnnouncements: [string id, ...]      ← persisted
                                                  ↑
runLiveTUI  → loadAnnouncements()                  │
              ↓                                    │
internal/announce.Catalog() (fixed 3-card)         │
              ↓ filter by VisibleCards             │
runMode.announceQueue                              │
              ↓                                    │
viewBody()  → announcementRow()                    │
              ↓                                    │
Esc on empty prompt → dismissCurrentAnnouncement() ┘
                                ↓
                  SaveDismissed(id) idempotent
```

## Files in commit

| File | Status | Lines | Role |
|---|---|---|---|
| `cmd/wagyu/announcement_cards.go` | new | +203 | TUI glue: `announcementRow()`, `activeAnnouncement()`, `dismissCurrentAnnouncement()`, `hideCurrentAnnouncementForSession()`, `loadAnnouncements()` |
| `cmd/wagyu/announcement_cards_test.go` | new | +341 | 11 TUI integration tests |
| `cmd/wagyu/tui_run.go` | modified | +67 | runMode fields, viewBody render slot, Esc handler, submitPrompt hide, runLiveTUI wiring, import |
| `docs/cycles/announcement-cards/deliverable.md` | new | +202 | TL;DR, layout diagram, gates, probe table, 8-item 存疑 |
| `internal/announce/catalog.go` | new | +224 | Card type, fixed Catalog (3 cards), VisibleCards filter, LoadDismissed / SaveDismissed against `settings.json` |
| `internal/announce/catalog_test.go` | new | +312 | 13 catalog tests |

## Gates

| Gate | Result |
|---|---|
| `git rebase main` | up-to-date (no rebase needed) |
| `go build -o /tmp/wagyu-announce ./cmd/wagyu` | exit 0, 73.1 MB |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` | 28 packages green; only 2 pre-existing flakes (`TestDispatchPrintSlash_LocalCommands`, `TestWrapRowsQuadratic`) — both verified independently of this cycle |
| `gofmt -l` on every touched Go file | empty |
| `go vet ./cmd/wagyu/ ./internal/announce/` | no new warnings |
| New tests (cycle code paths) | 13/13 catalog + 11/11 TUI = 24/24 pass |
| `internal/convstore/sqlite.go` | NOT TOUCHED ✓ |
| Probe-vs-clone | catalog is fixed (3 example entries); byte-equal pending a remote-feed wire |
| `git worktree remove --force` + `git branch -D` | done |

## Two pre-existing flakes (NOT regressions)

Confirmed both fail identically on `main` (657f012) without this cycle's code:

| Test | Why it flakes |
|---|---|
| `TestDispatchPrintSlash_LocalCommands` (print_slash_test.go:45) | Asserts `dispatchPrintSlash("/model")` returns `"minimax-m3\tMiniMax M3\n"` but does not pin `ANTIGRAVITY_HOME`, so the result depends on the user's `~/.gemini/antigravity-cli/settings.json` model field. Environmental. |
| `TestWrapRowsQuadratic` | Timing-flaky: passes 3-of-3 isolated runs after main-only checkout. |

## 存疑

1. Catalog is fixed (3 example entries). Wiring to a remote feed is a future cycle.
2. Persistence error surfacing is one-shot (no retry on `settings.json` lock).
3. `announceSessionHiddenID` is recorded but not yet consumed by any UI element.
4. Card row is suppressed while picker / resume / help overlays are visible.
5. `m.announceVisible` is mutated without a mutex — single-goroutine writes today.
6. Card uses `ⓘ` and box-drawing chars (UTF-8) — fine on default macOS Terminal.
7. `NewRunMode` no longer auto-loads the queue; that moved to `loadAnnouncements()` called from `runLiveTUI`. Existing legacy tests assert cursor Y == 8 in idle state and depend on the empty-queue default.
8. Catalog IDs are stable but not cryptographic.

## Cleanup

- Worktree `/private/var/folders/.../openagy-announcement-cards/` removed (`git worktree remove --force`).
- Branch `feature/announcement-cards` deleted (`git branch -D`).
- main is now `f29b662`.
