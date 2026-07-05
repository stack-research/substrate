//! Non-TUI subcommands: thin wrappers over substrate-core, scriptable output.

use std::path::Path;

use anyhow::Result;
use substrate_core::{
    home, thread, transcript, transcript::Window, turn, Name, ParticipantKind, Space,
};

pub fn init(root: &Path) -> Result<()> {
    let me = home::load_identity();
    let done = home::bootstrap_space(root, me.as_ref())?;
    println!("initialized space '{}' at {}", done.label, root.display());
    if !done.seeded.is_empty() {
        let names: Vec<&str> = done.seeded.iter().map(Name::as_str).collect();
        println!("seeded participants: {}", names.join(", "));
    }
    println!("registered in the machine registry — agents can already see it");
    Ok(())
}

pub fn spaces_list() -> Result<()> {
    let registry = home::SpacesRegistry::load(None)?;
    if registry.spaces.is_empty() {
        println!("no spaces registered — `substrate init` in a project, or `substrate spaces add`");
        return Ok(());
    }
    for (label, path) in &registry.spaces {
        let state = match Space::open(path) {
            Ok(space) => format!("{} thread(s)", space.list_threads()?.len()),
            Err(_) => "unopenable".to_string(),
        };
        println!("{label:<20} {} ({state})", path.display());
    }
    Ok(())
}

pub fn spaces_add(path: &Path, label: Option<&str>) -> Result<()> {
    Space::open(path)?; // must already be a space
    let canonical = path.canonicalize()?;
    let mut registry = home::SpacesRegistry::load(None)?;
    let label = registry.add(
        &label
            .map(str::to_string)
            .unwrap_or_else(|| home::label_for(&canonical)),
        &canonical,
    );
    registry.save(None)?;
    println!("registered '{label}' -> {}", canonical.display());
    Ok(())
}

pub fn spaces_remove(label: &str) -> Result<()> {
    let mut registry = home::SpacesRegistry::load(None)?;
    anyhow::ensure!(registry.remove(label), "no space labeled '{label}'");
    registry.save(None)?;
    println!("removed '{label}' from the registry (directory untouched)");
    Ok(())
}

pub fn add(root: &Path, name: &str, kind: ParticipantKind) -> Result<()> {
    let space = Space::open(root)?;
    space.add_participant(Name::new(name)?, kind)?;
    println!("added {kind} '{name}'");
    Ok(())
}

pub fn new_thread(
    root: &Path,
    name: &str,
    topic: &str,
    moderator: &str,
    turns: &[String],
) -> Result<()> {
    let space = Space::open(root)?;
    let turn_names: Vec<Name> = turns
        .iter()
        .map(|t| Name::new(t.trim()))
        .collect::<substrate_core::Result<_>>()?;
    let config = thread::create_thread(
        &space,
        &Name::new(name)?,
        topic,
        &Name::new(moderator)?,
        &turn_names,
    )?;
    let order: Vec<&str> = config.turn_order.iter().map(Name::as_str).collect();
    println!("created '{name}' — topic: {topic}");
    println!(
        "turns: {} (moderator first — your opening entry sets the instructions)",
        order.join(" → ")
    );
    Ok(())
}

pub fn status(root: &Path, thread: Option<&str>) -> Result<()> {
    let space = Space::open(root)?;
    match thread {
        Some(conv) => thread_status(&space, &Name::new(conv)?),
        None => space_status(&space),
    }
}

fn space_status(space: &Space) -> Result<()> {
    let config = space.config()?;
    println!("participants:");
    for p in &config.participants {
        println!("  {} ({})", p.name, p.kind);
    }
    let threads = space.list_threads()?;
    if threads.is_empty() {
        println!("no threads yet — try `substrate new`");
        return Ok(());
    }
    println!("threads:");
    for conv in threads {
        let s = turn::turn_status(space, &conv)?;
        println!(
            "  {} — {:?}, turn: {}{} — {}",
            conv,
            s.status,
            s.current,
            if s.paused {
                " (paused on moderator)"
            } else {
                ""
            },
            s.topic,
        );
    }
    Ok(())
}

fn thread_status(space: &Space, conv: &Name) -> Result<()> {
    let s = turn::turn_status(space, conv)?;
    let rendered = transcript::render_transcript(&transcript::load_entries(space, conv)?);
    let (_, total_lines) = transcript::window(&rendered, Window::All);

    println!("thread: {conv}");
    println!("topic: {}", s.topic);
    println!("status: {:?}", s.status);
    println!(
        "turn: {}{}",
        s.current,
        if s.paused {
            " (moderator — paused)"
        } else {
            ""
        }
    );
    let order: Vec<String> = s
        .turn_order
        .iter()
        .map(|name| {
            let mut label = name.to_string();
            if name == &s.moderator {
                label.push_str(" [mod]");
            }
            if let Some(remaining) = s.quieted.get(name) {
                label.push_str(&format!(" [quiet {remaining}]"));
            }
            if name == &s.current {
                label = format!("*{label}");
            }
            label
        })
        .collect();
    println!("order: {}", order.join(" → "));
    println!("transcript lines: {total_lines}");
    Ok(())
}

