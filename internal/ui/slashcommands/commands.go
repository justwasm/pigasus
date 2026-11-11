// Package slashcommands owns the slash-command vocabulary: which
// commands exist, what they are called, what they do, which spellings
// are aliases, and which panel a command opens.
//
// The vocabulary was three tables before this package (cycle 381,
// tranche 88): the host's slash-command map (internal/cliapp), the
// picker's catalogue (internal/ui/tuilib) and the panel registry's
// command table (internal/ui/panelregistry). Each was a partial list
// and they disagreed — the host spelled the aliases out as separate map
// keys, the catalogue carried them in an Aliases field, and a third
// copy of the alias rule scanned the catalogue at runtime. This
// package is the one owner: the rows, the alias rule and the answer to
// "which panel does this command open".
//
// # The alias rule
//
// One rule, and Canonical is where it lives: a command's own name
// resolves to itself, an alias resolves to the command it names, and
// anything else is not a command at all. An alias never shadows a
// command of its own — "branch" is an alias of "/fork" in the
// catalogue and a command of its own in the host, so the word resolves
// to itself.
//
// The host resolves the typed name through this rule before it
// consults its dispatch table, so an alias and its command reach the
// same handler. The picker and the /help COMMANDS tab render the
// aliases next to the name ("/exit (quit)"), so every advertised alias
// is a spelling the user may type.
//
// # The reference's catalogue
//
// Source of truth: the agy 1.4.8 binary's own picker
// (sha256 09fcdd8554e03bed9b2ec936e454c7f115d5610610de09ed7d79fa2feae76940).
// Pressing "/" at an empty prefix shows five rows and "↓ 100 more":
//
//	/add-dir          Add a directory to the workspace
//	/agents           List available custom agents
//	/artifact         View and review artifacts
//	/btw              Ask a side question without interrupting the current task
//	/changelog        Show release notes and changes
//	 ↓ 100 more
//
// — five visible rows plus 100 more is 105 entries, the same count the
// /help COMMANDS tab reports ("[1-33 of 105 items]"). A Down-walk of the
// whole picker yields those 105 names.
//
// The catalog holds 108 entries: the reference's 105 plus the clone's
// three documented superset commands (/feature-diff, /accounts,
// /provider); the clone's fourth superset row, /pool, makes 109 rows
// here. The reference's own rows are the 41 /science:* skills of the
// installed `science` plugin, the 6 chrome-devtools-plugin:* skills
// of the installed `chrome-devtools-plugin` plugin, the 11 workspace
// skills and the 47 built-in commands. The plugin rows are environment
// state — the reference enumerates the installed plugins at runtime and
// the clone keeps a snapshot of them.
//
// Cycle 351 (agy 1.4.8): the chrome-devtools-plugin:* rows are back in
// the picker (the 1.3.6 filter that hid them is gone), the six rows the
// snapshot was missing are in, and the two stale `/science:*skills-common`
// spellings the plugin no longer ships are out.
//
// The print mode keeps its own copy of the alias spellings
// (internal/printmode/print_slash.go spells "featdiff", "diff-agy",
// "record", "quit" and "new" out by hand). It is left alone on
// purpose: the print mode has its own slash grammar (internal/core/
// slashparse holds the host's rule, the print mode keeps its own —
// cycle 381, invariant 173), so canonicalising the names there would
// change what a print-mode run answers. Unifying the two is a
// behaviour change and wants its own evidence.
package slashcommands

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/prompt/customcmd"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// CommandScope defines the visibility and ownership boundary of a slash command.
type CommandScope int

const (
	// ScopeHost indicates a command that is purely handled by the TUI frontend host.
	// It is always available regardless of what backend or agent is connected.
	ScopeHost CommandScope = iota

	// ScopeCapability indicates a command that depends on specific capabilities
	// of the connected agent backend (e.g. model switching, reasoning effort).
	ScopeCapability

	// ScopeEngine indicates a command that is exclusive to a specific engine
	// (e.g. "pigasus" native commands like /accounts, /pool, /credits).
	ScopeEngine

	// ScopeDynamic indicates a command provided dynamically at runtime by an agent.
	ScopeDynamic
)

