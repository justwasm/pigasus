// canonical_test.go — the alias rule, pinned.
//
// Cycle 381, tranche 88: the rule that resolves a typed command name
// to its canonical name moved here from the host (the cycle-206
// canonicalPanelCommand). It is one rule: a command's own name
// resolves to itself, an alias resolves to the command it names, and
// anything else is not a command at all.
package slashcommands

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// TestCanonical pins the alias resolution.
//
// The cases are the cycle-206 ones: the aliases the reference answers
// with a panel, the commands that are their own names, and the empty
// name.
func TestCanonical(t *testing.T) {
	cases := map[string]string{
		"cs":               "codesearch",
		"search":           "codesearch",
		"settings":         "config",
		"quota":            "usage",
		"undo":             "rewind",
		"new":              "clear",
		"quit":             "exit",
		"featdiff":         "feature-diff",
		"diff-agy":         "feature-diff",
		"record":           "voice",
		"switch":           "resume",
		"conversation":     "resume",
		"providers":        "provider",
		"plugins":          "plugin",
		"teamwork":         "teamwork-preview",
		"teamwork-preview": "teamwork-preview",
		"codesearch":       "codesearch",
		"tasks":            "tasks",
		"artifact":         "artifact",
		"context":          "context",
		"clear":            "clear",
		"mcp":              "mcp",
	}
	for in, want := range cases {
		got, ok := Canonical(in)
		if !ok || got != want {
			t.Errorf("Canonical(%q) = (%q, %v), want (%q, true)", in, got, ok, want)
		}
	}
	// The leading slash is optional and never part of the answer.
	if got, ok := Canonical("/clear"); !ok || got != "clear" {
		t.Errorf("Canonical(\"/clear\") = (%q, %v), want (\"clear\", true)", got, ok)
	}
	// An unknown name is not a command.
	for _, in := range []string{"", "zzznotacommand", "/"} {
		got, ok := Canonical(in)
		if ok {
			t.Errorf("Canonical(%q) = (%q, true), want (false)", in, got)
		}
	}
}

// TestCanonicalBranchIsItsOwnCommand pins the one name the catalogue
// and the host disagree about.
//
// The catalogue lists "branch" as an alias of "/fork", and the /help
// COMMANDS tab renders it that way. The host answers "/branch" with a
// command of its own, so the word resolves to itself and never to
// "/fork".
func TestCanonicalBranchIsItsOwnCommand(t *testing.T) {
	got, ok := Canonical("branch")
	if !ok || got != "branch" {
		t.Fatalf("Canonical(\"branch\") = (%q, %v), want (\"branch\", true)", got, ok)
	}
	if got, ok := Canonical("fork"); !ok || got != "fork" {
		t.Fatalf("Canonical(\"fork\") = (%q, %v), want (\"fork\", true)", got, ok)
	}
}

// TestCatalogRowsCarryTheDocumentedAliases pins the two aliases the
// official-docs survey named (cycle 383, slice A).
//
//   - A-6  `/plugin` has the alias `/plugins`
//     (official-docs/cli/reference.md:41, official-docs/slash-commands.md:159).
//   - A-8  the command is `/teamwork-preview` and `/teamwork` is its
//     alias (official-docs/cli/reference.md:50,
//     official-docs/slash-commands.md:34).
//
// Neither alias is a command of its own — the exception the alias rule
// knows is "/branch" — so both resolve to the command they name.
func TestCatalogRowsCarryTheDocumentedAliases(t *testing.T) {
	cases := []struct {
		name    string
		aliases []string
	}{
		{"/plugin", []string{"plugins"}},
		{"/teamwork-preview", []string{"teamwork"}},
	}
	for _, c := range cases {
		bare := strings.TrimPrefix(c.name, "/")
		row, ok := rowsByName[bare]
		if !ok {
			t.Errorf("%s is not in the catalogue", c.name)
			continue
		}
		if len(row.Aliases) != len(c.aliases) {
			t.Errorf("%s carries the aliases %v, want %v", c.name, row.Aliases, c.aliases)
			continue
		}
		for i, want := range c.aliases {
			if row.Aliases[i] != want {
				t.Errorf("%s carries the alias %q, want %q", c.name, row.Aliases[i], want)
			}
			if canonicalNames[want] {
				t.Errorf("the alias %q of %s is a command of its own", want, c.name)
			}
			if got, ok := Canonical(want); !ok || got != bare {
				t.Errorf("Canonical(%q) = (%q, %v), want (%q, true)", want, got, ok, bare)
			}
		}
	}
}

// TestPanelFor pins the command → panel answer: the aliases reach
// their command's panel, and the commands that open no panel say so.
func TestPanelFor(t *testing.T) {
	cases := map[string]panelregistry.Kind{
		"mcp":          panelregistry.MCP,
		"codesearch":   panelregistry.CodeSearch,
		"cs":           panelregistry.CodeSearch,
		"search":       panelregistry.CodeSearch,
		"settings":     panelregistry.Config,
		"quota":        panelregistry.Usage,
		"undo":         panelregistry.Rewind,
		"featdiff":     panelregistry.FeatDiff,
		"diff-agy":     panelregistry.FeatDiff,
		"providers":    panelregistry.Provider,
		"pool":         panelregistry.Accounts,
		"accounts":     panelregistry.Accounts,
		"feature-diff": panelregistry.FeatDiff,
		"plugins":      panelregistry.Plugin,
	}
	for name, want := range cases {
		got, ok := PanelFor(name)
		if !ok || got != want {
			t.Errorf("PanelFor(%q) = (%v, %v), want (%v, true)", name, got, ok, want)
		}
	}
	for _, name := range []string{"clear", "help", "fork", "btw", "voice", "record", "branch", "teamwork", "teamwork-preview", "zzznotacommand"} {
		if _, ok := PanelFor(name); ok {
			t.Errorf("PanelFor(%q) unexpectedly opens a panel", name)
		}
	}
}

