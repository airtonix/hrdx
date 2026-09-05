# Plugin-aware hrdx: design-space brainstorm

## Baseline constraints

hrdx already has several useful boundaries:

- The UI owns mutations through the Bubble Tea update loop.
- External clients use a newline-delimited JSON socket API.
- Pane processes are real PTYs, optionally owned by a persistent holder.
- Custom harnesses are declarative launch descriptions.
- Menus and events already provide limited process-local extensibility.
- The product values one portable binary, small dependencies, real terminals, persistence, and Windows support.

A plugin system should therefore add a **long-lived integration boundary**, not turn every event into a shell command or treat plugins as passive executable records.

---

# Candidate models

## 1. Plugin as a peer service

A plugin is an independently running process that connects to hrdx over a bidirectional protocol.

It can:

- request capabilities during handshake;
- subscribe to selected state streams;
- contribute commands, views, menu items, status indicators, or pane types;
- issue scoped requests back to hrdx;
- maintain its own state and lifecycle;
- receive structured responses and events rather than scraping terminal output.

### Shape

```text
hrdx  <-- bidirectional IPC -->  plugin process
  |
  +-- UI state
  +-- pane/PTY lifecycle
  +-- holder sessions
  +-- capability broker
```

This is the most substantial departure from both excluded designs. The plugin is not a command attached to an event and not merely a record describing an executable. It is a **participant in a negotiated protocol**.

### Strengths

- Clear crash and resource boundary.
- Plugins can be written in any language.
- Natural support for asynchronous operations and subscriptions.
- Can evolve into richer integrations: dashboards, agent orchestration, workspace providers, diagnostics, notifications.
- Security can be enforced per connection and per capability.
- Plugin-owned state does not need to pollute hrdx's persisted state.

### Weaknesses

- Requires a protocol, discovery mechanism, supervision, and compatibility policy.
- More operational complexity than a simple launch descriptor.
- UI contributions need careful design to avoid turning hrdx into a general GUI host.

---

## 2. Plugin as a brokered child process

hrdx launches and supervises the plugin, but communication remains structured and bidirectional.

Unlike a plain executable record, the launch configuration is only one part of the system. The important abstraction is the **managed protocol session**.

Possible transports:

```text
hrdx -> plugin stdin/stdout
hrdx <-> plugin local socket
hrdx <-> plugin inherited socket/pipe
```

### Strengths

- Predictable lifecycle.
- Easy to associate a plugin with a workspace or hrdx instance.
- No separate service manager required.
- Can apply environment, working directory, and resource limits.

### Weaknesses

- hrdx becomes responsible for plugin process management.
- A plugin started for one workspace may outlive that workspace unless ownership is explicit.
- Stdio is awkward if the plugin itself wants to launch or manage terminal-oriented processes.

This is a good implementation mechanism for the peer-service model, but should not be the public conceptual model.

---

## 3. Plugin as a capability provider

A plugin does not primarily “handle events.” It exposes named capabilities that hrdx and other plugins can invoke.

Examples:

```text
workspace.provider
pane.provider
command.provider
contextual-action.provider
status-widget.provider
notification.provider
agent-adapter.provider
search-provider
clipboard-provider
```

The plugin advertises:

```json
{
  "plugin": "git-tools",
  "protocol": 1,
  "capabilities": [
    "workspace.provider",
    "contextual-action.provider"
  ]
}
```

The host then grants a narrower set of operations:

```json
{
  "grants": [
    "workspace.read",
    "pane.read_metadata",
    "ui.menu.contribute",
    "ui.notification"
  ]
}
```

### Strengths

- Better security and composability than broad “plugin access.”
- Lets hrdx add new surfaces incrementally.
- Makes compatibility explicit: unsupported capabilities can be ignored or downgraded.
- Supports plugins that are useful without being deeply coupled to UI internals.

### Weaknesses

- Capability taxonomy requires design discipline.
- Easy to create a sprawling pseudo-API.
- Some capabilities are inherently difficult to sandbox, especially arbitrary pane input or process execution.

This should be the semantic foundation of the recommended design.

