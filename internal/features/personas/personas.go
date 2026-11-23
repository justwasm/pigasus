// Package personas defines the Antigravity onboarding personas, product skins,
// and persona-specific capabilities (Software Engineering vs. Knowledge Work).
//
// The reference language server exposes two primary product skins:
//   - PRODUCT_SKIN_DEVELOPER (1): Software Engineering (full developer tooling)
//   - PRODUCT_SKIN_NON_TECHNICAL (2): Knowledge Work (simplified UI, outputs-centric)
//
// And five standard onboarding personas:
//   - engineering: Software Engineering (PRODUCT_SKIN_DEVELOPER)
//   - knowledge-work: Knowledge Work (PRODUCT_SKIN_NON_TECHNICAL)
//   - data: Data & Analytics (PRODUCT_SKIN_DEVELOPER)
//   - science: Science & Research (PRODUCT_SKIN_NON_TECHNICAL)
//   - design: Design & Creative (PRODUCT_SKIN_NON_TECHNICAL)
package personas

import (
	"strings"
)

// Product skin constants matching jetbox_state_pb.ProductSkin.
const (
	ProductSkinUnspecified  = 0
	ProductSkinDeveloper    = 1
	ProductSkinNonTechnical = 2
)

// Persona ID constants.
const (
	PersonaEngineering   = "engineering"
	PersonaKnowledgeWork = "knowledge-work"
	PersonaData          = "data"
	PersonaScience       = "science"
	PersonaDesign        = "design"
)

// Wire string constants for ProductSkin enum in protojson.
const (
	WireSkinUnspecified  = "PRODUCT_SKIN_UNSPECIFIED"
	WireSkinDeveloper    = "PRODUCT_SKIN_DEVELOPER"
	WireSkinNonTechnical = "PRODUCT_SKIN_NON_TECHNICAL"
)

// Suggestion is an onboarding suggestion action associated with a persona.
type Suggestion struct {
	CustomizationID string `json:"customizationId,omitempty"`
	Label           string `json:"label"`
	Prompt          string `json:"prompt"`
}

// PersonaInfo describes an onboarding persona.
type PersonaInfo struct {
	ID                 string       `json:"id"`
	DisplayName        string       `json:"displayName"`
	Skin               int          `json:"skin"`
	Description        string       `json:"description"`
	Suggestions        []Suggestion `json:"suggestions"`
	RecommendedPlugins []string     `json:"recommendedPlugins,omitempty"`
}

// CustomPersonaInfo describes the fallback custom persona.
type CustomPersonaInfo struct {
	Skin        int          `json:"skin"`
	Description string       `json:"description"`
	Suggestions []Suggestion `json:"suggestions"`
}

// Customization describes plugin metadata returned by GetOnboardingPersonas.
type Customization struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Logo        string `json:"logo,omitempty"`
}

