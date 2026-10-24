// help_golden_test.go — the clone's `--help` surface against the
// reference's, byte for byte.
//
// The golden file `testdata/help_1.4.6.txt` is the reference binary's
// own output, captured on this machine:
//
//	$ agy --help
//	$ agy agent --help
//	… one section per command, stdout and stderr separately
//
// Every section was captured against the 1.4.6 binary with a throwaway
// HOME, so the mutating plugin subcommands could not touch the real
// plugin directory. The 1.4.7 binary's help is byte-identical to it
// (【一手】, re-captured at 1.4.7 — 30 commands, stdout and stderr
// separately, same throwaway HOME), so the golden keeps its name and its
// bytes.
//
// The clone renders its help through a different program name
// ("antigravity" in the test rig, "pigasus" in the binary), so the
// comparison normalises that one word. Everything else — the flag
// table, the column widths, the subcommand listings, the usage blocks
// of the hidden subcommands — has to match exactly.
//
// This is the regression guard for the flag-by-flag diff that found
// `--print-partial-response` (a flag the reference does not have), the
// `remote-control serve` row the reference does not list, and the
// `plugin validate` usage block the clone was missing.
package cli

import (
	"os"
	"strings"
	"testing"
)

// helpGoldenSection is one command's captured output.
type helpGoldenSection struct {
	command string
	stdout  string
	stderr  string
}

// set records one stream's bytes for the section.
func (s *helpGoldenSection) set(stream, body string) {
	if stream == "stdout" {
		s.stdout = body
		return
	}
	s.stderr = body
}

// loadHelpGolden parses the golden file into its sections.
//
// The format is a flat sequence of blocks:
//
//	===BEGIN agy agent --help stdout===
//	…raw bytes…
//	===END===
//
// The raw bytes are kept verbatim (including trailing newlines and
// blank lines), so the comparison is a byte comparison.
func loadHelpGolden(t *testing.T) []helpGoldenSection {
	t.Helper()
	raw, err := os.ReadFile("testdata/help_1.4.6.txt")
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	var sections []helpGoldenSection
	index := map[string]int{}
	for _, chunk := range strings.Split(string(raw), "===BEGIN ")[1:] {
		headerEnd := strings.Index(chunk, "===\n")
		if headerEnd < 0 {
			t.Fatalf("malformed golden block header: %q", chunk)
		}
		fields := strings.Fields(chunk[:headerEnd])
		// fields = ["agy", <command words...>, <stream>]
		if len(fields) < 3 {
			t.Fatalf("malformed golden header %q", chunk[:headerEnd])
		}
		command := strings.Join(fields[1:len(fields)-1], " ")
		stream := fields[len(fields)-1]
		body := chunk[headerEnd+len("===\n"):]
		bodyEnd := strings.Index(body, "===END===")
		if bodyEnd < 0 {
			t.Fatalf("unterminated golden block for %q", chunk[:headerEnd])
		}
		body = body[:bodyEnd]
		key := command
		if idx, ok := index[key]; ok {
			sections[idx].set(stream, body)
			continue
		}
		index[key] = len(sections)
		sections = append(sections, helpGoldenSection{command: command})
		sections[len(sections)-1].set(stream, body)
	}
	if len(sections) == 0 {
		t.Fatal("golden file has no sections")
	}
	return sections
}

// TestHelpSurfaceMatchesReference1416 compares the clone's help output
// with the reference's, for the top level, the twelve visible
// subcommands and their sub-subcommands.
func TestHelpSurfaceMatchesReference1416(t *testing.T) {
	for _, section := range loadHelpGolden(t) {
		section := section
		t.Run(section.command, func(t *testing.T) {
			// Never pass a nil slice: ExecuteArgs falls back to
			// os.Args[1:] for nil, which is the go test binary's
			// own flags.
			args := strings.Fields(section.command)
			out, errb, _ := runJetski(t, args...)
			// The test rig names the program "antigravity"; the golden
			// file was captured from the binary that calls itself
			// "agy". That one word is the only allowed difference.
			out = strings.ReplaceAll(out, "antigravity", "agy")
			errb = strings.ReplaceAll(errb, "antigravity", "agy")
			if out != section.stdout {
				t.Errorf("stdout mismatch\n got: %q\nwant: %q", out, section.stdout)
			}
			if errb != section.stderr {
				t.Errorf("stderr mismatch\n got: %q\nwant: %q", errb, section.stderr)
			}
		})
	}
}
