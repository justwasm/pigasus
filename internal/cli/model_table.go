package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// modelTable renders the model catalogue the way the reference renders it.
//
// # 【一手】 The reference renders the list two ways, and the difference is
// only visible in a terminal.
//
// stdout is a terminal:
//
//	gemini-3.8-flash-medium   Gemini 3.8 Flash (Medium)
//	claude-opus-4-6-thinking  Claude Opus 4.6 (Thinking)
//
// stdout is a pipe or a file:
//
//	gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)
//	claude-opus-4-6-thinking\tClaude Opus 4.6 (Thinking)
//
// The aligned form is a text/tabwriter with minwidth 0, tabwidth 8 and
// padding 2. The widest catalogue id is 24 characters ("claude-opus-4-6-
// thinking"), so every label starts at column 26 — measured off a pty
// capture of agy 1.4.5, not inferred.
//
// The tab separated form is the machine readable one: one row per model,
// id and label joined by a single tab, no padding. It is what the reference
// writes when stdout is not a terminal, so a caller that pipes `agy models`
// into cut/awk sees the same bytes the reference produces.
func modelTable(w io.Writer, entries []ModelEntry, aligned bool) error {
	if !aligned {
		for _, m := range entries {
			if _, err := fmt.Fprintf(w, "%s\t%s\n", m.ID, m.Label); err != nil {
				return err
			}
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	for _, m := range entries {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", m.ID, m.Label); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// writeModelTable renders the model catalogue for the writer it is given:
// space aligned when that writer is a terminal, tab separated otherwise.
//
// The terminal check is on the writer, not on os.Stdout: the clone's
// subcommands can be pointed at a buffer or a pipe, and the reference's own
// rule is about the stream the catalogue is written to.
func writeModelTable(w io.Writer, entries []ModelEntry) error {
	return modelTable(w, entries, isTerminalWriter(w))
}
