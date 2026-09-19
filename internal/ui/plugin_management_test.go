package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/plugin"
	"github.com/patriceckhart/hrdx/internal/state"
)

func TestPluginManagementControls(t *testing.T) {
	model, _ := pluginModel(t, nil)
	model.openPluginManager()
	if model.mode != modeMenu || model.pickAction != "plugins" || len(model.pickItems) != 3 {
		t.Fatal("plugin management menu did not open")
	}
	reply := apiCall(t, &model, "plugins.status", nil)
	status, ok := reply.Data.(api.PluginStatuses)
	if reply.Err != "" || !ok || len(status.Plugins) != 1 || status.Plugins[0].ID != "example.ui" {
		t.Fatalf("public plugin status: %+v", reply)
	}
	entry := status.Plugins[0]
	if entry.Version != "1.0.0" || entry.Protocol != 1 || entry.PackagePath == "" || len(entry.RequestedGrants) == 0 || len(entry.Actions) != 1 || entry.Actions[0] != "example.ui.action" || len(entry.Views) != 1 || len(entry.WorkspaceScopes) != 1 {
		t.Fatalf("incomplete plugin diagnostics: %+v", entry)
	}
	if reply := apiCall(t, &model, "plugins.control", api.PluginControl{Plugin: "example.ui", Action: "stop"}); reply.Err != "" {
		t.Fatal(reply.Err)
	}
	if model.pluginStates["example.ui"].State != "stopped" {
		t.Fatal("socket stop did not use runtime lifecycle")
	}
	if reply := apiCall(t, &model, "plugins.control", api.PluginControl{Plugin: "example.ui", Action: "grant"}); reply.Code != api.CodeInvalidParams {
		t.Fatal("socket must not grant privileges")
	}
	if reply := apiCall(t, &model, "plugins.control", api.PluginControl{Plugin: "example.missing", Action: "start"}); reply.Code != api.CodeNotFound {
		t.Fatal("unapproved plugin was accepted")
	}
}

func TestPluginMenuScrollAndMouseAgree(t *testing.T) {
	model := newTestModel(t.TempDir())
	model.width, model.height = 12, 8
	model.mode, model.pickAction = modeMenu, "plugins"
	for index := 0; index < 12; index++ {
		model.pickItems = append(model.pickItems, menuItem{fmt.Sprintf("item%d 界界界", index), fmt.Sprintf("custom:item%d", index)})
	}
	model.openMenuBox(rect{})
	model.menuIndex = 11
	model.scrollMenuIntoView()
	rows := make([]string, 6)
	for index := range rows {
		rows[index] = strings.Repeat(" ", 12)
	}
	model.overlayMenu(rows)
	for _, row := range rows {
		if lipgloss.Width(row) != 12 {
			t.Fatalf("menu width=%d", lipgloss.Width(row))
		}
	}
	if !strings.Contains(strings.Join(rows, "\n"), "item11") {
		t.Fatal("selected item scrolled out of view")
	}
	events := api.NewBroadcaster()
	id, channel := events.Subscribe()
	defer events.Unsubscribe(id)
	model.SetEventBroadcaster(events)
	row := model.menuIndex - model.menuStart()
	model.updateMouse(tea.MouseMsg{X: model.menuAt.x + 1, Y: model.menuAt.y + 2 + row, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	select {
	case event := <-channel:
		if event.Data.(api.MenuActionEvent).ActionID != "item11" {
			t.Fatal("mouse targeted hidden item")
		}
	default:
		t.Fatal("menu click was not delivered")
	}
}

func TestPluginSettingsTabDisablesThroughApprovalFile(t *testing.T) {
	model, _ := pluginModel(t, nil)
	base := t.TempDir()
	model.statePath = filepath.Join(base, "state.json")
	for _, entry := range model.plugins.Registrations() {
		if err := state.SavePluginApproval(plugin.ApprovalPath(base, entry.Package.ID), entry.Approval); err != nil {
			t.Fatal(err)
		}
	}
	tabs := model.settingsTabs()
	if tabs[len(tabs)-1] != "plugins" {
		t.Fatalf("plugins tab missing: %v", tabs)
	}
	model.settingsTab = len(tabs) - 1
	rows := model.settingsRows()
	if len(rows) != 3 || rows[0].action != "plugin-enable:example.ui" {
		t.Fatalf("plugin settings rows: %+v", rows)
	}
	model.toggleSettingsRow(rows[0])
	approval, err := state.LoadPluginApproval(plugin.ApprovalPath(base, "example.ui"))
	if err != nil || approval.Enabled {
		t.Fatalf("settings disable did not persist: %+v %v", approval, err)
	}
	if model.pluginStates["example.ui"].State == "ready" {
		t.Fatal("disabled plugin kept running")
	}
	plain := newTestModel(t.TempDir())
	if len(plain.settingsTabs()) != len(settingsTabNames) {
		t.Fatal("plugins tab shown without the runtime")
	}
}
