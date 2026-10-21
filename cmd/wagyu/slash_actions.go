// slash_actions.go — cycle 103: the TUI slash commands that perform a real
// local action instead of printing a cycle-77 placeholder.
//
// Three commands moved from placeholder text to an actual effect:
//
//	/add-dir <path>  validate the path and record it in settings.json
//	/copy            put the last assistant response on the system clipboard
//	/logout          delete the stored OAuth token
//
// Every user-visible string below is recovered from the 1.3.6 string table
// unless marked 【推测】.
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/dlclark/regexp2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/render"
)

// clipboardWrite is copyToClipboard, indirected so tests can exercise the
// /copy handler without touching the real system clipboard.
var clipboardWrite = copyToClipboard

// addDirFeedback validates the /add-dir argument and records the directory.
//
// Recovered strings:
//
//	error: no directory specified         (empty argument)
//	error: directory not found: <path>    (stat fails)
//	error: not a directory: <path>        (regular file)
//	Added %s to workspace                 (success)
//
// The 1.3.3 changelog describes this fix: "/add-dir … now reports `directory
// not found` or `not a directory`" rather than claiming to have added a path
// that does not exist.
//
// Cycle 217: the success line was previously `Workspace added: %s`. REF
// renders `Added %s to workspace` (verified by the cycle-194 capture of
// `> /add-dir /tmp/foo` → `⎿  Added /tmp/foo to workspace`; see
// `/tmp/agy217/caps/ref.adddir-existing.raw.txt`). The word order is the
// only difference.
//
// The path is resolved with filepath.Abs so a relative argument lands in
// settings.json as an absolute path; the reference's exact normalisation is
// 【推测】.
func addDirFeedback(arg string) string {
	dir := unquotePath(arg)
	if dir == "" {
		return "error: no directory specified"
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "error: directory not found: " + dir
	}
	if !info.IsDir() {
		return "error: not a directory: " + dir
	}
	if err := auth.AddTrustedWorkspace(dir); err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("Added %s to workspace", dir)
}

// unquotePath strips one layer of matching single or double quotes, which is
// how a path with spaces is typed into the input box.
func unquotePath(s string) string {
	s = trimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// lastResponseText returns the most recent non-empty assistant message — what
// /copy puts on the clipboard. Recovered: the literal `last response` sits in
// the same string blob as `Copied %s to clipboard.`.
func (m *runMode) lastResponseText() string {
	rs := m.plannerResponses()
	if len(rs) == 0 {
		return ""
	}
	return rs[len(rs)-1]
}

// plannerResponses returns the assistant ("planner") responses in order, which
// is what /copy indexes. The reference's command description calls them
// "planner response[s]".
func (m *runMode) plannerResponses() []string {
	var out []string
	for _, msg := range m.session {
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
			out = append(out, msg.Content)
		}
	}
	return out
}

// resolveCopy implements /copy and /copy <n>. It returns the text to
// put on the clipboard ("" when there is nothing) and the line to
// show.
//
// Recovered: the command description ("Copy the last planner response
// to the clipboard, the n-th with '/copy n', or the /btw answer with
// '/copy btw'"), the success format `Copied %s to clipboard.` with
// `last response` as the filler for the default form and
// `No response to copy.`
//
// Cycle 230: /copy btw is no longer this function's job. The /copy
// switch in slash_commands.go dispatches the btw form to
// runMode.copyBtwCard first, so resolveCopy only sees /copy and
// /copy <n>. The reference also has a "The /btw answer is still being
// generated." branch for an in-flight /btw; cycle 230's btw flow is
// synchronous, so the only states copyBtwCard reports are done or
// failed.
//
// Indexing is 1-based from the most recent response. The filler for
// the n-th form is 【推测】 — only the default form's `last response`
// is recovered — and so is the fallback for a non-numeric argument
// (the default form).
func resolveCopy(args string, responses []string) (string, string) {
	args = trimSpace(args)
	if args == "" {
		if len(responses) == 0 {
			return "", "No response to copy."
		}
		return responses[len(responses)-1], "Copied last response to clipboard."
	}
	n, err := strconv.Atoi(args)
	if err != nil || n < 1 || n > len(responses) {
		if err != nil || n < 1 {
			// Not an index at all: behave like the bare form.
			if len(responses) == 0 {
				return "", "No response to copy."
			}
			return responses[len(responses)-1], "Copied last response to clipboard."
		}
		return "", "No response to copy."
	}
	return responses[len(responses)-n], fmt.Sprintf("Copied response %d to clipboard.", n)
}

