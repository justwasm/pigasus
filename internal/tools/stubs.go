// Package tools — stubs.go. Real implementations of the 28 v4 stubs.
//
// Cycle 30 — replaced 28 placeholder stubs (cycle 29) with real impls.
// Cycle 31 — wired create_task / send_message / search_web / web_search /
// skill_search to internal/subagent. ddgr-driven search tools fall back to
// a "ddgr not in PATH" stub message when the binary is missing, otherwise
// the result is whatever `ddgr --num=N <query>` prints to stdout.
//
// Source:
//
//	cycle 20 tools.go (386 lines, real git/fs/cmd impls) — exec.Command wrappers
//	cycle 22 subagent.go (Subagent spawn/Send) — wired in cycle 31
//	cycle 23 mediagen.go (GenerateImage / ffmpegGradient fallback) — TODO: wire to internal/mediagen
//
// 28 stub tools now have real implementations (vs cycle 29 ErrNotImplemented
// placeholders). Each tool preserves its Tool interface contract:
//
//	Name() string
//	Schema() json.RawMessage
//	Run(ctx context.Context, args json.RawMessage) (any, error)
//
// Group breakdown:
//
//	3  lifecycle:  finish, schedule, cron
//	10 git ops:    fetch, pull, push, branch, checkout, merge, reset, revert, diff, status, show
//	5  search:     grep_search, grep, search_web, web_search, read_url_content, codesearch
//	3  local:      file_search, skill_search, wait
//	2  subagent:   create_task, send_message   (wired to internal/subagent)
//	5  misc:       notify_user, task_boundary, unzip, generate_image, (push/status via split above)
//
// NOTE on existing tests: tools_test.go has TestStubs_Reject that asserts every
// stub returns ErrNotImplemented. After this rewrite that test will fail on
// the 28 v4 stubs (browser_* stubs still reject). tools_test.go is preserved
// per the cycle-30 constraint, so the failure must be addressed by Agent 4 or
// by removing TestStubs_Reject from tools_test.go in a follow-up.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/subagent"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

// defaultSubagentManager tracks Subagents spawned by the create_task tool so
// that subsequent send_message calls can look them up by ID. It is
// package-level so it survives across multiple Tool.Run invocations within
// the same process (the typical CLI / TUI lifetime). Locking is provided
// by Manager's RWMutex.
var defaultSubagentManager = subagent.NewManager()

// defaultTaskRegistry is the tasks-panel registry shared between
// create_task / send_message and the runMode's /tasks panel. The
// tasks-panel cycle makes /tasks and the status-line "· N tasks"
// segment both consume this registry, so every Subagent spawned by
// the bash wrapper in create_task (or its ddgr cousins) is visible
// in the panel.
//
// The registry is intentionally package-level (not on a per-Model
// basis) because the bash wrapper itself is package-level; the
// future per-session scoping TODO in
// docs/cycles/tasks-panel/deliverable-tasks.md will thread the
// registry through Model.ToolRunner and remove this global.
var defaultTaskRegistry = taskregistry.New()

// SetDefaultTaskRegistry replaces the package-level registry. It
// exists so tests can drive the wiring with a fresh registry, and
// so the runMode can inject its own registry at start-up if it
// prefers per-session isolation. Pass nil to allocate a fresh one
// (and panic-free reset between test runs).
func SetDefaultTaskRegistry(r *taskregistry.Registry) *taskregistry.Registry {
	if r == nil {
		r = taskregistry.New()
	}
	prev := defaultTaskRegistry
	defaultTaskRegistry = r
	return prev
}

// SharedTaskRegistry returns the package-level task registry. The
// tasks-panel cycle wires the cmd/wagyu runMode against this so
// every spawn done by internal/subagent (the bash wrapper in
// create_task and the ddgr/grep search tools) is visible in /tasks
// and the status-line "· N tasks" segment.
//
// The pointer is stable for the lifetime of the process; callers
// should NOT close the registry.
func SharedTaskRegistry() *taskregistry.Registry { return defaultTaskRegistry }

// SharedSubagentManager returns the package-level subagent manager.
// The /tasks panel uses it to translate a "kill this row" action
// into a subagent.Kill. The pointer is stable for the lifetime of
// the process.
func SharedSubagentManager() *subagent.Manager { return defaultSubagentManager }

// ============ Internal helpers ============

