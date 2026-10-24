package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func usageAgent(n string) string {
	return "Usage: " + n + " agent [flags]\n" +
		"\n" +
		"List available agents\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

// ---- agent / agents -------------------------------------------------------

func runAgents(j *JetskiCLI, args []string) error {
	if len(args) == 0 {
		WarnMode(j.ErrOut, j.FlagMode())
		if agentsLister != nil {
			names := agentsLister()
			if len(names) > 0 {
				sort.Strings(names)
				fmt.Fprintln(j.Out, "Available agents:")
				for _, name := range names {
					fmt.Fprintln(j.Out, name)
				}
			}
		}
		return nil
	}
	return fail(j, 1, fmt.Sprintf("Error: unexpected arguments: [%s]", strings.Join(args, " ")))
}

func legacyListAgentsFromMarkdown(pdir string, seen map[string]bool) []string {
	var names []string
	entries, err := os.ReadDir(filepath.Join(pdir, "agents"))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		n := strings.TrimSuffix(e.Name(), ".md")
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	return names
}

func legacyListAgentsFromJSON(pdir string, seen map[string]bool) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(pdir, "agents.json"))
	if err != nil {
		return nil, nil
	}
	var nested struct {
		Agents []struct {
			Name string `json:"name"`
		} `json:"agents"`
	}
	var names []string
	jerr := json.Unmarshal(data, &nested)
	if jerr == nil && nested.Agents != nil {
		for _, e := range nested.Agents {
			n := strings.TrimSpace(e.Name)
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			names = append(names, n)
		}
		return names, nil
	}
	var flat []struct {
		Name string `json:"name"`
	}
	if err2 := json.Unmarshal(data, &flat); err2 != nil {
		if jerr == nil {
			jerr = err2
		}
		return nil, jerr
	}
	for _, e := range flat {
		n := strings.TrimSpace(e.Name)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	return names, nil
}

// legacyListAgents enumerates a plugin's subagents from both sides of the
// 1.3.6 changelog line 13 / 35 fix:
//
//   - <plugin>/agents/*.md    — Markdown-sidecar entries, one per .md file
//   - <plugin>/agents.json    — root-level manifest, either nested
//     {agents: [{name, path}, ...]} or the older bare-array form
//
// The two sources are unioned, deduped by name (later definition wins on a
// tie, and the JSON manifest is read second so it can override the markdown
// name when the two disagree), and sorted. Empty / missing sources are
// silently dropped — the same way `legacyListSkills` returns nil for a
// missing directory. Parse errors against agents.json surface as
// (names, err, true): the listing is still returned so callers can decide
// whether the JSON-failure is fatal.
func legacyListAgents(pdir string) (names []string, jsonErr error, missing bool) {
	seen := map[string]bool{}
	names = append(names, legacyListAgentsFromMarkdown(pdir, seen)...)
	jNames, err := legacyListAgentsFromJSON(pdir, seen)
	if err != nil {
		jsonErr = err
	}
	names = append(names, jNames...)
	sort.Strings(names)
	missing = len(names) == 0
	return names, jsonErr, missing
}

// legacyLoadPluginAgents adds the agents field to legacyPluginEntry. The
// loader walks agents.json and agents/*.md once per plugin at load time; the
// list is what `agy plugin list --json` ships to machine consumers and the
// /plugin detail block's `Agents:` line.
func legacyLoadPluginAgents(pdir string) []string {
	names, _, _ := legacyListAgents(pdir)
	return names
}
