package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
)

// BenchmarkCtrlOToggleFullFrameRealistic benchmarks a realistic conversation
// with 50 turns (tool execution + outputs), toggling Ctrl+O and executing View().
func BenchmarkCtrlOToggleFullFrameRealistic(b *testing.B) {
	m := newBenchModel()
	m.viewport.width = 120
	m.viewport.height = 40

	for i := 0; i < 50; i++ {
		// User prompt
		m.conversation.response = append(m.conversation.response, transcript.UserPromptRow(fmt.Sprintf("Run test suite %d", i)))

		// Tool step block
		start := len(m.conversation.response)
		step := toolsteps.Step{
			Kind:   toolsteps.KindTool,
			Name:   "run_command",
			Title:  fmt.Sprintf("Bash(go test -v ./pkg%d/...)", i),
			Detail: strings.Repeat(fmt.Sprintf("=== RUN TestSub%d\n--- PASS: TestSub%d (0.01s)\n", i, i), 10),
		}
		lines := toolsteps.RenderBlock([]toolsteps.Step{step}, toolsteps.RenderOptions{
			Expanded:  false,
			Width:     m.viewport.width,
			Verbosity: toolsteps.VerbosityLow,
		})
		m.conversation.response = append(m.conversation.response, lines...)
		m.conversation.blocks = append(m.conversation.blocks, transcript.NewBlock(transcript.Block{
			Start: start,
			Lines: len(lines),
			Steps: []toolsteps.Step{step},
		}, lines, m.viewport.width, toolsteps.VerbosityLow))

		// Assistant reply
		m.conversation.response = append(m.conversation.response, fmt.Sprintf("Tests completed successfully for suite %d.", i))
	}

	_ = m.View()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.toggleTrajectoryExpanded()
		_ = m.View()
	}
}
