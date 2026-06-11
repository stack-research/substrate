use std::path::PathBuf;

use crate::name::Name;

pub type Result<T> = std::result::Result<T, SubstrateError>;

#[derive(Debug, thiserror::Error)]
pub enum SubstrateError {
    #[error("invalid name '{0}': {1}")]
    InvalidName(String, &'static str),

    #[error("'{0}' is already registered in this space")]
    DuplicateParticipant(Name),

    #[error("'{0}' is not a registered participant")]
    UnknownParticipant(String),

    #[error("thread '{0}' not found")]
    UnknownThread(String),

    #[error("thread '{0}' already exists")]
    ThreadExists(Name),

    #[error("not your turn: '{current}' holds the floor")]
    NotYourTurn { current: Name },

    #[error("'{0}' is not a participant in this thread")]
    NotInThread(Name),

    #[error("the thread has ended; no further entries")]
    Ended,

    #[error("the thread is still active — nothing to resume")]
    NotEnded,

    #[error("only the moderator may do that, and only on the moderator's turn")]
    NotModeratorsTurn,

    #[error("the moderator cannot be quieted")]
    CannotQuietModerator,

    #[error("a thread needs at least two participants")]
    TooFewParticipants,

    #[error("not a substrate space (no .substrate/config.yaml): {0}")]
    NotASpace(PathBuf),

    #[error("io error: {0}")]
    Io(#[from] std::io::Error),

    #[error("yaml error: {0}")]
    Yaml(#[from] serde_norway::Error),
}
