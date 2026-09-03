package ui

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Bubble Tea v1 reports some kitty-keyboard sequences as unexported
// "unknown" messages. rawInputBytes recovers their bytes via reflection.
func rawInputBytes(message tea.Msg) ([]byte, bool) {
	switch fmt.Sprintf("%T", message) {
	case "tea.unknownCSISequenceMsg":
		value := reflect.ValueOf(message)
		if value.Kind() == reflect.Slice {
			return value.Bytes(), true
		}
	case "tea.unknownInputByteMsg":
		value := reflect.ValueOf(message)
		if value.CanUint() {
			return []byte{byte(value.Uint())}, true
		}
	}
	return nil, false
}

// Modifier bits from the kitty keyboard protocol (value is 1 + bitmask).
const (
	modShift = 1
	modAlt   = 2
	modCtrl  = 4
)

// parseEnhancedFunctionalKey decodes unmodified functional keys emitted
// with an explicit modifier parameter by the kitty keyboard protocol.
func parseEnhancedFunctionalKey(data []byte) (tea.KeyMsg, bool) {
	var keyType tea.KeyType
	switch string(data) {
	case "\x1b[1;1A":
		keyType = tea.KeyUp
	case "\x1b[1;1B":
		keyType = tea.KeyDown
	case "\x1b[1;1C":
		keyType = tea.KeyRight
	case "\x1b[1;1D":
		keyType = tea.KeyLeft
	case "\x1b[1;1H":
		keyType = tea.KeyHome
	case "\x1b[1;1F":
		keyType = tea.KeyEnd
	case "\x1b[2;1~":
		keyType = tea.KeyInsert
	case "\x1b[3;1~":
		keyType = tea.KeyDelete
	case "\x1b[5;1~":
		keyType = tea.KeyPgUp
	case "\x1b[6;1~":
		keyType = tea.KeyPgDown
	default:
		return tea.KeyMsg{}, false
	}
	return tea.KeyMsg{Type: keyType}, true
}

// parseCSIU decodes a kitty keyboard sequence ESC [ code ; mods u.
// It returns the base codepoint and the modifier bitmask.
func parseCSIU(data []byte) (code rune, mods int, ok bool) {
	text := string(data)
	if !strings.HasPrefix(text, "\x1b[") || !strings.HasSuffix(text, "u") {
		return 0, 0, false
	}
	body := text[2 : len(text)-1]
	fields := strings.Split(body, ";")
	if len(fields) == 0 || fields[0] == "" {
		return 0, 0, false
	}
	codeField := strings.Split(fields[0], ":")[0]
	value, err := strconv.Atoi(codeField)
	if err != nil {
		return 0, 0, false
	}
	mods = 0
	if len(fields) > 1 {
		modField := strings.Split(fields[1], ":")[0]
		modValue, err := strconv.Atoi(modField)
		if err != nil {
			return 0, 0, false
		}
		if modValue > 0 {
			mods = modValue - 1
		}
	}
	return rune(value), mods, true
}

// legacyEncode translates a decoded CSI-u chord into the classic byte
// sequence, when one exists. ctrl+1 and friends have no legacy form.
func legacyEncode(code rune, mods int) []byte {
	var out []byte
	if mods&modAlt != 0 {
		out = append(out, 0x1b)
	}
	switch {
	case mods&modCtrl != 0 && code >= 'a' && code <= 'z':
		return append(out, byte(code-'a'+1))
	case mods&modCtrl != 0:
		return nil
	case code == 27:
		return append(out, 0x1b)
	case code == 13:
		return append(out, '\r')
	case code == 9:
		return append(out, '\t')
	case code == 127:
		return append(out, 0x7f)
	case code >= 32:
		return append(out, []byte(string(code))...)
	}
	return nil
}
