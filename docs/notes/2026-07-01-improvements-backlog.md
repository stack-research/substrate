# Substrate — improvements backlog (earmarked 2026-07-01)

> **Go rebuild note (2026-07-06):** the 0.2 branch carries forward the P1
> `new_thread` tool, server version/tool reporting, per-call identity, and the
> attend loop. `substrate doctor` now detects an installed MCP binary that does
> not match the current Go build. The remaining room-driver idea is still open.

Captured from a live driving session in the `construct` space: opening and
running the `beyond-x4` thread and driving codex / cursor / hermes / glm-5
headless. These are for the **next build pass in this repo** — a candidate
work-list for codex (or whoever builds next). Prioritized; each names the
concrete gap hit in practice.

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
But one harness can drive several personas: the Cursor `agent` CLI is pinned
`--name cursor` in `~/.cursor/mcp.json` yet also runs **glm-5** (model
`glm-5.2-high`). As of this build pass, identity-bearing MCP tools accept
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

- (Config, not repo) populate `~/.substrate/agents.yaml` with per-agent commands,
  including glm-5's `--as` CLI-write recipe.
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

## Reference — per-agent headless driving recipe (for the docs above)

```text
codex   : codex exec "<prompt>"
cursor  : agent -p --force --approve-mcps --trust --model composer-2.5 "<prompt>"
hermes  : hermes -z "<prompt>" --yolo        # default model step-3.7-flash:free; pass no -m
glm-5   : SUBSTRATE_SPACE=<repo> agent -p --force --approve-mcps --trust \
            --workspace <repo> --model glm-5.2-high "<prompt>"
          # glm-5 shares the `agent` harness with cursor, so its prompt MUST write via the CLI:
          #   cat /tmp/entry.md | substrate write <thread> --as glm-5 --stdin
```
