package state

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// PluginApproval is host-owned authorization, separate from workspace snapshots
// so a running TUI cannot overwrite approvals changed by the management CLI.
// Missing or disabled records never authorize execution.
type PluginApproval struct {
	Schema     int      `json:"schema"`
	ID         string   `json:"id"`
	Path       string   `json:"path"`
	Digest     string   `json:"digest"`
	Enabled    bool     `json:"enabled"`
	Grants     []string `json:"grants,omitempty"`
	Workspaces []string `json:"workspaces,omitempty"`
	Instance   bool     `json:"instance,omitempty"`
	// Config holds host-approved values for manifest-declared options only.
	// Never store secrets here: the file is ordinary user-readable state.
	Config map[string]any `json:"config,omitempty"`
}

func LoadPluginApproval(path string) (PluginApproval, error) {
	var approval PluginApproval
	info, err := os.Lstat(path)
	if err != nil {
		return approval, err
	}
	if !info.Mode().IsRegular() {
		return approval, fmt.Errorf("plugin approval must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return approval, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return approval, fmt.Errorf("cannot read plugin approval")
	}
	if err := json.Unmarshal(data, &approval); err != nil || approval.Schema != 1 {
		return PluginApproval{}, fmt.Errorf("invalid plugin approval")
	}
	return approval, nil
}

func SavePluginApproval(path string, approval PluginApproval) error {
	data, err := json.MarshalIndent(approval, "", "  ")
	if err != nil || len(data) > 64*1024 {
		return fmt.Errorf("invalid plugin approval")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cannot create approval directory")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".approval-*")
	if err != nil {
		return fmt.Errorf("cannot create approval file")
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("cannot write plugin approval")
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("cannot replace plugin approval")
	}
	return nil
}
