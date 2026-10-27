package tools

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
	reDDGSnippet = regexp.MustCompile(`(?s)<a class="result__snippet[^"]*"[^>]*>(.*?)</a>`)
	reDDGURL     = regexp.MustCompile(`(?s)<a class="result__url[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	reHTMLTags   = regexp.MustCompile(`<[^>]+>`)
)

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

// performWebSearch runs a web query and formats the extracted items into a summary,
// returning the exact formatSearchWebResult output.
func performWebSearch(ctx context.Context, query, domain string) (string, error) {
	effectiveQuery := strings.TrimSpace(query)
	if domain != "" {
		effectiveQuery += " site:" + strings.TrimSpace(domain)
	}

	endpoint := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(effectiveQuery)
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	page := string(body)

	type resultItem struct {
		title   string
		link    string
		snippet string
	}

	var items []resultItem
	urlMatches := reDDGURL.FindAllStringSubmatch(page, -1)
	snipMatches := reDDGSnippet.FindAllStringSubmatch(page, -1)

	maxItems := len(urlMatches)
	if len(snipMatches) < maxItems {
		maxItems = len(snipMatches)
	}
	if maxItems > 5 {
		maxItems = 5
	}

	for i := 0; i < maxItems; i++ {
		rawURL := urlMatches[i][1]
		displayURL := html.UnescapeString(reHTMLTags.ReplaceAllString(urlMatches[i][2], ""))
		actualLink := rawURL
		if strings.Contains(rawURL, "uddg=") {
			if u, err := url.Parse(rawURL); err == nil {
				if uddg := u.Query().Get("uddg"); uddg != "" {
					actualLink = uddg
				}
			}
		}

		snip := html.UnescapeString(reHTMLTags.ReplaceAllString(snipMatches[i][1], ""))
		snip = strings.Join(strings.Fields(snip), " ")

		title := strings.TrimSpace(displayURL)
		if title == "" {
			title = actualLink
		}

		items = append(items, resultItem{
			title:   title,
			link:    actualLink,
			snippet: snip,
		})
	}

	if len(items) == 0 {
		return "", fmt.Errorf("no candidates returned from GenerateContent")
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
