package web

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	duckDuckGoSearchBaseURL = "https://html.duckduckgo.com/html/"
	duckDuckGoLiteBaseURL   = "https://lite.duckduckgo.com/lite/"
	bingSearchBaseURL       = "https://www.bing.com/search"

	reDDGSnippet = regexp.MustCompile(`(?s)<a class="result__snippet[^"]*"[^>]*>(.*?)</a>`)
	reDDGURL     = regexp.MustCompile(`(?s)<a class="result__url[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)

	reAnchorTag       = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	reHrefAttr        = regexp.MustCompile(`(?i)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	reClassAttr       = regexp.MustCompile(`(?i)\bclass\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	reDDGLiteSnippet  = regexp.MustCompile(`(?is)<(?:td|span|div|a)\b[^>]*class\s*=\s*(?:"[^"]*\bresult-snippet\b[^"]*"|'[^']*\bresult-snippet\b[^']*')[^>]*>(.*?)</(?:td|span|div|a)>`)
	reTableRow        = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	reTableCell       = regexp.MustCompile(`(?is)<td\b[^>]*>(.*?)</td>`)
	reBingAlgoStart   = regexp.MustCompile(`(?is)<li\b[^>]*class\s*=\s*(?:"[^"]*\bb_algo\b[^"]*"|'[^']*\bb_algo\b[^']*')[^>]*>`)
	reBingHeadingLink = regexp.MustCompile(`(?is)<h2\b[^>]*>.*?<a\b([^>]*)>(.*?)</a>.*?</h2>`)
	reBingCaptionPara = regexp.MustCompile(`(?is)<div\b[^>]*class\s*=\s*(?:"[^"]*\bb_caption\b[^"]*"|'[^']*\bb_caption\b[^']*')[^>]*>.*?<p\b[^>]*>(.*?)</p>`)
	reParagraph       = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`)

	reHTMLTags = regexp.MustCompile(`<[^>]+>`)
)

type searchResultItem struct {
	title   string
	link    string
	snippet string
}

type searchSource struct {
	name    string
	baseURL string
	parse   func(page string) []searchResultItem
}

// formatSearchWebResult replicates the reference binary's
// google3/third_party/oneharness/internal/tools/tools.formatSearchWebResult
// byte for byte.
//
// 【反编译】 agy 1.4.8, 0x101e059f0. Decompiled signature:
//
//	func formatSearchWebResult(query string, summary string) string
//
// String parts:
//  1. fmt.Sprintf("The search for \"%s\"", query) — 19 B, vm 0x103502675
//  2. " returned the following summary:\n"            — 32 B, vm 0x1035be804
//  3. summary
//  4. "\n\nNote: Source URLs are HTTP redirects; check where they resolve
//     if you need the direct link."                   — 93 B, vm 0x103685a11
func formatSearchWebResult(query, summary string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("The search for %q", query))
	b.WriteString(" returned the following summary:\n")
	b.WriteString(summary)
	b.WriteString("\n\nNote: Source URLs are HTTP redirects; check where they resolve if you need the direct link.")
	return b.String()
}

// PerformWebSearch runs a web query across primary and fallback search sources
// and formats the extracted items into a summary, returning the exact
// formatSearchWebResult output.
func PerformWebSearch(ctx context.Context, query, domain string) (string, error) {
	effectiveQuery := strings.TrimSpace(query)
	if domain != "" {
		effectiveQuery += " site:" + strings.TrimSpace(domain)
	}

	sources := []searchSource{
		{name: "duckduckgo-html", baseURL: duckDuckGoSearchBaseURL, parse: parseDDGHTML},
		{name: "duckduckgo-lite", baseURL: duckDuckGoLiteBaseURL, parse: parseDDGLite},
		{name: "bing", baseURL: bingSearchBaseURL, parse: parseBingHTML},
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var failures []string

	for _, src := range sources {
		items, err := querySearchSource(ctx, client, src, effectiveQuery)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", src.name, err))
			continue
		}

		var sb strings.Builder
		for i, it := range items {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(fmt.Sprintf("%d. %s\n   URL: %s\n   %s", i+1, it.title, it.link, it.snippet))
		}
		return formatSearchWebResult(query, sb.String()), nil
	}

	return "", fmt.Errorf("web search failed for %q: %s", effectiveQuery, strings.Join(failures, "; "))
}

func querySearchSource(ctx context.Context, client *http.Client, src searchSource, effectiveQuery string) ([]searchResultItem, error) {
	if strings.TrimSpace(src.baseURL) == "" {
		return nil, fmt.Errorf("empty base URL")
	}

	sep := "?"
	if strings.Contains(src.baseURL, "?") {
		sep = "&"
	}
	endpoint := src.baseURL + sep + "q=" + url.QueryEscape(effectiveQuery)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	page := string(body)

	items := src.parse(page)
	if len(items) == 0 {
		if isSearchChallenge(resp.StatusCode, page) {
			return nil, fmt.Errorf("search blocked or challenged (HTTP %d) for %q", resp.StatusCode, effectiveQuery)
		}
		return nil, fmt.Errorf("search returned no results for %q (HTTP %d)", effectiveQuery, resp.StatusCode)
	}
	return items, nil
}

func isSearchChallenge(statusCode int, page string) bool {
	if statusCode == http.StatusAccepted {
		return true
	}
	lowerPage := strings.ToLower(page)
	return strings.Contains(lowerPage, "anomaly-modal") ||
		strings.Contains(lowerPage, "captcha") ||
		strings.Contains(lowerPage, "challenge")
}

func cleanSearchText(raw string) string {
	text := html.UnescapeString(reHTMLTags.ReplaceAllString(raw, ""))
	return strings.Join(strings.Fields(text), " ")
}

