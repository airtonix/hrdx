package ui

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/plugin"
	"github.com/patriceckhart/hrdx/internal/state"
)

type pluginSubscription struct {
	generation string
	last       string
	sequence   uint64
}
type pluginTickMsg struct{}

func pluginTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return pluginTickMsg{} })
}

func (m *Model) pluginSnapshot(approval state.PluginApproval) api.Status {
	status := m.apiStatus()
	status.Workspaces = slices.DeleteFunc(status.Workspaces, func(ws api.WorkspaceStatus) bool { return !plugin.AllowsWorkspace(approval, ws.Path) })
	for wi := range status.Workspaces {
		for ti := range status.Workspaces[wi].Tabs {
			if !plugin.HasGrant(approval, "pane.read_metadata") {
				status.Workspaces[wi].Tabs[ti].Panes = nil
			}
			for pi := range status.Workspaces[wi].Tabs[ti].Panes {
				status.Workspaces[wi].Tabs[ti].Panes[pi].Failure = ""
			}
		}
	}
	return status
}

func (m *Model) publishPluginSnapshots() tea.Cmd {
	m.syncPluginStates()
	for id, subscription := range m.pluginSubscriptions {
		request := plugin.Request{Plugin: id, Generation: subscription.generation, Context: context.Background()}
		approval, allowed := m.plugins.Authorize(request, "host.events.subscribe")
		if !allowed || !plugin.HasGrant(approval, "workspace.read") {
			delete(m.pluginSubscriptions, id)
			continue
		}
		snapshot := m.pluginSnapshot(approval)
		data, err := json.Marshal(snapshot)
		if err != nil || string(data) == subscription.last {
			continue
		}
		subscription.sequence++
		if m.plugins.Publish(id, subscription.generation, "snapshot.changed", map[string]any{"sequence": subscription.sequence, "snapshot": snapshot}) {
			subscription.last = string(data)
		}
	}
	if len(m.pluginSubscriptions) == 0 || m.quitting {
		m.pluginTicking = false
		return nil
	}
	return pluginTick()
}