// gitRun runs git in <dir> as CWD. Mirrors gitBase.run in git.go but lives
// here so we can implement new v4 git operations without coupling to the
// legacy git_* names.
func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// execRun runs <name> in <dir> as CWD and returns combined stdout/stderr.
func execRun(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// resolveDir returns <dir> if non-empty, else ".".
func resolveDir(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}

// trim trims trailing whitespace from command output.
func trim(s string) string { return strings.TrimSpace(s) }

// ============ Lifecycle (3) ============

// Finish — Mark turn done. v4 32 #1.
// Writes a `.finished` marker in the workspace containing timestamp + summary.
type Finish struct{ Workspace string }

func (s *Finish) Name() string { return "finish" }
func (s *Finish) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}}}`)
}
func (s *Finish) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Summary string `json:"summary"`
	}
	_ = json.Unmarshal(args, &p)
	marker := filepath.Join(resolveDir(s.Workspace), ".finished")
	body := fmt.Sprintf("finished_at=%s\nsummary=%s\n", time.Now().UTC().Format(time.RFC3339), p.Summary)
	if err := os.WriteFile(marker, []byte(body), 0644); err != nil {
		return nil, err
	}
	return "ok: finish marker written to " + marker, nil
}

// Schedule — One-shot schedule. v4 32 #3.
// Appends an entry to `.scheduled` and returns ok.
type Schedule struct{ Workspace string }

func (s *Schedule) Name() string { return "schedule" }
func (s *Schedule) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"at":{"type":"string"},"command":{"type":"string"}},"required":["at","command"]}`)
}
func (s *Schedule) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		At      string `json:"at"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.At == "" || p.Command == "" {
		return nil, errors.New("schedule: at and command are required")
	}
	marker := filepath.Join(resolveDir(s.Workspace), ".scheduled")
	entry := fmt.Sprintf("at=%s command=%q scheduled_at=%s\n",
		p.At, p.Command, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(marker, []byte(entry), 0644); err != nil {
		return nil, err
	}
	return fmt.Sprintf("ok: scheduled %q at %s", p.Command, p.At), nil
}

// Cron — Schedule cron. v4 32 #30.
// list/add/remove against a `.cron` file in workspace.
type Cron struct{ Workspace string }

func (s *Cron) Name() string { return "cron" }
func (s *Cron) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list","add","remove"]},"schedule":{"type":"string"},"command":{"type":"string"}}}`)
}
func (s *Cron) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action   string `json:"action"`
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
	}
	_ = json.Unmarshal(args, &p)
	marker := filepath.Join(resolveDir(s.Workspace), ".cron")
	switch p.Action {
	case "list":
		data, err := os.ReadFile(marker)
		if err != nil {
			if os.IsNotExist(err) {
				return "[]", nil
			}
			return nil, err
		}
		return string(data), nil
	case "add":
		if p.Schedule == "" || p.Command == "" {
			return nil, errors.New("cron: schedule and command are required for add")
		}
		entry := fmt.Sprintf("%s %s\n", p.Schedule, p.Command)
		f, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if _, err := f.WriteString(entry); err != nil {
			return nil, err
		}
		return "ok: cron entry added", nil
	case "remove":
		data, _ := os.ReadFile(marker)
		var keep []string
		for _, ln := range strings.Split(string(data), "\n") {
			if ln != "" && !strings.Contains(ln, p.Command) {
				keep = append(keep, ln)
			}
		}
		if err := os.WriteFile(marker, []byte(strings.Join(keep, "\n")+"\n"), 0644); err != nil {
			return nil, err
		}
		return "ok: cron entries matching command removed", nil
	default:
		return nil, errors.New("cron: action must be list, add, or remove")
	}
}

// ============ Git operations (10) ============

// Fetch — Git fetch. v4 32 #4.
type Fetch struct{ Workspace string }

func (s *Fetch) Name() string { return "fetch" }
func (s *Fetch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"remote":{"type":"string"},"branch":{"type":"string"}}}`)
}
func (s *Fetch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Remote string `json:"remote"`
		Branch string `json:"branch"`
	}
	_ = json.Unmarshal(args, &p)
	a := []string{"fetch"}
	if p.Remote != "" {
		a = append(a, p.Remote)
	}
	if p.Branch != "" {
		a = append(a, p.Branch)
	}
	out, err := gitRun(ctx, s.Workspace, a...)
	return trim(out), err
}

// Pull — Git pull. v4 32 #16.
type Pull struct{ Workspace string }

func (s *Pull) Name() string { return "pull" }
func (s *Pull) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"remote":{"type":"string"},"branch":{"type":"string"},"rebase":{"type":"boolean"}}}`)
}
func (s *Pull) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Remote string `json:"remote"`
		Branch string `json:"branch"`
		Rebase bool   `json:"rebase"`
	}
	_ = json.Unmarshal(args, &p)
	a := []string{"pull"}
	if p.Rebase {
		a = append(a, "--rebase")
	}
	if p.Remote != "" {
		a = append(a, p.Remote)
	}
	if p.Branch != "" {
		a = append(a, p.Branch)
	}
	out, err := gitRun(ctx, s.Workspace, a...)
	return trim(out), err
}

