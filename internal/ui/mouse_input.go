package ui

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Bubble Tea v1 can split an SGR mouse report at its 256-byte read boundary
// into an Alt+[ key followed by ordinary runes. Repair that representation
// before keyboard dispatch: forwarding either fragment would type into the
// child and reset local scrollback. Complete MouseMsg events are unchanged.
//
// Only the ambiguous Alt+[ prefix waits, for at most 30ms. Invalid, unfinished,
// and literal keyboard input is replayed unchanged, including paste metadata.
type mouseInputDecoder struct {
	pending []tea.KeyMsg
	body    string
	seq     uint64
}

type mouseInputTimeoutMsg struct{ seq uint64 }

func (d *mouseInputDecoder) decode(message tea.Msg) ([]tea.Msg, tea.Cmd) {
	if timeout, ok := message.(mouseInputTimeoutMsg); ok {
		if timeout.seq == d.seq {
			return d.flush(), nil
		}
		return nil, nil
	}
	key, isKey := message.(tea.KeyMsg)
	if len(d.pending) == 0 {
		if isKey && !key.Paste && key.Alt && key.Type == tea.KeyRunes && string(key.Runes) == "[" {
			d.pending = []tea.KeyMsg{key}
			d.seq++
			seq := d.seq
			return nil, tea.Tick(30*time.Millisecond, func(time.Time) tea.Msg {
				return mouseInputTimeoutMsg{seq: seq}
			})
		}
		return []tea.Msg{message}, nil
	}
	if isKey {
		if key.Type == tea.KeyRunes && !key.Alt && !key.Paste {
			d.pending = append(d.pending, key)
			d.body += string(key.Runes)
			mouse, consumed, incomplete := parseMouseInput(d.body)
			if consumed > 0 {
				messages := []tea.Msg{mouse}
				if rest := d.body[consumed:]; rest != "" {
					messages = append(messages, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(rest)})
				}
				d.clear()
				return messages, nil
			}
			if incomplete && len(d.body) <= 64 {
				return nil, nil
			}
			return d.flush(), nil
		}
		return append(d.flush(), message), nil
	}
	// Output and timer messages may interleave with fragments. A subsequent
	// actual input event, however, must not overtake a pending literal key.
	_, isMouse := message.(tea.MouseMsg)
	_, isRaw := rawInputBytes(message)
	if isMouse || isRaw {
		return append(d.flush(), message), nil
	}
	return []tea.Msg{message}, nil
}

func (d *mouseInputDecoder) clear() {
	d.pending = nil
	d.body = ""
}

func (d *mouseInputDecoder) flush() []tea.Msg {
	messages := make([]tea.Msg, len(d.pending))
	for i, key := range d.pending {
		messages[i] = key
	}
	d.clear()
	return messages
}

// parseMouseInput accepts one SGR body (<button;x;yM/m), optionally followed
// by typing coalesced in the same KeyRunes message. An incomplete prefix is
// distinguished from malformed input so neither can swallow real keystrokes.
func parseMouseInput(body string) (tea.MouseMsg, int, bool) {
	if body == "" {
		return tea.MouseMsg{}, 0, true
	}
	if body[0] != '<' {
		return tea.MouseMsg{}, 0, false
	}
	end := strings.IndexAny(body, "Mm")
	if end < 0 {
		parts := strings.Split(body[1:], ";")
		if len(parts) > 3 {
			return tea.MouseMsg{}, 0, false
		}
		for i, part := range parts {
			if part == "" && i != len(parts)-1 {
				return tea.MouseMsg{}, 0, false
			}
			for _, c := range part {
				if c < '0' || c > '9' {
					return tea.MouseMsg{}, 0, false
				}
			}
		}
		return tea.MouseMsg{}, 0, true
	}
	parts := strings.Split(body[1:end], ";")
	if len(parts) != 3 {
		return tea.MouseMsg{}, 0, false
	}
	var values [3]int
	for i, part := range parts {
		if part == "" {
			return tea.MouseMsg{}, 0, false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return tea.MouseMsg{}, 0, false
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return tea.MouseMsg{}, 0, false
		}
		values[i] = n
	}
	b, x, y := values[0], values[1], values[2]
	if b > 255 || x < 1 || y < 1 {
		return tea.MouseMsg{}, 0, false
	}
	mouse := tea.MouseMsg{X: x - 1, Y: y - 1, Shift: b&4 != 0, Alt: b&8 != 0, Ctrl: b&16 != 0}
	switch {
	case b&128 != 0:
		mouse.Button = tea.MouseButtonBackward + tea.MouseButton(b&3)
	case b&64 != 0:
		mouse.Button = tea.MouseButtonWheelUp + tea.MouseButton(b&3)
	case b&3 == 3:
		mouse.Button = tea.MouseButtonNone
		mouse.Action = tea.MouseActionRelease
	default:
		mouse.Button = tea.MouseButtonLeft + tea.MouseButton(b&3)
	}
	if !tea.MouseEvent(mouse).IsWheel() {
		if b&32 != 0 {
			mouse.Action = tea.MouseActionMotion
		} else if body[end] == 'm' {
			mouse.Action = tea.MouseActionRelease
		}
	}
	return mouse, end + 1, false
}
