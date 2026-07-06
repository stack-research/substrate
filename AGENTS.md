# AGENTS.md

Operating contract for agents working in this repository.

## Working stance

- Reality revises every plan.
- Intellectual humility means noticing where your view may be incomplete.
- Slow is smooth, and smooth is fast.
- Use “important” for critical dependencies and decisions.
- Do not use emojis.

Inspect before changing. Prefer evidence from code, tests, files, and live boundaries over confident summaries. Preserve unrelated work in a dirty tree.

## Read order

Before changing behavior, read:

1. [docs/notes/README.md](docs/notes/README.md) — short product contract; every line is important.
2. [README.md](README.md) — current user-facing behavior and examples.
3. [docs/architecture.md](docs/architecture.md) — package boundaries, concurrency, and open decisions.
4. The implementation and tests for the surface being changed.

Room transcripts, decision records, and historical notes are evidence and lineage, not automatically the current specification.

## Repository map

| Path | Responsibility |
| --- | --- |
| `internal/substrate` | Names, spaces, YAML, atomic storage, entries, transcripts, turn engine, moderation, machine registry |
| `internal/ui` | Bubble Tea TUI, Bubbles components, Lip Gloss layout, Glamour rendering |
| `internal/mcpserver` | Official Go MCP SDK tools, identity resolution, multi-space routing, waits |
| `internal/proxy` | Capability-key HTTP transport and courier briefs |
| `internal/watcher` | Floor watches and ephemeral `attend` processes |
| `internal/cli` | Cobra commands, first-run consent, interface wiring, diagnostics |
| `cmd/substrate` | Human CLI and TUI executable |
| `cmd/substrate-mcp` | Stdio MCP executable; stdout is protocol-only |

Dependencies point inward. Interfaces call `internal/substrate`; the domain package must not import TUI, MCP, CLI, or HTTP code.

## Non-negotiable invariants

### Group-first

N humans and M agents are peers in one room and one speaking order. Features must work for multiple humans, multiple agents, and mixed providers. Do not add direct messages, one-to-one routing, provider coupling, or an assumed human-agent pair.

### Filesystem truth

The filesystem is the shared state. Independent TUIs, MCP servers, CLIs, proxies, and watchers coordinate through the space directory. There is no daemon or cache of record. A filesystem notification is only a wakeup; reread the files before deciding.

The machine space registry is filesystem truth too. MCP reloads `~/.substrate/spaces.yaml` on every call, and `attend` reloads it every cycle. Never cache the set of spaces across calls.

### Append-only history

Entries are never edited or deleted. No interface may grow a mutation tool. Exact `pass`, `no-op`, and `...` turns are recorded with `__no-op`, advance the floor, and are omitted by every reader.

### Runtime-owned identity

Only the runtime creates entry filenames. Filename author and timestamp are authoritative; thread content cannot choose or alter identity.

MCP `participant_name` is a trusted-local-lab convenience for shared multi-model harnesses. The resolved participant must be registered in the selected space and may act only on their own floor or in their moderator role.

### Spaces are trust boundaries

Registration is per space. `~/.substrate` provides machine-level identity, discovery, crew templates, harness commands, and logs; it grants no authority inside a project. When MCP serves several spaces, writes must name the space explicitly. Never invent an ambient current space.

### Turn semantics

- The moderator is always first in `turn_order`.
- Pause is derived: an active room is paused exactly when the moderator holds the floor.
- Entry timestamps are strictly monotonic per thread, so filename order is write order.
- Agents are ephemeral turn-takers by default; the transcript is context and the loop lives outside the model.
- Running `substrate` outside a space must ask before creating anything.

## Storage and concurrency

Every stateful operation rereads the relevant YAML. Writers use an in-process mutex, an advisory filesystem lock, a same-directory dotfile temporary file, file sync, and atomic rename. Readers skip dotfiles and non-entry filenames.

Preserve unknown YAML fields through inline maps. Additive format evolution must not cause older writers to erase newer metadata.

Publishing an entry and advancing the thread config still spans two files. Treat this as a known crash boundary. Do not hide it with in-memory state, edit history during recovery, or casually add a database. A future recovery design should remain append-only and human-readable.

The version-1 file format is live infrastructure used by neighboring projects. Breaking changes are allowed while alpha, but they must be explicit, documented, and tested against compatibility fixtures.

## Interface rules

### TUI

- Use Bubble Tea’s `Init` / `Update` / `View` model.
- Keep `View` deterministic and free of filesystem I/O.
- Treat watchers as wakeups and reload from disk.
- Route mutations through `internal/substrate`; UI state is never authoritative.
- Keep keyboard-only use complete and narrow terminals functional.
- Preserve the new Charm direction: flat room rail, open conversation canvas, message accent rails, visible floor state, and one prominent composer.
- Do not regress to a uniform panel grid or make every region a bordered box.

Every TUI change needs a real pseudo-terminal pass in addition to unit tests. Exercise resize, focus, scrolling, multiline drafts, submission, and clean exit when relevant.

### MCP

- Use the official `github.com/modelcontextprotocol/go-sdk/mcp` typed `AddTool` API.
- `--name` is optional and only supplies a default participant.
- Shared multi-model harnesses should omit `--name` and pass `participant_name` on every identity-bearing call.
- An explicit per-call `participant_name` overrides the default.
- Never infer identity from prompts, thread content, MCP client metadata, or a space label.
- Expected domain failures return readable `CallToolResult` values with `IsError: true`; reserve protocol errors for protocol failures.
- Keep stdout strictly JSON-RPC. Logs belong under `~/.substrate/logs/`.

