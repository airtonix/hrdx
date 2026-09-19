# Plugin platform implementation checklist

Source: https://github.com/patriceckhart/hrdx/discussions/23

Supporting proposal: https://gist.github.com/airtonix/a038219ae8f8793d2b6d0fcb1d48272f

This is an implementation plan, not a claim that the proposed contracts are approved or implemented. Checked boxes record completed work only. Complete a small experimental vertical slice before committing to the full platform. Later phases cover the remaining proposed functionality, but require an explicit scope decision before implementation.

Current implementation: opt-in supervised stdio runtime, provenance-bound approval, revocation, and explicit reapproved reload, contextual actions with post-activation target revalidation, finder search providers, activation markers, typed configuration, notifications with severity and prioritized status, scoped metadata, ordinary and temporary pane operations, bounded pane input, snapshot subscriptions, private storage, floating plain-text views, detailed trusted status diagnostics, and lifecycle controls through keys, menus, settings, CLI, and the existing control socket. See `docs/plugin-platform.md` and the reference peer in `examples/plugins/hello`.

Not all work below is complete. Remaining areas are per-tab and per-pane grant scopes, automatic hot reload without explicit reapproval, and execution of the platform suite on native Windows. Unix uncatchable-host-exit cleanup is a documented limitation. Do not mark platform runs as done based on cross-compilation.

Validation of the current working tree: the full Go suite, vet, race suite, manifest and frame fuzzing, and reference-peer handshake/invocation/provider/shutdown smoke test pass on macOS arm64. The race suite also passes on Linux amd64 in a golang:1.25 container. All test packages cross-compile and vet for Windows amd64; native Windows execution is still unverified. No commits have been made.

## 1. Resolve the design boundary

- [x] Read the discussion and supporting plan again before implementation and incorporate subsequent maintainer decisions.
- [x] Inspect current implementation, tests, README, and all applicable AGENTS.md files for each affected package.
- [x] Record a provisional RFC with acceptance criteria, non-goals, package ownership, and the decisions below. Runtime decisions remain explicitly open.
- [x] Define plugins as trusted external executables, not sandboxed code.
- [x] Explain that grants restrict host-mediated operations only. Same-user plugins can otherwise access files, spawn processes, use the control socket, and potentially contact the holder.
- [x] Remove promises that capabilities prevent arbitrary OS process creation or isolate private files from other same-user processes.
- [x] Preserve the existing control socket as a separate trusted-client interface. Do not present a private plugin transport as protection against bypass through that socket.
- [x] Explicitly exclude OS sandboxing from v1 unless maintainers approve a separate cross-platform containment project.
- [x] Keep terminal panes PTY-backed and plugin views separate from terminal panes, holder identities, and persisted split trees.
- [x] Keep live UI mutations on the Bubble Tea update loop and reuse existing model operations.
- [x] Keep plugin processes tied to normal TUI shutdown rather than the session holder. Document Unix uncatchable-exit limitations.
- [x] Specify that normal panes created through approved plugin actions become host-owned and survive plugin disconnect.
- [x] Specify cleanup for explicitly temporary plugin resources without killing ordinary panes or holder sessions.
- [x] Keep harness.json behavior unchanged. Do not reinterpret custom harnesses as plugins.
- [x] Define independent manifest, wire protocol, and plugin implementation versions.
- [x] Treat zot as design inspiration, not a blanket compatibility target. No shared protocol compatibility is claimed.
- [x] Gate execution behind --plugins and floating views behind --plugin-views, both disabled by default.
- [x] Select and implement the hello reference integration with a contextual action, notification, and optional floating view.
- [x] Define measurable startup, memory, queue, and rendering overhead budgets for the first slice. Documented in the platform reference with a round-trip benchmark and goroutine bound test.
- [x] Keep the RFC provisional until the reference integration and failure tests exercise it.

## 2. Establish ownership and integration points

- [x] Assign manifest discovery, registry, supervisor, and grant policy to internal/plugin.
- [x] Define independent documented JSON wire types in internal/plugin/protocol.go, without exposing live Go types.
- [x] Keep existing control socket contracts in internal/api and expose only allowlisted peer operations.
- [x] Wire configuration and supervisor startup/shutdown in cmd/hrdx.
- [x] Put serializable approvals in internal/state. Store them separately from workspace snapshots to prevent the running TUI from overwriting CLI approval changes.
- [x] Implement UI contribution handling and plugin request adapters in internal/ui.
- [x] Inspect platform process helpers before introducing new launch or process-tree termination code.
- [x] Leave internal/holder, internal/term, and internal/vt unchanged.
- [x] Use instance-owned registries and supervisors, not mutable package globals.
- [x] Audit dependencies and binary-size impact. Prefer standard-library facilities and existing dependencies.

