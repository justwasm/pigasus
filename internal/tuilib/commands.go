// commands.go — hardcoded slash command catalog for tuilib picker.
//
// Source of truth: /tmp/ref_p*.bin captures from running the v1.3.5 binary
// (agy.bak-1.3.5-2026-10-15T08-23-29-532Z) and pressing "/" to trigger the
// slash menu. The picker shows 5 visible rows on first page:
//
//   /add-dir          Add a directory to the workspace
//   /agents           List available custom agents
//   /artifact         View and review artifacts
//   /btw              Ask a side question without interrupting the current task
//   /changelog        Show release notes and changes
//    ↓ 94 more
//
// The full catalog (99 entries total: 36 system + 6 skill-plugin-prefixed +
// 57 /science:* + 1 /wrangler) is recovered by sending Down 95 times in the
// live picker and capturing the cursor row at each offset. Cursor positions
// from /tmp/scan_0.bin through /tmp/scan_95.bin yield 96 unique entries;
// the last 3 (/web-perf, /workers-best-practices, /wrangler) are confirmed
// via /tmp/scan_98.bin (cursor at /wrangler, last visible row).
package tuilib

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/justwasm/pigasus/internal/customcmd"
)

// SlashCommand is one row in the picker.
type SlashCommand struct {
	Name        string   // /add-dir
	Description string   // Add a directory to the workspace
	// Cycle 83: aliases shown in /help COMMANDS tab next to the
	// command name. e.g. Aliases: ["quit"] renders as '/exit (quit)'.
	// Empty / nil means no alias display.
	Aliases []string
}