pub fn write(
    root: &Path,
    thread: &str,
    author: &str,
    message: Option<&str>,
    stdin: bool,
    file: Option<&Path>,
) -> Result<()> {
    let content = match (message, stdin, file) {
        (Some(m), false, None) => m.to_string(),
        (None, true, None) => {
            use std::io::Read;
            let mut buffer = String::new();
            std::io::stdin().read_to_string(&mut buffer)?;
            buffer
        }
        (None, false, Some(path)) => std::fs::read_to_string(path)?,
        _ => anyhow::bail!("pass exactly one of -m, --stdin, or --file"),
    };
    let space = Space::open(root)?;
    let written = turn::write_entry(&space, &Name::new(thread)?, &Name::new(author)?, &content)?;
    println!(
        "recorded {}{} — next: {}{}",
        written.filename,
        if written.no_op { " (no-op)" } else { "" },
        written.next,
        if written.paused {
            " (moderator — paused)"
        } else {
            ""
        },
    );
    Ok(())
}

/// The courier packet on stdout: pipe to pbcopy, paste to a web-only
/// assistant. Same text the HTTP brief serves, minus the write-back URL.
pub fn brief(root: &Path, thread: &str, for_name: Option<&str>) -> Result<()> {
    let space = Space::open(root)?;
    let for_name = for_name.map(Name::new).transpose()?;
    print!(
        "{}",
        crate::serve::brief_text(&space, &Name::new(thread)?, for_name.as_ref(), None)?
    );
    if for_name.is_some() {
        println!(
            "\nReply with plain ASCII markdown addressed to the whole thread. Use \
             printable ASCII characters plus normal line breaks; avoid Unicode, smart \
             quotes, decorative symbols, and invisible characters. Reply exactly \
             'pass' if you have nothing to add."
        );
    }
    Ok(())
}

/// HTTP transport adapter for proxied participants. See serve.rs.
pub fn serve(root: &Path, port: u16, proxies: &[String], key: Option<String>) -> Result<()> {
    let space = Space::open(root)?;
    anyhow::ensure!(
        key.is_none() || proxies.len() == 1,
        "--key only makes sense with exactly one --proxy"
    );
    let proxies = proxies
        .iter()
        .map(|name| {
            let name = Name::new(name)?;
            space.participant(name.as_str())?; // must be registered here
            Ok(crate::serve::Proxy {
                name,
                key: key.clone().unwrap_or_else(crate::serve::random_key),
            })
        })
        .collect::<Result<Vec<_>>>()?;
    crate::serve::serve(space, port, proxies)
}

/// Be an agent's hands: watch every registered space, and whenever the floor
/// reaches `name` in any active thread, run that agent's one-shot
/// command. The transcript is the agent's context, so each turn can be a
/// fresh session — the loop lives here, outside the model, where it's free.
pub fn attend(name: &str, exec: Option<&str>) -> Result<()> {
    use notify::Watcher;
    use std::collections::{HashMap, HashSet};
    use std::time::{Duration, Instant};
    use substrate_core::ThreadStatus;

    const RETRY_AFTER: Duration = Duration::from_secs(60);

    let me = Name::new(name)?;
    let command = match exec.map(str::to_string) {
        Some(c) => c,
        None => home::load_agent_command(&me)?.ok_or_else(|| {
            anyhow::anyhow!(
                "no command configured for '{me}' — add it to ~/.substrate/agents.yaml:\n\
                 agents:\n  {me}:\n    run: <one-shot harness command using $SUBSTRATE_PROMPT>\n\
                 or pass --exec"
            )
        })?,
    };
    println!("attending as {me} — command: {command}");

    let (tx, rx) = std::sync::mpsc::channel::<()>();
    let mut watcher = notify::recommended_watcher(move |r: notify::Result<notify::Event>| {
        if r.is_ok() {
            let _ = tx.send(());
        }
    })?;
    if let Some(home_dir) = home::substrate_home() {
        let _ = watcher.watch(&home_dir, notify::RecursiveMode::NonRecursive);
    }

    let mut watched: HashSet<std::path::PathBuf> = HashSet::new();
    // (when we last ran, how many entries the thread had then) — a changed
    // entry count means the floor came back legitimately; an unchanged one
    // within the window means our attempt failed, so back off.
    let mut last_run: HashMap<(String, Name), (Instant, usize)> = HashMap::new();

    loop {
        // the registry is truth, re-read every cycle — new spaces just appear
        let registry = home::SpacesRegistry::load(None)?;
        for (label, path) in &registry.spaces {
            let Ok(space) = Space::open(path) else {
                continue;
            };
            if watched.insert(path.clone()) {
                let _ = watcher.watch(&space.substrate_dir(), notify::RecursiveMode::Recursive);
            }
            for conv in space.list_threads()? {
                let Ok(status) = turn::turn_status(&space, &conv) else {
                    continue;
                };
                if status.status != ThreadStatus::Active || status.current != me {
                    continue;
                }
                let entries = std::fs::read_dir(space.thread_dir(&conv))?
                    .filter_map(|e| e.ok())
                    .filter_map(|e| e.file_name().into_string().ok())
                    .filter(|f| substrate_core::entry::parse_filename(f).is_some())
                    .count();
                let key = (label.clone(), conv.clone());
                if last_run
                    .get(&key)
                    .is_some_and(|(at, count)| *count == entries && at.elapsed() < RETRY_AFTER)
                {
                    continue; // same thread state as our failed attempt; back off
                }
                last_run.insert(key, (Instant::now(), entries));

                println!("[{label}/{conv}] the floor is {me}'s — running agent");
                let prompt = format!(
                    "You are participant '{me}' in substrate. It is your turn in \
                     thread '{conv}' (space '{label}', topic: {topic}). Use the \
                     substrate MCP tools: read_thread to catch up, then \
                     write_entry with your reply — or exactly 'pass' if you have \
                     nothing to add. Take only this one turn, then stop.",
                    topic = status.topic
                );
                let result = std::process::Command::new("sh")
                    .arg("-c")
                    .arg(&command)
                    .env("SUBSTRATE_PROMPT", &prompt)
                    .env("SUBSTRATE_SPACE", path)
                    .env("SUBSTRATE_SPACE_LABEL", label)
                    .env("SUBSTRATE_THREAD", conv.as_str())
                    .env("SUBSTRATE_TOPIC", &status.topic)
                    .status();
                match result {
                    Ok(code) if code.success() => println!("[{label}/{conv}] agent done"),
                    Ok(code) => eprintln!("[{label}/{conv}] agent exited with {code}"),
                    Err(e) => eprintln!("[{label}/{conv}] failed to run command: {e}"),
                }
            }
        }
        // wake on any file event; re-scan every 10s regardless
        let _ = rx.recv_timeout(Duration::from_secs(10));
        while rx.try_recv().is_ok() {} // collapse bursts
    }
}

