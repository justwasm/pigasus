package main

import "strings"

// stripSGR removes every ANSI SGR sequence so the panel text can be
// compared with a capture that had its colours normalised.
func stripSGR(s string) string {
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
