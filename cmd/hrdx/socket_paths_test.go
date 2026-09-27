package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSocketPathsPreferLegacyWhenAvailable(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	runtimeDir := t.TempDir()
	paths := selectSocketPaths(statePath, "linux", runtimeDir, func(string) error { return nil })
	if paths.api != filepath.Join(filepath.Dir(statePath), "hrdx.sock") || paths.holder != filepath.Join(filepath.Dir(statePath), "holder.sock") {
		t.Fatalf("socket paths = %+v, want legacy paths", paths)
	}
}

func TestSocketPathsFallBackTogether(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix runtime sockets are not used on Windows")
	}
	stateDir := t.TempDir()
	runtimeDir, err := os.MkdirTemp(os.TempDir(), "hr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtimeDir) })
	statePath := filepath.Join(stateDir, "state.json")
	paths := selectSocketPaths(statePath, "linux", runtimeDir, func(string) error { return os.ErrPermission })
	if filepath.Dir(paths.api) != filepath.Join(runtimeDir, "hrdx") || filepath.Dir(paths.holder) != filepath.Join(runtimeDir, "hrdx") {
		t.Fatalf("socket paths = %+v, want runtime paths", paths)
	}
	if paths.api == paths.holder {
		t.Fatal("API and holder share a socket path")
	}
	other := selectSocketPaths(filepath.Join(stateDir, "other.json"), "linux", runtimeDir, func(string) error { return os.ErrPermission })
	if paths.api == other.api || paths.holder == other.holder {
		t.Fatal("separate state files share runtime sockets")
	}
	restarted := selectSocketPaths(statePath, "linux", runtimeDir, func(string) error { return os.ErrPermission })
	if restarted != paths {
		t.Fatalf("restart paths = %+v, want %+v", restarted, paths)
	}
	info, err := os.Stat(filepath.Join(runtimeDir, "hrdx"))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("runtime socket directory: %v, %v", info, err)
	}
	listener, err := net.Listen("unix", paths.api)
	if err != nil {
		t.Fatalf("listen on runtime socket: %v", err)
	}
	listener.Close()
}

func TestSocketPathsKeepLiveLegacyHolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix runtime sockets are not used on Windows")
	}
	stateDir, err := os.MkdirTemp(os.TempDir(), "hs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(stateDir) })
	runtimeDir, err := os.MkdirTemp(os.TempDir(), "hr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtimeDir) })
	statePath := filepath.Join(stateDir, "state.json")
	legacy := legacySocketPaths(statePath)
	listener, err := net.Listen("unix", legacy.holder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	paths := selectSocketPaths(statePath, "darwin", runtimeDir, func(string) error { return os.ErrPermission })
	if paths.holder != legacy.holder || paths.api == legacy.api {
		t.Fatalf("paths = %+v, want existing holder and runtime API", paths)
	}
	apiListener, err := net.Listen("unix", legacy.api)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { apiListener.Close() })
	paths = selectSocketPaths(statePath, "darwin", runtimeDir, func(string) error { return os.ErrPermission })
	if paths != legacy {
		t.Fatalf("paths = %+v, want both live legacy sockets", paths)
	}
}

func TestSocketPathsInvalidRuntimePreservesLegacy(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	legacy := legacySocketPaths(statePath)
	for _, tc := range []struct{ goos, runtime string }{
		{"linux", ""},
		{"linux", "relative"},
		{"linux", filepath.Join(t.TempDir(), "missing")},
		{"windows", t.TempDir()},
	} {
		got := selectSocketPaths(statePath, tc.goos, tc.runtime, func(string) error { return os.ErrPermission })
		if got != legacy {
			t.Fatalf("%+v: paths = %+v, want %+v", tc, got, legacy)
		}
	}
}

func TestRuntimeSocketsRejectUnsafeDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix runtime sockets are not used on Windows")
	}
	runtimeDir := t.TempDir()
	dir := filepath.Join(runtimeDir, "hrdx")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	if _, err := runtimeSocketPaths(statePath, "linux", runtimeDir); err == nil {
		t.Fatal("accepted a publicly accessible runtime directory")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), dir); err == nil {
		if _, err := runtimeSocketPaths(statePath, "linux", runtimeDir); err == nil {
			t.Fatal("accepted a symlinked runtime directory")
		}
	}
}

func TestSocketPathLength(t *testing.T) {
	if !socketPathFits("/"+strings.Repeat("a", 102), "darwin") {
		t.Fatal("valid Darwin path rejected")
	}
	if socketPathFits("/"+strings.Repeat("a", 103), "darwin") {
		t.Fatal("overlong Darwin path accepted")
	}
	if !socketPathFits("/"+strings.Repeat("a", 106), "linux") {
		t.Fatal("valid Linux path rejected")
	}
}

func TestUnixSocketProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix runtime sockets are not used on Windows")
	}
	if err := probeUnixSocket(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := probeUnixSocket(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}
}
