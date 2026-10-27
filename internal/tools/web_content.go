// Package tools — web_content.go: the two catalogue backends that fetch
// from the web, plus the HTML helpers they share.
//
// Cycle 346: these lived in stubs_search.go, next to six other search
// tools the catalogue never registered (`grep`, `web_search`,
// `codesearch`, `file_search`, `skill_search`, and the `grep_search`
// that `tools_search.go` already implements as `GrepSearchTool`).
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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SearchWeb — Web search (reference name `search_web`).
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
