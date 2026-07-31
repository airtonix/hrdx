package term

import (
	"fmt"
	"strings"

	"github.com/patriceckhart/hrdx/internal/vt"
)

// Attribute bits mirrored from the vt package's unexported constants.
const (
	attrReverse   = 1 << 0
	attrUnderline = 1 << 1
	attrBold      = 1 << 2
	attrItalic    = 1 << 4
	attrBlink     = 1 << 5
)

// RenderLines returns the current view as ANSI-styled text, one string per
// row, honoring the scrollback offset. When focused, the cell under the
// cursor is drawn inverted (only in live view). An active selection is drawn
// reversed.
func (p *Pane) RenderLines(focused bool) []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.vt.Lock()
	defer p.vt.Unlock()

	cols, rows := p.vt.Size()
	cursor := p.vt.Cursor()
	historyLen := p.vt.HistoryLen()
	offset := p.scrollOffset
	if offset > historyLen {
		offset = historyLen
	}
	showCursor := focused && p.vt.CursorVisible() && !p.exited && offset == 0

	selStartL, selStartX, selEndL, selEndX := 0, 0, -1, -1
	if p.selActive {
		selStartL, selStartX, selEndL, selEndX = p.orderedSelection()
	}

	lines := make([]string, 0, rows)
	var out strings.Builder
	for y := 0; y < rows; y++ {
		out.Reset()
		last := ""
		// lineIndex addresses history (0..historyLen-1) then screen rows.
		lineIndex := historyLen - offset + y
		var historyGlyphs []vt.Glyph
		onScreenRow := -1
		if lineIndex < historyLen {
			historyGlyphs = p.vt.HistoryLine(lineIndex)
		} else {
			onScreenRow = lineIndex - historyLen
		}

		for x := 0; x < cols; x++ {
			var glyph vt.Glyph
			switch {
			case historyGlyphs != nil:
				if x < len(historyGlyphs) {
					glyph = historyGlyphs[x]
				} else {
					glyph = vt.Glyph{Char: ' ', FG: vt.DefaultFG, BG: vt.DefaultBG}
				}
			case onScreenRow >= 0 && onScreenRow < rows:
				glyph = p.vt.Cell(x, onScreenRow)
			default:
				glyph = vt.Glyph{Char: ' ', FG: vt.DefaultFG, BG: vt.DefaultBG}
			}

			if selEndL >= 0 && inSelection(lineIndex, x, selStartL, selStartX, selEndL, selEndX) {
				glyph.Mode ^= attrReverse
			}
			if showCursor && onScreenRow == cursor.Y && x == cursor.X {
				glyph.Mode ^= attrReverse
			}

			code := sgr(glyph)
			if code != last {
				out.WriteString(code)
				last = code
			}
			char := glyph.Char
			if char == 0 {
				char = ' '
			}
			out.WriteRune(char)
		}
		out.WriteString("\x1b[0m")
		lines = append(lines, out.String())
	}
	return lines
}

func inSelection(line, x, startL, startX, endL, endX int) bool {
	if line < startL || line > endL {
		return false
	}
	if line == startL && line == endL {
		return x >= startX && x <= endX
	}
	if line == startL {
		return x >= startX
	}
	if line == endL {
		return x <= endX
	}
	return true
}

// Render returns the view as one newline-joined string.
func (p *Pane) Render(focused bool) string {
	return strings.Join(p.RenderLines(focused), "\n")
}

func sgr(glyph vt.Glyph) string {
	var codes []string
	codes = append(codes, "0")
	if glyph.Mode&attrBold != 0 {
		codes = append(codes, "1")
	}
	if glyph.Mode&attrItalic != 0 {
		codes = append(codes, "3")
	}
	if glyph.Mode&attrUnderline != 0 {
		codes = append(codes, "4")
	}
	if glyph.Mode&attrBlink != 0 {
		codes = append(codes, "5")
	}
	if glyph.Mode&attrReverse != 0 {
		codes = append(codes, "7")
	}
	codes = append(codes, colorCodes(glyph.FG, false)...)
	codes = append(codes, colorCodes(glyph.BG, true)...)
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func colorCodes(color vt.Color, background bool) []string {
	base := 38
	reset := "39"
	if background {
		base = 48
		reset = "49"
	}
	switch {
	case color == vt.DefaultFG, color == vt.DefaultBG, color == vt.DefaultCursor:
		return []string{reset}
	case uint32(color) < 256:
		return []string{fmt.Sprintf("%d;5;%d", base, uint32(color))}
	case uint32(color) < 1<<24:
		value := uint32(color)
		return []string{fmt.Sprintf("%d;2;%d;%d;%d", base, value>>16&0xff, value>>8&0xff, value&0xff)}
	default:
		return []string{reset}
	}
}
