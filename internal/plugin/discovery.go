package plugin

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	MaxRoots       = 16
	MaxRootEntries = 256
)

// Root is a directory containing immediate package directories. Only the
// default user root is optional. Explicitly selected missing roots are errors.
type Root struct {
	Path     string
	Optional bool
}

type Diagnostic struct {
	Path string `json:"path"`
	Problem
}

// Package is an inventory record, not a runnable or approved registration.
// Deliberately omit argv and other manifest payloads from diagnostic output.
type Package struct {
	Path        string    `json:"path"`
	ID          string    `json:"id,omitempty"`
	Name        string    `json:"name,omitempty"`
	Version     string    `json:"version,omitempty"`
	Valid       bool      `json:"valid"`
	Diagnostics []Problem `json:"diagnostics"`
	Manifest    *Manifest `json:"-"`
}

type Inventory struct {
	Packages    []Package    `json:"packages"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func (i Inventory) HasProblems() bool {
	if len(i.Diagnostics) > 0 {
		return true
	}
	for _, entry := range i.Packages {
		if !entry.Valid {
			return true
		}
	}
	return false
}

// Discover reads each distinct canonical root in caller order and packages in
// lexical order. Duplicate IDs invalidate every claimant, with no precedence
// or shadowing. It never creates directories, loads state, or starts a process.
func Discover(roots []Root) Inventory {
	inventory := Inventory{Packages: []Package{}, Diagnostics: []Diagnostic{}}
	report := func(path, code, message string) {
		inventory.Diagnostics = append(inventory.Diagnostics, Diagnostic{Path: path, Problem: Problem{Code: code, Message: message}})
	}
	if len(roots) > MaxRoots {
		report("", "too_many_roots", "at most 16 discovery roots may be selected")
		return inventory
	}
	var seen []os.FileInfo
	for _, candidate := range roots {
		if candidate.Path == "" {
			report("", "invalid_root", "discovery root must not be empty")
			continue
		}
		absolute, err := filepath.Abs(candidate.Path)
		if err != nil {
			report(candidate.Path, "root_unreadable", "cannot resolve discovery root")
			continue
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			if candidate.Optional && errors.Is(err, os.ErrNotExist) {
				continue
			}
			report(absolute, "root_unreadable", "cannot resolve discovery root")
			continue
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.IsDir() {
			report(canonical, "root_unreadable", "cannot inspect discovery root directory")
			continue
		}
		duplicate := false
		for _, previous := range seen {
			if os.SameFile(info, previous) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		seen = append(seen, info)
		entries, err := rootEntries(canonical)
		if err != nil {
			var failure *Problem
			if errors.As(err, &failure) {
				report(canonical, failure.Code, failure.Message)
			} else {
				report(canonical, "root_unreadable", "cannot read discovery root directory")
			}
			continue
		}
		for _, entry := range entries {
			path := filepath.Join(canonical, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				inventory.Packages = append(inventory.Packages, Package{
					Path: path, Diagnostics: []Problem{*problem("package_symlink", "package directories must not be symlinks")},
				})
				continue
			}
			if entry.IsDir() {
				inventory.Packages = append(inventory.Packages, inspect(path))
			}
		}
	}
	ids := make(map[string][]int)
	for index, entry := range inventory.Packages {
		if entry.ID != "" {
			ids[entry.ID] = append(ids[entry.ID], index)
		}
	}
	for _, indexes := range ids {
		if len(indexes) < 2 {
			continue
		}
		for _, index := range indexes {
			entry := &inventory.Packages[index]
			entry.Valid = false
			entry.Diagnostics = append(entry.Diagnostics, *problem("duplicate_id", "plugin ID is declared by multiple packages, none takes precedence"))
		}
	}
	return inventory
}

func rootEntries(path string) ([]os.DirEntry, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(MaxRootEntries + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > MaxRootEntries {
		return nil, problem("root_too_large", "discovery root exceeds 256 entries, no packages in this root were inspected")
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
	return entries, nil
}

func inspect(path string) Package {
	entry := Package{Path: path, Diagnostics: []Problem{}}
	fail := func(code, message string) Package {
		entry.Diagnostics = append(entry.Diagnostics, *problem(code, message))
		return entry
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return fail("package_unreadable", "cannot open package directory")
	}
	defer root.Close()
	info, err := root.Lstat(ManifestFile)
	if errors.Is(err, os.ErrNotExist) {
		return fail("manifest_missing", "package requires plugin.json")
	}
	if err != nil {
		return fail("manifest_unreadable", "cannot inspect plugin.json")
	}
	if !info.Mode().IsRegular() {
		return fail("manifest_not_regular", "plugin.json must be a regular file, not a symlink or device")
	}
	file, err := root.Open(ManifestFile)
	if err != nil {
		return fail("manifest_unreadable", "cannot read plugin.json")
	}
	manifest, parseErr := ParseManifest(file)
	file.Close()
	if parseErr != nil {
		var failure *Problem
		if errors.As(parseErr, &failure) {
			return fail(failure.Code, failure.Message)
		}
		return fail("invalid_manifest", "cannot validate plugin.json")
	}
	entry.Manifest = &manifest
	entry.ID, entry.Name, entry.Version = manifest.ID, manifest.Name, manifest.Version
	// Root.Stat prevents symlink traversal outside the package. This is an
	// inventory check, not an execution approval or protection against later
	// edits. Any future launcher must revalidate provenance before execution.
	info, err = root.Stat(filepath.FromSlash(manifest.Entrypoint))
	if err != nil {
		return fail("entrypoint_unavailable", "entrypoint is missing, inaccessible, or resolves outside the package")
	}
	if !info.Mode().IsRegular() {
		return fail("entrypoint_not_regular", "entrypoint must resolve to a regular file inside the package")
	}
	if !executableFor(runtime.GOOS, manifest.Entrypoint, info.Mode()) {
		return fail("entrypoint_not_executable", "entrypoint requires executable permission on Unix or an .exe/.com extension on Windows")
	}
	entry.Valid = true
	return entry
}

// Inventory checks metadata only. It cannot certify executable format, runtime
// dependencies, or Windows ACLs without actually launching untrusted code.
func executableFor(goos, path string, mode os.FileMode) bool {
	if goos == "windows" {
		ext := strings.ToLower(filepath.Ext(path))
		return ext == ".exe" || ext == ".com"
	}
	return mode.Perm()&0o111 != 0
}
