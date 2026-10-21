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
	"path/filepath"
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
	// argsOf canonicalises the reference's AbsolutePath / StartLine /
	// EndLine / ContentOffset keys onto the struct tags above.
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	// Cycle 293: the 1.4.4 read_file declaration calls the resume offset
	// ByteOffset (view_file called it ContentOffset). Both address the raw
	// content, so the newer name wins only when the older one is absent.
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
	// Cycle truncated-webm-rejection — AGY 1.3.4 changelog line 42:
	// reject .webm files that look like a header-only or truncated
	// recording at read-time. Without this gate, the agent attaches a
	// malformed file and every later request in the conversation
	// dies trying to decode it. The check runs on raw bytes and never
	// hits the network.
	if IsWebMPath(p.Path) {
		if err := probeWebM(data); err != nil {
			return nil, err
		}
	}
	// Cycle 293: the reference's read_file result is not raw content. A
	// captured turn returns
	//
	//	Created At: 2026-10-21T22:54:50+08:00
	//	Completed At: 2026-10-21T22:54:50+08:00
	//	Showing lines 1-3 of 3
	//	1: line one
	//	2: line two
	//	3: line three
	//
	// and an empty file is "File is empty" with no Showing line.
	//
	// The 1.4.4 declaration promises "at most 800 lines or 46080 bytes" per
	// read. Both caps are 【一手】 (docs/cycles/cycle-293-model-tool-surface):
	//
	//   - 800 lines: a 900-line file read with no range reports
	//     "Showing lines 1-800 of 900"; StartLine=100 reports 100-899 (a
	//     window of 800), and a StartLine past EOF clamps to the last line
	//     ("Showing lines 3-3 of 3" on a 3-line file).
	//   - 46080 bytes: the cap counts the *numbered* text, not the raw file.
	//     A 100491-byte numbered range returns exactly 46080 bytes of it and
	//     says so before the body:
	//
	//	Partial read (lines 1-100 of 100). Showing bytes 0-46080 of 100491
	//	in this line range, truncated at the 46080-byte limit. To read the
	//	rest of this line range, call this tool again with the same
	//	StartLine and EndLine and ByteOffset=46080.
	//
	//     ByteOffset is an offset into that numbered text (a continuation
	//     starts mid-line, unnumbered, exactly where the previous read
	//     stopped). The "of N" total is the numbered length of the whole
	//     selected range, which is why it exceeds the file size.
	const (
		maxReadLines = 800
		maxReadBytes = 46080
	)
	raw := string(data)
	lines := strings.Split(raw, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)
	if total == 0 {
		return resultStamp(start) + "File is empty", nil
	}
	first, last := 1, total
	if p.StartLine > 1 {
		first = p.StartLine
	}
	if first > total {
		first = total
	}
	if p.EndLine > 0 && p.EndLine < last {
		last = p.EndLine
	}
	if last < first {
		last = first
	}
	if last-first+1 > maxReadLines {
		last = first + maxReadLines - 1
	}
	numbered := make([]string, 0, last-first+1)
	for i := first; i <= last; i++ {
		numbered = append(numbered, fmt.Sprintf("%d: %s", i, lines[i-1]))
	}
	body := strings.Join(numbered, "\n")
	if len(body) > maxReadBytes {
		off := p.ContentOffset
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
	const (
		maxViewLines = 800
		maxViewBytes = 46080
	)
	raw := string(data)
	// Unlike read_file, the trailing empty line is part of the file: a
	// "l1\nl2\nl3\nl4\n" file reports Total Lines: 5.
	lines := strings.Split(raw, "\n")
	totalLines := len(lines)
	totalBytes := len(raw)

	first, last := 1, totalLines
	if p.StartLine > 0 {
		first = p.StartLine
	}
	if p.EndLine > 0 {
		last = p.EndLine
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
		if p.EndLine > 0 && p.StartLine == 0 {
			// EndLine only: show the previous 800 lines.
			first = last - maxViewLines + 1
		} else {
			last = first + maxViewLines - 1
		}
	}
	rangeRaw := strings.Join(lines[first-1:last], "\n")

	off := p.ContentOffset
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
	// Without an offset the numbers are the file's real line numbers. With
	// one, the reference renumbers the returned slice from 1.
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
	return b.String(), nil
}