/// Watch a thread and report floor changes — the poll→push bridge.
/// Emits one line per change (always including the initial state and the
/// end), optionally filtered to one participant, optionally running a hook
/// command so any harness can be nudged its own way.
pub fn watch(root: &Path, thread: &str, for_name: Option<&str>, exec: Option<&str>) -> Result<()> {
    use notify::Watcher;
    use substrate_core::ThreadStatus;

    let space = Space::open(root)?;
    let conv = Name::new(thread)?;
    let for_name = for_name.map(Name::new).transpose()?;

    let (tx, rx) = std::sync::mpsc::channel::<()>();
    let mut watcher = notify::recommended_watcher(move |r: notify::Result<notify::Event>| {
        if r.is_ok() {
            let _ = tx.send(());
        }
    })?;
    watcher.watch(
        &space.thread_dir(&conv),
        notify::RecursiveMode::NonRecursive,
    )?;

    let mut last: Option<(Name, ThreadStatus)> = None;
    loop {
        let status = turn::turn_status(&space, &conv)?;
        let state = (status.current.clone(), status.status);
        if last.as_ref() != Some(&state) {
            let ended = status.status == ThreadStatus::Ended;
            let relevant = ended || for_name.as_ref().is_none_or(|n| n == &status.current);
            if relevant {
                if ended {
                    println!("{conv}: ended");
                } else {
                    println!(
                        "{conv}: turn {}{}",
                        status.current,
                        if status.paused {
                            " (moderator — paused)"
                        } else {
                            ""
                        }
                    );
                }
                if let Some(cmd) = exec {
                    let result = std::process::Command::new("sh")
                        .arg("-c")
                        .arg(cmd)
                        .env("SUBSTRATE_SPACE", root)
                        .env("SUBSTRATE_THREAD", conv.as_str())
                        .env("SUBSTRATE_TURN", status.current.as_str())
                        .env("SUBSTRATE_STATUS", if ended { "ended" } else { "active" })
                        .env("SUBSTRATE_TOPIC", &status.topic)
                        .status();
                    if let Err(e) = result {
                        eprintln!("exec failed: {e}");
                    }
                }
            }
            last = Some(state);
        }
        if status.status == ThreadStatus::Ended {
            return Ok(());
        }
        // wake on a file event, re-check every 15s regardless
        let _ = rx.recv_timeout(std::time::Duration::from_secs(15));
        while rx.try_recv().is_ok() {} // collapse bursts
    }
}

pub fn read(root: &Path, thread: &str, last: Option<usize>, from: Option<usize>) -> Result<()> {
    anyhow::ensure!(
        last.is_none() || from.is_none(),
        "--last and --from are mutually exclusive"
    );
    let space = Space::open(root)?;
    let conv = Name::new(thread)?;
    let rendered = transcript::render_transcript(&transcript::load_entries(&space, &conv)?);
    let window = match (last, from) {
        (Some(n), None) => Window::LastN(n),
        (None, Some(n)) => Window::FromLine(n),
        _ => Window::All,
    };
    let (text, _) = transcript::window(&rendered, window);
    println!("{text}");
    Ok(())
}
