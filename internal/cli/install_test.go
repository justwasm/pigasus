// Tests for the v1.3.5 `agy install` subcommand.
//
// `agy install` is informational: it prints instructions and a banner but
// never writes to disk (see install.go). These tests pin the exact
// stdout/stderr split for both branches (--dir on $PATH vs not) and assert
// that no filesystem mutation happens — the previous implementation copied
// the running binary into $HOME/.local/bin/agy, which clobbered installed
// copies during `go test`.
//
// Every test isolates $HOME (t.Setenv) so the reported install path is
// deterministic and nothing touches the real home directory.
package cli

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeInstallCtx builds a dispatchContext wired to two buffers.
func makeInstallCtx(t *testing.T, fs *flag.FlagSet) (*dispatchContext, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	errb := &bytes.Buffer{}
	return &dispatchContext{
		Out:  out,
		Err:  errb,
		Ctx:  context.Background(),
		FS:   fs,
		Args: nil,
	}, out, errb
}

// withOsArgs sets os.Args for the duration of the test.
func withOsArgs(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	os.Args = append([]string{"agy-test"}, args...)
	t.Cleanup(func() { os.Args = old })
}

// installFlagSet builds a *flag.FlagSet with the install subcommand's
// three flags.
func installFlagSet(t *testing.T, dir, skipAliases, skipPath string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("install-test", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.String("dir", dir, "install --dir")
	fs.Bool("skip-aliases", skipAliases == "true", "install --skip-aliases")
	fs.Bool("skip-path", skipPath == "true", "install --skip-path")
	return fs
}

// isolatedHome returns a temp $HOME and the canonical install path the
// reference always names, $HOME/.local/bin/agy.
func isolatedHome(t *testing.T) (home, installPath string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	return home, filepath.Join(home, ".local", "bin", "agy")
}

// ---- not-in-PATH branch ----------------------------------------------------

// TestInstall_NotInPath pins the "binary placed successfully" branch: the
// setup + warning block goes to stderr, the banner + Note to stdout.
func TestInstall_NotInPath(t *testing.T) {
	_, installPath := isolatedHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	dir := "~/.local/bin"

	d, out, errb := makeInstallCtx(t, installFlagSet(t, dir, "false", "false"))
	if err := runInstall(d); err != nil {
		t.Fatalf("runInstall: %v", err)
	}

	expDir := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	wantErr := "Running Pigasus CLI setup...\n" +
		"\nWarning: " + expDir + " is not present in your active PATH.\n" +
		"To use the 'agy' CLI globally, please manually add it to your shell profile:\n" +
		"\n" +
		"  echo 'export PATH=\"" + expDir + ":$PATH\"' >> ~/.bashrc && source ~/.bashrc\n" +
		"\n"
	if got := errb.String(); got != wantErr {
		t.Errorf("stderr mismatch\n--- got ---\n%q\n--- want ---\n%q", got, wantErr)
	}
	wantOut := "\n\u2705 Pigasus CLI binary placed successfully at " + installPath + "\n" +
		"Note: The binary is installed, but it is not in your active PATH. Please follow the instructions above to add it, then restart your terminal session.\n"
	if got := out.String(); got != wantOut {
		t.Errorf("stdout mismatch\n--- got ---\n%q\n--- want ---\n%q", got, wantOut)
	}
}

// TestInstall_DirOverride: --dir changes only the directory named in the
// PATH warning; the success banner still names $HOME/.local/bin/agy.
func TestInstall_DirOverride(t *testing.T) {
	_, installPath := isolatedHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	custom := "/tmp/agy-test-install-override"

	d, out, errb := makeInstallCtx(t, installFlagSet(t, custom, "false", "false"))
	if err := runInstall(d); err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if !strings.Contains(errb.String(), "Warning: "+custom+" is not present") {
		t.Errorf("stderr should warn about --dir %q\n--- got ---\n%s", custom, errb.String())
	}
	if !strings.Contains(errb.String(), "export PATH=\""+custom+":$PATH\"") {
		t.Errorf("stderr should echo the --dir export line\n--- got ---\n%s", errb.String())
	}
	if !strings.Contains(out.String(), "placed successfully at "+installPath) {
		t.Errorf("stdout should name the canonical install path %q, not --dir\n--- got ---\n%s", installPath, out.String())
	}
	if strings.Contains(out.String(), custom+"/agy") {
		t.Errorf("stdout must not use --dir as the install path\n--- got ---\n%s", out.String())
	}
}

// TestInstall_SkipPathStillWarns documents the (surprising) reference
// behaviour: --skip-path does not suppress the printed warning block.
func TestInstall_SkipPathStillWarns(t *testing.T) {
	isolatedHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	d, _, errb := makeInstallCtx(t, installFlagSet(t, "~/.local/bin", "false", "true"))
	if err := runInstall(d); err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if !strings.Contains(errb.String(), "is not present in your active PATH.") {
		t.Errorf("--skip-path must not suppress the warning (reference prints it)\n--- got ---\n%s", errb.String())
	}
}

// ---- in-PATH branch --------------------------------------------------------

// TestInstall_InPath pins the "installed successfully" branch.
func TestInstall_InPath(t *testing.T) {
	_, installPath := isolatedHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	d, out, errb := makeInstallCtx(t, installFlagSet(t, "/usr/bin", "false", "false"))
	if err := runInstall(d); err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	wantErr := "Running Pigasus CLI setup...\n" +
		"PATH verification: ~/.local/bin is correctly configured in active PATH environment.\n"
	if got := errb.String(); got != wantErr {
		t.Errorf("stderr mismatch\n--- got ---\n%q\n--- want ---\n%q", got, wantErr)
	}
	wantOut := "\n\u2705 Pigasus CLI installed successfully at " + installPath + "\n" +
		"Run 'agy' to start the CLI\n"
	if got := out.String(); got != wantOut {
		t.Errorf("stdout mismatch\n--- got ---\n%q\n--- want ---\n%q", got, wantOut)
	}
}

// ---- no filesystem mutation ------------------------------------------------

// TestInstall_NoFilesystemMutation guards the hazard that clobbered
// ~/.local/bin/agy during earlier `go test` runs: install must not create
// or copy anything.
func TestInstall_NoFilesystemMutation(t *testing.T) {
	home, installPath := isolatedHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	d, _, _ := makeInstallCtx(t, installFlagSet(t, "~/.local/bin", "false", "false"))
	if err := runInstall(d); err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if _, err := os.Stat(installPath); !os.IsNotExist(err) {
		t.Errorf("install must not create %s (stat err=%v)", installPath, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin")); !os.IsNotExist(err) {
		t.Errorf("install must not create %s", filepath.Join(home, ".local", "bin"))
	}
}

// ---- helpers ---------------------------------------------------------------

// TestInstall_HomeExpansion: expandHome handles "~", "~/sub" and absolute
// paths.
func TestInstall_HomeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("os.UserHomeDir unavailable: %v", err)
	}
	cases := []struct {
		in, want string
	}{
		{"~", home},
		{"~/foo/bar", filepath.Join(home, "foo", "bar")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}
	for _, c := range cases {
		if got := expandHome(c.in); got != c.want {
			t.Errorf("expandHome(%q): got %q, want %q", c.in, got, c.want)
		}
	}
}

// TestInstall_HelperDirInPath: dirInPath is true iff dir is a $PATH member.
func TestInstall_HelperDirInPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	if dirInPath("/some/random/dir") {
		t.Errorf("dirInPath(/some/random/dir) = true, want false")
	}
	t.Setenv("PATH", "/usr/bin:/bin:/custom/dir")
	if !dirInPath("/custom/dir") {
		t.Errorf("dirInPath(/custom/dir) = false, want true")
	}
	t.Setenv("PATH", "/usr/bin:/bin:/custom/dir/")
	if !dirInPath("/custom/dir") {
		t.Errorf("dirInPath(/custom/dir) = false with trailing-slash PATH, want true")
	}
	t.Setenv("PATH", "")
	if dirInPath("/anything") {
		t.Errorf("dirInPath with empty PATH = true, want false")
	}
}

