package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/justwasm/pigasus/internal/app/tools"
)

// GrepSearchTool — Source: tools.grep_search. Regex search over the workspace.
type GrepSearchTool struct{ Workspace string }

func (g *GrepSearchTool) Name() string { return "grep_search" }
func (g *GrepSearchTool) Schema() json.RawMessage {
	return tools.Obj(`"pattern":{"type":"string","description":"Go regular expression to search for."},`+
		`"path":{"type":"string","description":"Workspace-relative directory or file to search (default the workspace root)."},`+
		`"include":{"type":"string","description":"Glob the file name must match, e.g. *.go."},`+
		`"max_results":{"type":"integer","description":"Maximum number of matches to return."}`,
		"pattern")
}

func (g *GrepSearchTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Pattern    string `json:"pattern"`
		Query      string `json:"query"`
		Path       string `json:"path"`
		Include    string `json:"include"`
		MaxResults int    `json:"max_results"`
	}
	if err := tools.ArgsOf(args, &p); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		p.Pattern = p.Query
	}
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return nil, fmt.Errorf("grep_search: invalid pattern: %w", err)
	}
	root, err := tools.WorkspacePath(g.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	limit := normalizeMaxResults(p.MaxResults)
	var budget searchBudget
	var out []string
	searchedFiles := 0
	resultLimited := false
	outputLimited := false
	outputBytes := 0
	err = WalkFilesContext(ctx, root, func(path, rel string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.Include != "" {
			if ok, _ := doublestar.Match(p.Include, filepath.Base(path)); !ok {
				return nil
			}
		}
		if budget.remaining() <= 0 {
			budget.totalTruncated = true
			return ErrStopWalk
		}
		searched, readErr := budget.readTextFile(ctx, path, func(line []byte, lineNo int) error {
			if !re.Match(line) {
				return nil
			}
			text := strings.TrimRight(string(line), "\r")
			if len(text) > maxSearchSnippetBytes {
				text = TrimTo(text, maxSearchSnippetBytes) + " [line truncated]"
			}
			result := fmt.Sprintf("%s:%d: %s", rel, lineNo, text)
			if outputBytes+len(result)+1 > maxSearchOutputBytes-len(outputTruncatedMarker) {
				outputLimited = true
				return ErrStopWalk
			}
			outputBytes += len(result) + 1
			out = append(out, result)
			if len(out) >= limit {
				resultLimited = true
				return ErrStopWalk
			}
			return nil
		})
		if searched {
			searchedFiles++
		}
		return readErr
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil && err != ErrStopWalk {
		return nil, err
	}
	if outputLimited {
		// The collected results were sized to leave room for the marker, but
		// budget notes may not fit; renderSearchOutput only adds the marker
		// when it had to drop output itself, so ask for it explicitly here.
		notes := append(budgetNotes(&budget), strings.TrimPrefix(outputTruncatedMarker, "\n"))
		return renderSearchOutput(out, notes, "No matches found."), nil
	}
	if resultLimited {
		notes := budgetNotes(&budget)
		notes = append(notes, fmt.Sprintf("Search stopped after reaching max_results (%d); further matches may exist.", limit))
		return renderSearchOutput(out, notes, "No matches found."), nil
	}
	if searchedFiles == 0 {
		return renderSearchOutput(nil, budgetNotes(&budget), "No files were searched, double check your search parameter"), nil
	}
	return renderSearchOutput(out, budgetNotes(&budget), "No matches found."), nil
}

// FindByName — Source: tools.find_by_name. Glob match over file names.
type FindByName struct{ Workspace string }

func (f *FindByName) Name() string { return "find_by_name" }
func (f *FindByName) Schema() json.RawMessage {
	return tools.Obj(`"pattern":{"type":"string","description":"Glob matched against the file name, e.g. *.go. Use **/ to match any depth."},`+
		`"path":{"type":"string","description":"Workspace-relative directory to search (default the workspace root)."},`+
		`"max_results":{"type":"integer"}`, "pattern")
}
func (f *FindByName) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		MaxResults int    `json:"max_results"`
	}
	if err := tools.ArgsOf(args, &p); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		return nil, fmt.Errorf("find_by_name: pattern is required")
	}
	root, err := tools.WorkspacePath(f.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	limit := normalizeMaxResults(p.MaxResults)
	var out []string
	resultLimited := false
	matcher := globMatcher(p.Pattern)
	err = WalkFilesContext(ctx, root, func(path, rel string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if matcher(rel, filepath.Base(path)) {
			out = append(out, rel)
			if len(out) >= limit {
				resultLimited = true
				return ErrStopWalk
			}
		}
		return nil
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil && err != ErrStopWalk {
		return nil, err
	}
	var notes []string
	if resultLimited {
		notes = append(notes, fmt.Sprintf("Search stopped after reaching max_results (%d); further matches may exist.", limit))
	}
	return renderSearchOutput(out, notes, "No matches found."), nil
}

func globMatcher(pattern string) func(rel, base string) bool {
	if !strings.Contains(pattern, "/") {
		return func(_, base string) bool { ok, _ := doublestar.Match(pattern, base); return ok }
	}
	return func(rel, _ string) bool { ok, _ := doublestar.Match(pattern, rel); return ok }
}

// CodebaseSearch — Source: tools.codebase_search.
type CodebaseSearch struct{ Workspace string }

func (c *CodebaseSearch) Name() string { return "codebase_search" }
func (c *CodebaseSearch) Schema() json.RawMessage {
	return tools.Obj(`"query":{"type":"string","description":"Natural-language or keyword query."},`+
		`"path":{"type":"string","description":"Workspace-relative directory to search (default the workspace root)."},`+
		`"max_results":{"type":"integer"}`, "query")
}

type codebaseHit struct {
	rel    string
	score  int
	sample string
}

func (c *CodebaseSearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query      string `json:"query"`
		Path       string `json:"path"`
		MaxResults int    `json:"max_results"`
	}
	if err := tools.ArgsOf(args, &p); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(p.Query) > 4096 {
		return nil, fmt.Errorf("codebase_search: query exceeds 4096 bytes")
	}
	terms := strings.Fields(strings.ToLower(p.Query))
	if len(terms) == 0 {
		return nil, fmt.Errorf("codebase_search: query is required")
	}
	if len(terms) > 32 {
		return nil, fmt.Errorf("codebase_search: query exceeds 32 terms")
	}
	root, err := tools.WorkspacePath(c.Workspace, p.Path)
	if err != nil {
		return nil, err
	}
	limit := normalizeMaxResults(p.MaxResults)
	var budget searchBudget
	var hits []codebaseHit
	matchingFiles := 0
	err = WalkFilesContext(ctx, root, func(path, rel string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if budget.remaining() <= 0 {
			budget.totalTruncated = true
			return ErrStopWalk
		}
		score := 0
		samples := make([]string, len(terms))
		searched, readErr := budget.readTextFile(ctx, path, func(line []byte, _ int) error {
			lower := strings.ToLower(string(line))
			for i, term := range terms {
				count := strings.Count(lower, term)
				score += count
				if count > 0 && samples[i] == "" {
					samples[i] = TrimTo(strings.TrimSpace(string(line)), 160)
				}
			}
			return nil
		})
		if readErr != nil {
			return readErr
		}
		if searched && score > 0 {
			matchingFiles++
			sample := ""
			for _, candidate := range samples {
				if candidate != "" {
					sample = candidate
					break
				}
			}
			insertCodebaseHit(&hits, codebaseHit{rel: rel, score: score, sample: sample}, limit)
		}
		return nil
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil && err != ErrStopWalk {
		return nil, err
	}
	notes := budgetNotes(&budget)
	if matchingFiles > limit {
		notes = append(notes, fmt.Sprintf("Results truncated to max_results (%d).", limit))
	}
	return renderSearchOutput(codebaseHitLines(hits), notes, "No matches found."), nil
}

func insertCodebaseHit(hits *[]codebaseHit, hit codebaseHit, limit int) {
	items := *hits
	at := sort.Search(len(items), func(i int) bool {
		return items[i].score < hit.score || items[i].score == hit.score && items[i].rel > hit.rel
	})
	if at >= limit {
		return
	}
	items = append(items, codebaseHit{})
	copy(items[at+1:], items[at:])
	items[at] = hit
	if len(items) > limit {
		items = items[:limit]
	}
	*hits = items
}
func codebaseHitLines(hits []codebaseHit) []string {
	lines := make([]string, 0, len(hits))
	for _, hit := range hits {
		lines = append(lines, fmt.Sprintf("%s (score %d): %s", hit.rel, hit.score, hit.sample))
	}
	return lines
}
