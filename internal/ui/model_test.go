package ui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/state"
)

func newTestModel(paths ...string) Model {
	model := New(Config{Shell: "/bin/sh"}, paths, "", state.State{})
	model.width, model.height = 120, 35
	return model
}

func TestNewCreatesSpaceWithZotPane(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	if len(model.spaces) != 2 {
		t.Fatalf("spaces = %d, want 2", len(model.spaces))
	}
	first := model.spaces[0].tab()
	if model.spaces[0].name != "api" || len(first.panes) != 1 {
		t.Fatalf("space[0] = %+v, want one zot pane named api", model.spaces[0])
	}
	if first.panes[0].kind != "zot" {
		t.Fatalf("pane kind = %q, want zot", first.panes[0].kind)
	}
}

func TestPrefixSwitchesSpaces(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	model = updated.(Model)
	if model.mode != modePrefix {
		t.Fatalf("mode = %d, want modePrefix", model.mode)
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	model = updated.(Model)
	if model.selected != 1 {
		t.Fatalf("selected space = %d, want 1", model.selected)
	}
	if model.mode != modeTerminal {
		t.Fatalf("mode = %d, want modeTerminal after prefix command", model.mode)
	}
}

func TestTabsAddAndSwitch(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]

	model.addTab(currentSpace, "zot")
	if len(currentSpace.tabs) != 2 || currentSpace.active != 1 {
		t.Fatalf("tabs = %d active = %d, want 2/1", len(currentSpace.tabs), currentSpace.active)
	}
	if len(currentSpace.tab().panes) != 1 || currentSpace.tab().panes[0].kind != "zot" {
		t.Fatalf("new tab panes = %+v, want one zot pane", currentSpace.tab().panes)
	}

	model.selectTab(1)
	if currentSpace.active != 0 {
		t.Fatalf("active = %d, want wrap to 0", currentSpace.active)
	}
	model.selectTab(-1)
	if currentSpace.active != 1 {
		t.Fatalf("active = %d, want 1", currentSpace.active)
	}
}

func TestTabHit(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.addTab(currentSpace, "zot")

	// Labels are " 1 " (0..2) and " 2 " (3..5), then " + " (6..8).
	if index, isNew := model.tabHit(currentSpace, 1); index != 0 || isNew {
		t.Fatalf("hit(1) = %d/%v, want 0/false", index, isNew)
	}
	if index, isNew := model.tabHit(currentSpace, 4); index != 1 || isNew {
		t.Fatalf("hit(4) = %d/%v, want 1/false", index, isNew)
	}
	if _, isNew := model.tabHit(currentSpace, 7); !isNew {
		t.Fatal("hit(7) should be the + button")
	}
}

func TestSidebarHitMapsRows(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")

	// Row layout without git branches (temp dirs are not repos):
	// 0 blank, 1 WORKSPACES, 2 space api, 3 pane api/zot, 4 space web,
	// 5 pane web/zot, 6 blank, 7 new workspace, 8 blank, 9 AGENTS, 10+.
	kind, index, _ := model.sidebarHit(2)
	if kind != "space" || index != 0 {
		t.Fatalf("row 2 = %q/%d, want space/0", kind, index)
	}
	kind, index, sub := model.sidebarHit(3)
	if kind != "pane" || index != 0 || sub != 0 {
		t.Fatalf("row 3 = %q/%d/%d, want pane/0/0", kind, index, sub)
	}
	kind, _, _ = model.sidebarHit(7)
	if kind != "new" {
		t.Fatalf("row 7 = %q, want new", kind)
	}
}

func TestSidebarScrollClamps(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.height = 6 // tiny: 4 sidebar rows, two pinned for settings + blank
	total := len(model.sidebarRows())
	model.sideScroll = 1000
	if got := model.sidebarOffset(total); got != total-2 {
		t.Fatalf("offset = %d, want %d", got, total-2)
	}
	model.sideScroll = -5
	if got := model.sidebarOffset(total); got != 0 {
		t.Fatalf("offset = %d, want 0", got)
	}
}

func TestMouseSelectsSpace(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	// Sidebar row 4 (space web) is at screen Y=5.
	updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.selected != 1 {
		t.Fatalf("selected = %d, want 1", got.selected)
	}
}

