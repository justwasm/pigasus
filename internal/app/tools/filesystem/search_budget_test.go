package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func writeSearchFixture(t testing.TB, root, name, text string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runSearchFixture(t testing.TB, tool interface {
	Run(context.Context, json.RawMessage) (any, error)
}, args string) string {
	t.Helper()
	v, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("result has type %T", v)
	}
	return s
}

func TestBoundedSearchNoFilesVersusNoMatches(t *testing.T) {
	root := t.TempDir()
	grep := &GrepSearchTool{Workspace: root}
	if got := runSearchFixture(t, grep, `{"pattern":"missing"}`); got != "No files were searched, double check your search parameter" {
		t.Fatal(got)
	}
	writeSearchFixture(t, root, "text.txt", "hello\n")
	if got := runSearchFixture(t, grep, `{"pattern":"missing"}`); got != "No matches found." {
		t.Fatal(got)
	}
	if got := runSearchFixture(t, grep, `{"pattern":"hello","include":"*.go"}`); got != "No files were searched, double check your search parameter" {
		t.Fatal(got)
	}
	if got := runSearchFixture(t, grep, `{"Query":"hello","Path":"text.txt"}`); got != "text.txt:1: hello" {
		t.Fatal(got)
	}
}

func TestBoundedSearchCancellation(t *testing.T) {
	root := t.TempDir()
	writeSearchFixture(t, root, "x.txt", "hello\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		tool interface {
			Run(context.Context, json.RawMessage) (any, error)
		}
		args string
	}{
		{&GrepSearchTool{Workspace: root}, `{"pattern":"hello"}`},
		{&FindByName{Workspace: root}, `{"pattern":"*"}`},
		{&CodebaseSearch{Workspace: root}, `{"query":"hello"}`},
	} {
		if _, err := tc.tool.Run(ctx, json.RawMessage(tc.args)); !errors.Is(err, context.Canceled) {
			t.Fatalf("%T cancellation = %v", tc.tool, err)
		}
	}
	if err := WalkFilesContext(ctx, root, func(_, _ string) error { t.Fatal("called after cancellation"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	var budget searchBudget
	_, err := budget.readTextFile(ctx, filepath.Join(root, "x.txt"), func(_ []byte, _ int) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-read cancellation = %v", err)
	}
	if budget.readBytes > 32*1024 {
		t.Fatalf("read after cancellation: %d", budget.readBytes)
	}
}

type cancellationReader struct {
	cancel context.CancelFunc
	calls  int
}

func (r *cancellationReader) Read(p []byte) (int, error) {
	r.calls++
	r.cancel()
	p[0] = 'x'
	return 1, nil
}
func TestBoundedContextReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &cancellationReader{cancel: cancel}
	cr := &contextReader{ctx: ctx, r: r}
	buf := make([]byte, 2)
	if n, err := cr.Read(buf); n != 1 || err != nil {
		t.Fatalf("first read = %d, %v", n, err)
	}
	if n, err := cr.Read(buf); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("second read = %d, %v", n, err)
	}
	if r.calls != 1 || cr.n != 1 {
		t.Fatalf("calls=%d n=%d", r.calls, cr.n)
	}
}

func TestBoundedTextFileBinaryAndByteBudgets(t *testing.T) {
	root := t.TempDir()
	binary := writeSearchFixture(t, root, "binary", "\x00"+strings.Repeat("x", 1<<20))
	var budget searchBudget
	searched, err := budget.readTextFile(context.Background(), binary, func(_ []byte, _ int) error { t.Fatal("binary file callback"); return nil })
	if err != nil || searched || budget.readBytes > 32*1024 {
		t.Fatalf("searched=%v err=%v read=%d", searched, err, budget.readBytes)
	}

	text := writeSearchFixture(t, root, "large", strings.Repeat("x\n", int(maxSearchFileBytes/2)+20))
	budget = searchBudget{}
	_, err = budget.readTextFile(context.Background(), text, func(_ []byte, _ int) error { return nil })
	if err != nil || budget.readBytes != maxSearchFileBytes || !budget.fileTruncated {
		t.Fatalf("err=%v budget=%+v", err, budget)
	}
	budget = searchBudget{readBytes: maxSearchTotalBytes - 100}
	_, err = budget.readTextFile(context.Background(), text, func(_ []byte, _ int) error { return nil })
	if err != nil || budget.readBytes != maxSearchTotalBytes || !budget.totalTruncated {
		t.Fatalf("err=%v budget=%+v", err, budget)
	}
}

