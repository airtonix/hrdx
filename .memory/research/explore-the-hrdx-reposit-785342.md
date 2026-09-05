## Architecture overview

hrdx has no general plugin/extension runtime. “Plugin awareness” is currently implemented through two narrower mechanisms:

1. **Custom agent harness registration** from `harness.json`.
2. **Process-local API menu registration** through `menu.register`.

Both are owned by `internal/ui`; the API and holder layers provide transport but do not know about plugins or harnesses.

---

## Config loading and discovery

### Startup flow

`cmd/hrdx/main.go` is the composition root:

- Parses CLI flags and resolves the state path: `cmd/hrdx/main.go:140-159`.
- Loads persisted state before constructing the UI: `cmd/hrdx/main.go:161-169`.
- Builds `ui.Config`, including built-in binary overrides and Zot-specific arguments: `cmd/hrdx/main.go:180-207`.
- Constructs the model: `cmd/hrdx/main.go:209`.
- Wires the event broadcaster, holder client, API server, and Bubble Tea program: `cmd/hrdx/main.go:210-252`.

The state directory is therefore the natural configuration/plugin directory. The same directory currently contains:

- `state.json`
- `harness.json`
- `keys.json`
- `sounds.json`
- `themes/*.json`

### Harness loading

`ui.New` loads custom harnesses before validating the configured default agent:

- `internal/ui/model.go:410-431`
- `loadHarnesses(filepath.Dir(statePath))`: `internal/ui/model.go:414-420`
- `internal/ui/harness.go:43-70`

The harness schema is private to `internal/ui`:

- `kind`
- `binary`
- `args`
- `resume`
- `resume_first`
- `busy`
- `idle_title`
- `attention_title`

See `internal/ui/harness.go:18-41`.

Registration creates an `agentSpec` and appends it to the package-level `agentSpecs` registry:

- `internal/ui/harness.go:73-112`
- Registry declaration: `internal/ui/agents.go:122-143`

### Existing extension point

A plugin that only needs to make an external CLI visible as an agent can fit into this existing harness path:

- `agentByKind` / `isAgentKind`: `internal/ui/agents.go:145-156`
- Binary resolution and CLI overrides: `internal/ui/agents.go:158-168`
- Installed-agent discovery: `internal/ui/agents.go:227-237`
- Picker/cycling availability: `internal/ui/agents.go:239-259`
- Settings integration: `internal/ui/settings.go:47-92`
- Launch and resume argument construction: `internal/ui/model.go:624-639`

The harness is treated as a first-class agent in pickers, settings, pane naming, busy detection, persistence restore, and launch behavior.

### Config-loading constraints and risks

- **No runtime reload.** `harness.json` is read only during `ui.New`; changes require an hrdx restart.
- **Registry is global mutable state.** `agentSpecs` is package-level and `registerHarness` mutates it. Multiple `Model` instances in one process can accumulate or replace harnesses unexpectedly. This is especially relevant for tests and any future embedded/plugin host.
- **No plugin ownership/lifecycle.** There is no unregister, versioning, capability declaration, health check, or shutdown hook.
- **Unknown persisted kinds degrade to shells.** During restore, a saved kind that is no longer registered becomes `"shell"`: `internal/ui/persist.go:82-93`.
- **Harness identity is only a string.** The `kind` is used in state, API requests, pane names, settings, and detection. Renaming a kind breaks continuity.
- **Binary validation is deferred.** `registerHarness` does not verify the executable. Launch failures surface later as pane failures.
- **Argument model is static.** Arguments are passed directly to a local process. There is no structured environment, capability negotiation, plugin callback, or dynamic argument expansion.
- **Remote/container support remains process-wrapper support.** The holder keeps the local `ssh`, `docker`, or `kubectl` process alive; it does not understand remote plugin state or reconnect semantics.

---

## UI and lifecycle boundaries

### Bubble Tea owns live state

`ui.Model` contains all live workspace, tab, pane, menu, and plugin-adjacent state:

- `config`, `spaces`, pane IDs, disabled kinds, custom menus, event broadcaster, holder: `internal/ui/model.go:103-186`.

The model is mutated through Bubble Tea’s update loop:

- API requests arrive as `api.Request` messages: `internal/ui/model.go:789-932`.
- `handleAPI` performs API mutations inside the UI loop: `internal/ui/api.go:12-161`.
- Pane starts happen asynchronously and return `paneStartedMsg`: `internal/ui/model.go:609-655`, `831-852`.
- Process output/exit is represented by terminal update messages: `internal/ui/model.go:854-875`.
- Holder exits are translated into `HolderExitMsg`: `internal/ui/model.go:924-932`.

This is the main ownership boundary: plugins should not mutate `Model` directly from goroutines.

### Pane lifecycle

Pane creation follows:

