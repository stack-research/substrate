use std::fmt;
use std::str::FromStr;

use serde::{Deserialize, Serialize};

use crate::error::{Result, SubstrateError};

pub const MAX_NAME_LEN: usize = 64;

/// A validated participant or thread name.
///
/// Names are lowercase `[a-z0-9-]`, start with an alphanumeric, and are at
/// most 64 chars. Underscores are excluded so `__` stays unambiguous as the
/// entry-filename field separator, and a leading `.` is impossible so dotfiles
/// (temp files, `.substrate/`) are never confused with real names.
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
        let first = s.chars().next().unwrap();
        if !first.is_ascii_lowercase() && !first.is_ascii_digit() {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "must start with a lowercase letter or digit",
            ));
        }
        if !s
            .chars()
            .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '-')
        {
            return Err(SubstrateError::InvalidName(
                s.into(),
                "only lowercase letters, digits, and '-' allowed",
            ));
        }
        Ok(Name(s.to_string()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
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
        for ok in ["user-name", "claude-a", "codex-b", "4o", "a", "x-1-y"] {
            assert!(Name::new(ok).is_ok(), "{ok} should be valid");
        }
    }

    #[test]
    fn rejects_invalid_names() {
        for bad in [
            "", "Pat", "claude_a", "-bob", ".bob", "bob!", "bob bob", "café",
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
}
