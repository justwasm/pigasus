package skills

// The gate that keeps the *embedded* bundle and the *extracted* one in step.
//
// # Why this exists
//
// Two copies of the reference's builtin skills live in this repository:
//
//	internal/features/tools/skills/skills/   ← //go:embed, compiled into the binary
//	out/extracted-<version>/embed/assets/    ← the evidence tree, never compiled
//
// Until this test, nothing compared them. A release could change a SKILL.md,
// the extraction could pick it up, and the clone would keep shipping the old
// prose — which is not cosmetic: `automation/SKILL.md` tells the model how to
// write a `sidecar.json`, so stale text makes the clone teach a configuration
// the reference does not accept.
//
// That is exactly what happened at 1.5.18. `automation` and `run-workflow`
// had drifted from *every* release since they were first committed, while the
// other thirteen were byte-identical. `PROVENANCE.md` claimed the whole tree
// was "recovered verbatim" from 1.5.11; for those two it was not.
//
// # What is asserted
//
// The two trees must agree file for file, byte for byte, in both directions
// (a missing file and an extra file are both failures). The comparison is
// skipped — not failed — when `out/` is absent, because the snapshot
// distribution ships without the evidence tree (the same rule the
// `referencedrift` gate uses).

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// extractionRoots are the asset subtrees the embedded bundle is flattened
// from. The reference splits its bundle into `external/skills` and
// `shared/skills`; the installed layout (and therefore the embed tree) is one
// flat directory per skill, so both roots are merged here.
var extractionRoots = []string{
	filepath.Join("assets", "external", "skills"),
	filepath.Join("assets", "shared", "skills"),
}

// TestEmbeddedBundleMatchesTheExtraction is the gate.
func TestEmbeddedBundleMatchesTheExtraction(t *testing.T) {
	root, ok := findExtractedEmbed(t)
	if !ok {
		t.Skip("out/extracted-<version>/embed is absent (snapshot distribution); " +
			"the embedded-vs-extracted comparison needs the evidence tree")
	}

	embedded := map[string][]byte{}
	err := fs.WalkDir(skillFS, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(skillFS, p)
		if err != nil {
			return err
		}
		embedded[strings.TrimPrefix(p, "skills/")] = b
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded skills: %v", err)
	}

	extracted := map[string][]byte{}
	for _, rel := range extractionRoots {
		dir := filepath.Join(root, rel)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			name, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			// The two roots are flattened into one namespace; a name that
			// appears in both would silently shadow, so refuse it.
			if _, dup := extracted[name]; dup {
				return &dupError{name: name}
			}
			extracted[filepath.ToSlash(name)] = b
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	// Missing: extracted but not embedded. The clone would ship the old prose,
	// or nothing at all.
	var missing []string
	for name := range extracted {
		if _, ok := embedded[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("MISSING from the embedded bundle: skills/%s — it is in the extraction but not compiled in", name)
	}

	// Extra: embedded but not extracted. The clone would ship a file the
	// reference does not have.
	var extra []string
	for name := range embedded {
		if _, ok := extracted[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		t.Errorf("EXTRA in the embedded bundle: skills/%s — the extraction has no such file", name)
	}

	// Drift: present in both, different bytes.
	var drifted []string
	for name, want := range extracted {
		got, ok := embedded[name]
		if !ok || bytes.Equal(got, want) {
			continue
		}
		drifted = append(drifted, name)
	}
	sort.Strings(drifted)
	for _, name := range drifted {
		t.Errorf("DRIFTED: skills/%s — the embedded copy differs from the extraction.\n"+
			"  re-sync with: cp -R out/extracted-<version>/embed/assets/{external,shared}/skills/* "+
			"internal/features/tools/skills/skills/", name)
	}

	if missing != nil || extra != nil || drifted != nil {
		t.Logf("%d file(s) compared; %d missing, %d extra, %d drifted",
			len(extracted), len(missing), len(extra), len(drifted))
	}
}

// dupError reports a skill name present in both extraction roots.
type dupError struct{ name string }

func (e *dupError) Error() string {
	return "skill " + e.name + " appears under both assets/external/skills and assets/shared/skills"
}

// findExtractedEmbed locates the newest out/extracted-*/embed directory.
//
// The test lives in internal/features/tools/skills, so it walks up to the
// module root first. Picking the newest extraction rather than a hard-coded
// version means a version bump does not also require editing this test — the
// gate follows the tree the repository actually tracks.
func findExtractedEmbed(t *testing.T) (string, bool) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := ""
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			root = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}

	matches, err := filepath.Glob(filepath.Join(root, "out", "extracted-*", "embed"))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches)
	return matches[len(matches)-1], true
}
