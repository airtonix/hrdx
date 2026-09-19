package ui

import (
	"testing"

	"github.com/patriceckhart/hrdx/internal/term"
)

func TestPluginViewOccludesTerminalCursor(t *testing.T) {
	model := newTestModel(t.TempDir())
	model.width, model.height = 100, 30
	model.currentPane().term = term.NewHolderPane(&keyboardCaptureHost{}, 1, 80, 24)
	model.SetCursorSink(NewCursorSink())
	model.publishCursor()
	if _, _, visible := model.cursorSink.get(); !visible {
		t.Fatal("uncovered terminal cursor should be available")
	}
	model.pluginViews = map[string]*pluginView{"example.panel": {ID: "example.panel", Width: 100, Height: 100}}
	model.publishCursor()
	if _, _, visible := model.cursorSink.get(); visible {
		t.Fatal("cursor remained available behind an opaque view")
	}
	delete(model.pluginViews, "example.panel")
	model.publishCursor()
	if _, _, visible := model.cursorSink.get(); !visible {
		t.Fatal("terminal cursor did not recover after view close")
	}
}
