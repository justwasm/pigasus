package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ReplaceSpec is one entry of multi_replace_file_content.
type ReplaceSpec struct {
	OldString     string `json:"oldString"`
	NewString     string `json:"newString"`
	AllowMultiple bool   `json:"allowMultiple"`
}

// MultiReplaceFileContent — Source: tools.multi_replace_file_content.
type MultiReplaceFileContent struct{ Workspace string }

func (m *MultiReplaceFileContent) Name() string { return "multi_replace_file_content" }
func (m *MultiReplaceFileContent) Schema() json.RawMessage {
	return obj(`"path":{"type":"string"},`+
		`"replacements":{"type":"array","description":"Literal replacements applied in order.","items":{"type":"object","properties":{"oldString":{"type":"string"},"newString":{"type":"string"},"allowMultiple":{"type":"boolean"}},"required":["oldString","newString"]}}`,
		"path", "replacements")
}
func (m *MultiReplaceFileContent) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path         string        `json:"path"`
		Replacements []ReplaceSpec `json:"replacements"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := filePath(m.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	content := string(data)
	for i, r := range p.Replacements {
		if r.OldString == "" {
			return nil, fmt.Errorf("multi_replace_file_content: replacement %d has an empty oldString", i)
		}
		n := strings.Count(content, r.OldString)
		if n == 0 {
			return nil, fmt.Errorf("multi_replace_file_content: replacement %d: oldString not found in %s", i, p.Path)
		}
		limit := 1
		if r.AllowMultiple {
			limit = -1
		}
		content = strings.Replace(content, r.OldString, r.NewString, limit)
	}
	return nil, os.WriteFile(full, []byte(content), 0644)
}

// SedFile — Source: tools.sed_file.
type SedFile struct{ Workspace string }

func (s *SedFile) Name() string { return "sed_file" }
func (s *SedFile) Schema() json.RawMessage {
	return obj(`"path":{"type":"string"},`+
		`"pattern":{"type":"string","description":"Go regular expression."},`+
		`"replacement":{"type":"string","description":"Replacement text; $1..$9 expand capture groups."},`+
		`"start_line":{"type":"integer","description":"1-based first line of the range (inclusive)."},`+
		`"end_line":{"type":"integer","description":"1-based last line of the range (inclusive)."},`+
		`"global":{"type":"boolean","description":"Replace every match on a line instead of only the first."}`,
		"path", "pattern", "replacement")
}

func applySedToLines(lines []string, re *regexp.Regexp, replacement string, startLine, endLine int, global bool) (int, []string) {
	changed := 0
	for i := range lines {
		n := i + 1
		if startLine > 0 && n < startLine {
			continue
		}
		if endLine > 0 && n > endLine {
			continue
		}
		if !re.MatchString(lines[i]) {
			continue
		}
		if global {
			replaced := re.ReplaceAllString(lines[i], replacement)
			if replaced != lines[i] {
				changed++
			}
			lines[i] = replaced
			continue
		}
		lines[i] = replaceFirst(re, lines[i], replacement)
		changed++
	}
	return changed, lines
}

func (s *SedFile) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path        string `json:"path"`
		Pattern     string `json:"pattern"`
		Replacement string `json:"replacement"`
		StartLine   int    `json:"start_line"`
		EndLine     int    `json:"end_line"`
		Global      bool   `json:"global"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return nil, fmt.Errorf("sed_file: invalid pattern: %w", err)
	}
	full, err := filePath(s.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	hasTrailingNewline := strings.HasSuffix(string(data), "\n")
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	changed, updated := applySedToLines(lines, re, p.Replacement, p.StartLine, p.EndLine, p.Global)
	if changed == 0 {
		return nil, fmt.Errorf("sed_file: pattern not found in %s", p.Path)
	}
	out := strings.Join(updated, "\n")
	if hasTrailingNewline {
		out += "\n"
	}
	return nil, os.WriteFile(full, []byte(out), 0644)
}

