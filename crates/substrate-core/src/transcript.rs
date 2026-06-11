//! One shared transcript for everyone in the room. No-op entries are
//! dropped at load time, rendering is deterministic, and entries are
//! append-only — so transcript line numbers never shift, giving agents a
//! stable incremental cursor (`from_line = last_total + 1`).

use std::fs;

use crate::entry::{self, Entry};
use crate::error::Result;
use crate::name::Name;
use crate::space::Space;

/// All real entries of a thread, chronological. No-op turns are
/// skipped per the spec; stray files and dotfiles are ignored.
pub fn load_entries(space: &Space, thread: &Name) -> Result<Vec<Entry>> {
    // Surface UnknownThread for a missing room rather than a bare IO error.
    crate::thread::ThreadConfig::load(space, thread)?;

    let dir = space.thread_dir(thread);
    let mut filenames: Vec<String> = Vec::new();
    for dir_entry in fs::read_dir(&dir)? {
        if let Some(filename) = dir_entry?.file_name().to_str() {
            filenames.push(filename.to_string());
        }
    }
    filenames.sort(); // lexicographic == chronological by filename design

    let mut entries = Vec::new();
    for filename in &filenames {
        let content = match fs::read_to_string(dir.join(filename)) {
            Ok(c) => c,
            Err(_) => continue, // raced with a writer; next reload catches it
        };
        if let Some(entry) = entry::parse_file(filename, &content) {
            if !entry.no_op {
                entries.push(entry);
            }
        }
    }
    Ok(entries)
}

/// Deterministic text rendering shared by every reader.
pub fn render_transcript(entries: &[Entry]) -> String {
    let mut out = String::new();
    for entry in entries {
        out.push_str(&format!(
            "[{} @ {}]\n",
            entry.meta.author,
            entry.meta.timestamp.format("%Y-%m-%dT%H:%M:%SZ")
        ));
        out.push_str(&entry.body);
        out.push_str("\n\n");
    }
    out
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Window {
    All,
    LastN(usize),
    /// 1-based; past the end yields an empty window, not an error.
    FromLine(usize),
}

/// Apply a line window to a rendered transcript. Returns the windowed text
/// and the total line count of the full transcript (the agent's cursor).
pub fn window(text: &str, window: Window) -> (String, usize) {
    let lines: Vec<&str> = text.lines().collect();
    let total = lines.len();
    let selected: &[&str] = match window {
        Window::All => &lines,
        Window::LastN(n) => &lines[total.saturating_sub(n)..],
        Window::FromLine(n) => &lines[n.saturating_sub(1).min(total)..],
    };
    (selected.join("\n"), total)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn window_boundaries() {
        let text = "a\nb\nc\nd\n";
        assert_eq!(window(text, Window::All), ("a\nb\nc\nd".into(), 4));
        assert_eq!(window(text, Window::LastN(2)), ("c\nd".into(), 4));
        assert_eq!(window(text, Window::LastN(0)), (String::new(), 4));
        assert_eq!(window(text, Window::LastN(99)), ("a\nb\nc\nd".into(), 4));
        assert_eq!(window(text, Window::FromLine(1)), ("a\nb\nc\nd".into(), 4));
        assert_eq!(window(text, Window::FromLine(3)), ("c\nd".into(), 4));
        assert_eq!(window(text, Window::FromLine(4)), ("d".into(), 4));
        assert_eq!(window(text, Window::FromLine(5)), (String::new(), 4));
        assert_eq!(window(text, Window::FromLine(999)), (String::new(), 4));
        assert_eq!(window(text, Window::FromLine(0)), ("a\nb\nc\nd".into(), 4));
        assert_eq!(window("", Window::All), (String::new(), 0));
    }
}
