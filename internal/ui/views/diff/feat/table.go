// Package featdiff implements the feature-diff parity table and CLI output.
//
// Source of truth: internal/cli/testdata/feature_diff_table.json.
package feat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
)

const featDiffDataFile = "internal/cli/testdata/feature_diff_table.json"

// Table is the parsed JSON shape.
type Table struct {
	Version      string   `json:"version"`
	AgyVersion   string   `json:"agy_version"`
	CloneVersion string   `json:"clone_version"`
	Notes        string   `json:"notes"`
	Categories   []string `json:"categories"`
	Features     []Entry  `json:"features"`
}

// Entry is one row in the feature-diff source JSON.
type Entry struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Agy      bool   `json:"agy"`
	Clone    bool   `json:"clone"`
	Cycle    *int   `json:"cycle"`
	Evidence string `json:"evidence"`
	Notes    string `json:"notes"`
}

// Status classifies one entry: aligned / diverged / gap.
type Status string

const (
	Aligned  Status = "aligned"
	Diverged Status = "diverged"
	Gap      Status = "gap"
)

// ClassifyStatus folds the booleans into the three-word status.
func (e Entry) ClassifyStatus() Status {
	switch {
	case e.Agy && e.Clone:
		return Aligned
	case e.Agy && !e.Clone:
		return Gap
	default:
		return Diverged
	}
}

// CycleLabel renders the cycle number as a 5-wide field, or "—" when nil.
func (e Entry) CycleLabel() string {
	if e.Cycle == nil {
		return "—"
	}
	return strconv.Itoa(*e.Cycle)
}

var (
	featDiffOnce sync.Once
	featDiffData *Table
	featDiffErr  error
	featDiffPath string
)

// SourceLoaded reports whether the JSON file was found and parsed.
func SourceLoaded() bool {
	featDiffOnce.Do(loadSource)
	return featDiffData != nil
}

// SourceData returns the parsed table or nil when not found.
func SourceData() *Table {
	featDiffOnce.Do(loadSource)
	return featDiffData
}

// SourcePath returns the absolute path the JSON was loaded from.
func SourcePath() string {
	featDiffOnce.Do(loadSource)
	return featDiffPath
}

func loadSource() {
	path := findDataPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		featDiffErr = err
		return
	}
	var t Table
	if err := json.Unmarshal(data, &t); err != nil {
		featDiffErr = err
		return
	}
	featDiffData = &t
	featDiffPath = path
}

func findDataPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root := findProjectRoot(cwd)
	if root == "" {
		return ""
	}
	candidate := filepath.Join(root, featDiffDataFile)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func findProjectRoot(dir string) string {
	cur, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// SortedEntries returns the entries sorted by category then name within category.
func SortedEntries(t *Table) []Entry {
	catOrder := map[string]int{}
	for i, c := range t.Categories {
		catOrder[c] = i
	}
	out := make([]Entry, len(t.Features))
	copy(out, t.Features)
	sort.SliceStable(out, func(i, j int) bool {
		oi, oki := catOrder[out[i].Category]
		oj, okj := catOrder[out[j].Category]
		switch {
		case oki && !okj:
			return true
		case !oki && okj:
			return false
		case oki && okj && oi != oj:
			return oi < oj
		}
		return out[i].Name < out[j].Name
	})
	return out
}
