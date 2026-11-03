package projects

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// flagSet builds a flag set that carries the two project flags.
func flagSet(t *testing.T, args ...string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool(FlagNewProject, false, "Create a new project for this session")
	fs.String(FlagProject, "", "Project ID or project name for the current CLI session")
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return fs
}

// machine is the store and the cache a scenario runs against.
type machine struct {
	home string
	cwd  string
}

// newMachine builds an empty store and an empty cache under a scratch
// directory.
func newMachine(t *testing.T) machine {
	t.Helper()
	root := t.TempDir()
	m := machine{
		home: filepath.Join(root, "home"),
		cwd:  filepath.Join(root, "cwd"),
	}
	for _, dir := range []string{m.home, m.cwd} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return m
}

// options are the machine facts a scenario hands to Apply.
func (m machine) options() Options {
	return Options{Home: m.home, Cwd: m.cwd, NewID: func() string { return "new-id" }}
}

// seed writes one project into the store: the id p1, the name
// "My Project".
func (m machine) seed() {
	dir := filepath.Join(m.home, ".gemini", "config", "projects")
	_ = os.MkdirAll(dir, 0o755)
	body := "{\n  \"id\": \"p1\",\n  \"name\": \"My Project\",\n  \"projectResources\": {\n    \"resources\": []\n  }\n}\n"
	_ = os.WriteFile(filepath.Join(dir, "p1.json"), []byte(body), 0o644)
}

// cache reads the projects cache, or nil when there is none.
func (m machine) cache(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.home, ".gemini", "antigravity-cli", "cache", "projects.json"))
	if err != nil {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("the cache is not JSON: %v", err)
	}
	return out
}

// TestApply_Default pins that a session without the flags runs in the
// default project and writes nothing: the reference creates that project
// on its first run, so the resolution does not.
func TestApply_Default(t *testing.T) {
	m := newMachine(t)
	if got := Apply(flagSet(t), m.options()); got != DefaultProjectID {
		t.Errorf("the session runs in %q, want %q", got, DefaultProjectID)
	}
	if cache := m.cache(t); cache != nil {
		t.Errorf("the cache was written without a flag: %v", cache)
	}
	if _, err := os.Stat(filepath.Join(m.home, ".gemini", "config", "projects")); err == nil {
		t.Errorf("the store was created without a flag")
	}
}

// TestApply_NilFlagSet pins that a session without a flag set keeps the
// default instead of failing.
func TestApply_NilFlagSet(t *testing.T) {
	m := newMachine(t)
	if got := Apply(nil, m.options()); got != DefaultProjectID {
		t.Errorf("the session runs in %q, want %q", got, DefaultProjectID)
	}
	if cache := m.cache(t); cache != nil {
		t.Errorf("the cache was written without a flag set: %v", cache)
	}
}

