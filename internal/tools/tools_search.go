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

	"github.com/bmatcuk/doublestar/v4"
)

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

func grepFileLines(data []byte, rel string, re *regexp.Regexp, out *[]string, limit int) error {
	for i, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			*out = append(*out, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimRight(line, "\r")))
			if len(*out) >= limit {
				return errStopWalk
			}
		}
	}
	return nil
}

func (g *GrepSearchTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Pattern    string `json:"pattern"`
		Query      string `json:"query"`
		Path       string `json:"path"`
		Include    string `json:"include"`
		MaxResults int    `json:"max_results"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	// The reference's own search calls its pattern a "Query", and the
	// canonicaliser maps that name to "query" — the spelling the web
	// search uses. The two tools share the alias table, so the pattern is
	// read from both names here.
	if p.Pattern == "" {
		p.Pattern = p.Query
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
		if err != nil || !looksTextual(data) {
			return nil
		}
		return grepFileLines(data, rel, re, &out, limit)
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

// CodebaseSearch — Source: tools.codebase_search.
type CodebaseSearch struct{ Workspace string }

func (c *CodebaseSearch) Name() string { return "codebase_search" }
func (c *CodebaseSearch) Schema() json.RawMessage {
	return obj(`"query":{"type":"string","description":"Natural-language or keyword query."},`+
		`"path":{"type":"string","description":"Workspace-relative directory to search (default the workspace root)."},`+
		`"max_results":{"type":"integer"}`,
		"query")
}

func scoreTextMatches(data []byte, terms []string) (int, string) {
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
	return score, sample
}

type codebaseHit struct {
	rel    string
	score  int
	sample string
}

func formatCodebaseHits(hits []codebaseHit, limit int) string {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].rel < hits[j].rel
	})
	if limit <= 0 || limit > len(hits) {
		limit = len(hits)
	}
	var b strings.Builder
	for _, h := range hits[:limit] {
		fmt.Fprintf(&b, "%s (score %d): %s\n", h.rel, h.score, h.sample)
	}
	if b.Len() == 0 {
		return "No matches found."
	}
	return strings.TrimRight(b.String(), "\n")
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
	var hits []codebaseHit
	err = walkFiles(root, func(path string, rel string) error {
		data, err := os.ReadFile(path)
		if err != nil || !looksTextual(data) {
			return nil
		}
		score, sample := scoreTextMatches(data, terms)
		if score > 0 {
			hits = append(hits, codebaseHit{rel: rel, score: score, sample: trimTo(sample, 160)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return formatCodebaseHits(hits, p.MaxResults), nil
}
