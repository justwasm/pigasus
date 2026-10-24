# Cycle 189 — /rewind empty-state panel

## Why

Cycle 184's inventory put `/rewind` in class A (REF renders a full-screen
picker; OUR printed a single text line). Cycle 187 closed `/feedback`
stage 1 on top of the cycle-185 panel framework; this cycle closes
`/rewind`'s empty state the same way.

REF's `/rewind` is a conversation-step picker: one row per turn in
non-empty sessions, and a single literal `(current)` row in the empty /
startup state. The clone previously printed `Nothing to rewind.`.
Cycle 189 ships the empty-state panel only; the non-empty version
would need a step-extraction pass over `m.session` that this cycle
does not do.

## 1. Evidence

REF v1.4.0 on an empty session
(`/tmp/agycmp/panels/ppref.rewind.during.raw.txt`):

```
Rewind Conversation

> (current)

Keyboard: ↑/↓ Navigate  enter Select  esc Cancel
```

Three behaviours for `/rewind`:

| Behaviour | Where | Notes |
|---|---|---|
| Esc closes silently | `ppref.rewind.esc.raw.txt` | same `exitNotice()==false` set as `/mcp` `/permissions` `/codesearch` `/feedback` |
| Single selectable row | empty state | the only row is `(current)` (no list) |
| `Keyboard:` legend | last row | like `/mcp`, unlike `/hooks`/`/feedback`'s dot hint |

## 2. Change

`cmd/wagyu/slash_panels.go`

- New `panelRewind` constant in `panelKind`.
- New `panelRewindLines()` method — 5 rows: bold title, blank, blue
  `> (current)` row, blank, `Keyboard:` legend.
- `panelForCommand` learns `"rewind"` → `panelRewind`.
- `panelRowCount` learns `panelRewind` → 1.
- `panelLines` switch dispatches to `panelRewindLines`.
- `exitNotice()` adds `panelRewind` to the silent-close set.
- Header comment bumped from "cycles 185 + 187" to "cycles 185 + 187 +
  189".

`cmd/wagyu/slash_panels_test.go`

- `TestPanelForCommand` extended with `"rewind"` → `panelRewind`.
- `TestPanelRewindEmptyLayout` pins the 5 rows as plain text and
  checks that the `(current)` row carries the blue codes.
- `TestRewindExitNotice` pins `panelRewind.exitNotice() == false`.

`TODO.md`

- §2.1 row for `/rewind` now struck through and marked ✅ cycle 189.
- §2.1 top note adds `✅ Cycle 189 已关闭: /rewind 空状态`.
- §4 已关闭 table adds the cycle-189 row.
- §5 P1 list removes `/rewind`.

## 3. Verification

```
go test ./cmd/wagyu/ -run 'TestPanelRewind|TestRewindExit|
                            TestPanelForCommand' -count=1
# 3 tests pass

python3 /tmp/agycmp/cmp_panels.py \
  mcp tasks effort hooks permissions agents credits codesearch artifact \
  feedback rewind
# 11/11 IDENTICAL (SGR-normalised)

python3 /tmp/agycmp/pprw_our.py   # captures OUR /rewind
python3 /tmp/agycmp/cmp_panels.py rewind   # IDENTICAL

# ppour.rewind.esc.raw.txt shows status-row → ? for shortcuts,
# no "⎿  Exited /rewind command" line
```

## 4. Not done

- **Non-empty `/rewind`** — the conversation-step picker. The clone
  stores messages flat in `m.session`; extracting "steps" (one row per
  turn with message count + age) is a separate cycle.
- **`/rewind` Enter → actual rewind.** Even with a step list, the clone
  has no rewind implementation. Enter would still need to be wired to
  truncate `m.session` to the chosen step.
- **The remaining 8 P1 panels** — `/model`, `/config`, `/plugin`,
  `/skills`, `/usage`, `/context`, `/changelog`, `/diff`.
- **B echo format / C text fixes** that still need
  `cmd/wagyu/slash_commands.go` (the concurrent agent's WIP).
