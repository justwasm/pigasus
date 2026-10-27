package pluginsui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/plugins"
	"github.com/justwasm/pigasus/internal/textwrap"
	"github.com/justwasm/pigasus/palette"
)

const (
	PluginColor253C = "\x1b[39m"
	PluginGray244   = "\x1b[38;5;244m"
	PluginGray244C  = "\x1b[0m"
	PanelGray238    = "\x1b[38;5;238m"
	PanelGray238C   = "\x1b[0m"
)

// PluginColor253 is the panel's rule colour — the selected scheme's
// border.
var PluginColor253 = palette.SeparatorOpen

// KeyboardHint formats key-action pairs with the recovered reference palette.
func KeyboardHint(pairs ...string) string {
	var b strings.Builder
	b.WriteString("\x1b[1mKeyboard: \x1b[0m")
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(palette.BlueOpen)
		b.WriteString(pairs[i])
		b.WriteString(palette.BlueClose)
		b.WriteString(" ")
		b.WriteString(palette.DimGrayOpen)
		b.WriteString(pairs[i+1])
		b.WriteString(palette.DimGrayClose)
	}
	return b.String()
}

// TabHeader renders the Installed/Discover tab bar row.
func TabHeader(tab, nInstalled, nDiscover int) string {
	return "  " +
		TabLabel("Installed", nInstalled, tab == 0) + "    " +
		TabLabel("Discover", nDiscover, tab == 1) + " " +
		PluginColor253 + "  (tab to cycle)" + PluginColor253C
}

// TabLabel renders one tab label.
func TabLabel(label string, n int, active bool) string {
	count := "(" + strconv.Itoa(n) + ")"
	if !active {
		return PluginColor253 + label + " " + count + PluginColor253C
	}
	return "\x1b[1;4m" + palette.BlueOpen + label + "\x1b[0;4m" + palette.BlueOpen + " " +
		"\x1b[1m" + count + "\x1b[0m"
}

// Badge renders the [enabled] or [disabled] tag.
func Badge(enabled bool) string {
	if enabled {
		return palette.AgentFgOpen + "[enabled] \x1b[39m"
	}
	return PluginColor253 + "[disabled]" + PluginColor253C
}

// BlockedBadge renders the [blocked] tag when global plugin kill-switch is active.
func BlockedBadge() string {
	return "\x1b[38;5;166m[blocked] \x1b[39m"
}

// RowState returns "blocked", "enabled", or "disabled".
func RowState(enabled bool) string {
	if auth.PluginsBlocked() {
		return "blocked"
	}
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// RowBadge returns the badge matching RowState.
func RowBadge(enabled bool) string {
	switch RowState(enabled) {
	case "blocked":
		return BlockedBadge()
	case "enabled":
		return Badge(true)
	default:
		return Badge(false)
	}
}

// InstalledLine renders one Installed plugin row.
func InstalledLine(isCursor, enabled bool, name, desc string, max int) string {
	desc = TruncateTo(desc, max-(14+len(name)))
	b := RowBadge(enabled)
	if isCursor {
		return palette.BlueOpen + "> " + b + " " +
			"\x1b[1m" + palette.BlueOpen + name + "\x1b[0m" + " " +
			palette.DimGrayOpen + desc + palette.DimGrayClose
	}
	return "  " + b + " " +
		"\x1b[1m" + name + "\x1b[0m" + " " +
		palette.DimGrayOpen + desc + palette.DimGrayClose
}

// DiscoverLine renders one Discover plugin row.
func DiscoverLine(isCursor, installed bool, name, desc string, max int) string {
	desc = TruncateTo(desc, max-(5+len(name)))
	bulletColor := PluginColor253
	if installed {
		bulletColor = palette.AgentFgOpen
	}
	bullet := bulletColor + "●" + PluginColor253C
	if isCursor {
		return palette.BlueOpen + "> " + bullet + " " +
			"\x1b[1m" + palette.BlueOpen + name + "\x1b[0m" + " " +
			palette.DimGrayOpen + desc + palette.DimGrayClose
	}
	return "  " + bullet + " " +
		"\x1b[1m" + name + "\x1b[0m" + " " +
		palette.DimGrayOpen + desc + palette.DimGrayClose
}

// LocalRow renders Discover's "+ Install from local directory..." row.
func LocalRow(isCursor bool) string {
	text := "\x1b[1m+ Install from local directory...\x1b[0m"
	if isCursor {
		return palette.BlueOpen + "> " + text
	}
	return "  " + text
}

// DetailLine formats one indented detail row.
func DetailLine(label, value string) string {
	return "     " + PluginColor253 + " " + PadRight(label, 13) + value + PluginColor253C
}

// PadRight left-justifies s in a field of width n.
func PadRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// TruncateTo shortens s to at most avail columns, appending "..." when it has
// to cut. avail below four columns is treated as "nothing fits".
func TruncateTo(s string, avail int) string {
	if avail < 4 || len(s) <= avail {
		return s
	}
	return s[:avail-3] + "..."
}

// WrapWords wraps s into lines not exceeding width on single space boundaries.
//
// The wrap is the shared one (internal/textwrap), which is
// charmbracelet/x/ansi's word wrap — the library the reference's own
// binary embeds. Two things change with it: the budget is counted in
// terminal cells rather than in bytes (so a row of CJK holds half as
// many characters as a row of ASCII), and the rows are the library's.
//
// The reference's own rule holds: a word that is wider than the budget
// is emitted whole. It does not hyphenate it.
func WrapWords(s string, width int) []string {
	return textwrap.Words(s, width)
}

// WrappedDetail renders a labelled detail line whose value is word-wrapped.
func WrappedDetail(label, value string, max int) []string {
	width := max - 19
	if width < 1 {
		width = 1
	}
	chunks := WrapWords(value, width)
	out := []string{DetailLine(label, chunks[0])}
	for _, c := range chunks[1:] {
		out = append(out, strings.Repeat(" ", 18)+PluginColor253+" "+c+PluginColor253C)
	}
	return out
}

// Components returns comma-separated subdirectory names in the plugin cache.
func Components(name string, mkts []plugins.Marketplace) string {
	dir := filepath.Join(auth.AntigravityConfigHome(), "cache", "marketplaces",
		plugins.MarketplaceOf(name, mkts), "plugins", name, plugins.MarketplaceVersion(name, mkts))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "loading..."
	}
	var comps []string
	for _, e := range entries {
		if e.IsDir() {
			comps = append(comps, e.Name())
		}
	}
	sort.Strings(comps)
	if len(comps) == 0 {
		return "loading..."
	}
	return strings.Join(comps, ", ")
}