// Command is one row of the slash-command vocabulary.
type Command struct {
	// Name is the command's canonical name, in the display form the
	// picker and the /help panel render ("/clear").
	Name string
	// Description is the row's text: the picker's second column and
	// the /help COMMANDS tab's description.
	Description string
	// Aliases are the alternative spellings of the command. The
	// reference shows them next to the name in the /help COMMANDS tab
	// ("/exit (quit)") and the host answers them as the command
	// itself.
	Aliases []string
	// Panel is the panel the command opens. panelregistry.None when
	// the command opens no panel.
	//
	// The value comes from the panel registry, which owns the
	// command \u2192 panel mapping; this package only reads it, so the
	// mapping has exactly one owner.
	Panel panelregistry.Kind
	// Dispatched reports whether the host answers the command with
	// its own handler (the host's slash-command table). A row that is
	// neither dispatched nor opening a panel is a row the picker
	// merely advertises — a skill, answered by the skill lookup.
	Dispatched bool
	// Source identifies where this command originated (Builtin, Agent, Skill).
	Source CommandSource
	// Badge is an optional short indicator rendered in the popup (e.g. "agent", "acp", "skill").
	Badge string

	// Scope defines the visibility and ownership boundary of this command.
	Scope CommandScope
	// Engine identifies the required engine name when Scope == ScopeEngine (e.g. "pigasus").
	Engine string
	// RequiredCap specifies the capabilities needed when Scope == ScopeCapability.
	RequiredCap agentturn.Capability
}

// ScopeContext provides the runtime environment for filtering slash commands.
type ScopeContext struct {
	EngineName   string
	Capabilities agentturn.Capability
	Dynamic      []Command
}

// IsAvailable checks whether a command is accessible within the given ScopeContext.
func IsAvailable(c Command, ctx ScopeContext) bool {
	engineName := ctx.EngineName
	if engineName == "" {
		engineName = "pigasus"
	}
	caps := ctx.Capabilities
	if engineName == "pigasus" && caps == 0 {
		caps = agentturn.CapAll
	}

	switch c.Scope {
	case ScopeHost:
		return true
	case ScopeCapability:
		if c.RequiredCap == 0 {
			return true
		}
		return caps&c.RequiredCap != 0
	case ScopeEngine:
		return c.Engine == "" || c.Engine == engineName
	case ScopeDynamic:
		return true
	default:
		return true
	}
}

// CommandSource identifies where a command originates.
type CommandSource int

const (
	SourceBuiltin CommandSource = iota
	SourceAgent
	SourceSkill
)

// Default is the catalogue the picker offers, in registration order.
//
// The order is the reference's: the built-in commands first, then the
// skill rows. The /help COMMANDS tab sorts the same rows; the picker
// keeps this order.
var Default = buildCatalogue()

// Hidden lists the commands the host answers that the picker does not
// advertise.
//
// They are the TUI's hidden commands (docs/guide/hidden-commands-and-flags.md):
// "continue", "login", "narration", "quote" and "share" have a handler
// and no row in the catalogue, and "branch" is a command of its own
// even though the catalogue lists the word as an alias of "/fork".
var Hidden = []Command{
	{Name: "/branch", Dispatched: true},
	{Name: "/continue", Dispatched: true},
	{Name: "/login", Dispatched: true},
	{Name: "/narration", Dispatched: true},
	{Name: "/quote", Dispatched: true},
	{Name: "/share", Dispatched: true},
	// openagy-only host surfaces. They are remote-agent commands; the
	// reference has no CLI equivalent, so they stay off the picker.
	{Name: "/terminal", Dispatched: true},
	{Name: "/upload", Dispatched: true},
	{Name: "/triggers", Dispatched: true},
}