---

## 4. Declarative integration package

A plugin package contains a manifest describing contributions, schemas, and permissions. It may optionally point to a protocol-speaking process.

Example:

```json
{
  "id": "git-tools",
  "display_name": "Git Tools",
  "protocol": 1,
  "entrypoint": "git-tools",
  "contributes": {
    "actions": [
      {
        "id": "git-tools.diff",
        "label": "Open diff",
        "targets": ["workspace", "pane"]
      }
    ],
    "providers": ["workspace.provider"]
  },
  "requests": [
    "workspace.read",
    "pane.create",
    "ui.menu.contribute"
  ]
}
```

The manifest is declarative metadata, not the plugin implementation. The process uses the negotiated protocol to fulfill requests.

### Strengths

- Discoverable and inspectable before execution.
- Enables settings UI, enable/disable controls, validation, and documentation.
- Provides a stable contribution vocabulary.
- Can support static-only integrations with no process.

### Weaknesses

- Manifest schemas tend to become an accidental second API.
- Static declarations cannot express dynamic state well.
- It is tempting to put arbitrary shell commands into the manifest, recreating config hooks.

A manifest should describe identity, contributions, requested capabilities, and compatibility—not encode imperative behavior.

---

## 5. Embedded plugin runtime

Plugins run inside the hrdx process using one of:

- Go plugins;
- WebAssembly/WASI;
- Lua, JavaScript, or another embedded interpreter;
- a restricted bytecode runtime.

### Strengths

- Low IPC overhead.
- Direct access to carefully designed host interfaces.
- Easy synchronous APIs for small transformations.
- Potentially simple distribution for WASM plugins.

### Weaknesses

- Embedded Go plugins are not portable across Go versions, platforms, or builds.
- A panic, memory leak, deadlock, or blocking plugin can damage the TUI.
- Sandboxing is difficult unless using WASM.
- WASM introduces runtime and host-function complexity.
- Plugin upgrades become coupled to hrdx ABI/API details.
- Long-lived plugin work and process supervision are less natural.

Embedded plugins are suitable for pure, bounded transformations—such as formatting a status label or filtering a list—but are a poor default for process, workspace, or UI integrations.

---

## 6. External command adapter

hrdx executes a plugin command when a user invokes it, passing structured JSON over stdin and reading structured JSON from stdout.

This is simpler than a long-lived peer protocol:

```text
hrdx --plugin-request '{...}' | plugin
```

### Strengths

- Easy to implement and debug.
- Excellent for one-shot transformations.
- No persistent connection management.
- Works with any language.

### Weaknesses

- Poor fit for subscriptions, live status, progress, or UI state.
- Startup overhead for every action.
- No natural plugin-owned lifecycle.
- Tends to become event-to-command hooks if used too broadly.

This is useful as a **deliberately limited compatibility or utility mode**, but should not define the plugin architecture.

---

# IPC options

| IPC | Advantages | Problems | Fit |
|---|---|---|---|
| Existing NDJSON Unix/AF_UNIX socket | Familiar to hrdx, debuggable, cross-language, supports streams | Needs authentication/ownership checks and request multiplexing | Strong initial choice |
| Stdio pipes | Simple child supervision, no socket discovery | Plugin cannot easily multiplex terminal/process traffic; awkward restart and attachment | Good for child-only plugins |
| Unix socket / Windows named pipe | Local-only semantics, bidirectional, good lifecycle | Platform-specific implementation details | Strong production transport |
| Local TCP loopback | Portable and easy in many languages | Accidental network exposure, port discovery, weaker locality semantics | Avoid unless authenticated |
| JSON-RPC 2.0 | Existing ecosystem, request/response conventions | Verbose; can encourage generic API sprawl | Reasonable protocol discipline |
| Protobuf/gRPC | Typed schemas and streaming | Heavy dependency footprint and weaker fit for one-binary/minimal product | Later, if protocol scale demands it |
| MessagePack / CBOR | Compact binary messages | Less inspectable, more tooling burden | Not initially necessary |
| WASM host calls | Sandboxed embedded execution | Runtime complexity, poor fit for long-lived external integrations | Specialized option |