func TestMoveSpaceTo(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web", "/tmp/cli")
	api, web, cli := model.spaces[0], model.spaces[1], model.spaces[2]

	if !model.moveSpaceTo(api, 2) {
		t.Fatal("moveSpaceTo(api, 2) reported no change")
	}
	if model.spaces[0] != web || model.spaces[1] != cli || model.spaces[2] != api {
		t.Fatalf("order = %s/%s/%s, want web/cli/api",
			model.spaces[0].name, model.spaces[1].name, model.spaces[2].name)
	}
	if model.selected != 2 {
		t.Fatalf("selected = %d, want 2 (follows the moved space)", model.selected)
	}

	if !model.moveSpaceTo(api, 0) {
		t.Fatal("moveSpaceTo(api, 0) reported no change")
	}
	if model.spaces[0] != api || model.spaces[1] != web || model.spaces[2] != cli {
		t.Fatalf("order = %s/%s/%s, want api/web/cli",
			model.spaces[0].name, model.spaces[1].name, model.spaces[2].name)
	}
	if model.moveSpaceTo(api, 0) {
		t.Fatal("moveSpaceTo to the same index should report no change")
	}
	if model.moveSpaceTo(api, 5) {
		t.Fatal("moveSpaceTo out of range should report no change")
	}
}