## 3. Define identity, approval, and grants before execution

- [x] Define plugin ID syntax, normalization, namespace rules, and collision handling.
- [x] Define contribution IDs as plugin-namespaced identifiers.
- [x] Define resource identifiers for workspace, tab, pane, view, plugin instance, and connection generation.
- [x] Specify identifier lifetime across rename, reorder, close, reopen, TUI restart, and holder reattachment.
- [x] Avoid treating tab indexes, display names, or stale pane IDs as durable authorization identities.
- [x] Define how persisted scoped grants resolve safely to resources after restart.
- [x] Bind execution approval to canonical package path and a digest of package files.
- [x] Require reapproval after package edits or moves, reject runtime package symlinks, and require instance restart to load new approvals.
- [x] Prevent project-local packages from silently inheriting global package execution approval or grants.
- [x] Specify duplicate-ID behavior and whether shadowing is allowed. Duplicate IDs invalidate every claimant, with no precedence.
- [x] Separate explicit --trust execution approval from individually named host-operation grants.
- [x] Define requested, required, optional, granted, denied, and revoked capability semantics.
- [x] Default all grants to denied and require explicit approval rather than trusting manifest declarations.
- [x] Define per-capability scope, sensitivity, approval requirements, query visibility, and mutation behavior.
- [x] Keep terminal screen/title access, shell input, pane creation/closure, and workspace closure separately grantable.
- [x] Treat paths, metadata, titles, invocation context, and event payloads as potentially sensitive.
- [x] Require fresh approval for newly requested privileges during upgrades.
- [x] Revoke changed approvals on a 500ms poll, stop their processes, reject stale generations and queued work, and remove contributions. Document that completed side effects cannot be undone.
- [x] Authorize resolved targets and current connection grants at UI execution time.
- [x] Limit command results to validated notifications with a current grant and generation. Require explicit brokered calls for mutations.
- [ ] Define host-mediated process.spawn only if a later use case needs it, without implying control over a plugin's own OS privileges.

## 4. Manifest and discovery, without execution

- [x] Specify manifest schema version, plugin ID/version, display metadata, entrypoint, argv, protocol range, contributions, activation policy, and capability requests.
- [x] Decide which optional configuration schema features are supported without adding a large validation dependency.
- [x] Specify unknown optional fields, unsupported required features, and incompatible protocol behavior.
- [x] Bound manifest size, contribution count, string lengths, and configuration size.
- [x] Validate IDs, versions, protocol ranges, duplicate contributions, capability names, and required fields.
- [x] Resolve executable paths relative to the package using explicit argv, never shell evaluation. Inventory checks metadata only and never launches the entrypoint.
- [x] Define PATH lookup, absolute paths, symlink handling, and missing/non-executable entrypoint behavior.
- [x] Document inherited environment and package cwd as part of trusted execution. Do not log their values.
- [x] Define deterministic user, explicit project/instance, and development discovery roots on every supported platform.
- [x] Decide whether platform data-directory discovery adds value beyond the user and explicit development roots. Omit additional automatic discovery roots.
- [x] Require explicit trust before launching any discovered code.
- [x] Ensure discovery performs no executable probes, shell hooks, downloads, or package installation scripts.
- [x] Produce diagnostics for malformed manifests, duplicates, unsupported versions, missing entrypoints, inaccessible directories, and shadowed packages. Shadowing is rejected rather than selecting a winner.
- [x] Add plugins list and plugins doctor, or equivalent agreed CLI diagnostics, that work without launching plugins.
- [x] Add valid and invalid synthetic manifest fixtures.
- [x] Test precedence, duplicate IDs, provenance changes, malformed versions, unknown optional fields, limits, paths, and permission failures.
- [x] Test that inventory and diagnostics cannot start plugin processes.

## 5. Versioned NDJSON-over-stdio protocol

