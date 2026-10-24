// searchbox.go — cycle 204: search box rendering for /plugin panel.
package pluginsui

const (
	PluginSearchGray  = "\x1b[38;2;88;110;117m"
	PluginSearchGrayC = "\x1b[39m"

	PluginSearchCursor = "\x1b[7m\x1b[37m \x1b[0m"
	PluginSearchPrefix = "  Search: "

	PluginInstalledSearchHint              = "  (enter to search)"
	PluginInstalledSearchHintCommitted     = "  (type to continue searching)"
	PluginInstalledSearchHintSpan          = PluginSearchGray + PluginInstalledSearchHint + PluginSearchGrayC
	PluginInstalledSearchHintCommittedSpan = PluginSearchGray + PluginInstalledSearchHintCommitted + PluginSearchGrayC
)

// SearchBoxLine renders the plugin panel search box line for Installed and Discover tabs.
func SearchBoxLine(isInstalled, isDiscover, search, queryCommitted, queryAsleep, blinkOn bool, query string) string {
	if isInstalled && search {
		if queryCommitted && queryAsleep {
			return PluginSearchGray + PluginSearchPrefix + PluginSearchGrayC +
				query + " " + PluginInstalledSearchHintCommittedSpan
		}
		body := " " + query
		if blinkOn {
			body += PluginSearchCursor
		} else {
			body += " "
		}
		if queryCommitted {
			body += PluginInstalledSearchHintCommittedSpan
		} else {
			body += PluginInstalledSearchHintSpan
		}
		return PluginSearchGray + PluginSearchPrefix + PluginSearchGrayC + body
	}
	if isDiscover && search {
		if blinkOn {
			return PluginSearchGray + PluginSearchPrefix + PluginSearchGrayC + " " +
				query + PluginSearchCursor
		}
		return PluginSearchGray + PluginSearchPrefix + PluginSearchGrayC + " " +
			query + " "
	}
	return PluginSearchGray + "  Type to search plugins..." + PluginSearchGrayC
}