// All returns the canonical 5 personas extracted from upstream Antigravity.
func All() []PersonaInfo {
	return []PersonaInfo{
		{
			ID:          PersonaEngineering,
			DisplayName: "Software Engineering",
			Skin:        ProductSkinDeveloper,
			Description: "Antigravity can review code, trace errors, and ship across your stack.",
			Suggestions: []Suggestion{
				{
					CustomizationID: "antigravity-plugins-official/plugin/chrome-devtools-plugin",
					Label:           "Build me a web app",
					Prompt:          "Build me a web app that does <describe your app>",
				},
				{
					CustomizationID: "antigravity-plugins-official/plugin/gemini-api",
					Label:           "Add a Gemini feature to my app",
					Prompt:          "Suggest and add a Gemini API feature to my open project",
				},
				{
					Label:  "Bring over my setup from other Agentic Developer Tools",
					Prompt: "Import my MCP servers, skills, and instructions from Claude Code, Codex, Cursor, and Gemini CLI into Antigravity",
				},
			},
			RecommendedPlugins: []string{
				"antigravity-plugins-official/plugin/chrome-devtools-plugin",
				"antigravity-plugins-official/plugin/gemini-api",
			},
		},
		{
			ID:          PersonaKnowledgeWork,
			DisplayName: "Knowledge Work",
			Skin:        ProductSkinNonTechnical,
			Description: "Antigravity can turn notes and docs into plans, decks, and action items.",
			Suggestions: []Suggestion{
				{
					CustomizationID: "antigravity-plugins-official/plugin/gcalendar",
					Label:           "Brief me on my meetings every morning",
					Prompt:          "Brief me on my meetings every weekday at 8am",
				},
				{
					CustomizationID: "antigravity-plugins-official/plugin/gdrive",
					Label:           "Catch up on my recent meeting notes",
					Prompt:          "Summarize my meeting notes from the last 7 days in Drive",
				},
				{
					CustomizationID: "antigravity-plugins-official/plugin/gslides",
					Label:           "Turn a doc into a slide deck",
					Prompt:          "Turn <paste a Google Doc link> into a short slide deck with speaker notes",
				},
			},
			RecommendedPlugins: []string{
				"antigravity-plugins-official/plugin/gcalendar",
				"antigravity-plugins-official/plugin/gdrive",
				"antigravity-plugins-official/plugin/gslides",
			},
		},
		{
			ID:          PersonaData,
			DisplayName: "Data & Analytics",
			Skin:        ProductSkinDeveloper,
			Description: "Antigravity can query tables, clean spreadsheets, and chart trends.",
			Suggestions: []Suggestion{
				{
					CustomizationID: "antigravity-plugins-official/plugin/data-agent-kit-plugin",
					Label:           "Check a BigQuery table for anomalies",
					Prompt:          "Check <project.dataset.table> in BigQuery for anomalies vs. the 28-day trend",
				},
				{
					CustomizationID: "antigravity-plugins-official/plugin/gsheets",
					Label:           "Build a dashboard from my sheet",
					Prompt:          "Build a dashboard from <paste a Google Sheet link>",
				},
				{
					CustomizationID: "antigravity-plugins-official/plugin/data-agent-kit-plugin",
					Label:           "Analyze user trends and retention cohorts",
					Prompt:          "Analyze user trends and retention cohorts in <project.dataset.table>",
				},
			},
			RecommendedPlugins: []string{
				"antigravity-plugins-official/plugin/data-agent-kit-plugin",
				"antigravity-plugins-official/plugin/gsheets",
			},
		},
		{
			ID:          PersonaScience,
			DisplayName: "Science",
			Skin:        ProductSkinNonTechnical,
			Description: "Antigravity can review literature, design experiments, and analyze results.",
			Suggestions: []Suggestion{
				{
					CustomizationID: "antigravity-plugins-official/plugin/science",
					Label:           "Synthesize recent preprints and papers on a topic",
					Prompt:          "Synthesize recent preprints and papers on <your research topic>",
				},
				{
					CustomizationID: "antigravity-plugins-official/plugin/science",
					Label:           "Visualize a protein's 3D structure",
					Prompt:          "Visualize the 3D structure of <protein name or PDB id>",
				},
				{
					CustomizationID: "antigravity-plugins-official/plugin/science",
					Label:           "Prioritize genetic targets and pathways for a disease",
					Prompt:          "Prioritize genetic targets and pathways for <disease>",
				},
				{
					CustomizationID: "antigravity-plugins-official/plugin/science",
					Label:           "Analyze small-molecule bioactivity and drug-likeness",
					Prompt:          "Analyze bioactivity and drug-likeness of <compound name or SMILES>",
				},
			},
			RecommendedPlugins: []string{
				"antigravity-plugins-official/plugin/science",
			},
		},
		{
			ID:          PersonaDesign,
			DisplayName: "Design",
			Skin:        ProductSkinNonTechnical,
			Description: "Antigravity can turn your designs into live code, briefs, and assets.",
			Suggestions: []Suggestion{
				{
					CustomizationID: "antigravity-plugins-partners/plugin/figma",
					Label:           "Build a page from my Figma design",
					Prompt:          "Build <paste a Figma frame link> as a responsive web page",
				},
				{
					CustomizationID: "antigravity-plugins-official/plugin/modern-web-guidance-plugin",
					Label:           "Turn a screenshot into a web page",
					Prompt:          "Turn the screenshot I attach into a responsive web page",
				},
				{
					CustomizationID: "antigravity-plugins-official/plugin/gdocs",
					Label:           "Write a design brief from my research",
					Prompt:          "Write a design brief from <paste a Google Doc link>",
				},
			},
			RecommendedPlugins: []string{
				"antigravity-plugins-partners/plugin/figma",
				"antigravity-plugins-official/plugin/modern-web-guidance-plugin",
				"antigravity-plugins-official/plugin/gdocs",
			},
		},
	}
}

