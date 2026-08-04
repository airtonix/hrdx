package vt

import "github.com/mattn/go-runewidth"

func isControlCode(c rune) bool {
	return c < 0x20 || c == 0177
}

func (t *State) parse(c rune) {
	t.logf("%q", string(c))
	if isControlCode(c) {
		if t.handleControlCodes(c) || t.cur.Attr.Mode&attrGfx == 0 {
			return
		}
	}
	// TODO: update selection; see st.c:2450

	if t.mode&ModeWrap != 0 && t.cur.State&cursorWrapNext != 0 {
		t.lines[t.cur.Y][t.cur.X].Mode |= attrWrap
		t.newline(true)
	}

	width := runewidth.RuneWidth(c)
	if width == 0 {
		// A Glyph stores one rune, so combining marks cannot be retained yet.
		// They must not consume a terminal cell or shift subsequent drawing.
		return
	}
	if width > 2 {
		width = 1
	}
	if width == 2 && t.cur.X == t.cols-1 && t.mode&ModeWrap != 0 {
		t.lines[t.cur.Y][t.cur.X].Mode |= attrWrap
		t.newline(true)
	}

	if t.mode&ModeInsert != 0 && t.cur.X+width < t.cols {
		// TODO: move shiz, look at st.c:2458
		t.logln("insert mode not implemented")
	}

	t.clearWideAt(t.cur.X, t.cur.Y)
	if width == 2 && t.cur.X+1 < t.cols {
		t.clearWideAt(t.cur.X+1, t.cur.Y)
	}

	attr := t.cur.Attr
	if width == 2 && t.cur.X+1 < t.cols {
		attr.Mode |= attrWide
	}
	t.setChar(c, &attr, t.cur.X, t.cur.Y)
	if width == 2 && t.cur.X+1 < t.cols {
		dummy := t.cur.Attr
		dummy.Mode |= attrWideDummy
		t.setChar(0, &dummy, t.cur.X+1, t.cur.Y)
	}
	next := t.cur.X + width
	if next < t.cols {
		t.moveTo(next, t.cur.Y)
	} else {
		t.cur.X = t.cols - 1
		t.cur.State |= cursorWrapNext
	}
}

func (t *State) parseEsc(c rune) {
	if t.handleControlCodes(c) {
		return
	}
	next := t.parse
	t.logf("%q", string(c))
	switch c {
	case '[':
		next = t.parseEscCSI
	case '#':
		next = t.parseEscTest
	case 'P', // DCS - Device Control String
		'_', // APC - Application Program Command
		'^', // PM - Privacy Message
		']', // OSC - Operating System Command
		'k': // old title set compatibility
		t.str.reset()
		t.str.typ = c
		next = t.parseEscStr
	case '(': // set primary charset G0
		next = t.parseEscAltCharset
	case ')', // set secondary charset G1 (ignored)
		'*', // set tertiary charset G2 (ignored)
		'+': // set quaternary charset G3 (ignored)
	case 'D': // IND - linefeed
		if t.cur.Y == t.bottom {
			t.scrollUp(t.top, 1)
		} else {
			t.moveTo(t.cur.X, t.cur.Y+1)
		}
	case 'E': // NEL - next line
		t.newline(true)
	case 'H': // HTS - horizontal tab stop
		t.tabs[t.cur.X] = true
	case 'M': // RI - reverse index
		if t.cur.Y == t.top {
			t.scrollDown(t.top, 1)
		} else {
			t.moveTo(t.cur.X, t.cur.Y-1)
		}
	case 'Z': // DECID - identify terminal
		// TODO: write to our writer our id
	case 'c': // RIS - reset to initial state
		t.reset()
	case '=': // DECPAM - application keypad
		t.mode |= ModeAppKeypad
	case '>': // DECPNM - normal keypad
		t.mode &^= ModeAppKeypad
	case '7': // DECSC - save cursor
		t.saveCursor()
	case '8': // DECRC - restore cursor
		t.restoreCursor()
	case '\\': // ST - stop
	default:
		t.logf("unknown ESC sequence '%c'\n", c)
	}
	t.state = next
}

func (t *State) parseEscCSI(c rune) {
	if t.handleControlCodes(c) {
		return
	}
	t.logf("%q", string(c))
	if t.csi.put(byte(c)) {
		t.state = t.parse
		t.handleCSI()
	}
}

func (t *State) parseEscStr(c rune) {
	t.logf("%q", string(c))
	switch c {
	case '\033':
		t.state = t.parseEscStrEnd
	case '\a': // backwards compatiblity to xterm
		t.state = t.parse
		t.handleSTR()
	default:
		t.str.put(c)
	}
}

func (t *State) parseEscStrEnd(c rune) {
	if t.handleControlCodes(c) {
		return
	}
	t.logf("%q", string(c))
	t.state = t.parse
	if c == '\\' {
		t.handleSTR()
	}
}

func (t *State) parseEscAltCharset(c rune) {
	if t.handleControlCodes(c) {
		return
	}
	t.logf("%q", string(c))
	switch c {
	case '0': // line drawing set
		t.cur.Attr.Mode |= attrGfx
	case 'B': // USASCII
		t.cur.Attr.Mode &^= attrGfx
	case 'A', // UK (ignored)
		'<', // multinational (ignored)
		'5', // Finnish (ignored)
		'C', // Finnish (ignored)
		'K': // German (ignored)
	default:
		t.logf("unknown alt. charset '%c'\n", c)
	}
	t.state = t.parse
}

func (t *State) parseEscTest(c rune) {
	if t.handleControlCodes(c) {
		return
	}
	// DEC screen alignment test
	if c == '8' {
		for y := 0; y < t.rows; y++ {
			for x := 0; x < t.cols; x++ {
				t.setChar('E', &t.cur.Attr, x, y)
			}
		}
	}
	t.state = t.parse
}

func (t *State) handleControlCodes(c rune) bool {
	if !isControlCode(c) {
		return false
	}
	switch c {
	// HT
	case '\t':
		t.putTab(true)
	// BS
	case '\b':
		t.moveTo(t.cur.X-1, t.cur.Y)
	// CR
	case '\r':
		t.moveTo(0, t.cur.Y)
	// LF, VT, LF
	case '\f', '\v', '\n':
		// go to first col if mode is set
		t.newline(t.mode&ModeCRLF != 0)
	// BEL
	case '\a':
		// TODO: emit sound
		// TODO: window alert if not focused
	// ESC
	case 033:
		t.csi.reset()
		t.state = t.parseEsc
	// SO, SI
	case 016, 017:
		// different charsets not supported. apps should use the correct
		// alt charset escapes, probably for line drawing
	// SUB, CAN
	case 032, 030:
		t.csi.reset()
	// ignore ENQ, NUL, XON, XOFF, DEL
	case 005, 000, 021, 023, 0177:
	default:
		return false
	}
	return true
}