// TestApply_ResolvesTheSessionProject pins the id of the project the
// session runs in.
//
// The rule is the reference's own: a session without a project flag runs
// in the default project (official-docs/projects.md:62), --project names
// an existing project, and --new-project names the project the flag
// creates.
func TestApply_ResolvesTheSessionProject(t *testing.T) {
	for _, tc := range []struct {
		label string
		seed  bool
		args  []string
		want  string
	}{
		{label: "no flag", want: DefaultProjectID},
		{label: "the project id", seed: true, args: []string{"--project=p1"}, want: "p1"},
		{label: "the project name", seed: true, args: []string{"--project=My Project"}, want: "p1"},
		{label: "the project name in another case", seed: true, args: []string{"--project=my project"}, want: "p1"},
		{label: "a project that names none", seed: true, args: []string{"--project=nope"}, want: DefaultProjectID},
		{label: "a new project", args: []string{"--new-project"}, want: "new-id"},
		{label: "a new project, named", args: []string{"--new-project", "--project=Named"}, want: "new-id"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			m := newMachine(t)
			if tc.seed {
				m.seed()
			}
			if got := Apply(flagSet(t, tc.args...), m.options()); got != tc.want {
				t.Errorf("the session runs in %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDefaultProjectID pins the literal the reference's own store holds.
//
// 【一手】 2026-10-26: the reference's project store holds
// `default-cli-project.json` — {"id":"default-cli-project","name":"CLI
// Project"} — and the reference creates that file on its first run
// (out/blackbox-whitebox-final.md:119).
func TestDefaultProjectID(t *testing.T) {
	if DefaultProjectID != "default-cli-project" {
		t.Errorf("the default project id is %q, want default-cli-project", DefaultProjectID)
	}
}

// TestConfigPath pins the file a project lives in: the reference's own
// project directory, one JSON file per project, named after the id.
func TestConfigPath(t *testing.T) {
	m := newMachine(t)
	want := filepath.Join(m.home, ".gemini", "config", "projects", DefaultProjectID+".json")
	if got := ConfigPath(m.home, DefaultProjectID); got != want {
		t.Errorf("the project file is %q, want %q", got, want)
	}
}

// TestApply_ResolvesTheProject pins the three ways a project may be
// named: its id, its name and its name in another case.
func TestApply_ResolvesTheProject(t *testing.T) {
	for _, tc := range []struct {
		label string
		arg   string
	}{
		{label: "id", arg: "p1"},
		{label: "name", arg: "My Project"},
		{label: "name lower case", arg: "my project"},
	} {
		m := newMachine(t)
		m.seed()
		Apply(flagSet(t, "--project="+tc.arg), m.options())
		cache := m.cache(t)
		if cache == nil {
			t.Fatalf("%s: the cache was not written", tc.label)
		}
		if got := cache[m.cwd]; got != "p1" {
			t.Errorf("%s: the cache names %q, want p1", tc.label, got)
		}
	}
}

// TestApply_UnknownProject pins that a name that matches no project
// resolves to nothing and leaves the cache alone.
func TestApply_UnknownProject(t *testing.T) {
	m := newMachine(t)
	m.seed()
	Apply(flagSet(t, "--project=nope"), m.options())
	if cache := m.cache(t); cache != nil {
		t.Errorf("the cache was written for an unknown project: %v", cache)
	}
}

// TestApply_NewProject pins the bytes of the project a new session
// creates: the id, the name the workspace's directory carries and the one
// folder the workspace covers.
func TestApply_NewProject(t *testing.T) {
	m := newMachine(t)
	Apply(flagSet(t, "--new-project"), m.options())

	path := filepath.Join(m.home, ".gemini", "config", "projects", "new-id.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the new project: %v", err)
	}
	want := "{\n  \"id\": \"new-id\",\n  \"name\": \"" + filepath.Base(m.cwd) + "\",\n  \"projectResources\": {\n    \"resources\": [\n      {\n        \"folderUri\": \"file://" + m.cwd + "\"\n      }\n    ]\n  }\n}"
	if got := string(data); got != want {
		t.Errorf("the new project is\n%s\nwant\n%s", got, want)
	}
	cache := m.cache(t)
	if cache == nil {
		t.Fatalf("the cache was not written")
	}
	if got := cache[m.cwd]; got != "new-id" {
		t.Errorf("the cache names %q, want new-id", got)
	}
}

// TestApply_NewProjectNamed pins that --project names the project a new
// session creates.
func TestApply_NewProjectNamed(t *testing.T) {
	m := newMachine(t)
	Apply(flagSet(t, "--new-project", "--project=Named"), m.options())
	path := filepath.Join(m.home, ".gemini", "config", "projects", "new-id.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the new project: %v", err)
	}
	var got definition
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("the new project is not JSON: %v", err)
	}
	if got.Name != "Named" {
		t.Errorf("the new project is named %q, want Named", got.Name)
	}
}

// TestApply_CacheHitAndMiss pins that the cache names the workspace's
// project: an entry the workspace already has is replaced, and a
// workspace the cache does not know gets one.
func TestApply_CacheHitAndMiss(t *testing.T) {
	m := newMachine(t)
	m.seed()
	cacheDir := filepath.Join(m.home, ".gemini", "antigravity-cli", "cache")
	_ = os.MkdirAll(cacheDir, 0o755)
	_ = os.WriteFile(filepath.Join(cacheDir, "projects.json"),
		[]byte("{\n  \""+m.cwd+"\": \"old-id\",\n  \"/other/workspace\": \"other-id\"\n}"), 0o644)

	Apply(flagSet(t, "--project=p1"), m.options())
	cache := m.cache(t)
	if got := cache[m.cwd]; got != "p1" {
		t.Errorf("the cache names %q, want p1 (the entry is replaced)", got)
	}
	if got := cache["/other/workspace"]; got != "other-id" {
		t.Errorf("the other workspace names %q, want other-id (the entry is kept)", got)
	}
}
