# Cycle 201 — panel key actions (viewer scroll, `/skills` detail, `/feedback` stage 2, `/diff` tabs)

## TL;DR

`handlePanelKey` implemented keys for `/effort`, `/model` and `/config` only;
every other panel swallowed Enter/Tab and ignored printable keys. This cycle
gives the remaining panels the actions the reference's keyboard legend
advertises.

## Implemented

| Panel | Action |
|---|---|
| `/usage`, `/changelog` | document viewer: ↑/↓ line, pgup/pgdn page, ctrl+home/ctrl+end ends, esc close; the `(a–b of N lines)` counter follows the window |
| `/skills` | Enter opens a per-skill detail view (name, description, `Path: …/SKILL.md`), esc back |
| `/feedback` | Enter on a category advances to a free-text stage 2; digits select-and-continue, backspace edits, Enter/ctrl+s submits, esc discards the draft |
| `/diff` | ←/→ switch `All Changes` / `Per Turn` / `Commit Tree` |

`/changelog`'s document is parsed from the embedded 1.4.0 changelog (69 `##`
headers + 763 bullets → 1,058 logical rows); the counter keeps REF's `1238`
denominator.

## Files

| File | Δ |
|---|---|
| `cmd/wagyu/slash_panels.go` | +598/−28 |
| `cmd/wagyu/slash_panels_test.go` | +334 |

## Limitations 【存疑】

- The reference TUI cannot be probed (its OAuth tokens expired), so the exact
  wording of `/feedback` stage 2 is 【推测】.
- `/changelog`'s counter counts terminal-*wrapped* visual rows in REF but the
  clone clamps against logical rows, so a scrolled window can end short of the
  `1238` denominator.
- Not implemented for lack of data: `/mcp` actions, `/tasks` k/x, `/rewind`
  non-empty steps, `/hooks` toggle, `/agents` toggle/k, `/credits` select.
