package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/patriceckhart/hrdx/internal/plugin"
	"github.com/patriceckhart/hrdx/internal/state"
)

func TestPluginManagementApprovalAndUpgrade(t *testing.T) {
	root := t.TempDir()
	cliPluginFixture(t, root, "example.test")
	path := filepath.Join(root, "package")
	manifestPath := filepath.Join(path, "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest plugin.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Requests = []string{"ui.notification"}
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	statePath := filepath.Join(base, "state.json")
	run := func(want int, args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args = append(args, "--state", statePath)
		if code := runPlugins(args, &stdout, &stderr); code != want {
			t.Fatalf("%v: code %d stdout=%s stderr=%s", args, code, &stdout, &stderr)
		}
	}
	run(2, "approve", "--package", path)
	run(1, "approve", "--package", path, "--trust", "--grant", "pane.send_input")
	run(0, "approve", "--package", path, "--trust", "--grant", "ui.notification")
	approval, err := state.LoadPluginApproval(plugin.ApprovalPath(base, "example.test"))
	if err != nil || !approval.Enabled || len(approval.Grants) != 1 {
		t.Fatalf("approval: %+v %v", approval, err)
	}
	run(0, "inspect", "--id", "example.test")
	run(0, "disable", "--id", "example.test")
	run(0, "enable", "--id", "example.test")
	if err := os.WriteFile(filepath.Join(path, "changed"), []byte("upgrade"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(1, "enable", "--id", "example.test")
	run(0, "approve", "--package", path, "--trust", "--grant", "ui.notification")
	run(0, "revoke", "--id", "example.test")
	if _, err := os.Stat(plugin.ApprovalPath(base, "example.test")); !os.IsNotExist(err) {
		t.Fatalf("revoked approval still exists: %v", err)
	}
	run(1, "revoke", "--id", "example.test")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("approval CLI touched workspace state")
	}
}
