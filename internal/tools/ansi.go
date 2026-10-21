// ANSI escape stripping for command output.
//
// Source: google3/devtools/ai/sandbox/exebox — the reference wraps the
// command's stdout/stderr in a (*stripANSI) writer whose skipEsc drops
// ESC-introduced sequences before the bytes reach the tool result. A live
// 1.4.2 turn pins the behaviour (一手, docs/cycles/cycle-277):
//
//	printf "\033[31mRED\033[0m\n"   ->   RED
//
// Everything that is not an escape sequence survives untouched: the same
// probe shows a bare CR passing through, a tab passing through and a
// 100-char line NOT being wrapped. So this is a byte filter, not a
// terminal emulator (see normalizeCommandOutput, which pairs it with the
// LF -> CRLF rewrite the reference also performs).
package tools

import "strings"

// escByte is the control byte that introduces every ANSI/VT sequence.
const escByte = 0x1b

// stripANSI removes ANSI/VT escape sequences from s.
//
// Handled introducers:
//
//	ESC [ ... final    CSI (colour, cursor, erase, SGR …)  final = 0x40..0x7e
//	ESC ] ... BEL|ST   OSC (window title, hyperlinks)
//	ESC (|)|*|+|#|% X  two-byte-payload charset / keypad designators
//	ESC X              any other two-character escape
//
// A trailing lone ESC (or a truncated sequence at the end of the buffer) is
// dropped: a command that dies mid-sequence must not leak the introducer.
func stripANSI(s string) string {
	if !containsEsc(s) {
		return s
	}
	var b = make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] != escByte {
			b = append(b, s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			break // lone ESC at the very end
		}
		switch s[i+1] {
		case '[': // CSI: parameter/intermediate bytes then one final byte
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++ // consume the final byte
			}
			i = j
		case ']': // OSC: terminated by BEL or ST (ESC \)
			j := i + 2
			for j < len(s) {
				if s[j] == 0x07 {
					j++
					break
				}
				if s[j] == escByte && j+1 < len(s) && s[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
		case '(', ')', '*', '+', '#', '%', ' ':
			// Designators whose payload is a single byte: ESC ( B, ESC # 8 …
			i += 3
			if i > len(s) {
				i = len(s)
			}
		default:
			i += 2
		}
	}
	return string(b)
}

// containsEsc is a fast path so the common (escape-free) output is returned
// without a copy or an allocation.
func containsEsc(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == escByte {
			return true
		}
	}
	return false
}

// normalizeCommandOutput turns raw command bytes into the text the
// reference reports in the run_command result's Output section.
//
// Two steps, both pinned by live 1.4.2 probes (一手, cycle 277):
//
//  1. stripANSI — colour / cursor sequences are removed.
//  2. every LF becomes CRLF. This is a *naive* rewrite, not a terminal
//     emulation: an input that already contains CRLF comes out as CR CR LF
//     (`printf "a\r\nb\n"` -> `a\r\r\nb\r\n`), a bare CR is passed through
//     unchanged (`printf "x\rY\n"` -> `x\rY\r\n`) and a 100-character line is
//     not wrapped at the pty's 80 columns.
func normalizeCommandOutput(s string) string {
	return strings.ReplaceAll(stripANSI(s), "\n", "\r\n")
}
