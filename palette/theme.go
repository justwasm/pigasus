// theme.go — the live colour scheme, its code-highlighting palette,
// and the SGR form of a scheme colour.
//
// The clone's palette used to be a frozen table of true-colour escapes
// recovered from one capture of the reference.  The reference itself
// resolves its colours from the *selected* scheme: every slot of the
// scheme table feeds the styles its renderer paints with, so picking a
// scheme in /config repaints the whole interface.  This file makes the
// clone do the same — SetScheme recomputes the package's colours, and
// the packages that alias them refresh their aliases.
package palette

import (
	"strconv"
	"strings"
)

// Syntax is a colour scheme's code-highlighting palette: the roles the
// reference paints a Go token with.
//
// 【一手】 recovered from agy 1.4.8's /config → Color Scheme preview
// card.  The card's code block paints `import`/`func` in the keyword
// colour, `"fmt"` in the string colour, `main`/`greet`/`Printf` in the
// function colour, `string` in the type colour and `name` in the
// variable colour — and every one of them changes with the scheme.
//
// The values are the terminal indices the reference emits.  They are
// *not* scheme slots: the seven true-colour schemes' own slots do not
// produce them, and no stock chroma style matches them either (only
// `tokyonight-night` comes close, and it disagrees on the keyword and
// on the type).  A value of -1 means the reference leaves the role at
// the body's colour.
//
// The indices are what a 256-colour terminal shows.  The reference
// writes the *true* colours behind them — see SyntaxColors.
type Syntax struct {
	// Keyword paints `import`, `func` and the other keywords.
	Keyword int
	// String paints a string literal.
	String int
	// Function paints a function name.
	Function int
	// Type paints a type name.
	Type int
	// Variable paints an identifier.
	Variable int
	// Bold reports whether the reference paints the keyword bold.
	Bold bool
}

// syntaxPalettes holds each scheme's code-highlighting palette.
//
// 【一手】 read off agy 1.4.8's preview card under each of the eight
// schemes; the terminal scheme's roles are the sixteen-colour indices
// the reference paints them with.
var syntaxPalettes = map[string]Syntax{
	"terminal":                  {Keyword: 12, String: 3, Function: 4, Type: 5, Variable: -1, Bold: true},
	"light":                     {Keyword: 90, String: 28, Function: 21, Type: 29, Variable: 25},
	"solarized light":           {Keyword: 166, String: 100, Function: 36, Type: 62, Variable: 66},
	"colorblind-friendly light": {Keyword: 166, String: 91, Function: 26, Type: 30, Variable: 238},
	"dark":                      {Keyword: 173, String: 114, Function: 110, Type: 222, Variable: 146},
	"solarized dark":            {Keyword: 166, String: 100, Function: 36, Type: 62, Variable: 246},
	"colorblind-friendly dark":  {Keyword: 208, String: 141, Function: 222, Type: 45, Variable: 254},
	"tokyo night":               {Keyword: 141, String: 149, Function: 111, Type: 44, Variable: 153},
}

// SyntaxFor returns the code-highlighting palette of the named scheme.
// An unknown name falls back to the solarized light palette.
func SyntaxFor(name string) Syntax {
	key := strings.ToLower(strings.TrimSpace(name))
	if syn, ok := syntaxPalettes[key]; ok {
		return syn
	}
	return syntaxPalettes["solarized light"]
}

