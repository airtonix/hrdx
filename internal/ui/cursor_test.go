package ui

import (
	"os"
	"testing"

	"github.com/charmbracelet/x/term"
)

// Bubble Tea only issues window size queries when the program output
// satisfies term.File. If the cursor wrapper hides Fd, the TUI never
// receives a WindowSizeMsg and hangs on the startup screen.
func TestCursorOutputSatisfiesTermFile(t *testing.T) {
	out := NewCursorOutput(os.Stdout, NewCursorSink())
	if _, ok := out.(term.File); !ok {
		t.Fatal("cursor output must implement term.File (Fd/Read/Write/Close)")
	}
}

func TestCursorOutputAppendsCursorMove(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()

	sink := NewCursorSink()
	sink.Set(4, 2, true)
	out := NewCursorOutput(write, sink)
	if _, err := out.Write([]byte("frame")); err != nil {
		t.Fatal(err)
	}
	write.Close()

	buffer := make([]byte, 64)
	n, _ := read.Read(buffer)
	if got, want := string(buffer[:n]), "frame\x1b[3;5H"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
