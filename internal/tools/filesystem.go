// File-system tools. Source: google3/third_party/jetski/agent/tools.
//
// Cycle 29 — aligned with v4 antigravity-recovered binary 32-tool CLI surface.
// Nine fs tools registered (8 primary + 1 alias struct), all matching the
// v4 binary --tools listing:
//
//   - read_file          (kept, backward-compat, was original 1.3.3)
//   - edit_file          (kept, backward-compat, was original 1.3.3)
//   - append_file        (kept, backward-compat, was original 1.3.3)
//   - rename_file        (kept, backward-compat, was original 1.3.3)
//   - copy_file          (kept, backward-compat, was original 1.3.3)
//   - delete_file        (kept, in v4 32 list #20)
//   - list_dir           (renamed from list_directory, v4 32 list #12)
//   - create_file        (renamed from write_file, v4 32 list #14)
//
// Plus one alias struct that preserves the legacy "write_file" name (was
// the original 1.3.3 fs tool). The alias is implemented as a thin wrapper
// that delegates to CreateFile.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// viewFileDescription is the reference's 1.4.x prose for view_file (一手:
// captured request).
const viewFileDescription = "View the contents of a file from the local filesystem. This tool supports text files and following binary files: image, pdf, video, audio.\n" +
	"Text file usage:\n" +
	"- The lines of the file are 1-indexed\n" +
	"- You can view at most 800 lines at a time\n" +
	"- Specify StartLine and EndLine to view the lines of the file using slice notation:\n" +
	"  - Omit both to view the entire file, or the first 800 lines of the file, whichever is smaller.\n" +
	"  - Specify StartLine only to view the remaining lines of the file, or the next 800 lines, whichever is smaller\n" +
	"  - Specify EndLine only to view the remaining preceding lines of the file, or the previous 800 lines, whichever is smaller\n" +
	"  - Specify both to view a precise line range. This range must be smaller than 800 lines or only the first 800 lines of the range will be shown.\n" +
	"- Content is limited to 46080 bytes per view. If content is truncated, use the ContentOffset parameter to view the remaining content\n" +
	"Binary file usage:\n" +
	"- Do not provide StartLine or EndLine arguments, this tool always returns the entire file\n" +
	"- Files larger than 100 MB cannot be viewed."

// viewFileSchema is the reference's 1.4.x declaration for view_file (一手:
// copied from a captured request body, with Google's OBJECT / STRING /
// INTEGER upper-case type names lowered to JSON-Schema's). The key fact,
// stated in the declaration itself, is that AbsolutePath "Must be an absolute
// path" — the clone used to reject exactly that (see readPath).
var viewFileSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"AbsolutePath":{"type":"string","description":"Path to file to view. Must be an absolute path."},` +
	`"ContentOffset":{"type":"integer","description":"Optional. Byte offset into the content. Use this to view content beyond the initial byte limit when the tool output indicates content was truncated."},` +
	`"EndLine":{"type":"integer","description":"Optional. Endline to view, 1-indexed, inclusive. When specified, this value must be greater than or equal to StartLine."},` +
	`"StartLine":{"type":"integer","description":"Optional. Startline to view, 1-indexed, inclusive. When specified, this value must be less than or equal to EndLine."},` +
	`"toolAction":{"type":"string","description":"Brief 2-5 word phrase in -ing form describing the specific action."},` +
	`"toolSummary":{"type":"string","description":"Brief 2-5 word noun phrase describing the specific task."}` +
	`},"required":["AbsolutePath","toolSummary","toolAction"]}`)

// ReadFile — Source: tools.read_file. Backward-compat (not in v4 32 list,
// but kept for recovered-build callers that still invoke read_file). The
// catalog advertises it as view_file, which is what the model calls.
type ReadFile struct{ Workspace string }

func (r *ReadFile) Name() string { return "read_file" }
func (r *ReadFile) Schema() json.RawMessage {
	return viewFileSchema
}
func clampReadFileRange(total, startLine, endLine int) (int, int) {
	const maxReadLines = 800
	first, last := 1, total
	if startLine > 1 {
		first = startLine
	}
	if first > total {
		first = total
	}
	if endLine > 0 && endLine < last {
		last = endLine
	}
	if last < first {
		last = first
	}
	if last-first+1 > maxReadLines {
		last = first + maxReadLines - 1
	}
	return first, last
}

func formatReadFileResult(start time.Time, data []byte, startLine, endLine, byteOffset int) (string, error) {
	const maxReadBytes = 46080
	raw := string(data)
	lines := strings.Split(raw, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)
	if total == 0 {
		return resultStamp(start) + "File is empty", nil
	}
	first, last := clampReadFileRange(total, startLine, endLine)
	numbered := make([]string, 0, last-first+1)
	for i := first; i <= last; i++ {
		numbered = append(numbered, fmt.Sprintf("%d: %s", i, lines[i-1]))
	}
	body := strings.Join(numbered, "\n")
	if len(body) > maxReadBytes {
		off := byteOffset
		if off < 0 {
			off = 0
		}
		if off > len(body) {
			off = len(body)
		}
		end := off + maxReadBytes
		if end > len(body) {
			end = len(body)
		}
		note := fmt.Sprintf("Partial read (lines %d-%d of %d). Showing bytes %d-%d of %d in this line range, truncated at the %d-byte limit. To read the rest of this line range, call this tool again with the same StartLine and EndLine and ByteOffset=%d.\n",
			first, last, total, off, end, len(body), maxReadBytes, end)
		return resultStamp(start) + note + body[off:end], nil
	}
	return resultStamp(start) + fmt.Sprintf("Showing lines %d-%d of %d\n%s", first, last, total, body), nil
}

func (r *ReadFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Path          string `json:"path"`
		StartLine     int    `json:"start_line"`
		EndLine       int    `json:"end_line"`
		ContentOffset int    `json:"content_offset"`
		ByteOffset    int    `json:"byte_offset"`
		MaxBytes      int    `json:"max_bytes"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.ContentOffset == 0 {
		p.ContentOffset = p.ByteOffset
	}
	full, err := readPath(r.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if IsWebMPath(p.Path) {
		if err := probeWebM(data); err != nil {
			return nil, err
		}
	}
	if IsImagePath(p.Path) {
		if err := probeImage(p.Path, data); err != nil {
			return nil, err
		}
	}
	return formatReadFileResult(start, data, p.StartLine, p.EndLine, p.ContentOffset)
}

