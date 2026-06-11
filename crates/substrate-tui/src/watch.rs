//! Bridge notify file events into the tokio select loop. Any event in the
//! space means "reload" — at this scale, collapsing beats debouncing.

use std::path::Path;

use notify::{RecommendedWatcher, RecursiveMode, Watcher};
use tokio::sync::mpsc;

pub fn spawn(root: &Path) -> anyhow::Result<(RecommendedWatcher, mpsc::Receiver<()>)> {
    let (tx, rx) = mpsc::channel(4);
    let mut watcher = notify::recommended_watcher(move |result: notify::Result<notify::Event>| {
        if result.is_ok() {
            // a full channel already has a reload queued — drop, don't block
            let _ = tx.try_send(());
        }
    })?;
    watcher.watch(root, RecursiveMode::Recursive)?;
    Ok((watcher, rx))
}
