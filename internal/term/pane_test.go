package term

import (
	"strings"
	"testing"
	"time"
)

func startShellPane(t *testing.T, script string) *Pane {
	t.Helper()
	pane, err := Start("/bin/sh", []string{"-c", script}, t.TempDir(), 40, 6)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(pane.Close)
	return pane
}

func waitExit(t *testing.T, pane *Pane) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, open := <-pane.Updates():
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("pane did not exit")
		}
	}
}

func TestScrollbackAndSelection(t *testing.T) {
	pane := startShellPane(t, "for i in $(seq 1 20); do echo line-$i; done")
	waitExit(t, pane)

	// 20 lines + prompt rows on a 6-row screen must have filled history.
	if pane.Scroll(1000) == false {
		t.Fatal("expected scrollback to be available")
	}
	offset := pane.ScrollOffset()
	if offset == 0 {
		t.Fatal("offset = 0 after scrolling up")
	}

	top := pane.Render(false)
	if !strings.Contains(stripANSI(top), "line-1") {
		t.Fatalf("scrolled view missing early output:\n%s", stripANSI(top))
	}

	pane.ResetScroll()
	if pane.ScrollOffset() != 0 {
		t.Fatal("ResetScroll did not reset")
	}
	live := stripANSI(pane.Render(false))
	if !strings.Contains(live, "line-20") {
		t.Fatalf("live view missing latest output:\n%s", live)
	}

	// Select the first visible row in live view and check the text.
	pane.StartSelection(0, 0)
	pane.ExtendSelection(39, 1)
	pane.FinishSelection()
	text := pane.SelectionText()
	if !strings.Contains(text, "line-") {
		t.Fatalf("selection text = %q", text)
	}
	pane.ClearSelection()
	if pane.HasSelection() {
		t.Fatal("selection still active after clear")
	}
}

func TestHasScreenText(t *testing.T) {
	pane := startShellPane(t, "echo working on it")
	waitExit(t, pane)
	// The pane has exited, so HasScreenText must be false even though
	// the text is on screen.
	if pane.HasScreenText("working on it") {
		t.Fatal("exited pane must not report screen text")
	}

	live := startShellPane(t, "echo busy marker; sleep 30")
	deadline := time.Now().Add(5 * time.Second)
	for !live.HasScreenText("busy marker") {
		if time.Now().After(deadline) {
			t.Fatal("screen text not found")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if live.HasScreenText("absent needle") {
		t.Fatal("absent text must not match")
	}
	if live.HasScreenText("") {
		t.Fatal("empty needle must not match")
	}
}

func TestForegroundCommand(t *testing.T) {
	pane := startShellPane(t, "sleep 30")
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane.mu.Lock()
		pane.fgCheckedAt = time.Time{} // bypass the cache while polling
		pane.mu.Unlock()
		name := pane.ForegroundCommand()
		if name == "sleep" || name == "sh" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("foreground = %q, want sleep or sh", name)
		}
		time.Sleep(50 * time.Millisecond)
	}
	pane.Close()
	waitExit(t, pane)
	if name := pane.ForegroundCommand(); name != "" {
		t.Fatalf("foreground after exit = %q, want empty", name)
	}
}

func stripANSI(value string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range value {
		switch {
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		case r == 0x1b:
			inEscape = true
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
