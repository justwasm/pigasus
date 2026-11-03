package printmode

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
)

const (
	cDimGrayOpen  = "\x1b[38;2;128;128;128m"
	cDimGrayClose = "\x1b[39m"
)

// GlamourTTYCheck decides whether the print-mode text-format chunk handler routes
// through the markdown glamour renderer.
var GlamourTTYCheck = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

var glamourTTYCheck = func() bool {
	return GlamourTTYCheck()
}

// glamourRenderWidth resolves the print-mode glamour render width when the write target is an io.Writer.
var glamourRenderWidth = func(out io.Writer) int {
	type fdWriter interface {
		Fd() uintptr
	}
	if w, ok := out.(fdWriter); ok {
		fd := int(w.Fd())
		if term.IsTerminal(fd) {
			if cols, _, err := term.GetSize(fd); err == nil && cols > 0 {
				return cols
			}
		}
	}
	return 0
}

// SkillInvocationNames returns every name the reference accepts for a
// skill, in priority order: display name, display name with the plugin
// prefix removed, then the SKILL.md parent-directory name.
func SkillInvocationNames(s skills.SkillInfo) []string {
	var names []string
	seen := map[string]bool{}
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		names = append(names, n)
	}
	add(s.Name)
	if i := strings.LastIndex(s.Name, ":"); i >= 0 {
		add(s.Name[i+1:])
	}
	if dir := filepath.Base(filepath.Dir(s.Path)); dir != "" && dir != "." && dir != string(filepath.Separator) {
		add(dir)
	}
	return names
}

var slashSkillCatalogHook func() []skills.SkillInfo

func slashSkillLookup(name string) (skills.SkillInfo, bool) {
	if name == "" {
		return skills.SkillInfo{}, false
	}
	catalog := printSlashSkillInfos()
	if slashSkillCatalogHook != nil {
		catalog = slashSkillCatalogHook()
	}
	for _, s := range catalog {
		for _, n := range SkillInvocationNames(s) {
			if n == name {
				return s, true
			}
		}
	}
	return skills.SkillInfo{}, false
}

// SkillBody returns the markdown body of a SKILL.md — the bytes after the
// closing `---` frontmatter fence, trimmed. A file with no frontmatter is
// returned whole (some skills ship a plain markdown body). Missing files
// and unterminated frontmatter yield "".
func SkillBody(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return strings.TrimSpace(s)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}
	return ""
}

// SlashSkillPrompt builds the model prompt sent for one skill invocation.
// When args is non-empty it is appended as a separate block so the user's
// trailing text still reaches the model.
func SlashSkillPrompt(info skills.SkillInfo, args string) string {
	instr := SkillBody(info.Path)
	if instr == "" {
		instr = strings.TrimSpace(info.Description)
	}
	if instr == "" {
		instr = "Follow the " + info.Name + " skill."
	}
	if args != "" {
		instr = strings.TrimRight(instr, "\n") + "\n\n" + args
	}
	return instr
}

// customSubagents lists the markdown agents added to the default agent's <subagents> section.
func customSubagents() []agyprompt.Subagent {
	return setup.CustomSubagents()
}

func maybeConsumeStaleness() {}

// ParseEffortWord parses the user's typed level into a canonical word.
//
// The vocabulary is the catalogue's: the five levels a model may run at
// (internal/features/models) — low, medium, high, xhigh and max. The
// reference recognises all five (official-docs/cli/commands/effort.md),
// and the catalogue already holds them, so the gate here asks the
// catalogue instead of carrying a second copy of the list.
//
// The canonical word is the trimmed, lower-cased input; a word outside
// the vocabulary is refused.
func ParseEffortWord(input string) (string, bool) {
	word := strings.ToLower(strings.TrimSpace(input))
	if !models.IsEffortLevel(word) {
		return "", false
	}
	return word, true
}

// FeatDiffRunner executes the feature-diff report if configured.
var FeatDiffRunner = func(w io.Writer, mode string) {}
