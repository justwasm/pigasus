package livechat

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

//go:embed web
var embeddedWebFS embed.FS

// resolveWebFileSystem returns an http.FileSystem and a description string of where assets are loaded from.
// Priority:
// 1. Explicit customDir if specified and valid
// 2. Development disk directories (cmd/livechat/web or demo/web) if present with index.html
// 3. Embedded binary filesystem (//go:embed web)
func resolveWebFileSystem(customDir string) (http.FileSystem, string) {
	if customDir != "" {
		if fi, err := os.Stat(filepath.Join(customDir, "index.html")); err == nil && !fi.IsDir() {
			abs, _ := filepath.Abs(customDir)
			return http.Dir(abs), abs + " (custom directory)"
		}
	}

	candidates := []string{
		"internal/ui/livechat/web",
		"cmd/livechat/web",
		"demo/web",
		filepath.Join(os.Getenv("OPENAGY_ROOT"), "internal/ui/livechat/web"),
		filepath.Join(os.Getenv("OPENAGY_ROOT"), "cmd/livechat/web"),
		filepath.Join(os.Getenv("OPENAGY_ROOT"), "demo/web"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(filepath.Join(c, "index.html")); err == nil && !fi.IsDir() {
			abs, _ := filepath.Abs(c)
			return http.Dir(abs), abs + " (live disk)"
		}
	}

	sub, err := fs.Sub(embeddedWebFS, "web")
	if err == nil {
		return http.FS(sub), "embedded binary assets"
	}

	return http.FS(embeddedWebFS), "embedded binary assets (root)"
}
