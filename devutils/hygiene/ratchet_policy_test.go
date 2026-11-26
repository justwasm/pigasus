package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestReportOnlyCeilingIsAdvisory pins the roadmap's ratchet policy: an
// un-decomposed package keeps its ceiling visible but does not block
// unrelated work, while a decomposed one is binding.
func TestReportOnlyCeilingIsAdvisory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "big/a.go", `package big
 type S struct{}
 func (S) One() {}
`)
	rep, err := analyze(options{root: root, ratchet: "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	baseline := &modularityBaseline{Version: 1, Packages: map[string]packageCeiling{
		"big": {MaxLOC: intPointer(1), ReportOnly: true, Receivers: map[string]*int{"S": intPointer(0)}},
	}}
	var stdout, stderr bytes.Buffer
	if code := printAndCheckRatchet(&stdout, &stderr, rep, baseline); code != 0 {
		t.Fatalf("report_only ceiling must not fail: exit=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "[advisory] big production LOC 3 > ceiling 1 (+2)") {
		t.Errorf("LOC advisory missing:\n%s", out)
	}
	if !strings.Contains(out, "[advisory] big.S receiver methods 1 > ceiling 0 (+1)") {
		t.Errorf("receiver advisory missing:\n%s", out)
	}
	if !strings.Contains(out, "==> Modularity ratchet passed.") {
		t.Errorf("ratchet should pass under report_only:\n%s", out)
	}
}

// TestBindingCeilingStillFails pins that removing report_only restores the
// blocking behaviour, so tightening a ceiling is a real gate.
func TestBindingCeilingStillFails(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "big/a.go", "package big\nfunc A() {}\n")
	rep, err := analyze(options{root: root, ratchet: "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	baseline := &modularityBaseline{Version: 1, Packages: map[string]packageCeiling{
		"big": {MaxLOC: intPointer(1)},
	}}
	var stdout, stderr bytes.Buffer
	if code := printAndCheckRatchet(&stdout, &stderr, rep, baseline); code != 1 {
		t.Fatalf("binding ceiling must fail: exit=%d", code)
	}
	if strings.Contains(stderr.String(), "advisory") {
		t.Errorf("binding ceiling reported as advisory: %s", stderr.String())
	}
}

// TestFileCountCeiling pins the max_files gate, so a package cannot buy
// cohesion by fanning out into ever more small files.
func TestFileCountCeiling(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "p/a.go", "package p\nfunc A() {}\n")
	writeTestFile(t, root, "p/b.go", "package p\nfunc B() {}\n")
	rep, err := analyze(options{root: root, ratchet: "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	baseline := &modularityBaseline{Version: 1, Packages: map[string]packageCeiling{
		"p": {MaxLOC: intPointer(10), MaxFiles: intPointer(1)},
	}}
	var stdout, stderr bytes.Buffer
	if code := printAndCheckRatchet(&stdout, &stderr, rep, baseline); code != 1 {
		t.Fatalf("file ceiling must fail: exit=%d stdout=%s", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "production files 2 > ceiling 1 (+1)") {
		t.Fatalf("missing file-count violation: %s", stderr.String())
	}
}

// TestReportOnlyFileCeilingAllowsFanOut keeps the advisory path symmetric.
func TestReportOnlyFileCeilingAllowsFanOut(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "p/a.go", "package p\nfunc A() {}\n")
	writeTestFile(t, root, "p/b.go", "package p\nfunc B() {}\n")
	rep, err := analyze(options{root: root, ratchet: "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	baseline := &modularityBaseline{Version: 1, Packages: map[string]packageCeiling{
		"p": {MaxLOC: intPointer(10), MaxFiles: intPointer(1), ReportOnly: true},
	}}
	var stdout, stderr bytes.Buffer
	if code := printAndCheckRatchet(&stdout, &stderr, rep, baseline); code != 0 {
		t.Fatalf("report_only file ceiling must not fail: exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "[advisory] p production files 2 > ceiling 1 (+1)") {
		t.Errorf("missing file advisory:\n%s", stdout.String())
	}
}
