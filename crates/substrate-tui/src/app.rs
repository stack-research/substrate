//! All TUI state in one struct; `handle_key` stays pure (no I/O) and returns
//! an `Action` for the run loop to execute. Coven-tui-v2's shape.

use std::collections::HashMap;
use std::time::Instant;

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers, MouseEvent, MouseEventKind};
use substrate_core::{
    thread::ThreadConfig, transcript, turn::TurnStatus, Entry, Name, ParticipantKind, Space,
};
use tui_textarea::TextArea;

const CTRL_C_WINDOW_MS: u128 = 1500;
const WHEEL_SCROLL_LINES: usize = 3;
pub const FLASH_SECS: u64 = 5;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Mode {
    List,
    Thread,
    NewThread,
}

pub const FORM_FIELDS: usize = 3; // name, topic, turns

pub struct NewConvForm {
    pub fields: [TextArea<'static>; FORM_FIELDS],
    pub focus: usize,
}

impl NewConvForm {
    fn new(space: &Space, me: &Name, kinds: &HashMap<Name, ParticipantKind>) -> Self {
        // canonicalize so a relative root (".") still yields the dir's name
        let root = space
            .root()
            .canonicalize()
            .unwrap_or_else(|_| space.root().to_path_buf());
        let default_name = substrate_core::home::label_for(&root);
        let default_turns = kinds
            .keys()
            .filter(|n| *n != me)
            .map(Name::as_str)
            .collect::<Vec<_>>()
            .join(", ");
        let field = |text: &str| {
            let mut t = TextArea::from([text.to_string()]);
            t.move_cursor(tui_textarea::CursorMove::End);
            t
        };
        NewConvForm {
            fields: [
                field(&default_name),
                TextArea::default(),
                field(&default_turns),
            ],
            focus: 1, // name and turns have sensible defaults; start at topic
        }
    }

    fn value(&self, i: usize) -> String {
        self.fields[i].lines().join(" ").trim().to_string()
    }
}

#[derive(Debug, Clone)]
pub enum Action {
    Quit,
    Reload,
    Submit(String),
    ToggleMouseCapture,
}

pub struct ThreadView {
    pub name: Name,
    pub config: ThreadConfig,
    pub entries: Vec<Entry>,
    /// Scroll offset in wrapped transcript lines from the top.
    pub scroll: usize,
    pub stick_bottom: bool,
}

pub struct App {
    pub space: Space,
    pub me: Name,
    pub kinds: HashMap<Name, ParticipantKind>,
    pub mode: Mode,
    pub summaries: Vec<TurnStatus>,
    pub list_index: usize,
    pub view: Option<ThreadView>,
    pub input: TextArea<'static>,
    pub form: Option<NewConvForm>,
    pub flash: Option<(String, Instant)>,
    pub last_ctrl_c: Option<Instant>,
    /// Transcript viewport height from the last render, for paging.
    pub viewport_height: usize,
}

impl App {
    pub fn new(space: Space, me: Name) -> anyhow::Result<Self> {
        let kinds = space
            .config()?
            .participants
            .into_iter()
            .map(|p| (p.name, p.kind))
            .collect();
        let mut app = App {
            space,
            me,
            kinds,
            mode: Mode::List,
            summaries: Vec::new(),
            list_index: 0,
            view: None,
            input: TextArea::default(),
            form: None,
            flash: None,
            last_ctrl_c: None,
            viewport_height: 10,
        };
        app.reload()?;
        Ok(app)
    }

    /// Refresh everything from disk: the thread list and, if open, the
    /// current view. Called on file-watch events and after our own writes.
    pub fn reload(&mut self) -> anyhow::Result<()> {
        let threads = self.space.list_threads()?;
        self.summaries = threads
            .iter()
            .filter_map(|c| substrate_core::turn::turn_status(&self.space, c).ok())
            .collect();
        if !self.summaries.is_empty() {
            self.list_index = self.list_index.min(self.summaries.len() - 1);
        }
        if let Some(view) = &mut self.view {
            view.config = ThreadConfig::load(&self.space, &view.name)?;
            view.entries = transcript::load_entries(&self.space, &view.name)?;
        }
        // refresh kinds too — participants can be added while we're open
        if let Ok(config) = self.space.config() {
            self.kinds = config
                .participants
                .into_iter()
                .map(|p| (p.name, p.kind))
                .collect();
        }
        Ok(())
    }

    pub fn open_selected(&mut self) -> anyhow::Result<()> {
        let Some(summary) = self.summaries.get(self.list_index) else {
            return Ok(());
        };
        let name = summary.thread.clone();
        self.view = Some(ThreadView {
            config: ThreadConfig::load(&self.space, &name)?,
            entries: transcript::load_entries(&self.space, &name)?,
            name,
            scroll: 0,
            stick_bottom: true,
        });
        self.mode = Mode::Thread;
        Ok(())
    }

