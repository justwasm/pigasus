package storybook

// Catalog holds all registered stories.
type Catalog struct {
	stories      []Story
	byID         map[string]Story
	byCategory   map[Category][]Story
	collapsedCat map[Category]bool
}

// NewCatalog constructs an empty story catalog.
func NewCatalog() *Catalog {
	return &Catalog{
		byID:         make(map[string]Story),
		byCategory:   make(map[Category][]Story),
		collapsedCat: make(map[Category]bool),
	}
}

// Register adds a Story to the catalog.
func (c *Catalog) Register(s Story) {
	c.stories = append(c.stories, s)
	c.byID[s.ID] = s
	c.byCategory[s.Category] = append(c.byCategory[s.Category], s)
}

// Stories returns all stories in order.
func (c *Catalog) Stories() []Story {
	return c.stories
}

// StoriesByCategory returns the stories belonging to a category.
func (c *Catalog) StoriesByCategory(cat Category) []Story {
	return c.byCategory[cat]
}

// Find looks up a story by ID.
func (c *Catalog) Find(id string) (Story, bool) {
	s, ok := c.byID[id]
	return s, ok
}

// ToggleCategory toggles whether a category is collapsed in the sidebar.
func (c *Catalog) ToggleCategory(cat Category) {
	c.collapsedCat[cat] = !c.collapsedCat[cat]
}

// IsCategoryCollapsed reports whether a category is collapsed.
func (c *Catalog) IsCategoryCollapsed(cat Category) bool {
	return c.collapsedCat[cat]
}

// NavItem represents a row in the sidebar list (either a category header or a story).
type NavItem struct {
	IsCategory bool
	Category   Category
	Story      *Story
}

// VisibleNavItems returns the flattened list of navigable rows based on collapse states and search query.
func (c *Catalog) VisibleNavItems(filterQuery string) []NavItem {
	var items []NavItem
	filterLower := ""
	if filterQuery != "" {
		filterLower = filterQuery
	}

	for _, cat := range AllCategories {
		stories := c.byCategory[cat]
		if len(stories) == 0 {
			continue
		}

		// When filtering, only include stories matching filter
		var matchingStories []*Story
		for i := range stories {
			s := &stories[i]
			if filterLower == "" || containsIgnoreCase(s.Name, filterLower) || containsIgnoreCase(s.ID, filterLower) || containsIgnoreCase(s.UpstreamSym, filterLower) {
				matchingStories = append(matchingStories, s)
			}
		}

		if filterLower != "" && len(matchingStories) == 0 {
			continue
		}

		items = append(items, NavItem{
			IsCategory: true,
			Category:   cat,
		})

		// If collapsed (and not searching), don't show stories
		if filterLower == "" && c.collapsedCat[cat] {
			continue
		}

		for _, s := range matchingStories {
			items = append(items, NavItem{
				IsCategory: false,
				Category:   cat,
				Story:      s,
			})
		}
	}
	return items
}

func containsIgnoreCase(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c1 := s[i+j]
			c2 := substr[j]
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 32
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 32
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