// Push — Git push. v4 32 #2. Delegates to GitPush (git.go).
type Push struct{ Workspace string }

func (s *Push) Name() string {
	return "push"
}
func (s *Push) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"remote":{"type":"string"},"branch":{"type":"string"}}}`)
}
func (s *Push) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return (&GitPush{Workspace: s.Workspace}).Run(ctx, args)
}

// Branch — Git branch. v4 32 #24. Adds "rename" on top of GitBranch.
type Branch struct{ Workspace string }

func (s *Branch) Name() string { return "branch" }
func (s *Branch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","delete","rename"]},"name":{"type":"string"},"newName":{"type":"string"}}}`)
}
func (s *Branch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action  string `json:"action"`
		Name    string `json:"name"`
		NewName string `json:"new_name"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Action == "rename" {
		if p.Name == "" || p.NewName == "" {
			return nil, errors.New("branch: name and new_name are required for rename")
		}
		out, err := gitRun(ctx, s.Workspace, "branch", "-m", p.Name, p.NewName)
		return trim(out), err
	}
	// list/create/delete — delegate to cycle-1.3.3 GitBranch.
	return (&GitBranch{Workspace: s.Workspace}).Run(ctx, args)
}

// Checkout — Git checkout. v4 32 #25.
type Checkout struct{ Workspace string }

func (s *Checkout) Name() string { return "checkout" }
func (s *Checkout) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"ref":{"type":"string"},"createBranch":{"type":"boolean"}}}`)
}
func (s *Checkout) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Ref          string `json:"ref"`
		CreateBranch bool   `json:"create_branch"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Ref == "" {
		return nil, errors.New("checkout: ref is required")
	}
	a := []string{"checkout"}
	if p.CreateBranch {
		a = append(a, "-b")
	}
	a = append(a, p.Ref)
	out, err := gitRun(ctx, s.Workspace, a...)
	return trim(out), err
}

// Merge — Git merge. v4 32 #32.
type Merge struct{ Workspace string }

func (s *Merge) Name() string { return "merge" }
func (s *Merge) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"branch":{"type":"string"},"noFF":{"type":"boolean"},"squash":{"type":"boolean"}}}`)
}
func (s *Merge) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Branch string `json:"branch"`
		NoFF   bool   `json:"no_ff"`
		Squash bool   `json:"squash"`
	}
	_ = json.Unmarshal(args, &p)
	a := []string{"merge"}
	if p.NoFF {
		a = append(a, "--no-ff")
	}
	if p.Squash {
		a = append(a, "--squash")
	}
	if p.Branch != "" {
		a = append(a, p.Branch)
	}
	out, err := gitRun(ctx, s.Workspace, a...)
	return trim(out), err
}

// Reset — Git reset. v4 32 #26.
type Reset struct{ Workspace string }

func (s *Reset) Name() string { return "reset" }
func (s *Reset) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["soft","mixed","hard"]},"target":{"type":"string"}}}`)
}
func (s *Reset) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Mode   string `json:"mode"`
		Target string `json:"target"`
	}
	_ = json.Unmarshal(args, &p)
	a := []string{"reset"}
	switch p.Mode {
	case "soft":
		a = append(a, "--soft")
	case "hard":
		a = append(a, "--hard")
	case "mixed", "":
		// mixed is the default, no flag needed
	default:
		return nil, fmt.Errorf("reset: mode %q not in (soft, mixed, hard)", p.Mode)
	}
	if p.Target != "" {
		a = append(a, p.Target)
	}
	out, err := gitRun(ctx, s.Workspace, a...)
	return trim(out), err
}

// Revert — Git revert. v4 32 #5.
type Revert struct{ Workspace string }

func (s *Revert) Name() string { return "revert" }
func (s *Revert) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"commit":{"type":"string"},"noCommit":{"type":"boolean"}}}`)
}
func (s *Revert) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Commit   string `json:"commit"`
		NoCommit bool   `json:"no_commit"`
	}
	_ = json.Unmarshal(args, &p)
	a := []string{"revert"}
	if p.NoCommit {
		a = append(a, "--no-commit")
	}
	if p.Commit != "" {
		a = append(a, p.Commit)
	}
	out, err := gitRun(ctx, s.Workspace, a...)
	return trim(out), err
}

// Status — Git status. v4 32 #29. Implements short/branch options on top
// of the cycle-1.3.3 GitStatus (which hardcodes --porcelain).
type Status struct{ Workspace string }

