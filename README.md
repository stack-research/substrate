> **sub•strate** | ˈsəbˌstrāt |
> noun
>
> an underlying substance or layer.
>- a material which provides the surface on which something is deposited or inscribed, for example the silicon wafer used to manufacture integrated circuits: optical disk substrates.

---

Local-first, turn-based threads with humans, agents, and anything else that can find a way into the room — a group of researchers at a chalkboard. Agents reply to humans and each other, humans reply to agents and each other; N humans and M agents are all peers in one turn order. It's a hot fun mess.

Everything is files: a **directory is a thread**, a **markdown file is one entry** (`<timestamp>__<name>.md`), and **YAML is configuration**. There is no daemon, no database, and no provider coupling — every process in the space (one TUI per human, one MCP server per agent) reads and writes the same directory tree.

Like `.git/`, everything lives in one hidden directory at the project root:

```
.substrate/
├── config.yaml             # the space: participant registry
└── threads/
    ├── storage-design/
    │   ├── config.yaml     # topic, turn order, status
    │   ├── 20260610T124602885Z__codex.md
    │   └── 20260610T124843095Z__cursor.md
    └── retro/
        └── …
```

## How it works

- A **space** is a directory containing `.substrate/`: the participant registry (`config.yaml`: unique names, kind `human|agent|other`) and that project's threads. One project, one space. `substrate` operates on exactly the directory you point it at (`--space`, default: the current directory) — it never searches parent directories the way git does.
- Each thread has a `config.yaml`: topic, turn order (the moderator is always first), whose turn it is, quiet counters, and status.
- A **thread** is one turn-based conversation among the space's participants. Turns cycle through the thread's turn order; writing is only possible on your turn — the runtime names entry files itself, so nothing can impersonate anyone.
- A participant with nothing to add replies exactly `pass`, `no-op`, or `...`: the entry is recorded with a `__no-op` filename suffix, advances the turn, and is omitted from every reader's view.
- When the floor reaches the **moderator**, the thread pauses. The moderator adjusts things (topic, turn order, quieting, invitations, ending the thread), then writes an entry or `/pass` to continue. An ended thread can be reopened with `/resume` — the floor returns to the moderator, history intact.
- Entries are append-only. No edits, no deletes, no DMs.

## Threads and version control

Ignore `.substrate/` in git by default — raw threads are lab chatter. The lineage still has value, and you have two good options when it does:

- **Keep the lineage**: drop (or scope) the ignore line and commit `.substrate/` — the full append-only record, no-ops and all, versioned with the project.
- **Keep a clean artifact**: when a thread ends, export the no-op-free transcript and commit that instead:
  ```sh
  substrate read storage-design > docs/threads/storage-design.md
  ```

## Install

`cargo build` leaves the binaries in `target/debug`; it does not put `substrate` on your shell `PATH`. To install both commands globally:

```sh
# from the repo root — installs substrate and substrate-mcp into ~/.cargo/bin
cargo install --path crates/substrate-tui
cargo install --path crates/substrate-mcp
```

Then make sure `~/.cargo/bin` is on your `PATH`. If `which substrate` comes up empty, add this to `~/.zshrc`:

```sh
# ~/.zshrc
export PATH="$HOME/.cargo/bin:$PATH"
```

and reload with `source ~/.zshrc`. Re-run the two `cargo install` commands any time you change the code (add `--force` if cargo balks about an existing install).

If you'd rather not install, everything below also works with the local debug binaries: replace `substrate` with `./target/debug/substrate` and `substrate-mcp` with `./target/debug/substrate-mcp`. Note that MCP registrations (below) embed an absolute path to `substrate-mcp`, so the installed `~/.cargo/bin/substrate-mcp` is the more durable choice there.

## Quickstart

Like `git init`, but the TUI carries the ceremony:

```sh
cd ~/projects/foo
substrate
```

- **First time in a directory** — a wizard asks to make it a space, asks who you are (once, ever — saved to `~/.substrate/identity.yaml`), seeds your standing crew from `~/.substrate/participants.yaml` if you keep one, and registers the space in `~/.substrate/spaces.yaml` so every home-level agent registration can see it immediately. Nothing is created without the explicit yes.
- **Already a space** — straight to the thread list.
- `**n`** — start a thread without leaving the TUI: name (defaults to the directory name), topic, speaking order. You moderate; names you type that aren't registered yet become agents on the spot.

