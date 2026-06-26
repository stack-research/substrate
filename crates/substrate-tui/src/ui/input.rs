use ratatui::prelude::*;
use ratatui::widgets::{Block, Borders};

use crate::app::App;

pub fn render(frame: &mut Frame, app: &mut App, area: Rect) {
    let Some(view) = &app.view else { return };

    let my_turn = view.config.status == substrate_core::ThreadStatus::Active
        && view.config.current() == &app.me;
    let moderator = app.is_moderator_of_open_thread();
    let ended = view.config.status == substrate_core::ThreadStatus::Ended;
    let (title, color) = if ended {
        (" input ".to_string(), Color::DarkGray)
    } else if my_turn {
        (" › ".to_string(), Color::Green)
    } else if moderator {
        (" mod ".to_string(), Color::Green)
    } else {
        ("   ".to_string(), Color::DarkGray)
    };

    app.input.set_block(
        Block::default()
            .borders(Borders::TOP)
            .border_style(Style::default().fg(color))
            .title(title),
    );
    app.input.set_cursor_line_style(Style::default());
    frame.render_widget(&app.input, area);
}
