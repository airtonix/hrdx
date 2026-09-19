package state

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestPluginApprovalRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approvals", "example.test.json")
	approval := PluginApproval{Schema: 1, ID: "example.test", Path: filepath.Join(t.TempDir(), "package"), Digest: strings.Repeat("a", 64), Enabled: true, Grants: []string{"ui.notification"}, Workspaces: []string{t.TempDir()}}
	if err := SavePluginApproval(path, approval); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPluginApproval(path)
	if err != nil || !reflect.DeepEqual(loaded, approval) {
		t.Fatalf("round-trip: %+v %v", loaded, err)
	}
	approval.Enabled = false
	if err := SavePluginApproval(path, approval); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadPluginApproval(path)
	if err != nil || loaded.Enabled {
		t.Fatal("disable did not persist")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("approval file is not private")
		}
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatal("temporary approval files leaked")
	}
}

func TestPluginApprovalMissingMalformedAndLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	if _, err := LoadPluginApproval(path); !os.IsNotExist(err) {
		t.Fatal("missing approval must not authorize anything")
	}
	for _, data := range []string{`null`, `{}`, `{"schema":2}`, `invalid`, strings.Repeat("x", 65537)} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPluginApproval(path); err == nil {
			t.Fatalf("accepted invalid approval %q", data[:min(20, len(data))])
		}
	}
}
