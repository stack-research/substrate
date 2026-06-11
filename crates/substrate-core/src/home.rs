//! Machine-level configuration under `~/.substrate` (override with
//! `SUBSTRATE_HOME`): who you are, which spaces exist, your standing crew,
//! and how to run each agent. Spaces stay the trust boundary — everything
//! here is convenience for one machine, not authority over a space.

use std::collections::BTreeMap;
use std::fs;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::error::Result;
use crate::name::Name;
use crate::space::{write_atomic, Participant, ParticipantKind, Space};

pub const IDENTITY_FILE: &str = "identity.yaml";
pub const SPACES_FILE: &str = "spaces.yaml";
pub const PARTICIPANTS_FILE: &str = "participants.yaml";
pub const AGENTS_FILE: &str = "agents.yaml";

/// `$SUBSTRATE_HOME`, or `~/.substrate`.
pub fn substrate_home() -> Option<PathBuf> {
    if let Some(home) = std::env::var_os("SUBSTRATE_HOME") {
        return Some(PathBuf::from(home));
    }
    std::env::var_os("HOME").map(|home| PathBuf::from(home).join(".substrate"))
}

fn home_file(name: &str) -> Option<PathBuf> {
    substrate_home().map(|home| home.join(name))
}

// ---- identity: who is at this machine's keyboard --------------------------

#[derive(Debug, Serialize, Deserialize)]
struct Identity {
    name: Name,
}

/// The human identity stored once, ever (`identity.yaml`). `None` when the
/// question hasn't been asked yet.
pub fn load_identity() -> Option<Name> {
    let content = fs::read_to_string(home_file(IDENTITY_FILE)?).ok()?;
    serde_norway::from_str::<Identity>(&content)
        .ok()
        .map(|i| i.name)
}

pub fn save_identity(name: &Name) -> Result<()> {
    let Some(path) = home_file(IDENTITY_FILE) else {
        return Ok(()); // no home dir — nothing durable to write
    };
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }
    let yaml = serde_norway::to_string(&Identity { name: name.clone() })?;
    write_atomic(&path, &yaml)
}

// ---- spaces registry: which spaces this machine knows about ---------------

#[derive(Debug, Default, Serialize, Deserialize)]
pub struct SpacesRegistry {
    #[serde(default)]
    pub spaces: BTreeMap<String, PathBuf>,
}

impl SpacesRegistry {
    pub fn default_path() -> Option<PathBuf> {
        home_file(SPACES_FILE)
    }

    /// Missing file = empty registry, never an error.
    pub fn load(path: Option<&Path>) -> Result<Self> {
        let Some(path) = path.map(Path::to_path_buf).or_else(Self::default_path) else {
            return Ok(Self::default());
        };
        match fs::read_to_string(&path) {
            Ok(content) => Ok(serde_norway::from_str(&content)?),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Self::default()),
            Err(e) => Err(e.into()),
        }
    }

    pub fn save(&self, path: Option<&Path>) -> Result<()> {
        let Some(path) = path.map(Path::to_path_buf).or_else(Self::default_path) else {
            return Ok(());
        };
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)?;
        }
        let yaml = serde_norway::to_string(self)?;
        write_atomic(&path, &yaml)
    }

    /// Register a space under a label; an existing entry for the same path
    /// is left alone, a label collision with a different path gets a numeric
    /// suffix. Returns the label actually used.
    pub fn add(&mut self, label: &str, path: &Path) -> String {
        if let Some((existing, _)) = self.spaces.iter().find(|(_, p)| p.as_path() == path) {
            return existing.clone();
        }
        let mut label = label.to_string();
        let mut n = 2;
        while self.spaces.contains_key(&label) {
            label = format!("{label}-{n}");
            n += 1;
        }
        self.spaces.insert(label.clone(), path.to_path_buf());
        label
    }

    pub fn remove(&mut self, label: &str) -> bool {
        self.spaces.remove(label).is_some()
    }
}

/// Coerce a directory name into valid-label shape (lowercased, invalid runs
/// collapsed to '-') so ordinary directories never fail.
pub fn label_for(path: &Path) -> String {
    let raw = path.file_name().and_then(|n| n.to_str()).unwrap_or("space");
    let mut label = String::new();
    for c in raw.to_lowercase().chars() {
        if c.is_ascii_lowercase() || c.is_ascii_digit() {
            label.push(c);
        } else if !label.is_empty() && !label.ends_with('-') {
            label.push('-');
        }
    }
    let label = label.trim_end_matches('-').to_string();
    if label.is_empty() {
        "space".to_string()
    } else {
        label
    }
}

// ---- participants template: the standing crew ------------------------------

#[derive(Debug, Default, Serialize, Deserialize)]
struct ParticipantTemplate {
    #[serde(default)]
    participants: Vec<Participant>,
}

/// Crew seeded into every new space (`participants.yaml`). Missing = empty.
pub fn load_participant_template() -> Result<Vec<Participant>> {
    let Some(path) = home_file(PARTICIPANTS_FILE) else {
        return Ok(Vec::new());
    };
    match fs::read_to_string(&path) {
        Ok(content) => Ok(serde_norway::from_str::<ParticipantTemplate>(&content)?.participants),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Vec::new()),
        Err(e) => Err(e.into()),
    }
}