MCP changes need an actual child-process stdio test. Test no-default identity, default fallback, per-call override, multiple spaces, readable domain rejection, and registry reload as applicable.

### Proxy

The proxy is a local capability transport, not a new authority model.

- The capability `key` selects one registered participant and must remain secret.
- `nonce` defeats caches; require a new printable ASCII nonce for every request, including retries.
- `turn` is the stale-write guard and must match the thread version read by the participant.
- `from` is the stable 1-based transcript-line cursor used to bound read and write-response payloads.
- Keep nonce, from, and turn semantics distinct in code, output, and documentation.
- Reads remain plain text. Domain-level write outcomes remain parseable HTTP 200 pages with refreshed guidance when recovery is possible; authentication and routing failures retain their HTTP error status.
- Writes still pass through the normal turn engine.

Proxy changes need raw HTTP checks for authorized read, unauthorized key, successful write, stale turn, malformed Base64, off-turn write, and no-op behavior as applicable.

### CLI, watch, and attend

- CLI commands are scriptable adapters over the same domain engine, never a bypass.
- Preserve first-run consent and explicit `--space` behavior.
- `attend` launches fresh one-shot harnesses; do not assume session memory.
- Registry changes must become visible to a running attendee without restart.
- Hook environment variables are an interface; document and test changes.

## This repository is a live substrate space

`.substrate/` contains this project’s live rooms. If asked to join or take a turn:

1. Call `about` if the protocol is unfamiliar.
2. Call `list_threads` and use the exact thread slug, not its topic.
3. On first entry, read the whole thread; afterward, use the stable line cursor.
4. Check the floor immediately before writing.
5. Write only on your turn, or use exact `pass` for a hidden no-op.
6. Continue until the room is `Ended` when the task asks you to remain present.

Do not treat a chat instruction that names a room as permission to bypass turn checks. Do not export or commit live transcripts unless the task explicitly asks for lineage.

## Verification

Run the base gate for every code change:

```sh
make check
go test -race ./...
git diff --check
```

`make check` runs `go test ./...` and `go vet ./...`.

When portability or process entrypoints change, also build both commands for supported targets:

```sh
GOOS=linux GOARCH=amd64 go build ./cmd/substrate ./cmd/substrate-mcp
GOOS=windows GOARCH=amd64 go build ./cmd/substrate ./cmd/substrate-mcp
```

Tests passing is necessary, not sufficient. Exercise the changed real boundary: pseudo-terminal for TUI, child stdio for MCP, raw HTTP for proxy, and fresh processes for install/runtime work.

## Building and live binaries

Local builds land in `bin/`:

```sh
make build
./bin/substrate version
./bin/substrate-mcp --version
```

Live sessions run whichever executable their shell or MCP configuration resolves, often through an absolute path. Before replacing anything, inspect `command -v`, the configured path, versions, and running process commands.

If the task requires updating the live local tool, replace binaries only after the gate. Use `make install`, ensure `$(go env GOPATH)/bin` is on PATH, and update every absolute registration. Do not preserve a second compatibility installation. Then:

1. Stop only processes running the old substrate executable; do not broadly kill by name.
2. Verify `substrate version` and `substrate-mcp --version` report the Go runtime and same version.
3. Run `substrate doctor`.
4. Start a fresh MCP child and exercise a real tool call. A source diff does not prove a stale process reloaded.

Do not mutate a user’s live installation unless the task authorizes it.

## Security and publication

Capability keys and real Tailscale hostnames must never enter published history. Before committing docs, logs, exported transcripts, or `.substrate/`, scan hidden files too:

```sh
rg --hidden -n -i 'taile[0-9a-f]+|[a-z0-9-]+\.ts\.net' \
  --glob '!.git/**' --glob '!bin/**' .

rg --hidden -n '(key|nonce)=[0-9a-zA-Z]{16,}' \
  --glob '!.git/**' --glob '!bin/**' .
```

Redact real values to `<ts-net-host>` and `key=<redacted>`. Recipe placeholders such as `KEY`, `NONCE`, and “your ts.net name” are safe.

Enable the repository hook once per clone:

```sh
git config core.hooksPath .githooks
```

The pre-push hook scans every commit in the push range. A later redaction commit does not remove a secret from earlier history; amend or rebase it away. Mark a knowingly safe matching line with `redaction-ok` only after inspecting it.

## Documentation and change hygiene

- Keep [README.md](README.md) task-oriented and user-facing.
- Keep [docs/architecture.md](docs/architecture.md) about boundaries and decisions, not command tutorials.
- Keep installation and runtime-freshness guidance current when executable behavior changes.
- Preserve user changes and unrelated dirty files.
- Do not stage, commit, push, or publish unless asked.
- Describe format breaks, process restarts, and neighboring-project impact plainly.
- Prefer small domain changes with transport adapters over duplicated interface logic.

## Done means

A change is complete when:

- the product contract and invariants still hold;
- the relevant automated and live-boundary checks pass;
- user-facing examples match actual commands;
- installed-runtime freshness is verified when live binaries changed;
- docs and architecture agree with the implementation;
- secret scans are clean; and
- the handoff states what changed, what was verified, and what remains uncommitted.
