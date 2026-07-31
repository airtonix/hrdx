package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompleteDir(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"apple", "apricot", "banana", ".hidden"} {
		if err := os.Mkdir(filepath.Join(base, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "afile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	matches := completeDir(base + "/")
	if len(matches) != 3 {
		t.Fatalf("matches = %v, want apple/apricot/banana", matches)
	}

	matches = completeDir(base + "/ap")
	if len(matches) != 2 || filepath.Base(matches[0]) != "apple" || filepath.Base(matches[1]) != "apricot" {
		t.Fatalf("prefix matches = %v", matches)
	}

	matches = completeDir(base + "/ban")
	if len(matches) != 1 || filepath.Base(matches[0]) != "banana" {
		t.Fatalf("single match = %v", matches)
	}

	if matches := completeDir(base + "/zzz"); len(matches) != 0 {
		t.Fatalf("no-match = %v, want empty", matches)
	}
}

func TestCommonPrefix(t *testing.T) {
	if got := commonPrefix([]string{"/a/apple", "/a/apricot"}); got != "/a/ap" {
		t.Fatalf("commonPrefix = %q, want /a/ap", got)
	}
	if got := commonPrefix(nil); got != "" {
		t.Fatalf("commonPrefix(nil) = %q", got)
	}
}

func TestAdvanceCompletionCycles(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"apple", "apricot"} {
		if err := os.Mkdir(filepath.Join(base, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	model := newTestModel()
	model.input.SetValue(base + "/ap")

	// First tab: fills common prefix and records candidates.
	model.advanceCompletion(1)
	if len(model.completions) != 2 {
		t.Fatalf("completions = %v", model.completions)
	}
	// Next tabs cycle through the candidates.
	model.advanceCompletion(1)
	if model.input.Value() != base+"/apple" {
		t.Fatalf("value = %q, want apple", model.input.Value())
	}
	model.advanceCompletion(1)
	if model.input.Value() != base+"/apricot" {
		t.Fatalf("value = %q, want apricot", model.input.Value())
	}
	model.advanceCompletion(1)
	if model.input.Value() != base+"/apple" {
		t.Fatalf("value = %q, want wrap to apple", model.input.Value())
	}
}
