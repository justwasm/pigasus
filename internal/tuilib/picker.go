// picker.go — slash command picker state machine for the run mode.
//
// Lifecycle (bubble tea Update path, inside runMode):
//
//	idle             buffer doesn't start with "/"
//	picker-empty     buffer starts with "/" but filter returns 0 rows
//	                 (overlay still visible; renders "No matches" + hint row)
//	picker-shown     buffer starts with "/" with >=1 match (overlay
//	                 renders picker rows + ↓ N more + hint row)
//
// Behavior matches /tmp/agy_slash.txt and /tmp/agy_slash_add.txt:
//
//	"/a"          -> picker open, scroll offset 0, /add-dir highlighted
//	"/add"        -> picker open, /add-dir highlighted (filtered subset)
//	"/xyz"        -> picker open but empty; renders "   No matches" row
//	                 in dim 238 followed by the hint row (cycle 93).
//	Up arrow      -> previous row in filtered subset
//	Down arrow    -> next row in filtered subset
//	Tab           -> replace buffer with selected command's Name
//	Enter         -> submit buffer (as text; the "/" prefix is preserved)
//	Esc           -> (cycle 59: ignored, kept for future cancel binding)
//
// The picker state is held outside the buffer (no "/command \x00"
// sentinel inside m.buffer). It is purely a derived view of
// m.buffer + m.pickerIdx.
package tuilib

import "strings"

// PickerState is the tuilib slash command picker state.
type PickerState struct {
	// visible is true while the picker overlay should be rendered.
	// True when buffer starts with "/", regardless of whether the
	// filter returns 0 or more rows. False when the buffer doesn't
	// start with "/" (cycle 93: also false when there are no
	// matches; the overlay still renders the "No matches" row).
	visible bool
	// empty is true when visible but len(all) == 0 (cycle 93). The
	// picker overlay renderer uses this to emit "   No matches"
	// instead of the highlighted-row + ↓ N more rows.
	empty bool
	// all is the full filtered command set (recomputed when buffer changes).
	all []SlashCommand
	// idx is the currently highlighted row. -1 when all is empty.
	idx int
	// hiddenCommands is a session-level set of slash command names to
	// exclude from the picker regardless of the buffer. Cycle 264:
	// AGY 1.4.1 changelog "the /remote-control command is hidden"
	// when AGY_ADC_AUTH is in effect — the TUI consults this set so
	// the entry neither appears in the menu nor survives the
	// substring filter on "/remote". Nil means no overrides; the
	// picker behaves exactly as before.
	hiddenCommands map[string]bool
}

// Reset clears picker state. Called whenever the buffer's slash
// prefix is removed or replaced with a non-slash character.
func (p *PickerState) Reset() {
	p.visible = false
	p.empty = false
	p.all = nil
	p.idx = -1
}

// SetHiddenCommands replaces the picker's hidden-name set. Empty or
// nil arg list clears the set (the picker behaves like the cycle-83
// baseline). Names are normalised to start with "/" so callers can
// pass either "/remote-control" or "remote-control" interchangeably.
//
// Cycle 264: AGY 1.4.1 changelog "the /remote-control command is
// hidden" when ADC is in effect. The TUI calls this from NewRunMode
// based on IsBusinessAccount(); the picker consults hiddenCommands
// during Update, so callers do not need to call it again on every
// keystroke.
func (p *PickerState) SetHiddenCommands(names ...string) {
	if len(names) == 0 {
		p.hiddenCommands = nil
		return
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		if !strings.HasPrefix(n, "/") {
			n = "/" + n
		}
		set[n] = true
	}
	p.hiddenCommands = set
}

