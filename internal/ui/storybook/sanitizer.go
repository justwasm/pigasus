package storybook

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Sanitizer provides deterministic masking of dynamic elements in TUI views:
// - Absolute/user paths (/Users/... -> /workspace/...)
// - Execution elapsed times ((0.01s) -> (XX.Xs), 1.450s -> XX.Xs)
// - Wall-clock timestamps (HH:MM:SS, ISO dates, timestamped log files)
// - Real-time Braille spinner animation frames -> canonical frame
type Sanitizer struct {
	repoRoot  string
	userHome  string
	pathRules []*regexp.Regexp
	timeRules []timeRule
	durRules  []durRule
	animRules []animRule
}

type timeRule struct {
	re   *regexp.Regexp
	repl string
}

type durRule struct {
	re   *regexp.Regexp
	repl string
}

type animRule struct {
	re   *regexp.Regexp
	repl string
}

var (
	// Default instance singleton
	defaultSanitizer = newDefaultSanitizer()

	// Precompiled path matching regexes
	macUserPathRe     = regexp.MustCompile(`/Users/[a-zA-Z0-9._-]+`)
	linuxUserPathRe   = regexp.MustCompile(`/home/[a-zA-Z0-9._-]+`)
	windowsUserPathRe = regexp.MustCompile(`[a-zA-Z]:[/\\]Users[/\\][a-zA-Z0-9._-]+`)
	tempTestHomeRe    = regexp.MustCompile(`(?:/var/folders/[^\s:/]+(?:/[^\s:/]+)*/T|/tmp)/pigasus-test-home-[0-9]+`)

	// Timestamped log file matching (e.g. cli-20261123_001902.log)
	cliLogRe = regexp.MustCompile(`cli-\d{8}_\d{6}\.log`)

	// Execution time & duration matching regexes
	// Matches parenthesized duration: (0.01s), (12.3s), (14s), with optional ANSI escapes
	parenDurRe = regexp.MustCompile(`\(\s*(?:\x1b\[[0-9;]*m)*\s*\d+(?:\.\d+)?(?:m?s|µs|ns)\s*(?:\x1b\[[0-9;]*m)*\s*\)`)

	// Matches standalone durations like 1.450s, 0.05s, 14.2s using capture groups
	standaloneSecRe = regexp.MustCompile(`(^|[\s\(\[m>])(\d+\.\d{1,3}s)($|[,\.\s\)\x1b])`)

	// Matches contextual durations
	thoughtDurRe   = regexp.MustCompile(`\bThought for \d+(?:\.\d+)?s\b`)
	workedDurRe    = regexp.MustCompile(`\bworked \d+s\b`)
	relAgoRe       = regexp.MustCompile(`\b\d+[mhd]\s+ago\b`)
	quotaRefreshRe = regexp.MustCompile(`\bRefreshes in (?:\d+d )?\d+h(?:\s*\d+m)?\b`)

	// Timestamp regexes - handles boundaries even when preceded by ANSI SGR escapes ending in 'm'
	clock24Re = regexp.MustCompile(`(^|[^\d])([0-2][0-9]:[0-5][0-9]:[0-5][0-9])([^\d]|$)`)
	isoTimeRe = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?\b`)
	tzClockRe = regexp.MustCompile(`\b\d{1,2}:\d{2}(?::\d{2})?\s+(?:AM|PM|UTC|GMT)\b`)

	// Braille animation spinner frame normalization
	braille8DotSpinnerRe = regexp.MustCompile(`[⣾⣷⣯⣟⡿⢿⣻⣽]`)
	braille6DotSpinnerRe = regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]`)
)

func findModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func newDefaultSanitizer() *Sanitizer {
	s := &Sanitizer{
		pathRules: []*regexp.Regexp{
			macUserPathRe,
			linuxUserPathRe,
			windowsUserPathRe,
		},
		timeRules: []timeRule{
			{re: cliLogRe, repl: "cli-YYYYMMDD_HHMMSS.log"},
			{re: isoTimeRe, repl: "YYYY-MM-DDTHH:MM:SSZ"},
			{re: clock24Re, repl: "${1}HH:MM:SS${3}"},
			{re: tzClockRe, repl: "HH:MM UTC"},
		},
		durRules: []durRule{
			{re: parenDurRe, repl: "(XX.Xs)"},
			{re: standaloneSecRe, repl: "${1}XX.Xs${3}"},
			{re: thoughtDurRe, repl: "Thought for XX.Xs"},
			{re: workedDurRe, repl: "worked XXs"},
			{re: relAgoRe, repl: "XXm ago"},
			{re: quotaRefreshRe, repl: "Refreshes in XXd XXh XXm"},
		},
		animRules: []animRule{
			{re: braille8DotSpinnerRe, repl: "⣾"},
			{re: braille6DotSpinnerRe, repl: "⠋"},
		},
	}

	if modRoot := findModuleRoot(); modRoot != "" {
		s.repoRoot = modRoot
	} else if wd, err := os.Getwd(); err == nil && wd != "" {
		s.repoRoot = wd
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		s.userHome = home
	}

	return s
}

// DefaultSanitizer returns the process-wide default sanitizer instance.
func DefaultSanitizer() *Sanitizer {
	return defaultSanitizer
}

// Sanitize applies standard deterministic masking to input string s.
func Sanitize(s string) string {
	return defaultSanitizer.Sanitize(s)
}

// Sanitize masks timestamps, durations, and dynamic paths.
func (s *Sanitizer) Sanitize(text string) string {
	if text == "" {
		return ""
	}

	res := text

	// 1. Paths: replace repoRoot first to preserve relative directory context
	if s.repoRoot != "" {
		res = strings.ReplaceAll(res, s.repoRoot, "/workspace/openagy")
		// Also handle forward slash normalized repo root on Windows
		normRoot := filepath.ToSlash(s.repoRoot)
		if normRoot != s.repoRoot {
			res = strings.ReplaceAll(res, normRoot, "/workspace/openagy")
		}
	}

	// Also replace tilde-shortened repo paths (~/GitHub/openagy or ~/work/pigasus/pigasus)
	if s.userHome != "" && s.repoRoot != "" {
		if rel, err := filepath.Rel(s.userHome, s.repoRoot); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			tildeRepo := "~/" + filepath.ToSlash(rel)
			res = strings.ReplaceAll(res, tildeRepo, "~/workspace/openagy")
		}
	}

	// Replace user home if set
	if s.userHome != "" {
		res = strings.ReplaceAll(res, s.userHome, "/workspace")
		normHome := filepath.ToSlash(s.userHome)
		if normHome != s.userHome {
			res = strings.ReplaceAll(res, normHome, "/workspace")
		}
	}

	// In test mode or when HOME / OPENAGY_CONFIG_HOME are overridden by testguard:
	if cfgHome := os.Getenv("OPENAGY_CONFIG_HOME"); cfgHome != "" {
		res = strings.ReplaceAll(res, cfgHome, "/workspace")
	}
	if agyHome := os.Getenv("ANTIGRAVITY_HOME"); agyHome != "" {
		res = strings.ReplaceAll(res, agyHome, "/workspace/.gemini/antigravity-cli")
	}
	if curHome := os.Getenv("HOME"); curHome != "" {
		res = strings.ReplaceAll(res, curHome, "/workspace")
	}
	res = tempTestHomeRe.ReplaceAllString(res, "/workspace")

	// Apply generic user path regexes (/Users/xxx, /home/xxx)
	for _, pr := range s.pathRules {
		res = pr.ReplaceAllString(res, "/workspace")
	}

	// 2. Durations and execution elapsed times
	for _, dr := range s.durRules {
		res = dr.re.ReplaceAllString(res, dr.repl)
	}

	// 3. Timestamps
	for _, tr := range s.timeRules {
		res = tr.re.ReplaceAllString(res, tr.repl)
	}

	// 4. Animation frames (spinners)
	for _, ar := range s.animRules {
		res = ar.re.ReplaceAllString(res, ar.repl)
	}

	return res
}
