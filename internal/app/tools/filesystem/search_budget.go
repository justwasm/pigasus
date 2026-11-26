package filesystem

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"strings"
)

const (
	maxSearchFileBytes    int64 = 8 << 20
	maxSearchTotalBytes   int64 = 64 << 20
	maxSearchOutputBytes        = 512 << 10
	maxSearchResults            = 200
	maxSearchLineBytes          = 256 << 10
	maxSearchSnippetBytes       = 4096
)

// searchBudget counts bytes actually read from eligible files. A per-file cap
// bounds pathological files; a total cap bounds work across the whole tree.
type searchBudget struct {
	readBytes      int64
	fileTruncated  bool
	totalTruncated bool
	lineTruncated  bool
}

func (b *searchBudget) remaining() int64 {
	return maxSearchTotalBytes - b.readBytes
}

// contextReader checks cancellation on every underlying read and tracks bytes
// fetched from disk (including read-ahead by bufio.Reader).
type contextReader struct {
	ctx context.Context
	r   io.Reader
	n   int64
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

// readTextFile streams lines from a regular file, checking a bounded prefix
// before treating it as text. It never reads more than the remaining global
// budget or maxSearchFileBytes from a single file. Unreadable files are
// skipped, matching the search tools' historical best-effort behavior.
func (b *searchBudget) readTextFile(ctx context.Context, path string, fn func([]byte, int) error) (searched bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	f, err := os.Open(path)
	if err != nil {
		return false, nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false, nil
	}

	allowance := info.Size()
	if allowance > maxSearchFileBytes {
		allowance = maxSearchFileBytes
	}
	remaining := b.remaining()
	if allowance > remaining {
		allowance = remaining
	}
	if allowance < 0 {
		allowance = 0
	}

	cr := &contextReader{ctx: ctx, r: f}
	defer func() { b.readBytes += cr.n }()
	limited := io.LimitReader(cr, allowance)
	reader := bufio.NewReaderSize(limited, 32*1024)
	prefix := make([]byte, 8192+3)
	n, prefixErr := io.ReadFull(reader, prefix)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if prefixErr != nil && prefixErr != io.EOF && prefixErr != io.ErrUnexpectedEOF {
		return false, nil
	}
	prefix = prefix[:n]
	if !LooksTextual(prefix) {
		return false, nil
	}
	searched = true
	partialFile := info.Size() > allowance
	if partialFile {
		if remaining <= maxSearchFileBytes && allowance == remaining {
			b.totalTruncated = true
		} else {
			b.fileTruncated = true
		}
	}

	lines := bufio.NewReaderSize(io.MultiReader(bytes.NewReader(prefix), reader), 32*1024)
	lineNo := 1
	sawBytes := false
	lastWasNewline := false
	process := func(line []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(line, lineNo)
	}
	var line []byte
	overlong := false
	for {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		fragment, readErr := lines.ReadSlice('\n')
		if len(fragment) > 0 {
			sawBytes = true
			lastWasNewline = fragment[len(fragment)-1] == '\n'
			if lastWasNewline {
				fragment = fragment[:len(fragment)-1]
			}
			if !overlong {
				if len(line)+len(fragment) > maxSearchLineBytes {
					overlong = true
					b.lineTruncated = true
					line = line[:0]
				} else {
					line = append(line, fragment...)
				}
			}
			if lastWasNewline || readErr == io.EOF {
				if !overlong && (!partialFile || lastWasNewline) {
					if err := process(line); err != nil {
						return true, err
					}
				}
				line = line[:0]
				overlong = false
				if lastWasNewline {
					lineNo++
				}
			}
		}
		if readErr == io.EOF {
			// ReadSlice can return an empty fragment after a buffer-full
			// fragment; finish that unterminated line too.
			if len(line) > 0 && !overlong && !partialFile {
				if err := process(line); err != nil {
					return true, err
				}
			}
			break
		}
		if readErr != nil && readErr != bufio.ErrBufferFull {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return true, ctxErr
			}
			b.fileTruncated = true
			break
		}
	}
	// Match strings.Split's historical treatment of empty files and a final
	// newline: both produce one final empty line for grep's ^$ pattern.
	if (!sawBytes || lastWasNewline) && !partialFile {
		if err := process(nil); err != nil {
			return true, err
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return true, ctxErr
	}
	return true, nil
}

func normalizeMaxResults(n int) int {
	if n <= 0 {
		return maxSearchResults
	}
	if n > maxSearchResults {
		return maxSearchResults
	}
	return n
}

const outputTruncatedMarker = "\n[Results truncated: output byte budget reached.]"

func truncateUTF8(s string, maxBytes int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !isUTF8Boundary(s, cut) {
		cut--
	}
	return s[:cut]
}

func isUTF8Boundary(s string, i int) bool {
	return i == 0 || i == len(s) || s[i]&0xc0 != 0x80
}

// renderSearchOutput bounds final output while retaining room for a visible
// marker if any result or note has to be omitted.
func renderSearchOutput(lines, notes []string, empty string) string {
	if len(lines) == 0 {
		lines = []string{empty}
	}
	var b strings.Builder
	appendLine := func(line string) bool {
		line = strings.ToValidUTF8(line, "\uFFFD")
		sep := ""
		if b.Len() > 0 {
			sep = "\n"
		}
		available := maxSearchOutputBytes - b.Len() - len(sep)
		if len(line) > available {
			if available > 0 {
				b.WriteString(sep)
				b.WriteString(truncateUTF8(line, available))
			}
			return false
		}
		b.WriteString(sep)
		b.WriteString(line)
		return true
	}
	truncated := false
	for _, line := range lines {
		if !appendLine(line) {
			truncated = true
			break
		}
	}
	if !truncated {
		for _, note := range notes {
			if !appendLine(note) {
				truncated = true
				break
			}
		}
	}
	if truncated {
		return truncateUTF8(b.String(), maxSearchOutputBytes-len(outputTruncatedMarker)) + outputTruncatedMarker
	}
	return b.String()
}

func budgetNotes(b *searchBudget) []string {
	var notes []string
	if b.fileTruncated {
		notes = append(notes, "Search incomplete: one or more files exceeded the 8 MiB per-file scan budget.")
	}
	if b.totalTruncated {
		notes = append(notes, "Search incomplete: the 64 MiB total file-scan budget was reached.")
	}
	if b.lineTruncated {
		notes = append(notes, "Search incomplete: lines exceeding 256 KiB were skipped.")
	}
	return notes
}
