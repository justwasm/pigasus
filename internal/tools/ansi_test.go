package tools

import "testing"

func TestStripANSI(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello\n", "hello\n"},
		{"empty", "", ""},
		// The live 1.4.2 probe: SGR colour is dropped.
		{"sgr", "\x1b[31mRED\x1b[0m\n", "RED\n"},
		{"sgr-bold", "\x1b[1mBOLD\x1b[0m", "BOLD"},
		{"cursor-move", "a\x1b[2Jb", "ab"},
		{"csi-with-params", "\x1b[38;5;196mX\x1b[m", "X"},
		{"csi-private", "\x1b[?25lY\x1b[?25h", "Y"},
		// OSC is terminated by BEL …
		{"osc-bel", "\x1b]0;title\x07Z", "Z"},
		// … or by ST.
		{"osc-st", "\x1b]8;;http://x\x1b\\Z", "Z"},
		{"charset-designator", "\x1b(Bplain", "plain"},
		{"two-char-escape", "a\x1b=b", "ab"},
		{"lone-escape-at-end", "tail\x1b", "tail"},
		{"truncated-csi-at-end", "tail\x1b[3", "tail"},
		// CR and TAB are data, not escapes — the reference passes them through.
		{"cr-and-tab", "a\rY\ttab\n", "a\rY\ttab\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripANSI(tc.in); got != tc.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// normalizeCommandOutput = stripANSI + naive LF -> CRLF. The expectations are
// the reference's own captured Output strings (一手, docs/cycles/cycle-277).
func TestNormalizeCommandOutput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain-lf", "a\nb\n", "a\r\nb\r\n"},
		// `printf "a\r\nb\n"` -> "a\r\r\nb\r\n": no CR awareness.
		{"existing-crlf", "a\r\nb\n", "a\r\r\nb\r\n"},
		// `printf "x\rY\n"` -> "x\rY\r\n": a bare CR is passed through.
		{"bare-cr", "x\rY\n", "x\rY\r\n"},
		// `printf "\033[31mRED\033[0m\n"` -> "RED\r\n".
		{"ansi-then-crlf", "\x1b[31mRED\x1b[0m\n", "RED\r\n"},
		{"stderr-and-stdout-merge", "TOERR\ntab\there\n", "TOERR\r\ntab\there\r\n"},
		{"empty", "", ""},
		// A final line without a newline gets none: the formatter's own "\n"
		// supplies the terminator, exactly as in the reference probe.
		{"no-trailing-newline", "no-newline-tail", "no-newline-tail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeCommandOutput(tc.in); got != tc.want {
				t.Errorf("normalizeCommandOutput(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
