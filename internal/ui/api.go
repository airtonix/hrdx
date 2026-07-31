package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/api"
)

// handleAPI executes one external API request inside the update loop and
// answers on the request's reply channel. It returns follow-up commands
// (pane starts) for methods that create panes.
func (m *Model) handleAPI(request api.Request) tea.Cmd {
	answer := func(data any, code, err string) {
		select {
		case request.Reply <- api.Reply{Data: data, Err: err, Code: code}:
		default:
		}
	}
	ok := func(data any) { answer(data, "", "") }

	switch request.Method {
	case "status":
		ok(m.apiStatus())
		return nil

	case "workspace.create":
		payload, valid := request.Payload.(api.WorkspaceCreate)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		return m.apiWorkspaceCreate(payload, answer)

	case "workspace.close":
		payload, valid := request.Payload.(api.WorkspaceRef)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		target := m.spaceByRef(payload.Workspace)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("workspace %q not found", payload.Workspace))
			return nil
		}
		for index, currentSpace := range m.spaces {
			if currentSpace == target {
				m.selected = index
			}
		}
		m.closeCurrentSpace()
		ok(map[string]any{"type": "workspace_closed", "workspace": target.name})
		m.publish(api.Event{Event: api.EventWorkspaceClosed,
			Data: api.WorkspaceEvent{Workspace: target.name, Path: target.cwd}})
		return nil

	case "pane.create":
		payload, valid := request.Payload.(api.PaneCreate)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		return m.apiPaneCreate(payload, answer)

	case "pane.send_text":
		payload, valid := request.Payload.(api.PaneSendText)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		target, _ := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return nil
		}
		if target.term == nil || !target.running {
			answer(nil, api.CodeError, fmt.Sprintf("pane %d is not running", payload.Pane))
			return nil
		}
		text := payload.Text
		if payload.Enter {
			text += "\r"
		}
		target.term.Write([]byte(text))
		ok(map[string]any{"type": "pane_send_text", "pane_id": payload.Pane})
		return nil

	case "pane.read":
		payload, valid := request.Payload.(api.PaneRef)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		target, _ := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return nil
		}
		screen := ""
		if target.term != nil {
			screen = target.term.PlainScreen()
		}
		ok(map[string]any{"type": "pane_read", "pane_id": payload.Pane, "screen": screen})
		return nil

	case "pane.close":
		payload, valid := request.Payload.(api.PaneRef)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		target, owner := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return nil
		}
		m.focusPane(owner, target)
		for index, currentSpace := range m.spaces {
			if currentSpace == owner {
				m.selected = index
			}
		}
		m.closeCurrentPane()
		ok(map[string]any{"type": "pane_closed", "pane_id": payload.Pane})
		m.publish(api.Event{Event: api.EventPaneClosed,
			Data: api.PaneEvent{Pane: payload.Pane, Name: target.name, Kind: target.kind, Workspace: owner.name}})
		return nil

	case "pane.busy":
		payload, valid := request.Payload.(api.PaneRef)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		target, _ := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return nil
		}
		ok(m.paneBusy(target))
		return nil
	}

	answer(nil, api.CodeUnknownMethod, "unknown method "+request.Method)
	return nil
}

// publish sends an event to API subscribers when a broadcaster is wired.
func (m *Model) publish(event api.Event) {
	m.events.Publish(event)
}

// spaceByRef finds a workspace by name or path.
func (m *Model) spaceByRef(ref string) *space {
	for _, currentSpace := range m.spaces {
		if currentSpace.name == ref || currentSpace.cwd == ref {
			return currentSpace
		}
	}
	return nil
}

// apiStatus builds the full state snapshot.
func (m *Model) apiStatus() api.Status {
	status := api.Status{Type: "status", Version: m.config.Version}
	for spaceIndex, currentSpace := range m.spaces {
		ws := api.WorkspaceStatus{
			Name:     currentSpace.name,
			Path:     currentSpace.cwd,
			Selected: spaceIndex == m.selected,
			Branch:   m.gitBranch(currentSpace.cwd).value,
		}
		for tabIndex, currentTab := range currentSpace.tabs {
			tabStatus := api.TabStatus{
				Name:   currentTab.name,
				Active: tabIndex == currentSpace.active,
			}
			for _, currentPane := range currentTab.panes {
				tabStatus.Panes = append(tabStatus.Panes, api.PaneStatus{
					ID:      currentPane.id,
					Name:    currentPane.name,
					Kind:    currentPane.kind,
					Running: currentPane.running,
					Busy:    m.paneBusy(currentPane),
					Failure: currentPane.failure,
				})
			}
			ws.Tabs = append(ws.Tabs, tabStatus)
		}
		status.Workspaces = append(status.Workspaces, ws)
	}
	return status
}

