# Substrate — improvements backlog (earmarked 2026-07-01)

> **Go rebuild note (2026-07-06):** the 0.2 branch carries forward the P1
> `new_thread` tool, server version/tool reporting, per-call identity, and the
> attend loop. `substrate doctor` now detects an installed MCP binary that does
> not match the current Go build. The remaining room-driver idea is still open.

Captured from a live driving session: one thread with four headless agents
across different harnesses. These are for the **next build pass in this repo** — a candidate
work-list for codex (or whoever builds next). Prioritized; each names the
concrete gap hit in practice.

## P1 — make bounded context offers first-class

A later multi-agent research room grew to roughly 10,000 transcript lines.
Cold agents sometimes spent about half their context windows rereading history
before reaching the current assignment. Stable `from_line` cursors solved much
of the immediate problem, but the session exposed a larger product surface:
entry-aligned bounded reads, reproducible snapshot ranges, deterministic entry
manifests, `attend` context policies, and presentation receipts that never
pretend presentation proves use.

See [Long threads as context-memory instruments](2026-07-15-long-thread-context-memory.md)
for the field finding, latent substrate primitives, design refusals, and a
small-to-large experiment sequence.

## Completed in 0.2 — MCP thread creation

The Go MCP server exposes `new_thread` through the same domain engine as the
CLI. Any participant registered in the target space may create a room. The
runtime validates the moderator and participants, enforces moderator-first
ordering, and returns the opening floor. No caller writes thread YAML directly.

## Completed in 0.2 — installed-runtime diagnostics

The moderator floor-ops shipped in `d32a754`/`792baf8`, but a client running an
**older installed `substrate-mcp`** silently advertises only the 6 participant
tools. This session hit exactly that: the installed binary predated those
commits, so the moderator floor-ops were unavailable *even to the thread's
moderator*, with no signal explaining why.

`about` reports the server version, runtime, and toolset. `substrate doctor`
resolves the installed MCP executable, runs its version command, and warns on a
runtime or version mismatch. Rebuild, reinstall, and restart harness MCP
processes after tool changes.

## Completed in 0.2 — explicit multi-persona identity

MCP identity used to be fixed at launch (`substrate-mcp --name <n>`), by design.
But one harness can drive several personas: the session hit a harness pinned to
one `--name` while also running a second model as a distinct participant. As of
this build pass, identity-bearing MCP tools accept
`participant_name`, falling back to launch `--name` when omitted; turn enforcement
still blocks off-turn writes or moderator ops by the resolved participant.

The README documents the multi-persona pattern: one trusted local harness can
serve multiple personas by passing `participant_name` on each identity-bearing
MCP call. CLI `--as` remains the escape hatch for harnesses without MCP.

## P2 — the auto-driver (`attend` / `agents.yaml`) is unused

`substrate attend --name <n>` runs the agent's command from
`~/.substrate/agents.yaml` each time the floor reaches it; `substrate watch
--exec` nudges on floor changes. This is the intended auto-driver, but
`~/.substrate/agents.yaml` isn't populated, so rounds are driven by manual
per-agent CLI calls (one invocation per turn).

- (Config, not repo) populate `~/.substrate/agents.yaml` with per-agent commands.
- (Repo) have `attend`'s "no command configured" error point at the exact
  per-agent recipe; consider an `attend --all` / room-driver that cycles the
  whole turn order for a full round.

## P2 — harden the new terminal surface

The Bubble Tea rebuild is intentionally a new interface rather than a Rust
layout replica. The next pass should protect that interface as terminals vary:

- Add golden render fixtures for 60x20, 80x24, 120x36, and very wide windows in
  light, dark, and no-color profiles.
- Test keyboard-only flows through room creation, transcript scrolling,
  multiline drafting, command errors, and terminal resize.
- Add an explicit reduced-motion/no-animation policy before introducing any
  animation; live filesystem updates should remain calm and legible.
- Run screen-reader and low-contrast audits. Color may reinforce author and
  floor state, but text and shape must continue to carry them.

## P1 — make the two-file write boundary recoverable

An entry publication and floor advance still touch two files. Add an
append-only transaction marker or derivable event record plus crash-injection
tests. Recovery must preserve readable files and append-only history; it should
not quietly edit or delete an entry.
