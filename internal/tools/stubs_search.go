package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/subagent"
)

// GrepSearch — Regex content search. v4 32 #6.
// Tries ripgrep first (richer output), falls back to grep -rn.
type GrepSearch struct{ Workspace string }

func (s *GrepSearch) Name() string { return "grep_search" }
func (s *GrepSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"includePattern":{"type":"string"},"context":{"type":"integer"}},"required":["pattern"]}`)
}
func (s *GrepSearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Pattern        string `json:"pattern"`
		Path           string `json:"path"`
		IncludePattern string `json:"include_pattern"`
		Context        int    `json:"context"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		return nil, errors.New("grep_search: pattern is required")
	}
	dir := resolveDir(s.Workspace)
	if p.Path != "" {
		dir = filepath.Join(dir, p.Path)
	}
	if _, err := exec.LookPath("rg"); err == nil {
		rg := []string{"--no-heading", "--line-number"}
		if p.Context != 0 {
			rg = append(rg, fmt.Sprintf("-C%d", p.Context))
		}
		if p.IncludePattern != "" {
			rg = append(rg, "--glob", p.IncludePattern)
		}
		rg = append(rg, p.Pattern, dir)
		out, err := execRun(ctx, "", "rg", rg...)
		if err == nil {
			return out, nil
		}
	}
	grep := []string{"-rn"}
	if p.Context != 0 {
		grep = append(grep, fmt.Sprintf("-C%d", p.Context))
	}
	if p.IncludePattern != "" {
		grep = append(grep, "--include="+p.IncludePattern)
	}
	grep = append(grep, p.Pattern, dir)
	out, err := execRun(ctx, "", "grep", grep...)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return nil, err
	}
	return out, nil
}

// Grep — Grep alt. v4 32 #15. Always uses grep -rn.
type Grep struct{ Workspace string }

func (s *Grep) Name() string { return "grep" }
func (s *Grep) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"caseInsensitive":{"type":"boolean"}},"required":["pattern"]}`)
}
func (s *Grep) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Pattern         string `json:"pattern"`
		Path            string `json:"path"`
		CaseInsensitive bool   `json:"case_insensitive"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		return nil, errors.New("grep: pattern is required")
	}
	dir := resolveDir(s.Workspace)
	if p.Path != "" {
		dir = filepath.Join(dir, p.Path)
	}
	a := []string{"-rn"}
	if p.CaseInsensitive {
		a = append(a, "-i")
	}
	a = append(a, p.Pattern, dir)
	out, err := execRun(ctx, "", "grep", a...)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return nil, err
	}
	return out, nil
}

// SearchWeb — Web search (reference name `search_web`). v4 32 #8.
type SearchWeb struct {
	Workspace string
	Provider  func(ctx context.Context, query, domain string) (string, error)
}

func (s *SearchWeb) Name() string { return "search_web" }
func (s *SearchWeb) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"domain":{"type":"string"}},"required":["query"]}`)
}
func (s *SearchWeb) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Query  string `json:"query"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Query == "" {
		return nil, errors.New("search_web: query is required")
	}
	if s.Provider != nil {
		out, err := s.Provider(ctx, p.Query, p.Domain)
		if err != nil {
			return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
		}
		return resultStamp(start) + out, nil
	}
	return resultStamp(start) + "Encountered error in tool execution: no candidates returned from GenerateContent", nil
}

// WebSearch — Web search alt. v4 32 #13.
type WebSearch struct{ Workspace string }

func (s *WebSearch) Name() string { return "web_search" }
func (s *WebSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"recencyDays":{"type":"integer"}},"required":["query"]}`)
}
func (s *WebSearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query       string `json:"query"`
		RecencyDays int    `json:"recency_days"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Query == "" {
		return nil, errors.New("web_search: query is required")
	}
	if _, err := exec.LookPath("ddgr"); err != nil {
		return fmt.Sprintf("web_search: ddgr not in PATH (query=%q, recency=%dd)",
			p.Query, p.RecencyDays), nil
	}
	sa := subagent.NewSubagent("ddgr", "--num=10", p.Query)
	defaultTaskRegistry.AttachSubagent(sa, "")
	if err := sa.Start(); err != nil {
		return nil, fmt.Errorf("web_search: start ddgr: %w", err)
	}
	if err := sa.Wait(ctx); err != nil {
		return nil, fmt.Errorf("web_search: ddgr wait: %w", err)
	}
	if sa.State != "done" {
		return nil, fmt.Errorf("web_search: ddgr state=%s err=%v", sa.State, sa.Err)
	}
	return sa.Result, nil
}

// ReadURLContent — Fetch URL content (reference name `read_url_content`).
type ReadURLContent struct {
	Workspace      string
	ConversationID string
	Home           string
}

func (s *ReadURLContent) Name() string { return "read_url_content" }
func (s *ReadURLContent) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"Url":{"type":"string"},"MaxBytes":{"type":"integer"}},"required":["Url"]}`)
}

func saveArticleContent(home, convID, article string) (string, error) {
	if convID == "" {
		convID = os.Getenv("OPENAGY_CONVERSATION_ID")
	}
	if convID == "" {
		convID = processConversationID()
	}
	step := nextStep(convID)
	dir := filepath.Join(scheduleHome(home), "brain", convID, ".system_generated", "steps", fmt.Sprintf("%d", step))
	path := filepath.Join(dir, "content.md")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(article), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (s *ReadURLContent) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		URL      string `json:"url"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.URL == "" {
		return nil, errors.New("read_url_content: url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return resultStamp(start) + fmt.Sprintf("Encountered error in tool execution: HTTP %d for %s", resp.StatusCode, p.URL), nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
	}
	title, description := htmlTitleAndDescription(string(body))
	article := htmlToText(string(body))
	if p.MaxBytes > 0 && len(article) > p.MaxBytes {
		article = article[:p.MaxBytes]
	}

	path, err := saveArticleContent(s.Home, s.ConversationID, article)
	if err != nil {
		return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
	}
	return resultStamp(start) +
		"Title: " + title + "\n\n" +
		"OG Description: " + description + "\n\n" +
		"The full content of the article at " + p.URL + " has been saved to: " + path + "\n\n" +
		"You can use the view_file tool to read specific sections if needed.", nil
}