- [x] Specify LF-terminated JSON frames on stdout and drain/discard stderr without retaining sensitive output.
- [x] Document the request, response, event, cancellation, and error envelope.
- [x] Define hello, hello_ack, ready, shutdown, and shutdown_ack ordering. Contributions are manifest-declared rather than dynamically registered.
- [x] Validate peer identity against the supervisor-selected package rather than trusting peer-provided identity.
- [x] Define protocol negotiation, required versus optional features, and unsupported-version errors.
- [x] Distinguish protocol feature advertisement from permission grants.
- [x] Define directional connection-local IDs, duplicate outstanding-request rejection, ignored late replies, and 32-call limits.
- [x] Specify byte limits, read/write deadlines, handshake timeout, request timeout, shutdown timeout, and maximum queued bytes/frames.
- [x] Reject malformed/ambiguous JSON, partial final lines, oversized frames, invalid UTF-8, and out-of-order or unknown runtime frames.
- [x] Define cancellation and late-response handling without claiming rollback.
- [x] Document that ambiguous mutation timeouts require reconciliation, not automatic retries.
- [x] Implement one bounded writer path and force cleanup when a non-reading peer blocks shutdown.
- [x] Reject runtime frames before readiness and stale frames from prior plugin generations.
- [x] Define stable structured error codes and sanitized messages.
- [x] Defer progress and heartbeat frames. Calls have deadlines and process/transport exits are observed independently.
- [x] Provide language-neutral wire examples and synthetic helper peers.
- [x] Test framing across arbitrary read boundaries, concurrent calls, timeout, cancellation, malformed/oversized frames, slow readers, and disconnect cleanup.
- [x] Keep future socket or named-pipe transports out of v1 and avoid transport-specific assumptions in protocol objects.

## 6. Supervisor and process lifecycle

- [x] Implement discovered, validated, disabled/approved, starting, handshaking, ready, degraded, stopping, stopped, and failed states as needed.
- [x] Define allowed transitions and distinguish invalid packages, denied approval, handshake failure, crash, and intentional stop.
- [x] Launch approved executables directly with explicit argv, cwd, and environment.
- [x] Associate each process, transport, request, and contribution with a plugin instance and generation.
- [x] Use lazy activation for the reference command and explicit start/stop/restart controls.
- [x] Define how a declared but inactive command is presented and how activation failure reaches the user.
- [x] Implement 500ms graceful shutdown followed by Unix process-group cleanup or Windows kill-on-close job cleanup.
- [x] Ensure plugins and their managed descendants do not survive TUI shutdown unintentionally.
- [x] Close pipes, reap processes, cancel pending work, and release goroutines on every startup and shutdown failure path.
- [x] Remove session-owned contributions through the UI loop on disconnect.
- [x] Leave existing host-owned workspaces, normal panes, agents, and holder sessions running on plugin failure.
- [x] Use zero stderr retention: drain to discard rather than introducing potentially sensitive log files.
- [x] Do not log protocol payloads, terminal contents, secrets, or sensitive argv.
- [x] Keep supervisor notifications and UI delivery bounded and non-blocking.
- [x] Implement explicit development reload with old-generation cleanup and approval checks.
- [x] Defer automatic restart. Failed or stopped sessions require an explicit restart and cannot be resurrected by queued invocations.
- [ ] Test on macOS, Linux, and native Windows, including launch failure, hung handshake, blocked stdin/stdout/stderr, crash, and unresponsive shutdown. macOS native and Linux container runs pass; Windows needs CI.
- [x] Keep Windows job-object termination distinct from Unix signals and document native Windows, WSL, and Git Bash executable differences.
- [x] Test rapid start/stop/reload, shutdown during activation, descendant cleanup, and no leaked goroutines or child processes.

## 7. Preferences and experimental enablement

- [x] Persist only host-owned enabled state, approved provenance, grants, supported configuration, and activation preferences.
- [x] Keep live connections, process IDs, callbacks, subscriptions, views, and plugin-private data out of state.json.
- [x] Add optional fields with safe zero values and preserve existing state files.
- [x] Use explicit --plugins and --plugin-views flags instead of introducing a generic feature-flag framework.
- [x] Ensure plugins cannot enable experimental surfaces or approve themselves.
- [x] Do not put credentials or secret configuration values in ordinary state.json.
- [x] Define missing-plugin, unsupported-config, revoked-grant, and downgrade behavior.
- [x] Make incompatible preferences diagnostic rather than destructive to workspaces or panes.
- [x] Test snapshot and restore together, including absent fields, corrupt plugin preferences, removed packages, and provenance changes.
- [x] Expose enablement, approval, granted permissions, and lifecycle status through CLI controls.

## 8. First usable vertical slice: contextual action and notification

