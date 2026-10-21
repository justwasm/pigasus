// Probe dump helper for cycle 218. This test is build-tagged so it does
// not run as part of the normal suite; it's invoked directly to capture
// the post-cycle-218 clone's rendered SKILL.md viewer output to text
// files. Run with: go test -vet=off -tags probe218 -run TestProbe218
// -v ./cmd/wagyu/.
//go:build probe218
// +build probe218

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProbe218_DumpAll captures the cycle-218 clone's rendered viewer
// output for each of the five host SKILL.md files. The output is plain
// (SGR-stripped) so it can be compared byte-for-byte against REF captures
// when those become available again.
func TestProbe218_DumpAll(t *testing.T) {
	out := os.TempDir() // files live in $TMPDIR/probe218-*.txt
	targets := []struct {
		slug string
		path string
	}{
		{"agents-sdk", "/Users/gear/.gemini/skills/agents-sdk/SKILL.md"},
		{"agy-customizations", "/Users/gear/.gemini/antigravity-cli/builtin/skills/agy-customizations/SKILL.md"},
		{"antigravity-guide", "/Users/gear/.gemini/antigravity-cli/builtin/skills/antigravity_guide/SKILL.md"},
		{"a11y-debugging", "/Users/gear/.gemini/config/plugins/chrome-devtools-plugin/skills/a11y-debugging/SKILL.md"},
		{"uv", "/Users/gear/.gemini/config/plugins/science/skills/uv/SKILL.md"},
	}
	for _, tgt := range targets {
		tgt := tgt
		t.Run(tgt.slug, func(t *testing.T) {
			data, err := os.ReadFile(tgt.path)
			if err != nil {
				t.Skipf("SKILL.md not on this host (%v)", err)
			}
			body := string(data)
			if strings.HasPrefix(body, "---\n") || strings.HasPrefix(body, "---\r\n") {
				rest := body[strings.IndexByte(body, '\n')+1:]
				if idx := strings.Index(rest, "\n---"); idx >= 0 {
					body = strings.TrimPrefix(rest[idx+4:], "\r\n")
				}
			}
			p := writeSkillMD(t, body)
			m := detailRunner(t, 50, 220, p, tgt.slug, "d")
			doc := m.skillsDetailDocument(jsonSkillInfo{
				Name: tgt.slug, Description: "d", Path: p,
			})
			var b strings.Builder
			// Catalogue header (title / description / path / blank) — keep
			// it so the dump matches a /skills Enter sequence.
			b.WriteString("# catalogue header\n")
			for _, l := range doc[:4] {
				for _, r := range l.rows {
					b.WriteString(stripSGR(r) + "\n")
				}
			}
			b.WriteString("# body (" + tgt.slug + ")\n")
			for _, l := range doc[4:] {
				for _, r := range l.rows {
					b.WriteString(stripSGR(r) + "\n")
				}
			}
			fp := filepath.Join(out, "probe218-"+tgt.slug+".txt")
			if err := os.WriteFile(fp, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s (%d bytes, %d logical lines)", fp, b.Len(), len(doc))
		})
	}
}
