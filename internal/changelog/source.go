package changelog

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	sourceOnce sync.Once
	sourceData []string
	sourcePath string
)

// Loaded reports whether the reference file was found and parsed.
func Loaded() bool {
	sourceOnce.Do(loadSource)
	return sourceData != nil
}

// Lines returns the raw lines from the reference file.
func Lines() []string {
	sourceOnce.Do(loadSource)
	return sourceData
}

// FilePath returns the absolute path the source was loaded from.
func FilePath() string {
	sourceOnce.Do(loadSource)
	return sourcePath
}

func loadSource() {
	path := FindReferencePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	sourceData = strings.Split(text, "\n")
	sourcePath = path
}

// FindReferencePath returns the absolute path to changelog_reference.txt.
func FindReferencePath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root := findProjectRoot(cwd)
	if root == "" {
		return ""
	}
	candidate := filepath.Join(root, "internal", "cli", "testdata", "changelog_reference.txt")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func findProjectRoot(dir string) string {
	cur, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// IsSourceHeader reports whether a line is a release header (matches `^\d+\.\d+\.\d+:$`).
func IsSourceHeader(line string) bool {
	if len(line) < 5 || line[len(line)-1] != ':' {
		return false
	}
	body := line[:len(line)-1]
	parts := strings.Split(body, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for i := 0; i < len(p); i++ {
			c := p[i]
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// HeaderLabel turns `1.3.6:` into `## 1.3.6`.
func HeaderLabel(line string) string {
	return "## " + strings.TrimSuffix(line, ":")
}

// Styled renders the raw bullet-list source lines into the panel's visual layout.
func Styled(src []string) []string {
	out := []string{TitleRow, "", Tagline}
	for _, raw := range src {
		line := strings.TrimRight(raw, "\r")
		switch {
		case IsSourceHeader(line):
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, HeaderOpen+HeaderLabel(line)+HeaderEnd, "")
		case strings.HasPrefix(line, "· "):
			out = append(out, "  "+TextOpen+"\u2022 "+PlainInline(line[2:])+TextClose)
		case line == "":
			out = append(out, "")
		}
	}
	return out
}

// Wrapped is the live-terminal variant of Styled: each bullet is word-wrapped.
func Wrapped(src []string, width int) []string {
	budget := WrapBudget(width)
	out := []string{TitleRow, "", Tagline}
	for _, raw := range src {
		line := strings.TrimRight(raw, "\r")
		switch {
		case IsSourceHeader(line):
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, HeaderOpen+HeaderLabel(line)+HeaderEnd, "")
		case strings.HasPrefix(line, "· "):
			out = append(out, WrapParagraph(PlainInline(line[2:]), true, budget)...)
		case line == "":
			out = append(out, "")
		}
	}
	return out
}
