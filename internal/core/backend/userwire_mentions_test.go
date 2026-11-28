package backend

// Guard for the @-mention envelope the reference actually sends for a slash
// command.
//
// The clone used to prepend a command's instruction to the user's text and
// send that as the turn. The reference does something else: the turn is the
// command text verbatim, and the instruction rides in <ADDITIONAL_METADATA>
// as an @-mention expansion. The three fixtures beside this file are the
// verbatim user contents of live agy 1.5.18 requests, so the comparison below
// is against the wire, not against our own idea of it.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// cst is the zone the captures were taken in.
var cst = time.FixedZone("CST", 8*3600)

func readCapture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// TestWrapUserContentWithMentions_MatchesCaptures rebuilds each captured
// envelope from the clone's own pieces and requires byte equality.
//
// This is the test that would have caught the mistake it replaces: the clone
// was sending the right *text* in the wrong *place*, and a substring check on
// the instruction alone passes for both shapes.
func TestWrapUserContentWithMentions_MatchesCaptures(t *testing.T) {
	cases := []struct {
		name    string
		capture string
		raw     string
		command string
		body    string
		now     time.Time
	}{
		{
			name:    "learn",
			capture: "capture_learn_1_5_18.txt",
			raw:     "/learn always run make arch",
			command: "/learn",
			body:    "<LEARN>\n" + learnBodyForTest + "</LEARN>",
			now:     time.Date(2026, 11, 28, 0, 53, 2, 0, cst),
		},
		{
			name:    "goal",
			capture: "capture_goal_1_5_18.txt",
			raw:     "/goal ship the release",
			command: "/goal",
			body:    goalBodyForTest,
			now:     time.Date(2026, 11, 28, 0, 54, 44, 0, cst),
		},
		{
			name:    "grill-me",
			capture: "capture_grillme_1_5_18.txt",
			raw:     "/grill-me the retry design",
			command: "/grill-me",
			body:    "<GRILL_ME>\n" + grillMeBodyForTest + "</GRILL_ME>",
			now:     time.Date(2026, 11, 28, 0, 58, 8, 0, cst),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := readCapture(t, tc.capture)
			got := WrapUserContentWithMentions(tc.raw, []Mention{{
				Name: tc.command,
				Kind: MentionKindSlashCommand,
				Body: tc.body,
			}}, "", false, tc.now)
			if got == want {
				return
			}
			if len(got) != len(want) {
				t.Errorf("length = %d, want %d (the captured body's)", len(got), len(want))
			}
			n := len(got)
			if len(want) < n {
				n = len(want)
			}
			for i := 0; i < n; i++ {
				if got[i] != want[i] {
					t.Fatalf("first difference at byte %d:\n got %q\nwant %q",
						i, near(got, i), near(want, i))
				}
			}
			t.Fatalf("common prefix equal; got tail %q, want tail %q",
				tailOf(got, n), tailOf(want, n))
		})
	}
}

// TestWrapUserContentWithMentions_LeavesRawTurnsAlone pins the boundary: a
// turn WrapUserContent passes through untouched carries no
// <ADDITIONAL_METADATA>, so there is nowhere for an expansion to go. It must
// come back verbatim rather than grow an element the reference never sends.
func TestWrapUserContentWithMentions_LeavesRawTurnsAlone(t *testing.T) {
	const sys = "The following is a <SYSTEM_MESSAGE> not actually sent by the user."
	got := WrapUserContentWithMentions(sys, []Mention{{
		Name: "/learn", Kind: MentionKindSlashCommand, Body: "body",
	}}, "", false, time.Now())
	if got != sys {
		t.Errorf("a system message grew a mention block:\n%q", got)
	}
}

func near(s string, i int) string {
	lo := i - 40
	if lo < 0 {
		lo = 0
	}
	hi := i + 40
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}

func tailOf(s string, from int) string {
	if from >= len(s) {
		return ""
	}
	return s[from:]
}
