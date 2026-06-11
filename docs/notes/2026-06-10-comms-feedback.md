# Field-test feedback — first real agent session (2026-06-10)

> **Status (same day):** shipped, then extended by the usage walkthrough.
> Multi-space substrate-mcp (registry at `~/.substrate/spaces.yaml`, `space`
> tool param, federated listing), file-watch wake inside `wait_for_turn`,
> `substrate watch --exec`, timeout-means-retry instructions — all in.
> The walkthrough then added the "git init" UX: `substrate` runs a first-run
> wizard (consent + once-ever identity), `n` creates rooms in-TUI (named
> after the directory, unknown turn names auto-register as agents),
> `substrate init` seeds from `~/.substrate/participants.yaml` and
> auto-registers the space, the registry is re-read per call (new spaces
> visible to running agent sessions instantly), and `substrate attend <name>`
> runs agents as ephemeral turn-takers from `~/.substrate/agents.yaml`.
> Remaining: `turn_available` MCP notification (blocked on host support).

Source: the `comms` conversation in the memory space
(`~/.projects/stack-research/memory/comms`) between user-name (moderator, TUI) and
cursor (agent, Cursor via substrate-mcp), plus cursor's post-session summary.
The protocol itself validated end-to-end: check_turn → read_conversation →
write_entry → wait_for_turn, multi-turn async chat, clean `/end`.

## Agreed direction

**Multi-space MCP: one `substrate-mcp` server, many spaces** (user-name + cursor
both converged on this; "N duplicate MCP registrations per space" is the
thing to avoid).

Dev sketch for when this is picked up:

- `substrate-mcp --name <agent>` with spaces from repeatable `--space` flags
  and/or a `~/.substrate/spaces.yaml` registry.
- Tools gain a `space` parameter; conversation addressing stays explicit
  (no ambient "current space" — a misrouted write is worse than a verbose arg).
- `list_conversations` federates across spaces with explicit space labels.
- Identity invariant unchanged: one `--name` fixed at launch, verified
  against each space's own registry. Names stay **per-space** (the space is
  the trust boundary); an agent reusing its name across spaces is convention.

## Open design bits (parked, not yet decided)

- Participant naming across spaces (global-by-convention vs enforced).
- Federated vs space-scoped `list_conversations` default.
- Space root vs git root when they differ.

## Turn delivery: poll → push ladder

Long-poll `wait_for_turn` is a bridge, not the end state. Realistic steps:

1. **Now-ish**: `wait_for_turn` wakes on a notify file-watch instead of its
   internal 1.5s sleep loop. No protocol change, instant wake.
2. **Interim**: `substrate watch --exec <cmd>` CLI hook so any harness can be
   nudged its own way (file watcher → harness-specific re-prompt).
3. **Target**: `turn_available` MCP notification when the floor advances —
   blocked today on host support (notifications reach the MCP *client*, not
   the model's context; no mainstream harness reliably wakes an agent on one).

## Agent-ops fix (cheap, do with next batch)

Encode in the server's `get_info` instructions: a `wait_for_turn` timeout
means "still waiting — call it again"; an agent is done with a room only when
`status: ended`. (cursor initially timed out once and stopped.)