    pub fn close_view(&mut self) {
        self.view = None;
        self.mode = Mode::List;
    }

    pub fn flash(&mut self, message: impl Into<String>) {
        self.flash = Some((message.into(), Instant::now()));
    }

    pub fn tick(&mut self) {
        if let Some((_, at)) = &self.flash {
            if at.elapsed().as_secs() >= FLASH_SECS {
                self.flash = None;
            }
        }
    }

    /// Put text back in the input box (a failed submit shouldn't eat it).
    pub fn restore_input(&mut self, text: &str) {
        self.input = TextArea::from(text.lines().map(String::from).collect::<Vec<_>>());
        self.input.move_cursor(tui_textarea::CursorMove::Bottom);
        self.input.move_cursor(tui_textarea::CursorMove::End);
    }

    fn take_input(&mut self) -> String {
        let text = self.input.lines().join("\n");
        self.input = TextArea::default();
        text
    }

    fn clear_input(&mut self) {
        self.input = TextArea::default();
    }

    fn scroll_up(&mut self, lines: usize) {
        if let Some(view) = &mut self.view {
            view.scroll = view.scroll.saturating_sub(lines.max(1));
            view.stick_bottom = false;
        }
    }

    fn scroll_down(&mut self, lines: usize) {
        if let Some(view) = &mut self.view {
            view.scroll += lines.max(1);
        }
    }

    fn scroll_top(&mut self) {
        if let Some(view) = &mut self.view {
            view.scroll = 0;
            view.stick_bottom = false;
        }
    }

    fn scroll_bottom(&mut self) {
        if let Some(view) = &mut self.view {
            view.stick_bottom = true;
        }
    }

    pub fn handle_key(&mut self, key: KeyEvent) -> Option<Action> {
        // double Ctrl-C quits from anywhere
        if key.code == KeyCode::Char('c') && key.modifiers.contains(KeyModifiers::CONTROL) {
            if let Some(last) = self.last_ctrl_c {
                if last.elapsed().as_millis() < CTRL_C_WINDOW_MS {
                    return Some(Action::Quit);
                }
            }
            self.last_ctrl_c = Some(Instant::now());
            self.flash("press ctrl-c again to quit");
            return None;
        }

        match self.mode {
            Mode::List => self.handle_list_key(key),
            Mode::Thread => self.handle_thread_key(key),
            Mode::NewThread => self.handle_form_key(key),
        }
    }

    fn handle_form_key(&mut self, key: KeyEvent) -> Option<Action> {
        let Some(form) = &mut self.form else {
            self.mode = Mode::List;
            return None;
        };
        match key.code {
            KeyCode::Esc => {
                self.form = None;
                self.mode = Mode::List;
            }
            KeyCode::Tab | KeyCode::Down => form.focus = (form.focus + 1) % FORM_FIELDS,
            KeyCode::BackTab | KeyCode::Up => {
                form.focus = (form.focus + FORM_FIELDS - 1) % FORM_FIELDS
            }
            KeyCode::Enter => {
                if let Err(e) = self.create_from_form() {
                    self.flash(e.to_string()); // form stays open, values intact
                }
            }
            _ => {
                form.fields[form.focus].input(key);
            }
        }
        None
    }

    fn create_from_form(&mut self) -> anyhow::Result<()> {
        let Some(form) = &self.form else {
            return Ok(());
        };
        let name = Name::new(&form.value(0))?;
        let topic = form.value(1);
        anyhow::ensure!(!topic.is_empty(), "a topic helps the room — add one");
        let turns: Vec<Name> = form
            .value(2)
            .split([',', ' '])
            .filter(|s| !s.is_empty())
            .map(Name::new)
            .collect::<substrate_core::Result<_>>()?;
        // names typed here that aren't registered yet become agents — the
        // moderator naming them is the registration
        let mut added = Vec::new();
        for turn_name in &turns {
            if self.space.participant(turn_name.as_str()).is_err() {
                self.space
                    .add_participant(turn_name.clone(), ParticipantKind::Agent)?;
                added.push(turn_name.as_str().to_string());
            }
        }
        substrate_core::thread::create_thread(&self.space, &name, &topic, &self.me, &turns)?;
        self.form = None;
        self.reload()?;
        if !added.is_empty() {
            self.flash(format!("registered new agent(s): {}", added.join(", ")));
        }
        // open the new room directly
        if let Some(i) = self.summaries.iter().position(|s| s.thread == name) {
            self.list_index = i;
            self.open_selected()?;
        }
        self.flash(format!("created '{name}' — you moderate"));
        Ok(())
    }

