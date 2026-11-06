package tui

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	browserflags "github.com/justwasm/pigasus/internal/core/cliflags/browser"
	logfileflags "github.com/justwasm/pigasus/internal/core/cliflags/logfile"
	projectflags "github.com/justwasm/pigasus/internal/core/cliflags/projects"
)

// The golden pins the exact bytes of the flags a session starts with: the
// reference's flag table for the seven flags in scope, the project
// resolution and the projects cache, the browser policy and the log file.
//
// It is the dump of the two trees, byte for byte: the host's own functions
// before the move and the core packages after it produced the same bytes.
//
// The three entry points below are the ones the dump exercises — the
// browser's policy, the project's resolution and the log file — and they
// are the host's wiring, one call per policy.

var (
	goldenApplyProject = func(fs *flag.FlagSet) {
		projectflags.Apply(fs, projectflags.Options{NewID: agyprompt.NewConversationID})
	}
	goldenApplyBrowser = func(fs *flag.FlagSet) { browserflags.Apply(fs) }
	goldenApplyLogFile = func(fs *flag.FlagSet) { logfileflags.Apply(fs) }
)

// goldenUUID matches the conversation id a new project is minted with.
var goldenUUID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// goldenStamp matches the log package's timestamp prefix.
var goldenStamp = regexp.MustCompile(`\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)

// TestSessionFlagsGolden pins the bytes of the session's flags.
func TestSessionFlagsGolden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "session_flags.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got := sessionFlagsDump(t)
	if got != string(want) {
		t.Errorf("the session's flags changed:\ngot:\n%s\nwant:\n%s", got, string(want))
	}
}

// sessionFlagsDump renders the bytes the golden pins.
func sessionFlagsDump(t *testing.T) string {
	t.Helper()
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	var b strings.Builder
	sessionDumpFlagTable(&b)
	sessionDumpProjects(t, &b)
	sessionDumpBrowser(t, &b)
	sessionDumpLogFile(t, &b)
	return b.String()
}

// sessionFlagSet builds the reference's own flag set and parses the given
// command line against it.
func sessionFlagSet(t *testing.T, args ...string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for _, spec := range cli.StandardFlags() {
		switch spec.Type {
		case "bool":
			fs.Bool(spec.Name, spec.Def == "true", spec.Usage)
		case "int":
			n := 0
			if spec.Def != "" {
				n, _ = strconv.Atoi(spec.Def)
			}
			fs.Int(spec.Name, n, spec.Usage)
		case "duration":
			fs.Duration(spec.Name, 0, spec.Usage)
		default:
			fs.String(spec.Name, spec.Def, spec.Usage)
		}
	}
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return fs
}

// sessionMask replaces the machine's own paths and the minted ids with
// fixed placeholders, so two runs on two machines produce the same bytes.
func sessionMask(s, root string) string {
	s = strings.ReplaceAll(s, root, "<ROOT>")
	return goldenUUID.ReplaceAllString(s, "<UUID>")
}

// sessionDumpFlagTable pins the reference's contract for the flags in
// scope: the name, the type, the default, the help text, the hidden flag
// and the short alias.
func sessionDumpFlagTable(w io.Writer) {
	fmt.Fprintln(w, "## flag table")
	byName := map[string]cliflags.FlagSpec{}
	for _, spec := range cli.StandardFlags() {
		byName[spec.Name] = spec
	}
	names := []string{
		"cdp_port",
		"local_chrome_headless",
		"local_chrome_user_data_dir",
		"log-file",
		"new-project",
		"project",
		"use_local_chrome",
	}
	for _, name := range names {
		spec := byName[name]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%t\t%s\n",
			spec.Name, spec.Type, spec.Def, spec.Usage, spec.Hidden, spec.Short)
	}
}

// sessionDumpProjects pins the project resolution: the default, the
// explicit values, the alias forms (the id, the name, the name in another
// case), the invalid value, a new project and the cache hit and miss.
func sessionDumpProjects(t *testing.T, w io.Writer) {
	cases := []struct {
		label string
		seed  func(home, cwd string)
		args  []string
	}{
		{label: "default"},
		{label: "explicit id", seed: sessionSeedProject, args: []string{"--project=p1"}},
		{label: "explicit name", seed: sessionSeedProject, args: []string{"--project=My Project"}},
		{label: "explicit name lower case", seed: sessionSeedProject, args: []string{"--project=my project"}},
		{label: "explicit file name", seed: sessionSeedProject, args: []string{"--project=p1.json"}},
		{label: "invalid", args: []string{"--project=nope"}},
		{label: "new project", args: []string{"--new-project"}},
		{label: "new project named", args: []string{"--new-project", "--project=Named"}},
		{label: "cache hit", seed: sessionSeedCacheHit, args: []string{"--project=p1"}},
		{label: "cache miss", seed: sessionSeedCacheMiss, args: []string{"--project=p1"}},
		{label: "cache without a project", seed: sessionSeedCacheHit, args: []string{"--project=nope"}},
	}
	for _, tc := range cases {
		sessionDumpProjectCase(t, w, tc.label, tc.seed, tc.args)
	}
}

// sessionDumpProjectCase runs one project scenario in its own root and
// dumps the store and the cache before and after the resolution.
func sessionDumpProjectCase(t *testing.T, w io.Writer, label string, seed func(home, cwd string), args []string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(root, "cwd")
	for _, dir := range []string{home, cwd} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if seed != nil {
		seed(home, cwd)
	}
	t.Setenv("HOME", home)
	t.Chdir(cwd)

	fmt.Fprintf(w, "## project: %s\n", label)
	fmt.Fprintf(w, "cache before %s\n", sessionDumpCache(home, root))
	goldenApplyProject(sessionFlagSet(t, args...))
	fmt.Fprintf(w, "cache after %s\n", sessionDumpCache(home, root))
	fmt.Fprintf(w, "store %s\n", sessionDumpStore(home, root))
}

// sessionDumpCache reads the projects cache and renders it, or "<absent>".
func sessionDumpCache(home, root string) string {
	path := filepath.Join(home, ".gemini", "antigravity-cli", "cache", "projects.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "<absent>"
	}
	return sessionMask(strings.TrimRight(string(data), "\n"), root)
}

// sessionDumpStore lists the project store and renders every file in it.
func sessionDumpStore(home, root string) string {
	dir := filepath.Join(home, ".gemini", "config", "projects")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "<absent>"
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s %s\n", sessionMask(name, root), sessionMask(strings.TrimRight(string(data), "\n"), root))
	}
	return strings.TrimRight(b.String(), "\n")
}

// sessionSeedProject writes one project into the store: the id p1, the
// name "My Project".
func sessionSeedProject(home, cwd string) {
	dir := filepath.Join(home, ".gemini", "config", "projects")
	_ = os.MkdirAll(dir, 0o755)
	body := "{\n  \"id\": \"p1\",\n  \"name\": \"My Project\",\n  \"projectResources\": {\n    \"resources\": []\n  }\n}\n"
	_ = os.WriteFile(filepath.Join(dir, "p1.json"), []byte(body), 0o644)
}

// sessionSeedCacheHit writes a project into the store and a cache that
// already names the workspace: the resolution replaces the entry.
func sessionSeedCacheHit(home, cwd string) {
	sessionSeedProject(home, cwd)
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "cache")
	_ = os.MkdirAll(dir, 0o755)
	body := "{\n  \"" + cwd + "\": \"old-id\",\n  \"/other/workspace\": \"other-id\"\n}\n"
	_ = os.WriteFile(filepath.Join(dir, "projects.json"), []byte(body), 0o644)
}

// sessionSeedCacheMiss writes a project into the store and a cache that
// does not name the workspace: the resolution adds the entry.
func sessionSeedCacheMiss(home, cwd string) {
	sessionSeedProject(home, cwd)
	dir := filepath.Join(home, ".gemini", "antigravity-cli", "cache")
	_ = os.MkdirAll(dir, 0o755)
	body := "{\n  \"/other/workspace\": \"other-id\"\n}\n"
	_ = os.WriteFile(filepath.Join(dir, "projects.json"), []byte(body), 0o644)
}

// sessionDumpBrowser pins the browser policy: the default, the explicit
// flags, the invalid values and the session without a flag set.
func sessionDumpBrowser(t *testing.T, w io.Writer) {
	cases := []struct {
		label string
		args  []string
		none  bool
	}{
		{label: "default"},
		{label: "explicit", args: []string{
			"--use_local_chrome",
			"--local_chrome_headless=false",
			"--local_chrome_user_data_dir=/tmp/pigasus-chrome-profile",
			"--cdp_port=9333",
		}},
		{label: "explicit headless", args: []string{"--local_chrome_headless=true"}},
		{label: "invalid port", args: []string{"--cdp_port=0"}},
		{label: "negative port", args: []string{"--cdp_port=-1"}},
		{label: "no flag set", none: true},
	}
	for _, tc := range cases {
		browsercdp.Configure(browsercdp.DefaultConfig())
		if tc.none {
			goldenApplyBrowser(nil)
		} else {
			goldenApplyBrowser(sessionFlagSet(t, tc.args...))
		}
		cfg := browsercdp.CurrentConfig()
		fmt.Fprintf(w, "## browser: %s\n", tc.label)
		fmt.Fprintf(w, "use_local_chrome=%v\nheadless=%v\nuser_data_dir=%q\nport=%d\n",
			cfg.UseLocalChrome, cfg.Headless, cfg.UserDataDir, cfg.Port)
	}
}

// sessionDumpLogFile pins the log file: the default, the explicit path
// and the path that cannot be opened.
func sessionDumpLogFile(t *testing.T, w io.Writer) {
	fmt.Fprintln(w, "## log file: default")
	goldenApplyLogFile(sessionFlagSet(t))
	fmt.Fprintf(w, "redirected=%v\n", log.Writer() != os.Stderr)
	log.SetOutput(os.Stderr)

	root := t.TempDir()
	path := filepath.Join(root, "cli.log")
	fmt.Fprintln(w, "## log file: explicit")
	goldenApplyLogFile(sessionFlagSet(t, "--log-file="+path))
	log.Printf("zz log line")
	redirected := log.Writer() != os.Stderr
	log.SetOutput(os.Stderr)
	data, err := os.ReadFile(path)
	fmt.Fprintf(w, "created=%v\nredirected=%v\n", err == nil, redirected)
	if err == nil {
		fmt.Fprintf(w, "content=%q\n", sessionMask(goldenStamp.ReplaceAllString(string(data), "<TS> "), root))
	}

	fmt.Fprintln(w, "## log file: invalid")
	goldenApplyLogFile(sessionFlagSet(t, "--log-file="+filepath.Join(root, "missing", "cli.log")))
	fmt.Fprintf(w, "redirected=%v\n", log.Writer() != os.Stderr)
	log.SetOutput(os.Stderr)
}
