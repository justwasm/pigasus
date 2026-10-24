package tuitools

import (
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/tuilib"
)

// CategoryWords is the singular/plural pair for each count category.
var CategoryWords = map[string][2]string{
	"file":     {"file", "files"},
	"edit":     {"file", "files"},
	"search":   {"search", "searches"},
	"command":  {"command", "commands"},
	"tool":     {"tool", "tools"},
	"task":     {"task", "tasks"},
	"cron":     {"cron", "crons"},
	"page":     {"page", "pages"},
	"resource": {"resource", "resources"},
	"question": {"question", "questions"},
	"timer":    {"timer", "timers"},
	"image":    {"image", "images"},
	"step":     {"step", "steps"},
	"result":   {"result", "results"},
	"match":    {"match", "matches"},
}

// ToolCategories maps a recovered tool name onto its count category.
var ToolCategories = map[string]string{
	"run_command":                "command",
	"command_status":             "command",
	"send_command_input":         "command",
	"view_file":                  "file",
	"read_file":                  "file",
	"write_to_file":              "edit",
	"write_file":                 "edit",
	"create_file":                "edit",
	"replace_file_content":       "edit",
	"edit_file":                  "edit",
	"multi_replace_file_content": "edit",
	"sed_file":                   "edit",
	"list_dir":                   "file",
	"notebook_edit":              "edit",
	"grep_search":                "search",
	"find_by_name":               "search",
	"codebase_search":            "search",
	"search_web":                 "search",
	"read_url_content":           "page",
	"view_content_chunk":         "page",
	"manage_task":                "task",
	"send_message":               "task",
	"wait":                       "task",
	"schedule":                   "cron",
	"list_resources":             "resource",
	"read_resource":              "resource",
	"ask_question":               "question",
	"generate_image":             "image",
}

// CategoryOrder is the stable order categories appear in a mixed group header.
var CategoryOrder = []string{
	"file", "edit", "search", "command", "page", "resource", "question",
	"timer", "image", "task", "cron", "tool", "step",
}

// ToolCategory maps a tool name onto its count category.
func ToolCategory(name string) string {
	if c, ok := ToolCategories[name]; ok {
		return c
	}
	return "tool"
}

// ParenArg returns the inside of the final parenthesised group in a title.
func ParenArg(title string) string {
	i := strings.LastIndexByte(title, '(')
	if i < 0 || !strings.HasSuffix(title, ")") {
		return ""
	}
	return title[i+1 : len(title)-1]
}

// StripBashParens extracts the inner command from a "Bash (cmd)" title.
func StripBashParens(title string) (cmd string, ok bool) {
	if !strings.HasPrefix(title, "Bash ") {
		return "", false
	}
	rest := strings.TrimPrefix(title, "Bash ")
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")") {
		return rest[1 : len(rest)-1], true
	}
	return "", false
}

// SplitShellSegments splits a shell command string into pipeline segments.
func SplitShellSegments(s string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if cur.Len() == 0 && (c == ' ' || c == '\t') {
			continue
		}
		if c == '\n' {
			if cur.Len() > 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			continue
		}
		if i+1 < len(s) && s[i+1] == c && (c == '&' || c == '|') {
			if cur.Len() > 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			i++
			continue
		}
		if c == ';' {
			if cur.Len() > 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

// FileNameArg returns the basename of the parenthesised argument.
func FileNameArg(title string) string {
	if p := ParenArg(title); p != "" {
		return filepath.Base(p)
	}
	return ""
}

// SearchArg returns the query in parenthesised title.
func SearchArg(title string) string {
	if p := ParenArg(title); p != "" {
		return p
	}
	return ""
}

// CountLabel formats n with its singular/plural word pair.
func CountLabel(n int, word [2]string) string {
	return tuilib.CountLabel(n, word)
}
