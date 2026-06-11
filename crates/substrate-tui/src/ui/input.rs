use ratatui::prelude::*;
use ratatui::widgets::{Block, Borders};

use crate::app::App;

pub fn render(frame: &mut Frame, app: &mut App, area: Rect) {
    let Some(view) = &app.view else { return };

    let my_turn = view.config.status == substrate_core::ThreadStatus::Active
        && view.config.current() == &app.me;
    let (title, color) = if view.config.status == substrate_core::ThreadStatus::Ended {
        (" thread ended ".to_string(), Color::DarkGray)
    } else if my_turn {
        (
            " your turn — enter sends, alt-enter for a new line ".to_string(),
            Color::Green,
        )
    } else {
        (
            format!(" waiting — {} holds the floor ", view.config.current()),
            Color::DarkGray,
        )
    };

    app.input.set_block(
        Block::default()
            .borders(Borders::ALL)
            .border_style(Style::default().fg(color))
            .title(title),
    );
    app.input.set_cursor_line_style(Style::default());
    frame.render_widget(&app.input, area);
}
