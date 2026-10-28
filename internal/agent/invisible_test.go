package agent

import (
	"strings"
	"testing"
)

// 【一手】 agy 1.4.9: "Fixed invisible U+FFF9 and U+FFFA characters
// reaching the terminal around wrapped code in agent replies, which
// made some terminals and multiplexers draw boxes or leave stale
// characters on screen."

const (
	anchorRune    = "\ufff9"
	separatorRune = "\ufffa"
)

// TestSanitizeText_DropsTheInvisibleCharacters pins that the two
// interlinear-annotation characters never survive the one-shot
// sanitiser.
func TestSanitizeText_DropsTheInvisibleCharacters(t *testing.T) {
	in := "before " + anchorRune + separatorRune + " after"
	got := SanitizeText(in)
	if strings.ContainsRune(got, '\ufff9') || strings.ContainsRune(got, '\ufffa') {
		t.Errorf("SanitizeText(%q) = %q, want no invisible characters", in, got)
	}
	if got != "before  after" {
		t.Errorf("SanitizeText(%q) = %q, want %q", in, got, "before  after")
	}
}

// TestSanitizer_DropsTheInvisibleCharacters pins the streaming path:
// the characters are dropped as they arrive.
func TestSanitizer_DropsTheInvisibleCharacters(t *testing.T) {
	s := NewSanitizer()
	out := s.Push("code " + anchorRune + "block" + separatorRune + " end")
	out += s.Finish()
	if strings.ContainsRune(out, '\ufff9') || strings.ContainsRune(out, '\ufffa') {
		t.Errorf("the stream carried an invisible character: %q", out)
	}
	if out != "code block end" {
		t.Errorf("the stream = %q, want %q", out, "code block end")
	}
}

// TestSanitizer_DropsAnInvisibleCharacterSplitAcrossChunks is the case
// the byte-level hold-back exists for: a chunk ends in the middle of
// the three-byte sequence, so the halves must not reach the terminal
// as two broken runes.
func TestSanitizer_DropsAnInvisibleCharacterSplitAcrossChunks(t *testing.T) {
	for _, split := range []int{1, 2} {
		s := NewSanitizer()
		full := "a" + anchorRune + "b"
		out := s.Push(full[:1+split])
		out += s.Push(full[1+split:])
		out += s.Finish()
		if out != "ab" {
			t.Errorf("split at %d: the stream = %q, want %q", split, out, "ab")
		}
	}
}

// TestSanitizer_KeepsTheBytesAroundAnInvisibleCharacter pins that only
// the two characters are dropped: the rest of the text, including a
// lone EF or BF that is not part of one of them, survives.
func TestSanitizer_KeepsTheBytesAroundAnInvisibleCharacter(t *testing.T) {
	s := NewSanitizer()
	out := s.Push("x\xefy\xbfz")
	out += s.Finish()
	if out != "x\xefy\xbfz" {
		t.Errorf("the stream = %q, want %q", out, "x\xefy\xbfz")
	}
}

// TestStripInvisible_HoldsTheTail pins the hold-back itself: a tail
// that could start one of the sequences is held, and a tail that
// cannot is not.
func TestStripInvisible_HoldsTheTail(t *testing.T) {
	cases := []struct {
		in   string
		held string
		want string
	}{
		{"abc", "", "abc"},
		{"abc\xef", "\xef", "abc"},
		{"abc\xef\xbf", "\xef\xbf", "abc"},
		{"abc\xbf", "", "abc\xbf"},
		{"abc\xef\xbf\xb9", "", "abc"},
		{"abc\xef\xbf\xba", "", "abc"},
	}
	for _, tc := range cases {
		got, held := stripInvisible(tc.in)
		if held != tc.held {
			t.Errorf("stripInvisible(%q) held %q, want %q", tc.in, held, tc.held)
		}
		if got != tc.want {
			t.Errorf("stripInvisible(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