func replaceFirst(re *regexp.Regexp, line, replacement string) string {
	loc := re.FindStringSubmatchIndex(line)
	if loc == nil {
		return line
	}
	return string(re.ExpandString([]byte(line[:loc[0]]), replacement, line, loc)) + line[loc[1]:]
}

// ViewContentChunk — Source: tools.view_content_chunk.
type ViewContentChunk struct{ Workspace string }

func (v *ViewContentChunk) Name() string { return "view_content_chunk" }
func (v *ViewContentChunk) Schema() json.RawMessage {
	return obj(`"path":{"type":"string"},`+
		`"offset":{"type":"integer","description":"0-based byte offset to start at."},`+
		`"length":{"type":"integer","description":"Number of bytes to return."}`,
		"path")
}
func (v *ViewContentChunk) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Length int    `json:"length"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := filePath(v.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Offset > len(data) {
		p.Offset = len(data)
	}
	end := len(data)
	if p.Length > 0 && p.Offset+p.Length < end {
		end = p.Offset + p.Length
	}
	return fmt.Sprintf("[bytes %d-%d of %d]\n%s", p.Offset, end, len(data), string(data[p.Offset:end])), nil
}

// NotebookEdit — Source: tools.notebook_edit.
type NotebookEdit struct{ Workspace string }

func (n *NotebookEdit) Name() string { return "notebook_edit" }
func (n *NotebookEdit) Schema() json.RawMessage {
	return obj(`"path":{"type":"string"},`+
		`"cell_index":{"type":"integer","description":"0-based index into the notebook's cells."},`+
		`"source":{"type":"string","description":"New cell source."},`+
		`"cell_type":{"type":"string","description":"Set the cell type: code or markdown."}`,
		"path", "cell_index", "source")
}
func (n *NotebookEdit) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path      string `json:"path"`
		CellIndex int    `json:"cell_index"`
		Source    string `json:"source"`
		CellType  string `json:"cell_type"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := filePath(n.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	var nb map[string]any
	if err := json.Unmarshal(data, &nb); err != nil {
		return nil, fmt.Errorf("notebook_edit: %s is not a notebook: %w", p.Path, err)
	}
	cells, _ := nb["cells"].([]any)
	if p.CellIndex < 0 || p.CellIndex >= len(cells) {
		return nil, fmt.Errorf("notebook_edit: cell_index %d out of range (notebook has %d cells)", p.CellIndex, len(cells))
	}
	cell, _ := cells[p.CellIndex].(map[string]any)
	if cell == nil {
		return nil, fmt.Errorf("notebook_edit: cell %d is malformed", p.CellIndex)
	}
	cell["source"] = splitSourceLines(p.Source)
	if p.CellType != "" {
		cell["cell_type"] = p.CellType
	}
	cells[p.CellIndex] = cell
	nb["cells"] = cells
	out, err := json.Marshal(nb)
	if err != nil {
		return nil, err
	}
	return nil, os.WriteFile(full, out, 0644)
}

func splitSourceLines(src string) []string {
	if src == "" {
		return []string{}
	}
	lines := strings.SplitAfter(src, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// NotebookExecution — Source: tools.notebook_execution.
type NotebookExecution struct{ Workspace string }

func (n *NotebookExecution) Name() string { return "notebook_execution" }
func (n *NotebookExecution) Schema() json.RawMessage {
	return obj(`"path":{"type":"string"},`+
		`"cell_index":{"type":"integer","description":"0-based index of the cell to execute."},`+
		`"timeout":{"type":"integer","description":"Seconds before the execution is killed."}`,
		"path")
}
func (n *NotebookExecution) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path      string `json:"path"`
		CellIndex int    `json:"cell_index"`
		Timeout   int    `json:"timeout"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	full, err := filePath(n.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(full); err != nil {
		return nil, err
	}
	ctx2 := ctx
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx2, cancel = context.WithTimeout(ctx, secondsToDuration(p.Timeout))
		defer cancel()
	}
	return runNotebook(ctx2, n.Workspace, full)
}
