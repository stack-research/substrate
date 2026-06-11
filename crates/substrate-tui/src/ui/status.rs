use ratatui::prelude::*;

use crate::app::App;
use crate::commands;

pub fn render(frame: &mut Frame, app: &App, area: Rect) {
    let Some(view) = &app.view else { return };

    let line = if let Some((message, _)) = &app.flash {
        Line::from(Span::styled(
            format!(" {message}"),
            Style::default().fg(Color::Yellow),
        ))
    } else if app.is_moderator_of_open_thread() && view.config.is_paused() {
        Line::from(Span::styled(
            format!(" the room is paused for you · {}", commands::HELP),
            Style::default().fg(Color::Green),
        ))
    } else {
        let order: Vec<String> = view
            .config
            .turn_order
            .iter()
            .map(|name| {
                let mut label = name.to_string();
                if name == &view.config.moderator {
                    label.push('*');
                }
                if let Some(remaining) = view.config.quieted.get(name) {
                    label.push_str(&format!("(quiet {remaining})"));
                }
                if name == view.config.current() {
                    label = format!("[{label}]");
                }
                label
            })
            .collect();
        Line::from(Span::styled(
            format!(" order: {} · esc back · /help", order.join(" → ")),
            Style::default().fg(Color::DarkGray),
        ))
    };
    frame.render_widget(line, area);
}
