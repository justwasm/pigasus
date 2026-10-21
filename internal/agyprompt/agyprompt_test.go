package agyprompt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The capture is a real agy 1.4.4 request body recorded by the cycle-274
// wrapper. Reproducing it byte for byte is the whole point of this package:
// every static section, the list rendering, and the user_information layout
// are pinned by the reference rather than by our reading of it.
func TestBuildMatchesCapture(t *testing.T) {
	raw, err := os.ReadFile("testdata/capture_1.4.4.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		OSVersion      string     `json:"os_version"`
		Workspaces     []string   `json:"workspaces"`
		CWD            string     `json:"cwd"`
		AppDataDir     string     `json:"app_data_dir"`
		ConversationID string     `json:"conversation_id"`
		ArtifactDir    string     `json:"artifact_dir"`
		Subagents      []Subagent `json:"subagents"`
		Skills         []Skill    `json:"skills"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/capture_1.4.4.txt")
	if err != nil {
		t.Fatal(err)
	}

	got := Build(Options{
		OSVersion:      fx.OSVersion,
		Workspaces:     fx.Workspaces,
		CWD:            fx.CWD,
		AppDataDir:     fx.AppDataDir,
		ConversationID: fx.ConversationID,
		ArtifactDir:    fx.ArtifactDir,
		Subagents:      fx.Subagents,
		Skills:         fx.Skills,
	})
	if got != string(want) {
		// Report the first divergence rather than dumping 34 KB.
		i := 0
		for i < len(got) && i < len(want) && got[i] == want[i] {
			i++
		}
		lo := max(0, i-80)
		t.Fatalf("prompt differs at byte %d (got %d bytes, want %d)\ngot:  %q\nwant: %q",
			i, len(got), len(want), got[lo:min(len(got), i+80)], string(want)[lo:min(len(want), i+80)])
	}
}

func TestSectionOrderAndFraming(t *testing.T) {
	got := Build(DefaultOptions("/tmp/ws", "/tmp/agent", "00000000-0000-0000-0000-000000000000"))
	re := regexp.MustCompile(`^<([a-z_]+)>`)
	var names []string
	for _, line := range strings.Split(got, "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			names = append(names, m[1])
		}
	}
	want := []string{"identity", "artifacts", "conversation_transcript", "guidelines",
		"communication_style", "slash_commands", "messaging", "subagents", "skills", "user_information"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("sections = %v, want %v", names, want)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("prompt ends with a newline; the capture does not")
	}
}

func TestBuiltinSubagentsMatchCapture(t *testing.T) {
	want, err := os.ReadFile("testdata/capture_1.4.4.txt")
	if err != nil {
		t.Fatal(err)
	}
	body := sectionBody(string(want), "subagents")
	got := renderSubagents(BuiltinSubagents())
	if got != body {
		t.Fatalf("builtin subagents differ from the capture\n got: %q\nwant: %q", got, body)
	}
}

func TestArtifactDirDefaultsUnderAppData(t *testing.T) {
	got := Build(Options{
		OSVersion:      "mac",
		Workspaces:     []string{"/w"},
		CWD:            "/w",
		AppDataDir:     "/home/u/.gemini/antigravity-cli",
		ConversationID: "abc",
	})
	want := "Artifact Directory Path: /home/u/.gemini/antigravity-cli/brain/abc\n"
	if !strings.Contains(got, want) {
		t.Fatalf("prompt does not contain %q", want)
	}
}

func TestDiscoverSkills(t *testing.T) {
	root := t.TempDir()
	write := func(dir, body string) {
		p := filepath.Join(root, dir)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("alpha", "---\nname: alpha\ndescription: Plain one-liner.\n---\nbody\n")
	write("beta", "---\nname: beta\ndescription: >-\n  Folded across\n  two lines.\n---\nbody\n")
	write("gamma", "---\nname: gamma\ndescription: |\n  Kept on\n  two lines.\n---\nbody\n")
	write("delta", "---\nname: delta\ndescription: \"Quoted: value\"\n---\nbody\n")
	write("epsilon", "no frontmatter here\n")
	write("zeta", "---\ndescription: No name key.\n---\nbody\n")

	got := DiscoverSkills([]string{root})
	byName := map[string]Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if _, ok := byName["epsilon"]; ok {
		t.Fatal("a SKILL.md without frontmatter was listed")
	}
	if byName["alpha"].Description != "Plain one-liner." {
		t.Fatalf("alpha description = %q", byName["alpha"].Description)
	}
	if byName["beta"].Description != "Folded across two lines." {
		t.Fatalf("beta description = %q", byName["beta"].Description)
	}
	if byName["gamma"].Description != "Kept on\ntwo lines." {
		t.Fatalf("gamma description = %q", byName["gamma"].Description)
	}
	if byName["delta"].Description != "Quoted: value" {
		t.Fatalf("delta description = %q", byName["delta"].Description)
	}
	if z, ok := byName["zeta"]; !ok || z.Name != "zeta" {
		t.Fatalf("a nameless frontmatter should fall back to the directory name, got %+v", z)
	}
	if byName["alpha"].Path != filepath.Join(root, "alpha", "SKILL.md") {
		t.Fatalf("alpha path = %q", byName["alpha"].Path)
	}
}

// sectionBody returns the body between <name> and </name>, including the
// leading newline, exactly as Build renders it.
func sectionBody(prompt, name string) string {
	open := "<" + name + ">"
	close := "</" + name + ">"
	i := strings.Index(prompt, open)
	if i < 0 {
		return ""
	}
	rest := prompt[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
