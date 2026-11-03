// scan_test.go — the workspace walk's own tests.
package codesearch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scanTree builds a self-contained workspace under t.TempDir() with the
// files the assertions below need. Returns the root path.
func scanTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(rel, body string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("a.txt", "line1\nline2 needle here\nline4\nline5\nneedle again\n")
	must("d.txt", "NEEDLE upper\n")
	must("dbl.txt", "needle needle here\n")
	must("end.txt", "aaa zwk\n")
	must("sub/c.txt", "alpha zwk beta\nneedle\n")
	// A long line whose match sits past the 216-cell content budget.
	must("long.txt", strings.Repeat("P", 300)+" zwk "+strings.Repeat("Q", 300)+"\n")
	return root
}

// TestScan exercises the walk: the file policy (skip-dirs, size cap,
// hidden entries), per-line counting, the first-match span and the
// hard result cap.
func TestScan(t *testing.T) {
	root := scanTree(t)
	re, err := CompileMatcher("needle", false)
	if err != nil {
		t.Fatal(err)
	}
	hits := Scan(root, re, MaxHits)
	if len(hits) != 5 {
		t.Fatalf("Scan needle hits: got %d, want 5: %+v", len(hits), hits)
	}

	// findHit returns the first hit whose file path matches rel, or nil.
	findHit := func(rel string) *Hit {
		for i := range hits {
			if hits[i].File == rel {
				return &hits[i]
			}
		}
		return nil
	}

	// a.txt's two matches are on lines 2 and 5 — the first one captures
	// the context window.
	a := findHit("a.txt")
	if a == nil || a.Line != 2 {
		t.Fatalf("a.txt first hit = %+v, want line=2", a)
	}
	if !a.HasPrev || a.Prev != "line1" {
		t.Errorf("a.txt prev = %+v, want line1", a)
	}
	if !a.HasNext || !strings.Contains(a.Next, "line4") {
		t.Errorf("a.txt next = %+v, want line4", a)
	}

	// d.txt holds UPPERCASE — the smart-case rule still resolves needle
	// → lowercase match since the *pattern* is all lowercase.
	if findHit("d.txt") == nil {
		t.Fatal("d.txt (uppercase) should be found under the smart-case rule")
	}

	// dbl.txt: a single "needle needle here" line must count once, with
	// the first match highlighted (ms = 0, me = 6).
	dbl := findHit("dbl.txt")
	if dbl == nil {
		t.Fatal("dbl.txt should be found")
	}
	if dbl.MS != 0 || dbl.ME != 6 {
		t.Errorf("dbl.txt first-match span = (%d,%d), want (0,6)", dbl.MS, dbl.ME)
	}

	// sub/c.txt descends into nested directories.
	if findHit("sub/c.txt") == nil {
		t.Fatal("sub/c.txt should be reached")
	}

	// A fixed-string search against an empty workspace.
	if got := Scan("", re, MaxHits); got != nil {
		t.Errorf("Scan(\"\") = %+v, want nil", got)
	}

	// The hard result cap: a file with more matches than the cap keeps
	// the list at exactly MaxHits.
	huge := filepath.Join(t.TempDir(), "huge.txt")
	body := strings.Repeat("needle\n", MaxHits+10)
	if err := os.WriteFile(huge, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	hugeHits := Scan(filepath.Dir(huge), re, MaxHits)
	if len(hugeHits) != MaxHits {
		t.Errorf("huge file: Scan returned %d hits, want cap %d",
			len(hugeHits), MaxHits)
	}
}
