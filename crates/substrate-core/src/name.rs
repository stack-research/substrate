use std::fmt;
use std::str::FromStr;

use serde::{Deserialize, Serialize};

use crate::error::{Result, SubstrateError};

pub const MAX_NAME_LEN: usize = 64;
const PATH_SLASH_ESCAPE: &str = "%2F";

/// A validated participant or thread name.
///
/// Names are lowercase `[a-z0-9-/\.]`, start and end with an alphanumeric,
/// and are at most 64 chars. Underscores are excluded so `__` stays
/// unambiguous as the entry-filename field separator, and leading/traversal
/// dot forms are rejected so dotfiles and parent directories are never
/// confused with real names.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub struct Name(String);

impl Name {
    pub fn new(s: &str) -> Result<Self> {
        if s.is_empty() {
            return Err(SubstrateError::InvalidName(s.into(), "empty"));
        }
        if s.len() > MAX_NAME_LEN {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "longer than 64 chars",
            ));
        }
        if s.chars().any(char::is_control) {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "control characters not allowed",
            ));
        }
        let first = s.chars().next().unwrap();
        if first == '.' {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "must not start with '.'",
            ));
        }
        if !first.is_ascii_lowercase() && !first.is_ascii_digit() {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "must start with a lowercase letter or digit",
            ));
        }
        if s.ends_with(['/', '.', '-']) {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "must not end with '/', '.', or '-'",
            ));
        }
        if s.contains("..") {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "must not contain '..'",
            ));
        }
        if s.contains("//") {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "must not contain '//'",
            ));
        }
        if s.contains("__") {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "must not contain '__'",
            ));
        }
        if !s
            .chars()
            .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || matches!(c, '-' | '/' | '.'))
        {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "only lowercase letters, digits, '-', '/', and '.' allowed",
            ));
        }
        Ok(Name(s.to_string()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }

    /// Encode this name as one reversible filesystem path component.
    ///
    /// Canonical names may contain `/`, but path components must never contain
    /// a raw slash. `%` is not a legal name character, so `%2F` is an
    /// unambiguous token for round-tripping slash-bearing names from disk.
    pub fn to_path_component(&self) -> String {
        let component = self.0.replace('/', PATH_SLASH_ESCAPE);
        debug_assert!(!component.contains('/'));
        component
    }

    /// Decode a filesystem path component written by `to_path_component`.
    pub fn from_path_component(component: &str) -> Result<Self> {
        if component.contains('/') {
            return Err(SubstrateError::InvalidName(
                component.into(),
                "path component must not contain '/'",
            ));
        }

        let mut decoded = String::with_capacity(component.len());
        let mut chars = component.chars();
        while let Some(c) = chars.next() {
            if c != '%' {
                decoded.push(c);
                continue;
            }

            match (chars.next(), chars.next()) {
                (Some('2'), Some('F')) => decoded.push('/'),
                _ => {
                    return Err(SubstrateError::InvalidName(
                        component.into(),
                        "invalid filesystem escape",
                    ));
                }
            }
        }

        Name::new(&decoded)
    }
}

impl fmt::Display for Name {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl FromStr for Name {
    type Err = SubstrateError;

    fn from_str(s: &str) -> Result<Self> {
        Name::new(s)
    }
}

impl TryFrom<String> for Name {
    type Error = SubstrateError;

    fn try_from(s: String) -> Result<Self> {
        Name::new(&s)
    }
}

impl From<Name> for String {
    fn from(n: Name) -> String {
        n.0
    }
}

impl AsRef<str> for Name {
    fn as_ref(&self) -> &str {
        &self.0
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn accepts_valid_names() {
        for ok in [
            "user-name",
            "claude-a",
            "codex-b",
            "4o",
            "a",
            "x-1-y",
            "claude/opus-4.8",
            "cursor/glm-5.2",
            "claude/fable-5",
            "x.y",
        ] {
            assert!(Name::new(ok).is_ok(), "{ok} should be valid");
        }
    }

    #[test]
    fn rejects_invalid_names() {
        for bad in [
            "",
            "Pat",
            "claude_a",
            "-bob",
            ".bob",
            "bob!",
            "bob bob",
            "café",
            "../x",
            "a//b",
            "a__b",
            "/x",
            "x/",
            ".x",
            "x/..",
            "x.",
            "x-",
            "line\nbreak",
        ] {
            assert!(Name::new(bad).is_err(), "{bad:?} should be invalid");
        }
        assert!(Name::new(&"a".repeat(65)).is_err());
        assert!(Name::new(&"a".repeat(64)).is_ok());
    }

    #[test]
    fn serde_roundtrip_and_rejects_bad_input() {
        let n: Name = serde_norway::from_str("claude-a").unwrap();
        assert_eq!(n.as_str(), "claude-a");
        assert!(serde_norway::from_str::<Name>("Not A Name").is_err());
        assert_eq!(serde_norway::to_string(&n).unwrap().trim(), "claude-a");
    }

    #[test]
    fn path_component_roundtrip() {
        let name = Name::new("claude/opus-4.8").unwrap();
        let component = name.to_path_component();
        assert_eq!(component, "claude%2Fopus-4.8");
        assert!(!component.contains('/'));
        assert_eq!(Name::from_path_component(&component).unwrap(), name);
        assert!(Name::from_path_component("claude%2fopus-4.8").is_err());
        assert!(Name::from_path_component("bad/path").is_err());
    }
}
