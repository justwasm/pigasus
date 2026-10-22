package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agyprompt"
)

// resolvePrintMode returns true if --print, --prompt or -p was given.
func resolvePrintMode(fs *flag.FlagSet) bool {
	return flagWasSet(fs, "print") || flagWasSet(fs, "prompt") || flagWasSet(fs, "p")
}

// customAgentFromFlags instantiates a custom agent configured from --agent.
func customAgentFromFlags(fs *flag.FlagSet) *agent.Agent {
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
		out = append(out, agyprompt.Subagent{Name: a.Name, Description: a.Description})
	}
	return out
}

// agentBody is the custom agent's Markdown body, or "" for the default agent.
func agentBody(a *agent.Agent) string {
	if a == nil {
		return ""
	}
	return a.Body
}

func knownAgent(name string) bool {
	return agent.ValidateAgent(name) == nil
}

// printPromptSet returns true if any of the print-prompt flags was explicitly set.
func printPromptSet(fs *flag.FlagSet) bool {
	for _, n := range []string{"print", "prompt", "p"} {
		if flagWasSet(fs, n) {
			return true
		}
	}
	return false
}

// printPrompt returns the prompt text passed to --print / --prompt / -p.
func printPrompt(fs *flag.FlagSet) string {
	for _, n := range []string{"print", "prompt", "p"} {
		if flagWasSet(fs, n) {
			return flagStringValue(fs, n)
		}
	}
	return ""
}

// flagWasSet reports whether the named flag was explicitly passed on the command line.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	var set bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// warnAgentMode warns when --mode is used with --agent, matching v1.3.5.
func warnAgentMode(fs *flag.FlagSet, sessionCreation bool) {
	if fs == nil {
		return
	}
	if flagStringValue(fs, "agent") != "" && flagStringValue(fs, "mode") != "" {
		fmt.Fprintln(os.Stderr, "warning: --mode is ignored when --agent is set")
	}
}

// resolveBoolAlias reads the canonical flag name first, falling back to alias.
func resolveBoolAlias(fs *flag.FlagSet, canonical, alias string) bool {
	if fs == nil {
		return false
	}
	if flagBoolValue(fs, canonical) {
		return true
	}
	if alias != "" && flagBoolValue(fs, alias) {
		return true
	}
	return false
}

// flagBoolValue returns the bool value of a flag, or false if unset.
func flagBoolValue(fs *flag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	return f.Value.String() == "true"
}

// flagIntValue returns the int value of a flag, or 0 if unset or not an int.
func flagIntValue(fs *flag.FlagSet, name string) int {
	if fs == nil {
		return 0
	}
	f := fs.Lookup(name)
	if f == nil {
		return 0
	}
	v, err := strconv.Atoi(f.Value.String())
	if err != nil {
		return 0
	}
	return v
}

// flagStringValue returns the string value of a flag, or "" if unset.
func flagStringValue(fs *flag.FlagSet, name string) string {
	if fs == nil {
		return ""
	}
	f := fs.Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}

// flagStringValues returns the per-occurrence values of a comma-separated flag.
func flagStringValues(fs *flag.FlagSet, name string) []string {
	raw := flagStringValue(fs, name)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
