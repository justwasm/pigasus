package convstore

// delta_test.go — the incremental save path.
//
// SaveConversationDelta is what makes a save cost a turn instead of a
// conversation: it writes the steps that differ from the list the store
// already holds. The diff is the whole mechanism, so it is what these tests
// pin — including the case that made the naive version wrong (a streamed
// reply rewritten as it completes is a change at the *end* of the list, not
// an append).

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// deltaFixture writes a conversation with the given steps and returns its id.
func deltaFixture(t *testing.T, id string, steps []Step) {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	if err := SaveConversation(id, "/tmp/delta-ws", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
}

// loadSteps reads the conversation back the way a resume does.
func loadSteps(t *testing.T, id string) []Step {
	t.Helper()
	c, err := LoadConversation(ConversationPath(id))
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	return c.Steps
}

// TestDeltaAppendsTheNewTail is the ordinary case: a turn appended two steps
// and the delta writes exactly those.
func TestDeltaAppendsTheNewTail(t *testing.T) {
	const id = "d1d1d1d1-0000-4000-8000-000000000801"
	prev := []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "first"},
		{Idx: 1, Type: StepModelResponse, Status: 3, Text: "answer"},
	}
	deltaFixture(t, id, prev)

	next := append(append([]Step{}, prev...),
		Step{Idx: 2, Type: StepUserInput, Status: 3, Text: "second"},
		Step{Idx: 3, Type: StepModelResponse, Status: 3, Text: "second answer"},
	)
	if err := SaveConversationDelta(id, "/tmp/delta-ws", prev, next); err != nil {
		t.Fatalf("SaveConversationDelta: %v", err)
	}

	got := loadSteps(t, id)
	if len(got) != 4 {
		t.Fatalf("stored %d steps, want 4: %+v", len(got), got)
	}
	for i, want := range []string{"first", "answer", "second", "second answer"} {
		if got[i].Text != want {
			t.Errorf("step %d text = %q, want %q", i, got[i].Text, want)
		}
	}
}

// TestDeltaRewritesTheStreamedTail is the case a naive "append from len(prev)"
// gets wrong.
//
// A reply is stored while it is still streaming, so the last step of prev is
// the partial text and the last step of next is the finished text — the same
// index, different content. The delta has to rewrite that row; a diff that
// only ever appends would leave the truncated reply on disk.
func TestDeltaRewritesTheStreamedTail(t *testing.T) {
	const id = "d2d2d2d2-0000-4000-8000-000000000802"
	partial := []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "explain"},
		{Idx: 1, Type: StepModelResponse, Status: 3, Text: "The answer is"},
	}
	deltaFixture(t, id, partial)

	complete := []Step{
		partial[0],
		{Idx: 1, Type: StepModelResponse, Status: 3, Text: "The answer is 42."},
	}
	if err := SaveConversationDelta(id, "/tmp/delta-ws", partial, complete); err != nil {
		t.Fatalf("SaveConversationDelta: %v", err)
	}

	got := loadSteps(t, id)
	if len(got) != 2 {
		t.Fatalf("stored %d steps, want 2", len(got))
	}
	if got[1].Text != "The answer is 42." {
		t.Errorf("the streamed reply was not completed on disk: %q", got[1].Text)
	}
}

// TestDeltaOfAnUnchangedListWritesNothing pins the cheapest case: a save that
// finds nothing to write is a no-op, not a rewrite.
func TestDeltaOfAnUnchangedListWritesNothing(t *testing.T) {
	const id = "d3d3d3d3-0000-4000-8000-000000000803"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "one"},
		{Idx: 1, Type: StepModelResponse, Status: 3, Text: "two"},
	}
	deltaFixture(t, id, steps)

	before := fileModTime(t, ConversationPath(id))
	if err := SaveConversationDelta(id, "/tmp/delta-ws", steps, steps); err != nil {
		t.Fatalf("SaveConversationDelta: %v", err)
	}
	after := fileModTime(t, ConversationPath(id))
	if !after.Equal(before) {
		t.Error("an unchanged save rewrote the conversation file")
	}
}

// TestDeltaFallsBackWhenTheStoreHasNoFile pins the repair: a delta whose base
// the store does not have cannot diff, so it writes everything. Otherwise a
// session whose file was deleted would never come back.
func TestDeltaFallsBackWhenTheStoreHasNoFile(t *testing.T) {
	const id = "d4d4d4d4-0000-4000-8000-000000000804"
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	prev := []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "gone"},
		{Idx: 1, Type: StepModelResponse, Status: 3, Text: "from disk"},
	}
	next := append(append([]Step{}, prev...),
		Step{Idx: 2, Type: StepUserInput, Status: 3, Text: "and back"},
	)
	if err := SaveConversationDelta(id, "/tmp/delta-ws", prev, next); err != nil {
		t.Fatalf("SaveConversationDelta: %v", err)
	}

	got := loadSteps(t, id)
	if len(got) != 3 {
		t.Fatalf("stored %d steps, want 3 (the whole list): %+v", len(got), got)
	}
}

// TestChangedFrom points at the first step the delta has to write.
func TestChangedFrom(t *testing.T) {
	base := []Step{
		{Idx: 0, Type: StepUserInput, Text: "a"},
		{Idx: 1, Type: StepModelResponse, Text: "b"},
		{Idx: 2, Type: StepUserInput, Text: "c"},
	}
	cases := []struct {
		name string
		prev []Step
		next []Step
		want int
	}{
		{
			name: "nothing changed",
			prev: base,
			next: base,
			want: 3,
		},
		{
			name: "appended one",
			prev: base,
			next: append(append([]Step{}, base...), Step{Idx: 3, Type: StepModelResponse, Text: "d"}),
			want: 3,
		},
		{
			name: "last step rewritten",
			prev: base,
			next: []Step{base[0], base[1], {Idx: 2, Type: StepUserInput, Text: "c, finished"}},
			want: 2,
		},
		{
			name: "middle step rewritten",
			prev: base,
			next: []Step{base[0], {Idx: 1, Type: StepModelResponse, Text: "b!"}, base[2]},
			want: 1,
		},
		{
			name: "first step rewritten",
			prev: base,
			next: []Step{{Idx: 0, Type: StepUserInput, Text: "a!"}, base[1], base[2]},
			want: 0,
		},
		{
			name: "list shrank",
			prev: base,
			next: base[:1],
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := changedFrom(tc.prev, tc.next); got != tc.want {
				t.Errorf("changedFrom = %d, want %d", got, tc.want)
			}
		})
	}
}

// fileModTime is the conversation file's modification time.
func fileModTime(t *testing.T, path string) time.Time {
	t.Helper()
	st, err := os.Stat(filepath.Clean(path))
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return st.ModTime()
}
