use std::collections::BTreeMap;
use std::fs;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::error::{Result, SubstrateError};
use crate::name::Name;
use crate::space::{Space, THREAD_CONFIG_FILE};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ThreadStatus {
    #[default]
    Active,
    Ended,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ThreadConfig {
    pub topic: String,
    pub created_at: DateTime<Utc>,
    pub moderator: Name,
    /// Every participant of the thread, any mix of kinds, moderator
    /// always first — a new room opens paused on them so they can lay out
    /// instructions beyond the topic line. turn_order[next_index] holds the
    /// floor.
    pub turn_order: Vec<Name>,
    #[serde(default)]
    pub next_index: usize,
    /// name -> remaining turns to skip.
    #[serde(default)]
    pub quieted: BTreeMap<Name, u32>,
    #[serde(default)]
    pub status: ThreadStatus,
}

impl ThreadConfig {
    pub fn load(space: &Space, thread: &Name) -> Result<Self> {
        let path = space.thread_dir(thread).join(THREAD_CONFIG_FILE);
        let content = fs::read_to_string(&path).map_err(|e| {
            if e.kind() == std::io::ErrorKind::NotFound {
                SubstrateError::UnknownThread(thread.to_string())
            } else {
                SubstrateError::Io(e)
            }
        })?;
        Ok(serde_norway::from_str(&content)?)
    }

    pub fn save(&self, space: &Space, thread: &Name) -> Result<()> {
        let yaml = serde_norway::to_string(self)?;
        crate::space::write_atomic(&space.thread_dir(thread).join(THREAD_CONFIG_FILE), &yaml)
    }

    /// Who holds the floor.
    pub fn current(&self) -> &Name {
        &self.turn_order[self.next_index.min(self.turn_order.len() - 1)]
    }

    /// The thread pauses exactly when the moderator holds the floor —
    /// derived, never stored.
    pub fn is_paused(&self) -> bool {
        self.status == ThreadStatus::Active && self.current() == &self.moderator
    }
}

/// Create a thread directory + thread.yaml.
///
/// `turns` is the speaking order; the moderator is force-prepended first
/// (and removed from wherever else they were listed), so the room opens
/// paused on them: their first entry sets instructions/context beyond the
/// topic line. Everyone must be a registered participant of the space, and
/// the room needs at least two.
pub fn create_thread(
    space: &Space,
    thread: &Name,
    topic: &str,
    moderator: &Name,
    turns: &[Name],
) -> Result<ThreadConfig> {
    let dir = space.thread_dir(thread);
    if dir.join(THREAD_CONFIG_FILE).exists() {
        return Err(SubstrateError::ThreadExists(thread.clone()));
    }

    space.participant(moderator.as_str())?;
    let mut turn_order: Vec<Name> = vec![moderator.clone()];
    for name in turns {
        space.participant(name.as_str())?;
        if name != moderator && !turn_order.contains(name) {
            turn_order.push(name.clone());
        }
    }
    if turn_order.len() < 2 {
        return Err(SubstrateError::TooFewParticipants);
    }

    let config = ThreadConfig {
        topic: topic.to_string(),
        created_at: Utc::now(),
        moderator: moderator.clone(),
        turn_order,
        next_index: 0,
        quieted: BTreeMap::new(),
        status: ThreadStatus::Active,
    };
    fs::create_dir_all(&dir)?;
    config.save(space, thread)?;
    Ok(config)
}