- [x] Build the hello reference plugin and synthetic helper peers covering handshake, declared actions, notifications, crashes, and unresponsive shutdown.
- [x] Implement only the capability vocabulary required by this slice.
- [x] Route all host reads and mutations through the UI adapter with buffered replies.
- [x] Include trusted connection identity and resolved scope in internal requests without allowing the peer to spoof them.
- [x] Validate contributions against the manifest and namespace them by plugin/session ownership.
- [x] Reuse menu presentation without changing existing menu.register lifetime or replacement semantics.
- [x] Maintain a separate ownership path for plugin contributions and remove them on disconnect or revocation.
- [x] Invoke plugin actions directly over correlated requests, not best-effort broadcast events.
- [x] Capture only approved invocation context and reject actions targeting resources that closed while activation was pending.
- [x] Show loading, unavailable, canceled, and failed invocation states without blocking rendering. Activation progress, unavailable targets, and failed operations are shown. Distinct user cancellation is not implemented.
- [x] Bound and sanitize labels and notifications, including control characters and display width.
- [x] Keep host menus, settings, and ordinary terminal input working during plugin activity.
- [x] Test action registration, collisions, activation, invocation, notification, denial, timeout, resource closure, crash, reload, and cleanup end to end.
- [x] Verify socket menu registrations still behave as documented.
- [ ] Demonstrate the first slice with plugins disabled and enabled on all supported platforms. macOS and Linux done; Windows needs CI.
- [x] Review the provisional schema after the fixture works, then stabilize only the exercised contract.

## 9. Read-only metadata and scoped subscriptions

- [x] Define semantic workspace/tab/pane DTOs and selected-context queries without exposing live UI structs or state-file representations.
- [x] Define pagination, result limits, truncation, and resource version/lifetime rules.
- [x] Filter fields according to grants rather than returning the full existing status response unconditionally.
- [x] Add scoped workspace/tab/pane lifecycle, selection, title, and busy-state subscriptions only where supported by a clear capability.
- [x] Keep screen content separately grantable and omit terminal titles from metadata/events.
- [x] Filter every event by recipient scope and current grants, including paths and names.
- [x] Use bounded best-effort queues, sequence gaps, retries of changed snapshots, and query-based resynchronization.
- [x] Install subscriptions and capture the initial snapshot on the same UI-loop turn.
- [x] Remove subscriptions on disconnect, resource closure, scope revocation, and reload.
- [x] Never stream raw keyboard input or terminal output through general subscriptions.
- [x] Test cross-workspace isolation, grant revocation, stale IDs, resource reorder/rename, overflow, resync, and reconnect.
- [x] Preserve best-effort non-blocking event publication and existing socket event contracts.

## 10. Expanded commands, notifications, and status contributions

- [x] Define command discovery, target applicability, argument validation, and structured result schemas.
- [x] Add the explicit prefix action ctrl+b P for lifecycle controls, leaving ordinary terminal keys untouched.
- [x] Recheck notification results against current grants and connection generation. Other host actions require explicit brokered calls.
- [x] Define notification severity, expiry, dismissal, action IDs, and rate limits.
- [x] Define status placement, priority, count, text limits, severity, actions, and width-aware truncation.
- [x] Coalesce repeated status updates and prevent notification floods from starving the UI.
- [x] Ensure plugin text cannot inject terminal controls into host rendering.
- [x] Test tiny windows, wide glyphs, combining characters, ANSI resets, stale actions, and disconnect cleanup.

## 11. Approved host actions and pane lifetime

- [x] Agree on a minimal allowlist for workspace/tab/pane selection, creation, closure, screen reads, and input operations.
- [x] Add only operations justified by a concrete plugin, with separate grants and user confirmation where appropriate.
- [x] Route operations through existing model, layout, PTY, and holder lifecycle code rather than duplicating it.
- [x] Limit pane creation to existing harness kinds and normal splits/tabs, without arbitrary argv.
- [x] Implement bounded, non-blocking PTY input before exposing pane.send_text to peers. Pane input is an ordered queue drained by one goroutine, keyboard input is never dropped, automation input is bounded and reports busy.
- [x] Ensure resize requests respect host-owned layout and pass content dimensions inside borders to PTYs.
- [x] Define close/cancel behavior when a pane start is pending.
- [x] Distinguish durable host-owned panes from explicitly temporary plugin resources in ownership and cleanup paths.
- [x] Test normal plugin-created pane survival across plugin crash, TUI quit, and holder reattachment. Durable panes persist through quit like host panes and reattach through the unchanged holder path.
- [x] Test temporary pane cleanup on disconnect, explicit close, failed startup, and TUI quit without detached holder leaks. Disconnect and persisted-state exclusion are tested directly. Quit reuses the existing floating-pane close path.
- [x] Test denied mutations, stale targets, duplicate/retried requests, last-pane/tab rules, and no duplicate split-tree leaves.
- [x] Preserve existing keyboard and socket operation semantics.

