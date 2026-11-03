package convstore

import (
	"os"
	"path/filepath"
	"testing"
)

func openFixture(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join("testdata", "small.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTables(t *testing.T) {
	db := openFixture(t)
	tables, err := db.Tables()
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	want := map[string]bool{"steps": true, "trajectory_meta": true}
	if len(tables) != len(want) {
		t.Fatalf("Tables() = %v, want 2 entries", tables)
	}
	for _, n := range tables {
		if !want[n] {
			t.Errorf("unexpected table %q", n)
		}
	}
}

func TestCount(t *testing.T) {
	db := openFixture(t)
	for table, want := range map[string]int{"steps": 4, "trajectory_meta": 1} {
		got, err := db.Count(table)
		if err != nil {
			t.Fatalf("Count(%s): %v", table, err)
		}
		if got != want {
			t.Errorf("Count(%s) = %d, want %d", table, got, want)
		}
	}
}

func TestScanColumnsAndValues(t *testing.T) {
	db := openFixture(t)
	var seen int
	err := db.Scan("steps", func(r *Row) error {
		seen++
		cols := r.Columns()
		if len(cols) != 6 || cols[0] != "idx" || cols[5] != "step_format" {
			t.Errorf("row %d columns = %v", r.RowID, cols)
		}
		if r.Int("idx") != r.RowID {
			// steps.idx is an INTEGER PRIMARY KEY, i.e. a rowid alias, so the
			// record body holds NULL and the value comes from the rowid.
			t.Errorf("row %d: idx = %d", r.RowID, r.Int("idx"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if seen != 4 {
		t.Errorf("scanned %d rows, want 4", seen)
	}
}

func TestScanTrajectoryMetaText(t *testing.T) {
	db := openFixture(t)
	var gotTraj, gotCasc string
	var gotType, gotSource int64
	err := db.Scan("trajectory_meta", func(r *Row) error {
		gotTraj = r.Text("trajectory_id")
		gotCasc = r.Text("cascade_id")
		gotType = r.Int("trajectory_type")
		gotSource = r.Int("source")
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if gotTraj != "traj-1" || gotCasc != "casc-1" || gotType != 4 || gotSource != 17 {
		t.Errorf("got (%q,%q,%d,%d)", gotTraj, gotCasc, gotType, gotSource)
	}
}

func TestOverflowPayload(t *testing.T) {
	// A 70000-byte blob and a 90000-byte blob both exceed the 4096-byte page,
	// so they exercise the overflow chain (single-page and multi-page).
	db := openFixture(t)
	want := map[int64]int{1: 70000, 3: 90000}
	err := db.Scan("steps", func(r *Row) error {
		n, ok := want[r.Int("idx")]
		if !ok {
			return nil
		}
		if got := len(r.Bytes("step_payload")); got != n {
			t.Errorf("idx %d: payload length %d, want %d", r.Int("idx"), got, n)
		}
		delete(want, r.Int("idx"))
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(want) != 0 {
		t.Errorf("rows not visited: %v", want)
	}
}

func TestNullBlob(t *testing.T) {
	db := openFixture(t)
	err := db.Scan("steps", func(r *Row) error {
		if r.Int("idx") == 1 || r.Int("idx") == 3 {
			if r.Bytes("metadata") != nil {
				t.Errorf("idx %d: metadata = %v, want nil", r.Int("idx"), r.Bytes("metadata"))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
}

func TestMissingTable(t *testing.T) {
	db := openFixture(t)
	if err := db.Scan("nope", func(*Row) error { return nil }); err == nil {
		t.Fatal("expected an error for a missing table")
	}
}

func TestOpenRejectsNonSQLite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "junk.db")
	if err := os.WriteFile(p, []byte("not a database at all, really"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil {
		t.Fatal("expected an error for a non-SQLite file")
	}
}

func TestOpenRejectsEmptyFile(t *testing.T) {
	// The reference accepts a zero-byte conversations/<id>.db as "exists", so
	// the reader must fail cleanly rather than panic.
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.db")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil {
		t.Fatal("expected an error for an empty file")
	}
}
