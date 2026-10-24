// mermaid.go -- frontmatter extraction + render hint for mermaid
// fenced code blocks.
//
// Cycle mermaid-v11-frontmatter (AGY 1.3.6 changelog line 9). Before
// this cycle a mermaid block that began with a YAML frontmatter
// fence tripped the renderer in two ways:
//
//  1. The chat UI (which renders mermaid client-side) saw the raw
//     fenced source as having a leading "--"-style thematic break
//     instead of YAML metadata, so a block such as the one below
//     was thrown at the Mermaid v11 parser with the "---" fence
//     still attached -- and the parser discarded the whole block
//     instead of treating the frontmatter as diagram metadata. The
//     agent and the artifact viewer both showed the raw source.
//
//     ```mermaid
//     ---
//     title: My Diagram
//     ---
//     graph TD; A-->B
//     ```
//
//  2. The reference binary's 1.3.6 fix detects mermaid fenced blocks,
//     strips the YAML frontmatter from the source, parses it
//     (yaml.v3), and threads the resulting keys (commonly "title:",
//     "theme:", "config:", "themeVariables:") into the renderer's
//     options object. The cleaner source then reaches the Mermaid
//     v11 parser without the leading thematic break.
//
// This package does not render Mermaid diagrams -- that's done in the
// chat UI's web view -- but it does own the markdown-side
// normalisation. After mermaid-v11-frontmatter:
//
//   - MermaidFrontmatter(body) carves the optional YAML fence off the
//     front of the mermaid source and returns the YAML body and the
//     cleaned source.
//   - MermaidRenderBlock is the text we emit through Render and
//     RenderANSI when a TUI transcript can't show the diagram (no
//     Mermaid renderer in the terminal). It carries the parsed
//     frontmatter back to the user as a short "[mermaid title=..."
//     theme=...]" header so the metadata is surfaced instead of
//     silently dropped. When the YAML is malformed the original
//     block -- frontmatter and all -- round trips verbatim, which is
//     what the changelog anchor promises: "render raw".
//   - The goldmark walker (mdrender.go) recognises mermaid-tagged
//     FencedCodeBlocks and routes through MermaidRenderBlock instead
//     of the plain four-space indent.
//
// yaml.v3 is already a direct dependency (go.mod, gopkg.in/yaml.v3
// v3.0.1); the cycle adds no third-party code.
package mdrender

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// mermaidLanguage is the bare info string the chat-gateway emits on
// a fenced mermaid code block. We refuse any block whose info string
// does not start with this token so we don't accidentally reformat
// python blocks that happen to start with "---".
const mermaidLanguage = "mermaid"

// MermaidBlock is one fenced mermaid block carved out of an
// already-rendered transcript. It's the shape MermaidRenderBlock
// works on; MermaidBlocksFromMarkdown produces a slice of these.
type MermaidBlock struct {
	// Frontmatter holds the parsed YAML of the leading fence, or
	// nil when the block had no frontmatter. Keys are kept verbatim
	// as the YAML author wrote them -- title, theme, config (a
	// mapping), and themeVariables (also a mapping) are the four
	// the reference threads into the renderer; we keep any others
	// so future cycles don't have to re-extend the API.
	Frontmatter map[string]any
	// Source is the cleaned mermaid source: the frontmatter bytes
	// are gone, the leading blank line that separated fence from
	// frontmatter is gone, and the trailing blank line that
	// separated frontmatter from diagram is gone.
	Source string
	// Raw is the block's source verbatim, including the frontmatter
	// and the YAML fences. MermaidRenderBlock falls back to
	// rendering this when the YAML parser rejects the frontmatter,
	// so the user can still see the diagram source even though the
	// metadata didn't parse.
	Raw string
}

// MermaidFrontmatter splits a mermaid block body into the optional
// leading YAML fence and the diagram source that follows.
//
// The grammar is the same as the SKILL.md / Markdown frontmatter
// grammar that cmd/wagyu/print_slash.go:skillFrontmatter uses, but
// inlined here so the mdrender package has no os dependency.
//
// Returns (yamlBody, cleaned, true) when the block starts with a
// YAML fence line followed by at least one line and a closing YAML
// fence line; the leading and closing fence lines are NOT included
// in the returned YAML, the trailing newline after the closing
// fence is removed, and cleaned is the diagram source starting at
// the next line.
//
// Returns ("", body, false) when the block has no frontmatter;
// body is the input unchanged.
func findClosingFence(lines []string) int {
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return i
		}
	}
	return -1
}

func MermaidFrontmatter(body string) (yamlBody string, cleaned string, ok bool) {
	if body == "" {
		return "", body, false
	}
	src := body
	if strings.HasPrefix(src, "\r\n") {
		src = strings.ReplaceAll(src, "\r\n", "\n")
	}
	lines := strings.Split(src, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", body, false
	}
	close := findClosingFence(lines)
	if close < 0 {
		return "", body, false
	}
	yamlBody = strings.Join(lines[1:close], "\n")
	rest := lines[close+1:]
	cleaned = strings.Join(rest, "\n")
	cleaned = strings.TrimPrefix(cleaned, "\n")
	return yamlBody, cleaned, true
}

