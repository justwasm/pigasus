// nonnetwork.go implements the advertised tools that can run entirely
// locally. Everything here is a real executor; the tools that need a Google
// service (search_web, read_url_content, generate_image, call_mcp,
// list_resources, read_resource) stay on ErrNotImplemented in catalog.go.
//
// Evidence for the behaviour implemented here:
//
//   - Tool NAMES are recovered from the converter family (catalog.go).
//   - The workspace path rule is recovered verbatim: the binary carries
//     `expected path %s to not start with /`. Cycle 275 showed that string is
//     NOT a blanket ban on absolute paths (the reference's own declarations
//     require them), so write tools accept absolute paths inside the
//     workspace and read tools accept any absolute path. Both refuse to
//     escape the workspace on a relative path.
//   - The headless interaction text is recovered: the binary's no-tool-call
//     reminder ends "Remember that the user cannot answer any questions."
//     so the interactive tools below report that the user cannot be asked
//     rather than fabricating an answer.
//
// The argument SCHEMAS are this clone's shapes. The reference ships them
// through a Cortex protobuf tool schema that is not in the decompiled set,
// so the field names below are reconstructed from each tool's documented
// behaviour and are graded 【推测】 in docs/cycles/cycle-98/. Name and
// user-visible behaviour match the reference; the argument encoding does not.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/subagentquestions"
)

// ---- workspace paths -------------------------------------------------------

// workspacePath resolves a model-supplied path for a WRITE-side tool.
// Cycle 275 — this used to reject EVERY absolute path with the recovered
// string above, generalised from one hit in the binary's string table. The
// reference does not behave that way: its view_file declaration says
// "AbsolutePath … Must be an absolute path", its write_to_file /
// replace_file_content declarations say "TargetFile … Must be an absolute
// path", and a live 1.4.2 turn read /Users/gear/Documents/GitHub/openagy/
// AGENTS.md, the ~/GitHub symlink spelling of it, and /etc/hosts (docs/
// cycles/cycle-275). The model therefore sends absolute paths and every one
// of them came back `(Failed)`.
//
// The rule now: an absolute path is accepted when it lands inside the
// workspace (compared after symlink resolution, so the two spellings of this
// checkout are one directory); a relative path may still not escape.
func workspacePath(ws, p string) (string, error) {
	if p == "" {
		return ws, nil
	}
	root := canonicalPath(ws)
	if filepath.IsAbs(p) {
		full := filepath.Clean(p)
		if !within(root, canonicalPath(full)) {
			return "", fmt.Errorf("expected path %s to not start with /", p)
		}
		return full, nil
	}
	full := filepath.Join(ws, p)
	// filepath.Join cleans the result, so an escaping ".." shows up as a
	// prefix mismatch against the cleaned workspace root.
	if !within(root, canonicalPath(full)) {
		return "", fmt.Errorf("path %s escapes the workspace", p)
	}
	return full, nil
}

