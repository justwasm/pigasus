// cmd/agy228probe — the cycle-228 wire-surface probe. It walks
// subagenttools.Tools(deps) and prints a JSON snapshot of the AGY 1.4.0
// subagent toolset as the clone will advertise it. No network, no
// model — just the schemas.
//
// This is the offline replacement for a real `agy -p "..."` invocation
// (network blocked here). The cycle deliverable quotes its output
// verbatim under "live probe results".
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/app/subagenttools"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "agy228probe:", err)
		os.Exit(1)
	}
}

func run() error {
	dir, err := os.MkdirTemp("", "agy228-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	deps := subagenttools.Deps{
		CWD:      dir,
		AgentDir: filepath.Join(dir, "agent"),
	}

	tools := subagenttools.Tools(deps)
	names := []string{}
	stubs := []string{}
	schemas := []map[string]any{}
	for _, t := range tools {
		def := t.Definition()
		names = append(names, def.Name)
		if strings.Contains(def.Description, "not yet implemented (cycle 228 stub)") {
			stubs = append(stubs, def.Name)
		}
		params, _ := json.Marshal(def.Parameters)
		schemas = append(schemas, map[string]any{
			"name":        def.Name,
			"description": def.Description,
			"parameters":  string(params),
		})
	}
	snapshot := map[string]any{
		"cycle":     228,
		"tools":     len(tools),
		"names":     names,
		"stubs":     stubs,
		"schemas":   schemas,
		"agentDir":  deps.AgentDir,
		"subagents": filepath.Join(deps.AgentDir, "subagents"),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snapshot); err != nil {
		return err
	}

	want := []string{"invoke_subagent", "define_subagent", "send_message", "manage_subagents"}
	for _, name := range want {
		found := false
		for _, n := range names {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("wire surface missing %q (have %v)", name, names)
		}
	}
	if len(tools) != 4 {
		return fmt.Errorf("wire surface has %d tools, want 4", len(tools))
	}
	return nil
}
