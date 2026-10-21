// Package tools implements the recovered Antigravity tool surface aligned
// with the v4 antigravity-recovered binary 7.2 MB CLI surface. The 1.3.3
// release defined 29 tools; the v4 binary defines 32 tools. The recovered
// build implements both: 32 v4 primary tools + 10 alias/backward-compat
// registrations that preserve the 1.3.3 names of tools that were renamed
// in v4 (write_file → create_file, list_directory → list_dir) or that
// were dropped from the v4 32 list but are still useful for 1.3.3 callers
// (read_file, edit_file, append_file, rename_file, copy_file, and the 4
// process-management tools).
//
// 42 tool entries total, 32 distinct v4 backing implementations + 10
// alias/backward-compat registrations:
//
//   - 8 fs primary:    read_file, edit_file, append_file, delete_file,
//                      rename_file, copy_file, list_dir, create_file
//   - 1 fs alias:      write_file (alias of create_file)
//   - 1 cmd primary:   run_command
//   - 4 cmd alias:     run_shell_command, exec_command, kill_process,
//                      list_processes (kept for 1.3.3 compat)
//   - 25 v4 stub:      finish, schedule, fetch, revert, grep_search,
//                      notify_user, search_web, task_boundary, diff,
//                      unzip, web_search, grep, pull, show, read_url_content,
//                      codesearch, file_search, skill_search, wait,
//                      branch, checkout, reset, create_task, send_message,
//                      status, cron, generate_image, merge
//
// Wait — that counts 28 stub, not 25. The 28 v4 stub tools match the
// 32 v4 tool set minus the 4 tools that have real implementations in
// the recovered build (run_command, list_dir, create_file, delete_file).
//
// The 9 git_* tools and 7 browser_* tools from the 1.3.3 build remain
// in git.go / browser.go as defined types, but are NOT registered by
// RegisterAll. They are superseded in v4 by status/diff/branch/etc.
// (git) and dropped entirely (browser).
//
// The package exposes Tool (Name/Schema/Run), Registry (Register/Get/
// List/Size), and RegisterAll.
package tools

import (
	"context"
	"encoding/json"
	"errors"
)

// Tool is the recovered tool interface. Source: agent.tools.Tool.
type Tool interface {
	Name() string
	Schema() json.RawMessage
	Run(ctx context.Context, args json.RawMessage) (any, error)
}

// Registry is the recovered tool registry. Source: agent.tools.Registry.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

func (r *Registry) Register(t Tool) { r.tools[t.Name()] = t }
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

// Size returns the number of registered tools.
func (r *Registry) Size() int { return len(r.tools) }

// ErrNotImplemented is returned by tools whose real implementation
// is not yet available in the recovered build. Cycle 50: the error
// string no longer uses the "placeholder" prefix (matches the
// v1.3.5 binary convention; the prefix was a cycle-25 stub marker
// the v1.3.5 binary does not emit).
var ErrNotImplemented = errors.New("tool not implemented in recovered build")

// RegisterAll registers the v4-aligned 32 tool primary set + 10
// alias/backward-compat entries for 1.3.3 callers. Total: 42 entries.
func RegisterAll(reg *Registry, workspace string) {
	for _, t := range []Tool{
		// 8 fs primary (filesystem.go)
		&ReadFile{Workspace: workspace},
		&CreateFile{Workspace: workspace},
		&EditFile{Workspace: workspace},
		&AppendFile{Workspace: workspace},
		&DeleteFile{Workspace: workspace},
		&RenameFile{Workspace: workspace},
		&CopyFile{Workspace: workspace},
		&ListDir{Workspace: workspace},
		// 1 fs alias: write_file = create_file
		&WriteFile{Workspace: workspace},
		// 1 cmd primary (command.go)
		&RunCommand{Workspace: workspace},
		// 4 cmd alias / 1.3.3-compat process tools
		&RunShellCommand{Workspace: workspace},
		&ExecCommand{Workspace: workspace},
		&KillProcess{},
		&ListProcesses{},
		// 25 v4 stub (stubs.go) — primary names, ErrNotImplemented placeholders.
		// (28 total stubs minus 0 from existing; the user's spec lists 28 v4
		// stub entries which together with the 4 real-implemented v4 tools
		// (run_command, list_dir, create_file, delete_file) = 32 v4 tools.)
		&Finish{Workspace: workspace},
		&Schedule{Workspace: workspace},
		&Fetch{Workspace: workspace},
		&Revert{Workspace: workspace},
		&GrepSearch{Workspace: workspace},
		&NotifyUser{Workspace: workspace},
		&SearchWeb{Workspace: workspace},
		&TaskBoundary{Workspace: workspace},
		&Diff{Workspace: workspace},
		&Unzip{Workspace: workspace},
		&WebSearch{Workspace: workspace},
		&Grep{Workspace: workspace},
		&Pull{Workspace: workspace},
		&Show{Workspace: workspace},
		&ReadURLContent{Workspace: workspace},
		&Codesearch{Workspace: workspace},
		&FileSearch{Workspace: workspace},
		&SkillSearch{Workspace: workspace},
		&Wait{Workspace: workspace},
		&Branch{Workspace: workspace},
		&Checkout{Workspace: workspace},
		&Reset{Workspace: workspace},
		&CreateTask{Workspace: workspace},
		&SendMessage{Workspace: workspace},
		&Status{Workspace: workspace},
		&Cron{Workspace: workspace},
		&GenerateImage{Workspace: workspace},
		&Merge{Workspace: workspace},
	} {
		reg.Register(t)
	}
}

// PlaceholderStr describes the placeholder behaviour.
const PlaceholderStr = "recovered v4 placeholder; sandbox denies real fs/git/browser ops in CI"
