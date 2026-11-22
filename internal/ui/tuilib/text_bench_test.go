package tuilib

import (
	"strings"
	"testing"
)

func BenchmarkWrapRows_PlainAscii(b *testing.B) {
	line := "func (m *RootModel) HandleKey(key tea.Key) (bool, tea.Cmd) { return m.handleVimKey(key) }"
	width := 120

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WrapRows(line, width)
	}
}

func BenchmarkWrapRows_LongAscii(b *testing.B) {
	line := strings.Repeat("apple banana orange cherry grape mango pineapple watermelon ", 10)
	width := 120

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WrapRows(line, width)
	}
}

func BenchmarkWrapRows_AnsiColored(b *testing.B) {
	line := "\x1b[38;2;129;201;149m●\x1b[39m \x1b[38;2;253;214;99;1mBash\x1b[0m\x1b[38;2;189;193;198m(go test ./internal/...)\x1b[39m \x1b[38;2;189;193;198m(ctrl+o to expand)\x1b[39m"
	width := 120

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WrapRows(line, width)
	}
}
