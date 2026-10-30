package canvas_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/canvas"
)

func TestCanvasFacade(t *testing.T) {
	line := canvas.Line{
		Content: "hello world",
		Highlights: []canvas.Range{
			{Start: 0, End: 5},
		},
	}
	rendered := line.Render()
	if rendered == "" {
		t.Fatal("expected non-empty rendered string")
	}
}
