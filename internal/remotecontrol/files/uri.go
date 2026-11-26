package files

import (
	"net/url"
	"strings"
)

// ParseURIPath preserves the language server's file-URI decoding, also used by
// project/worktree requests. Access control belongs to the installed FS port.
func ParseURIPath(rawURI string) string {
	if strings.HasPrefix(rawURI, "file://") {
		u, err := url.Parse(rawURI)
		if err == nil && u.Path != "" {
			return u.Path
		}
		return strings.TrimPrefix(rawURI, "file://")
	}
	return rawURI
}
