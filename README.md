# hrdx

A Bubble Tea multiplexer for coding agents: workspaces in a sidebar (with their git branch and ahead/behind counts), tabs per workspace, real terminal panes on the right. Supported agents: [zot](https://www.zot.sh), pi, Claude Code (`claude`), and Codex CLI (`codex`).

Every pane is a real PTY session. Agent panes run the full interactive agent TUI (streaming, slash commands, sessions), and shell panes run your login shell. Pane output is parsed by a vt10x terminal emulator, so what you see is the real program, not a wrapped interpretation.

hrdx enables the kitty keyboard protocol in the host terminal, so modified chords without a legacy encoding (for example zot's ctrl+1 model switcher) pass through to panes in terminals that support it (Ghostty, kitty, WezTerm, iTerm2, foot).

## Requirements

- Go 1.25 or newer, macOS or Linux
- at least one agent CLI installed and authenticated: `zot`, `pi`, `claude`, or `codex`

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/patriceckhart/hrdx/main/install.sh | bash
```

The installer detects OS and architecture, downloads the matching release archive, verifies its sha256 against the release checksums, and installs the binary into the first writable directory of `/usr/local/bin`, `~/.local/bin`, `~/bin`. Pin a version or prefix with `bash -s -- v0.0.1 ~/bin`.

Update later with:

```sh
hrdx update           # download and install the newest release
hrdx update --check   # show what update is available, install nothing
```

The TUI also checks for a newer release at startup (cached for 12 hours) and shows an `update x.y.z` badge in the header plus a hint in the footer when one is available.

## Run

```sh
go run ./cmd/hrdx
```

Open several projects as workspaces:

```sh
go run ./cmd/hrdx \
  --cwd ~/Developer/api \
  --cwd ~/Developer/web
```

Use Claude Code as the default agent:

```sh
go run ./cmd/hrdx --agent claude
```

### Flags

| Flag | Purpose |
|---|---|
| `--cwd PATH` | Open a project as a workspace, repeatable |
| `--agent ID` | Default agent for new panes: `zot`, `pi`, `claude`, `codex` (default `zot`) |
| `--provider ID` | Pass a provider to every zot pane (zot only) |
| `--model ID` | Pass a model to every zot pane (zot only) |
| `--reasoning LEVEL` | Set the reasoning level (zot only) |
| `--continue` | Resume each project's latest session |
| `--zot-bin PATH` | Use a specific zot binary |
| `--pi-bin PATH` | Use a specific pi binary |
| `--claude-bin PATH` | Use a specific claude binary |
| `--codex-bin PATH` | Use a specific codex binary |
| `--shell PATH` | Shell for shell panes (default `$SHELL`) |
| `--state PATH` | State file for workspace persistence (empty disables) |
| `--fresh` | Ignore saved workspaces and start clean |

### Keys

All keys go to the focused terminal, except the `ctrl+b` prefix (tmux style):

| After `ctrl+b` | Action |
|---|---|
| `c` / `C` | Split right / below (opens a picker: installed agents or shell) |
| `a` | Cycle the default agent through the installed ones |
| `s` / `S` | Split with a new shell pane directly (right / below), also `%`/`\"` and `\|`/`-` |
| `w` | New workspace (directory prompt with tab completion, then agent/shell picker) |
| `t` | New tab in the current workspace (opens the agent/shell picker) |
| `n` / `p` | Next / previous tab |
| `]` / `[` | Next / previous workspace |
| `r` | Rename the focused pane |
| `m` | Open the pane context menu |
| `=` | Equalize all splits |
| `u` / `d` (or `pgup`/`pgdown`) | Scroll the focused pane's history |
| `esc` / `G` | Back to live output, clear selection |
| `x` | Close pane (sibling takes its room) |
| `X` | Close workspace |
| `ctrl+b` | Send a literal ctrl+b to the pane |
| `q` | Quit |
| `left` / `right` | Scroll the hint row in the footer (narrow terminals) |

### Mouse

- Click a tab in the tab bar to switch, or `+` to open a new tab; right-click a tab for new/rename/close
- Click a workspace or pane in the `workspaces` section to select it; right-click a workspace for rename/close/new tab
- Wheel over the sidebar scrolls it when the list is longer than the window
- Click an agent in the `agents` section to jump straight to that pane
- Click `+ new workspace` to add a project
- Click a pane to focus it; clicks and wheel events are forwarded to the child when it enabled mouse reporting (agent TUIs do)
- Right-click a pane for the context menu: rename, split left/right/up/down (with agent/shell picker), close
- Drag a shared pane border to resize the panes on either side

### Scrollback and selection

Each pane keeps up to 5000 lines of scrollback (primary screen only, like a normal terminal). The wheel scrolls the pane under the cursor: children that capture the mouse (agent TUIs) get the wheel events forwarded and scroll themselves; plain shells scroll the local history, with a `SCROLL` indicator in the footer showing how far back you are. Full-screen apps without mouse support receive arrow keys instead. `shift+pgup` / `shift+pgdn` scroll the focused pane from the keyboard; typing or `ctrl+b esc` snaps back to live.

Drag with the left mouse button to select text (hold `shift` to force local selection over mouse-capturing apps). On release the selection is copied to the system clipboard via OSC 52 plus `pbcopy`/`wl-copy`/`xclip` when available.

## Persistence

Workspaces, panes, split layout, ratios, and selection are saved to `--state` (default: `~/Library/Application Support/hrdx/state.json` on macOS, `$XDG_CONFIG_HOME/hrdx/state.json` on Linux) after every structural change, atomically. On the next launch everything is restored: shell panes get a fresh shell, and agent panes relaunch resuming their latest session for that directory (`--continue` for zot, pi, and claude; `resume --last` for codex). `--fresh` skips restoring; `--state ""` disables persistence entirely.

## Architecture

```text
cmd/hrdx         CLI entry point
internal/ui      Bubble Tea state, sidebar, split layout, input routing, agents
internal/term    PTY panes: screen, scrollback, selection, renderer, key encoder
internal/vt      vendored vt10x terminal emulator (MIT) + scrollback history
internal/state   JSON workspace persistence (atomic save, tolerant restore)
```

Each pane owns one subprocess on a PTY. A reader goroutine feeds output into a vt10x virtual screen and signals the UI through an update channel; the renderer converts the screen (colors, bold, reverse, cursor) back into ANSI for Bubble Tea. Key presses are encoded into the byte sequences a real terminal would send, honoring application cursor mode.

## Current scope

Panes are children of the TUI and stop when it exits; workspaces and agent sessions come back on relaunch via the state file and each agent's own session store. Detach and reattach with live processes and a server-owned runtime with a socket API are the natural next milestones.

## Releases

Every push to `main` that passes CI cuts a release automatically: the release workflow bumps the patch version (`v0.0.1`, `v0.0.2`, ... rolling over to `v0.1.0` after `.99`), tags it, and GoReleaser builds `linux`/`darwin` (`amd64` + `arm64`) archives with checksums onto a GitHub Release. Put `[release=skip]` in a commit message to skip the release for that push.

## Development

```sh
make check
```
