//! substrate-core: data model, storage, and turn engine for substrate.
//!
//! Local-first, turn-based group threads. A directory is a thread,
//! a markdown file is one entry, and the filesystem is the shared state for
//! every process in the room (one TUI per human, one MCP server per agent).

pub mod entry;
pub mod error;
pub mod home;
pub mod name;
pub mod space;
pub mod thread;
pub mod transcript;
pub mod turn;

pub use entry::{Entry, EntryMeta};
pub use error::{Result, SubstrateError};
pub use name::Name;
pub use space::{Participant, ParticipantKind, Space, SpaceConfig};
pub use thread::{ThreadConfig, ThreadStatus};
