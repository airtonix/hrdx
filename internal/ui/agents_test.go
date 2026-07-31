package ui

import (
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

func TestAgentKinds(t *testing.T) {
	for _, kind := range []string{"zot", "pi", "claude", "codex"} {
		if !isAgentKind(kind) {
			t.Fatalf("%s should be an agent kind", kind)
		}
	}
	if isAgentKind("shell") || isAgentKind("nope") {
		t.Fatal("shell/nope must not be agent kinds")
	}
}

func TestBinaryForHonorsOverride(t *testing.T) {
	config := Config{AgentBins: map[string]string{"claude": "/opt/claude"}}
	if got := config.binaryFor("claude"); got != "/opt/claude" {
		t.Fatalf("binaryFor = %q, want /opt/claude", got)
	}
	if got := config.binaryFor("codex"); got != "codex" {
		t.Fatalf("binaryFor = %q, want codex", got)
	}
}

func TestDefaultAgentUsedForNewPanes(t *testing.T) {
	model := New(Config{Shell: "/bin/sh", DefaultAgent: "claude"}, []string{"/tmp/api"}, "", state.State{})
	first := model.spaces[0].tab()
	if first.panes[0].kind != "claude" {
		t.Fatalf("pane kind = %q, want claude", first.panes[0].kind)
	}
	if first.panes[0].name != "claude 1" {
		t.Fatalf("pane name = %q, want claude 1", first.panes[0].name)
	}
}

func TestInvalidDefaultAgentFallsBack(t *testing.T) {
	model := New(Config{Shell: "/bin/sh", DefaultAgent: "gemini"}, []string{"/tmp/api"}, "", state.State{})
	if model.config.DefaultAgent != "zot" {
		t.Fatalf("default agent = %q, want zot", model.config.DefaultAgent)
	}
}

func TestRestoreKeepsAgentKinds(t *testing.T) {
	saved := state.State{Workspaces: []state.Workspace{{
		Name: "api", CWD: "/tmp/api",
		Tabs: []state.Tab{{Panes: []state.Pane{
			{Kind: "codex", Name: "codex 1"},
			{Kind: "pi", Name: "pi 1"},
			{Kind: "mystery", Name: "x"},
		}}},
	}}}
	model := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	panes := model.spaces[0].tab().panes
	if panes[0].kind != "codex" || !panes[0].resume {
		t.Fatalf("pane 0 = %+v, want resumable codex", panes[0])
	}
	if panes[1].kind != "pi" || !panes[1].resume {
		t.Fatalf("pane 1 = %+v, want resumable pi", panes[1])
	}
	if panes[2].kind != "shell" || panes[2].resume {
		t.Fatalf("pane 2 = %+v, want non-resuming shell fallback", panes[2])
	}
}
