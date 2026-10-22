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

// normalizeCommandOutput = stripANSI + CR -> LF. The expectations are the
// reference's own captured output strings (一手, live 1.4.4 request bodies;
// docs/cycles/command-output-truncation). The reference does not rewrite LF
// to CRLF: the plain result keeps the command's line endings.
func TestNormalizeCommandOutput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain-lf", "a\nb\n", "a\nb\n"},
		// `printf 'a\r\nb\n'` -> "a\n\nb\n": the CR becomes an LF, so an
		// existing CRLF turns into a blank line.
		{"existing-crlf", "a\r\nb\n", "a\n\nb\n"},
		// `printf 'a\rb'` -> "a\nb": a bare CR is a line break.
		{"bare-cr", "x\rY\n", "x\nY\n"},
		// `printf "\033[31mRED\033[0m\n"` -> "RED\n".
		{"ansi-then-newline", "\x1b[31mRED\x1b[0m\n", "RED\n"},
		{"stderr-and-stdout-merge", "TOERR\ntab\there\n", "TOERR\ntab\there\n"},
		{"empty", "", ""},
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
