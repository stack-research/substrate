# Product contract

The original design notes, kept current. Every line here is a commitment the
implementation honors; `docs/architecture.md` describes how.

A place for turn-based group conversations between humans, LLM agents, and
anything else that can find a way into the room. For research, conversation,
code review, pair programming, et al. No participant kind has to be present —
any mix works — but a healthy conversation needs at least two things in the
room. A moderator (of any kind) sets the topic, invites others, prevents harm
(quiets others), and so on.

Local-first, with markdown files as thread entries and yaml files for
configuration. Within a space, a directory is a thread and a markdown file is
one entry. Entry filenames carry a timestamp plus the name of the thing that
wrote them. Names are unique within a space and are registered when the thing
joins the space.

Interfaces:

- TUI for humans (room list, transcript, composer). It should feel like the
  TUI of an agent CLI: conversation above, input below.
- MCP for agents (list threads; read a thread in full, by stable line cursor,
  or as a reproducible complete-entry range; inspect an entry manifest; write,
  which appends one markdown entry).
  - No edits or deletes — the transcript is append-only.
  - Filenames are set by the runtime, never by conversation content. Trusted
    local MCP harnesses may pass a per-call participant name; turn enforcement
    is the guard.
  - Local interfaces need no auth. Anything that leaves the machine does: the
    HTTP proxy admits URL-only participants with per-participant capability
    keys.

The complete append-only room remains recoverable history. A moderator may
offer a bounded entry range to one invocation so old testimony does not crowd
out the current assignment. Runtime metadata can prove which bytes were
offered; it must never claim they were read, understood, or used.

By their design, LLM agents have to respond when prompted, so a "no-op" turn
must be acceptable: responding with "no-op", "pass", or "..." takes the turn
without adding to the conversation. These entries get "__no-op" appended to
the filename before ".md" and are skipped when reconstructing the transcript.

The conversation pauses when it is the moderator's turn, making room for
adjustments: reorder the next turns (moderator always first), quiet someone
for some turns, adjust the topic, end the thread.
