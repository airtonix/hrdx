package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

func TestApprovalBindsPackageAndScopes(t *testing.T) {
	root := t.TempDir()
	path := packageFixture(t, root, "package", "example.test")
	entry := InspectPackage(path)
	workspace := t.TempDir()
	approval, err := Approve(entry, []string{"ui.notification"}, []string{workspace}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyApproval(entry, approval); err != nil {
		t.Fatal(err)
	}
	if !HasGrant(approval, "ui.notification") || HasGrant(approval, "pane.send_input") {
		t.Fatal("incorrect grants")
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !AllowsWorkspace(approval, absolute) || AllowsWorkspace(approval, filepath.Join(absolute, "child")) {
		t.Fatal("workspace scope is not exact")
	}
	for _, grant := range []string{"pane.send_input", "unknown.capability"} {
		if _, err := Approve(entry, []string{grant}, nil, false); err == nil {
			t.Fatal("unrequested grant accepted")
		}
	}
	store := t.TempDir()
	if err := state.SavePluginApproval(ApprovalPath(store, entry.ID), approval); err != nil {
		t.Fatal(err)
	}
	registrations, diagnostics := LoadRegistrations(store)
	if len(registrations) != 1 || len(diagnostics) != 0 {
		t.Fatalf("load: %+v %+v", registrations, diagnostics)
	}
	writeFixture(t, filepath.Join(path, "asset"), []byte("new package content"), 0o600)
	if err := VerifyApproval(InspectPackage(path), approval); err == nil {
		t.Fatal("changed package inherited approval")
	}
	registrations, diagnostics = LoadRegistrations(store)
	if len(registrations) != 0 || len(diagnostics) != 1 {
		t.Fatal("changed package loaded")
	}
	shadow := packageFixture(t, t.TempDir(), "shadow", "example.test")
	if err := VerifyApproval(InspectPackage(shadow), approval); err == nil {
		t.Fatal("shadowed ID inherited approval")
	}
	approval.Enabled = false
	if err := state.SavePluginApproval(ApprovalPath(store, entry.ID), approval); err != nil {
		t.Fatal(err)
	}
	registrations, diagnostics = LoadRegistrations(store)
	if len(registrations) != 0 || len(diagnostics) != 0 {
		t.Fatal("disabled package should not activate")
	}
}

func TestWorkspaceScopePreservesPublicSymlinkPath(t *testing.T) {
	entry := InspectPackage(packageFixture(t, t.TempDir(), "package", "example.test"))
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	approval, err := Approve(entry, nil, []string{alias}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !AllowsWorkspace(approval, alias) || AllowsWorkspace(approval, target) {
		t.Fatal("workspace scope must match the public CWD spelling, not an implicit filesystem alias")
	}
}

func TestUnknownCapabilityCannotBeApproved(t *testing.T) {
	entry := InspectPackage(packageFixture(t, t.TempDir(), "package", "example.test"))
	entry.Manifest.Requests = append(entry.Manifest.Requests, "process.spawn")
	if _, err := Approve(entry, []string{"process.spawn"}, nil, true); err == nil {
		t.Fatal("unsupported capability became approvable")
	}
}

func TestStorageIsolationQuotaAndCancellation(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	call := func(id, method, key string, value any) Reply {
		t.Helper()
		params, err := json.Marshal(map[string]any{"key": key, "value": value})
		if err != nil {
			t.Fatal(err)
		}
		return store.Call(t.Context(), id, method, params)
	}
	if result := call("example.one", "storage.set", "value", 42); result.Error != nil {
		t.Fatal(result.Error)
	}
	result := call("example.two", "storage.get", "value", nil)
	data, _ := json.Marshal(result.Result)
	if string(data) != `{"value":null}` {
		t.Fatalf("private storage leaked: %s", data)
	}
	for index := 1; index < 64; index++ {
		if result := call("example.one", "storage.set", fmt.Sprint(index), index); result.Error != nil {
			t.Fatal(result.Error)
		}
	}
	if result := call("example.one", "storage.set", "overflow", 0); result.Error == nil || result.Error.Code != "quota" {
		t.Fatal("storage quota not enforced")
	}
	if result := call("example.one", "storage.delete", "value", nil); result.Error != nil {
		t.Fatal(result.Error)
	}
	if result := call("example.one", "storage.set", "overflow", 0); result.Error != nil {
		t.Fatal(result.Error)
	}
	if result := call("example.one", "storage.get", "../escape", nil); result.Error == nil {
		t.Fatal("storage traversal accepted")
	}
	if result := call("../outside", "storage.get", "value", nil); result.Error == nil {
		t.Fatal("invalid plugin ID accepted")
	}
	if result := call("example.one", "storage.set", "large", strings.Repeat("x", 33000)); result.Error == nil {
		t.Fatal("oversized value accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result := store.Call(ctx, "example.one", "storage.delete", json.RawMessage(`{"key":"overflow"}`)); result.Error == nil {
		t.Fatal("canceled operation succeeded")
	}
}

func TestStorageSerializesIndependentStoreInstances(t *testing.T) {
	directory := t.TempDir()
	var workers sync.WaitGroup
	for index := 0; index < 16; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			store := Store{Directory: directory}
			result := store.Call(t.Context(), "example.test", "storage.set", json.RawMessage(fmt.Sprintf(`{"key":"key%d","value":%d}`, index, index)))
			if result.Error != nil {
				t.Error(result.Error)
			}
		}(index)
	}
	workers.Wait()
	data, err := os.ReadFile(filepath.Join(directory, "example.test", "values.json"))
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]int
	if json.Unmarshal(data, &values) != nil || len(values) != 16 {
		t.Fatalf("lost concurrent writes: %s", data)
	}
}
