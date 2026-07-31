package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSettingsOpenToggleClose(t *testing.T) {
	model := newTestModel("/tmp/api")

	// ctrl+b , opens the settings window.
	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	model = updated.(Model)
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	model = updated.(Model)
	if model.mode != modeSettings {
		t.Fatalf("mode = %d, want modeSettings", model.mode)
	}

	// Enter toggles the selected agent row.
	first := model.settingsRows()[0]
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	kind := first.action[len("toggle:"):]
	if !model.disabled[kind] {
		t.Fatalf("agent %q should be disabled after enter", kind)
	}

	// Tab switches to the sound section.
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.settingsTab != 1 {
		t.Fatalf("settingsTab = %d, want 1", model.settingsTab)
	}
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !model.soundOn {
		t.Fatal("sound should be on after toggling in the sound tab")
	}

	// Esc closes.
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.mode != modeTerminal {
		t.Fatalf("mode = %d, want modeTerminal after esc", model.mode)
	}
}

func TestSettingsMouseOutsideCloses(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.openSettings()
	updated, _ := model.updateMouse(tea.MouseMsg{X: 0, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.mode != modeTerminal {
		t.Fatalf("mode = %d, want modeTerminal after outside click", got.mode)
	}
}

func TestSettingsSidebarEntryOpens(t *testing.T) {
	model := newTestModel("/tmp/api")
	// The pinned settings row sits above the trailing blank line; body
	// height is m.height-2, so its body row is height-2-2 and its screen
	// Y is one more than that.
	y := model.height - 3
	updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.mode != modeSettings {
		t.Fatalf("mode = %d, want modeSettings after sidebar click", got.mode)
	}
}

func TestTrackBusyDebouncesSound(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.soundOn = true
	target := model.spaces[0].tab().panes[0]

	// Idle pane that was never busy: no command.
	if cmd := model.trackBusy(target); cmd != nil {
		t.Fatal("idle pane must not schedule a sound")
	}

	// Simulate a busy phase, then idle: a confirm command is scheduled.
	model.wasBusy[target.id] = true
	if cmd := model.trackBusy(target); cmd == nil {
		t.Fatal("busy -> idle should schedule the confirm tick")
	}
	if model.wasBusy[target.id] {
		t.Fatal("transition should consume the busy mark")
	}

	// With sound off nothing is scheduled.
	model.soundOn = false
	model.wasBusy[target.id] = true
	if cmd := model.trackBusy(target); cmd != nil {
		t.Fatal("sound off must not schedule a confirm tick")
	}
}
