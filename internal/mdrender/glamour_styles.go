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
	cfg.LinkText = ansi.StylePrimitive{
		Color: strPtr("135"), // magenta-ish, distinguishes from URLs
	}
}

func applyLegacyCodeStyles(cfg *ansi.StyleConfig) {
	cfg.Code = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix: "`",
			BlockSuffix: "`",
			Color:       strPtr("220"), // gold on default bg
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

func resolveColorSchemeColors(scheme string) (docColor, headColor, h1Bg string) {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "dark", "solarized dark", "colorblind-friendly dark":
		return "#839496", "33", "24"
	case "tokyo night":
		return "#c8d3f5", "75", "60"
	case "terminal":
		return "", "33", "24"
	default:
		return "#073642", "27", "63"
	}
}

// buildANSIConfig is the chat-transcript palette, recovered byte-for-
// byte from the AGY 1.4.2 `--conversation` PTY capture (cycle 271).
func buildANSIConfig() ansi.StyleConfig {
	scheme := CurrentColorScheme()
	docColor, headColor, h1Bg := resolveColorSchemeColors(scheme)

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
			Color:  strPtr("249"),
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
	applyANSITextAndCodeStyles(&cfg)
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
		},
	}
	cfg.H2 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "## "}}
	cfg.H3 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "### "}}
	cfg.H4 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "#### "}}
	cfg.H5 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "##### "}}
	cfg.H6 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Prefix: "###### ", Bold: boolPtr(false)}}
}

func applyANSITextAndCodeStyles(cfg *ansi.StyleConfig) {
	cfg.Strikethrough = ansi.StylePrimitive{CrossedOut: boolPtr(true)}
	cfg.Emph = ansi.StylePrimitive{Italic: boolPtr(true)}
	cfg.Strong = ansi.StylePrimitive{Bold: boolPtr(true)}
	cfg.Link = ansi.StylePrimitive{
		Color:     strPtr("36"),
		Underline: boolPtr(true),
	}
	cfg.LinkText = ansi.StylePrimitive{
		Color: strPtr("29"),
		Bold:  boolPtr(true),
	}
	cfg.Code = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:           strPtr("32"),
			BackgroundColor: strPtr("254"),
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
