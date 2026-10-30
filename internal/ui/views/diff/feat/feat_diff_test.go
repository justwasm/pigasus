package feat_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/featdiff"
)

func mustGetwd(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return cwd
}

func TestFeatDiff_LoadsJSONAndEmitsAligned(t *testing.T) {
	if !featdiff.SourceLoaded() {
		t.Skipf("feature_diff_table.json not reachable from cwd %q — skipping", mustGetwd(t))
	}
	path := featdiff.SourcePath()
	if !strings.HasSuffix(path, "internal/cli/testdata/feature_diff_table.json") {
		t.Errorf("source path = %q, want suffix internal/cli/testdata/feature_diff_table.json", path)
	}
	tbl := featdiff.SourceData()
	if tbl == nil {
		t.Fatal("SourceData() = nil despite SourceLoaded() = true")
	}
	if got := len(tbl.Features); got < 30 {
		t.Errorf("feature count = %d, want ≥ 30", got)
	}
	for i, e := range tbl.Features {
		if e.Name == "" {
			t.Errorf("entry %d missing name", i)
		}
		if e.Category == "" {
			t.Errorf("entry %d (%q) missing category", i, e.Name)
		}
		if !e.Agy && !e.Clone {
			t.Errorf("entry %d (%q) has both agy=false and clone=false", i, e.Name)
		}
	}

	buf := &bytes.Buffer{}
	featdiff.RunCLI(buf, "text")
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("renderer emitted %d lines, want ≥ 3 (header + rule + ≥ 1 row)", len(lines))
	}
	if !strings.HasPrefix(lines[0], "Feature") {
		t.Errorf("first line = %q, want prefix %q", lines[0], "Feature")
	}
	if !strings.Contains(lines[0], "AGY") || !strings.Contains(lines[0], "Clone") ||
		!strings.Contains(lines[0], "Cycle") || !strings.Contains(lines[0], "Status") {
		t.Errorf("header line missing one of AGY / Clone / Cycle / Status: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "──") {
		t.Errorf("second line = %q, want ── rule", lines[1])
	}
}

func TestFeatDiff_ClassifyStatus(t *testing.T) {
	c1, c2 := 1, 2

	aligned := featdiff.Entry{Name: "Synth aligned", Category: "agent-loop", Agy: true, Clone: true, Cycle: &c1}
	gap := featdiff.Entry{Name: "Synth gap", Category: "agent-loop", Agy: true, Clone: false, Cycle: &c2}
	diverged := featdiff.Entry{Name: "Synth diverged", Category: "agent-loop", Agy: false, Clone: true, Cycle: &c1}

	if got, want := aligned.ClassifyStatus(), featdiff.Aligned; got != want {
		t.Errorf("aligned.ClassifyStatus() = %q, want %q", got, want)
	}
	if got, want := gap.ClassifyStatus(), featdiff.Gap; got != want {
		t.Errorf("gap.ClassifyStatus() = %q, want %q", got, want)
	}
	if got, want := diverged.ClassifyStatus(), featdiff.Diverged; got != want {
		t.Errorf("diverged.ClassifyStatus() = %q, want %q", got, want)
	}

	if got, want := (featdiff.Entry{Cycle: nil}).CycleLabel(), "—"; got != want {
		t.Errorf("nil cycle label = %q, want %q", got, want)
	}
	if got, want := (featdiff.Entry{Cycle: &c1}).CycleLabel(), "1"; got != want {
		t.Errorf("cycle=1 label = %q, want %q", got, want)
	}

	tbl := &featdiff.Table{
		Version:      "test",
		AgyVersion:   "9.9.9",
		CloneVersion: "0.0.0-test",
		Categories:   []string{"agent-loop"},
		Features:     []featdiff.Entry{aligned, gap, diverged},
	}
	buf := &bytes.Buffer{}
	featdiff.WriteTextTable(buf, tbl)
	out := buf.String()
	for _, want := range []string{"aligned", "gap", "diverged"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderer missing status %q in output:\n%s", want, out)
		}
	}
}

func TestFeatDiff_JSONRoundTrip(t *testing.T) {
	if !featdiff.SourceLoaded() {
		t.Skipf("feature_diff_table.json not reachable from cwd %q — skipping", mustGetwd(t))
	}
	buf := &bytes.Buffer{}
	featdiff.RunCLI(buf, "json")

	var got featdiff.Table
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("--output-format json produced invalid JSON: %v\noutput: %q", err, buf.String())
	}
	src := featdiff.SourceData()
	if src == nil {
		t.Fatal("src data is nil despite loaded")
	}
	if got.Version != src.Version {
		t.Errorf("round-trip version = %q, want %q", got.Version, src.Version)
	}
	if got.AgyVersion != src.AgyVersion {
		t.Errorf("round-trip agy_version = %q, want %q", got.AgyVersion, src.AgyVersion)
	}
	if got.CloneVersion != src.CloneVersion {
		t.Errorf("round-trip clone_version = %q, want %q", got.CloneVersion, src.CloneVersion)
	}
	if len(got.Categories) != len(src.Categories) {
		t.Errorf("round-trip categories length = %d, want %d", len(got.Categories), len(src.Categories))
	}
	if len(got.Features) != len(src.Features) {
		t.Errorf("round-trip features length = %d, want %d", len(got.Features), len(src.Features))
	}
	if len(got.Features) > 0 && len(src.Features) > 0 {
		if got.Features[0].Name != src.Features[0].Name {
			t.Errorf("round-trip features[0].name = %q, want %q", got.Features[0].Name, src.Features[0].Name)
		}
		if got.Features[0].Agy != src.Features[0].Agy {
			t.Errorf("round-trip features[0].agy = %v, want %v", got.Features[0].Agy, src.Features[0].Agy)
		}
		if got.Features[0].Clone != src.Features[0].Clone {
			t.Errorf("round-trip features[0].clone = %v, want %v", got.Features[0].Clone, src.Features[0].Clone)
		}
	}
}
