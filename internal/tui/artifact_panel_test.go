// artifact_panel_test.go — the /artifact panel through the host.
//
// The panel's own rows are tested in internal/ui/views/artifactpanel. These
// tests pin the *wiring*: that the host hands the panel to the registry, that
// the picker's cursor reaches the rendered frame, and that opening the viewer
// takes the full viewport. That is the chain that was broken — the panel was
// registered as two closures over a static row list, so the cursor moved in no
// one's state and Enter opened nothing.
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/artifacts"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// artifactFixture writes n artifacts into the conversation's brain directory,
// each with the sidecar artifacts.List reads.
//
// The files are real: the panel reads them through the same os.ReadFile the
// production path uses, so a passing test is evidence about the whole chain
// rather than about a mock. The directory is the one artifacts.Dir computes, so
// the fixture cannot drift from the code under test.
func artifactFixture(t *testing.T, m *RootModel, names []string) {
	t.Helper()
	dir := artifacts.Dir(m.session.conversationID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	for _, name := range names {
		body := "# " + name + "\n\nfirst body line\nsecond body line\n"
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write artifact %s: %v", name, err)
		}
		sidecar := `{"summary":"` + name + `","updatedAt":"2026-11-26T00:00:00Z","userFacing":true}`
		if err := os.WriteFile(filepath.Join(dir, name+".md"+artifacts.MetadataSuffix), []byte(sidecar), 0o644); err != nil {
			t.Fatalf("write sidecar %s: %v", name, err)
		}
	}
}

// artifactModel builds a RootModel with /artifact open over a fresh
// conversation id and a private data directory.
//
// The data directory is per-test so the artifacts a test writes never reach the
// developer's own brain directory.
func artifactModel(t *testing.T) *RootModel {
	t.Helper()
	t.Setenv("OPENAGY_DATA_DIR", t.TempDir())
	m := mkPanel(panelregistry.Artifact, "/artifact", 0)
	m.session.conversationID = "conv-artifact-fixture"
	m.viewport.width = 100
	m.viewport.height = 30
	return m
}

// TestArtifactPickerRendersTheCursor is the end-to-end form of the defect: two
// artifacts, ↓ once, and the second row carries the cursor in the frame the
// host draws.
func TestArtifactPickerRendersTheCursor(t *testing.T) {
	m := artifactModel(t)
	artifactFixture(t, m, []string{"alpha", "beta"})

	lines := plainFrame(m)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "› alpha.md") {
		t.Fatalf("the picker should start on the first artifact:\n%s", joined)
	}

	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down should be handled by the panel")
	}
	joined = strings.Join(plainFrame(m), "\n")
	if !strings.Contains(joined, "› beta.md") {
		t.Errorf("the cursor should be on the second artifact:\n%s", joined)
	}
	if strings.Contains(joined, "› alpha.md") {
		t.Errorf("the first row should no longer carry the cursor:\n%s", joined)
	}
}

// TestArtifactEnterOpensTheFullViewportViewer pins the second layer through the
// host: Enter opens the viewer, the frame becomes the file's numbered lines, and
// the panel takes the full viewport.
func TestArtifactEnterOpensTheFullViewportViewer(t *testing.T) {
	m := artifactModel(t)
	artifactFixture(t, m, []string{"alpha"})

	if m.isFullViewportPanel() {
		t.Fatal("the picker is not a full-viewport panel")
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter should be handled by the panel")
	}
	if !m.overlays.panel.art.Viewer() {
		t.Fatal("enter should open the viewer")
	}
	if !m.isFullViewportPanel() {
		t.Error("the open viewer should take the full viewport")
	}

	joined := strings.Join(plainFrame(m), "\n")
	if !strings.Contains(joined, "alpha") {
		t.Errorf("the viewer's title should be the artifact's name:\n%s", joined)
	}
	if !strings.Contains(joined, "first body line") {
		t.Errorf("the viewer should show the file's text:\n%s", joined)
	}
	if !strings.Contains(joined, "[0%  L1  1-4/4]") {
		t.Errorf("the viewer's counter is missing:\n%s", joined)
	}
}

// TestArtifactEscGoesBackThenOut keeps Esc's two meanings distinct through the
// host: viewer → list → closed.
func TestArtifactEscGoesBackThenOut(t *testing.T) {
	m := artifactModel(t)
	artifactFixture(t, m, []string{"alpha"})

	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.overlays.panel.art.Viewer() {
		t.Fatal("the viewer should be open")
	}

	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.overlays.panel.art.Viewer() {
		t.Error("esc should leave the viewer")
	}
	if m.overlays.panel.kind != panelregistry.Artifact {
		t.Error("esc from the viewer must not close the panel")
	}

	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.overlays.panel.kind != panelregistry.None {
		t.Error("esc from the list should close the panel")
	}
}

// plainFrame strips the SGR spans from the panel's frame.
//
// It is plainLines (slash_panels_test.go) under the name this file reads
// better with.
func plainFrame(m *RootModel) []string { return plainLines(m) }

// TestArtifactViewerCursorIsVimDriven pins the feature the reference's viewer
// has and the clone did not: the gutter's `>` is a **line cursor** driven by
// vim keys, not a static marker and not the scroll offset.
//
// 【一手】 the reference's capture after `j j j` shows the cursor on line 4
// with the window still at `1-28`, and after `G` the cursor on the last line
// with the window at `75-102`. See docs/guide/compaction.md.
func TestArtifactViewerCursorIsVimDriven(t *testing.T) {
	m := artifactModel(t)
	var b strings.Builder
	for i := 1; i <= 102; i++ {
		b.WriteString("row line\n")
	}
	dir := artifacts.Dir(m.session.conversationID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "long.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sidecar := `{"summary":"long","updatedAt":"2026-11-26T00:00:00Z","userFacing":true}`
	if err := os.WriteFile(filepath.Join(dir, "long.md"+artifacts.MetadataSuffix), []byte(sidecar), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.overlays.panel.art.Viewer() {
		t.Fatal("enter should open the viewer")
	}

	// At the top: the cursor is on line 1 and the window starts at 1.
	top := strings.Join(plainFrame(m), "\n")
	if !strings.Contains(top, ">   1") {
		t.Fatalf("the cursor should start on line 1:\n%s", top)
	}
	if !strings.Contains(top, "L1") {
		t.Fatalf("the counter should report L1:\n%s", top)
	}

	// j j j moves the cursor to line 4 and leaves the window alone.
	for i := 0; i < 3; i++ {
		m.handlePanelKey(tea.Key{Code: 'j'})
	}
	moved := strings.Join(plainFrame(m), "\n")
	if !strings.Contains(moved, ">   4") {
		t.Errorf("j j j should put the cursor on line 4:\n%s", moved)
	}
	if !strings.Contains(moved, "L4") {
		t.Errorf("the counter should report L4:\n%s", moved)
	}
	if !strings.Contains(moved, "1-") {
		t.Errorf("j j j must not scroll the window:\n%s", moved)
	}

	// G jumps to the last line and the window follows it there.
	m.handlePanelKey(tea.Key{Code: 'G'})
	bottom := strings.Join(plainFrame(m), "\n")
	if !strings.Contains(bottom, "L102") {
		t.Errorf("G should put the cursor on the last line:\n%s", bottom)
	}
	if !strings.Contains(bottom, "100%") {
		t.Errorf("G should scroll the window to the end:\n%s", bottom)
	}
	if !strings.Contains(bottom, "/102]") {
		t.Errorf("the counter should count 102 lines:\n%s", bottom)
	}
}
