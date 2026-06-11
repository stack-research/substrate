//! The turn engine: write validation, no-op handling, quiet-skipping,
//! and moderator operations. Stateless between calls — every function is
//! load → validate → mutate → save against the filesystem.

use std::collections::BTreeMap;

use chrono::{Duration, Utc};

use crate::entry;
use crate::error::{Result, SubstrateError};
use crate::name::Name;
use crate::space::{write_atomic, Space};
use crate::thread::{ThreadConfig, ThreadStatus};

#[derive(Debug, Clone)]
pub struct TurnStatus {
    pub thread: Name,
    pub topic: String,
    pub status: ThreadStatus,
    pub current: Name,
    pub moderator: Name,
    /// True when the moderator holds the floor (the room is waiting).
    pub paused: bool,
    pub turn_order: Vec<Name>,
    pub quieted: BTreeMap<Name, u32>,
}

pub fn turn_status(space: &Space, thread: &Name) -> Result<TurnStatus> {
    let config = ThreadConfig::load(space, thread)?;
    Ok(TurnStatus {
        thread: thread.clone(),
        topic: config.topic.clone(),
        status: config.status,
        current: config.current().clone(),
        moderator: config.moderator.clone(),
        paused: config.is_paused(),
        turn_order: config.turn_order.clone(),
        quieted: config.quieted.clone(),
    })
}

#[derive(Debug, Clone)]
pub struct Written {
    pub filename: String,
    pub no_op: bool,
    pub next: Name,
    pub paused: bool,
}

/// Write one entry to the room, on the author's turn only.
///
/// Exact "no-op"/"pass"/"..." replies become `__no-op` files: still written,
/// still advancing the turn, but invisible to readers. The runtime picks the
/// filename — content can never impersonate another participant.
pub fn write_entry(space: &Space, thread: &Name, author: &Name, content: &str) -> Result<Written> {
    let mut config = ThreadConfig::load(space, thread)?;
    if config.status == ThreadStatus::Ended {
        return Err(SubstrateError::Ended);
    }
    if !config.turn_order.contains(author) {
        return Err(SubstrateError::NotInThread(author.clone()));
    }
    if config.current() != author {
        return Err(SubstrateError::NotYourTurn {
            current: config.current().clone(),
        });
    }

    let no_op = entry::is_no_op(content);
    let dir = space.thread_dir(thread);
    let timestamp = next_timestamp(&dir)?;
    let filename = entry::entry_filename(timestamp, author, no_op);
    let meta = entry::EntryMeta {
        author: author.clone(),
        timestamp,
    };
    write_atomic(
        &dir.join(&filename),
        &entry::render_file(&meta, content.trim()),
    )?;

    advance(&mut config);
    config.save(space, thread)?;
    Ok(Written {
        filename,
        no_op,
        next: config.current().clone(),
        paused: config.is_paused(),
    })
}

/// Pick a timestamp strictly after every existing entry in the room.
///
/// Wall-clock alone isn't enough: sub-millisecond writes would collide, and
/// bumping only the colliding file could push one author's filename ahead of
/// the clock, letting a later entry by someone else sort before it. Strict
/// per-thread monotonicity keeps filename order == write order.
fn next_timestamp(dir: &std::path::Path) -> Result<chrono::DateTime<Utc>> {
    let mut last: Option<chrono::DateTime<Utc>> = None;
    for dir_entry in std::fs::read_dir(dir)? {
        if let Some(filename) = dir_entry?.file_name().to_str() {
            if let Some((t, _, _)) = entry::parse_filename(filename) {
                last = Some(last.map_or(t, |l| l.max(t)));
            }
        }
    }
    // Truncate to the millisecond precision the filename will carry.
    let now = entry::parse_timestamp(&entry::format_timestamp(Utc::now()))
        .expect("own format always parses");
    Ok(match last {
        Some(l) if now <= l => l + Duration::milliseconds(1),
        _ => now,
    })
}

/// Move the floor to the next participant, skipping quieted ones and
/// decrementing their counters. The moderator can never be quieted, so this
/// always lands somewhere.
fn advance(config: &mut ThreadConfig) {
    let len = config.turn_order.len();
    config.next_index = (config.next_index + 1) % len;
    for _ in 0..len {
        let current = config.turn_order[config.next_index].clone();
        match config.quieted.get_mut(&current) {
            Some(remaining) if *remaining > 0 => {
                *remaining -= 1;
                if *remaining == 0 {
                    config.quieted.remove(&current);
                }
                config.next_index = (config.next_index + 1) % len;
            }
            _ => break,
        }
    }
}