var (
	stepMu sync.Mutex
	steps  = map[string]int{}
)

func nextStep(conversationID string) int {
	stepMu.Lock()
	defer stepMu.Unlock()
	steps[conversationID]++
	return steps[conversationID]
}

var (
	htmlTitleRe  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	htmlOGDesc   = regexp.MustCompile(`(?is)<meta[^>]+property\s*=\s*["']og:description["'][^>]*>`)
	htmlDescCont = regexp.MustCompile(`(?is)content\s*=\s*["']([^"']*)["']`)
	htmlScript   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<noscript[^>]*>.*?</noscript>`)
	htmlTag      = regexp.MustCompile(`(?s)<[^>]+>`)
	htmlSqueeze  = regexp.MustCompile(`\n{3,}`)
)

func htmlTitleAndDescription(page string) (title, description string) {
	if m := htmlTitleRe.FindStringSubmatch(page); len(m) == 2 {
		title = strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(m[1], "")))
	}
	if m := htmlOGDesc.FindString(page); m != "" {
		if c := htmlDescCont.FindStringSubmatch(m); len(c) == 2 {
			description = strings.TrimSpace(html.UnescapeString(c[1]))
		}
	}
	if title == "" {
		title = "Live Content"
	}
	if description == "" {
		description = "Fetched live"
	}
	return title, description
}

func htmlToText(page string) string {
	page = htmlScript.ReplaceAllString(page, "")
	for _, tag := range []string{"</p>", "</div>", "</li>", "</h1>", "</h2>", "</h3>", "</h4>", "</tr>", "<br>", "<br/>", "<br />"} {
		page = strings.ReplaceAll(page, tag, "\n")
	}
	page = htmlTag.ReplaceAllString(page, "")
	page = html.UnescapeString(page)
	lines := strings.Split(page, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.TrimSpace(strings.Join(strings.Fields(line), " ")))
	}
	text := strings.TrimSpace(htmlSqueeze.ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
	return text + "\n"
}

// Codesearch — BM25-ish code search. v4 32 #19.
type Codesearch struct{ Workspace string }

func (s *Codesearch) Name() string { return "codesearch" }
func (s *Codesearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"topK":{"type":"integer"}},"required":["query"]}`)
}
func (s *Codesearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Query == "" {
		return nil, errors.New("codesearch: query is required")
	}
	if p.TopK <= 0 {
		p.TopK = 20
	}
	dir := resolveDir(s.Workspace)
	if _, err := exec.LookPath("rg"); err == nil {
		rg := []string{"--no-heading", "-n", "-S", "-m", fmt.Sprintf("%d", p.TopK), p.Query, dir}
		out, err := execRun(ctx, "", "rg", rg...)
		if err == nil {
			return out, nil
		}
	}
	a := []string{"-rn", fmt.Sprintf("-m%d", p.TopK), p.Query, dir}
	out, err := execRun(ctx, "", "grep", a...)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return nil, err
	}
	return out, nil
}

// FileSearch — File name search. v4 32 #21.
type FileSearch struct{ Workspace string }

func (s *FileSearch) Name() string { return "file_search" }
func (s *FileSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}`)
}
func (s *FileSearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Query == "" {
		return nil, errors.New("file_search: query is required")
	}
	if p.Limit <= 0 {
		p.Limit = 100
	}
	dir := resolveDir(s.Workspace)
	q := strings.ToLower(p.Query)
	matches := make([]string, 0, p.Limit)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.Contains(strings.ToLower(d.Name()), q) {
			matches = append(matches, path)
			if len(matches) >= p.Limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

// SkillSearch — Search skills. v4 32 #22.
type SkillSearch struct{ Workspace string }

func (s *SkillSearch) Name() string { return "skill_search" }
func (s *SkillSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"topK":{"type":"integer"}},"required":["query"]}`)
}

func limitMatches(out string, topK int) []string {
	var matches []string
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		if ln == "" {
			continue
		}
		matches = append(matches, ln)
		if len(matches) >= topK {
			break
		}
	}
	if matches == nil {
		return []string{}
	}
	return matches
}

func (s *SkillSearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Query == "" {
		return nil, errors.New("skill_search: query is required")
	}
	if p.TopK <= 0 {
		p.TopK = 10
	}
	dir := resolveDir(s.Workspace)
	skillsDir := filepath.Join(dir, "skills")
	if _, err := os.Stat(skillsDir); err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	sa := subagent.NewSubagent("grep", "-rln", p.Query, skillsDir)
	defaultTaskRegistry.AttachSubagent(sa, "")
	if err := sa.Start(); err != nil {
		return nil, fmt.Errorf("skill_search: start grep: %w", err)
	}
	if err := sa.Wait(ctx); err != nil {
		if exitErr, ok := sa.Err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []string{}, nil
		}
		return nil, fmt.Errorf("skill_search: grep wait: %w", err)
	}
	return limitMatches(sa.Result, p.TopK), nil
}