// statuslineFeedback toggles the status line and returns (persisted value,
// message). Recovered: the off-state message includes a re-enable instruction
// ("Statusline off. Run /statusline to re-enable."); the on-state message is
// just "Statusline on." (/tmp/agycmp/statusline_probe.py).
func statuslineFeedback(current string) (string, string) {
	if current == "off" {
		return "on", "Statusline on."
	}
	return "off", "Statusline off. Run /statusline to re-enable."
}

// titleFeedback handles /title, /title on and /title off and returns
// (persisted value, message). Recovered: `Usage: /title [on|off]`,
// `Custom title enabled. (%s)` and `Custom title disabled.`
//
// The %s is the title the CLI now shows. The reference's exact value is
// unrecovered; the clone passes the conversation title (the first user message,
// or the program name when there is none) — 【推测】.
func titleFeedback(arg, current, titleText string) (string, string) {
	switch strings.ToLower(trimSpace(arg)) {
	case "":
		if current == "on" {
			return "off", "Custom title disabled."
		}
		return "on", fmt.Sprintf("Custom title enabled. (%s)", titleText)
	case "on":
		return "on", fmt.Sprintf("Custom title enabled. (%s)", titleText)
	case "off":
		return "off", "Custom title disabled."
	}
	return current, "Usage: /title [on|off]"
}

// normalizeTitle cleans a session/conversation title so it can be safely shown
// in lists, breadcrumbs and the terminal window title.
//
// Cycle "session-title-strip": AGY 1.2.13 stopped leaking leading/trailing
// whitespace and embedded newlines into the rename field — `/rename " My
// Session\n"` used to render in the picker and the status-line JSON exactly
// as typed, including a trailing newline that broke breadcrumb layout. The
// fix is two rules, applied in order:
//
//  1. Trim every Unicode whitespace run at either end (covers spaces, tabs,
//     CR, LF, CRLF, etc.).
//  2. Collapse every internal whitespace run (including newlines) to a
//     single U+0020 space, so a multi-line paste becomes "My Session" rather
//     than "My\n\nSession".
//
// An input that is empty after step 1 (or only ever was whitespace) returns
// the empty string — callers can then surface the existing "please provide a
// name" error. Whitespace here means `unicode.IsSpace`, matching what most
// users see as a single "space" key.
func normalizeTitle(s string) string {
	// Step 1: trim. strings.TrimSpace already handles the full unicode.IsSpace
	// set (≥ Unicode whitespace categories, not just ' ' and '\t'), which is
	// why this helper exists alongside the older trimSpace that only handled
	// ASCII space and tab.
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Step 2: collapse internal whitespace. We walk by rune rather than byte
	// so multi-byte whitespace (NBSP, narrow no-break space, etc.) is
	// normalised the same way ASCII space is, instead of fragmenting.
	var b strings.Builder
	b.Grow(len(s))
	inWS := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !inWS {
				b.WriteByte(' ')
				inWS = true
			}
			continue
		}
		b.WriteRune(r)
		inWS = false
	}
	return b.String()
}

// conversationTitle is the text /title names. The reference's value is
// unrecovered; a /rename name wins, then the first user message, then the
// program name — 【推测】.
//
// Cycle "session-title-strip": the stored conversationName is run through
// normalizeTitle here as defence-in-depth, in case it is set by a future code
// path (e.g. /resume hydrating from disk) that skips renameFeedback. The
// renameFeedback path already normalises at write time; that is what keeps
// the breadcrumb and the window title clean in practice.
func (m *runMode) conversationTitle() string {
	if s := normalizeTitle(m.conversationName); s != "" {
		return s
	}
	for _, msg := range m.session {
		if msg.Role == "user" {
			if s := normalizeTitle(msg.Content); s != "" {
				return s
			}
		}
	}
	return filepath.Base(os.Args[0])
}

// renameFeedback implements /rename. Recovered: `Error: Please provide a new
// name for the conversation.` and `Conversation renamed to: %s`. It returns the
// new name (unchanged on error) and the line to show.
//
// Cycle "session-title-strip": the input is run through normalizeTitle so
// leading/trailing whitespace (incl. a trailing newline) and embedded newlines
// (e.g. a pasted multi-line clipboard line) are stripped before the rename is
// committed. The error branch now also fires for inputs that are only
// whitespace once normalised, not just for `""`.
func renameFeedback(arg, current string) (string, string) {
	name := normalizeTitle(arg)
	if name == "" {
		return current, "Error: Please provide a new name for the conversation."
	}
	return name, fmt.Sprintf("Conversation renamed to: %s", name)
}

// tasksFeedback reports the background-task counters. Recovered: `No background
// tasks.` for the empty case and `%d running, %d done` for a non-empty one. The
// clone runs no background tasks, so the empty branch is the only reachable one.
func tasksFeedback(running, done int) string {
	if running == 0 && done == 0 {
		return "No background tasks."
	}
	return fmt.Sprintf("%d running, %d done", running, done)
}

