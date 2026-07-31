# hrdx

**Run all your coding agents. In one terminal. At once.**

hrdx is a minimal and lightweight terminal multiplexer built for the agent era: your projects as workspaces in a sidebar, tabs per workspace, and real terminal panes running [Codex CLI](https://learn.chatgpt.com/docs/codex/cli), [Claude Code](https://code.claude.com/docs/en/quickstart), [zot](https://www.zot.sh), [pi](https://www.pi.dev) or plain shells side by side. Kick off an agent in one project, switch to the next, and let the sidebar spinners tell you who is still working.

- **Real terminals, not wrappers.** Every pane is a genuine PTY session with a full terminal emulator behind it. Agent TUIs run exactly as they do standalone: streaming, slash commands, sessions, mouse support, all of it.
- **Everything in view.** The sidebar shows every workspace with its git branch and ahead/behind counts, every pane with a live status dot, and an agents list that jumps you straight to any running agent, including ones you started by hand inside a shell.
- **Feels like your terminal.** Scrollback, mouse selection with clipboard copy, drag-to-resize splits, drag-to-reorder workspaces, right-click context menus, and kitty keyboard protocol pass-through so even exotic chords like ctrl+1 reach your agent.
- **Picks up where you left off.** Workspaces, tabs, splits, and ratios survive restarts. Agent panes relaunch resuming their latest session for that project, automatically.
- **Yours to tune.** A settings window (`ctrl+b ,` or the gear in the sidebar) lets you switch individual agents on or off and enable a notification sound when an agent finishes its turn. All persisted.
- **Bring your own agent.** Register any agent CLI as a custom harness via a small JSON file, including its own busy detection for the sidebar spinner and finish sound. It shows up in pickers, cycling, and settings like the built-ins. See [Custom harnesses](#custom-harnesses).

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/patriceckhart/hrdx/main/install.sh | bash
```

macOS or Linux, plus at least one agent CLI on your PATH: `codex`, `claude`, `pi` or `zot`. Update any time with `hrdx update`.

## Run

```sh
hrdx
```

Open several projects at once, or pick your default agent:

```sh
hrdx --cwd ~/Developer/api --cwd ~/Developer/web
hrdx --agent claude
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
| `--codex-bin PATH` | Use a specific codex binary |
| `--claude-bin PATH` | Use a specific claude binary |
| `--pi-bin PATH` | Use a specific pi binary |
| `--zot-bin PATH` | Use a specific zot binary |
| `--shell PATH` | Shell for shell panes (default `$SHELL`) |
| `--state PATH` | State file for workspace persistence (empty disables) |
| `--fresh` | Ignore saved workspaces and start clean |

## Keys

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
| `,` | Settings window: enable / disable agents, finish sound |
| `x` | Close pane (sibling takes its room) |
| `X` | Close workspace |
| `ctrl+b` | Send a literal ctrl+b to the pane |
| `q` | Quit |
| `left` / `right` | Scroll the hint row in the footer (narrow terminals) |

## Mouse

Everything is clickable: tabs, workspaces, panes, agents, menus, and the settings entry at the bottom of the sidebar. Drag workspaces to reorder them, drag pane borders to resize, right-click for context menus, and drag with the left button to select text (copied straight to your clipboard). Wheel events go to the pane under the cursor: agent TUIs scroll themselves, shells scroll their local history, and `shift+pgup` / `shift+pgdn` do the same from the keyboard.

## Custom harnesses

Any agent CLI beyond the built-ins can be registered by dropping a `harnesses.json` next to the state file (`~/Library/Application Support/hrdx/` on macOS, `$XDG_CONFIG_HOME/hrdx/` on Linux). Registered harnesses appear everywhere the built-ins do: in the pickers, in agent cycling, in the sidebar agents list, and in the settings window for enabling and disabling.

```json
[
  {
    "kind": "aider",
    "binary": "aider",
    "args": ["--no-auto-commits"],
    "resume": ["--restore-chat-history"],
    "busy": "Waiting for the model"
  },
  { "kind": "goose" }
]
```

| Field | Purpose |
|---|---|
| `kind` | Identifier used in pickers and pane names (required, must not collide with built-ins) |
| `binary` | Executable to launch (default: same as `kind`) |
| `args` | Extra arguments passed on every launch |
| `resume` | Arguments that resume the latest session when a restored pane relaunches |
| `resume_first` | Put the resume args before `args` (for subcommands like `resume --last`) |
| `busy` | A substring visible on screen only while the harness is working; drives the busy spinner and the finish sound. Empty: braille spinner detection, like the built-ins |

## Persistence

Workspaces, panes, split layout, ratios, and selection are saved automatically (default: `~/Library/Application Support/hrdx/state.json` on macOS, `$XDG_CONFIG_HOME/hrdx/state.json` on Linux). On the next launch everything is restored: shell panes get a fresh shell, and agent panes relaunch resuming their latest session for that directory. `--fresh` skips restoring; `--state ""` disables persistence entirely.

## Development

```sh
make check
```

## License

MIT
