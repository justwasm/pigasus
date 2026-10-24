package printmode

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agyprompt"
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

func splitSlash(buf string) (string, string) {
	buf = strings.TrimPrefix(buf, "/")
	parts := strings.Fields(buf)
	if len(parts) == 0 {
		return "", ""
	}
	return parts[0], strings.TrimSpace(strings.TrimPrefix(buf, parts[0]))
}

// SkillInvocationNames returns every name the reference accepts for a
// skill, in priority order: display name, display name with the plugin
// prefix removed, then the SKILL.md parent-directory name.
func SkillInvocationNames(s jsonSkillInfo) []string {
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

var slashSkillCatalogHook func() []jsonSkillInfo

func slashSkillLookup(name string) (jsonSkillInfo, bool) {
	if name == "" {
		return jsonSkillInfo{}, false
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
	return jsonSkillInfo{}, false
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
func SlashSkillPrompt(info jsonSkillInfo, args string) string {
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

// CustomAgentFromFlags instantiates a custom agent configured from --agent.
func CustomAgentFromFlags(fs *flag.FlagSet) *agent.Agent {
	name := flagStringValue(fs, "agent")
	if name == "" || name == "default" {
		return nil
	}
	if a, ok := agent.Lookup(name); ok {
		return &a
	}
	return nil
}

// customSubagents lists the markdown agents added to the default agent's <subagents> section.
func customSubagents() []agyprompt.Subagent {
	agents := agent.Discover()
	out := make([]agyprompt.Subagent, 0, len(agents))
	for _, a := range agents {
		out = append(out, agyprompt.Subagent{
			Name:        a.Name,
			Description: a.Description,
		})
	}
	return out
}

// AgentBody returns a custom agent's Markdown body, or "" when there is none.
func AgentBody(a *agent.Agent) string {
	if a == nil {
		return ""
	}
	return a.Body
}

func maybeConsumeStaleness() {}

func trimSpace(s string) string {
	return strings.TrimSpace(s)
}

// ParseEffortWord parses the user's typed level into a canonical word.
func ParseEffortWord(input string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "low":
		return "low", true
	case "medium":
		return "medium", true
	case "high":
		return "high", true
	}
	return "", false
}

// FeatDiffRunner executes the feature-diff report if configured.
var FeatDiffRunner = func(w io.Writer, mode string) {}