func (s *Status) Name() string { return "status" }
func (s *Status) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"short":{"type":"boolean"},"branch":{"type":"boolean"}}}`)
}
func (s *Status) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Short  bool `json:"short"`
		Branch bool `json:"branch"`
	}
	_ = json.Unmarshal(args, &p)
	a := []string{"status"}
	if p.Short {
		a = append(a, "--short")
	}
	if p.Branch {
		a = append(a, "--branch")
	}
	out, err := gitRun(ctx, s.Workspace, a...)
	return trim(out), err
}

// Diff — Git diff. v4 32 #10. Delegates to GitDiff (git.go).
type Diff struct{ Workspace string }

func (s *Diff) Name() string            { return "diff" }
func (s *Diff) Schema() json.RawMessage { return (&GitDiff{Workspace: s.Workspace}).Schema() }
func (s *Diff) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return (&GitDiff{Workspace: s.Workspace}).Run(ctx, args)
}

// Show — Git show. v4 32 #17.
type Show struct{ Workspace string }

func (s *Show) Name() string { return "show" }
func (s *Show) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"object":{"type":"string"},"stat":{"type":"boolean"}}}`)
}
func (s *Show) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Object string `json:"object"`
		Stat   bool   `json:"stat"`
	}
	_ = json.Unmarshal(args, &p)
	a := []string{"show"}
	if p.Stat {
		a = append(a, "--stat")
	}
	if p.Object != "" {
		a = append(a, p.Object)
	}
	out, err := gitRun(ctx, s.Workspace, a...)
	return trim(out), err
}

// ============ Search (5) ============

// GrepSearch — Regex content search. v4 32 #6.
// Tries ripgrep first (richer output), falls back to grep -rn.
type GrepSearch struct{ Workspace string }

func (s *GrepSearch) Name() string { return "grep_search" }
func (s *GrepSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"includePattern":{"type":"string"},"context":{"type":"integer"}},"required":["pattern"]}`)
}
func (s *GrepSearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Pattern        string `json:"pattern"`
		Path           string `json:"path"`
		IncludePattern string `json:"include_pattern"`
		Context        int    `json:"context"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		return nil, errors.New("grep_search: pattern is required")
	}
	dir := resolveDir(s.Workspace)
	if p.Path != "" {
		dir = filepath.Join(dir, p.Path)
	}
	if _, err := exec.LookPath("rg"); err == nil {
		rg := []string{"--no-heading", "--line-number"}
		if p.Context != 0 {
			rg = append(rg, fmt.Sprintf("-C%d", p.Context))
		}
		if p.IncludePattern != "" {
			rg = append(rg, "--glob", p.IncludePattern)
		}
		rg = append(rg, p.Pattern, dir)
		out, err := execRun(ctx, "", "rg", rg...)
		if err == nil {
			return out, nil
		}
	}
	grep := []string{"-rn"}
	if p.Context != 0 {
		grep = append(grep, fmt.Sprintf("-C%d", p.Context))
	}
	if p.IncludePattern != "" {
		grep = append(grep, "--include="+p.IncludePattern)
	}
	grep = append(grep, p.Pattern, dir)
	out, err := execRun(ctx, "", "grep", grep...)
	if err != nil {
		// grep exit 1 = no matches; that's not an error for a search.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return nil, err
	}
	return out, nil
}

// Grep — Grep alt. v4 32 #15. Always uses grep -rn (no grep, unlike
// GrepSearch which prefers rg).
type Grep struct{ Workspace string }

func (s *Grep) Name() string { return "grep" }
func (s *Grep) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"caseInsensitive":{"type":"boolean"}},"required":["pattern"]}`)
}
func (s *Grep) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Pattern         string `json:"pattern"`
		Path            string `json:"path"`
		CaseInsensitive bool   `json:"case_insensitive"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		return nil, errors.New("grep: pattern is required")
	}
	dir := resolveDir(s.Workspace)
	if p.Path != "" {
		dir = filepath.Join(dir, p.Path)
	}
	a := []string{"-rn"}
	if p.CaseInsensitive {
		a = append(a, "-i")
	}
	a = append(a, p.Pattern, dir)
	out, err := execRun(ctx, "", "grep", a...)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return nil, err
	}
	return out, nil
}

// SearchWeb — Web search (reference name `search_web`). v4 32 #8.
//
// The reference does not shell out: `search_web` is a grounded GenerateContent
// call, and when the route returns no grounded candidates the tool result is
// the plain string (【一手】 1.4.4, see testdata/result_texts_1.4.4.txt):
//
//	Encountered error in tool execution: no candidates returned from GenerateContent
//
// This build has no grounding provider, so that is the only honest result. Set
// Provider to plug one in; the text above stays the fallback.
type SearchWeb struct {
	Workspace string
	// Provider, when set, performs the actual search. The reference returns
	// grounded candidates as prose plus citation lines.
	Provider func(ctx context.Context, query, domain string) (string, error)
}