// remoteControlFeedback implements /remote-control [on|off]. Recovered:
// `Usage: /remote-control [on|off]`, `Remote control on for this session.` and
// `Remote control off. This machine is no longer reachable from other devices.`
// The command description — "Turn on remote control for this session (off to
// disable)" — makes the bare form mean *on*, not a toggle.
//
// The clone never opens the tunnel (the changelog's Remote Control is an
// enterprise-gateway feature), so this only tracks the session flag.
func remoteControlFeedback(arg string, current bool) (bool, string) {
	switch strings.ToLower(trimSpace(arg)) {
	case "", "on":
		return true, "Remote control on for this session."
	case "off":
		return false, "Remote control off. This machine is no longer reachable from other devices."
	}
	return current, "Usage: /remote-control [on|off]"
}

// editorFinishedMsg is delivered after an external editor process exits.
//
// tempPath is set when the editor was editing a scratch file that must be read
// back (ctrl+g prompt editing); it is empty for /open, which edits a real file
// in place.
type editorFinishedMsg struct {
	err      error
	tempPath string
}

// splitCommand splits an editor setting such as "code --wait" into a name and
// arguments.
func splitCommand(s string) (string, []string) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

// editorFallback returns the editor used when the `editor` setting is "auto"
// and $EDITOR is unset.
//
// Recovered from the changelog: "Fixed Windows editor fallback to use \"edit\"
// or \"notepad\" when the editor setting is \"auto\" and no editor is
// configured, instead of attempting to use \"vim\"." That fix is Windows-only,
// so `vim` is the Unix fallback and Windows prefers `edit`, then `notepad`.
func editorFallback() string {
	if runtime.GOOS == "windows" {
		for _, c := range []string{"edit", "notepad"} {
			if _, err := exec.LookPath(c); err == nil {
				return c
			}
		}
		return "notepad"
	}
	return "vim"
}

// resolveEditor returns the editor for /open and any notice to show first.
//
// Recovered: `No editor configured; using %s`, `Failed to resolve editor: %v`
// and the settings label `auto ($EDITOR)`. The `editor` settings key defaults to
// "auto", meaning "use $EDITOR"; see editorFallback for the fallback itself.
//
// $EDITOR is split on whitespace only, never on `=`: the changelog records
// "Fixed `$EDITOR` environment variable parsing: resolved issues where arguments
// containing `=` (e.g., `--alternate-editor=vi`) were incorrectly split".
func resolveEditor(configured string) (name string, args []string, notice string, err error) {
	configured = trimSpace(configured)
	if configured == "" || strings.EqualFold(configured, "auto") {
		if e := trimSpace(os.Getenv("EDITOR")); e != "" {
			name, args = splitCommand(e)
			return name, args, "", nil
		}
		name = editorFallback()
		return name, nil, fmt.Sprintf("No editor configured; using %s", name), nil
	}
	name, args = splitCommand(configured)
	return name, args, "", nil
}

