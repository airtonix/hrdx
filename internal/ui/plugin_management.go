package ui

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/plugin"
	"github.com/patriceckhart/hrdx/internal/state"
)

func (m *Model) handlePluginAPI(request api.Request) tea.Cmd {
	answer := func(reply api.Reply) {
		select {
		case request.Reply <- reply:
		default:
		}
	}
	if m.plugins == nil || m.quitting {
		answer(api.Reply{Code: api.CodeError, Err: "experimental plugins are disabled or shutting down"})
		return nil
	}
	if request.Method == "plugins.status" {
		result := api.PluginStatuses{Type: "plugin_status", Plugins: []api.PluginStatus{}}
		registrations := make(map[string]plugin.Registration)
		for _, registration := range m.plugins.Registrations() {
			registrations[registration.Package.ID] = registration
		}
		for _, status := range m.plugins.Statuses() {
			public := api.PluginStatus{ID: status.ID, State: status.State, Generation: status.Generation, Error: status.Error}
			if registration, ok := registrations[status.ID]; ok {
				manifest := registration.Package.Manifest
				public.Name = registration.Package.Name
				public.Version = registration.Package.Version
				public.PackagePath = registration.Package.Path
				public.GrantedGrants = slices.Clone(registration.Approval.Grants)
				public.WorkspaceScopes = slices.Clone(registration.Approval.Workspaces)
				public.InstanceScope = registration.Approval.Instance
				if manifest != nil {
					public.Protocol = plugin.ProtocolVersion
					public.RequestedGrants = slices.Clone(manifest.Requests)
					public.Views = slices.Clone(manifest.Contributes.Views)
					for _, action := range manifest.Contributes.Actions {
						public.Actions = append(public.Actions, action.ID)
					}
					for _, provider := range manifest.Contributes.Providers {
						public.Providers = append(public.Providers, provider.ID)
					}
					public.Markers = slices.Clone(manifest.Activation.Markers)
					public.Config = plugin.EffectiveConfig(manifest, registration.Approval)
				}
			}
			result.Plugins = append(result.Plugins, public)
		}
		answer(api.Reply{Data: result})
		return nil
	}
	params, ok := request.Payload.(api.PluginControl)
	if !ok || (params.Action != "start" && params.Action != "stop" && params.Action != "restart" && params.Action != "reload") {
		answer(api.Reply{Code: api.CodeInvalidParams, Err: "action must be start, stop, restart, or reload"})
		return nil
	}
	found := false
	for _, entry := range m.plugins.Registrations() {
		if entry.Package.ID == params.Plugin {
			found = true
		}
	}
	if !found {
		answer(api.Reply{Code: api.CodeNotFound, Err: "plugin is not approved in this instance"})
		return nil
	}
	var command tea.Cmd
	switch params.Action {
	case "start":
		if err := m.plugins.Start(params.Plugin); err != nil {
			answer(api.Reply{Code: api.CodeError, Err: "cannot start plugin"})
			return nil
		}
	case "stop":
		m.plugins.Stop(params.Plugin)
	case "restart", "reload":
		runtime := m.plugins
		action := params.Action
		command = func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if action == "reload" {
				return pluginResultMsg{id: params.Plugin, err: runtime.Reload(ctx, params.Plugin)}
			}
			return pluginResultMsg{id: params.Plugin, err: runtime.Restart(ctx, params.Plugin)}
		}
	}
	m.syncPluginStates()
	answer(api.Reply{Data: map[string]any{"type": "plugin_control", "accepted": true}})
	return command
}

// The existing host menu supplies live lifecycle controls without giving peers
// access to settings, callbacks, or the socket's unrestricted authority.
func (m *Model) openPluginManager() tea.Cmd {
	if m.plugins == nil {
		return m.flashStatus("Plugins are disabled. Start hrdx with --plugins after explicit approval.")
	}
	m.syncPluginStates()
	items := []menuItem{}
	for _, status := range m.plugins.Statuses() {
		items = append(items, menuItem{status.ID + " [" + status.State + "]: start/restart", "plugin-restart:" + status.ID}, menuItem{"Reload approved package " + status.ID, "plugin-reload:" + status.ID}, menuItem{"Stop " + status.ID, "plugin-stop:" + status.ID})
	}
	if len(items) == 0 {
		return m.flashInfo("No approved plugins. Use hrdx plugins approve to add one.")
	}
	m.closeMenu()
	m.mode = modeMenu
	m.pickAction = "plugins"
	m.pickItems = items
	m.openMenuBox(rect{x: 2, y: 1})
	return nil
}

// pluginSettingsRows lists approved plugins with their enablement and state.
// Enabling and disabling write the approval record the CLI also uses, so a
// running instance observes the change through the normal watcher path.
func (m Model) pluginSettingsRows() []settingsRow {
	if m.plugins == nil {
		return []settingsRow{{"plugins are disabled, start hrdx with --plugins", "plugin-none"}}
	}
	var rows []settingsRow
	for _, status := range m.plugins.Statuses() {
		enabled := status.State != "blocked"
		rows = append(rows, settingsRow{settingsCheck(enabled) + status.ID + " [" + status.State + "]", "plugin-enable:" + status.ID})
		if enabled {
			rows = append(rows, settingsRow{"    restart " + status.ID, "plugin-restart:" + status.ID}, settingsRow{"    stop " + status.ID, "plugin-stop:" + status.ID})
		}
	}
	if len(rows) == 0 {
		rows = append(rows, settingsRow{"no approved plugins, use hrdx plugins approve", "plugin-none"})
	}
	return rows
}

// togglePluginSetting runs a settings row action. Disabling persists to the
// approval file so it survives restarts and is observed by other instances.
// Re-enabling from the settings window is not offered: it requires the CLI,
// which revalidates the package before trusting it again.
func (m *Model) togglePluginSetting(action string) tea.Cmd {
	if m.plugins == nil {
		return nil
	}
	if id, ok := strings.CutPrefix(action, "plugin-enable:"); ok {
		if m.pluginStates[id].State == "blocked" {
			return m.flashStatus("Re-enable with: hrdx plugins enable --id " + id)
		}
		if m.statePath == "" {
			return m.flashStatus("Plugin approvals need a state location")
		}
		path := plugin.ApprovalPath(filepath.Dir(m.statePath), id)
		approval, err := state.LoadPluginApproval(path)
		if err != nil || approval.ID != id {
			return m.flashStatus("Cannot read approval for " + id)
		}
		approval.Enabled = false
		if err := state.SavePluginApproval(path, approval); err != nil {
			return m.flashStatus("Cannot save approval for " + id)
		}
		m.plugins.Stop(id)
		m.syncPluginStates()
		return m.flashInfo("Plugin disabled: " + id)
	}
	return m.runPluginMenu(action, nil, nil, nil)
}
