package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBuildPrefixKeysDefaults(t *testing.T) {
	keys := buildPrefixKeys(nil)
	for key, action := range map[string]string{
		"c": "picker-right", "a": "agent-right", "A": "agent-down",
		"/": "find", "tab": "pane-next", "q": "quit",
	} {
		if keys[key] != action {
			t.Fatalf("keys[%q] = %q, want %q", key, keys[key], action)
		}
	}
}

func TestBuildPrefixKeysOverride(t *testing.T) {
	keys := buildPrefixKeys(map[string]string{"find": "f", "agent-cycle": "g"})
	if keys["f"] != "find" || keys["g"] != "agent-cycle" {
		t.Fatalf("overrides not applied: %v", keys)
	}
	if keys["/"] == "find" {
		t.Fatal("override should replace the default key")
	}
}

func TestLoadKeymap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, keymapFile),
		[]byte(`{"find": "f", "bogus": "x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	overrides, problem := loadKeymap(dir)
	if overrides["find"] != "f" {
		t.Fatalf("overrides = %v, want find -> f", overrides)
	}
	if _, ok := overrides["bogus"]; ok {
		t.Fatal("unknown action must be dropped")
	}
	if problem == "" {
		t.Fatal("unknown action should surface a problem message")
	}
}

func TestPrefixAgentSplit(t *testing.T) {
	model := newTestModel("/tmp/api")

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	model = updated.(Model)
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(Model)

	currentTab := model.spaces[0].tab()
	if len(currentTab.panes) != 2 {
		t.Fatalf("panes = %d, want 2 after ctrl+b a", len(currentTab.panes))
	}
	if kind := currentTab.panes[1].kind; kind != model.config.DefaultAgent {
		t.Fatalf("new pane kind = %q, want default agent %q", kind, model.config.DefaultAgent)
	}
}

func TestPrefixHintEntriesUseSpaceSeparator(t *testing.T) {
	model := newTestModel("/tmp/api")
	for _, entry := range model.prefixHintEntries() {
		for _, r := range entry[0] {
			if r == '/' && entry[1] != "find" {
				t.Fatalf("hint keys %q contain a slash separator", entry[0])
			}
		}
	}
}
