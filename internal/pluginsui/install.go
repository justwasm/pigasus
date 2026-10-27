package pluginsui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/palette"
)

const (
	PluginCursorSpan       = "\x1b[7m\x1b[37m \x1b[0m"
	PluginLocalPlaceholder = " ~/path/to/plugin"
)

// CompleteLocalInstallPath expands the typed path against the filesystem.
func CompleteLocalInstallPath(query string) string {
	q := strings.TrimSpace(query)
	if q == "" || strings.HasSuffix(q, "/") {
		return q
	}
	parent, leaf := filepath.Split(q)
	if parent == "" {
		parent = "."
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		if info, err := os.Stat(q); err == nil && info.IsDir() {
			return q + "/"
		}
		return q
	}
	var matches []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, leaf) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		if info, err := os.Stat(q); err == nil && info.IsDir() {
			return q + "/"
		}
		return q
	}
	sort.Strings(matches)
	return filepath.Join(parent, matches[0]) + "/"
}

// InstallLocalPlugin copies src into ~/.gemini/config/plugins/<name>/
// and writes plugins.<name>.enabled=true into config.json.
func InstallLocalPlugin(src string) error {
	info, err := os.Stat(src)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("directory %q does not contain a valid plugin", src)
	}
	raw, err := os.ReadFile(filepath.Join(src, "plugin.json"))
	if err != nil {
		return fmt.Errorf("directory %q does not contain a valid plugin", src)
	}
	var man struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &man); err != nil {
		return fmt.Errorf("directory %q does not contain a valid plugin", src)
	}
	if strings.TrimSpace(man.Name) == "" {
		man.Name = filepath.Base(src)
	}
	dst := filepath.Join(auth.GeminiConfigHome(), "plugins", man.Name)
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("plugin %q is already installed", man.Name)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	if err := CopyDir(src, dst); err != nil {
		return err
	}
	return auth.SetPluginEnabled(man.Name, true)
}

// CopyDir recursively copies src to dst.
func CopyDir(src, dst string) error {
	return os.CopyFS(dst, os.DirFS(src))
}

// LocalInstallRow renders the inline path prompt row.
func LocalInstallRow(isCursor bool, query string, blinkOn bool) string {
	label := "\x1b[1m+ Install from local directory:\x1b[0m"
	prefix := "  " + label + " "
	if isCursor {
		prefix = palette.BlueOpen + "> " + label + palette.BlueClose + " "
	}
	if query == "" {
		return prefix + PluginCursorSpan + palette.DimGrayOpen + PluginLocalPlaceholder + "\x1b[39m"
	}
	if blinkOn {
		return prefix + query + PluginCursorSpan
	}
	return prefix + query + " "
}

// LocalInstallHint returns the validation hint line below the prompt.
func LocalInstallHint(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	info, err := os.Stat(q)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "  " + palette.SeparatorOpen + "└─ " + palette.DimGrayOpen + "✗ Directory not found (press Tab to complete)\x1b[39m"
		}
		return "  " + palette.SeparatorOpen + "└─ " + palette.DimGrayOpen + "✗ Directory not readable\x1b[39m"
	}
	if !info.IsDir() {
		return "  " + palette.SeparatorOpen + "└─ " + palette.DimGrayOpen + "✗ Path is not a directory\x1b[39m"
	}
	if _, err := os.Stat(filepath.Join(q, "plugin.json")); err != nil {
		return "  " + palette.SeparatorOpen + "└─ " + palette.DimGrayOpen + "✗ Directory does not contain plugin.json (press Tab to complete subdirs)\x1b[39m"
	}
	return ""
}

// LocalInstallLegend is the footer the prompt uses.
func LocalInstallLegend() string {
	return KeyboardHint("enter / ctrl+s", "Install Plugin",
		"tab", "Complete Path", "esc", "Cancel")
}