// SyntaxColors is a colour scheme's code-highlighting palette in true
// colour: the colours behind the indices Syntax carries.
//
// 【一手】 recovered from agy 1.4.8's /config → Color Scheme preview
// card, read off the reference's own output bytes in a true-colour
// terminal.  The reference writes *only* 24-bit escapes for the card —
// it never falls back to a 256-colour index — so these are the values
// it emits, one per role.  Each of them quantises to the index the
// 256-colour capture showed, which is how the two tables were
// cross-checked against each other.
//
// The terminal scheme is the exception: its roles are the basic
// sixteen-colour codes the reference writes (`94` for the keyword,
// `33` for the string, `34` for the function, `35` for the type), and
// the keyword is bold.
type SyntaxColors struct {
	// Keyword paints `import`, `func` and the other keywords.
	Keyword Color
	// String paints a string literal.
	String Color
	// Function paints a function name.
	Function Color
	// Type paints a type name.
	Type Color
	// Variable paints an identifier.
	Variable Color
	// KeywordAttr carries the SGR parameters the reference writes in the
	// same escape as the keyword: "1" for the terminal scheme's bold
	// keyword and "22" for the solarized schemes' normal-intensity one.
	// The other schemes write the keyword's colour alone.
	KeywordAttr string
}

// syntaxColorPalettes holds each scheme's code-highlighting palette in
// true colour.
//
// 【一手】 agy 1.4.8, the preview card under each of the eight schemes,
// captured in a true-colour terminal and read off the wire.
var syntaxColorPalettes = map[string]SyntaxColors{
	"terminal": {
		Keyword:     ANSI(12),
		String:      ANSI(3),
		Function:    ANSI(4),
		Type:        ANSI(5),
		KeywordAttr: "1",
	},
	"light": {
		Keyword:  Hex("#770088"),
		String:   Hex("#008800"),
		Function: Hex("#0000ff"),
		Type:     Hex("#008855"),
		Variable: Hex("#0055aa"),
	},
	"solarized light": {
		Keyword:     Hex("#cb4b16"),
		String:      Hex("#859900"),
		Function:    Hex("#2aa198"),
		Type:        Hex("#6c71c4"),
		Variable:    Hex("#657b83"),
		KeywordAttr: "22",
	},
	"colorblind-friendly light": {
		Keyword:  Hex("#e8710a"),
		String:   Hex("#7b1fa2"),
		Function: Hex("#1967d2"),
		Type:     Hex("#00838f"),
		Variable: Hex("#3c4043"),
	},
	"dark": {
		Keyword:  Hex("#d8884b"),
		String:   Hex("#97c67b"),
		Function: Hex("#75b3db"),
		Type:     Hex("#eede7b"),
		Variable: Hex("#b5bcc5"),
	},
	"solarized dark": {
		Keyword:     Hex("#cb4b16"),
		String:      Hex("#859900"),
		Function:    Hex("#2aa198"),
		Type:        Hex("#6c71c4"),
		Variable:    Hex("#839496"),
		KeywordAttr: "22",
	},
	"colorblind-friendly dark": {
		Keyword:  Hex("#fb8c00"),
		String:   Hex("#b388ff"),
		Function: Hex("#ffe57f"),
		Type:     Hex("#00e5ff"),
		Variable: Hex("#e0e0e0"),
	},
	"tokyo night": {
		Keyword:  Hex("#bb9af7"),
		String:   Hex("#9ece6a"),
		Function: Hex("#7aa2f7"),
		Type:     Hex("#2ac3de"),
		Variable: Hex("#c0caf5"),
	},
}

// SyntaxColorsFor returns the true-colour code-highlighting palette of
// the named scheme.  An unknown name falls back to the solarized light
// palette.
func SyntaxColorsFor(name string) SyntaxColors {
	key := strings.ToLower(strings.TrimSpace(name))
	if syn, ok := syntaxColorPalettes[key]; ok {
		return syn
	}
	return syntaxColorPalettes["solarized light"]
}

// liveScheme is the scheme the package's colours follow.
var liveScheme = "solarized light"

// schemeListeners holds the callbacks run when the live scheme changes.
var schemeListeners []func()

// OnSchemeChange registers fn to run whenever the live colour scheme
// changes.  The packages that alias the palette's colours refresh their
// aliases from it — an alias copied once would keep the old scheme's
// escape.
func OnSchemeChange(fn func()) {
	schemeListeners = append(schemeListeners, fn)
}

