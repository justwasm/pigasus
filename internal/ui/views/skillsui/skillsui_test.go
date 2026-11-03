package skillsui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillContentHashAndDedup(t *testing.T) {
	tmp := t.TempDir()
	f1 := filepath.Join(tmp, "skill1.md")
	f2 := filepath.Join(tmp, "skill2.md")
	f3 := filepath.Join(tmp, "skill3.md")

	content1 := "---\nname: skill-one\n---\nHello world\n"
	content2 := "---\nname: skill-two\n---\nDifferent content\n"

	if err := os.WriteFile(f1, []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f3, []byte(content2), 0644); err != nil {
		t.Fatal(err)
	}

	h1 := SkillContentHash(f1)
	h2 := SkillContentHash(f2)
	h3 := SkillContentHash(f3)

	if h1 == "" || h1 != h2 {
		t.Fatalf("expected h1 == h2, got %q vs %q", h1, h2)
	}
	if h1 == h3 {
		t.Fatalf("expected h1 != h3, got %q", h1)
	}

	skills := []SkillInfo{
		{Name: "skill-one", Path: f1},
		{Name: "skill-dup", Path: f2},
		{Name: "skill-two", Path: f3},
	}
	deduped := DedupeSkillInfosByHash(skills)
	if len(deduped) != 2 {
		t.Fatalf("expected 2 deduped skills, got %d", len(deduped))
	}
	if deduped[0].Name != "skill-one" || deduped[1].Name != "skill-two" {
		t.Fatalf("unexpected dedup order or contents: %+v", deduped)
	}
}

func TestVerbatimSkillArgs(t *testing.T) {
	args := "@path/to/file arg2"
	if got := VerbatimSkillArgs(args); got != args {
		t.Fatalf("expected %q, got %q", args, got)
	}
}

func TestPanelRow(t *testing.T) {
	s := SkillInfo{Name: "my-skill", Description: "a test skill", Builtin: true}
	row := PanelRow(s, true, 80)
	if !strings.Contains(row, "my-skill") || !strings.Contains(row, "built-in") || !strings.Contains(row, "› ") {
		t.Fatalf("unexpected panel row: %q", row)
	}
}

func TestSkillsDetailVisibleAndCounter(t *testing.T) {
	if vis := SkillsDetailVisible(50); vis != 26 {
		t.Fatalf("unexpected vis for 50: %d", vis)
	}
	if vis := SkillsDetailVisible(12); vis != 5 {
		t.Fatalf("unexpected floor vis for 12: %d", vis)
	}
	if vis := SkillsDetailVisible(0); vis != 11 {
		t.Fatalf("unexpected vis for 0: %d", vis)
	}
	counter := SkillsDetailCounter(0, 10, 25)
	if counter != "(1–10 of 25 lines)" {
		t.Fatalf("unexpected counter: %q", counter)
	}
}

func TestTableAndFootnotes(t *testing.T) {
	lines := []string{
		"| Col 1 | Col 2 |",
		"| ----- | ----- |",
		"| [A](https://example.com) | B |",
	}
	fns := NewMDTableFootnotes(80)
	tbl := MDRenderTable(0, len(lines), lines, fns)
	if len(tbl) == 0 {
		t.Fatal("expected rendered table rows")
	}
	if len(fns.Order) != 1 || fns.Order[0].URL != "https://example.com" {
		t.Fatalf("expected 1 footnote with URL, got %+v", fns.Order)
	}
}
