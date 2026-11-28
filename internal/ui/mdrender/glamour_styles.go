// glamour_styles.go — custom Glamour styles, ANSI themes, and margin/padding
// configurations for internal/mdrender.
//
// Style decisions documented inline. The blocks mimic crush's
// internal/ui/styles/quickstyle.go (lines 231-339) plus the cycle
// 175 / 1.3.3 file-link contract:
//
//   - headings keep cycle-175 "# …" prefix in plain mode
//   - blockquote "> " per line (cycle 175 contract from the legacy walker)
//   - "•" U+2022 unordered bullet (cycle 175)
//   - "1." ordered-list marker (cycle 175)
//   - 4-space code indent (cycle 175)
//   - "*bold*" / "_italic_" marker rewrites for plain variants
package mdrender

import (
	"strings"
	"sync"

	"charm.land/glamour/v2/ansi"
	"github.com/justwasm/pigasus/palette"
)

var (
	currentColorSchemeMu sync.RWMutex
	currentColorScheme   = "solarized light"
)

// SetColorScheme configures the active color scheme for markdown and code rendering.
// It invalidates cached renderers and updates the chroma code-block syntax style.
func SetColorScheme(scheme string) {
	currentColorSchemeMu.Lock()
	currentColorScheme = scheme
	currentColorSchemeMu.Unlock()
	setChromaScheme(scheme)
	InvalidateCache()
}

// CurrentColorScheme returns the currently active color scheme name.
func CurrentColorScheme() string {
	currentColorSchemeMu.RLock()
	defer currentColorSchemeMu.RUnlock()
	return currentColorScheme
}

// boolPtr is a tiny helper for glamour's *bool fields. The pointer
// is what glamour checks (nil = "default"); we can't pass literal
// false because glamour's cascade treats `nil` differently from
// `&false`.
func boolPtr(b bool) *bool { return &b }

// uintPtr is the *uint counterpart. ansi.StyleBlock.Indent is *uint
// (glamour zero = no indent). We need a pointer to disambiguate
// "no indent" (nil) from "indent 0" (impossible).
func uintPtr(u uint) *uint { return &u }

// strPtr is the *string counterpart. ansi uses *string for Color /
// BackgroundColor. nil = "no colour override"; &"" = "explicit
// empty" (glamour's cascade treats both the same in practice).
func strPtr(s string) *string { return &s }

// styleConfigFor builds the ansi.StyleConfig for variant v.
func styleConfigFor(v mdVariant) ansi.StyleConfig {
	switch v {
	case variantANSI:
		return buildANSIConfig()
	case variantPlain:
		return buildPlainConfig()
	case variantUser:
		return buildUserConfig()
	case variantQuiet:
		return buildQuietConfig()
	}
	return buildANSIConfig()
}

// buildLegacyShapeConfig is the cycle-175 marker-preserving shape:
// "# " headings, "> " blockquotes, the U+2421 bullet, the "---"
// horizontal rule and a 4-space code fence. It is the base for the
// Plain / Quiet variants, whose consumers (the --print non-TTY path
// and the OAuth login prompt) pin those marker bytes.
func buildLegacyShapeConfig() ansi.StyleConfig {
	cfg := ansi.StyleConfig{
		Document: ansi.StyleBlock{},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: strPtr("240"), // dim gray
			},
			Indent:      uintPtr(1),
			IndentToken: strPtr("> "), // cycle 175 contract
		},
		List: ansi.StyleList{
			LevelIndent: 4, // cycle 175 visual
		},
		HorizontalRule: ansi.StylePrimitive{
			Format: "\n---\n", // cycle 175 contract
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "\u2421 ", // cycle 175 — bullet
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ", // cycle 175 — "1." stays; glamour adds "1"
		},
		Task: ansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
	}
	applyLegacyHeadings(&cfg)
	applyLegacyTextStyles(&cfg)
	applyLegacyCodeStyles(&cfg)
	return cfg
}

func applyLegacyHeadings(cfg *ansi.StyleConfig) {
	cfg.Heading = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix: "\n", // cycle 175
			Bold:        boolPtr(true),
			Color:       strPtr("39"), // bright blue, brand-blue family
		},
	}
	cfg.H1 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "# ", Bold: boolPtr(true)}}
	cfg.H2 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "## ", Bold: boolPtr(true)}}
	cfg.H3 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "### ", Bold: boolPtr(true)}}
	cfg.H4 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "#### ", Bold: boolPtr(true)}}
	cfg.H5 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "##### ", Bold: boolPtr(true)}}
	cfg.H6 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "###### ", Bold: boolPtr(true)}}
}

func applyLegacyTextStyles(cfg *ansi.StyleConfig) {
	cfg.Strikethrough = ansi.StylePrimitive{CrossedOut: boolPtr(true)}
	cfg.Emph = ansi.StylePrimitive{Italic: boolPtr(true)}
	cfg.Strong = ansi.StylePrimitive{Bold: boolPtr(true)}
	cfg.Link = ansi.StylePrimitive{
		Color:     strPtr("39"),
		Underline: boolPtr(false),
	}
	// The link text follows the scheme's own link slot. It used to be the
	// ANSI-256 literal "135" (a magenta), which never moved when the scheme
	// changed — the reference's link colour is the scheme's LinkFg (#8ab4f8
	// under solarized dark, #7aa2f7 under tokyo night).
	cfg.LinkText = ansi.StylePrimitive{
		Color: strPtr(linkTextColor()),
	}
}

