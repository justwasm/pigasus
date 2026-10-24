// Package termcolor provides terminal color detection and ANSI string manipulation.
package termcolor

import "strings"

// StripSGR removes every ANSI SGR sequence so terminal text can be compared
// without colour formatting.
func StripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		j := i + 1
		if j < len(s) && s[j] == '[' {
			j++
			for j < len(s) && !((s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z')) {
				j++
			}
			j++
		}
		i = j - 1
	}
	return b.String()
}
