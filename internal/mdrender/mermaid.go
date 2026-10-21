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
func MermaidFrontmatter(body string) (yamlBody string, cleaned string, ok bool) {
	if body == "" {
		return "", body, false
	}
	// Normalize CRLF to LF so the fence matchers below work on
	// either line ending. The original bytes are restored on the
	// path that returns the input unchanged (which has no CRLF -> LF
	// issues -- the comparisons below still match the fence line),
	// but for the frontmatter case we hand the YAML to a yaml.v3
	// decoder that is itself line-ending agnostic.
	src := body
	if strings.HasPrefix(src, "\r\n") {
		src = strings.ReplaceAll(src, "\r\n", "\n")
	}

	lines := strings.Split(src, "\n")
	if len(lines) == 0 {
		return "", body, false
	}
	if strings.TrimSpace(lines[0]) != "---" {
		return "", body, false
	}
	close := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			close = i
			break
		}
	}
	if close < 0 {
		// Unterminated frontmatter: treat as if absent. The AGY
		// 1.3.6 changelog only fixes the "starts with YAML
		// frontmatter" case, so we deliberately do not synthesise
		// a closing fence; the raw source would have failed any
		// consumer's parser too.
		return "", body, false
	}
	// Drop the opening fence (line 0) and the closing fence
	// (lines[close]); keep everything between. The join over \n
	// recovers the original line content verbatim (single leading
	// newline, no trailing newline because the closer line itself
	// is dropped).
	yamlBody = strings.Join(lines[1:close], "\n")
	// Re-stitch: anything from the closing fence's trailing
	// newline to the next newline is the diagram source. If there
	// is no trailing newline (close+1 == len(lines)), lines[close+1:]
	// is an empty slice and we drop into the empty-source case.
	rest := lines[close+1:]
	cleaned = strings.Join(rest, "\n")
	// Strip a single leading newline so the cleaned source doesn't
	// start with a blank line that would have been the visual
	// separator between frontmatter and diagram.
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
func MermaidRenderBlock(block MermaidBlock) string {
	var buf bytes.Buffer
	if block.Frontmatter != nil {
		// Successful parse: emit the sorted-key header. Sorting
		// gives a stable wire form for tests and for users who
		// snapshot their transcripts.
		keys := make([]string, 0, len(block.Frontmatter))
		for k := range block.Frontmatter {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteString("[mermaid")
		for _, k := range keys {
			v := block.Frontmatter[k]
			switch t := v.(type) {
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
	} else if block.Raw != "" && block.Raw != block.Source {
		// Parse failure path. Render raw keeps the user's diagram
		// visible; the header records why the metadata was dropped.
		buf.WriteString("[mermaid frontmatter=invalid]\n")
	}
	// Body to emit: prefer Raw when the frontmatter is invalid or
	// absent so the user sees the same bytes the model emitted,
	// including the YAML fence. Otherwise prefer Source, the
	// cleaned diagram body.
	body := block.Source
	if block.Frontmatter == nil && block.Raw != "" && block.Raw != block.Source {
		body = block.Raw
	}
	// Indent every line by four spaces, matching the bare fenced
	// block path in mdrender.go. An empty line still gets its
	// 4-space prefix so the rendered transcript stays visually
	// homogeneous with the surrounding markdown.
	for _, line := range strings.Split(body, "\n") {
		buf.WriteString("    ")
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	return buf.String()
}

// MermaidBlocksFromMarkdown finds every mermaid fenced code block in
// a markdown source and returns them as MermaidBlocks. The fence
// grammar is "three backticks, then 'mermaid', then newline, then
// body, then closing 'three backticks' on its own line".
//
// The opening fence is matched character-by-character so a block
// whose info string starts with "mermaid" but is followed by a
// hyphen (e.g. "mermaid-foo") is still accepted (the reference's
// gateway uses the bare token), and so is "mermaid ..." with
// trailing info. The closing fence is a three-backtick line,
// optionally preceded by any amount of leading whitespace
// (CommonMark section 4.5).
//
// This is a *string* scan, not a goldmark AST walk: callers that
// already have an AST (the walker in mdrender.go) hand the block
// body directly to MermaidFrontmatter. The string scan exists so the
// print path and any external tool can post-process a full
// transcript without re-parsing the markdown tree.
func MermaidBlocksFromMarkdown(src string) []MermaidBlock {
	var out []MermaidBlock
	if src == "" {
		return out
	}
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trim, "```") {
			continue
		}
		// The remainder of the line, after the three backticks,
		// is the info string. Match startsWith("mermaid") so the
		// bare info and any variant ("mermaid foo", ...) are
		// accepted.
		rest := trim[3:]
		if !strings.HasPrefix(rest, mermaidLanguage) {
			continue
		}
		// Either rest == "mermaid" or the next byte is whitespace.
		if len(rest) > len(mermaidLanguage) {
			next := rest[len(mermaidLanguage)]
			if next != ' ' && next != '\t' && next != '\r' {
				continue
			}
		}
		// Find the closing fence. CommonMark 4.5 allows the
		// closer's indent to be up to three spaces; we accept any
		// leading whitespace to mirror the spec without making
		// this function a full parser. The closing fence's info
		// string, if any, is ignored (CommonMark allows it but
		// most renderers don't honour it).
		j := i + 1
		closed := false
		for ; j < len(lines); j++ {
			closerTrim := strings.TrimLeft(lines[j], " \t")
			if !strings.HasPrefix(closerTrim, "```") {
				continue
			}
			closerRest := closerTrim[3:]
			// The closer backticks may extend past three (e.g.
			// six in a row); we accept any sequence of three or
			// more backticks with no following non-blank text.
			trimmed := strings.TrimLeft(closerRest, "`")
			trimmed = strings.TrimRight(trimmed, " \t\r")
			if trimmed != "" {
				// Non-empty info string on the closer.
				// CommonMark allows it but the reference ignores
				// it; match the reference.
				continue
			}
			closed = true
			break
		}
		if !closed {
			// Unterminated block -- return what we have with a
			// sentinel so the caller can render the partial
			// source without crashing. This matches the spec
			// test "mermaid block without closing fence handled
			// gracefully".
			body := strings.Join(lines[i+1:], "\n")
			out = append(out, MermaidBlock{
				Source: body,
				Raw:    trim + "\n" + body,
			})
			return out
		}
		rawBody := strings.Join(lines[i+1:j], "\n")
		raw := trim + "\n" + rawBody + "\n" + lines[j]
		yamlBody, cleaned, hasFM := MermaidFrontmatter(rawBody)
		if !hasFM {
			out = append(out, MermaidBlock{
				Source: cleaned,
				Raw:    raw,
			})
			i = j
			continue
		}
		fm, err := ParseMermaidFrontmatter(yamlBody)
		if err != nil {
			// Malformed YAML -- keep the frontmatter bytes in
			// Raw so the renderer falls back to emitting the
			// full block verbatim. Source stays at the cleaned
			// diagram body, Frontmatter stays nil so the header
			// path picks the invalid-FM label instead of the
			// success path.
			out = append(out, MermaidBlock{
				Source:      cleaned,
				Raw:         raw,
				Frontmatter: nil,
			})
			i = j
			continue
		}
		out = append(out, MermaidBlock{
			Source:      cleaned,
			Raw:         raw,
			Frontmatter: fm,
		})
		i = j
	}
	return out
}
