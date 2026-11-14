package storybook_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/storybook"
)

func TestCatalog_AllCategoriesAndOrder(t *testing.T) {
	cat := storybook.DefaultCatalog()
	stories := cat.Stories()
	t.Logf("Total registered stories: %d", len(stories))

	if len(stories) == 0 {
		t.Fatalf("DefaultCatalog() returned 0 stories")
	}

	seenIDs := make(map[string]bool)
	categoryCounts := make(map[storybook.Category]int)

	for _, s := range stories {
		if s.ID == "" {
			t.Errorf("Story has empty ID: %+v", s)
		}
		if seenIDs[s.ID] {
			t.Errorf("Duplicate story ID: %s", s.ID)
		}
		seenIDs[s.ID] = true

		if s.Name == "" {
			t.Errorf("Story %s has empty Name", s.ID)
		}
		if s.UpstreamSym == "" {
			t.Errorf("Story %s has empty UpstreamSym (must map to upstream jetski/cortex symbol)", s.ID)
		}
		if len(s.States) == 0 {
			t.Errorf("Story %s has 0 states configured", s.ID)
		}
		if s.Factory == nil {
			t.Errorf("Story %s has nil Factory", s.ID)
		}
		categoryCounts[s.Category]++
	}

	for _, expectedCat := range storybook.AllCategories {
		count := categoryCounts[expectedCat]
		if count == 0 {
			t.Errorf("Category %q has 0 registered stories", expectedCat)
		}
	}
}

func TestCatalog_RenderAllStoriesAndStates(t *testing.T) {
	cat := storybook.DefaultCatalog()
	stories := cat.Stories()

	dimensions := []struct {
		w, h int
	}{
		{80, 24},
		{120, 35},
		{50, 15},
	}

	for _, s := range stories {
		t.Run(s.ID, func(t *testing.T) {
			for stateIdx, st := range s.States {
				t.Run(st.ID, func(t *testing.T) {
					inst := s.Factory()
					if inst == nil {
						t.Fatalf("Factory() returned nil for story %s", s.ID)
					}
					inst.SetState(stateIdx)

					for _, dim := range dimensions {
						inst.SetSize(dim.w, dim.h)
						view := inst.View()
						if strings.TrimSpace(view) == "" {
							t.Errorf("Story %s state %s returned empty view at %dx%d", s.ID, st.ID, dim.w, dim.h)
						}
					}
				})
			}
		})
	}
}

func TestCatalog_FilterStories(t *testing.T) {
	cat := storybook.DefaultCatalog()

	itemsAll := cat.VisibleNavItems("")
	if len(itemsAll) == 0 {
		t.Fatalf("expected nav items for empty query, got 0")
	}

	itemsCursor := cat.VisibleNavItems("cursor")
	if len(itemsCursor) == 0 {
		t.Fatalf("expected matches for 'cursor', got 0")
	}

	foundCursor := false
	for _, it := range itemsCursor {
		if !it.IsCategory && strings.Contains(strings.ToLower(it.Story.Name), "cursor") {
			foundCursor = true
			break
		}
	}
	if !foundCursor {
		t.Errorf("expected to find CJKCursor in filtered results for 'cursor'")
	}
}

func TestLayout_DualPane(t *testing.T) {
	cases := []struct {
		termW, termH int
		minCanvasW   int
	}{
		{100, 30, 60},
		{60, 20, 30},
		{40, 10, 19},
	}

	for _, tc := range cases {
		layout := storybook.ComputeLayout(tc.termW, tc.termH)
		if layout.SidebarWidth+layout.CanvasWidth+1 != layout.TotalWidth {
			t.Errorf("layout columns sum mismatch for termW=%d: sidebar=%d canvas=%d total=%d",
				tc.termW, layout.SidebarWidth, layout.CanvasWidth, layout.TotalWidth)
		}
		if layout.CanvasWidth < tc.minCanvasW {
			t.Errorf("canvas width %d below expected minimum %d for termW=%d",
				layout.CanvasWidth, tc.minCanvasW, tc.termW)
		}
	}
}