// ViewFile is the view_file executor — the tool custom agents get (the 1.4.4
// seven-declaration surface) and the 60-name catalogue's name for a read.
//
// Its result format is NOT read_file's. 【一手】 captured against agy 1.4.4 via
// a custom agent (docs/cycles/cycle-293-model-tool-surface):
//
//	Created At: …
//	Completed At: …
//	File Path: `file:///tmp/a.txt`
//	Total Lines: 5
//	Total Bytes: 12
//	Showing lines 1 to 5
//	The following code has been modified to include a line number before every line, in the format: <line_number>: <original_line>. Please note that any changes targeting the original code should remove the line number, colon, and leading space.
//	1: l1
//	…
//	5:
//	The above content shows the entire, complete file contents of the requested file.
//
// Total Lines counts the empty line after a trailing newline (5, not 4), and
// a partial view closes with either the "does NOT show the entire file
// contents" sentence or, when the 46080-byte cap bit, "The above content was
// truncated because individual lines are very long. Only a portion of the
// content is shown."
type ViewFile struct{ Workspace string }

func (v *ViewFile) Name() string            { return "view_file" }
func (v *ViewFile) Description() string     { return viewFileDescription }
func (v *ViewFile) Schema() json.RawMessage { return viewFileSchema }

func viewFileRange(lines []string, totalLines, startLine, endLine int) (first, last int, rangeRaw string) {
	const maxViewLines = 800
	first, last = 1, totalLines
	if startLine > 0 {
		first = startLine
	}
	if endLine > 0 {
		last = endLine
	}
	if first > totalLines {
		first = totalLines
	}
	if first < 1 {
		first = 1
	}
	if last > totalLines {
		last = totalLines
	}
	if last < first {
		last = first
	}
	if last-first+1 > maxViewLines {
		if endLine > 0 && startLine == 0 {
			first = last - maxViewLines + 1
		} else {
			last = first + maxViewLines - 1
		}
	}
	return first, last, strings.Join(lines[first-1:last], "\n")
}

func formatViewFileBody(full, rangeRaw string, totalLines, totalBytes, first, last, off int, start time.Time) string {
	const maxViewBytes = 46080
	if off < 0 {
		off = 0
	}
	if off > len(rangeRaw) {
		off = len(rangeRaw)
	}
	end := off + maxViewBytes
	if end > len(rangeRaw) {
		end = len(rangeRaw)
	}
	slice := rangeRaw[off:end]
	truncated := end < len(rangeRaw)

	var b strings.Builder
	b.WriteString(resultStamp(start))
	fmt.Fprintf(&b, "File Path: `file://%s`\nTotal Lines: %d\nTotal Bytes: %d\nShowing lines %d to %d\n",
		full, totalLines, totalBytes, first, last)
	if truncated {
		fmt.Fprintf(&b, "Content truncated: showing bytes %d-%d of %d. To see more, call this tool again with the same line range and ContentOffset=%d.\n",
			off, end, len(rangeRaw), end)
	}
	b.WriteString("The following code has been modified to include a line number before every line, in the format: <line_number>: <original_line>. Please note that any changes targeting the original code should remove the line number, colon, and leading space.\n")
	num := first
	if off > 0 {
		num = 1
	}
	frags := strings.Split(slice, "\n")
	for i, frag := range frags {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%d: %s", num+i, frag)
	}
	if truncated {
		b.WriteString("\nThe above content was truncated because individual lines are very long. Only a portion of the content is shown.\n")
	} else if off == 0 && first == 1 && last == totalLines {
		b.WriteString("\nThe above content shows the entire, complete file contents of the requested file.\n")
	} else {
		b.WriteString("\nThe above content does NOT show the entire file contents. If you need to view any lines of the file which were not shown to complete your task, call this tool again to view those lines.\n")
	}
	return b.String()
}

func (v *ViewFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Path          string `json:"path"`
		StartLine     int    `json:"start_line"`
		EndLine       int    `json:"end_line"`
		ContentOffset int    `json:"content_offset"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := readPath(v.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if IsWebMPath(p.Path) {
		if err := probeWebM(data); err != nil {
			return nil, err
		}
	}
	if IsImagePath(p.Path) {
		if err := probeImage(p.Path, data); err != nil {
			return nil, err
		}
	}
	raw := string(data)
	lines := strings.Split(raw, "\n")
	first, last, rangeRaw := viewFileRange(lines, len(lines), p.StartLine, p.EndLine)
	return formatViewFileBody(full, rangeRaw, len(lines), len(raw), first, last, p.ContentOffset, start), nil
}