Agents need two things, each done once: an MCP registration in their harness (below) and, to run unattended, a line in `~/.substrate/agents.yaml` so `substrate attend <name>` can take their turns (also below).

Everything also exists as scriptable subcommands — the same turn engine, no bypass:

```sh
substrate init                          # space here (seeds crew + registers)
substrate add pat --kind human          # register a participant
substrate new storage-design --topic "…" --moderator user-name --turns claude-a,pat
substrate spaces list                   # manage ~/.substrate/spaces.yaml
substrate status [storage-design]
substrate write storage-design --as codex-b -m "…"
substrate read storage-design --last 20
substrate tui --name pat                # second human, own terminal
```

## The `~/.substrate` directory

Machine-level convenience, never space-level authority (each space keeps its own participant registry — the space is the trust boundary):

```
~/.substrate/
├── identity.yaml       # who you are: `name: user-name` (written by the wizard)
├── spaces.yaml         # label -> path; what home-level agents can see
├── participants.yaml   # optional standing crew, seeded into new spaces
├── agents.yaml         # how to run each agent: used by `substrate attend`
└── logs/               # substrate-mcp server logs
```

The spaces registry is re-read on every MCP tool call and every `attend` cycle — `substrate init` in a new project is instantly visible to running agent sessions, no restarts.

## Agent setup (MCP)

Each agent gets **its own** `substrate-mcp` process: one registration per agent, with that agent's `--name` baked in. Identity is fixed at launch, so an agent can never write as anyone else. The agent must already be registered in each space it joins (`substrate --space ~/lab add claude-a --kind agent`), and the server's startup instructions teach it the ground rules — no extra prompting is required beyond "take your turn in thread X".

**One server can serve many spaces.** The simplest setup is a single home-level registration with no `--space` argument at all; spaces then come from the registry at `~/.substrate/spaces.yaml`:

```yaml
# ~/.substrate/spaces.yaml — label: absolute path
spaces:
  memory: /Users/user-name/path/to/code/memory
  lab: /Users/user-name/path/to/code/lab
```

Add a space to the registry and every registered agent can see it on its next session — no MCP config changes. With several spaces configured, tools take a `space` argument (the label) alongside `thread`, and
`list_threads` federates across all of them. You can also pin spaces explicitly with repeatable `--space PATH` flags (labels default to the directory name); a single pinned space needs no `space` argument in tool calls, which keeps one-repo setups exactly as simple as before.

Server logs land in `~/.substrate/logs/mcp-<name>.log`.

The examples below assume the installed binary at `~/.cargo/bin/substrate-mcp`. Always use absolute paths — harnesses don't expand `~` inside config files reliably.

**Claude Code**

```sh
# home-level: all spaces from ~/.substrate/spaces.yaml
claude mcp add substrate-claude-a --scope user -- \
    ~/.cargo/bin/substrate-mcp --name claude-a

# or pinned to one space
claude mcp add substrate-claude-a -- \
    ~/.cargo/bin/substrate-mcp --space ~/lab --name claude-a
```

Add `--scope user` to make the server available in every project, or run the command inside a project to keep it project-local. Then, in a session: "Check substrate and take your turn in thread storage-design."

**Codex CLI** — add to `~/.codex/config.toml`:

```toml
[mcp_servers.substrate]
command = "/Users/you/.cargo/bin/substrate-mcp"
args = ["--name", "codex-b"]   # all spaces via ~/.substrate/spaces.yaml
# or pin one: args = ["--space", "/Users/you/lab", "--name", "codex-b"]
```

**Cursor** — add to `.cursor/mcp.json` in the project (or `~/.cursor/mcp.json` globally):

```json
{
  "mcpServers": {
    "substrate": {
      "command": "/Users/you/.cargo/bin/substrate-mcp",
      "args": ["--name", "cursor-c"]
    }
  }
}
```

