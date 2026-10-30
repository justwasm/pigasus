// Package agentsdir — manifest loader.
//
// AGY 1.2.16 changelog line 126 (parent `.agents/` directory being
// ignored when a session started in a subdirectory) describes not just
// directory discovery but the *merging* of customization manifests —
// `skills.json`, `rules.json`, and the like — across every .agents/
// directory the walker surfaces. The walker in agentsdir.go handles
// directory discovery; this file handles the file-load + merge step.
//
// Two helpers:
//
//   - LoadManifest reads a single named file from a single agents
//     directory, returning the raw bytes (or nil when absent).
//   - LoadAllManifests walks the parent-directory chain and returns the
//     bytes for every level that contains the named file, in CWD-closest
//     order. Callers merge the bytes themselves — manifest shapes vary,
//     and the merge rule belongs to the consumer.
//
// The merge below is deliberately minimal: an `agents.json`-shaped
// document with an `agents` key is the canonical case the cycle ships
// (see cmd/wagyu/plugin_agents.go for the matching schema). Future
// cycles can extend this package without changing its public API.
package agentsdir

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Manifest is one customization file found at one agents directory.
// The raw bytes are preserved so callers that expect non-JSON content
// (e.g. a Markdown customizations page) can read it unchanged.
type Manifest struct {
	Dir  string // absolute path of the .agents directory
	Name string // basename of the file inside the agents directory
	Path string // absolute path of the file (Dir + Name)
	Data []byte // raw file bytes
}

// LoadManifest reads dir/name and returns the manifest. A missing file
// yields (Manifest{}, false, nil) so callers can treat "absent" as
// "skip" without an extra error check.
//
// dir must be an absolute path or be resolvable via the working
// directory of the caller; the caller (Walk above) is responsible for
// absolute-path enforcement upstream.
func LoadManifest(dir, name string) (Manifest, bool, error) {
	if dir == "" || name == "" {
		return Manifest{}, false, errors.New("agentsdir: empty dir or name")
	}
	if filepath.Base(name) != name {
		return Manifest{}, false, errors.New("agentsdir: name must be a basename, no path separators")
	}
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, false, nil
		}
		return Manifest{}, false, err
	}
	return Manifest{Dir: dir, Name: name, Path: path, Data: data}, true, nil
}

// LoadAllManifests walks from startDir up to stopDir, calling Walk for
// the agents-directory chain and loading `name` from each hit. The
// returned slice is in CWD-closest order so a caller-merging by
// JSON-merge sees the project-local entry win.
//
// `name` is the basename of the file to load from each .agents/
// directory (e.g. "skills.json", "rules.json"). File contents that fail
// to read for any reason other than absence are returned as errors in a
// parallel slice; the missing-file case is filtered out.
func LoadAllManifests(startDir, stopDir, name string, dirNames []string) ([]Manifest, []error) {
	entries := Walk(startDir, stopDir, dirNames)
	if len(entries) == 0 {
		return nil, nil
	}
	out := make([]Manifest, 0, len(entries))
	var errs []error
	for _, e := range entries {
		m, ok, err := LoadManifest(e.Path, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out, errs
}

// MergeManifestBytes is the trivial byte-concatenation merge: when two
// .agents directories each carry the same manifest, the project-local
// bytes are kept verbatim and a second copy from a deeper parent is
// appended with a separator. Useful for test fixtures that load two
// `skills.json` files and want a single parseable stream.
//
// Callers that need structured JSON merging should parse `Data` per
// Manifest in LoadAllManifests and merge their structures themselves —
// the byte-concat path is a convenience, not a correctness claim.
func MergeManifestBytes(ms []Manifest) []byte {
	if len(ms) == 0 {
		return nil
	}
	if len(ms) == 1 {
		return ms[0].Data
	}
	var total int
	for _, m := range ms {
		total += len(m.Data) + 1 // newline separator
	}
	out := make([]byte, 0, total)
	for i, m := range ms {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, m.Data...)
	}
	return out
}

// AgentsJSONEntry mirrors the schema cmd/wagyu/plugin_agents.go uses
// for its plugin-side `agents.json`. The schema is shared so a future
// cross-walk loader (project-local AND plugin) reads both files with
// the same struct without copying constants.
type AgentsJSONEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// AgentsJSON is the doc shape; an array of entries at the root is the
// pre-1.3.3 fallback.
type AgentsJSON struct {
	Agents []AgentsJSONEntry `json:"agents"`
}

// ParseAgentsJSON returns the entries from raw JSON `data`. Both the
// nested (`{"agents": [...]}`) and flat (`[...]`) forms are accepted.
// Malformed bytes yield (nil, error) — the caller is expected to
// surface the error and continue with whatever the markdown-side
// enumerator returned.
func ParseAgentsJSON(data []byte) ([]AgentsJSONEntry, error) {
	if len(data) == 0 {
		return nil, errors.New("agentsdir: empty agents.json body")
	}
	var doc AgentsJSON
	if err := json.Unmarshal(data, &doc); err == nil && doc.Agents != nil {
		return doc.Agents, nil
	} else if err != nil {
		// Try the flat-array fallback. json.Unmarshal returns the
		// first parse error, so the second attempt has to start with
		// a fresh state.
		var flat []AgentsJSONEntry
		if err2 := json.Unmarshal(data, &flat); err2 == nil {
			return flat, nil
		} else {
			return nil, err
		}
	}
	// The "no error but also no agents key" branch: the file parsed as
	// `{}` or some other object without an `agents` field. Try flat
	// anyway for the pre-1.3.3 shape.
	var flat []AgentsJSONEntry
	if err := json.Unmarshal(data, &flat); err == nil && flat != nil {
		return flat, nil
	}
	return nil, nil
}
