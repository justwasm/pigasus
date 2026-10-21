package mdrender_test

import (
	"os"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/mdrender"
)

// TestRender_SmokeExample exercises a realistic Markdown blob end-to-end
// and dumps both Render and RenderANSI output to t.Log. Useful as a
// visual sanity check when developing the walker; not strictly required
// for correctness (covered by the per-feature tests in mdrender_test.go).
func TestRender_SmokeExample(t *testing.T) {
	src := `# Heading One

This is a *paragraph* with **bold text**, ` + "`inline code`" + `,
and a [link](https://example.com).

## Subheading

- first bullet
- second bullet
- third bullet with ` + "`code`" + ` inside

> A blockquote with multiple
> lines of text.

` + "```" + `go
func main() { println("hi") }
` + "```" + `

1. ordered item
2. another
3. yet another
`

	if got, err := mdrender.Render(src); err != nil {
		t.Fatalf("Render: %v", err)
	} else {
		t.Logf("Render output:\n%s", got)
		// Spot-check the structure survives intact.
		for _, want := range []string{
			"# Heading One",
			"_paragraph_",
			"*bold text*",
			"`inline code`",
			"https://example.com",
			"## Subheading",
			// Cycle 175: list-item walker now emits U+2022
			// BULLET instead of `- ` for unordered items,
			// matching the cycle-155 reference's render.
			"\u2421 first bullet",
			"\u2421 third bullet with `code` inside",
			"> A blockquote with multiple",
			"> lines of text.",
			"    func main()",
			"1. ordered item",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("Render output missing %q", want)
			}
		}
	}

	if err := mdrender.RenderANSI(src, os.Stdout); err != nil {
		t.Fatalf("RenderANSI: %v", err)
	}
}