// TestInstall_ScanOsArgs: scanOsArgs handles --key, --key=value, --key value.
func TestInstall_ScanOsArgs(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		key       string
		wantVal   string
		wantFound bool
	}{
		{"bare", []string{"install", "--dir"}, "dir", "", true},
		{"=value", []string{"install", "--dir=/tmp/x"}, "dir", "/tmp/x", true},
		{"space value", []string{"install", "--dir", "/tmp/x"}, "dir", "/tmp/x", true},
		{"absent", []string{"install", "other"}, "dir", "", false},
		{"bare no value (followed by another flag)", []string{"install", "--dir", "--skip-path"}, "dir", "", true},
		{"different key", []string{"install", "--other=/tmp/x"}, "dir", "", false},
		{"prefix match must equal", []string{"install", "--directory=/tmp/x"}, "dir", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withOsArgs(t, c.args...)
			got, found := scanOsArgs(c.key)
			if found != c.wantFound {
				t.Errorf("found: got %v, want %v", found, c.wantFound)
			}
			if got != c.wantVal {
				t.Errorf("value: got %q, want %q", got, c.wantVal)
			}
		})
	}
}

// TestInstall_OsArgsFallback: with FS=nil (the parser path) the flags come
// from os.Args.
func TestInstall_OsArgsFallback(t *testing.T) {
	isolatedHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	custom := "/tmp/agy-test-install-osargs"
	withOsArgs(t, "install", "--dir="+custom, "--skip-path")

	d, out, errb := makeInstallCtx(t, nil)
	if err := runInstall(d); err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if !strings.Contains(errb.String(), "Warning: "+custom+" is not present") {
		t.Errorf("os.Args fallback: --dir %q not reflected in warning\n--- stderr ---\n%s", custom, errb.String())
	}
	if !strings.Contains(out.String(), "placed successfully at") {
		t.Errorf("os.Args fallback: banner missing\n--- stdout ---\n%s", out.String())
	}
}
