//! Terminal lifecycle and the main event loop (coven-tui-v2's shape):
//! a blocking crossterm reader thread feeds a channel; tokio::select! over
//! keys, file-watch events, and a tick; all I/O lives here, not in App.

use std::io::{self, Stdout};
use std::path::{Path, PathBuf};
use std::time::Duration;

use anyhow::{Context, Result};
use crossterm::{
    event::{self, Event, KeyCode, KeyEvent, KeyEventKind},
    execute,
    terminal::{disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen},
};
use ratatui::prelude::*;
use substrate_core::{home, turn, Name, ParticipantKind, Space};
use tokio::sync::mpsc;

use crate::app::{Action, App};
use crate::commands::{self, SlashCommand};
use crate::{ui, watch};

pub fn run(root: PathBuf, name_flag: Option<String>) -> Result<()> {
    let name_flag = name_flag.map(|n| Name::new(&n)).transpose()?;
    let rt = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()?;
    rt.block_on(run_app(root, name_flag))
}

async fn run_app(root: PathBuf, name_flag: Option<Name>) -> Result<()> {
    let mut terminal = setup_terminal()?;

    let original_hook = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |panic_info| {
        let _ = restore_terminal_basic();
        original_hook(panic_info);
    }));

    // Orphan watchdog: if our process tree dies (terminal window closed,
    // spawning script crashed) we get reparented to PID 1 — and a TUI with
    // no terminal must exit, not spin. The event loop can't see this case:
    // crossterm's read spins internally on a dead pty and draw writes can
    // wedge, so an independent thread checks and pulls the plug.
    std::thread::spawn(|| loop {
        std::thread::sleep(Duration::from_secs(2));
        if std::os::unix::process::parent_id() == 1 {
            let _ = restore_terminal_basic();
            std::process::exit(0);
        }
    });

    let (key_tx, mut key_rx) = mpsc::channel::<KeyEvent>(32);
    let _input_task = spawn_input_task(key_tx);

    let result = match preflight(&mut terminal, &mut key_rx, &root, name_flag).await {
        Ok(Some((space, me, note))) => main_loop(&mut terminal, &mut key_rx, space, me, note).await,
        Ok(None) => Ok(()), // user declined the wizard or quit
        Err(e) => Err(e),
    };
    restore_terminal(&mut terminal)?;
    result
}

/// Everything before the thread: open the space or run the "git init"-style
/// wizard, and figure out who is at the keyboard (asked once, ever).
async fn preflight(
    terminal: &mut Terminal<CrosstermBackend<Stdout>>,
    key_rx: &mut mpsc::Receiver<KeyEvent>,
    root: &Path,
    name_flag: Option<Name>,
) -> Result<Option<(Space, Name, Option<String>)>> {
    let known_me = || name_flag.clone().or_else(home::load_identity);

    let Ok(space) = Space::open(root) else {
        // wizard step 1: explicit consent — never create on a typo
        loop {
            terminal.draw(|f| ui::wizard::draw_confirm_create(f, root))?;
            match key_rx.recv().await.map(|k| k.code) {
                Some(KeyCode::Char('y') | KeyCode::Enter) => break,
                Some(KeyCode::Char('n') | KeyCode::Char('q') | KeyCode::Esc) | None => {
                    return Ok(None)
                }
                _ => {}
            }
        }
        // wizard step 2: who are you (skipped when already known)
        let me = match known_me() {
            Some(me) => me,
            None => match ask_name(terminal, key_rx).await? {
                Some(me) => me,
                None => return Ok(None),
            },
        };
        let done = home::bootstrap_space(root, Some(&me))?;
        if home::load_identity().is_none() {
            let _ = home::save_identity(&me);
        }
        let note = format!(
            "space '{}' created and registered — agents can already see it · n: new thread",
            done.label
        );
        return Ok(Some((done.space, me, Some(note))));
    };

    // existing space: --name wins, then identity, then a lone human, then ask
    let me = if let Some(me) = known_me() {
        me
    } else {
        let humans: Vec<Name> = space
            .config()?
            .participants
            .into_iter()
            .filter(|p| p.kind == ParticipantKind::Human)
            .map(|p| p.name)
            .collect();
        match humans.as_slice() {
            [only] => only.clone(),
            _ => match ask_name(terminal, key_rx).await? {
                Some(me) => me,
                None => return Ok(None),
            },
        }
    };

    let mut note = None;
    if space.participant(me.as_str()).is_err() {
        space.add_participant(me.clone(), ParticipantKind::Human)?;
        note = Some(format!("registered you ('{me}') in this space"));
    }
    if home::load_identity().is_none() {
        let _ = home::save_identity(&me);
    }
    Ok(Some((space, me, note)))
}