func TestModel_KeyboardNavigation(t *testing.T) {
	cat := storybook.DefaultCatalog()
	m := storybook.NewModel(cat)

	// Set window size
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model, ok := newM.(storybook.Model)
	if !ok {
		t.Fatalf("Update WindowSizeMsg did not return storybook.Model")
	}

	// Initial render should not be empty
	view := model.View()
	if strings.TrimSpace(view.Content) == "" {
		t.Errorf("View content was empty after WindowSizeMsg")
	}

	// Press Tab to cycle states
	newM, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = newM.(storybook.Model)

	// Press 't' to cycle themes
	newM, _ = model.Update(tea.KeyPressMsg{Code: 't'})
	model = newM.(storybook.Model)

	// Press Enter to focus playground
	newM, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = newM.(storybook.Model)

	// Press Esc to exit playground back to sidebar
	newM, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = newM.(storybook.Model)

	// Press 'q' to quit
	newM, _ = model.Update(tea.KeyPressMsg{Code: 'q'})
	model = newM.(storybook.Model)
	if !model.Quitting() {
		t.Errorf("expected Quitting() = true after pressing 'q'")
	}
}

func TestCtrlO_InteractiveToggle(t *testing.T) {
	cat := storybook.DefaultCatalog()
	ctrlO := tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}

	// 1. TrajectoryToggle Story
	trajStory, ok := cat.Find("toolsteps-trajectory-toggle")
	if !ok {
		t.Fatalf("expected toolsteps-trajectory-toggle story to exist")
	}
	inst := trajStory.Factory()
	inst.SetSize(80, 24)
	inst.SetState(0) // Collapsed

	v0 := inst.View()
	if !strings.Contains(v0, "ctrl+o to expand") {
		t.Errorf("expected collapsed view to contain 'ctrl+o to expand', got:\n%s", v0)
	}

	cmd, handled := inst.HandleKey(ctrlO)
	if !handled || cmd != nil {
		t.Errorf("expected Ctrl+O to be handled with nil cmd")
	}

	v1 := inst.View()
	if !strings.Contains(v1, "ctrl+o to collapse") {
		t.Errorf("expected expanded view to contain 'ctrl+o to collapse', got:\n%s", v1)
	}

	// Toggle back
	inst.HandleKey(ctrlO)
	v2 := inst.View()
	if !strings.Contains(v2, "ctrl+o to expand") {
		t.Errorf("expected re-collapsed view to contain 'ctrl+o to expand', got:\n%s", v2)
	}

	// 2. Attachments Story
	attStory, ok := cat.Find("composer-attachments")
	if !ok {
		t.Fatalf("expected composer-attachments story to exist")
	}
	attInst := attStory.Factory()
	attInst.SetSize(80, 24)
	attInst.SetState(0) // Collapsed

	av0 := attInst.View()
	if !strings.Contains(av0, "ctrl+o to expand") {
		t.Errorf("expected collapsed attachments to contain 'ctrl+o to expand', got:\n%s", av0)
	}

	_, handled = attInst.HandleKey(ctrlO)
	if !handled {
		t.Errorf("expected Ctrl+O to be handled on attachments story")
	}
	av1 := attInst.View()
	if !strings.Contains(av1, "ctrl+o to collapse") {
		t.Errorf("expected expanded attachments to contain 'ctrl+o to collapse', got:\n%s", av1)
	}

	// 3. Batch Steps Story
	batchStory, ok := cat.Find("toolsteps-batch")
	if !ok {
		t.Fatalf("expected toolsteps-batch story to exist")
	}
	batchInst := batchStory.Factory()
	batchInst.SetSize(80, 24)
	batchInst.SetState(0) // Expanded

	bv0 := batchInst.View()
	if !strings.Contains(bv0, "ctrl+o to collapse") {
		t.Errorf("expected expanded batch steps to contain 'ctrl+o to collapse', got:\n%s", bv0)
	}

	_, handled = batchInst.HandleKey(ctrlO)
	if !handled {
		t.Errorf("expected Ctrl+O to be handled on batch steps story")
	}
	bv1 := batchInst.View()
	if !strings.Contains(bv1, "ctrl+o to expand") {
		t.Errorf("expected collapsed batch steps to contain 'ctrl+o to expand', got:\n%s", bv1)
	}

	// 4. BashStep Story (Dynamic Tail Overflow)
	bashStory, ok := cat.Find("toolsteps-bash-step")
	if !ok {
		t.Fatalf("expected toolsteps-bash-step story to exist")
	}
	bashInst := bashStory.Factory()
	bashInst.SetSize(80, 24)
	bashInst.SetState(3) // Dynamic Tail Overflow

	bashV0 := bashInst.View()
	if !strings.Contains(bashV0, "<output +") {
		t.Errorf("expected expanded bash step to contain '<output +...', got:\n%s", bashV0)
	}
	if !strings.Contains(bashV0, "TestIsEngineHost") {
		t.Errorf("expected expanded bash step to contain tail test output 'TestIsEngineHost', got:\n%s", bashV0)
	}
	if !strings.Contains(bashV0, "ctrl+o to collapse") {
		t.Errorf("expected expanded bash step to contain 'ctrl+o to collapse', got:\n%s", bashV0)
	}

	// Toggle to collapsed
	_, handled = bashInst.HandleKey(ctrlO)
	if !handled {
		t.Errorf("expected Ctrl+O to be handled on bash step story")
	}
	bashV1 := bashInst.View()
	if !strings.Contains(bashV1, "ctrl+o to expand") {
		t.Errorf("expected collapsed bash step to contain 'ctrl+o to expand', got:\n%s", bashV1)
	}
	if strings.Contains(bashV1, "<output +70 lines>") {
		t.Errorf("collapsed bash step should hide detail rows, got:\n%s", bashV1)
	}
}

