# Cycle 287 — Input field mouse selection, auto copy, and select all (Cmd+A)

User request: **Support mouse selection and auto copy in the prompt input field (matching transcript selection), and support Cmd+A to select all.**

## Summary of Changes

1. **In-App Input Field Mouse Selection**:
   - Implemented `inputOffsetAt(x, y int) int` and `setInputCursorOffset(offset int)` to map screen coordinates to byte and rune offsets within `m.input.Value()`, properly handling prompt prefixes (`> ` and `  `), display cell widths, and multi-line/multibyte runes (`ansi.StringWidth`).
   - Wired `tea.MouseClickMsg`: clicking inside the input field (`isInputY`) positions the cursor and initiates a drag (`inputSelDragging = true`).
   - Wired `tea.MouseMotionMsg`: dragging moves the cursor and live-updates the selection range `[inputSelStart, inputSelEnd]`.
   - Wired `tea.MouseReleaseMsg`: on release, if a non-empty range is selected, the selected text is automatically copied to the clipboard via OSC 52 (`m.copySelectionCmd`) and system clipboard fallback (`copyToClipboard` / `pbcopy`).
   - Out-of-bounds drag motions clamp cleanly to offset 0 (start) or `len(val)` (end).

2. **Visual Feedback (Reverse Video Highlight)**:
   - Implemented `applyInputSelectionHighlight` and `applySelectionHighlightToRow` using `selHighlightOpen` (`\x1b[0m\x1b[7m`) and `selHighlightClose` (`\x1b[0m`), matching the existing transcript selection visual styling.

3. **Select All (Cmd+A / Ctrl+Shift+A)**:
   - Handled `Cmd+A` (`key.Mod.Contains(tea.ModSuper)` or `tea.ModMeta`) and `Ctrl+Shift+A` (`tea.ModCtrl | tea.ModShift`).
   - Selects the entire buffer `[0, len(val)]`, moves cursor to the end, highlights the entire input, and auto-copies the full text to the clipboard.
   - Preserves standard readline/emacs `Ctrl+A` as `LineStart`.

4. **Selection Editing & Clipboard Integration**:
   - `Backspace` / `Delete` with an active selection deletes the selected text.
   - Typing printable text or pasting (`insertPastedText` / `Ctrl+V`) replaces the selected text.
   - `Cmd+C` explicitly copies the active input selection to the clipboard.
   - `Escape` or arrow keys deselect without clearing the input text.
   - Submitting a prompt clears any active selection.

## Verification

- `go test -v -run TestInputSelection ./cmd/wagyu`: **PASS** (14 tests covering click, drag forward, drag backward, bounds clamping, Cmd+A, Ctrl+Shift+A, Backspace, Delete, typing replacement, paste replacement, Escape, arrow keys, multiline, and UTF-8).
- `go test -v -run TestPickerMouse ./cmd/wagyu`: **PASS** (all 6 tests).
- `go test -v -run "Test_Selection|TestSelection" ./cmd/wagyu`: **PASS** (all transcript selection tests).
- `go test ./cmd/... ./internal/...`: **PASS** (all packages).
- Live PTY verification: verified reverse-video highlight during drag and OSC 52 auto-copy on mouse release.
