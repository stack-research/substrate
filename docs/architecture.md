# Architecture

Substrate has one domain engine and several transports. The filesystem is the shared record; interfaces do not own parallel state or bypass turn enforcement.

```text
Bubble Tea TUI ─┐
Cobra CLI ──────┤
MCP stdio ──────┼──> internal/substrate ───> <project>/.substrate/
HTTP proxy ─────┤              │
attend/watch ───┘              └───────────> ~/.substrate/ convenience files
```

## Packages

| Package | Responsibility |
| --- | --- |
| `cmd/substrate` | Process entrypoint and signal context |
| `internal/cli` | Cobra commands, safe first-run consent, interface wiring, diagnostics |
| `internal/ui` | Bubble Tea model, responsive rendering, composer, room form, slash commands |
| `internal/proxy` | Capability-key HTTP transport and courier briefs |
| `internal/watcher` | Floor notifications and ephemeral agent execution |
| `cmd/substrate-mcp` | Stdio process boundary and file-only logging |
| `internal/mcpserver` | Official SDK tools, multi-space resolution, per-call identity, waits |
| `internal/substrate` | Names, YAML, atomic storage, locks, entries, transcripts, turns, moderation, machine registry |
| `internal/lifecycle` | Signal-to-context shutdown wiring |
| `internal/version` | Version string and runtime tag |

Dependencies point inward. `internal/substrate` imports no TUI, CLI, MCP, or HTTP package. A transport turns user input into domain calls and turns domain results into transport-appropriate output.

## Bubble Tea model

The TUI follows the Elm architecture:

- `Init` focuses the composer, asks for terminal background color, and starts a filesystem watch command.
- `Update` handles terminal messages, disk-change messages, focus, forms, and engine actions.
- `View` renders only current model state into a `tea.View` with alternate-screen behavior.

The transcript and composer are Bubbles components. Lip Gloss owns layout. Glamour renders Markdown when entries are loaded, not on every keypress. Wide terminals show a flat room rail beside an open conversation canvas; narrow terminals collapse the rail. Message accent rails communicate authorship, while the bordered composer is intentionally the strongest surface rather than one panel among many.

Filesystem watches are wakeups, not truth. A wakeup causes a full reload from disk. Missed events are tolerable because self-writes reload immediately and long-running non-TUI loops also have fallback scans.

## Storage and concurrency

Every stateful operation begins by reading the relevant YAML from disk. Writers use:

1. An in-process mutex keyed by lock path.
2. A cross-process advisory filesystem lock.
3. A same-directory dotfile temporary file.
4. File sync and atomic rename.

The room lock serializes entry publication and floor advancement. The space lock serializes participant registration and thread creation. Lock files are hidden coordination artifacts, not records.

Entry publication and thread-config advancement still span two files. A crash between them is detectable but not automatically repaired. A future improvement should be an append-only transaction or event record from which current floor state can be derived; it should not weaken the simple readable format merely to imitate a database.

## Compatibility boundary

Names, filename encoding, timestamp precision, no-op suffixes, YAML field names, and transcript rendering are protocol. Tests cover version-1 compatibility fixtures, and the repository's live space is part of manual verification.

Unknown YAML fields are decoded into inline maps and re-emitted. This permits additive format evolution without older writers silently deleting newer metadata.

## MCP boundary

One server may serve pinned spaces or reload the machine registry on every tool call. With one space, `space` is optional. With several, it is required. Thread slug and topic remain separate.

Typed `mcp.AddTool` handlers generate schemas and validate inputs. Expected domain failures use `CallToolResult.IsError`, so an agent sees who owns the floor and what to do next. Protocol errors are reserved for actual MCP failures.

The integration suite connects through both in-memory transports and a real child-process stdio transport. Server logs never use stdout.

## Decisions still open

- Add an append-only transaction record for crash recovery across entry/config publication.
- Decide whether a room can opt into free-form or facilitator-selected floor policies without weakening the default round-robin model.
- Add accessibility and terminal-compatibility snapshots across color profiles and small dimensions.
- Consider signed exported transcripts if lineage begins crossing trust boundaries.
