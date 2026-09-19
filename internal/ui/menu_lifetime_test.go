package ui

import "testing"

// A peer or socket client may remove a workspace while its host menu is open.
// Activating that stale menu must never operate on the newly selected workspace.
func TestStaleWorkspaceMenuCannotCloseAnotherWorkspace(t *testing.T) {
	model := newTestModel(t.TempDir(), t.TempDir())
	remaining := model.spaces[1]
	model.openSpaceMenu(model.spaces[0], rect{})
	model.closeCurrentSpace()
	updated, _ := model.runMenuAction("space-close")
	model = updated.(Model)
	if len(model.spaces) != 1 || model.spaces[0] != remaining {
		t.Fatal("stale menu closed another workspace")
	}
}

func TestStalePaneMenuCannotCloseAnotherPane(t *testing.T) {
	model := newTestModel(t.TempDir(), t.TempDir())
	remaining := model.spaces[1]
	model.addPaneSide(remaining, "shell", true, false)
	model.openMenu(model.spaces[0].tab().panes[0], rect{})
	model.closeCurrentSpace()
	updated, _ := model.runMenuAction("close")
	model = updated.(Model)
	if len(remaining.tab().panes) != 2 {
		t.Fatal("stale menu closed a different pane")
	}
}

func TestStaleTabPickerCannotCreateInvisiblePane(t *testing.T) {
	model := newTestModel(t.TempDir(), t.TempDir())
	removed := model.spaces[0]
	model.openKindPicker("tab", removed, "", rect{})
	model.closeCurrentSpace()
	_, command := model.runKindPick("shell")
	if command != nil || len(removed.tabs) != 1 {
		t.Fatal("stale picker created a pane outside the model")
	}
}