### Recommended IPC direction

Use a **versioned, bidirectional, local-only framed protocol**, initially implemented over:

- Unix domain sockets on Unix;
- named pipes or an equivalent local transport on Windows;
- optionally inherited stdio for plugins launched directly by hrdx.

The existing NDJSON API could serve as inspiration, but plugin IPC likely deserves:

- request IDs and concurrent in-flight calls;
- explicit message kinds;
- subscriptions;
- cancellation;
- progress;
- capability negotiation;
- protocol-level error types.

A length-prefixed JSON or JSON-RPC-like protocol is sufficient initially. Avoid introducing gRPC solely for plugins.

---

# Capability design

Capabilities should be narrow, composable, and separately grantable.

## Read capabilities

```text
workspace.list
workspace.read
tab.read
pane.read_metadata
pane.read_screen
pane.read_title
pane.observe_busy
events.subscribe
```

## Mutation capabilities

```text
workspace.create
workspace.close
tab.create
pane.create
pane.close
pane.send_input
pane.resize
```

## UI contribution capabilities

```text
ui.menu.contribute
ui.command.contribute
ui.status.contribute
ui.panel.contribute
ui.notification
ui.open_prompt
```

## Process capabilities

```text
process.spawn
process.spawn_in_workspace
process.open_terminal
```

These should be considered high-risk. A plugin that can arbitrarily send input to panes is close to having control of the user's shell or coding agent.

## Storage capabilities

```text
storage.plugin_private
storage.workspace_scoped
storage.global
```

Plugin-private storage should be namespaced and separate from hrdx's core state file. Plugins should not directly write hrdx's persisted JSON.

## Capability properties

Each capability should specify:

- whether it is read-only or mutating;
- whether it is workspace-scoped, pane-scoped, or global;
- whether it is available only interactively;
- whether it survives hrdx restart;
- whether it requires user approval;
- whether it can be delegated to another plugin.

A useful rule is:

> Plugins may observe broadly only with explicit declaration; they may mutate narrowly only with explicit grants.

---

# Lifecycle models

## Host-discovered, user-enabled

1. hrdx scans known plugin directories.
2. It validates manifests without launching plugins.
3. The settings UI lists available plugins.
4. Disabled plugins remain installed but inactive.
5. Enabled plugins launch on demand or at startup.

Best default for user control and startup performance.

## Lazy activation

Plugins launch only when:

- a contributed command is selected;
- a relevant workspace is opened;
- a pane of a contributed kind is created;
- a subscription is requested.

Best for lightweight startup and avoiding unnecessary processes.

## Workspace-scoped activation

A plugin may declare:

```text
activate when workspace matches:
  - file marker
  - path pattern
  - project type
```

This is useful for language/tool integrations, but matching must be declarative and local. It should not execute arbitrary shell probes during startup.

## Instance-scoped activation

The user explicitly starts a plugin for the current hrdx instance. It can observe all workspaces subject to grants.

Useful for orchestration and dashboards.

## Persistent daemon

A separately managed plugin daemon survives hrdx restarts and reconnects later.

This is powerful but should not be required initially. It introduces:

- stale registrations;
- ownership ambiguity;
- authentication and version skew;
- cleanup and orphan handling.

### Recommended lifecycle

Support these phases:

```text
discovered -> approved -> activated -> connected -> ready
                                      -> degraded
                                      -> stopping
                                      -> stopped
```

The protocol should include:

- `hello`;
- version and capability negotiation;
- host-granted capabilities;
- readiness notification;
- heartbeat or connection liveness;
- cancellation;
- graceful shutdown;
- crash/restart policy;
- explicit reason for deactivation.

A plugin crash should remove its contributions cleanly without affecting panes or the TUI.

---

# Embedded versus external