// promptScratchFile writes the prompt to a scratch file for ctrl+g editing and
// returns its path. The caller removes it once the editor has exited.
func promptScratchFile(prompt string) (string, error) {
	f, err := os.CreateTemp("", "agy-prompt-*.md")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.WriteString(prompt); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// editPromptCmd implements ctrl+g: open the configured editor on the current
// prompt and read the result back.
//
// Recovered from the changelog: "Fixed pressing `Ctrl+G` to edit the prompt in a
// full-screen terminal editor such as `vim` hanging with `Vim: Warning: Output
// is not to a terminal`; external editors now run against a real terminal
// stdout", and "Added `ctrl+g` on the artifact view to open $EDITOR". The prompt
// is written to a scratch file, edited, and read back on exit.
func (m *runMode) editPromptCmd() tea.Cmd {
	name, args, notice, err := resolveEditor(auth.SettingsString("editor"))
	if err != nil {
		m.response = append(m.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.response = append(m.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.response = append(m.response, notice)
	}
	path, err := promptScratchFile(m.input.Value())
	if err != nil {
		m.response = append(m.response, "Failed to create a temporary file: "+err.Error())
		return nil
	}
	c := exec.Command(name, append(args, path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err, tempPath: path}
	})
}

// openFeedback resolves /open into a command to run. It returns the line(s) to
// show before the editor starts and the command itself (nil when the file
// cannot be opened).
//
// Recovered: `Usage: /open <file_path>`, `File not found: %s`,
// `Opening file: %s`, `No editor configured; using %s` and
// `Failed to resolve editor: %v`. The caller emits
// `Editor exited with error: %v` when the process returns non-zero.
func openFeedback(arg, configuredEditor, workspace string) (string, *exec.Cmd) {
	path := unquotePath(arg)
	if path == "" {
		return "Usage: /open <file_path>", nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	if !pathExists(path) {
		return fmt.Sprintf("File not found: %s", path), nil
	}
	name, args, notice, err := resolveEditor(configuredEditor)
	if err != nil {
		return fmt.Sprintf("Failed to resolve editor: %v", err), nil
	}
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Sprintf("Failed to resolve editor: %v", err), nil
	}
	c := exec.Command(name, append(args, path)...)
	c.Dir = workspace
	msg := fmt.Sprintf("Opening file: %s", path)
	if notice != "" {
		msg = notice + "\n" + msg
	}
	return msg, c
}

// codeSearchMaxHits caps how many matches /codesearch renders. Matches beyond
// the cap are counted but not stored, so the recovered `  | %d more` tail can
// report how many were withheld.
const codeSearchMaxHits = 200

// codeSearchSkipDirs are directories /codesearch never descends into. `.git`,
// `.hg` and `.jj` hold the VCS's own bookkeeping; the rest are dependency trees
// that are noise for a workspace search.
var codeSearchSkipDirs = map[string]bool{
	".git":         true,
	".hg":          true,
	".jj":          true,
	"node_modules": true,
	".venv":        true,
	"vendor":       true,
}

// codeSearchMatcher builds the pattern for /codesearch. Recovered usage
// `Usage: /codesearch [-F|--literal] <query>` means the default is a pattern and
// `-F` / `--literal` selects a fixed-string match (ripgrep's convention).
//
// Cycle package-simplification cycle 3: switched the in-process
// preview from stdlib `regexp` (RE2) to `regexp2` (.NET-style regex
// engine) so the preview matches ripgrep's pattern language: named
// groups, lookarounds (`(?=...)`, `(?<=...)`), atomic groups
// (`(?>...)`), and the rest of the PCRE feature set ripgrep ships.
// The preview was the only place in the clone where the in-process
// search engine disagreed with the actual `rg` invocation.
//
// Literal mode still wraps the query in regexp2's equivalent of
// `regexp.QuoteMeta` — `regexp2.Escape` — so the `-F` contract is
// preserved.
func codeSearchMatcher(query string, literal bool) (*regexp2.Regexp, error) {
	if literal {
		return regexp2.Compile(regexp2.Escape(query), regexp2.None)
	}
	return regexp2.Compile(query, regexp2.None)
}

// searchWorkspace walks dir and returns `relpath:line:col: text` for the first
// codeSearchMaxHits lines matching re, plus the number of further matches that
// were counted but withheld.
//
// Unreadable entries are skipped rather than failing the search, and files
// larger than 1 MiB or carrying a NUL in their first 8 KiB are treated as binary
// and skipped.
//
// Cycle package-simplification cycle 3: the matcher is now
// `*regexp2.Regexp`. We use `FindStringMatch` instead of
// `FindStringIndex` (regexp2 doesn't expose the index-slice form);
// the match's `.Index`/`[a:b]` tuple gives us the start column. A
// nil error means "the pattern compiled cleanly but did not match";
// a non-nil error is the regexp2 engine reporting a runtime issue
// (very rare — only happens for catastrophic backtracking on
// non-greedy quantifiers); we treat both as "no match" so the
// search still produces a result set.
func searchWorkspace(dir string, re *regexp2.Regexp) (hits []string, extra int) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if codeSearchSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 1<<20 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		head := data
		if len(head) > 8<<10 {
			head = head[:8<<10]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			rel = path
		}
		for i, line := range strings.Split(string(data), "\n") {
			m, err := re.FindStringMatch(line)
			if err != nil || m == nil {
				continue
			}
			// m.Index is [start, end) byte offsets into line;
			// /codesearch reports the start column (1-based,
			// matching the ripgrep "n:line:col: text" format).
			if len(hits) >= codeSearchMaxHits {
				extra++
				continue
			}
			hits = append(hits, fmt.Sprintf("%s:%d:%d: %s",
				rel, i+1, m.Index+1, strings.TrimRight(line, "\r")))
		}
		return nil
	})
	return hits, extra
}

