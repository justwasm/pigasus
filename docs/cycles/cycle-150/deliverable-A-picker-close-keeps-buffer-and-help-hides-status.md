# Cycle 150 — Picker Esc keeps buffer; help overlay hides status row

## Why

Two byte-level fidelity gaps fixed in a single small cycle:

1. **Picker Esc** — the v1.3.6 reference's picker overlay treats Esc
   as a *dismiss*, not a *commit + clear*. After Esc returns the
   prompt area to its previous state with the typed text intact
   (e.g. typing `/changelog` then Esc leaves the prompt showing
   `/changelog`), so the user can edit and resubmit. The clone
   was resetting the input alongside the picker, which wiped the
   typed text and produced a divergent picker-closed capture.

2. **Help overlay suppresses status row** — when `/help` owns the
   screen, the reference's captures show only the panel body and
   keyboard hint; the bottom-right `Gemini 3.8 Flash · high`
   label never appears. The clone was rendering the model label
   because the cycle-84 code path set `leftPlain = ""` /
   `left = ""` but left `right` populated. That comment claimed
   the ref showed "only the right model label"; capturing the
   ref binary against `/help` shows that's a misreading — the
   model label is *also* suppressed.

## Captured evidence

Picker closed state, ref binary (saved at `/tmp/agycmp/hotpath/picker/ref.002-closed.txt`):

```
────────────...
> /                                    ← buffer text preserved
────────────...
                                                                              Gemini 3.8 Flash · high
```

Help general overlay, ref binary (`/tmp/agycmp/hotpath/help_tabs/ref.000-general.txt`):

```
Keyboard: ↑/↓ Navigate  ←/→ Switch View  esc Close
                                        ← blank line, NO model label here
```

## What changes

### `cmd/wagyu/tui_v135.go::Update` (Esc branch)

Removed `m.input.Reset()` after `m.picker.Reset()` (lines 1266–1268
→ now 1266–1274 with a cycle-150 comment block). The picker reset
remains so the overlay closes; the input buffer is left untouched
so the typed text stays in the prompt area.

### `cmd/wagyu/tui_v135.go::View` (status row draw)

The status row draw at line 1869–1872 is now gated on `!m.helpMode`.
Cycle 150 comment explains the discrepancy with the cycle-84 record.

## What does NOT change in cycle 150

- The slash-command-echo removal from cycle 149 still applies —
  the picker opening (no Enter) doesn't echo, and closing the
  picker via Esc doesn't echo either.
- The picker overlay's filtering, navigation, and selection logic
  are unchanged.
- Cycle 148's `(Failed)` suffix on resumed tool calls is unaffected.
- The Ctrl+C-twice-to-exit behaviour from cycle 144 still works.

## Hard constraints / 存疑 / limitations

- 【推测】The picker-Esc-keeps-buffer behaviour matches what the user
  *intuitively expects* (Esc is "cancel this attempt", not
  "commit and clear"). The exact behavioural match against the
  v1.3.6 binary was confirmed by capturing ref.002-closed.txt
  with `/changelog` typed.
- 【推测】The help-suppresses-status-row behaviour was inferred from
  the byte-exact captures at `/tmp/agycmp/hotpath/help_tabs/`. The
  cycle-84 comment claimed the opposite; capturing made it clear.