// Custom returns the fallback custom persona.
func Custom() CustomPersonaInfo {
	return CustomPersonaInfo{
		Skin:        ProductSkinNonTechnical,
		Description: "Antigravity can draft docs, analyze files, and organize your work.",
		Suggestions: []Suggestion{
			{
				CustomizationID: "antigravity-plugins-official/plugin/gcalendar",
				Label:           "Brief me on my meetings every morning",
				Prompt:          "Brief me on my meetings every weekday at 8am",
			},
			{
				CustomizationID: "antigravity-plugins-official/plugin/gdrive",
				Label:           "Catch up on my recent meeting notes",
				Prompt:          "Summarize my meeting notes from the last 7 days in Drive",
			},
			{
				CustomizationID: "antigravity-plugins-official/plugin/gsheets",
				Label:           "Summarize a spreadsheet",
				Prompt:          "Summarize the key trends in <paste a Google Sheet link>",
			},
		},
	}
}

// AllCustomizations returns the plugin customizations referenced by personas.
func AllCustomizations() []Customization {
	return []Customization{
		{ID: "antigravity-plugins-official/plugin/chrome-devtools-plugin", DisplayName: "Chrome DevTools"},
		{ID: "antigravity-plugins-official/plugin/gemini-api", DisplayName: "Gemini API"},
		{ID: "antigravity-plugins-official/plugin/gcalendar", DisplayName: "Google Calendar"},
		{ID: "antigravity-plugins-official/plugin/gdrive", DisplayName: "Google Drive"},
		{ID: "antigravity-plugins-official/plugin/gslides", DisplayName: "Google Slides"},
		{ID: "antigravity-plugins-official/plugin/data-agent-kit-plugin", DisplayName: "Data Agent Kit"},
		{ID: "antigravity-plugins-official/plugin/gsheets", DisplayName: "Google Sheets"},
		{ID: "antigravity-plugins-official/plugin/science", DisplayName: "Science Tools"},
		{ID: "antigravity-plugins-partners/plugin/figma", DisplayName: "Figma"},
		{ID: "antigravity-plugins-official/plugin/modern-web-guidance-plugin", DisplayName: "Modern Web Guidance"},
		{ID: "antigravity-plugins-official/plugin/gdocs", DisplayName: "Google Docs"},
	}
}

// Resolve normalizes a user input persona name into a canonical PersonaInfo.
func Resolve(name string) (PersonaInfo, bool) {
	norm := strings.ToLower(strings.TrimSpace(name))
	norm = strings.ReplaceAll(norm, "_", "-")
	switch norm {
	case "engineering", "swe", "dev", "developer", "software-engineering", "coding":
		return findPersona(PersonaEngineering)
	case "knowledge-work", "kw", "knowledge", "knowledgework", "non-technical", "nontech", "general":
		return findPersona(PersonaKnowledgeWork)
	case "data", "analytics", "data-analytics":
		return findPersona(PersonaData)
	case "science", "scientific", "biology", "bio":
		return findPersona(PersonaScience)
	case "design", "creative", "ui", "ux":
		return findPersona(PersonaDesign)
	default:
		return PersonaInfo{}, false
	}
}