## 12. Plugin-owned views, separately gated and deferred

- [x] Reconfirm demand before implementing views. Start with floating views before considering grid-cell integration.
- [x] Use bounded host-rendered plain-text rows rather than arbitrary terminal output.
- [x] Define declared view IDs, connection ownership, open/update/close, focus, resize, and disconnect cleanup.
- [x] Define rendering dimensions, clipping, colors, cursor behavior, Unicode width, frame limits, and redraw rate limits.
- [x] Reject controls and ANSI so views cannot change clipboard, cursor, or terminal modes.
- [x] Keep placement, borders, z-order, mouse hit testing, menus, settings, and footer under host ownership.
- [x] Require ui.view.input and deliver normalized focused content input without exposing Bubble Tea objects.
- [x] Give host modals, Escape, the prefix, and frame close controls precedence over view input.
- [x] Specify paste, mouse capture, wheel, drag, keyboard modifiers, and focus restoration.
- [x] Keep views out of terminal pane lists, holder sessions, and persisted terminal split trees.
- [x] For grid views, design a separate layout representation or explicit node typing while preserving the invariant that each persistent terminal pane appears exactly once. Docked views reserve an edge strip outside the split tree instead of adding node types.
- [x] Define behavior for missing plugins, zero-sized content, stale frames, and view movement between frame types.
- [x] Test focus stacking, obscured views, modal input isolation, narrow windows, wide/combining glyphs, malicious control sequences, redraw floods, and crash cleanup.

## 13. Private storage and plugin configuration

- [x] Reconfirm whether host-mediated storage is necessary rather than a documented plugin data directory.
- [x] Define user and workspace storage namespaces, path safety, symlink policy, key/value or file semantics, and platform locations.
- [x] State that namespacing is a host API contract, not OS-level isolation from trusted same-user executables.
- [x] Keep plugin-owned schemas and migrations outside core workspace state.
- [x] Define 64-key/1-MiB quotas, 32-KiB values, replacement writes, cancellation, and structured storage errors.
- [x] Retain private data on disable/removal, namespaced by stable plugin ID.
- [x] Serialize independent instances with Unix flock or Windows byte-range locks.
- [x] Avoid persisting sensitive configuration in plain core state and document any supported secret-handling boundary.
- [x] Test traversal, malformed keys, symlinks, quota exhaustion, failed writes, migration, workspace isolation, and concurrent access.

## 14. Workspace activation and pull-based providers

- [x] Require concrete integrations before selecting provider types such as search, diagnostics, workspace discovery, or completions.
- [x] Define declarative workspace activation markers and matching rules without executable probes.
- [x] Limit filesystem scanning and handle inaccessible paths without stalling the UI.
- [x] Do not let opening an untrusted repository silently approve or execute its plugins.
- [x] Define provider query, scope, limits, deadline, cancellation, and typed result schemas.
- [x] Discard stale results after query changes, resource closure, revocation, or plugin reload.
- [x] Keep provider rendering host-owned unless an explicitly granted view is used.
- [x] Test workspace switching, activation races, large result sets, cancellation, denial, and unavailable providers.

## 15. Management and operational diagnostics

- [x] Provide inspect, enable, disable, approve/revoke, start, stop, restart, and development reload controls.
- [x] Show package provenance, plugin version, protocol version, contributions, requested/granted capabilities, scopes, lifecycle state, and sanitized last failure.
- [x] Show duplicate/shadowed, invalid, missing, incompatible, and blocked packages without launching them. plugins doctor reports inventory diagnostics and blocked approvals; the running instance reports blocked registrations.
- [x] Display enabled experimental surfaces and restart counts if automatic restart exists. No automatic restart exists, so no restart counter is reported. Flags are visible in plugins.status errors and the settings section.
- [x] Keep lifecycle menus and socket controls usable when peers hang or disconnect.
- [x] Add equivalent settings UI only after core CLI lifecycle controls are usable.
- [x] Define explicit local package installation/removal behavior if a managed install command is added. Avoid automatic downloads and install hooks.
- [x] Add tested plugins.status and plugins.control methods without exposing approvals or unrestricted peer access.
- [x] Bound diagnostic history and prevent accidental exposure of terminal text, environment values, or private configuration.