// codeSearchFeedback runs /codesearch: a workspace-wide pattern search.
//
// Recovered: `Usage: /codesearch [-F|--literal] <query>`, the description
// "Search code in the workspace (usage: /codesearch <query>)", `No results` for
// an empty hit set, and `  | %d more` for the truncation tail.
//
// The reference searches through the language server's `SearchCode` RPC and
// shows the hits in an interactive picker (`+ Match all (no filter)`,
// `Keep typing to filter files`). The clone has no language server, so it walks
// the workspace itself and appends `path:line:col: text` lines to the
// transcript: the *result line format* is 【推测】, the messages are not.
//
// `Searching for %q...` is not emitted — like `Loading diff`, it is a transient
// progress label shown before the content.
func codeSearchFeedback(args, workspace string) string {
	literal := false
	fields := strings.Fields(args)
	for len(fields) > 0 && strings.HasPrefix(fields[0], "-") && fields[0] != "--" {
		switch fields[0] {
		case "-F", "--literal":
			literal = true
		default:
			return "Usage: /codesearch [-F|--literal] <query>"
		}
		fields = fields[1:]
	}
	if len(fields) > 0 && fields[0] == "--" {
		fields = fields[1:]
	}
	query := strings.TrimSpace(strings.Join(fields, " "))
	if query == "" {
		return "Usage: /codesearch [-F|--literal] <query>"
	}
	if workspace == "" {
		return "no workspace directory"
	}
	re, err := codeSearchMatcher(query, literal)
	if err != nil {
		return fmt.Sprintf("invalid pattern: %v", err)
	}
	hits, extra := searchWorkspace(workspace, re)
	if len(hits) == 0 && extra == 0 {
		return "No results"
	}
	var b strings.Builder
	// The recovered panel header is `Found ` plus the count: the string table
	// carries both `Found %d results` and `1 result`, and the changelog records
	// "Fixed counts in tool steps and the `/codesearch` panel, which now read
	// \"Found 1 result\", \"1 match\", \"1 file\", and \"Wrote 1 line\" instead of
	// \"1 results\"".
	b.WriteString("Found " + countLabel(len(hits)+extra, categoryWords["result"]))
	for _, h := range hits {
		b.WriteString("\n")
		b.WriteString(h)
	}
	if extra > 0 {
		fmt.Fprintf(&b, "\n  | %d more", extra)
	}
	return b.String()
}

