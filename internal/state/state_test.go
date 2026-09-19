package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	zero := 0
	one := 1
	original := State{
		Selected:        1,
		DisableAutoCopy: true,
		Workspaces: []Workspace{
			{
				Name:     "api",
				CWD:      "/tmp/api",
				Panes:    []Pane{{Kind: "zot", Name: "zot 1"}, {Kind: "shell", Name: "shell 1"}},
				Selected: 1,
				Layout: &Node{
					Vertical: true,
					Ratio:    0.3,
					A:        &Node{Pane: &zero},
					B:        &Node{Pane: &one},
				},
			},
		},
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Workspaces) != 1 || loaded.Selected != 1 || !loaded.DisableAutoCopy {
		t.Fatalf("loaded = %+v", loaded)
	}
	ws := loaded.Workspaces[0]
	if ws.Name != "api" || ws.CWD != "/tmp/api" || len(ws.Panes) != 2 || ws.Selected != 1 {
		t.Fatalf("workspace = %+v", ws)
	}
	if ws.Layout == nil || !ws.Layout.Vertical || ws.Layout.Ratio != 0.3 {
		t.Fatalf("layout = %+v", ws.Layout)
	}
	if ws.Layout.A == nil || ws.Layout.A.Pane == nil || *ws.Layout.A.Pane != 0 {
		t.Fatalf("layout.A = %+v", ws.Layout.A)
	}
}

func TestLoadMissingFile(t *testing.T) {
	loaded, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(loaded.Workspaces) != 0 {
		t.Fatalf("Load missing = %+v, %v", loaded, err)
	}
}

func TestConfigBasePrefersXDGButKeepsExistingLegacyData(t *testing.T) {
	xdg, legacy := t.TempDir(), t.TempDir()
	// Fresh setup: XDG wins on Unix-like systems including macOS.
	for _, goos := range []string{"darwin", "linux"} {
		if got := configBase(goos, xdg, legacy); got != xdg {
			t.Fatalf("%s fresh: configBase = %q, want %q", goos, got, xdg)
		}
	}
	// Existing legacy data and nothing under XDG yet: stay on legacy.
	if err := os.Mkdir(filepath.Join(legacy, "hrdx"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := configBase("darwin", xdg, legacy); got != legacy {
		t.Fatalf("legacy data ignored: configBase = %q, want %q", got, legacy)
	}
	// Once the user moved their directory, XDG wins again.
	if err := os.Mkdir(filepath.Join(xdg, "hrdx"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := configBase("darwin", xdg, legacy); got != xdg {
		t.Fatalf("migrated data ignored: configBase = %q, want %q", got, xdg)
	}
	// Windows, relative, and empty XDG values keep the platform directory.
	if got := configBase("windows", xdg, legacy); got != legacy {
		t.Fatal("windows must keep its native config directory")
	}
	if got := configBase("darwin", "relative/dir", legacy); got != legacy {
		t.Fatal("relative XDG_CONFIG_HOME must be ignored")
	}
	if got := configBase("darwin", "", legacy); got != legacy {
		t.Fatal("empty XDG_CONFIG_HOME must fall back to the platform default")
	}
	// A platform lookup failure still yields a usable XDG path.
	if got := configBase("darwin", xdg, ""); got != xdg {
		t.Fatalf("configBase without platform dir = %q", got)
	}
}

func TestDefaultPathHonorsXDGConfigHome(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", custom)
	t.Setenv("HOME", t.TempDir()) // no legacy hrdx directory under this home
	want := filepath.Join(custom, "hrdx", "state.json")
	got := DefaultPath()
	if runtime.GOOS == "windows" {
		if got == want {
			t.Fatal("DefaultPath honored XDG_CONFIG_HOME on Windows")
		}
		return
	}
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
}