1. UI creates a logical pane.
2. UI persists the layout.
3. UI returns a response/event.
4. A `tea.Cmd` starts or attaches the PTY.

API-created panes use this sequence in `internal/ui/api.go:272-400`.

Launching is centralized in `startPane`:

- Resolves shell or agent command.
- Applies harness args and resume args.
- Chooses holder-backed or local execution.
- Uses 80×24 until a real terminal size is available.

See `internal/ui/model.go:609-655`.

Pane removal closes the terminal and updates the split tree:

- `internal/ui/model.go:3061-3098`
- Tab cleanup: `internal/ui/model.go:3100-3115`

Important implication: an extension that creates or owns panes should use the existing pane lifecycle rather than bypassing `startPane`, `removePane`, persistence, or layout operations.

---

## Terminal boundary

`internal/term.Pane` is the terminal-facing abstraction:

- Local PTY or holder-backed session: `internal/term/pane.go:27-45`
- Holder transport interface: `internal/term/pane.go:17-25`
- Local startup: `internal/term/pane.go:67-101`
- Holder-backed construction: `internal/term/pane.go:116-134`
- Output feeding: `internal/term/pane.go:136-143`
- Exit signaling: `internal/term/pane.go:145-156`

All subprocesses receive a normalized environment:

- `TERM=xterm-256color`
- `TERM_PROGRAM=vscode`
- `HRDX=1`

See `internal/term/pane.go:169-201`.

This is the strongest current “awareness” signal available to child tools. A plugin can detect hrdx through `HRDX=1`, but cannot query hrdx capabilities from the terminal environment.

Busy/idle awareness is screen/title based:

- Foreground process matching: `internal/ui/agents.go:170-190`
- Optional title markers: `internal/ui/agents.go:192-211`
- Busy tracking and event publication: `internal/ui/model.go:331-369`, with detection implementation later in the same file around the `paneBusy` logic.

Risks:

- Foreground detection compares process names/binaries and can fail for wrappers or remote commands.
- Screen substring detection is inherently heuristic and may produce false busy/idle transitions.
- Title markers are substring matches, not structured protocol messages.
- A shell pane can be recognized as an agent only when its current foreground process matches a registered agent binary.

---

## Persistence boundary

The serializable model lives in `internal/state`:

- Pane identity includes `Kind`, `Name`, and holder `Session`: `internal/state/state.go:13-21`.
- Tabs, layouts, workspaces, and preferences are JSON structures: `internal/state/state.go:23-78`.
- Default path, load, and atomic save: `internal/state/state.go:80-128`.

Live-to-saved conversion is correctly isolated in `internal/ui/persist.go`:

- Snapshot: `internal/ui/persist.go:5-43`
- Restore: `internal/ui/persist.go:64-110`
- Layout validation/fallback: `internal/ui/persist.go:112-160`

Extension implications:

- A new plugin-owned persisted field belongs in `internal/state`, with conversion in `internal/ui/persist.go`.
- Process-local registrations should not be persisted under the current contract.
- Harness configuration is external to `state.json`; persisted panes only retain the string `Kind`.
- Restoring requires the harness registry to be populated before `restore`, which is why `ui.New` loads harnesses first.
- Removing or renaming a harness changes restore behavior and can silently turn panes into shells.

---

## Socket/API boundary

### Transport

The public API is newline-delimited JSON over a Unix/AF_UNIX socket next to the state file:

- Protocol and ownership contract: `internal/api/api.go:1-15`
- Socket startup and stale-socket handling: `internal/api/server.go:40-96`
- Per-connection request handling: `internal/api/server.go:98-122`

`cmd/hrdx` forwards API requests into the Bubble Tea loop:

- `program.Send(request)`: `cmd/hrdx/main.go:236-245`
- UI handles them in `internal/ui/api.go:12-161`.

The API server itself must not touch UI state directly.

### Existing extension points

#### New API methods

Additions require coordinated changes to:

1. Public request/response types: `internal/api/api.go`
2. Wire-method dispatch and parameter decoding: `internal/api/server.go:124-200`
3. UI-loop handling: `internal/ui/api.go:16-161`
4. API tests and README examples.

Current methods include pane/workspace control, status, waiting, and menu registration.

#### Menu registration

`menu.register` is the existing plugin-like control surface:

- Public schema: `internal/api/api.go:86-92`
- Wire dispatch: `internal/api/server.go:173-178`
- Validation and process-local storage: `internal/ui/api.go:168-200`
- Menu composition: `internal/ui/model.go:218-242`
- Selection event publication: `internal/ui/api.go:202-242`

Registrations are:

- Ephemeral.
- Limited to 64 entries.
- Scoped to pane, tab, or sidebar.
- Identified by an `action_id`.
- Display-only; selecting one emits an event but does not invoke plugin code inside hrdx.

The plugin/client must remain connected to receive `menu.action` events and perform the action externally.

