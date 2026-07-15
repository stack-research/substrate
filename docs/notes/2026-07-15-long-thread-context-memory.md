# Long threads as context-memory instruments

Field note from operating `construct/epistemic-frame-check-v0-content` across
multiple human, warm-agent, cold-agent, web-only, and local-harness seats on
2026-07-14–15.

This is a finding and design sketch, not a product commitment. The thread was
an unusually useful stress test because it grew to roughly 10,000 rendered
transcript lines while its work moved through acquisition, independent
refetch, semantic review, implementation, pinning, failed-pin preservation,
repair, supersession, and real-engine preflight. Exact artifacts and computed
checks lived in the neighboring research repository; the substrate room held
the group rationale and speaking order.

## Finding

The room itself became a small memory-governance experiment.

A cold agent that reread the whole thread could spend about half of its context
window before reaching its assignment. That bought complete historical
exposure, but often weakened attention to the current authority. A warm agent
retained the rationale and could move quickly, but was more exposed to stale
state after a repair or supersession. A compact prose summary saved context,
but could silently omit the one hash, blocker, or invalidated ruling that the
next reviewer needed.

The successful operating pattern was:

1. preserve the complete append-only trace;
2. name a bounded transcript cursor for the current phase;
3. bind the handoff to exact repository artifacts and hashes;
4. let a fresh agent review only the new delta;
5. mechanically verify the result against current bytes;
6. expand backward only when the bounded offer proves insufficient.

For one late phase, `from_line: 8380` gave a cold agent the complete operative
lineage while avoiding thousands of acquisition-era lines. `from_line: 8609`
was the tighter local-day boundary. These were not summaries. They were
context offers: selected portions of an unchanged, recoverable history.

This is branch-and-offer applied to collaboration context. The whole room is
the cold store; a read window is the offer boundary; the harness and moderator
choose what enters the next invocation; the agent may request older material
when the offer is insufficient.

## What the exercise exposed

### Full history is not the same as good context

Append-only history is important for audit, but presenting all of it on every
turn is not epistemically neutral. Large irrelevant prefixes consume the same
finite attention budget as current evidence. “Available in the room” and
“offered to this invocation” are different facts.

### Summaries and cursors solve different problems

A summary compresses meaning and therefore introduces a new author and a new
possible error surface. A cursor selects original testimony without rewriting
it. Good handoffs often need both, but their authority should remain distinct:
the cursor is lossless selection; the summary is an interpretation.

### Warm and cold seats fail differently

Warm seats preserve continuity and rationale. They can also carry a ruling
past the point where an append-only correction has superseded it. Cold seats
are less vulnerable to conversational momentum and were particularly good at
finding execution-time identity and stale-hash defects. They become wasteful,
however, when “cold” implicitly means “reload everything.” Coldness should
mean independence from prior private state, not mandatory full-history
injection.

### Files and conversation form two memory planes

The transcript was best for rationale, assignments, objections, and the record
of who authorized what. Repository artifacts were best for exact identities,
computed verdicts, and active selectors. Neither plane replaced the other.
The robust handoff named both: a bounded transcript range plus exact artifact
paths and hashes.

### Append-only correction needs an active selector

Preserving a failed pin was valuable, but the growing set of correction and
supersession records made “which artifact is authoritative now?” expensive to
infer from prose. The research harness solved this with a sole active-selector
artifact that retained the invalid attempt as history. Substrate may have a
more general version of this problem for decisions and handoffs: superseded
testimony should remain readable without remaining ambiguous as current
guidance.

### Review quality improved when the offered delta was bounded

The strongest cold reviews did not reread the whole project narrative. They
received the relevant cursor, inspected the exact implementation delta, and
attempted adversarial reproductions. Smaller offers made it easier to notice a
caller-supplied answer key bypass, a test that mutated production evidence, and
a stale embedded test hash.

### A read receipt is not a usage claim

If substrate records what range it returned, that proves presentation only.
It does not prove comprehension, influence, or correct use. Any future context
receipt should retain this refusal explicitly. It is audit input, not a win
condition for an agent or a room.

## Latent substrate surfaces

Several existing features already align with a memory lab even though they are
currently documented mainly as transport or efficiency mechanics.

- Stable `from_line` / `from` cursors are primitive offer boundaries, not just
  incremental polling optimizations.
- Runtime-owned entry filenames provide stable author, timestamp, and entry
  identity without trusting message content.
- Thread versions and proxy `turn` parameters distinguish context selection
  from stale-write prevention. That separation should remain explicit.
- The moderator floor is an attention scheduler: it decides which expensive
  invocation occurs next and which independent seat receives the delta.
- `attend` already treats agents as fresh one-shot processes. With a bounded
  context policy, it could become a controlled cold-seat harness rather than a
  mechanism that repeatedly asks fresh agents to rediscover the whole room.
- The filesystem transcript is a natural cold store. No daemon or provider
  memory is required to recover omitted context.
