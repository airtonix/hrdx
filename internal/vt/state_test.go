package vt

import "testing"

func TestWriteBuffersUTF8RunesSplitAcrossCalls(t *testing.T) {
	input := []byte("└──┘")
	for split := 1; split < len(input); split++ {
		terminal := New(WithSize(8, 2))
		if n, err := terminal.Write(input[:split]); err != nil || n != split {
			t.Fatalf("split %d first write = (%d, %v)", split, n, err)
		}
		if n, err := terminal.Write(input[split:]); err != nil || n != len(input)-split {
			t.Fatalf("split %d second write = (%d, %v)", split, n, err)
		}
		terminal.Lock()
		for column, want := range []rune("└──┘") {
			if got := terminal.Cell(column, 0).Char; got != want {
				terminal.Unlock()
				t.Fatalf("split %d cell %d = %q, want %q", split, column, got, want)
			}
		}
		terminal.Unlock()
	}
}

func TestWideRunesOccupyTwoCells(t *testing.T) {
	terminal := New(WithSize(8, 2))
	if _, err := terminal.Write([]byte("a界b")); err != nil {
		t.Fatal(err)
	}
	terminal.Lock()
	defer terminal.Unlock()

	if cursor := terminal.Cursor(); cursor.X != 4 || cursor.Y != 0 {
		t.Fatalf("cursor = (%d,%d), want (4,0)", cursor.X, cursor.Y)
	}
	if got := terminal.Cell(1, 0); got.Char != '界' || got.Mode&attrWide == 0 {
		t.Fatalf("wide cell = %+v", got)
	}
	if got := terminal.Cell(2, 0); got.Mode&attrWideDummy == 0 {
		t.Fatalf("wide continuation = %+v", got)
	}
	if got := terminal.Cell(3, 0).Char; got != 'b' {
		t.Fatalf("cell after wide rune = %q, want b", got)
	}
}

func TestOverwritingWideContinuationClearsWholeGlyph(t *testing.T) {
	terminal := New(WithSize(8, 2))
	if _, err := terminal.Write([]byte("界\x1b[1;2Hx")); err != nil {
		t.Fatal(err)
	}
	terminal.Lock()
	defer terminal.Unlock()

	if got := terminal.Cell(0, 0); got.Char != ' ' || got.Mode&(attrWide|attrWideDummy) != 0 {
		t.Fatalf("wide lead after continuation overwrite = %+v", got)
	}
	if got := terminal.Cell(1, 0); got.Char != 'x' || got.Mode&(attrWide|attrWideDummy) != 0 {
		t.Fatalf("replacement cell = %+v", got)
	}
}

func TestEraseIntersectingWideRuneClearsWholeGlyph(t *testing.T) {
	terminal := New(WithSize(8, 2))
	if _, err := terminal.Write([]byte("界x\x1b[1;2H\x1b[K")); err != nil {
		t.Fatal(err)
	}
	terminal.Lock()
	defer terminal.Unlock()

	if got := terminal.Cell(0, 0); got.Char != ' ' || got.Mode&(attrWide|attrWideDummy) != 0 {
		t.Fatalf("wide lead after erase = %+v", got)
	}
}

func TestCombiningMarkDoesNotAdvanceCursor(t *testing.T) {
	terminal := New(WithSize(8, 2))
	if _, err := terminal.Write([]byte("e\u0301x")); err != nil {
		t.Fatal(err)
	}
	terminal.Lock()
	defer terminal.Unlock()
	if cursor := terminal.Cursor(); cursor.X != 2 {
		t.Fatalf("cursor X = %d, want 2", cursor.X)
	}
	if got := terminal.Cell(1, 0).Char; got != 'x' {
		t.Fatalf("cell after combining mark = %q, want x", got)
	}
}