### Event lifecycle

The event path is:

1. UI detects a lifecycle/busy/menu change.
2. `Model.publish` sends it to `api.Broadcaster`.
3. API subscribers receive pushed JSON events.

Key definitions:

- Event names and payloads: `internal/api/api.go:131-172`
- Broadcaster: `internal/api/api.go:174-220`
- Publication sites: `internal/ui/api.go:61-75`, `internal/ui/api.go:126-145`, `internal/ui/api.go:323-334`, and busy tracking in `internal/ui/model.go:331-369`
- Subscription handling: `internal/api/server.go:263-290`

Constraints and risks:

- Subscriber channels buffer only 64 events.
- Slow subscribers drop events rather than blocking the UI.
- Events are not durable and have no sequence number or replay.
- A plugin must use `status` to recover from missed events.
- `events.subscribe` consumes the connection permanently; it cannot multiplex ordinary requests afterward.
- The event stream currently covers workspace/pane lifecycle, busy transitions, and menu actions, not startup failures, holder attach failures, configuration changes, or plugin discovery.

---

## Holder/UI boundary

### Holder process

The holder is a separate `hrdx --holder` process:

- Entry point: `cmd/hrdx/main.go:122-135`
- Spawn/connect logic: `internal/holder/client.go:31-102`
- Holder server and single-client model: `internal/holder/server.go:49-129`

The holder owns PTYs and survives TUI restarts. Its protocol is a private framed protocol:

- Frame types and protocol version: `internal/holder/protocol.go:1-85`
- Operations: `hello`, `start`, `attach`, `resize`, `kill`, `fg`, `list`, `shutdown`: `internal/holder/protocol.go:43-79`
- Server dispatch: `internal/holder/server.go:160-234`
- PTY launch: `internal/holder/server.go:236-278`
- Output buffering/replay: `internal/holder/server.go:281-328`

The UI talks to it through `holder.Client`:

- Start/attach/write/resize/kill/list: `internal/holder/client.go:216-275`
- Exit callback: `internal/holder/client.go:209-214`

### Holder extension constraints

- The holder serves exactly one TUI client at a time: `internal/holder/server.go:49-64`, `111-129`.
- It has no plugin registry or plugin event namespace.
- It treats processes as opaque commands with args, cwd, and environment.
- The holder protocol is versioned independently from the public API: `internal/holder/protocol.go:13-16`.
- Incompatible protocol versions cause the old holder to be shut down and sessions to be lost: `internal/holder/client.go:81-101`.
- Detached output is capped at 1 MiB per session: `internal/holder/server.go:27-29`.
- Holder session IDs are persisted in state but verified against workspace CWD before reattachment: `internal/ui/model.go:668-714`.

A plugin should not communicate directly with the holder unless it genuinely needs PTY ownership semantics. The public API and UI model are safer extension boundaries.

---

## Recommended extension strategies

### If “plugin awareness” means recognizing external agent CLIs

Use/extend the harness model:

- Add capabilities to `harnessSpec` / `agentSpec`.
- Keep registration before restore.
- Ensure `isAgentKind`, launch construction, settings, status, and persistence all agree on the new identity.
- Add explicit migration behavior for renamed or removed kinds.

### If it means letting external plugins integrate with hrdx

The existing API is the natural boundary:

- `menu.register` for UI affordances.
- `events.subscribe` for lifecycle notifications.
- `status`, `pane.read`, `pane.send_text`, `pane.create`, and `pane.close` for control.
- Add API methods only through the `api → server → UI update loop` path.

### If it means in-process plugins

There is currently no safe lifecycle boundary for them. An in-process design would need, at minimum:

- Explicit registration/unregistration.
- Per-instance registry rather than package globals.
- Startup/shutdown hooks.
- Capability/version metadata.
- Error isolation.
- Event ownership and backpressure policy.
- A decision about whether plugin state is persisted.
- A clear rule preventing plugins from mutating `Model` outside the Bubble Tea loop.

---

## Overall risks

1. **Global harness registry** makes plugin discovery process-global and difficult to isolate.
2. **String-only plugin identity** makes state compatibility fragile.
3. **Heuristic busy detection** can misclassify wrappers, remote agents, and changing UIs.
4. **Best-effort events** can lose plugin notifications without replay or sequence tracking.
5. **API socket access is local but unauthenticated**; any process able to connect can control panes and read terminal screens.
6. **Holder sessions are opaque and single-client**, so a plugin cannot independently attach to a pane through the holder.
7. **No lifecycle events for configuration or failures** means plugins must poll status to discover several important transitions.
8. **No dynamic discovery** means `harness.json` edits, binary installation, and plugin availability changes are restart-sensitive.
9. **Persistence fallback is lossy**: missing harness definitions restore as shell panes rather than preserving an unavailable plugin identity.
