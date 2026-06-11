use chrono::{DateTime, NaiveDateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::name::Name;

/// Replies matching one of these (trimmed, case-insensitive) are no-op turns.
pub const NO_OP_TOKENS: [&str; 3] = ["no-op", "pass", "..."];

const NO_OP_SUFFIX: &str = "no-op";

/// Filename timestamps: fixed-width UTC with milliseconds, so lexicographic
/// order is chronological order and the string is filesystem-safe everywhere.
const TS_FORMAT: &str = "%Y%m%dT%H%M%S%3fZ";

pub fn is_no_op(content: &str) -> bool {
    let t = content.trim();
    NO_OP_TOKENS.iter().any(|tok| t.eq_ignore_ascii_case(tok))
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct EntryMeta {
    pub author: Name,
    pub timestamp: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    pub meta: EntryMeta,
    pub body: String,
    pub no_op: bool,
    pub filename: String,
}

pub fn format_timestamp(t: DateTime<Utc>) -> String {
    t.format(TS_FORMAT).to_string()
}

pub fn parse_timestamp(s: &str) -> Option<DateTime<Utc>> {
    NaiveDateTime::parse_from_str(s, TS_FORMAT)
        .ok()
        .map(|n| n.and_utc())
}

pub fn entry_filename(t: DateTime<Utc>, author: &Name, no_op: bool) -> String {
    let ts = format_timestamp(t);
    if no_op {
        format!("{ts}__{author}__{NO_OP_SUFFIX}.md")
    } else {
        format!("{ts}__{author}.md")
    }
}

/// Parse an entry filename into (timestamp, author, no_op).
///
/// Returns `None` for anything that isn't an entry file (thread.yaml,
/// dotfiles, hand-dropped strays). The author in the filename is authoritative:
/// the runtime writes it, so thread content can never impersonate.
pub fn parse_filename(filename: &str) -> Option<(DateTime<Utc>, Name, bool)> {
    let stem = filename.strip_suffix(".md")?;
    let parts: Vec<&str> = stem.split("__").collect();
    let (ts_part, name_part, no_op) = match parts.as_slice() {
        [ts, name] => (ts, name, false),
        [ts, name, suffix] if *suffix == NO_OP_SUFFIX => (ts, name, true),
        _ => return None,
    };
    let timestamp = parse_timestamp(ts_part)?;
    let author = Name::new(name_part).ok()?;
    Some((timestamp, author, no_op))
}

/// Render the on-disk file content: YAML frontmatter ("metadata headers" per
/// the spec) followed by the body.
pub fn render_file(meta: &EntryMeta, body: &str) -> String {
    let yaml = serde_norway::to_string(meta).expect("EntryMeta always serializes");
    format!("---\n{yaml}---\n\n{body}\n")
}

/// Parse an entry from its filename and file content.
///
/// The filename alone determines author, timestamp, and no-op status;
/// frontmatter is duplicate metadata for humans and git, and is simply
/// stripped from the body. Garbled or missing frontmatter never fails —
/// the whole content just becomes the body.
pub fn parse_file(filename: &str, content: &str) -> Option<Entry> {
    let (timestamp, author, no_op) = parse_filename(filename)?;
    let body = strip_frontmatter(content).trim().to_string();
    Some(Entry {
        meta: EntryMeta { author, timestamp },
        body,
        no_op,
        filename: filename.to_string(),
    })
}

fn strip_frontmatter(content: &str) -> &str {
    let Some(rest) = content.strip_prefix("---\n") else {
        return content;
    };
    match rest.find("\n---") {
        Some(end) => {
            let after = &rest[end + 4..];
            // The closing fence must end its line.
            match after.strip_prefix('\n') {
                Some(body) => body,
                None if after.is_empty() => "",
                None => content,
            }
        }
        None => content,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    fn ts(ms: i64) -> DateTime<Utc> {
        Utc.timestamp_millis_opt(1_770_000_000_000 + ms).unwrap()
    }

    fn name(s: &str) -> Name {
        Name::new(s).unwrap()
    }

    #[test]
    fn timestamp_roundtrip() {
        let t = ts(123);
        let s = format_timestamp(t);
        assert_eq!(s.len(), "20260609T203015123Z".len());
        assert_eq!(parse_timestamp(&s), Some(t));
    }

    #[test]
    fn filename_roundtrip() {
        let t = ts(42);
        let f = entry_filename(t, &name("claude-a"), false);
        assert_eq!(parse_filename(&f), Some((t, name("claude-a"), false)));

        let f = entry_filename(t, &name("codex-b"), true);
        assert!(f.ends_with("__codex-b__no-op.md"));
        assert_eq!(parse_filename(&f), Some((t, name("codex-b"), true)));
    }

    #[test]
    fn filename_rejects_strays() {
        for bad in [
            "thread.yaml",
            "notes.md",
            "20260609T203015123Z__Bad_Name.md",
            "20260609T203015123Z__a__b__c.md",
            "20260609T203015123Z__a__noop.md",
            "garbage__user-name.md",
            ".20260609T203015123Z__user-name.md.tmp",
        ] {
            assert_eq!(parse_filename(bad), None, "{bad} should not parse");
        }
    }

    #[test]
    fn shuffled_filenames_sort_chronologically() {
        let mut names: Vec<String> = [5, 3, 999, 0, 42, 100]
            .iter()
            .map(|ms| entry_filename(ts(*ms), &name("user-name"), false))
            .collect();
        names.sort();
        let parsed: Vec<i64> = names
            .iter()
            .map(|f| parse_filename(f).unwrap().0.timestamp_subsec_millis() as i64)
            .collect();
        assert_eq!(parsed, vec![0, 3, 5, 42, 100, 999]);
    }

    #[test]
    fn file_roundtrip_with_frontmatter() {
        let meta = EntryMeta {
            author: name("pat"),
            timestamp: ts(7),
        };
        let body = "First line.\n\nSecond paragraph with --- inside.";
        let rendered = render_file(&meta, body);
        assert!(rendered.starts_with("---\n"));

        let f = entry_filename(meta.timestamp, &meta.author, false);
        let entry = parse_file(&f, &rendered).unwrap();
        assert_eq!(entry.meta, meta);
        assert_eq!(entry.body, body);
        assert!(!entry.no_op);
    }

    #[test]
    fn file_without_frontmatter_is_tolerated() {
        let f = entry_filename(ts(0), &name("user-name"), false);
        let entry = parse_file(&f, "just a bare body\n").unwrap();
        assert_eq!(entry.body, "just a bare body");
        assert_eq!(entry.meta.author, name("user-name"));
    }

    #[test]
    fn no_op_detection() {
        for yes in ["no-op", "pass", "...", " PASS ", "No-Op", "...\n"] {
            assert!(is_no_op(yes), "{yes:?} should be a no-op");
        }
        for no in ["I'll pass", "no op", "….", "pass the salt", ""] {
            assert!(!is_no_op(no), "{no:?} should not be a no-op");
        }
    }
}
