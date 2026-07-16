# Locally hosted rooms

**Status:** accepted direction; the first network build slice is proposed below.
Command and packet names remain provisional until they are exercised at the
real boundary.

Substrate is a locally hosted conversation server. One machine holds the
authoritative room while humans, agents, and other tools join through whatever
interface and transport they can use.

The closest familiar model is a player-hosted multiplayer game. The host's
machine owns the match state and validates every move. Remote players do not
merge their own copies of the world or trust one another to agree about what
happened. They connect to the host, observe an authoritative snapshot, submit
an action, and receive the resulting state.

For substrate, the authoritative state is not hidden inside a long-running
server process. It is the space's human-readable `.substrate/` directory. A
network process is only a gateway into the same domain engine used by the TUI,
MCP server, CLI, proxy, watcher, and attendee. If every process exits, the room
still exists.

A compact product description is:

> Hosted locally, joined remotely. The room is authoritative; clients are
> replaceable.

## Four distinct roles

The hosting model becomes clearer when four roles remain separate.

### The space host owns storage authority

The host machine contains the canonical `.substrate/` directory. It orders
entry publication, assigns runtime identity to entries, advances the floor,
and recovers interrupted writes. Remote clients never write room files
directly. They request domain actions from a host-side adapter.

The host process does not become a cache or daemon of record. It rereads the
filesystem before deciding. Notifications remain wakeups, not truth.

### The moderator owns conversational authority

The moderator chooses the topic and speaking order, invites and quiets
participants, and ends or resumes the thread. The moderator may be a human or
an agent and need not be the operator of the host machine.

This is a protocol distinction, not a claim that the moderator is
cryptographically independent of the host operator. Someone with direct
filesystem access can alter or remove the room. Remote participants therefore
trust the host machine with plaintext, availability, and record integrity, just
as players trust the operator of a locally hosted game server.

### Participants own their turns

A participant may read through any supported interface, but may append only
when the floor is theirs. The host validates registration, thread membership,
quieting, active status, and current floor. A transport cannot grant authority
that the room does not already grant.

### Transports own reachability

MCP, CLI, HTTP, a private overlay network, a tunnel, and a future native remote
client are ways to reach the room. They may differ in authentication,
encryption, latency, and presentation, but they must not invent competing room
semantics.

Network encryption and room authorization are different concerns. A private
network or TLS protects requests in transit. A participant capability selects
an identity at the host boundary. The turn engine decides whether that identity
may act now.

Every remotely exposed write must also carry the thread version the participant
observed. Unlike a local convenience adapter, the native remote boundary should
not accept an omitted stale-write guard.

## Why the server-authoritative model fits

### There is one history, not converging replicas

Every accepted turn becomes one runtime-named append-only entry in one room.
Readers may request different windows, but they are windows into the same
history. Substrate does not need peer-to-peer transcript merging, distributed
consensus, or eventual membership convergence.

### Server-side validation prevents client collisions

A client may draft while another participant has the floor, but an off-turn
write is rejected. Two clients cannot both make valid moves from the same room
version. This addresses the conversational equivalent of two players trying to
occupy the same authoritative state transition.

### Reconnects are ordinary

A participant can reconnect with its capability, the last stable line or entry
cursor it received, and the current thread version. It does not need the
previous client process or model session. A stale cursor affects how much text
is returned; it does not alter room truth. A stale turn version rejects a write
before publication.

### Lost responses do not require duplicate turns

If a write succeeds but its network response is lost, retrying with the old
thread version is rejected because the room has advanced. The participant can
read the refreshed transcript and see whether its entry landed. Cache nonces,
transcript cursors, capability keys, and stale-write versions must remain
separate concepts.

### Host failure pauses rather than corrupts the room

If the network gateway stops, remote participation stops but the files remain.
If the host process crashes during publication, the append-only transaction
intent permits the next reader or writer to finish or abort the state
transition without editing entry history. Availability and durability are
separate.

### Agent processes can remain ephemeral

A remote seat does not imply an immortal model session. `attend` can still
launch a fresh one-shot agent only when that participant has the floor. The
authoritative room supplies continuity; model-session memory is optional.

## A remote join

The intended path should feel like joining a small private game:

1. The host registers a participant in the space.
2. The moderator invites that participant into a thread.
3. The host creates a participant-specific capability and a small join packet.
4. The operator transfers the join packet over an appropriate private channel.
5. The participant connects and receives the available rooms, topic, status,
   floor, thread version, and an initial transcript window.
6. The participant waits or reads incrementally until the floor is theirs.
7. The participant submits one entry against the version it observed.
8. The host runs the ordinary turn engine, appends the entry, advances the
   floor, and returns the refreshed state.
9. The participant may disconnect. The room does not depend on its session.

The join packet is a bearer secret, not conversation content. It must never be
written into a room, exported transcript, committed file, shell history, or
ordinary request log. A participant should receive a different capability from
every other participant. Rotation and revocation should not require changing
room history.

The explicit one-time packet export necessarily reveals the capability to the
operator creating it. Ordinary status, diagnostics, and later command output
should not print it again.

The packet should contain only what a client needs, likely:

- a protocol version;
- an advertised host URL;
- the participant name;
- the participant capability;
- an optional initial space or thread hint.

It should not contain a private key for the host machine, filesystem paths, or
authority broader than the named participant.

## Hosting modes

The product should distinguish three deployment shapes rather than treating all
HTTP exposure as equivalent.

### Loopback courier

This is the current `substrate serve` posture. The gateway listens only on
localhost and issues participant-specific read and write URLs. A human may
carry those URLs to a URL-only participant, or place a deliberate tunnel in
front of the listener.

### Private-network host

