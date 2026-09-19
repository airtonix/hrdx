package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/patriceckhart/hrdx/internal/plugin"
)

func TestPluginsUsage(t *testing.T) {
	for _, tt := range []struct {
		args []string
		code int
	}{
		{nil, 2},
		{[]string{"start"}, 2},
		{[]string{"--help"}, 0},
		{[]string{"list", "--help"}, 0},
		{[]string{"list", "--unknown"}, 2},
		{[]string{"list", "unexpected"}, 2},
	} {
		var stdout, stderr bytes.Buffer
		if code := runPlugins(tt.args, &stdout, &stderr); code != tt.code {
			t.Fatalf("runPlugins(%v) = %d, stderr=%s", tt.args, code, &stderr)
		}
		if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("usage should go to stderr: stdout=%s stderr=%s", &stdout, &stderr)
		}
	}
}

func TestPluginsEmptyInventory(t *testing.T) {
	for _, command := range []string{"list", "doctor"} {
		var stdout, stderr bytes.Buffer
		code := runPlugins([]string{command, "--state=", "--json"}, &stdout, &stderr)
		if code != 0 || stderr.Len() != 0 || stdout.String() != "{\n  \"packages\": [],\n  \"diagnostics\": []\n}\n" {
			t.Fatalf("empty %s: code=%d stdout=%s stderr=%s", command, code, &stdout, &stderr)
		}
	}
}

func TestPluginsDefaultRootDoesNotLoadState(t *testing.T) {
	base := t.TempDir()
	statePath := filepath.Join(base, "state.json")
	if err := os.WriteFile(statePath, []byte("not valid state"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "plugins")
	cliPluginFixture(t, root, "example.test")
	var stdout, stderr bytes.Buffer
	code := runPlugins([]string{"doctor", "--state", statePath, "--json"}, &stdout, &stderr)
	var inventory plugin.Inventory
	if err := json.Unmarshal(stdout.Bytes(), &inventory); err != nil {
		t.Fatal(err)
	}
	if code != 0 || stderr.Len() != 0 || len(inventory.Packages) != 1 || !inventory.Packages[0].Valid {
		t.Fatalf("inventory: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	if strings.Contains(stdout.String(), "synthetic-private-argument") || strings.Contains(stdout.String(), `"args"`) {
		t.Fatal("inventory should not serialize manifest arguments")
	}
	data, err := os.ReadFile(statePath)
	if err != nil || string(data) != "not valid state" {
		t.Fatal("inventory modified state")
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 2 {
		t.Fatalf("inventory created files: %v, %v", entries, err)
	}
}

func TestPluginsListAndDoctorExitCodes(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for _, tt := range []struct {
		command string
		want    int
	}{{"list", 0}, {"doctor", 1}} {
		var stdout, stderr bytes.Buffer
		code := runPlugins([]string{tt.command, "--state=", "--dir", missing, "--json"}, &stdout, &stderr)
		if code != tt.want || !strings.Contains(stdout.String(), "root_unreadable") {
			t.Fatalf("%s code=%d stdout=%s stderr=%s", tt.command, code, &stdout, &stderr)
		}
	}
}

func TestPluginsNoPrecedenceAcrossRoots(t *testing.T) {
	base, extra := t.TempDir(), t.TempDir()
	cliPluginFixture(t, filepath.Join(base, "plugins"), "example.same")
	cliPluginFixture(t, extra, "example.same")
	var stdout, stderr bytes.Buffer
	code := runPlugins([]string{"doctor", "--state", filepath.Join(base, "state.json"), "--dir", extra}, &stdout, &stderr)
	if code != 1 || strings.Count(stdout.String(), "duplicate_id") != 2 || strings.Contains(stdout.String(), "valid (inventory only)") {
		t.Fatalf("duplicate approval risk: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
}

func TestPluginsHumanOutputEscapesPaths(t *testing.T) {
	// The path need not exist and uses no actual control-containing file names,
	// so this test also runs on Windows.
	path := filepath.Join(t.TempDir(), "missing\x1b[31m\npath")
	var stdout, stderr bytes.Buffer
	code := runPlugins([]string{"doctor", "--state=", "--dir", path}, &stdout, &stderr)
	if code != 1 || strings.ContainsRune(stdout.String(), '\x1b') || strings.Contains(stdout.String(), "\npath") {
		t.Fatalf("unsafe output: code=%d output=%q", code, stdout.String())
	}
}

func TestPluginsOutputError(t *testing.T) {
	for _, args := range [][]string{{"list", "--state="}, {"list", "--state=", "--json"}} {
		var stderr bytes.Buffer
		if code := runPlugins(args, brokenPluginWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "cannot write inventory") {
			t.Fatalf("output failure: code=%d stderr=%s", code, &stderr)
		}
	}
}

type brokenPluginWriter struct{}

func (brokenPluginWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func cliPluginFixture(t *testing.T, root, id string) {
	t.Helper()
	path := filepath.Join(root, "package")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := plugin.Manifest{
		Schema: 1, ID: id, Version: "1.0.0", Entrypoint: "plugin.exe",
		Protocol: plugin.ProtocolRange{Min: 1, Max: 1}, Args: []string{"synthetic-private-argument"},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, plugin.ManifestFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "plugin.exe"), []byte("never execute"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestPluginsMainRouting(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, self, "-test.run=^TestPluginsMainHelper$")
	command.Env = append(os.Environ(), "HRDX_PLUGINS_MAIN_HELPER=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("main routing: %v, output=%s", err, output)
	}
	var inventory plugin.Inventory
	if err := json.Unmarshal(output, &inventory); err != nil || len(inventory.Packages) != 0 {
		t.Fatalf("main should emit only inventory JSON: %s, %v", output, err)
	}
}

func TestPluginsMainHelper(t *testing.T) {
	if os.Getenv("HRDX_PLUGINS_MAIN_HELPER") != "1" {
		return
	}
	os.Args = []string{"hrdx", "plugins", "list", "--state=", "--json"}
	main()
}
