// Package diffrow git.go — the unified-diff hunk header.
//
// 【strings】 the hunk header is git's own: "@@ -a,b +c,d @@", optionally
// followed by the section heading git copies from the file.
package row

import "strconv"

// HunkHeader holds the line ranges a unified diff's hunk header declares.
type HunkHeader struct {
	// OldStart is the first line of the hunk in the old file.
	OldStart int
	// OldCount is the number of lines the hunk spans in the old file.
	OldCount int
	// NewStart is the first line of the hunk in the new file.
	NewStart int
	// NewCount is the number of lines the hunk spans in the new file.
	NewCount int
}

// ParseHunkHeader parses a "@@ -a,b +c,d @@" header.
//
// The counts are optional — git writes "@@ -a +c @@" for a hunk of one
// line — and default to one.
func ParseHunkHeader(line string) (HunkHeader, bool) {
	var h HunkHeader
	if len(line) < 4 || line[:2] != "@@" {
		return h, false
	}
	rest := line[2:]
	old, rest, ok := parseRange(rest)
	if !ok {
		return h, false
	}
	new, _, ok := parseRange(rest)
	if !ok {
		return h, false
	}
	h.OldStart, h.OldCount = old[0], old[1]
	h.NewStart, h.NewCount = new[0], new[1]
	return h, true
}

// parseRange reads a "-a,b" or "+a,b" range and returns the rest of the
// header.
func parseRange(s string) (rng [2]int, rest string, ok bool) {
	// git writes a space between the two ranges: "@@ -1,3 +1,4 @@".
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	if len(s) == 0 || (s[0] != '-' && s[0] != '+') {
		return rng, s, false
	}
	s = s[1:]
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return rng, s, false
	}
	start, err := strconv.Atoi(s[:end])
	if err != nil {
		return rng, s, false
	}
	count := 1
	s = s[end:]
	if len(s) > 0 && s[0] == ',' {
		s = s[1:]
		end = 0
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		if end > 0 {
			if n, err := strconv.Atoi(s[:end]); err == nil {
				count = n
			}
			s = s[end:]
		}
	}
	return [2]int{start, count}, s, true
}