func TestPanels_InteractiveKeyHandling(t *testing.T) {
	cat := storybook.DefaultCatalog()

	// 1. Onboarding Wizard interaction
	wizStory, ok := cat.Find("panel-onboarding")
	if !ok {
		t.Fatalf("panel-onboarding story not found")
	}
	wizInst := wizStory.Factory()
	wizInst.SetSize(80, 24)
	wizInst.SetState(0)

	downKey := tea.KeyPressMsg{Code: tea.KeyDown}
	enterKey := tea.KeyPressMsg{Code: tea.KeyEnter}
	spaceKey := tea.KeyPressMsg{Code: tea.KeySpace}

	_, handled := wizInst.HandleKey(downKey)
	if !handled {
		t.Errorf("onboarding wizard should handle down arrow")
	}
	// Press '5' for dark scheme
	key5 := tea.KeyPressMsg{Code: '5', Text: "5"}
	_, handled = wizInst.HandleKey(key5)
	if !handled {
		t.Errorf("onboarding wizard should handle '5' key")
	}

	// Press Enter to advance to StepTerms
	_, _ = wizInst.HandleKey(enterKey)
	termsView := wizInst.View()
	if !strings.Contains(termsView, "Terms") && !strings.Contains(termsView, "agreement") && !strings.Contains(termsView, "[ Done ]") {
		t.Errorf("wizard did not advance to terms on Enter: %s", termsView)
	}

	// Toggle agreement with Space
	_, handled = wizInst.HandleKey(spaceKey)
	if !handled {
		t.Errorf("onboarding wizard should handle Space key on terms step")
	}

	// 2. Changelog pagination & scrolling
	clStory, ok := cat.Find("panel-changelog")
	if !ok {
		t.Fatalf("panel-changelog story not found")
	}
	clInst := clStory.Factory()
	clInst.SetSize(80, 24)
	clInst.SetState(0)

	_, handled = clInst.HandleKey(downKey)
	if !handled {
		t.Errorf("changelog should handle down arrow")
	}
	pgDnKey := tea.KeyPressMsg{Code: tea.KeyPgDown}
	_, handled = clInst.HandleKey(pgDnKey)
	if !handled {
		t.Errorf("changelog should handle page down")
	}
	homeKey := tea.KeyPressMsg{Code: tea.KeyHome}
	_, handled = clInst.HandleKey(homeKey)
	if !handled {
		t.Errorf("changelog should handle Home key")
	}

	// 3. Plugins navigation
	plugStory, ok := cat.Find("panel-plugins")
	if !ok {
		t.Fatalf("panel-plugins story not found")
	}
	plugInst := plugStory.Factory()
	plugInst.SetSize(80, 24)
	plugInst.SetState(0)

	_, handled = plugInst.HandleKey(downKey)
	if !handled {
		t.Errorf("plugins UI should handle down arrow")
	}

	// 4. Skills navigation
	skillStory, ok := cat.Find("panel-skills")
	if !ok {
		t.Fatalf("panel-skills story not found")
	}
	skillInst := skillStory.Factory()
	skillInst.SetSize(80, 24)
	skillInst.SetState(0)

	_, handled = skillInst.HandleKey(downKey)
	if !handled {
		t.Errorf("skills UI should handle down arrow")
	}

	// 5. Permissions navigation
	permStory, ok := cat.Find("panel-permissions")
	if !ok {
		t.Fatalf("panel-permissions story not found")
	}
	permInst := permStory.Factory()
	permInst.SetSize(80, 24)
	permInst.SetState(0)

	_, handled = permInst.HandleKey(downKey)
	if !handled {
		t.Errorf("permissions panel should handle down arrow")
	}
}

