package transcript

import (
	"fmt"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
)

func BenchmarkLiveStepBatch50ExpandedDiffs(b *testing.B) {
	oldText := "package main\n\nfunc alpha() int {\n\treturn 1\n}\n\nfunc beta() string {\n\treturn \"hello\"\n}\n"
	newText := "package main\n\nfunc alpha() int {\n\treturn 2\n}\n\nfunc beta() string {\n\treturn \"world\"\n}\n"
	opts := Options{
		Width:     120,
		Expanded:  true,
		Verbosity: toolsteps.VerbosityMedium,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var doc Document
		batch := 0
		batchBreak := false
		for s := 0; s < 50; s++ {
			diff := tuitools.BuildEditDiff(oldText, newText)
			diff.Path = fmt.Sprintf("pkg/file_%d.go", s%5)
			running := toolsteps.Step{
				Kind:    toolsteps.KindTool,
				Name:    "edit_file",
				Title:   fmt.Sprintf("Edit(%s)", diff.Path),
				Running: true,
			}
			batch, batchBreak = doc.AppendRunningStep(&running, batch, batchBreak, opts)
			done := toolsteps.Step{
				Kind:  toolsteps.KindTool,
				Name:  "edit_file",
				Title: fmt.Sprintf("Edit(%s)", diff.Path),
				Diff:  diff,
			}
			doc.FinishLiveStep(&done, opts)
		}
	}
}