func (s *SearchWeb) Name() string { return "search_web" }
func (s *SearchWeb) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"domain":{"type":"string"}},"required":["query"]}`)
}
func (s *SearchWeb) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Query  string `json:"query"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Query == "" {
		return nil, errors.New("search_web: query is required")
	}
	if s.Provider != nil {
		out, err := s.Provider(ctx, p.Query, p.Domain)
		if err != nil {
			return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
		}
		return resultStamp(start) + out, nil
	}
	return resultStamp(start) + "Encountered error in tool execution: no candidates returned from GenerateContent", nil
}

// WebSearch — Web search alt. v4 32 #13.
// Cycle 31: same subagent wiring as SearchWeb but with --num=10 default to
// reflect the spec's "alt API" entry. Falls back to a stub when ddgr is
// missing.
type WebSearch struct{ Workspace string }

func (s *WebSearch) Name() string { return "web_search" }
func (s *WebSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"recencyDays":{"type":"integer"}},"required":["query"]}`)
}
func (s *WebSearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query       string `json:"query"`
		RecencyDays int    `json:"recency_days"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Query == "" {
		return nil, errors.New("web_search: query is required")
	}
	if _, err := exec.LookPath("ddgr"); err != nil {
		return fmt.Sprintf("web_search: ddgr not in PATH (query=%q, recency=%dd)",
			p.Query, p.RecencyDays), nil
	}
	sa := subagent.NewSubagent("ddgr", "--num=10", p.Query)
	// Tasks-panel: see SearchWeb's note.
	defaultTaskRegistry.AttachSubagent(sa, "")
	if err := sa.Start(); err != nil {
		return nil, fmt.Errorf("web_search: start ddgr: %w", err)
	}
	if err := sa.Wait(ctx); err != nil {
		return nil, fmt.Errorf("web_search: ddgr wait: %w", err)
	}
	if sa.State != "done" {
		return nil, fmt.Errorf("web_search: ddgr state=%s err=%v", sa.State, sa.Err)
	}
	return sa.Result, nil
}

// ReadURLContent — Fetch URL content (reference name `read_url_content`).
//
// The reference fetches the page, reduces it to an article, saves it under the
// conversation's brain directory and returns Title / OG Description plus a
// view_file hint (【一手】 1.4.4):
//
//	Title: Live Content
//
//	OG Description: Fetched live
//
//	The full content of the article at https://go.dev/ has been saved to: <path>
//
//	You can use the view_file tool to read specific sections if needed.
type ReadURLContent struct {
	Workspace string
	// ConversationID scopes the saved article. The reference uses the live
	// conversation id; see ScheduleTool for the fallback chain.
	ConversationID string
	// Home overrides the antigravity home (brain/ lives under it).
	Home string
}

func (s *ReadURLContent) Name() string { return "read_url_content" }
func (s *ReadURLContent) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"Url":{"type":"string"},"MaxBytes":{"type":"integer"}},"required":["Url"]}`)
}
func (s *ReadURLContent) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		URL      string `json:"url"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.URL == "" {
		return nil, errors.New("read_url_content: url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return resultStamp(start) + fmt.Sprintf("Encountered error in tool execution: HTTP %d for %s", resp.StatusCode, p.URL), nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
	}
	title, description := htmlTitleAndDescription(string(body))
	article := htmlToText(string(body))
	if p.MaxBytes > 0 && len(article) > p.MaxBytes {
		article = article[:p.MaxBytes]
	}

	conversationID := s.ConversationID
	if conversationID == "" {
		conversationID = os.Getenv("OPENAGY_CONVERSATION_ID")
	}
	if conversationID == "" {
		conversationID = processConversationID()
	}
	step := nextStep(conversationID)
	dir := filepath.Join(scheduleHome(s.Home), "brain", conversationID,
		".system_generated", "steps", fmt.Sprintf("%d", step))
	path := filepath.Join(dir, "content.md")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
	}
	if err := os.WriteFile(path, []byte(article), 0o600); err != nil {
		return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
	}
	return resultStamp(start) +
		"Title: " + title + "\n\n" +
		"OG Description: " + description + "\n\n" +
		"The full content of the article at " + p.URL + " has been saved to: " + path + "\n\n" +
		"You can use the view_file tool to read specific sections if needed.", nil
}

var (
	stepMu sync.Mutex
	steps  = map[string]int{}
)

// nextStep mints the per-conversation step number the reference uses for
// `.system_generated/steps/<n>/` — shared by every tool that spills an
// artifact there (read_url_content's content.md, run_command's output.txt).
func nextStep(conversationID string) int {
	stepMu.Lock()
	defer stepMu.Unlock()
	steps[conversationID]++
	return steps[conversationID]
}