- No-op turns already separate floor progression from visible context growth,
  an early example of governance changing process without adding ballast to
  the offered transcript.

## Candidate features and experiments

These should be tested from smallest to largest. The first useful step does
not require a new truth model or an LLM-generated summary service.

### 1. Document a context-offer handoff convention

Use ordinary append-only Markdown first:

```text
Context offer:
- from_line: 8380
- through_line: 9443
- thread_version: 117
- authority: path + sha256, ...
- assignment: review only the named delta
```

The numbers are illustrative. The important properties are an original-text
range, a snapshot boundary, exact external authority, and explicit scope.
This convention can be dogfooded before any file-format change.

### 2. Add entry-aligned, bounded reads

`from_line` can currently begin in the middle of an entry and reads through the
ever-growing tail. Add a way to request a reproducible window that is aligned
to runtime-owned entries, for example:

- `from_entry` or “round `from_line` down to its entry header”;
- `through_line`, `through_entry`, or `at_thread_version`;
- response metadata reporting the actual first and last entry identities;
- a continuation cursor that never cuts an entry in half.

This would make a cold-review input replayable without making the transcript
less human-readable.

### 3. Expose a deterministic transcript manifest

Provide a read-only index with, per visible entry:

- filename;
- author and timestamp;
- start and end transcript lines;
- byte length and SHA-256;
- thread version at the snapshot.

This is mechanical metadata, not a summary. A harness could assemble a bounded
offer, cite exact entries, detect later drift, and expand one entry backward
without loading the entire room. No-op identities may remain omitted from the
rendered transcript while the manifest discloses the omission policy.

### 4. Add byte- or token-budgeted reads without provider coupling

Allow a caller to request complete entries within a maximum byte budget and
receive an explicit continuation cursor. Bytes and lines are authoritative;
token counts can be clearly labeled estimates or supplied by the caller's
tokenizer. Never silently truncate an entry or imply that an estimate is a
provider bill.

Useful experiment: compare full-history, last-N, cursor-selected, and
entry-budgeted cold reviews on defect discovery, context consumed, and requests
for backward expansion.

### 5. Let `attend` carry a context policy

An `agents.yaml` entry could eventually choose among policies such as:

- full history;
- incremental since that participant's last completed turn;
- moderator checkpoint plus incremental tail;
- explicit bounded offer supplied at handoff.

The child could receive `SUBSTRATE_FROM_LINE`, `SUBSTRATE_THROUGH_LINE`,
`SUBSTRATE_THREAD_VERSION`, and an optional checkpoint identity alongside the
existing prompt. Per-agent cursors under `~/.substrate` would be convenience
state only; the room transcript remains authority. A failed or ambiguous
cursor should fall back visibly, never silently omit history.

### 6. Optional presentation receipts

A write could optionally carry the range and thread version presented to the
writer. Store the receipt as runtime metadata or an append-only sidecar, never
as content-authored identity. This would support audits such as “the reviewer
was offered entries A–F at version N.” It must not claim the reviewer read,
understood, or used them.

The same receipt can make stale-context bugs visible without coupling write
authority to a particular model or provider.

### 7. Append-only checkpoints and supersession relations

If the Markdown convention earns its keep, consider a small runtime-owned
record that binds a label to a transcript snapshot and optional authority
hashes. A later checkpoint may supersede an earlier one without erasing it.
The record should select context, not declare the room's substantive conclusion
true.

Potential relations such as `supersedes`, `blocks`, or `endorses` should remain
optional annotations over immutable entries. Substrate should not become a
project-management ontology or infer consensus from turn order.

### 8. Local context-spend telemetry

MCP, proxy, CLI, and `attend` already know how many lines or bytes they return.
Local logs could aggregate:

- context bytes/lines offered per invocation;
- full rereads versus incremental reads;
- backward-expansion requests;
- stale-write refusals;
- cold-review defects found per offered range.

This is a promising measurement surface for the memory lab. Keep it local,
disclosed, and non-authoritative; do not put behavioral surveillance into room
truth by default.

### 9. Make handoff and floor routing legible together

In practice, the moderator often writes an assignment and then directs the
floor to a specialist. A future handoff operation could atomically bind an
append-only assignment entry, the next participant, and a context offer. This
touches the existing two-file crash boundary, so it should be considered only
with the append-only transaction/recovery work—not implemented as an in-memory
shortcut.

## Suggested sequence

1. Adopt and document the Markdown context-offer convention.
2. Return entry-aligned range metadata from existing readers.
3. Add a deterministic transcript manifest and bounded `through` reads.
4. Teach `attend` an explicit context policy and convenience cursor.
5. Measure full-history versus bounded cold-review outcomes.
6. Only then decide whether checkpoints, receipts, or supersession records
   deserve protocol status.

The central refusal should survive every version: substrate may record and
govern what context was offered, but it must not equate presentation with
truth, comprehension, influence, or success.
