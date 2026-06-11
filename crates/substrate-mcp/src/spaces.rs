//! One server, many spaces. Each space gets a label; tools address rooms as
//! (space, thread). Identity is still a single `--name` fixed at
//! launch, verified against each space's own registry — names are per-space,
//! the space is the trust boundary.
//!
//! The set of spaces is re-resolved from the registry on every tool call —
//! the filesystem is truth, including for "which spaces exist". A space
//! created mid-session (`substrate init`) is visible on the agent's next call.

use std::path::PathBuf;

use substrate_core::home::{label_for, SpacesRegistry};
use substrate_core::{Name, Space};

/// How the server finds its spaces: pinned paths from `--space`, or a
/// registry file re-read on every resolution.
#[derive(Clone)]
pub struct SpaceSource {
    paths: Vec<PathBuf>,
    registry_file: Option<PathBuf>,
}

impl SpaceSource {
    pub fn new(paths: Vec<PathBuf>, registry_file: Option<PathBuf>) -> Self {
        SpaceSource {
            paths,
            registry_file,
        }
    }

    pub fn describe(&self) -> String {
        if !self.paths.is_empty() {
            format!(
                "pinned: {}",
                self.paths
                    .iter()
                    .map(|p| p.display().to_string())
                    .collect::<Vec<_>>()
                    .join(", ")
            )
        } else {
            let path = self
                .registry_file
                .clone()
                .or_else(SpacesRegistry::default_path);
            format!(
                "registry: {}",
                path.map(|p| p.display().to_string())
                    .unwrap_or_else(|| "(none)".into())
            )
        }
    }

    /// Resolve the current set of spaces. An empty registry yields an empty
    /// set, not an error — spaces may be created later in the session.
    pub fn load(&self) -> anyhow::Result<SpaceSet> {
        let labeled: Vec<(String, PathBuf)> = if !self.paths.is_empty() {
            self.paths
                .iter()
                .map(|p| (label_for(p), p.clone()))
                .collect()
        } else {
            SpacesRegistry::load(self.registry_file.as_deref())?
                .spaces
                .into_iter()
                .collect()
        };

        let mut spaces = Vec::new();
        for (label, path) in labeled {
            anyhow::ensure!(
                Name::new(&label).is_ok(),
                "space label '{label}' is not a valid name — labels are lowercase \
                 a-z0-9- (registry file keys let you label {} explicitly)",
                path.display()
            );
            anyhow::ensure!(
                !spaces.iter().any(|s: &LabeledSpace| s.label == label),
                "duplicate space label '{label}'"
            );
            // A registry entry whose directory vanished shouldn't take the
            // whole server down — skip it; resolve() will report it missing.
            match Space::open(&path) {
                Ok(space) => spaces.push(LabeledSpace { label, space }),
                Err(e) => tracing::warn!(%label, "skipping unopenable space: {e}"),
            }
        }
        Ok(SpaceSet { spaces })
    }
}

pub struct LabeledSpace {
    pub label: String,
    pub space: Space,
}

pub struct SpaceSet {
    spaces: Vec<LabeledSpace>,
}

impl SpaceSet {
    pub fn iter(&self) -> impl Iterator<Item = &LabeledSpace> {
        self.spaces.iter()
    }

    pub fn labels(&self) -> Vec<&str> {
        self.spaces.iter().map(|s| s.label.as_str()).collect()
    }

    pub fn len(&self) -> usize {
        self.spaces.len()
    }

    pub fn is_empty(&self) -> bool {
        self.spaces.is_empty()
    }

    /// Resolve a tool's optional `space` argument. With one space configured
    /// the argument may be omitted; with several it must name one.
    pub fn resolve(&self, label: Option<&str>) -> Result<&Space, String> {
        match label {
            Some(label) => self
                .spaces
                .iter()
                .find(|s| s.label == label)
                .map(|s| &s.space)
                .ok_or_else(|| {
                    format!(
                        "unknown space '{label}' — configured spaces: {}",
                        self.labels().join(", ")
                    )
                }),
            None if self.spaces.len() == 1 => Ok(&self.spaces[0].space),
            None if self.spaces.is_empty() => {
                Err("no spaces exist yet — a moderator creates one with `substrate init`".into())
            }
            None => Err(format!(
                "several spaces are configured — pass `space`: {}",
                self.labels().join(", ")
            )),
        }
    }

    /// Which spaces the participant is registered in.
    pub fn registered_in(&self, me: &Name) -> Vec<&str> {
        self.spaces
            .iter()
            .filter(|s| s.space.participant(me.as_str()).is_ok())
            .map(|s| s.label.as_str())
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use std::path::Path;

    use super::*;
    use substrate_core::ParticipantKind;
    use tempfile::TempDir;

    fn make_space(dir: &Path, with: &[&str]) {
        let space = Space::init(dir).unwrap();
        for name in with {
            space
                .add_participant(Name::new(name).unwrap(), ParticipantKind::Agent)
                .unwrap();
        }
    }

    #[test]
    fn loads_explicit_paths_and_resolves() {
        let root = TempDir::new().unwrap();
        let a = root.path().join("lab-a");
        let b = root.path().join("lab-b");
        make_space(&a, &["cursor"]);
        make_space(&b, &["cursor", "claude-x"]);

        let set = SpaceSource::new(vec![a, b], None).load().unwrap();
        assert_eq!(set.labels(), vec!["lab-a", "lab-b"]);
        assert!(set.resolve(Some("lab-b")).is_ok());
        assert!(set.resolve(Some("nope")).is_err());
        assert!(set.resolve(None).is_err()); // ambiguous with two spaces

        let me = Name::new("claude-x").unwrap();
        assert_eq!(set.registered_in(&me), vec!["lab-b"]);
    }

    #[test]
    fn single_space_needs_no_label() {
        let root = TempDir::new().unwrap();
        let a = root.path().join("solo");
        make_space(&a, &["cursor"]);
        let set = SpaceSource::new(vec![a], None).load().unwrap();
        assert!(set.resolve(None).is_ok());
    }

    #[test]
    fn registry_grows_mid_session() {
        let root = TempDir::new().unwrap();
        let registry = root.path().join("spaces.yaml");
        let source = SpaceSource::new(vec![], Some(registry.clone()));

        // empty registry: empty set, helpful resolve error, no crash
        let set = source.load().unwrap();
        assert!(set.is_empty());
        assert!(set.resolve(None).unwrap_err().contains("substrate init"));

        // a space appears; the same source sees it on the next load
        let a = root.path().join("memory");
        make_space(&a, &["cursor"]);
        std::fs::write(&registry, format!("spaces:\n  memory: {}\n", a.display())).unwrap();
        let set = source.load().unwrap();
        assert_eq!(set.labels(), vec!["memory"]);
        assert!(set.resolve(None).is_ok());
    }

    #[test]
    fn vanished_space_is_skipped_not_fatal() {
        let root = TempDir::new().unwrap();
        let a = root.path().join("real");
        make_space(&a, &["cursor"]);
        let registry = root.path().join("spaces.yaml");
        std::fs::write(
            &registry,
            format!(
                "spaces:\n  real: {}\n  gone: {}\n",
                a.display(),
                root.path().join("gone").display()
            ),
        )
        .unwrap();

        let set = SpaceSource::new(vec![], Some(registry)).load().unwrap();
        assert_eq!(set.labels(), vec!["real"]);
    }
}