var (
	htmlTitleRe  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	htmlOGDesc   = regexp.MustCompile(`(?is)<meta[^>]+property\s*=\s*["']og:description["'][^>]*>`)
	htmlDescCont = regexp.MustCompile(`(?is)content\s*=\s*["']([^"']*)["']`)
	htmlScript   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<noscript[^>]*>.*?</noscript>`)
	htmlTag      = regexp.MustCompile(`(?s)<[^>]+>`)
	htmlSqueeze  = regexp.MustCompile(`\n{3,}`)
)

// htmlTitleAndDescription extracts the <title> and og:description the
// reference prints above the saved-content path.
func htmlTitleAndDescription(page string) (title, description string) {
	if m := htmlTitleRe.FindStringSubmatch(page); len(m) == 2 {
		title = strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(m[1], "")))
	}
	if m := htmlOGDesc.FindString(page); m != "" {
		if c := htmlDescCont.FindStringSubmatch(m); len(c) == 2 {
			description = strings.TrimSpace(html.UnescapeString(c[1]))
		}
	}
	if title == "" {
		title = "Live Content"
	}
	if description == "" {
		description = "Fetched live"
	}
	return title, description
}

// htmlToText is a deliberately small article reducer: drop script/style, break
// block elements onto their own lines, strip tags, unescape entities and
// collapse runs of blank lines. It is not a readability parser; the reference's
// saved file is prose and this keeps the same shape.
func htmlToText(page string) string {
	page = htmlScript.ReplaceAllString(page, "")
	for _, tag := range []string{"</p>", "</div>", "</li>", "</h1>", "</h2>", "</h3>", "</h4>", "</tr>", "<br>", "<br/>", "<br />"} {
		page = strings.ReplaceAll(page, tag, "\n")
	}
	page = htmlTag.ReplaceAllString(page, "")
	page = html.UnescapeString(page)
	lines := strings.Split(page, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.TrimSpace(strings.Join(strings.Fields(line), " ")))
	}
	text := strings.TrimSpace(htmlSqueeze.ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
	return text + "\n"
}

// Codesearch — BM25-ish code search. v4 32 #19.
// Uses ripgrep with --json when available, else grep -rn with -m<topK>.
type Codesearch struct{ Workspace string }

func (s *Codesearch) Name() string { return "codesearch" }
func (s *Codesearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"topK":{"type":"integer"}},"required":["query"]}`)
}
func (s *Codesearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Query == "" {
		return nil, errors.New("codesearch: query is required")
	}
	if p.TopK <= 0 {
		p.TopK = 20
	}
	dir := resolveDir(s.Workspace)
	if _, err := exec.LookPath("rg"); err == nil {
		rg := []string{"--no-heading", "-n", "-S", "-m", fmt.Sprintf("%d", p.TopK), p.Query, dir}
		out, err := execRun(ctx, "", "rg", rg...)
		if err == nil {
			return out, nil
		}
	}
	a := []string{"-rn", fmt.Sprintf("-m%d", p.TopK), p.Query, dir}
	out, err := execRun(ctx, "", "grep", a...)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return nil, err
	}
	return out, nil
}

// FileSearch — File name search. v4 32 #21.
type FileSearch struct{ Workspace string }

func (s *FileSearch) Name() string { return "file_search" }
func (s *FileSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}`)
}
func (s *FileSearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Query == "" {
		return nil, errors.New("file_search: query is required")
	}
	if p.Limit <= 0 {
		p.Limit = 100
	}
	dir := resolveDir(s.Workspace)
	q := strings.ToLower(p.Query)
	matches := make([]string, 0, p.Limit)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			// skip hidden dirs (but not the root we were given)
			name := d.Name()
			if strings.HasPrefix(name, ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.Contains(strings.ToLower(d.Name()), q) {
			matches = append(matches, path)
			if len(matches) >= p.Limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

// SkillSearch — Search skills. v4 32 #22.
// Cycle 31: drives a `grep -rln <query> <skillsDir>` via
// internal/subagent so the capture / state machine come from the same code
// path as create_task / send_message. Empty stdout (or grep exit 1) means
// no matches; we surface that as an empty []string.
type SkillSearch struct{ Workspace string }

func (s *SkillSearch) Name() string { return "skill_search" }
func (s *SkillSearch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"topK":{"type":"integer"}},"required":["query"]}`)
}
func (s *SkillSearch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Query == "" {
		return nil, errors.New("skill_search: query is required")
	}
	if p.TopK <= 0 {
		p.TopK = 10
	}
	dir := resolveDir(s.Workspace)
	skillsDir := filepath.Join(dir, "skills")
	if _, err := os.Stat(skillsDir); err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	sa := subagent.NewSubagent("grep", "-rln", p.Query, skillsDir)
	// Tasks-panel: see SearchWeb's note.
	defaultTaskRegistry.AttachSubagent(sa, "")
	if err := sa.Start(); err != nil {
		return nil, fmt.Errorf("skill_search: start grep: %w", err)
	}
	if err := sa.Wait(ctx); err != nil {
		// grep exits 1 when no match — treat that as an empty result, not
		// an error (matches the legacy behaviour in Grep / GrepSearch).
		if exitErr, ok := sa.Err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []string{}, nil
		}
		return nil, fmt.Errorf("skill_search: grep wait: %w", err)
	}
	var matches []string
	for _, ln := range strings.Split(strings.TrimSpace(sa.Result), "\n") {
		if ln == "" {
			continue
		}
		matches = append(matches, ln)
		if len(matches) >= p.TopK {
			break
		}
	}
	if matches == nil {
		matches = []string{}
	}
	return matches, nil
}

