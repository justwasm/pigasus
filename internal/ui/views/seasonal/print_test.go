package seasonal

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// wantSceneNames are the five scenes of the Halloween event, in the
// order the reference draws them.
//
// 【推测】 The names are the clone's own labels — the reference carries
// no scene table — so they are pinned here as the clone's contract.
var wantSceneNames = []string{"pumpkin", "moon", "ghost", "witch-hat", "bat"}

// TestHalloweenSceneNames pins the five scene names and their order.
func TestHalloweenSceneNames(t *testing.T) {
	ev := Halloween(2026)
	if ev == nil {
		t.Fatal("Halloween(2026) = nil, want the halloween event")
	}
	got := ev.ArtNames()
	if len(got) != len(wantSceneNames) {
		t.Fatalf("ArtNames() = %v, want %v", got, wantSceneNames)
	}
	for i, want := range wantSceneNames {
		if got[i] != want {
			t.Errorf("ArtNames()[%d] = %q, want %q", i, got[i], want)
		}
	}
}

// TestArtByName pins the lookup, including its case-insensitivity.
func TestArtByName(t *testing.T) {
	ev := Halloween(2026)
	for _, name := range wantSceneNames {
		art, ok := ev.ArtByName(name)
		if !ok {
			t.Errorf("ArtByName(%q) not found", name)
			continue
		}
		if art.Name != name {
			t.Errorf("ArtByName(%q).Name = %q", name, art.Name)
		}
		if len(art.Rows) == 0 {
			t.Errorf("ArtByName(%q) carries no raster", name)
		}
	}
	if _, ok := ev.ArtByName("PUMPKIN"); !ok {
		t.Error("ArtByName(\"PUMPKIN\") not found, want the case-insensitive match")
	}
	if _, ok := ev.ArtByName("bogus"); ok {
		t.Error("ArtByName(\"bogus\") found a scene, want none")
	}
}

// TestPrintScenesOneScene checks that a single scene is written as the
// bare picture — no caption, one line per art row.
func TestPrintScenesOneScene(t *testing.T) {
	ev := Halloween(2026)
	var buf bytes.Buffer
	if err := ev.PrintScenes(&buf, []string{"pumpkin"}); err != nil {
		t.Fatalf("PrintScenes failed: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("printed %d lines, want 7 (the art is 14 sub-pixel rows):\n%s", len(lines), buf.String())
	}
	if strings.Contains(buf.String(), "pumpkin") {
		t.Error("one scene must be printed without a caption")
	}
	if !strings.Contains(lines[0], "\x1b[38;2;") {
		t.Errorf("the art must carry 24-bit colour, got %q", lines[0])
	}
}

// TestPrintScenesAll checks the caption rule: several scenes are
// written with their names above them.
func TestPrintScenesAll(t *testing.T) {
	ev := Halloween(2026)
	var buf bytes.Buffer
	if err := ev.PrintScenes(&buf, nil); err != nil {
		t.Fatalf("PrintScenes failed: %v", err)
	}
	out := buf.String()
	for _, name := range wantSceneNames {
		if !strings.Contains(out, name+"\n") {
			t.Errorf("the output carries no caption for %q", name)
		}
	}
	// Five captions plus 5*7 art lines, with a blank line between the
	// scenes.
	if got := strings.Count(out, "\n"); got < 5+5*7 {
		t.Errorf("the output carries %d lines, want at least %d", got, 5+5*7)
	}
}

// TestPrintScenesUnknownName pins the error of a name that is not a
// scene: it must name the bad value and list the scenes.
func TestPrintScenesUnknownName(t *testing.T) {
	ev := Halloween(2026)
	var buf bytes.Buffer
	err := ev.PrintScenes(&buf, []string{"bogus"})
	if err == nil {
		t.Fatal("PrintScenes(\"bogus\") = nil error, want a failure")
	}
	msg := err.Error()
	if !strings.Contains(msg, "bogus") {
		t.Errorf("the error does not echo the bad name: %v", err)
	}
	for _, name := range wantSceneNames {
		if !strings.Contains(msg, name) {
			t.Errorf("the error does not list %q: %v", name, err)
		}
	}
	if buf.Len() != 0 {
		t.Errorf("a failed print must write nothing, got %q", buf.String())
	}
}

// TestHalloweenIsAvailableOutOfSeason checks that the art does not
// depend on the calendar: the *themed startup* is gated (see Current),
// the pictures are not.
func TestHalloweenIsAvailableOutOfSeason(t *testing.T) {
	if Current(time.Date(2026, time.July, 1, 12, 0, 0, 0, time.Local)) != nil {
		t.Fatal("Current() in July = an event, want nil")
	}
	ev := Halloween(2026)
	if ev == nil || len(ev.Arts) == 0 {
		t.Fatal("Halloween(2026) carries no art, want the five scenes")
	}
}
