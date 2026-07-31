package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.0.1", "0.0.2", true},
		{"0.0.2", "0.0.1", false},
		{"0.0.99", "0.1.0", true},
		{"0.1.0", "0.0.99", false},
		{"0.0.5", "0.0.5", false},
		{"v0.0.1", "v0.0.2", true},
		{"0.0.5 (abc1234, 2026-01-01)", "0.0.6", true},
		{"1.0.0", "0.9.9", false},
	}
	for _, c := range cases {
		if got := VersionLess(c.a, c.b); got != c.want {
			t.Errorf("VersionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestVersionOnly(t *testing.T) {
	if got := VersionOnly("0.0.5 (43da5e5, 2026-05-12)"); got != "0.0.5" {
		t.Fatalf("VersionOnly = %q, want 0.0.5", got)
	}
	if got := VersionOnly("0.0.5"); got != "0.0.5" {
		t.Fatalf("VersionOnly = %q, want 0.0.5", got)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, checkFile)
	want := cache{
		CheckedAt: time.Now().UTC().Truncate(time.Second),
		CurrentAt: "0.0.1",
		Latest:    "v0.0.2",
		URL:       "https://example.com",
	}
	if err := writeCache(path, want); err != nil {
		t.Fatal(err)
	}
	got, ok := readCache(path)
	if !ok || got.Latest != want.Latest || got.CurrentAt != want.CurrentAt {
		t.Fatalf("readCache = %+v/%v, want %+v", got, ok, want)
	}
}

func TestReadCacheMissingOrBad(t *testing.T) {
	if _, ok := readCache(filepath.Join(t.TempDir(), "nope.json")); ok {
		t.Fatal("missing file should be a cache miss")
	}
	bad := filepath.Join(t.TempDir(), checkFile)
	if err := os.WriteFile(bad, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCache(bad); ok {
		t.Fatal("bad json should be a cache miss")
	}
}

func TestCheckSkipsDevBuild(t *testing.T) {
	// Must not hit the network; dev builds return the zero value fast.
	info := Check(t.Context(), t.TempDir(), "0.0.0")
	if info.Available {
		t.Fatal("dev build must never report an update")
	}
}

func TestLookupChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.txt")
	content := "abc123  hrdx_0.0.2_darwin_arm64.tar.gz\ndef456  hrdx_0.0.2_linux_amd64.tar.gz\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := lookupChecksum(path, "hrdx_0.0.2_linux_amd64.tar.gz")
	if err != nil || got != "def456" {
		t.Fatalf("lookupChecksum = %q/%v, want def456", got, err)
	}
	if _, err := lookupChecksum(path, "missing.tar.gz"); err == nil {
		t.Fatal("missing asset should error")
	}
}
