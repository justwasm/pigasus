package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// ---- workspace paths -------------------------------------------------------

// workspacePath resolves a model-supplied path for a WRITE-side tool.
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
	if !within(root, canonicalPath(full)) {
		return "", fmt.Errorf("path %s escapes the workspace", p)
	}
	return full, nil
}

// readPath resolves a model-supplied path for a READ-ONLY tool.
func readPath(ws, p string) (string, error) {
	if p == "" {
		return ws, nil
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return workspacePath(ws, p)
}

// canonicalPath resolves symlinks for the longest existing prefix of p.
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
func filePath(ws, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	return workspacePath(ws, p)
}

// argsOf unmarshals tool arguments, tolerating a nil/empty payload.
func argsOf(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	canon, err := canonicaliseJSONKeys(raw)
	if err != nil {
		return json.Unmarshal(raw, v)
	}
	return json.Unmarshal(canon, v)
}

// canonicaliseJSONKeys returns a copy of raw with every JSON
// object key converted from CamelCase to snake_case.
func canonicaliseJSONKeys(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
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
// converted to snake_case (and lowercased).
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

// camelToSnake converts CamelCase / PascalCase to snake_case.
func camelToSnake(s string) string {
	if s == "" {
		return s
	}
	if alias, ok := toolKeyAliases[s]; ok {
		return alias
	}
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

var toolKeyAliases = map[string]string{
	"AbsolutePath":        "path",
	"absolute_path":       "path",
	"TargetFile":          "path",
	"target_file":         "path",
	"FilePath":            "path",
	"file_path":           "path",
	"File":                "path",
	"file":                "path",
	"DirectoryPath":       "path",
	"directory":           "path",
	"SearchPath":          "path",
	"search_path":         "path",
	"SearchDirectory":     "path",
	"search_directory":    "path",
	"CommandLine":         "command",
	"command_line":        "command",
	"Command":             "command",
	"Cmd":                 "command",
	"OldString":           "oldString",
	"oldString":           "oldString",
	"NewString":           "newString",
	"newString":           "newString",
	"CodeContent":         "content",
	"code_content":        "content",
	"TargetContent":       "oldString",
	"target_content":      "oldString",
	"ReplacementContent":  "newString",
	"replacement_content": "newString",
	"StartLine":           "start_line",
	"startLine":           "start_line",
	"EndLine":             "end_line",
	"endLine":             "end_line",
	"MaxBytes":            "maxBytes",
	"MaxLines":            "maxLines",
	"Query":               "query",
	"Pattern":             "pattern",
	"SearchTerm":          "pattern",
	"Url":                 "url",
	"URL":                 "url",
	"Prompt":              "prompt",
	"Goal":                "goal",
	"Global":              "global",
	"Recurse":             "recursive",
	"Includes":            "includes",
	"ReplaceAll":          "replaceAll",
	"replaceAll":          "replaceAll",
	"AllowMultiple":       "allowMultiple",
	"allowMultiple":       "allowMultiple",
	"TaskId":              "id",
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}
