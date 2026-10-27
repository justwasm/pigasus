package vim

import "testing"

// Host-facing surface: the Insert-mode editing keys, Visual paste and
// case operators, the accessors the TUI glue will call, and the
// multi-line (charwise and linewise) range cases the first two files
// did not reach.

// --- Insert-mode editing keys ----------------------------------------

func TestInsertNavigationAndDelete(t *testing.T) {
	s := newWith(t, "one two")
	feed(t, s, "A<left><left><delete><home><end><bs>")
	checkText(t, s, "one t")
	checkCursor(t, s, 0, 5)

	// Vertical motion in Insert mode keeps the desired column.
	s = newWith(t, "ab\ncd")
	feed(t, s, "i<down>X")
	checkText(t, s, "ab\nXcd")
	checkCursor(t, s, 1, 1)

	// home/end
	s = newWith(t, "abc")
	feed(t, s, "A<home>")
	checkCursor(t, s, 0, 0)
	feed(t, s, "<end>")
	checkCursor(t, s, 0, 3)
}

// --- Visual paste and case -------------------------------------------

func TestVisualPaste(t *testing.T) {
	// Yank "ab" charwise, then select "ab" and paste over it: the text is
	// unchanged and the register is preserved.
	s := newWith(t, "abcd")
	feed(t, s, "yl")
	feed(t, s, "vlp")
	checkText(t, s, "abcd")
	checkMode(t, s, Normal)
	if s.register != "ab" {
		t.Fatalf("register = %q, want ab", s.register)
	}

	// A longer register lengthens the line (the selection is replaced).
	s = newWith(t, "abcd")
	feed(t, s, "y$")
	feed(t, s, "vlp")
	checkText(t, s, "abcdcd")
}

func TestVisualCaseOperators(t *testing.T) {
	s := newWith(t, "hello\nworld")
	feed(t, s, "VjU")
	checkText(t, s, "HELLO\nWORLD")
	checkMode(t, s, Normal)

	s = newWith(t, "HELLO\nWORLD")
	feed(t, s, "Vju")
	checkText(t, s, "hello\nworld")
}

func TestVisualCaseUndo(t *testing.T) {
	// A Visual case operator must be undoable: the snapshot is the
	// pre-transform buffer, so u reverts the whole selection.
	s := newWith(t, "hello")
	feed(t, s, "vllU")
	checkText(t, s, "HELlo")
	feed(t, s, "u")
	checkText(t, s, "hello")
}

func TestNormalCaseToggle(t *testing.T) {
	s := newWith(t, "hello")
	feed(t, s, "3~")
	checkText(t, s, "HELlo")
	checkCursor(t, s, 0, 3)
}

func TestNormalCaseToggleMultiLine(t *testing.T) {
	s := newWith(t, "ab\ncd")
	feed(t, s, "~")
	checkText(t, s, "Ab\ncd")
	// Toggling the last character of a line toggles it in place.
	s = newWith(t, "ab\ncd")
	feed(t, s, "$~")
	checkText(t, s, "aB\ncd")
}

// --- Multi-line ranges ------------------------------------------------

func TestVisualCharwiseAcrossLines(t *testing.T) {
	s := newWith(t, "ab\ncd")
	feed(t, s, "vjd")
	checkText(t, s, "d")
	checkMode(t, s, Normal)
}

func TestLinewiseRangeText(t *testing.T) {
	s := newWith(t, "aa\nbb\ncc")
	feed(t, s, "2dd")
	checkText(t, s, "cc")
	if s.register != "aa\nbb\n" {
		t.Fatalf("register = %q, want %q", s.register, "aa\nbb\n")
	}
}

// --- Bell / pending ---------------------------------------------------

func TestInvalidKeyBells(t *testing.T) {
	s := newWith(t, "abc")
	res := s.Handle(Key{Rune: 'q'})
	if !res.Consumed || !res.Bell {
		t.Fatalf("q: %+v, want consumed+bell", res)
	}
	if s.Pending != "" {
		t.Fatalf("pending = %q, want empty", s.Pending)
	}
}

