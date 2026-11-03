// filter.go — tab and search filtering for the /provider panel.
package providerpanel

import (
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/features/models"
)

// TabKind represents the active view tab in the provider panel.
type TabKind int

const (
	// TabConfigured shows configured custom providers, their models, and built-ins.
	TabConfigured TabKind = iota
	// TabPresets shows the Catwalk presets catalog.
	TabPresets
	// TabAll shows configured providers, built-ins, and presets.
	TabAll
)

// FilterRows filters the built rows by active tab and search query.
// It retains group headers if and only if at least one item under that group matches.
func FilterRows(rows []Row, providers map[string]models.ProviderConfig, tab TabKind, query string) []Row {
	return FilterRowsWithCollapse(rows, providers, tab, query, nil)
}

// FilterRowsWithCollapse filters rows taking the collapsed state into account.
// Providers and built-in groups default to expanded.
// When query is non-empty, matching items are automatically expanded.
func FilterRowsWithCollapse(rows []Row, providers map[string]models.ProviderConfig, tab TabKind, query string, collapsed map[string]bool) []Row {
	q := strings.ToLower(strings.TrimSpace(query))

	// Step 1: Filter by Tab
	var tabRows []Row
	switch tab {
	case TabConfigured:
		for _, r := range rows {
			if r.Kind == RowPreset {
				continue
			}
			tabRows = append(tabRows, r)
		}
	case TabPresets:
		for _, r := range rows {
			if r.Kind == RowPreset {
				// In Presets tab, omit the redundant group header row
				if r.Model == "" && r.Label == "" {
					continue
				}
				tabRows = append(tabRows, r)
			}
		}
	case TabAll:
		tabRows = append(tabRows, rows...)
	default:
		tabRows = append(tabRows, rows...)
	}

	if q == "" {
		var out []Row
		n := len(tabRows)
		i := 0
		for i < n {
			r := tabRows[i]
			switch r.Kind {
			case RowProvider:
				pID := r.Provider
				isExp := collapsed == nil || !collapsed[pID]
				r.Expanded = isExp
				out = append(out, r)
				j := i + 1
				for j < n && tabRows[j].Kind == RowModel && tabRows[j].Provider == pID {
					if isExp {
						out = append(out, tabRows[j])
					}
					j++
				}
				i = j
			case RowBuiltin:
				if r.Model == "" {
					isExp := collapsed == nil || !collapsed[r.Provider]
					r.Expanded = isExp
					out = append(out, r)
					j := i + 1
					for j < n && tabRows[j].Kind == RowBuiltin && tabRows[j].Model != "" {
						if isExp {
							out = append(out, tabRows[j])
						}
						j++
					}
					i = j
				} else {
					out = append(out, r)
					i++
				}
			default:
				out = append(out, r)
				i++
			}
		}
		return out
	}

	// Step 2: Relevance scoring for Catwalk preset providers
	searchIdx := models.CatwalkSearchIndex()
	tokens := strings.Fields(q)

	scorePreset := func(presetID, label string) int {
		pid := strings.ToLower(presetID)
		if pid == q {
			return 100 // exact ID match
		}
		entry, ok := searchIdx[pid]
		var name string
		if ok {
			name = entry.NameLower
			if name == q {
				return 95 // exact Name match
			}
		}
		if strings.HasPrefix(pid, q) {
			return 80 // ID prefix match
		}
		if name != "" && strings.HasPrefix(name, q) {
			return 75 // Name prefix match
		}
		if strings.Contains(pid, q) {
			return 60 // ID substring match
		}
		if name != "" && strings.Contains(name, q) {
			return 55 // Name substring match
		}
		if strings.Contains(strings.ToLower(label), q) {
			return 50 // Label match
		}
		if ok {
			for i, mid := range entry.ModelIDs {
				mname := ""
				if i < len(entry.ModelNames) {
					mname = entry.ModelNames[i]
				}
				if mid == q || mname == q {
					return 40 // exact model match
				}
				if strings.HasPrefix(mid, q) || (mname != "" && strings.HasPrefix(mname, q)) {
					return 30 // prefix model match
				}
				if strings.Contains(mid, q) || (mname != "" && strings.Contains(mname, q)) {
					return 20 // substring model match
				}
			}

			// Multi-token match: all tokens present in provider ID, Name, or model IDs/Names
			if len(tokens) > 1 {
				allMatch := true
				for _, tok := range tokens {
					matchedTok := strings.Contains(pid, tok) || (name != "" && strings.Contains(name, tok))
					if !matchedTok {
						for i, mid := range entry.ModelIDs {
							mname := ""
							if i < len(entry.ModelNames) {
								mname = entry.ModelNames[i]
							}
							if strings.Contains(mid, tok) || (mname != "" && strings.Contains(mname, tok)) {
								matchedTok = true
								break
							}
						}
					}
					if !matchedTok {
						allMatch = false
						break
					}
				}
				if allMatch {
					return 35 // multi-token match
				}
			}
		}
		return 0 // no match
	}

	// Process rows group by group
	var out []Row
	n := len(tabRows)
	i := 0
	for i < n {
		r := tabRows[i]
		switch r.Kind {
		case RowProvider:
			pID := r.Provider
			pConfig := providers[pID]
			provMatches := strings.Contains(strings.ToLower(pID), q) ||
				strings.Contains(strings.ToLower(pConfig.Name), q) ||
				strings.Contains(strings.ToLower(pConfig.WireType()), q) ||
				strings.Contains(strings.ToLower(pConfig.Endpoint()), q)

			var modelRows []Row
			j := i + 1
			for j < n && tabRows[j].Kind == RowModel && tabRows[j].Provider == pID {
				modelRows = append(modelRows, tabRows[j])
				j++
			}

			if provMatches {
				r.Expanded = true
				out = append(out, r)
				out = append(out, modelRows...)
			} else {
				var matchedModels []Row
				for _, mr := range modelRows {
					mMatches := strings.Contains(strings.ToLower(mr.Model), q)
					for _, pm := range pConfig.EffectiveModels() {
						if pm.ID == mr.Model {
							if strings.Contains(strings.ToLower(pm.Name), q) ||
								strings.Contains(strings.ToLower(pm.Model), q) {
								mMatches = true
							}
							break
						}
					}
					if mMatches {
						matchedModels = append(matchedModels, mr)
					}
				}
				if len(matchedModels) > 0 {
					r.Expanded = true
					out = append(out, r)
					out = append(out, matchedModels...)
				}
			}
			i = j

		case RowBuiltin:
			if r.Model == "" {
				var matchedBuiltins []Row
				j := i + 1
				for j < n && tabRows[j].Kind == RowBuiltin && tabRows[j].Model != "" {
					bm := tabRows[j]
					if strings.Contains(strings.ToLower(bm.Model), q) ||
						strings.Contains(strings.ToLower(bm.Label), q) {
						matchedBuiltins = append(matchedBuiltins, bm)
					}
					j++
				}
				if len(matchedBuiltins) > 0 {
					r.Expanded = true
					out = append(out, r)
					out = append(out, matchedBuiltins...)
				}
				i = j
			} else {
				if strings.Contains(strings.ToLower(r.Model), q) || strings.Contains(strings.ToLower(r.Label), q) {
					out = append(out, r)
				}
				i++
			}

		case RowPreset:
			type scoredPreset struct {
				row   Row
				score int
			}

			if r.Model == "" && r.Label == "" {
				// Presets group header: collect following presets and sort by relevance
				var matchedPresets []scoredPreset
				j := i + 1
				for j < n && tabRows[j].Kind == RowPreset && tabRows[j].Label != "" {
					pr := tabRows[j]
					if s := scorePreset(pr.Provider, pr.Label); s > 0 {
						matchedPresets = append(matchedPresets, scoredPreset{row: pr, score: s})
					}
					j++
				}
				if len(matchedPresets) > 0 {
					sort.SliceStable(matchedPresets, func(a, b int) bool {
						return matchedPresets[a].score > matchedPresets[b].score
					})
					out = append(out, r)
					for _, sp := range matchedPresets {
						out = append(out, sp.row)
					}
				}
				i = j
			} else {
				// Flat preset rows (e.g. inside TabPresets)
				// Collect all remaining preset rows to sort them by relevance
				var presetBatch []scoredPreset
				j := i
				for j < n && tabRows[j].Kind == RowPreset && tabRows[j].Label != "" {
					pr := tabRows[j]
					if s := scorePreset(pr.Provider, pr.Label); s > 0 {
						presetBatch = append(presetBatch, scoredPreset{row: pr, score: s})
					}
					j++
				}
				sort.SliceStable(presetBatch, func(a, b int) bool {
					return presetBatch[a].score > presetBatch[b].score
				})
				for _, sp := range presetBatch {
					out = append(out, sp.row)
				}
				i = j
			}

		default:
			i++
		}
	}

	return out
}
