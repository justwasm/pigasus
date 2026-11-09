package seasonal

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// bannerCase is one branch of the themed banner.
//
// The five cases between them cover every branch the renderer has: the
// account row with a tier and without one, the sentence addressed to the
// account's name and without a name, and the event that is not in
// season (the banner draws nothing).
type bannerCase struct {
	name  string
	email string
	model string
	tier  string
	cwd   string
	nilEv bool
}

// bannerCases are the branches the themed banner's golden pins.
var bannerCases = []bannerCase{
	{name: "tier", email: "user@example.com", model: "Gemini 3.8 Flash (High)", tier: "Google AI Pro", cwd: "~/Documents/GitHub/openagy"},
	{name: "no-tier", email: "user@example.com", model: "Gemini 3.8 Flash (High)", tier: "", cwd: "~/Documents/GitHub/openagy"},
	{name: "greeting-with-name", email: "user+work@example.com", model: "Gemini 3.8 Flash (High)", tier: "", cwd: "~/Documents/GitHub/openagy"},
	{name: "greeting-without-name", email: "", model: "Gemini 3.8 Flash (High)", tier: "", cwd: "~/Documents/GitHub/openagy"},
	{name: "nil-event", nilEv: true},
}

// bannerState is the fixture the golden renders.
//
// The product's name and the version are *fixture literals*: the golden
// pins the renderer's bytes, not the clone's version. A version bump
// does not change what the banner draws.
var bannerState = State{
	CLIName: "Pigasus CLI",
	Version: "1.4.12",
}

// renderBannerCases renders every branch of the themed banner into one
// string — the shape the golden file holds.
func renderBannerCases(t *testing.T, st State) string {
	t.Helper()
	ev := Current(time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC))
	if ev == nil {
		t.Fatal("no event in October")
	}
	var b strings.Builder
	for _, c := range bannerCases {
		b.WriteString("=== " + c.name + " ===\n")
		var buf strings.Builder
		e := ev
		if c.nilEv {
			e = nil
		}
		RenderBanner(&buf, e, State{
			Email:   c.email,
			Tier:    c.tier,
			Model:   c.model,
			Cwd:     c.cwd,
			CLIName: st.CLIName,
			Version: st.Version,
		})
		b.WriteString(buf.String())
	}
	return b.String()
}

// TestRenderBannerGolden pins the themed banner's exact bytes.
//
// The golden was captured from the host's own banner before it moved
// into this package (the host's renderSeasonalBanner), so the two trees
// render the same bytes — a two-tree byte dump compared them and found
// them identical.
//
// 【一手】 agy 1.4.7 with the Halloween event on
// (out/changelog-147/ref/config-seasonal-on.txt): the art block is 46
// cells wide, the information column starts at column 46, and the
// seasonal sentence follows the art.
func TestRenderBannerGolden(t *testing.T) {
	oldArt, oldGreeting := SetBannerPicks(2, 0)
	t.Cleanup(func() { SetBannerPicks(oldArt, oldGreeting) })
	got := renderBannerCases(t, bannerState)
	want, err := os.ReadFile(filepath.Join("testdata", "banner.golden"))
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	if !bytes.Equal([]byte(got), want) {
		t.Errorf("the themed banner's bytes changed:\n got %d bytes\nwant %d bytes\nfirst difference at %s",
			len(got), len(want), firstDifference(got, string(want)))
	}
}

// firstDifference names the first byte that differs between two
// renderings.
func firstDifference(got, want string) string {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			return "byte " + strconv.Itoa(i)
		}
	}
	return "byte " + strconv.Itoa(n)
}

// TestRenderBannerDrawsOneOfTheEventScenes pins that the themed
// banner draws one of the event's scenes, and that the scenes are not
// the same picture.
//
// 【一手】 agy 1.4.7: the themed banner is not one picture — every launch
// draws one of five scenes, each with its own colours.
func TestRenderBannerDrawsOneOfTheEventScenes(t *testing.T) {
	ev := Current(time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC))
	if ev == nil {
		t.Fatal("no event in October")
	}
	first := map[string]bool{}
	for s := range ev.Arts {
		oldArt, oldGreeting := SetBannerPicks(s, 0)
		var b strings.Builder
		RenderBanner(&b, ev, State{
			Email:   "user@example.com",
			Model:   "Gemini 3.8 Flash (High)",
			Tier:    "Google AI Pro",
			Cwd:     "~/Documents/GitHub/openagy",
			CLIName: "Pigasus CLI",
			Version: "1.4.12",
		})
		SetBannerPicks(oldArt, oldGreeting)
		lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
		if len(lines) != 8 {
			t.Fatalf("scene %d: the banner has %d lines, want 8", s, len(lines))
		}
		row := strings.TrimRight(string([]rune(strip(lines[0]))[:46]), " ")
		want := strings.TrimRight(strip(ev.Art(s).Lines()[0]), " ")
		if row != want {
			t.Errorf("scene %d: the banner's first art row is %q, want %q", s, row, want)
		}
		first[row] = true
	}
	if len(first) != len(ev.Arts) {
		t.Errorf("the banner drew %d distinct pictures, want %d", len(first), len(ev.Arts))
	}
}