// SetScheme makes the package's colours follow the named scheme.
//
// The name is matched case-insensitively against the reference's eight
// scheme names.  It reports whether the name named a scheme; an
// unknown name leaves the current scheme alone.
func SetScheme(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	sc, ok := Schemes[key]
	if !ok {
		return false
	}
	if key != liveScheme {
		liveScheme = key
		applyScheme(sc)
		for _, fn := range schemeListeners {
			fn()
		}
	}
	return true
}

// CurrentScheme returns the name of the live scheme and its colours.
func CurrentScheme() (string, SchemeColors) {
	sc, _ := Schemes[liveScheme]
	return liveScheme, sc
}

// PushScheme makes the package's colours follow the named scheme and
// returns a function that restores the previous scheme.
//
// The TUI pushes the session's selected scheme around the work that
// paints a frame — the update that handles the keys and the view that
// renders — so a frame is painted with the selected scheme's colours
// and the palette keeps its default colours outside a frame.
func PushScheme(name string) func() {
	prev := liveScheme
	if !SetScheme(name) {
		return func() {}
	}
	return func() { SetScheme(prev) }
}

// Seq returns the SGR sequence that switches the colour on, or the
// empty string when the slot carries none.
//
// A true colour opens as the 24-bit escape the reference emits for it
// in a truecolour terminal; a terminal index opens as the
// sixteen-colour escape when the index is one of the first sixteen and
// as the 256-colour escape otherwise — the split the reference's own
// terminal scheme shows.
func (c Color) Seq(background bool) string {
	switch {
	case c.value == "":
		return ""
	case c.ansi:
		n, err := strconv.Atoi(c.value)
		if err != nil {
			return ""
		}
		return ansiSeq(n, background)
	default:
		return hexSeq(c.value, background)
	}
}

// ansiSeq opens a terminal index.
func ansiSeq(n int, background bool) string {
	switch {
	case n < 0 || n > 255:
		return ""
	case n < 8:
		if background {
			return "\x1b[" + strconv.Itoa(40+n) + "m"
		}
		return "\x1b[" + strconv.Itoa(30+n) + "m"
	case n < 16:
		if background {
			return "\x1b[" + strconv.Itoa(100+n-8) + "m"
		}
		return "\x1b[" + strconv.Itoa(90+n-8) + "m"
	default:
		if background {
			return "\x1b[48;5;" + strconv.Itoa(n) + "m"
		}
		return "\x1b[38;5;" + strconv.Itoa(n) + "m"
	}
}

// hexSeq opens a true colour.
func hexSeq(value string, background bool) string {
	if len(value) != 7 || value[0] != '#' {
		return ""
	}
	r := hexByte(value[1:3])
	g := hexByte(value[3:5])
	b := hexByte(value[5:7])
	prefix := "38;2;"
	if background {
		prefix = "48;2;"
	}
	return "\x1b[" + prefix + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) + ";" + strconv.Itoa(int(b)) + "m"
}

// seqWith opens a colour with extra SGR parameters in the same escape
// — the form the reference emits for a bold or a faint colour.
//
// An empty extra leaves the escape alone: the reference never writes a
// trailing `;` before the `m`.
func seqWith(c Color, background bool, extra string) string {
	s := c.Seq(background)
	if s == "" {
		return ""
	}
	if extra == "" {
		return s
	}
	return s[:len(s)-1] + ";" + extra + "m"
}

// SeqWith opens a foreground colour with extra SGR parameters in the
// same escape — the form the reference emits for a bold or an
// underlined colour.
//
// 【一手】 agy 1.4.8's preview card writes the user's line as
// `38;2;25;103;210;1` — the colour and the bold in one escape — and
// the link as `38;2;26;115;232;4`.
func SeqWith(c Color, extra string) string {
	return seqWith(c, false, extra)
}

// pairSeq opens a foreground and a background in one escape — the form
// the reference emits for a diff line number.
func pairSeq(fg, bg Color) string {
	f := fg.Seq(false)
	b := bg.Seq(true)
	switch {
	case f == "":
		return b
	case b == "":
		return f
	}
	return f[:len(f)-1] + ";" + b[2:]
}