| Dimension | Embedded | External |
|---|---|---|
| Failure isolation | Weak except WASM | Strong |
| Startup latency | Low | Moderate |
| Language choice | Restricted | Any language |
| Portability | Difficult for native ABI | Strong |
| Security | Difficult | Easier to constrain |
| Long-lived state | Easy in-process, risky | Natural |
| UI access | Easy but dangerous | Explicit protocol required |
| Resource limits | Hard | Process-level controls possible |
| Distribution | Potentially single package | Separate executable/package |
| Debugging | Harder | Easier |
| Product fit | Small pure functions | Real integrations |

### Recommendation

- **Default: external plugins.**
- **Optional later: WASM for pure, sandboxed computation.**
- **Avoid native embedded plugins**, especially Go's native plugin mechanism.

---

# Declarative contributions

A plugin should declare contributions rather than registering arbitrary callbacks.

Possible contribution types:

## Commands

```json
{
  "id": "git-tools.open-log",
  "label": "Open Git log",
  "available_when": ["workspace.selected"],
  "input": {
    "workspace": "required"
  }
}
```

Invocation is then a protocol request to the plugin, not a shell command.

## Contextual actions

```json
{
  "id": "git-tools.blame",
  "label": "Blame current file",
  "targets": ["pane"],
  "requires": ["pane.read_metadata"]
}
```

The host supplies structured context:

```json
{
  "target": "pane",
  "pane_id": 12,
  "workspace_path": "/project",
  "tab_index": 0
}
```

## Providers

Providers could dynamically return:

- workspace candidates;
- pane kinds;
- search results;
- status badges;
- completion entries.

Providers should be pull-based or subscription-based, rather than receiving every hrdx event by default.

## Views and panels

A plugin may contribute a panel, but the first version should avoid embedding arbitrary rendering logic. Safer initial forms include:

- text/ANSI content;
- structured lists;
- tables;
- selectable actions;
- links back to panes.

Full custom rendering would tightly couple plugins to Bubble Tea and terminal dimensions.

## Pane providers

A plugin may provide a pane kind whose contents come from the plugin protocol rather than a PTY. This conflicts with hrdx's “every pane is a real PTY” identity.

Possible choices:

1. Do not support non-PTY panes initially.
2. Permit plugin panels outside the pane tree.
3. Define plugin panes as PTY-backed adapters only.
4. Add a separate “view” concept distinct from panes.

The strongest product-preserving choice is **separate plugin views**, not virtual panes.

---

# Compatibility strategy

## Protocol compatibility

Use independent versions for:

```text
protocol version
capability version
manifest schema version
plugin implementation version
```

Rules:

- Unknown optional fields are ignored.
- Unknown capabilities are not granted.
- Required capabilities cause activation failure with a clear message.
- New host methods must not invalidate old plugins.
- Plugin requests must include a request ID.
- Host responses should include structured error codes.
- Events should be versioned by name and payload shape.

## hrdx compatibility

Plugins should not depend on:

- internal Go packages;
- Bubble Tea message types;
- terminal emulator internals;
- state-file layout;
- holder wire protocol;
- undocumented screen scraping.

The stable surface should be a public plugin protocol and manifest schema.

## Existing integrations

Existing surfaces can coexist:

| Existing mechanism | Future role |
|---|---|
| `harness.json` | Continue to describe agent launch behavior |
| Socket API | Continue serving scripts and editors |
| `menu.register` | Remain a low-level external-client compatibility feature |
| Event subscriptions | Become a possible transport primitive, not the plugin contract |
| Holder protocol | Remain internal to PTY persistence |
| Shell commands | Remain supported for user workflows, not plugin callbacks |

A migration path could allow a plugin process to use the existing socket API initially, while a dedicated plugin protocol evolves. However, relying indefinitely on unrestricted socket access would undermine capability control.

---

# Comparison of architectural directions