// buildCatalogue is the advertised catalogue with the panel kinds the
// panel registry holds.
//
// The registry owns the command \u2192 panel mapping, so the rows read it
// rather than carrying a second copy of it.
func buildCatalogue() []Command {
	rows := []Command{
		{Name: "/add-dir", Description: "Add a directory to the workspace", Dispatched: true},
		{
			Name:        "/agents",
			Description: "List available custom agents",
			Scope:       ScopeCapability,
			RequiredCap: agentturn.CapAgents,
			Dispatched:  true,
		},
		{Name: "/artifact", Description: "View and review artifacts", Dispatched: true},
		{Name: "/btw", Description: "Ask a side question without interrupting the current task", Dispatched: true},
		{Name: "/changelog", Description: "Show release notes and changes", Dispatched: true},
		{Name: "/clear", Description: "Clear conversation and start a new one", Aliases: []string{"new"}, Dispatched: true},
		{Name: "/codesearch", Description: "Search code in the workspace (usage: /codesearch <query>)", Aliases: []string{"cs", "search"}, Dispatched: true},
		{Name: "/config", Description: "Open settings panel", Aliases: []string{"settings"}, Dispatched: true},
		{Name: "/context", Description: "Visualize current context usage", Dispatched: true},
		{Name: "/copy", Description: "Copy the last planner response to the clipboard, the n-th with '/copy n', or the /btw answer with '/copy btw'", Dispatched: true},
		{Name: "/credits", Description: "Show remaining G1 credits and purchase link", Dispatched: true},
		{Name: "/diff", Description: "View uncommitted changes and per-turn diffs", Dispatched: true},
		{Name: "/effort", Description: "Set the reasoning effort", Dispatched: true},
		{Name: "/exit", Description: "Exit the CLI", Aliases: []string{"quit"}, Dispatched: true},
		{Name: "/feature-diff", Description: "Show AGY 1.4.2 feature parity (aligned / gap / diverged)", Aliases: []string{"featdiff", "diff-agy"}, Dispatched: true},
		{Name: "/feedback", Description: "Submit qualitative feedback to improve the agent", Dispatched: true},
		{Name: "/fork", Description: "Create a branch of the current conversation at this point, optionally specifying a project ID to fork into", Aliases: []string{"branch"}, Dispatched: true},
		{Name: "/help", Description: "Show available commands and keybindings", Dispatched: true},
		{Name: "/hooks", Description: "Manage hook configurations for tool events", Dispatched: true},
		{Name: "/keybindings", Description: "Set custom keybindings", Dispatched: true},
		{Name: "/logout", Description: "Log out", Dispatched: true},
		{Name: "/mcp", Description: "Manage MCP servers", Dispatched: true},
		{Name: "/model", Description: "Set a model, or run a single prompt on another model", Dispatched: true},
		{Name: "/open", Description: "Open a file or view opened/edited files", Dispatched: true},
		{Name: "/permissions", Description: "Manage tool permissions", Dispatched: true},
		{Name: "/plugin", Description: "Manage, discover, install, uninstall, enable, and disable plugins", Aliases: []string{"plugins"}, Dispatched: true},
		{Name: "/projects", Description: "View and manage local projects", Panel: panelregistry.Projects, Dispatched: true},
		{Name: "/remote-control", Description: "Turn on remote control for this session (off to disable)", Dispatched: true},
		{Name: "/rename", Description: "Rename the current conversation", Dispatched: true},
		{Name: "/resume", Description: "Browse and resume past conversations", Aliases: []string{"switch", "conversation"}, Dispatched: true},
		{Name: "/rewind", Description: "Rewind conversation to a previous message", Aliases: []string{"undo"}, Dispatched: true},
		{Name: "/skills", Description: "List available skills", Dispatched: true},
		{Name: "/statusline", Description: "Toggle the statusline", Dispatched: true},
		{
			Name:        "/tasks",
			Description: "View background tasks",
			Scope:       ScopeCapability,
			RequiredCap: agentturn.CapTasks,
			Dispatched:  true,
		},
		{Name: "/title", Description: "Toggle custom terminal window title", Dispatched: true},
		{Name: "/usage", Description: "View model quota usage", Aliases: []string{"quota"}, Dispatched: true},
		{Name: "/voice", Description: "Dictate a prompt using your microphone", Aliases: []string{"record"}, Dispatched: true},
		{Name: "/accounts", Description: "Manage and switch signed-in Google accounts"},
		{Name: "/pool", Description: "Configure the account-pool routing policy"},
		{Name: "/provider", Description: "Manage custom model providers and endpoints", Aliases: []string{"providers"}},
		{Name: "/goal", Description: "Run until the specified goal is completely finished.", Dispatched: true},
		{Name: "/compact", Description: "Compact the conversation now (summarize history to free up the context window).", Dispatched: true},
		{Name: "/schedule", Description: "Run an instruction on a recurring schedule or as a one-time timer.", Dispatched: true},
		{Name: "/browser", Description: "Invoke a browser agent for web tasks.", Dispatched: true},
		{Name: "/plan", Description: "Plan carefully before executing a task.", Dispatched: true},
		{Name: "/grill-me", Description: "Interview me to align on a plan.", Dispatched: true},
		{Name: "/teamwork-preview", Description: "Invoke a team of agents to autonomously tackle large projects.", Aliases: []string{"teamwork"}, Dispatched: true},
		{Name: "/swarm", Description: "(Preview & Quota Heavy) Run a swarm: several agents working in parallel on one task, each in its own conversation, collaborating through a shared message board.", Dispatched: true},
		{Name: "/learn", Description: "Reflect on recent successes or corrections to capture reusable skills or rules.", Dispatched: true},
		{Name: "/boost", Description: "Invoke the Boost multi-agent orchestrator for complex tasks.", Dispatched: true},
		{Name: "/agents-sdk", Description: "Build AI agents on Cloudflare Workers using the Agents SDK. Load when creating stateful agents, durable workflows, real-time WebSocket apps, scheduled tasks, or chat agents with built-in state."},
		{Name: "/agy-customizations", Description: "Comprehensive guide and reference for the Antigravity Customization System. Use to explain how customizations work, their loading priority, discoverability, and authoring conventions."},
		{Name: "/antigravity-guide", Description: "Provides a comprehensive guide, quick reference, and sitemap for Google Antigravity (AGY), including the Antigravity CLI (agy), Antigravity 2.0, browser surface, and configuration conventions."},
		{Name: "/chrome-devtools-plugin:a11y-debugging", Description: "Uses Chrome DevTools MCP for accessibility (a11y) debugging and auditing based on web.dev guidelines. Use when testing semantic HTML, ARIA labels, keyboard navigation, color contrast, and screen-reader landmarks."},
		{Name: "/chrome-devtools-plugin:chrome-devtools", Description: "Uses Chrome DevTools via MCP for efficient debugging, troubleshooting and browser automation. Use when debugging web pages, automating browser interactions, capturing screenshots, or inspecting network/CPU profiles."},
		{Name: "/chrome-devtools-plugin:cookie-debugging", Description: "Uses Chrome DevTools MCP for inspecting, debugging, and testing cookies, session state, authentication issues, and cookie consent compliance. Use when diagnosing 401/403 errors, authentication redirects, session expiration, Cookie/Set-Cookie header issues, cookie banner consent conformance, or third-party cookie/SameSite/Partitioned cookie warnings."},
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
		{Name: "/science:alphagenome-atlas-website-links", Description: "Constructs deep-links and URLs for the AlphaGenome Atlas website. Supports generating single-variant exploration links (1-based VCF-style coordinates), genomic locus views (1-based closed chr:start-end), candidate summary tables, and AlphaGenome reference vs. alternate predictions. Use when visualizing, exploring, charting, or linking genetic variants and genomic loci on the AlphaGenome Atlas, or when asked to inspect, view, or link predictions for a genomic variant. Don't use for computing raw AlphaGenome variant predictions directly without Atlas URLs (use alphagenome-single-variant-analysis or alphagenome-variant-impact-score)."},
		{Name: "/science:alphagenome-single-variant-analysis", Description: "Analyzes genetic variant effects on gene expression (RNA-seq), chromatin accessibility (DNASE), histone marks (ChIP), and transcription factors using AlphaGenome. Use when the user asks for variant-effect prediction at a single locus."},
		{Name: "/science:alphagenome-variant-impact-score", Description: "Score, annotate, and analyze the functional impact of genetic variants using AlphaGenome Variant Impact (AVI) scores. Query variants in chr:pos:ref>alt format, annotate VCF/tabular callsets, perform saturation mutagenesis window scans (1-based closed chr:start-end), and extract GENCODE v46 GTF gene/exon/junction coordinates all via the AlphaGenome Atlas API."},
		{Name: "/science:biomolecular-structure-viewer", Description: "Interactive 3D biomolecular structure viewer via Mol*. Renders 3D molecular structures from local workspace files (.cif, .mmcif, .pdb) or conversation artifacts, and manages user 3D annotations and highlighted residues. Use when the user asks to visualise a protein or nucleic acid, display a 3D structure, or asks about 3D biomolecular structure annotations and highlighted residues."},
		{Name: "/science:chembl-database", Description: "Query the ChEMBL database for bioactive molecules, drug targets, bioactivity data, approved drugs, and chemical structures. Use when the user asks about drug-target interactions, IC50/Ki values, or medicinal chemistry."},
		{Name: "/science:clinical-trials-database", Description: "Query ClinicalTrials.gov via APIv2. Use when you want to search for trials by condition, drug, location, status, or phase; retrieve trial details; or filter by eligibility criteria."},
		{Name: "/science:clinvar-database", Description: "Use when needing clinical significance, pathogenicity classifications (e.g., Pathogenic, Benign, VUS), clinical evidence rationales, or finding variants that have been reviewed by expert panels (ClinGen)."},
		{Name: "/science:credentials", Description: "Instructions for handling API keys and credentials safely, verifying their presence, and prompting the user to add them if missing using a safe protocol."},
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
		{Name: "/science:predictingthepast", Description: "Ancient text restoration, attribution, dating, contextualization, and embedding via Aeneas (Latin) / Ithaca (Ancient Greek). Use when asked to \"restore\", \"attribute\", \"date\", \"contextualize\", \"find parallels\", \"where was it written\", \"when was it written\", \"embed\", or \"analyze\" an ancient text, inscription, or epigraphic document, or when the user mentions \"Aeneas\", or \"Ithaca\"."},
		{Name: "/science:protein-sequence-msa", Description: "Perform multiple sequence alignment of proteins with EBI Clustal Omega. Use when you need to align multiple sequences to assess similarity, domain conservation, or phylogenetic relationships."},
		{Name: "/science:protein-sequence-similarity-search", Description: "Searches for homologous protein sequences using MMseqs2 (fast, default) or BLAST (comprehensive, fallback). Trigger this whenever the user provides a query protein and wants similar sequences, homologs, or remote homology."},
		{Name: "/science:pubchem-database", Description: "Query PubChem, search by name/CID/SMILES, retrieve properties, similarity/substructure searches, bioactivity, for cheminformatics. Use when a user asks about a small molecule's identifiers, structure, or bioactivity data."},
		{Name: "/science:pubmed-database", Description: "Search PubMed for scientific literature, including published clinical trials. Fetch abstracts and full text. Link published research to biological entities (genes, proteins, variants, drugs, diseases)."},
		{Name: "/science:pymol", Description: "Visualize, analyze, and render protein and molecular structures using PyMOL. Use when the user wants to create images of protein structures, perform structural alignment, measure distances/angles, or color by property."},
		{Name: "/science:quickgo-database", Description: "Query QuickGO and the Evidence & Conclusion Ontology (ECO) REST API. Use this when you need to map genes to biological processes, molecular functions, or cellular components; retrieve GO annotations; or traverse the ontology graph."},
		{Name: "/science:reactome-database", Description: "Query the Reactome database (Analysis and Content Services). Use when the user asks about pathway analysis, gene list enrichment, retrieving reaction/pathway diagrams, or mapping identifiers across biological systems."},
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
	for i := range rows {
		if kind, ok := panelregistry.CommandPanel(strings.TrimPrefix(rows[i].Name, "/")); ok {
			rows[i].Panel = kind
		}
		assignCommandScope(&rows[i])
	}
	return rows
}

func assignCommandScope(c *Command) {
	name := strings.TrimPrefix(c.Name, "/")
	switch name {
	case "help", "clear", "exit", "copy", "codesearch", "keybindings",
		"open", "statusline", "title", "diff", "artifact", "changelog",
		"btw", "resume", "rename", "rewind", "feedback", "logout":
		c.Scope = ScopeHost
	case "model":
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapModelSwitch
	case "effort":
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapReasoningTier
	case "context", "compact":
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapContextInspect
	case "tasks":
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapTasks
	case "permissions":
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapNativeTools
	case "plugin", "plugins", "skills", "mcp":
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapPlugins
	case "agents":
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapAgents
	case "accounts", "pool", "credits", "usage", "quota":
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapGoogleAccounts
	case "hooks":
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapHooks
	case "config", "goal", "schedule", "browser", "plan", "learn", "grill-me", "teamwork", "teamwork-preview", "boost", "fork", "voice", "remote-control", "swarm":
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapWorkflows
	case "provider", "providers", "feature-diff":
		c.Scope = ScopeEngine
		c.Engine = "pigasus"
	default:
		// Any plugin skill or workspace skill (e.g. /science:*, /terminal-browser, etc.)
		c.Scope = ScopeCapability
		c.RequiredCap = agentturn.CapPlugins
	}
}

// Find searches for a command in the catalogue by name or alias.
func Find(name string) (Command, bool) {
	clean := strings.TrimPrefix(name, "/")
	if canon, ok := Canonical(clean); ok {
		clean = canon
	}
	c, ok := rowsByName[clean]
	return c, ok
}

// rowsByName indexes every command \u2014 the advertised ones and the hidden
// ones \u2014 by its bare name.
var rowsByName = buildRowsByName()

func buildRowsByName() map[string]Command {
	m := make(map[string]Command, len(Default)+len(Hidden))
	for _, c := range Default {
		m[strings.TrimPrefix(c.Name, "/")] = c
	}
	for _, c := range Hidden {
		m[strings.TrimPrefix(c.Name, "/")] = c
	}
	return m
}

// canonicalNames holds the bare name of every command \u2014 the advertised
// ones and the hidden ones.
var canonicalNames = buildCanonicalNames()

func buildCanonicalNames() map[string]bool {
	m := make(map[string]bool, len(Default)+len(Hidden))
	for _, c := range Default {
		m[strings.TrimPrefix(c.Name, "/")] = true
	}
	for _, c := range Hidden {
		m[strings.TrimPrefix(c.Name, "/")] = true
	}
	return m
}

// aliasTargets maps every alias to the bare name of the command it
// names.
//
// An alias never shadows a command of its own: "branch" is an alias of
// "/fork" in the catalogue, but the host answers "/branch" with its own
// handler, so the word resolves to itself.
var aliasTargets = buildAliasTargets()

func buildAliasTargets() map[string]string {
	m := map[string]string{}
	for _, c := range Default {
		target := strings.TrimPrefix(c.Name, "/")
		for _, a := range c.Aliases {
			if canonicalNames[a] {
				continue
			}
			m[a] = target
		}
	}
	return m
}

// Canonical resolves a typed command name to the name the host's
// dispatch table and the panel table are keyed by.
//
// The rule is one: a command's own name resolves to itself, an alias
// resolves to the command it names, and anything else is not a command
// at all \u2014 the second value is false and the caller keeps the name it
// had.
//
// The argument may carry the leading slash ("/clear") or not ("clear");
// the answer never does.
func Canonical(name string) (string, bool) {
	n := strings.TrimPrefix(name, "/")
	if n == "" {
		return "", false
	}
	if canonicalNames[n] {
		return n, true
	}
	if target, ok := aliasTargets[n]; ok {
		return target, true
	}
	return "", false
}

// PanelFor returns the panel the named command opens, resolving an
// alias first.
//
// The second value is false for every command that opens no panel.
func PanelFor(name string) (panelregistry.Kind, bool) {
	canonical, ok := Canonical(name)
	if !ok {
		return panelregistry.None, false
	}
	row, ok := rowsByName[canonical]
	if !ok || row.Panel == panelregistry.None {
		return panelregistry.None, false
	}
	return row.Panel, true
}

// DescriptionOf returns the row's description for the named command,
// resolving an alias first.
//
// The name may carry the leading slash ("/exit") or not ("exit"). The
// second value is false for a command the catalogue does not hold, or for
// a row that carries no description of its own (the hidden commands).
//
// The description is the reference's own row text — the picker's second
// column and the /help COMMANDS tab's description — so a surface that
// needs the command's name reads it here instead of carrying a second
// copy of the string.
func DescriptionOf(name string) (string, bool) {
	canonical, ok := Canonical(name)
	if !ok {
		return "", false
	}
	row, ok := rowsByName[canonical]
	if !ok || row.Description == "" {
		return "", false
	}
	return row.Description, true
}

// Known reports whether token names a command the picker offers \u2014
// exactly, or as one of that command's aliases ("/quit" is an alias of
// "/exit").
func Known(token string) bool {
	for _, c := range Default {
		if c.Name == token {
			return true
		}
		for _, alias := range c.Aliases {
			if alias == token || "/"+alias == token {
				return true
			}
		}
	}
	return false
}

// Sorted returns Default sorted by Name in plain byte-ascending order.
// The reference's `/help` COMMANDS tab is a flat alphabetical sort of
// the whole catalog, even though the "/" picker keeps registration
// order (built-ins in registration order, then skills). Verified
// against REF v1.4.0: `/agents-sdk` and `/agy-customizations` sit
// between `/agents` and `/artifact` in /help, while the picker's
// second and third rows are `/agents` and `/artifact` \u2014 see
// /tmp/agycmp/ref_catalog.txt (help, sorted) vs
// /tmp/agycmp/hotpath/picker/ref.000-slash.txt (picker, unsorted).
//
// The sort key is the Name as stored (the "/clear" of the display
// form "/clear (new)"), which reproduces REF's ordering exactly.
func Sorted() []Command {
	out := make([]Command, len(Default))
	copy(out, Default)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Filter returns the subset of Default whose Name contains prefix as a
// case-insensitive substring anywhere in the name (not just at the
// start). The reference v1.3.6 picker uses substring matching: typing
// `/s` matches `/codesearch` (contains "s"), `/config (settings)`,
// `/resume (switch)`, and any other command name with an "s" anywhere.
//
// Cycle 351: the chrome-devtools-plugin:* filter is GONE. The
// reference's picker at 1.4.8 shows every command the catalogue holds,
// plugin skills included: the picker at an empty prefix reads "\u2193 100
// more" under the five visible rows (5 + 100 = 105 items), and a
// Down-walk of the whole list yields the same 105 names the /help
// COMMANDS tab counts ("[1-33 of 105 items]"). The chrome-devtools-plugin
// rows are six of them.
//
// The 1.3.6 evidence the filter was built on (cycle 135 / cycle 146)
// said the picker held 94 entries while /help held 99; at 1.4.8 both
// surfaces hold the same set, so the split no longer exists.
//
// Evidence 【一手】: agy 1.4.8, "/" at an empty prefix plus a 130-step
// Down-walk of the picker.
func Filter(prefix string) []Command {
	return FilterScoped(prefix, ScopeContext{
		EngineName:   "pigasus",
		Capabilities: agentturn.CapAll,
	})
}

// containsFold is strings.Contains with Unicode case-folding. The
// picker's substring filter is case-insensitive so `/SKILL` matches
// `/skills`. Cycle 235: walk runes (one rune per step) and fold with
// unicode.ToLower so multi-byte graphemes (CJK: 科, 学; Hiragana:
// ひらがな; Cyrillic: п) and the Turkish dotted İ (U+0130, which
// simple-folds to U+0069 'i') compare as single tokens instead of
// being dropped by the prior ASCII byte-by-byte scan. Strings that
// are not valid UTF-8 fall back to bytes.Equal so non-UTF8 binary
// names \u2014 none in the current catalog, but the picker accepts
// arbitrary plugin names \u2014 still get a deterministic answer.
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

// FilterWithCustom is the picker integration point for the cycle
// custom-slash-commands-no-plugin-injection contract. The picker calls
// it to merge the hardcoded catalogue with customcmd-loaded plugin /
// project / global commands. The merge follows the AGY 1.2.16
// precedence (built-in > plugin > project > global) so plugin-defined
// commands are never silently overridden by a same-named project
// command.
//
// The custom slice is the output of customcmd.Load; the picker passes
// whatever the loader returned at startup. Passing nil falls back to
// Filter with no extra rows.
//
// Cycle 351: the chrome-devtools-plugin:* filter is gone here too \u2014 the
// 1.4.8 picker shows plugin rows (see Filter).
func FilterWithCustom(prefix string, custom []customcmd.Command) []Command {
	if len(custom) == 0 {
		return Filter(prefix)
	}
	builtins := make([]customcmd.BuiltIn, len(Default))
	for i, c := range Default {
		builtins[i] = customcmd.BuiltIn{Name: c.Name, Description: c.Description}
	}
	merged, _ := customcmd.MergeWithBuiltins(builtins, custom)
	out := make([]Command, 0, len(merged))
	for _, c := range merged {
		name := "/" + c.Name
		if prefix == "" || containsFold(name, prefix) {
			out = append(out, Command{Name: name, Description: c.Description})
		}
	}
	return out
}

// FilterScoped returns the commands visible under the given ScopeContext matching the prefix.
func FilterScoped(prefix string, ctx ScopeContext) []Command {
	engineName := ctx.EngineName
	if engineName == "" {
		engineName = "pigasus"
	}
	caps := ctx.Capabilities
	if engineName == "pigasus" && caps == 0 {
		caps = agentturn.CapAll
	}

	var visible []Command
	for _, c := range Default {
		if IsAvailable(c, ctx) {
			visible = append(visible, c)
		}
	}

	if len(ctx.Dynamic) == 0 {
		out := make([]Command, 0, len(visible))
		for _, c := range visible {
			if prefix == "" || containsFold(c.Name, prefix) {
				out = append(out, c)
			}
		}
		return out
	}

	visibleNames := make(map[string]bool, len(visible))
	for _, c := range visible {
		visibleNames[strings.TrimPrefix(c.Name, "/")] = true
	}
	out := make([]Command, 0, len(visible)+len(ctx.Dynamic))
	for _, c := range visible {
		if prefix == "" || containsFold(c.Name, prefix) {
			out = append(out, c)
		}
	}
	for _, dyn := range ctx.Dynamic {
		cleanName := strings.TrimPrefix(dyn.Name, "/")
		name := "/" + cleanName
		if visibleNames[cleanName] {
			name = "/agent " + cleanName
		}
		row := dyn
		row.Name = name
		row.Scope = ScopeDynamic
		if row.Source == SourceBuiltin {
			row.Source = SourceAgent
		}
		if row.Badge == "" {
			row.Badge = "agent"
		}
		if prefix == "" || containsFold(row.Name, prefix) || containsFold(cleanName, prefix) {
			out = append(out, row)
		}
	}
	return out
}

// FilterWithDynamic merges the built-in catalogue with dynamic commands
// (e.g. from an active agent backend) and filters by prefix.
// Defaults to the native pigasus engine with all capabilities.
func FilterWithDynamic(prefix string, dynamic []Command) []Command {
	return FilterScoped(prefix, ScopeContext{
		EngineName:   "pigasus",
		Capabilities: agentturn.CapAll,
		Dynamic:      dynamic,
	})
}