// resultStamp renders the Created At / Completed At header the reference puts
// in front of every tool result (【一手】 captured for run_command in cycle 275
// and for read_file / write_file / edit_file in cycle 293). There is no blank
// line between the header and the body for these three tools; run_command's
// extra blank line is part of its own result text.
func resultStamp(start time.Time) string {
	return "Created At: " + start.Format(time.RFC3339) + "\nCompleted At: " + time.Now().Format(time.RFC3339) + "\n"
}

// CreateFile — Source: tools.create_file (v4 32 #14). The new primary
// file-write tool. Renamed from 1.3.3 write_file to match v4 naming.
type CreateFile struct{ Workspace string }

func (c *CreateFile) Name() string { return "create_file" }
func (c *CreateFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}
func (c *CreateFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	// Cycle 275: workspacePath, so an absolute TargetFile is accepted inside
	// the workspace and refused outside it. filepath.Join silently turned
	// "/etc/x" into "<ws>/etc/x".
	full, err := workspacePath(c.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	// Cycle 293: the reference refuses to clobber an existing file unless
	// Overwrite=true, and returns the refusal as a normal tool result (not an
	// error) — 【一手】:
	//
	//	Encountered error in tool execution: /tmp/x already exists. To
	//	overwrite it, retry with Overwrite=true. To edit it, view its
	//	contents first and use a code edit tool. Otherwise, choose a
	//	different file name
	if _, statErr := os.Stat(full); statErr == nil && !p.Overwrite {
		return resultStamp(start) + fmt.Sprintf("Encountered error in tool execution: %s already exists. To overwrite it, retry with Overwrite=true. To edit it, view its contents first and use a code edit tool. Otherwise, choose a different file name", full), nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(full, []byte(p.Content), 0644); err != nil {
		return nil, err
	}
	return resultStamp(start) + "Created file.", nil
}

// WriteFile — Alias of CreateFile. The original 1.3.3 name for the file-
// write tool, preserved as an alias struct so that 1.3.3 callers that
// still invoke "write_file" keep working after the v4 rename.
type WriteFile struct{ Workspace string }

func (w *WriteFile) Name() string { return "write_file" }
func (w *WriteFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}
func (w *WriteFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	// Cycle 293: the 1.4.4 write_file declaration carries Append / Overwrite
	// alongside Path / Content. Append is honoured (a captured Append=true
	// turn grew the file) and still reports "Created file."; Overwrite gates
	// the clobber, which CreateFile enforces.
	var p struct {
		Append bool `json:"append"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Append {
		start := time.Now()
		if _, err := (&AppendFile{Workspace: w.Workspace}).Run(ctx, args); err != nil {
			return nil, err
		}
		return resultStamp(start) + "Created file.", nil
	}
	// Thin alias: delegate to CreateFile (same backing implementation).
	return (&CreateFile{Workspace: w.Workspace}).Run(ctx, args)
}

// EditFile — Source: tools.edit_file. Backward-compat (not in v4 32 list,
// but kept for 1.3.3 callers). Applies a search/replace edit.
type EditFile struct{ Workspace string }

func (e *EditFile) Name() string { return "edit_file" }
func (e *EditFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"oldString":{"type":"string"},"newString":{"type":"string"}},"required":["path","oldString","newString"]}`)
}
func (e *EditFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Path        string `json:"path"`
		OldString   string `json:"oldString"`
		NewString   string `json:"newString"`
		Target      string `json:"target"`
		Replacement string `json:"replacement"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	// Cycle 293: the 1.4.4 edit_file declaration names the two halves Target
	// and Replacement (Target is "the exact string to be replaced"). Accept
	// both spellings so reference-shaped and older calls share one executor.
	if p.OldString == "" {
		p.OldString = p.Target
	}
	if p.NewString == "" {
		p.NewString = p.Replacement
	}
	full, err := workspacePath(e.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	content := string(data)
	if p.OldString == "" {
		return nil, fmt.Errorf("edit_file: Target is required")
	}
	if !strings.Contains(content, p.OldString) {
		// Cycle 293: a Target that does not occur is a normal result, not an
		// error — 【一手】 captured from 1.4.4:
		//
		//	Encountered error in tool execution: target content not found in
		//	the file; the Target must exactly match existing file text,
		//	including whitespace
		return resultStamp(start) + "Encountered error in tool execution: target content not found in the file; the Target must exactly match existing file text, including whitespace", nil
	}
	// Cycle 293: the reference's result text (【一手】):
	//
	//	Made 1 replacement in /tmp/f.txt starting at line 1 (+1/-1 lines).
	//
	// The +N/-M halves are the line counts of Replacement and Target; the
	// starting line is the 1-indexed line the first match begins on. Only the
	// first occurrence is replaced, so the count is always 1.
	line := strings.Count(content[:strings.Index(content, p.OldString)], "\n") + 1
	updated := strings.Replace(content, p.OldString, p.NewString, 1)
	if err := os.WriteFile(full, []byte(updated), 0644); err != nil {
		return nil, err
	}
	return resultStamp(start) + fmt.Sprintf("Made 1 replacement in %s starting at line %d (+%d/-%d lines).",
		full, line, countTextLines(p.NewString), countTextLines(p.OldString)), nil
}

// countTextLines counts the lines a literal spans, ignoring one trailing
// newline (the edit result's +N/-M halves are formatted this way: a one-line
// Target is "1", not "2").
func countTextLines(s string) int {
	if s == "" {
		return 0
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Count(s, "\n") + 1
}

// AppendFile — Source: tools.append_file. Backward-compat (not in v4 32
// list, but kept for 1.3.3 callers).
type AppendFile struct{ Workspace string }

func (a *AppendFile) Name() string { return "append_file" }
func (a *AppendFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}
func (a *AppendFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := workspacePath(a.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	_, err = f.WriteString(p.Content)
	return nil, err
}

// DeleteFile — Source: tools.delete_file (v4 32 #20). The only fs tool
// that survived the v4 rename with the same name.
type DeleteFile struct{ Workspace string }

func (d *DeleteFile) Name() string { return "delete_file" }
func (d *DeleteFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (d *DeleteFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := workspacePath(d.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	return nil, os.Remove(full)
}

// RenameFile — Source: tools.rename_file. Backward-compat (not in v4 32
// list, but kept for 1.3.3 callers).
type RenameFile struct{ Workspace string }

func (r *RenameFile) Name() string { return "rename_file" }
func (r *RenameFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"]}`)
}
func (r *RenameFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	from, err := workspacePath(r.Workspace, p.From)
	if err != nil {
		return nil, err
	}
	to, err := workspacePath(r.Workspace, p.To)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return nil, err
	}
	return nil, os.Rename(from, to)
}