// TestPanelKindsMatchTheRegistry pins the derivation: the row's panel
// kind is the one the panel registry holds.
func TestPanelKindsMatchTheRegistry(t *testing.T) {
	for _, c := range Default {
		kind, ok := panelregistry.CommandPanel(c.Name[1:])
		if !ok {
			if c.Panel != panelregistry.None {
				t.Errorf("%s carries panel %v, the registry holds none", c.Name, c.Panel)
			}
			continue
		}
		if c.Panel != kind {
			t.Errorf("%s carries panel %v, the registry holds %v", c.Name, c.Panel, kind)
		}
	}
}

// TestKnown pins the composer's "is this a command" question: the
// picker offers the command exactly, or under one of its aliases.
func TestKnown(t *testing.T) {
	for _, token := range []string{"/clear", "/exit", "/quit", "/new", "/cs", "/quit"} {
		if !Known(token) {
			t.Errorf("Known(%q) = false, want true", token)
		}
	}
	// The bare spelling of a command is not a command token: the
	// reference colours "/clear" blue, and "clear" alone is prose.
	for _, token := range []string{"clear", "exit", "zzznotacommand", ""} {
		if Known(token) {
			t.Errorf("Known(%q) = true, want false", token)
		}
	}
}

// TestDispatchedFlagsAreConsistent pins the dispatched flag.
//
// A row may be dispatched *and* open a panel: the panel is tried
// first, and the handler answers the cases the panel does not (the
// "/model <name>" setter, for instance).
//
// The advertised rows the host does not dispatch are the three
// panel-only commands of the clone's superset and the skill rows the
// picker merely advertises.
func TestDispatchedFlagsAreConsistent(t *testing.T) {
	notDispatched := []string{
		"/accounts", "/pool", "/provider",
		"/agents-sdk", "/agy-customizations", "/antigravity-guide",
		"/chrome-devtools-plugin:a11y-debugging",
		"/chrome-devtools-plugin:chrome-devtools",
		"/chrome-devtools-plugin:cookie-debugging",
		"/chrome-devtools-plugin:debug-optimize-lcp",
		"/chrome-devtools-plugin:memory-leak-debugging",
		"/chrome-devtools-plugin:troubleshooting",
		"/cloudflare", "/cloudflare-email-service", "/durable-objects",
		"/google-antigravity-sdk:google-antigravity-sdk",
		"/modern-web-guidance-plugin:chrome-extensions",
		"/modern-web-guidance-plugin:modern-web-guidance",
		"/sandbox-sdk",
		"/science:alphafold-database-fetch-and-analyze",
		"/science:alphagenome-atlas-website-links",
		"/science:alphagenome-single-variant-analysis",
		"/science:alphagenome-variant-impact-score",
		"/science:biomolecular-structure-viewer",
		"/science:chembl-database",
		"/science:clinical-trials-database",
		"/science:clinvar-database",
		"/science:credentials",
		"/science:dbsnp-database",
		"/science:embl-ebi-ols",
		"/science:encode-ccres-database",
		"/science:ensembl-database",
		"/science:foldseek-structural-search",
		"/science:gnomad-database",
		"/science:gtex-database",
		"/science:human-protein-atlas-database",
		"/science:interpro-database",
		"/science:jaspar-database",
		"/science:literature-search-arxiv",
		"/science:literature-search-biorxiv",
		"/science:literature-search-europepmc",
		"/science:literature-search-openalex",
		"/science:ncbi-sequence-fetch",
		"/science:openfda-database",
		"/science:opentargets-database",
		"/science:pdb-database",
		"/science:predictingthepast",
		"/science:protein-sequence-msa",
		"/science:protein-sequence-similarity-search",
		"/science:pubchem-database",
		"/science:pubmed-database",
		"/science:pymol",
		"/science:quickgo-database",
		"/science:reactome-database",
		"/science:string-database",
		"/science:ucsc-conservation-and-tfbs",
		"/science:unibind-database",
		"/science:uniprot-database",
		"/science:uv",
		"/science:workflow-skill-creator",
		"/terminal-browser", "/web-perf", "/workers-best-practices", "/wrangler",
	}
	want := map[string]bool{}
	for _, n := range notDispatched {
		want[n] = true
	}
	for _, c := range Default {
		if c.Dispatched == want[c.Name] {
			t.Errorf("%s: dispatched = %v, want %v", c.Name, c.Dispatched, !want[c.Name])
		}
		delete(want, c.Name)
	}
	for name := range want {
		t.Errorf("%s is not in the catalogue", name)
	}
	for _, c := range Hidden {
		if !c.Dispatched {
			t.Errorf("%s is a hidden command, so the host dispatches it", c.Name)
		}
		if c.Description != "" {
			t.Errorf("%s is hidden, so the picker does not describe it", c.Name)
		}
	}
}