// readPath resolves a model-supplied path for a READ-ONLY tool. The
// reference places no workspace restriction on reads — view_file read
// /etc/hosts on a live 1.4.2 turn — so an absolute path is used as-is.
// Relative paths keep the workspace-relative rule (and may not escape).
func readPath(ws, p string) (string, error) {
	if p == "" {
		return ws, nil
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return workspacePath(ws, p)
}

// canonicalPath resolves symlinks for the longest existing prefix of p. The
// same directory can be spelled two ways (~/GitHub is a symlink to
// ~/Documents/GitHub on this machine) and the reference accepts both, so
// containment has to be compared after resolution. A path that does not exist
// yet (create_file) resolves its existing ancestor and re-appends the rest.
func canonicalPath(p string) string {
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir, base := filepath.Split(p)
	if dir == "" || filepath.Clean(dir) == p {
		return p
	}
	return filepath.Join(canonicalPath(filepath.Clean(dir)), base)
}

// within reports whether p is at or below root.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// filePath resolves a path argument that must name a file, not a directory.
// It applies the same absolute-path and escape rules as workspacePath.
func filePath(ws, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	return workspacePath(ws, p)
}

// argsOf unmarshals tool arguments, tolerating a nil/empty payload.
//
// Cycle 133: the recovered wire format from the v1.3.6 reference
// uses capitalised JSON keys (`AbsolutePath`, `TargetFile`,
// `CommandLine`, `Query`, `Pattern`, `OldString`, `NewString`,
// `Content`, etc.). The cycle-72 tool catalog has been decoding
// only lowercase keys (`path`, `command`, `query`, `pattern`,
// `oldString`, `newString`, `content`) — so a perfectly valid call
// from the model would unmarshal with `Path == ""`, fall back to
// the workspace root, and fail at `os.ReadFile(<directory>)`,
// reporting `(Failed)` for what was a successful call.
//
// The fix is a key-canonicalisation pre-pass: walk the JSON object,
// convert every key from CamelCase (`AbsolutePath`) to snake_case
// (`absolute_path`), and unmarshal the rewritten bytes. The
// original raw bytes are never mutated; we copy into a fresh map.
// Arrays keep their structure; nested objects are recursively
// canonicalised. This is intentionally simple — it does not handle
// JSON-in-string values (which would need careful escaping), but
// the recovered wire format has no such cases.
func argsOf(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	canon, err := canonicaliseJSONKeys(raw)
	if err != nil {
		// If canonicalisation fails (e.g. a malformed payload),
		// fall back to the raw bytes — the unmarshal will produce
		// its own descriptive error and the tool caller will
		// surface it.
		return json.Unmarshal(raw, v)
	}
	return json.Unmarshal(canon, v)
}

// canonicaliseJSONKeys returns a copy of raw with every JSON
// object key converted from CamelCase to snake_case. Non-object
// payloads pass through unchanged. Nested objects are walked
// recursively; arrays keep their element order.
func canonicaliseJSONKeys(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	// Quick path: only objects need rewriting.
	if raw[0] != '{' {
		return raw, nil
	}
	var anyV any
	if err := json.Unmarshal(raw, &anyV); err != nil {
		return nil, err
	}
	canon := canonicaliseAny(anyV)
	return json.Marshal(canon)
}

// canonicaliseAny returns a deep copy of v with object keys
// converted to snake_case (and lowercased). Scalars and arrays
// pass through (with arrays recursed element-wise).
func canonicaliseAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[camelToSnake(k)] = canonicaliseAny(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = canonicaliseAny(vv)
		}
		return out
	default:
		return v
	}
}

