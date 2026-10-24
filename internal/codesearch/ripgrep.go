// ripgrep.go — ripgrep --json stream execution and parsing.
package codesearch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultRgCmd returns the default ripgrep invocation. Public so
// tests can call it to assert against the same flag list the
// production path uses.
func DefaultRgCmd(ctx context.Context, root string, a Args) *exec.Cmd {
	args := []string{"--json", "--no-heading", "--no-messages"}
	if a.Literal {
		args = append(args, "--fixed-strings")
	}
	for _, g := range a.Include {
		args = append(args, "-g", g)
	}
	for _, g := range a.Exclude {
		args = append(args, "-g", "!"+g)
	}
	// Always exclude the skip-dirs via a glob negation.
	for d := range SkipDirs {
		args = append(args, "-g", "!"+d)
	}
	// Hidden files are skipped by default.
	args = append(args, "--hidden")
	args = append(args, "-e", a.Query)
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = root
	return cmd
}

func parseRgMatch(row []byte) (Match, bool) {
	if len(row) == 0 {
		return Match{}, false
	}
	var msg rgMessage
	if err := json.Unmarshal(row, &msg); err != nil || msg.Type != "match" {
		return Match{}, false
	}
	m := Match{
		File: filepath.ToSlash(msg.Data.Path.Text),
		Line: msg.Data.LineNumber,
		Col:  1,
		Text: strings.TrimRight(msg.Data.Lines.Text, "\n"),
	}
	if len(msg.Data.Submatches) > 0 {
		start := msg.Data.Submatches[0].Start
		end := msg.Data.Submatches[0].End
		if start < 0 {
			start = 0
		}
		if end > len(m.Text) {
			end = len(m.Text)
		}
		m.Col = utf8RuneCount(m.Text[:start]) + 1
	}
	return m, true
}

func scanRgOutput(ctx context.Context, cmd *exec.Cmd, stdout io.Reader, s *Search) error {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		m, ok := parseRgMatch(scanner.Bytes())
		if !ok {
			continue
		}
		if err := send(ctx, s, m); err != nil {
			_ = cmd.Process.Kill()
			return err
		}
		if s.Limit > 0 && countSent(s) >= MaxHits {
			// rg has likely more matches buffered; ask it to stop.
			_ = cmd.Process.Kill()
			break
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		// Best effort: surface as the search error so the panel can
		// show "falling back to local search" or similar.
		return err
	}
	return nil
}

// runRipgrep shells out to rg --json. The output format is one JSON
// object per line; we only care about type=="match" records.
//
// rg's JSON carries byte offsets into the line (submatches[*].start/end);
// we convert those to rune column numbers so the panel's csFit math keeps
// working (its ellipsis-truncation operates on runes, not bytes).
func (r *Runner) runRipgrep(ctx context.Context, s *Search) error {
	cmd := r.RgBin(ctx, r.Workdir, argsOf(s))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		// Spawn itself failed (e.g. blocked by an endpoint security
		// agent — "operation not permitted"). Surface so Run can
		// fall back to the walk.
		return err
	}
	// Track the child so Cancel() can kill it. Run already maps
	// ctx.Canceled → StateCancelled; Cancel() is a separate
	// convenience for the panel's Esc handler.
	r.attachCmd(cmd)
	defer r.detachCmd(cmd)

	if err := scanRgOutput(ctx, cmd, stdout, s); err != nil {
		return err
	}
	return cmd.Wait()
}

// rgMessage is the shape of one ripgrep --json output line. Only the
// fields we read are present.
type rgMessage struct {
	Type string `json:"type"`
	Data struct {
		Path       rgPath `json:"path"`
		Lines      rgText `json:"lines"`
		LineNumber int    `json:"line_number"`
		Submatches []struct {
			Match rgText `json:"match"`
			Start int    `json:"start"`
			End   int    `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

// rgPath decodes the {"text": "..."} wrapper ripgrep uses.
type rgPath struct {
	Text string `json:"text"`
}

// rgText is the same wrapper for arbitrary text payloads.
type rgText struct {
	Text string `json:"text"`
}
