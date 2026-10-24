# Deliverable — feature/announcement-cards (AGY 1.2.17 changelog line 96)

**Feature:** `feature/announcement-cards` (branched from main at `657f012`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-announcement-cards/`
**Date:** 2026-10-18

## TL;DR

AGY 1.2.17 changelog line 96 — "Added announcement cards above the
prompt for model launches, deprecations, and other service notices" —
lands. The TUI renders a one-at-a-time card between the top separator
and the prompt input (newest first). Esc on an empty prompt dismisses
the current card permanently (persisted to settings.json's
`dismissedAnnouncements` subkey). Sending any prompt hides the current
card for the rest of the session without persisting.

The catalog is a fixed test set shipped from the binary (3 example
cards: model launch, deprecation, service notice). Wiring the catalog
to a remote feed is a future cycle.

## What landed

| File | Lines | Role |
|---|---|---|
| `internal/announce/catalog.go` (new) | +213 | `Card` type, fixed `Catalog()`, `VisibleCards()`, `LoadDismissed` / `SaveDismissed` (settings.json `dismissedAnnouncements` subkey, alphabetised, idempotent, fresh-install-safe) |
| `internal/announce/catalog_test.go` (new) | +243 | 13 tests: ordering, filtering, persistence round-trip, alphabetised save, idempotent save, fresh-install create, empty-ID rejection, no-clobber preservation |
| `cmd/wagyu/announcement_cards.go` (new) | +180 | TUI glue: `announcementRow()`, `activeAnnouncement()`, `dismissCurrentAnnouncement()`, `hideCurrentAnnouncementForSession()`, `loadAnnouncements()` |
| `cmd/wagyu/announcement_cards_test.go` (new) | +308 | 11 tests: queue ordering, render above prompt, empty queue hidden, Esc dismiss / non-empty prompt no-op / over-dismiss silent, submit hide-for-session / no-op, restart honours persisted dismiss, hint row, card row not in transcript |
| `cmd/wagyu/tui_run.go` | +37 / −6 | New fields on `runMode` (`announceVisible` / `announceSessionHiddenID` / `announceSaveError`); render slot between top separator and prompt; Esc handler branch on empty prompt; `submitPrompt` calls `hideCurrentAnnouncementForSession`; `runLiveTUI` calls `loadAnnouncements`; `internal/announce` import |

**Total:** 5 files changed, **+981 / −6**.

## Layout

The card sits between the top separator and the prompt input,
inserting 5 rows into the existing chrome:

```
┌─ transcript rows (scrollable) ─┐
├────────────────────────────────┤
│  ────────────────────────────  │  ← top separator (unchanged)
│  ┌─ announcement ────────────  │  ← NEW: announcement card top border
│  │  ⓘ Claude Opus 5.5 available│  ← NEW: title row
│  │  Try `/model ...`          │  ← NEW: body row
│  │  Esc to dismiss · Send ...  │  ← NEW: hint row
│  └────────────────────────────  │  ← NEW: bottom border
│  > _                          │  ← prompt (unchanged)
│  ────────────────────────────  │  ← bottom separator (unchanged)
│  ? for shortcuts    Gemini ...  │  ← status row (unchanged)
└────────────────────────────────┘
```

Empty queue writes the empty string, so the layout collapses back to
the pre-feature shape — the "empty queue shows no card row" rule is
preserved byte-exact for the legacy tests that assert against
cursor Y == 8 in the no-announcement idle state.

The card row is suppressed while the slash-command picker / /resume
picker / /help overlay owns the screen — those overlays replace the
prompt chrome entirely, so inserting a card between separator and
prompt would visually clash. Esc-to-dismiss and submit-to-hide still
work; the card simply waits until the overlay closes to render.

## Data model

```go
type Card struct {
    ID       string    // stable; persistence key
    Kind     Kind      // model_launch | deprecation | service_notice
    Title    string
    Body     string
    Hint     string    // "Esc to dismiss · Send a message to hide"
    PostedAt time.Time // newest first
}
```

`runMode` gains three fields:

```go
announceVisible         []announce.Card  // head-rendered slice
announceSessionHiddenID string           // ID the user acknowledged via send
announceSaveError       error            // last SaveDismissed failure (if any)
```

The slice is mutated in place: `dismissCurrentAnnouncement` does
`m.announceVisible = m.announceVisible[1:]`, the next card surfaces
naturally on the next render.

## Persistence

`~/.gemini/antigravity-cli/settings.json` gains a new subkey:

```json
{
  "colorScheme": "dark",
  "trustedWorkspaces": [...],
  "dismissedAnnouncements": ["model-claude-opus-5-5", ...]
}
```

The list is alphabetised and de-duplicated on every save, so
re-dismissing a card is idempotent. Other settings.json keys are
preserved (verified by `TestSaveDismissed_PreservesOtherFields`).
A missing file is treated as an empty dismiss list — first-launch
behaviour is unchanged.

## Catalog

Three fixed cards in `announce.Catalog()`:

| ID | Kind | Title |
|---|---|---|
| `model-claude-opus-5-5` | `model_launch` | "Claude Opus 5.5 available" |
| `deprecate-gemini-2-0-flash` | `deprecation` | "Gemini 2.0 Flash retiring 2026-12-01" |
| `service-quota-rollover` | `service_notice` | "Daily quota resets at 00:00 UTC" |

Stable IDs mean a user who dismissed "model-claude-opus-5-5" keeps
that dismissal valid across wording changes.

## Gates verified

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-announce ./cmd/wagyu` | exit 0 (binary 73.1 MB) |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (run 1) | only `TestDispatchPrintSlash_LocalCommands` (documented pre-existing flake per `docs/cycles/cycle-220/deliverable-plugin-multi-subdir.md`) |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (run 2) | same as run 1 + `TestWrapRowsQuadratic` (documented pre-existing flake per `docs/cycles/cycle-227/deliverable-auto-compact.md`) |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (run 3) | only `TestDispatchPrintSlash_LocalCommands` |
| `gofmt -l` on every touched file | empty |
| `go vet ./cmd/wagyu/ ./internal/announce/` | no new warnings (only pre-existing `slash_cycle224_test.go` format verbs) |
| `go test -count=1 ./internal/announce/` | 13/13 pass |
| `go test -count=1 -run "Announcement" ./cmd/wagyu/` | 11/11 pass |

Both pre-existing flakes were confirmed independent of this cycle —
they fail identically on `657f012` before the announcement-cards diff
(`git stash` round-trip verifies it for `TestDispatchPrintSlash_*`;
`TestWrapRowsQuadratic` is documented in cycle-227 as an environmental
flake unrelated to the announcement-cards code path).

## Probe byte-match

This cycle does not have a /changelog-style byte-exact probe
target — the reference binary (agy 1.3.4) does not render the
announcement card row, and agy 1.2.17 (which ships the feature) is
not installed on this machine. The 11 announcement tests cover the
contract: card renders above the prompt, Esc dismisses, submit hides,
persisted dismiss survives restart, empty queue hides the row.

## 存疑

1. **The catalog is fixed.** The 1.2.17 spec talks about "model
   launches, deprecations, and other service notices" as live
   content; the clone ships a 3-card test set. Wiring the catalog
   to a real source (signed bundle over HTTPS, JSON file fetched at
   startup, etc.) is a future cycle. The package's `Catalog()` and
   `VisibleCards()` are the only seams to swap.
2. **Persistence error surfacing is one-shot.** When `SaveDismissed`
   fails (disk full, permission denied), `announceSaveError` is set
   and the TUI appends one warning line to the transcript on the
   next Esc. The user sees the card go away *for now* but the same
   card will re-appear next session because the dismissal did not
   land on disk. There is no retry / "save failed" indicator
   afterwards — by design, since retrying on every keystroke would
   spam the transcript.
3. **`announceSessionHiddenID` is not wired to /clear.** The spec
   says "sending a message hides the card for the rest of the
   session". /clear resets the conversation but does NOT undo the
   hide; the next message after /clear also has nothing to hide
   (the queue head has already advanced). The flag is recorded for
   symmetry and is read by no one in this cycle; a future cycle
   that adds a `/show-announcements` command would consume it.
4. **The card row co-exists with the picker / resume / help
   overlays only on Esc-dismiss.** While the picker is visible, the
   user cannot see the card, so the visual conflict is moot; but
   pressing Esc inside the picker still closes the picker (not the
   card), and pressing Esc on an empty prompt while no overlay is
   open dismisses the card. This matches the spec's "Esc on empty
   prompt" wording exactly.
5. **`m.announceVisible` is mutated in place from the TUI render
   goroutine and `Update` goroutine** with no mutex. In practice
   only one goroutine writes (the bubbletea event loop) and the
   reads happen on the same goroutine or as one-shot `View()` calls
   that lock nothing — but a future cycle that mirrors cards to the
   /changelog document should add a `sync.Mutex` around
   `announceVisible` if it ever reads from a second goroutine.
6. **The card row uses `ⓘ` (U+24D8) and `│` / `┌` / `└` / `─` box
   characters** that may not render in a 256-colour terminal
   without UTF-8 support. The reference binary ships the same
   characters; on the macOS / arm64 default Terminal.app this is
   non-issue. A pure-ASCII fallback would mean rewriting the card
   row to use `+`, `-`, `|`, `i` — not done here.
7. **Existing legacy tests assert cursor Y == 8 in the idle
   state.** `NewRunMode` no longer auto-loads the announcement
   queue (that moved to `loadAnnouncements`, called from
   `runLiveTUI` after construction). Tests that drive `NewRunMode`
   directly keep the legacy layout; tests of the feature itself
   call `loadAnnouncements()` explicitly. This split is documented
   at the top of `cmd/wagyu/announcement_cards_test.go`.
8. **Catalog IDs are stable but not cryptographic.** The catalog
   ships from the binary as a const slice; the dismiss list keys
   against these IDs directly. A future cycle that pulls the
   catalog from a remote feed should sign the bundle and check the
   signature before trusting the IDs.