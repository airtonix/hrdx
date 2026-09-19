package ui

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/plugin"
)

// Providers are pull-based: the host finder queries ready plugins that
// declared a search provider, with a deadline, and renders typed rows itself.
// Results are tagged with the query and plugin generation so stale answers
// after a query change, revocation, or reload are discarded.

const (
	maxProviderRows  = 32
	providerRowLimit = 120
)

// providerRow is one typed result. Selecting it invokes the plugin's
// declared action with the row's opaque ID, or jumps to a pane when the
// provider names one in scope.
type providerRow struct {
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	ID     string `json:"id,omitempty"`
	Pane   int    `json:"pane_id,omitempty"`
}

type providerQueryMsg struct {
	plugin     string
	provider   string
	generation string
	query      string
	rows       []providerRow
	err        error
}

// pluginProviderQuery dispatches the current finder query to every ready
// provider with a matching scope and activation. It is idempotent per query:
// a repeated call with the same query issues nothing.
func (m *Model) pluginProviderQuery() tea.Cmd {
	if m.plugins == nil || m.mode != modeFind {
		return nil
	}
	query := strings.TrimSpace(m.input.Value())
	if query == m.providerQuery {
		return nil
	}
	m.providerQuery = query
	m.providerRows = nil
	if len(query) < 2 {
		return nil
	}
	owner := m.currentSpace()
	var commands []tea.Cmd
	for _, entry := range m.plugins.Registrations() {
		status := m.pluginStates[entry.Package.ID]
		if status.State != "ready" || !plugin.HasGrant(entry.Approval, "ui.provider.contribute") || !plugin.AllowsWorkspace(entry.Approval, owner.cwd) || !m.pluginActive(entry, owner) {
			continue
		}
		for _, provider := range entry.Package.Manifest.Contributes.Providers {
			id, providerID, generation := entry.Package.ID, provider.ID, status.Generation
			params := map[string]any{"provider_id": providerID, "query": query, "limit": maxProviderRows}
			if plugin.HasGrant(entry.Approval, "workspace.read") {
				params["workspace"] = owner.cwd
			}
			runtime := m.plugins
			commands = append(commands, func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), plugin.ProviderTimeout)
				defer cancel()
				data, err := runtime.CallGeneration(ctx, id, generation, "provider.query", params)
				message := providerQueryMsg{plugin: id, provider: providerID, generation: generation, query: query, err: err}
				if err == nil {
					var result struct {
						Rows []providerRow `json:"rows"`
					}
					if json.Unmarshal(data, &result) != nil {
						message.err = plugin.ErrInvalidResult
					}
					message.rows = result.Rows
				}
				return message
			})
		}
	}
	return tea.Batch(commands...)
}

func (m *Model) handleProviderResult(message providerQueryMsg) tea.Cmd {
	if m.plugins == nil || m.mode != modeFind || message.query != m.providerQuery || message.err != nil {
		return nil
	}
	approval, allowed := m.plugins.Authorize(plugin.Request{Plugin: message.plugin, Generation: message.generation, Context: context.Background()}, "ui.provider.contribute")
	if !allowed {
		return nil
	}
	m.providerRows = slices.DeleteFunc(m.providerRows, func(row providerCandidate) bool {
		return row.plugin == message.plugin && row.provider == message.provider
	})
	count := 0
	for _, row := range message.rows {
		if count >= maxProviderRows {
			break
		}
		if !plugin.SafeText(row.Label, providerRowLimit) || row.Label == "" || !plugin.PlainText(row.Detail, providerRowLimit) || len(row.ID) > 256 || !plugin.PlainText(row.ID, 256) {
			continue
		}
		if row.Pane != 0 {
			_, owner := m.paneByID(row.Pane)
			if owner == nil || !plugin.AllowsWorkspace(approval, owner.cwd) {
				continue
			}
		}
		m.providerRows = append(m.providerRows, providerCandidate{plugin: message.plugin, provider: message.provider, generation: message.generation, row: row})
		count++
	}
	m.findIndex = clampInt(m.findIndex, 0, max(0, len(m.findCandidates())+len(m.providerRows)-1))
	return nil
}

type providerCandidate struct {
	plugin, provider, generation string
	row                          providerRow
}

func (c providerCandidate) label() string {
	label := c.row.Label
	if c.row.Detail != "" {
		label += "  " + c.row.Detail
	}
	return label + " (" + c.plugin + ")"
}

// selectProviderRow jumps to a pane the provider named or invokes the plugin
// with the chosen row. Either way the finder closes.
func (m *Model) selectProviderRow(candidate providerCandidate) tea.Cmd {
	m.closeFind()
	if candidate.row.Pane != 0 {
		if _, owner := m.paneByID(candidate.row.Pane); owner != nil {
			for spaceIndex, currentSpace := range m.spaces {
				if currentSpace != owner {
					continue
				}
				for tabIndex, currentTab := range currentSpace.tabs {
					for paneIndex, currentPane := range currentTab.panes {
						if currentPane.id == candidate.row.Pane {
							m.jumpTo(findCandidate{candidate.label(), spaceIndex, tabIndex, paneIndex})
							return nil
						}
					}
				}
			}
		}
	}
	if m.plugins == nil {
		return nil
	}
	runtime := m.plugins
	params := map[string]any{"provider_id": candidate.provider, "id": candidate.row.ID}
	id, generation := candidate.plugin, candidate.generation
	return func() tea.Msg {
		result, err := runtime.CallGeneration(context.Background(), id, generation, "provider.select", params)
		return pluginResultMsg{id: id, generation: generation, result: result, err: err}
	}
}
