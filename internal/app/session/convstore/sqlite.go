// Package convstore reads the agy reference binary's on-disk conversation
// store at ~/.gemini/antigravity-cli.
//
// Why this exists
// ---------------
// The clone keeps no conversation state of its own, so `--conversation=<id>`
// had nothing to load and `-c` had nothing to continue. The reference stores
// one SQLite database per conversation under
// ~/.gemini/antigravity-cli/conversations/<uuid>.db, plus an index in
// conversation_summaries.db. Reading those files is what makes the two
// binaries interoperable: the reference's conversations become loadable in
// the clone, and the clone's conversations become visible to the reference.
//
// Why database/sql + ncruces/go-sqlite3
// -------------------------------------
// Cycle 175: this file previously implemented a from-scratch B-tree reader
// (header validation, varint decoding, table-leaf walk, overflow chains,
// record/serial-type decoding, CREATE TABLE column parsing) so the convstore
// could remain dependency-free. That work has been deleted in favour of the
// standard library's database/sql plus
// github.com/ncruces/go-sqlite3, which is a cgo-free pure-Go SQLite built
// by translating the SQLite C source through wasm2go. The only new direct
// dependencies are ncruces/go-sqlite3 itself and its wasm payload package;
// nothing else changes in the module graph.
//
// Why a thin wrapper
// ------------------
// The package's outward API (DB, Row, Scan, Tables, Count, isUnreadableStore)
// is preserved so callers in internal/convstore/agy.go — and the test suite in
// sqlite_test.go — continue to work without change. Open validates the file's
// SQLite magic and zero-byte edge cases that the ncruces driver would
// otherwise surface late, so the same unreadable-store classification in
// agy.go keeps working.
package convstore

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const sqliteMagic = "SQLite format 3\x00"

// Open opens path for reading. It validates the SQLite header up front so the
// reference's zero-byte and non-SQLite corner cases are reported in the same
// shape the previous hand-rolled reader used to report them; the real driver
// only takes over once the magic is right.
func Open(path string) (*DB, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hdr := make([]byte, 16)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return nil, fmt.Errorf("sqlite: read header: %w", err)
	}
	if string(hdr) != sqliteMagic {
		return nil, fmt.Errorf("sqlite: %s is not a SQLite 3 database", path)
	}

	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	// Hand the file off to the driver; both fd's are read-only and we want the
	// driver to handle all subsequent reads from its own offset accounting.
	if _, err := sdb.Exec("PRAGMA query_only = 1"); err != nil {
		sdb.Close()
		return nil, err
	}
	return &DB{sdb: sdb, path: path}, nil
}

// DB is a read-only handle on a SQLite 3 database file.
type DB struct {
	sdb  *sql.DB
	path string
}

// Close releases the underlying connection pool.
func (d *DB) Close() error {
	if d == nil || d.sdb == nil {
		return nil
	}
	return d.sdb.Close()
}

// Path is the file this handle reads.
func (d *DB) Path() string { return d.path }

// hasTable reports whether the user-defined table exists. It returns false on
// a missing sqlite_master or any other schema error so callers can produce
// the same "table X not found" message the hand-rolled reader used to.
func (d *DB) hasTable(name string) (bool, error) {
	row := d.sdb.QueryRow(
		`SELECT 1 FROM sqlite_master WHERE type='table' AND name=? LIMIT 1`, name)
	var one int
	switch err := row.Scan(&one); {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// Tables lists the table names in the database.
func (d *DB) Tables() ([]string, error) {
	rows, err := d.sdb.Query(
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Count returns the number of rows in a table.
func (d *DB) Count(table string) (int, error) {
	exists, err := d.hasTable(table)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, fmt.Errorf("sqlite: table %q not found in %s", table, d.path)
	}
	var n int
	if err := d.sdb.QueryRow(`SELECT COUNT(*) FROM ` + quoteIdent(table)).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Row is one decoded table row. Column values are stored as opaque any values
// populated lazily by Int/Text/Bytes.
type Row struct {
	cols  []string
	vals  []any
	RowID int64
}

// Columns returns the table's column names in record order. The synthetic
// `rowid` column Scan prepends is excluded.
func (r *Row) Columns() []string {
	return r.cols
}

func (r *Row) index(name string) int {
	for i, c := range r.cols {
		if c == name {
			return i
		}
	}
	return -1
}

func (r *Row) value(name string) any {
	i := r.index(name)
	if i < 0 || i >= len(r.vals) {
		return nil
	}
	return r.vals[i]
}

// Int returns the named column as an integer, or 0 when absent or not numeric.
func (r *Row) Int(name string) int64 {
	switch v := r.value(name).(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case []byte:
		if n, ok := parseIntBytes(v); ok {
			return n
		}
	}
	return 0
}

// Text returns the named column as a string, or "" when absent or not text.
func (r *Row) Text(name string) string {
	switch v := r.value(name).(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	return ""
}

// Bytes returns the named column as raw bytes, or nil when absent.
func (r *Row) Bytes(name string) []byte {
	switch v := r.value(name).(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	}
	return nil
}

// Scan walks every row of a table in rowid order and calls fn for each.
//
// `rowid` is prepended as a synthetic column so callers see the SQLite rowid
// even on tables whose INTEGER PRIMARY KEY column absorbs it into the
// declared schema — the reference's `steps.idx` is exactly that shape, so
// `r.Int("idx") == r.RowID` holds as it did with the B-tree reader.
func (d *DB) Scan(table string, fn func(*Row) error) error {
	exists, err := d.hasTable(table)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("sqlite: table %q not found in %s", table, d.path)
	}
	q := `SELECT rowid, * FROM ` + quoteIdent(table) + ` ORDER BY rowid`
	rows, err := d.sdb.Query(q)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	// Strip the synthetic "rowid" out of the user-visible column list but
	// keep it as the first destination so the raw rowid is captured.
	cols = cols[1:]
	dest := make([]any, len(cols)+1)
	var rid int64
	dest[0] = &rid
	rawVals := make([]sql.RawBytes, len(cols))
	for i := range rawVals {
		dest[i+1] = &rawVals[i]
	}

	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("sqlite: %s row %d: %w", table, rid, err)
		}
		vals := make([]any, len(cols))
		for i, b := range rawVals {
			if b == nil {
				vals[i] = nil
			} else {
				vals[i] = append([]byte(nil), b...)
			}
		}
		if err := fn(&Row{cols: append([]string(nil), cols...), vals: vals, RowID: rid}); err != nil {
			return err
		}
	}
	return rows.Err()
}

func parseIntBytes(b []byte) (int64, bool) {
	if len(b) == 0 {
		return 0, false
	}
	var n int64
	off := 0
	if b[0] == '-' {
		if len(b) == 1 {
			return 0, false
		}
		off = 1
	}
	for ; off < len(b); off++ {
		c := b[off]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	if b[0] == '-' {
		n = -n
	}
	return n, true
}

// quoteIdent wraps a name in double-quotes with embedded double-quote
// escaping. table comes from sqlite_master in this module, so the value is
// already vetted to be a plain identifier, but the SQL layer requires the
// quoting for reserved words and case-sensitive names.
func quoteIdent(name string) string {
	out := make([]byte, 0, len(name)+2)
	out = append(out, '"')
	for i := 0; i < len(name); i++ {
		if name[i] == '"' {
			out = append(out, '"', '"')
		} else {
			out = append(out, name[i])
		}
	}
	out = append(out, '"')
	return string(out)
}
