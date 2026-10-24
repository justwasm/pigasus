package pluginsui

import (
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/plugins"
)

// FilterDiscover returns the Discover items matching query across marketplaces.
// When query is empty, it returns all marketplace items.
func FilterDiscover(mkts []plugins.Marketplace, query string) []plugins.Row {
	if query == "" {
		return marketplaceItems(mkts)
	}
	q := strings.ToLower(query)
	var out []plugins.Row
	for _, mk := range mkts {
		prefix := strings.ToLower(mk.Name) + " "
		for _, p := range mk.Items {
			if strings.Contains(prefix+strings.ToLower(p.Name+" "+p.Desc), q) {
				out = append(out, p)
			}
		}
	}
	return out
}

func marketplaceItems(mkts []plugins.Marketplace) []plugins.Row {
	var out []plugins.Row
	for _, mk := range mkts {
		out = append(out, mk.Items...)
	}
	return out
}

// FilterInstalled returns the Installed rows that match query (name + desc).
// The empty query returns rows unchanged.
func FilterInstalled(rows []plugins.Row, query string) []plugins.Row {
	if query == "" {
		return rows
	}
	q := strings.ToLower(query)
	var out []plugins.Row
	for _, p := range rows {
		if strings.Contains(strings.ToLower(p.Name+" "+p.Desc), q) {
			out = append(out, p)
		}
	}
	return out
}

// FilterInstalledEnabledOnly returns rows filtered by their enabled flag if enabledOnly is true.
func FilterInstalledEnabledOnly(all []plugins.Row, enabledOnly bool) []plugins.Row {
	if !enabledOnly {
		return all
	}
	out := make([]plugins.Row, 0, len(all))
	for _, r := range all {
		on, err := auth.PluginEnabled(r.Name)
		if err != nil || !on {
			continue
		}
		out = append(out, r)
	}
	return out
}
