package printmode

// Guard for the composed-command envelope in print mode.
//
// `agy -p '/goal /learn do the thing'` sends the turn text verbatim plus one
// [Slash Command] block per expandable command. Captured from live 1.5.18:
// 3,055 bytes once the timestamp is normalised. The clone used to send the
// raw text, so a stacked prompt reached the model with no instruction at all.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// readCapture loads a verbatim request body captured from agy 1.5.18.
func readCapture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestPrintComposedMentions_StackedCommands(t *testing.T) {
	wire, mentions, ok := printComposedMentions("/goal /learn do the thing")
	if !ok {
		t.Fatal("a stacked prompt did not expand")
	}
	if want := "/goal /learn do the thing"; wire != want {
		t.Errorf("wire text = %q, want %q (the turn is verbatim)", wire, want)
	}
	if len(mentions) != 2 {
		t.Fatalf("mentions = %d, want 2", len(mentions))
	}
	if mentions[0].Name != "/goal" || mentions[1].Name != "/learn" {
		t.Errorf("mention order = %s, %s; want /goal then /learn", mentions[0].Name, mentions[1].Name)
	}
	// The bodies are the pinned ones: /goal bare, /learn wrapped.
	if strings.Contains(mentions[0].Body, "<LEARN>") {
		t.Error("/goal body carries the /learn wrapper")
	}
	if !strings.HasPrefix(mentions[1].Body, "<LEARN>\n") || !strings.HasSuffix(mentions[1].Body, "</LEARN>") {
		t.Errorf("/learn body is not wrapped: %q", mentions[1].Body[:40])
	}
}

// TestPrintComposedMentions_MatchesCapture rebuilds the captured envelope and
// requires byte equality once the timestamp is normalised. The fixture is the
// verbatim request body of the live `agy -p '/goal /learn do the thing'`.
func TestPrintComposedMentions_MatchesCapture(t *testing.T) {
	wire, mentions, ok := printComposedMentions("/goal /learn do the thing")
	if !ok {
		t.Fatal("did not expand")
	}
	// The timestamp line's width varies with the instant (a whole-second
	// RFC3339 is 51 bytes where a fractional one is 53), so replace it with a
	// fixed-width stand-in on BOTH sides and compare the result against the
	// capture put through the same substitution.
	const ts = "The current local time is: <TS>"
	got := backend.WrapUserContentWithMentions(wire, mentions, "", false,
		time.Date(2026, 11, 28, 2, 45, 52, 0, time.FixedZone("CST", 8*3600)))
	got = regexp.MustCompile(`The current local time is: [^\n]*`).ReplaceAllString(got, ts)

	ref := readCapture(t, "capture_composed_goal_learn_1_5_18.txt")
	ref = regexp.MustCompile(`The current local time is: [^\n]*`).ReplaceAllString(ref, ts)
	if got != ref {
		if len(got) != len(ref) {
			t.Fatalf("envelope = %d bytes, want %d (the capture's)", len(got), len(ref))
		}
		n := len(got)
		for i := 0; i < n; i++ {
			if got[i] != ref[i] {
				lo := i - 50
				if lo < 0 {
					lo = 0
				}
				t.Fatalf("first difference at byte %d:\n got %q\nwant %q", i, got[lo:minInt(i+50, n)], ref[lo:minInt(i+50, n)])
			}
		}
		t.Fatal("equal length, no differing byte")
	}
}

// TestPrintComposedMentions_VerbatimCommandsExpandNothing pins that /plan and
// /boost stay out of the envelope, and that a prompt with no expandable
// command is left alone.
func TestPrintComposedMentions_VerbatimCommandsExpandNothing(t *testing.T) {
	for _, prompt := range []string{
		"/plan refactor the auth middleware",
		"/boost analyze the retry layer",
		"/teamwork build a parser",
		"summarise the retry logic",
	} {
		wire, mentions, ok := printComposedMentions(prompt)
		if ok || len(mentions) != 0 {
			t.Errorf("%q expanded into %d mention(s), want none", prompt, len(mentions))
		}
		if wire != prompt {
			t.Errorf("%q rewritten to %q, want it untouched", prompt, wire)
		}
	}
}

// TestPrintComposedMentions_MidTextCommand pins the reference's trailing-append
// quirk in print mode too: a command in the middle of text is appended to the
// end and expanded.
func TestPrintComposedMentions_MidTextCommand(t *testing.T) {
	wire, mentions, ok := printComposedMentions("please /learn this")
	if !ok {
		t.Fatal("mid-text /learn did not expand")
	}
	if want := "please /learn this/learn"; wire != want {
		t.Errorf("wire text = %q, want %q", wire, want)
	}
	if len(mentions) != 1 || mentions[0].Name != "/learn" {
		t.Errorf("mentions = %+v, want one /learn", mentions)
	}
}
