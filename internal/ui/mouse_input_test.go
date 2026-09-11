package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func mousePrefixKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune("[")}
}

func mouseBodyKey(body string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(body)}
}

func TestMouseInputReassemblesEveryBodyBoundary(t *testing.T) {
	const body = "<64;40;5M"
	for split := 1; split < len(body); split++ {
		var decoder mouseInputDecoder
		if messages, cmd := decoder.decode(mousePrefixKey()); len(messages) != 0 || cmd == nil {
			t.Fatal("prefix was not held with a fallback timeout")
		}
		if messages, _ := decoder.decode(mouseBodyKey(body[:split])); len(messages) != 0 {
			t.Fatalf("split=%d: incomplete report emitted input: %v", split, messages)
		}
		// PTY notifications must not flush an in-progress host input report.
		if messages, _ := decoder.decode(spinTickMsg{}); len(messages) != 1 {
			t.Fatal("unrelated UI update was swallowed")
		}
		messages, _ := decoder.decode(mouseBodyKey(body[split:]))
		want := []tea.Msg{tea.MouseMsg{X: 39, Y: 4, Button: tea.MouseButtonWheelUp}}
		if !reflect.DeepEqual(messages, want) {
			t.Fatalf("split=%d: messages=%v, want %v", split, messages, want)
		}
		if messages, _ := decoder.decode(mouseInputTimeoutMsg{decoder.seq}); len(messages) != 0 {
			t.Fatal("completed report replayed on timeout")
		}
	}
}

func TestMouseInputPreservesLiteralKeys(t *testing.T) {
	for _, next := range []tea.KeyMsg{
		mouseBodyKey("hello"),
		mouseBodyKey("<64;-1;5M"),
		mouseBodyKey("<64;0;5M"),
		mouseBodyKey("<64;9999999999999999999999999;5M"),
		mouseBodyKey("<" + strings.Repeat("1", 65)),
		{Type: tea.KeyCtrlA},
		{Type: tea.KeyEscape},
		{Type: tea.KeyRunes, Runes: []rune("<64;40;5M"), Paste: true},
	} {
		var decoder mouseInputDecoder
		prefix := mousePrefixKey()
		decoder.decode(prefix)
		messages, _ := decoder.decode(next)
		want := []tea.Msg{prefix, next}
		if !reflect.DeepEqual(messages, want) {
			t.Fatalf("key=%v: messages=%v, want unchanged keys %v", next, messages, want)
		}
	}
}

func TestMouseInputTimeoutAndStaleTimeout(t *testing.T) {
	var decoder mouseInputDecoder
	prefix := mousePrefixKey()
	decoder.decode(prefix)
	seq := decoder.seq
	messages, _ := decoder.decode(mouseInputTimeoutMsg{seq})
	if !reflect.DeepEqual(messages, []tea.Msg{prefix}) {
		t.Fatalf("literal Alt+[ was lost: %v", messages)
	}
	decoder.decode(prefix)
	if messages, _ := decoder.decode(mouseInputTimeoutMsg{seq}); len(messages) != 0 {
		t.Fatal("old timeout flushed a newer prefix")
	}
	partial := mouseBodyKey("<64;")
	decoder.decode(partial)
	messages, _ = decoder.decode(mouseInputTimeoutMsg{decoder.seq})
	if !reflect.DeepEqual(messages, []tea.Msg{prefix, partial}) {
		t.Fatalf("unfinished input was not replayed unchanged: %v", messages)
	}
}

func TestMouseInputPreservesTrailingTyping(t *testing.T) {
	var decoder mouseInputDecoder
	decoder.decode(mousePrefixKey())
	messages, _ := decoder.decode(mouseBodyKey("<65;40;5Mhello界"))
	want := []tea.Msg{
		tea.MouseMsg{X: 39, Y: 4, Button: tea.MouseButtonWheelDown},
		mouseBodyKey("hello界"),
	}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("messages=%v, want %v", messages, want)
	}
}

func TestMouseInputDecodesButtonsAndModifiers(t *testing.T) {
	for _, tc := range []struct {
		body string
		want tea.MouseMsg
	}{
		{"<92;2;3M", tea.MouseMsg{X: 1, Y: 2, Button: tea.MouseButtonWheelUp, Ctrl: true, Alt: true, Shift: true}},
		{"<0;2;3m", tea.MouseMsg{X: 1, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}},
		{"<32;2;3M", tea.MouseMsg{X: 1, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion}},
		{"<35;2;3m", tea.MouseMsg{X: 1, Y: 2, Button: tea.MouseButtonNone, Action: tea.MouseActionMotion}},
		{"<66;2;3M", tea.MouseMsg{X: 1, Y: 2, Button: tea.MouseButtonWheelLeft}},
		{"<128;2;3M", tea.MouseMsg{X: 1, Y: 2, Button: tea.MouseButtonBackward}},
	} {
		got, consumed, incomplete := parseMouseInput(tc.body)
		if got != tc.want || consumed != len(tc.body) || incomplete {
			t.Fatalf("%q: got %+v/%d/%v, want %+v", tc.body, got, consumed, incomplete, tc.want)
		}
	}
}

func TestMouseInputNormalEventsPassThrough(t *testing.T) {
	for _, msg := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyEscape},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("text")},
		tea.MouseMsg{Button: tea.MouseButtonWheelUp},
	} {
		var decoder mouseInputDecoder
		messages, cmd := decoder.decode(msg)
		if cmd != nil || !reflect.DeepEqual(messages, []tea.Msg{msg}) {
			t.Fatalf("normal event %v changed: %v", msg, messages)
		}
	}
}
