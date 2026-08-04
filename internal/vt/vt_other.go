//go:build plan9 || nacl || windows
// +build plan9 nacl windows

package vt

import (
	"bufio"
	"unicode"
	"unicode/utf8"
)

type terminal struct {
	*State
	utf8Pending []byte
}

func newTerminal(info TerminalInfo) *terminal {
	t := &terminal{State: newState(info.w)}
	t.init(info.cols, info.rows)
	return t
}

func (t *terminal) init(cols, rows int) {
	t.numlock = true
	t.state = t.parse
	t.cur.Attr.FG = DefaultFG
	t.cur.Attr.BG = DefaultBG
	t.Resize(cols, rows)
	t.reset()
}

func (t *terminal) Write(p []byte) (int, error) {
	t.lock()
	defer t.unlock()

	data := make([]byte, 0, len(t.utf8Pending)+len(p))
	data = append(data, t.utf8Pending...)
	data = append(data, p...)
	t.utf8Pending = t.utf8Pending[:0]

	for len(data) > 0 {
		if !utf8.FullRune(data) {
			t.utf8Pending = append(t.utf8Pending, data...)
			break
		}
		c, size := utf8.DecodeRune(data)
		data = data[size:]
		if c == unicode.ReplacementChar && size == 1 {
			t.logln("invalid utf8 sequence")
			continue
		}
		t.put(c)
	}
	return len(p), nil
}

// TODO: add tests for expected blocking behavior
func (t *terminal) Parse(br *bufio.Reader) error {
	var locked bool
	defer func() {
		if locked {
			t.unlock()
		}
	}()
	for {
		c, sz, err := br.ReadRune()
		if err != nil {
			return err
		}
		if c == unicode.ReplacementChar && sz == 1 {
			t.logln("invalid utf8 sequence")
			break
		}
		if !locked {
			t.lock()
			locked = true
		}

		// put rune for parsing and update state
		t.put(c)

		// break if our buffer is empty, or if buffer contains an
		// incomplete rune.
		n := br.Buffered()
		if n == 0 || (n < 4 && !fullRuneBuffered(br)) {
			break
		}
	}
	return nil
}

func fullRuneBuffered(br *bufio.Reader) bool {
	n := br.Buffered()
	buf, err := br.Peek(n)
	if err != nil {
		return false
	}
	return utf8.FullRune(buf)
}

func (t *terminal) Resize(cols, rows int) {
	t.lock()
	defer t.unlock()
	_ = t.resize(cols, rows)
}
