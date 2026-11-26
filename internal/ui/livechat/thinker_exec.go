package livechat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/justwasm/pigasus/internal/app/tools/web"
	"github.com/justwasm/pigasus/internal/voice"
	"github.com/justwasm/pigasus/procgroup"
)

var urlRegex = regexp.MustCompile(`https?://[^\s'"<>]+`)

func extractURLs(text string) []string {
	matches := urlRegex.FindAllString(text, -1)
	var clean []string
	for _, u := range matches {
		u = strings.Trim(u, `"'(),;<>`)
		if u != "" {
			clean = append(clean, u)
		}
	}
	return clean
}

func fetchURLContent(ctx context.Context, rawURL string) (string, error) {
	return web.FetchURLText(ctx, rawURL, web.FetchURLOptions{
		MaxBytes:        3500,
		Timeout:         20 * time.Second,
		UserAgent:       "Mozilla/5.0 (compatible; Pigasus-LiveChat/1.0)",
		ExtractFragment: true,
	})
}

func executeRawCommand(ctx context.Context, ws string, cmdStr string) (string, error) {
	shell := "sh"
	if _, err := exec.LookPath("zsh"); err == nil {
		shell = "zsh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", cmdStr)
	cmd.Dir = ws
	procgroup.Set(cmd)

	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if len(outStr) > 2500 {
		outStr = outStr[:2500] + "\n...(output truncated)"
	}
	if err != nil {
		if outStr != "" {
			return outStr, err
		}
		return fmt.Sprintf("Command failed: %v", err), err
	}
	if outStr == "" {
		return "Command executed successfully with empty output.", nil
	}
	return outStr, nil
}

func hasUnbalancedQuotes(s string) bool {
	var inSingle, inDouble bool
	escaped := false
	for _, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		}
	}
	return inSingle || inDouble
}

func looksLikeQuestionOrProse(task string) bool {
	trimmed := strings.TrimSpace(task)
	if trimmed == "" {
		return false
	}
	if strings.HasSuffix(trimmed, "?") || strings.HasSuffix(trimmed, "？") {
		return true
	}
	lower := strings.ToLower(trimmed)
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "what", "what's", "who", "who's", "where", "where's", "when", "why", "how",
		"explain", "define", "describe", "tell", "search", "look", "please",
		"can", "could", "would", "should", "is", "are", "was", "were", "do", "does", "did":
		return true
	}
	for _, r := range fields[0] {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}

func isShellBuiltin(cmd string) bool {
	switch cmd {
	case "cd", "echo", "printf", "pwd", "export", "alias", "true", "false", "which", "type", "command":
		return true
	default:
		return false
	}
}

func isKnownDevCLI(cmd string) bool {
	switch cmd {
	case "git", "go", "npm", "npx", "yarn", "pnpm", "node", "python", "python3", "pip", "pip3",
		"cargo", "rustc", "make", "cmake", "docker", "kubectl", "helm", "terraform",
		"curl", "wget", "jq", "rg", "grep", "sed", "awk", "ls", "cat", "head", "tail",
		"mkdir", "rm", "cp", "mv", "touch", "chmod", "chown", "tar", "zip", "unzip",
		"ssh", "scp", "rsync", "brew", "apt", "yum", "echo", "printf", "export", "env",
		"uname", "uptime", "whoami", "pwd", "cal", "ps", "df", "du":
		return true
	default:
		return false
	}
}

