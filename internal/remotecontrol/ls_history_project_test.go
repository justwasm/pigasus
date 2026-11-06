package remotecontrol

import (
	"os"
	"path/filepath"
	"testing"

	projectflags "github.com/justwasm/pigasus/internal/core/cliflags/projects"
)

// TestLSProjectIDsPutTheDefaultProjectFirst pins the order the Web UI's
// project list carries: the default project comes first — it is the one
// the reference selects.
//
// The id is the one the project package owns, so the expectation asks
// that package; the literal itself is pinned there.
//
// The home directory is redirected: the test must not reach the user's
// own home.
func TestLSProjectIDsPutTheDefaultProjectFirst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".gemini", "config", "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	write := func(id, name string) {
		body := `{"id":"` + id + `","name":"` + name + `"}`
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
	}
	write(projectflags.DefaultProjectID, "CLI Project")
	write("aaa-workspace", "Workspace")

	d := &Dispatcher{}
	ids := d.lsProjectIDs()
	if len(ids) != 2 {
		t.Fatalf("the project list is %v, want two ids", ids)
	}
	if ids[0] != projectflags.DefaultProjectID {
		t.Errorf("the first project is %q, want %q", ids[0], projectflags.DefaultProjectID)
	}
}

func TestReadProjectAndReadProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".gemini", "config", "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	customProj := `{"id":"proj-custom","name":"Custom Project","projectResources":{"resources":[{"folderUri":"file:///tmp/custom"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "proj-custom.json"), []byte(customProj), 0o644); err != nil {
		t.Fatalf("write proj-custom: %v", err)
	}
	defaultProj := `{"id":"default-cli-project","name":"CLI Project","projectResources":{}}`
	if err := os.WriteFile(filepath.Join(dir, "default-cli-project.json"), []byte(defaultProj), 0o644); err != nil {
		t.Fatalf("write default-cli-project: %v", err)
	}

	d := &Dispatcher{
		cfg: Config{
			Session: &SessionState{
				Workspace: "/workspace/test-session",
			},
		},
	}

	// 1. ReadProject default-cli-project injects session workspace
	res, rpcErr, ok := d.handleLanguageServerReadProject(nil, []byte(`{"id":"default-cli-project"}`))
	if !ok || rpcErr != nil {
		t.Fatalf("ReadProject default-cli-project failed: ok=%v err=%v", ok, rpcErr)
	}
	m, ok := res.(map[string]any)
	if !ok || m["project"] == nil {
		t.Fatalf("expected project in result, got %+v", res)
	}
	projMap, _ := m["project"].(map[string]any)
	if projMap["id"] != "default-cli-project" {
		t.Errorf("proj ID = %v, want default-cli-project", projMap["id"])
	}
	resMap, _ := projMap["projectResources"].(map[string]any)
	resources, _ := resMap["resources"].([]any)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource in default-cli-project, got %v", resMap)
	}
	firstRes, _ := resources[0].(map[string]any)
	if firstRes["folderUri"] != "file:///workspace/test-session" {
		t.Errorf("expected folderUri file:///workspace/test-session, got %v", firstRes["folderUri"])
	}

	// 2. ReadProject outside-of-project
	res, rpcErr, ok = d.handleLanguageServerReadProject(nil, []byte(`{"id":"outside-of-project"}`))
	if !ok || rpcErr != nil {
		t.Fatalf("ReadProject outside-of-project failed: ok=%v err=%v", ok, rpcErr)
	}
	m = res.(map[string]any)
	projMap, _ = m["project"].(map[string]any)
	if projMap["id"] != "outside-of-project" {
		t.Errorf("proj ID = %v, want outside-of-project", projMap["id"])
	}

	// 3. ReadProject not found
	res, rpcErr, ok = d.handleLanguageServerReadProject(nil, []byte(`{"id":"non-existent"}`))
	if !ok || rpcErr != nil {
		t.Fatalf("ReadProject non-existent failed: ok=%v err=%v", ok, rpcErr)
	}
	m = res.(map[string]any)
	if m["notFoundOnDisk"] != true {
		t.Errorf("expected notFoundOnDisk=true, got %+v", res)
	}

	// 4. ReadProjects preserves full fields and returns notFoundIds
	res, rpcErr, ok = d.handleLanguageServerReadProjects(nil, []byte(`{"ids":["proj-custom","non-existent"]}`))
	if !ok || rpcErr != nil {
		t.Fatalf("ReadProjects failed: ok=%v err=%v", ok, rpcErr)
	}
	m = res.(map[string]any)
	projsList, _ := m["projects"].([]any)
	notFoundList, _ := m["notFoundIds"].([]string)
	if len(projsList) != 1 {
		t.Fatalf("expected 1 project in ReadProjects, got %d", len(projsList))
	}
	if len(notFoundList) != 1 || notFoundList[0] != "non-existent" {
		t.Fatalf("expected notFoundIds [non-existent], got %v", notFoundList)
	}
	customMap, _ := projsList[0].(map[string]any)
	if customMap["name"] != "Custom Project" {
		t.Errorf("custom name = %v, want Custom Project", customMap["name"])
	}
}