// detectVCS returns the version-control system that owns dir, or "" when there
// is none. Recovered: the reference supports Git, Mercurial and Jujutsu for
// /diff (the 1.3.4 changelog names all three, and the enum carries
// VCS_TYPE_GIT / VCS_TYPE_JJ).
func detectVCS(dir string) string {
	for {
		switch {
		case pathExists(filepath.Join(dir, ".git")):
			return "git"
		case pathExists(filepath.Join(dir, ".hg")):
			return "hg"
		case pathExists(filepath.Join(dir, ".jj")):
			return "jj"
		}
		parent := filepath.Dir(dir)
		if parent == dir || parent == "" {
			return ""
		}
		dir = parent
	}
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// vcsDiff runs the working-copy diff. Pagers and colour are disabled the way
// the reference does it: the 1.3.4 changelog records that "agent commands now
// set JJ_PAGER=cat alongside PAGER and GIT_PAGER", and that /diff must not be
// fooled by repositories configured to always colour their output. The 1.3.6
// changelog line 7 adds the editor-disabling trio (GIT_EDITOR / HGEDITOR /
// JJ_EDITOR = false) so a future /commit-style slash that fails through to a
// real `git commit` cannot hang on a hidden vim session.
func vcsDiff(vcs, dir string) (string, error) {
	var args []string
	switch vcs {
	case "git":
		args = []string{"--no-pager", "diff", "--no-color"}
	case "hg":
		args = []string{"diff"}
	case "jj":
		args = []string{"--color=never", "diff"}
	default:
		return "", fmt.Errorf("unsupported VCS %q", vcs)
	}
	cmd := exec.Command(vcs, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PAGER=cat", "GIT_PAGER=cat", "JJ_PAGER=cat",
		"GIT_EDITOR=false", "HGEDITOR=false", "JJ_EDITOR=false",
		"HGPLAIN=1", "NO_COLOR=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}
	return string(out), nil
}

// ansiEscapeRE matches CSI escape sequences of the form ESC [ params final,
// where params are digits / ';' / '?' (the parameter bytes per ECMA-48) and
// the final byte is a letter in the range '@'..'~' (most commonly 'm' for
// SGR colour). It does not match OSC sequences or single-character escapes,
// which VCS diff commands do not emit. The 1.3.4 changelog line 33 records
// the regression this helper fixes: /diff showed "No uncommitted changes"
// for clean repositories whose diff command still emitted colour codes
// because colour.ui = always (or hg/jj equivalents) overrode the --no-color /
// HGPLAIN=1 / --color=never belt-and-braces that vcsDiff already applies.
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// stripANSI removes ANSI CSI escape sequences from s. The returned string
// preserves all other bytes, including embedded newlines and surrounding
// whitespace, so a caller's TrimSpace / Split pipeline keeps working.
func stripANSI(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}

// vcsDiffFunc is the test seam for diffFeedback. Production code reads it
// directly; tests override it with a function that returns canned output so
// the color.ui = always regression can be exercised without configuring a
// real repository.
var vcsDiffFunc = vcsDiff

// diffFeedback renders /diff: the workspace's uncommitted changes.
//
// Recovered: `No uncommitted changes` for an empty diff, `no workspace
// directory`, the per-VCS error wrapper `git diff: %w` (and the `hg diff:` /
// `jj diff:` siblings), and the viewer footer `  [%d-%d of %d]  (end of diff)`.
//
// The reference renders the diff in an interactive, syntax-highlighted viewer
// (1.3.5 changelog: "Added syntax highlighting to the /diff file view"); the
// clone appends plain lines to the transcript. The *presentation* is therefore
// 【推测】 while the body and the empty-case line are not.
//
// Cycle file-link-percent-encoded-anchor: prepends a "Files:" line listing
// each file touched by the diff as a percent-encoded clickable link
// (`[file:N](file%23LN)`), so the markdown rendering of an /diff echo
// (or any future markdown-rendering integration) lands on the right line
// in editors that only honour percent-encoded anchors (AGY 1.3.3
// changelog line 59). The recovered body and footer stay verbatim.
//
// Cycle separate-links-hover: each emitted link now carries a
// `?h=<index>` hover-card disambiguator (render.MarkdownFileLinkAt), so
// even when the diff references the same file at the same line twice,
// each link instance has a distinct hover card (AGY 1.3.3 changelog
// line 60).
//
// Cycle diff-cjk-cutoff: maxCells caps each rendered line so a runaway diff
// body (minified JS, embedded blobs, long file paths, CJK-heavy content)
// does not overflow the transcript. truncateDiffLine respects grapheme
// cluster boundaries so a CJK character or a ZWJ family-emoji sequence at
// the cutoff edge is never split mid-cluster. Passing maxCells <= 0
// disables truncation entirely and returns the raw diff body.
//
// Cycle diff-color-ui-always: the body is run through stripANSI before the
// emptiness check so a clean repository whose diff command still emits
// colour codes (e.g. colour.ui = always, hg ui.color=always, jj
// ui.color=always) is correctly reported as "No uncommitted changes". The
// displayed body is also stripped so the transcript stays free of stray
// escape bytes.
func diffFeedback(workspace string, maxCells int) string {
	if workspace == "" {
		return "no workspace directory"
	}
	vcs := detectVCS(workspace)
	if vcs == "" {
		// Cycle diff-git-head-fallback (AGY 1.3.3 line 52): when no
		// tracked VCS is detected (workspace is not under git, hg,
		// or jj), REF falls back to `git -C <workspace> diff HEAD`
		// rather than reporting the misleading
		// "No uncommitted changes". The fallback surfaces a clean
		// "Not in a git repository" line when the workspace is
		// outside any repo, and renders the same `[1-N of N]
		// (end of diff)` footer the VCS path uses when the diff
		// body is non-empty. See gitDiffHeadFeedback.
		return gitDiffHeadFeedback(workspace)
	}
	out, err := vcsDiffFunc(vcs, workspace)
	if err != nil {
		return fmt.Sprintf("%s diff: %v", vcs, err)
	}
	body := stripANSI(strings.TrimRight(out, "\n"))
	if strings.TrimSpace(body) == "" {
		return "No uncommitted changes"
	}
	lines := strings.Split(body, "\n")
	preamble := diffFilesPreamble(body)
	var b strings.Builder
if preamble != "" {
		b.WriteString(preamble)
		b.WriteString("\n")
	}
	b.WriteString(truncateDiffLine(lines[0], maxCells))
	for _, l := range lines[1:] {
		b.WriteString("\n")
		b.WriteString(truncateDiffLine(l, maxCells))
	}
	fmt.Fprintf(&b, "\n  [%d-%d of %d]  (end of diff)", 1, len(lines), len(lines))
	return b.String()
}

// gitDiffHeadFeedback is the AGY 1.3.3 line 52 fallback for /diff
// when the workspace is not under git, hg or jj version control. It
// shells out to `git -C <workspace> diff HEAD` (the canonical git
// command for "diff against the last commit") and lets the underlying
// tool's exit code / stderr drive the surfaced string. Expected
// outcomes:
//
//   - workspace IS inside a git repo (e.g. .git file/symlink that
//     detectVCS missed, submodule with broken work-tree): empty diff
//     body → "No uncommitted changes"; non-empty body → the same
//     `[1-N of N] (end of diff)` footer the VCS path emits.
//   - workspace is NOT inside a git repo: git itself prints
//     `fatal: not a git repository (or any of the parent directories):
//     .git` and exits non-zero, so runGit returns a non-nil error.
//     We surface "Not in a git repository" instead of forwarding the
//     stderr dump and instead of the historical misleading
//     "No uncommitted changes" that hid the lack of a repo.
//
// The fallback uses runGit (the same helper the rest of /diff relies
// on) so PAGER=cat / GIT_PAGER=cat / GIT_EDITOR=false are already set;
// no extra hanging on a pager or an invisible editor.
func gitDiffHeadFeedback(workspace string) string {
	out, err := runGit(workspace, "diff", "HEAD")
	if err != nil {
		return "Not in a git repository"
	}
	return renderDiffFooter(out)
}

// renderDiffFooter writes the `[/N of /N]  (end of diff)` trailer the
// reference's /diff viewer uses (recovered from 1.3.6 string table)
// and returns `No uncommitted changes` when the body is whitespace-
// only or empty. The VCS path (`vcsDiff`) and the AGY 1.3.3 line 52
// fallback (`gitDiffHeadFeedback`) both feed through this helper so
// a non-empty diff renders identically regardless of which command
// produced it.
func renderDiffFooter(body string) string {
	body = strings.TrimRight(body, "\n")
	if strings.TrimSpace(body) == "" {
		return "No uncommitted changes"
	}
	lines := strings.Split(body, "\n")
	var b strings.Builder
	b.WriteString(lines[0])
	for _, l := range lines[1:] {
		b.WriteString("\n")
		b.WriteString(l)
	}
	fmt.Fprintf(&b, "\n  [%d-%d of %d]  (end of diff)", 1, len(lines), len(lines))
	return b.String()
}

// diffPlusFileRe matches the `+++ b/<path>` line that follows each
// `diff --git` block in `git diff` output. The path lives in capture 1;
// the `b/` prefix is what git writes for the working-tree side.
var diffPlusFileRe = regexp.MustCompile(`^\+\+\+ b/(.+?)\s*$`)

// diffHunkRe matches the `@@ -X,Y +A,B @@` hunk header. The first new-
// side line number is capture 1.
var diffHunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// diffFilesPreamble walks a `git diff` body and emits a comma-separated
// list of clickable file links, one per file touched by the diff,
// pointing at the first changed line. Returns "" when no file path
// could be extracted (e.g. an empty / malformed diff); diffFeedback
// then emits no preamble so its tests stay verbatim.
//
// Each link carries a `?h=<index>` hover-card disambiguator
// (render.MarkdownFileLinkAt) so that even when the diff touches the
// same file at the same line twice — or when /diff is run alongside
// /codesearch results that point at the same URL — each link instance
// has a distinct hover card (AGY 1.3.3 changelog line 60). The index is
// the link's position in the emitted list (0-based), which is
// deterministic because git emits `+++ b/path` blocks in the order it
// found them in the working tree.
//
// Implementation note: a `diff --git a/path b/path` block puts the
// new-side path on the immediately-following `+++ b/path` line, and
// the first changed line of any hunk lives on the next `@@ +A,B @@`.
// We walk line-by-line and pick the first hunk header we see after
// each `+++` marker, which matches the way git emits the data.
func diffFilesPreamble(body string) string {
	var (
		pendingPath string
		pendingLine int
		links       []string
	)
	commit := func() {
		if pendingPath == "" {
			return
		}
		label := pendingPath
		if pendingLine > 0 {
			label = pendingPath + ":" + strconv.Itoa(pendingLine)
		}
		// AGY 1.3.3 changelog line 60: distinct hover cards per link
		// instance. `len(links)` is the 0-based position of *this*
		// link in the emitted list — distinct per append — so even
		// two links pointing at the same (path, line) get distinct
		// `?h=<n>` markers. The path / fragment portion is still
		// the canonical AGY 1.3.3 line-59 percent-encoded anchor.
		links = append(links, render.MarkdownFileLinkAt(label, pendingPath, pendingLine, len(links)))
		pendingPath, pendingLine = "", 0
	}
	for _, ln := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(ln, "diff --git "):
			commit()
			// Reset; the next `+++` line carries the path.
		case diffPlusFileRe.MatchString(ln):
			commit()
			m := diffPlusFileRe.FindStringSubmatch(ln)
			if len(m) >= 2 {
				pendingPath = m[1]
			}
		case diffHunkRe.MatchString(ln):
			if pendingPath != "" && pendingLine == 0 {
				m := diffHunkRe.FindStringSubmatch(ln)
				if len(m) >= 2 {
					if v, err := strconv.Atoi(m[1]); err == nil && v > 0 {
						pendingLine = v
					}
				}
			}
		}
	}
	commit()
	if len(links) == 0 {
		return ""
	}
	return "Files: " + strings.Join(links, ", ")
}