func looksLikeRawShellCommand(task string) bool {
	trimmed := strings.TrimSpace(task)
	if trimmed == "" || strings.ContainsAny(trimmed, "\r\n") {
		return false
	}
	if looksLikeQuestionOrProse(trimmed) || hasUnbalancedQuotes(trimmed) {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	// Skip leading VAR=val env assignments.
	idx := 0
	for idx < len(fields) && strings.Contains(fields[idx], "=") && !strings.HasPrefix(fields[idx], "=") && !strings.HasPrefix(fields[idx], "-") {
		idx++
	}
	if idx >= len(fields) {
		return false
	}
	cmdTok := fields[idx]
	for _, r := range cmdTok {
		if r > unicode.MaxASCII || strings.ContainsRune("?？!！,，;；'\"", r) {
			return false
		}
	}
	if strings.HasPrefix(cmdTok, "./") || strings.HasPrefix(cmdTok, "../") || strings.HasPrefix(cmdTok, "/") || strings.HasPrefix(cmdTok, "~/") {
		return true
	}
	if isShellBuiltin(cmdTok) {
		return true
	}
	if _, err := exec.LookPath(cmdTok); err != nil {
		return false
	}
	rest := fields[idx+1:]
	if len(rest) == 0 || isKnownDevCLI(cmdTok) {
		return true
	}
	for _, arg := range rest {
		if strings.HasPrefix(arg, "-") || strings.ContainsAny(arg, "/.|&;><$") {
			return true
		}
	}
	return false
}

func looksLikeFailedShellIntent(task string) bool {
	trimmed := strings.TrimSpace(task)
	runCmdRegex := regexp.MustCompile(`(?i)^(?:run|execute)\s+([a-zA-Z0-9_\-\./]+)(?:\s+.*)?$`)
	return runCmdRegex.MatchString(trimmed)
}

func extractExplicitShellCommand(ws string, task string) (string, bool) {
	trimmed := strings.TrimSpace(task)
	if trimmed == "" {
		return "", false
	}

	// A. Check for backticks `command`
	backtickRegex := regexp.MustCompile("`([^`]+)`")
	if m := backtickRegex.FindStringSubmatch(trimmed); len(m) > 1 {
		return strings.TrimSpace(m[1]), true
	}

	lower := strings.ToLower(trimmed)
	words := strings.Fields(lower)

	// B. Direct terminal commands
	if len(words) == 1 {
		switch words[0] {
		case "cal", "date", "pwd", "whoami", "uname", "uptime", "ls":
			return words[0], true
		}
	}

	// C. Calendar
	for _, w := range words {
		if w == "cal" || w == "'cal'" || w == "\"cal\"" {
			return "cal", true
		}
	}

	// D. Git status / diff / log (Avoid matching "github" or natural-language questions!)
	if strings.Contains(lower, "git status") || (strings.Contains(lower, "status") && !strings.Contains(lower, "http") && !looksLikeQuestionOrProse(trimmed)) {
		return "git status -s", true
	}
	if strings.Contains(lower, "git diff") {
		return "git diff --stat", true
	}
	if !looksLikeQuestionOrProse(trimmed) {
		for _, w := range words {
			if w == "diff" {
				return "git diff --stat", true
			}
		}
	}
	if strings.Contains(lower, "git log") {
		return "git log -n 5 --oneline", true
	}

	// E. Tests (only explicit test execution requests, not prose questions about tests)
	isTestCmd := lower == "test" || lower == "tests" ||
		strings.Contains(lower, "run test") ||
		strings.Contains(lower, "run the test") ||
		strings.Contains(lower, "go test") ||
		strings.Contains(lower, "npm test") ||
		strings.Contains(lower, "make test") ||
		strings.Contains(lower, "check test") ||
		strings.Contains(lower, "execute test")
	if isTestCmd && !looksLikeQuestionOrProse(trimmed) {
		if _, err := os.Stat(filepath.Join(ws, "go.mod")); err == nil {
			return "go test ./...", true
		} else if _, err := os.Stat(filepath.Join(ws, "package.json")); err == nil {
			return "npm test", true
		} else {
			return "make test", true
		}
	}

	// F. "run the [cmd] command" pattern
	runTheCmdRegex := regexp.MustCompile(`(?i)run\s+the\s+([a-zA-Z0-9_\-\./]+)\s+command`)
	if m := runTheCmdRegex.FindStringSubmatch(trimmed); len(m) > 1 {
		return m[1], true
	}

	// G. "run [cmd]" or "execute [cmd]" pattern
	runCmdRegex := regexp.MustCompile(`(?i)^(?:run|execute)\s+([a-zA-Z0-9_\-\./]+)(?:\s+(.*))?$`)
	if m := runCmdRegex.FindStringSubmatch(trimmed); len(m) > 1 {
		cmdName := m[1]
		if _, err := exec.LookPath(cmdName); err == nil {
			if len(m) > 2 && m[2] != "" {
				return cmdName + " " + m[2], true
			}
			return cmdName, true
		}
	}

	// H. "use curl to fetch ..." -> extract curl command
	if strings.HasPrefix(lower, "use curl") || strings.Contains(lower, "curl ") {
		urls := extractURLs(trimmed)
		if len(urls) > 0 {
			return fmt.Sprintf("curl -sL %s", urls[0]), true
		}
	}

	// I. Direct raw shell command (strictly validated; never matches natural-language prose)
	if looksLikeRawShellCommand(trimmed) {
		return trimmed, true
	}

	return "", false
}

func cleanSearchQueryText(q string) (query, domain string) {
	q = strings.TrimSpace(q)
	q = strings.Trim(q, `"'.,;?!？！。，；`)
	// Strip nested Chinese question wrappers after explicit search verbs (e.g. "查一查什么叫X").
	for _, inner := range []string{"请问什么叫", "请问什么是", "什么叫", "什么是", "啥叫", "啥是"} {
		if strings.HasPrefix(q, inner) {
			rest := strings.TrimSpace(strings.TrimPrefix(q, inner))
			if rest != "" {
				q = rest
				break
			}
		}
	}
	q = strings.Trim(q, `"'.,;?!？！。，；`)
	if idx := strings.Index(strings.ToLower(q), " site:"); idx != -1 {
		domain = strings.TrimSpace(q[idx+6:])
		q = strings.TrimSpace(q[:idx])
	}
	return q, domain
}

func extractExplicitSearchQuery(task string) (query, domain string, ok bool) {
	trimmed := strings.TrimSpace(task)
	if trimmed == "" {
		return "", "", false
	}
	lower := strings.ToLower(trimmed)

	enPrefixes := []string{
		"search the web for ",
		"search the web about ",
		"search the web: ",
		"web search for ",
		"web search: ",
		"search for ",
		"search google for ",
		"google for ",
		"google ",
		"look up online ",
		"look up on the web ",
		"look up ",
		"find information on ",
		"find information about ",
		"find out about ",
	}
	for _, p := range enPrefixes {
		if strings.HasPrefix(lower, p) {
			q, d := cleanSearchQueryText(trimmed[len(p):])
			if q != "" {
				return q, d, true
			}
		}
	}

	zhPrefixes := []string{
		"帮我查一下",
		"帮我查一查",
		"帮我查",
		"帮我搜一下",
		"帮我搜一搜",
		"帮我搜",
		"网上搜索",
		"谷歌搜索",
		"百度一下",
		"搜索一下",
		"搜一下",
		"搜一搜",
		"查一下",
		"查一查",
		"查询一下",
		"搜索",
		"查询",
	}
	for _, p := range zhPrefixes {
		if strings.HasPrefix(trimmed, p) {
			q, d := cleanSearchQueryText(strings.TrimPrefix(trimmed, p))
			if q != "" {
				return q, d, true
			}
		}
	}

	re := regexp.MustCompile(`(?i)^search\s+(.+?)\s+(?:on the web|online|on google)$`)
	if m := re.FindStringSubmatch(trimmed); len(m) > 1 {
		q, d := cleanSearchQueryText(m[1])
		if q != "" {
			return q, d, true
		}
	}

	return "", "", false
}

func extractSearchQuery(task string) (query, domain string, ok bool) {
	if q, d, ok := extractExplicitSearchQuery(task); ok {
		return q, d, true
	}

	trimmed := strings.TrimSpace(task)
	if trimmed == "" {
		return "", "", false
	}
	// Do not hijack explicit shell commands (e.g. "what is the result of running cal").
	if _, isShell := extractExplicitShellCommand("", trimmed); isShell {
		return "", "", false
	}

	lower := strings.ToLower(trimmed)
	enQuestionPrefixes := []string{
		"what is ",
		"what are ",
		"what's ",
		"who is ",
		"who are ",
		"who's ",
		"define ",
		"definition of ",
		"explain ",
		"tell me about ",
		"find out what ",
	}
	for _, p := range enQuestionPrefixes {
		if strings.HasPrefix(lower, p) {
			q, d := cleanSearchQueryText(trimmed[len(p):])
			if q != "" {
				return q, d, true
			}
		}
	}

	zhQuestionPrefixes := []string{
		"请问什么叫",
		"请问什么是",
		"什么叫",
		"什么是",
		"啥叫",
		"啥是",
		"解释一下",
		"介绍一下",
		"科普一下",
		"请问",
	}
	for _, p := range zhQuestionPrefixes {
		if strings.HasPrefix(trimmed, p) {
			q, d := cleanSearchQueryText(strings.TrimPrefix(trimmed, p))
			if q != "" {
				return q, d, true
			}
		}
	}

	// Chinese definition/explanation suffixes (e.g. "格林斯潘第一定律 定义", "格林斯潘第一定律是什么").
	noPunct := strings.TrimRight(trimmed, `"'.,;?!？！。，； `)
	for _, suf := range []string{"是什么意思", "是什么", "的定义", " 定义", "的原理"} {
		if strings.HasSuffix(noPunct, suf) && len(noPunct) > len(suf) {
			q, d := cleanSearchQueryText(noPunct)
			if q != "" {
				return q, d, true
			}
		}
	}

	return "", "", false
}

func executeThinkerTask(ws string, task string, history ...[]voice.LiveTurnRecord) (string, error) {
	return executeThinkerTaskWithContext(context.Background(), ws, task, history...)
}

func executeThinkerTaskWithContext(parentCtx context.Context, ws string, task string, history ...[]voice.LiveTurnRecord) (string, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, 45*time.Second)
	defer cancel()

	trimmedTask := strings.TrimSpace(task)
	if trimmedTask == "" {
		return "", errors.New("empty thinker task")
	}

	// 1. Explicit web search request
	if query, domain, ok := extractExplicitSearchQuery(trimmedTask); ok {
		result, err := performWebSearch(ctx, query, domain)
		if err != nil {
			return "", fmt.Errorf("web search for %q failed: %w", query, err)
		}
		return result, nil
	}

	// 2. Check if task contains a URL (or references a URL in recent history)
	urls := extractURLs(trimmedTask)
	if len(urls) == 0 && len(history) > 0 && len(history[0]) > 0 {
		lowerTask := strings.ToLower(trimmedTask)
		if strings.Contains(lowerTask, "changelog") || strings.Contains(lowerTask, "fetch") || strings.Contains(lowerTask, "url") || strings.Contains(lowerTask, "link") || strings.Contains(lowerTask, "version") || strings.Contains(lowerTask, "summarize") || strings.Contains(lowerTask, "explain") {
			for i := len(history[0]) - 1; i >= 0; i-- {
				histURLs := extractURLs(history[0][i].Content)
				if len(histURLs) > 0 {
					urls = histURLs
					break
				}
			}
		}
	}

	if len(urls) > 0 {
		targetURL := urls[0]
		if !strings.Contains(targetURL, "#") {
			verRegex := regexp.MustCompile(`\b\d+\.\d+\.\d+\b`)
			if m := verRegex.FindString(trimmedTask); m != "" {
				targetURL += "#" + m
			}
		}
		content, err := fetchURLContent(ctx, targetURL)
		if err != nil {
			return "", fmt.Errorf("fetch url %s failed: %w", targetURL, err)
		}
		if len(content) > 0 {
			return content, nil
		}
	}

	// 3. Explicit or validated shell command (never pass arbitrary prose to zsh -c)
	if cmdStr, ok := extractExplicitShellCommand(ws, trimmedTask); ok {
		return executeRawCommand(ctx, ws, cmdStr)
	}

	// 4. Natural-language question / definition / lookup query -> web search
	if query, domain, ok := extractSearchQuery(trimmedTask); ok {
		result, err := performWebSearch(ctx, query, domain)
		if err != nil {
			return "", fmt.Errorf("web search for %q failed: %w", query, err)
		}
		return result, nil
	}

	// 5. Explicit "run/execute <unknown>" should fail cleanly rather than searching or invoking zsh -c
	if looksLikeFailedShellIntent(trimmedTask) {
		return "", fmt.Errorf("cannot execute task as shell command: %q", trimmedTask)
	}

	// 6. Fallback for remaining natural-language queries: search the web instead of running zsh -c
	result, err := performWebSearch(ctx, trimmedTask, "")
	if err != nil {
		return "", fmt.Errorf("web search for %q failed: %w", trimmedTask, err)
	}
	return result, nil
}

func summarizeResult(res string) string {
	lines := strings.Split(strings.TrimSpace(res), "\n")
	if len(lines) == 1 {
		return lines[0]
	}
	return fmt.Sprintf("%s (%d lines)", lines[0], len(lines))
}