func TestSidebarDragReordersSpaces(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	api, web := model.spaces[0], model.spaces[1]

	// Rows: 0 blank, 1 WORKSPACES, 2 space api, 3 pane, 4 space web,
	// 5 pane. Screen Y is row + 1. Press on api's name arms the drag.
	updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.dragSpace != api {
		t.Fatal("press on a workspace row should arm the drag")
	}

	// Drag down onto web's rows: api moves below web.
	updated, _ = got.updateMouse(tea.MouseMsg{X: 3, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	got = updated.(Model)
	if got.spaces[0] != web || got.spaces[1] != api {
		t.Fatalf("order after drag = %s/%s, want web/api", got.spaces[0].name, got.spaces[1].name)
	}
	if got.selected != 1 {
		t.Fatalf("selected = %d, want 1 (follows the dragged space)", got.selected)
	}

	updated, _ = got.updateMouse(tea.MouseMsg{X: 3, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	got = updated.(Model)
	if got.dragSpace != nil || got.dragMoved {
		t.Fatal("release should clear the drag state")
	}
}

func TestSidebarClickWithoutMotionKeepsOrder(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	api := model.spaces[0]

	updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	updated, _ = got.updateMouse(tea.MouseMsg{X: 3, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	got = updated.(Model)
	if got.spaces[0] != api {
		t.Fatal("a plain click must not reorder workspaces")
	}
	if got.selected != 0 || got.dragSpace != nil {
		t.Fatalf("selected = %d dragSpace = %v, want 0/nil", got.selected, got.dragSpace)
	}
}

func TestMouseTabBarAddsTab(t *testing.T) {
	model := newTestModel("/tmp/api")
	// Tab bar is screen row 1; "+" for a single tab sits at local x 3..5.
	// The click opens the kind picker; choosing a kind adds the tab.
	updated, _ := model.updateMouse(tea.MouseMsg{X: sidebarWidth + 1 + 4, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.mode != modeMenu || got.pickAction != "tab" {
		t.Fatalf("mode/pick = %d/%q, want menu/tab picker", got.mode, got.pickAction)
	}
	updated, _ = got.runKindPick("shell")
	got = updated.(Model)
	if len(got.spaces[0].tabs) != 2 {
		t.Fatalf("tabs = %d, want 2 after pick", len(got.spaces[0].tabs))
	}
	if got.spaces[0].tab().panes[0].kind != "shell" {
		t.Fatalf("pane kind = %q, want shell", got.spaces[0].tab().panes[0].kind)
	}
}

func TestEncodeSGRMouseWheel(t *testing.T) {
	got := encodeSGRMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}, 4, 9)
	if string(got) != "\x1b[<64;5;10M" {
		t.Fatalf("wheel encoding = %q", got)
	}
}

func TestParseCSIU(t *testing.T) {
	code, mods, ok := parseCSIU([]byte("\x1b[49;5u"))
	if !ok || code != '1' || mods&modCtrl == 0 {
		t.Fatalf("parseCSIU = %q/%d/%v, want '1'/ctrl/true", code, mods, ok)
	}
	code, mods, ok = parseCSIU([]byte("\x1b[98;5u"))
	if !ok || code != 'b' || mods&modCtrl == 0 {
		t.Fatalf("parseCSIU = %q/%d/%v, want 'b'/ctrl/true", code, mods, ok)
	}
	if _, _, ok := parseCSIU([]byte("\x1b[5~")); ok {
		t.Fatal("parseCSIU accepted a non CSI-u sequence")
	}
}

func TestResolveDirRejectsMissing(t *testing.T) {
	if _, err := resolveDir("/definitely/not/here-12345"); err == nil {
		t.Fatal("resolveDir accepted a missing directory")
	}
	path, err := resolveDir("/tmp")
	if err != nil || path == "" {
		t.Fatalf("resolveDir(/tmp) = %q/%v", path, err)
	}
}

func TestAnsiCutWideRunes(t *testing.T) {
	// 4 cells of plain text.
	if got := ansiCut("abcd", 0, 2); got != "ab" {
		t.Fatalf("cut = %q, want ab", got)
	}
	// CJK: each rune is 2 cells. Cutting at 2 keeps exactly one rune.
	if got := ansiCut("世界", 0, 2); got != "世" {
		t.Fatalf("cut = %q, want 世", got)
	}
	// Cutting at 3 splits the second rune: its first cell becomes a space.
	if got := ansiCut("世界", 0, 3); got != "世 " {
		t.Fatalf("cut = %q, want '世 '", got)
	}
	// Escapes survive and do not count as cells.
	if got := ansiCut("\x1b[31mab\x1b[0mcd", 0, 3); got != "\x1b[31mab\x1b[0mc" {
		t.Fatalf("cut = %q", got)
	}
	// A from-cut starting inside a wide rune pads the tail cell.
	if got := ansiCut("世x", 1, 3); got != " x" {
		t.Fatalf("cut = %q, want ' x'", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello world", 8); got != "hello w…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("hi", 8); got != "hi" {
		t.Fatalf("truncate = %q", got)
	}
}

func TestPaneMenuHidesCloseForOnlyPane(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.menuPane = currentSpace.tab().panes[0]

	for _, item := range model.menuItems() {
		if item.action == "close" {
			t.Fatal("close pane should be hidden when the workspace has only one pane")
		}
	}

	model.addPane(currentSpace, "shell", true)
	if items := model.menuItems(); items[len(items)-1].action != "close" {
		t.Fatal("close pane should be available when another pane is open")
	}
}

func TestCloseCurrentPaneClamps(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.addPane(currentSpace, "shell", true)
	currentTab := currentSpace.tab()
	if currentTab.selected != 1 {
		t.Fatalf("selected pane = %d, want 1", currentTab.selected)
	}
	model.closeCurrentPane()
	if len(currentTab.panes) != 1 || currentTab.selected != 0 {
		t.Fatalf("panes = %d selected = %d, want 1/0", len(currentTab.panes), currentTab.selected)
	}
	if !strings.HasPrefix(currentTab.panes[0].name, "zot") {
		t.Fatalf("remaining pane = %q, want the zot pane", currentTab.panes[0].name)
	}
}

func TestCloseLastPaneClosesTab(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.addTab(currentSpace, "zot")
	if len(currentSpace.tabs) != 2 {
		t.Fatalf("tabs = %d, want 2", len(currentSpace.tabs))
	}
	model.closeCurrentPane()
	if len(currentSpace.tabs) != 1 {
		t.Fatalf("tabs = %d, want 1 after closing the tab's only pane", len(currentSpace.tabs))
	}
}

func TestGitBranchDetection(t *testing.T) {
	dir := t.TempDir()
	if branch := readGitBranch(dir); branch != "" {
		t.Fatalf("branch in non-repo = %q, want empty", branch)
	}
	gitDir := dir + "/.git"
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitDir+"/HEAD", []byte("ref: refs/heads/feature/tabs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if branch := readGitBranch(dir); branch != "feature/tabs" {
		t.Fatalf("branch = %q, want feature/tabs", branch)
	}
	if err := os.WriteFile(gitDir+"/HEAD", []byte("0123456789abcdef0123456789abcdef01234567\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if branch := readGitBranch(dir); branch != "0123456" {
		t.Fatalf("detached branch = %q, want short hash", branch)
	}
}
