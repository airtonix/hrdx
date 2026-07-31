package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

// resetHarnesses removes all custom entries registered by a test.
func resetHarnesses() {
	kept := agentSpecs[:0]
	for _, spec := range agentSpecs {
		if !spec.custom {
			kept = append(kept, spec)
		}
	}
	agentSpecs = kept
}

func writeHarnessFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, harnessFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadHarnessesRegistersCustomKinds(t *testing.T) {
	defer resetHarnesses()
	dir := writeHarnessFile(t, `[
		{"kind": "aider", "binary": "aider", "args": ["--no-auto-commits"],
		 "resume": ["--restore-chat-history"], "busy": "Waiting for the model"},
		{"kind": "goose"}
	]`)

	if problem := loadHarnesses(dir); problem != "" {
		t.Fatalf("loadHarnesses = %q, want no problem", problem)
	}
	if !isAgentKind("aider") || !isAgentKind("goose") {
		t.Fatal("custom kinds should register as agents")
	}
	spec := agentByKind("aider")
	if spec.binary != "aider" || len(spec.args) != 1 || spec.busyMatch != "Waiting for the model" {
		t.Fatalf("aider spec = %+v", spec)
	}
	if !spec.custom {
		t.Fatal("registered harness must be marked custom")
	}
	if agentByKind("goose").binary != "goose" {
		t.Fatal("binary should default to kind")
	}
}

func TestLoadHarnessesRejectsInvalidEntries(t *testing.T) {
	defer resetHarnesses()
	dir := writeHarnessFile(t, `[
		{"kind": "zot", "binary": "evil"},
		{"kind": "shell"},
		{"binary": "nameless"},
		{"kind": "ok"}
	]`)

	problem := loadHarnesses(dir)
	if problem == "" {
		t.Fatal("expected a problem report for invalid entries")
	}
	if agentByKind("zot").binary != "zot" {
		t.Fatal("built-in zot must not be overridden")
	}
	if !isAgentKind("ok") {
		t.Fatal("valid entries should still register")
	}
}

func TestLoadHarnessesMissingFileIsFine(t *testing.T) {
	if problem := loadHarnesses(t.TempDir()); problem != "" {
		t.Fatalf("missing file reported %q", problem)
	}
}

func TestLoadHarnessesBadJSON(t *testing.T) {
	dir := writeHarnessFile(t, "{not json")
	if problem := loadHarnesses(dir); problem == "" {
		t.Fatal("bad json should be reported")
	}
}

func TestCustomHarnessInSettingsAndToggle(t *testing.T) {
	defer resetHarnesses()
	if err := registerHarness(harnessSpec{Kind: "aider"}); err != nil {
		t.Fatal(err)
	}
	model := New(Config{Shell: "/bin/sh"}, []string{"/tmp/api"}, "", state.State{})

	found := false
	for _, row := range model.settingsRows() {
		if row.action == "toggle:aider" {
			found = true
		}
	}
	if !found {
		t.Fatal("custom harness missing from the settings rows")
	}

	model.toggleAgent("aider")
	if !model.disabled["aider"] {
		t.Fatal("custom harness should toggle off")
	}
	saved := model.snapshot()
	if len(saved.DisabledAgents) != 1 || saved.DisabledAgents[0] != "aider" {
		t.Fatalf("snapshot disabled = %v, want [aider]", saved.DisabledAgents)
	}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	if !restored.disabled["aider"] {
		t.Fatal("restore should keep the custom harness disabled")
	}
}

func TestCustomHarnessRestoresPanes(t *testing.T) {
	defer resetHarnesses()
	if err := registerHarness(harnessSpec{Kind: "aider", Resume: []string{"--restore"}}); err != nil {
		t.Fatal(err)
	}
	saved := state.State{Workspaces: []state.Workspace{{
		Name: "api", CWD: "/tmp/api",
		Tabs: []state.Tab{{Panes: []state.Pane{{Kind: "aider", Name: "aider 1"}}}},
	}}}
	model := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	restored := model.spaces[0].tab().panes[0]
	if restored.kind != "aider" || !restored.resume {
		t.Fatalf("restored pane = %+v, want resumable aider", restored)
	}
}

func TestReRegisterReplacesCustomHarness(t *testing.T) {
	defer resetHarnesses()
	if err := registerHarness(harnessSpec{Kind: "aider", Busy: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := registerHarness(harnessSpec{Kind: "aider", Busy: "new"}); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, spec := range agentSpecs {
		if spec.kind == "aider" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("aider registered %d times, want 1", count)
	}
	if agentByKind("aider").busyMatch != "new" {
		t.Fatal("re-registration should replace the entry")
	}
}