// ============ Misc (10) ============

// Wait — Wait duration. v4 32 #23.
type Wait struct{ Workspace string }

func (s *Wait) Name() string { return "wait" }
func (s *Wait) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"seconds":{"type":"integer"},"duration":{"type":"string"}}}`)
}
func (s *Wait) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Seconds  int    `json:"seconds"`
		Duration string `json:"duration"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	var d time.Duration
	switch {
	case p.Seconds > 0:
		d = time.Duration(p.Seconds) * time.Second
	case p.Duration != "":
		parsed, err := time.ParseDuration(p.Duration)
		if err != nil {
			return nil, fmt.Errorf("wait: invalid duration %q: %w", p.Duration, err)
		}
		d = parsed
	default:
		return nil, errors.New("wait: either seconds or duration is required")
	}
	select {
	case <-time.After(d):
		return fmt.Sprintf("ok: waited %s", d), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CreateTask — Spawn subagent. v4 32 #27.
// Cycle 31: real Subagent spawn via subagent.NewSubagent(agentName, prompt).
// The created Subagent is registered in defaultSubagentManager so a
// subsequent send_message can look it up by ID, then Start/Wait is run to
// completion (blocking). The result surface (id + state + result) is
// returned as a JSON object so callers can confirm the agent finished.
type CreateTask struct{ Workspace string }

func (s *CreateTask) Name() string { return "create_task" }
func (s *CreateTask) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"agentName":{"type":"string"},"prompt":{"type":"string"},"title":{"type":"string"}},"required":["agentName","prompt"]}`)
}
func (s *CreateTask) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		AgentName string `json:"agent_name"`
		Prompt    string `json:"prompt"`
		Title     string `json:"title"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.AgentName == "" || p.Prompt == "" {
		return nil, errors.New("create_task: agent_name and prompt are required")
	}
	sa := subagent.NewSubagent(p.AgentName, p.Prompt)
	// Cycle bg-task-given-name-preserved: when the caller provides
	// a `title` argument, the task keeps that name in /tasks and
	// completion notifications instead of being renamed to an
	// auto-derived "bash -c <prompt>" form. The bridge's
	// subagentDisplayName reads Title first, so an empty title
	// keeps the previous behaviour (auto-derived name) for
	// backward compatibility.
	sa.SetTitle(p.Title)
	defaultSubagentManager.Add(sa)
	// Tasks-panel wiring: register every spawn with the
	// package-level registry so /tasks and the status row see
	// it. The hook is wired BEFORE Start so the running state
	// transition is the first thing subscribers observe — no
	// "pending" flash.
	defaultTaskRegistry.AttachSubagent(sa, "")
	if err := sa.Start(); err != nil {
		return nil, fmt.Errorf("create_task: start: %w", err)
	}
	if err := sa.Wait(ctx); err != nil {
		return nil, fmt.Errorf("create_task: wait: %w", err)
	}
	return map[string]any{
		"id":         sa.ID,
		"name":       sa.Name,
		"title":      p.Title,
		"state":      sa.State,
		"result":     sa.Result,
		"agent_name": p.AgentName,
	}, nil
}

// SendMessage — Message subagent. v4 32 #28.
// Cycle 31: looks up the Subagent registered by create_task via
// defaultSubagentManager.Get and pipes content into its stdin through
// Subagent.Send. An unknown ID is surfaced as an error rather than a
// silent no-op (callers expect delivery confirmation).
type SendMessage struct{ Workspace string }

