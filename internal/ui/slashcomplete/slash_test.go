package slashcomplete

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// fixture is a small, explicit command catalogue used across the tests
// so the package has no dependency on the built-in default catalogue or
// on a RootModel.
func fixture() []slashcommands.Command {
	return []slashcommands.Command{
		{Name: "/clear", Description: "Clear the conversation"},
		{Name: "/copy", Description: "Copy a planner response"},
		{Name: "/compact", Description: "Compact the context"},
		{Name: "/model", Description: "Pick a model"},
		{Name: "/help", Description: "Show help"},
	}
}

func names(cmds []slashcommands.Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}

func TestMatchEmptyInput(t *testing.T) {
	got := Match("", fixture())
	if len(got) != 0 {
		t.Fatalf("empty input should match nothing, got %v", names(got))
	}
}

func TestMatchNoLeadingSlash(t *testing.T) {
	got := Match("clear", fixture())
	if len(got) != 0 {
		t.Fatalf("input without leading slash should match nothing, got %v", names(got))
	}
}

func TestMatchSinglePrefix(t *testing.T) {
	got := Match("/mo", fixture())
	if len(got) != 1 || got[0].Name != "/model" {
		t.Fatalf("/mo should match only /model, got %v", names(got))
	}
}

func TestMatchMultiplePrefix(t *testing.T) {
	got := Match("/co", fixture())
	if len(got) != 2 {
		t.Fatalf("/co should match /copy and /compact, got %v", names(got))
	}
}

func TestMatchNoMatch(t *testing.T) {
	got := Match("/zzz", fixture())
	if len(got) != 0 {
		t.Fatalf("/zzz should match nothing, got %v", names(got))
	}
}

func TestMatchStopsAtWhitespace(t *testing.T) {
	// The token is read up to the first space: "/copy btw" matches on
	// "/copy" only.
	got := Match("/copy btw", fixture())
	if len(got) != 1 || got[0].Name != "/copy" {
		t.Fatalf("token should stop at whitespace and match /copy, got %v", names(got))
	}
}

func TestMatchCaseInsensitive(t *testing.T) {
	got := Match("/MO", fixture())
	if len(got) != 1 || got[0].Name != "/model" {
		t.Fatalf("/MO should match /model case-insensitively, got %v", names(got))
	}
}

func TestMatchNilCatalogueUsesDefault(t *testing.T) {
	// A nil catalogue falls through to the scoped default catalogue;
	// "/help" is a built-in so it must appear.
	got := Match("/help", nil)
	if len(got) == 0 {
		t.Fatalf("nil catalogue should fall back to the default catalogue")
	}
}

func TestCompleteEmptyInput(t *testing.T) {
	res := Complete("", fixture())
	if res.Kind != Empty {
		t.Fatalf("empty input kind = %v, want Empty", res.Kind)
	}
	if res.Text != "" {
		t.Fatalf("empty input text = %q, want empty", res.Text)
	}
}

func TestCompleteSingleMatch(t *testing.T) {
	res := Complete("/mo", fixture())
	if res.Kind != Single {
		t.Fatalf("kind = %v, want Single", res.Kind)
	}
	if res.Text != "/model" {
		t.Fatalf("text = %q, want /model", res.Text)
	}
}

func TestCompleteMultipleMatchesCommonPrefix(t *testing.T) {
	res := Complete("/co", fixture())
	if res.Kind != Multiple {
		t.Fatalf("kind = %v, want Multiple", res.Kind)
	}
	// /copy and /compact share the prefix "/co".
	if res.Text != "/co" {
		t.Fatalf("text = %q, want /co (longest common prefix)", res.Text)
	}
	if len(res.Matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(res.Matches))
	}
}

func TestCompleteMultipleMatchesDeeperPrefix(t *testing.T) {
	cat := []slashcommands.Command{
		{Name: "/config"},
		{Name: "/context"},
		{Name: "/copy"},
	}
	res := Complete("/con", cat)
	if res.Kind != Multiple {
		t.Fatalf("kind = %v, want Multiple", res.Kind)
	}
	// /config and /context share "/con"; /copy is excluded.
	if res.Text != "/con" {
		t.Fatalf("text = %q, want /con", res.Text)
	}
	if len(res.Matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(res.Matches))
	}
}

func TestCompleteNoMatch(t *testing.T) {
	res := Complete("/zzz", fixture())
	if res.Kind != NoMatch {
		t.Fatalf("kind = %v, want NoMatch", res.Kind)
	}
	if res.Text != "" {
		t.Fatalf("text = %q, want empty", res.Text)
	}
}

func TestCompleteCommandWithArgs(t *testing.T) {
	// "/copy btw" completes on the command token "/copy".
	res := Complete("/copy btw", fixture())
	if res.Kind != Single || res.Text != "/copy" {
		t.Fatalf("got kind=%v text=%q, want Single /copy", res.Kind, res.Text)
	}
}

func TestCompleteExactMatchIsSingle(t *testing.T) {
	res := Complete("/copy", fixture())
	if res.Kind != Single || res.Text != "/copy" {
		t.Fatalf("exact command should be Single /copy, got kind=%v text=%q", res.Kind, res.Text)
	}
}

func TestHandleWritesThroughPort(t *testing.T) {
	var written string
	port := PortFunc(func(s string) { written = s })
	res := Handle("/mo", fixture(), port)
	if res.Kind != Single {
		t.Fatalf("kind = %v, want Single", res.Kind)
	}
	if written != "/model" {
		t.Fatalf("port wrote %q, want /model", written)
	}
}

func TestHandleNoMatchDoesNotWrite(t *testing.T) {
	called := false
	port := PortFunc(func(s string) { called = true })
	res := Handle("/zzz", fixture(), port)
	if res.Kind != NoMatch {
		t.Fatalf("kind = %v, want NoMatch", res.Kind)
	}
	if called {
		t.Fatalf("port should not be written on NoMatch")
	}
}

func TestHandleNilPortIsSafe(t *testing.T) {
	// A nil port must not panic; the result is still computed.
	res := Handle("/mo", fixture(), nil)
	if res.Kind != Single || res.Text != "/model" {
		t.Fatalf("nil port: got kind=%v text=%q", res.Kind, res.Text)
	}
}

func TestHandleEmptyInputDoesNotWrite(t *testing.T) {
	called := false
	port := PortFunc(func(s string) { called = true })
	res := Handle("", fixture(), port)
	if res.Kind != Empty {
		t.Fatalf("kind = %v, want Empty", res.Kind)
	}
	if called {
		t.Fatalf("port should not be written on Empty")
	}
}

func TestLongestCommonPrefix(t *testing.T) {
	cases := []struct {
		name  string
		names []string
		want  string
	}{
		{"empty", nil, ""},
		{"single", []string{"/model"}, "/model"},
		{"shared", []string{"/copy", "/compact"}, "/co"},
		{"none", []string{"/copy", "/help"}, "/"},
		{"identical", []string{"/x", "/x"}, "/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := longestCommonPrefix(tc.names); got != tc.want {
				t.Fatalf("longestCommonPrefix(%v) = %q, want %q", tc.names, got, tc.want)
			}
		})
	}
}