async fn ask_name(
    terminal: &mut Terminal<CrosstermBackend<Stdout>>,
    key_rx: &mut mpsc::Receiver<KeyEvent>,
) -> Result<Option<Name>> {
    let mut input = tui_textarea::TextArea::default();
    let mut error: Option<String> = None;
    loop {
        terminal.draw(|f| ui::wizard::draw_ask_name(f, &input, error.as_deref()))?;
        let Some(key) = key_rx.recv().await else {
            return Ok(None);
        };
        match key.code {
            KeyCode::Esc => return Ok(None),
            KeyCode::Enter => match Name::new(input.lines().join("").trim()) {
                Ok(name) => return Ok(Some(name)),
                Err(e) => error = Some(e.to_string()),
            },
            _ => {
                input.input(key);
            }
        }
    }
}

async fn main_loop(
    terminal: &mut Terminal<CrosstermBackend<Stdout>>,
    key_rx: &mut mpsc::Receiver<KeyEvent>,
    space: Space,
    me: Name,
    note: Option<String>,
) -> Result<()> {
    // the watcher must stay alive for the loop's lifetime
    let (_watcher, mut watch_rx) = watch::spawn(&space.substrate_dir())?;
    let mut tick = tokio::time::interval(Duration::from_millis(250));

    let mut app = App::new(space, me)?;
    if let Some(note) = note {
        app.flash(note);
    }

    loop {
        terminal.draw(|f| ui::render(f, &mut app))?;

        tokio::select! {
            key = key_rx.recv() => {
                let Some(key) = key else {
                    break; // input thread exited: terminal is gone, so are we
                };
                match app.handle_key(key) {
                    Some(Action::Quit) => break,
                    Some(Action::Reload) => {
                        if let Err(e) = app.reload() {
                            app.flash(e.to_string());
                        }
                    }
                    Some(Action::Submit(text)) => submit(&mut app, &text),
                    None => {}
                }
            }
            Some(()) = watch_rx.recv() => {
                if let Err(e) = app.reload() {
                    app.flash(e.to_string());
                }
            }
            _ = tick.tick() => {
                app.tick();
            }
        }
    }
    Ok(())
}

/// Execute a submitted input line: a slash command or a plain entry.
fn submit(app: &mut App, text: &str) {
    let Some(view) = &app.view else { return };
    let thread = view.name.clone();

    let outcome: anyhow::Result<String> = if let Some(first) = text.trim().strip_prefix('/') {
        let _ = first;
        run_command(app, &thread, text)
    } else {
        write_entry(app, &thread, text)
    };

    match outcome {
        Ok(message) => {
            if let Some(view) = &mut app.view {
                view.stick_bottom = true;
            }
            if let Err(e) = app.reload() {
                app.flash(e.to_string());
            } else {
                app.flash(message);
            }
        }
        Err(e) => {
            // don't eat what they typed
            app.restore_input(text);
            app.flash(e.to_string());
        }
    }
}

fn write_entry(app: &App, thread: &Name, text: &str) -> anyhow::Result<String> {
    let written = turn::write_entry(&app.space, thread, &app.me, text)?;
    Ok(format!(
        "recorded{} — next: {}{}",
        if written.no_op { " (no-op)" } else { "" },
        written.next,
        if written.paused {
            " (you hold the floor)"
        } else {
            ""
        },
    ))
}