// ---- agent commands: how to run each agent on this machine -----------------

#[derive(Debug, Default, Serialize, Deserialize)]
struct AgentsFile {
    #[serde(default)]
    agents: BTreeMap<Name, AgentEntry>,
}

#[derive(Debug, Serialize, Deserialize)]
struct AgentEntry {
    run: String,
}

/// One-shot command per agent (`agents.yaml`), used by `substrate attend`.
pub fn load_agent_command(name: &Name) -> Result<Option<String>> {
    let Some(path) = home_file(AGENTS_FILE) else {
        return Ok(None);
    };
    match fs::read_to_string(&path) {
        Ok(content) => Ok(serde_norway::from_str::<AgentsFile>(&content)?
            .agents
            .remove(name)
            .map(|e| e.run)),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(e.into()),
    }
}

// ---- bootstrap: `substrate init` / the TUI wizard, one code path -----------

pub struct Bootstrapped {
    pub space: Space,
    pub label: String,
    pub seeded: Vec<Name>,
}

/// Make a directory a space the "git init" way: create substrate.yaml, seed
/// the standing crew from the template, register `me` as a human, and add
/// the space to the machine registry so home-level MCP registrations see it
/// on their next tool call.
pub fn bootstrap_space(root: &Path, me: Option<&Name>) -> Result<Bootstrapped> {
    let space = Space::init(root)?;

    let mut seeded = Vec::new();
    for participant in load_participant_template()? {
        if space
            .add_participant(participant.name.clone(), participant.kind)
            .is_ok()
        {
            seeded.push(participant.name);
        }
    }
    if let Some(me) = me {
        if space.participant(me.as_str()).is_err() {
            space.add_participant(me.clone(), ParticipantKind::Human)?;
            seeded.push(me.clone());
        }
    }

    let canonical = root.canonicalize().unwrap_or_else(|_| root.to_path_buf());
    let mut registry = SpacesRegistry::load(None)?;
    let label = registry.add(&label_for(&canonical), &canonical);
    registry.save(None)?;

    Ok(Bootstrapped {
        space,
        label,
        seeded,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    /// Serialize env-mutating tests.
    static ENV_LOCK: std::sync::Mutex<()> = std::sync::Mutex::new(());

    fn with_home<T>(f: impl FnOnce(&Path) -> T) -> T {
        let _guard = ENV_LOCK.lock().unwrap();
        let home = TempDir::new().unwrap();
        std::env::set_var("SUBSTRATE_HOME", home.path());
        let result = f(home.path());
        std::env::remove_var("SUBSTRATE_HOME");
        result
    }

    #[test]
    fn identity_roundtrip() {
        with_home(|_| {
            assert_eq!(load_identity(), None);
            save_identity(&Name::new("user-name").unwrap()).unwrap();
            assert_eq!(load_identity(), Some(Name::new("user-name").unwrap()));
        });
    }

    #[test]
    fn registry_add_remove_and_label_collisions() {
        with_home(|_| {
            let mut registry = SpacesRegistry::load(None).unwrap();
            assert!(registry.spaces.is_empty());

            let a = registry.add("lab", Path::new("/x/lab"));
            let same = registry.add("lab", Path::new("/x/lab")); // same path: no-op
            let other = registry.add("lab", Path::new("/y/lab")); // collision: suffixed
            assert_eq!(a, "lab");
            assert_eq!(same, "lab");
            assert_eq!(other, "lab-2");

            registry.save(None).unwrap();
            let reloaded = SpacesRegistry::load(None).unwrap();
            assert_eq!(reloaded.spaces.len(), 2);
            let mut reloaded = reloaded;
            assert!(reloaded.remove("lab-2"));
            assert!(!reloaded.remove("nope"));
        });
    }

    #[test]
    fn bootstrap_seeds_and_registers() {
        with_home(|home| {
            std::fs::write(
                home.join(PARTICIPANTS_FILE),
                "participants:\n  - name: claude-a\n    kind: agent\n",
            )
            .unwrap();

            let dir = TempDir::new().unwrap();
            let root = dir.path().join("My Lab");
            let me = Name::new("user-name").unwrap();
            let done = bootstrap_space(&root, Some(&me)).unwrap();

            assert_eq!(done.label, "my-lab");
            assert_eq!(done.seeded.len(), 2); // template crew + me
            assert!(done.space.participant("user-name").is_ok());
            assert!(done.space.participant("claude-a").is_ok());

            let registry = SpacesRegistry::load(None).unwrap();
            assert!(registry.spaces.contains_key("my-lab"));
        });
    }

    #[test]
    fn label_sanitization() {
        assert_eq!(label_for(Path::new("/x/My Lab")), "my-lab");
        assert_eq!(label_for(Path::new("/x/.tmpA1b2")), "tmpa1b2");
        assert_eq!(label_for(Path::new("/x/___")), "space");
    }
}