// Update re-derives the picker state from the current buffer. Returns
// true if the state changed (caller should redraw).
//
// Rules (matching v1.3.5 binary):
//   - buffer doesn't start with "/"             -> hidden
//   - buffer starts with "/" but matches no rows -> visible, empty
//     (cycle 93: ref shows "   No matches" overlay for /xyz etc.)
//   - buffer starts with "/" with matches       -> visible, populated
//   - when the filtered count drops below idx+1, clamp idx to last
//     valid row.
func (p *PickerState) Update(buffer []rune) bool {
	prev := *p
	if len(buffer) == 0 || buffer[0] != '/' {
		p.Reset()
		return p.visible != prev.visible || len(p.all) != len(prev.all)
	}
	// Buffer starts with "/". Split on whitespace to get the prefix.
	// Picker matches the prefix as a slash command substring.
	prefix := readToken(buffer)
	p.all = FilterSlashCommands(prefix)
	if len(p.hiddenCommands) > 0 && len(p.all) > 0 {
		filtered := p.all[:0:0]
		for _, c := range p.all {
			if p.hiddenCommands[c.Name] {
				continue
			}
			filtered = append(filtered, c)
		}
		p.all = filtered
	}
	p.visible = true
	if len(p.all) == 0 {
		// Cycle 93: keep the overlay visible so it can render the
		// "No matches" row + hint row (matches ref byte-for-byte).
		p.empty = true
		p.idx = -1
		return true
	}
	p.empty = false
	if p.idx < 0 {
		p.idx = 0
	} else if p.idx >= len(p.all) {
		p.idx = len(p.all) - 1
	}
	return true
}

// Selected returns the currently highlighted command. nil when picker
// is not visible.
func (p *PickerState) Selected() *SlashCommand {
	if !p.visible || p.idx < 0 || p.idx >= len(p.all) {
		return nil
	}
	return &p.all[p.idx]
}

// Visible reports whether the picker overlay should be rendered.
// Exported so cmd/wagyu's tuilib key dispatch (the same package
// that drives Up/Down) can decide whether to navigate the picker or
// the prompt history when the user presses an arrow key.
func (p *PickerState) Visible() bool { return p.visible }

// Empty reports whether the picker is visible but has no filtered
// rows (cycle 93). The overlay renderer uses this to emit the
// "   No matches" row + hint row instead of the picker rows + ↓ N
// more tail. Always false when Visible() is false.
func (p *PickerState) Empty() bool { return p.visible && len(p.all) == 0 }

// Len returns the number of filtered commands. 0 means the picker
// has no rows to display. The picker is still Visible() (cycle 93)
// when the buffer starts with "/" but no row matches; in that case
// Empty() is true.
func (p *PickerState) Len() int { return len(p.all) }

// SelectedAt returns a pointer to the i-th row in the filtered list,
// or nil when i is out of range. Cycle 60 adds this so the picker
// renderer can enumerate the visible rows without copying the slice.
func (p *PickerState) SelectedAt(i int) *SlashCommand {
	if i < 0 || i >= len(p.all) {
		return nil
	}
	return &p.all[i]
}

// Idx returns the currently highlighted row's index. -1 when the
// picker is hidden.
func (p *PickerState) Idx() int { return p.idx }

// MoveUp moves the highlight up by 1, clamping at 0. Returns true
// when the highlight actually changed. Callers should branch on this
// to skip the historyUp path when the picker is being driven.
func (p *PickerState) MoveUp() bool {
	if !p.visible || len(p.all) == 0 {
		return false
	}
	if p.idx <= 0 {
		return false
	}
	p.idx--
	return true
}

// MoveDown moves the highlight down by 1, clamping at len(all)-1.
func (p *PickerState) MoveDown() bool {
	if !p.visible || len(p.all) == 0 {
		return false
	}
	if p.idx >= len(p.all)-1 {
		return false
	}
	p.idx++
	return true
}

// SetIdx moves the highlight to the i-th row, clamping to the valid
// range. Returns true when the highlight actually changed (or when i
// was in range and the picker was visible). False when the picker is
// hidden or has no rows.
//
// Cycle 232 (picker mouse click + hover) drives SetIdx from the mouse
// handler; cmd/wagyu's picker overlay calls this so a click on row 3
// or a hover over row 0 mirrors what an Up/Down arrow would do, but
// landing on a specific row instead of stepping.
func (p *PickerState) SetIdx(i int) bool {
	if !p.visible || len(p.all) == 0 {
		return false
	}
	if i < 0 {
		i = 0
	}
	if i >= len(p.all) {
		i = len(p.all) - 1
	}
	if p.idx == i {
		return false
	}
	p.idx = i
	return true
}

// readToken returns the leading slash-prefix token from buffer (up to
// the first whitespace, or end of buffer).
func readToken(buffer []rune) string {
	end := len(buffer)
	for i, r := range buffer {
		if r == ' ' || r == '\t' || r == '\n' {
			end = i
			break
		}
	}
	return string(buffer[:end])
}
