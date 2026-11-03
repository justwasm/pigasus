// tokens.go — the prompt's own token arithmetic.
//
// The /context panel reports the reference prompt's three categories
// separately, so one section has to be measured on its own.

package agyprompt

// SectionTokenCount measures one complete XML section.
//
// The section is its body plus the opening and the closing tag, and
// the reference counts three bytes per token.
func SectionTokenCount(name, body string) int {
	if body == "" {
		return 0
	}
	return (len(body) + 2*len(name) + 2 + 1) / 3
}
