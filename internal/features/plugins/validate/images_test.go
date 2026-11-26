package validate_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/justwasm/pigasus/internal/features/plugins/validate"
)

func TestCheck_ImagePathFindings(t *testing.T) {
	tests := []struct {
		name        string
		logo        string
		assets      fstest.MapFS
		severity    validate.Severity
		message     string
		wantFinding bool
	}{
		{
			name:        "absent logo is a note",
			severity:    validate.SeverityNote,
			message:     "no logo",
			wantFinding: true,
		},
		{
			name:        "blank logo warns",
			logo:        `,"logo":"  "`,
			severity:    validate.SeverityWarning,
			message:     `"logo" is blank`,
			wantFinding: true,
		},
		{
			name:        "backslashes warn",
			logo:        `,"logo":"assets\\logo.svg"`,
			severity:    validate.SeverityWarning,
			message:     "uses backslashes",
			wantFinding: true,
		},
		{
			name:        "absolute path warns",
			logo:        `,"logo":"/tmp/logo.png"`,
			severity:    validate.SeverityWarning,
			message:     "is an absolute path",
			wantFinding: true,
		},
		{
			name:        "traversal warns",
			logo:        `,"logo":"../outside.png"`,
			severity:    validate.SeverityWarning,
			message:     "is outside the plugin root",
			wantFinding: true,
		},
		{
			name:        "hidden path warns",
			logo:        `,"logo":".github/logo.png"`,
			severity:    validate.SeverityWarning,
			message:     "won't be packaged",
			wantFinding: true,
		},
		{
			name:        "wrong extension warns",
			logo:        `,"logo":"assets/logo.txt"`,
			assets:      fstest.MapFS{"assets/logo.txt": file("image bytes")},
			severity:    validate.SeverityWarning,
			message:     "is ignored and the Marketplace build rejects it",
			wantFinding: true,
		},
		{
			name:        "missing image warns",
			logo:        `,"logo":"assets/missing.png"`,
			severity:    validate.SeverityWarning,
			message:     `logo file "assets/missing.png" doesn't exist`,
			wantFinding: true,
		},
		{
			name:   "existing packaged image passes",
			logo:   `,"logo":"assets/logo.svg"`,
			assets: fstest.MapFS{"assets/logo.svg": file("svg bytes")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := fstest.MapFS{"plugin.json": file(`{"name":"image-plugin"` + tc.logo + `}`)}
			for name, entry := range tc.assets {
				files[name] = entry
			}
			got := check(t, files, "plugin.json")
			if tc.wantFinding {
				requireFinding(t, got.Report, tc.severity, "plugin.json", tc.message)
			} else {
				requireNoFinding(t, got.Report, "plugin.json", "logo")
			}
		})
	}
}

func TestCheck_LocalImageSymlinkBehavior(t *testing.T) {
	t.Run("follows an existing image symlink", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "image.svg")
		if err := os.WriteFile(outside, []byte("image"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"name":"linked-plugin","logo":"assets/logo.svg"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "assets", "logo.svg")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		got := validate.Check(validate.Source{Files: os.DirFS(root), Dir: "local-linked-plugin", Manifest: "plugin.json"})
		if !got.ManifestOK {
			t.Fatalf("ManifestOK = false: %+v", got.Report.Findings)
		}
		requireNoFinding(t, got.Report, "plugin.json", "doesn't exist")
		if errors, _ := got.Report.Counts(); errors != 0 {
			t.Errorf("symlinked image produced errors: %+v", got.Report.Findings)
		}
	})

	t.Run("broken image symlink warns", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"name":"broken-link-plugin","logo":"assets/logo.svg"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "missing.svg"), filepath.Join(root, "assets", "logo.svg")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		got := validate.Check(validate.Source{Files: os.DirFS(root), Dir: "local-broken-link-plugin", Manifest: "plugin.json"})
		requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", `logo file "assets/logo.svg" doesn't exist`)
	})
}

func TestCheck_ImageTraversalCannotResolveWithinMapFS(t *testing.T) {
	got := check(t, fstest.MapFS{
		"plugin.json": file(`{"name":"traversal-plugin","logo":"../../outside.png"}`),
		"outside.png": file("not addressable via the parent-relative path"),
	}, "plugin.json")
	requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", "is outside the plugin root")
	if got.Report.Manifest != "plugin.json" {
		t.Errorf("manifest = %q, want selected manifest retained", got.Report.Manifest)
	}
}

func TestCheck_ImageURLs(t *testing.T) {
	for _, tc := range []struct {
		name string
		logo string
		want string
		ok   bool
	}{
		{"accepted Google URL", `https://google.com/logo.png`, "", true},
		{"accepted Google subdomain", `https://foo.gstatic.com/logo.png`, "", true},
		{"other HTTPS domain warns", `https://example.com/logo.png`, "isn't shown", false},
		{"non HTTPS URL warns", `http://google.com/logo.png`, "is ignored", false},
		{"malformed URL warns", `https://exa mple.com/logo.png`, "can't parse it as a URL", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := check(t, fstest.MapFS{"plugin.json": file(`{"name":"url-plugin","logo":"` + tc.logo + `"}`)}, "plugin.json")
			if tc.ok {
				requireNoFinding(t, got.Report, "plugin.json", "logo")
			} else {
				requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", tc.want)
			}
		})
	}
}
