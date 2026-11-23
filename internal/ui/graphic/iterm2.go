package graphic

import (
	"encoding/base64"
	"strconv"
	"strings"
)

// EncodeITerm2 encodes raw image bytes (e.g. PNG, JPEG, GIF) into an iTerm2
// OSC 1337 inline image escape sequence.
func EncodeITerm2(imgBytes []byte, opts ImageOptions) string {
	if len(imgBytes) == 0 {
		return ""
	}

	encoded := base64.StdEncoding.EncodeToString(imgBytes)

	var b strings.Builder
	b.Grow(len(encoded) + 96)
	b.WriteString("\x1b]1337;File=inline=1;size=")
	b.WriteString(strconv.Itoa(len(imgBytes)))
	if opts.Name != "" {
		b.WriteString(";name=")
		b.WriteString(base64.StdEncoding.EncodeToString([]byte(opts.Name)))
	}
	if opts.Cols > 0 {
		b.WriteString(";width=")
		b.WriteString(strconv.Itoa(opts.Cols))
	}
	if opts.Rows > 0 {
		b.WriteString(";height=")
		b.WriteString(strconv.Itoa(opts.Rows))
	}
	b.WriteByte(':')
	b.WriteString(encoded)
	b.WriteByte('\x07')

	return b.String()
}
