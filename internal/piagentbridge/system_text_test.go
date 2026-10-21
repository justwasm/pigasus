package piagentbridge

import (
	"testing"

	pi "github.com/MichaelKinsy/PiG/ai"
)

// A coding.Session keeps its instructions in the system message's Sections and
// leaves Content empty. Reading Content alone silently dropped the entire
// system prompt from every request; these cases pin the PiG-compatible
// rendering (content, then sections, joined with a blank line).
func TestSystemTextRendersSections(t *testing.T) {
	preamble := "<identity>\nYou are Antigravity.\n</identity>"
	tools := "<tools>\n- read: read a file\n</tools>"
	sections := pi.OrderedSections{
		{Name: "preamble", Value: &preamble},
		{Name: "tools", Value: &tools},
	}

	if got, want := systemText(pi.SystemMessage{Sections: sections}), preamble+"\n\n"+tools; got != want {
		t.Fatalf("sections-only system message = %q, want %q", got, want)
	}

	// A message that carries both must not lose either half.
	if got, want := systemText(pi.SystemMessage{Content: pi.SystemText("lead"), Sections: sections}), "lead\n\n"+preamble+"\n\n"+tools; got != want {
		t.Fatalf("content+sections = %q, want %q", got, want)
	}

	// Empty and nil section values are skipped, not rendered as blank lines.
	empty := ""
	if got, want := systemText(pi.SystemMessage{Sections: pi.OrderedSections{{Name: "x", Value: &empty}}}), ""; got != want {
		t.Fatalf("empty section = %q, want %q", got, want)
	}

	if got, want := systemText(pi.SystemMessage{Content: pi.SystemText("plain")}), "plain"; got != want {
		t.Fatalf("content-only = %q, want %q", got, want)
	}
}