    fn handle_list_key(&mut self, key: KeyEvent) -> Option<Action> {
        match key.code {
            KeyCode::Char('q') => Some(Action::Quit),
            KeyCode::Char('r') => Some(Action::Reload),
            KeyCode::Char('n') => {
                self.form = Some(NewConvForm::new(&self.space, &self.me, &self.kinds));
                self.mode = Mode::NewThread;
                None
            }
            KeyCode::Up | KeyCode::Char('k') => {
                self.list_index = self.list_index.saturating_sub(1);
                None
            }
            KeyCode::Down | KeyCode::Char('j') => {
                if !self.summaries.is_empty() {
                    self.list_index = (self.list_index + 1).min(self.summaries.len() - 1);
                }
                None
            }
            KeyCode::Enter => {
                if let Err(e) = self.open_selected() {
                    self.flash(e.to_string());
                }
                None
            }
            _ => None,
        }
    }

    fn handle_thread_key(&mut self, key: KeyEvent) -> Option<Action> {
        match key.code {
            KeyCode::Esc => {
                self.close_view();
                None
            }
            KeyCode::F(2) => Some(Action::ToggleMouseCapture),
            KeyCode::Char('u') if key.modifiers.contains(KeyModifiers::CONTROL) => {
                self.clear_input();
                None
            }
            KeyCode::Enter
                if key
                    .modifiers
                    .intersects(KeyModifiers::ALT | KeyModifiers::SHIFT) =>
            {
                self.input.insert_newline();
                None
            }
            KeyCode::Enter => {
                let text = self.take_input();
                if text.trim().is_empty() {
                    None
                } else {
                    Some(Action::Submit(text))
                }
            }
            KeyCode::PageUp => {
                self.scroll_up((self.viewport_height.max(1) / 2).max(1));
                None
            }
            KeyCode::Up if key.modifiers.contains(KeyModifiers::CONTROL) => {
                self.scroll_up(1);
                None
            }
            KeyCode::PageDown => {
                // render clamps; hitting the bottom re-sticks
                self.scroll_down((self.viewport_height.max(1) / 2).max(1));
                None
            }
            KeyCode::Down if key.modifiers.contains(KeyModifiers::CONTROL) => {
                self.scroll_down(1);
                None
            }
            KeyCode::Home if key.modifiers.contains(KeyModifiers::CONTROL) => {
                self.scroll_top();
                None
            }
            KeyCode::End if key.modifiers.contains(KeyModifiers::CONTROL) => {
                self.scroll_bottom();
                None
            }
            _ => {
                self.input.input(key);
                None
            }
        }
    }

    pub fn handle_mouse(&mut self, mouse: MouseEvent) -> Option<Action> {
        if self.mode != Mode::Thread {
            return None;
        }
        match mouse.kind {
            MouseEventKind::ScrollUp => self.scroll_up(WHEEL_SCROLL_LINES),
            MouseEventKind::ScrollDown => self.scroll_down(WHEEL_SCROLL_LINES),
            _ => {}
        }
        None
    }

