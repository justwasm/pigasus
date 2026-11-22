package viewport

import (
	"fmt"
	"strings"
	"testing"
)

func TestLineDiffEngine_InitialRender(t *testing.T) {
	lines := []string{
		"Line 0: Header",
		"Line 1: Status running",
		"Line 2: Footer",
	}

	engine := NewLineDiffEngine(80, len(lines))
	engine.SetOrigin(1, 1)

	rendered := engine.RenderDiff(lines)
	if rendered == "" {
		t.Fatal("initial render should produce ANSI sequences for all lines")
	}

	// Verify all 3 lines are positioned
	if !strings.Contains(rendered, "\x1b[1;1HLine 0: Header\x1b[K") {
		t.Errorf("rendered output missing row 1: %q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[2;1HLine 1: Status running\x1b[K") {
		t.Errorf("rendered output missing row 2: %q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[3;1HLine 2: Footer\x1b[K") {
		t.Errorf("rendered output missing row 3: %q", rendered)
	}
}

func TestLineDiffEngine_IdenticalFrame(t *testing.T) {
	lines := []string{
		"Line 0",
		"Line 1",
		"Line 2",
	}

	engine := NewLineDiffEngine(80, len(lines))
	_ = engine.RenderDiff(lines)

	// Second render with identical content should return empty string (zero ANSI output)
	second := engine.RenderDiff(lines)
	if second != "" {
		t.Fatalf("expected empty diff on identical frame, got %q", second)
	}
	if engine.Grid().DirtyCount() != 0 {
		t.Fatalf("dirty count = %d, want 0", engine.Grid().DirtyCount())
	}
}

func TestLineDiffEngine_SingleLineChange(t *testing.T) {
	lines := []string{
		"Row 0 (static)",
		"Row 1 (counter: 0)",
		"Row 2 (static)",
		"Row 3 (static)",
	}

	engine := NewLineDiffEngine(80, len(lines))
	_ = engine.RenderDiff(lines)

	// Mutate only row 1
	updated := []string{
		"Row 0 (static)",
		"Row 1 (counter: 1)",
		"Row 2 (static)",
		"Row 3 (static)",
	}

	diffOut := engine.RenderDiff(updated)
	if diffOut == "" {
		t.Fatal("expected diff output for changed row")
	}

	// Should contain row 2 CUP (\x1b[2;1H)
	if !strings.Contains(diffOut, "\x1b[2;1HRow 1 (counter: 1)\x1b[K") {
		t.Errorf("expected update on row 2, got %q", diffOut)
	}

	// Must NOT contain updates for static rows (Row 0, 2, 3)
	if strings.Contains(diffOut, "\x1b[1;1H") || strings.Contains(diffOut, "\x1b[3;1H") || strings.Contains(diffOut, "\x1b[4;1H") {
		t.Errorf("diff output touched unchanged rows: %q", diffOut)
	}

	if engine.Grid().DirtyCount() != 1 {
		t.Fatalf("dirty count = %d, want 1", engine.Grid().DirtyCount())
	}
}

func TestLineDiffEngine_ResizeAndForceRedraw(t *testing.T) {
	lines := []string{"Line 0", "Line 1"}
	engine := NewLineDiffEngine(80, 2)
	_ = engine.RenderDiff(lines)

	// Force redraw
	engine.ForceRedraw()
	diff := engine.RenderDiff(lines)
	if diff == "" {
		t.Fatal("ForceRedraw should cause full output")
	}

	// Shrink/expand height
	expanded := []string{"Line 0", "Line 1", "Line 2", "Line 3"}
	diffExp := engine.RenderDiff(expanded)
	if !strings.Contains(diffExp, "\x1b[3;1HLine 2") || !strings.Contains(diffExp, "\x1b[4;1HLine 3") {
		t.Errorf("expansion missing new rows: %q", diffExp)
	}
}

func TestLineHashGrid_ZeroCopyBytes(t *testing.T) {
	grid := NewLineHashGrid(40, 5)
	grid.ClearDirty()

	b := []byte("streaming token buffer")
	dirty := grid.SetLineBytes(2, b)
	if !dirty {
		t.Fatal("expected line 2 to be marked dirty")
	}
	if !grid.IsDirty(2) {
		t.Fatal("IsDirty(2) should be true")
	}

	// Same bytes again -> not dirty
	dirtyAgain := grid.SetLineBytes(2, b)
	if dirtyAgain {
		t.Fatal("expected identical line bytes not to be dirty")
	}
}

// ---------------- Microbenchmarks ----------------

func BenchmarkLineHashGrid_HashLine(b *testing.B) {
	line := "2026-11-22T06:39:03Z [agentturn] turn started with prompt 'OpenAGY Phase 2: Engine Sovereignty'"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = HashLine(line)
	}
}

func BenchmarkLineHashGrid_HashLineBytes(b *testing.B) {
	lineBytes := []byte("2026-11-22T06:39:03Z [agentturn] turn started with prompt 'OpenAGY Phase 2: Engine Sovereignty'")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = HashLineBytes(lineBytes)
	}
}

func BenchmarkLineDiffEngine_Identical(b *testing.B) {
	n := 60
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		lines[i] = fmt.Sprintf("Line %02d: steady state terminal content that does not change between frames", i)
	}
	engine := NewLineDiffEngine(80, n)
	_ = engine.RenderDiff(lines)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = engine.RenderDiff(lines)
	}
}

func BenchmarkLineDiffEngine_SingleLineChange(b *testing.B) {
	n := 60
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		lines[i] = fmt.Sprintf("Line %02d: steady state terminal content", i)
	}
	engine := NewLineDiffEngine(80, n)
	_ = engine.RenderDiff(lines)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lines[n-1] = fmt.Sprintf("Line %02d: active ticker value = %d", n-1, i)
		_ = engine.RenderDiff(lines)
	}
}

func BenchmarkLineDiffEngine_StreamingAppend(b *testing.B) {
	n := 40
	lines := make([]string, n)
	for i := 0; i < n-1; i++ {
		lines[i] = fmt.Sprintf("Line %02d: completed transcript text here", i)
	}
	engine := NewLineDiffEngine(80, n)
	_ = engine.RenderDiff(lines)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lines[n-1] = strings.Repeat("streaming token ", (i%10)+1)
		_ = engine.RenderDiff(lines)
	}
}

func BenchmarkLineDiffEngine_FullRedraw(b *testing.B) {
	n := 50
	linesA := make([]string, n)
	linesB := make([]string, n)
	for i := 0; i < n; i++ {
		linesA[i] = fmt.Sprintf("Frame A Line %02d: initial contents", i)
		linesB[i] = fmt.Sprintf("Frame B Line %02d: full screen update content", i)
	}
	engine := NewLineDiffEngine(80, n)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			_ = engine.RenderDiff(linesA)
		} else {
			_ = engine.RenderDiff(linesB)
		}
	}
}