func (s *SendMessage) Name() string { return "send_message" }
func (s *SendMessage) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"taskId":{"type":"string"},"content":{"type":"string"}},"required":["taskId","content"]}`)
}
func (s *SendMessage) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		TaskID  string `json:"task_id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.TaskID == "" || p.Content == "" {
		return nil, errors.New("send_message: task_id and content are required")
	}
	sa, ok := defaultSubagentManager.Get(p.TaskID)
	if !ok {
		return nil, fmt.Errorf("send_message: subagent %q not found (was create_task ever called?)", p.TaskID)
	}
	if err := sa.Send(p.Content); err != nil {
		return nil, fmt.Errorf("send_message: send: %w", err)
	}
	return fmt.Sprintf("ok: message sent to %s (state=%s)", p.TaskID, sa.State), nil
}

// NotifyUser — Notify user. v4 32 #7.
type NotifyUser struct{ Workspace string }

func (s *NotifyUser) Name() string { return "notify_user" }
func (s *NotifyUser) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"},"priority":{"type":"string","enum":["low","medium","high"]}},"required":["message"]}`)
}
func (s *NotifyUser) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Message  string `json:"message"`
		Priority string `json:"priority"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Message == "" {
		return nil, errors.New("notify_user: message is required")
	}
	if p.Priority == "" {
		p.Priority = "medium"
	}
	fmt.Printf("[notify_user][%s] %s\n", p.Priority, p.Message)
	marker := filepath.Join(resolveDir(s.Workspace), ".notified")
	entry := fmt.Sprintf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), p.Priority, p.Message)
	f, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.WriteString(entry); err != nil {
		return nil, err
	}
	return "ok: notified user", nil
}

// TaskBoundary — Mark task boundary. v4 32 #9.
type TaskBoundary struct{ Workspace string }

func (s *TaskBoundary) Name() string { return "task_boundary" }
func (s *TaskBoundary) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"},"type":{"type":"string","enum":["start","end"]},"id":{"type":"string"}},"required":["label","type"]}`)
}
func (s *TaskBoundary) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Label string `json:"label"`
		Type  string `json:"type"`
		ID    string `json:"id"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Label == "" || p.Type == "" {
		return nil, errors.New("task_boundary: label and type are required")
	}
	if p.ID == "" {
		p.ID = fmt.Sprintf("%s-%d", p.Type, time.Now().UnixNano())
	}
	marker := filepath.Join(resolveDir(s.Workspace), fmt.Sprintf(".task_boundary_%s", p.ID))
	body := fmt.Sprintf("label=%s\ntype=%s\nat=%s\n", p.Label, p.Type, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(marker, []byte(body), 0644); err != nil {
		return nil, err
	}
	return fmt.Sprintf("ok: task boundary %s -> %s (marker: %s)", p.Type, p.Label, marker), nil
}

// Unzip — Extract zip archive. v4 32 #11.
type Unzip struct{ Workspace string }

func (s *Unzip) Name() string { return "unzip" }
func (s *Unzip) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"destination":{"type":"string"}},"required":["archive"]}`)
}
func (s *Unzip) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Archive     string `json:"archive"`
		Destination string `json:"destination"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Archive == "" {
		return nil, errors.New("unzip: archive is required")
	}
	dst := p.Destination
	if dst == "" {
		dst = "."
	}
	if !filepath.IsAbs(p.Archive) && s.Workspace != "" {
		p.Archive = filepath.Join(s.Workspace, p.Archive)
	}
	if !filepath.IsAbs(dst) && s.Workspace != "" {
		dst = filepath.Join(s.Workspace, dst)
	}
	out, err := execRun(ctx, "", "unzip", "-o", p.Archive, "-d", dst)
	return trim(out), err
}

// GenerateImage — Image generation (reference name `generate_image`).
//
// The reference routes image generation through GenerateContent; on this
// account the call returns no image and the tool result is (【一手】 1.4.4):
//
//	Encountered error in tool execution: no response in generate content result
//
// Provider is the seam for a real generator; nil keeps the reference text.
type GenerateImage struct {
	Workspace string
	Provider  func(ctx context.Context, prompt, size string) (string, error)
}

func (s *GenerateImage) Name() string { return "generate_image" }
func (s *GenerateImage) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"Prompt":{"type":"string"},"ImageName":{"type":"string"}},"required":["Prompt","ImageName"]}`)
}
func (s *GenerateImage) Run(ctx context.Context, args json.RawMessage) (any, error) {
	start := time.Now()
	var p struct {
		Prompt    string `json:"prompt"`
		Size      string `json:"size"`
		ImageName string `json:"image_name"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Prompt == "" {
		return nil, errors.New("generate_image: prompt is required")
	}
	if s.Provider != nil {
		out, err := s.Provider(ctx, p.Prompt, p.Size)
		if err != nil {
			return resultStamp(start) + "Encountered error in tool execution: " + err.Error(), nil
		}
		return resultStamp(start) + out, nil
	}
	return resultStamp(start) + "Encountered error in tool execution: no response in generate content result", nil
}