// ---- Moderator operations ----------------------------------------------
//
// All legal only while the moderator holds the floor. They do not consume
// the turn: the moderator chains adjustments during one pause and ends
// their turn by writing an entry (or a "pass" no-op).

fn load_for_moderator(space: &Space, thread: &Name) -> Result<ThreadConfig> {
    let config = ThreadConfig::load(space, thread)?;
    if config.status == ThreadStatus::Ended {
        return Err(SubstrateError::Ended);
    }
    if config.current() != &config.moderator {
        return Err(SubstrateError::NotModeratorsTurn);
    }
    Ok(config)
}

pub fn set_topic(space: &Space, thread: &Name, topic: &str) -> Result<()> {
    let mut config = load_for_moderator(space, thread)?;
    config.topic = topic.to_string();
    config.save(space, thread)
}

/// Replace the speaking order. The moderator is force-prepended first; quiet
/// counters for anyone no longer in the room are dropped. The floor stays
/// with the moderator (index 0), so the new order takes effect when their
/// turn ends.
pub fn reorder_turns(space: &Space, thread: &Name, new_order: &[Name]) -> Result<()> {
    let mut config = load_for_moderator(space, thread)?;
    let mut turn_order: Vec<Name> = vec![config.moderator.clone()];
    for name in new_order {
        space.participant(name.as_str())?;
        if name != &config.moderator && !turn_order.contains(name) {
            turn_order.push(name.clone());
        }
    }
    if turn_order.len() < 2 {
        return Err(SubstrateError::TooFewParticipants);
    }
    config.quieted.retain(|name, _| turn_order.contains(name));
    config.next_index = 0;
    config.turn_order = turn_order;
    config.save(space, thread)
}

/// Quiet a participant for their next `turns` turns (skipped when reached).
pub fn quiet(space: &Space, thread: &Name, name: &Name, turns: u32) -> Result<()> {
    let mut config = load_for_moderator(space, thread)?;
    if name == &config.moderator {
        return Err(SubstrateError::CannotQuietModerator);
    }
    if !config.turn_order.contains(name) {
        return Err(SubstrateError::NotInThread(name.clone()));
    }
    if turns == 0 {
        config.quieted.remove(name);
    } else {
        config.quieted.insert(name.clone(), turns);
    }
    config.save(space, thread)
}

pub fn unquiet(space: &Space, thread: &Name, name: &Name) -> Result<()> {
    quiet(space, thread, name, 0)
}

/// Add a registered participant to the room, appended at the end of the
/// order so they speak last in the coming round. The floor stays with the
/// moderator (index 0).
pub fn invite(space: &Space, thread: &Name, name: &Name) -> Result<()> {
    let mut config = load_for_moderator(space, thread)?;
    space.participant(name.as_str())?;
    if config.turn_order.contains(name) {
        return Ok(()); // already in the room
    }
    config.turn_order.push(name.clone());
    config.save(space, thread)
}

/// Hand the floor directly to a participant — the conductor's baton to
/// `quiet`'s hush. Unlike the other moderator ops this works at ANY time,
/// not just on the moderator's pause: the round continues from the chosen
/// speaker in the existing order. Explicitly calling on a quieted
/// participant clears their quiet counter (explicit beats implicit).
/// Callers gate this on the moderator role — the floor itself may be
/// anywhere when the baton moves.
pub fn set_next(space: &Space, thread: &Name, name: &Name) -> Result<()> {
    let mut config = ThreadConfig::load(space, thread)?;
    if config.status == ThreadStatus::Ended {
        return Err(SubstrateError::Ended);
    }
    let Some(slot) = config.turn_order.iter().position(|n| n == name) else {
        return Err(SubstrateError::NotInThread(name.clone()));
    };
    config.next_index = slot;
    config.quieted.remove(name);
    config.save(space, thread)
}

/// End the thread. Entries stay on disk and readable forever; all writes
/// are rejected from here on.
pub fn end_thread(space: &Space, thread: &Name) -> Result<()> {
    let mut config = load_for_moderator(space, thread)?;
    config.status = ThreadStatus::Ended;
    config.save(space, thread)
}

/// Reopen an ended thread. The floor returns to the moderator (as at
/// creation) so the reopening entry can say why the room is back. Callers
/// gate this on the moderator role — an ended thread has no floor to check.
pub fn resume_thread(space: &Space, thread: &Name) -> Result<()> {
    let mut config = ThreadConfig::load(space, thread)?;
    if config.status != ThreadStatus::Ended {
        return Err(SubstrateError::NotEnded);
    }
    config.status = ThreadStatus::Active;
    config.next_index = 0; // moderator first, like a fresh opening
    config.save(space, thread)
}
