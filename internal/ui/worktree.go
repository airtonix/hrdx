package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type worktreeInfo struct {
	path   string
	branch string
}

func gitWorktreeRoot(cwd string) string {
	if absolute, err := filepath.Abs(cwd); err == nil {
		cwd = absolute
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	gitPath := filepath.Join(cwd, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	gitDir := gitPath
	if !info.IsDir() {
		data, err := os.ReadFile(gitPath)
		if err != nil || !strings.HasPrefix(strings.TrimSpace(string(data)), "gitdir:") {
			return ""
		}
		gitDir = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(cwd, gitDir)
		}
	}
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		common := strings.TrimSpace(string(data))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		gitDir = common
	}
	if filepath.Base(gitDir) != ".git" {
		return ""
	}
	root := filepath.Dir(gitDir)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return filepath.Clean(root)
}

func runGit(cwd string, args ...string) ([]byte, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Dir = cwd
	return cmd.Output()
}

func listWorktrees(cwd string) ([]worktreeInfo, error) {
	output, err := runGit(cwd, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	var result []worktreeInfo
	var current *worktreeInfo
	for _, line := range strings.Split(string(output), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			if current != nil {
				result = append(result, *current)
			}
			current = &worktreeInfo{path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch ") && current != nil:
			current.branch = strings.TrimPrefix(line, "branch refs/heads/")
		case strings.HasPrefix(line, "HEAD ") && current != nil && current.branch == "":
			head := strings.TrimPrefix(line, "HEAD ")
			current.branch = "(detached) " + head[:min(7, len(head))]
		}
	}
	if current != nil {
		result = append(result, *current)
	}
	return result, nil
}

func (m Model) openWorktreePicker(base *space, at rect) (tea.Model, tea.Cmd) {
	if base == nil || gitWorktreeRoot(base.cwd) == "" {
		return m, m.flashStatus("workspace is not a Git worktree")
	}
	worktrees, err := listWorktrees(base.cwd)
	if err != nil {
		return m, m.flashStatus(err.Error())
	}
	var items []menuItem
	for _, worktree := range worktrees {
		alreadyOpen := false
		for _, current := range m.spaces {
			if filepath.Clean(current.cwd) == filepath.Clean(worktree.path) {
				alreadyOpen = true
				break
			}
		}
		if !alreadyOpen {
			items = append(items, menuItem{worktree.branch + "  " + worktree.path, "worktree-open:" + worktree.path})
		}
	}
	if len(items) == 0 {
		return m, m.flashInfo("no unopened worktrees")
	}
	m.mode = modeMenu
	m.menuPane, m.menuTab, m.menuSpace = nil, nil, nil
	m.pickItems, m.pickAction, m.pickSpace = items, "worktree-open", base
	m.openMenuBox(at)
	return m, nil
}

func (m Model) runWorktreePick(path string) (tea.Model, tea.Cmd) {
	base := m.pickSpace
	m.closeMenu()
	if base == nil {
		return m, nil
	}
	newSpace := m.addSpaceKind(path, m.config.DefaultAgent)
	m.selected = len(m.spaces) - 1
	m.persist()
	return m, m.startPane(newSpace, newSpace.tab().panes[0])
}