func TestBoxBorders_RenderingAndInteraction(t *testing.T) {
	cat := storybook.DefaultCatalog()
	story, ok := cat.Find("foundations-box-borders")
	if !ok {
		t.Fatalf("expected foundations-box-borders story to exist")
	}

	inst := story.Factory()
	inst.SetSize(90, 30)

	// Test Normal / Sharp
	inst.SetState(0)
	v0 := inst.View()
	if !strings.Contains(v0, "┌") || !strings.Contains(v0, "┐") || !strings.Contains(v0, "└") || !strings.Contains(v0, "┘") {
		t.Errorf("normal-sharp border view missing single-line box characters:\n%s", v0)
	}

	// Test Rounded Curved
	inst.SetState(1)
	v1 := inst.View()
	if !strings.Contains(v1, "╭") || !strings.Contains(v1, "╮") || !strings.Contains(v1, "╰") || !strings.Contains(v1, "╯") {
		t.Errorf("rounded-curved border view missing curved characters:\n%s", v1)
	}

	// Test Thick Heavy
	inst.SetState(2)
	v2 := inst.View()
	if !strings.Contains(v2, "┏") || !strings.Contains(v2, "┓") || !strings.Contains(v2, "┗") || !strings.Contains(v2, "┛") {
		t.Errorf("thick-heavy border view missing heavy characters:\n%s", v2)
	}

	// Test Double Line
	inst.SetState(3)
	v3 := inst.View()
	if !strings.Contains(v3, "╔") || !strings.Contains(v3, "╗") || !strings.Contains(v3, "╚") || !strings.Contains(v3, "╝") {
		t.Errorf("double-line border view missing double-line characters:\n%s", v3)
	}

	// Test Dashed & Dotted
	inst.SetState(4)
	v4 := inst.View()
	if !strings.Contains(v4, "┈") || !strings.Contains(v4, "┊") {
		t.Errorf("dotted-dashed border view missing dash characters:\n%s", v4)
	}

	// Test Left Accent
	inst.SetState(5)
	v5 := inst.View()
	if !strings.Contains(v5, "┃") || !strings.Contains(v5, "/btw") {
		t.Errorf("left-accent border view missing ┃ or /btw text:\n%s", v5)
	}

	// Test Gallery
	inst.SetState(6)
	v6 := inst.View()
	if !strings.Contains(v6, "Terminal Box-Drawing & Border Style Matrix") {
		t.Errorf("gallery view missing matrix header:\n%s", v6)
	}

	// Test interactive width resizing (+ / -) and title toggle (space)
	inst.SetState(0)
	plusKey := tea.KeyPressMsg{Code: '+', Text: "+"}
	_, handled := inst.HandleKey(plusKey)
	if !handled {
		t.Errorf("expected '+' to be handled on box borders story")
	}

	minusKey := tea.KeyPressMsg{Code: '-', Text: "-"}
	_, handled = inst.HandleKey(minusKey)
	if !handled {
		t.Errorf("expected '-' to be handled on box borders story")
	}

	spaceKey := tea.KeyPressMsg{Code: tea.KeySpace}
	_, handled = inst.HandleKey(spaceKey)
	if !handled {
		t.Errorf("expected Space to be handled on box borders story")
	}
}

func TestHerdrStory_Interactive(t *testing.T) {
	cat := storybook.DefaultCatalog()
	s, ok := cat.Find("organism-herdr-multiplexer")
	if !ok {
		t.Fatalf("organism-herdr-multiplexer not found in catalog")
	}
	inst := s.Factory()
	inst.SetSize(90, 24)
	inst.SetState(1) // Multi-tab state

	view := inst.View()
	if !strings.Contains(view, "spaces") || !strings.Contains(view, "agents") {
		t.Errorf("expected Herdr view to contain spaces and agents sidebar, got:\n%s", view)
	}
	if !strings.Contains(view, "sh") || !strings.Contains(view, "pigasus-srv") {
		t.Errorf("expected multi-tab names in view:\n%s", view)
	}

	// Test Ctrl+B prefix toggle
	ctrlB := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	cmd, handled := inst.HandleKey(ctrlB)
	if !handled || cmd != nil {
		t.Errorf("expected Ctrl+B to be handled")
	}
	viewArmed := inst.View()
	if !strings.Contains(viewArmed, "PREFIX") {
		t.Errorf("expected armed prefix in view:\n%s", viewArmed)
	}

	// Switch tab with 'n'
	inst.HandleKey(tea.KeyPressMsg{Code: 'n'})
	viewAfterN := inst.View()
	if strings.Contains(viewAfterN, "PREFIX") {
		t.Errorf("expected prefix to be disarmed after command")
	}
}

