package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

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
		// mixed is the default
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

// Status — Git status. v4 32 #29.
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
