# Cycle 202 — an open panel consumes the page-scroll keys

## TL;DR

Cycle 201 gave the viewers (`/changelog`, `/usage`) pgup/pgdn/ctrl+home/
ctrl+end, but `tui_run.go` ran the transcript scroll bindings *before* the panel
branch. In the default (alt-screen) mode those keys were therefore consumed by
`handleScrollKey`, so the panel reacted only to ↑/↓ and viewer scrolling worked
only under `--no-alt-screen`.

Cycle 185's stated contract is that an open panel "owns every key until it is
dismissed"; the panel branch now precedes the scroll branch, so the viewers
scroll in the normal mode too.

## Files

| File | Δ |
|---|---|
| `cmd/wagyu/tui_run.go` | +11/−7 (the two blocks reordered) |

## Verified (tmux, default alt-screen, 200×40)

`/changelog`: `(1–17 of 1238)` → PageDown `(17–33)` → ctrl+end
`(1059–1075)` → ctrl+home `(1–17)`.
