package storybook

import (
	tea "charm.land/bubbletea/v2"
)

// Category groups stories by architectural domain.
type Category string

const (
	CategoryFoundations Category = "1. Foundations"
	CategoryComposer    Category = "2. Composer & Inputs"
	CategoryPopups      Category = "3. Autocomplete Popups"
	CategoryToolSteps   Category = "4. Tool Steps & Execution"
	CategoryPanels      Category = "5. Panels & Dialogs"
	CategoryStatus      Category = "6. Status & Overlays"
	CategoryWorkspace   Category = "7. Workspace & Multiplexer (Herdr)"
)

// AtomicLevel defines the Atomic Design hierarchy level for the component.
type AtomicLevel string

const (
	LevelAtom     AtomicLevel = "Atom"
	LevelMolecule AtomicLevel = "Molecule"
	LevelOrganism AtomicLevel = "Organism"
	LevelModal    AtomicLevel = "Modal"
)

// LevelTag returns a full uppercase badge for the atomic design level.
func (l AtomicLevel) LevelTag() string {
	switch l {
	case LevelAtom:
		return "[ATOM]"
	case LevelMolecule:
		return "[MOLECULE]"
	case LevelOrganism:
		return "[ORGANISM]"
	case LevelModal:
		return "[MODAL]"
	default:
		return "[COMPONENT]"
	}
}

// ShortTag returns a compact 3-character badge for sidebar listings.
func (l AtomicLevel) ShortTag() string {
	switch l {
	case LevelAtom:
		return "[A]"
	case LevelMolecule:
		return "[M]"
	case LevelOrganism:
		return "[O]"
	case LevelModal:
		return "[D]"
	default:
		return "[C]"
	}
}

// AllCategories lists the categories in canonical display order.
var AllCategories = []Category{
	CategoryFoundations,
	CategoryComposer,
	CategoryPopups,
	CategoryToolSteps,
	CategoryPanels,
	CategoryStatus,
	CategoryWorkspace,
}

// State represents a preset configuration or visual variation of a component.
type State struct {
	ID          string
	Name        string
	Description string
}

// StoryInstance is an active, stateful or rendered instance of a story.
type StoryInstance interface {
	// SetState switches the component to the variation at stateIndex.
	SetState(stateIndex int)
	// SetSize updates the available dimensions for rendering.
	SetSize(width, height int)
	// View renders the component's visual representation.
	View() string
	// HandleKey delivers a key event to the component during interactive playground mode.
	// Returns true if the key was consumed by the component.
	HandleKey(key tea.KeyMsg) (tea.Cmd, bool)
}

// Story defines a single component exhibit in the Storybook catalogue.
type Story struct {
	ID          string
	Name        string      // Display name aligned with upstream jetski/cortex
	Level       AtomicLevel // Atomic Design level (Atom, Molecule, Organism, Modal)
	Category    Category    // Parent category
	Description string      // Component purpose and role
	UpstreamSym string      // google3/third_party/jetski symbol
	States      []State     // Available state variations
	Factory     func() StoryInstance
}

// AtomicLevelOrDefault returns the story's explicit level, or derives one from its category.
func (s Story) AtomicLevelOrDefault() AtomicLevel {
	if s.Level != "" {
		return s.Level
	}
	switch s.Category {
	case CategoryFoundations:
		return LevelAtom
	case CategoryToolSteps:
		return LevelMolecule
	case CategoryComposer, CategoryPopups, CategoryWorkspace:
		return LevelOrganism
	case CategoryPanels:
		return LevelModal
	default:
		return LevelMolecule
	}
}

// FocusArea identifies where keyboard focus currently resides.
type FocusArea int

const (
	FocusSidebar FocusArea = iota
	FocusPlayground
)
