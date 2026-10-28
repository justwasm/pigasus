package vim

// --- Normal / Visual mode --------------------------------------------

func (s *State) handleNormalCtrl(k Key) (Result, bool) {
	if k.Mod&Ctrl == 0 {
		return Result{}, false
	}
	switch {
	case k.Name == "enter" || k.Rune == 's':
		return Result{Consumed: true, Submit: true}, true
	case k.Rune == 'r':
		s.clearPending()
		s.redoOp()
		return Result{Consumed: true}, true
	case k.Rune == 'j':
		// Vim's <C-j> is a plain down-motion. Consume it here so the
		// host's insert-newline fallback never fires in Normal mode.
		s.clearPending()
		s.applyMotion(motionDown, 1, 0)
		return Result{Consumed: true}, true
	}
	return Result{}, true // other chords (ctrl+o/g/c/d/l…) belong to the host
}

func (s *State) handleNormalNav(name string) bool {
	var m motion
	switch name {
	case "left":
		m = motionLeft
	case "right":
		m = motionRight
	case "up":
		m = motionUp
	case "down":
		m = motionDown
	case "home":
		m = motionLineStart
	case "end":
		m = motionLineEnd
	default:
		return false
	}
	s.clearPending()
	s.applyMotion(m, 1, 0)
	return true
}

func (s *State) handleNormal(k Key) Result {
	if res, handled := s.handleNormalCtrl(k); handled {
		return res
	}
	if k.Name == "esc" {
		s.clearPending()
		if s.Mode == Visual || s.Mode == VisualLine {
			s.Mode = Normal
		}
		s.breakUndo()
		return Result{Consumed: true}
	}
	if k.Name == "enter" {
		// vim.normal.submit itself binds only ctrl+s / ctrl+enter, so a
		// bare Enter misses that map and falls through to the global one,
		// where the reference's PromptModel submits on KeyEnter whenever
		// the mode is not Insert. Normal, Visual and Visual-Line all
		// submit; only Insert treats Enter as "newline" (see handleInsert).
		return Result{Consumed: true, Submit: true}
	}
	if s.handleNormalNav(k.Name) {
		return Result{Consumed: true}
	}
	if k.Rune == 0 {
		return Result{}
	}
	return s.handleNormalRune(k.Rune)
}

// handleNormalRune feeds one printable rune into the Normal-mode key
// accumulator.
//
// A pending operator that was carried over from an earlier prompt may
// only *complete*: it never makes the engine wait for more keys. When
// the key does not complete it, the operator is dropped and the key is
// read on its own — otherwise the stale "d" would swallow the "i" the
// user presses to start typing the next prompt.
func (s *State) handleNormalRune(r rune) Result {
	carried := s.pendingCarried
	s.Pending += string(r)
	c, st := parse(s.Pending, s.Mode == Visual || s.Mode == VisualLine)
	if carried && st != statusComplete {
		s.clearPending()
		return s.handleNormalRune(r)
	}
	switch st {
	case statusIncomplete:
		return Result{Consumed: true}
	case statusInvalid:
		s.clearPending()
		return Result{Consumed: true, Bell: true}
	}
	s.clearPending()
	return s.execute(c)
}
