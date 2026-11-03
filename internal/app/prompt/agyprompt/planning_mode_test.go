package agyprompt

import (
	"strings"
	"testing"
)

func TestPlanningModeArtifacts_DefaultDisabled(t *testing.T) {
	opts := DefaultOptions("/tmp/ws", "/tmp/appdata", "conv-123")
	opts.PlanningMode = false

	prompt := Build(opts)
	artifacts := sectionBody(prompt, "artifacts")
	if strings.Contains(artifacts, "When in planning mode") {
		t.Errorf("planning mode text should not be present when PlanningMode=false")
	}
	if strings.Contains(artifacts, "task.md") {
		t.Errorf("task.md should not be present when PlanningMode=false")
	}
	if strings.Contains(artifacts, "implementation_plan.md") {
		t.Errorf("implementation_plan.md should not be present when PlanningMode=false")
	}
	if strings.Contains(artifacts, "walkthrough.md") {
		t.Errorf("walkthrough.md should not be present when PlanningMode=false")
	}
}

func TestPlanningModeArtifacts_Enabled(t *testing.T) {
	opts := DefaultOptions("/tmp/ws", "/tmp/appdata", "conv-123")
	opts.PlanningMode = true

	prompt := Build(opts)
	artifacts := sectionBody(prompt, "artifacts")

	wantSnippets := []string{
		"When in planning mode, you will work with three special artifacts.",
		"# Tasks\nPath: <appDataDir>/brain/<conversation-id>/task.md",
		"**Updating task.md**: Mark items as `[/]` when starting work on them, and `[x]` when completed.",
		"# Implementation Plan\nPath: <appDataDir>/brain/<conversation-id>/implementation_plan.md",
		"## User Review Required",
		"## Open Questions",
		"## Proposed Changes",
		"## Verification Plan",
		"### Automated Tests",
		"### Manual Verification",
		"# Walkthrough\nPath: <appDataDir>/brain/<conversation-id>/walkthrough.md",
	}

	for _, want := range wantSnippets {
		if !strings.Contains(artifacts, want) {
			t.Errorf("artifacts section missing expected planning mode snippet:\nwant: %s\ngot artifacts: %s", want, artifacts)
		}
	}
}

func TestPlanningModeArtifacts_CustomArtifactPath(t *testing.T) {
	opts := DefaultOptions("/tmp/ws", "/tmp/appdata", "conv-123")
	opts.PlanningMode = true
	opts.ArtifactDirectoryPath = "/custom/brain/dir"

	prompt := Build(opts)
	artifacts := sectionBody(prompt, "artifacts")

	wantSnippets := []string{
		"Path: /custom/brain/dir/task.md",
		"Path: /custom/brain/dir/implementation_plan.md",
		"Path: /custom/brain/dir/walkthrough.md",
	}

	for _, want := range wantSnippets {
		if !strings.Contains(artifacts, want) {
			t.Errorf("artifacts section missing expected custom path snippet %q", want)
		}
	}
}

func TestPlanningModeArtifacts_SectionBody(t *testing.T) {
	opts := DefaultOptions("/tmp/ws", "/tmp/appdata", "conv-123")
	opts.PlanningMode = true

	artifacts := SectionBody(opts, "artifacts")
	for _, artifactFile := range []string{"task.md", "implementation_plan.md", "walkthrough.md"} {
		if !strings.Contains(artifacts, artifactFile) {
			t.Errorf("SectionBody(opts, \"artifacts\") missing %q", artifactFile)
		}
	}
}

func TestPlanningModeArtifacts_BuildAgent(t *testing.T) {
	opts := DefaultOptions("/tmp/ws", "/tmp/appdata", "conv-123")
	opts.PlanningMode = true

	prompt := BuildAgent("# Test Agent\n\nAgent description here.", opts)
	artifacts := sectionBody(prompt, "artifacts")

	for _, artifactFile := range []string{"task.md", "implementation_plan.md", "walkthrough.md"} {
		if !strings.Contains(artifacts, artifactFile) {
			t.Errorf("BuildAgent artifacts section missing %q", artifactFile)
		}
	}
}