func findPersona(id string) (PersonaInfo, bool) {
	for _, p := range All() {
		if p.ID == id {
			return p, true
		}
	}
	return PersonaInfo{}, false
}

// ResolveSkin resolves a skin name or alias to its numeric ProductSkin constant.
func ResolveSkin(name string) (int, bool) {
	norm := strings.ToLower(strings.TrimSpace(name))
	norm = strings.ReplaceAll(norm, "_", "-")
	switch norm {
	case "1", "developer", "dev", "swe", "engineering", "coding", "product-skin-developer":
		return ProductSkinDeveloper, true
	case "2", "non-technical", "nontechnical", "nontech", "knowledge-work", "kw", "product-skin-non-technical":
		return ProductSkinNonTechnical, true
	case "0", "unspecified", "product-skin-unspecified":
		return ProductSkinUnspecified, true
	default:
		return ProductSkinUnspecified, false
	}
}

// SkinWire returns the canonical protojson enum name for a ProductSkin.
func SkinWire(skin int) string {
	switch skin {
	case ProductSkinDeveloper:
		return WireSkinDeveloper
	case ProductSkinNonTechnical:
		return WireSkinNonTechnical
	default:
		return WireSkinUnspecified
	}
}

// SkinFromWire parses a raw JSON value (int, float, or string) into a numeric ProductSkin.
func SkinFromWire(raw any) (int, bool) {
	switch v := raw.(type) {
	case float64:
		i := int(v)
		if i >= 0 && i <= 2 {
			return i, true
		}
	case int:
		if v >= 0 && v <= 2 {
			return v, true
		}
	case string:
		switch strings.ToUpper(strings.TrimSpace(v)) {
		case WireSkinDeveloper, "DEVELOPER":
			return ProductSkinDeveloper, true
		case WireSkinNonTechnical, "NON_TECHNICAL", "NON-TECHNICAL":
			return ProductSkinNonTechnical, true
		case WireSkinUnspecified, "UNSPECIFIED":
			return ProductSkinUnspecified, true
		default:
			return ResolveSkin(v)
		}
	}
	return ProductSkinUnspecified, false
}

// GetOnboardingPersonasResponse builds the JSON-RPC response map for GetOnboardingPersonas.
func GetOnboardingPersonasResponse() map[string]any {
	personas := All()
	custom := Custom()
	customizations := AllCustomizations()

	pList := make([]any, len(personas))
	for i, p := range personas {
		sList := make([]any, len(p.Suggestions))
		for j, s := range p.Suggestions {
			item := map[string]any{
				"label":  s.Label,
				"prompt": s.Prompt,
			}
			if s.CustomizationID != "" {
				item["customizationId"] = s.CustomizationID
			}
			sList[j] = item
		}
		pList[i] = map[string]any{
			"id":          p.ID,
			"displayName": p.DisplayName,
			"skin":        p.Skin,
			"description": p.Description,
			"suggestions": sList,
		}
	}

	cSuggestions := make([]any, len(custom.Suggestions))
	for j, s := range custom.Suggestions {
		item := map[string]any{
			"label":  s.Label,
			"prompt": s.Prompt,
		}
		if s.CustomizationID != "" {
			item["customizationId"] = s.CustomizationID
		}
		cSuggestions[j] = item
	}

	customMap := map[string]any{
		"skin":        custom.Skin,
		"description": custom.Description,
		"suggestions": cSuggestions,
	}

	cList := make([]any, len(customizations))
	for i, c := range customizations {
		entry := map[string]any{
			"id":          c.ID,
			"displayName": c.DisplayName,
		}
		if c.Logo != "" {
			entry["logo"] = c.Logo
		}
		cList[i] = entry
	}

	return map[string]any{
		"personas":       pList,
		"customPersona":  customMap,
		"customizations": cList,
	}
}
