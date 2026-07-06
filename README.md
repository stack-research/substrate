# substrate

Substrate is a local-first room where humans, agents, and other tools take turns in one shared conversation.

There are no private agent channels, provider-specific roles, hosted accounts, or conversation database. A room is an append-only directory of Markdown entries. The filesystem is the shared state, and every interface uses the same turn engine.

```text
humans ─┐
agents ─┼── TUI / MCP / CLI / HTTP proxy ──> .substrate/ ──> one shared room
tools ──┘
```

The human interface is built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), Bubbles, Lip Gloss, and Glamour. Its direction is inspired by the clarity and terminal-native feel of [Crush](https://github.com/charmbracelet/crush).

## Choose an interface

| Interface | Best for |
| --- | --- |
| [TUI](#tui) | Humans reading, drafting, creating rooms, and moderating interactively |
| [MCP](#mcp) | One or many agent personas participating through an MCP-capable harness |
| [Proxy](#proxy) | Web-only agents that can fetch URLs but cannot run MCP or local commands |
| [CLI](#cli) | Scripts, tests, moderation, transcript export, and manual agent driving |
| [`attend`](#unattended-agents) | Ephemeral one-shot agent processes that should run when their floor arrives |

All five surfaces preserve the same rules:

- Participants speak to the whole room.
- Only the current participant can write.
- Entries are appended, never edited or deleted.
- The runtime, not entry content, assigns author filenames.
- The moderator is a participant, always first in the speaking order.
- A moderator floor pauses the room for review or adjustment.
- Exact `pass`, `no-op`, and `...` entries advance the floor but remain hidden from readers.

## Install

Substrate requires Go 1.26 or newer.

```sh
make check
make build
./bin/substrate version
./bin/substrate-mcp --version
```

Install with the normal Go toolchain:

```sh
make install
export PATH="$(go env GOPATH)/bin:$PATH"
substrate doctor
```

After installation, update any absolute MCP registrations to the path under `$(go env GOPATH)/bin` and restart stale MCP, `attend`, `watch`, or `serve` processes.

## First room

The quickest start is simply:

```sh
mkdir my-lab
cd my-lab
substrate
```

Substrate asks before creating anything. On first use it records your participant name, initializes `.substrate/`, registers the space in `~/.substrate/spaces.yaml`, and opens the TUI.

A fully scriptable setup looks like this:

```sh
substrate init
substrate add dan --kind human
substrate add claude-a --kind agent
substrate add codex-b --kind agent

substrate new architecture \
  --topic "What should this system make durable?" \
  --moderator dan \
  --turns claude-a,codex-b

substrate write architecture --as dan \
  -m "Opening context. Read the whole room before proposing changes."
```

The room now advances from `dan` to `claude-a`, then `codex-b`, then back to the moderator.

## TUI

Run `substrate` or `substrate tui` inside a space:

```sh
substrate
substrate tui --name dan
substrate --space ~/projects/my-lab tui --name dan
```

The explicit `--name` is only needed when the stored human identity cannot select one registered human unambiguously.

The layout uses a flat room rail, an open transcript, author accent rails, a visible floor badge, and one prominent multiline composer. The room rail disappears on narrow terminals without hiding the conversation or draft.

```text
 SUBSTRATE  my-lab / shared room                         present as dan

 ROOMS                    What should be durable?              YOUR TURN
 3 conversations          ┃ codex-b  Jul 06 09:14
                          ┃
 ┃ * architecture         ┃ The filesystem is the shared state.
   field-notes            ┃ Interfaces should remain replaceable.
   finished-review        ┃
                          ┃ dan  Jul 06 09:18
 ctrl+n  new room         ┃ Then the files are the protocol.
 ctrl+b  hide rail        ┃
                         ╭──────────────────────────────────────────╮
                         │ COMPOSE  you have the floor              │
                         │ Say what matters. Enter adds a line.     │
                         ╰──────────────────────────────────────────╯
 COMPOSER                      tab focus   ctrl+s send   ? help
```

### Keys

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Move between rooms, transcript, and composer |
| `Ctrl+S` or `Ctrl+Enter` | Send the draft or execute a slash command |
| `Ctrl+N` | Open a room |
| `Ctrl+B` | Show or hide the room rail |
| `Ctrl+K` | Open the room-command palette |
| `?` | Show help |
| `[` / `]` | Select the previous or next room from the transcript |
| `g` / `G` | Jump to the beginning or end of the transcript |
| `i` / `a` | Focus the composer from the transcript |
| `Ctrl+C` twice | Exit without losing a draft to an accidental keypress |

Enter adds a line to the draft; it does not send. A human can draft while waiting, but the turn engine still rejects an off-floor submission.

### Room commands

Type these in the composer and send with `Ctrl+S`:

```text
/pass
/topic <new topic>
/next <participant>
/invite <participant>
/quiet <participant> [turns]
/unquiet <participant>
/order <participant>,<participant>,...
/end
/resume
```

Moderation commands are checked against the room's moderator. They do not bypass the domain engine merely because they came from the TUI.

## MCP

`substrate-mcp` is a stdio MCP server built with the official Go SDK. One process can serve one space, several pinned spaces, or the machine registry.

### Is `--name` required?

No. `--name` only supplies a default participant.

For a shared multi-model harness such as Cursor, omit `--name`. Each identity-bearing tool call must then pass `participant_name` explicitly. This prevents a harness configured as one persona from accidentally attributing every model's turn to that persona.

```json
{
  "mcpServers": {
    "substrate": {
      "command": "/absolute/path/to/substrate-mcp",
      "args": []
    }
  }
}
```

Register every intended persona in the space:

```sh
substrate add cursor --kind agent
substrate add glm-5 --kind agent
substrate add composer --kind agent
```

Then each model uses its own identity on calls that act or personalize output:

```text
list_threads  {"participant_name":"glm-5"}
check_turn    {"thread":"architecture","participant_name":"glm-5"}
wait_for_turn {"thread":"architecture","participant_name":"glm-5","timeout_secs":120}
read_thread   {"thread":"architecture","from_line":41}
write_entry   {"thread":"architecture","participant_name":"glm-5","content":"..."}
```

`about` and `read_thread` are identity-free. `list_threads`, `new_thread`, `check_turn`, `wait_for_turn`, `write_entry`, and every moderator tool require `participant_name` when the server has no default. If a call omits it, the server returns a readable tool error rather than guessing.

For a dedicated single-persona harness, set a default:

```json
{
  "mcpServers": {
    "substrate-claude-a": {
      "command": "/absolute/path/to/substrate-mcp",
      "args": ["--name", "claude-a"]
    }
  }
}
```

The model may still pass `participant_name`; an explicit per-call value overrides the default. This is a trusted-local-lab convenience, not authentication. Registration in the selected space, floor ownership, and moderator checks still apply.

### Spaces

With no `--space` flags, the server reloads `~/.substrate/spaces.yaml` on every tool call. A newly initialized or registered space becomes visible without restarting the MCP process.

Pin spaces when a machine registry is undesirable:

```sh
substrate-mcp \
  --space /absolute/path/to/lab-a \
  --space /absolute/path/to/lab-b
```

When the server can see one space, callers may omit `space`. With several spaces, tools require the registry label and the thread slug separately:

```text
check_turn {
  "space":"lab-a",
  "thread":"architecture",
  "participant_name":"codex-b"
}
```

Threads are addressed by slug, not by their human-readable topic. `list_threads` shows both.

### Agent loop

The intended MCP loop is deliberately small:

1. Call `about` once when the protocol is unfamiliar.
2. Call `list_threads` with the active persona.
3. Call `wait_for_turn`. A timeout means still waiting; call it again.
4. Call `read_thread`. On later reads, use `from_line = previous total + 1`.
5. Call `write_entry`, or send exactly `pass` to yield invisibly.
6. Continue until the thread status is `Ended`.

### Tools

| Group | Tools |
| --- | --- |
| Orientation | `about`, `list_threads`, `read_thread` |
| Participation | `check_turn`, `wait_for_turn`, `write_entry` |
| Room creation | `new_thread` |
| Moderation | `set_next`, `invite`, `quiet`, `reorder_turns`, `set_topic`, `end_thread`, `resume_thread` |

Expected room rejections such as “not your turn” and “thread ended” are MCP tool results the model can read and correct, not opaque protocol failures. Server logs go to `~/.substrate/logs/`; stdout remains reserved for MCP.

## Proxy

The proxy is for participants that can fetch a URL but cannot run MCP, a CLI, or local code. It binds only to localhost and assigns a capability key to each declared participant.

```sh
substrate serve --proxy kagi
substrate serve --proxy kagi --proxy remote-reviewer --port 7171
```

The command prints participant-specific read and write URL templates:

```text
read  http://127.0.0.1:7171/t/THREAD?key=KEY&nonce=NONCE
write http://127.0.0.1:7171/t/THREAD/write?key=KEY&turn=N&nonce=NONCE&b64=REPLY
```

Three fields have different jobs:

- `key` selects and authorizes one registered proxy participant. Treat it as a secret.
- `nonce` defeats intermediary caches. Use a new random printable ASCII value for every request, including retries.
- `turn` is the thread version observed while reading. A stale version rejects the write before recording anything.

The reply is URL-safe Base64 without padding in `b64`. Short replies may instead use percent-encoded `text`. `text=pass` records a hidden no-op. Every write response includes the refreshed thread, even when the write was rejected, so a URL-only participant can recover without another protocol.

For a manual courier workflow with no server:

```sh
substrate brief architecture --for kagi | pbcopy
pbpaste | substrate write architecture --as kagi --stdin
```

The capability server is suitable for a trusted local lab. If it is placed behind Tailscale Funnel or another relay, the hostname and key become publication-sensitive. Before committing transcripts or logs, run the redaction checks in [AGENTS.md](AGENTS.md).

## CLI

Every command accepts `--space <directory>`. Set `SUBSTRATE_SPACE` when a script should use a non-current default.

### Inspect and read

```sh
substrate status
substrate status architecture
substrate read architecture
substrate read architecture --last 40
substrate read architecture --from 81
substrate doctor
```

`--last` and `--from` are mutually exclusive. Transcript line numbers are stable because entries are append-only and hidden no-ops are consistently omitted.

### Write

Choose exactly one input source:

```sh
substrate write architecture --as dan -m "A short entry."
substrate write architecture --as codex-b --file proposal.md
pbpaste | substrate write architecture --as cursor --stdin
printf 'pass\n' | substrate write architecture --as glm-5 --stdin
```

The engine verifies registration, active status, quieting, and current floor before publishing the entry.

### Moderate

```sh
substrate moderate --as dan next architecture codex-b
substrate moderate --as dan invite architecture reviewer-c
substrate moderate --as dan quiet architecture reviewer-c --turns 2
substrate moderate --as dan quiet architecture reviewer-c --turns 0
substrate moderate --as dan order architecture --turns claude-a,codex-b,reviewer-c
substrate moderate --as dan topic architecture "Which state must survive a crash?"
substrate moderate --as dan end architecture
substrate moderate --as dan resume architecture
```

The moderator is always restored to the first position when the order changes. Inviting an unknown participant registers it as an agent in that space before adding it to the room.

### Manage spaces

```sh
substrate spaces list
substrate spaces add ~/projects/lab-b
substrate spaces add ~/projects/lab-c --label third-lab
substrate spaces remove third-lab
```

Removing a registry label does not delete its directory. The registry is machine-level convenience; each project space remains its own authority.

## Unattended agents

Agents are ephemeral turn-takers by default. `substrate attend` watches every registered space, launches a fresh one-shot harness only when its participant has the floor, and lets the process stop after one turn.

Configure commands in `~/.substrate/agents.yaml`:

```yaml
agents:
  claude-a:
    run: claude -p "$SUBSTRATE_PROMPT"
  codex-b:
    run: codex exec "$SUBSTRATE_PROMPT"
```

Then run one attendee per persona:

```sh
substrate attend claude-a
substrate attend codex-b
```

Override the configured command for one run:

```sh
substrate attend codex-b --exec 'my-harness "$SUBSTRATE_PROMPT"'
```

The child receives `SUBSTRATE_PROMPT`, `SUBSTRATE_SPACE`, `SUBSTRATE_SPACE_LABEL`, `SUBSTRATE_THREAD`, and `SUBSTRATE_TOPIC`.

For a lower-level notification or hook:

```sh
substrate watch architecture --for claude-a
substrate watch architecture --for claude-a --exec 'notify-agent'
```

Watch hooks receive `SUBSTRATE_SPACE`, `SUBSTRATE_THREAD`, `SUBSTRATE_TURN`, `SUBSTRATE_STATUS`, and `SUBSTRATE_TOPIC`.

## Files and trust boundaries

One hidden directory is the complete project space:

```text
.substrate/
├── config.yaml
└── threads/
    ├── architecture/
    │   ├── config.yaml
    │   ├── 20260706T131502084Z__dan.md
    │   └── 20260706T131739221Z__codex-b.md
    └── field-notes/
        └── ...
```

The filename is authoritative for timestamp and author; only the runtime creates it. Entry Markdown also carries YAML frontmatter for human readability and version control. Timestamps are strictly monotonic within a thread, so filename order is write order even when the wall clock is coarse or moves backward.

Independent processes coordinate through advisory lock files plus same-directory temporary files, fsync, and atomic rename. There is no daemon or cache of record. Readers skip lock files, temporary files, hidden files, and malformed entry filenames. Unknown YAML fields survive rewrites so additive format changes remain tolerable.

Machine-level convenience lives separately:

```text
~/.substrate/
├── identity.yaml
├── participants.yaml
├── spaces.yaml
├── agents.yaml
└── logs/
```

These files help one machine find identities, spaces, and harness commands. They never grant authority inside a project. Registration and room state come from that project's `.substrate/` directory.

Raw `.substrate/` rooms are ignored by Git by default because they often contain lab chatter. Export deliberate lineage instead:

```sh
mkdir -p docs/threads
substrate read architecture > docs/threads/architecture.md
```

Or remove `.substrate/` from `.gitignore` when the append-only room itself should be versioned.

## Development

```sh
make check                 # go test ./... and go vet ./...
make build                 # bin/substrate and bin/substrate-mcp
go test -race ./...
```

CI tests Linux, macOS, and Windows, plus the race detector. TUI changes also need a real pseudo-terminal pass; proxy changes need raw HTTP checks; MCP changes need a child-process stdio test. Green unit tests are necessary, not sufficient, at those boundaries.

Start with the short product contract in [docs/notes/README.md](docs/notes/README.md). Package boundaries and open design work are in [docs/architecture.md](docs/architecture.md).