| Direction | Isolation | Richness | Portability | Complexity | Main risk |
|---|---:|---:|---:|---:|---|
| Event-to-command hooks | Low | Low | High | Low | Becomes untyped shell automation |
| Executable/plugin records | Medium | Low–medium | High | Low | Static metadata mistaken for lifecycle |
| One-shot JSON commands | Medium | Medium | High | Low | Poor live behavior |
| External peer protocol | High | High | High | Medium | Protocol/API sprawl |
| Capability broker | High | High | High | High | Permission taxonomy complexity |
| Embedded native plugins | Low | High | Low | Medium | Crashes and ABI coupling |
| WASM plugins | High | Medium | Medium | High | Runtime and host API burden |
| Plugin-owned panes | High | High | High | High | Contradicts real-PTY model |
| Plugin-owned views | High | Medium–high | High | Medium | Requires constrained UI schema |

---

# Recommended architecture

## “Capability-oriented local plugin peers”

Build plugins as **external, long-lived peers** using a versioned local protocol.

### Core components

```text
Plugin registry
  - discovers manifests
  - validates compatibility
  - records enablement

Plugin supervisor
  - lazy launches processes
  - owns connection
  - applies restart/backoff policy
  - removes contributions on failure

Capability broker
  - negotiates requested capabilities
  - applies user/config grants
  - scopes access to workspace/pane/instance

Plugin protocol
  - handshake
  - request/response
  - notifications/subscriptions
  - cancellation/progress
  - structured errors

Contribution registry
  - commands
  - contextual actions
  - status items
  - constrained views/providers
```

## Initial scope

Start with a deliberately small first version:

1. External plugin discovery through a manifest.
2. Lazy activation.
3. Local bidirectional IPC.
4. Handshake and capability negotiation.
5. Plugin-contributed commands and contextual actions.
6. Read-only workspace/pane metadata.
7. Notifications and structured results.
8. Plugin-private storage.
9. Crash cleanup and optional restart.
10. No arbitrary plugin-rendered panes yet.
11. No arbitrary process spawning by plugins by default.
12. No direct mutation of hrdx state files.

## Example conceptual exchange

```text
Host -> Plugin:
  hello
  requested_protocol: 1
  host_version: ...
  available_contexts: ...

Plugin -> Host:
  ready
  requires: ["workspace.read", "ui.command.contribute"]
  contributes: [...]

Host -> Plugin:
  grants:
    - workspace.read
    - ui.command.contribute

Host -> Plugin:
  command.invoke
  command_id: "git-tools.open-log"
  context:
    workspace_path: "/project"
    pane_id: 12

Plugin -> Host:
  command.result
  actions:
    - {type: "open_pane", kind: "shell", text: "git log"}
```

The plugin requests an operation through a typed protocol. It does not map a raw hrdx event to a shell command.

---

# Important boundaries

## Do not grant the socket wholesale

A plugin should not receive unrestricted access to the existing control socket. That would bypass capability negotiation and make the plugin system equivalent to “trusted local scripts.”

If reuse is desired, add an authenticated, scoped connection mode or a host-issued plugin token.

## Do not expose raw Bubble Tea types

The protocol should describe semantic concepts:

```text
workspace
tab
pane
command
view
notification
```

not implementation details such as:

```text
tea.Msg
tea.Cmd
internal/ui.Model
```

## Do not make plugins part of persisted core state

Persist:

- plugin enabled/disabled preference;
- optionally plugin configuration;
- stable contribution selections if needed.

Do not persist:

- live connection handles;
- process IDs;
- plugin callbacks;
- arbitrary plugin state inside hrdx's state schema.

## Do not make every event observable by default

Use subscriptions that are:

- explicitly requested;
- filtered by event type;
- optionally scoped to a workspace or pane;
- bounded and droppable where appropriate.

This preserves hrdx's non-blocking event behavior and reduces data exposure.

---

# Final recommendation

Adopt **external, supervised, capability-negotiated plugin peers** with declarative manifests and constrained contributions.

The key distinction is:

> A plugin is a participant that owns an integration protocol and may receive narrowly scoped capabilities—not a command attached to an event and not merely an executable description.

Prioritize commands, contextual actions, read-only providers, notifications, and structured views first. Defer arbitrary panes, process spawning, embedded runtimes, and persistent daemons until real integrations demonstrate that they are necessary. This preserves hrdx's portable, PTY-centric design while creating a durable extension boundary capable of supporting richer integrations later.
