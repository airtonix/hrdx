package ui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/term"
)

// scrollInputProbe runs real Bubble Tea input decoding without starting PTYs.
type scrollInputProbe struct {
	model      Model
	resets     int
	reachedTop bool
	top        int
}

func (*scrollInputProbe) Init() tea.Cmd { return nil }
func (*scrollInputProbe) View() string  { return "" }
func (p *scrollInputProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlC {
		return p, tea.Quit
	}
	before := p.model.currentPane().term.ScrollOffset()
	updated, cmd := p.model.Update(msg)
	p.model = updated.(Model)
	after := p.model.currentPane().term.ScrollOffset()
	if after == p.top {
		p.reachedTop = true
	}
	if before > 0 && after == 0 {
		p.resets++
	}
	return p, cmd
}

func runScrollInput(t *testing.T, history int, captures bool, input io.Reader) (*scrollInputProbe, *keyboardCaptureHost) {
	t.Helper()
	model := newTestModel(t.TempDir())
	host := &keyboardCaptureHost{}
	target := model.currentPane()
	target.term = term.NewHolderPane(host, 1, 80, 6)
	target.running = true
	t.Cleanup(target.term.MarkExited)
	target.term.Feed([]byte(strings.Repeat("line\r\n", history+5)))
	if captures {
		target.term.Feed([]byte("\x1b[?1000h\x1b[?1006h"))
	}
	probe := &scrollInputProbe{model: model, top: history}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	program := tea.NewProgram(probe, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	return probe, host
}

func TestRapidWheelInputStopsAtOldestLine(t *testing.T) {
	const wheel = "\x1b[<64;40;5M"
	// Reach the 553-line boundary with complete reports, then continue upward
	// in a burst. The final upward steps must stay at 553, not jump to zero.
	readers := make([]io.Reader, 0, 186)
	for i := 0; i < 185; i++ {
		readers = append(readers, strings.NewReader(wheel))
	}
	readers = append(readers, strings.NewReader(strings.Repeat(wheel, 1000)+"\x03"))
	probe, host := runScrollInput(t, 553, false, io.MultiReader(readers...))
	if !probe.reachedTop {
		t.Fatal("test did not reach the oldest line")
	}
	if probe.resets != 0 || len(host.bytes(probe.model.currentPane().term)) != 0 {
		t.Fatalf("upward wheel input reset scrollback %d times and sent %d bytes to child", probe.resets, len(host.bytes(probe.model.currentPane().term)))
	}
	if got := probe.model.currentPane().term.ScrollOffset(); got != 553 {
		t.Fatalf("offset=%d, want clamped at 553", got)
	}
}

func TestRapidWheelInputPreservesEveryStep(t *testing.T) {
	const wheel = "\x1b[<64;40;5M"
	probe, host := runScrollInput(t, 4000, false, strings.NewReader(strings.Repeat(wheel, 1000)+"\x03"))
	if got := probe.model.currentPane().term.ScrollOffset(); got != 3000 {
		t.Fatalf("offset=%d, want 3000 (all 1000 upward wheel events)", got)
	}
	if probe.resets != 0 || len(host.bytes(probe.model.currentPane().term)) != 0 {
		t.Fatalf("resets=%d, child input bytes=%d", probe.resets, len(host.bytes(probe.model.currentPane().term)))
	}
}

func TestRapidWheelInputStillReachesCapturingChild(t *testing.T) {
	const up = "\x1b[<64;40;5M"
	const down = "\x1b[<65;40;5M"
	probe, host := runScrollInput(t, 553, true, strings.NewReader(strings.Repeat(up+down, 500)+"\x03"))
	// Child coordinates are pane-local, just as for intact MouseMsg input.
	model := newTestModel(t.TempDir())
	panes := model.layoutFor(model.currentSpace().tab())
	inner := panes[0].r.inner()
	x := 39 - model.sidebarContentWidth() - 1 - inner.x
	y := 4 - 2 - inner.y
	wantUp := encodeSGRMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp}, x, y)
	wantDown := encodeSGRMouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown}, x, y)
	if want := strings.Repeat(string(wantUp)+string(wantDown), 500); string(host.bytes(probe.model.currentPane().term)) != want {
		t.Fatalf("child did not receive all 1000 wheel events in order (got %d bytes, want %d)", len(host.bytes(probe.model.currentPane().term)), len(want))
	}
}