// camelToSnake converts CamelCase / PascalCase to snake_case. The
// reference's wire format uses CamelCase keys
// (`AbsolutePath`, `TargetFile`, `CommandLine`, `Query`); the
// clone's struct tags use snake_case / lowercase (`absolute_path`,
// `path`, `command_line`, `query`). The transformation inserts an
// underscore before each uppercase letter that follows a lowercase
// letter or digit, then lowercases the whole string. Examples:
//
//	AbsolutePath  → absolute_path
//	Path          → path
//	CommandLine   → command_line
//	OldString     → old_string
//	NewString     → new_string
//	Content       → content
//
// Cycle 133 follow-up: the clone's struct tags use the short
// canonical names (`path`, `command`, `query`) — not snake_case.
// So we register a small known-aliases map that maps CamelCase
// wire keys down to the canonical struct tag. Unknown keys fall
// through to plain lowercase (so a struct tag of `pattern` still
// matches a wire key of `Pattern`).
func camelToSnake(s string) string {
	if s == "" {
		return s
	}
	if alias, ok := toolKeyAliases[s]; ok {
		return alias
	}
	// Fast path: already snake_case / lowercase.
	if !hasUpper(s) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s) + 4)
	for i, r := range s {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && unicode.IsLower(rune(s[i-1]))
			if prevLower {
				sb.WriteByte('_')
			}
			sb.WriteRune(unicode.ToLower(r))
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// toolKeyAliases maps the v1.3.6 wire-format CamelCase keys to
// the canonical struct tag the clone's tool catalog uses. The
// aliases were recovered by `strings ~/.local/bin/agy | grep` on
// the recovered tool-call arguments (cycle 99 / 100) and the
// recovered view_file arg shape (cycle 99). Adding a new alias is
// safer than renaming a struct tag because the tag is part of the
// JSON wire contract the off-line harness reads back.
var toolKeyAliases = map[string]string{
	// path variants → "path"
	"AbsolutePath":  "path",
	"absolute_path": "path",
	"TargetFile":    "path",
	"target_file":   "path",
	"FilePath":      "path",
	"file_path":     "path",
	"File":          "path",
	"file":          "path",
	"DirectoryPath": "path",
	"directory":     "path",
	"SearchPath":    "path",
	"search_path":   "path",

	// command variants → "command"
	"CommandLine":  "command",
	"command_line": "command",
	"Command":      "command",
	"Cmd":          "command",

	// oldString / newString — both already match the struct tag,
	// but include the wire-format capitalised forms.
	"OldString": "oldString",
	"oldString": "oldString",
	"NewString": "newString",
	"newString": "newString",

	// Cycle 275: the reference's write_to_file calls its payload
	// CodeContent and replace_file_content calls its two halves
	// TargetContent / ReplacementContent (TargetContent is documented as
	// "the exact string to be replaced"). Without these three the
	// executors would see an empty payload and write nothing / fail the
	// match.
	"CodeContent":         "content",
	"code_content":        "content",
	"TargetContent":       "oldString",
	"target_content":      "oldString",
	"ReplacementContent":  "newString",
	"replacement_content": "newString",

	// startLine / endLine (sed_file, view_file) — struct tag is
	// snake_case, so map every variant to "start_line" / "end_line".
	"StartLine": "start_line",
	"startLine": "start_line",
	"EndLine":   "end_line",
	"endLine":   "end_line",

	// maxBytes / maxLines — struct tag uses camelCase; map the
	// wire variants to the same.
	"MaxBytes": "maxBytes",
	"MaxLines": "maxLines",

	// query / pattern / url / prompt / goal — lowercase struct tags.
	"Query":      "query",
	"Pattern":    "pattern",
	"SearchTerm": "pattern",
	"Url":        "url",
	"URL":        "url",
	"Prompt":     "prompt",
	"Goal":       "goal",

	// global / recursive / include — lowercase struct tags.
	"Global":   "global",
	"Recurse":  "recursive",
	"Includes": "includes",

	// replaceAll / allowMultiple — camelCase struct tags.
	"ReplaceAll":    "replaceAll",
	"replaceAll":    "replaceAll",
	"AllowMultiple": "allowMultiple",
	"allowMultiple": "allowMultiple",

	// Cycle 293: manage_task's declaration names the kill target TaskId,
	// while the executor's field is id.
	"TaskId": "id",
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// ---- search ----------------------------------------------------------------

// GrepSearchTool — Source: tools.grep_search. Regex search over the workspace.
type GrepSearchTool struct{ Workspace string }

func (g *GrepSearchTool) Name() string { return "grep_search" }
func (g *GrepSearchTool) Schema() json.RawMessage {
	return obj(`"pattern":{"type":"string","description":"Go regular expression to search for."},`+
		`"path":{"type":"string","description":"Workspace-relative directory or file to search (default the workspace root)."},`+
		`"include":{"type":"string","description":"Glob the file name must match, e.g. *.go."},`+
		`"max_results":{"type":"integer","description":"Maximum number of matches to return."}`,
		"pattern")
}
func (g *GrepSearchTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Include    string `json:"include"`
		MaxResults int    `json:"max_results"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return nil, fmt.Errorf("grep_search: invalid pattern: %w", err)
	}
	root, err := workspacePath(g.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	limit := p.MaxResults
	if limit <= 0 {
		limit = 200
	}
	var out []string
	err = walkFiles(root, func(path string, rel string) error {
		if p.Include != "" {
			if ok, _ := doublestar.Match(p.Include, filepath.Base(path)); !ok {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !looksTextual(data) {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimRight(line, "\r")))
				if len(out) >= limit {
					return errStopWalk
				}
			}
		}
		return nil
	})
	if err != nil && err != errStopWalk {
		return nil, err
	}
	if len(out) == 0 {
		return "No files were searched, double check your search parameter", nil
	}
	return strings.Join(out, "\n"), nil
}

// FindByName — Source: tools.find_by_name. Glob match over file names.
type FindByName struct{ Workspace string }

func (f *FindByName) Name() string { return "find_by_name" }
func (f *FindByName) Schema() json.RawMessage {
	return obj(`"pattern":{"type":"string","description":"Glob matched against the file name, e.g. *.go. Use **/ to match any depth."},`+
		`"path":{"type":"string","description":"Workspace-relative directory to search (default the workspace root)."},`+
		`"max_results":{"type":"integer"}`,
		"pattern")
}
func (f *FindByName) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		MaxResults int    `json:"max_results"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		return nil, fmt.Errorf("find_by_name: pattern is required")
	}
	root, err := workspacePath(f.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	limit := p.MaxResults
	if limit <= 0 {
		limit = 200
	}
	var out []string
	matcher := globMatcher(p.Pattern)
	err = walkFiles(root, func(path string, rel string) error {
		if matcher(rel, filepath.Base(path)) {
			out = append(out, rel)
			if len(out) >= limit {
				return errStopWalk
			}
		}
		return nil
	})
	if err != nil && err != errStopWalk {
		return nil, err
	}
	if len(out) == 0 {
		return "No matches found.", nil
	}
	return strings.Join(out, "\n"), nil
}

// globMatcher compiles a glob that may contain **/ for any depth. A bare
// pattern (no slash) is matched against the base name, as find -name does.
// doublestar.Match handles all three branches natively: `**/foo.txt`
// already matches `foo.txt` at the root, so the prior branching is gone.
func globMatcher(pattern string) func(rel, base string) bool {
	if !strings.Contains(pattern, "/") {
		return func(_, base string) bool {
			ok, _ := doublestar.Match(pattern, base)
			return ok
		}
	}
	return func(rel, _ string) bool {
		ok, _ := doublestar.Match(pattern, rel)
		return ok
	}
}

// CodebaseSearch — Source: tools.codebase_search. The reference performs a
// semantic search server-side; this clone is offline, so it ranks files by a
// lexical term-frequency fallback. The tool NAME and the shape of the result
// list match; the ranking does not.
type CodebaseSearch struct{ Workspace string }

func (c *CodebaseSearch) Name() string { return "codebase_search" }
func (c *CodebaseSearch) Schema() json.RawMessage {
	return obj(`"query":{"type":"string","description":"Natural-language or keyword query."},`+
		`"path":{"type":"string","description":"Workspace-relative directory to search (default the workspace root)."},`+
		`"max_results":{"type":"integer"}`,
		"query")
}
func (c *CodebaseSearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query      string `json:"query"`
		Path       string `json:"path"`
		MaxResults int    `json:"max_results"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	terms := strings.Fields(strings.ToLower(p.Query))
	if len(terms) == 0 {
		return nil, fmt.Errorf("codebase_search: query is required")
	}
	root, err := workspacePath(c.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	type hit struct {
		rel    string
		score  int
		sample string
	}
	var hits []hit
	err = walkFiles(root, func(path string, rel string) error {
		data, err := os.ReadFile(path)
		if err != nil || !looksTextual(data) {
			return nil
		}
		lower := strings.ToLower(string(data))
		score := 0
		sample := ""
		for _, t := range terms {
			n := strings.Count(lower, t)
			if n == 0 {
				continue
			}
			score += n
			if sample == "" {
				for _, line := range strings.Split(string(data), "\n") {
					if strings.Contains(strings.ToLower(line), t) {
						sample = strings.TrimSpace(line)
						break
					}
				}
			}
		}
		if score > 0 {
			hits = append(hits, hit{rel: rel, score: score, sample: trimTo(sample, 160)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].rel < hits[j].rel
	})
	limit := p.MaxResults
	if limit <= 0 || limit > len(hits) {
		limit = len(hits)
	}
	var b strings.Builder
	for _, h := range hits[:limit] {
		fmt.Fprintf(&b, "%s (score %d): %s\n", h.rel, h.score, h.sample)
	}
	if b.Len() == 0 {
		return "No matches found.", nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// ---- file mutation ---------------------------------------------------------

// ReplaceSpec is one entry of multi_replace_file_content.
type ReplaceSpec struct {
	OldString     string `json:"oldString"`
	NewString     string `json:"newString"`
	AllowMultiple bool   `json:"allowMultiple"`
}

// MultiReplaceFileContent — Source: tools.multi_replace_file_content.
// Applies several literal replacements to one file, each at most once unless
// allowMultiple is set. All replacements are located before any is written, so
// a bad entry leaves the file untouched.
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

// SedFile — Source: tools.sed_file. Applies a line-range substitution, or a
// regex substitution over the whole file.
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
	changed := 0
	for i := range lines {
		n := i + 1
		if p.StartLine > 0 && n < p.StartLine {
			continue
		}
		if p.EndLine > 0 && n > p.EndLine {
			continue
		}
		if !re.MatchString(lines[i]) {
			continue
		}
		if p.Global {
			replaced := re.ReplaceAllString(lines[i], p.Replacement)
			if replaced != lines[i] {
				changed++
			}
			lines[i] = replaced
			continue
		}
		lines[i] = replaceFirst(re, lines[i], p.Replacement)
		changed++
	}
	if changed == 0 {
		return nil, fmt.Errorf("sed_file: pattern not found in %s", p.Path)
	}
	out := strings.Join(lines, "\n")
	if hasTrailingNewline {
		out += "\n"
	}
	return nil, os.WriteFile(full, []byte(out), 0644)
}

// replaceFirst expands only the first match, which is what sed does without /g.
func replaceFirst(re *regexp.Regexp, line, replacement string) string {
	loc := re.FindStringSubmatchIndex(line)
	if loc == nil {
		return line
	}
	return string(re.ExpandString([]byte(line[:loc[0]]), replacement, line, loc)) + line[loc[1]:]
}

// ViewContentChunk — Source: tools.view_content_chunk. Reads a byte range of a
// previously viewed file, for content too large to return in one call.
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

// ---- notebooks -------------------------------------------------------------

// NotebookEdit — Source: tools.notebook_edit. Replaces the source of one cell
// of an .ipynb file. The notebook is decoded and re-encoded so the rest of the
// document is preserved.
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

// splitSourceLines encodes a cell source the way nbformat does: a list of
// lines that each keep their trailing newline except the last.
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

// NotebookExecution — Source: tools.notebook_execution. Executes one cell of
// an .ipynb through nbconvert in a temporary copy, so the notebook on disk is
// not mutated by the run.
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
	// nbconvert executes a whole notebook; running a single cell in isolation
	// would lose the kernel state earlier cells established, so delegate to
	// the reference's own contract: execute the notebook and report the exit.
	ctx2 := ctx
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx2, cancel = context.WithTimeout(ctx, secondsToDuration(p.Timeout))
		defer cancel()
	}
	return runNotebook(ctx2, n.Workspace, full)
}

// ---- background commands ---------------------------------------------------

// CommandStatus — Source: tools.command_status.
type CommandStatus struct{}

func (c *CommandStatus) Name() string { return "command_status" }
func (c *CommandStatus) Schema() json.RawMessage {
	return obj(`"id":{"type":"string","description":"Identifier returned by run_command when background was set."}`, "id")
}
func (c *CommandStatus) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	t, ok := backgroundTasks.get(p.ID)
	if !ok {
		return nil, fmt.Errorf("command_status: unknown command id %q", p.ID)
	}
	return t.status(), nil
}

// SendCommandInput — Source: tools.send_command_input. Writes a line to the
// stdin of a background command.
type SendCommandInput struct{}

func (s *SendCommandInput) Name() string { return "send_command_input" }
func (s *SendCommandInput) Schema() json.RawMessage {
	return obj(`"id":{"type":"string"},`+
		`"input":{"type":"string","description":"Line written to the command's stdin."}`,
		"id", "input")
}
func (s *SendCommandInput) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		ID    string `json:"id"`
		Input string `json:"input"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	t, ok := backgroundTasks.get(p.ID)
	if !ok {
		return nil, fmt.Errorf("send_command_input: unknown command id %q", p.ID)
	}
	if err := t.writeStdin(p.Input + "\n"); err != nil {
		return nil, err
	}
	return t.status(), nil
}

// ManageTask — Source: tools.manage_task. Lists or kills background commands.
// The reference's fuller task model (subagents, scheduled triggers) is not
// reproduced; the local process table is what this clone can manage.
type ManageTask struct{}

func (m *ManageTask) Name() string { return "manage_task" }
func (m *ManageTask) Schema() json.RawMessage {
	return obj(`"action":{"type":"string","description":"list or kill."},`+
		`"id":{"type":"string","description":"Command id, required for kill."}`,
		"action")
}
func (m *ManageTask) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Action string `json:"action"`
		ID     string `json:"id"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	switch p.Action {
	case "list":
		all := backgroundTasks.list()
		if len(all) == 0 {
			// 【一手】 1.4.4 (standard and custom-agent turns):
			// "No background tasks are currently running."
			return resultStamp(start) + "No background tasks are currently running.", nil
		}
		var b strings.Builder
		for _, s := range all {
			b.WriteString(s)
			b.WriteByte('\n')
		}
		return resultStamp(start) + strings.TrimRight(b.String(), "\n"), nil
	case "kill":
		if p.ID == "" {
			return nil, fmt.Errorf("manage_task: id is required for kill")
		}
		t, ok := backgroundTasks.get(p.ID)
		if !ok {
			return nil, fmt.Errorf("manage_task: unknown command id %q", p.ID)
		}
		out, err := t.kill()
		if err != nil {
			return out, err
		}
		return resultStamp(start) + fmt.Sprint(out), nil
	}
	return nil, fmt.Errorf("manage_task: unknown action %q (want list or kill)", p.Action)
}

// ---- control flow ----------------------------------------------------------

// WaitTool — Source: tools.wait. Sleeps, bounded by the workspace's context.
type WaitTool struct{}

func (w *WaitTool) Name() string { return "wait" }
func (w *WaitTool) Schema() json.RawMessage {
	return obj(`"seconds":{"type":"number","description":"Seconds to wait."}`, "seconds")
}
func (w *WaitTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Seconds float64 `json:"seconds"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Seconds < 0 {
		return nil, fmt.Errorf("wait: seconds must not be negative")
	}
	if p.Seconds > 600 {
		return nil, fmt.Errorf("wait: seconds must not exceed 600")
	}
	timer := time.NewTimer(time.Duration(p.Seconds * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	return fmt.Sprintf("Waited %gs.", p.Seconds), nil
}

// FinishTool — Source: tools.finish. Signals that the task is complete; the loop
// treats a finish call as the terminal step.
type FinishTool struct{}

func (f *FinishTool) Name() string { return "finish" }
func (f *FinishTool) Schema() json.RawMessage {
	return obj(`"summary":{"type":"string","description":"Optional summary of what was completed."}`)
}
func (f *FinishTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Summary string `json:"summary"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Summary == "" {
		return "Task marked complete.", nil
	}
	return "Task marked complete: " + p.Summary, nil
}

// ScheduleTool — Source: tools.schedule. The reference registers a deferred or
// recurring prompt with the session's scheduler and returns immediately with a
// background-task acknowledgement (【一手】 1.4.4; see
// testdata/result_texts_1.4.4.txt). This clone has no scheduler backend yet, so
// it mints the task, writes the log file the reference names, and returns the
// reference's text verbatim. Nothing fires when the delay elapses; that gap is
// recorded in docs/cycles/cycle-293-model-tool-surface/deliverable.md.
type ScheduleTool struct {
	// ConversationID scopes the brain/ log path. The reference uses the
	// live conversation id; when a caller cannot supply one, the tool
	// falls back to the OPENAGY_CONVERSATION_ID env var and then to a
	// process-stable id.
	ConversationID string
	// Home overrides the antigravity home directory (brain/ lives under
	// it). Empty resolves $ANTIGRAVITY_HOME then ~/.gemini/antigravity-cli.
	Home string
}

func (s *ScheduleTool) Name() string { return "schedule" }
func (s *ScheduleTool) Schema() json.RawMessage {
	return obj(`"Prompt":{"type":"string","description":"The prompt to run when the schedule fires."},` +
		`"DurationSeconds":{"type":"integer","description":"One-shot delay in seconds."},` +
		`"CronExpression":{"type":"string","description":"Recurring schedule expression."},` +
		`"IsDaemon":{"type":"boolean","description":"Keep the schedule alive across turns."},` +
		`"MaxIterations":{"type":"integer","description":"Stop after this many runs."},` +
		`"TimerCondition":{"type":"string","description":"Optional condition that must hold before firing."}`)
}
func (s *ScheduleTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Prompt          string `json:"prompt"`
		DurationSeconds int    `json:"duration_seconds"`
		CronExpression  string `json:"cron_expression"`
		IsDaemon        bool   `json:"is_daemon"`
		MaxIterations   int    `json:"max_iterations"`
		TimerCondition  string `json:"timer_condition"`
		// Older clone spellings, kept for --agent transcripts.
		Command      string `json:"command"`
		DelaySeconds int    `json:"delay_seconds"`
		Cron         string `json:"cron"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Prompt == "" {
		p.Prompt = p.Command
	}
	if p.DurationSeconds == 0 {
		p.DurationSeconds = p.DelaySeconds
	}
	if p.CronExpression == "" {
		p.CronExpression = p.Cron
	}
	if p.Prompt == "" {
		return nil, fmt.Errorf("schedule: Prompt is required")
	}
	if p.DurationSeconds == 0 && p.CronExpression == "" {
		return nil, fmt.Errorf("schedule: set DurationSeconds or CronExpression")
	}

	conversationID := s.ConversationID
	if conversationID == "" {
		conversationID = os.Getenv("OPENAGY_CONVERSATION_ID")
	}
	if conversationID == "" {
		conversationID = processConversationID()
	}
	taskID := nextScheduleTaskID(conversationID)
	logPath := filepath.Join(scheduleHome(s.Home), "brain", conversationID,
		".system_generated", "tasks", taskID+".log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return nil, fmt.Errorf("schedule: create task dir: %w", err)
	}
	header := fmt.Sprintf("prompt: %s\n", p.Prompt)
	if p.CronExpression != "" {
		header += "cron: " + p.CronExpression + "\n"
	} else {
		header += fmt.Sprintf("delay_seconds: %d\n", p.DurationSeconds)
	}
	if err := os.WriteFile(logPath, []byte(header), 0o600); err != nil {
		return nil, fmt.Errorf("schedule: write task log: %w", err)
	}

	// The reference has no Completed At on this result: the command was
	// backgrounded, not finished.
	return "Created At: " + start.Format(time.RFC3339) + "\n" +
		"Tool is running as a background task with task id: " + conversationID + "/" + taskID + "\n" +
		"Task logs are available at: file://" + logPath + "\n" +
		"YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: A) either proceed to other relevant work (if any) or, B) simply update the user with a short message (that you have launched the command and will wait for it to finish) and end the turn.\n" +
		" DO NOTHING ELSE.", nil
}

var (
	scheduleSeqMu sync.Mutex
	scheduleSeq   = map[string]int{}
	procConvID    string
	procConvOnce  sync.Once
)

// nextScheduleTaskID mints the reference's per-conversation task ids
// ("task-1", "task-2", …).
func nextScheduleTaskID(conversationID string) string {
	scheduleSeqMu.Lock()
	defer scheduleSeqMu.Unlock()
	scheduleSeq[conversationID]++
	return fmt.Sprintf("task-%d", scheduleSeq[conversationID])
}

// processConversationID is a last-resort stable id so the acknowledged log path
// stays inside one directory for the life of the process.
func processConversationID() string {
	procConvOnce.Do(func() {
		procConvID = fmt.Sprintf("schedule-%d", os.Getpid())
	})
	return procConvID
}

// scheduleHome resolves the antigravity home the reference writes brain/ under.
func scheduleHome(override string) string {
	if override != "" {
		return override
	}
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
}

// ---- user interaction ------------------------------------------------------
//
// Every tool below reports that the user cannot be reached. This is the
// headless behaviour: the reference's no-tool-call reminder ends "Remember
// that the user cannot answer any questions.", and print mode has no prompt.

// noUserText is returned by the interactive tools when nothing can be asked.
const noUserText = "The user cannot be asked in this mode, so no answer was provided."

// AskQuestion — Source: tools.ask_question.
//
// Cycle subagent-question-picker: the tool routes through
// internal/subagentquestions when wired. With both Manager and
// SubAgentID set, every question is enqueued and the tool waits
// for the picker (or, in headless mode, the auto-skip) to settle
// the question. With a nil Manager the tool keeps the cycle-214
// behaviour: the parent cannot be asked, so it returns the
// recovered noUserText and the model sees the reference's
// equivalent reminder.
type AskQuestion struct {
	// SubAgentQuestions is the optional subagent question store
	// the parent TUI / print runtime wires in. nil keeps the
	// pre-picker behaviour.
	SubAgentQuestions *subagentquestions.Manager
	// SubAgentID identifies the subagent scope this tool call
	// is running in. Empty (subagentquestions.ParentKey) means
	// the parent's own ask_question; non-empty routes the
	// pending question through the named subagent's bucket so
	// the picker's "<agent> has a question" alert names the
	// right agent.
	SubAgentID string
}

func (a *AskQuestion) Name() string { return "ask_question" }
func (a *AskQuestion) Schema() json.RawMessage {
	return obj(`"questions":{"type":"array","description":"Questions to ask.","items":{"type":"object","properties":{"question":{"type":"string"},"options":{"type":"array","items":{"type":"string"}}},"required":["question"]}}`,
		"questions")
}
func (a *AskQuestion) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Questions []struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		} `json:"questions"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if len(p.Questions) == 0 {
		return nil, fmt.Errorf("ask_question: at least one question is required")
	}
	if a.SubAgentQuestions == nil {
		return noUserText, nil
	}
	// Cycle subagent-question-picker: enqueue every question on
	// the per-subagent pending list and wait for an answer (or
	// a headless auto-skip) before returning. In headless mode
	// the Manager already transitioned every Enqueue to
	// StateSkipped with SkipAnswer, so Wait returns immediately
	// and the subagent continues without a real reply — the
	// AGY 1.3.6 changelog line that motivates this whole
	// cycle: "Headless -p runs skip these questions so the
	// subagent can continue."
	//
	// In interactive mode the runMode's tool runner picks up
	// the pending question via a tea.Msg / channel, opens the
	// picker, and Answer / Skip unblocks the Wait here.
	var answers []string
	for i, q := range p.Questions {
		opts := q.Options
		if len(opts) == 0 {
			// Default option list the picker can render
			// as inline choices; today the picker reads
			// Options verbatim, so passing nil keeps
			// freeform questions freeform.
		}
		queued := a.SubAgentQuestions.Enqueue(a.SubAgentID, q.Question, opts)
		if queued == nil {
			answers = append(answers, noUserText)
			continue
		}
		ans, _ := queued.Wait()
		answers = append(answers, ans)
		_ = i
	}
	if len(answers) == 1 {
		return answers[0], nil
	}
	return strings.Join(answers, "\n"), nil
}

// AskPermission — Source: tools.ask_permission.
type AskPermission struct{}

func (a *AskPermission) Name() string { return "ask_permission" }
func (a *AskPermission) Schema() json.RawMessage {
	return obj(`"action":{"type":"string","description":"Action the agent wants to perform."},`+
		`"target":{"type":"string","description":"Resource the action applies to."}`,
		"action")
}
func (a *AskPermission) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		Target string `json:"target"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Action == "" {
		return nil, fmt.Errorf("ask_permission: action is required")
	}
	return permissions.DeniedToolResult(p.Action, p.Target), nil
}

// AskCustomPermission — Source: tools.ask_custom_permission.
type AskCustomPermission struct{}

func (a *AskCustomPermission) Name() string { return "ask_custom_permission" }
func (a *AskCustomPermission) Schema() json.RawMessage {
	return obj(`"action":{"type":"string"},`+
		`"target":{"type":"string"},`+
		`"reason":{"type":"string","description":"Why the permission is needed."}`,
		"action")
}
func (a *AskCustomPermission) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Reason string `json:"reason"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Action == "" {
		return nil, fmt.Errorf("ask_custom_permission: action is required")
	}
	return permissions.DeniedToolResult(p.Action, p.Target), nil
}

// NotifyUserTool — Source: tools.notify_user. Records a notification in the
// session inbox; there is no desktop notifier in print mode.
type NotifyUserTool struct{}

func (n *NotifyUserTool) Name() string { return "notify_user" }
func (n *NotifyUserTool) Schema() json.RawMessage {
	return obj(`"message":{"type":"string","description":"Notification text."}`, "message")
}
func (n *NotifyUserTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Message string `json:"message"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Message == "" {
		return nil, fmt.Errorf("notify_user: message is required")
	}
	inbox.add(p.Message)
	return "Notification recorded.", nil
}