func TestSidebar_MouseWheelAndClick(t *testing.T) {
	cat := storybook.DefaultCatalog()
	m := storybook.NewModel(cat)

	// Set window size
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model, ok := newM.(storybook.Model)
	if !ok {
		t.Fatalf("expected storybook.Model type")
	}

	initialIdx := model.SelectedNavIdx()

	// 1. Mouse wheel down over sidebar (X = 10, Y = 10)
	wheelDown := tea.MouseWheelMsg(tea.Mouse{X: 10, Y: 10, Button: tea.MouseWheelDown})
	newM, _ = model.Update(wheelDown)
	model = newM.(storybook.Model)
	if model.SelectedNavIdx() != initialIdx+1 {
		t.Errorf("expected SelectedNavIdx to increment to %d, got %d", initialIdx+1, model.SelectedNavIdx())
	}

	// 2. Mouse wheel up over sidebar (X = 10, Y = 10)
	wheelUp := tea.MouseWheelMsg(tea.Mouse{X: 10, Y: 10, Button: tea.MouseWheelUp})
	newM, _ = model.Update(wheelUp)
	model = newM.(storybook.Model)
	if model.SelectedNavIdx() != initialIdx {
		t.Errorf("expected SelectedNavIdx to decrement back to %d, got %d", initialIdx, model.SelectedNavIdx())
	}

	// 3. Mouse left click over sidebar row 0 (X = 10, Y = 3 -> item index 0)
	clickSidebarRow0 := tea.MouseClickMsg(tea.Mouse{X: 10, Y: 3, Button: tea.MouseLeft})
	newM, _ = model.Update(clickSidebarRow0)
	model = newM.(storybook.Model)
	if model.Focus() != storybook.FocusSidebar {
		t.Errorf("expected FocusSidebar after clicking sidebar, got %v", model.Focus())
	}
	if model.SelectedNavIdx() != 0 {
		t.Errorf("expected SelectedNavIdx to be 0 for Y=3 click, got %d", model.SelectedNavIdx())
	}

	// Mouse left click over sidebar row 1 (X = 10, Y = 4 -> item index 1)
	clickSidebarRow1 := tea.MouseClickMsg(tea.Mouse{X: 10, Y: 4, Button: tea.MouseLeft})
	newM, _ = model.Update(clickSidebarRow1)
	model = newM.(storybook.Model)
	if model.SelectedNavIdx() != 1 {
		t.Errorf("expected SelectedNavIdx to be 1 for Y=4 click, got %d", model.SelectedNavIdx())
	}

	// 4. Mouse left click over canvas / playground (X = 50, Y = 10)
	clickCanvas := tea.MouseClickMsg(tea.Mouse{X: 50, Y: 10, Button: tea.MouseLeft})
	newM, _ = model.Update(clickCanvas)
	model = newM.(storybook.Model)
	if model.Focus() != storybook.FocusPlayground {
		t.Errorf("expected FocusPlayground after clicking canvas, got %v", model.Focus())
	}
}

