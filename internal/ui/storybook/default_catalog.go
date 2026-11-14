package storybook

// DefaultCatalog creates and populates a new Catalog with all component stories across the 6 architectural categories.
func DefaultCatalog() *Catalog {
	c := NewCatalog()
	RegisterFoundationsStories(c)
	RegisterComposerStories(c)
	RegisterPopupsStories(c)
	RegisterToolStepsStories(c)
	RegisterPanelsStories(c)
	RegisterPanelsExtendedStories(c)
	RegisterManagementStories(c)
	RegisterCollaborationStories(c)
	RegisterAuthFeedbackStories(c)
	RegisterAdvancedStepsStories(c)
	RegisterStatusStories(c)
	RegisterStatusExtendedStories(c)
	RegisterAtomicStories(c)
	RegisterHerdrStories(c)
	return c
}
