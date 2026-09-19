package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func packageFixture(t testing.TB, root, folder, id string) string {
	t.Helper()
	path := filepath.Join(root, folder)
	if err := os.MkdirAll(filepath.Join(path, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := strings.ReplaceAll(validManifest, "example.git-tools", id)
	manifest = strings.ReplaceAll(manifest, "bin/plugin", "bin/plugin.exe")
	writeFixture(t, filepath.Join(path, ManifestFile), []byte(manifest), 0o600)
	writeFixture(t, filepath.Join(path, "bin", "plugin.exe"), []byte("not an executable, discovery must not probe it"), 0o700)
	return path
}

func writeFixture(t testing.TB, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverOrderingAndDuplicateRoots(t *testing.T) {
	root := t.TempDir()
	packageFixture(t, root, "z-last", "example.last")
	packageFixture(t, root, "a-first", "example.first")
	writeFixture(t, filepath.Join(root, "README"), []byte("ignored"), 0o600)
	inventory := Discover([]Root{{Path: root}, {Path: filepath.Join(root, ".")}})
	if inventory.HasProblems() || len(inventory.Packages) != 2 {
		t.Fatalf("inventory = %+v", inventory)
	}
	if inventory.Packages[0].ID != "example.first" || inventory.Packages[1].ID != "example.last" {
		t.Fatalf("packages not sorted: %+v", inventory.Packages)
	}
	if !reflect.DeepEqual(inventory, Discover([]Root{{Path: root}})) {
		t.Fatal("discovery should be deterministic and deduplicate roots")
	}
}

func TestDiscoverRootCaseSensitivity(t *testing.T) {
	base := t.TempDir()
	upper, lower := filepath.Join(base, "Root"), filepath.Join(base, "root")
	packageFixture(t, upper, "first", "example.first")
	packageFixture(t, lower, "second", "example.second")
	upperInfo, err := os.Stat(upper)
	if err != nil {
		t.Fatal(err)
	}
	lowerInfo, err := os.Stat(lower)
	if err != nil {
		t.Fatal(err)
	}
	inventory := Discover([]Root{{Path: upper}, {Path: lower}})
	if inventory.HasProblems() || len(inventory.Packages) != 2 {
		t.Fatalf("case handling (same directory=%v): %+v", os.SameFile(upperInfo, lowerInfo), inventory)
	}
}

func TestDiscoverDuplicateIDsInvalidateEveryPackage(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	packageFixture(t, first, "one", "example.same")
	packageFixture(t, second, "two", "example.same")
	inventory := Discover([]Root{{Path: first}, {Path: second}})
	if len(inventory.Packages) != 2 || !inventory.HasProblems() {
		t.Fatalf("inventory = %+v", inventory)
	}
	for _, entry := range inventory.Packages {
		if entry.Valid || len(entry.Diagnostics) != 1 || entry.Diagnostics[0].Code != "duplicate_id" {
			t.Fatalf("duplicate should not shadow or win: %+v", entry)
		}
	}
}

func TestDiscoverMissingAndInvalidRoots(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if inventory := Discover([]Root{{Path: missing, Optional: true}}); inventory.HasProblems() || len(inventory.Packages) != 0 {
		t.Fatalf("missing optional root: %+v", inventory)
	}
	for _, root := range []Root{{Path: missing}, {}} {
		inventory := Discover([]Root{root})
		if !inventory.HasProblems() || len(inventory.Diagnostics) != 1 {
			t.Fatalf("invalid root: %+v", inventory)
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("discovery must not create missing roots")
	}
	file := filepath.Join(t.TempDir(), "file")
	writeFixture(t, file, nil, 0o600)
	if inventory := Discover([]Root{{Path: file}}); len(inventory.Diagnostics) != 1 || inventory.Diagnostics[0].Code != "root_unreadable" {
		t.Fatalf("file root: %+v", inventory)
	}
}

func TestDiscoverLimits(t *testing.T) {
	inventory := Discover(make([]Root, MaxRoots+1))
	if len(inventory.Diagnostics) != 1 || inventory.Diagnostics[0].Code != "too_many_roots" {
		t.Fatalf("root limit: %+v", inventory)
	}
	root := t.TempDir()
	for index := 0; index <= MaxRootEntries; index++ {
		writeFixture(t, filepath.Join(root, fmt.Sprintf("entry-%03d", index)), nil, 0o600)
	}
	inventory = Discover([]Root{{Path: root}})
	if len(inventory.Packages) != 0 || len(inventory.Diagnostics) != 1 || inventory.Diagnostics[0].Code != "root_too_large" {
		t.Fatalf("entry limit: %+v", inventory)
	}
}

func TestDiscoverInvalidPackages(t *testing.T) {
	tests := []struct {
		name, code string
		change     func(*testing.T, string)
	}{
		{"missing manifest", "manifest_missing", func(t *testing.T, path string) {
			if err := os.Remove(filepath.Join(path, ManifestFile)); err != nil {
				t.Fatal(err)
			}
		}},
		{"malformed manifest", "invalid_json", func(t *testing.T, path string) {
			writeFixture(t, filepath.Join(path, ManifestFile), []byte(`{"args": "synthetic-private-value",`), 0o600)
		}},
		{"directory manifest", "manifest_not_regular", func(t *testing.T, path string) {
			if err := os.Remove(filepath.Join(path, ManifestFile)); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(path, ManifestFile), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing executable", "entrypoint_unavailable", func(t *testing.T, path string) {
			if err := os.Remove(filepath.Join(path, "bin", "plugin.exe")); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory executable", "entrypoint_not_regular", func(t *testing.T, path string) {
			if err := os.Remove(filepath.Join(path, "bin", "plugin.exe")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(path, "bin", "plugin.exe"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := packageFixture(t, root, "package", "example.test")
			tt.change(t, path)
			inventory := Discover([]Root{{Path: root}})
			if len(inventory.Packages) != 1 || inventory.Packages[0].Valid || len(inventory.Packages[0].Diagnostics) != 1 || inventory.Packages[0].Diagnostics[0].Code != tt.code {
				t.Fatalf("inventory = %+v", inventory)
			}
			data, err := json.Marshal(inventory)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "synthetic-private-value") {
				t.Fatal("manifest payload leaked into diagnostics")
			}
		})
	}
}

func TestDiscoverSymlinks(t *testing.T) {
	root := t.TempDir()
	path := packageFixture(t, root, "package", "example.test")
	external := filepath.Join(t.TempDir(), "external.exe")
	writeFixture(t, external, nil, 0o700)
	alias := filepath.Join(t.TempDir(), "root-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if inventory := Discover([]Root{{Path: root}, {Path: alias}}); inventory.HasProblems() || len(inventory.Packages) != 1 {
		t.Fatalf("root alias should deduplicate: %+v", inventory)
	}
	if err := os.Symlink(path, filepath.Join(root, "linked-package")); err != nil {
		t.Fatal(err)
	}
	if inventory := Discover([]Root{{Path: root}}); inventory.Packages[0].Diagnostics[0].Code != "package_symlink" {
		t.Fatalf("package alias should be rejected: %+v", inventory)
	}
	executable := filepath.Join(path, "bin", "plugin.exe")
	if err := os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, executable); err != nil {
		t.Fatal(err)
	}
	if entry := inspect(path); entry.Valid || entry.Diagnostics[0].Code != "entrypoint_unavailable" {
		t.Fatalf("external executable should be rejected: %+v", entry)
	}
	if err := os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(path, "bin", "target.exe"), nil, 0o700)
	if err := os.Symlink("target.exe", executable); err != nil {
		t.Fatal(err)
	}
	if entry := inspect(path); !entry.Valid {
		t.Fatalf("internal executable alias should be valid: %+v", entry)
	}
	if err := os.Rename(filepath.Join(path, ManifestFile), filepath.Join(path, "other.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.json", filepath.Join(path, ManifestFile)); err != nil {
		t.Fatal(err)
	}
	if entry := inspect(path); entry.Valid || entry.Diagnostics[0].Code != "manifest_not_regular" {
		t.Fatalf("manifest alias should be rejected: %+v", entry)
	}
}

func TestDiscoverPermissionErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not model Windows ACLs")
	}
	root := t.TempDir()
	path := packageFixture(t, root, "package", "example.test")
	executable := filepath.Join(path, "bin", "plugin.exe")
	if err := os.Chmod(executable, 0o600); err != nil {
		t.Fatal(err)
	}
	if entry := inspect(path); entry.Valid || entry.Diagnostics[0].Code != "entrypoint_not_executable" {
		t.Fatalf("executable permission should be required: %+v", entry)
	}
	manifestPath := filepath.Join(path, ManifestFile)
	if err := os.Chmod(manifestPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(manifestPath, 0o600) })
	if file, err := os.Open(manifestPath); err == nil {
		file.Close()
		t.Skip("current user can read files without permission bits")
	}
	if entry := inspect(path); entry.Diagnostics[0].Code != "manifest_unreadable" {
		t.Fatalf("unreadable manifest: %+v", entry)
	}
}

func TestExecutableFor(t *testing.T) {
	for _, tt := range []struct {
		goos, path string
		mode       os.FileMode
		want       bool
	}{
		{"windows", "plugin.EXE", 0o600, true},
		{"windows", "plugin.com", 0o600, true},
		{"windows", "plugin.cmd", 0o700, false},
		{"windows", "plugin.bat", 0o700, false},
		{"windows", "plugin", 0o700, false},
		{"linux", "plugin", 0o700, true},
		{"darwin", "plugin", 0o600, false},
	} {
		if got := executableFor(tt.goos, tt.path, tt.mode); got != tt.want {
			t.Errorf("executableFor(%s, %s, %v) = %v", tt.goos, tt.path, tt.mode, got)
		}
	}
}

func TestDiscoverNeverExecutes(t *testing.T) {
	root := t.TempDir()
	path := packageFixture(t, root, "package", "example.test")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(path, "bin", "plugin.exe"), data, 0o700)
	marker := filepath.Join(root, "executed")
	t.Setenv("HRDX_PLUGIN_DISCOVERY_TEST_MARKER", marker)
	manifest := strings.ReplaceAll(validManifest, "bin/plugin", "bin/plugin.exe")
	manifest = strings.Replace(manifest, `"--stdio"`, `"-test.run=^TestDiscoveryProcessHelper$"`, 1)
	writeFixture(t, filepath.Join(path, ManifestFile), []byte(manifest), 0o600)
	if inventory := Discover([]Root{{Path: root}}); inventory.HasProblems() {
		t.Fatalf("inventory = %+v", inventory)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("discovery executed the plugin")
	}
}

func TestDiscoveryProcessHelper(t *testing.T) {
	if marker := os.Getenv("HRDX_PLUGIN_DISCOVERY_TEST_MARKER"); marker != "" {
		if err := os.WriteFile(marker, []byte("executed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