This is the next useful product surface. The gateway remains on the moderator's
or operator's machine but is reachable through a private encrypted network or
an explicitly configured reverse tunnel. Substrate advertises the reachable
base URL while continuing to keep the space on the host filesystem.

Network exposure must be explicit. Loopback remains the default. Substrate
should not casually bind every interface or imply that a plaintext public
listener is safe because its URLs contain capability keys.

### Publicly reachable host

Direct public hosting brings rate limiting, abuse handling, TLS lifecycle,
capability rotation, request-log hygiene, and denial-of-service concerns. It is
not necessary to prove the locally hosted room model and should not be smuggled
into the first private-network build.

## URL-only courier and native remote client

The current proxy and a future native remote client serve different constraints
and should remain visibly different adapters.

The URL-only proxy uses fetchable GET URLs, query-string capability keys, cache
nonces, and encoded replies because some participants can do nothing except
fetch a URL. That compromise is useful, but URLs are unusually likely to enter
browser history, proxy logs, screenshots, and copied diagnostics.

A native remote client has no reason to inherit those compromises. Its network
contract should:

- send the capability in an authorization header rather than the URL;
- send entry content in a POST body rather than a query string;
- require the observed thread version on every write;
- return structured outcomes for accepted, stale, off-floor, ended, and
  unauthorized requests;
- preserve the same line cursors, entry bounds, manifests, and turn semantics
  as every other interface.

Both adapters still call the same domain engine. This is transport diversity,
not two room protocols. The URL-only route remains available for constrained
participants; the native route becomes the safer default for Substrate-aware
clients on another machine.

## Host migration and split brain

The file format makes a room portable, but portability is not live replication.
A safe host migration is deliberately boring:

1. stop every writer and network gateway for the old host;
2. copy the complete space, including transaction records;
3. open and verify it on the new host;
4. issue or re-advertise connection material for the new address;
5. resume writes only on the new host.

There must never be two writable hosts for one copied space. Independent copies
would create two valid-looking histories with no principled automatic merge.
Substrate should fail toward an explicit stopped room rather than pretend this
is a distributed database.

## Presence, identity, and trust claims

Several tempting claims should remain out of scope unless the runtime can prove
them.

- Registration means a participant is known to one space; it does not mean the
  participant is online.
- A delivered transcript window proves bytes were returned; it does not prove
  they were read or understood.
- A capability proves possession of a bearer secret; it does not prove a legal
  identity, device identity, or enduring model identity.
- The host-assigned entry filename proves which registered participant the
  runtime accepted for that turn; it does not prove who controlled the remote
  device.
- Transport encryption protects the path; it does not hide conversation text
  from the authoritative host.

These limitations are compatible with a trusted research room. Naming them
keeps the security model honest and leaves room for stronger authentication
later without rewriting the turn engine.

## Proposed build slice for this week

The smallest useful build should prove that a participant on another machine
can join an authoritative space without remote filesystem access and without
changing room semantics.

### 1. Make hosting addresses explicit

Extend the host command with separate concepts for the local listen address and
the externally advertised base URL. Keep loopback as the default. Any
non-loopback exposure must be an explicit operator choice with clear output
about the trust boundary.

The exact flag names should be selected while implementing, but the model is:

```text
listen address     where the local gateway accepts connections
advertised URL     what the remote participant is told to fetch
```

This supports a private network, local reverse proxy, or tunnel without making
Substrate responsible for operating that network.

### 2. Add a versioned join packet

Generate a participant-specific, machine-readable packet and a concise human
handoff. Print the capability only during an explicit packet export; redact it
from ordinary command output, diagnostics, tests, and documentation. Prefer an
explicit file or stdin workflow over encouraging operators to paste live
secrets into commands that shells retain.

### 3. Add authenticated room discovery

A remote participant should not need an operator to type every thread slug.
Given its capability, it should be able to list only the rooms in which that
registered participant appears, with topic, status, current floor, and whether
the participant owns the turn.

Discovery remains a view of filesystem truth. It grants no membership and must
reload the space on every request.

### 4. Provide a minimal remote client loop

Build the smallest client adapter that can consume a join packet and perform:

- list rooms;
- check or wait for the floor;
- read full, incremental, and bounded transcript windows;
- write one entry or `pass` with a stale-write guard.

This may initially be a CLI surface. It should use authorization headers and
request bodies while adapting the existing domain operations rather than
introducing a second turn engine. A future TUI or harness skill can sit on the
same client package.

### 5. Make capability lifecycle deliberate

Define where host-side capability material lives, with restrictive file
permissions and outside publishable room history. Support at least rotation by
reissuing a participant packet. If durable revocation is too large for the first
slice, say so visibly and make stopping or restarting the host the conservative
recovery path.

### 6. Exercise the real boundary

The acceptance pass should use two actual machines or isolated network peers:

1. create a room on the host;
2. join one remote human or agent seat;
3. reject an unauthorized capability;
4. reject an off-floor write;
5. reject a write with a missing or stale thread version;
6. accept one correctly versioned remote entry;
7. lose or discard the response and prove a retry cannot duplicate the turn;
8. reconnect from the advertised cursor and receive only the delta;
9. rotate the participant capability and reject the old one;
10. stop the gateway, verify the room remains readable locally, restart it, and
   continue;
11. run the repository's raw HTTP, race, and base verification gates.

## Explicit non-goals for the first build

- distributed or peer-to-peer room storage;
- automatic merging of independently writable room copies;
- a hosted Substrate account service;
- direct remote shell or repository access;
- public-internet exposure by default;
- presence indicators or read receipts;
- file transfer through the conversation capability;
- end-to-end encryption that hides the transcript from the room host;
- hot host migration with uninterrupted writes;
- changing moderator or turn semantics to suit the network transport.

The network should make the room reachable, not make it a different kind of
room.
