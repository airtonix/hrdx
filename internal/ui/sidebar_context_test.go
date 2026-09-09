package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/state"
)

func TestSidebarContextTargets(t *testing.T) {
	for _, collapsed := range []bool{false, true} {
		for _, multipleTabs := range []bool{false, true} {
			model := newTestModel(t.TempDir(), t.TempDir())
			model.sideCollapsed = collapsed
			owner := model.spaces[1]
			if multipleTabs {
				model.addTab(owner, "shell")
			}
			targetTab := owner.tabs[0]
			extra := &pane{id: model.nextID, kind: "shell"}
			model.nextID++
			targetTab.panes = append(targetTab.panes, extra)
			targetTab.layout = fallbackLayout(targetTab.panes)
			for rowIndex, row := range model.sidebarRows() {
				if row.kind != "pane" || row.space != 1 || row.tab != 0 {
					continue
				}
				model.selected = 0
				owner.active = len(owner.tabs) - 1
				previous := model.spaces[0].tab().panes[0]
				target := targetTab.panes[row.pane]
				model.paneAttention[previous.id] = true
				model.paneAttention[target.id] = true
				updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: rowIndex + 1, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
				got := updated.(Model)
				if got.mode != modeMenu || got.menuSpace != nil {
					t.Fatalf("collapsed=%v multipleTabs=%v row=%d: expected scoped menu, got space %p", collapsed, multipleTabs, rowIndex, got.menuSpace)
				}
				if multipleTabs && row.pane == 0 {
					if got.menuTab != targetTab {
						t.Fatal("combined row did not target tab")
					}
				} else if got.menuPane != target {
					t.Fatal("pane row did not target pane")
				}
				if got.selected != 1 || owner.active != 0 || got.paneAttention[target.id] || !got.paneAttention[previous.id] {
					t.Fatal("context menu did not focus and acknowledge only its target")
				}
			}
		}
	}
}

func TestSidebarContextNonTargetsDoNotChangeFocus(t *testing.T) {
	model := newTestModel(t.TempDir(), t.TempDir())
	for rowIndex, row := range model.sidebarRows() {
		if row.kind == "space" || row.kind == "pane" {
			continue
		}
		model.selected = 1
		target := model.spaces[1].tab().panes[0]
		model.paneAttention[target.id] = true
		updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: rowIndex + 1, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
		got := updated.(Model)
		if got.selected != 1 || got.mode != modeTerminal || !got.paneAttention[target.id] {
			t.Fatalf("right-clicking %q row changed focus or attention", row.kind)
		}
	}
}

func TestSidebarPaneContextClosesOnlyPane(t *testing.T) {
	model := newTestModel(t.TempDir())
	owner := model.spaces[0]
	first := owner.tab().panes[0]
	extra := &pane{id: model.nextID, kind: "shell"}
	model.nextID++
	owner.tab().panes = append(owner.tab().panes, extra)
	owner.tab().layout = fallbackLayout(owner.tab().panes)
	updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: 6, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.menuPane != extra || got.menuSpace != nil {
		t.Fatal("split pane row opened wrong menu")
	}
	updated, _ = got.runMenuAction("close")
	got = updated.(Model)
	if len(got.spaces) != 1 || len(owner.tabs) != 1 || len(owner.tab().panes) != 1 || owner.tab().panes[0] != first {
		t.Fatal("closing split pane removed unrelated content")
	}
	if !layoutComplete(owner.tab().layout, owner.tab().panes) {
		t.Fatal("invalid remaining layout")
	}
	// A singleton pane must not expose workspace close or last-pane close.
	updated, _ = got.updateMouse(tea.MouseMsg{X: 3, Y: 5, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	got = updated.(Model)
	for _, item := range got.menuItems() {
		if item.action == "space-close" || item.action == "close" {
			t.Fatal("singleton pane exposes destructive close")
		}
	}
}

func TestSidebarTabClosePersistsRemainingTab(t *testing.T) {
	for _, active := range []int{0, 1} {
		model := newTestModel(t.TempDir(), t.TempDir())
		owner := model.spaces[1]
		model.addTab(owner, "shell")
		owner.active = active
		remaining := owner.tabs[1-active]
		model.selected = 0
		for rowIndex, row := range model.sidebarRows() {
			if row.kind != "pane" || row.space != 1 || row.tab != active {
				continue
			}
			updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: rowIndex + 1, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
			model = updated.(Model)
			break
		}
		updated, _ := model.runMenuAction("tab-close")
		model = updated.(Model)
		if len(model.spaces) != 2 || len(owner.tabs) != 1 || owner.active != 0 || owner.tabs[0] != remaining {
			t.Fatal("tab close damaged workspace or remaining tab")
		}
		restored := New(Config{}, nil, "", state.State{})
		restored.restore(model.snapshot())
		if len(restored.spaces) != 2 || len(restored.spaces[1].tabs) != 1 || restored.spaces[1].active != 0 {
			t.Fatal("remaining tab did not survive snapshot/restore")
		}
		model.openTabMenu(remaining, rect{})
		for _, item := range model.menuItems() {
			if item.action == "tab-close" {
				t.Fatal("final tab offers close")
			}
		}
		model.runMenuAction("tab-close")
		if len(owner.tabs) != 1 {
			t.Fatal("final tab was removed")
		}
	}
}
