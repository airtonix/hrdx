package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Run performs the in-place self-update:
//
//  1. Resolves the latest release tag via the GitHub API (same code
//     path the in-TUI notice uses, so we never disagree about latest).
//  2. Picks the asset matching the current GOOS/GOARCH using the name
//     template defined in .goreleaser.yaml.
//  3. Downloads checksums.txt and the asset to a temp directory.
//  4. Verifies the asset's sha256 against checksums.txt.
//  5. Extracts the hrdx binary from the archive.
//  6. Atomically replaces the running binary with the new one.
//
// Refuses to operate on dev builds (version == "0.0.0") because there
// is no meaningful "is newer" comparison and we'd happily downgrade a
// freshly-compiled local binary back to whatever's on GitHub.
func Run(version string) error {
	if version == "" || version == "dev" || version == "0.0.0" {
		return errors.New("dev build (version 0.0.0): `hrdx update` is disabled. Build a release tag or download from https://github.com/patriceckhart/hrdx/releases")
	}
	current := VersionOnly(version)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	fmt.Println("hrdx update: querying latest release...")
	tag, releaseURL, err := FetchLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("query latest release: %w", err)
	}
	latest := strings.TrimPrefix(tag, "v")

	if !VersionLess(current, latest) {
		fmt.Printf("hrdx %s is already up to date.\n", current)
		return nil
	}
	fmt.Printf("hrdx update: %s -> %s\n", current, latest)
	fmt.Printf("hrdx update: release page %s\n", releaseURL)

	assetName, err := releaseAssetName(latest)
	if err != nil {
		return err
	}
	fmt.Printf("hrdx update: target asset %s\n", assetName)

	// Standard GoReleaser layout: assets live under
	//   https://github.com/<owner>/<repo>/releases/download/<tag>/<file>
	base := strings.TrimSuffix(releaseURL, "/")
	base = strings.Replace(base, "/releases/tag/", "/releases/download/", 1)

	assetURL := base + "/" + assetName
	sumsURL := base + "/checksums.txt"

	tmp, err := os.MkdirTemp("", "hrdx-update-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	fmt.Println("hrdx update: downloading checksums.txt...")
	sumsPath := filepath.Join(tmp, "checksums.txt")
	if err := downloadFile(ctx, sumsURL, sumsPath); err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	wantSum, err := lookupChecksum(sumsPath, assetName)
	if err != nil {
		return err
	}

	fmt.Println("hrdx update: downloading archive...")
	archivePath := filepath.Join(tmp, assetName)
	if err := downloadFile(ctx, assetURL, archivePath); err != nil {
		return fmt.Errorf("download archive: %w", err)
	}

	fmt.Println("hrdx update: verifying checksum...")
	gotSum, err := sha256File(archivePath)
	if err != nil {
		return fmt.Errorf("hash archive: %w", err)
	}
	if !strings.EqualFold(gotSum, wantSum) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", assetName, gotSum, wantSum)
	}

	fmt.Println("hrdx update: extracting...")
	extractDir := filepath.Join(tmp, "extracted")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return fmt.Errorf("mkdir extract: %w", err)
	}
	if err := extractArchive(archivePath, extractDir); err != nil {
		return fmt.Errorf("extract archive: %w", err)
	}

	newBin := filepath.Join(extractDir, "hrdx")
	if st, err := os.Stat(newBin); err != nil || st.IsDir() {
		return fmt.Errorf("extracted archive does not contain an hrdx binary at %s", newBin)
	}

	curBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current binary path: %w", err)
	}
	// Resolve symlinks so 'hrdx' on $PATH that points elsewhere gets
	// actually replaced rather than us writing next to a stale link.
	if resolved, err := filepath.EvalSymlinks(curBin); err == nil {
		curBin = resolved
	}

	fmt.Printf("hrdx update: replacing %s\n", curBin)
	if err := replaceBinary(curBin, newBin); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	fmt.Printf("hrdx update: installed %s\n", latest)
	return nil
}

// RunCheck prints what would happen without doing the download.
func RunCheck(version string) error {
	if version == "" || version == "dev" || version == "0.0.0" {
		fmt.Println("hrdx: dev build (version 0.0.0), `hrdx update` is disabled")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tag, url, err := FetchLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("query latest release: %w", err)
	}
	latest := strings.TrimPrefix(tag, "v")
	current := VersionOnly(version)
	if !VersionLess(current, latest) {
		fmt.Printf("hrdx %s is up to date (latest: %s)\n", current, latest)
		return nil
	}
	fmt.Printf("hrdx %s -> %s available\n  release: %s\n  run 'hrdx update' to install\n", current, latest, url)
	return nil
}

// releaseAssetName returns the archive filename for the current
// platform. Must stay in sync with archives.name_template in
// .goreleaser.yaml:
//
//	{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
func releaseAssetName(version string) (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	switch goos {
	case "linux", "darwin":
		// supported
	default:
		return "", fmt.Errorf("unsupported OS for hrdx update: %s (download manually from the release page)", goos)
	}
	switch goarch {
	case "amd64", "arm64":
		// supported
	default:
		return "", fmt.Errorf("unsupported CPU arch for hrdx update: %s", goarch)
	}
	return fmt.Sprintf("hrdx_%s_%s_%s.tar.gz", version, goos, goarch), nil
}

// downloadFile fetches url to dst, streaming through io.Copy so big
// archives don't balloon memory.
func downloadFile(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

// lookupChecksum parses a GoReleaser checksums.txt file and returns
// the sha256 hex for the named asset.
func lookupChecksum(path, asset string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum for %s not listed in checksums.txt", asset)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractArchive shells out to the system tar rather than pulling in a
// Go archive lib; every supported platform ships one.
func extractArchive(archive, dst string) error {
	cmd := exec.Command("tar", "-xzf", archive, "-C", dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// replaceBinary writes the new binary in place of the old one,
// preserving the old binary's permissions. Renames in-place, which
// works while the binary is running because the kernel keeps the
// in-memory inode alive until the process exits.
func replaceBinary(cur, newBin string) error {
	info, err := os.Stat(cur)
	if err != nil {
		return fmt.Errorf("stat current binary: %w", err)
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o755
	}

	// Atomic rename if we're on the same filesystem.
	if err := os.Rename(newBin, cur); err == nil {
		_ = os.Chmod(cur, mode)
		return nil
	}
	// Cross-fs (temp dir on tmpfs vs binary on a different mount):
	// fall back to copy + chmod.
	if err := copyFile(newBin, cur); err != nil {
		return fmt.Errorf("copy new binary into place: %w", err)
	}
	_ = os.Chmod(cur, mode)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
