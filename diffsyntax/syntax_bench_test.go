package diffsyntax

import (
	"sync"
	"testing"
)

func TestHighlightCodeLine_MatchesTokenizeLine(t *testing.T) {
	cases := []struct {
		file string
		line string
	}{
		{"foo.go", `func Foo() string { return "hi" }`},
		{"foo.go", `// comment with func`},
		{"foo.go", `++i`},
		{"foo.go", `--flag`},
		{"foo.go", ``},
		{"foo.py", `def bar(x=42): return "ok"`},
		{"foo.rs", `let v: Vec<u8> = Vec::new();`},
		{"foo.xyz", `func unknown() {}`},
		{"", `func emptyFile() {}`},
	}
	for _, tc := range cases {
		want := tc.line
		if tc.file != "" {
			if tokens := TokenizeLine(tc.file, " "+tc.line); len(tokens) > 1 {
				want = RenderInlineTokenStream(tokens[1:])
			}
		}
		got := HighlightCodeLine(tc.file, tc.line)
		if got != want {
			t.Errorf("HighlightCodeLine(%q, %q) = %q, want %q", tc.file, tc.line, got, want)
		}
	}
}

func TestCachedLexer_SharesBasenameAndConcurrent(t *testing.T) {
	a := getCachedLexer("pkg/alpha/main.go")
	b := getCachedLexer("pkg/beta/main.go")
	if a == nil || a != b {
		t.Fatalf("expected shared *cachedLexer across directories, got %p vs %p", a, b)
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = HighlightCodeLine("dir/sub/file.go", `func Foo() int { return 42 }`)
				_ = TokenizeLine("dir/sub/file.go", `+return "ok"`)
			}
		}(g)
	}
	wg.Wait()
}

func BenchmarkHighlightCodeLine_Go(b *testing.B) {
	const line = `func (d *Document) FinishLiveStep(step *toolsteps.Step, opts Options) bool {`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = HighlightCodeLine("internal/ui/transcript/livesteps.go", line)
	}
}

func BenchmarkTokenizeLine_UnknownExt(b *testing.B) {
	const line = `+some plain text line in an unknown extension file`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = TokenizeLine("notes.unknownext", line)
	}
}
