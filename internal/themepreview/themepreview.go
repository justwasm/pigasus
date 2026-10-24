// Package themepreview renders the Color Scheme preview card the
// reference shows beside the /config Color Scheme submenu.
//
// The card is a 63-column, 22-line rendering of a small Go diff, wrapped
// in a rounded border, with every token painted in the *hovered* colour
// scheme's 256-colour palette. It exists so the user can see what a
// scheme looks like before committing to it.
//
// The card was recovered from agy 1.4.3's capture of the Color Scheme
// submenu (the 22-line card beside the eight scheme rows).
package themepreview

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/diffaction"
	"github.com/justwasm/pigasus/internal/onboarding"
)

// CardWidth is the card's outer width in columns, borders included.
const CardWidth = 63

// CardHeight is the card's outer height in lines, borders included.
const CardHeight = 22

// innerWidth is the width the card's content lines are padded to.
const innerWidth = CardWidth - 2

// innerHeight is the number of content lines between the two borders.
const innerHeight = CardHeight - 2

// borderFg is the card's border colour: the 90 (bright black) the
// reference uses for the frame.
const borderFg = "\x1b[90m"

// reset closes every colour span the card opens.
const reset = "\x1b[m"

// Render returns the preview card for scheme as a slice of lines.
//
// The card's 20 content lines are a miniature Go diff: the user's
// greeting, the change, the diff body, and then one line per semantic
// token (thought, tool, task, error, warning, link, accent, dim).
func Render(scheme onboarding.ColorScheme) []string {
	p := onboarding.PaletteFor(scheme)

	fg := func(code int) string {
		return fmt.Sprintf("\x1b[38;5;%dm", code)
	}

	inner := make([]string, innerHeight)

	// Line 1: ` > you: add a greeting function`
	inner[0] = fmt.Sprintf(" %s> you:%s  add a greeting function", fg(p.Link)+"\x1b[1m", reset)
	// Line 2: blank
	inner[1] = ""
	// Line 3: `   Here's the change:`
	inner[2] = fmt.Sprintf(" %s  Here's the change:%s", fg(p.String)+"\x1b[1m", reset)
	// Line 4: blank
	inner[3] = ""
	// Line 5: `   3     import  "fmt"`
	inner[4] = fmt.Sprintf("   %s3 %s    %simport%s  %s\"fmt\"%s",
		borderFg, reset,
		fg(p.Keyword)+"\x1b[1m", reset,
		fg(p.String), reset)
	// Line 6: `   4 `
	inner[5] = fmt.Sprintf("   %s4 %s", borderFg, reset)
	// Line 7: `   5  -   func  main() {`
	inner[6] = fmt.Sprintf("   %s5 %s\x1b[48;5;131m %s-   func%s  %smain%s() {",
		borderFg, reset,
		fg(p.Err), reset,
		fg(p.FunctionName), reset)
	// Line 8: `   5  +   func  greet(name  string) {`
	inner[7] = fmt.Sprintf("   %s5 %s\x1b[48;5;65m %s+   func%s  %sgreet%s(name  %sstring%s) {",
		borderFg, reset,
		fg(p.String), reset,
		fg(p.FunctionName), reset,
		fg(p.Type), reset)
	// Line 9: `   6  +     fmt.Printf("Hello, %s!\n", name)`
	inner[8] = fmt.Sprintf("   %s6 %s\x1b[48;5;65m %s+ %s    fmt.%sPrintf%s(%s\"Hello, %%s!\\n\"%s, name)",
		borderFg, reset,
		fg(p.String), reset,
		fg(p.FunctionName), reset,
		fg(p.String), reset)
	// Line 10: `   7    }`
	inner[9] = fmt.Sprintf("   %s7 %s    }", borderFg, reset)
	// Line 11: blank
	inner[10] = ""
	// Line 12: `   ▾ Thought Process`
	inner[11] = fmt.Sprintf(" %s  ▾ Thought Process%s", fg(p.Dim_), reset)
	// Line 13: `     I need to add a greeting function. I'll use fmt.Printf.`
	inner[12] = fmt.Sprintf(" %s    I need to add a greeting function. I'll use fmt.Printf.%s", fg(p.Dim_), reset)
	// Line 14: `   ⚙ tool:  write_file main.go`
	inner[13] = fmt.Sprintf(" %s  ⚙ tool:  %swrite_file main.go", fg(p.Tool), reset)
	// Line 15: `   ◉ task:  Implementing greeting`
	inner[14] = fmt.Sprintf(" %s  ◉ task:  %sImplementing greeting", fg(p.Task), reset)
	// Line 16: `   ✗ error:  compilation failed`
	inner[15] = fmt.Sprintf(" %s  ✗ error:  %scompilation failed", fg(p.Err), reset)
	// Line 17: `   ⚠ warning:  deprecation warning`
	inner[16] = fmt.Sprintf(" %s  ⚠ warning:  %sdeprecation warning", fg(p.Warn), reset)
	// Line 18: `   → link: file:///path/to/main.go`
	inner[17] = fmt.Sprintf("   → link: %s%sfile:///path/to/main.go%s", fg(p.Link), "\x1b[4m", reset)
	// Line 19: `   ★ accent:  highlighted text`
	inner[18] = fmt.Sprintf(" %s  ★ accent:  %shighlighted text", fg(p.Accent)+"\x1b[1m", reset)
	// Line 20: `   · dim: press Enter to continue`
	inner[19] = fmt.Sprintf(" %s  · dim: press Enter to continue%s", fg(p.Dim_), reset)

	card := make([]string, CardHeight)
	card[0] = borderFg + "╭" + strings.Repeat("─", innerWidth) + "╮" + reset
	for i := 0; i < innerHeight; i++ {
		plainText := diffaction.StripANSI(inner[i])
		runeCount := len([]rune(plainText))
		pad := innerWidth - runeCount
		if pad < 0 {
			pad = 0
		}
		card[i+1] = borderFg + "│" + reset + inner[i] + strings.Repeat(" ", pad) + borderFg + "│" + reset
	}
	card[CardHeight-1] = borderFg + "╰" + strings.Repeat("─", innerWidth) + "╯" + reset

	return card
}
