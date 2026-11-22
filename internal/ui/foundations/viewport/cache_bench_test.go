package viewport

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkWrapSlot_RebuildMiddleExpand(b *testing.B) {
	// 500 lines before, 1 collapsed line, 500 lines after
	width := 120
	basePrefix := make([]string, 500)
	for i := range basePrefix {
		basePrefix[i] = fmt.Sprintf("line %d: %s", i, strings.Repeat("code ", 10))
	}
	baseSuffix := make([]string, 500)
	for i := range baseSuffix {
		baseSuffix[i] = fmt.Sprintf("suffix line %d: %s", i, strings.Repeat("data ", 10))
	}

	collapsed := append(append([]string{}, basePrefix...), "● Bash(go test ./...) (ctrl+o to expand)")
	collapsed = append(collapsed, baseSuffix...)

	// Expanded version has 50 lines in the middle
	expandedMiddle := make([]string, 50)
	for i := range expandedMiddle {
		expandedMiddle[i] = fmt.Sprintf("  │ test output row %d: %s", i, strings.Repeat("detail ", 8))
	}
	expanded := append(append([]string{}, basePrefix...), "● Bash(go test ./...) (ctrl+o to collapse)")
	expanded = append(expanded, expandedMiddle...)
	expanded = append(expanded, baseSuffix...)

	var slot WrapSlot
	slot.Rebuild(collapsed, width)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Toggle expand then collapse
		slot.Rebuild(expanded, width)
		slot.Rebuild(collapsed, width)
	}
}
