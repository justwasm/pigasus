package onboarding

import (
	"fmt"
	"io"
	"strings"
)

// ANSI escape codes. We hand-build the SGR sequences instead of
// pulling in fatih/color so the wizard has zero deps beyond stdlib —
// this is the v1.3.5 binary's approach (it inlines the SGR strings
// from glamour/ansi).
const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiUnder   = "\x1b[4m"
	ansiReverse = "\x1b[7m"
)

// 256-color foreground: \x1b[38;5;Nm where N is 0..255.
// 256-color background: \x1b[48;5;Nm.
// These are vars (not consts) because they are function literals.
var (
	fg256 = func(n int) string { return fmt.Sprintf("\x1b[38;5;%dm", n) }
	bg256 = func(n int) string { return fmt.Sprintf("\x1b[48;5;%dm", n) }
)

// Marker icons — every preview footer uses these 7 Unicode glyphs
// followed by a label. The 1:1 verification checks that the byte
// sequences of these markers appear in renderColorScheme output.
//
//	⚙ tool        — sub-agent / tool invocation
//	◉ task        — todo list / task tracker
//	✗ error       — error message
//	⚠ warning     — warning / advisory
//	→ link        — hyperlink (URL underlined)
//	★ accent      — accent highlight
//	· dim         — dim / secondary text
const (
	MarkerTool    = "⚙"
	MarkerTask    = "◉"
	MarkerError   = "✗"
	MarkerWarning = "⚠"
	MarkerLink    = "→"
	MarkerAccent  = "★"
	MarkerDim     = "·"
)

// renderMarkersFooter writes the 7-icon marker line to w.
func renderMarkersFooter(w io.Writer) error {
	markers := []struct{ icon, label string }{
		{MarkerTool, "tool"},
		{MarkerTask, "task"},
		{MarkerError, "error"},
		{MarkerWarning, "warning"},
		{MarkerLink, "link"},
		{MarkerAccent, "accent"},
		{MarkerDim, "dim"},
	}
	var sb strings.Builder
	for i, m := range markers {
		if i > 0 {
			sb.WriteString("   ")
		}
		sb.WriteString(m.icon)
		sb.WriteString(" ")
		sb.WriteString(m.label)
	}
	_, err := fmt.Fprintf(w, "%s\n", sb.String())
	return err
}