// DiscoverDetails renders the detail block under a Discover row.
func DiscoverDetails(name string, mkts []plugins.Marketplace, max int) []string {
	out := WrappedDetail("Description:", plugins.DescFor(name, mkts), max)
	out = append(out, DetailLine("Components:", Components(name, mkts)))
	out = append(out, DetailLine("Agents:", plugins.UnpackedAgents(name, mkts)))
	out = append(out, DetailLine("Marketplace:", plugins.MarketplaceOf(name, mkts)))
	out = append(out, DetailLine("Version:", plugins.MarketplaceVersion(name, mkts)))
	return out
}

// InstalledDetails renders the detail block under an Installed row.
func InstalledDetails(name string, mkts []plugins.Marketplace) []string {
	man := plugins.ReadManifest(name)
	skills := strings.Join(plugins.Skills(name), ", ")
	agents := strings.Join(plugins.Agents(name), ", ")
	if agents == "" {
		agents = "-"
	}
	version := man.Version
	if version == "" {
		version = "-"
	}
	return []string{
		DetailLine("Description:", plugins.DescFor(name, mkts)),
		DetailLine("Skills:", skills),
		DetailLine("Agents:", agents),
		DetailLine("Marketplace:", plugins.MarketplaceOf(name, mkts)),
		DetailLine("Path:", plugins.Dir(name)),
		DetailLine("Version:", version),
	}
}

// InstalledEmptyNote renders the message when no installed plugins match.
func InstalledEmptyNote(searchCommitted, enabledOnly bool, count int) []string {
	if count > 0 {
		return nil
	}
	if searchCommitted {
		return []string{
			PanelGray238 + "  No installed plugins match your search." + PanelGray238C,
			"",
		}
	}
	if enabledOnly {
		return []string{
			PanelGray238 + "  No enabled plugins — press `/` to show all." + PanelGray238C,
			"",
		}
	}
	return nil
}

// DiscoverLegend is the Discover footer.
func DiscoverLegend(onLocal, search bool) string {
	esc := "Exit"
	if search {
		esc = "Exit Search"
	}
	args := []string{"↑/↓", "Navigate", "←/→", "Page", "tab", "Cycle Tabs"}
	if onLocal {
		args = append(args, "enter / ctrl+s", "Enter Local Path")
	} else {
		args = append(args, "enter", "Expand Details", "ctrl+s", "Install")
	}
	args = append(args, "esc", esc)
	return KeyboardHint(args...)
}

// InstalledLegend is the Installed tab footer.
func InstalledLegend(search, committed bool) string {
	esc := "Exit"
	enter := "Expand Details"
	if search {
		esc = "Exit Search"
		if !committed {
			enter = "Search"
		}
	}
	return KeyboardHint("↑/↓", "Navigate", "←/→", "Page",
		"tab", "Cycle Tabs", "enter", enter,
		"space", "Toggle Enable",
		"ctrl+s", "Uninstall", "esc", esc)
}
