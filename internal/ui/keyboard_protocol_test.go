package ui

import (
	"bytes"
	"testing"

	"github.com/airtonix/hrdx/internal/term"
)

// keyboardCaptureHost is the holder-side transport needed by NewHolderPane.
// It records only pane input; the other operations are irrelevant here.
type keyboardCaptureHost struct {
	input []byte
}

func (h *keyboardCaptureHost) Write(_ int64, data []byte) {
	h.input = append(h.input, data...)
}

func (*keyboardCaptureHost) Resize(int64, int, int)  {}
func (*keyboardCaptureHost) Kill(int64)              {}
func (*keyboardCaptureHost) Foreground(int64) string { return "" }

func TestEnhancedFunctionalKeysDriveLocalModes(t *testing.T) {
	model := newTestModel("/tmp/api")
	target := model.currentPane()
	host := &keyboardCaptureHost{}
	target.term = term.NewHolderPane(host, 1, 80, 24)
	target.running = true
	model.openKindPicker("tab", model.currentSpace(), "", rect{x: 1, y: 1})
	model.menuIndex = 1

	updated, _ := model.updateRaw([]byte("\x1b[1;1A"))
	model = updated.(Model)
	if model.menuIndex != 0 {
		t.Fatalf("menuIndex after enhanced up = %d, want 0", model.menuIndex)
	}
	updated, _ = model.updateRaw([]byte("\x1b[1;1B"))
	model = updated.(Model)
	if model.menuIndex != 1 {
		t.Fatalf("menuIndex after enhanced down = %d, want 1", model.menuIndex)
	}
	model.navKeys = buildNavigationKeys(map[string]string{"navigate-up": "home"})
	updated, _ = model.updateRaw([]byte("\x1b[1;1H"))
	model = updated.(Model)
	if model.menuIndex != 0 {
		t.Fatalf("menuIndex after enhanced custom home = %d, want 0", model.menuIndex)
	}
	model.updateRaw([]byte("\x1b[999~"))
	if len(host.input) != 0 {
		t.Fatalf("local mode leaked raw input to pane: %q", host.input)
	}
}

func TestEnhancedFunctionalKeysFollowChildProtocolInTerminalMode(t *testing.T) {
	model := newTestModel("/tmp/api")
	target := model.currentPane()
	host := &keyboardCaptureHost{}
	target.term = term.NewHolderPane(host, 1, 80, 24)
	target.running = true
	input := []byte("\x1b[1;1A")

	model.updateRaw(input)
	if want := []byte("\x1b[A"); !bytes.Equal(host.input, want) {
		t.Fatalf("legacy terminal input = %q, want %q", host.input, want)
	}

	host.input = nil
	target.term.Feed([]byte("\x1b[>1u"))
	model.updateRaw(input)
	if !bytes.Equal(host.input, input) {
		t.Fatalf("kitty terminal input = %q, want %q", host.input, input)
	}
}

func TestCSIUControlsReturnToLegacyEncodingAfterChildExitsAltScreen(t *testing.T) {
	model := newTestModel("/tmp/api")
	target := model.currentPane()
	target.kind = "shell"
	host := &keyboardCaptureHost{}
	target.term = term.NewHolderPane(host, 1, 80, 24)
	target.running = true

	// While the full-screen child is active, its kitty request and xterm
	// fallback make CSI-u the correct encoding to pass through.
	target.term.Feed([]byte("\x1b[?1049h\x1b[>1u\x1b[>4;2m"))
	childInput := []byte("\x1b[97;5u") // ctrl+a
	model.updateRaw(childInput)
	if !bytes.Equal(host.input, childInput) {
		t.Fatalf("input while child active = %q, want CSI-u %q", host.input, childInput)
	}

	// The child returns to the shell without a kitty pop or xterm fallback
	// reset. The alternate screen switch still restores the shell's keyboard
	// state, so common line-editing controls return to classic bytes.
	host.input = nil
	target.term.Feed([]byte("\x1b[?1049l"))
	for _, input := range [][]byte{
		[]byte("\x1b[97;5u"),  // ctrl+a
		[]byte("\x1b[101;5u"), // ctrl+e
		[]byte("\x1b[108;5u"), // ctrl+l
	} {
		model.updateRaw(input)
	}
	if want := []byte{0x01, 0x05, 0x0c}; !bytes.Equal(host.input, want) {
		t.Fatalf("shell input after child exit = %q, want legacy controls %q", host.input, want)
	}
}
