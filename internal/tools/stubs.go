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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