// bodyColor is the body text's colour from the active scheme.
func bodyColor() string {
	return schemeSlotColor("Fg")
}

// codeSpanText is the colour a code span's own text is drawn in.
//
// It is the scheme's Accent, not the body colour. The distinction is the whole
// point of the element: a code span painted in the body colour is not a span
// at all, it is the sentence it sits in.
//
// The pair is recovered from a PTY capture rather than guessed. Cycle 158
// recorded agy 1.3.6 drawing a code span as \x1b[48;5;254m\x1b[38;5;69m —
// background index 254 and foreground index 69. Quantise the `light` scheme's
// slots against the xterm-256 cube and both land exactly:
//
//	Light.Accent #4285f4 -> rgb(66,133,244) -> index 69  rgb(95,135,255)
//	Light.Border #dadce0 -> rgb(218,220,224) -> index 254 rgb(228,228,228)
//
// The clone's own pre-1.5.17 constant said the same thing in solarized-light
// terms — palette.InlineCodeOpen was "38;5;32m48;5;254m", and
// SolarizedLight.Accent #268bd2 quantises to 32 while SolarizedLight.Border
// #eee8d5 quantises to 254. The recipe was always accent-on-border; what was
// wrong was that it had been frozen at one scheme's indices instead of being
// resolved from the live one.
func codeSpanText() string {
	return schemeSlotColor("Accent")
}

// codeSpanSurface is the tint a code span sits on — the scheme's Border, the
// colour it already spends on the surfaces one step off the background.
func codeSpanSurface() string {
	return schemeSlotColor("Border")
}

// linkTextColor is the link text's colour from the active scheme.
//
// glamour accepts a hex string ("#8ab4f8") or a terminal index ("12"), which
// is exactly what a palette slot's Value() carries — so the slot is passed
// through rather than translated.
func linkTextColor() string {
	return schemeSlotColor("LinkFg")
}

// schemeSlotColor reads one slot of the scheme this renderer is drawing.
//
// The renderer's own CurrentColorScheme is the source, not the palette's
// live scheme: the two are set by the same host call, but a render reached
// from anywhere else must still describe the scheme it was asked for.
func schemeSlotColor(slot string) string {
	c, ok := palette.Schemes[CurrentColorScheme()].Slot(slot)
	if !ok || !c.IsSet() {
		return "39"
	}
	return c.Value()
}

func applyLegacyCodeStyles(cfg *ansi.StyleConfig) {
	cfg.Code = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix: "`",
			BlockSuffix: "`",
			// Inline code is the scheme's Accent on its Border — see
			// codeSpanText for the capture this pair comes from. Both slots
			// move with the scheme, so the span follows /config; painting it
			// in the body colour instead made it disappear into the prose.
			Color:           strPtr(codeSpanText()),
			BackgroundColor: strPtr(codeSpanSurface()),
		},
	}
	cfg.CodeBlock = ansi.StyleCodeBlock{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockPrefix: "    ", // 4-space indent — cycle 175 walker contract.
			},
		},
	}
}

// buildPlainConfig is the plain-text variant. Same structure as
// ANSI, but with no Colour / Bold / Italics set so output reaches
// a file or pipe unstyled.
func buildPlainConfig() ansi.StyleConfig {
	cfg := buildLegacyShapeConfig()
	cfg.Document.StylePrimitive.Color = nil
	cfg.Heading.StylePrimitive.Color = nil
	cfg.Heading.StylePrimitive.Bold = nil
	cfg.Link.Color = nil
	cfg.LinkText.Color = nil
	cfg.Code.StylePrimitive.Color = nil
	cfg.BlockQuote.StylePrimitive.Color = nil
	cfg.H1.StylePrimitive.Bold = nil
	cfg.H2.StylePrimitive.Bold = nil
	cfg.H3.StylePrimitive.Bold = nil
	cfg.H4.StylePrimitive.Bold = nil
	cfg.H5.StylePrimitive.Bold = nil
	cfg.H6.StylePrimitive.Bold = nil
	cfg.Emph.Italic = nil
	cfg.Strong.Bold = nil
	return cfg
}

