package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// CursorSink shares the desired hardware cursor position between the model
// (which knows where the focused input point is) and the render output
// (which repositions the terminal cursor after every frame). Bubble Tea v1
// parks the hardware cursor at the bottom-left after rendering; terminals
// draw IME and dead-key composition previews at the hardware cursor, so
// without repositioning those previews appear in the corner instead of at
// the focused pane's input line.
type CursorSink struct {
	mu   sync.Mutex
	x, y int
	ok   bool
}

func NewCursorSink() *CursorSink { return &CursorSink{} }

// Set stores the cursor cell in screen coordinates (0-based), or marks it
// unavailable when ok is false.
func (s *CursorSink) Set(x, y int, ok bool) {
	s.mu.Lock()
	s.x, s.y, s.ok = x, y, ok
	s.mu.Unlock()
}

func (s *CursorSink) get() (int, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.x, s.y, s.ok
}

// cursorOutput wraps the program's output file and appends a cursor-move
// after every write, keeping the hardware cursor on the focused input
// point. It must keep exposing Fd(): Bubble Tea detects the TTY through it
// for window size queries; hiding it would leave the program without
// WindowSizeMsg, stuck on the startup screen.
type cursorOutput struct {
	out  *os.File
	sink *CursorSink
}

// NewCursorOutput returns an output for tea.WithOutput that keeps the
// hardware cursor at the position published through sink.
func NewCursorOutput(out *os.File, sink *CursorSink) io.Writer {
	return cursorOutput{out: out, sink: sink}
}

func (o cursorOutput) Write(p []byte) (int, error) {
	n, err := o.out.Write(p)
	if err != nil {
		return n, err
	}
	if x, y, ok := o.sink.get(); ok {
		_, _ = fmt.Fprintf(o.out, "\x1b[%d;%dH", y+1, x+1)
	}
	return n, err
}

// Fd exposes the underlying TTY descriptor for terminal size queries.
func (o cursorOutput) Fd() uintptr { return o.out.Fd() }

// Read satisfies the term.File interface Bubble Tea checks for.
func (o cursorOutput) Read(p []byte) (int, error) { return o.out.Read(p) }

// Close closes the underlying file.
func (o cursorOutput) Close() error { return o.out.Close() }
