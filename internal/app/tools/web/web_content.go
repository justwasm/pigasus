// Package tools — web_content.go: the two catalogue backends that fetch
// from the web, plus the HTML helpers they share.
//
// Cycle 346: these lived in stubs_search.go, next to six other search
// tools the catalogue never registered (`grep`, `web_search`,
// `codesearch`, `file_search`, `skill_search`, and the `grep_search`
// that `tools_search.go` already implements as `GrepSearchTool`).
package web

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
	"time"

	"github.com/justwasm/pigasus/internal/app/tools"
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
			return tools.ResultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
		}
		return tools.ResultStamp(start) + out, nil
	}
	return tools.ResultStamp(start) + "Encountered error in tool execution: no candidates returned from GenerateContent", nil
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
		convID = tools.ProcessConversationID()
	}
	step := tools.NextStep(convID)
	dir := filepath.Join(tools.ScheduleHome(home), "brain", convID, ".system_generated", "steps", fmt.Sprintf("%d", step))
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
	body, _, err := fetchURLBody(ctx, p.URL, 0, "")
	if err != nil {
		return tools.ResultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
	}
	title, description := htmlTitleAndDescription(string(body))
	article := extractHTMLText(string(body), true)
	if p.MaxBytes > 0 && len(article) > p.MaxBytes {
		article = article[:p.MaxBytes]
	}

	path, err := saveArticleContent(s.Home, s.ConversationID, article)
	if err != nil {
		return tools.ResultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
	}
	return tools.ResultStamp(start) +
		"Title: " + title + "\n\n" +
		"OG Description: " + description + "\n\n" +
		"The full content of the article at " + p.URL + " has been saved to: " + path + "\n\n" +
		"You can use the view_file tool to read specific sections if needed.", nil
}

var (
	ghBlobRe     = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/blob/([^/]+)/(.*)$`)
	htmlTitleRe  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	htmlOGDesc   = regexp.MustCompile(`(?is)<meta[^>]+property\s*=\s*["']og:description["'][^>]*>`)
	htmlDescCont = regexp.MustCompile(`(?is)content\s*=\s*["']([^"']*)["']`)
	htmlScript   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<noscript[^>]*>.*?</noscript>`)
	htmlBlockEnd = regexp.MustCompile(`(?i)</(?:p|div|li|h[1-6]|tr)>|<br\s*/?>`)
	htmlTag      = regexp.MustCompile(`(?s)<[^>]+>`)
	htmlSqueeze  = regexp.MustCompile(`\n{3,}`)
)

// FetchURLOptions configures FetchURLText.
type FetchURLOptions struct {
	MaxBytes        int
	Timeout         time.Duration
	UserAgent       string
	ExtractFragment bool
}

// NormalizeFetchURL trims surrounding whitespace/quotes, splits any #fragment,
// and rewrites GitHub blob URLs to raw.githubusercontent.com URLs.
func NormalizeFetchURL(rawURL string) (cleanURL, fragment string) {
	cleanURL = strings.TrimSpace(rawURL)
	cleanURL = strings.Trim(cleanURL, `"'(),;<>`)
	if idx := strings.Index(cleanURL, "#"); idx != -1 {
		fragment = cleanURL[idx+1:]
		cleanURL = cleanURL[:idx]
	}
	if m := ghBlobRe.FindStringSubmatch(cleanURL); m != nil {
		cleanURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", m[1], m[2], m[3], m[4])
	}
	return cleanURL, fragment
}

// ExtractMarkdownSection extracts the markdown heading section matching fragment.
func ExtractMarkdownSection(text, fragment string) (string, bool) {
	if fragment == "" {
		return "", false
	}
	fragLower := strings.ToLower(fragment)
	fragNorm := strings.ToLower(strings.ReplaceAll(fragment, ".", ""))
	fragHyphenSpace := strings.ReplaceAll(fragLower, "-", " ")

	lines := strings.Split(text, "\n")
	var secLines []string
	inSec := false
	sectionLevel := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			headerText := strings.TrimSpace(trimmed[level:])
			headerLower := strings.ToLower(headerText)
			headerNorm := strings.ToLower(strings.ReplaceAll(headerText, ".", ""))
			if !inSec {
				if strings.Contains(headerNorm, fragNorm) ||
					strings.Contains(headerLower, fragLower) ||
					strings.Contains(headerLower, fragHyphenSpace) {
					inSec = true
					sectionLevel = level
					secLines = append(secLines, line)
					continue
				}
			} else if level <= sectionLevel {
				break
			}
		}
		if inSec {
			secLines = append(secLines, line)
		}
	}
	if len(secLines) > 0 {
		return strings.TrimSpace(strings.Join(secLines, "\n")), true
	}
	return "", false
}

func fetchURLBody(ctx context.Context, rawURL string, timeout time.Duration, userAgent string) (body []byte, contentType string, err error) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (compatible; Pigasus/1.0)"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create http request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/plain, text/markdown, text/html, application/json, */*")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	body, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", fmt.Errorf("read response body: %w", err)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

func extractHTMLText(raw string, preserveParagraphs bool) string {
	page := htmlScript.ReplaceAllString(raw, "")
	page = htmlBlockEnd.ReplaceAllString(page, "\n")
	page = htmlTag.ReplaceAllString(page, " ")
	page = html.UnescapeString(page)
	lines := strings.Split(page, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned := strings.TrimSpace(strings.Join(strings.Fields(line), " "))
		if preserveParagraphs || cleaned != "" {
			out = append(out, cleaned)
		}
	}
	if !preserveParagraphs {
		return strings.Join(out, "\n")
	}
	text := strings.TrimSpace(htmlSqueeze.ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
	if text == "" {
		return ""
	}
	return text + "\n"
}

// StripHTML removes script/style/noscript blocks and HTML tags, returning clean text.
func StripHTML(raw string) string {
	return extractHTMLText(raw, false)
}

// FetchURLText fetches a URL, optionally extracts a markdown #fragment section,
// strips HTML when the response is HTML, and truncates to opts.MaxBytes if > 0.
func FetchURLText(ctx context.Context, rawURL string, opts FetchURLOptions) (string, error) {
	cleanURL, fragment := NormalizeFetchURL(rawURL)
	if cleanURL == "" {
		return "", errors.New("fetch url: url is required")
	}

	body, ct, err := fetchURLBody(ctx, cleanURL, opts.Timeout, opts.UserAgent)
	if err != nil {
		return "", err
	}
	text := string(body)

	matchedFragment := false
	if opts.ExtractFragment && fragment != "" {
		if sec, ok := ExtractMarkdownSection(text, fragment); ok {
			text = sec
			matchedFragment = true
		}
	}

	if !matchedFragment {
		ctLower := strings.ToLower(ct)
		lowerText := strings.ToLower(text)
		if strings.Contains(ctLower, "html") || strings.Contains(lowerText, "<html") || strings.Contains(lowerText, "<!doctype html") {
			text = StripHTML(text)
		}
	}

	if opts.MaxBytes > 0 && len(text) > opts.MaxBytes {
		text = text[:opts.MaxBytes]
	}
	return text, nil
}

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
