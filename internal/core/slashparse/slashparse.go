// Package slashparse owns the slash-command grammar.
//
// The tree carried the grammar in three places: the interactive host
// parsed "/cmd args" with one rule, the print mode parsed it with
// another, and the `/plugin` tail was parsed with a third spelling of
// the first rule. The three copies also carried two different trims.
// This package is the one owner.
//
// Two rules live here, and they are NOT the same rule:
//
//   - Rule A — the interactive rule. The leading "/" is required; the
//     separator set is space, tab and newline; the tail is trimmed of
//     spaces and tabs (Trim).
//
//   - Rule B — the print-mode rule. The leading "/" is optional; the
//     separator set is every Unicode whitespace rune (the set
//     strings.Fields uses); the tail is trimmed of every Unicode
//     whitespace rune (TrimUnicode).
//
// The rules differ observably. "/cmd\vargs" is ("cmd\vargs", "") under
// rule A and ("cmd", "args") under rule B, and "/cmd\rargs" and
// "/cmd\fargs" differ the same way. A single rule would have been a
// behaviour change in whichever surface moved, so the package carries
// both rules as two named functions and keeps them adjacent: a future
// unification — when the reference's own behaviour for the exotic
// separators is recovered — is a one-place change.
//
// The print-mode dispatcher refuses a prompt that does not start with
// "/" before it parses it, so rule B's optional leading "/" is not
// reachable in production. Rule B's separator set is: "/help\vfoo"
// dispatches the /help command under rule B and is an unknown command
// under rule A.
//
// Every function here is a byte-preserving copy of the implementation it
// replaces. The separator sets are compared byte by byte, not by rune,
// so a multi-byte separator is never split in the middle.
package slashparse

import "strings"

// Trim trims leading and trailing ASCII space and tab characters.
//
// This is rule A's trim. It is deliberately not strings.TrimSpace: a
// newline, a vertical tab or a form feed inside the tail survives it.
func Trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// TrimUnicode trims leading and trailing Unicode whitespace.
//
// This is rule B's trim: the same rule strings.TrimSpace applies.
func TrimUnicode(s string) string {
	return strings.TrimSpace(s)
}

// Split parses "/cmd args" into ("cmd", "args") under rule A.
//
// The leading "/" is required: a buffer that does not start with one
// yields ("", ""). The command is what follows the "/" up to the first
// separator, and the arguments are what follows the separator with
// spaces and tabs trimmed off both ends (Trim). A buffer with no
// separator yields the whole tail as the command and no arguments.
//
// The command is *not* trimmed: "/cmd " yields ("cmd", "") and
// "/ cmd" yields ("", "cmd").
func Split(buf string) (string, string) {
	if buf == "" || buf[0] != '/' {
		return "", ""
	}
	return SplitVerb(buf[1:])
}

// SplitVerb splits "verb rest" into ("verb", "rest") under rule A's
// scan.
//
// This is the `/plugin` shape: the argument tail of a slash command,
// which carries a verb and the rest of the line. The separator set is
// space, tab and newline; the verb is what precedes the first
// separator and the rest is what follows it with spaces and tabs
// trimmed off both ends (Trim).
//
// A tail with no separator yields the whole tail as the verb and an
// empty rest. The verb is whatever precedes the first separator, so it
// never carries a separator itself.
func SplitVerb(args string) (string, string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case ' ', '\t', '\n':
			return args[:i], Trim(args[i+1:])
		}
	}
	return args, ""
}

// SplitFields parses "cmd args" into ("cmd", "args") under rule B.
//
// The leading "/" is optional; the separator set is every Unicode
// whitespace rune; the arguments are trimmed of every Unicode
// whitespace rune (TrimUnicode).
//
// The command is the first field, and the arguments are the remainder
// of the buffer after that field with the Unicode whitespace trimmed
// off both ends. A buffer with no field yields ("", "").
func SplitFields(buf string) (string, string) {
	buf = strings.TrimPrefix(buf, "/")
	parts := strings.Fields(buf)
	if len(parts) == 0 {
		return "", ""
	}
	return parts[0], TrimUnicode(strings.TrimPrefix(buf, parts[0]))
}

// ParseLeadingCommands parses multiple consecutive leading slash commands from buf.
// It returns the slice of command names (without the leading '/') and
// the remaining task text with leading and trailing whitespace trimmed.
// Consecutive slash commands are separated by whitespace.
// Parsing stops at the first non-slash token, and the remainder is returned as task.
func ParseLeadingCommands(buf string) (commands []string, task string) {
	cur := strings.TrimSpace(buf)
	for strings.HasPrefix(cur, "/") {
		afterSlash := cur[1:]
		if afterSlash == "" || afterSlash[0] == '/' {
			break
		}
		end := 0
		for end < len(afterSlash) {
			c := afterSlash[end]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				break
			}
			end++
		}
		cmd := afterSlash[:end]
		if cmd == "" {
			break
		}
		commands = append(commands, cmd)
		cur = strings.TrimSpace(afterSlash[end:])
	}
	task = cur
	return commands, task
}