    pub fn is_moderator_of_open_thread(&self) -> bool {
        self.view
            .as_ref()
            .is_some_and(|v| v.config.moderator == self.me)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use substrate_core::{thread, ParticipantKind};
    use tempfile::TempDir;

    fn key(code: KeyCode) -> KeyEvent {
        KeyEvent::new(code, KeyModifiers::NONE)
    }

    fn test_app() -> (TempDir, App) {
        let dir = TempDir::new().unwrap();
        let space = Space::init(dir.path()).unwrap();
        for (name, kind) in [
            ("user-name", ParticipantKind::Human),
            ("claude-a", ParticipantKind::Agent),
        ] {
            space
                .add_participant(Name::new(name).unwrap(), kind)
                .unwrap();
        }
        thread::create_thread(
            &space,
            &Name::new("lab").unwrap(),
            "t",
            &Name::new("user-name").unwrap(),
            &[Name::new("claude-a").unwrap()],
        )
        .unwrap();
        let app = App::new(space, Name::new("user-name").unwrap()).unwrap();
        (dir, app)
    }

    #[test]
    fn list_navigation_and_open() {
        let (_dir, mut app) = test_app();
        assert_eq!(app.mode, Mode::List);
        assert_eq!(app.summaries.len(), 1);

        assert!(app.handle_key(key(KeyCode::Enter)).is_none());
        assert_eq!(app.mode, Mode::Thread);
        assert!(app.view.is_some());
        assert!(app.is_moderator_of_open_thread());

        assert!(app.handle_key(key(KeyCode::Esc)).is_none());
        assert_eq!(app.mode, Mode::List);
        assert!(app.view.is_none());
    }

    #[test]
    fn typing_and_submit() {
        let (_dir, mut app) = test_app();
        app.handle_key(key(KeyCode::Enter)); // open

        for c in "hello".chars() {
            app.handle_key(key(KeyCode::Char(c)));
        }
        // empty after newline-only input is not submitted
        let action = app.handle_key(key(KeyCode::Enter));
        assert!(matches!(action, Some(Action::Submit(t)) if t == "hello"));
        // input cleared after submit
        assert_eq!(app.input.lines().join(""), "");

        let action = app.handle_key(key(KeyCode::Enter));
        assert!(action.is_none(), "empty input should not submit");
    }

    #[test]
    fn multiline_input_and_ctrl_u_clear() {
        let (_dir, mut app) = test_app();
        app.handle_key(key(KeyCode::Enter)); // open

        for c in "hello".chars() {
            app.handle_key(key(KeyCode::Char(c)));
        }
        app.handle_key(KeyEvent::new(KeyCode::Enter, KeyModifiers::ALT));
        for c in "world".chars() {
            app.handle_key(key(KeyCode::Char(c)));
        }
        let action = app.handle_key(key(KeyCode::Enter));
        assert!(matches!(action, Some(Action::Submit(t)) if t == "hello\nworld"));

        for c in "draft".chars() {
            app.handle_key(key(KeyCode::Char(c)));
        }
        app.handle_key(KeyEvent::new(KeyCode::Char('u'), KeyModifiers::CONTROL));
        assert_eq!(app.input.lines().join(""), "");
    }

    #[test]
    fn thread_scroll_keys_move_the_view() {
        let (_dir, mut app) = test_app();
        app.handle_key(key(KeyCode::Enter)); // open
        app.viewport_height = 10;

        app.handle_key(key(KeyCode::PageDown));
        assert_eq!(app.view.as_ref().unwrap().scroll, 5);
        app.handle_key(KeyEvent::new(KeyCode::Up, KeyModifiers::CONTROL));
        assert_eq!(app.view.as_ref().unwrap().scroll, 4);
        assert!(!app.view.as_ref().unwrap().stick_bottom);
        app.handle_key(KeyEvent::new(KeyCode::Home, KeyModifiers::CONTROL));
        assert_eq!(app.view.as_ref().unwrap().scroll, 0);
        app.handle_key(KeyEvent::new(KeyCode::End, KeyModifiers::CONTROL));
        assert!(app.view.as_ref().unwrap().stick_bottom);
    }

    #[test]
    fn mouse_wheel_scrolls_thread_view() {
        let (_dir, mut app) = test_app();
        app.handle_key(key(KeyCode::Enter)); // open

        app.handle_mouse(MouseEvent {
            kind: MouseEventKind::ScrollDown,
            column: 0,
            row: 0,
            modifiers: KeyModifiers::NONE,
        });
        assert_eq!(app.view.as_ref().unwrap().scroll, WHEEL_SCROLL_LINES);

        app.handle_mouse(MouseEvent {
            kind: MouseEventKind::ScrollUp,
            column: 0,
            row: 0,
            modifiers: KeyModifiers::NONE,
        });
        assert_eq!(app.view.as_ref().unwrap().scroll, 0);
        assert!(!app.view.as_ref().unwrap().stick_bottom);
    }

    #[test]
    fn f2_toggles_mouse_capture_mode() {
        let (_dir, mut app) = test_app();
        app.handle_key(key(KeyCode::Enter)); // open

        assert!(matches!(
            app.handle_key(key(KeyCode::F(2))),
            Some(Action::ToggleMouseCapture)
        ));
    }

    #[test]
    fn double_ctrl_c_quits() {
        let (_dir, mut app) = test_app();
        let ctrl_c = KeyEvent::new(KeyCode::Char('c'), KeyModifiers::CONTROL);
        assert!(app.handle_key(ctrl_c).is_none());
        assert!(matches!(app.handle_key(ctrl_c), Some(Action::Quit)));
    }

    #[test]
    fn q_quits_from_list_only() {
        let (_dir, mut app) = test_app();
        assert!(matches!(
            app.handle_key(key(KeyCode::Char('q'))),
            Some(Action::Quit)
        ));
        app.handle_key(key(KeyCode::Enter)); // open thread
        assert!(app.handle_key(key(KeyCode::Char('q'))).is_none()); // 'q' is just typing now
        assert_eq!(app.input.lines().join(""), "q");
    }
}