func TestStories_MouseWheelDelegation(t *testing.T) {
	cat := storybook.DefaultCatalog()

	// 1. Onboarding Wizard mouse wheel
	if s, ok := cat.Find("panel-onboarding"); ok {
		inst := s.Factory()
		inst.SetSize(100, 30)
		inst.SetState(0)

		vBefore := inst.View()
		wheelDown := tea.MouseWheelMsg(tea.Mouse{X: 50, Y: 10, Button: tea.MouseWheelDown})
		if h, ok := inst.(interface {
			HandleMouse(tea.MouseMsg) (tea.Cmd, bool)
		}); ok {
			_, handled := h.HandleMouse(wheelDown)
			if !handled {
				t.Errorf("expected onboarding story to handle MouseWheelDown")
			}
			vAfter := inst.View()
			if vBefore == vAfter {
				t.Errorf("expected view to change after MouseWheelDown on onboarding story")
			}
		} else {
			t.Errorf("onboarding story does not implement HandleMouse")
		}
	} else {
		t.Errorf("panel-onboarding story not found")
	}

	// 2. Changelog Panel mouse wheel
	if s, ok := cat.Find("panel-changelog"); ok {
		inst := s.Factory()
		inst.SetSize(100, 30)
		inst.SetState(0)

		wheelDown := tea.MouseWheelMsg(tea.Mouse{X: 50, Y: 10, Button: tea.MouseWheelDown})
		if h, ok := inst.(interface {
			HandleMouse(tea.MouseMsg) (tea.Cmd, bool)
		}); ok {
			_, handled := h.HandleMouse(wheelDown)
			if !handled {
				t.Errorf("expected changelog story to handle MouseWheelDown")
			}
		} else {
			t.Errorf("changelog story does not implement HandleMouse")
		}
	} else {
		t.Errorf("panel-changelog story not found")
	}

	// 3. Context Panel mouse wheel
	if s, ok := cat.Find("panel-context"); ok {
		inst := s.Factory()
		inst.SetSize(100, 30)
		inst.SetState(0)

		wheelDown := tea.MouseWheelMsg(tea.Mouse{X: 50, Y: 10, Button: tea.MouseWheelDown})
		if h, ok := inst.(interface {
			HandleMouse(tea.MouseMsg) (tea.Cmd, bool)
		}); ok {
			_, handled := h.HandleMouse(wheelDown)
			if !handled {
				t.Errorf("expected context story to handle MouseWheelDown")
			}
		} else {
			t.Errorf("context story does not implement HandleMouse")
		}
	} else {
		t.Errorf("panel-context story not found")
	}

	// 4. Usage Panel mouse wheel
	if s, ok := cat.Find("panel-usage"); ok {
		inst := s.Factory()
		inst.SetSize(100, 30)
		inst.SetState(0)

		wheelDown := tea.MouseWheelMsg(tea.Mouse{X: 50, Y: 10, Button: tea.MouseWheelDown})
		if h, ok := inst.(interface {
			HandleMouse(tea.MouseMsg) (tea.Cmd, bool)
		}); ok {
			_, handled := h.HandleMouse(wheelDown)
			if !handled {
				t.Errorf("expected usage story to handle MouseWheelDown")
			}
		} else {
			t.Errorf("usage story does not implement HandleMouse")
		}
	} else {
		t.Errorf("panel-usage story not found")
	}
}

func TestFocusCircleTransition_HollowToSolid(t *testing.T) {
	cat := storybook.DefaultCatalog()
	m := storybook.NewModel(cat)

	// Set window size (130 columns gives ample room for full breadcrumbs)
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 130, Height: 30})
	model := newM.(storybook.Model)

	// 1. In FocusSidebar mode, view should show hollow circle ○
	viewNav := model.View().Content
	if !strings.Contains(viewNav, "○") {
		t.Errorf("expected view in FocusSidebar mode to contain hollow circle ○, got:\n%s", viewNav)
	}
	if !strings.Contains(viewNav, "Enter to interact") {
		t.Errorf("expected view in FocusSidebar mode to show Enter hint, got:\n%s", viewNav)
	}

	// 2. Press Enter to switch to FocusPlayground mode
	newM, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = newM.(storybook.Model)
	if model.Focus() != storybook.FocusPlayground {
		t.Fatalf("expected FocusPlayground after pressing Enter")
	}

	viewInteractive := model.View().Content
	if !strings.Contains(viewInteractive, "●") {
		t.Errorf("expected view in FocusPlayground mode to contain solid circle ●, got:\n%s", viewInteractive)
	}
	if !strings.Contains(viewInteractive, "INTERACTIVE") {
		t.Errorf("expected view in FocusPlayground mode to show INTERACTIVE indicator, got:\n%s", viewInteractive)
	}

	// 3. Press Esc to return to FocusSidebar mode
	newM, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = newM.(storybook.Model)
	if model.Focus() != storybook.FocusSidebar {
		t.Fatalf("expected FocusSidebar after pressing Esc")
	}

	viewReturned := model.View().Content
	if !strings.Contains(viewReturned, "○") {
		t.Errorf("expected view after Esc to return to hollow circle ○, got:\n%s", viewReturned)
	}
}
