use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

use serde::{Deserialize, Serialize};

use crate::error::{Result, SubstrateError};
use crate::name::Name;

/// Everything substrate writes for a space lives under `.substrate/` in the
/// project root — like `.git/`, one hidden directory, project untouched.
pub const SUBSTRATE_DIR: &str = ".substrate";
pub const SPACE_CONFIG_FILE: &str = ".substrate/config.yaml";
pub const THREADS_DIR: &str = ".substrate/threads";
pub const THREAD_CONFIG_FILE: &str = "config.yaml";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SpaceConfig {
    #[serde(default = "default_version")]
    pub version: u32,
    #[serde(default)]
    pub participants: Vec<Participant>,
}

fn default_version() -> u32 {
    1
}

impl Default for SpaceConfig {
    fn default() -> Self {
        SpaceConfig {
            version: 1,
            participants: Vec::new(),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Participant {
    pub name: Name,
    pub kind: ParticipantKind,
}

/// Descriptive metadata only (display, listings). The turn engine never
/// branches on kind: humans, agents, and anything else are peers in the room.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ParticipantKind {
    Human,
    Agent,
    Other,
}

impl std::fmt::Display for ParticipantKind {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match self {
            ParticipantKind::Human => "human",
            ParticipantKind::Agent => "agent",
            ParticipantKind::Other => "other",
        })
    }
}

impl std::str::FromStr for ParticipantKind {
    type Err = String;

    fn from_str(s: &str) -> std::result::Result<Self, String> {
        match s {
            "human" => Ok(ParticipantKind::Human),
            "agent" => Ok(ParticipantKind::Agent),
            "other" => Ok(ParticipantKind::Other),
            _ => Err(format!("unknown kind '{s}' (human|agent|other)")),
        }
    }
}

/// Handle to a space root directory. Every operation re-reads from disk:
/// the filesystem is the shared state for all processes in the room, so
/// nothing is cached.
#[derive(Debug, Clone)]
pub struct Space {
    root: PathBuf,
}

impl Space {
    /// Create a new space: the directory (if needed) plus `.substrate/`.
    pub fn init(root: &Path) -> Result<Space> {
        fs::create_dir_all(root.join(THREADS_DIR))?;
        let config_path = root.join(SPACE_CONFIG_FILE);
        if config_path.exists() {
            return Err(SubstrateError::Io(std::io::Error::new(
                std::io::ErrorKind::AlreadyExists,
                format!("{} already exists", config_path.display()),
            )));
        }
        let space = Space {
            root: root.to_path_buf(),
        };
        space.save_config(&SpaceConfig::default())?;
        Ok(space)
    }

    pub fn open(root: &Path) -> Result<Space> {
        if !root.join(SPACE_CONFIG_FILE).is_file() {
            return Err(SubstrateError::NotASpace(root.to_path_buf()));
        }
        Ok(Space {
            root: root.to_path_buf(),
        })
    }

    pub fn root(&self) -> &Path {
        &self.root
    }

    pub fn config(&self) -> Result<SpaceConfig> {
        let content = fs::read_to_string(self.root.join(SPACE_CONFIG_FILE))?;
        Ok(serde_norway::from_str(&content)?)
    }

    pub fn save_config(&self, config: &SpaceConfig) -> Result<()> {
        let yaml = serde_norway::to_string(config)?;
        write_atomic(&self.root.join(SPACE_CONFIG_FILE), &yaml)
    }

    pub fn add_participant(&self, name: Name, kind: ParticipantKind) -> Result<()> {
        let mut config = self.config()?;
        if config.participants.iter().any(|p| p.name == name) {
            return Err(SubstrateError::DuplicateParticipant(name));
        }
        config.participants.push(Participant { name, kind });
        self.save_config(&config)
    }

    pub fn participant(&self, name: &str) -> Result<Participant> {
        self.config()?
            .participants
            .into_iter()
            .find(|p| p.name.as_str() == name)
            .ok_or_else(|| SubstrateError::UnknownParticipant(name.to_string()))
    }

    /// The space's `.substrate/` directory — the only place substrate
    /// writes, and the right thing to file-watch.
    pub fn substrate_dir(&self) -> PathBuf {
        self.root.join(SUBSTRATE_DIR)
    }

    pub fn thread_dir(&self, thread: &Name) -> PathBuf {
        self.root.join(THREADS_DIR).join(thread.to_path_component())
    }

    /// All threads in the space: directories under `.substrate/threads/`
    /// containing a config.yaml.
    pub fn list_threads(&self) -> Result<Vec<Name>> {
        let mut found = Vec::new();
        let threads_root = self.root.join(THREADS_DIR);
        if !threads_root.is_dir() {
            return Ok(found); // hand-made or empty space: just no threads
        }
        for dir_entry in fs::read_dir(&threads_root)? {
            let dir_entry = dir_entry?;
            if !dir_entry.path().join(THREAD_CONFIG_FILE).is_file() {
                continue;
            }
            if let Some(name) = dir_entry
                .file_name()
                .to_str()
                .and_then(|s| Name::from_path_component(s).ok())
            {
                found.push(name);
            }
        }
        found.sort();
        Ok(found)
    }
}

static TMP_COUNTER: AtomicU64 = AtomicU64::new(0);

/// Write via dotfile temp + rename (atomic on APFS and other POSIX
/// filesystems). Readers skip non-entry filenames, so a torn write is
/// never visible to anyone in the room.
pub(crate) fn write_atomic(path: &Path, content: &str) -> Result<()> {
    let dir = path.parent().unwrap_or_else(|| Path::new("."));
    let tmp = dir.join(format!(
        ".tmp-{}-{}",
        std::process::id(),
        TMP_COUNTER.fetch_add(1, Ordering::Relaxed)
    ));
    fs::write(&tmp, content)?;
    fs::rename(&tmp, path)?;
    Ok(())
}