// DefaultSlashCommands is the catalog of built-in slash commands
// surfaced by the tuilib picker. Order matches the v1.3.5 binary's
// /tmp/ref_p*.bin first-page + cursor-walk capture. Source-of-truth:
// live tmux captures of /Users/gear/.local/bin/agy.bak-1.3.5-*.
//
// 99 entries total, matching the ref's "↓ 94 more" hint exactly.
var DefaultSlashCommands = []SlashCommand{
	{Name: "/add-dir", Description: "Add a directory to the workspace"},
	{Name: "/agents", Description: "List available custom agents"},
	{Name: "/artifact", Description: "View and review artifacts"},
	{Name: "/btw", Description: "Ask a side question without interrupting the current task"},
	{Name: "/changelog", Description: "Show release notes and changes"},
	{Name: "/clear", Description: "Clear conversation and start a new one", Aliases: []string{"new"}},
	{Name: "/codesearch", Description: "Search code in the workspace (usage: /codesearch <query>)", Aliases: []string{"cs", "search"}},
	{Name: "/config", Description: "Open settings panel", Aliases: []string{"settings"}},
	{Name: "/context", Description: "Visualize current context usage"},
	{Name: "/copy", Description: "Copy the last planner response to the clipboard, the n-th with '/copy n', or the /btw answer with '/copy btw'"},
	{Name: "/credits", Description: "Show remaining G1 credits and purchase link"},
	{Name: "/diff", Description: "View uncommitted changes and per-turn diffs"},
	{Name: "/effort", Description: "Set the reasoning effort"},
	{Name: "/exit", Description: "Exit the CLI", Aliases: []string{"quit"}},
	{Name: "/feature-diff", Description: "Show AGY 1.4.2 feature parity (aligned / gap / diverged)", Aliases: []string{"featdiff", "diff-agy"}},
	{Name: "/feedback", Description: "Submit qualitative feedback to improve the agent"},
	{Name: "/fork", Description: "Create a branch of the current conversation at this point, optionally specifying a project ID to fork into", Aliases: []string{"branch"}},
	{Name: "/help", Description: "Show available commands and keybindings"},
	{Name: "/hooks", Description: "Manage hook configurations for tool events"},
	{Name: "/keybindings", Description: "Set custom keybindings"},
	{Name: "/logout", Description: "Log out"},
	{Name: "/mcp", Description: "Manage MCP servers"},
	{Name: "/model", Description: "Set a model, or run a single prompt on another model"},
	{Name: "/open", Description: "Open a file or view opened/edited files"},
	{Name: "/permissions", Description: "Manage tool permissions"},
	{Name: "/plugin", Description: "Manage, discover, install, uninstall, enable, and disable plugins"},
	{Name: "/remote-control", Description: "Turn on remote control for this session (off to disable)"},
	{Name: "/rename", Description: "Rename the current conversation"},
	{Name: "/resume", Description: "Browse and resume past conversations", Aliases: []string{"switch", "conversation"}},
	{Name: "/rewind", Description: "Rewind conversation to a previous message", Aliases: []string{"undo"}},
	{Name: "/skills", Description: "List available skills"},
	{Name: "/statusline", Description: "Toggle the statusline"},
	{Name: "/tasks", Description: "View background tasks"},
	{Name: "/title", Description: "Toggle custom terminal window title"},
	{Name: "/usage", Description: "View model quota usage", Aliases: []string{"quota"}},
	{Name: "/voice", Description: "Dictate a prompt using your microphone", Aliases: []string{"record"}},
	{Name: "/accounts", Description: "Manage and switch signed-in Google accounts"},
	{Name: "/provider", Description: "Manage custom model providers and endpoints", Aliases: []string{"providers"}},
	{Name: "/goal", Description: "Run until the specified goal is completely finished."},
	{Name: "/compact", Description: "Compact the conversation now (summarize history to free up the context window)."},
	{Name: "/schedule", Description: "Run an instruction on a recurring schedule or as a one-time timer."},
	{Name: "/browser", Description: "Invoke a browser agent for web tasks."},
	{Name: "/plan", Description: "Plan carefully before executing a task."},
	{Name: "/grill-me", Description: "Interview me to align on a plan."},
	{Name: "/teamwork", Description: "Invoke a team of agents to autonomously tackle large projects."},
	{Name: "/learn", Description: "Reflect on recent successes or corrections to capture reusable skills or rules."},
	{Name: "/boost", Description: "Invoke the Boost multi-agent orchestrator for complex tasks."},
	{Name: "/agents-sdk", Description: "Build AI agents on Cloudflare Workers using the Agents SDK. Load when creating stateful agents, durable workflows, real-time WebSocket apps, scheduled tasks, or chat agents with built-in state."},
	{Name: "/agy-customizations", Description: "Comprehensive guide and reference for the Antigravity Customization System. Use to explain how customizations work, their loading priority, discoverability, and authoring conventions."},
	{Name: "/antigravity-guide", Description: "Provides a comprehensive guide, quick reference, and sitemap for Google Antigravity (AGY), including the Antigravity CLI (agy), Antigravity 2.0, browser surface, and configuration conventions."},
	{Name: "/chrome-devtools-plugin:a11y-debugging", Description: "Uses Chrome DevTools MCP for accessibility (a11y) debugging and auditing based on web.dev guidelines. Use when testing semantic HTML, ARIA labels, keyboard navigation, color contrast, and screen-reader landmarks."},
	{Name: "/chrome-devtools-plugin:chrome-devtools", Description: "Uses Chrome DevTools via MCP for efficient debugging, troubleshooting and browser automation. Use when debugging web pages, automating browser interactions, capturing screenshots, or inspecting network/CPU profiles."},
	{Name: "/chrome-devtools-plugin:debug-optimize-lcp", Description: "Guides debugging and optimizing Largest Contentful Paint (LCP) using Chrome DevTools MCP tools. Use this skill whenever the user asks about LCP performance, hero image loading, font-render blocking, or critical resource prioritization."},
	{Name: "/chrome-devtools-plugin:memory-leak-debugging", Description: "Diagnoses and resolves memory leaks in JavaScript/Node.js applications. Use when a user reports high memory usage, OOM errors, or wants to analyze heap snapshots, retained objects, or detached DOM nodes."},
	{Name: "/chrome-devtools-plugin:troubleshooting", Description: "Uses Chrome DevTools MCP and documentation to troubleshoot connection and target issues. Trigger this skill when list_pages, new_page, or navigate commands fail or hang, or when browser attachment errors occur."},
	{Name: "/cloudflare", Description: "Comprehensive Cloudflare platform skill covering Workers, Pages, storage (KV, D1, R2), AI (Workers AI, Vectorize, Agents SDK), feature flags, networking, and observability."},
	{Name: "/cloudflare-email-service", Description: "Send and receive transactional emails with Cloudflare Email Service (Email Sending + Email Routing). Use when building email sending (Workers bindings), inbound parsing, DMARC/SPF/DKIM, or routing rules."},
	{Name: "/durable-objects", Description: "Create and review Cloudflare Durable Objects. Use when building stateful coordination (chat rooms, multiplayer games, booking systems), implementing per-key consistency, or needing hibernation/WebSocket support."},
	{Name: "/google-antigravity-sdk:google-antigravity-sdk", Description: "Design, implement, and debug autonomous AI agents and multi-agent systems using the Google Antigravity (AGY) SDK. ACTIVATE this skill when the user asks about SDK architecture, tool wiring, session lifecycle, or routing."},
	{Name: "/modern-web-guidance-plugin:chrome-extensions", Description: "Build and publish Chrome Extensions using Manifest V3 best practices. Use this skill whenever the user asks to create, modify, debug, or understand Manifest V3 extensions, service workers, or chrome.* APIs."},
	{Name: "/modern-web-guidance-plugin:modern-web-guidance", Description: "Search tool for modern web development best practices. MANDATORY: Execute FIRST for all HTML/CSS and clientside JS tasks. Do NOT skip — web APIs and CSS evolve quickly."},
	{Name: "/sandbox-sdk", Description: "Build sandboxed applications for secure code execution. Load when building AI code execution, code interpreters, CI/CD systems, interactive developer environments, or any untrusted-code runtime."},
	{Name: "/science:alphafold-database-fetch-and-analyze", Description: "Retrieve and analyze AlphaFold predicted structures for a protein. Use when the user provides a specific UniProt Accession ID and wants structural predictions, pLDDT confidence scores, or predicted aligned error visualization."},
	{Name: "/science:alphagenome-single-variant-analysis", Description: "Analyzes genetic variant effects on gene expression (RNA-seq), chromatin accessibility (DNASE), histone marks (ChIP), and transcription factors using AlphaGenome. Use when the user asks for variant-effect prediction at a single locus."},
	{Name: "/science:chembl-database", Description: "Query the ChEMBL database for bioactive molecules, drug targets, bioactivity data, approved drugs, and chemical structures. Use when the user asks about drug-target interactions, IC50/Ki values, or medicinal chemistry."},
	{Name: "/science:clinical-trials-database", Description: "Query ClinicalTrials.gov via APIv2. Use when you want to search for trials by condition, drug, location, status, or phase; retrieve trial details; or filter by eligibility criteria."},
	{Name: "/science:clinvar-database", Description: "Use when needing clinical significance, pathogenicity classifications (e.g., Pathogenic, Benign, VUS), clinical evidence rationales, or finding variants that have been reviewed by expert panels (ClinGen)."},
	{Name: "/science:dbsnp-database", Description: "Use when you want to look up, map, and search for short genetic variants (SNPs, indels) in NCBI's dbSNP database. Resolves between rsIDs, genomic coordinates (GRCh37/GRCh38), and allele frequencies."},
	{Name: "/science:embl-ebi-ols", Description: "Query and search the EMBL-EBI Ontology Lookup Service (OLS) for biomedical ontology terms, definitions, and hierarchies across 250+ ontologies (GO, HPO, DO, ChEBI, MONDO, Uberon, CL, etc.)."},
	{Name: "/science:encode-ccres-database", Description: "Query the ENCODE Registry of cis-Regulatory Elements (cCREs) via the SCREEN GraphQL API, or make custom queries to the ENCODE Portal REST API for processed epigenomic signal tracks."},
	{Name: "/science:ensembl-database", Description: "Query the Ensembl REST API and Ensembl genome database to resolve gene, transcript, and protein IDs, fetch genomic or protein sequences, retrieve gene structures (exons), and map variants to consequences."},
	{Name: "/science:foldseek-structural-search", Description: "Performs 3D structural searches of proteins against various databases (PDB, AlphaFold, CATH, MGnify, etc.) using the Foldseek API. Use ONLY when comparing a query structure against a structural database."},
	{Name: "/science:gnomad-database", Description: "Query the Genome Aggregation Database (gnomAD). Use when determining the rarity or allele frequency of specific genetic variants, retrieving gene-level constraint metrics (pLI, LOEUF), or filtering by population."},
	{Name: "/science:gtex-database", Description: "Use when you want to retrieve quantitative RNA expression data and variant eQTL information from the GTEx (Genotype-Tissue Expression) Project across 50+ human tissues."},
	{Name: "/science:human-protein-atlas-database", Description: "Use when you want to retrieve semi-quantitative protein expression and spatial localisation data from the Human Protein Atlas (HPA)."},
	{Name: "/science:interpro-database", Description: "Query InterPro for protein families, domains, families, and sites in proteins; find all proteins in a family or sharing a domain; explore species distribution for a domain; retrieve structural domain annotations."},
	{Name: "/science:jaspar-database", Description: "Query the JASPAR database for Transcription Factor (TF) binding profiles. Use when retrieving Position Frequency Matrices (PFMs) or Position Weight Matrices (PWMs) for a TF of interest, or scanning a sequence for predicted TFBS."},
	{Name: "/science:literature-search-arxiv", Description: "Search for scientific papers, preprints, and publications on arXiv. Extract metadata, abstracts, and download full-text PDFs or HTML versions of papers in physics, math, CS, q-bio, and stat categories."},
	{Name: "/science:literature-search-biorxiv", Description: "Browse, filter, and download life sciences, biology, and medical preprints from bioRxiv and medRxiv. Supports fetching paper metadata by DOI, full-text PDFs, and version history."},
	{Name: "/science:literature-search-europepmc", Description: "Search Europe PMC for scientific literature and download open-access full texts and PDFs. Retrieve full-text XML/plain text by PMCID, get citations, and resolve DOI <-> PMID mappings across 40M+ abstracts."},
	{Name: "/science:literature-search-openalex", Description: "Query the OpenAlex scholarly database for research papers, authors, institutions, topics, sources, publishers, funders, geo-locations, and keywords. OpenAlex is the open replacement for Microsoft Academic Graph."},
	{Name: "/science:ncbi-sequence-fetch", Description: "Retrieve protein and nucleotide sequences from NCBI databases using E-utilities. Supports direct accession lookup, CDS translation, gene+organism queries, and FASTA formatting."},
	{Name: "/science:openfda-database", Description: "Query, search, and download data from the openFDA API for drugs, devices, foods, tobacco, cosmetics, animal and veterinary products, substances, and enforcement reports."},
	{Name: "/science:opentargets-database", Description: "Query Open Targets Platform for target-disease associations, drug target discovery, tractability/safety data, genetics/omics evidence, known drugs, and clinical trial information for a target or disease."},
	{Name: "/science:pdb-database", Description: "Use when you want to search for or download experimentally-determined 3D structures for biomolecules (proteins, nucleic acids, bound ligands). Supports PDB ID queries, sequence search, and structure fetch."},
	{Name: "/science:protein-sequence-msa", Description: "Perform multiple sequence alignment of proteins with EBI Clustal Omega. Use when you need to align multiple sequences to assess similarity, domain conservation, or phylogenetic relationships."},
	{Name: "/science:protein-sequence-similarity-search", Description: "Searches for homologous protein sequences using MMseqs2 (fast, default) or BLAST (comprehensive, fallback). Trigger this whenever the user provides a query protein and wants similar sequences, homologs, or remote homology."},
	{Name: "/science:pubchem-database", Description: "Query PubChem, search by name/CID/SMILES, retrieve properties, similarity/substructure searches, bioactivity, for cheminformatics. Use when a user asks about a small molecule's identifiers, structure, or bioactivity data."},
	{Name: "/science:pubmed-database", Description: "Search PubMed for scientific literature, including published clinical trials. Fetch abstracts and full text. Link published research to biological entities (genes, proteins, variants, drugs, diseases)."},
	{Name: "/science:pymol", Description: "Visualize, analyze, and render protein and molecular structures using PyMOL. Use when the user wants to create images of protein structures, perform structural alignment, measure distances/angles, or color by property."},
	{Name: "/science:quickgo-database", Description: "Query QuickGO and the Evidence & Conclusion Ontology (ECO) REST API. Use this when you need to map genes to biological processes, molecular functions, or cellular components; retrieve GO annotations; or traverse the ontology graph."},
	{Name: "/science:reactome-database", Description: "Query the Reactome database (Analysis and Content Services). Use when the user asks about pathway analysis, gene list enrichment, retrieving reaction/pathway diagrams, or mapping identifiers across biological systems."},
	{Name: "/science:science-skills-common", Description: "Shared Python package for Science Skills, currently containing http_client -- a unified HTTP client with rate limiting, retries, and exponential backoff, plus helpers used across all Science Skills."},
	{Name: "/science:scienceskillscommon", Description: "Shared Python package for Science Skills, currently containing http_client -- a unified HTTP client with rate limiting, retries, and exponential backoff, plus helpers used across all Science Skills."},
	{Name: "/science:string-database", Description: "Query the STRING database for protein-protein interactions (PPIs), functional enrichment, and homology. Use when the user asks about interactions for a gene set, network visualization, or functional partners across species."},
	{Name: "/science:ucsc-conservation-and-tfbs", Description: "Fetch Evolutionary Conservation scores (phyloP, phastCons) and Transcription Factor Binding Sites (TFBS) from the UCSC Genome Browser. Use when analysing a variant's conservation or whether a region is bound by known TFs."},
	{Name: "/science:unibind-database", Description: "Queries the UniBind database for experimentally validated transcription factor (TF) binding sites. Use when retrieving direct TF-DNA interaction evidence for a TF of interest in human/mouse."},
	{Name: "/science:uniprot-database", Description: "Access protein metadata, function, taxonomy, and sequences across UniProtKB, UniParc, and UniRef. Use when searching for proteins, mapping identifiers, retrieving reviewed (Swiss-Prot) entries, or fetching canonical sequences."},
	{Name: "/science:uv", Description: "Checks whether the uv Python package manager is installed and installs it if missing. Ensures uv is on PATH. Use when another skill requires uv as a dependency, or when scaffolding Python projects."},
	{Name: "/science:workflow-skill-creator", Description: "Distills a completed user workflow or interaction into a reusable agent skill. Use when the user asks to turn their workflow, interaction, or multi-step procedure into a portable skill artifact."},
	{Name: "/terminal-browser", Description: "A real browser running inside the terminal. It splits the human's terminal pane automatically, so you can show a website side by side with the conversation without leaving the CLI."},
	{Name: "/web-perf", Description: "Analyzes web performance using Chrome DevTools MCP. Measures Core Web Vitals (LCP, INP, CLS) and supplementary metrics (FCP, TBT, Speed Index), inspecting network/CPU, and produces a prioritized list of optimizations."},
	{Name: "/workers-best-practices", Description: "Reviews and authors Cloudflare Workers code against production best practices. Load when writing new Workers, reviewing Worker code, configuring bindings, secrets, wrangler config, or hardening for production."},
	{Name: "/wrangler", Description: "Cloudflare Workers CLI for deploying, developing, and managing Workers, KV, R2, D1, Vectorize, Hyperdrive, Workers AI, Containers, Queues, Workflows, Pages, and more. Use when scaffolding new Workers projects, configuring wrangler.jsonc/wrangler.toml, setting up bindings, running local dev (wrangler dev), or deploying to Cloudflare."},
}

