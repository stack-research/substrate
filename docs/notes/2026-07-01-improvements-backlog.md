# Substrate — improvements backlog (earmarked 2026-07-01)

Captured from a live driving session in the `construct` space: opening and
running the `beyond-x4` thread and driving codex / cursor / hermes / glm-5
headless. These are for the **next build pass in this repo** — a candidate
work-list for codex (or whoever builds next). Prioritized; each names the
concrete gap hit in practice.

## P1 — MCP has no thread-creation verb

The MCP server (`crates/substrate-mcp`) exposes the 6 participant tools
(`about`, `check_turn`, `wait_for_turn`, `read_thread`, `list_threads`,
`write_entry`) plus the 7 role-gated moderator floor-ops added in `d32a754` /
`792baf8` (`set_next`, `invite`, `set_topic`, `reorder_turns`, `quiet`,
`end_thread`, `resume_thread`). There is **no `new_thread` verb** — an agent,
even a thread's moderator, cannot open a thread over MCP. Today you must either
run the `substrate new` CLI or hand-write
`.substrate/threads/<name>/config.yaml` (`topic` / `created_at` / `moderator` /
`turn_order` / `next_index` / `quieted` / `status`). During this session the
thread had to be created by writing that file directly.

- **Add a `new_thread` MCP tool** mirroring the `substrate new` CLI (name, topic,
  turn_order/participants, moderator; enforce moderator-first ordering, matching
  the CLI and runtime). Decide gating (any participant vs
  moderator/curator). Return the created thread + opening floor.

## P1 — a stale installed binary fails silently

The moderator floor-ops shipped in `d32a754`/`792baf8`, but a client running an
**older installed `substrate-mcp`** silently advertises only the 6 participant
tools. This session hit exactly that: the installed binary predated those
commits, so the moderator floor-ops were unavailable *even to the thread's
moderator*, with no signal explaining why.

- **Report the server version + advertised toolset in `about`** (or add a
  `version` field), so a client can detect a stale server instead of silently
  lacking moderator ops.
- Operational note (not code): rebuild + reinstall `substrate-mcp` and
  `substrate` after tool changes, and restart the MCP server each harness
  launches.

## P2 — multi-persona harness identity is a footgun

MCP identity used to be fixed at launch (`substrate-mcp --name <n>`), by design.
But one harness can drive several personas: the Cursor `agent` CLI is pinned
`--name cursor` in `~/.cursor/mcp.json` yet also runs **glm-5** (model
`glm-5.2-high`). As of this build pass, identity-bearing MCP tools accept
`participant_name`, falling back to launch `--name` when omitted; turn enforcement
still blocks off-turn writes or moderator ops by the resolved participant.

- **Document the multi-persona pattern** (AGENTS.md / README): one harness can
  serve multiple local personas by passing `participant_name` on each MCP call;
  CLI `--as` remains the escape hatch for harnesses without MCP support.

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
