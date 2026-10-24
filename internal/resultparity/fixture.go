// Package resultparity binds the captured agy 1.4.4 tool-result transcript to
// the clone's executors.
//
// internal/tools/testdata/result_texts_1.4.4.txt is the only 【一手】 record of
// what the reference returns to the model for each of its declared tools. Until
// this package existed it was cited in code comments and hand-copied into test
// constants, so nothing failed when a constant drifted from the capture —
// cycle 228's `not yet implemented (cycle 228 stub)` text reached the model for
// months that way, and was found by grep rather than by a test.
//
// Load parses the capture into sections; replay_test.go replays every section
// it can and records an explicit, reasoned skip for the rest. A section that is
// neither replayed nor skipped fails TestCaptureIsFullyAccountedFor, so the
// fixture cannot quietly stop being read.
package resultparity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Section is one captured call/result pair.
type Section struct {
	// Name is the tool name from the "## <name> {json}" header.
	Name string
	// Args is the argument object, verbatim from the header (compacted, but
	// byte-identical in value).
	Args json.RawMessage
	// Note is the free-form "; …" note on the header line, if any.
	Note string
	// Notes are the "#" comment lines that precede the body.
	Notes []string
	// Body is the captured result text, with the <stamp> marker removed and
	// trailing newlines trimmed.
	Body string
	// HasStamp records whether the body was preceded by the "<stamp>" marker
	// (the two timestamp lines the reference puts in front of every result).
	HasStamp bool
	// Line is the 1-based line number of the "## " header, for failure messages.
	Line int
}

func parseSectionHeader(path string, lineNum int, line string) (*Section, error) {
	s := &Section{Line: lineNum}
	rest := strings.TrimPrefix(line, "## ")
	sp := strings.IndexByte(rest, ' ')
	if sp < 0 {
		return nil, fmt.Errorf("%s:%d: section header has no arguments: %q", path, lineNum, line)
	}
	s.Name = rest[:sp]
	rest = rest[sp+1:]

	obj, tail, err := scanJSONObject(rest)
	if err != nil {
		return nil, fmt.Errorf("%s:%d: %w", path, lineNum, err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, obj); err != nil {
		return nil, fmt.Errorf("%s:%d: bad argument JSON: %w", path, lineNum, err)
	}
	s.Args = json.RawMessage(compact.Bytes())
	s.Note = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tail), ";"))
	return s, nil
}

func parseSectionBody(s *Section, block []string) {
	k := 0
	for ; k < len(block); k++ {
		t := strings.TrimSpace(block[k])
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "#") {
			s.Notes = append(s.Notes, strings.TrimSpace(strings.TrimPrefix(t, "#")))
			continue
		}
		break
	}
	if k < len(block) && strings.TrimSpace(block[k]) == "<stamp>" {
		s.HasStamp = true
		k++
	}
	s.Body = strings.TrimRight(strings.Join(block[k:], "\n"), "\n")
}

// Load parses the result-text capture at path.
func Load(path string) ([]Section, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")

	var out []Section
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "## ") {
			continue
		}
		s, err := parseSectionHeader(path, i+1, lines[i])
		if err != nil {
			return nil, err
		}
		var block []string
		j := i + 1
		for ; j < len(lines) && !strings.HasPrefix(lines[j], "## "); j++ {
			block = append(block, lines[j])
		}
		i = j - 1
		parseSectionBody(s, block)
		out = append(out, *s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no sections found", path)
	}
	return out, nil
}

// scanJSONObject returns the leading {...} object in s (balanced, string- and
// escape-aware) and whatever follows it.
func scanJSONObject(s string) (json.RawMessage, string, error) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return nil, "", fmt.Errorf("no argument object in %q", s)
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// ordinary string byte
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return json.RawMessage(s[start : i+1]), s[i+1:], nil
			}
		}
	}
	return nil, "", fmt.Errorf("unterminated argument object in %q", s)
}