func resolveColorSchemeColors(scheme string) (docColor, headColor, h1Bg string, isDark bool) {
	norm := strings.ToLower(strings.TrimSpace(scheme))
	switch norm {
	case "dark", "colorblind-friendly dark":
		sc := palette.Schemes[norm]
		return sc.Fg.Value(), sc.Accent.Value(), "24", true
	case "solarized dark":
		sc := palette.Schemes[norm]
		return sc.Fg.Value(), sc.Accent.Value(), "24", true
	case "tokyo night":
		sc := palette.Schemes[norm]
		return sc.Fg.Value(), sc.Accent.Value(), "60", true
	case "terminal":
		return "", "33", "24", true
	case "light", "colorblind-friendly light":
		sc := palette.Schemes[norm]
		return sc.Fg.Value(), sc.Accent.Value(), "63", false
	case "solarized light":
		sc := palette.Schemes[norm]
		return sc.Fg.Value(), sc.Accent.Value(), "63", false
	default:
		if sc, ok := palette.Schemes[norm]; ok {
			isDk := sc.Bg.IsSet() && (strings.Contains(norm, "dark") || norm == "tokyo night")
			return sc.Fg.Value(), sc.Accent.Value(), "24", isDk
		}
		return "#073642", "27", "63", false
	}
}

// buildANSIConfig is the chat-transcript palette, recovered byte-for-
// byte from the AGY 1.4.2 `--conversation` PTY capture (cycle 271).
func buildANSIConfig() ansi.StyleConfig {
	scheme := CurrentColorScheme()
	docColor, headColor, h1Bg, isDark := resolveColorSchemeColors(scheme)

	var docColorPtr *string
	if docColor != "" {
		docColorPtr = strPtr(docColor)
	}

	cfg := ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: docColorPtr,
			},
			Margin: uintPtr(2),
		},
		BlockQuote: ansi.StyleBlock{
			Indent:      uintPtr(1),
			IndentToken: strPtr("│ "),
		},
		List: ansi.StyleList{
			LevelIndent: 4,
		},
		HorizontalRule: ansi.StylePrimitive{
			// The rule is the scheme's border colour. It used to be the
			// ANSI-256 literal "249", the one primitive in this document
			// that kept its colour across every scheme.
			Color:  strPtr(schemeSlotColor("Border")),
			Format: "──────",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
	}
	applyANSIHeadings(&cfg, headColor, h1Bg)
	applyANSITextAndCodeStyles(&cfg, headColor, isDark)
	return cfg
}

func applyANSIHeadings(cfg *ansi.StyleConfig, headColor, h1Bg string) {
	cfg.Heading = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix: "\n",
			Color:       strPtr(headColor),
			Bold:        boolPtr(true),
		},
	}
	cfg.H1 = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix:          " ",
			Suffix:          " ",
			Color:           strPtr("228"),
			BackgroundColor: strPtr(h1Bg),
			Bold:            boolPtr(true),
			Underline:       boolPtr(true),
		},
	}
	cfg.H2 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Bold: boolPtr(true)}}
	cfg.H3 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Bold: boolPtr(true), Italic: boolPtr(true)}}
	cfg.H4 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Italic: boolPtr(true), Underline: boolPtr(true)}}
	cfg.H5 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Italic: boolPtr(true)}}
	cfg.H6 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Underline: boolPtr(true)}}
}

func applyANSITextAndCodeStyles(cfg *ansi.StyleConfig, accentColor string, isDark bool) {
	cfg.Strikethrough = ansi.StylePrimitive{CrossedOut: boolPtr(true)}
	cfg.Emph = ansi.StylePrimitive{Italic: boolPtr(true)}
	cfg.Strong = ansi.StylePrimitive{Bold: boolPtr(true)}
	linkColor := "36"
	if accentColor != "" {
		linkColor = accentColor
	}
	cfg.Link = ansi.StylePrimitive{
		Color:     strPtr(linkColor),
		Underline: boolPtr(true),
	}
	// The link text is the scheme's own link colour. It used to be the
	// ANSI-256 literal "29", which never moved when the scheme changed: the
	// reference draws link text in the scheme's LinkFg (#8ab4f8 under
	// solarized dark, #7aa2f7 under tokyo night).
	cfg.LinkText = ansi.StylePrimitive{
		Color: strPtr(linkTextColor()),
		Bold:  boolPtr(true),
	}
	// Inline code is the scheme's Accent on its Border, not the body colour.
	// The body colour made the span byte-for-byte identical to the words
	// around it, so `code` stopped being a span at all. codeSpanText records
	// the capture the pair comes from.
	cfg.Code = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:           strPtr(codeSpanText()),
			BackgroundColor: strPtr(codeSpanSurface()),
		},
	}
	cfg.CodeBlock = ansi.StyleCodeBlock{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: strPtr("242"),
			},
			Margin: uintPtr(2),
		},
	}
}

// buildUserConfig is the shape used for user-typed markdown echoes
// (/learn, /goal). It deliberately keeps the cycle-175 markers
// ("# ", "> ", "•" via U+2421) and the no-margin layout.
func buildUserConfig() ansi.StyleConfig {
	return buildLegacyShapeConfig()
}

// buildQuietConfig is for the OAuth login prompt. No colour, no
// styling, no horizontal-rule format.
func buildQuietConfig() ansi.StyleConfig {
	cfg := buildPlainConfig()
	cfg.Heading.StylePrimitive.BlockSuffix = "\n"
	cfg.HorizontalRule.Format = "\n--------\n"
	return cfg
}