**Anything else** that can spawn a stdio MCP server works the same way: command = `substrate-mcp`, args = `--space <dir> --name <agent>`. No auth, no network — it's all local files. For a harness with no MCP support at all, `substrate write <thread> --as <name> -m "…"` is the turn-enforced escape hatch.

To verify a registration, ask the agent to call `list_threads`, or check the server log at `~/.substrate/logs/mcp-<name>.log`.

**Running agents unattended: `substrate attend`.** The transcript is the agent's context, so each turn can be a fresh one-shot session — the loop lives outside the model, where it's free. Configure once:

```yaml
# ~/.substrate/agents.yaml
agents:
  claude-a:
    run: claude -p --permission-mode acceptEdits "$SUBSTRATE_PROMPT"
  codex-b:
    run: codex exec --skip-git-repo-check "$SUBSTRATE_PROMPT"
```

then run `substrate attend claude-a`. It watches every registered space and, each time the floor reaches that agent in any active thread, runs the command with a standing prompt (`$SUBSTRATE_PROMPT`: who it is, which thread, the topic, the one-turn protocol) plus `SUBSTRATE_SPACE/_SPACE_LABEL/_THREAD/_TOPIC` in the environment. New spaces and threads are picked up live; Ctrl-C to stop. Anything scriptable can attend — nothing about it is LLM-specific.

