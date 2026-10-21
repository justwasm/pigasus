// conversation.go — which conversation a run continues.
//
// Before this file, `--conversation` was registered but ignored and `-c` only
// dropped into the TUI with an empty transcript, so
// `antigravity --conversation=<id>` could not load anything. The reference
// keeps every conversation in ~/.gemini/antigravity-cli, and
// internal/convstore now reads that store, so the two binaries can share it.
//
// Recovered behaviour (1.3.6, verified against the live binary):
//
//	--conversation=<id>   adopt <id> iff conversations/<id>.db exists as a
//	                      regular file (a zero-byte file counts, a directory
//	                      does not); otherwise print
//	                        warning: conversation "<id>" not found
//	                      on stderr and start a fresh conversation. An empty
//	                      value is silent, the lookup is case-sensitive, and
//	                      the last occurrence wins.
//	-c / --continue       pick the most recent conversation for the current
//	                      directory. The pick is workspace-scoped, and a
//	                      directory that has no conversation of its own
//	                      inherits its parent's (verified: running in
//	                      /tmp/agyconvprobe continued /tmp's conversation).
//
// The workspace lookup reads the two caches the reference maintains next to the
// conversation databases:
//
//	cache/last_conversations.json    {"<absolute dir>": "<conversation id>"}
//	cache/conversation_metadata.json {"conversations": {"<id>": {"summary":
//	                                  {"WorkspaceURIs": ["file:///…"],
//	                                   "UpdatedAt": …}, "last_modified_time": …}}}
//
// The reference rewrites last_conversations.json from conversation_metadata.json
// on startup, so the metadata cache is treated as authoritative here and
// last_conversations.json as a fast path.
//
// The clone does not write either cache yet: it reads the reference's store but
// leaves it untouched. That is the honest limitation of this cycle — a run
// started by the clone is not offered to `-c` afterwards. See
// docs/cycles/cycle-120/deliverable-A-conversation.md.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/canvas"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/mdrender"
)

// conversationHistoryTurns caps how many trailing user/assistant messages a
// resumed conversation replays into the new model call, and
// conversationHistoryChars bounds their combined size. The reference feeds the
// whole conversation; the clone bounds it because the agw transport has no
// server-side conversation state, so every replayed turn is re-sent in full and
// real conversations hold model replies of tens of kilobytes. Both bounds are
// 【推测】 — they trade context fidelity for a bounded request.
const (
	conversationHistoryTurns = 40
	conversationHistoryChars = 100_000
)

// userPromptRow renders the user's own message in the transcript. Recovered
// from the reference's conversation view: the prompt is echoed in bold blue
// (SGR 1 + 38;2;25;103;210) — same blue the warning bullet uses —
// behind a "> " marker. Cycle 126 swapped the 256-colour 38;5;26 the
// cycle-92 capture observed for the 24-bit RGB the v1.3.6 binary
// actually emits; the visible shade is identical in tmux.
func userPromptRow(text string) string {
	return cWarningBlueOpen + "> " + text + cWarningBlueClose
}

// renderLineHighlighted renders a single transcript line, optionally
// painting `highlights` byte ranges through vito/midterm's Canvas so
// the styled slice goes through the same renderer AGY 1.4.2 uses for
// partial repaints and search-match highlighting (175 vito/midterm
// symbols in the reference pclntab).
//
// nil / empty highlights ⇒ the line passes through the Canvas's
// empty-format path, which appends only a trailing SGR reset (the
// existing transcript renderer also ends with cReset on every line,
// so the visual output is identical for the no-highlight case).
//
// Cycle vito-canvas: this is the foundation for the upcoming
// chat-search cycle that will thread match ranges through the / Ctrl+F
// search pane. No existing call site is updated; future cycles will
// switch the per-line renderers in this file (userPromptRow, the
// indentModelReply step inside renderMarkdown, etc.) onto this
// wrapper once the search-bar model is in place.
func renderLineHighlighted(content string, highlights []canvas.Range) string {
	if len(highlights) == 0 {
		// No highlight plumbing needed: defer to the same Canvas
		// path so behaviour is byte-identical to the future
		// call sites (which will pass an empty slice on a
		// non-search query). Keeps the single-renderer invariant
		// that "every line goes through renderLineHighlighted".
		return canvas.Line{Content: content}.Render()
	}
	return canvas.Line{Content: content, Highlights: highlights}.Render()
}

