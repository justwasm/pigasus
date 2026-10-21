// Git tools. Source: google3/third_party/jetski/agent/tools.
// Nine tools recovered in 1.3.3:
//
//   - git_status
//   - git_diff
//   - git_log
//   - git_branch
//   - git_commit
//   - git_push
//   - git_add
//   - git_remote
//   - git_tag
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

// gitBase is shared by every git_* tool.
type gitBase struct {
	Workspace string
}

func (g *gitBase) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Workspace
	cmd.Env = AgentShellEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// GitStatus — Source: tools.git_status.
type GitStatus struct{ Workspace string }

func (g *GitStatus) Name() string            { return "git_status" }
func (g *GitStatus) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (g *GitStatus) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return (&gitBase{Workspace: g.Workspace}).run(ctx, "status", "--porcelain")
}

// GitDiff — Source: tools.git_diff.
type GitDiff struct{ Workspace string }

func (g *GitDiff) Name() string { return "git_diff" }
func (g *GitDiff) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"staged":{"type":"boolean"},"path":{"type":"string"}}}`)
}
func (g *GitDiff) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Staged bool   `json:"staged"`
		Path   string `json:"path"`
	}
	_ = json.Unmarshal(args, &p)
	gitArgs := []string{"diff"}
	if p.Staged {
		gitArgs = append(gitArgs, "--staged")
	}
	if p.Path != "" {
		gitArgs = append(gitArgs, "--", p.Path)
	}
	return (&gitBase{Workspace: g.Workspace}).run(ctx, gitArgs...)
}

// GitLog — Source: tools.git_log.
type GitLog struct{ Workspace string }

func (g *GitLog) Name() string { return "git_log" }
func (g *GitLog) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"},"oneline":{"type":"boolean"}}}`)
}
func (g *GitLog) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Limit   int  `json:"limit"`
		Oneline bool `json:"oneline"`
	}
	_ = json.Unmarshal(args, &p)
	gitArgs := []string{"log"}
	if p.Oneline {
		gitArgs = append(gitArgs, "--oneline")
	} else {
		gitArgs = append(gitArgs, "--pretty=format:%H %an %ad %s", "--date=short")
	}
	if p.Limit > 0 {
		gitArgs = append(gitArgs, fmt.Sprintf("-%d", p.Limit))
	}
	return (&gitBase{Workspace: g.Workspace}).run(ctx, gitArgs...)
}

// GitBranch — Source: tools.git_branch.
type GitBranch struct{ Workspace string }

func (g *GitBranch) Name() string { return "git_branch" }
func (g *GitBranch) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","delete"]},"name":{"type":"string"}}}`)
}
func (g *GitBranch) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	gitArgs := []string{"branch"}
	switch p.Action {
	case "create":
		if p.Name == "" {
			return nil, errors.New("git_branch: name is required for create")
		}
		gitArgs = append(gitArgs, p.Name)
	case "delete":
		if p.Name == "" {
			return nil, errors.New("git_branch: name is required for delete")
		}
		gitArgs = append(gitArgs, "-D", p.Name)
	default: // list
		gitArgs = append(gitArgs, "-a")
	}
	return (&gitBase{Workspace: g.Workspace}).run(ctx, gitArgs...)
}

// GitCommit — Source: tools.git_commit.
type GitCommit struct{ Workspace string }

func (g *GitCommit) Name() string { return "git_commit" }
func (g *GitCommit) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"},"amend":{"type":"boolean"}},"required":["message"]}`)
}
func (g *GitCommit) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Message string `json:"message"`
		Amend   bool   `json:"amend"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if p.Message == "" && !p.Amend {
		return nil, errors.New("git_commit: message is required (unless amend)")
	}
	gitArgs := []string{"commit"}
	if p.Amend {
		gitArgs = append(gitArgs, "--amend", "--no-edit")
	} else {
		gitArgs = append(gitArgs, "-m", p.Message)
	}
	return (&gitBase{Workspace: g.Workspace}).run(ctx, gitArgs...)
}

// GitPush — Source: tools.git_push.
type GitPush struct{ Workspace string }

func (g *GitPush) Name() string { return "git_push" }
func (g *GitPush) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"remote":{"type":"string"},"branch":{"type":"string"},"setUpstream":{"type":"boolean"}}}`)
}
func (g *GitPush) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Remote      string `json:"remote"`
		Branch      string `json:"branch"`
		SetUpstream bool   `json:"setUpstream"`
	}
	_ = json.Unmarshal(args, &p)
	gitArgs := []string{"push"}
	if p.SetUpstream {
		gitArgs = append(gitArgs, "-u")
	}
	if p.Remote != "" {
		gitArgs = append(gitArgs, p.Remote)
	}
	if p.Branch != "" {
		gitArgs = append(gitArgs, p.Branch)
	}
	return (&gitBase{Workspace: g.Workspace}).run(ctx, gitArgs...)
}

// GitAdd — Source: tools.git_add.
type GitAdd struct{ Workspace string }

func (g *GitAdd) Name() string { return "git_add" }
func (g *GitAdd) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"all":{"type":"boolean"}}}`)
}
func (g *GitAdd) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
		All  bool   `json:"all"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	gitArgs := []string{"add"}
	if p.All {
		gitArgs = append(gitArgs, "-A")
	} else if p.Path != "" {
		gitArgs = append(gitArgs, p.Path)
	} else {
		gitArgs = append(gitArgs, "-A")
	}
	return (&gitBase{Workspace: g.Workspace}).run(ctx, gitArgs...)
}

// GitRemote — Source: tools.git_remote.
type GitRemote struct{ Workspace string }

func (g *GitRemote) Name() string { return "git_remote" }
func (g *GitRemote) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list","add","remove"]},"name":{"type":"string"},"url":{"type":"string"}}}`)
}
func (g *GitRemote) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		Name   string `json:"name"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	gitArgs := []string{"remote"}
	switch p.Action {
	case "add":
		if p.Name == "" || p.URL == "" {
			return nil, errors.New("git_remote: name and url are required for add")
		}
		gitArgs = append(gitArgs, "add", p.Name, p.URL)
	case "remove":
		if p.Name == "" {
			return nil, errors.New("git_remote: name is required for remove")
		}
		gitArgs = append(gitArgs, "remove", p.Name)
	default:
		gitArgs = append(gitArgs, "-v")
	}
	return (&gitBase{Workspace: g.Workspace}).run(ctx, gitArgs...)
}

// GitTag — Source: tools.git_tag.
type GitTag struct{ Workspace string }

func (g *GitTag) Name() string { return "git_tag" }
func (g *GitTag) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","delete"]},"name":{"type":"string"},"message":{"type":"string"}}}`)
}
func (g *GitTag) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action  string `json:"action"`
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	gitArgs := []string{"tag"}
	switch p.Action {
	case "create":
		if p.Name == "" {
			return nil, errors.New("git_tag: name is required for create")
		}
		gitArgs = append(gitArgs, "-a", p.Name, "-m", p.Message)
	case "delete":
		if p.Name == "" {
			return nil, errors.New("git_tag: name is required for delete")
		}
		gitArgs = append(gitArgs, "-d", p.Name)
	default:
		// list is the default; no extra args
	}
	return (&gitBase{Workspace: g.Workspace}).run(ctx, gitArgs...)
}
