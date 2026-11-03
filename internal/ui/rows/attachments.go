// attachments.go — the attachment indicator's rows.
//
// The indicator sits above the composer and tells the user what the next
// turn will carry: the paperclip glyph, how many attachments there are,
// and — when the user expanded it — one line per attachment.
//
// The four shapes are the reference's own:
//
//   - the collapsed one, with the single attachment named:
//     "  ▸ 📎 1 media attached (name, FORMAT, size)  (ctrl+o to expand)";
//   - the collapsed one, with many attachments:
//     "  ▸ 📎 N media attached  (ctrl+o to expand)";
//   - the expanded header:
//     "  ▾ 📎 N Media(s) attached  (ctrl+o to collapse)";
//   - the expanded item:
//     "    N. name · FORMAT · size".
//
// The rows are dim gray, and the colour is read from the palette when the
// row is drawn, so the rows follow the selected colour scheme.

package rows

import (
	"fmt"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/palette"
)

// AttachmentRows renders the attachment indicator's rows.
//
// media is the attachments of the next turn, and expanded is the
// composer's own state: the collapsed indicator is a single row, and the
// expanded one is a header plus a row per attachment.
//
// The name is the shape's, not the caller's: the indicator is the
// transcript's own shape, and another caller that shows the same bytes
// would be wrong to be told it was "the composer's media rows".
func AttachmentRows(media []clipboard.Media, expanded bool) []string {
	n := len(media)
	if n == 0 {
		return nil
	}
	if !expanded {
		if n == 1 {
			med := media[0]
			row := fmt.Sprintf("  ▸ 📎 1 media attached (%s, %s, %s)  (ctrl+o to expand)",
				med.Filename, med.FormatName(), clipboard.FormatBytes(med.ByteSize()))
			return []string{dimRow(row)}
		}
		row := fmt.Sprintf("  ▸ 📎 %d media attached  (ctrl+o to expand)", n)
		return []string{dimRow(row)}
	}

	rows := make([]string, 0, n+1)
	header := fmt.Sprintf("  ▾ 📎 %d Media(s) attached  (ctrl+o to collapse)", n)
	rows = append(rows, dimRow(header))
	for i, med := range media {
		row := fmt.Sprintf("    %d. %s · %s · %s",
			i+1, med.Filename, med.FormatName(), clipboard.FormatBytes(med.ByteSize()))
		rows = append(rows, dimRow(row))
	}
	return rows
}

// dimRow renders one line of text in the dim-gray span.
func dimRow(text string) string {
	return palette.DimGrayOpen + text + palette.DimGrayClose
}