// conversationSelection is the outcome of resolving --conversation / -c.
type conversationSelection struct {
	// ID is the conversation the run continues, or "" for a fresh one.
	ID string
	// Source records how it was chosen: "flag", "continue", or "".
	Source string
	// Store is the loaded conversation, or nil when nothing was loaded.
	Store *convstore.Conversation
	// History is the model context to replay before the new prompt.
	History []convstore.ChatTurn
	// PriorUserTurns counts the user turns already in the conversation. The
	// reference's envelope counts them: `-c` on a one-turn conversation
	// reported num_turns 2 while a fresh run reports 1.
	PriorUserTurns int
}

// resolveConversationSelection decides what a run continues, printing the
// reference's warning on warn when an explicitly named conversation is missing.
func resolveConversationSelection(fs *flag.FlagSet, warn io.Writer) conversationSelection {
	if id := strings.TrimSpace(flagStringValue(fs, "conversation")); id != "" {
		if path := convstore.FindConversation(id); path != "" {
			return loadConversationSelection(id, "flag", path)
		}
		fmt.Fprintf(warn, "warning: conversation %q not found\n", id)
		return conversationSelection{}
	}
	if resolveBoolAlias(fs, "continue", "c") {
		cwd := workspaceDir()
		// Cycle resume-continuation: the v1.4.0 changelog adds
		// parent/child walks to --continue. The cycle-120
		// cache-only lookup covers the in-workspace case the ref
		// already wrote; the parent/child walk covers the
		// "subdirectory" and "another session is open" cases the
		// changelog fixed.
		if id := mostRecentConversation(cwd); id != "" {
			return loadConversationSelection(id, "continue", convstore.ConversationPath(id))
		}
		if id := convstore.MostRecentInWorkspace(cwd); id != "" {
			return loadConversationSelection(id, "continue", convstore.ConversationPath(id))
		}
		cand, ferr := convstore.FindMostRecent(cwd, continueParentMaxDepth)
		if ferr == nil && cand != nil {
			if id := convstore.MostRecentInWorkspace(cand.Dir); id != "" {
				return loadConversationSelection(id, "continue", convstore.ConversationPath(id))
			}
		}
		// Nothing to continue. The reference's output for this case is
		// unrecovered (its quota is exhausted, so the live binary
		// cannot be probed); the v1.3.5 binary printed nothing. v1.4.0
		// falls back to opening the /resume picker; the clone
		// runLiveTUI keeps that path so the operator still has a
		// recovery outlet.
		return conversationSelection{}
	}
	return conversationSelection{}
}

func loadConversationSelection(id, source, path string) conversationSelection {
	sel := conversationSelection{ID: id, Source: source}
	c, err := convstore.LoadConversation(path)
	if err != nil {
		// The reference only checks that the file exists; a database it
		// cannot read is still adopted, just without a transcript.
		return sel
	}
	sel.Store = c
	sel.History = c.LastChatTurnsBudget(conversationHistoryTurns, conversationHistoryChars)
	for _, t := range c.ChatTurns() {
		if t.Role == "user" {
			sel.PriorUserTurns++
		}
	}
	return sel
}

func workspaceDir() string {
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return ""
}

// --- the reference's caches -------------------------------------------------

