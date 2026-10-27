package lsserver

import (
	"strings"
	"testing"
)

// argCase is one argument vector and what the reference made of it.
type argCase struct {
	name      string
	args      []string
	help      bool
	pattern   string
	complaint string
}

// referenceArgCases lists the argument vectors and the reference's
// answers to them.
//
// 【一手】 every case was observed against the reference binary
// (out/langserver/flags.txt).
func referenceArgCases() []argCase {
	return append(helpArgCases(), complaintArgCases()...)
}

// helpArgCases lists the argument vectors that ask for help.
func helpArgCases() []argCase {
	return []argCase{
		{
			name: "the help asks for the help",
			args: []string{"--help"},
			help: true,
		},
		{
			name:    "the argument behind the help is a pattern",
			args:    []string{"--help", "port"},
			help:    true,
			pattern: "port",
		},
		{
			name: "the help wins over an unknown flag",
			args: []string{"--bogus1", "--help"},
			help: true,
		},
	}
}

// complaintArgCases lists the argument vectors the reference
// complains about.
func complaintArgCases() []argCase {
	return []argCase{
		{
			name:      "an unknown flag is named",
			args:      []string{"--bogus1", "--bogus2"},
			complaint: "flags provided but not defined: -bogus1 -bogus2",
		},
		{
			name:      "a value that does not parse wins",
			args:      []string{"-cdp_port=notanumber"},
			complaint: `invalid value "notanumber" for flag -cdp_port: strconv.ParseInt: parsing "notanumber": invalid syntax`,
		},
		{
			name:      "a boolean that does not parse",
			args:      []string{"-standalone=maybe"},
			complaint: `invalid boolean value "maybe" for  -standalone: strconv.ParseBool: parsing "maybe": invalid syntax`,
		},
		{
			name:      "a flag without its argument",
			args:      []string{"-cdp_port"},
			complaint: "flag needs an argument: -cdp_port",
		},
		{
			name:      "the next argument is the value even when it looks like a flag",
			args:      []string{"-cdp_port", "--stamp"},
			complaint: `invalid value "--stamp" for flag -cdp_port: strconv.ParseInt: parsing "--stamp": invalid syntax`,
		},
		{
			name: "a boolean takes no argument",
			args: []string{"--stamp"},
		},
		{
			name: "a positional argument stops the scan",
			args: []string{"foo", "--bogus"},
		},
		{
			name: "the terminator stops the scan",
			args: []string{"--", "--bogus"},
		},
	}
}

// TestInspectArgsFollowsTheReference checks the argument rules the
// reference's parser has and Go's `flag` package does not.
func TestInspectArgsFollowsTheReference(t *testing.T) {
	fs, _ := newFlagSet(t)
	for _, c := range referenceArgCases() {
		t.Run(c.name, func(t *testing.T) {
			got := InspectArgs(fs, c.args)
			if got.Help != c.help {
				t.Errorf("Help = %v, want %v", got.Help, c.help)
			}
			if got.Pattern != c.pattern {
				t.Errorf("Pattern = %q, want %q", got.Pattern, c.pattern)
			}
			if got.Complaint != c.complaint {
				t.Errorf("Complaint = %q, want %q", got.Complaint, c.complaint)
			}
		})
	}
}

// TestPrintUsagePattern checks the reference's "help for one flag"
// mode and its complaint about a pattern that matches nothing.
func TestPrintUsagePattern(t *testing.T) {
	fs, _ := newFlagSet(t)
	var out strings.Builder
	PrintUsage(fs, &out, "cdp_port")
	if !strings.Contains(out.String(), "  -cdp_port=9222: Port for Chrome DevTools Protocol\n") {
		t.Errorf("the pattern answer does not carry the flag:\n%s", out.String())
	}
	if strings.Contains(out.String(), "-http_server_port") {
		t.Error("the pattern answer carries a flag that does not match")
	}
	out.Reset()
	PrintUsage(fs, &out, "CDP_PORT")
	if out.String() != "flag: no flags match help pattern \"CDP_PORT\"\n" {
		t.Errorf("complaint = %q", out.String())
	}
}

// TestPrintUsagePatternMatchesEveryFlagWithIt checks that the match
// is a case-sensitive substring.
func TestPrintUsagePatternMatchesEveryFlagWithIt(t *testing.T) {
	fs, _ := newFlagSet(t)
	var out strings.Builder
	PrintUsage(fs, &out, "port")
	for _, want := range []string{"-cdp_port", "-http_server_port", "-https_server_port", "-lsp_port"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the answer does not carry %q", want)
		}
	}
}