func TestBoundedTextFileLines(t *testing.T) {
	for _, text := range []string{"", "\n", "a", "a\n", "a\nb\r\n", strings.Repeat("a", 32*1024), strings.Repeat("a", 32*1024) + "\n"} {
		t.Run(fmt.Sprintf("len%d-final%d", len(text), strings.Count(text, "\n")), func(t *testing.T) {
			root := t.TempDir()
			file := writeSearchFixture(t, root, "text", text)
			var budget searchBudget
			var lines []string
			_, err := budget.readTextFile(context.Background(), file, func(line []byte, n int) error {
				if n != len(lines)+1 {
					t.Fatalf("line number %d", n)
				}
				lines = append(lines, string(line))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.Split(text, "\n"); !reflect.DeepEqual(lines, want) {
				t.Fatalf("lines=%q want=%q", lines, want)
			}
		})
	}
}

func TestBoundedSearchLongLinesAndUTF8(t *testing.T) {
	root := t.TempDir()
	writeSearchFixture(t, root, "long", strings.Repeat("x", maxSearchLineBytes+1)+"\nhello\n")
	got := runSearchFixture(t, &GrepSearchTool{Workspace: root}, `{"pattern":"hello"}`)
	if !strings.Contains(got, "long:2: hello") || !strings.Contains(got, "256 KiB") {
		t.Fatal(got)
	}
	// The binary prefix ends within a multibyte rune, not at its boundary.
	writeSearchFixture(t, root, "utf8", strings.Repeat("x", 8191)+"你好\n")
	got = runSearchFixture(t, &GrepSearchTool{Workspace: root}, `{"pattern":"你好","path":"utf8"}`)
	if !utf8.ValidString(got) || !strings.Contains(got, "line truncated") {
		t.Fatal(got)
	}
	if !LooksTextual([]byte(strings.Repeat("x", 8191) + "你好")) {
		t.Fatal("valid UTF-8 prefix rejected")
	}
}

func TestBoundedSearchResultLimits(t *testing.T) {
	for _, n := range []int{0, -1, 1, 200, int(^uint(0) >> 1)} {
		want := n
		if want <= 0 || want > maxSearchResults {
			want = maxSearchResults
		}
		if got := normalizeMaxResults(n); got != want {
			t.Fatalf("normalize(%d)=%d want=%d", n, got, want)
		}
	}
	root := t.TempDir()
	writeSearchFixture(t, root, "text", strings.Repeat("hello\n", 201))
	got := runSearchFixture(t, &GrepSearchTool{Workspace: root}, `{"pattern":"hello","max_results":2147483647}`)
	if strings.Count(got, ": hello") != maxSearchResults || !strings.Contains(got, "max_results (200)") {
		t.Fatal(got)
	}
	for i := 0; i < 3; i++ {
		writeSearchFixture(t, root, fmt.Sprintf("f%d", i), "hello")
	}
	got = runSearchFixture(t, &FindByName{Workspace: root}, `{"pattern":"f*","max_results":1}`)
	if !strings.HasPrefix(got, "f0\n") || !strings.Contains(got, "max_results (1)") {
		t.Fatal(got)
	}
}

func TestBoundedSearchOutputBytes(t *testing.T) {
	for _, lines := range [][]string{
		{strings.Repeat("界", maxSearchOutputBytes)},
		{strings.Repeat("a", maxSearchOutputBytes), "extra"},
		{strings.Repeat("a", maxSearchOutputBytes-1), "extra"},
		{strings.Repeat("a", maxSearchOutputBytes-10)},
	} {
		got := renderSearchOutput(lines, []string{"note beyond bytes"}, "empty")
		if len(got) > maxSearchOutputBytes || !utf8.ValidString(got) || !strings.Contains(got, "truncated") {
			t.Fatalf("len=%d valid=%v", len(got), utf8.ValidString(got))
		}
	}
	root := t.TempDir()
	writeSearchFixture(t, root, "text", strings.Repeat(strings.Repeat("界", 2000)+"\n", 200))
	got := runSearchFixture(t, &GrepSearchTool{Workspace: root}, `{"pattern":"界"}`)
	if len(got) > maxSearchOutputBytes || !utf8.ValidString(got) || !strings.Contains(got, "output byte budget") {
		t.Fatalf("len=%d valid=%v", len(got), utf8.ValidString(got))
	}
}

func TestBoundedCodebaseRankingAndSamples(t *testing.T) {
	root := t.TempDir()
	writeSearchFixture(t, root, "a", "second first\nFIRST\n")
	writeSearchFixture(t, root, "b", "second\nfirst first first\n")
	writeSearchFixture(t, root, "c", "no terms here\n")
	got := runSearchFixture(t, &CodebaseSearch{Workspace: root}, `{"query":"first second","max_results":1}`)
	if !strings.HasPrefix(got, "b (score 4): first first first") {
		t.Fatal(got)
	}
	got = runSearchFixture(t, &CodebaseSearch{Workspace: root}, `{"query":"absent"}`)
	if got != "No matches found." {
		t.Fatal(got)
	}
}

func TestBoundedSearchPathContainment(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeSearchFixture(t, outside, "text", "sensitive")
	for _, tc := range []struct {
		tool interface {
			Run(context.Context, json.RawMessage) (any, error)
		}
		args string
	}{
		{&GrepSearchTool{Workspace: root}, fmt.Sprintf(`{"pattern":"sensitive","path":%q}`, outside)},
		{&FindByName{Workspace: root}, fmt.Sprintf(`{"pattern":"*","path":%q}`, outside)},
		{&CodebaseSearch{Workspace: root}, fmt.Sprintf(`{"query":"sensitive","path":%q}`, outside)},
	} {
		if _, err := tc.tool.Run(context.Background(), json.RawMessage(tc.args)); err == nil {
			t.Fatalf("%T accepted outside workspace", tc.tool)
		}
	}
}

func TestBoundedWalkDirSymlinksPruningAndErrors(t *testing.T) {
	root := t.TempDir()
	writeSearchFixture(t, root, "keep/a", "a")
	writeSearchFixture(t, root, "node_modules/explicit", "b")
	outside := t.TempDir()
	writeSearchFixture(t, outside, "other", "outside")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(filepath.Join(root, "keep/a"), filepath.Join(root, "filelink")); err != nil {
		t.Skip(err)
	}
	var got []string
	if err := WalkFiles(root, func(_, rel string) error { got = append(got, rel); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{filepath.Join("keep", "a")}) {
		t.Fatal(got)
	}
	got = nil
	if err := WalkFilesContext(context.Background(), filepath.Join(root, "node_modules"), func(_, rel string) error { got = append(got, rel); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"explicit"}) {
		t.Fatal(got)
	}
	if err := WalkFilesContext(context.Background(), filepath.Join(root, "link"), func(_, _ string) error { t.Fatal("descended into root directory symlink"); return nil }); err != nil {
		t.Fatal(err)
	}
	got = nil
	if err := WalkFilesContext(context.Background(), filepath.Join(root, "filelink"), func(_, rel string) error { got = append(got, rel); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"filelink"}) {
		t.Fatalf("explicit file symlink paths=%v", got)
	}
	if err := WalkFilesContext(context.Background(), filepath.Join(root, "missing"), func(_, _ string) error { return nil }); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := WalkFilesContext(ctx, root, func(_, _ string) error { cancel(); return ErrStopWalk }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func BenchmarkBoundedGrepSearch(b *testing.B) {
	root := b.TempDir()
	writeSearchFixture(b, root, "text", strings.Repeat("ordinary source line with no hit\n", 32768))
	g := &GrepSearchTool{Workspace: root}
	b.ReportAllocs()
	b.SetBytes(1 << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := g.Run(context.Background(), json.RawMessage(`{"pattern":"nonexistent"}`)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBoundedBinaryPrefix(b *testing.B) {
	root := b.TempDir()
	file := writeSearchFixture(b, root, "binary", "\x00"+strings.Repeat("x", 8<<20))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var budget searchBudget
		if _, err := budget.readTextFile(context.Background(), file, func(_ []byte, _ int) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBoundedWalkDir(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 1000; i++ {
		writeSearchFixture(b, root, fmt.Sprintf("d%d/f%04d.go", i/100, i), "text")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := WalkFilesContext(context.Background(), root, func(_, _ string) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}

// Ensure cancellation tests use only deterministic in-memory readers.
var _ io.Reader = (*cancellationReader)(nil)

func TestBoundedSearchBudgetCutDoesNotInventLineEnd(t *testing.T) {
	root := t.TempDir()
	file := writeSearchFixture(t, root, "text", "complete\ncut-in-the-middle\n")
	budget := searchBudget{readBytes: maxSearchTotalBytes - int64(len("complete\ncut"))}
	var lines []string
	_, err := budget.readTextFile(context.Background(), file, func(line []byte, _ int) error {
		lines = append(lines, string(line))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lines, []string{"complete"}) {
		t.Fatalf("processed truncated line: %q", lines)
	}
	if !budget.totalTruncated || budget.readBytes != maxSearchTotalBytes {
		t.Fatalf("budget=%+v", budget)
	}
}

func TestBoundedCodebaseTopKOrderAndLimits(t *testing.T) {
	var hits []codebaseHit
	for _, h := range []codebaseHit{
		{rel: "z", score: 3}, {rel: "b", score: 7}, {rel: "a", score: 7}, {rel: "x", score: 1}, {rel: "c", score: 7},
	} {
		insertCodebaseHit(&hits, h, 2)
	}
	if !reflect.DeepEqual(hits, []codebaseHit{{rel: "a", score: 7}, {rel: "b", score: 7}}) {
		t.Fatal(hits)
	}
	root := t.TempDir()
	tool := &CodebaseSearch{Workspace: root}
	for _, query := range []string{strings.Repeat("q", 4097), strings.Repeat("term ", 33)} {
		args, err := json.Marshal(map[string]any{"query": query})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tool.Run(context.Background(), args); err == nil {
			t.Fatal("accepted unbounded query")
		}
	}
}

func TestWalkFilesContextExplicitRootSymlinkToFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "alias.txt")
	if err := os.WriteFile(target, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	var got []string
	if err := WalkFilesContext(context.Background(), link, func(path, rel string) error { got = append(got, rel); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"alias.txt"}) {
		t.Fatalf("paths=%v", got)
	}
}

func TestBoundedSearchOutputBytesMarkerExactlyOnce(t *testing.T) {
	// renderSearchOutput only appends the marker when it dropped output; the
	// grep output-limit path passes the marker as a note, so the combined
	// string must still carry it exactly once and stay within budget.
	for _, lines := range [][]string{
		{strings.Repeat("a", maxSearchOutputBytes-1), "extra"},
		{strings.Repeat("a", maxSearchOutputBytes-10)},
	} {
		notes := []string{"note beyond bytes", strings.TrimPrefix(outputTruncatedMarker, "\n")}
		got := renderSearchOutput(lines, notes, "empty")
		if n := strings.Count(got, "output byte budget reached"); n != 1 {
			t.Fatalf("marker count = %d, want 1", n)
		}
		if len(got) > maxSearchOutputBytes || !utf8.ValidString(got) {
			t.Fatalf("len=%d valid=%v", len(got), utf8.ValidString(got))
		}
	}
	var lines []string
	for i := 0; i < 4000; i++ {
		lines = append(lines, fmt.Sprintf("%04d %s", i, strings.Repeat("x", 200)))
	}
	got := renderSearchOutput(lines, []string{"note one", "note two"}, "empty")
	if n := strings.Count(got, "output byte budget reached"); n != 1 {
		t.Fatalf("marker count = %d, want 1", n)
	}
	if len(got) > maxSearchOutputBytes {
		t.Fatalf("len=%d exceeds budget", len(got))
	}
}