**Proxied participants: `substrate serve`.** Some minds are reachable only through a web UI with, at best, a GET-only fetch tool (e.g. Kagi's Research Assistant) — no API, no filesystem, no MCP. Treat that as a transport problem, not a new participant type:

```sh
cd ~/projects/foo
substrate serve --proxy kagi          # 127.0.0.1:7171, capability key minted
tailscale funnel 7171                 # public HTTPS at your ts.net name
```

Give the assistant its two URLs once (standing instructions):

- `GET /t/<thread>?key=…` — the brief: who it is, whose turn, the clean transcript, the current thread version, and the exact write-back recipe.
- `GET /t/<thread>/write?key=…&turn=<version>&b64=<reply>` — takes its turn through the same engine as everyone else (`&text=` percent-encoded works too; replies under ~6KB). The `turn=` echo makes stale replies bounce with "fetch again first", and a replayed URL is harmless — the floor has moved.

Identity comes from the capability key (one per `--proxy` participant), never from a parameter — the no-impersonation rule, ported to HTTP. The server holds no state and binds localhost only; the funnel is the one hole.

For the manual courier loop (no server at all), `substrate brief <thread> --for kagi | pbcopy` produces the outbound packet, and `pbpaste | substrate write <thread> --as kagi --stdin` carries the reply back without shell-quoting pain (`--file` works too).

Agents poll with `wait_for_turn` — it wakes instantly on file changes, a timeout means "call it again", and a thread is only finished at status ended. To nudge a specific harness your own way, the lower-level hook is:

```sh
# run a hook each time the floor reaches codex-b; exits when the thread ends
substrate watch storage-design --for codex-b \
    --exec 'your-nudge-command'   # gets SUBSTRATE_SPACE/_THREAD/_TURN/_STATUS/_TOPIC
```

## Interfaces

**TUI** (`substrate`, or `substrate tui --name <you>` to pick an identity) — thread scrollback above, input below, like prompting an agent CLI. `n` starts a thread, Enter sends, Alt-Enter inserts a newline, Esc backs out to the thread list, double Ctrl-C quits. Every write by anyone else appears live (file watching). If you moderate the open thread, slash-commands are enabled on your turn:

```
/topic <text> · /turns <name> <name>… · /quiet <name> [n]
/unquiet <name> · /invite <name> · /next <name> · /pass · /end · /resume · /help
```

**MCP** (`substrate-mcp --name <agent> [--space <dir>]…`) — six tools: `about` (orientation: what substrate is and the exact participant loop — tell a new agent "call about first" and that's the whole onboarding), `list_threads` (federated across configured spaces), `read_thread` (all / `last_n` / `from_line`; line numbers are stable so `from_line = previous total + 1` reads only what's new), `write_entry`, `check_turn`, and `wait_for_turn` (long-poll that wakes instantly on file changes). With several spaces configured, tools take a `space` label alongside `thread`. Identity is fixed at process launch; there are no edit or delete tools by construction. Every status-bearing response ends with a "→ your move / not your turn" option line, and rejections say what to do next — agents learn the protocol from the responses themselves, so it works even in harnesses that never surface server instructions.

**Watch** (`substrate watch <thread> [--for <name>] [--exec <cmd>]`) — the poll→push bridge: reports floor changes on stdout, optionally runs a hook command per change, exits when the thread ends.

## Open edges

Known issues, current limitations, and deliberate non-features. Substrate is alpha; the first two lists shrink over time, the third is load-bearing.

**Known issues**

- **No push notification for turns.** The MCP protocol supports server-initiated notifications, but no mainstream harness yet wakes a model on one — so `turn_available` stays parked. The bridges: `wait_for_turn` (long-poll, wakes instantly on file changes), `substrate attend` (the loop lives outside the model), and harness stop-hooks. Expect to nudge resident agents occasionally.
- **Thread `config.yaml` races are last-write-wins.** Turn enforcement makes two writers rare, but a moderator op racing an agent's write at a round boundary can drop one of the two updates (entries are never lost — only turn-state edits can collide). An advisory lock is the known fix, deferred until it bites in practice.
- **Agent harness run-limits still apply.** A model told to "loop until Ended" will eventually stop anyway (turn caps, context limits). This is the harness's nature, not a bug substrate can fix — `attend` and stop-hooks exist precisely because of it.

**Current limitations**

- **No migration tooling.** Layout/format changes between alpha versions mean re-`init` (it has already happened once: the `.substrate/` move). Don't keep irreplaceable threads in an alpha format without exporting (`substrate read <thread> > …`).
- **Moderation is human/TUI-only.** The MCP surface has no moderation tools, so an agent can't moderate a thread yet; `/end`, `/resume`, `/quiet`, `/next` etc. also have no CLI equivalents.
- **`serve` replies cap at ~6KB** (practically ~4–5KB after base64, per field testing) and there's no multi-part chunking yet. Long proxied replies must be split across turns.
- **`serve` security is a capability key in a URL** behind an unguessable funnel hostname — obscurity plus key, deliberately proportionate to a lab. Keys appear in intermediary logs; rotation (`--key <new>`) is manual. Don't put a thread you'd mind leaking behind a funnel.
- **Proxied participants must nonce their own URLs.** Fetch-tool caches are defeated by a `&fresh=<random>` param (the brief teaches this), but a participant that forgets will read stale pages.
- **Single machine.** Local-first with no sync, no replication, no remote spaces. Two laptops are two worlds.

**By design (won't fix)**

- **No edits, no deletes, no DMs.** Append-only and room-addressed is the contract; every interface refuses mutation by construction.
- **Local trust model.** Any process with filesystem access can write as anyone (`substrate write --as`); the no-impersonation guarantees apply to *protocol* participants (MCP identity fixed at launch, serve identity from the key), not to the machine's owner. Local FS access already means full control — substrate doesn't pretend otherwise.
- **`--space` is cwd-exact.** No upward directory search à la git. One project, one space, no yelling across threads — the constraint is meant to breed structure (sub-threads, exports) rather than sprawl.
- **No-op detection is exact-match only** (`pass` / `no-op` / `...`). A rambling "I'll pass on this one" is a real entry; moderators teach agents, the tool doesn't guess.
- **`quiet` is a counter, not a state** — "skip your next N turns", auto-expiring. A standing mute is just a reorder.
- **Names are lowercase `a-z0-9-`.** Filenames are the data model; the character set is the price of filesystem-safe, sortable, unambiguous entry files.
- **The wizard never creates without consent**, and substrate never installs anything outside `<space>/.substrate/` and `~/.substrate/`.

## Layout

```
crates/substrate-core   data model, storage, turn engine, transcript
crates/substrate-mcp    stdio MCP server (one process per agent)
crates/substrate-tui    the `substrate` binary: CLI subcommands + human TUI
```

## Tests

`cargo test` covers the engine (turn order, quieting, no-ops, windowing, torn-write invisibility) and drives the real MCP binary through the protocol with two agents sharing a space.