func TestInsertPending(t *testing.T) {
	s := newWith(t, "abc")
	if s.InsertPending() {
		t.Fatal("fresh state should have no pending sequence")
	}
	s.Handle(Key{Rune: 'd'})
	if !s.InsertPending() {
		t.Fatal("a lone d should leave the engine pending")
	}
	s.Handle(Key{Rune: 'w'})
	if s.InsertPending() {
		t.Fatal("dw should have completed the sequence")
	}
}

// --- Accessors --------------------------------------------------------

func TestAccessors(t *testing.T) {
	s := New("a\nb")
	if got := s.LineCount(); got != 2 {
		t.Fatalf("LineCount = %d, want 2", got)
	}
	if got := s.Value(); got != "a\nb" {
		t.Fatalf("Value = %q", got)
	}
	s.SetText("x\ny\nz")
	if got := s.Text(); got != "x\ny\nz" {
		t.Fatalf("Text after SetText = %q", got)
	}
	if got := s.LineCount(); got != 3 {
		t.Fatalf("LineCount after SetText = %d, want 3", got)
	}
	if !New("").isBlank() || !New("  \n\t").isBlank() || New("a").isBlank() {
		t.Fatal("isBlank classified wrong")
	}
}

func TestKeyPrintable(t *testing.T) {
	if !(Key{Rune: 'a'}).Printable() {
		t.Fatal("a rune key must be printable")
	}
	if (Key{Name: "esc"}).Printable() {
		t.Fatal("a named key must not be printable")
	}
}

// --- Insert First and the Normal-mode control keys --------------------

func TestSetInsertFirst(t *testing.T) {
	s := New("hi")
	if s.Mode != Normal {
		t.Fatalf("New starts in %v, want Normal", s.Mode)
	}
	s.SetInsertFirst(true)
	if s.Mode != Insert {
		t.Fatalf("SetInsertFirst(true) → %v, want Insert", s.Mode)
	}
	if !s.InsertFirst {
		t.Fatal("InsertFirst flag not set")
	}
	s.SetInsertFirst(false)
	if s.Mode != Normal {
		t.Fatalf("SetInsertFirst(false) → %v, want Normal", s.Mode)
	}
	// Idempotent: re-applying the same value must not move the cursor or
	// change the mode.
	s.Mode = Insert
	s.SetInsertFirst(false) // already false → no-op
	if s.Mode != Insert {
		t.Fatalf("redundant SetInsertFirst moved the mode to %v", s.Mode)
	}
}

func TestNormalEnterSubmits(t *testing.T) {
	// The vim-normal map binds only ctrl+s / ctrl+enter, so a bare Enter
	// falls through to the global binding, where the reference submits
	// whenever the mode is not Insert. Normal, Visual and Visual-Line all
	// submit; only Insert turns Enter into a newline.
	s := newWith(t, "ab\ncd")
	feed(t, s, "l")
	res := s.Handle(Key{Name: "enter"})
	if !res.Consumed || !res.Submit {
		t.Fatalf("Normal Enter: %+v, want consumed+submit", res)
	}
	checkText(t, s, "ab\ncd")
	checkCursor(t, s, 0, 1)

	for _, key := range []string{"v", "V"} {
		s = newWith(t, "ab\ncd")
		feed(t, s, key)
		if r := s.Handle(Key{Name: "enter"}); !r.Consumed || !r.Submit {
			t.Fatalf("%s-mode Enter: %+v, want consumed+submit", key, r)
		}
	}
}

func TestNormalCtrlJIsDown(t *testing.T) {
	// <C-j> is a plain down-motion in Normal mode, and must be consumed
	// so the host's insert-newline fallback never runs.
	s := newWith(t, "ab\ncd")
	feed(t, s, "<c-j>")
	checkCursor(t, s, 1, 0)
	checkText(t, s, "ab\ncd")
}

func TestNormalCtrlSSubmits(t *testing.T) {
	s := newWith(t, "hi")
	res := s.Handle(Key{Rune: 's', Mod: Ctrl})
	if !res.Consumed || !res.Submit {
		t.Fatalf("ctrl+s in Normal: %+v, want consumed+submit", res)
	}
	// An unrelated chord is left for the host.
	if res := s.Handle(Key{Rune: 'o', Mod: Ctrl}); res.Consumed {
		t.Fatalf("ctrl+o should be left to the host, got %+v", res)
	}
}