// ParseMermaidFrontmatter parses the YAML bytes returned by
// MermaidFrontmatter and returns a normalised frontmatter map. YAML
// decoding errors yield (nil, err) -- callers fall back to rendering
// the raw source. yaml.v3's map ordering is *not* stable, so the
// returned map's iteration order is implementation-defined; callers
// that need a stable wire form should rely on MermaidRenderBlock's
// sorted header.
func ParseMermaidFrontmatter(yamlBody string) (map[string]any, error) {
	if yamlBody == "" {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(yamlBody), &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		// An "yaml:" scalar or empty body decodes to nil. Treat it
		// as "frontmatter present, empty" so the caller still
		// strips it instead of falling back to raw.
		return map[string]any{}, nil
	}
	return doc, nil
}

// MermaidRenderBlock returns the text form that Render and
// RenderANSI should emit for a single mermaid block when no live
// Mermaid renderer is wired up (the TUI and the print path are both
// in this category -- both write a transcript of model output, the
// diagram itself is rendered by the chat UI's web view).
//
// The returned text is one or more lines:
//
//   - Lines starting with "[mermaid ...]" carry the parsed
//     frontmatter keys back to the user so the metadata doesn't
//     silently disappear. Only scalar keys are emitted; nested
//     mappings like "config:" and "themeVariables:" are mentioned
//     by name with a count (e.g. "config=2-keys") so the user can
//     tell something parsed without us serialising arbitrary user
//     YAML to the transcript.
//   - The diagram body is emitted verbatim, indented by four spaces
//     to match the regular code-block render so transcripts stay
//     visually consistent with the surrounding markdown.
//
// When the YAML is malformed the raw block (frontmatter + diagram)
// is emitted indented -- that's the spec point: "render raw" -- and
// the header line records "frontmatter=invalid" so the user can see
// the metadata was rejected, not absent.
func formatMermaidFrontmatter(fm map[string]any) string {
	keys := make([]string, 0, len(fm))
	for k := range fm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf strings.Builder
	buf.WriteString("[mermaid")
	for _, k := range keys {
		switch t := fm[k].(type) {
		case nil:
			fmt.Fprintf(&buf, " %s=", k)
		case string:
			fmt.Fprintf(&buf, " %s=%q", k, t)
		case bool, int, int64, float64, uint64:
			fmt.Fprintf(&buf, " %s=%v", k, t)
		case map[string]any:
			fmt.Fprintf(&buf, " %s=%d-keys", k, len(t))
		case []any:
			fmt.Fprintf(&buf, " %s=%d-items", k, len(t))
		default:
			fmt.Fprintf(&buf, " %s=...", k)
		}
	}
	buf.WriteString("]\n")
	return buf.String()
}

func MermaidRenderBlock(block MermaidBlock) string {
	var buf bytes.Buffer
	if block.Frontmatter != nil {
		buf.WriteString(formatMermaidFrontmatter(block.Frontmatter))
	} else if block.Raw != "" && block.Raw != block.Source {
		buf.WriteString("[mermaid frontmatter=invalid]\n")
	}
	body := block.Source
	if block.Frontmatter == nil && block.Raw != "" && block.Raw != block.Source {
		body = block.Raw
	}
	for _, line := range strings.Split(body, "\n") {
		buf.WriteString("    ")
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	return buf.String()
}

func isMermaidFenceOpener(line string) (string, bool) {
	trim := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trim, "```") {
		return "", false
	}
	rest := trim[3:]
	if !strings.HasPrefix(rest, mermaidLanguage) {
		return "", false
	}
	if len(rest) > len(mermaidLanguage) {
		next := rest[len(mermaidLanguage)]
		if next != ' ' && next != '\t' && next != '\r' {
			return "", false
		}
	}
	return trim, true
}

func findMermaidFenceCloser(lines []string, start int) (int, bool) {
	for j := start; j < len(lines); j++ {
		closerTrim := strings.TrimLeft(lines[j], " \t")
		if !strings.HasPrefix(closerTrim, "```") {
			continue
		}
		closerRest := closerTrim[3:]
		trimmed := strings.TrimLeft(closerRest, "`")
		trimmed = strings.TrimRight(trimmed, " \t\r")
		if trimmed != "" {
			continue
		}
		return j, true
	}
	return -1, false
}

func parseMermaidBlock(trim, rawBody, closerLine string) MermaidBlock {
	raw := trim + "\n" + rawBody + "\n" + closerLine
	yamlBody, cleaned, hasFM := MermaidFrontmatter(rawBody)
	if !hasFM {
		return MermaidBlock{Source: cleaned, Raw: raw}
	}
	fm, err := ParseMermaidFrontmatter(yamlBody)
	if err != nil {
		return MermaidBlock{Source: cleaned, Raw: raw, Frontmatter: nil}
	}
	return MermaidBlock{Source: cleaned, Raw: raw, Frontmatter: fm}
}

func MermaidBlocksFromMarkdown(src string) []MermaidBlock {
	var out []MermaidBlock
	if src == "" {
		return out
	}
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		trim, ok := isMermaidFenceOpener(lines[i])
		if !ok {
			continue
		}
		j, closed := findMermaidFenceCloser(lines, i+1)
		if !closed {
			body := strings.Join(lines[i+1:], "\n")
			out = append(out, MermaidBlock{
				Source: body,
				Raw:    trim + "\n" + body,
			})
			return out
		}
		rawBody := strings.Join(lines[i+1:j], "\n")
		out = append(out, parseMermaidBlock(trim, rawBody, lines[j]))
		i = j
	}
	return out
}
