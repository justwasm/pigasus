package clipboard

import "strings"

// NormalizeText strips carriage returns that a Windows-style clipboard or
// terminal may include, converting CRLF to LF and dropping stray CRs.
func NormalizeText(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "")
}
