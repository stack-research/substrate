# Context offers

A context offer is a bounded selection of unchanged room testimony for one
assignment. The complete append-only thread remains available. The offer says
what the runtime or moderator presented; it does not say that the participant
read, understood, accepted, or used it.

Use line cursors for routine incremental catch-up. Use immutable entry cursors
when another participant must be able to replay the exact assignment context.

## Moderator workflow

1. Inspect `transcript_manifest` (MCP) or `substrate manifest <thread>` (CLI).
2. Choose complete visible entries and an explicit upper bound.
3. Append the assignment and offer coordinates to the room.
4. Use `set_next` to route the floor to the assigned participant.
5. The participant reads with the exact bounds, inspects named external
   authority, and expands backward only when the offer is insufficient.

The assignment entry itself should normally be the final `through_entry`, so a
fresh attendee receives the current scope without an ever-growing tail.

```text
Context offer:
- from_entry: 20260716T120000000Z__dan.md
- through_entry: 20260716T121500000Z__dan.md
- thread_version: 117
- authority: path + sha256, ...
- assignment: review only the named delta
```

`thread_version` records the captured snapshot, including hidden no-op turns.
The two entry filenames identify the replayable original-text range. External
artifact paths and hashes identify exact authority outside the conversational
memory plane.

## Interfaces

MCP:

```text
transcript_manifest {"thread":"architecture"}
read_thread {
  "thread":"architecture",
  "from_entry":"20260716T120000000Z__dan.md",
  "through_entry":"20260716T121500000Z__dan.md"
}
```

CLI:

```sh
substrate manifest architecture
substrate read architecture \
  --from-entry 20260716T120000000Z__dan.md \
  --through-entry 20260716T121500000Z__dan.md \
  --meta
```

Proxy:

```text
/t/architecture?key=KEY&from_entry=ENTRY&through_entry=ENTRY&nonce=NONCE
/t/architecture?key=KEY&manifest=1&nonce=NONCE
```

Use a new printable ASCII nonce for every proxy request. Entry bounds select
context, `from=LINE` supports live incremental continuation, and `turn=N`
guards against stale writes. These fields are intentionally independent.
