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
