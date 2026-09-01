package ui

import (
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	firstSpace := model.spaces[0]
	model.addPane(firstSpace, "shell", true)
	model.addTab(firstSpace, "zot")
	model.selected = 1

	saved := model.snapshot()
	if len(saved.Workspaces) != 2 || saved.Selected != 1 {
		t.Fatalf("snapshot = %+v", saved)
	}
	ws := saved.Workspaces[0]
	if len(ws.Tabs) != 2 || ws.Active != 1 {
		t.Fatalf("workspace 0 tabs = %d active = %d, want 2/1", len(ws.Tabs), ws.Active)
	}
	if len(ws.Tabs[0].Panes) != 2 || ws.Tabs[0].Layout == nil || !ws.Tabs[0].Layout.Vertical {
		t.Fatalf("tab 0 = %+v", ws.Tabs[0])
	}

	restored := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	if len(restored.spaces) != 2 || restored.selected != 1 {
		t.Fatalf("restored = %d spaces selected %d", len(restored.spaces), restored.selected)
	}
	first := restored.spaces[0]
	if len(first.tabs) != 2 || first.active != 1 {
		t.Fatalf("restored tabs = %d active = %d, want 2/1", len(first.tabs), first.active)
	}
	firstTab := first.tabs[0]
	if len(firstTab.panes) != 2 || firstTab.panes[0].kind != "zot" || firstTab.panes[1].kind != "shell" {
		t.Fatalf("restored panes = %+v", firstTab.panes)
	}
	if !firstTab.panes[0].resume {
		t.Fatal("restored zot pane should resume with --continue")
	}
	if firstTab.panes[1].resume {
		t.Fatal("shell panes must not set resume")
	}
	if firstTab.layout == nil || !layoutComplete(firstTab.layout, firstTab.panes) {
		t.Fatal("restored layout incomplete")
	}
}

func TestSnapshotExcludesFloatingPanes(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.addPane(currentSpace, "shell", true)
	floating := model.addFloatingPane(currentSpace, "shell", "center", 40, 30)
	if model.currentPane() != floating {
		t.Fatal("new floating pane should be focused")
	}

	saved := model.snapshot()
	tab := saved.Workspaces[0].Tabs[0]
	if len(tab.Panes) != 2 {
		t.Fatalf("saved panes = %d, want only 2 split panes", len(tab.Panes))
	}
	if tab.Layout == nil {
		t.Fatal("split layout should still be persisted")
	}
}

func TestRestoreLegacyStateWithoutTabs(t *testing.T) {
	legacy := state.State{Workspaces: []state.Workspace{{
		Name:  "api",
		CWD:   "/tmp/api",
		Panes: []state.Pane{{Kind: "zot", Name: "zot 1"}, {Kind: "shell", Name: "shell 1"}},
		Layout: &state.Node{
			Vertical: true, Ratio: 0.5,
			A: &state.Node{Pane: intPtr(0)},
			B: &state.Node{Pane: intPtr(1)},
		},
		Selected: 1,
	}}}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", legacy)
	if len(restored.spaces) != 1 {
		t.Fatalf("spaces = %d, want 1", len(restored.spaces))
	}
	first := restored.spaces[0]
	if len(first.tabs) != 1 || len(first.tab().panes) != 2 {
		t.Fatalf("legacy upgrade = %d tabs / %d panes, want 1/2", len(first.tabs), len(first.tab().panes))
	}
}

func TestRestoreBrokenLayoutFallsBack(t *testing.T) {
	bad := state.State{Workspaces: []state.Workspace{{
		Name: "api",
		CWD:  "/tmp/api",
		Tabs: []state.Tab{{
			Panes: []state.Pane{{Kind: "zot", Name: "zot 1"}, {Kind: "shell", Name: "shell 1"}},
			// Layout references only pane 0, so it is incomplete.
			Layout: &state.Node{Pane: intPtr(0)},
		}},
	}}}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", bad)
	if len(restored.spaces) != 1 {
		t.Fatalf("spaces = %d, want 1", len(restored.spaces))
	}
	firstTab := restored.spaces[0].tab()
	if firstTab.layout == nil || !layoutComplete(firstTab.layout, firstTab.panes) {
		t.Fatal("fallback layout missing panes")
	}
}

func TestRestoreSkipsInvalidWorkspaces(t *testing.T) {
	bad := state.State{Workspaces: []state.Workspace{
		{Name: "empty", CWD: "/tmp/x"},
		{Name: "nocwd", Tabs: []state.Tab{{Panes: []state.Pane{{Kind: "zot", Name: "zot 1"}}}}},
	}}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", bad)
	if len(restored.spaces) != 0 {
		t.Fatalf("spaces = %d, want 0", len(restored.spaces))
	}
}

func TestNewSkipsDuplicateCwd(t *testing.T) {
	saved := state.State{Workspaces: []state.Workspace{{
		Name: "api",
		CWD:  "/tmp/api",
		Tabs: []state.Tab{{
			Panes:  []state.Pane{{Kind: "zot", Name: "zot 1"}},
			Layout: &state.Node{Pane: intPtr(0)},
		}},
	}}}
	model := New(Config{Shell: "/bin/sh"}, []string{"/tmp/api", "/tmp/web"}, "", saved)
	if len(model.spaces) != 2 {
		t.Fatalf("spaces = %d, want 2 (no duplicate for /tmp/api)", len(model.spaces))
	}
}

func intPtr(v int) *int { return &v }