func loadLastConversations() map[string]string {
	b, err := os.ReadFile(filepath.Join(convstore.StoreDir(), "cache", "last_conversations.json"))
	if err != nil {
		return nil
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

type conversationMetadata struct {
	Conversations map[string]struct {
		Summary struct {
			ID            string   `json:"ID"`
			Title         string   `json:"Title"`
			Preview       string   `json:"Preview"`
			NumSteps      int      `json:"NumSteps"`
			UpdatedAt     string   `json:"UpdatedAt"`
			WorkspaceURIs []string `json:"WorkspaceURIs"`
		} `json:"summary"`
		LastModifiedTime string `json:"last_modified_time"`
	} `json:"conversations"`
}

func loadConversationMetadata() *conversationMetadata {
	b, err := os.ReadFile(filepath.Join(convstore.StoreDir(), "cache", "conversation_metadata.json"))
	if err != nil {
		return nil
	}
	var m conversationMetadata
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return &m
}

// mostRecentConversation picks the conversation `-c` would continue from dir.
//
// Order: the recorded last conversation for dir or its nearest ancestor, then
// the most recently modified conversation whose workspace is dir or one of its
// ancestors. The ancestor rule is what makes a fresh subdirectory continue the
// conversation that owns its workspace.
func mostRecentConversation(dir string) string {
	if dir == "" {
		return ""
	}
	if id := lookupLastConversation(dir); id != "" {
		return id
	}
	return lookupMetadataConversation(dir)
}

func lookupLastConversation(dir string) string {
	last := loadLastConversations()
	if len(last) == 0 {
		return ""
	}
	for d := filepath.Clean(dir); ; {
		if id := last[d]; id != "" && convstore.HasConversation(id) {
			return id
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

func lookupMetadataConversation(dir string) string {
	meta := loadConversationMetadata()
	if meta == nil {
		return ""
	}
	for d := filepath.Clean(dir); ; {
		uri := "file://" + d
		bestID, bestTime := "", ""
		for id, entry := range meta.Conversations {
			if !hasString(entry.Summary.WorkspaceURIs, uri) || !convstore.HasConversation(id) {
				continue
			}
			if entry.LastModifiedTime > bestTime {
				bestID, bestTime = id, entry.LastModifiedTime
			}
		}
		if bestID != "" {
			return bestID
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

func hasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// --- rendering a loaded conversation ----------------------------------------

// seedConversation renders a loaded conversation into the transcript so
// `--conversation=<id>` and `-c` open on the previous turns instead of an empty
// screen.
//
// The rows reuse the live rendering path: a prompt is echoed exactly as a typed
// prompt is, a reply is appended as-is, and a run of tool calls coalesces into
// the same step block ctrl+o expands. Whether the reference renders a resumed
// transcript identically is unrecovered — it takes a live model turn and the
// account's quota is exhausted — so the layout is 【推测】, assembled from the
// recovered per-step renderers.
//
// Tool *results* are not restored: the reference keeps them in a step shape the
// clone does not decode yet, so an expanded loaded step shows its title only.
func (m *runMode) seedConversation(c *convstore.Conversation) {
	if c == nil {
		return
	}
	steps := c.Transcript()
	for i := 0; i < len(steps); {
		if steps[i].Role() == "tool" {
			j := i
			var group []toolStep
			for j < len(steps) && steps[j].Role() == "tool" {
				group = append(group, loadedToolStep(steps[j]))
				j++
			}
			block := toolBlockLines(group, m.expanded, m.width)
			start := len(m.response)
			m.response = append(m.response, block...)
			m.blocks = append(m.blocks, turnBlock{start: start, lines: len(block), steps: group})
			i = j
			continue
		}
		s := steps[i]
		switch s.Role() {
		case "user":
			// Cycle 271: the AGY 1.4.2 restored transcript shows only
			// model reply steps — the user's prompts are absent (verified
			// on two real conversations and a synthetic fixture). Seed the
			// message into the live session so the model still sees the
			// prior turn, but do not echo a "> " row into m.response.
			m.session = append(m.session, backend.Message{Role: "user", Content: s.Text})
			// Nothing was rendered, so do not append the separator
			// blank either — it would push an empty row above the
			// first reply (the banner already ends with one).
			i++
			continue
		case "model":
			// AGY 1.2.13: defensive guard for old DBs. Transcript()
			// already drops empty-text model steps, but if a row ever
			// does reach this branch with whitespace-only Text
			// (e.g. a step that survived an earlier buggy save), we
			// still skip rendering a blank row and still skip
			// re-seeding an empty assistant message into the live
			// session. The new writes are blocked at sessionToSteps,
			// so this is belt-and-suspenders.
			if strings.TrimSpace(s.Text) == "" {
				i++
				continue
			}
			// Cycle 128: model replies get inline-backtick styling
			// (``code`` ⇒ dim background + bright text). The session
			// history still stores the raw text — only the rendered
			// transcript gets the colour escapes.
			//
			// Cycle 155: renderMarkdown layers bullet conversion
			// (``- `` → ``• ``), bold styling (``**text**`` ⇒ ESC[1m),
			// and 4-space indent inside fenced code blocks on top of
			// the backtick pass. Cycle 154's 2-space indent runs last
			// so the model-reply block still reads as a quote.
			m.response = append(m.response, renderMarkdown(s.Text, m.width))
			m.session = append(m.session, backend.Message{Role: "assistant", Content: s.Text})
		default:
			// Errors and system notices. The reference's rendering of a
			// resumed one is unrecovered; the clone indents it like a
			// slash-command notice.
			m.response = append(m.response, "  "+firstLine(s.Text))
		}
		m.response = append(m.response, "")
		i++
	}
}

// loadedToolStep turns a stored tool call into the transcript's step shape.
// Cycle 148: the convstore records `Step.Status` for every tool call:
//
//	status=3 → success
//	status=4 → failed (errored at the tool runner level)
//	status=5 → completed-skipped or alternate success (not an error)
//	status=6/7 → rare (probably permission-denied / cancelled)
//
// Previously the clone ignored status and never marked a loaded
// step with `(Failed)`. The v1.3.6 reference renders a "(Failed)"
// suffix on every tool call that errored at runtime; cycle 148
// restores that by mapping status=4 (and 6/7 as a defensive
// fallback) to `err: true` so the renderer picks up the suffix.
func loadedToolStep(s convstore.Step) toolStep {
	st := newToolStep(s.ToolName, json.RawMessage(s.ToolArgs), s.ToolResult, loadedStepErr(s.Status))
	return st
}

// loadedStepErr maps a convstore Step.Status to the renderer-side
// `err` flag. Returns true when the stored status indicates the
// tool call errored at runtime (so the transcript shows
// "(Failed)" — the v1.3.6 reference's byte-exact suffix for
// err'd tool calls).
func loadedStepErr(status int64) bool {
	switch status {
	case 4, 6, 7:
		return true
	default:
		return false
	}
}

// styleInlineBackticks returns line with every matched pair of
// single backticks wrapping their contents in the cInlineCodeOpen /
// cInlineCodeClose colour escapes, so that ``like this`` renders as
// a chip with a slightly darker background instead of being
// indistinguishable from the rest of the paragraph.
//
// Cycle 128: this is the *inline* path, not the fenced code-block
// path — the conversation surface is line-at-a-time, so multi-line
// code fences (````...`) are still rendered as plain text. The
// helper is deliberately tolerant of unbalanced backticks: if a
// line has an odd count (e.g. a tool result containing a stray
// `), every unmatched backtick passes through unchanged. This
// keeps tool output (often rich with single backticks used as
// shell quoting) from being corrupted.
//
// The styling budget is bounded by counting runs first; if a line
// has more than 8 paired backtick runs (a plausible cap on a model
// reply's ``code``-span count), the line is passed through
// unchanged so the byte budget cannot blow up.

// indentModelReply prefixes every non-empty line of a styled model
// reply with indent. Cycle 154: the v1.3.6 reference indents the
// entire rendered model-reply block by two spaces — the prose reads
// as a quote/blockquote that visually separates it from the tool-call
// bullets above and below. The indent is applied to the *styled*
// string (post styleInlineBackticks) so the colour escapes ride in
// front of the spaces, keeping the rendered cell width correct.
//
// An empty line in the input stays empty — only non-empty lines are
// indented, so the visual rhythm of paragraphs separated by blanks
// is preserved.
func indentModelReply(styled, indent string) string {
	if styled == "" {
		return styled
	}
	lines := strings.Split(styled, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// renderMarkdown applies the v1.3.6 reference's markdown render to a
// resumed model reply.
//
// Cycle 175: the cycle-155 hand-rolled pipeline (bullet conversion,
// bold-spans, code-block indent) is replaced with internal/mdrender
// (added in cycle 168, refined in 174 / 175). The library now
// handles:
//
//   - bullet conversion: `- ` → `• ` for unordered list items
//   - bold spans: `**text**` → ESC[1m text ESC[22m
//   - italic spans: `*text*` → ESC[3m text ESC[23m (cycle 175 bonus
//     over cycle 155, which did not recover italic styling)
//   - code-block indent: lines inside ``` ``` get a 4-space indent,
//     the fence markers themselves are stripped from the output
//   - links: link labels get italic styling
//
// Two passes remain in renderMarkdown itself:
//
//   - styleInlineBackticks — wraps “ `text` “ runs in the
//     cInlineCodeOpen / cInlineCodeClose pair (a TTY-only colour
//     hint, not a Markdown concept). mdrender doesn't apply this
//     because it would be incorrect for plain-text consumers like
//     internal/cli/login_prompt.go's call to Render.
//   - blank-line collapse — collapses "text\n\n\ntext" to
//     "text\n\ntext" so the cycle-153 wrapRows consumer sees a
//     single blank between paragraphs (matches reference behaviour
//     at 200×60). mdrender preserves the source's blank-line count
//     verbatim, which the reference does not.
//
// Cycle 155 history: closes the "model message shows raw markdown"
// gap at 200×60 (and 100×30) — the reference renders bullets and
// bold, the pre-155 clone passed them through unchanged.
func renderMarkdown(text string, width int) string {
	if text == "" {
		return text
	}
	// Cycle glamour-migration: route through charm.land/glamour/v2
	// v2.0.1 (the version AGY 1.4.2 ships). The four-step pipeline
	// is:
	//
	//  1. Pre-process: rewrite markdown link destinations through
	//     PreprocessFileLinks so `#L<n>` anchors become
	//     `%23L<n>` before glamour parses the source (glamour
	//     has no AST visitor we can hook).
	//  2. Strip fenced mermaid / math blocks into placeholders so
	//     the legacy MermaidRenderBlock / expandLaTeX can render
	//     them with the cycle's existing frontmatter-aware logic,
	//     and glamour does not mangle the body.
	//  3. Render the placeholder-laden source through glamour.
	//  4. Restore the placeholders into glamour's output.
	//
	// The glamour call falls back to the legacy mdrender.RenderANSI
	// path on any parser error (cycle 1.4.2 "fallback to raw
	// text with warning banner" spirit); see glamour_adapter.go
	// ANSI for the contract.
	pre := mdrender.PreprocessFileLinks(text)
	stripped, placeholders := mdrender.StripMermaidAndMath(pre)
	// Cycle 268: thread the caller's terminal width through to
	// glamour. Before this, the chat transcript and step-detail
	// panel capped word-wrap at glamour's hard default of 80
	// columns regardless of how wide the tmux pane was —
	// verified by a 250-col PTY observation. The TUI's
	// renderMarkdown callers (cmd/wagyu/tui_run.go:3498 and the
	// conversation seeding path at conversation.go:365) pass
	// m.width; tests pass 0 which falls through to
	// mdrender.CurrentWidth() and resolves via $COLUMNS / x/term.
	convWidth := width
	if convWidth <= 0 {
		convWidth = mdrender.CurrentWidth()
	}
	// Cycle 271: the AGY 1.4.2 reply block spans (terminal width − 3)
	// cells. buildANSIConfig sets Document.Margin = 2, which makes
	// glamour's output (renderWidth − 2) cells wide, so render one
	// column narrower than the caller's terminal width. The old
	// external indentModelReply("  ") that used to provide the two
	// leading cells is gone — the document margin now owns them.
	if convWidth > 0 {
		convWidth--
	}
	rendered, err := mdrender.ANSI(stripped, convWidth)
	if err != nil || rendered == "" {
		// Last-resort fallback path: cycle 175 walker, used by
		// every byte-pinned test in this file's test suite. New
		// glamour behaviour is captured separately in
		// TestGlamour_RenderMarkdown_ChatTranscript.
		var buf bytes.Buffer
		if wErr := mdrender.RenderANSI(text, &buf); wErr != nil {
			return text
		}
		rendered = buf.String()
	} else {
		rendered = mdrender.RestoreMermaidAndMath(rendered, placeholders)
	}
	out := strings.TrimRight(rendered, "\n")

	// Cycle 158 post-pass: styleInlineBackticks runs per-line so
	// the bold / italic escapes from mdrender don't have to know
	// about the cInlineCodeOpen / cInlineCodeClose sequences. The
	// library's plain-text Render() emits `` `text` `` verbatim,
	// but the TUI transcript needs the dim-background chip.
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		line = styleInlineBackticks(line)
		// Cycle 271: AGY 1.4.2 preserves consecutive blank rows.
		// glamour emits two of them between a bullet list and an
		// ordered list that follows a single source blank (the
		// list's block suffix plus the preserved blank), and the
		// reference capture shows both. Cycle 160's collapse was
		// written against the legacy walker and ref 1.3.5; with
		// glamour it removes a row the reference keeps. Blank
		// rows are still normalised to a truly empty string so
		// the TUI's row renderer does not paint a 117-cell
		// colour band.
		if lineIsBlankANSI(line) {
			line = ""
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// lineIsBlankANSI reports whether line carries no visible characters
// once SGR escape sequences are skipped. glamour's document margin and
// per-run colouring mean a visually blank row is often
// "  \x1b[38;5;23m            \x1b[m" rather than "".
func lineIsBlankANSI(line string) bool {
	for i := 0; i < len(line); {
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && line[j] != 'm' {
				j++
			}
			if j < len(line) {
				i = j + 1
				continue
			}
		}
		switch line[i] {
		case ' ', '\t', '\r':
			i++
			continue
		default:
			return false
		}
	}
	return true
}

// styleInlineBackticks wraps every matched “ ` “ run in
// cInlineCodeOpen / cInlineCodeClose. Mirrors the cycle-155
// styleBoldSpans tolerance rules: an odd count of backticks passes
// through unchanged, and the run count is capped at 8 to bound the
// byte budget. Cycle 175: the bold half of the cycle-155 hand-rolled
// pipeline moved to internal/mdrender; only the inline-backtick ANSI
// pass stays in cmd/wagyu because it is a TTY colour hint, not a
// Markdown concept.
func styleInlineBackticks(line string) string {
	if !strings.ContainsRune(line, '`') {
		return line
	}
	// Count paired runs up-front. Any odd count or > 8 paired runs
	// falls through so tool output is untouched.
	const maxPairs = 8
	open := false
	pairs := 0
	for i := 0; i < len(line); i++ {
		if line[i] == '`' {
			if open {
				pairs++
				open = false
			} else {
				open = true
			}
		}
	}
	if open || pairs == 0 || pairs > maxPairs {
		return line
	}
	var sb strings.Builder
	sb.Grow(len(line) + pairs*len(cInlineCodeOpen))
	open = false
	for i := 0; i < len(line); i++ {
		if line[i] == '`' {
			if open {
				sb.WriteString(cInlineCodeClose)
				open = false
			} else {
				sb.WriteString(cInlineCodeOpen)
				open = true
			}
			continue
		}
		sb.WriteByte(line[i])
	}
	return sb.String()
}

// resumeConversation is the cycle-167 /resume picker callback. It
// loads the picked database via convstore.LoadConversation (the same
// path `--conversation=<id>` and `-c` already exercise at startup),
// renders the transcript into m.response / m.session via the
// existing seedConversation helper, and updates the conversation id
// so subsequent steps persist against the resumed db.
//
// Returns nil — like the rest of the resume flow, the render is
// synchronous and needs no follow-up Cmd.
func (m *runMode) resumeConversation(meta convstore.ConversationMeta) tea.Cmd {
	c, err := convstore.LoadConversation(meta.Path)
	if err != nil {
		m.response = append(m.response,
			"  Failed to load "+meta.ID+": "+err.Error())
		return nil
	}
	// Wipe the live transcript before seeding so the resumed turns
	// replace any pre-existing scratch content. The reference's
	// picker behaves the same — picking a conversation replaces
	// whatever was on screen.
	m.response = nil
	m.session = nil
	m.blocks = nil
	m.liveSteps = nil
	m.activeTool = ""
	m.seedConversation(c)
	m.conversationID = meta.ID
	if c != nil && c.ID != "" {
		// Prefer the id inside the database when it differs from the
		// filename — convstore IDs are stable; filenames can be
		// renamed/moved.
		m.conversationID = c.ID
	}
	// Cycle 278: clear the cached auto-title, exactly as /continue does
	// (slash_commands.go). Without this, resuming B after working in A and
	// sending one prompt overwrites B's stored title with A's derived one
	// (saveSession writes m.autoTitle back into the summaries index).
	m.autoTitle = ""
	// Cycle 278: no transcript echo. Verified against agy 1.4.2 at 200x40 —
	// picking a row replaces the transcript and prints nothing else; the
	// old "⎿ Resumed conversation <id>" line was a cycle-167 invention
	// (the string does not occur in the binary).
	return nil
}