// CopyFile — Source: tools.copy_file. Backward-compat (not in v4 32 list,
// but kept for 1.3.3 callers).
type CopyFile struct{ Workspace string }

func (c *CopyFile) Name() string { return "copy_file" }
func (c *CopyFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"]}`)
}
func (c *CopyFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	from, err := workspacePath(c.Workspace, p.From)
	if err != nil {
		return nil, err
	}
	to, err := workspacePath(c.Workspace, p.To)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return nil, err
	}
	return nil, os.WriteFile(to, data, 0644)
}

// ListDir — Source: tools.list_dir (v4 32 #12). Renamed from
// 1.3.3 list_directory to match v4 naming.
type ListDir struct{ Workspace string }

func (l *ListDir) Name() string { return "list_dir" }
func (l *ListDir) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"recursive":{"type":"boolean"}}}`)
}
func (l *ListDir) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	// Cycle 275: readPath, so an absolute DirectoryPath is listed instead of
	// being joined onto the workspace ("/ws" + "/etc" == "/ws/etc").
	full, err := readPath(l.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	if p.Recursive {
		var entries []string
		err := filepath.Walk(full, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if path == full {
				return nil
			}
			rel, _ := filepath.Rel(full, path)
			if info.IsDir() {
				entries = append(entries, rel+"/")
			} else {
				entries = append(entries, rel)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return entries, nil
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	return out, nil
}
