package permissions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRules_MissingFileIsEmpty(t *testing.T) {
	s, err := LoadRules(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("LoadRules(missing): %v", err)
	}
	if len(s.Allow) != 0 || len(s.Deny) != 0 {
		t.Errorf("missing file yielded rules: %+v", s)
	}
}

func TestLoadRules_StringAndObjectShapes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	body := `{
	  "colorScheme": "dark",
	  "permissions": {
	    "allow": ["run_command:go test", "view_file", {"tool": "grep_search", "pattern": "src/"}],
	    "deny":  ["run_command:rm -rf"]
	  }
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadRules(path)
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	wantAllow := []Rule{
		{Tool: "run_command", Pattern: "go test"},
		{Pattern: "view_file"},
		{Tool: "grep_search", Pattern: "src/"},
	}
	if len(s.Allow) != len(wantAllow) {
		t.Fatalf("allow = %+v, want %+v", s.Allow, wantAllow)
	}
	for i := range wantAllow {
		if s.Allow[i] != wantAllow[i] {
			t.Errorf("allow[%d] = %+v, want %+v", i, s.Allow[i], wantAllow[i])
		}
	}
	if len(s.Deny) != 1 || s.Deny[0] != (Rule{Tool: "run_command", Pattern: "rm -rf"}) {
		t.Errorf("deny = %+v", s.Deny)
	}
}

// TestLoadRules_UnknownEntriesSkipped keeps a real-CLI settings.json loadable.
func TestLoadRules_UnknownEntriesSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	body := `{"permissions": {"allow": [42, null, {"unrelated": true}, "view_file"]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadRules(path)
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	if len(s.Allow) != 1 || s.Allow[0].Pattern != "view_file" {
		t.Errorf("allow = %+v, want the single string rule", s.Allow)
	}
}

func TestLoadRules_EmptyPath(t *testing.T) {
	s, err := LoadRules("")
	if err != nil || len(s.Allow) != 0 {
		t.Errorf("LoadRules(\"\") = %+v, %v", s, err)
	}
}