func extractAttr(attrs string, re *regexp.Regexp) string {
	m := re.FindStringSubmatch(attrs)
	if len(m) < 3 {
		return ""
	}
	if m[1] != "" {
		return html.UnescapeString(m[1])
	}
	return html.UnescapeString(m[2])
}

func hasClassToken(classAttr, token string) bool {
	for _, f := range strings.Fields(classAttr) {
		if f == token {
			return true
		}
	}
	return false
}

func unwrapDDGLink(rawURL string) string {
	actualLink := strings.TrimSpace(html.UnescapeString(rawURL))
	if strings.Contains(actualLink, "uddg=") {
		if u, err := url.Parse(actualLink); err == nil {
			if uddg := u.Query().Get("uddg"); uddg != "" {
				return uddg
			}
		}
	}
	return actualLink
}

func parseDDGHTML(page string) []searchResultItem {
	urlMatches := reDDGURL.FindAllStringSubmatch(page, -1)
	snipMatches := reDDGSnippet.FindAllStringSubmatch(page, -1)

	maxItems := len(urlMatches)
	if len(snipMatches) < maxItems {
		maxItems = len(snipMatches)
	}
	if maxItems > 5 {
		maxItems = 5
	}

	items := make([]searchResultItem, 0, maxItems)
	for i := 0; i < maxItems; i++ {
		actualLink := unwrapDDGLink(urlMatches[i][1])
		title := cleanSearchText(urlMatches[i][2])
		if title == "" {
			title = actualLink
		}
		snip := cleanSearchText(snipMatches[i][1])

		items = append(items, searchResultItem{
			title:   title,
			link:    actualLink,
			snippet: snip,
		})
	}
	return items
}

func parseDDGLite(page string) []searchResultItem {
	type linkEntry struct {
		title string
		link  string
	}

	var links []linkEntry
	for _, m := range reAnchorTag.FindAllStringSubmatch(page, -1) {
		attrs, inner := m[1], m[2]
		if !hasClassToken(extractAttr(attrs, reClassAttr), "result-link") {
			continue
		}
		href := extractAttr(attrs, reHrefAttr)
		if href == "" {
			continue
		}
		actualLink := unwrapDDGLink(href)
		title := cleanSearchText(inner)
		if title == "" {
			title = actualLink
		}
		links = append(links, linkEntry{title: title, link: actualLink})
	}

	snipMatches := reDDGLiteSnippet.FindAllStringSubmatch(page, -1)
	if len(links) > 0 && len(snipMatches) > 0 {
		maxItems := len(links)
		if len(snipMatches) < maxItems {
			maxItems = len(snipMatches)
		}
		if maxItems > 5 {
			maxItems = 5
		}
		items := make([]searchResultItem, 0, maxItems)
		for i := 0; i < maxItems; i++ {
			items = append(items, searchResultItem{
				title:   links[i].title,
				link:    links[i].link,
				snippet: cleanSearchText(snipMatches[i][1]),
			})
		}
		return items
	}

	// Fallback for standard DDG Lite table rows without explicit result-link classes.
	if isSearchChallenge(http.StatusOK, page) {
		return nil
	}
	rows := reTableRow.FindAllStringSubmatch(page, -1)
	var items []searchResultItem
	for i := 0; i < len(rows) && len(items) < 5; i++ {
		anchors := reAnchorTag.FindAllStringSubmatch(rows[i][1], -1)
		var matched *linkEntry
		for _, a := range anchors {
			href := extractAttr(a[1], reHrefAttr)
			if href == "" {
				continue
			}
			if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") && !strings.Contains(href, "uddg=") {
				continue
			}
			actualLink := unwrapDDGLink(href)
			title := cleanSearchText(a[2])
			if title == "" {
				title = actualLink
			}
			matched = &linkEntry{title: title, link: actualLink}
			break
		}
		if matched == nil || i+1 >= len(rows) {
			continue
		}
		cells := reTableCell.FindAllStringSubmatch(rows[i+1][1], -1)
		var snip string
		for _, c := range cells {
			if s := cleanSearchText(c[1]); s != "" {
				snip = s
			}
		}
		if snip == "" {
			continue
		}
		items = append(items, searchResultItem{
			title:   matched.title,
			link:    matched.link,
			snippet: snip,
		})
		i++
	}
	return items
}

func parseBingHTML(page string) []searchResultItem {
	starts := reBingAlgoStart.FindAllStringIndex(page, -1)
	if len(starts) == 0 {
		return nil
	}

	items := make([]searchResultItem, 0, 5)
	for i, loc := range starts {
		if len(items) >= 5 {
			break
		}
		end := len(page)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		block := page[loc[0]:end]
		if olEnd := strings.Index(strings.ToLower(block), "</ol>"); olEnd != -1 {
			block = block[:olEnd]
		}

		hm := reBingHeadingLink.FindStringSubmatch(block)
		if len(hm) < 3 {
			continue
		}
		link := strings.TrimSpace(extractAttr(hm[1], reHrefAttr))
		if link == "" {
			continue
		}
		title := cleanSearchText(hm[2])
		if title == "" {
			title = link
		}

		var snip string
		if cm := reBingCaptionPara.FindStringSubmatch(block); len(cm) >= 2 {
			snip = cleanSearchText(cm[1])
		} else if pm := reParagraph.FindStringSubmatch(block); len(pm) >= 2 {
			snip = cleanSearchText(pm[1])
		}

		items = append(items, searchResultItem{
			title:   title,
			link:    link,
			snippet: snip,
		})
	}
	return items
}