// SortedSlashCommands returns DefaultSlashCommands sorted by Name in
// plain byte-ascending order. The reference's `/help` COMMANDS tab is a
// flat alphabetical sort of the whole catalog, even though the "/"
// picker keeps registration order (built-ins in registration order,
// then skills). Verified against REF v1.4.0: `/agents-sdk` and
// `/agy-customizations` sit between `/agents` and `/artifact` in
// /help, while the picker's second and third rows are `/agents` and
// `/artifact` — see /tmp/agycmp/ref_catalog.txt (help, sorted) vs
// /tmp/agycmp/hotpath/picker/ref.000-slash.txt (picker, unsorted).
//
// The sort key is the Name as stored (the "/clear" of the display
// form "/clear (new)"), which reproduces REF's ordering exactly.
func SortedSlashCommands() []SlashCommand {
	out := make([]SlashCommand, len(DefaultSlashCommands))
	copy(out, DefaultSlashCommands)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// FilterSlashCommands returns the subset of DefaultSlashCommands whose
// Name contains prefix as a case-insensitive substring anywhere in
// the name (not just at the start). The reference v1.3.6 picker
// uses substring matching: typing `/s` matches `/codesearch`
// (contains "s"), `/config (settings)`, `/resume (switch)`, and
// any other command name with an "s" anywhere.
//
// Cycle 146: chrome-devtools-plugin:* entries ARE excluded from
// the picker (cycle 135's filter, restored after cycle 146's
// revert was wrong). The reference v1.3.6 picker at empty prefix
// shows 94 entries (5 visible + "↓ 89 more" at offset=0;
// "↓ 86 more" at offset=3 after 5 Down presses — 86 + 3 + 5 =
// 94 total). The /help COMMANDS tab uses a separate filter that
// keeps all 99 entries (footer "[1-18 of 99 items]").
//
// /help COMMANDS tab uses a separate filter (the cycle-83 helper)
// and includes the chrome-devtools-plugin:* entries — captured at
// /tmp/agycmp/hotpath/help_tabs/ref.001-second.txt showing
// "[1-18 of 99 items]".
func FilterSlashCommands(prefix string) []SlashCommand {
	out := make([]SlashCommand, 0, len(DefaultSlashCommands))
	for _, c := range DefaultSlashCommands {
		if strings.Contains(c.Name, "chrome-devtools-plugin:") {
			continue
		}
		if prefix == "" || containsFold(c.Name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// containsFold is strings.Contains with Unicode case-folding. The
// picker's substring filter is case-insensitive so `/SKILL` matches
// `/skills`. Cycle 235: walk runes (one rune per step) and fold with
// unicode.ToLower so multi-byte graphemes (CJK: 科, 学; Hiragana:
// ひらがな; Cyrillic: п) and the Turkish dotted İ (U+0130, which
// simple-folds to U+0069 'i') compare as single tokens instead of
// being dropped by the prior ASCII byte-by-byte scan. Strings that
// are not valid UTF-8 fall back to bytes.Equal so non-UTF8 binary
// names — none in the current catalog, but the picker accepts
// arbitrary plugin names — still get a deterministic answer.
func containsFoldRunes(sr, pr []rune) bool {
	if len(pr) > len(sr) {
		return false
	}
	for i := 0; i+len(pr) <= len(sr); i++ {
		match := true
		for j := 0; j < len(pr); j++ {
			if unicode.ToLower(sr[i+j]) != unicode.ToLower(pr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func containsFoldBytes(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			sc, pc := s[i+j], substr[j]
			if 'A' <= sc && sc <= 'Z' {
				sc += 'a' - 'A'
			}
			if 'A' <= pc && pc <= 'Z' {
				pc += 'a' - 'A'
			}
			if sc != pc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func containsFold(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	if utf8.ValidString(s) && utf8.ValidString(substr) {
		return containsFoldRunes([]rune(s), []rune(substr))
	}
	return containsFoldBytes(s, substr)
}

// hasPrefixFold is strings.HasPrefix with ASCII case-folding. The
// picker matches '/ADD' against '/add-dir' so capital letters still
// find entries.
func hasPrefixFold(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		sc := s[i]
		pc := prefix[i]
		if 'A' <= sc && sc <= 'Z' {
			sc += 'a' - 'A'
		}
		if 'A' <= pc && pc <= 'Z' {
			pc += 'a' - 'A'
		}
		if sc != pc {
			return false
		}
	}
	return true
}

// FilterSlashCommandsWithCustom is the picker integration point for
// the cycle custom-slash-commands-no-plugin-injection contract. The
// picker calls it to merge the hardcoded DefaultSlashCommands
// catalog with customcmd-loaded plugin / project / global commands.
// The merge follows the AGY 1.2.16 precedence (built-in > plugin >
// project > global) so plugin-defined commands are never silently
// overridden by a same-named project command.
//
// The custom slice is the output of customcmd.Load; the picker
// passes whatever the loader returned at startup. Passing nil falls
// back to FilterSlashCommands with no extra rows.
//
// The existing FilterSlashCommands stays unchanged so the cycle-83 /
// cycle-146 / cycle-184 picker tests (which compare the catalog
// against DefaultSlashCommands alone) still pin their invariants.
func FilterSlashCommandsWithCustom(prefix string, custom []customcmd.Command) []SlashCommand {
	if len(custom) == 0 {
		return FilterSlashCommands(prefix)
	}
	builtins := make([]customcmd.BuiltIn, len(DefaultSlashCommands))
	for i, c := range DefaultSlashCommands {
		builtins[i] = customcmd.BuiltIn{Name: c.Name, Description: c.Description}
	}
	merged, _ := customcmd.MergeWithBuiltins(builtins, custom)
	out := make([]SlashCommand, 0, len(merged))
	for _, c := range merged {
		name := "/" + c.Name
		if strings.Contains(name, "chrome-devtools-plugin:") {
			continue
		}
		if prefix == "" || containsFold(name, prefix) {
			out = append(out, SlashCommand{Name: name, Description: c.Description})
		}
	}
	return out
}