## 16. Documentation and compatibility

- [x] Update README with implemented behavior, enablement, trust, lifecycle, controls, and limitations.
- [x] Document that execution approval is trust in executable code and that grants are not sandboxing.
- [x] Document discovery precedence, provenance approval, upgrades, and project-local trust.
- [x] Publish the manifest and wire reference with examples and explicit limits.
- [x] Document cancellation, timeout, side-effect, event loss/resync, and disconnect semantics.
- [x] Document host-owned panes, holder persistence, and the rejection of temporary peer-created panes.
- [x] Document approval/private-storage locations, permissions, retention, and secret-handling limitations.
- [x] Supply the standard-library-only hello peer and language-neutral NDJSON authoring documentation.
- [x] Add a protocol compatibility matrix and make any zot compatibility claims precise and tested. No zot compatibility is claimed.
- [x] Keep JSON examples synchronized with protocol types and fixtures.
- [x] Update contributor guidance for fixture plugins and platform tests if needed. Not needed: fixtures are Go helper processes described in the platform reference.
- [x] Document feature-flag stabilization or migration without silently overriding explicit user choices. Flags stay opt-in; stabilization requires a maintainer decision.

## 17. Validation required for every implementation increment

- [x] Check git status and preserve unrelated changes before each increment.
- [x] Add focused tests for success, rejection, boundaries, cancellation, failure, and cleanup in the owning package.
- [x] Use synthetic data, temporary directories, controlled environments, and cleanup hooks.
- [x] Prefer channels, hooks, or polling with deadlines over fixed sleeps.
- [x] Use Go helper processes where practical rather than requiring third-party CLIs, real agents, external networks, or a particular shell.
- [x] Run focused package tests during implementation.
- [x] Run gofmt on changed Go files only.
- [x] Run go test ./... and go vet ./... for each Go increment.
- [x] Run go test -race ./... for plugin process, protocol, socket, lifecycle, and broader runtime changes.
- [ ] Run CI tests on macOS, Linux, and native Windows, not only cross-compilation. Local macOS and Linux (container) race suites pass; Windows pending CI.
- [ ] Verify native Windows, WSL, and Git Bash launch/documentation distinctions where applicable. Documented and cross-compiled only.
- [x] Inspect git diff --check, git status --short --branch, and the complete diff before reporting completion.
- [x] Keep README aligned with every newly observable behavior.
- [x] Report failures and skipped validation explicitly and do not mark incomplete acceptance criteria as done.
- [x] Do not commit, create branches, push, or open pull requests without explicit user authorization.

## 18. Release-readiness acceptance checks

- [x] A fresh install starts no plugins and incurs no material terminal/startup regression.
- [x] Existing harnesses, socket clients, state files, pane layouts, and holder sessions remain compatible.
- [x] An explicitly approved reference plugin can activate lazily, contribute an action, receive scoped context, and show a notification.
- [x] Denied operations remain denied through direct requests, returned actions, subscriptions, reloads, and stale responses.
- [x] Replacing a package or shadowing its ID cannot silently inherit execution approval or privileges.
- [x] Hung or noisy plugins cannot block the update loop, exhaust unbounded queues, or inject uncontrolled terminal sequences through host-rendered surfaces.
- [x] Plugin shutdown, crash, and reload leave no managed child processes, pending requests, stale menus, subscriptions, or temporary holder sessions.
- [x] Plugin failure does not kill ordinary host-owned panes or corrupt split trees and persisted state.
- [x] Logs and diagnostics avoid sensitive host payloads and have bounded retention.
- [ ] Resource, concurrency, and cross-platform budgets are measured against the agreed baseline. macOS baseline recorded and Linux suite passes; Windows measurements need CI.
- [x] Experimental flags remain opt-in until maintainers explicitly approve stabilization.

## Explicitly outside the initial implementation

- [x] Keep embedded Go plugins, WASM runtimes, remote plugin services, marketplaces, signing infrastructure, and automatic downloads out of scope unless separately approved.
- [x] Keep durable event replay out of scope in favor of scoped snapshot/resync.
- [x] Keep direct holder access, arbitrary split-tree mutation, fake terminal panes, and unrestricted host-screen rendering outside the plugin contract.
- [x] Add socket/named-pipe plugin transports only for a demonstrated independently managed or multi-instance service requirement, reusing the same protocol and grant broker.
