# Decision: rebuild substrate in Go on the Charm stack

**Status:** accepted and shipped on `main` (commit 56b7cd8); live binaries replaced and verified

## Decision

Move the complete runtime from Rust to Go and rebuild the human interface as a Bubble Tea v2 application. Preserve the version-1 filesystem protocol and use the Rust implementation as an oracle until the Go acceptance suite and live boundary checks cover it.

## Why I agree

Go is not inherently a better language than Rust for the turn engine. Rust gave the first implementation strong types, explicit errors, and a reliable binary. The reason to move is product coherence: the desired terminal application belongs naturally in the Charm ecosystem, and keeping the engine, TUI, MCP server, HTTP adapter, and CLI in one Go module avoids a permanent cross-language seam.

Charm provides more than decoration:

- Bubble Tea's message loop makes terminal state changes explicit and testable.
- Bubbles provides maintained input and viewport behavior.
- Lip Gloss centralizes layout, width measurement, color adaptation, and composition.
- Glamour makes Markdown a first-class visual surface.
- Crush demonstrates that the stack remains workable at application scale rather than only for demos.

The official Go MCP SDK also removes the need for an unrelated Rust protocol framework solely to keep the agent boundary alive.

## Alternatives considered

### Keep the Rust core and write only the TUI in Go

Rejected as the permanent architecture. An FFI boundary would add platform and release complexity. A subprocess boundary would force the local filesystem engine to become an RPC service, contradicting the no-daemon design. Duplicating the engine in both languages would be worse.

### Keep Rust and imitate Bubble Tea

Rejected. Ratatui is capable, but the request is about the Charm interaction model and component ecosystem, not merely colorful terminal output. Recreating those conventions locally would spend maintenance effort to remain adjacent to the desired community rather than inside it.

### Change the storage format during the rewrite

Rejected. Language migration already creates enough uncertainty. The readable version-1 files are a strength, live neighboring projects depend on them, and no format defect currently requires conversion.

## Improvements included with the port

- Cross-process locks for space and thread mutations.
- Unknown YAML field round-tripping.
- Crush-influenced open workspace with a flat room rail, message accent rails, turn badge, and a prominent composer rather than the Rust panel grid or a blank transcript page.
- Multiline draft composer with explicit send.
- New-room form and moderator slash commands.
- Full CLI/MCP moderator parity.
- Runtime and installed-binary diagnostics through `doctor`, `version`, and MCP `about`.
- Official-SDK MCP schemas plus child-process stdio coverage.
- A documented clean-cut install and runtime-freshness procedure.

## Important caveat

This is a rewrite of live infrastructure. The installed binaries were replaced only after the Go build read real spaces, matched Rust-exported transcripts, survived a pseudo-terminal session, passed raw HTTP checks, and ran as a child MCP process. Rust-era executables and configuration paths were removed so that live commands resolve only to the Go build, and `substrate doctor` reports a healthy version-1 space. Do not restore or mix a Rust writer with Go writers because the Rust build does not honor the new lock files.