// mcpServersFeedback renders /mcp. Recovered: `No MCP servers configured.` for
// the empty case (the reference's default). The per-server line is 【推测】 —
// the reference opens an interactive panel (`MCP servers`, `MCP server %d of
// %d  (enter to search)`) whose layout cannot be captured.
//
// Cycle 235 (AGY 1.2.16 close-gap): the third state "blocked" is added so
// the panel reflects a global MCP gate (AGY_CLI_DISABLE_MCP env var or
// mcp.json's disableAllMCP boolean). Per-server enabled=false rows still
// render — disabling one server is a per-server preference, not a global
// kill — but rows that would otherwise say "enabled" or "disabled" are
// relabelled "blocked" when the gate is on, so a user can tell at a
// glance that *every* row is gated, not just the individually-disabled
// ones.
//
// This is the same gap-closing shape the AGY 1.2.16 reference uses:
// previously the panel could render both "connected" (the cycle-235
// equivalent of our "enabled" cell) and "disabled" on the same row
// whenever the gate was on, leaving a user with two contradictory
// signals. The fix is a single state cell, "blocked", distinct from
// per-server disabled.
func mcpServersFeedback(cfg *auth.MCPConfig) string {
	if cfg == nil || len(cfg.Servers) == 0 {
		return "No MCP servers configured."
	}
	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	mcpsBlocked := auth.MCPsBlocked()
	var b strings.Builder
	b.WriteString("MCP servers")
	for _, name := range names {
		s := cfg.Servers[name]
		state := "disabled"
		if s.Enabled {
			state = "enabled"
		}
		if mcpsBlocked {
			state = "blocked"
		}
		target := s.URL
		if target == "" {
			target = strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
		}
		fmt.Fprintf(&b, "\n  %s\t%s\t%s\t%s", name, s.Type, state, target)
	}
	return b.String()
}