// apiResolveKind validates a requested pane kind, defaulting to the
// current default agent.
func (m *Model) apiResolveKind(kind string) (string, error) {
	if kind == "" {
		return m.config.DefaultAgent, nil
	}
	if kind == "shell" || isAgentKind(kind) {
		return kind, nil
	}
	return "", fmt.Errorf("unknown kind %q", kind)
}

func (m *Model) apiWorkspaceCreate(payload api.WorkspaceCreate, answer func(any, string, string)) tea.Cmd {
	kind, err := m.apiResolveKind(payload.Agent)
	if err != nil {
		answer(nil, api.CodeInvalidParams, err.Error())
		return nil
	}
	path, err := resolveDir(payload.Path)
	if err != nil {
		answer(nil, api.CodeInvalidParams, err.Error())
		return nil
	}
	for _, currentSpace := range m.spaces {
		if currentSpace.cwd == path {
			answer(nil, api.CodeError, fmt.Sprintf("%s is already open", path))
			return nil
		}
	}
	newSpace := m.addSpaceKind(path, kind)
	m.selected = len(m.spaces) - 1
	m.persist()
	newPane := newSpace.tab().panes[0]
	answer(map[string]any{
		"type": "workspace_created", "workspace": newSpace.name, "pane_id": newPane.id,
	}, "", "")
	m.publish(api.Event{Event: api.EventWorkspaceCreated,
		Data: api.WorkspaceEvent{Workspace: newSpace.name, Path: newSpace.cwd}})
	m.publish(api.Event{Event: api.EventPaneCreated,
		Data: api.PaneEvent{Pane: newPane.id, Name: newPane.name, Kind: newPane.kind, Workspace: newSpace.name}})
	return m.startPane(newSpace, newPane)
}

func (m *Model) apiPaneCreate(payload api.PaneCreate, answer func(any, string, string)) tea.Cmd {
	kind, err := m.apiResolveKind(payload.Kind)
	if err != nil {
		answer(nil, api.CodeInvalidParams, err.Error())
		return nil
	}
	target := m.currentSpace()
	if payload.Workspace != "" {
		target = m.spaceByRef(payload.Workspace)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("workspace %q not found", payload.Workspace))
			return nil
		}
	}
	if target == nil {
		answer(nil, api.CodeError, "no workspace open")
		return nil
	}
	for index, currentSpace := range m.spaces {
		if currentSpace == target {
			m.selected = index
		}
	}

	var newPane *pane
	switch strings.ToLower(payload.Split) {
	case "", "right":
		newPane = m.addPaneSide(target, kind, true, false)
	case "down":
		newPane = m.addPaneSide(target, kind, false, false)
	case "tab":
		newPane = m.addTab(target, kind)
	default:
		answer(nil, api.CodeInvalidParams, fmt.Sprintf("unknown split %q (right, down, tab)", payload.Split))
		return nil
	}
	m.resizePanes(target)
	m.persist()
	answer(map[string]any{
		"type": "pane_created", "workspace": target.name, "pane_id": newPane.id,
	}, "", "")
	m.publish(api.Event{Event: api.EventPaneCreated,
		Data: api.PaneEvent{Pane: newPane.id, Name: newPane.name, Kind: newPane.kind, Workspace: target.name}})
	return m.startPane(target, newPane)
}

// StartAPIServer creates and starts the socket server; send forwards
// requests into the running Bubble Tea program (program.Send).
func StartAPIServer(socket string, send func(api.Request), events *api.Broadcaster) (*api.Server, error) {
	server := api.NewServer(socket, send, events)
	if err := server.Start(); err != nil {
		return nil, err
	}
	return server, nil
}