fn run_command(app: &App, thread: &Name, text: &str) -> anyhow::Result<String> {
    let command = commands::parse(text).map_err(anyhow::Error::msg)?;
    let is_moderator = app.is_moderator_of_open_thread();
    if !command.anyone_may() && !is_moderator {
        anyhow::bail!("you're not the moderator here");
    }

    let space = &app.space;
    match command {
        SlashCommand::Help => Ok(commands::HELP.to_string()),
        SlashCommand::Pass => {
            let written = turn::write_entry(space, thread, &app.me, "pass")?;
            Ok(format!("passed — next: {}", written.next))
        }
        SlashCommand::Topic(topic) => {
            turn::set_topic(space, thread, &topic)?;
            Ok(format!("topic set: {topic}"))
        }
        SlashCommand::Turns(names) => {
            let parsed: Vec<Name> = names
                .iter()
                .map(|n| Name::new(n))
                .collect::<substrate_core::Result<_>>()?;
            turn::reorder_turns(space, thread, &parsed)?;
            Ok(format!("turn order set: {}", names.join(" → ")))
        }
        SlashCommand::Quiet { name, turns } => {
            let name = Name::new(&name)?;
            turn::quiet(space, thread, &name, turns)?;
            Ok(format!("{name} quieted for {turns} turn(s)"))
        }
        SlashCommand::Unquiet(name) => {
            let name = Name::new(&name)?;
            turn::unquiet(space, thread, &name)?;
            Ok(format!("{name} may speak again"))
        }
        SlashCommand::Invite(name) => {
            let name = Name::new(&name)?;
            // same policy as the new-thread form: the moderator naming
            // someone IS the registration — unknown names become agents
            let registered = if space.participant(name.as_str()).is_err() {
                space.add_participant(name.clone(), ParticipantKind::Agent)?;
                " (registered as a new agent)"
            } else {
                ""
            };
            turn::invite(space, thread, &name)?;
            Ok(format!(
                "{name} joins the thread at the end of the round{registered}"
            ))
        }
        SlashCommand::Next(name) => {
            let name = Name::new(&name)?;
            turn::set_next(space, thread, &name)?;
            Ok(format!("the floor passes to {name}"))
        }
        SlashCommand::End => {
            turn::end_thread(space, thread)?;
            Ok("thread ended".to_string())
        }
        SlashCommand::Resume => {
            turn::resume_thread(space, thread)?;
            Ok("thread resumed — the floor is yours; say why the thread is back".to_string())
        }
    }
}

fn spawn_input_task(key_tx: mpsc::Sender<KeyEvent>) -> tokio::task::JoinHandle<()> {
    tokio::task::spawn_blocking(move || loop {
        // poll/read errors mean the terminal is gone (orphaned process,
        // dead pty). Exit, never swallow: an erroring poll stops blocking
        // for its timeout, and the swallowed-error loop spins a whole core.
        match event::poll(Duration::from_millis(50)) {
            Ok(true) => match event::read() {
                Ok(Event::Key(key)) => {
                    // ignore key release events (Windows/kitty protocols)
                    if key.kind == KeyEventKind::Press && key_tx.blocking_send(key).is_err() {
                        break;
                    }
                }
                Ok(_) => {}
                Err(_) => break,
            },
            Ok(false) => {}
            Err(_) => break,
        }
        if key_tx.is_closed() {
            break;
        }
    })
}

fn setup_terminal() -> Result<Terminal<CrosstermBackend<Stdout>>> {
    enable_raw_mode().context("failed to enable raw mode")?;
    let mut stdout = io::stdout();
    execute!(stdout, EnterAlternateScreen).context("failed to enter alternate screen")?;
    Ok(Terminal::new(CrosstermBackend::new(stdout))?)
}

fn restore_terminal(terminal: &mut Terminal<CrosstermBackend<Stdout>>) -> Result<()> {
    disable_raw_mode()?;
    execute!(terminal.backend_mut(), LeaveAlternateScreen)?;
    terminal.show_cursor()?;
    Ok(())
}

fn restore_terminal_basic() -> Result<()> {
    disable_raw_mode()?;
    execute!(io::stdout(), LeaveAlternateScreen)?;
    Ok(())
}