// SendMessageTool — Source: tools.send_message. Delivers a message to a running
// background command. The reference addresses subagents and tasks; this clone
// has no subagent bus, so the background-command table is the only reachable
// target and anything else is reported as unknown.
type SendMessageTool struct{}

func (s *SendMessageTool) Name() string { return "send_message" }
func (s *SendMessageTool) Schema() json.RawMessage {
	return obj(`"id":{"type":"string","description":"Target command id."},`+
		`"message":{"type":"string","description":"Text delivered to the target."}`,
		"id", "message")
}
func (s *SendMessageTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	t, ok := backgroundTasks.get(p.ID)
	if !ok {
		return nil, fmt.Errorf("send_message: unknown task %q", p.ID)
	}
	if err := t.writeStdin(p.Message + "\n"); err != nil {
		return nil, err
	}
	return "Message delivered.", nil
}

// ManageInbox — Source: tools.manage_inbox. Lists or clears the notice inbox.
type ManageInbox struct{}

func (m *ManageInbox) Name() string { return "manage_inbox" }
func (m *ManageInbox) Schema() json.RawMessage {
	return obj(`"action":{"type":"string","description":"list or clear."}`, "action")
}
func (m *ManageInbox) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	switch p.Action {
	case "list":
		items := inbox.list()
		if len(items) == 0 {
			return "Inbox is empty.", nil
		}
		return strings.Join(items, "\n"), nil
	case "clear":
		n := inbox.clear()
		return fmt.Sprintf("Cleared %d inbox item(s).", n), nil
	}
	return nil, fmt.Errorf("manage_inbox: unknown action %q (want list or clear)", p.Action)
}

// ListPermissions — Source: tools.list_permissions. Reports the local rules.
type ListPermissions struct {
	Workspace string
	// Policy is the engine the session is using; injected by the caller.
	Allow []string
	Deny  []string
}

func (l *ListPermissions) Name() string { return "list_permissions" }
func (l *ListPermissions) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (l *ListPermissions) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var b strings.Builder
	if len(l.Deny) == 0 && len(l.Allow) == 0 {
		b.WriteString("No permission rules configured.\n")
	}
	for _, r := range l.Deny {
		fmt.Fprintf(&b, "deny  %s\n", r)
	}
	for _, r := range l.Allow {
		fmt.Fprintf(&b, "allow %s\n", r)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
