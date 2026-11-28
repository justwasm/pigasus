package tui

// Guard for the composed-command wire shape.
//
// The grammar itself is pinned in internal/core/slashparse (compose_test.go,
// one case per capture). This file pins the app-tier half — that the TUI
// reaches the scanner at all, and that the envelope it builds matches a real
// capture byte for byte once the timestamp is normalised.
//
// The two fixtures beside backend's capture_*_1_5_18.txt files are verbatim
// <USER_REQUEST> blocks from live agy 1.5.18 requests; the inputs below are
// what was typed to produce them.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var captureTimestamp = regexp.MustCompile(`The current local time is: [^\n]*`)

// readComposedCapture loads a captured request body and normalises the only
// field that cannot match: the local-time line.
func readComposedCapture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "core", "backend", "testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return captureTimestamp.ReplaceAllString(string(b), "The current local time is: <TS>")
}

// composedTurnWire runs one submitted line through the host and returns the
// request's user content, timestamp-normalised and with the settings block
// (which the reference sends only on a fresh session) removed.
func composedTurnWire(t *testing.T, line string) string {
	t.Helper()
	m := newSlashModel(t)
	// A line that starts with "/" is the dispatcher's; anything else reaches
	// the composed path through the submit hook, exactly as a typed prompt
	// does.
	var handled bool
	if strings.HasPrefix(line, "/") {
		handled, _ = m.dispatchSlashLine(line)
	} else {
		handled, _ = m.composeOrdinaryPrompt(line)
	}
	if !handled {
		t.Fatalf("%q was not handled", line)
	}
	wire := lastUserWire(m)
	if i := strings.Index(wire, "\n<USER_SETTINGS_CHANGE>"); i >= 0 {
		wire = wire[:i]
	}
	return captureTimestamp.ReplaceAllString(wire, "The current local time is: <TS>")
}

// TestComposedCommands_MatchCaptures pins the whole shape for two stacks the
// reference was captured doing.
func TestComposedCommands_MatchCaptures(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		capFi string
	}{
		{
			name:  "three expandable commands",
			line:  "/schedule /goal /learn z",
			capFi: "capture_composed_schedule_goal_learn_1_5_18.txt",
		},
		{
			name:  "verbatim command breaks the run",
			line:  "/plan /goal /learn x",
			capFi: "capture_composed_plan_goal_learn_1_5_18.txt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := readComposedCapture(t, tc.capFi)
			got := composedTurnWire(t, tc.line)
			if got == want {
				return
			}
			if len(got) != len(want) {
				t.Fatalf("length = %d, want %d", len(got), len(want))
			}
			n := len(got)
			for i := 0; i < n; i++ {
				if got[i] != want[i] {
					lo := i - 60
					if lo < 0 {
						lo = 0
					}
					hi := i + 60
					if hi > n {
						hi = n
					}
					t.Fatalf("first difference at byte %d:\n got %q\nwant %q", i, got[lo:hi], want[lo:hi])
				}
			}
			t.Fatal("equal length, no differing byte")
		})
	}
}

// TestComposedCommands_EachExpandsToItsOwnBlock pins the property that made
// this change necessary: stacked commands produce one block each, in order,
// rather than one concatenated instruction.
func TestComposedCommands_EachExpandsToItsOwnBlock(t *testing.T) {
	wire := composedTurnWire(t, "/schedule /goal /learn z")

	// The order is the reference's: the order the commands appear.
	var positions []int
	for _, header := range []string{
		"/schedule is a [Slash Command]:",
		"/goal is a [Slash Command]:",
		"/learn is a [Slash Command]:",
	} {
		idx := strings.Index(wire, header)
		if idx < 0 {
			t.Fatalf("missing %q in wire:\n%s", header, wire)
		}
		positions = append(positions, idx)
	}
	for i := 1; i < len(positions); i++ {
		if positions[i] < positions[i-1] {
			t.Errorf("blocks are out of order: %v", positions)
		}
	}
}

// TestComposedCommand_VerbatimCommandsStayOutOfTheWire pins that /plan and
// /boost contribute no block: the reference sends them as text and never
// expands them.
func TestComposedCommand_VerbatimCommandsStayOutOfTheWire(t *testing.T) {
	wire := composedTurnWire(t, "/plan /goal /learn x")
	for _, forbidden := range []string{
		"/plan is a [Slash Command]:",
		"/boost is a [Slash Command]:",
		"<PLAN>",
		"<ORCHESTRATOR>",
	} {
		if strings.Contains(wire, forbidden) {
			t.Errorf("wire carries %q, which the reference never sends:\n%s", forbidden, wire)
		}
	}
	if !strings.Contains(wire, "/learn is a [Slash Command]:") {
		t.Errorf("wire is missing the one expandable command (/learn):\n%s", wire)
	}
}

// TestComposedCommand_MidTextCommandIsExpanded pins the case the leading-run
// parser used to drop silently: a command in the middle of ordinary text.
//
// The reference appends the command name to the text and expands it — the
// quirk is reproduced deliberately (see slashparse/compose.go), and this test
// exists so nobody "tidies" it away without noticing it is captured
// behaviour.
func TestComposedCommand_MidTextCommandIsExpanded(t *testing.T) {
	wire := composedTurnWire(t, "please /learn this")

	if want := "<USER_REQUEST>\nplease /learn this/learn\n</USER_REQUEST>"; !strings.HasPrefix(wire, want) {
		t.Fatalf("wire does not start with the reference's rewritten turn\ngot  %q\nwant %q", headOfLine(wire), want)
	}
	if !strings.Contains(wire, "/learn is a [Slash Command]:") {
		t.Errorf("mid-text /learn was not expanded:\n%s", wire)
	}
}

func headOfLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestComposedCommand_PlainPromptIsUntouched pins the fall-through: a prompt
// with no expandable command must not take the composed path.
func TestComposedCommand_PlainPromptIsUntouched(t *testing.T) {
	m := newSlashModel(t)
	if handled, _ := m.composeOrdinaryPrompt("summarise the retry logic"); handled {
		t.Fatal("a plain prompt was treated as a composed command")
	}
	// And a non-expandable command alone is the handler's business, not this
	// path's — it must not be swallowed here.
	m2 := newSlashModel(t)
	if handled, _ := m2.composeSlashTurn("/help"); handled {
		t.Fatal("/help was treated as a composed command")
	}
}
