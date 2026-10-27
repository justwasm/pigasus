package themepreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/onboarding"
	"github.com/justwasm/pigasus/palette"
)

// The card paints its code roles in the scheme's own true colour.
//
// 【一手】 agy 1.4.8's preview card writes 24-bit escapes for the code
// roles under every scheme but the terminal one — whose roles are the
// basic sixteen-colour codes — and the colours change with the scheme.
func TestCardPaintsTheSchemesSyntaxColours(t *testing.T) {
	for _, scheme := range onboarding.AllSchemes() {
		syn := palette.SyntaxColorsFor(scheme.String())
		card := strings.Join(Render(scheme), "\n")
		roles := []struct {
			name string
			col  palette.Color
			seq  string
		}{
			{"keyword", syn.Keyword, palette.SeqWith(syn.Keyword, syn.KeywordAttr)},
			{"string", syn.String, syn.String.Seq(false)},
			{"function", syn.Function, syn.Function.Seq(false)},
			{"type", syn.Type, syn.Type.Seq(false)},
			{"variable", syn.Variable, syn.Variable.Seq(false)},
		}
		for _, r := range roles {
			if r.seq == "" {
				continue
			}
			if !strings.Contains(card, r.seq) {
				t.Errorf("%s: the card is missing the %s colour %s", scheme, r.name, r.seq)
			}
		}
	}
}

// The reference writes the colour and the attribute in one escape, and
// closes a bold run with the body colour's own escape.
//
// 【一手】 agy 1.4.8's card: the user's line is `38;2;25;103;210;1` and
// the body that follows it is `38;2;32;33;36;22` — the body colour and
// the normal intensity in one escape.
func TestCardWritesTheColourAndTheAttributeInOneEscape(t *testing.T) {
	card := strings.Join(Render(onboarding.SchemeSolarizedLight), "\n")
	if !strings.Contains(card, "\x1b[38;2;38;139;210;1m> you: ") {
		t.Error("the user's line is not the user colour and the bold in one escape")
	}
	if !strings.Contains(card, "\x1b[38;2;7;54;66;22madd a greeting function") {
		t.Error("the body after the bold is not the body colour and the normal intensity in one escape")
	}
	if !strings.Contains(card, "\x1b[38;2;38;139;210;4mfile:///path/to/main.go") {
		t.Error("the link is not the link colour and the underline in one escape")
	}
}

// The card's shape: a 63-column, 22-line box.
func TestCardShape(t *testing.T) {
	card := Render(onboarding.SchemeSolarizedLight)
	if len(card) != CardHeight {
		t.Fatalf("the card has %d lines, want %d", len(card), CardHeight)
	}
	for i, line := range card {
		if n := len([]rune(stripANSI(line))); n != CardWidth {
			t.Errorf("line %d is %d columns wide, want %d", i, n, CardWidth)
		}
	}
	if !strings.HasPrefix(card[0], "\x1b[38;2;238;232;213m╭") {
		t.Errorf("the top border is not the scheme's border colour: %q", card[0])
	}
	if !strings.HasSuffix(card[CardHeight-1], "╯\x1b[39m") {
		t.Errorf("the bottom border is malformed: %q", card[CardHeight-1])
	}
}

// stripANSI removes the SGR escapes from a line.
func stripANSI(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			j := i + 1
			if j < len(line) && line[j] == '[' {
				j++
				for j < len(line) && (line[j] == ';' || (line[j] >= '0' && line[j] <= '9')) {
					j++
				}
				if j < len(line) {
					j++
				}
			}
			i = j
			continue
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

// The card's bytes, pinned.
//
// 【一手】 agy 1.4.8's preview card under the solarized light scheme,
// captured in a true-colour terminal and compared cell by cell with the
// clone's — the two agree on every cell of the card.
func TestCardGoldenSolarizedLight(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "card_solarized_light.golden"))
	if err != nil {
		t.Fatal(err)
	}
	lines := Render(onboarding.SchemeSolarizedLight)
	got := strings.Join(lines, "\n") + "\n"
	if got != string(want) {
		t.Errorf("the card's bytes changed:\ngot:\n%q\nwant:\n%q", got, string(want))
	}
}