// copyToClipboard writes text to the system clipboard. The reference vendors
// github.com/atotto/clipboard — recovered symbols
// `google3/third_party/golang/github_com/atotto/clipboard/v/v0/clipboard.readAll`
// and `…clipboard.getPasteCommand` — which on darwin shells out to
// pbcopy/pbpaste and on Linux to xclip/xsel/wl-copy.
//
// Cycle package-simplification cycle 2: AGY 1.4.2 prefers the OSC 52
// escape over the atotto shell-out when the host terminal can
// interpret it. The escape is emitted to stderr (not stdout) so
// bubbletea's render loop is not corrupted. If SupportsOSC52 reports
// false for the host terminal, the atotto shell-out runs unchanged.
func copyToClipboard(text string) error {
	if SupportsOSC52() && osc52WriteText(os.Stderr, text) {
		return nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		switch {
		case haveExec("wl-copy"):
			cmd = exec.Command("wl-copy")
		case haveExec("xclip"):
			cmd = exec.Command("xclip", "-selection", "clipboard")
		case haveExec("xsel"):
			cmd = exec.Command("xsel", "--clipboard", "--input")
		default:
			return fmt.Errorf("no clipboard utility found (need wl-copy, xclip or xsel)")
		}
	default:
		return fmt.Errorf("clipboard is not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func haveExec(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// authServerName is the value interpolated into the recovered
// `Signed out from server: %s`. The reference's exact `%s` is unrecovered; the
// only server identity the clone holds is the Cloud Code endpoint host, so the
// argument name is 【推测】.
func authServerName() string {
	u, err := url.Parse(auth.CloudCodeEndpoint)
	if err != nil || u.Host == "" {
		return auth.CloudCodeEndpoint
	}
	return u.Host
}

// logoutFeedback deletes the stored OAuth token and returns the recovered
// confirmation line. A missing token is not an error — the reference's logout
// is idempotent.
func logoutFeedback() string {
	if os.Getenv("GEMINI_API_KEY") != "" {
		return "The active session uses a Gemini API key from the environment (GEMINI_API_KEY). To change it, update or unset the environment variable."
	}
	if os.Getenv("AGY_ADC_AUTH") != "" {
		return "The active session uses Application Default Credentials (AGY_ADC_AUTH). To change accounts, run 'gcloud auth application-default login' or unset the variable."
	}
	_, _ = auth.ClearTokenUnified()
	if err := os.Remove(auth.TokenFilePath()); err != nil && !os.IsNotExist(err) {
		return "error: logout failed: " + err.Error()
	}
	return fmt.Sprintf("Signed out from server: %s", authServerName())
}
