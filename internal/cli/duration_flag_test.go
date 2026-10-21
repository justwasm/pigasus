// duration_flag_test.go — cycle 117: the duration flag's parse error.
//
// stdlib's flag package registers a duration flag through its own
// durationValue, whose Set replaces time.ParseDuration's error with the generic
// "parse error". The reference reports the underlying error verbatim:
//
//	invalid value "abc" for flag -print-timeout: time: invalid duration "abc"
//
// (stdlib: `…: parse error`). jetski therefore registers durations through its
// own durationValue, which propagates the real error.
package cli

import (
	"flag"
	"strings"
	"testing"
	"time"
)

// TestDurationFlagErrorIsVerbatim pins the wording of a bad duration value.
func TestDurationFlagErrorIsVerbatim(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(new(strings.Builder))
	var d time.Duration
	fs.Var(&durationValue{v: &d}, "print-timeout", "")

	err := fs.Parse([]string{"--print-timeout=abc"})
	if err == nil {
		t.Fatal("expected a parse error")
	}
	want := `invalid value "abc" for flag -print-timeout: time: invalid duration "abc"`
	if err.Error() != want {
		t.Errorf("error = %q\nwant   %q", err.Error(), want)
	}
}

// TestDurationFlagParses checks a valid duration still lands in the destination
// and that String() round-trips it (flagStringValue reads Value.String()).
func TestDurationFlagParses(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	var d time.Duration
	fs.Var(&durationValue{v: &d}, "print-timeout", "")

	if err := fs.Parse([]string{"--print-timeout=1m30s"}); err != nil {
		t.Fatal(err)
	}
	if d != 90*time.Second {
		t.Errorf("parsed %v, want 1m30s", d)
	}
	if got := fs.Lookup("print-timeout").Value.String(); got != "1m30s" {
		t.Errorf("Value.String() = %q, want 1m30s", got)
	}

	// The zero value is what the usage block reports as the default.
	var zero time.Duration
	if got := (durationValue{v: &zero}).String(); got != "0s" {
		t.Errorf("zero String() = %q, want 0s", got)
	}
}
