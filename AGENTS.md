# Notes for agents working on this repo

The spec is [docs/notes/README.md](docs/notes/README.md). Read it before changing behavior — it is short and every line is load-bearing.

## Orientation for a new session

- **Layout**: `crates/substrate-core` (data model, storage, turn engine, transcript) · `crates/substrate-mcp` (stdio MCP server, one process per agent) · `crates/substrate-tui` (the `substrate` binary: CLI subcommands + human TUI + `serve`/`attend`/`watch`).
- **This repo is itself a substrate space.** `.substrate/` here holds live threads, and the dev agent (`claude`) is a registered participant — you may be asked to "take your turn in thread X". Use the substrate MCP tools; call `about` first if they're new to you. The room's findings often become this repo's fixes, sometimes within the same rotation.
- **Code changes reach nobody until reinstalled.** Live sessions (TUIs, MCP servers, `attend` loops, other projects) run `~/.cargo/bin/substrate{,-mcp}`, not `target/debug`. After a change passes the gate: `cargo install --path crates/substrate-tui --force` and `--path crates/substrate-mcp --force`. Running MCP servers pick the new binary up on their next harness respawn — expect a lag, and don't mistake a stale server for a failed fix (we've burned time on that twice).
- **These binaries are live infrastructure now.** Other projects' spaces and `~/.substrate` (identity, space registry, agents.yaml) depend on the current on-disk formats. Still alpha — breaking changes are allowed — but they now break *neighbors*, not just this garage: say so loudly in the change description, and prefer formats that tolerate unknown fields.
- **Tests passing is necessary, not sufficient.** The bugs that mattered here were caught by *live* verification after green suites: the orphaned-TUI core burn (scripted pty session), the librarian-unparseable write responses (raw HTTP GETs). For TUI behavior drive a real pty; for `serve` speak raw HTTP; for MCP use the child-process integration tests as the template.

## Design invariants (do not break)

- **Group-first.** N humans + M agents are peers in one room. Any feature must hold for multiple humans AND multiple mixed-provider agents at once. No one-to-one routing, no DMs, no provider coupling anywhere.
- **The filesystem is the shared state.** Concurrent processes (TUIs, MCP servers, scripts) coordinate only through the space directory. No daemon, no cache of record. Every operation re-reads from disk.
- **Append-only.** Entries are never edited or deleted. No interface may grow a mutation tool.
- **The runtime names files.** Author identity comes from the filename, which only the runtime writes (`--name` is fixed at process launch). Nothing in thread content may influence identity.
- **Spaces are the trust boundary.** One `substrate-mcp` may serve many spaces (`crates/substrate-mcp/src/spaces.rs`), but names are registered per-space, identity is verified against each space's own registry, and writes always name their space explicitly when more than one is configured — never an ambient "current space". `~/.substrate` (`substrate-core/src/home.rs`) is machine-level *convenience* (identity, space registry, crew template, agent commands), never space-level authority. A space's own data lives in `<project>/.substrate/` (config.yaml + threads/), git-ignored by default — lineage is opt-in via .gitignore or `substrate read <thread> > docs/…`.
- **The set of spaces is filesystem-truth too.** The MCP server and `substrate attend` re-read `~/.substrate/spaces.yaml` on every call/cycle; never cache it across calls. A `substrate init` anywhere must be visible to running sessions without restarts.
- **The wizard never creates without consent.** `substrate` in a non-space directory asks before writing anything; typo-running it must stay harmless.
- **Agents are ephemeral turn-takers by default** (`substrate attend`): the transcript is the context, the loop lives outside the model. Don't design features that assume a long-lived agent session.
- **Entry timestamps are strictly monotonic per thread** (`turn::next_timestamp`) so lexicographic filename order == write order. Don't replace this with bare wall-clock naming.
- **No-op turns** (`pass` / `no-op` / `...`, exact match) are recorded on disk but skipped by all readers.
- **Moderator pause is derived**, never stored: the room is paused iff `turn_order[next_index] == moderator`. The moderator is always first in `turn_order`.

## Before publishing / committing docs and transcripts

An active `tailscale funnel` in front of `substrate serve` is protected by obscurity (an unguessable hostname) plus a capability key. That's acceptable for the lab — **as long as the repo never broadcasts either**. Before making the repo public, committing exported thread transcripts, or pasting logs into docs/notes, scan and redact:

```sh
# real tailscale hostnames (docs placeholders like "your ts.net name" are fine)
grep -rniE 'taile[0-9a-f]+|[a-z0-9-]+\.ts\.net' --exclude-dir=target .

# live capability keys (long hex after key=; recipe text like "key=…" is fine)
grep -rnE '(key|fresh)=[0-9a-zA-Z]{16,}' --exclude-dir=target .
```

Redact hits to `<ts-net-host>` / `key=<redacted>`. Thread transcripts are the likeliest leak path — participants paste serve URLs into entries — so run the scan over `.substrate/` and any `docs/threads/` exports too. Rotating the key (`substrate serve --key <new>`) after any suspected leak costs one line in Kagi's standing instructions.

**Enforced by a pre-push hook** ([.githooks/pre-push](.githooks/pre-push)): it scans *every commit in the push range* (not just HEAD — old commits publish their history too) and blocks the push with the offending lines. Enable once per clone:

```sh
git config core.hooksPath .githooks
```

Two things the hook will teach you the hard way otherwise: a follow-up "redact" commit does NOT clear a block — the leak still exists in the earlier commit, so amend or rebase it away; and a knowingly-safe line can be allowlisted with a `redaction-ok` comment on that line.

## Working here

- `cargo test` — engine + CLI + protocol-level MCP tests; keep it green.
- `cargo clippy --all-targets` — keep it clean.
- TUI changes: the event loop pattern is a single `App` struct, pure `handle_key -> Option<Action>`, and all I/O in `run.rs`'s `tokio::select!` loop. Don't introduce event-enum sprawl.
- rmcp idiom: `#[tool_router]` impl + `#[tool_handler] impl ServerHandler`; the handler macro routes through `Self::tool_router()`. Domain rejections (not your turn, ended) are `CallToolResult::error(...)` the model can read, not protocol errors.
- YAML via `serde_norway` (maintained `serde_yaml` fork). Atomic writes via dotfile temp + rename (`space::write_atomic`); readers must keep skipping dotfiles and non-entry filenames.
